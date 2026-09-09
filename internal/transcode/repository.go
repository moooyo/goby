package transcode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidRecord      = errors.New("invalid encoding record")
	ErrRecordNotFound     = errors.New("encoding record not found")
	ErrRecordConflict     = errors.New("encoding record conflicts with persisted state")
	ErrRecordUnauthorized = errors.New("encoding scope is no longer authorized")
)

// MaxPlanBytes bounds the persisted JSON representation of an encoding plan.
// Individual fields, including VOD cut points, have narrower semantic limits.
const MaxPlanBytes = 128 * 1024

// Repository persists bounded status projections, never a media file or token.
// Recover must run only after the server acquires exclusive catalog ownership.
type Repository interface {
	Recover(context.Context) error
	Create(context.Context, Record) error
	Update(context.Context, Record) error
}

type PGRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

var _ Repository = (*PGRepository)(nil)

// Recover terminates abandoned work without reviving jobs or changing playback
// state. It must not run concurrently with a live conversion manager.
func (r *PGRepository) Recover(ctx context.Context) error {
	if r == nil || r.pool == nil {
		return ErrInvalidRecord
	}
	_, err := r.pool.Exec(ctx, `UPDATE encoding_jobs SET state = 'interrupted',
		error_code = 'server_restart', updated_at = GREATEST(updated_at, clock_timestamp())
		WHERE state IN ('queued', 'running')`)
	if err != nil {
		return fmt.Errorf("recover interrupted encodings: %w", err)
	}
	return nil
}

// Create starts only a queued job. The caller already verified current library
// access and opened the indexed source; this repository independently locks and
// rechecks the account, Emby authentication session, and active playback owner.
// These locks serialize insertion with logout, disablement, and playback stop.
func (r *PGRepository) Create(ctx context.Context, record Record) error {
	plan, err := validateEncodingRecord(record)
	if err != nil || r == nil || r.pool == nil {
		return ErrInvalidRecord
	}
	if record.State != "queued" || record.OutputBytes != 0 || record.ErrorCode != "" {
		return ErrInvalidRecord
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin encoding creation: %w", err)
	}
	defer rollbackEncoding(tx)
	scope := record.Spec.Scope
	var id string
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND NOT is_disabled
		AND jsonb_typeof(policy) = 'object'
		AND (NOT (policy ? 'EnableMediaPlayback') OR policy -> 'EnableMediaPlayback' = 'true'::jsonb)
		FOR SHARE`, scope.UserID).Scan(&id); err != nil {
		return encodingAuthorizationError("authorize encoding account", err)
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE id = $1 AND user_id = $2
		AND device_id = $3 AND kind = 'emby' AND revoked_at IS NULL
		AND expires_at > clock_timestamp() FOR SHARE`, scope.AuthSessionID, scope.UserID, scope.DeviceID).
		Scan(&id); err != nil {
		return encodingAuthorizationError("authorize encoding authentication session", err)
	}
	// Catalog deletion locks the item before cascading to playback rows. Take
	// this key lock first as well, so the later item foreign-key check cannot
	// wait behind deletion while we hold a playback lock that deletion needs.
	if err := tx.QueryRow(ctx, `SELECT id FROM items WHERE id = $1 FOR KEY SHARE`, scope.ItemID).Scan(&id); err != nil {
		return encodingAuthorizationError("authorize encoding item existence", err)
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM play_sessions WHERE id = $1 AND user_id = $2
		AND auth_session_id = $3 AND device_id = $4 AND item_id = $5 AND media_source_id = $6
		AND state IN ('Prepared','Playing','Paused') AND expires_at > clock_timestamp() FOR SHARE`,
		scope.PlaySessionID, scope.UserID, scope.AuthSessionID, scope.DeviceID, scope.ItemID, scope.SourceID).
		Scan(&id); err != nil {
		return encodingAuthorizationError("authorize encoding playback session", err)
	}
	result, err := tx.Exec(ctx, `INSERT INTO encoding_jobs (id, user_id, auth_session_id, device_id,
		play_session_id, item_id, media_source_id, source_stamp, plan, state, output_bytes,
		error_code, created_at, updated_at, last_access_at)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12,$13,$14,$15
		FROM sessions authentication JOIN play_sessions play ON play.id = $5
		WHERE authentication.id = $3 AND authentication.expires_at > clock_timestamp()
		AND play.expires_at > clock_timestamp()`,
		record.ID, scope.UserID, scope.AuthSessionID, scope.DeviceID, scope.PlaySessionID,
		scope.ItemID, scope.SourceID, record.Spec.SourceStamp, plan, record.State, record.OutputBytes,
		record.ErrorCode, record.CreatedAt.UTC(), record.UpdatedAt.UTC(), record.LastAccessAt.UTC())
	if err != nil {
		var constraint *pgconn.PgError
		if errors.As(err, &constraint) && constraint.Code == "23505" {
			return ErrRecordConflict
		}
		return fmt.Errorf("insert encoding record: %w", err)
	}
	if result.RowsAffected() != 1 {
		// Time can pass while acquiring later locks even though the locked
		// account and session claims cannot change before this transaction ends.
		return ErrRecordUnauthorized
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit encoding creation: %w", err)
	}
	return nil
}

// Update fixes every ownership and plan dimension at insertion. Progress and
// access timestamps never regress. Repeating a terminal state can touch access
// time but cannot rewrite its result; a different terminal or active state is a
// conflict. No live-auth check is made here, allowing logout-triggered cleanup.
func (r *PGRepository) Update(ctx context.Context, record Record) error {
	plan, err := validateEncodingRecord(record)
	if err != nil || r == nil || r.pool == nil {
		return ErrInvalidRecord
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin encoding update: %w", err)
	}
	defer rollbackEncoding(tx)
	scope := record.Spec.Scope
	var current string
	var unchanged bool
	err = tx.QueryRow(ctx, `SELECT state,
		source_stamp = $8 AND plan = $9::jsonb AND created_at = $10
		FROM encoding_jobs WHERE id = $1 AND user_id = $2 AND auth_session_id = $3
		AND device_id = $4 AND play_session_id = $5 AND item_id = $6 AND media_source_id = $7
		FOR UPDATE`, record.ID, scope.UserID, scope.AuthSessionID, scope.DeviceID,
		scope.PlaySessionID, scope.ItemID, scope.SourceID, record.Spec.SourceStamp, plan, record.CreatedAt.UTC()).
		Scan(&current, &unchanged)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRecordNotFound
	}
	if err != nil {
		return fmt.Errorf("lock owned encoding record: %w", err)
	}
	if !unchanged || !encodingTransitionAllowed(current, record.State) {
		return ErrRecordConflict
	}
	terminal := current != "queued" && current != "running"
	_, err = tx.Exec(ctx, `UPDATE encoding_jobs SET state = $2,
		output_bytes = CASE WHEN $7 THEN output_bytes ELSE GREATEST(output_bytes, $3) END,
		error_code = CASE WHEN $7 THEN error_code ELSE $4 END,
		updated_at = GREATEST(updated_at, $5), last_access_at = GREATEST(last_access_at, $6)
		WHERE id = $1`, record.ID, record.State, record.OutputBytes, record.ErrorCode,
		record.UpdatedAt.UTC(), record.LastAccessAt.UTC(), terminal)
	if err != nil {
		return fmt.Errorf("update encoding status: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit encoding update: %w", err)
	}
	return nil
}

func encodingAuthorizationError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRecordUnauthorized
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func encodingTransitionAllowed(current, next string) bool {
	if current == next {
		return true
	}
	switch current {
	case "queued":
		return next == "running" || next == "failed" || next == "cancelled" || next == "interrupted"
	case "running":
		return next == "completed" || next == "failed" || next == "cancelled" || next == "interrupted"
	default:
		return false
	}
}

func validateEncodingRecord(record Record) ([]byte, error) {
	if !encodingHexID(record.ID) || record.OutputBytes < 0 || !encodingErrorCode(record.ErrorCode) {
		return nil, ErrInvalidRecord
	}
	switch record.State {
	case "queued", "running", "completed", "failed", "cancelled", "interrupted":
	default:
		return nil, ErrInvalidRecord
	}
	scope := record.Spec.Scope
	for _, value := range []string{scope.UserID, scope.AuthSessionID, scope.PlaySessionID, scope.ItemID, scope.SourceID, record.Spec.SourceStamp} {
		if !encodingIdentifier(value, false) {
			return nil, ErrInvalidRecord
		}
	}
	if !encodingIdentifier(scope.DeviceID, true) || record.CreatedAt.IsZero() ||
		!encodingTimestamp(record.CreatedAt) || !encodingTimestamp(record.UpdatedAt) || !encodingTimestamp(record.LastAccessAt) ||
		record.UpdatedAt.Before(record.CreatedAt) || record.LastAccessAt.Before(record.CreatedAt) {
		return nil, ErrInvalidRecord
	}
	if err := ValidatePlan(record.Spec.Plan); err != nil {
		return nil, ErrInvalidRecord
	}
	plan, err := json.Marshal(record.Spec.Plan)
	if err != nil || len(plan) > MaxPlanBytes {
		return nil, ErrInvalidRecord
	}
	return plan, nil
}

func encodingIdentifier(value string, empty bool) bool {
	return (empty || value != "") && len(value) <= 256 && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

func encodingTimestamp(value time.Time) bool {
	return value.Year() >= 1 && value.Year() <= 9999 && value.UTC().Year() >= 1 && value.UTC().Year() <= 9999
}

func encodingHexID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func encodingErrorCode(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '_' {
			return false
		}
	}
	return true
}

func rollbackEncoding(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

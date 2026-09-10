package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/moooyo/goby/internal/activity"
)

const (
	ManagedSessionDefaultLimit  = 50
	MaxManagedSessions          = 200
	MaxManagedSessionStartIndex = 2147483647
	ManagedSessionActive        = "active"
	ManagedSessionRevoked       = "revoked"
	ManagedSessionDisabled      = "disabled"
	ManagedSessionExpired       = "expired"
	ManagedSessionAll           = "all"
)

var ErrManagedSessionNotFound = errors.New("authentication session not found")

// ManagedSession contains only administrator-visible authentication metadata.
// Active means the login remains authorized; it does not mean the client is
// online or playing. Status precedence is revoked, disabled, expired, active.
// Disabled includes an admin login whose account no longer has the admin role;
// UserIsDisabled and UserIsAdministrator distinguish these account conditions.
type ManagedSession struct {
	SessionID           string
	UserID              string
	UserName            string
	UserIsAdministrator bool
	UserIsDisabled      bool
	Kind                string
	Client              Client
	CreatedAt           time.Time
	LastSeenAt          time.Time
	ExpiresAt           time.Time
	RevokedAt           *time.Time
	Status              string
	IsCurrent           bool
}

// ManagedSessionFilter selects login records independently of client presence.
// Empty Kind includes both login kinds. Empty Status defaults to active; all
// includes historical and currently unauthorized records. SearchTerm is a
// literal, case-insensitive substring and is never interpreted as a SQL pattern.
type ManagedSessionFilter struct {
	UserID     string
	Kind       string
	Status     string
	DeviceID   string
	SearchTerm string
	StartIndex int
	Limit      int
}

// ManagedSessionsPage counts and selects records from one statement snapshot.
// Creation time and session ID define a deterministic order that does not move
// when client activity updates LastSeenAt. Separate requests have new snapshots.
type ManagedSessionsPage struct {
	Items            []ManagedSession
	TotalRecordCount int64
	StartIndex       int
	Limit            int
}

// ManagedSessionRevocation is returned only after durable revocation commits.
// The caller may then disconnect sockets and retire conversion work using the
// trusted session ID. Repeated revocation preserves the original RevokedAt.
type ManagedSessionRevocation struct {
	SessionID             string
	UserID                string
	Kind                  string
	RevokedAt             time.Time
	CurrentSessionRevoked bool
}

// ManagedSessionValidationError contains safe field messages for native forms.
type ManagedSessionValidationError struct {
	Fields map[string]string
}

func (e *ManagedSessionValidationError) Error() string {
	return "invalid managed session input"
}

func (e *ManagedSessionValidationError) Unwrap() error {
	return ErrInvalidInput
}

func validateManagedSessionFilter(filter ManagedSessionFilter) (ManagedSessionFilter, error) {
	fields := make(map[string]string)
	if filter.UserID != "" && !validRevalidationID(filter.UserID) {
		fields["UserId"] = "user ID must contain at most 256 UTF-8 bytes without surrounding whitespace or controls"
	}
	if filter.Kind != "" && filter.Kind != "admin" && filter.Kind != "emby" {
		fields["Kind"] = "session kind must be admin or emby"
	}
	if filter.Status == "" {
		filter.Status = ManagedSessionActive
	}
	switch filter.Status {
	case ManagedSessionActive, ManagedSessionRevoked, ManagedSessionDisabled, ManagedSessionExpired, ManagedSessionAll:
	default:
		fields["Status"] = "session status must be active, revoked, disabled, expired, or all"
	}
	if err := validateClient(Client{DeviceID: filter.DeviceID}); err != nil {
		fields["DeviceId"] = "device ID must contain at most 256 UTF-8 bytes and no null bytes"
	}
	if !utf8.ValidString(filter.SearchTerm) || len(filter.SearchTerm) > maxClientFieldBytes || strings.ContainsRune(filter.SearchTerm, '\x00') {
		fields["SearchTerm"] = "search term must contain at most 256 UTF-8 bytes and no null bytes"
	}
	if filter.StartIndex < 0 || filter.StartIndex > MaxManagedSessionStartIndex {
		fields["StartIndex"] = "start index must be between 0 and 2147483647"
	}
	if filter.Limit == 0 {
		filter.Limit = ManagedSessionDefaultLimit
	}
	if filter.Limit < 1 || filter.Limit > MaxManagedSessions {
		fields["Limit"] = "limit must be between 1 and 200"
	}
	if len(fields) != 0 {
		return ManagedSessionFilter{}, &ManagedSessionValidationError{Fields: fields}
	}
	return filter, nil
}

// ListManagedSessions accepts only a trusted principal previously authenticated
// as an admin. Both authorization and the list run inside the same transaction.
func (s *Store) ListManagedSessions(ctx context.Context, actor Principal, filter ManagedSessionFilter) (ManagedSessionsPage, error) {
	filter, err := validateManagedSessionFilter(filter)
	if err != nil {
		return ManagedSessionsPage{}, err
	}
	if !validManagedActor(actor) {
		return ManagedSessionsPage{}, ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedSessionsPage{}, fmt.Errorf("begin managed session list: %w", err)
	}
	defer rollback(tx)
	// Follow account-before-authentication order while keeping the actor's role
	// and authentication row fixed until the list and final check complete.
	var id string
	err = tx.QueryRow(ctx, "SELECT id FROM users WHERE id = $1 FOR SHARE", actor.User.ID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedSessionsPage{}, ErrUnauthorized
	}
	if err != nil {
		return ManagedSessionsPage{}, fmt.Errorf("lock managed session reader account: %w", err)
	}
	err = tx.QueryRow(ctx, "SELECT id FROM sessions WHERE id = $1 AND user_id = $2 FOR SHARE", actor.SessionID, actor.User.ID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedSessionsPage{}, ErrUnauthorized
	}
	if err != nil {
		return ManagedSessionsPage{}, fmt.Errorf("lock managed session reader authentication: %w", err)
	}
	if err := authorizeManagedSessionActor(ctx, tx, actor, nil); err != nil {
		return ManagedSessionsPage{}, err
	}
	// The count and page must share one snapshot even under READ COMMITTED.
	// LEFT JOIN retains the total when the requested page is empty. One captured
	// clock value also gives every status decision the same expiration boundary.
	rows, err := tx.Query(ctx, `WITH observation AS MATERIALIZED (
		SELECT clock_timestamp() AS observed_at
	), states AS MATERIALIZED (
		SELECT a.id AS session_id, u.id AS user_id, u.name AS user_name,
			u.is_administrator, u.is_disabled, a.kind, a.client_name, a.device_id,
			a.device_name, a.client_version, a.created_at, a.last_seen_at,
			a.expires_at, a.revoked_at,
			CASE WHEN a.revoked_at IS NOT NULL THEN 'revoked'
				WHEN u.is_disabled OR (a.kind = 'admin' AND NOT u.is_administrator) THEN 'disabled'
				WHEN a.expires_at <= observation.observed_at THEN 'expired'
				ELSE 'active' END AS status
		FROM sessions a JOIN users u ON u.id = a.user_id CROSS JOIN observation
		WHERE ($1 = '' OR a.user_id = $1) AND ($2 = '' OR a.kind = $2)
		AND ($3 = '' OR a.device_id = $3)
		AND ($4 = '' OR strpos(lower(u.name), lower($4)) > 0
			OR strpos(lower(a.client_name), lower($4)) > 0
			OR strpos(lower(a.device_id), lower($4)) > 0
			OR strpos(lower(a.device_name), lower($4)) > 0
			OR strpos(lower(a.client_version), lower($4)) > 0)
	), filtered AS MATERIALIZED (
		SELECT * FROM states WHERE $5 = 'all' OR status = $5
	), page AS (
		SELECT * FROM filtered ORDER BY created_at DESC, session_id DESC LIMIT $6 OFFSET $7
	)
	SELECT (SELECT count(*) FROM filtered), page.session_id, page.user_id,
		page.user_name, page.is_administrator, page.is_disabled, page.kind,
		page.client_name, page.device_id, page.device_name, page.client_version,
		page.created_at, page.last_seen_at, page.expires_at, page.revoked_at, page.status
	FROM (SELECT 1) anchor LEFT JOIN page ON true
	ORDER BY page.created_at DESC, page.session_id DESC`, filter.UserID, filter.Kind,
		filter.DeviceID, filter.SearchTerm, filter.Status, filter.Limit, filter.StartIndex)
	if err != nil {
		return ManagedSessionsPage{}, fmt.Errorf("list managed sessions: %w", err)
	}
	defer rows.Close()
	result := ManagedSessionsPage{Items: make([]ManagedSession, 0), StartIndex: filter.StartIndex, Limit: filter.Limit}
	for rows.Next() {
		var sessionID, userID, userName, kind, clientName, deviceID, deviceName, version, status pgtype.Text
		var isAdministrator, isDisabled pgtype.Bool
		var createdAt, lastSeenAt, expiresAt, revokedAt pgtype.Timestamptz
		if err := rows.Scan(&result.TotalRecordCount, &sessionID, &userID, &userName,
			&isAdministrator, &isDisabled, &kind, &clientName, &deviceID, &deviceName,
			&version, &createdAt, &lastSeenAt, &expiresAt, &revokedAt, &status); err != nil {
			return ManagedSessionsPage{}, fmt.Errorf("read managed session: %w", err)
		}
		if !sessionID.Valid {
			continue
		}
		session := ManagedSession{
			SessionID: sessionID.String, UserID: userID.String, UserName: userName.String,
			UserIsAdministrator: isAdministrator.Bool, UserIsDisabled: isDisabled.Bool,
			Kind: kind.String, Client: Client{Name: clientName.String, DeviceID: deviceID.String, Device: deviceName.String, Version: version.String},
			CreatedAt: createdAt.Time, LastSeenAt: lastSeenAt.Time, ExpiresAt: expiresAt.Time, Status: status.String,
			IsCurrent: sessionID.String == actor.SessionID && userID.String == actor.User.ID,
		}
		if revokedAt.Valid {
			value := revokedAt.Time
			session.RevokedAt = &value
		}
		result.Items = append(result.Items, session)
	}
	if err := rows.Err(); err != nil {
		return ManagedSessionsPage{}, fmt.Errorf("read managed session list: %w", err)
	}
	rows.Close()
	if err := authorizeManagedSessionActor(ctx, tx, actor, nil); err != nil {
		return ManagedSessionsPage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedSessionsPage{}, fmt.Errorf("commit managed session list: %w", err)
	}
	return result, nil
}

// RevokeManagedSession revokes exactly one authentication session by immutable
// ID. It never deletes history, changes account revisions, or prevents a later
// login on the same device. An unknown target is reported only after the actor
// has been reauthorized. Self-revocation is a successful committed mutation.
func (s *Store) RevokeManagedSession(ctx context.Context, actor Principal, id string) (ManagedSessionRevocation, error) {
	if !validRevalidationID(id) {
		return ManagedSessionRevocation{}, &ManagedSessionValidationError{Fields: map[string]string{
			"Id": "session ID must contain between 1 and 256 UTF-8 bytes without surrounding whitespace or controls",
		}}
	}
	if !validManagedActor(actor) {
		return ManagedSessionRevocation{}, ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedSessionRevocation{}, fmt.Errorf("begin managed session revocation: %w", err)
	}
	defer rollback(tx)
	// Share M5a's mutation lock before any row lock. Reciprocal administrator
	// revocations and account mutations therefore cannot acquire crossed locks.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", managedUsersLockID); err != nil {
		return ManagedSessionRevocation{}, fmt.Errorf("lock managed session mutations: %w", err)
	}
	var targetUserID string
	err = tx.QueryRow(ctx, "SELECT user_id FROM sessions WHERE id = $1 AND kind IN ('admin', 'emby')", id).Scan(&targetUserID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ManagedSessionRevocation{}, fmt.Errorf("read managed session owner: %w", err)
	}
	// All accounts precede all authentication rows, both in deterministic ID
	// order. The owner lookup does not retain a session lock ahead of accounts.
	rows, err := tx.Query(ctx, "SELECT id FROM users WHERE id = ANY($1::text[]) ORDER BY id FOR UPDATE", []string{actor.User.ID, targetUserID})
	if err != nil {
		return ManagedSessionRevocation{}, fmt.Errorf("lock managed session accounts: %w", err)
	}
	for rows.Next() {
		var lockedID string
		if err := rows.Scan(&lockedID); err != nil {
			rows.Close()
			return ManagedSessionRevocation{}, fmt.Errorf("read locked managed session account: %w", err)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ManagedSessionRevocation{}, fmt.Errorf("read locked managed session accounts: %w", err)
	}
	rows, err = tx.Query(ctx, `SELECT id, user_id, kind, revoked_at FROM sessions
		WHERE id = ANY($1::text[]) AND kind IN ('admin', 'emby') ORDER BY id FOR UPDATE`, []string{actor.SessionID, id})
	if err != nil {
		return ManagedSessionRevocation{}, fmt.Errorf("lock managed authentication sessions: %w", err)
	}
	var result ManagedSessionRevocation
	var targetRevokedAt *time.Time
	for rows.Next() {
		var sessionID, userID, kind string
		var revokedAt *time.Time
		if err := rows.Scan(&sessionID, &userID, &kind, &revokedAt); err != nil {
			rows.Close()
			return ManagedSessionRevocation{}, fmt.Errorf("read locked authentication session: %w", err)
		}
		if sessionID == id && userID == targetUserID {
			result.SessionID, result.UserID, result.Kind = sessionID, userID, kind
			targetRevokedAt = revokedAt
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ManagedSessionRevocation{}, fmt.Errorf("read locked authentication sessions: %w", err)
	}
	if err := authorizeManagedSessionActor(ctx, tx, actor, nil); err != nil {
		return ManagedSessionRevocation{}, err
	}
	if result.SessionID == "" {
		return ManagedSessionRevocation{}, ErrManagedSessionNotFound
	}
	if targetRevokedAt != nil {
		result.RevokedAt = *targetRevokedAt
	} else {
		if err := tx.QueryRow(ctx, `UPDATE sessions SET revoked_at = clock_timestamp()
			WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL RETURNING revoked_at`, result.SessionID, result.UserID).Scan(&result.RevokedAt); err != nil {
			return ManagedSessionRevocation{}, fmt.Errorf("revoke managed authentication session: %w", err)
		}
		auditActor, err := identityActivityActor(actor)
		if err != nil {
			return ManagedSessionRevocation{}, err
		}
		if err := activity.Record(ctx, tx, activity.Event{
			Action: activity.ActionSessionRevoked, Source: activity.SourceNative, Actor: auditActor,
			Resource: activity.Resource{Kind: activity.ResourceSession, ID: result.SessionID},
			Count:    1,
		}); err != nil {
			return ManagedSessionRevocation{}, err
		}
	}
	result.CurrentSessionRevoked = result.SessionID == actor.SessionID && result.UserID == actor.User.ID
	var expectedSelfRevocation *time.Time
	if result.CurrentSessionRevoked {
		expectedSelfRevocation = &result.RevokedAt
	}
	// A queued or slow mutation must not commit using an expired actor. The
	// only revocation accepted here is this transaction's exact self-revocation;
	// the preceding check required the same locked actor to be unrevoked.
	if err := authorizeManagedSessionActor(ctx, tx, actor, expectedSelfRevocation); err != nil {
		return ManagedSessionRevocation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedSessionRevocation{}, fmt.Errorf("commit managed session revocation: %w", err)
	}
	return result, nil
}

// Call only after locking the actor's account and authentication row. Client
// role flags and expiration snapshots cannot authorize administrator access.
func authorizeManagedSessionActor(ctx context.Context, tx pgx.Tx, actor Principal, expectedSelfRevocation *time.Time) error {
	var authorized bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM sessions a JOIN users u ON u.id = a.user_id
		WHERE a.id = $1 AND a.user_id = $2 AND a.kind = 'admin'
		AND u.is_administrator AND NOT u.is_disabled AND a.expires_at > clock_timestamp()
		AND (a.revoked_at IS NULL OR ($3::timestamptz IS NOT NULL AND a.revoked_at = $3))
	)`, actor.SessionID, actor.User.ID, expectedSelfRevocation).Scan(&authorized)
	if err != nil {
		return fmt.Errorf("authorize managed session actor: %w", err)
	}
	if !authorized {
		return ErrUnauthorized
	}
	return nil
}

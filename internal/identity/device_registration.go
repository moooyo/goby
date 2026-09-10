package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type loginIssue struct {
	userID       string
	passwordHash string
	token        string
	digest       []byte
	sessionID    string
	client       Client
	kind         string
	lifetime     time.Duration
	peerIP       string
}

func (s *Store) issueLogin(ctx context.Context, issue loginIssue) (Credentials, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Credentials{}, fmt.Errorf("begin authentication issuance: %w", err)
	}
	defer rollback(tx)
	register := issue.kind == "emby" && issue.client.DeviceID != ""
	if register {
		// This precedes the account lock also used by disable/password changes.
		// Device deletion takes the same advisory lock before every account row.
		if err := lockDeviceRegistration(ctx, tx, issue.client.DeviceID); err != nil {
			return Credentials{}, err
		}
	}
	user, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users
		WHERE id = $1 AND password_hash = $2 AND NOT is_disabled AND ($3 <> 'admin' OR is_administrator) FOR SHARE`,
		issue.userID, issue.passwordHash, issue.kind))
	if errors.Is(err, pgx.ErrNoRows) {
		return Credentials{}, ErrInvalidCredentials
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("revalidate authentication account: %w", err)
	}
	var generation *int64
	var customName *string
	if register {
		var id int64
		err = tx.QueryRow(ctx, `INSERT INTO devices
			(reported_device_id, reported_name, app_name, app_version, last_user_id, ip_address)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (reported_device_id) WHERE deleted_at IS NULL DO UPDATE SET
			reported_name = EXCLUDED.reported_name, app_name = EXCLUDED.app_name, app_version = EXCLUDED.app_version,
			last_user_id = EXCLUDED.last_user_id, last_seen_at = GREATEST(devices.last_seen_at, clock_timestamp()),
			ip_address = CASE WHEN EXCLUDED.ip_address <> '' THEN EXCLUDED.ip_address ELSE devices.ip_address END
			RETURNING id, custom_name`, issue.client.DeviceID, issue.client.Device, issue.client.Name,
			issue.client.Version, user.ID, issue.peerIP).Scan(&id, &customName)
		if err != nil {
			return Credentials{}, fmt.Errorf("register ordinary device: %w", err)
		}
		generation = &id
	}
	var createdAt, expiresAt time.Time
	err = tx.QueryRow(ctx, `WITH issued_at AS MATERIALIZED (SELECT clock_timestamp() AS instant)
		INSERT INTO sessions (id, user_id, token_hash, kind, client_name, device_id, device_name, client_version,
			created_at, last_seen_at, expires_at, device_registry_id)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, instant, instant,
			instant + ($9::bigint * interval '1 second'), $10 FROM issued_at RETURNING created_at, expires_at`,
		issue.sessionID, user.ID, issue.digest, issue.kind, issue.client.Name, issue.client.DeviceID,
		issue.client.Device, issue.client.Version, int64(issue.lifetime/time.Second), generation).Scan(&createdAt, &expiresAt)
	if err != nil {
		return Credentials{}, fmt.Errorf("create authentication session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Credentials{}, fmt.Errorf("commit authentication issuance: %w", err)
	}
	client := issue.client
	if customName != nil {
		client.Device = *customName
	}
	return Credentials{User: user, Token: issue.token, SessionID: issue.sessionID, CreatedAt: createdAt,
		ExpiresAt: expiresAt, Client: client}, nil
}

// touchOrdinaryDevice runs only after the account and credential are locked.
// The authenticated database row supplies raw metadata. A principal's projected
// custom name or caller-controlled Client fields cannot overwrite reported data.
// No device advisory, additional account, or credential lock may be acquired here.
func touchOrdinaryDevice(ctx context.Context, tx pgx.Tx, principal Principal, peerIP string) error {
	if principal.Kind != "emby" || principal.IsApplicationKey() {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE devices d SET reported_name = a.device_name, app_name = a.client_name,
		app_version = a.client_version, last_user_id = a.user_id,
		last_seen_at = GREATEST(d.last_seen_at, clock_timestamp()),
		ip_address = CASE WHEN $3 <> '' THEN $3 ELSE d.ip_address END
		FROM sessions a WHERE a.id = $1 AND a.user_id = $2 AND a.kind = 'emby'
		AND a.device_registry_id = d.id AND d.id > 1 AND d.deleted_at IS NULL AND a.revoked_at IS NULL
		AND a.expires_at > clock_timestamp() AND (d.reported_name IS DISTINCT FROM a.device_name
		 OR d.app_name IS DISTINCT FROM a.client_name OR d.app_version IS DISTINCT FROM a.client_version
		 OR d.last_user_id IS DISTINCT FROM a.user_id OR ($3 <> '' AND d.ip_address IS DISTINCT FROM $3)
		 OR d.last_seen_at <= clock_timestamp() - ($4::bigint * interval '1 second'))`,
		principal.SessionID, principal.User.ID, peerIP, int64(ClientSessionTouchInterval/time.Second))
	if err != nil {
		return fmt.Errorf("record ordinary device activity: %w", err)
	}
	return nil
}

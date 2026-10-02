package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// readSteadyApplicationClient checks current persisted authority and every
// activity predicate using shared row locks. A missing binding or due mutation
// returns false only after releasing all locks; callers restart an exclusive
// transaction rather than upgrading competing readers. A nil requested client
// touches an existing context and never trusts projected client metadata.
func (s *Store) readSteadyApplicationClient(ctx context.Context, previous Principal, requested *Client) (Principal, bool, error) {
	if !previous.IsApplicationKey() || !validRevalidationID(previous.SessionID) || !validRevalidationID(previous.ClientSessionID) {
		return Principal{}, false, ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Principal{}, false, fmt.Errorf("begin application client activity check: %w", err)
	}
	defer rollback(tx)
	var credentialID string
	err = tx.QueryRow(ctx, "SELECT id FROM sessions WHERE id = $1 FOR SHARE", previous.SessionID).Scan(&credentialID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, false, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, false, fmt.Errorf("lock application client credential for reading: %w", err)
	}
	// Evaluate authorization after the credential lock wait, including a
	// revocation committed while authentication's initial lookup was running.
	if err := CheckApplicationKey(ctx, tx, credentialID, false); err != nil {
		return Principal{}, false, err
	}
	var keyID int64
	err = tx.QueryRow(ctx, "SELECT id FROM application_keys WHERE credential_id = $1 FOR SHARE", credentialID).Scan(&keyID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && keyID != previous.ApplicationKeyID) {
		return Principal{}, false, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, false, fmt.Errorf("lock application client key for reading: %w", err)
	}
	client := Client{}
	if requested != nil {
		client = *requested
		var defaults Client
		err = tx.QueryRow(ctx, `SELECT c.client_name, c.device_id, c.device_name, c.client_version
			FROM application_key_clients c JOIN sessions a ON a.id = c.credential_id
			WHERE a.id = $1 AND c.client_name = a.client_name AND c.device_id = a.device_id`, credentialID).
			Scan(&defaults.Name, &defaults.DeviceID, &defaults.Device, &defaults.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return Principal{}, false, ErrUnauthorized
		}
		if err != nil {
			return Principal{}, false, fmt.Errorf("read default application client activity: %w", err)
		}
		if client.Name == "" {
			client.Name = defaults.Name
		}
		if client.DeviceID == "" {
			client.DeviceID = defaults.DeviceID
		}
		if client.Device == "" {
			client.Device = defaults.Device
		}
		if client.Version == "" {
			client.Version = defaults.Version
		}
	}
	principal := Principal{Kind: ApplicationKeyKind, SessionID: credentialID, ApplicationKeyID: keyID, PeerIP: previous.PeerIP}
	err = tx.QueryRow(ctx, `SELECT id, client_name, device_id, device_name, client_version, last_seen_at
		FROM application_key_clients WHERE credential_id = $1
		AND (($2 AND client_name = $3 AND device_id = $4) OR (NOT $2 AND id = $5)) FOR SHARE`,
		credentialID, requested != nil, client.Name, client.DeviceID, previous.ClientSessionID).
		Scan(&principal.ClientSessionID, &principal.Client.Name, &principal.Client.DeviceID,
			&principal.Client.Device, &principal.Client.Version, &principal.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		if requested != nil {
			return Principal{}, false, nil
		}
		return Principal{}, false, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, false, fmt.Errorf("read locked application client activity: %w", err)
	}
	if requested != nil && principal.Client != client {
		return Principal{}, false, nil
	}
	var deviceID int64
	err = tx.QueryRow(ctx, `SELECT d.id FROM application_key_devices d
		JOIN application_keys k ON k.reported_device_numeric_id = d.id
		WHERE k.credential_id = $1 FOR SHARE OF d`, credentialID).Scan(&deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, false, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, false, fmt.Errorf("lock application client device for reading: %w", err)
	}
	// Evaluate clock-based predicates only after the last possible row wait.
	var due bool
	err = tx.QueryRow(ctx, `SELECT c.last_seen_at <= clock_timestamp() - ($3::bigint * interval '1 second')
		OR a.last_seen_at <= clock_timestamp() - ($3::bigint * interval '1 second')
		OR a.device_name IS DISTINCT FROM c.device_name OR a.client_version IS DISTINCT FROM c.client_version
		OR k.last_used_at IS NULL OR k.last_used_at <= clock_timestamp() - ($3::bigint * interval '1 second')
		OR (d.deleted_at IS NULL AND (d.last_seen_at <= clock_timestamp() - ($3::bigint * interval '1 second')
			OR d.reported_name IS DISTINCT FROM c.device_name OR d.app_name IS DISTINCT FROM a.client_name
			OR d.app_version IS DISTINCT FROM c.client_version))
		FROM sessions a JOIN application_keys k ON k.credential_id = a.id
		JOIN application_key_clients c ON c.credential_id = a.id AND c.id = $2
		JOIN application_key_devices d ON d.id = k.reported_device_numeric_id
		WHERE a.id = $1`, credentialID, principal.ClientSessionID,
		int64(ClientSessionTouchInterval/time.Second)).Scan(&due)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, false, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, false, fmt.Errorf("check application client activity predicates: %w", err)
	}
	if due {
		return Principal{}, false, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return Principal{}, false, fmt.Errorf("commit application client activity check: %w", err)
	}
	return principal, true, nil
}

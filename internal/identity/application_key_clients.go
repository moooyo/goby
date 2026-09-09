package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ResolveEmbyForClient retains normal login identity and binds an application
// key to a persisted client context. A client/device pair never issues another
// bearer token or another row in the shared credential table.
func (s *Store) ResolveEmbyForClient(ctx context.Context, token string, client Client) (Principal, error) {
	principal, err := s.ResolveEmby(ctx, token)
	if err != nil || !principal.IsApplicationKey() || client == (Client{}) {
		return principal, err
	}
	if err := validateClient(client); err != nil {
		return Principal{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Principal{}, fmt.Errorf("begin application client binding: %w", err)
	}
	defer rollback(tx)
	// Every bind and activity write locks the shared credential before its
	// sidecar and client context. Revocation therefore retires all contexts.
	var credentialID string
	err = tx.QueryRow(ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", principal.SessionID).Scan(&credentialID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, fmt.Errorf("lock application client credential: %w", err)
	}
	if err := CheckApplicationKey(ctx, tx, credentialID, false); err != nil {
		return Principal{}, err
	}
	var keyID int64
	err = tx.QueryRow(ctx, "SELECT id FROM application_keys WHERE credential_id = $1 FOR UPDATE", credentialID).Scan(&keyID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && keyID != principal.ApplicationKeyID) {
		return Principal{}, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, fmt.Errorf("lock application client key: %w", err)
	}
	var defaults Client
	err = tx.QueryRow(ctx, `SELECT c.client_name, c.device_id, c.device_name, c.client_version
		FROM application_key_clients c JOIN sessions a ON a.id = c.credential_id
		WHERE a.id = $1 AND c.client_name = a.client_name AND c.device_id = a.device_id`, credentialID).
		Scan(&defaults.Name, &defaults.DeviceID, &defaults.Device, &defaults.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, fmt.Errorf("read default application client: %w", err)
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
	var clientSessionID string
	err = tx.QueryRow(ctx, `SELECT id FROM application_key_clients
		WHERE credential_id = $1 AND client_name = $2 AND device_id = $3`, credentialID, client.Name, client.DeviceID).Scan(&clientSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM application_key_clients WHERE credential_id = $1", credentialID).Scan(&count); err != nil {
			return Principal{}, fmt.Errorf("count application client contexts: %w", err)
		}
		if count >= MaxApplicationKeyClients {
			return Principal{}, fmt.Errorf("%w: application key client context limit reached", ErrInvalidInput)
		}
		clientSessionID, err = randomID()
		if err != nil {
			return Principal{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO application_key_clients
			(id, credential_id, client_name, device_id, device_name, client_version)
			VALUES ($1, $2, $3, $4, $5, $6)`, clientSessionID, credentialID,
			client.Name, client.DeviceID, client.Device, client.Version); err != nil {
			return Principal{}, fmt.Errorf("create application client context: %w", err)
		}
	} else if err != nil {
		return Principal{}, fmt.Errorf("find application client context: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE application_key_clients
		SET device_name = $2, client_version = $3,
		last_seen_at = CASE WHEN last_seen_at <= clock_timestamp() - ($4::bigint * interval '1 second')
			THEN clock_timestamp() ELSE last_seen_at END
		WHERE id = $1 AND (device_name IS DISTINCT FROM $2 OR client_version IS DISTINCT FROM $3
		OR last_seen_at <= clock_timestamp() - ($4::bigint * interval '1 second'))`,
		clientSessionID, client.Device, client.Version, int64(ClientSessionTouchInterval/time.Second)); err != nil {
		return Principal{}, fmt.Errorf("update application client metadata: %w", err)
	}
	principal.ClientSessionID = clientSessionID
	principal.Client = client
	if err := touchApplicationKeyUsage(ctx, tx, principal); err != nil {
		return Principal{}, err
	}
	principal, err = scanApplicationKeyPrincipal(tx.QueryRow(ctx, `SELECT k.id, a.id, c.id,
		c.client_name, c.device_id, c.device_name, c.client_version, c.last_seen_at
		FROM sessions a JOIN application_keys k ON k.credential_id = a.id
		JOIN application_key_clients c ON c.credential_id = a.id
		WHERE a.id = $1 AND c.id = $2 AND a.kind = 'application_key'
		AND a.user_id IS NULL AND a.expires_at IS NULL AND a.revoked_at IS NULL`, credentialID, clientSessionID))
	if err != nil {
		return Principal{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Principal{}, fmt.Errorf("commit application client binding: %w", err)
	}
	return principal, nil
}

func clientSessionIdentity(principal Principal) string {
	if principal.IsApplicationKey() {
		return principal.ClientSessionID
	}
	return principal.SessionID
}

// ResolveApplicationKeyPlaybackContext restores a media URL's existing client
// context only within its already authenticated application credential. A play
// ID is a correlation hint; it never authenticates the caller or transfers the
// principal to a user. Playback state and expiration remain the media layer's
// responsibility so its existing error semantics are preserved.
func (s *Store) ResolveApplicationKeyPlaybackContext(ctx context.Context, previous Principal, playID string) (Principal, error) {
	if !previous.IsApplicationKey() || !validRevalidationID(previous.SessionID) || !validRevalidationID(previous.ClientSessionID) {
		return Principal{}, ErrUnauthorized
	}
	if !validRevalidationID(playID) {
		return Principal{}, ErrNotFound
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return Principal{}, fmt.Errorf("begin application playback context: %w", err)
	}
	defer rollback(tx)
	if err := authorizeApplicationKeyActor(ctx, tx, previous, nil); err != nil {
		return Principal{}, err
	}
	principal, err := scanApplicationKeyPrincipal(tx.QueryRow(ctx, `SELECT k.id, a.id, c.id,
		c.client_name, c.device_id, c.device_name, c.client_version, c.last_seen_at
		FROM play_sessions p JOIN sessions a ON a.id = p.auth_session_id
		JOIN application_keys k ON k.credential_id = a.id
		JOIN application_key_clients c ON c.id = p.application_client_id AND c.credential_id = a.id
		WHERE p.id = $1 AND a.id = $2 AND k.id = $3 AND p.user_id IS NULL
		AND a.kind = 'application_key' AND a.user_id IS NULL AND a.expires_at IS NULL AND a.revoked_at IS NULL`,
		playID, previous.SessionID, previous.ApplicationKeyID))
	if err != nil && !errors.Is(err, ErrUnauthorized) {
		return Principal{}, err
	}
	if authErr := authorizeApplicationKeyActor(ctx, tx, previous, nil); authErr != nil {
		return Principal{}, authErr
	}
	if errors.Is(err, ErrUnauthorized) {
		return Principal{}, ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return Principal{}, fmt.Errorf("commit application playback context: %w", err)
	}
	return principal, nil
}

package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrApplicationKeyDeviceRemoved preserves a known shared-device family's
// ownership while retaining not-found behavior. Compatibility adapters must not
// fall through to an ordinary device that reports the same identifier.
var ErrApplicationKeyDeviceRemoved = fmt.Errorf("%w: application server device generation removed", ErrDeviceNotFound)

// registerApplicationKeyDevice runs under the key-management advisory lock.
// The first generation preserves the reserved schema-16 identity. Removed
// generations remain as history, so only subsequent generations use the shared
// ordinary-device sequence and no old numeric identifier can be reused.
func registerApplicationKeyDevice(ctx context.Context, tx pgx.Tx, client Client, peerIP string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM application_key_devices
		WHERE reported_device_id = $1 AND deleted_at IS NULL FOR UPDATE`, client.DeviceID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO application_key_devices
			(id, reported_device_id, reported_name, app_name, app_version, ip_address)
			VALUES (CASE WHEN EXISTS (SELECT 1 FROM application_key_devices)
				THEN nextval('devices_id_seq') ELSE 1 END, $1, $2, $3, $4, $5) RETURNING id`,
			client.DeviceID, client.Device, client.Name, client.Version, peerIP).Scan(&id)
		if err != nil {
			return 0, fmt.Errorf("register application server device: %w", err)
		}
		return id, nil
	}
	if err != nil {
		return 0, fmt.Errorf("find application server device: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE application_key_devices SET reported_name = $2,
		app_name = $3, app_version = $4, ip_address = $5, last_seen_at = clock_timestamp(),
		revision = revision + CASE WHEN reported_name IS DISTINCT FROM $2 OR app_name IS DISTINCT FROM $3
			OR app_version IS DISTINCT FROM $4 OR ip_address IS DISTINCT FROM $5 THEN 1 ELSE 0 END
		WHERE id = $1`, id, client.Device, client.Name, client.Version, peerIP); err != nil {
		return 0, fmt.Errorf("refresh application server device: %w", err)
	}
	return id, nil
}

// touchApplicationKeyDeviceUsage must follow the caller's credential and key
// sidecar locks. It updates only the existing generation; activity can neither
// revive a removed device nor move a credential into a replacement generation.
func touchApplicationKeyDeviceUsage(ctx context.Context, tx pgx.Tx, principal Principal) error {
	if !principal.IsApplicationKey() {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE application_key_devices d SET reported_name = c.device_name,
		app_name = a.client_name, app_version = c.client_version,
		last_seen_at = CASE WHEN d.last_seen_at <= clock_timestamp() - ($3::bigint * interval '1 second')
			THEN clock_timestamp() ELSE d.last_seen_at END,
		revision = d.revision + CASE WHEN d.reported_name IS DISTINCT FROM c.device_name
			OR d.app_name IS DISTINCT FROM a.client_name OR d.app_version IS DISTINCT FROM c.client_version THEN 1 ELSE 0 END
		FROM application_keys k JOIN sessions a ON a.id = k.credential_id
		JOIN application_key_clients c ON c.credential_id = a.id AND c.id = $2
		WHERE a.id = $1 AND a.revoked_at IS NULL AND d.id = k.reported_device_numeric_id AND d.deleted_at IS NULL
		AND (d.reported_name IS DISTINCT FROM c.device_name OR d.app_name IS DISTINCT FROM a.client_name
			OR d.app_version IS DISTINCT FROM c.client_version
			OR d.last_seen_at <= clock_timestamp() - ($3::bigint * interval '1 second'))`,
		principal.SessionID, principal.ClientSessionID, int64(ClientSessionTouchInterval/time.Second))
	if err != nil {
		return fmt.Errorf("record application server device activity: %w", err)
	}
	return nil
}

func findApplicationKeyDeviceGeneration(ctx context.Context, tx pgx.Tx, reference deviceReference) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM application_key_devices
		WHERE ($1::bigint > 0 AND id = $1)
		OR ($1::bigint = 0 AND reported_device_id = $2)
		ORDER BY (deleted_at IS NULL) DESC, id DESC LIMIT 1`, reference.id, reference.reported).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrDeviceNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("find application server device generation: %w", err)
	}
	return id, nil
}

// Management locks precede every account, credential, sidecar, and device lock.
// In particular, a key deleting its own shared device never holds its client
// sidecar while waiting for a different key credential in the same generation.
func (s *Store) beginApplicationKeyDeviceOperation(ctx context.Context, actor Principal, reference deviceReference, mutate bool) (pgx.Tx, int64, *time.Time, error) {
	if !validDeviceActor(actor, false) {
		return nil, 0, nil, ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, 0, nil, fmt.Errorf("begin application server device operation: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			rollback(tx)
		}
	}()
	if err := authorizeDeviceActor(ctx, tx, actor, false, nil); err != nil {
		return nil, 0, nil, err
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", managedUsersLockID); err != nil {
		return nil, 0, nil, fmt.Errorf("lock application server device management: %w", err)
	}
	if err := authorizeDeviceActor(ctx, tx, actor, false, nil); err != nil {
		return nil, 0, nil, err
	}
	if actor.User.ID != "" {
		var account string
		if err := tx.QueryRow(ctx, "SELECT id FROM users WHERE id = $1 FOR SHARE", actor.User.ID).Scan(&account); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, 0, nil, ErrUnauthorized
			}
			return nil, 0, nil, fmt.Errorf("lock application server device actor account: %w", err)
		}
	}
	id, err := findApplicationKeyDeviceGeneration(ctx, tx, reference)
	if err != nil {
		return nil, 0, nil, err
	}
	locking := " FOR SHARE"
	if mutate {
		locking = " FOR UPDATE"
	}
	if err := lockApplicationKeyDeviceRows(ctx, tx, `SELECT id FROM sessions
		WHERE id = $1 OR id IN (SELECT credential_id FROM application_keys WHERE reported_device_numeric_id = $2)
		ORDER BY id`+locking, actor.SessionID, id); err != nil {
		return nil, 0, nil, err
	}
	if err := lockApplicationKeyDeviceRows(ctx, tx, `SELECT credential_id FROM application_keys
		WHERE credential_id = $1 OR reported_device_numeric_id = $2 ORDER BY credential_id`+locking, actor.SessionID, id); err != nil {
		return nil, 0, nil, err
	}
	if actor.IsApplicationKey() {
		var clientID string
		if err := tx.QueryRow(ctx, `SELECT id FROM application_key_clients
			WHERE id = $1 AND credential_id = $2 FOR SHARE`, actor.ClientSessionID, actor.SessionID).Scan(&clientID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, 0, nil, ErrUnauthorized
			}
			return nil, 0, nil, fmt.Errorf("lock application server device actor client: %w", err)
		}
	}
	var deletedAt *time.Time
	if err := tx.QueryRow(ctx, "SELECT deleted_at FROM application_key_devices WHERE id = $1"+locking, id).Scan(&deletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, nil, ErrDeviceNotFound
		}
		return nil, 0, nil, fmt.Errorf("lock application server device generation: %w", err)
	}
	if err := authorizeDeviceActor(ctx, tx, actor, false, nil); err != nil {
		return nil, 0, nil, err
	}
	ok = true
	return tx, id, deletedAt, nil
}

func lockApplicationKeyDeviceRows(ctx context.Context, tx pgx.Tx, statement string, args ...any) error {
	rows, err := tx.Query(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("lock application server device credentials: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("read application server device credential lock: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read application server device credential locks: %w", err)
	}
	return nil
}

func readApplicationKeyDevice(ctx context.Context, tx pgx.Tx, id int64) (ManagedDevice, error) {
	var row deviceRecord
	err := tx.QueryRow(ctx, `SELECT d.id, d.revision, d.reported_device_id, d.reported_name, d.custom_name,
		d.app_name, d.app_version, d.created_at, d.last_seen_at, d.ip_address,
		(SELECT count(*) FROM application_keys k JOIN sessions a ON a.id = k.credential_id
		 WHERE k.reported_device_numeric_id = d.id AND a.kind = 'application_key' AND a.revoked_at IS NULL)
		FROM application_key_devices d WHERE d.id = $1 AND d.deleted_at IS NULL`, id).
		Scan(&row.ID, &row.Revision, &row.ReportedDeviceID, &row.ReportedName, &row.CustomName,
			&row.AppName, &row.AppVersion, &row.CreatedAt, &row.LastSeenAt, &row.IPAddress, &row.ActiveLoginCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedDevice{}, ErrDeviceNotFound
	}
	if err != nil {
		return ManagedDevice{}, fmt.Errorf("read application server device: %w", err)
	}
	return row.device(), nil
}

// LookupApplicationKeyDevice addresses the hidden shared server registry. It
// never looks up an ordinary device or treats a client-context ID as a device.
func (s *Store) LookupApplicationKeyDevice(ctx context.Context, actor Principal, lookup string) (ManagedDevice, error) {
	reference, err := compatibilityDeviceReference(lookup)
	if err != nil {
		return ManagedDevice{}, err
	}
	tx, id, deletedAt, err := s.beginApplicationKeyDeviceOperation(ctx, actor, reference, false)
	if err != nil {
		return ManagedDevice{}, err
	}
	defer rollback(tx)
	if deletedAt != nil {
		return ManagedDevice{}, ErrApplicationKeyDeviceRemoved
	}
	result, err := readApplicationKeyDevice(ctx, tx, id)
	if err != nil {
		return ManagedDevice{}, err
	}
	if err := authorizeDeviceActor(ctx, tx, actor, false, nil); err != nil {
		return ManagedDevice{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedDevice{}, fmt.Errorf("commit application server device lookup: %w", err)
	}
	return result, nil
}

// UpdateApplicationKeyDeviceOptions preserves reported metadata and credentials.
// An empty name removes only the custom label of the current generation.
func (s *Store) UpdateApplicationKeyDeviceOptions(ctx context.Context, actor Principal, lookup, customName string) (ManagedDevice, error) {
	reference, err := compatibilityDeviceReference(lookup)
	if err != nil {
		return ManagedDevice{}, err
	}
	name, err := normalizeDeviceName(customName)
	if err != nil {
		return ManagedDevice{}, err
	}
	tx, id, deletedAt, err := s.beginApplicationKeyDeviceOperation(ctx, actor, reference, true)
	if err != nil {
		return ManagedDevice{}, err
	}
	defer rollback(tx)
	if deletedAt != nil {
		return ManagedDevice{}, ErrApplicationKeyDeviceRemoved
	}
	if _, err := tx.Exec(ctx, `UPDATE application_key_devices SET custom_name = $2, revision = revision + 1
		WHERE id = $1 AND custom_name IS DISTINCT FROM $2`, id, name); err != nil {
		return ManagedDevice{}, fmt.Errorf("update application server device options: %w", err)
	}
	result, err := readApplicationKeyDevice(ctx, tx, id)
	if err != nil {
		return ManagedDevice{}, err
	}
	if err := authorizeDeviceActor(ctx, tx, actor, false, nil); err != nil {
		return ManagedDevice{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedDevice{}, fmt.Errorf("commit application server device options: %w", err)
	}
	return result, nil
}

// DeleteApplicationKeyDevice revokes every parent credential attached to exactly
// one server generation. History and client contexts remain stored but cannot
// authenticate. Numeric retries return the old generation's retirement IDs.
func (s *Store) DeleteApplicationKeyDevice(ctx context.Context, actor Principal, lookup string) (DeviceDeletion, error) {
	reference, err := compatibilityDeviceReference(lookup)
	if err != nil {
		return DeviceDeletion{}, err
	}
	tx, id, deletedAt, err := s.beginApplicationKeyDeviceOperation(ctx, actor, reference, true)
	if err != nil {
		return DeviceDeletion{}, err
	}
	defer rollback(tx)
	result := DeviceDeletion{ID: id, RevokedSessionIDs: make([]string, 0)}
	rows, err := tx.Query(ctx, `SELECT credential_id FROM application_keys
		WHERE reported_device_numeric_id = $1 ORDER BY credential_id`, id)
	if err != nil {
		return DeviceDeletion{}, fmt.Errorf("read application server device retirement scope: %w", err)
	}
	for rows.Next() {
		var credentialID string
		if err := rows.Scan(&credentialID); err != nil {
			rows.Close()
			return DeviceDeletion{}, fmt.Errorf("read application server device retirement credential: %w", err)
		}
		result.RevokedSessionIDs = append(result.RevokedSessionIDs, credentialID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return DeviceDeletion{}, fmt.Errorf("read application server device retirement credentials: %w", err)
	}
	var selfRevocation *time.Time
	if deletedAt != nil {
		result.DeletedAt = *deletedAt
	} else {
		if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&result.DeletedAt); err != nil {
			return DeviceDeletion{}, fmt.Errorf("read application server device deletion time: %w", err)
		}
		updated, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = $2
			WHERE id = ANY($1::text[]) AND kind = 'application_key' AND revoked_at IS NULL`, result.RevokedSessionIDs, result.DeletedAt)
		if err != nil {
			return DeviceDeletion{}, fmt.Errorf("revoke application server device credentials: %w", err)
		}
		result.RevokedLoginCount = updated.RowsAffected()
		if _, err := tx.Exec(ctx, `UPDATE application_key_devices SET deleted_at = $2, revision = revision + 1 WHERE id = $1`, id, result.DeletedAt); err != nil {
			return DeviceDeletion{}, fmt.Errorf("remove application server device generation: %w", err)
		}
		if actor.IsApplicationKey() {
			for _, credentialID := range result.RevokedSessionIDs {
				if credentialID == actor.SessionID {
					selfRevocation = &result.DeletedAt
					break
				}
			}
		}
	}
	if err := authorizeDeviceActor(ctx, tx, actor, false, selfRevocation); err != nil {
		return DeviceDeletion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeviceDeletion{}, fmt.Errorf("commit application server device deletion: %w", err)
	}
	return result, nil
}

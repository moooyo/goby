package identity

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

func lockDeviceRegistration(ctx context.Context, tx pgx.Tx, reported string) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1::integer, hashtext($2))", deviceRegistrationLockNamespace, reported); err != nil {
		return fmt.Errorf("lock device registration: %w", err)
	}
	return nil
}

func (s *Store) beginDeviceMutation(ctx context.Context, actor Principal, native bool) (pgx.Tx, error) {
	if !validDeviceActor(actor, native) {
		return nil, ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin device mutation: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", managedUsersLockID); err != nil {
		rollback(tx)
		return nil, fmt.Errorf("lock device management: %w", err)
	}
	if err := authorizeDeviceActor(ctx, tx, actor, native, nil); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}

func (s *Store) UpdateManagedDeviceOptions(ctx context.Context, actor Principal, id, revision int64, customName string) (ManagedDevice, error) {
	reference, err := nativeDeviceReference(id)
	if err != nil {
		return ManagedDevice{}, err
	}
	if revision < 1 {
		return ManagedDevice{}, deviceInput("Revision", "revision must be a positive decimal integer")
	}
	return s.updateDeviceOptions(ctx, actor, reference, revision, customName, true)
}

func (s *Store) UpdateEmbyDeviceOptions(ctx context.Context, actor Principal, lookup, customName string) (ManagedDevice, error) {
	reference, err := compatibilityDeviceReference(lookup)
	if err != nil {
		return ManagedDevice{}, err
	}
	return s.updateDeviceOptions(ctx, actor, reference, 0, customName, false)
}

func (s *Store) updateDeviceOptions(ctx context.Context, actor Principal, reference deviceReference, revision int64, customName string, native bool) (ManagedDevice, error) {
	name, err := normalizeDeviceName(customName)
	if err != nil {
		return ManagedDevice{}, err
	}
	tx, err := s.beginDeviceMutation(ctx, actor, native)
	if err != nil {
		return ManagedDevice{}, err
	}
	defer rollback(tx)
	if err := lockDeviceActor(ctx, tx, actor, native); err != nil {
		return ManagedDevice{}, err
	}
	id, _, err := findDeviceGeneration(ctx, tx, reference)
	if err != nil {
		return ManagedDevice{}, err
	}
	var currentRevision int64
	var currentName *string
	var deletedAt *time.Time
	err = tx.QueryRow(ctx, "SELECT revision, custom_name, deleted_at FROM devices WHERE id = $1 FOR UPDATE", id).
		Scan(&currentRevision, &currentName, &deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ManagedDevice{}, ErrDeviceNotFound
		}
		return ManagedDevice{}, fmt.Errorf("lock device options: %w", err)
	}
	if err := authorizeDeviceActor(ctx, tx, actor, native, nil); err != nil {
		return ManagedDevice{}, err
	}
	if deletedAt != nil {
		return ManagedDevice{}, ErrDeviceNotFound
	}
	if native && currentRevision != revision {
		return ManagedDevice{}, ErrDeviceRevisionConflict
	}
	changed := (currentName == nil) != (name == nil) || (currentName != nil && name != nil && *currentName != *name)
	if changed {
		if currentRevision == math.MaxInt64 {
			return ManagedDevice{}, ErrDeviceRevisionConflict
		}
		if _, err := tx.Exec(ctx, "UPDATE devices SET custom_name = $2, revision = revision + 1 WHERE id = $1", id, name); err != nil {
			return ManagedDevice{}, fmt.Errorf("update device options: %w", err)
		}
	}
	result, err := readManagedDevice(ctx, tx, id)
	if err != nil {
		return ManagedDevice{}, err
	}
	if err := authorizeDeviceActor(ctx, tx, actor, native, nil); err != nil {
		return ManagedDevice{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedDevice{}, fmt.Errorf("commit device options: %w", err)
	}
	return result, nil
}

func (s *Store) DeleteManagedDevice(ctx context.Context, actor Principal, id, revision int64) (DeviceDeletion, error) {
	reference, err := nativeDeviceReference(id)
	if err != nil {
		return DeviceDeletion{}, err
	}
	if revision < 1 {
		return DeviceDeletion{}, deviceInput("Revision", "revision must be a positive decimal integer")
	}
	return s.deleteDevice(ctx, actor, reference, revision, true)
}

func (s *Store) DeleteEmbyDevice(ctx context.Context, actor Principal, lookup string) (DeviceDeletion, error) {
	reference, err := compatibilityDeviceReference(lookup)
	if err != nil {
		return DeviceDeletion{}, err
	}
	return s.deleteDevice(ctx, actor, reference, 0, false)
}

// lockDeviceDeletion fixes registration first, then all accounts and existing
// credentials in the established management order. Registering a new credential
// holds the same device advisory lock before its account, so no new generation
// member can appear after the owner set has been selected.
func lockDeviceDeletion(ctx context.Context, tx pgx.Tx, actor Principal, native bool, id int64, reported string) ([]string, error) {
	if err := lockDeviceRegistration(ctx, tx, reported); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM users
		WHERE id = $1 OR id IN (SELECT user_id FROM sessions WHERE device_registry_id = $2 AND kind = 'emby')
		ORDER BY id FOR UPDATE`, actor.User.ID, id)
	if err != nil {
		return nil, fmt.Errorf("lock device login accounts: %w", err)
	}
	for rows.Next() {
		var ignored string
		if err := rows.Scan(&ignored); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read device account locks: %w", err)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read device account locks: %w", err)
	}
	rows, err = tx.Query(ctx, `SELECT id, kind, device_registry_id FROM sessions
		WHERE id = $1 OR (device_registry_id = $2 AND kind = 'emby') ORDER BY id FOR UPDATE`, actor.SessionID, id)
	if err != nil {
		return nil, fmt.Errorf("lock device login credentials: %w", err)
	}
	result := make([]string, 0)
	for rows.Next() {
		var sessionID, kind string
		var generation *int64
		if err := rows.Scan(&sessionID, &kind, &generation); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read device credential locks: %w", err)
		}
		if kind == "emby" && generation != nil && *generation == id {
			result = append(result, sessionID)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read device credential locks: %w", err)
	}
	if actor.IsApplicationKey() {
		var contextID string
		if err := tx.QueryRow(ctx, `SELECT id FROM application_key_clients WHERE id = $1 AND credential_id = $2 FOR SHARE`,
			actor.ClientSessionID, actor.SessionID).Scan(&contextID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrUnauthorized
			}
			return nil, fmt.Errorf("lock device deletion application context: %w", err)
		}
	}
	if err := authorizeDeviceActor(ctx, tx, actor, native, nil); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) deleteDevice(ctx context.Context, actor Principal, reference deviceReference, revision int64, native bool) (DeviceDeletion, error) {
	tx, err := s.beginDeviceMutation(ctx, actor, native)
	if err != nil {
		return DeviceDeletion{}, err
	}
	defer rollback(tx)
	id, reported, err := findDeviceGeneration(ctx, tx, reference)
	if err != nil {
		if authErr := lockDeviceActor(ctx, tx, actor, native); authErr != nil {
			return DeviceDeletion{}, authErr
		}
		return DeviceDeletion{}, err
	}
	sessions, err := lockDeviceDeletion(ctx, tx, actor, native, id, reported)
	if err != nil {
		return DeviceDeletion{}, err
	}
	var currentRevision int64
	var deletedAt *time.Time
	err = tx.QueryRow(ctx, "SELECT revision, deleted_at FROM devices WHERE id = $1 FOR UPDATE", id).Scan(&currentRevision, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeviceDeletion{}, ErrDeviceNotFound
	}
	if err != nil {
		return DeviceDeletion{}, fmt.Errorf("lock device removal: %w", err)
	}
	if err := authorizeDeviceActor(ctx, tx, actor, native, nil); err != nil {
		return DeviceDeletion{}, err
	}
	result := DeviceDeletion{ID: id, RevokedSessionIDs: sessions}
	var selfRevocation *time.Time
	if deletedAt != nil {
		result.DeletedAt = *deletedAt
	} else {
		if native && currentRevision != revision {
			return DeviceDeletion{}, ErrDeviceRevisionConflict
		}
		if currentRevision == math.MaxInt64 {
			return DeviceDeletion{}, ErrDeviceRevisionConflict
		}
		if err := tx.QueryRow(ctx, `UPDATE devices SET deleted_at = clock_timestamp(), revision = revision + 1
			WHERE id = $1 RETURNING deleted_at`, id).Scan(&result.DeletedAt); err != nil {
			return DeviceDeletion{}, fmt.Errorf("remove ordinary device: %w", err)
		}
		changed, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = $2
			WHERE device_registry_id = $1 AND kind = 'emby' AND revoked_at IS NULL`, id, result.DeletedAt)
		if err != nil {
			return DeviceDeletion{}, fmt.Errorf("retire device credentials: %w", err)
		}
		result.RevokedLoginCount = changed.RowsAffected()
		if actor.Kind == "emby" {
			for _, sessionID := range sessions {
				if sessionID == actor.SessionID {
					selfRevocation = &result.DeletedAt
					break
				}
			}
		}
	}
	if err := authorizeDeviceActor(ctx, tx, actor, native, selfRevocation); err != nil {
		return DeviceDeletion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeviceDeletion{}, fmt.Errorf("commit device removal: %w", err)
	}
	result.DeletedAt = result.DeletedAt.UTC()
	return result, nil
}

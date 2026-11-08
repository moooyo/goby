package identity

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"strconv"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const backupPolicySecretHeader = "GBP\x01"

func validBackupPolicyPassphrase(value []byte) bool {
	return len(value) >= 12 && len(value) <= 1024 && utf8.Valid(value)
}

func backupPolicySecretAAD(generation int64) ([]byte, error) {
	if generation < 1 {
		return nil, ErrInvalidInput
	}
	return []byte("goby/backup-policy/passphrase/v1\x00" + strconv.FormatInt(generation, 10)), nil
}

// SealBackupPolicyPassphrase is called only after native administrator and
// management locks have been acquired in the same transaction. The passphrase
// is caller-owned and is never converted to an immutable Go string.
func (s *Store) SealBackupPolicyPassphrase(ctx context.Context, tx pgx.Tx, generation int64, passphrase []byte) ([]byte, error) {
	if !validBackupPolicyPassphrase(passphrase) || s == nil || s.applicationKeyVault == nil {
		return nil, ErrInvalidInput
	}
	aad, err := backupPolicySecretAAD(generation)
	if err != nil {
		return nil, err
	}
	allow, err := s.allowVaultMasterCreation(ctx, tx)
	if err != nil {
		return nil, err
	}
	// Retained policy ciphertext is an additional witness even before the
	// shared admission chain has reached this schema on an older deployment.
	if allow {
		allow, err = s.allowBackupPolicyMasterCreation(ctx, tx)
		if err != nil {
			return nil, err
		}
	}
	key, err := s.applicationKeyVault.loadMasterKey(ctx, allow)
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	gcm, err := applicationKeyGCM(key[:])
	if err != nil {
		return nil, err
	}
	sealed := make([]byte, len(backupPolicySecretHeader)+applicationKeyNonceSize)
	copy(sealed, backupPolicySecretHeader)
	if _, err := rand.Read(sealed[len(backupPolicySecretHeader):]); err != nil {
		return nil, ErrApplicationKeyVaultUnavailable
	}
	return gcm.Seal(sealed, sealed[len(backupPolicySecretHeader):], passphrase, aad), ctx.Err()
}

func openBackupPolicyPassphrase(gcm cipher.AEAD, generation int64, sealed []byte) ([]byte, error) {
	aad, err := backupPolicySecretAAD(generation)
	if err != nil || gcm == nil || len(sealed) < 44 || len(sealed) > 1056 || string(sealed[:4]) != backupPolicySecretHeader {
		return nil, ErrApplicationKeyVaultCiphertext
	}
	plain, err := gcm.Open(nil, sealed[4:16], sealed[16:], aad)
	if err != nil || !validBackupPolicyPassphrase(plain) {
		clear(plain)
		return nil, ErrApplicationKeyVaultCiphertext
	}
	return plain, nil
}

// OpenBackupPolicyPassphrase returns a caller-owned buffer that must be cleared.
func (v *ApplicationKeyVault) OpenBackupPolicyPassphrase(ctx context.Context, generation int64, sealed []byte) ([]byte, error) {
	if v == nil {
		return nil, ErrApplicationKeyVaultUnavailable
	}
	key, err := v.loadMasterKey(ctx, false)
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	gcm, err := applicationKeyGCM(key[:])
	if err != nil {
		return nil, err
	}
	return openBackupPolicyPassphrase(gcm, generation, sealed)
}

// LockBackupPolicyMutation preserves the shared credential-management order.
func LockBackupPolicyMutation(ctx context.Context, tx pgx.Tx, actor Principal) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", managedUsersLockID); err != nil {
		return ErrApplicationKeyVaultUnavailable
	}
	return CheckAdministrator(ctx, tx, actor, AdministratorNative, true)
}

func validateBackupPolicyRecovery(ctx context.Context, tx pgx.Tx, gcm cipher.AEAD) (int64, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('backup_policy') IS NOT NULL`).Scan(&exists); err != nil {
		return 0, applicationKeyBackupDatabaseError(ctx)
	}
	if !exists {
		return 0, nil
	}
	var generation int64
	var sealed []byte
	err := tx.QueryRow(ctx, `SELECT secret_generation, CASE WHEN octet_length(passphrase_ciphertext) BETWEEN 44 AND 1056 THEN passphrase_ciphertext END FROM backup_policy WHERE id=1 AND passphrase_ciphertext IS NOT NULL`).Scan(&generation, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, applicationKeyBackupDatabaseError(ctx)
	}
	plain, err := openBackupPolicyPassphrase(gcm, generation, sealed)
	clear(plain)
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func (s *Store) allowBackupPolicyMasterCreation(ctx context.Context, tx pgx.Tx) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('backup_policy') IS NOT NULL`).Scan(&exists); err != nil {
		return false, err
	}
	if !exists {
		return true, nil
	}
	var generation int64
	var sealed []byte
	err := tx.QueryRow(ctx, `SELECT secret_generation,passphrase_ciphertext FROM backup_policy WHERE id=1 AND passphrase_ciphertext IS NOT NULL`).Scan(&generation, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, ErrApplicationKeyVaultUnavailable
	}
	plain, err := s.applicationKeyVault.OpenBackupPolicyPassphrase(ctx, generation, sealed)
	clear(plain)
	return false, err
}

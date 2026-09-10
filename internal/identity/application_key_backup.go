package identity

import (
	"context"
	"crypto/cipher"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"

	"github.com/jackc/pgx/v5"
)

const (
	applicationKeyBackupTokenSize  = 43
	applicationKeyBackupSealedSize = len(applicationKeyHeader) + applicationKeyNonceSize + applicationKeyBackupTokenSize + applicationKeyTagSize
)

var (
	ErrApplicationKeyBackupInvalid     = errors.New("invalid application key backup state")
	ErrApplicationKeyBackupUnavailable = errors.New("application key backup state unavailable")
)

// ApplicationKeyBackupWitness describes authenticated application-key history.
// SealedKeyCount includes revoked keys. HasMasterKey reports whether the caller
// received or supplied an exact master key; an all-zero output buffer when it
// is false must never be archived as a master key.
type ApplicationKeyBackupWitness struct {
	SealedKeyCount int64
	HasMasterKey   bool
}

// WitnessBackup authenticates every sealed application key using one safely
// opened master file and copies that exact key into masterKey only on success.
// masterKey must be a caller-owned 32-byte buffer, and the caller must clear it
// after use. A missing master is accepted only when no sealed keys exist; this
// operation never creates a key. On failure or when HasMasterKey is false, the
// output buffer is cleared.
//
// The caller must invoke this serially on the same exported, read-only,
// repeatable-read snapshot transaction used by pg_dump. The caller owns tx and
// must not concurrently use or complete it while this operation is running.
func (v *ApplicationKeyVault) WitnessBackup(ctx context.Context, tx pgx.Tx, masterKey []byte) (ApplicationKeyBackupWitness, error) {
	clear(masterKey)
	if len(masterKey) != applicationKeyMasterSize || tx == nil {
		return ApplicationKeyBackupWitness{}, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return ApplicationKeyBackupWitness{}, err
	}
	if v == nil {
		return ApplicationKeyBackupWitness{}, ErrApplicationKeyVaultUnavailable
	}
	key, err := v.loadMasterKey(ctx, false)
	defer clear(key[:])
	if err != nil && !errors.Is(err, ErrApplicationKeyVaultMissing) {
		return ApplicationKeyBackupWitness{}, err
	}
	var supplied []byte
	if err == nil {
		supplied = key[:]
	}
	witness, err := ValidateApplicationKeyRecovery(ctx, tx, supplied)
	if err != nil {
		return ApplicationKeyBackupWitness{}, err
	}
	if err := ctx.Err(); err != nil {
		return ApplicationKeyBackupWitness{}, err
	}
	if witness.HasMasterKey {
		copy(masterKey, key[:])
	}
	return witness, nil
}

// ValidateApplicationKeyRecovery authenticates every sealed application key in
// the caller's transaction, including revoked history. masterKey must be nil
// when absent or an exact 32-byte key read from protected extracted storage.
// The function neither reads the active vault nor returns plaintext tokens.
// It does not mutate the supplied key; the caller owns and must clear it.
//
// The caller owns tx, uses it serially, and must supply a stable snapshot. For
// recovery, invoke this after raw archive fingerprints have been validated and
// before revoking imported credentials or accepting the restored state.
func ValidateApplicationKeyRecovery(ctx context.Context, tx pgx.Tx, masterKey []byte) (ApplicationKeyBackupWitness, error) {
	if tx == nil || (len(masterKey) != 0 && len(masterKey) != applicationKeyMasterSize) {
		return ApplicationKeyBackupWitness{}, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return ApplicationKeyBackupWitness{}, err
	}
	if err := validateApplicationKeyBackupLinkage(ctx, tx); err != nil {
		return ApplicationKeyBackupWitness{}, err
	}
	var gcm cipher.AEAD
	if len(masterKey) != 0 {
		var err error
		gcm, err = applicationKeyGCM(masterKey)
		if err != nil {
			return ApplicationKeyBackupWitness{}, err
		}
	}
	// Invalid sizes are represented by NULL instead of fetching unbounded
	// ciphertext or hash values. Every row is retained, regardless of revocation.
	rows, err := tx.Query(ctx, `SELECT k.id, k.credential_id,
		CASE WHEN octet_length(k.secret_ciphertext) = $1 THEN k.secret_ciphertext END,
		CASE WHEN octet_length(a.token_hash) = $2 THEN a.token_hash END
		FROM application_keys k JOIN sessions a ON a.id = k.credential_id
		ORDER BY k.id`, applicationKeyBackupSealedSize, sha256.Size)
	if err != nil {
		return ApplicationKeyBackupWitness{}, applicationKeyBackupDatabaseError(ctx)
	}
	defer rows.Close()
	witness := ApplicationKeyBackupWitness{HasMasterKey: len(masterKey) != 0}
	for rows.Next() {
		var id int64
		var credentialID string
		var sealed, tokenHash []byte
		if err := rows.Scan(&id, &credentialID, &sealed, &tokenHash); err != nil {
			return ApplicationKeyBackupWitness{}, applicationKeyBackupDatabaseError(ctx)
		}
		if err := ctx.Err(); err != nil {
			return ApplicationKeyBackupWitness{}, err
		}
		if id <= 0 || !validRevalidationID(credentialID) {
			return ApplicationKeyBackupWitness{}, ErrApplicationKeyBackupInvalid
		}
		if gcm == nil {
			return ApplicationKeyBackupWitness{}, ErrApplicationKeyVaultMissing
		}
		if err := authenticateApplicationKeyBackupRow(gcm, credentialID, sealed, tokenHash); err != nil {
			return ApplicationKeyBackupWitness{}, err
		}
		witness.SealedKeyCount++
	}
	if err := rows.Err(); err != nil {
		return ApplicationKeyBackupWitness{}, applicationKeyBackupDatabaseError(ctx)
	}
	if err := ctx.Err(); err != nil {
		return ApplicationKeyBackupWitness{}, err
	}
	return witness, nil
}

func validateApplicationKeyBackupLinkage(ctx context.Context, tx pgx.Tx) error {
	var valid bool
	err := tx.QueryRow(ctx, `SELECT
		NOT EXISTS (SELECT 1 FROM application_keys k
			LEFT JOIN sessions a ON a.id = k.credential_id
			LEFT JOIN application_key_devices d ON d.id = k.reported_device_numeric_id
			WHERE a.kind IS DISTINCT FROM 'application_key' OR a.user_id IS NOT NULL
			OR a.expires_at IS NOT NULL OR a.device_registry_id IS NOT NULL
			OR d.id IS NULL OR k.reported_device_numeric_id <= 0
			OR (d.deleted_at IS NOT NULL AND a.revoked_at IS NULL))
		AND NOT EXISTS (SELECT 1 FROM sessions a
			LEFT JOIN application_keys k ON k.credential_id = a.id
			WHERE a.kind = 'application_key' AND k.id IS NULL)
		AND NOT EXISTS (SELECT 1 FROM application_key_clients c
			LEFT JOIN application_keys k ON k.credential_id = c.credential_id
			WHERE k.id IS NULL OR c.id = c.credential_id)`).Scan(&valid)
	if err != nil {
		return applicationKeyBackupDatabaseError(ctx)
	}
	if !valid {
		return ErrApplicationKeyBackupInvalid
	}
	return nil
}

func authenticateApplicationKeyBackupRow(gcm cipher.AEAD, credentialID string, sealed, tokenHash []byte) error {
	if gcm == nil || !validRevalidationID(credentialID) || len(tokenHash) != sha256.Size {
		return ErrApplicationKeyBackupInvalid
	}
	if len(sealed) != applicationKeyBackupSealedSize || string(sealed[:len(applicationKeyHeader)]) != applicationKeyHeader {
		return ErrApplicationKeyVaultCiphertext
	}
	nonceEnd := len(applicationKeyHeader) + applicationKeyNonceSize
	plaintext, err := gcm.Open(nil, sealed[len(applicationKeyHeader):nonceEnd], sealed[nonceEnd:], []byte(applicationKeyPurpose+credentialID))
	if err != nil {
		return ErrApplicationKeyVaultCiphertext
	}
	defer clear(plaintext)
	if len(plaintext) != applicationKeyBackupTokenSize {
		return ErrApplicationKeyVaultCiphertext
	}
	// Match tokenDigest's strict Base64URL semantics without creating an
	// immutable plaintext string that cannot be explicitly cleared.
	var decoded [sha256.Size]byte
	defer clear(decoded[:])
	n, err := base64.RawURLEncoding.Strict().Decode(decoded[:], plaintext)
	if err != nil || n != len(decoded) {
		return ErrApplicationKeyVaultCiphertext
	}
	digest := sha256.Sum256(plaintext)
	defer clear(digest[:])
	if subtle.ConstantTimeCompare(digest[:], tokenHash) != 1 {
		return ErrApplicationKeyVaultCiphertext
	}
	return nil
}

// RevokeRecoveredCredentials invalidates every imported credential kind by
// setting revoked_at on previously unrevoked sessions. It retains all users,
// passwords, policies, devices, application keys, clients, and credential
// history, including the original timestamp of already revoked credentials.
// The result counts only newly revoked rows.
//
// The caller must first validate the raw archive fingerprints and the recovered
// key witness. The caller owns this recovery transaction and must commit or
// roll it back; this helper never begins or completes a transaction.
func RevokeRecoveredCredentials(ctx context.Context, tx pgx.Tx) (int64, error) {
	if tx == nil {
		return 0, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	result, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = transaction_timestamp()
		WHERE revoked_at IS NULL`)
	if err != nil {
		return 0, applicationKeyBackupDatabaseError(ctx)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func applicationKeyBackupDatabaseError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrApplicationKeyBackupUnavailable
}

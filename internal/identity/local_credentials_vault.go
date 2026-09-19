package identity

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const (
	profilePinHeader     = "GPP\x01"
	profilePinPurpose    = "goby/profile-pin/v1\x00"
	profilePinSealedSize = len(profilePinHeader) + applicationKeyNonceSize + 4 + applicationKeyTagSize
)

// Profile PIN encryption reuses the protected master file and backup witness,
// with a separate envelope and associated-data domain bound to the account ID.
func (v *ApplicationKeyVault) sealProfilePin(ctx context.Context, userID, pin string, allowCreate bool) ([]byte, error) {
	if v == nil {
		return nil, ErrApplicationKeyVaultUnavailable
	}
	key, err := v.loadMasterKey(ctx, allowCreate)
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	gcm, err := applicationKeyGCM(key[:])
	if err != nil {
		return nil, err
	}
	sealed := make([]byte, len(profilePinHeader)+applicationKeyNonceSize)
	copy(sealed, profilePinHeader)
	nonce := sealed[len(profilePinHeader):]
	if _, err := rand.Read(nonce); err != nil {
		return nil, ErrApplicationKeyVaultUnavailable
	}
	plaintext := []byte(pin)
	defer clear(plaintext)
	return gcm.Seal(sealed, nonce, plaintext, []byte(profilePinPurpose+userID)), ctx.Err()
}

func openProfilePin(gcm cipher.AEAD, userID string, sealed []byte) (string, error) {
	if gcm == nil || !validRevalidationID(userID) || len(sealed) != profilePinSealedSize || string(sealed[:len(profilePinHeader)]) != profilePinHeader {
		return "", ErrApplicationKeyVaultCiphertext
	}
	nonceEnd := len(profilePinHeader) + applicationKeyNonceSize
	plaintext, err := gcm.Open(nil, sealed[len(profilePinHeader):nonceEnd], sealed[nonceEnd:], []byte(profilePinPurpose+userID))
	if err != nil {
		return "", ErrApplicationKeyVaultCiphertext
	}
	defer clear(plaintext)
	if len(plaintext) != 4 {
		return "", ErrApplicationKeyVaultCiphertext
	}
	for _, digit := range plaintext {
		if digit < '0' || digit > '9' {
			return "", ErrApplicationKeyVaultCiphertext
		}
	}
	return string(plaintext), nil
}

func (v *ApplicationKeyVault) openProfilePin(ctx context.Context, userID string, sealed []byte) (string, error) {
	if v == nil {
		return "", ErrApplicationKeyVaultUnavailable
	}
	key, err := v.loadMasterKey(ctx, false)
	if err != nil {
		return "", err
	}
	defer clear(key[:])
	gcm, err := applicationKeyGCM(key[:])
	if err != nil {
		return "", err
	}
	pin, err := openProfilePin(gcm, userID, sealed)
	if err != nil {
		return "", err
	}
	return pin, ctx.Err()
}

func hasProfilePinColumn(ctx context.Context, tx pgx.Tx) (bool, error) {
	var found bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_attribute
		WHERE attrelid='users'::regclass AND attname='profile_pin_ciphertext' AND NOT attisdropped)`).Scan(&found)
	return found, err
}

// allowVaultMasterCreation requires the management advisory lock. Either kind
// of retained ciphertext proves a master must already exist, including after a
// restart with no application keys and only profile PINs in the database.
func (s *Store) allowVaultMasterCreation(ctx context.Context, tx pgx.Tx) (bool, error) {
	if s.applicationKeyVault == nil {
		return false, ErrApplicationKeyVaultUnavailable
	}
	var id string
	var sealed []byte
	err := tx.QueryRow(ctx, `SELECT credential_id,secret_ciphertext FROM application_keys ORDER BY id LIMIT 1`).Scan(&id, &sealed)
	if err == nil {
		_, err := s.applicationKeyVault.Open(ctx, id, sealed)
		return false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("read credential vault witness: %w", err)
	}
	hasColumn, err := hasProfilePinColumn(ctx, tx)
	if err != nil || !hasColumn {
		return !hasColumn, err
	}
	err = tx.QueryRow(ctx, `SELECT id,profile_pin_ciphertext FROM users WHERE profile_pin_ciphertext IS NOT NULL ORDER BY id LIMIT 1`).Scan(&id, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read profile credential vault witness: %w", err)
	}
	_, err = s.applicationKeyVault.openProfilePin(ctx, id, sealed)
	return false, err
}

func (s *Store) prepareProfilePin(ctx context.Context, tx pgx.Tx, userID, pin string) ([]byte, error) {
	if pin == "" {
		return nil, nil
	}
	if err := validateLocalSecret(pin, true); err != nil {
		return nil, err
	}
	var hasPassword bool
	if err := tx.QueryRow(ctx, "SELECT has_password FROM users WHERE id=$1", userID).Scan(&hasPassword); err != nil {
		return nil, err
	}
	if !hasPassword {
		return nil, managedUserFieldError("ProfilePin", "Configure a normal account password before adding a profile PIN.")
	}
	allowCreate, err := s.allowVaultMasterCreation(ctx, tx)
	if err != nil {
		return nil, err
	}
	return s.applicationKeyVault.sealProfilePin(ctx, userID, pin, allowCreate)
}

// GetOwnProfilePin is the only plaintext projection. It requires a current
// ordinary owner login; native administrators, application keys, public views,
// and other users never receive a PIN. No generic User JSON contains this field.
func (s *Store) GetOwnProfilePin(ctx context.Context, actor Principal, userID string) (string, error) {
	_, pin, err := s.GetOwnProfileConfiguration(ctx, actor, userID)
	return pin, err
}

// GetOwnProfileConfiguration reads the configuration and decrypted profile PIN
// under one account lock so a mixed preference patch has no torn projection.
func (s *Store) GetOwnProfileConfiguration(ctx context.Context, actor Principal, userID string) (json.RawMessage, string, error) {
	if !validManagedActor(actor) || actor.Kind != "emby" || actor.User.ID != userID {
		return nil, "", ErrUnauthorized
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer rollback(tx)
	if _, err := lockClientSession(ctx, tx, actor, false); err != nil {
		return nil, "", err
	}
	var sealed []byte
	var configuration json.RawMessage
	if err := tx.QueryRow(ctx, "SELECT profile_pin_ciphertext,configuration FROM users WHERE id=$1", userID).Scan(&sealed, &configuration); err != nil {
		return nil, "", err
	}
	pin := ""
	if sealed != nil {
		pin, err = s.applicationKeyVault.openProfilePin(ctx, userID, sealed)
		if err != nil {
			return nil, "", err
		}
	}
	if _, err := lockClientSession(ctx, tx, actor, false); err != nil {
		return nil, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}
	return configuration, pin, nil
}

// validateProfilePinRecovery extends the existing master-file witness without
// exposing plaintext. Pre-42 archives have no PIN ciphertext and remain valid.
func validateProfilePinRecovery(ctx context.Context, tx pgx.Tx, gcm cipher.AEAD) (int64, error) {
	hasColumn, err := hasProfilePinColumn(ctx, tx)
	if err != nil {
		return 0, applicationKeyBackupDatabaseError(ctx)
	}
	if !hasColumn {
		return 0, nil
	}
	rows, err := tx.Query(ctx, `SELECT id,CASE WHEN octet_length(profile_pin_ciphertext)=$1 THEN profile_pin_ciphertext END
		FROM users WHERE profile_pin_ciphertext IS NOT NULL ORDER BY id`, profilePinSealedSize)
	if err != nil {
		return 0, applicationKeyBackupDatabaseError(ctx)
	}
	defer rows.Close()
	var count int64
	for rows.Next() {
		var id string
		var sealed []byte
		if rows.Scan(&id, &sealed) != nil {
			return 0, applicationKeyBackupDatabaseError(ctx)
		}
		if gcm == nil {
			return 0, ErrApplicationKeyVaultMissing
		}
		if _, err := openProfilePin(gcm, id, sealed); err != nil {
			return 0, err
		}
		count++
	}
	if rows.Err() != nil {
		return 0, applicationKeyBackupDatabaseError(ctx)
	}
	return count, ctx.Err()
}

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

const (
	providerConfigurationHeader   = "GPC\x01"
	providerConfigurationPurpose  = "goby/provider-configuration/v1\x00"
	maxProviderConfigurationBytes = 131072
)

// LockProviderConfigurationMutation preserves the management/account/session
// lock order used by all master-key consumers before locking the singleton.
func LockProviderConfigurationMutation(ctx context.Context, tx pgx.Tx, actor Principal) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", managedUsersLockID); err != nil {
		return err
	}
	return CheckAdministrator(ctx, tx, actor, AdministratorNative, true)
}

func providerConfigurationAAD(revision string) ([]byte, error) {
	value, err := strconv.ParseInt(revision, 10, 64)
	if err != nil || value < 1 || strconv.FormatInt(value, 10) != revision {
		return nil, ErrInvalidInput
	}
	return []byte(providerConfigurationPurpose + revision), nil
}

// SealProviderConfiguration encrypts the complete override document using the
// existing protected master file and a separate, revision-bound AAD domain.
// The caller holds LockProviderConfigurationMutation in this transaction.
func (s *Store) SealProviderConfiguration(ctx context.Context, tx pgx.Tx, revision string, document []byte) ([]byte, error) {
	if len(document) < 2 || len(document) > maxProviderConfigurationBytes || !utf8.Valid(document) {
		return nil, ErrInvalidInput
	}
	aad, err := providerConfigurationAAD(revision)
	if err != nil {
		return nil, err
	}
	allow, err := s.allowVaultMasterCreation(ctx, tx)
	if err != nil {
		return nil, err
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
	sealed := make([]byte, len(providerConfigurationHeader)+applicationKeyNonceSize)
	copy(sealed, providerConfigurationHeader)
	nonce := sealed[len(providerConfigurationHeader):]
	if _, err := rand.Read(nonce); err != nil {
		return nil, ErrApplicationKeyVaultUnavailable
	}
	return gcm.Seal(sealed, nonce, document, aad), ctx.Err()
}

func openProviderConfiguration(gcm cipher.AEAD, revision string, sealed []byte) ([]byte, error) {
	aad, err := providerConfigurationAAD(revision)
	if err != nil || gcm == nil || len(sealed) < 34 || len(sealed) > maxProviderConfigurationBytes+32 || string(sealed[:4]) != providerConfigurationHeader {
		return nil, ErrApplicationKeyVaultCiphertext
	}
	plain, err := gcm.Open(nil, sealed[4:16], sealed[16:], aad)
	if err != nil || len(plain) < 2 || len(plain) > maxProviderConfigurationBytes || !utf8.Valid(plain) {
		clear(plain)
		return nil, ErrApplicationKeyVaultCiphertext
	}
	return plain, nil
}

// OpenProviderConfiguration never creates a key. Callers must clear the
// returned byte slice after decoding it and must not log its contents.
func (s *Store) OpenProviderConfiguration(ctx context.Context, revision string, sealed []byte) ([]byte, error) {
	if s.applicationKeyVault == nil {
		return nil, ErrApplicationKeyVaultUnavailable
	}
	key, err := s.applicationKeyVault.loadMasterKey(ctx, false)
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	gcm, err := applicationKeyGCM(key[:])
	if err != nil {
		return nil, err
	}
	return openProviderConfiguration(gcm, revision, sealed)
}

func providerConfigurationTableExists(ctx context.Context, tx pgx.Tx) (bool, error) {
	var found bool
	err := tx.QueryRow(ctx, `SELECT to_regclass('provider_configuration') IS NOT NULL`).Scan(&found)
	return found, err
}

// These two helpers extend the shared master-file admission and recovery
// witness. Historical archives without the table remain valid.
func (s *Store) allowProviderConfigurationMasterCreation(ctx context.Context, tx pgx.Tx) (bool, error) {
	exists, err := providerConfigurationTableExists(ctx, tx)
	if err != nil || !exists {
		return !exists, err
	}
	var revision string
	var sealed []byte
	err = tx.QueryRow(ctx, `SELECT revision::text, configuration_ciphertext FROM provider_configuration WHERE id=1`).Scan(&revision, &sealed)
	if err != nil {
		return false, ErrApplicationKeyVaultUnavailable
	}
	if sealed == nil {
		return true, nil
	}
	plain, err := s.OpenProviderConfiguration(ctx, revision, sealed)
	clear(plain)
	return false, err
}

func validateProviderConfigurationRecovery(ctx context.Context, tx pgx.Tx, gcm cipher.AEAD) (int64, error) {
	exists, err := providerConfigurationTableExists(ctx, tx)
	if err != nil || !exists {
		return 0, err
	}
	var revision string
	var sealed []byte
	err = tx.QueryRow(ctx, `SELECT revision::text,
		CASE WHEN octet_length(configuration_ciphertext) BETWEEN 34 AND 131104 THEN configuration_ciphertext END,
		configuration_ciphertext IS NOT NULL
		FROM provider_configuration WHERE id=1`).Scan(&revision, &sealed, &exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrApplicationKeyBackupInvalid
	}
	if err != nil {
		return 0, applicationKeyBackupDatabaseError(ctx)
	}
	if !exists {
		return 0, nil
	}
	if gcm == nil {
		return 0, ErrApplicationKeyVaultMissing
	}
	plain, err := openProviderConfiguration(gcm, revision, sealed)
	clear(plain)
	if err != nil {
		return 0, err
	}
	return 1, ctx.Err()
}

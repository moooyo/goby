package identity

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5"
)

const NotificationReceiverPurpose = "receiver"
const NotificationTargetPurpose = "target"

func NotificationSecretBinding(values ...string) string {
	raw, _ := json.Marshal(values)
	return string(raw)
}
func ValidNotificationSecret(value string) bool {
	if len(value) < 16 || len(value) > 2048 {
		return false
	}
	for _, c := range value {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func notificationSecretAAD(purpose, binding string) ([]byte, error) {
	if purpose != NotificationReceiverPurpose && purpose != NotificationTargetPurpose || len(binding) == 0 || len(binding) > 2048 {
		return nil, ErrInvalidInput
	}
	return []byte("goby/notification/" + purpose + "/v1\x00" + binding), nil
}

// LockNotificationMutation preserves the established management/account/session
// lock order. Personal registrations accept only an ordinary Emby login.
func LockNotificationMutation(ctx context.Context, tx pgx.Tx, actor Principal, native bool) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", managedUsersLockID); err != nil {
		return ErrUnauthorized
	}
	if native {
		return CheckAdministrator(ctx, tx, actor, AdministratorNative, true)
	}
	return CheckNotificationSession(ctx, tx, actor, true)
}
func CheckNotificationSession(ctx context.Context, tx pgx.Tx, actor Principal, lock bool) error {
	if actor.Kind != "emby" || actor.IsApplicationKey() {
		return ErrClientSessionForbidden
	}
	_, err := lockClientSession(ctx, tx, actor, lock)
	return err
}

// SealNotificationSecret requires LockNotificationMutation in the same tx.
func (s *Store) SealNotificationSecret(ctx context.Context, tx pgx.Tx, purpose, binding, secret string) ([]byte, error) {
	if !ValidNotificationSecret(secret) {
		return nil, ErrInvalidInput
	}
	aad, err := notificationSecretAAD(purpose, binding)
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
	sealed := make([]byte, 4+applicationKeyNonceSize)
	copy(sealed, "GNT\x01")
	if _, err = rand.Read(sealed[4:]); err != nil {
		return nil, ErrApplicationKeyVaultUnavailable
	}
	plain := []byte(secret)
	defer clear(plain)
	return gcm.Seal(sealed, sealed[4:], plain, aad), ctx.Err()
}
func openNotificationSecret(gcm cipher.AEAD, purpose, binding string, sealed []byte) (string, error) {
	aad, err := notificationSecretAAD(purpose, binding)
	if err != nil || gcm == nil || len(sealed) < 48 || len(sealed) > 2080 || string(sealed[:4]) != "GNT\x01" {
		return "", ErrApplicationKeyVaultCiphertext
	}
	plain, err := gcm.Open(nil, sealed[4:16], sealed[16:], aad)
	if err != nil {
		return "", ErrApplicationKeyVaultCiphertext
	}
	defer clear(plain)
	if !ValidNotificationSecret(string(plain)) {
		return "", ErrApplicationKeyVaultCiphertext
	}
	return string(plain), nil
}
func (s *Store) OpenNotificationSecret(ctx context.Context, purpose, binding string, sealed []byte) (string, error) {
	if s.applicationKeyVault == nil {
		return "", ErrApplicationKeyVaultUnavailable
	}
	key, err := s.applicationKeyVault.loadMasterKey(ctx, false)
	if err != nil {
		return "", err
	}
	defer clear(key[:])
	gcm, err := applicationKeyGCM(key[:])
	if err != nil {
		return "", err
	}
	return openNotificationSecret(gcm, purpose, binding, sealed)
}

func notificationSecretsExist(ctx context.Context, tx pgx.Tx) (bool, error) {
	var found bool
	err := tx.QueryRow(ctx, `SELECT to_regclass('notification_transport') IS NOT NULL`).Scan(&found)
	return found, err
}
func notificationSecretRows(ctx context.Context, tx pgx.Tx) (pgx.Rows, error) {
	return tx.Query(ctx, `SELECT 'receiver',ARRAY['receiver',credential_generation::text],CASE WHEN octet_length(credential_ciphertext) BETWEEN 48 AND 2080 THEN credential_ciphertext END FROM notification_transport WHERE credential_ciphertext IS NOT NULL
	UNION ALL SELECT 'target',ARRAY[id,session_id,user_id,device_id,token_generation::text],CASE WHEN octet_length(token_ciphertext) BETWEEN 48 AND 2080 THEN token_ciphertext END FROM notification_registrations ORDER BY 1,2`)
}
func validateNotificationRecovery(ctx context.Context, tx pgx.Tx, gcm cipher.AEAD) (int64, error) {
	exists, err := notificationSecretsExist(ctx, tx)
	if err != nil || !exists {
		return 0, err
	}
	rows, err := notificationSecretRows(ctx, tx)
	if err != nil {
		return 0, applicationKeyBackupDatabaseError(ctx)
	}
	defer rows.Close()
	var count int64
	for rows.Next() {
		var purpose string
		var values []string
		var sealed []byte
		if rows.Scan(&purpose, &values, &sealed) != nil {
			return 0, ErrApplicationKeyBackupInvalid
		}
		if _, err := openNotificationSecret(gcm, purpose, NotificationSecretBinding(values...), sealed); err != nil {
			return 0, err
		}
		count++
	}
	if rows.Err() != nil {
		return 0, applicationKeyBackupDatabaseError(ctx)
	}
	return count, nil
}
func (s *Store) allowNotificationMasterCreation(ctx context.Context, tx pgx.Tx) (bool, error) {
	exists, err := notificationSecretsExist(ctx, tx)
	if err != nil || !exists {
		return !exists, err
	}
	rows, err := notificationSecretRows(ctx, tx)
	if err != nil {
		return false, ErrApplicationKeyVaultUnavailable
	}
	defer rows.Close()
	if !rows.Next() {
		return rows.Err() == nil, rows.Err()
	}
	var purpose string
	var values []string
	var sealed []byte
	if rows.Scan(&purpose, &values, &sealed) != nil {
		return false, ErrApplicationKeyVaultCiphertext
	}
	rows.Close()
	_, err = s.OpenNotificationSecret(ctx, purpose, NotificationSecretBinding(values...), sealed)
	return false, err
}

// NormalizeNotificationRestore runs only after raw fingerprints and the sealed
// secret witness passed. Imported registrations never resume external delivery.
func NormalizeNotificationRestore(ctx context.Context, tx pgx.Tx) error {
	exists, err := notificationSecretsExist(ctx, tx)
	if err != nil || !exists {
		return err
	}
	var overflow bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notification_transport WHERE enabled AND revision=9223372036854775807) OR EXISTS(SELECT 1 FROM notification_registrations WHERE enabled AND revision=9223372036854775807)`).Scan(&overflow); err != nil {
		return ErrApplicationKeyBackupInvalid
	}
	if overflow {
		return ErrApplicationKeyBackupInvalid
	}
	_, err = tx.Exec(ctx, `UPDATE notification_transport SET enabled=false,revision=revision+1 WHERE enabled;
	UPDATE notification_registrations SET enabled=false,revision=revision+1,last_outcome='backup_restored',updated_at=clock_timestamp() WHERE enabled;
	UPDATE notification_deliveries SET state='cancelled',lease_id='',lease_until=NULL,refs='[]',outcome='backup_restored',updated_at=clock_timestamp() WHERE state IN ('pending','sending')`)
	return err
}

// Keep compile-time dependencies small; callers form generation bindings using
// decimal integer strings rather than lossy JSON numbers.
func NotificationGeneration(value int64) string { return strconv.FormatInt(value, 10) }

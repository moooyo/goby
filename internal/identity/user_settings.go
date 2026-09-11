package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	MaxUserSettingsBytes      = 128 * 1024
	MaxUserSettingsEntries    = 256
	MaxUserSettingsKeyBytes   = 256
	MaxUserSettingsValueBytes = 16 * 1024
)

var ErrUserSettingsLimit = errors.New("user settings exceed their limits")
var ErrStoredUserSettings = errors.New("stored user settings are invalid")

// UserSettingsPatch retains the distinction between a string and deletion.
// These preferences belong to the user across login sessions and devices.
type UserSettingsPatch map[string]*string

// GetUserSettings reads the persisted user-wide preferences. Missing rows are
// the actual empty default; reading does not create or update a preference row.
func (s *Store) GetUserSettings(ctx context.Context, actor Principal, userID string) (map[string]string, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin user settings read: %w", err)
	}
	defer rollback(tx)
	if err := authorizeUserSettings(ctx, tx, actor, userID, true); err != nil {
		return nil, err
	}
	values, err := readUserSettings(ctx, tx, userID, false)
	if err != nil {
		return nil, err
	}
	if err := authorizeUserSettings(ctx, tx, actor, userID, false); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit user settings read: %w", err)
	}
	return values, nil
}

// PatchUserSettings merges only supplied keys into the latest locked document.
// A null or empty string removes a key; no-op patches retain the timestamp. The
// account's policy, UserConfiguration and management revision remain separate.
func (s *Store) PatchUserSettings(ctx context.Context, actor Principal, userID string, input UserSettingsPatch) error {
	patch, err := cloneUserSettingsPatch(input)
	if err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin user settings mutation: %w", err)
	}
	defer rollback(tx)
	if err := authorizeUserSettings(ctx, tx, actor, userID, true); err != nil {
		return err
	}
	if len(patch) == 0 {
		return authorizeUserSettings(ctx, tx, actor, userID, false)
	}
	// The unique key serializes even concurrent first writes. The insert and
	// merge share the transaction, so failed validation cannot leave an empty row.
	if _, err := tx.Exec(ctx, `INSERT INTO user_settings(user_id) VALUES($1)
		ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
		return fmt.Errorf("initialize user settings: %w", err)
	}
	values, err := readUserSettings(ctx, tx, userID, true)
	if err != nil {
		return err
	}
	if err := authorizeUserSettings(ctx, tx, actor, userID, false); err != nil {
		return err
	}
	before, _ := json.Marshal(values)
	for key, value := range patch {
		for existing := range values {
			if strings.EqualFold(existing, key) {
				key = existing
				break
			}
		}
		if value == nil || *value == "" {
			delete(values, key)
		} else {
			values[key] = *value
		}
	}
	encoded, err := encodeUserSettings(values)
	if err != nil {
		return err
	}
	if bytes.Equal(before, encoded) {
		return authorizeUserSettings(ctx, tx, actor, userID, false)
	}
	if _, err := tx.Exec(ctx, `UPDATE user_settings SET settings=$2::jsonb,
		updated_at=clock_timestamp() WHERE user_id=$1`, userID, encoded); err != nil {
		return fmt.Errorf("update user settings: %w", err)
	}
	if err := authorizeUserSettings(ctx, tx, actor, userID, false); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit user settings mutation: %w", err)
	}
	return nil
}

func authorizeUserSettings(ctx context.Context, tx pgx.Tx, actor Principal, userID string, lock bool) error {
	if !validRevalidationID(userID) {
		return ErrInvalidInput
	}
	check := func() error {
		if actor.IsApplicationKey() {
			return CheckAdministrator(ctx, tx, actor, AdministratorEmby, false)
		}
		if actor.Kind != "emby" || actor.ApplicationKeyID != 0 || actor.ClientSessionID != "" ||
			!validRevalidationID(actor.User.ID) || !validRevalidationID(actor.SessionID) {
			return ErrUnauthorized
		}
		var administrator bool
		err := tx.QueryRow(ctx, `SELECT u.is_administrator FROM users u
			JOIN sessions s ON s.user_id=u.id WHERE u.id=$1 AND s.id=$2
			AND s.kind='emby' AND s.revoked_at IS NULL AND NOT u.is_disabled
			AND s.expires_at>clock_timestamp()`, actor.User.ID, actor.SessionID).Scan(&administrator)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		if err != nil {
			return fmt.Errorf("authorize user settings: %w", err)
		}
		if userID != actor.User.ID && !administrator {
			return ErrClientSessionForbidden
		}
		return nil
	}
	if err := check(); err != nil || !lock {
		return err
	}
	// Account management locks all actor/target accounts in this same order
	// before any credential. Acquiring a target after a session could deadlock.
	ids := []string{userID}
	if !actor.IsApplicationKey() && actor.User.ID != userID {
		ids = append(ids, actor.User.ID)
	}
	rows, err := tx.Query(ctx, `SELECT id FROM users WHERE id=ANY($1::text[]) ORDER BY id FOR SHARE`, ids)
	if err != nil {
		return fmt.Errorf("lock user settings accounts: %w", err)
	}
	found := false
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return errors.New("read user settings account lock")
		}
		found = found || id == userID
	}
	rows.Close()
	if rows.Err() != nil {
		return fmt.Errorf("read user settings accounts: %w", rows.Err())
	}
	if !found {
		if err := check(); err != nil {
			return err
		}
		return ErrNotFound
	}
	if actor.IsApplicationKey() {
		if err := CheckAdministrator(ctx, tx, actor, AdministratorEmby, true); err != nil {
			return err
		}
	} else if _, err := lockClientSession(ctx, tx, actor, false); err != nil {
		return err
	}
	return check()
}

func readUserSettings(ctx context.Context, tx pgx.Tx, userID string, lock bool) (map[string]string, error) {
	query := `SELECT settings FROM user_settings WHERE user_id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	var data []byte
	err := tx.QueryRow(ctx, query, userID).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read user settings: %w", err)
	}
	var values map[string]string
	// A JSON null cannot be accepted as the empty string by Go's string decoder.
	var members map[string]json.RawMessage
	if json.Unmarshal(data, &members) != nil || members == nil {
		return nil, ErrStoredUserSettings
	}
	for _, member := range members {
		if len(member) == 0 || member[0] != '"' {
			return nil, ErrStoredUserSettings
		}
	}
	if json.Unmarshal(data, &values) != nil {
		return nil, ErrStoredUserSettings
	}
	if _, err := encodeUserSettings(values); err != nil {
		return nil, ErrStoredUserSettings
	}
	return values, nil
}

func cloneUserSettingsPatch(input UserSettingsPatch) (UserSettingsPatch, error) {
	if len(input) > MaxUserSettingsEntries {
		return nil, ErrUserSettingsLimit
	}
	result := make(UserSettingsPatch, len(input))
	for key, value := range input {
		if err := validateUserSetting(key, value); err != nil {
			return nil, err
		}
		for existing := range result {
			if strings.EqualFold(existing, key) {
				// Ordered wire decoding resolves aliases before this boundary.
				// An unordered domain map cannot define which alias is last.
				return nil, ErrInvalidInput
			}
		}
		if value == nil {
			result[key] = nil
		} else {
			copy := *value
			result[key] = &copy
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > MaxUserSettingsBytes {
		return nil, ErrUserSettingsLimit
	}
	return result, nil
}

func encodeUserSettings(values map[string]string) ([]byte, error) {
	if values == nil {
		return nil, ErrInvalidInput
	}
	if len(values) > MaxUserSettingsEntries {
		return nil, ErrUserSettingsLimit
	}
	seen := make([]string, 0, len(values))
	for key, value := range values {
		if err := validateUserSetting(key, &value); err != nil {
			return nil, err
		}
		for _, existing := range seen {
			if strings.EqualFold(existing, key) {
				return nil, ErrInvalidInput
			}
		}
		seen = append(seen, key)
	}
	encoded, err := json.Marshal(values)
	if err != nil || len(encoded) > MaxUserSettingsBytes {
		return nil, ErrUserSettingsLimit
	}
	return encoded, nil
}

func validateUserSetting(key string, value *string) error {
	if len(key) > MaxUserSettingsKeyBytes || value != nil && len(*value) > MaxUserSettingsValueBytes {
		return ErrUserSettingsLimit
	}
	if key == "" || !utf8.ValidString(key) || strings.IndexFunc(key, unicode.IsControl) >= 0 ||
		value != nil && (!utf8.ValidString(*value) || strings.ContainsRune(*value, '\x00')) {
		return ErrInvalidInput
	}
	return nil
}

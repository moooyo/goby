package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
)

type UserPreferences struct {
	UserID        string `json:"UserId"`
	Revision      int64  `json:"Revision,string"`
	Configuration UserConfiguration
}

// Preference operations lock all involved accounts in identity-management
// order, then the actual credential. Writes use account UPDATE locks from the
// beginning so they never upgrade shared locks while updating configuration.
func authorizeUserPreferences(ctx context.Context, tx pgx.Tx, actor Principal, userID string, lock, write bool) error {
	if actor.IsApplicationKey() || actor.ApplicationKeyID != 0 || actor.ClientSessionID != "" ||
		(actor.Kind != "admin" && actor.Kind != "emby") || !validRevalidationID(actor.User.ID) ||
		!validRevalidationID(actor.SessionID) || !validRevalidationID(userID) {
		return ErrUnauthorized
	}
	check := func() error {
		if actor.Kind == "admin" {
			return CheckAdministrator(ctx, tx, actor, AdministratorNative, false)
		}
		var administrator bool
		var policyJSON json.RawMessage
		var deviceID string
		var observedAt time.Time
		err := tx.QueryRow(ctx, `SELECT u.is_administrator,u.policy,s.device_id,clock_timestamp()
			FROM users u JOIN sessions s ON s.user_id=u.id
			WHERE u.id=$1 AND s.id=$2 AND s.kind='emby' AND NOT u.is_disabled
			AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND (NOT s.local_auth OR $3)`, actor.User.ID, actor.SessionID, IsLocalPeer(actor.PeerIP)).
			Scan(&administrator, &policyJSON, &deviceID, &observedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		if err != nil {
			return fmt.Errorf("read preference authority: %w", err)
		}
		policy, err := ParseRuntimePolicy(policyJSON)
		if err != nil || deviceID != actor.Client.DeviceID || !loginPolicyAllows(policyJSON, deviceID, observedAt) ||
			!policy.EnableRemoteAccess && !IsLocalPeer(actor.PeerIP) {
			return ErrUnauthorized
		}
		if userID != actor.User.ID && !administrator || userID == actor.User.ID &&
			(!policy.EnableUserPreferenceAccess || !policy.AllowsFeature(FeaturePreferences)) {
			return ErrClientSessionForbidden
		}
		return nil
	}
	if err := check(); err != nil || !lock {
		return err
	}
	clause := " FOR SHARE"
	if write {
		clause = " FOR UPDATE"
	}
	rows, err := tx.Query(ctx, `SELECT id FROM users WHERE id=ANY($1::text[]) ORDER BY id`+clause, []string{actor.User.ID, userID})
	if err != nil {
		return fmt.Errorf("lock preference accounts: %w", err)
	}
	found := false
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		found = found || id == userID
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var credential string
	if err := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE id=$1 AND user_id=$2 AND kind=$3 FOR SHARE`,
		actor.SessionID, actor.User.ID, actor.Kind).Scan(&credential); errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorized
	} else if err != nil {
		return fmt.Errorf("lock preference credential: %w", err)
	}
	if err := check(); err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetUserPreferences(ctx context.Context, actor Principal, userID string) (UserPreferences, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserPreferences{}, err
	}
	defer rollback(tx)
	if err := authorizeUserPreferences(ctx, tx, actor, userID, true, false); err != nil {
		return UserPreferences{}, err
	}
	result, err := readUserPreferences(ctx, tx, userID)
	if err != nil {
		return UserPreferences{}, err
	}
	if err := authorizeUserPreferences(ctx, tx, actor, userID, false, false); err != nil {
		return UserPreferences{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserPreferences{}, err
	}
	return result, nil
}

func readUserPreferences(ctx context.Context, tx pgx.Tx, userID string) (UserPreferences, error) {
	var result UserPreferences
	var raw json.RawMessage
	result.UserID = userID
	if err := tx.QueryRow(ctx, `SELECT configuration,configuration_revision FROM users WHERE id=$1`, userID).Scan(&raw, &result.Revision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserPreferences{}, ErrNotFound
		}
		return UserPreferences{}, fmt.Errorf("read user configuration: %w", err)
	}
	result.Configuration = ProjectUserConfiguration(raw)
	return result, nil
}

// UpdateUserPreferences merges the supported patch in a serialized transaction.
// A nil revision is the compatibility API's atomic merge; native callers supply
// an exact positive revision. Neither policy nor management revision is changed.
func (s *Store) UpdateUserPreferences(ctx context.Context, actor Principal, userID string, revision *int64, patch UserConfigurationPatch) (UserPreferences, error) {
	if revision != nil && *revision < 1 {
		return UserPreferences{}, ErrInvalidInput
	}
	patch, pin, err := splitProfilePinPatch(patch)
	if err != nil {
		return UserPreferences{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserPreferences{}, err
	}
	defer rollback(tx)
	if pin != nil {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", managedUsersLockID); err != nil {
			return UserPreferences{}, err
		}
	}
	if err := authorizeUserPreferences(ctx, tx, actor, userID, true, true); err != nil {
		return UserPreferences{}, err
	}
	current, err := readUserPreferences(ctx, tx, userID)
	if err != nil {
		return UserPreferences{}, err
	}
	if revision != nil && *revision != current.Revision {
		return UserPreferences{}, ErrRevisionConflict
	}
	updated, err := ApplyUserConfigurationPatch(current.Configuration, patch)
	if err != nil {
		return UserPreferences{}, err
	}
	before, _ := json.Marshal(current.Configuration)
	after, _ := json.Marshal(updated)
	var beforeFields, afterFields map[string]json.RawMessage
	_ = json.Unmarshal(before, &beforeFields)
	_ = json.Unmarshal(after, &afterFields)
	changed := make(map[string]json.RawMessage)
	for name := range patch {
		field := configurationField(name)
		if !bytes.Equal(beforeFields[field], afterFields[field]) {
			changed[field] = afterFields[field]
		}
	}
	pinChanged := false
	if pin != nil {
		pinChanged, err = s.updateProfilePinPreference(ctx, tx, userID, *pin)
		if err != nil {
			return UserPreferences{}, err
		}
	}
	if len(changed) != 0 || pinChanged {
		current.Revision, err = nextPreferenceRevision(current.Revision)
		if err != nil {
			return UserPreferences{}, err
		}
		encoded, _ := json.Marshal(changed)
		// Merge only explicitly changed validated fields. Projection defaults,
		// readonly echoes and unrelated historical JSON must never rewrite the
		// persisted account document merely because one preference changed.
		command, err := tx.Exec(ctx, `UPDATE users SET configuration=configuration || $2::jsonb,
			configuration_revision=$3,updated_at=clock_timestamp() WHERE id=$1 AND jsonb_typeof(configuration)='object'
			AND octet_length((configuration || $2::jsonb)::text)<=1048576`, userID, encoded, current.Revision)
		if err != nil {
			return UserPreferences{}, fmt.Errorf("persist user configuration: %w", err)
		}
		if command.RowsAffected() != 1 {
			return UserPreferences{}, ErrStoredUserSettings
		}
		current.Configuration = updated
	}
	if pinChanged {
		auditActor, err := identityActivityActor(actor)
		if err != nil {
			return UserPreferences{}, err
		}
		if err := activity.Record(ctx, tx, activity.Event{Action: activity.ActionUserUpdated,
			Source: identityActivitySource(actor.Kind), Actor: auditActor,
			Resource: activity.Resource{Kind: activity.ResourceUser, ID: userID}, Revision: current.Revision, Count: 1}); err != nil {
			return UserPreferences{}, err
		}
	}
	if err := authorizeUserPreferences(ctx, tx, actor, userID, false, false); err != nil {
		return UserPreferences{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserPreferences{}, err
	}
	return current, nil
}

const MaxDisplayPreferencesRows = 2048
const MaxDisplayPreferencesBytes = 64 << 10

type DisplayPreferences struct {
	ID          string `json:"Id"`
	Client      string
	SortBy      string
	SortOrder   string
	CustomPrefs map[string]string
	Revision    int64 `json:"Revision,string"`
}

type DisplayPreferencesPatch map[string]json.RawMessage

func defaultDisplayPreferences(id, client string) DisplayPreferences {
	return DisplayPreferences{ID: id, Client: client, SortBy: "SortName", SortOrder: "Ascending", CustomPrefs: map[string]string{}}
}

func validDisplayScope(id, client string) bool {
	return id != "" && client != "" && validPreferenceText(id, 256) && validPreferenceText(client, 256)
}

func displayCustomPreferences(raw json.RawMessage) (map[string]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, ErrInvalidInput
	}
	values := make(map[string]string)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, ErrInvalidInput
		}
		name, valid := key.(string)
		if _, duplicate := values[name]; !valid || duplicate || len(values) >= 256 {
			return nil, ErrInvalidInput
		}
		var encoded json.RawMessage
		if decoder.Decode(&encoded) != nil || len(encoded) == 0 || encoded[0] != '"' {
			return nil, ErrInvalidInput
		}
		var value string
		if json.Unmarshal(encoded, &value) != nil {
			return nil, ErrInvalidInput
		}
		values[name] = value
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') {
		return nil, ErrInvalidInput
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrInvalidInput
	}
	return values, nil
}

func applyDisplayPreferencesPatch(current DisplayPreferences, patch DisplayPreferencesPatch) (DisplayPreferences, error) {
	values := map[string]any{"Id": current.ID, "Client": current.Client, "SortBy": current.SortBy, "SortOrder": current.SortOrder, "CustomPrefs": current.CustomPrefs}
	seen := make(map[string]bool)
	for key, raw := range patch {
		name := ""
		for allowed := range values {
			if strings.EqualFold(key, allowed) {
				name = allowed
				break
			}
		}
		if name == "" || seen[name] || !utf8.Valid(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return DisplayPreferences{}, ErrInvalidInput
		}
		if name == "CustomPrefs" {
			entries, err := displayCustomPreferences(raw)
			if err != nil {
				return DisplayPreferences{}, err
			}
			seen[name], values[name] = true, entries
			continue
		}
		seen[name], values[name] = true, raw
	}
	encoded, err := json.Marshal(values)
	if err != nil || len(encoded) > MaxDisplayPreferencesBytes {
		return DisplayPreferences{}, ErrUserSettingsLimit
	}
	var result DisplayPreferences
	if json.Unmarshal(encoded, &result) != nil || result.ID != current.ID || result.Client != current.Client ||
		!validPreferenceText(result.SortBy, 512) || result.SortOrder != "Ascending" && result.SortOrder != "Descending" ||
		result.CustomPrefs == nil || len(result.CustomPrefs) > 256 {
		return DisplayPreferences{}, ErrInvalidInput
	}
	for key, value := range result.CustomPrefs {
		if key == "" || !validPreferenceText(key, 256) || len(value) > 16<<10 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return DisplayPreferences{}, ErrInvalidInput
		}
	}
	result.Revision = current.Revision
	return result, nil
}

func readDisplayPreferences(ctx context.Context, tx pgx.Tx, userID, id, client string) (DisplayPreferences, error) {
	result := defaultDisplayPreferences(id, client)
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT preferences,revision FROM display_preferences WHERE user_id=$1 AND client=$2 AND preferences_id=$3`, userID, client, id).
		Scan(&raw, &result.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return DisplayPreferences{}, err
	}
	var patch DisplayPreferencesPatch
	if json.Unmarshal(raw, &patch) != nil {
		return DisplayPreferences{}, ErrStoredUserSettings
	}
	loaded, err := applyDisplayPreferencesPatch(result, patch)
	if err != nil {
		return DisplayPreferences{}, ErrStoredUserSettings
	}
	return loaded, nil
}

func (s *Store) GetDisplayPreferences(ctx context.Context, actor Principal, userID, id, client string) (DisplayPreferences, error) {
	if !validDisplayScope(id, client) {
		return DisplayPreferences{}, ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DisplayPreferences{}, err
	}
	defer rollback(tx)
	if err := authorizeUserPreferences(ctx, tx, actor, userID, true, false); err != nil {
		return DisplayPreferences{}, err
	}
	result, err := readDisplayPreferences(ctx, tx, userID, id, client)
	if err != nil {
		return DisplayPreferences{}, err
	}
	if err := authorizeUserPreferences(ctx, tx, actor, userID, false, false); err != nil {
		return DisplayPreferences{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DisplayPreferences{}, err
	}
	return result, nil
}

func (s *Store) UpdateDisplayPreferences(ctx context.Context, actor Principal, userID, id, client string, revision *int64, patch DisplayPreferencesPatch) (DisplayPreferences, error) {
	if !validDisplayScope(id, client) || revision != nil && *revision < 0 {
		return DisplayPreferences{}, ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DisplayPreferences{}, err
	}
	defer rollback(tx)
	if err := authorizeUserPreferences(ctx, tx, actor, userID, true, true); err != nil {
		return DisplayPreferences{}, err
	}
	current, err := readDisplayPreferences(ctx, tx, userID, id, client)
	if err != nil {
		return DisplayPreferences{}, err
	}
	if revision != nil && *revision != current.Revision {
		return DisplayPreferences{}, ErrRevisionConflict
	}
	updated, err := applyDisplayPreferencesPatch(current, patch)
	if err != nil {
		return DisplayPreferences{}, err
	}
	before, _ := json.Marshal(current)
	after, _ := json.Marshal(updated)
	if current.Revision > 0 && bytes.Equal(before, after) {
		if err := authorizeUserPreferences(ctx, tx, actor, userID, false, false); err != nil {
			return DisplayPreferences{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return DisplayPreferences{}, err
		}
		return current, nil
	}
	if current.Revision == 0 {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM display_preferences WHERE user_id=$1`, userID).Scan(&count); err != nil {
			return DisplayPreferences{}, err
		}
		if count >= MaxDisplayPreferencesRows {
			return DisplayPreferences{}, ErrUserSettingsLimit
		}
		updated.Revision = 1
	} else {
		updated.Revision, err = nextPreferenceRevision(current.Revision)
		if err != nil {
			return DisplayPreferences{}, err
		}
	}
	encoded, _ := json.Marshal(map[string]any{"Id": updated.ID, "Client": updated.Client, "SortBy": updated.SortBy, "SortOrder": updated.SortOrder, "CustomPrefs": updated.CustomPrefs})
	if _, err := tx.Exec(ctx, `INSERT INTO display_preferences(user_id,client,preferences_id,preferences,revision)
		VALUES($1,$2,$3,$4::jsonb,$5) ON CONFLICT(user_id,client,preferences_id)
		DO UPDATE SET preferences=EXCLUDED.preferences,revision=EXCLUDED.revision,updated_at=clock_timestamp()`,
		userID, client, id, encoded, updated.Revision); err != nil {
		return DisplayPreferences{}, err
	}
	if err := authorizeUserPreferences(ctx, tx, actor, userID, false, false); err != nil {
		return DisplayPreferences{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DisplayPreferences{}, err
	}
	return updated, nil
}

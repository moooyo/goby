package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/activity"
	"golang.org/x/crypto/bcrypt"
)

const maxUserCopyItemRows = 100000

type userCopySelection struct {
	policy, configuration, data bool
}

// Empty options copy no facets. The pinned SDK declares the three option names
// but no default selection; the operation never guesses a broader copy scope.
func selectUserCopyOptions(sourceID string, options []string) (userCopySelection, error) {
	var selected userCopySelection
	if sourceID != "" && !validRevalidationID(sourceID) {
		return selected, managedUserFieldError("CopyFromUserId", "copy source must identify one existing user")
	}
	if len(options) > 3 || (sourceID == "" && len(options) != 0) {
		return selected, managedUserFieldError("UserCopyOptions", "select at most three distinct copy options and supply CopyFromUserId")
	}
	seen := make(map[string]bool, len(options))
	for _, option := range options {
		if seen[option] {
			return userCopySelection{}, managedUserFieldError("UserCopyOptions", "select each copy option once")
		}
		seen[option] = true
		switch option {
		case "UserPolicy":
			selected.policy = true
		case "UserConfiguration":
			selected.configuration = true
		case "UserData":
			selected.data = true
		default:
			return userCopySelection{}, managedUserFieldError("UserCopyOptions", "copy options must be UserPolicy, UserConfiguration, or UserData")
		}
	}
	return selected, nil
}

// CreateManagedUserCopy creates a new passwordless member and copies only the
// requested supported facets. Credentials, roles, disabled/lockout state,
// session history, device registrations and sharing grants remain independent.
// The source and actor accounts use the same lock order and final authorization
// check as ordinary managed-user writes; nothing is committed on a copy error.
func (s *Store) CreateManagedUserCopy(ctx context.Context, actor Principal, name, sourceID string, options []string) (User, error) {
	selected, err := selectUserCopyOptions(sourceID, options)
	if err != nil {
		return User{}, err
	}
	if sourceID == "" {
		return s.CreateManagedUser(ctx, actor, name, "", false)
	}
	name, normalized, err := normalizeName(name)
	if err != nil {
		return User{}, managedUserFieldError("Name", managedInputMessage(err))
	}
	if !validManagedActor(actor) {
		return User{}, ErrUnauthorized
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(""), passwordCost)
	if err != nil {
		return User{}, fmt.Errorf("hash copied user password: %w", err)
	}
	id, err := randomID()
	if err != nil {
		return User{}, err
	}
	tx, source, err := s.beginManagedUserMutation(ctx, actor, sourceID, false)
	if err != nil {
		return User{}, err
	}
	defer rollback(tx)
	policyJSON, configurationJSON := []byte(`{}`), []byte(`{}`)
	if selected.policy {
		policy, err := ParseManagedPolicy(source.User.Policy)
		if err != nil {
			return User{}, managedUserFieldError("CopyFromUserId", "the source has invalid supported policy values")
		}
		if err := validateManagedLibraries(ctx, tx, policy.EnabledFolders); err != nil {
			return User{}, err
		}
		if err := validateManagedLibraries(ctx, tx, policy.EnableContentDeletionFromFolders); err != nil {
			return User{}, err
		}
		// Marshaling the typed allowlist strips opaque stored values, role mirrors
		// and authentication state rather than duplicating the raw JSON document.
		policyJSON, err = json.Marshal(policy)
		if err != nil {
			return User{}, fmt.Errorf("encode copied user policy: %w", err)
		}
	}
	if selected.configuration {
		configurationJSON, err = copyUserConfiguration(source.User.Configuration)
		if err != nil {
			return User{}, err
		}
	}
	if selected.data {
		if err := lockCopiedUserDataItems(ctx, tx, sourceID); err != nil {
			return User{}, err
		}
	}
	user, err := scanUser(tx.QueryRow(ctx, `INSERT INTO users
		(id, name, normalized_name, password_hash, has_password, is_administrator, is_disabled, policy, configuration)
		VALUES ($1, $2, $3, $4, false, false, false, $5::jsonb, $6::jsonb) RETURNING `+userColumns,
		id, name, normalized, string(hash), policyJSON, configurationJSON))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_normalized_name_key" {
		return User{}, managedUserFieldError("Name", "username is already in use")
	}
	if err != nil {
		return User{}, fmt.Errorf("create copied user: %w", err)
	}
	if selected.data {
		if _, err := tx.Exec(ctx, `INSERT INTO user_item_data
			(user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_at)
			SELECT $1, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_at
			FROM user_item_data WHERE user_id=$2 ORDER BY item_id`, id, sourceID); err != nil {
			return User{}, fmt.Errorf("copy user media state: %w", err)
		}
	}
	auditActor, err := identityActivityActor(actor)
	if err != nil {
		return User{}, err
	}
	changed := []activity.Field{activity.FieldName, activity.FieldIsAdministrator}
	if selected.policy {
		changed = append(changed, activity.FieldPolicy)
	}
	if err := activity.Record(ctx, tx, activity.Event{
		Action: activity.ActionUserCreated, Source: identityActivitySource(actor.Kind), Actor: auditActor,
		Resource: activity.Resource{Kind: activity.ResourceUser, ID: user.ID}, Revision: 1, Count: 1,
		ChangedFields: changed,
	}); err != nil {
		return User{}, err
	}
	if err := recheckManagedMutation(ctx, tx, actor, false, nil); err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit copied user creation: %w", err)
	}
	return user, nil
}

func lockCopiedUserDataItems(ctx context.Context, tx pgx.Tx, sourceID string) error {
	// Account locks already block normal source-state writes. Lock referenced
	// items before reading their data so scanning/deletion cannot remove the
	// foreign-key targets midway through the copy. No playback sessions move.
	rows, err := tx.Query(ctx, `SELECT i.id FROM items i JOIN user_item_data d ON d.item_id=i.id
		WHERE d.user_id=$1 ORDER BY i.id LIMIT $2 FOR KEY SHARE OF i`, sourceID, maxUserCopyItemRows+1)
	if err != nil {
		return fmt.Errorf("lock copied user media items: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("read copied user media item: %w", err)
		}
		count++
		if count > maxUserCopyItemRows {
			return managedUserFieldError("UserCopyOptions", "UserData exceeds the 100000-item copy limit")
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read copied user media items: %w", err)
	}
	return nil
}

// Only preferences already supported by the UserConfiguration DTO are copied.
// Opaque values and PIN/password fields are never transferred to a new user.
func copyUserConfiguration(raw json.RawMessage) ([]byte, error) {
	fail := func() ([]byte, error) {
		return nil, managedUserFieldError("CopyFromUserId", "the source has invalid supported user configuration")
	}
	var values map[string]json.RawMessage
	if len(raw) == 0 || len(raw) > 1<<20 || !utf8.Valid(raw) || json.Unmarshal(raw, &values) != nil || values == nil || len(values) > 256 {
		return fail()
	}
	result := make(map[string]json.RawMessage)
	flags := []string{"DisplayMissingEpisodes", "EnableLocalPassword", "EnableNextEpisodeAutoPlay", "HidePlayedInLatest",
		"HidePlayedInMoreLikeThis", "HidePlayedInSuggestions", "PlayDefaultAudioTrack", "RememberAudioSelections", "RememberSubtitleSelections"}
	lists := []string{"LatestItemsExcludes", "MyMediaExcludes", "OrderedViews"}
	known := append(append([]string{}, flags...), lists...)
	known = append(known, "IntroSkipMode", "SubtitleMode", "ResumeRewindSeconds")
	for name, raw := range values {
		for _, canonical := range known {
			if strings.EqualFold(name, canonical) && name != canonical {
				return fail()
			}
		}
		switch {
		case slices.Contains(flags, name):
			var value *bool
			if json.Unmarshal(raw, &value) != nil || value == nil {
				return fail()
			}
		case slices.Contains(lists, name):
			var entries []json.RawMessage
			if json.Unmarshal(raw, &entries) != nil || entries == nil || len(entries) > 1024 {
				return fail()
			}
			for _, entry := range entries {
				var value string
				if json.Unmarshal(entry, &value) != nil || !validRevalidationID(value) {
					return fail()
				}
			}
		case name == "IntroSkipMode" || name == "SubtitleMode":
			var value string
			allowed := []string{"ShowButton", "AutoSkip", "None"}
			if name == "SubtitleMode" {
				allowed = []string{"Default", "Always", "OnlyForced", "None", "Smart", "HearingImpaired"}
			}
			if json.Unmarshal(raw, &value) != nil || !slices.Contains(allowed, value) {
				return fail()
			}
		case name == "ResumeRewindSeconds":
			var value *int32
			if json.Unmarshal(raw, &value) != nil || value == nil || *value < 0 {
				return fail()
			}
		default:
			continue
		}
		result[name] = append(json.RawMessage(nil), raw...)
	}
	return json.Marshal(result)
}

package identity_test

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestUserConfigurationMergePreservesHistoricalJSONAndIndependentRevisions(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	bootstrapTestAdmin(t, ctx, store)
	viewer, err := store.CreateUser(ctx, "Configuration viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, actor := clientSessionPrincipal(t, ctx, store, viewer, "viewer-password", identity.Client{DeviceID: "configuration-device"}, "emby")
	if _, err := pool.Exec(ctx, `UPDATE users SET configuration='{
		"GroupedFolders":["old-group"],"CustomPreferences":{"legacy":{"number":17}},
		"IntroSkipMode":"OldPrivateMode","HidePlayedInSuggestions":{"historical":true},"ResumeRewindSeconds":-99
	}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	snapshot := func() (string, int64, int64, string) {
		var raw, updated string
		var revision, management int64
		if err := pool.QueryRow(ctx, `SELECT (configuration-'AudioLanguagePreference')::text,configuration_revision,management_revision,updated_at::text FROM users WHERE id=$1`, viewer.ID).
			Scan(&raw, &revision, &management, &updated); err != nil {
			t.Fatal(err)
		}
		return raw, revision, management, updated
	}
	before, _, management, _ := snapshot()
	current, err := store.GetUserPreferences(ctx, actor, viewer.ID)
	if err != nil || current.Revision != 1 {
		t.Fatalf("read initial preferences: %v", err)
	}
	updated, err := store.UpdateUserPreferences(ctx, actor, viewer.ID, &current.Revision,
		identity.UserConfigurationPatch{"AudioLanguagePreference": json.RawMessage(`"eng"`)})
	if err != nil || updated.Revision != 2 || updated.Configuration.AudioLanguagePreference != "eng" {
		t.Fatalf("merge preferences: %v", err)
	}
	after, revision, managedAfter, updatedAt := snapshot()
	if after != before || revision != 2 || managedAfter != management {
		t.Fatal("preference merge rewrote historical JSON or management revision")
	}
	unchanged, err := store.UpdateUserPreferences(ctx, actor, viewer.ID, &updated.Revision, identity.UserConfigurationPatch{
		"AudioLanguagePreference": json.RawMessage(`"eng"`), "IntroSkipMode": json.RawMessage(`"None"`), "HidePlayedInSuggestions": json.RawMessage(`false`),
	})
	after, revision, _, nextUpdatedAt := snapshot()
	if err != nil || unchanged.Revision != 2 || revision != 2 || after != before || nextUpdatedAt != updatedAt {
		t.Fatal("default echo or no-op rewrote persisted JSON")
	}
	if _, err := store.UpdateUserPreferences(ctx, actor, viewer.ID, &current.Revision, identity.UserConfigurationPatch{"AudioLanguagePreference": json.RawMessage(`"fra"`)}); !errors.Is(err, identity.ErrRevisionConflict) {
		t.Fatal("stale preference CAS was accepted")
	}
	store = identity.New(pool)
	restored, err := store.GetUserPreferences(ctx, actor, viewer.ID)
	if err != nil || restored.Configuration.AudioLanguagePreference != "eng" || restored.Revision != 2 {
		t.Fatal("new store lost persisted configuration")
	}
	discovery, err := store.UpdateUserPreferences(ctx, actor, viewer.ID, &restored.Revision, identity.UserConfigurationPatch{
		"DisplayMissingEpisodes": json.RawMessage(`true`), "HidePlayedInSuggestions": json.RawMessage(`true`),
	})
	if err != nil || discovery.Revision != 3 {
		t.Fatalf("write discovery preferences with independent CAS: %v", err)
	}
	store = identity.New(pool)
	discovery, err = store.GetUserPreferences(ctx, actor, viewer.ID)
	if err != nil || discovery.Revision != 3 || !discovery.Configuration.DisplayMissingEpisodes || !discovery.Configuration.HidePlayedInSuggestions {
		t.Fatal("new store lost independently persisted discovery preferences")
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET configuration_revision=$2 WHERE id=$1`, viewer.ID, int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	maximum := int64(math.MaxInt64)
	if _, err := store.UpdateUserPreferences(ctx, actor, viewer.ID, &maximum, identity.UserConfigurationPatch{"AudioLanguagePreference": json.RawMessage(`"fra"`)}); !errors.Is(err, identity.ErrRevisionConflict) {
		t.Fatal("configuration CAS overflow was accepted")
	}
}

func TestDisplayPreferencesIsolateUserAndClientAndDoNotMutateUserSettings(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	bootstrapTestAdmin(t, ctx, store)
	viewer, err := store.CreateUser(ctx, "Display viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, "Display other", "other-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, actor := clientSessionPrincipal(t, ctx, store, viewer, "viewer-password", identity.Client{DeviceID: "display-device"}, "emby")
	_, otherActor := clientSessionPrincipal(t, ctx, store, other, "other-password", identity.Client{DeviceID: "other-device"}, "emby")
	zero := int64(0)
	prefs, err := store.UpdateDisplayPreferences(ctx, actor, viewer.ID, "shared-folder", "web", &zero, identity.DisplayPreferencesPatch{
		"SortBy": json.RawMessage(`"DateCreated"`), "SortOrder": json.RawMessage(`"Descending"`), "CustomPrefs": json.RawMessage(`{"layout":"poster"}`),
	})
	if err != nil || prefs.Revision != 1 {
		t.Fatalf("write display preferences: %v", err)
	}
	if _, err := store.UpdateDisplayPreferences(ctx, actor, viewer.ID, "shared-folder", "web", &zero, identity.DisplayPreferencesPatch{}); !errors.Is(err, identity.ErrRevisionConflict) {
		t.Fatal("stale display CAS was accepted")
	}
	unchanged, err := store.UpdateDisplayPreferences(ctx, actor, viewer.ID, "shared-folder", "web", &prefs.Revision, identity.DisplayPreferencesPatch{"SortBy": json.RawMessage(`"DateCreated"`)})
	if err != nil || unchanged.Revision != prefs.Revision {
		t.Fatal("display no-op changed its independent revision")
	}
	store = identity.New(pool)
	web, err := store.GetDisplayPreferences(ctx, actor, viewer.ID, "shared-folder", "web")
	if err != nil || web.SortBy != "DateCreated" || !reflect.DeepEqual(web.CustomPrefs, map[string]string{"layout": "poster"}) {
		t.Fatal("display preference readback changed its stored scope")
	}
	tv, err := store.GetDisplayPreferences(ctx, actor, viewer.ID, "shared-folder", "tv")
	if err != nil || tv.Revision != 0 || len(tv.CustomPrefs) != 0 {
		t.Fatal("another client inherited the first client's preferences")
	}
	foreign, err := store.GetDisplayPreferences(ctx, otherActor, other.ID, "shared-folder", "web")
	if err != nil || foreign.Revision != 0 || len(foreign.CustomPrefs) != 0 {
		t.Fatal("another user inherited display preferences")
	}
	if _, err := store.GetDisplayPreferences(ctx, otherActor, viewer.ID, "shared-folder", "web"); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatal("foreign user preference access was accepted")
	}
	var configurationRevision, settings int
	if pool.QueryRow(ctx, `SELECT configuration_revision,(SELECT count(*) FROM user_settings) FROM users WHERE id=$1`, viewer.ID).Scan(&configurationRevision, &settings) != nil || configurationRevision != 1 || settings != 0 {
		t.Fatal("display preferences changed configuration or UserSettings")
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy || '{"EnableUserPreferenceAccess":false}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDisplayPreferences(ctx, actor, viewer.ID, "shared-folder", "web"); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatal("current preference policy was not enforced")
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateUserPreferences(ctx, actor, viewer.ID, nil, identity.UserConfigurationPatch{}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("revoked preference authority was reused")
	}
}

func TestPreferenceWritersRecheckAuthorityAfterWaitingForCurrentRows(t *testing.T) {
	for _, scenario := range []string{"account policy", "credential revocation"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, pool, store := identityTestStore(t)
			bootstrapTestAdmin(t, ctx, store)
			user, err := store.CreateUser(ctx, "Waiting preference viewer", "viewer-password", false)
			if err != nil {
				t.Fatal(err)
			}
			_, actor := clientSessionPrincipal(t, ctx, store, user, "viewer-password", identity.Client{DeviceID: "waiting-preference-device"}, "emby")
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(ctx)
			var blockerPID int32
			if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
				t.Fatal(err)
			}
			lockSQL, id, fragment := "SELECT id FROM users WHERE id=$1 FOR UPDATE", user.ID, "SELECT id FROM users"
			if scenario == "credential revocation" {
				lockSQL, id, fragment = "SELECT id FROM sessions WHERE id=$1 FOR UPDATE", actor.SessionID, "SELECT id FROM sessions"
			}
			if _, err := blocker.Exec(ctx, lockSQL, id); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var writeErr error
			go func() {
				defer close(done)
				_, writeErr = store.UpdateUserPreferences(ctx, actor, user.ID, nil, identity.UserConfigurationPatch{"SubtitleMode": json.RawMessage(`"None"`)})
			}()
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, fragment, done)
			want := identity.ErrClientSessionForbidden
			if scenario == "credential revocation" {
				_, err = blocker.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.SessionID)
				want = identity.ErrUnauthorized
			} else {
				_, err = blocker.Exec(ctx, `UPDATE users SET policy=policy || '{"EnableUserPreferenceAccess":false}'::jsonb WHERE id=$1`, user.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("preference writer did not finish after its authority changed")
			}
			if !errors.Is(writeErr, want) {
				t.Fatalf("waiting preference mutation returned %v, want current-authority rejection", writeErr)
			}
			var revision int64
			var raw string
			if err := pool.QueryRow(ctx, "SELECT configuration_revision,configuration::text FROM users WHERE id=$1", user.ID).Scan(&revision, &raw); err != nil {
				t.Fatal(err)
			}
			if revision != 1 || raw != "{}" {
				t.Fatal("a rejected waiting mutation changed preferences")
			}
		})
	}
}

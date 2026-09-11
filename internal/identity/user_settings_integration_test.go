package identity_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

func userSettingText(value string) *string { return &value }

func TestUserSettingsPersistAcrossClientsAndPreserveAccountConfiguration(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	viewer, err := store.CreateUser(ctx, "Preference Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, "Preference Other", "other-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, actor := clientSessionPrincipal(t, ctx, store, viewer, "viewer-password", identity.Client{DeviceID: "first-client"}, "emby")
	_, second := clientSessionPrincipal(t, ctx, store, viewer, "viewer-password", identity.Client{DeviceID: "second-client"}, "emby")
	_, administrator := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{}, "emby")
	_, otherActor := clientSessionPrincipal(t, ctx, store, other, "other-password", identity.Client{}, "emby")
	if _, err := pool.Exec(ctx, `UPDATE users SET configuration='{"AudioLanguagePreference":"eng"}'::jsonb,
		policy=policy || '{"RetainedPreferenceSentinel":true}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var value string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
			'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
			'managed',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM managed_settings m))::text`).Scan(&value); err != nil {
			t.Fatal("snapshot unrelated configuration")
		}
		return value
	}
	before := snapshot()
	values, err := store.GetUserSettings(ctx, actor, viewer.ID)
	if err != nil || values == nil || len(values) != 0 {
		t.Fatal("missing preferences did not produce their empty default")
	}
	if err := store.PatchUserSettings(ctx, actor, viewer.ID, identity.UserSettingsPatch{}); err != nil {
		t.Fatal(err)
	}
	if err := store.PatchUserSettings(ctx, actor, viewer.ID, identity.UserSettingsPatch{"absent": nil}); err != nil {
		t.Fatal(err)
	}
	if err := store.PatchUserSettings(ctx, actor, viewer.ID, identity.UserSettingsPatch{"blank": userSettingText("")}); err != nil {
		t.Fatal(err)
	}
	var count int
	if pool.QueryRow(ctx, "SELECT count(*) FROM user_settings").Scan(&count) != nil || count != 0 {
		t.Fatal("reads and empty changes created persisted preferences")
	}
	if err := store.PatchUserSettings(ctx, actor, viewer.ID, identity.UserSettingsPatch{
		"genreLimitOnDetails": userSettingText("2"), "theme": userSettingText("dark"),
	}); err != nil {
		t.Fatal(err)
	}
	store = identity.New(pool)
	values, err = store.GetUserSettings(ctx, second, viewer.ID)
	if err != nil || !reflect.DeepEqual(values, map[string]string{"genreLimitOnDetails": "2", "theme": "dark"}) {
		t.Fatal("new reader or another client lost the same user's settings")
	}
	if err := store.PatchUserSettings(ctx, administrator, viewer.ID, identity.UserSettingsPatch{
		"genreLimitOnDetails": userSettingText("1"), "added": userSettingText("yes"), "theme": nil,
	}); err != nil {
		t.Fatal(err)
	}
	values, err = store.GetUserSettings(ctx, actor, viewer.ID)
	if err != nil || !reflect.DeepEqual(values, map[string]string{"genreLimitOnDetails": "1", "added": "yes"}) {
		t.Fatal("partial merge changed retained values or failed to remove a null key")
	}
	if err := store.PatchUserSettings(ctx, second, viewer.ID, identity.UserSettingsPatch{"ADDED": userSettingText("updated")}); err != nil {
		t.Fatal(err)
	}
	values, err = store.GetUserSettings(ctx, actor, viewer.ID)
	if err != nil || !reflect.DeepEqual(values, map[string]string{"genreLimitOnDetails": "1", "added": "updated"}) {
		t.Fatal("a differently cased update replaced the original stored key spelling")
	}
	var timestamp time.Time
	if err := pool.QueryRow(ctx, "SELECT updated_at FROM user_settings WHERE user_id=$1", viewer.ID).Scan(&timestamp); err != nil {
		t.Fatal(err)
	}
	if err := store.PatchUserSettings(ctx, actor, viewer.ID, identity.UserSettingsPatch{
		"genreLimitOnDetails": userSettingText("1"), "absent": nil,
	}); err != nil {
		t.Fatal(err)
	}
	var after time.Time
	if pool.QueryRow(ctx, "SELECT updated_at FROM user_settings WHERE user_id=$1", viewer.ID).Scan(&after) != nil || !after.Equal(timestamp) {
		t.Fatal("an unchanged preference patch updated the timestamp")
	}
	values, err = store.GetUserSettings(ctx, otherActor, other.ID)
	if err != nil || len(values) != 0 {
		t.Fatal("another user inherited the first user's preferences")
	}
	if snapshot() != before {
		t.Fatal("user preferences changed account configuration, policy, revision, or server settings")
	}
}

func TestUserSettingsRevalidateIdentityAndCurrentAdministratorRole(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	viewer, err := store.CreateUser(ctx, "Preference Authority", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	credentials, actor := clientSessionPrincipal(t, ctx, store, viewer, "viewer-password", identity.Client{}, "emby")
	_, administrator := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{}, "emby")
	_, native := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{}, "admin")
	patch := identity.UserSettingsPatch{"theme": userSettingText("dark")}
	forged := actor
	forged.User.IsAdministrator = true
	for _, caller := range []identity.Principal{actor, forged} {
		if _, err := store.GetUserSettings(ctx, caller, admin.ID); !errors.Is(err, identity.ErrClientSessionForbidden) {
			t.Fatalf("another user's settings read used claimed authority: %v", err)
		}
		if err := store.PatchUserSettings(ctx, caller, admin.ID, patch); !errors.Is(err, identity.ErrClientSessionForbidden) {
			t.Fatalf("another user's settings mutation used claimed authority: %v", err)
		}
	}
	forged = actor
	forged.User.ID = admin.ID
	for _, caller := range []identity.Principal{native, forged} {
		if _, err := store.GetUserSettings(ctx, caller, admin.ID); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("invalid credential audience or owner accessed preferences: %v", err)
		}
	}
	if _, err := store.GetUserSettings(ctx, administrator, "missing-user"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("missing authorized target was not distinguished: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator=false WHERE id=$1", admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.PatchUserSettings(ctx, administrator, viewer.ID, patch); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatalf("stale administrator retained cross-user authority: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled=true WHERE id=$1", viewer.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.PatchUserSettings(ctx, actor, viewer.ID, patch); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("disabled actor changed preferences: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled=false WHERE id=$1", viewer.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(ctx, credentials.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetUserSettings(ctx, actor, viewer.ID); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("revoked actor read preferences: %v", err)
	}
	if err := store.PatchUserSettings(ctx, actor, viewer.ID, patch); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("revoked actor changed preferences: %v", err)
	}
	var count int
	if pool.QueryRow(ctx, "SELECT count(*) FROM user_settings").Scan(&count) != nil || count != 0 {
		t.Fatal("rejected authorization created preference state")
	}
}

func TestUserSettingsConcurrentFirstWritesMergeWithoutLostKeys(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{}, "emby")
	const attempts = 24
	start := make(chan struct{})
	results := make(chan error, attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			<-start
			results <- store.PatchUserSettings(ctx, actor, admin.ID, identity.UserSettingsPatch{
				fmt.Sprintf("parallel-%02d", index): userSettingText(fmt.Sprintf("value-%02d", index)),
			})
		}(index)
	}
	close(start)
	for index := 0; index < attempts; index++ {
		if err := <-results; err != nil {
			t.Fatalf("parallel preference update: %v", err)
		}
	}
	values, err := identity.New(pool).GetUserSettings(ctx, actor, admin.ID)
	if err != nil || len(values) != attempts {
		t.Fatal("concurrent first writes lost preference keys")
	}
	for index := 0; index < attempts; index++ {
		if values[fmt.Sprintf("parallel-%02d", index)] != fmt.Sprintf("value-%02d", index) {
			t.Fatal("concurrent preference value changed")
		}
	}
}

func TestUserSettingsLimitsApplyToMergedStateAndRejectCorruption(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{}, "emby")
	seed := identity.UserSettingsPatch{}
	for index := 0; index < identity.MaxUserSettingsEntries; index++ {
		seed[fmt.Sprintf("key-%03d", index)] = userSettingText("value")
	}
	if err := store.PatchUserSettings(ctx, actor, admin.ID, seed); err != nil {
		t.Fatal(err)
	}
	row := func() string {
		var value string
		if pool.QueryRow(ctx, "SELECT to_jsonb(s)::text FROM user_settings s WHERE user_id=$1", admin.ID).Scan(&value) != nil {
			t.Fatal("read bounded preference state")
		}
		return value
	}
	before := row()
	for _, patch := range []identity.UserSettingsPatch{
		{"new-key": userSettingText("value")},
		{strings.Repeat("k", identity.MaxUserSettingsKeyBytes+1): userSettingText("value")},
		{"key-000": userSettingText(strings.Repeat("v", identity.MaxUserSettingsValueBytes+1))},
	} {
		if err := store.PatchUserSettings(ctx, actor, admin.ID, patch); !errors.Is(err, identity.ErrUserSettingsLimit) {
			t.Fatalf("oversized preferences accepted: %v", err)
		}
		if row() != before {
			t.Fatal("a rejected size limit changed preferences or timestamp")
		}
	}
	for _, patch := range []identity.UserSettingsPatch{
		{"": userSettingText("value")}, {"bad\nkey": nil}, {"key-000": userSettingText("bad\x00value")},
		{"key-000": userSettingText(string([]byte{0xff}))},
	} {
		if err := store.PatchUserSettings(ctx, actor, admin.ID, patch); !errors.Is(err, identity.ErrInvalidInput) {
			t.Fatalf("invalid preference text accepted: %v", err)
		}
		if row() != before {
			t.Fatal("rejected preference text changed state")
		}
	}
	if err := store.PatchUserSettings(ctx, actor, admin.ID, identity.UserSettingsPatch{
		"key-000": nil, "replacement": userSettingText("value"),
	}); err != nil {
		t.Fatal("a bounded delete-and-add patch exceeded the final-state limit")
	}
	if _, err := pool.Exec(ctx, `UPDATE user_settings SET settings='{"invalid":null}'::jsonb WHERE user_id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	before = row()
	if _, err := store.GetUserSettings(ctx, actor, admin.ID); !errors.Is(err, identity.ErrStoredUserSettings) {
		t.Fatalf("invalid stored null became an empty string: %v", err)
	}
	if err := store.PatchUserSettings(ctx, actor, admin.ID, identity.UserSettingsPatch{"new": userSettingText("value")}); !errors.Is(err, identity.ErrStoredUserSettings) {
		t.Fatalf("corrupted stored preferences were silently replaced: %v", err)
	}
	if row() != before {
		t.Fatal("corruption rejection modified the original record")
	}
}

func TestUserSettingsExpiryWhileWaitingForBusinessRowRollsBack(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{}, "emby")
	if err := store.PatchUserSettings(ctx, actor, admin.ID, identity.UserSettingsPatch{"retained": userSettingText("yes")}); err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	var userID string
	if blocker.QueryRow(ctx, "SELECT user_id FROM user_settings WHERE user_id=$1 FOR UPDATE", admin.ID).Scan(&userID) != nil {
		t.Fatal("lock only the owned preference row")
	}
	var expires time.Time
	if pool.QueryRow(ctx, `UPDATE sessions SET expires_at=clock_timestamp()+interval '1 second'
		WHERE id=$1 RETURNING expires_at`, actor.SessionID).Scan(&expires) != nil {
		t.Fatal("bound the owned actor lifetime")
	}
	result := make(chan error, 1)
	go func() {
		result <- store.PatchUserSettings(ctx, actor, admin.ID, identity.UserSettingsPatch{"uncommitted": userSettingText("no")})
	}()
	blocked := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
			WHERE $1::integer=ANY(pg_blocking_pids(pid)))`, int(blocker.Conn().PgConn().PID())).Scan(&blocked) != nil {
			t.Fatal("observe the owned preference lock wait")
		}
		if blocked {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("preference mutation never waited for its business row")
	}
	if delay := time.Until(expires) + 25*time.Millisecond; delay > 0 {
		time.Sleep(delay)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("actor expiry during a business wait did not reject the mutation: %v", err)
	}
	var unchanged bool
	if pool.QueryRow(ctx, `SELECT settings='{"retained":"yes"}'::jsonb FROM user_settings WHERE user_id=$1`, admin.ID).Scan(&unchanged) != nil || !unchanged {
		t.Fatal("expired mutation changed the owned preference record")
	}
}

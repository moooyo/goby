package identity_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func TestUserSettingsJSONPreservesCanonicalReadsAndNoopPatches(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	credentials, actor := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{}, "emby")
	data, err := store.GetUserSettingsJSON(ctx, actor, admin.ID)
	if err != nil || string(data) != "{}" {
		t.Fatalf("missing settings did not return the canonical empty default: %q %v", data, err)
	}
	for _, patch := range []identity.UserSettingsPatch{{}, {"absent": nil}} {
		if err := store.PatchUserSettings(ctx, actor, admin.ID, patch); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_settings").Scan(&count); err != nil || count != 0 {
		t.Fatalf("reads or missing-key deletions persisted an empty row: %d %v", count, err)
	}
	values := map[string]string{"z": "<>&", "a": "", "line": "\u2028"}
	var updatedAt time.Time
	if err := pool.QueryRow(ctx, `INSERT INTO user_settings(user_id, settings)
		VALUES($1, '{"z":"<>&","a":"","line":"\u2028"}'::jsonb)
		RETURNING updated_at`, admin.ID).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	data, err = store.GetUserSettingsJSON(ctx, actor, admin.ID)
	if err != nil || !bytes.Equal(data, want) {
		t.Fatalf("JSON read did not preserve canonical Go encoding: %q %v", data, err)
	}
	returned, err := store.GetUserSettings(ctx, actor, admin.ID)
	if err != nil || !reflect.DeepEqual(returned, values) {
		t.Fatalf("map read disagrees with JSON read: %#v %v", returned, err)
	}
	data[0] = '['
	returned["z"] = "local mutation"
	data, err = store.GetUserSettingsJSON(ctx, actor, admin.ID)
	if err != nil || !bytes.Equal(data, want) {
		t.Fatalf("later JSON read shared an earlier caller's mutable result: %q %v", data, err)
	}
	if err := store.PatchUserSettings(ctx, actor, admin.ID, identity.UserSettingsPatch{
		"Z": userSettingText("<>&"), "absent": nil,
	}); err != nil {
		t.Fatal(err)
	}
	var after time.Time
	if err := pool.QueryRow(ctx, "SELECT updated_at FROM user_settings WHERE user_id=$1", admin.ID).Scan(&after); err != nil || !after.Equal(updatedAt) {
		t.Fatalf("canonical no-op changed the timestamp: %v %v", after, err)
	}
	if err := store.PatchUserSettings(ctx, actor, admin.ID, identity.UserSettingsPatch{"z": userSettingText("updated")}); err != nil {
		t.Fatal(err)
	}
	values["z"] = "updated"
	want, err = json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	data, err = store.GetUserSettingsJSON(ctx, actor, admin.ID)
	if err != nil || !bytes.Equal(data, want) {
		t.Fatalf("JSON read reused a stale document after mutation: %q %v", data, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE user_settings SET settings='{"invalid":null}'::jsonb WHERE user_id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if data, err := store.GetUserSettingsJSON(ctx, actor, admin.ID); !errors.Is(err, identity.ErrStoredUserSettings) || data != nil {
		t.Fatalf("JSON read exposed invalid stored state: %q %v", data, err)
	}
	if err := store.Revoke(ctx, credentials.Token); err != nil {
		t.Fatal(err)
	}
	if data, err := store.GetUserSettingsJSON(ctx, actor, admin.ID); !errors.Is(err, identity.ErrUnauthorized) || data != nil {
		t.Fatalf("revoked credentials did not take precedence over stored corruption: %q %v", data, err)
	}
}

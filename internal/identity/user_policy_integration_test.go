package identity_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func assertStoredUserPolicy(t *testing.T, user identity.User, want map[string]any) {
	t.Helper()
	var actual map[string]any
	if err := json.Unmarshal(user.Policy, &actual); err != nil {
		t.Fatalf("user %s does not carry a valid stored policy: %v", user.ID, err)
	}
	if !reflect.DeepEqual(actual, want) {
		t.Errorf("user %s policy = %#v, want %#v", user.ID, actual, want)
	}
}

func TestStoreReadsCurrentPolicyAcrossUserAndSessionPaths(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	assertStoredUserPolicy(t, admin, map[string]any{})
	viewer, err := store.CreateUser(ctx, "Policy Viewer", "viewer-password", false)
	if err != nil {
		t.Fatalf("create policy viewer: %v", err)
	}
	assertStoredUserPolicy(t, viewer, map[string]any{})
	stored := map[string]any{
		"EnableMediaPlayback": true, "EnableAllFolders": false, "EnabledFolders": []any{"allowed-library"},
		"IsAdministrator": true, "SensitiveMarker": "private-policy-sentinel",
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET policy = $1::jsonb WHERE id = $2", encoded, viewer.ID); err != nil {
		t.Fatalf("update stored viewer policy: %v", err)
	}
	current, err := store.GetUser(ctx, viewer.ID)
	if err != nil {
		t.Fatalf("read user policy: %v", err)
	}
	assertStoredUserPolicy(t, current, stored)
	if current.IsAdministrator {
		t.Error("raw policy changed the authoritative administrator column")
	}
	users, err := store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list stored user policies: %v", err)
	}
	found := false
	for _, user := range users {
		if user.ID == viewer.ID {
			assertStoredUserPolicy(t, user, stored)
			found = true
		}
	}
	if !found {
		t.Fatal("policy viewer is missing from user listing")
	}
	credentials, err := store.Authenticate(ctx, viewer.Name, "viewer-password", identity.Client{}, "emby")
	if err != nil {
		t.Fatalf("authenticate policy viewer: %v", err)
	}
	assertStoredUserPolicy(t, credentials.User, stored)
	stored["EnableMediaPlayback"] = false
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = jsonb_set(policy, '{EnableMediaPlayback}', 'false'::jsonb) WHERE id = $1`, viewer.ID); err != nil {
		t.Fatalf("revoke playback in stored policy: %v", err)
	}
	principal, err := store.Resolve(ctx, credentials.Token, "emby")
	if err != nil {
		t.Fatalf("resolve current session policy: %v", err)
	}
	assertStoredUserPolicy(t, principal.User, stored)
	if principal.User.IsAdministrator {
		t.Error("session resolution trusted the raw administrator claim")
	}
	for _, value := range []any{current, credentials, principal} {
		wire, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal identity metadata safely: %v", err)
		}
		if bytes.Contains(wire, []byte("private-policy-sentinel")) || bytes.Contains(wire, []byte(`"Policy"`)) {
			t.Errorf("identity JSON encoding exposed raw policy: %s", wire)
		}
	}
}

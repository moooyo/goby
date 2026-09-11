package identity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func assertStoredUserConfiguration(t *testing.T, user identity.User, want map[string]any) {
	t.Helper()
	var actual map[string]any
	if err := json.Unmarshal(user.Configuration, &actual); err != nil {
		t.Fatalf("user %s does not carry valid stored configuration: %v", user.ID, err)
	}
	if !reflect.DeepEqual(actual, want) {
		t.Errorf("user %s configuration = %#v, want %#v", user.ID, actual, want)
	}
	wire, err := json.Marshal(user)
	if err != nil {
		t.Fatalf("marshal identity user safely: %v", err)
	}
	for _, forbidden := range []string{`"Configuration"`, `"configuration"`, "private-configuration-sentinel"} {
		if bytes.Contains(wire, []byte(forbidden)) {
			t.Errorf("identity user JSON exposed stored configuration: %s", wire)
		}
	}
}

func TestStoreReadsCurrentConfigurationAcrossUserAndSessionPaths(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	assertStoredUserConfiguration(t, admin, map[string]any{})
	viewer, err := store.CreateUser(ctx, "Configuration Viewer", "viewer-password", false)
	if err != nil {
		t.Fatalf("create configuration viewer: %v", err)
	}
	assertStoredUserConfiguration(t, viewer, map[string]any{})
	stored := map[string]any{
		"AudioLanguagePreference": "eng", "SubtitleLanguagePreference": "zho",
		"PlayDefaultAudioTrack": false, "SubtitleMode": "Smart",
		"GroupedFolders": []any{"television-library"}, "IsAdministrator": true,
		"CustomPreferences": map[string]any{"PrivateMarker": "private-configuration-sentinel", "Nested": []any{true, "value"}},
	}
	writeConfiguration := func(value map[string]any) {
		t.Helper()
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "UPDATE users SET configuration = $1::jsonb WHERE id = $2", encoded, viewer.ID); err != nil {
			t.Fatalf("update stored viewer configuration: %v", err)
		}
	}
	snapshotUser := func() string {
		t.Helper()
		var snapshot string
		if err := pool.QueryRow(ctx, "SELECT to_jsonb(u)::text FROM users u WHERE id = $1", viewer.ID).Scan(&snapshot); err != nil {
			t.Fatalf("snapshot stored configuration account: %v", err)
		}
		return snapshot
	}
	writeConfiguration(stored)
	before := snapshotUser()
	var initialCredentials identity.Credentials
	var previous identity.Principal
	for _, candidate := range []struct {
		name  string
		store *identity.Store
	}{
		{name: "original store", store: store},
		{name: "reopened store", store: identity.New(pool)},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			current, err := candidate.store.GetUser(ctx, viewer.ID)
			if err != nil {
				t.Fatalf("get stored user configuration: %v", err)
			}
			users, err := candidate.store.ListUsers(ctx)
			if err != nil {
				t.Fatalf("list stored user configuration: %v", err)
			}
			found := false
			for _, user := range users {
				if user.ID == admin.ID {
					assertStoredUserConfiguration(t, user, map[string]any{})
				}
				if user.ID == viewer.ID {
					assertStoredUserConfiguration(t, user, stored)
					found = true
				}
			}
			if !found {
				t.Fatal("configuration viewer is missing from user listing")
			}
			credentials, err := candidate.store.Authenticate(ctx, viewer.Name, "viewer-password", identity.Client{}, "emby")
			if err != nil {
				t.Fatalf("authenticate configuration viewer: %v", err)
			}
			resolved, err := candidate.store.Resolve(ctx, credentials.Token, "emby")
			if err != nil {
				t.Fatalf("resolve stored user configuration: %v", err)
			}
			refreshed, err := candidate.store.RevalidateSession(ctx, resolved)
			if err != nil {
				t.Fatalf("revalidate stored user configuration: %v", err)
			}
			for _, user := range []identity.User{current, credentials.User, resolved.User, refreshed.User} {
				assertStoredUserConfiguration(t, user, stored)
				if user.ID != viewer.ID || user.IsAdministrator || user.IsDisabled {
					t.Error("stored configuration changed the authoritative user identity or role")
				}
			}
			if resolved.CanManageServer() || refreshed.CanManageServer() {
				t.Error("stored configuration granted administrator authority to a viewer")
			}
			if candidate.store == store {
				initialCredentials, previous = credentials, resolved
			}
		})
	}
	if t.Failed() {
		t.FailNow()
	}
	if after := snapshotUser(); after != before {
		t.Fatal("user and session reads changed persisted account configuration, policy, role, or revision")
	}
	stored["AudioLanguagePreference"] = "fra"
	stored["PlayDefaultAudioTrack"] = true
	writeConfiguration(stored)
	before = snapshotUser()
	previous.User.Configuration = json.RawMessage(`{"AudioLanguagePreference":"stale","IsAdministrator":true}`)
	resolved, err := store.Resolve(ctx, initialCredentials.Token, "emby")
	if err != nil {
		t.Fatalf("resolve newly stored user configuration: %v", err)
	}
	refreshed, err := identity.New(pool).RevalidateSession(ctx, previous)
	if err != nil {
		t.Fatalf("revalidate newly stored user configuration: %v", err)
	}
	for _, principal := range []identity.Principal{resolved, refreshed} {
		assertStoredUserConfiguration(t, principal.User, stored)
		if principal.User.IsAdministrator || principal.CanManageServer() {
			t.Error("session configuration refresh changed the authoritative administrator role")
		}
	}
	if after := snapshotUser(); after != before {
		t.Fatal("session configuration refresh changed persisted account data")
	}
}

func TestApplicationKeyPrincipalRejectsUserConfiguration(t *testing.T) {
	valid := identity.Principal{Kind: identity.ApplicationKeyKind, ApplicationKeyID: 12, SessionID: "credential", ClientSessionID: "client"}
	if !valid.IsApplicationKey() || !valid.CanManageServer() {
		t.Fatal("empty user snapshot does not retain application key authority")
	}
	valid.User.Configuration = json.RawMessage{}
	if !valid.IsApplicationKey() || !valid.CanManageServer() {
		t.Fatal("empty configuration bytes do not retain application key authority")
	}
	for _, configuration := range []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`null`), json.RawMessage(`invalid`), json.RawMessage(`{"IsAdministrator":true}`)} {
		principal := valid
		principal.User.Configuration = configuration
		if principal.IsApplicationKey() || principal.CanManageServer() {
			t.Error("user configuration was accepted as an empty application key user snapshot")
		}
		refreshed, err := identity.New(nil).RevalidateSession(context.Background(), principal)
		if !errors.Is(err, identity.ErrUnauthorized) || !reflect.DeepEqual(refreshed, identity.Principal{}) {
			t.Errorf("application key with user configuration was not rejected before database access: %v", err)
		}
	}
}

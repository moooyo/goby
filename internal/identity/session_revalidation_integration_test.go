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

func TestStoreRevalidateSessionRefreshesRolePolicyClientAndTimes(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	credentials, previous := clientSessionPrincipal(t, ctx, store, admin, "administrator-password",
		identity.Client{Name: "Original Client", DeviceID: "original-device", Device: "Original TV", Version: "1.0"}, "emby")
	initial, err := store.RevalidateSession(ctx, previous)
	if err != nil || !reflect.DeepEqual(initial, previous) {
		t.Fatalf("unchanged session did not revalidate faithfully: error = %v", err)
	}
	freshPolicy := map[string]any{
		"EnableMediaPlayback": false, "EnableAllFolders": false, "EnabledFolders": []any{"allowed-library"},
		"IsAdministrator": true, "PrivateMarker": "fresh-policy-sentinel",
	}
	encodedPolicy, err := json.Marshal(freshPolicy)
	if err != nil {
		t.Fatal(err)
	}
	var userCreated time.Time
	if err := pool.QueryRow(ctx, `UPDATE users SET name = 'Renamed Viewer', is_administrator = false,
		has_password = false, policy = $2::jsonb, created_at = now() - interval '1 year'
		WHERE id = $1 RETURNING created_at`, admin.ID, encodedPolicy).Scan(&userCreated); err != nil {
		t.Fatal(err)
	}
	freshClient := identity.Client{Name: "Current Client", DeviceID: "current-device", Device: "Current TV", Version: "2.0"}
	var expires, lastSeen time.Time
	if err := pool.QueryRow(ctx, `UPDATE sessions SET client_name = $2, device_id = $3,
		device_name = $4, client_version = $5, expires_at = now() + interval '2 days',
		last_seen_at = now() - interval '8 minutes' WHERE id = $1 RETURNING expires_at, last_seen_at`,
		previous.SessionID, freshClient.Name, freshClient.DeviceID, freshClient.Device, freshClient.Version).
		Scan(&expires, &lastSeen); err != nil {
		t.Fatal(err)
	}
	// Only the trusted identity pair and session kind survive from the previous
	// resolution. Every mutable claim is intentionally stale or contradictory.
	previous.User.Name = "Stale Name"
	previous.User.IsAdministrator = true
	previous.User.IsDisabled = true
	previous.User.HasPassword = true
	previous.User.CreatedAt = time.Time{}
	previous.User.Policy = json.RawMessage(`{"EnableAllFolders":true,"PrivateMarker":"stale-policy-sentinel"}`)
	previous.Client = identity.Client{Name: "Forged Client", DeviceID: "forged-device", Device: "Forged TV", Version: "9.9"}
	previous.ExpiresAt = time.Time{}
	previous.LastSeenAt = time.Time{}
	refreshed, err := identity.New(pool).RevalidateSession(ctx, previous)
	if err != nil {
		t.Fatalf("refresh a valid demoted Emby session: %v", err)
	}
	if refreshed.SessionID != credentials.SessionID || refreshed.User.ID != admin.ID || refreshed.Kind != "emby" ||
		refreshed.User.Name != "Renamed Viewer" || refreshed.User.IsAdministrator || refreshed.User.IsDisabled ||
		refreshed.User.HasPassword || !refreshed.User.CreatedAt.Equal(userCreated) || refreshed.Client != freshClient ||
		!refreshed.ExpiresAt.Equal(expires) || !refreshed.LastSeenAt.Equal(lastSeen) {
		t.Errorf("session revalidation did not project current database state: %+v", refreshed)
	}
	assertStoredUserPolicy(t, refreshed.User, freshPolicy)
	wire, err := json.Marshal(refreshed)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{credentials.Token, "fresh-policy-sentinel", "stale-policy-sentinel", "token_hash", "password_hash"} {
		if bytes.Contains(wire, []byte(forbidden)) {
			t.Errorf("refreshed principal JSON exposed private value %q", forbidden)
		}
	}
	var storedExpires, storedLastSeen time.Time
	if err := pool.QueryRow(ctx, "SELECT expires_at, last_seen_at FROM sessions WHERE id = $1", credentials.SessionID).
		Scan(&storedExpires, &storedLastSeen); err != nil {
		t.Fatal(err)
	}
	if !storedExpires.Equal(expires) || !storedLastSeen.Equal(lastSeen) {
		t.Error("revalidation changed session activity or login expiration")
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = true WHERE id = $1", admin.ID); err != nil {
		t.Fatal(err)
	}
	promoted, err := store.RevalidateSession(ctx, refreshed)
	if err != nil || !promoted.User.IsAdministrator {
		t.Errorf("revalidation did not reflect the current promoted role: error = %v", err)
	}
}

func TestStoreRevalidateSessionRejectsRevokedExpiredDisabledAndReboundSessions(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, previous := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{}, "emby")
	other, err := store.CreateUser(ctx, "Other Revalidation User", "other-password", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		statement string
		argument  string
	}{
		{"revoked", "UPDATE sessions SET revoked_at = now() WHERE id = $1", previous.SessionID},
		{"expired", "UPDATE sessions SET expires_at = now() - interval '1 second' WHERE id = $1", previous.SessionID},
		{"disabled", "UPDATE users SET is_disabled = true WHERE id = $1", admin.ID},
		{"current kind changed", "UPDATE sessions SET kind = 'admin' WHERE id = $1", previous.SessionID},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = false WHERE id = $1", admin.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE sessions SET kind = 'emby', revoked_at = NULL,
				created_at = now() - interval '31 days', expires_at = now() + interval '1 day' WHERE id = $1`, previous.SessionID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, test.statement, test.argument); err != nil {
				t.Fatal(err)
			}
			refreshed, err := store.RevalidateSession(ctx, previous)
			if !errors.Is(err, identity.ErrUnauthorized) || !reflect.DeepEqual(refreshed, identity.Principal{}) {
				t.Errorf("inactive session returned principal = %+v, error = %v", refreshed, err)
			}
		})
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET kind = 'emby' WHERE id = $1", previous.SessionID); err != nil {
		t.Fatal(err)
	}
	wrongOwner := previous
	wrongOwner.User.ID = other.ID
	unknownSession := previous
	unknownSession.SessionID = "unknown-session"
	unknownOwner := previous
	unknownOwner.User.ID = "unknown-user"
	_, adminCookie := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{}, "admin")
	for name, principal := range map[string]identity.Principal{
		"wrong owner": wrongOwner, "unknown session": unknownSession,
		"unknown owner": unknownOwner, "admin cookie": adminCookie,
	} {
		t.Run(name, func(t *testing.T) {
			refreshed, err := store.RevalidateSession(ctx, principal)
			if !errors.Is(err, identity.ErrUnauthorized) || !reflect.DeepEqual(refreshed, identity.Principal{}) {
				t.Errorf("invalid session owner or kind returned principal = %+v, error = %v", refreshed, err)
			}
		})
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET user_id = $2 WHERE id = $1", previous.SessionID, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevalidateSession(ctx, previous); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("a reassigned session retained its previous owner: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevalidateSession(ctx, wrongOwner); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("a deleted session owner remained authenticated: %v", err)
	}
}

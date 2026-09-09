package identity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func clientSessionPrincipal(t *testing.T, ctx context.Context, store *identity.Store, user identity.User, password string, client identity.Client, kind string) (identity.Credentials, identity.Principal) {
	t.Helper()
	credentials, err := store.Authenticate(ctx, user.Name, password, client, kind)
	if err != nil {
		t.Fatalf("authenticate client session: %v", err)
	}
	principal, err := store.Resolve(ctx, credentials.Token, kind)
	if err != nil {
		t.Fatalf("resolve client session: %v", err)
	}
	return credentials, principal
}

func TestStoreClientSessionsOwnershipCapabilitiesAndCurrentRole(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	viewer, err := store.CreateUser(ctx, "Session Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, "Other Session Viewer", "other-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, adminPrincipal := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{Name: "Admin App"}, "emby")
	_, cookiePrincipal := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{Name: "Admin Browser"}, "admin")
	client := identity.Client{Name: "Example Player", DeviceID: "shared-device", Device: "Example TV", Version: "1.2.3"}
	credentials, viewerPrincipal := clientSessionPrincipal(t, ctx, store, viewer, "viewer-password", client, "emby")
	secondCredentials, _ := clientSessionPrincipal(t, ctx, store, viewer, "viewer-password", identity.Client{DeviceID: "second-device"}, "emby")
	_, otherPrincipal := clientSessionPrincipal(t, ctx, store, other, "other-password", client, "emby")
	capabilities, err := identity.ParseClientCapabilities([]byte(`{
		"PlayableMediaTypes":["Video"],"SupportedCommands":["Play"],"SupportsMediaControl":true,
		"DeviceProfile":{"DirectPlayProfiles":[{"Type":"Video","Container":"mp4","VideoCodec":"h264"}]},
		"PushToken":"private-push-token-sentinel","UserId":"pretend-owner","Client":"pretend-client"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateClientCapabilities(ctx, viewerPrincipal, viewerPrincipal.SessionID, capabilities); err != nil {
		t.Fatalf("save own capabilities: %v", err)
	}
	store = identity.New(pool)
	sessions, err := store.ListClientSessions(ctx, viewerPrincipal, identity.ClientSessionFilter{})
	if err != nil || len(sessions) != 2 {
		t.Fatalf("ordinary session list count = %d, error = %v, want 2", len(sessions), err)
	}
	found := false
	for _, session := range sessions {
		if session.UserID != viewer.ID {
			t.Error("ordinary account saw another owner's session")
		}
		if session.SessionID == viewerPrincipal.SessionID {
			found = true
			if session.UserName != viewer.Name || session.Client != client ||
				!session.ExpiresAt.Equal(credentials.ExpiresAt) || session.CreatedAt.IsZero() || session.LastSeenAt.IsZero() ||
				!reflect.DeepEqual(session.Capabilities, capabilities) {
				t.Errorf("persisted session metadata or capabilities changed: %+v", session)
			}
		}
	}
	if !found {
		t.Fatal("calling session missing after reopening the store")
	}
	encoded, err := json.Marshal(sessions)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{credentials.Token, "private-push-token-sentinel", "pretend-owner", "pretend-client", "token_hash", "RemoteEndPoint", "password_hash"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Errorf("safe session projection exposed %q", forbidden)
		}
	}
	var stored []byte
	if err := pool.QueryRow(ctx, "SELECT client_capabilities FROM sessions WHERE id = $1", credentials.SessionID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("PushToken")) || bytes.Contains(stored, []byte("pretend")) {
		t.Error("capability persistence retained secret or identity fields")
	}
	for _, principal := range []identity.Principal{viewerPrincipal, adminPrincipal} {
		if err := store.UpdateClientCapabilities(ctx, principal, otherPrincipal.SessionID, capabilities); !errors.Is(err, identity.ErrClientSessionForbidden) {
			t.Errorf("cross-session capability update = %v, want ErrClientSessionForbidden", err)
		}
	}
	forgedRole := viewerPrincipal
	forgedRole.User.IsAdministrator = true
	sessions, err = store.ListClientSessions(ctx, forgedRole, identity.ClientSessionFilter{})
	if err != nil || len(sessions) != 2 {
		t.Errorf("claimed administrator role changed visibility: count = %d, error = %v", len(sessions), err)
	}
	forgedOwner := viewerPrincipal
	forgedOwner.User.ID = other.ID
	if _, err := store.ListClientSessions(ctx, forgedOwner, identity.ClientSessionFilter{}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("forged session owner list = %v, want ErrUnauthorized", err)
	}
	if err := store.UpdateClientCapabilities(ctx, forgedOwner, "", capabilities); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("forged session owner update = %v, want ErrUnauthorized", err)
	}
	if err := store.TouchClientSession(ctx, forgedOwner); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("forged session owner touch = %v, want ErrUnauthorized", err)
	}
	for _, filter := range []identity.ClientSessionFilter{
		{SessionID: otherPrincipal.SessionID}, {SessionID: "missing-session"},
		{SessionID: viewerPrincipal.SessionID, DeviceID: "wrong-device"},
	} {
		if listed, err := store.ListClientSessions(ctx, viewerPrincipal, filter); err != nil || len(listed) != 0 {
			t.Errorf("private or unmatched filter returned %d sessions, error = %v", len(listed), err)
		}
	}
	sessions, err = store.ListClientSessions(ctx, viewerPrincipal, identity.ClientSessionFilter{DeviceID: client.DeviceID})
	if err != nil || len(sessions) != 1 || sessions[0].SessionID != viewerPrincipal.SessionID {
		t.Errorf("device filter crossed ownership: sessions = %+v, error = %v", sessions, err)
	}
	sessions, err = store.ListClientSessions(ctx, adminPrincipal, identity.ClientSessionFilter{})
	if err != nil || len(sessions) != 4 {
		t.Fatalf("administrator Emby list count = %d, error = %v, want 4", len(sessions), err)
	}
	for _, session := range sessions {
		if session.SessionID == cookiePrincipal.SessionID {
			t.Error("administrator cookie appeared in Emby session list")
		}
	}
	if _, err := store.ListClientSessions(ctx, cookiePrincipal, identity.ClientSessionFilter{}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("administrator cookie accessed Emby session store: %v", err)
	}
	if err := store.Revoke(ctx, secondCredentials.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = true WHERE id = $1", other.ID); err != nil {
		t.Fatal(err)
	}
	sessions, err = store.ListClientSessions(ctx, adminPrincipal, identity.ClientSessionFilter{})
	if err != nil || len(sessions) != 2 {
		t.Errorf("revoked or disabled target remains visible: count = %d, error = %v", len(sessions), err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = false WHERE id = $1", admin.ID); err != nil {
		t.Fatal(err)
	}
	sessions, err = store.ListClientSessions(ctx, adminPrincipal, identity.ClientSessionFilter{})
	if err != nil || len(sessions) != 1 || sessions[0].SessionID != adminPrincipal.SessionID {
		t.Errorf("stale administrator role retained global visibility: sessions = %+v, error = %v", sessions, err)
	}
	if err := store.UpdateClientCapabilities(ctx, viewerPrincipal, "", identity.ClientCapabilities{}); err != nil {
		t.Fatalf("replace capabilities with empty snapshot: %v", err)
	}
	sessions, err = store.ListClientSessions(ctx, viewerPrincipal, identity.ClientSessionFilter{})
	if err != nil || len(sessions) != 1 || !reflect.DeepEqual(sessions[0].Capabilities, identity.ClientCapabilities{}) {
		t.Errorf("empty replacement retained prior capabilities: sessions = %+v, error = %v", sessions, err)
	}
}

func TestStoreClientSessionsRejectStalePrincipals(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, principal := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{}, "emby")
	for _, state := range []string{"revoked", "expired", "disabled", "wrong-kind"} {
		t.Run(state, func(t *testing.T) {
			if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = false WHERE id = $1", admin.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE sessions SET kind = 'emby', revoked_at = NULL,
				created_at = now() - interval '31 days', expires_at = now() + interval '30 days'
				WHERE id = $1`, principal.SessionID); err != nil {
				t.Fatal(err)
			}
			statement := map[string]string{
				"revoked":    "UPDATE sessions SET revoked_at = now() WHERE id = $1",
				"expired":    "UPDATE sessions SET expires_at = now() - interval '1 second' WHERE id = $1",
				"disabled":   "UPDATE users SET is_disabled = true WHERE id = $1",
				"wrong-kind": "UPDATE sessions SET kind = 'admin' WHERE id = $1",
			}[state]
			id := principal.SessionID
			if state == "disabled" {
				id = admin.ID
			}
			if _, err := pool.Exec(ctx, statement, id); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ListClientSessions(ctx, principal, identity.ClientSessionFilter{}); !errors.Is(err, identity.ErrUnauthorized) {
				t.Errorf("stale principal list = %v, want ErrUnauthorized", err)
			}
			if err := store.UpdateClientCapabilities(ctx, principal, "", identity.ClientCapabilities{}); !errors.Is(err, identity.ErrUnauthorized) {
				t.Errorf("stale principal update = %v, want ErrUnauthorized", err)
			}
			if err := store.TouchClientSession(ctx, principal); !errors.Is(err, identity.ErrUnauthorized) {
				t.Errorf("stale principal activity = %v, want ErrUnauthorized", err)
			}
		})
	}
}

func TestStoreClientSessionPresenceDoesNotExpireAuthentication(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, caller := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{DeviceID: "caller"}, "emby")
	credentials, idle := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{DeviceID: "idle"}, "emby")
	if _, err := pool.Exec(ctx, "UPDATE sessions SET last_seen_at = now() - interval '6 minutes' WHERE id = $1", idle.SessionID); err != nil {
		t.Fatal(err)
	}
	if sessions, err := store.ListClientSessions(ctx, caller, identity.ClientSessionFilter{}); err != nil || len(sessions) != 1 {
		t.Errorf("default presence window returned %d sessions, error = %v", len(sessions), err)
	}
	for _, seconds := range []int{0, 600} {
		if sessions, err := store.ListClientSessions(ctx, caller, identity.ClientSessionFilter{ActiveWithinSeconds: &seconds}); err != nil || len(sessions) != 2 {
			t.Errorf("activity window %d returned %d sessions, error = %v", seconds, len(sessions), err)
		}
	}
	if _, err := store.Resolve(ctx, credentials.Token, "emby"); err != nil {
		t.Fatalf("presence timeout invalidated login: %v", err)
	}
	if err := store.TouchClientSession(ctx, idle); err != nil {
		t.Fatalf("touch idle session: %v", err)
	}
	var touched, expires time.Time
	if err := pool.QueryRow(ctx, "SELECT last_seen_at, expires_at FROM sessions WHERE id = $1", idle.SessionID).Scan(&touched, &expires); err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(credentials.ExpiresAt) {
		t.Error("session activity extended login expiration")
	}
	if sessions, err := store.ListClientSessions(ctx, caller, identity.ClientSessionFilter{}); err != nil || len(sessions) != 2 {
		t.Errorf("touched session did not return to presence list: count = %d, error = %v", len(sessions), err)
	}
	if err := store.TouchClientSession(ctx, idle); err != nil {
		t.Fatal(err)
	}
	var touchedAgain time.Time
	if err := pool.QueryRow(ctx, "SELECT last_seen_at FROM sessions WHERE id = $1", idle.SessionID).Scan(&touchedAgain); err != nil {
		t.Fatal(err)
	}
	if !touched.Equal(touchedAgain) {
		t.Error("consecutive activity writes ignored the touch interval")
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = now() - interval '31 days',
		expires_at = now() - interval '1 second' WHERE id = $1`, idle.SessionID); err != nil {
		t.Fatal(err)
	}
	unboundedPresence := 0
	if sessions, err := store.ListClientSessions(ctx, caller, identity.ClientSessionFilter{ActiveWithinSeconds: &unboundedPresence}); err != nil || len(sessions) != 1 {
		t.Errorf("disabled presence filter exposed expired authentication: count = %d, error = %v", len(sessions), err)
	}
}

func TestStoreClientSessionListAndCapabilityLimits(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, principal := clientSessionPrincipal(t, ctx, store, admin, "administrator-password", identity.Client{}, "emby")
	// Synthetic sessions exercise the SQL limit without hundreds of bcrypt calls.
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, kind, expires_at)
		SELECT 'limit-session-' || n::text, $1, decode(lpad(to_hex(n), 64, '0'), 'hex'),
		'emby', now() + interval '1 day' FROM generate_series(1, $2::int) AS n`,
		admin.ID, identity.MaxClientSessions+1); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListClientSessions(ctx, principal, identity.ClientSessionFilter{})
	if err != nil || len(sessions) != identity.MaxClientSessions {
		t.Errorf("default bounded list count = %d, error = %v", len(sessions), err)
	}
	sessions, err = store.ListClientSessions(ctx, principal, identity.ClientSessionFilter{Limit: 1})
	if err != nil || len(sessions) != 1 {
		t.Errorf("explicit bounded list count = %d, error = %v", len(sessions), err)
	}
	for _, limit := range []int{-1, identity.MaxClientSessions + 1} {
		if _, err := store.ListClientSessions(ctx, principal, identity.ClientSessionFilter{Limit: limit}); !errors.Is(err, identity.ErrInvalidInput) {
			t.Errorf("invalid limit %d returned %v", limit, err)
		}
	}
	for _, seconds := range []int{-1, 30*24*60*60 + 1} {
		if _, err := store.ListClientSessions(ctx, principal, identity.ClientSessionFilter{ActiveWithinSeconds: &seconds}); !errors.Is(err, identity.ErrInvalidInput) {
			t.Errorf("invalid activity window %d returned %v", seconds, err)
		}
	}
	for _, capabilities := range []identity.ClientCapabilities{
		{DeviceProfile: json.RawMessage(`[]`)},
		{DeviceProfile: json.RawMessage(`{"DirectPlayProfiles":[{"Type":"Invalid"}]}`)},
		{SupportedCommands: make([]string, identity.MaxClientCapabilityEntries+1)},
	} {
		if err := store.UpdateClientCapabilities(ctx, principal, "", capabilities); !errors.Is(err, identity.ErrInvalidInput) {
			t.Errorf("store accepted an invalid programmatic capability value: %v", err)
		}
	}
	for _, value := range []string{`[]`, fmt.Sprintf(`{"data":"%s"}`, string(bytes.Repeat([]byte{'x'}, 131073)))} {
		if _, err := pool.Exec(ctx, "UPDATE sessions SET client_capabilities = $2::jsonb WHERE id = $1", principal.SessionID, value); err == nil {
			t.Error("database accepted invalid capability object or storage size")
		}
	}
}

package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func sessionAuthorityProjection(principal identity.Principal) identity.Principal {
	principal.User.Name = ""
	principal.User.HasPassword = false
	principal.User.HasLocalPassword = false
	principal.User.HasProfilePin = false
	principal.User.CreatedAt = time.Time{}
	principal.User.Configuration = nil
	return principal
}

func TestStoreRevalidateSessionAuthorityOmitsAccountDisplayAndConfiguration(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, previous := clientSessionPrincipal(t, ctx, store, admin, "administrator-password",
		identity.Client{Name: "Authority client", DeviceID: "authority-device", Device: "Authority player", Version: "1"}, "emby")
	configuration, err := json.Marshal(map[string]string{"PrivateConfiguration": strings.Repeat("private payload ", 6000)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET name = 'Fresh authority owner', is_administrator = false,
		policy = '{"EnableMediaPlayback":false}'::jsonb, configuration = $2::jsonb WHERE id = $1`, admin.ID, configuration); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET last_seen_at = clock_timestamp() - interval '2 minutes' WHERE id = $1", previous.SessionID); err != nil {
		t.Fatal(err)
	}
	full, err := store.RevalidateSession(ctx, previous)
	if err != nil || len(full.User.Configuration) < 64<<10 || full.User.Name != "Fresh authority owner" || full.User.IsAdministrator {
		t.Fatalf("full revalidation lost the current account projection: %v", err)
	}
	narrow, err := store.RevalidateSessionAuthority(ctx, previous)
	if err != nil || !reflect.DeepEqual(narrow, sessionAuthorityProjection(full)) {
		t.Fatalf("authority projection changed current authentication facts: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	transactional, err := identity.RevalidateSessionInTransaction(ctx, tx, previous)
	if err != nil || !reflect.DeepEqual(transactional, full) {
		t.Fatalf("transaction revalidation lost the full configuration contract: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.TouchClientSessionFromAddress(ctx, narrow, "192.0.2.45"); err != nil {
		t.Fatalf("authority-only principal could not record client activity: %v", err)
	}
	after, err := store.RevalidateSession(ctx, narrow)
	if err != nil || !after.LastSeenAt.After(full.LastSeenAt) || !after.ExpiresAt.Equal(full.ExpiresAt) ||
		!reflect.DeepEqual(after.User, full.User) {
		t.Fatalf("activity from a narrow principal changed account or expiration facts: %v", err)
	}
}

func TestStoreRevalidateSessionAuthorityPreservesCurrentLoginChecks(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, previous := clientSessionPrincipal(t, ctx, store, admin, "administrator-password",
		identity.Client{Name: "Authority policy client", DeviceID: "policy-device"}, "emby")
	for _, test := range []struct {
		name      string
		statement string
		peer      string
		allowed   bool
	}{
		{"default", "", "192.0.2.45", true},
		{"remote policy denied", `UPDATE users SET policy = '{"EnableRemoteAccess":false}'::jsonb WHERE id = $1`, "192.0.2.45", false},
		{"local policy allowed", `UPDATE users SET policy = '{"EnableRemoteAccess":false}'::jsonb WHERE id = $1`, "127.0.0.1", true},
		{"invalid policy", `UPDATE users SET policy = '{"EnableRemoteAccess":null}'::jsonb WHERE id = $1`, "127.0.0.1", false},
		{"device denied", `UPDATE users SET policy = '{"EnableAllDevices":false,"EnabledDevices":["other-device"]}'::jsonb WHERE id = $1`, "127.0.0.1", false},
		{"device allowed", `UPDATE users SET policy = '{"EnableAllDevices":false,"EnabledDevices":["policy-device"]}'::jsonb WHERE id = $1`, "127.0.0.1", true},
		{"disabled", "UPDATE users SET is_disabled = true WHERE id = $1", "127.0.0.1", false},
		{"revoked", "UPDATE sessions SET revoked_at = clock_timestamp() WHERE user_id = $1", "127.0.0.1", false},
		{"expired", "UPDATE sessions SET expires_at = clock_timestamp() - interval '1 second' WHERE user_id = $1", "127.0.0.1", false},
		{"local authentication remote denied", "UPDATE sessions SET local_auth = true WHERE user_id = $1", "192.0.2.45", false},
		{"local authentication local allowed", "UPDATE sessions SET local_auth = true WHERE user_id = $1", "127.0.0.1", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = false, policy = '{}'::jsonb WHERE id = $1", admin.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE sessions SET local_auth = false, revoked_at = NULL,
				created_at = clock_timestamp() - interval '1 day', expires_at = clock_timestamp() + interval '1 day' WHERE user_id = $1`, admin.ID); err != nil {
				t.Fatal(err)
			}
			if test.statement != "" {
				if _, err := pool.Exec(ctx, test.statement, admin.ID); err != nil {
					t.Fatal(err)
				}
			}
			principal := previous
			principal.PeerIP = test.peer
			full, fullErr := store.RevalidateSession(ctx, principal)
			narrow, narrowErr := store.RevalidateSessionAuthority(ctx, principal)
			if test.allowed {
				if fullErr != nil || narrowErr != nil || !reflect.DeepEqual(narrow, sessionAuthorityProjection(full)) {
					t.Fatalf("valid authority projection differs: full=%v narrow=%v", fullErr, narrowErr)
				}
			} else if !errors.Is(fullErr, identity.ErrUnauthorized) || !errors.Is(narrowErr, identity.ErrUnauthorized) ||
				!reflect.DeepEqual(narrow, identity.Principal{}) {
				t.Fatalf("authority projection bypassed current authentication: full=%v narrow=%v", fullErr, narrowErr)
			}
		})
	}
}

func TestStoreRevalidateSessionAuthorityPreservesApplicationKeyBindings(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Authority application")
	previous := bindApplicationClient(t, ctx, store, key, identity.Client{Name: "Authority client", DeviceID: "authority-context", Device: "Authority player", Version: "1"})
	previous.PeerIP = "192.0.2.45"
	if _, err := pool.Exec(ctx, "UPDATE application_key_clients SET last_seen_at = clock_timestamp() - interval '2 minutes' WHERE id = $1", previous.ClientSessionID); err != nil {
		t.Fatal(err)
	}
	full, err := store.RevalidateSession(ctx, previous)
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := store.RevalidateSessionAuthority(ctx, previous)
	if err != nil || !reflect.DeepEqual(narrow, full) || !narrow.IsApplicationKey() || narrow.PeerIP != previous.PeerIP {
		t.Fatalf("application authority projection changed current binding or peer: %v", err)
	}
	if err := store.TouchClientSessionFromAddress(ctx, narrow, previous.PeerIP); err != nil {
		t.Fatalf("narrow application principal could not record activity: %v", err)
	}
	touched, err := store.RevalidateSessionAuthority(ctx, narrow)
	if err != nil || !touched.LastSeenAt.After(narrow.LastSeenAt) {
		t.Fatalf("application activity was not refreshed: %v", err)
	}
	for name, mutate := range map[string]func(*identity.Principal){
		"key binding":     func(p *identity.Principal) { p.ApplicationKeyID++ },
		"client binding":  func(p *identity.Principal) { p.ClientSessionID = "unknown-client" },
		"user projection": func(p *identity.Principal) { p.User.Configuration = json.RawMessage(`{}`) },
	} {
		t.Run(name, func(t *testing.T) {
			principal := previous
			mutate(&principal)
			if _, err := store.RevalidateSessionAuthority(ctx, principal); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("narrow projection accepted an invalid application binding: %v", err)
			}
		})
	}
	if _, err := store.RevokeApplicationKey(ctx, admin, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevalidateSessionAuthority(ctx, previous); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("narrow projection accepted revoked application authority: %v", err)
	}
}

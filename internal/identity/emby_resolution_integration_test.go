package identity_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type embyResolutionQueryTrace struct {
	queries atomic.Int64
}

func (trace *embyResolutionQueryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	trace.queries.Add(1)
	return ctx
}

func (*embyResolutionQueryTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func embyResolutionReader(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*identity.Store, *embyResolutionQueryTrace) {
	t.Helper()
	trace := &embyResolutionQueryTrace{}
	config := pool.Config()
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	return identity.New(reader), trace
}

func TestResolveEmbyCredentialKindsUseOneQuery(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Resolution key")
	if _, err := pool.Exec(ctx, `UPDATE sessions SET device_name='Another bound device',client_version='other',
		last_seen_at=clock_timestamp()-interval '1 day' WHERE id=$1`, key.CredentialID); err != nil {
		t.Fatal(err)
	}
	var clientLastSeenAt time.Time
	if err := pool.QueryRow(ctx, "SELECT last_seen_at FROM application_key_clients WHERE credential_id=$1", key.CredentialID).Scan(&clientLastSeenAt); err != nil {
		t.Fatal(err)
	}
	viewer, err := store.CreateUser(ctx, "Emby resolution viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	credentials, _ := clientSessionPrincipal(t, ctx, store, viewer, "viewer-password",
		identity.Client{Name: "Resolution player", DeviceID: "resolution-device", Device: "Reported name", Version: "2.0"}, "emby")
	if _, err := pool.Exec(ctx, `UPDATE users SET local_password_hash=password_hash,
		profile_pin_ciphertext=decode(repeat('00',36),'hex'),
		policy='{"EnableRemoteAccess":true,"EnableAllDevices":true}'::jsonb,
		configuration='{"AudioLanguagePreference":"eng"}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE devices SET custom_name='Managed device name'
		WHERE id=(SELECT device_registry_id FROM sessions WHERE id=$1)`, credentials.SessionID); err != nil {
		t.Fatal(err)
	}
	native, _ := managedLogin(t, ctx, store, admin.User, "administrator-password", "admin")
	reader, trace := embyResolutionReader(t, ctx, pool)
	peer := "192.0.2.42"
	login, err := store.ResolveWithPeer(ctx, credentials.Token, "emby", peer)
	if err != nil {
		t.Fatal(err)
	}
	if !login.User.HasLocalPassword || !login.User.HasProfilePin || login.Client.Device != "Managed device name" {
		t.Fatal("login projection fixture lacks persisted credential and device metadata")
	}
	for _, test := range []struct {
		name, token string
		queries     int64
		kind        string
	}{
		{name: "login", token: credentials.Token, queries: 1, kind: "emby"},
		{name: "application key", token: key.Token, queries: 1, kind: identity.ApplicationKeyKind},
		{name: "native administrator", token: native.Token, queries: 1},
		{name: "unknown", token: strings.Repeat("A", 43), queries: 1},
		{name: "malformed", token: "malformed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace.queries.Store(0)
			principal, err := reader.ResolveEmbyForClientWithPeer(ctx, test.token, identity.Client{}, peer)
			if queries := trace.queries.Load(); queries != test.queries {
				t.Fatalf("credential resolution issued %d queries, want %d", queries, test.queries)
			}
			if test.kind == "" {
				if !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("invalid Emby credential result = %v", err)
				}
				return
			}
			if err != nil || principal.Kind != test.kind || principal.PeerIP != peer {
				t.Fatalf("credential resolution lost its kind or trusted peer: %v", err)
			}
			if test.kind == "emby" {
				if !reflect.DeepEqual(principal, login) {
					t.Fatal("combined resolution changed the exact-kind login projection")
				}
			} else if !principal.IsApplicationKey() || principal.ApplicationKeyID != key.ID ||
				principal.SessionID != key.CredentialID || principal.Client != key.Client ||
				!principal.ExpiresAt.IsZero() || !principal.LastSeenAt.Equal(clientLastSeenAt) {
				t.Fatal("application key lost its userless default-client projection")
			}
		})
	}
	for _, kind := range []string{"emby", "admin", identity.ApplicationKeyKind} {
		if _, err := reader.ResolveWithPeer(ctx, key.Token, kind, peer); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("application key crossed exact-kind resolution for %s: %v", kind, err)
		}
	}
}

func TestResolveEmbyLoginRestrictionsRemainCurrent(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	viewer := bootstrapTestAdmin(t, ctx, store)
	credentials, _ := clientSessionPrincipal(t, ctx, store, viewer, "administrator-password",
		identity.Client{Name: "Policy player", DeviceID: "policy-device"}, "emby")
	reader, trace := embyResolutionReader(t, ctx, pool)
	var observedAt time.Time
	if err := pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&observedAt); err != nil {
		t.Fatal(err)
	}
	closedStart := (observedAt.Local().Hour() + 12) % 24
	closedSchedule := fmt.Sprintf(`UPDATE users SET policy='{"AccessSchedules":[{"DayOfWeek":"Everyday","StartHour":%d,"EndHour":%d}]}'::jsonb WHERE id=$1`, closedStart, closedStart+1)
	for _, test := range []struct {
		name, mutation, restore, peer string
	}{
		{"disabled account", "UPDATE users SET is_disabled=true WHERE id=$1", "UPDATE users SET is_disabled=false WHERE id=$1", "192.168.1.2"},
		{"revoked session", "UPDATE sessions SET revoked_at=clock_timestamp() WHERE user_id=$1", "UPDATE sessions SET revoked_at=NULL WHERE user_id=$1", "192.168.1.2"},
		{"expired session", "UPDATE sessions SET created_at=clock_timestamp()-interval '2 days', expires_at=clock_timestamp()-interval '1 day' WHERE user_id=$1", "UPDATE sessions SET expires_at=clock_timestamp()+interval '1 day' WHERE user_id=$1", "192.168.1.2"},
		{"local credential", "UPDATE sessions SET local_auth=true WHERE user_id=$1", "UPDATE sessions SET local_auth=false WHERE user_id=$1", "192.0.2.42"},
		{"remote policy", `UPDATE users SET policy='{"EnableRemoteAccess":false}'::jsonb WHERE id=$1`, "UPDATE users SET policy='{}'::jsonb WHERE id=$1", "192.0.2.42"},
		{"device policy", `UPDATE users SET policy='{"EnableAllDevices":false,"EnabledDevices":[]}'::jsonb WHERE id=$1`, "UPDATE users SET policy='{}'::jsonb WHERE id=$1", "192.168.1.2"},
		{"access schedule", closedSchedule, "UPDATE users SET policy='{}'::jsonb WHERE id=$1", "192.168.1.2"},
		{"invalid policy", `UPDATE users SET policy='{"AccessSchedules":[{"DayOfWeek":"Everyday","StartHour":0,"EndHour":0}]}'::jsonb WHERE id=$1`, "UPDATE users SET policy='{}'::jsonb WHERE id=$1", "192.168.1.2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, test.mutation, viewer.ID); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := pool.Exec(ctx, test.restore, viewer.ID); err != nil {
					t.Error(err)
				}
			})
			trace.queries.Store(0)
			if _, err := reader.ResolveEmbyForClientWithPeer(ctx, credentials.Token, identity.Client{}, test.peer); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("combined resolution ignored current login restriction: %v", err)
			}
			if queries := trace.queries.Load(); queries != 1 {
				t.Fatalf("rejected login issued %d queries, want 1", queries)
			}
		})
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET local_auth=true WHERE user_id=$1", viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ResolveEmbyForClientWithPeer(ctx, credentials.Token, identity.Client{}, "192.168.1.2"); err != nil {
		t.Fatalf("local peer could not use a local credential: %v", err)
	}
}

func TestResolveEmbyApplicationKeyRequiresSidecarAndDefaultClient(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	reader, trace := embyResolutionReader(t, ctx, pool)
	for _, test := range []struct {
		name, mutation string
	}{
		{"missing sidecar", "DELETE FROM application_keys WHERE credential_id=$1"},
		{"missing default client", "DELETE FROM application_key_clients WHERE credential_id=$1 AND device_id='persistent-server-id'"},
		{"mismatched default client", "UPDATE application_key_clients SET client_name='Different default' WHERE credential_id=$1 AND device_id='persistent-server-id'"},
		{"revoked credential", "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			key := issueApplicationKey(t, ctx, store, admin, test.name)
			bindApplicationClient(t, ctx, store, key, identity.Client{Name: "Other client", DeviceID: "other-device", Device: "Other device", Version: "2.0"})
			if _, err := pool.Exec(ctx, test.mutation, key.CredentialID); err != nil {
				t.Fatal(err)
			}
			trace.queries.Store(0)
			if _, err := reader.ResolveEmby(ctx, key.Token); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("invalid key retained authentication: %v", err)
			}
			if queries := trace.queries.Load(); queries != 1 {
				t.Fatalf("rejected key issued %d queries, want 1", queries)
			}
		})
	}
}

func TestResolveEmbyDatabaseErrorsDoNotBecomeUnauthorized(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	viewer := bootstrapTestAdmin(t, ctx, store)
	credentials, _ := managedLogin(t, ctx, store, viewer, "administrator-password", "emby")
	trace := &embyResolutionQueryTrace{}
	config := pool.Config()
	config.ConnConfig.RuntimeParams["search_path"] = "pg_catalog"
	config.ConnConfig.Tracer = trace
	broken, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broken.Close)
	_, err = identity.New(broken).ResolveEmby(ctx, credentials.Token)
	var databaseError *pgconn.PgError
	if errors.Is(err, identity.ErrUnauthorized) || !errors.As(err, &databaseError) || databaseError.Code != "42P01" {
		t.Fatalf("database failure lost its underlying SQL error: %v", err)
	}
	if queries := trace.queries.Load(); queries != 1 {
		t.Fatalf("SQL failure triggered %d credential queries, want 1", queries)
	}
}

func TestResolveEmbyNullableTimesPreserveScanErrors(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	user := bootstrapTestAdmin(t, ctx, store)
	credentials, _ := managedLogin(t, ctx, store, user, "administrator-password", "emby")
	reader, trace := embyResolutionReader(t, ctx, pool)
	for _, test := range []struct {
		name, mutation, restore string
		original                time.Time
	}{
		{"account creation", "UPDATE users SET created_at='infinity' WHERE id=$1", "UPDATE users SET created_at=$2 WHERE id=$1", user.CreatedAt},
		{"session expiration", "UPDATE sessions SET expires_at='infinity' WHERE user_id=$1", "UPDATE sessions SET expires_at=$2 WHERE user_id=$1", credentials.ExpiresAt},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, test.mutation, user.ID); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := pool.Exec(ctx, test.restore, user.ID, test.original); err != nil {
					t.Error(err)
				}
			})
			if _, err := store.Resolve(ctx, credentials.Token, "emby"); err == nil || errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("exact-kind resolution did not expose its timestamp scan failure: %v", err)
			}
			trace.queries.Store(0)
			if _, err := reader.ResolveEmby(ctx, credentials.Token); err == nil || errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("combined resolution concealed its timestamp scan failure: %v", err)
			}
			if queries := trace.queries.Load(); queries != 1 {
				t.Fatalf("timestamp scan failure triggered %d queries, want 1", queries)
			}
		})
	}
}

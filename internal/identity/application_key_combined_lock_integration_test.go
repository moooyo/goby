package identity_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type applicationKeyClientQueryTrace struct {
	reads  atomic.Int64
	before func(context.Context, string)
}

func (trace *applicationKeyClientQueryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.Join(strings.Fields(data.SQL), " ")
	if strings.HasPrefix(statement, "SELECT ") || strings.HasPrefix(statement, "WITH credential ") {
		trace.reads.Add(1)
	}
	if trace.before != nil {
		trace.before(ctx, statement)
	}
	return ctx
}

func (*applicationKeyClientQueryTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func applicationKeyClientQueryStore(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*identity.Store, *applicationKeyClientQueryTrace) {
	t.Helper()
	trace := &applicationKeyClientQueryTrace{}
	configuration := pool.Config()
	configuration.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	return identity.New(reader), trace
}

func TestStoreApplicationKeyClientValidationSharesItsKeyLockRead(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Combined client reads")
	principal := bindApplicationClient(t, ctx, store, key, identity.Client{
		Name: "Bound", DeviceID: "bound-device", Device: "Bound Device", Version: "1"})
	reader, trace := applicationKeyClientQueryStore(t, ctx, pool)

	before := applicationClientActivitySnapshot(t, ctx, pool)
	if err := reader.TouchClientSession(ctx, principal); err != nil {
		t.Fatal(err)
	}
	if reads := trace.reads.Load(); reads != 5 {
		t.Fatalf("steady touch issued %d reads, want 5", reads)
	}
	if after := applicationClientActivitySnapshot(t, ctx, pool); after != before {
		t.Fatal("steady touch wrote application client activity")
	}

	for _, test := range []struct {
		name   string
		client identity.Client
		reads  int64
	}{
		{"steady_binding", principal.Client, 7},
		{"metadata_restart", identity.Client{Name: "Bound", DeviceID: "bound-device", Device: "Renamed Device", Version: "2"}, 10},
		{"first_binding", identity.Client{Name: "New Client", DeviceID: "new-device", Device: "New Device", Version: "3"}, 11},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace.reads.Store(0)
			resolved, err := reader.ResolveEmbyForClient(ctx, key.Token, test.client)
			if err != nil || resolved.Client != test.client || resolved.ApplicationKeyID != key.ID || resolved.SessionID != key.CredentialID {
				t.Fatalf("client resolution changed its credential or metadata: %v", err)
			}
			if test.name != "first_binding" && resolved.ClientSessionID != principal.ClientSessionID {
				t.Fatal("existing client resolution replaced its context")
			}
			if reads := trace.reads.Load(); reads != test.reads {
				t.Fatalf("client resolution issued %d reads, want %d", reads, test.reads)
			}
		})
	}
}

func TestStoreApplicationKeyCombinedLockRetainsCurrentCredentialPredicates(t *testing.T) {
	for _, lockMode := range []string{"SHARE", "UPDATE"} {
		t.Run(lockMode, func(t *testing.T) {
			ctx, pool, store, admin, _ := applicationKeyTestStore(t)
			// Keep malformed scopes isolated to this test schema so each SQL
			// predicate must independently reject a formerly authenticated key.
			if _, err := pool.Exec(ctx, "ALTER TABLE sessions DROP CONSTRAINT sessions_credential_scope_check"); err != nil {
				t.Fatal(err)
			}
			reader, trace := applicationKeyClientQueryStore(t, ctx, pool)
			for _, test := range []struct {
				name, mutation string
			}{
				{"kind", "UPDATE sessions SET kind = 'emby' WHERE id = $1"},
				{"user", "UPDATE sessions SET user_id = (SELECT id FROM users LIMIT 1) WHERE id = $1"},
				{"expiration", "UPDATE sessions SET expires_at = clock_timestamp() + interval '1 hour' WHERE id = $1"},
				{"revocation", "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1"},
				{"missing_credential", "DELETE FROM sessions WHERE id = $1"},
				{"missing_key", "DELETE FROM application_keys WHERE credential_id = $1"},
				{"replacement_key", `WITH previous AS (
					DELETE FROM application_keys WHERE credential_id = $1
					RETURNING credential_id, secret_ciphertext, created_by, last_used_at, ip_address, reported_device_numeric_id
				) INSERT INTO application_keys
					(credential_id, secret_ciphertext, created_by, last_used_at, ip_address, reported_device_numeric_id)
					SELECT credential_id, secret_ciphertext, created_by, last_used_at, ip_address, reported_device_numeric_id FROM previous`},
				{"missing_default", `DELETE FROM application_key_clients c USING sessions a
					WHERE a.id = $1 AND c.credential_id = a.id AND c.client_name = a.client_name AND c.device_id = a.device_id`},
			} {
				t.Run(test.name, func(t *testing.T) {
					key := issueApplicationKey(t, ctx, store, admin, "Changed authority "+test.name)
					principal := bindApplicationClient(t, ctx, store, key, identity.Client{
						Name: "Bound", DeviceID: "bound-device", Device: "Bound Device", Version: "1"})
					client := principal.Client
					if lockMode == "UPDATE" {
						client.Version = "2"
					}
					var mutated, attemptedWrite bool
					var before string
					trace.before = func(work context.Context, statement string) {
						if mutated && (strings.HasPrefix(statement, "INSERT ") || strings.HasPrefix(statement, "UPDATE ") || strings.HasPrefix(statement, "DELETE ")) {
							attemptedWrite = true
						}
						if mutated || statement != "SELECT id FROM sessions WHERE id = $1 FOR "+lockMode {
							return
						}
						// The token lookup has completed. For UPDATE, the full
						// shared transaction has also released its row locks.
						if _, err := pool.Exec(work, test.mutation, key.CredentialID); err != nil {
							t.Fatal(err)
						}
						mutated = true
						before = applicationClientActivitySnapshot(t, work, pool)
					}
					t.Cleanup(func() { trace.before = nil })
					_, err := reader.ResolveEmbyForClient(ctx, key.Token, client)
					if !mutated {
						t.Fatal("client resolution never reached the selected credential lock")
					}
					if !errors.Is(err, identity.ErrUnauthorized) {
						t.Fatalf("client resolution accepted changed credential authority: %v", err)
					}
					if attemptedWrite {
						t.Fatal("client resolution attempted a write before rejecting changed authority")
					}
					if after := applicationClientActivitySnapshot(t, ctx, pool); after != before {
						t.Fatal("rejected client resolution wrote activity or client metadata")
					}
					// An already selected context can still record activity;
					// it does not need the default used by explicit resolution.
					if test.name == "missing_default" {
						if err := reader.TouchClientSession(ctx, principal); err != nil {
							t.Fatalf("existing context activity required the missing default: %v", err)
						}
					}
				})
			}
		})
	}
}

func TestStoreApplicationKeyCombinedLockPreservesDatabaseErrors(t *testing.T) {
	for _, lockMode := range []string{"SHARE", "UPDATE"} {
		t.Run(lockMode, func(t *testing.T) {
			ctx, pool, store, admin, _ := applicationKeyTestStore(t)
			key := issueApplicationKey(t, ctx, store, admin, "Unavailable key relation")
			principal := bindApplicationClient(t, ctx, store, key, identity.Client{
				Name: "Bound", DeviceID: "bound-device", Device: "Bound Device", Version: "1"})
			reader, trace := applicationKeyClientQueryStore(t, ctx, pool)
			var changed bool
			trace.before = func(work context.Context, statement string) {
				if changed || statement != "SELECT id FROM sessions WHERE id = $1 FOR "+lockMode {
					return
				}
				if _, err := pool.Exec(work, "ALTER TABLE application_keys RENAME TO unavailable_client_keys"); err != nil {
					t.Fatal(err)
				}
				changed = true
			}
			var err error
			if lockMode == "SHARE" {
				err = reader.TouchClientSession(ctx, principal)
			} else {
				client := principal.Client
				client.Version = "2"
				_, err = reader.ResolveEmbyForClient(ctx, key.Token, client)
			}
			var databaseError *pgconn.PgError
			if !changed || errors.Is(err, identity.ErrUnauthorized) || !errors.As(err, &databaseError) || databaseError.Code != "42P01" {
				t.Fatalf("key-lock failure lost its database error: %v", err)
			}
			if !strings.Contains(err.Error(), "lock application client key") {
				t.Fatalf("key-lock database error lost its operation context: %v", err)
			}
		})
	}
}

func TestStoreApplicationKeySteadyTouchRejectsMalformedIDsBeforeQuerying(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Malformed client scope")
	principal := applicationKeyPrincipal(t, ctx, store, key)
	reader, trace := applicationKeyClientQueryStore(t, ctx, pool)
	for _, field := range []string{"credential", "client"} {
		for _, value := range []string{" leading-space", "trailing-space ", "control\ncharacter", strings.Repeat("x", 257), string([]byte{0xff})} {
			candidate := principal
			if field == "credential" {
				candidate.SessionID = value
			} else {
				candidate.ClientSessionID = value
			}
			if err := reader.TouchClientSession(ctx, candidate); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("malformed %s ID did not return unauthorized: %v", field, err)
			}
			if reads := trace.reads.Load(); reads != 0 {
				t.Fatalf("malformed %s ID issued %d reads", field, reads)
			}
		}
	}
}

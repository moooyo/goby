package identity_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type administratorReadTrace struct {
	queries atomic.Int64
	before  func(context.Context, *pgx.Conn)
	after   func()
}

func (trace *administratorReadTrace) TraceQueryStart(ctx context.Context, connection *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	trace.queries.Add(1)
	if trace.before != nil {
		trace.before(ctx, connection)
	}
	return ctx
}

func (trace *administratorReadTrace) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err == nil && trace.after != nil {
		trace.after()
	}
}

func administratorReadPool(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Pool, *administratorReadTrace) {
	t.Helper()
	trace := &administratorReadTrace{}
	configuration := pool.Config()
	configuration.MaxConns = 1
	configuration.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	return reader, trace
}

func TestCheckAdministratorReadUsesOneStatementWithoutActorLocks(t *testing.T) {
	ctx, pool, store, native, _ := applicationKeyTestStore(t)
	_, emby := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
	application := applicationKeyPrincipal(t, ctx, store, issueApplicationKey(t, ctx, store, native, "Independent administrator read"))
	reader, trace := administratorReadPool(t, ctx, pool)
	for _, test := range []struct {
		name     string
		actor    identity.Principal
		audience identity.AdministratorAudience
	}{
		{"native", native, identity.AdministratorNative},
		{"emby", emby, identity.AdministratorEmby},
		{"application", application, identity.AdministratorEmby},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace.queries.Store(0)
			tx, err := reader.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			if err := identity.CheckAdministrator(ctx, administratorQueryAdapter{tx}, test.actor, test.audience, false); err != nil {
				_ = tx.Rollback(ctx)
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if count := trace.queries.Load(); count != 3 {
				t.Fatalf("transactional read issued %d statements, want 3", count)
			}
			blocker := administratorTestTransaction(t, ctx, pool)
			if _, err := blocker.Exec(ctx, "SELECT id FROM sessions WHERE id=$1 FOR UPDATE", test.actor.SessionID); err != nil {
				t.Fatal(err)
			}
			trace.queries.Store(0)
			bounded, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			if err := identity.CheckAdministratorRead(bounded, reader, test.actor, test.audience); err != nil {
				t.Fatalf("independent read waited for an actor lock: %v", err)
			}
			if count := trace.queries.Load(); count != 1 {
				t.Fatalf("independent read issued %d statements, want 1", count)
			}
			if err := blocker.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			t.Log("administrator SQL statements: transaction=3 independent=1")
		})
	}
}

func TestCheckAdministratorReadRefreshesCommittedAuthority(t *testing.T) {
	ctx, pool, store, native, _ := applicationKeyTestStore(t)
	_, emby := managedLogin(t, ctx, store, native.User, "administrator-password", "emby")
	emby.PeerIP = "192.0.2.20"
	local := emby
	local.PeerIP = "127.0.0.1"
	for _, test := range []struct {
		name      string
		actor     identity.Principal
		audience  identity.AdministratorAudience
		statement string
		id        string
		want      error
	}{
		{"native revoked", native, identity.AdministratorNative, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", native.SessionID, identity.ErrUnauthorized},
		{"emby revoked", emby, identity.AdministratorEmby, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", emby.SessionID, identity.ErrUnauthorized},
		{"native disabled", native, identity.AdministratorNative, "UPDATE users SET is_disabled=true WHERE id=$1", native.User.ID, identity.ErrUnauthorized},
		{"emby disabled", emby, identity.AdministratorEmby, "UPDATE users SET is_disabled=true WHERE id=$1", native.User.ID, identity.ErrUnauthorized},
		{"native demoted", native, identity.AdministratorNative, "UPDATE users SET is_administrator=false WHERE id=$1", native.User.ID, identity.ErrUnauthorized},
		{"emby demoted", emby, identity.AdministratorEmby, "UPDATE users SET is_administrator=false WHERE id=$1", native.User.ID, identity.ErrClientSessionForbidden},
		{"native expired", native, identity.AdministratorNative, "UPDATE sessions SET expires_at=clock_timestamp()-interval '1 minute' WHERE id=$1", native.SessionID, identity.ErrUnauthorized},
		{"emby expired", emby, identity.AdministratorEmby, "UPDATE sessions SET expires_at=clock_timestamp()-interval '1 minute' WHERE id=$1", emby.SessionID, identity.ErrUnauthorized},
		{"malformed policy", emby, identity.AdministratorEmby, `UPDATE users SET policy='{"EnableRemoteAccess":null}'::jsonb WHERE id=$1`, native.User.ID, identity.ErrUnauthorized},
		{"remote policy", emby, identity.AdministratorEmby, `UPDATE users SET policy='{"EnableRemoteAccess":false}'::jsonb WHERE id=$1`, native.User.ID, identity.ErrUnauthorized},
		{"trusted local peer", local, identity.AdministratorEmby, `UPDATE users SET policy='{"EnableRemoteAccess":false}'::jsonb WHERE id=$1`, native.User.ID, nil},
		{"device policy", emby, identity.AdministratorEmby, `UPDATE users SET policy='{"EnableAllDevices":false,"EnabledDevices":[]}'::jsonb WHERE id=$1`, native.User.ID, identity.ErrUnauthorized},
		{"lockout policy", emby, identity.AdministratorEmby, `UPDATE users SET policy='{"LockedOutDate":1}'::jsonb WHERE id=$1`, native.User.ID, identity.ErrUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator=true,is_disabled=false,policy='{}'::jsonb WHERE id=$1", native.User.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=NULL,created_at=clock_timestamp()-interval '1 hour',
				expires_at=clock_timestamp()+interval '1 day' WHERE id=ANY($1::text[])`, []string{native.SessionID, emby.SessionID}); err != nil {
				t.Fatal(err)
			}
			if err := identity.CheckAdministratorRead(ctx, pool, test.actor, test.audience); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, test.statement, test.id); err != nil {
				t.Fatal(err)
			}
			if err := identity.CheckAdministratorRead(ctx, pool, test.actor, test.audience); !errors.Is(err, test.want) {
				t.Fatalf("committed authority change returned %v, want %v", err, test.want)
			}
		})
	}
}

func TestCheckAdministratorReadKeepsApplicationClientOwnership(t *testing.T) {
	ctx, pool, store, native, _ := applicationKeyTestStore(t)
	alpha := applicationKeyPrincipal(t, ctx, store, issueApplicationKey(t, ctx, store, native, "Independent alpha"))
	beta := applicationKeyPrincipal(t, ctx, store, issueApplicationKey(t, ctx, store, native, "Independent beta"))
	for _, field := range []string{"client", "parent", "key"} {
		actor := alpha
		switch field {
		case "client":
			actor.ClientSessionID = beta.ClientSessionID
		case "parent":
			actor.SessionID = beta.SessionID
		case "key":
			actor.ApplicationKeyID = beta.ApplicationKeyID
		}
		if err := identity.CheckAdministratorRead(ctx, pool, actor, identity.AdministratorEmby); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("independent read accepted foreign %s: %v", field, err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", alpha.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := identity.CheckAdministratorRead(ctx, pool, alpha, identity.AdministratorEmby); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("independent read retained a revoked application credential: %v", err)
	}
	if err := identity.CheckAdministratorRead(ctx, pool, beta, identity.AdministratorEmby); err != nil {
		t.Fatalf("revoking one credential affected another: %v", err)
	}
}

func TestCheckAdministratorReadPreservesAdmissionCancellationAndSQLFailures(t *testing.T) {
	ctx, pool, _, actor, _ := applicationKeyTestStore(t)
	reader, trace := administratorReadPool(t, ctx, pool)
	held, err := reader.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	trace.queries.Store(0)
	waiting, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	err = identity.CheckAdministratorRead(waiting, reader, actor, identity.AdministratorNative)
	cancel()
	held.Release()
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, identity.ErrAdministratorReadUnavailable) || errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("pool wait lost cancellation or availability classification: %v", err)
	}
	if count := trace.queries.Load(); count != 0 {
		t.Fatalf("cancelled pool wait issued %d statements", count)
	}
	if err := identity.CheckAdministratorRead(ctx, reader, actor, identity.AdministratorNative); err != nil {
		t.Fatalf("cancelled pool wait retained its connection: %v", err)
	}
	completed, cancelCompleted := context.WithCancel(ctx)
	trace.after = cancelCompleted
	err = identity.CheckAdministratorRead(completed, reader, actor, identity.AdministratorNative)
	trace.after = nil
	cancelCompleted()
	if !errors.Is(err, context.Canceled) || errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("completed query ignored request cancellation: %v", err)
	}
	queryContext, cancelQuery := context.WithCancel(ctx)
	trace.before = func(context.Context, *pgx.Conn) { cancelQuery() }
	err = identity.CheckAdministratorRead(queryContext, reader, actor, identity.AdministratorNative)
	trace.before = nil
	cancelQuery()
	if !errors.Is(err, context.Canceled) || errors.Is(err, identity.ErrAdministratorReadUnavailable) || errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("query cancellation changed error classification: %v", err)
	}
	trace.before = func(work context.Context, connection *pgx.Conn) { _ = connection.Close(work) }
	err = identity.CheckAdministratorRead(ctx, reader, actor, identity.AdministratorNative)
	trace.before = nil
	if !errors.Is(err, identity.ErrAdministratorReadUnavailable) || errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("lost connection was classified as an authorization SQL error: %v", err)
	}
	if err := identity.CheckAdministratorRead(ctx, reader, actor, identity.AdministratorNative); err != nil {
		t.Fatalf("failed independent read retained its closed connection: %v", err)
	}
	brokenConfig := pool.Config()
	brokenConfig.ConnConfig.RuntimeParams["search_path"] = "pg_catalog"
	broken, err := pgxpool.NewWithConfig(ctx, brokenConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broken.Close)
	err = identity.CheckAdministratorRead(ctx, broken, actor, identity.AdministratorNative)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "42P01" || errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, identity.ErrAdministratorReadUnavailable) {
		t.Fatalf("authorization SQL failure lost its database error: %v", err)
	}
	if strings.Contains(err.Error(), actor.SessionID) {
		t.Fatal("authorization SQL error exposed the credential identifier")
	}
	reader.Close()
	if err := identity.CheckAdministratorRead(ctx, reader, actor, identity.AdministratorNative); !errors.Is(err, identity.ErrAdministratorReadUnavailable) || errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("closed pool was classified as invalid credentials: %v", err)
	}
}

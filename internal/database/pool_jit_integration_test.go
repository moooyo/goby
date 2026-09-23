package database_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestOpenDisablesJITOnPhysicalConnections(t *testing.T) {
	for _, incoming := range []string{"", "on"} {
		name := "server-default"
		if incoming != "" {
			name = "url-jit-on"
		}
		t.Run(name, func(t *testing.T) {
			ctx, fixture := migrationTestPool(t)
			schema := openJITSchema(t, ctx, fixture)
			independent := openJITIndependent(t, ctx, openJITDSN(t, schema, ""))
			independentPID, defaultJIT := openJITState(t, ctx, independent, schema)
			explicitOn := openJITIndependent(t, ctx, openJITDSN(t, schema, "on"))
			explicitPID, setting := openJITState(t, ctx, explicitOn, schema)
			if setting != "on" {
				t.Fatalf("independent startup JIT override = %q, want on", setting)
			}
			pool := openJITPool(t, ctx, openJITDSN(t, schema, incoming))
			first := acquireOpenJIT(t, ctx, pool)
			firstPID := requireOpenJITOff(t, ctx, first, schema)
			second := acquireOpenJIT(t, ctx, pool)
			secondPID := requireOpenJITOff(t, ctx, second, schema)
			if firstPID == secondPID {
				t.Fatal("simultaneous checkouts did not use distinct physical connections")
			}
			// Occupy the remaining slots so closing one session requires a real
			// replacement, including when pool startup is still creating connections.
			holdOpenJITRemainder(t, ctx, pool, schema, 2)
			createdBefore := pool.Stat().NewConnsCount()
			detached := first.Hijack()
			cleanupOpenJITConnection(t, detached)
			if pid := requireOpenJITOff(t, ctx, detached, schema); pid != firstPID {
				t.Fatal("Hijack replaced the observed physical session")
			}
			if err := detached.Close(ctx); err != nil {
				t.Fatalf("close the owned detached session: %v", err)
			}
			replacement := acquireOpenJIT(t, ctx, pool)
			replacementPID := requireOpenJITOff(t, ctx, replacement, schema)
			if replacementPID == firstPID || replacementPID == secondPID || pool.Stat().NewConnsCount() <= createdBefore {
				t.Fatal("closed physical connection was not replaced by a new JIT-disabled session")
			}
			if pid, value := openJITState(t, ctx, independent, schema); pid != independentPID || value != defaultJIT {
				t.Fatal("Goby changed the existing independent session's JIT setting")
			}
			freshIndependent := openJITIndependent(t, ctx, openJITDSN(t, schema, ""))
			if _, value := openJITState(t, ctx, freshIndependent, schema); value != defaultJIT {
				t.Fatal("Goby changed the default for new independent PostgreSQL sessions")
			}
			if pid, value := openJITState(t, ctx, explicitOn, schema); pid != explicitPID || value != "on" {
				t.Fatal("Goby changed the independent session's explicit JIT setting")
			}
		})
	}
}

func TestOpenDisablesJITForCatalogOwnerAndDeploymentLease(t *testing.T) {
	// The deployment lease spans the database, so this test remains serial like
	// the existing lease integration tests even though its schema is isolated.
	ctx, fixture := migrationTestPool(t)
	schema := openJITSchema(t, ctx, fixture)
	pool := openJITPool(t, ctx, openJITDSN(t, schema, "on"))
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate the owned JIT test schema: %v", err)
	}
	store, err := library.New(pool, openJITUnusedProber{}, nil)
	if err != nil {
		t.Fatalf("acquire the real catalog owner: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := store.Close(cleanup); err != nil {
			t.Errorf("close the JIT test catalog owner: %v", err)
		}
	})
	var ownerPID int32
	for call := 0; call < 2; call++ {
		var pid int32
		var setting, actualSchema string
		if err := store.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
			return tx.QueryRow(`SELECT pg_backend_pid(), current_setting('jit'), current_schema()`).
				Scan(&pid, &setting, &actualSchema)
		}); err != nil {
			t.Fatalf("observe the catalog owner's actual session: %v", err)
		}
		if setting != "off" || actualSchema != schema || ownerPID != 0 && pid != ownerPID {
			t.Fatalf("catalog owner lost its stable JIT-disabled session: pid=%d jit=%q schema=%q", pid, setting, actualSchema)
		}
		ownerPID = pid
	}
	candidate := acquireOpenJIT(t, ctx, pool)
	leasePID := requireOpenJITOff(t, ctx, candidate, schema)
	// The catalog owner and candidate occupy two slots. Holding the rest leaves
	// this exact candidate as the lease's only possible idle connection.
	holdOpenJITRemainder(t, ctx, pool, schema, 2)
	candidate.Release()
	lease, err := database.AcquireLease(ctx, pool)
	if err != nil {
		t.Fatalf("acquire the real deployment lease: %v", err)
	}
	cleanupDeploymentLease(t, lease)
	// The monitor owns the leased connection. Observe its advisory-lock PID
	// instead of concurrently issuing SHOW on that now-Hijacked connection.
	var holdsLease bool
	if err := fixture.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_catalog.pg_locks l JOIN pg_catalog.pg_stat_activity a ON a.pid=l.pid
		WHERE l.locktype='advisory' AND l.mode='ExclusiveLock' AND l.granted AND l.objsubid=1
		AND l.database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database())
		AND l.classid=$1::oid AND l.objid=$2::oid AND l.pid=$3
		AND a.usename=current_user AND a.application_name='goby')`,
		uint32(uint64(deploymentLeaseTestID)>>32), uint32(uint64(deploymentLeaseTestID)&0xffffffff), leasePID).
		Scan(&holdsLease); err != nil {
		t.Fatalf("observe the deployment lease's physical session: %v", err)
	}
	if !holdsLease || leasePID == ownerPID || !lease.Protects(pool) {
		t.Fatal("deployment lease did not Hijack the independently observed JIT-disabled session")
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("close the JIT test deployment lease: %v", err)
	}
}

type openJITQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func openJITState(t *testing.T, ctx context.Context, query openJITQuery, schema string) (int32, string) {
	t.Helper()
	var pid int32
	var setting, actualSchema string
	if err := query.QueryRow(ctx, `SELECT pg_backend_pid(), current_setting('jit'), current_schema()`).
		Scan(&pid, &setting, &actualSchema); err != nil {
		t.Fatalf("read actual PostgreSQL session policy: %v", err)
	}
	if pid <= 0 || actualSchema != schema {
		t.Fatalf("session escaped the owned schema: pid=%d schema=%q", pid, actualSchema)
	}
	return pid, setting
}

func requireOpenJITOff(t *testing.T, ctx context.Context, query openJITQuery, schema string) int32 {
	t.Helper()
	pid, setting := openJITState(t, ctx, query, schema)
	if setting != "off" {
		t.Fatalf("Goby physical session %d has jit=%q, want off", pid, setting)
	}
	return pid
}

func openJITSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var schema string
	if err := pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatalf("read the isolated schema: %v", err)
	}
	return schema
}

func openJITDSN(t *testing.T, schema, jit string) string {
	t.Helper()
	raw := os.Getenv("GOBY_TEST_DATABASE_URL")
	// Config.ConnString retains its original text, not fixture RuntimeParams.
	// Bind the actual schema explicitly in both pgx-supported input formats.
	if strings.HasPrefix(raw, "postgres://") || strings.HasPrefix(raw, "postgresql://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal("parse the integration database URL")
		}
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil {
			t.Fatal("parse the integration database URL parameters")
		}
		query.Set("search_path", schema)
		if jit != "" {
			query.Set("jit", jit)
		}
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	quote := func(value string) string {
		return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value) + "'"
	}
	raw += " search_path=" + quote(schema)
	if jit != "" {
		raw += " jit=" + quote(jit)
	}
	return raw
}

func openJITPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatal("open the real Goby integration pool")
	}
	t.Cleanup(pool.Close)
	return pool
}

func acquireOpenJIT(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *pgxpool.Conn {
	t.Helper()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire a Goby physical connection: %v", err)
	}
	t.Cleanup(connection.Release)
	return connection
}

func holdOpenJITRemainder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema string, alreadyHeld int) {
	t.Helper()
	for held := alreadyHeld; held < int(pool.Config().MaxConns); held++ {
		connection := acquireOpenJIT(t, ctx, pool)
		requireOpenJITOff(t, ctx, connection, schema)
	}
}

func openJITIndependent(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("open an independent integration session")
	}
	cleanupOpenJITConnection(t, connection)
	return connection
}

func cleanupOpenJITConnection(t *testing.T, connection *pgx.Conn) {
	t.Helper()
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := connection.Close(cleanup); err != nil {
			t.Errorf("close an owned JIT test connection: %v", err)
		}
	})
}

type openJITUnusedProber struct{}

func (openJITUnusedProber) ProbeFile(context.Context, *os.File) (media.Info, error) {
	return media.Info{}, errors.New("the JIT session test must not probe media")
}

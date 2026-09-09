package database_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// This advisory key is the database migration serialization protocol. Holding
// the same key in a separate session exercises actual concurrent startup waits.
const migrationTimeoutLockID int64 = 4919415424202458190

type migrationTimeoutConnectionState struct {
	pid     int32
	timeout string
}

func migrationTimeoutPool(t *testing.T, ctx context.Context, source *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	config := source.Config().Copy()
	config.MaxConns = 1
	config.MinConns = 0
	config.ConnConfig.RuntimeParams["statement_timeout"] = "200ms"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("create short-timeout migration pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func readMigrationTimeoutState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) migrationTimeoutConnectionState {
	t.Helper()
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	connection, err := pool.Acquire(queryCtx)
	if err != nil {
		t.Fatalf("acquire timeout observation connection: %v", err)
	}
	defer connection.Release()
	var state migrationTimeoutConnectionState
	if err := connection.QueryRow(queryCtx, "SELECT pg_backend_pid()").Scan(&state.pid); err != nil {
		t.Fatalf("read migration backend PID: %v", err)
	}
	if err := connection.QueryRow(queryCtx, "SHOW statement_timeout").Scan(&state.timeout); err != nil {
		t.Fatalf("read connection statement timeout: %v", err)
	}
	return state
}

type migrationTimeoutBlocker struct {
	connection *pgx.Conn
	held       bool
}

func holdMigrationTimeoutLock(t *testing.T, ctx context.Context, source *pgxpool.Pool) *migrationTimeoutBlocker {
	t.Helper()
	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	connection, err := pgx.ConnectConfig(connectCtx, source.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatalf("open independent migration lock observer: %v", err)
	}
	blocker := &migrationTimeoutBlocker{connection: connection}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if err := connection.Close(closeCtx); err != nil {
			t.Errorf("close migration lock observer: %v", err)
		}
	})
	if _, err := connection.Exec(connectCtx, "SELECT pg_advisory_lock($1)", migrationTimeoutLockID); err != nil {
		t.Fatalf("hold migration serialization lock: %v", err)
	}
	blocker.held = true
	return blocker
}

func (blocker *migrationTimeoutBlocker) release(t *testing.T) {
	t.Helper()
	if !blocker.held {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var released bool
	err := blocker.connection.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", migrationTimeoutLockID).Scan(&released)
	if err != nil || !released {
		t.Errorf("release migration serialization lock: released = %v, error = %v", released, err)
		// Closing the owning session also releases its lock if an explicit
		// unlock failed, allowing the waiting test goroutine to finish.
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = blocker.connection.Close(closeCtx)
	}
	blocker.held = false
}

type migrationTimeoutAttempt struct {
	result   chan error
	cancel   context.CancelFunc
	finished bool
	err      error
}

func startMigrationTimeoutAttempt(t *testing.T, caller context.Context, pool *pgxpool.Pool, blocker *migrationTimeoutBlocker) *migrationTimeoutAttempt {
	t.Helper()
	ctx, cancel := context.WithCancel(caller)
	attempt := &migrationTimeoutAttempt{result: make(chan error, 1), cancel: cancel}
	t.Cleanup(func() {
		cancel()
		blocker.release(t)
		if attempt.finished {
			return
		}
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case attempt.err = <-attempt.result:
			attempt.finished = true
		case <-timer.C:
			t.Error("migration goroutine did not finish after cancellation and lock release")
		}
	})
	go func() {
		attempt.result <- database.Migrate(ctx, pool)
	}()
	return attempt
}

func (attempt *migrationTimeoutAttempt) wait(t *testing.T, timeout time.Duration) error {
	t.Helper()
	if attempt.finished {
		return attempt.err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case attempt.err = <-attempt.result:
		attempt.finished = true
		return attempt.err
	case <-timer.C:
		t.Fatal("timed out waiting for the migration result")
		return nil
	}
}

func waitForMigrationTimeoutLock(t *testing.T, ctx context.Context, blocker *migrationTimeoutBlocker, attempt *migrationTimeoutAttempt, pid int32) {
	t.Helper()
	pollCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case attempt.err = <-attempt.result:
			attempt.finished = true
			t.Fatalf("migration finished before its advisory lock wait was observed: %v", attempt.err)
		default:
		}
		var waiting bool
		err := blocker.connection.QueryRow(pollCtx, `SELECT EXISTS (
			SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND pid = $1
			AND classid::bigint = $2 AND objid::bigint = $3 AND objsubid = 1 AND NOT granted
		)`, pid, migrationTimeoutLockID>>32, migrationTimeoutLockID&0xffffffff).Scan(&waiting)
		if err != nil {
			t.Fatalf("observe migration advisory lock wait: %v", err)
		}
		if waiting {
			return
		}
		select {
		case <-pollCtx.Done():
			t.Fatalf("migration did not enter its advisory lock wait: %v", pollCtx.Err())
		case <-ticker.C:
		}
	}
}

func assertMigrationTimeoutTablesAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, name := range []string{"schema_migrations", "server_settings"} {
		var absent bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NULL", name).Scan(&absent); err != nil {
			t.Fatalf("inspect rollback of %s: %v", name, err)
		}
		if !absent {
			t.Errorf("unsuccessful migration left table %s in its owned schema", name)
		}
	}
}

func TestMigrateWaitsPastPoolStatementTimeoutAndRestoresSameConnection(t *testing.T) {
	ctx, source := migrationTestPool(t)
	original := readMigrationTimeoutState(t, ctx, source)
	pool := migrationTimeoutPool(t, ctx, source)
	before := readMigrationTimeoutState(t, ctx, pool)
	if before.timeout != "200ms" {
		t.Fatalf("short-timeout fixture has statement_timeout %q, want 200ms", before.timeout)
	}
	blocker := holdMigrationTimeoutLock(t, ctx, source)
	attempt := startMigrationTimeoutAttempt(t, ctx, pool, blocker)
	waitForMigrationTimeoutLock(t, ctx, blocker, attempt, before.pid)
	// Start this interval only after PostgreSQL confirms the blocked statement.
	// A transaction that inherited the ordinary 200ms timeout cannot survive it.
	timer := time.NewTimer(450 * time.Millisecond)
	defer timer.Stop()
	select {
	case attempt.err = <-attempt.result:
		attempt.finished = true
		t.Fatalf("migration stopped during a legitimate lock wait: %v", attempt.err)
	case <-timer.C:
	}
	blocker.release(t)
	if err := attempt.wait(t, 5*time.Second); err != nil {
		t.Fatalf("migrate after releasing the serialization lock: %v", err)
	}
	after := readMigrationTimeoutState(t, ctx, pool)
	if after.pid != before.pid || after.timeout != before.timeout {
		t.Errorf("successful migration changed pooled connection state: before = %+v, after = %+v", before, after)
	}
	if unaffected := readMigrationTimeoutState(t, ctx, source); unaffected.timeout != original.timeout {
		t.Errorf("migration changed another pool's timeout: before = %s, after = %s", original.timeout, unaffected.timeout)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version <= 0 {
		t.Errorf("successful migration did not create an applied schema: version = %d, error = %v", version, err)
	}
}

func TestMigrateRespectsCallerCancellationAndRestoresPoolTimeout(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			ctx, source := migrationTestPool(t)
			pool := migrationTimeoutPool(t, ctx, source)
			before := readMigrationTimeoutState(t, ctx, pool)
			blocker := holdMigrationTimeoutLock(t, ctx, source)
			var caller context.Context
			var cancel context.CancelFunc
			wanted := context.Canceled
			if mode == "deadline" {
				caller, cancel = context.WithTimeout(ctx, 750*time.Millisecond)
				wanted = context.DeadlineExceeded
			} else {
				caller, cancel = context.WithCancel(ctx)
			}
			defer cancel()
			attempt := startMigrationTimeoutAttempt(t, caller, pool, blocker)
			waitForMigrationTimeoutLock(t, ctx, blocker, attempt, before.pid)
			if mode == "cancel" {
				cancel()
			}
			if err := attempt.wait(t, 3*time.Second); err == nil {
				t.Fatal("cancelled caller unexpectedly completed a blocked migration")
			}
			if !errors.Is(caller.Err(), wanted) {
				t.Errorf("caller context ended with %v, want %v", caller.Err(), wanted)
			}
			// pgx may discard a connection when cancelling socket I/O. The next
			// connection must still inherit the original pool setting.
			after := readMigrationTimeoutState(t, ctx, pool)
			if after.timeout != before.timeout {
				t.Errorf("cancelled migration leaked its local timeout: before = %s, after = %s", before.timeout, after.timeout)
			}
			assertMigrationTimeoutTablesAbsent(t, ctx, source)
			blocker.release(t)
			retryCtx, retryCancel := context.WithTimeout(ctx, 5*time.Second)
			defer retryCancel()
			if err := database.Migrate(retryCtx, pool); err != nil {
				t.Fatalf("migrate after caller cancellation: %v", err)
			}
			if final := readMigrationTimeoutState(t, ctx, pool); final.timeout != before.timeout {
				t.Errorf("retry leaked the migration timeout: got %s, want %s", final.timeout, before.timeout)
			}
		})
	}
}

func TestMigrateFailureRollsBackDDLAndRestoresStatementTimeout(t *testing.T) {
	ctx, source := migrationTestPool(t)
	// This conflicting table belongs solely to migrationTestPool's new schema.
	if _, err := source.Exec(ctx, "CREATE TABLE users (sentinel text NOT NULL)"); err != nil {
		t.Fatalf("create owned migration conflict fixture: %v", err)
	}
	if _, err := source.Exec(ctx, "INSERT INTO users (sentinel) VALUES ('preserve this row')"); err != nil {
		t.Fatalf("insert migration conflict sentinel: %v", err)
	}
	pool := migrationTimeoutPool(t, ctx, source)
	before := readMigrationTimeoutState(t, ctx, pool)
	migrateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := database.Migrate(migrateCtx, pool)
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != "42P07" {
		t.Fatalf("migration must fail on the existing users table: %v", err)
	}
	after := readMigrationTimeoutState(t, ctx, pool)
	if after.pid != before.pid || after.timeout != before.timeout {
		t.Errorf("failed migration changed pooled connection state: before = %+v, after = %+v", before, after)
	}
	assertMigrationTimeoutTablesAbsent(t, ctx, source)
	var rows int
	var sentinel string
	if err := source.QueryRow(ctx, "SELECT count(*), min(sentinel) FROM users").Scan(&rows, &sentinel); err != nil {
		t.Fatalf("read preserved migration conflict fixture: %v", err)
	}
	if rows != 1 || sentinel != "preserve this row" {
		t.Errorf("failed migration modified pre-existing owned data: rows = %d, sentinel = %q", rows, sentinel)
	}
}

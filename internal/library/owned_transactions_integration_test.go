package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWithOwnedTxCallerCancellationCommitsEveryQueryMethod(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
	callerCtx, cancelCaller := context.WithCancel(ctx)
	defer cancelCaller()
	err := ownedTransactionsRun(t, callerCtx, store, func(tx OwnedTx) error {
		cancelCaller()
		if !errors.Is(callerCtx.Err(), context.Canceled) {
			return errors.New("caller was not cancelled inside the callback")
		}
		tag, err := tx.Exec(`INSERT INTO server_settings (key, value)
			VALUES ('owned-transactions-cancellation', 'inserted')`)
		if err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("insert after caller cancellation: affected = %d, error = %v", tag.RowsAffected(), err)
		}
		var value string
		var actualPID int32
		if err := tx.QueryRow(`UPDATE server_settings SET value = 'committed'
			WHERE key = 'owned-transactions-cancellation' RETURNING value, pg_backend_pid()`).Scan(&value, &actualPID); err != nil {
			return fmt.Errorf("scan a returning row after caller cancellation: %w", err)
		}
		if value != "committed" || actualPID != ownerPID {
			return fmt.Errorf("returning row did not use the owner: value = %q, pid = %d", value, actualPID)
		}
		rows, err := tx.Query("SELECT number FROM generate_series(1, 4) AS numbers(number)")
		if err != nil {
			return fmt.Errorf("query rows after caller cancellation: %w", err)
		}
		defer rows.Close()
		count, sum := 0, 0
		for rows.Next() {
			var number int
			if err := rows.Scan(&number); err != nil {
				return fmt.Errorf("scan rows after caller cancellation: %w", err)
			}
			count++
			sum += number
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("finish rows after caller cancellation: %w", err)
		}
		if count != 4 || sum != 10 {
			return fmt.Errorf("cancelled caller read an incomplete result: count = %d, sum = %d", count, sum)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("owned callback did not commit after caller cancellation: %v", err)
	}
	ownedTransactionsExpectSetting(t, ctx, pool, "owned-transactions-cancellation", "committed")
	ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
}

func TestWithOwnedTxCallbackFailureRollsBackAndReleasesOwnership(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
	callbackFailure := errors.New("owned callback rejected its write")
	err := ownedTransactionsRun(t, ctx, store, func(tx OwnedTx) error {
		if _, err := tx.Exec(`INSERT INTO server_settings (key, value)
			VALUES ('owned-transactions-callback-failure', 'must roll back')`); err != nil {
			return fmt.Errorf("write before callback rejection: %w", err)
		}
		return callbackFailure
	})
	if !errors.Is(err, callbackFailure) {
		t.Fatalf("callback error was not preserved: %v", err)
	}
	ownedTransactionsExpectAbsent(t, ctx, pool, "owned-transactions-callback-failure")
	ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
}

func TestWithOwnedTxPanicClosesOutstandingRowsAndRollsBack(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
	for _, queryKind := range []string{"query", "query row"} {
		marker := errors.New("owned callback panic marker")
		key := "owned-transactions-panic-" + queryKind
		var recovered any
		var returnedErr error
		var escapedRow OwnedRow
		var escapedRows OwnedRows
		finished := make(chan error, 1)
		go func() {
			defer func() {
				recovered = recover()
				finished <- returnedErr
			}()
			returnedErr = store.WithOwnedTx(ctx, func(tx OwnedTx) error {
				if _, err := tx.Exec("INSERT INTO server_settings (key, value) VALUES ($1, 'must roll back')", key); err != nil {
					return fmt.Errorf("write before callback panic: %w", err)
				}
				if queryKind == "query row" {
					escapedRow = tx.QueryRow("SELECT repeat('p', 16384) FROM generate_series(1, 8)")
				} else {
					var err error
					escapedRows, err = tx.Query("SELECT repeat('p', 16384) FROM generate_series(1, 8)")
					if err != nil {
						return fmt.Errorf("open the result before callback panic: %w", err)
					}
				}
				panic(marker)
			})
		}()
		if err := ownedTransactionsAwait(t, ctx, finished); err != nil {
			t.Fatalf("%s callback returned before the intended panic: %v", queryKind, err)
		}
		if recovered != marker {
			t.Fatalf("%s callback did not preserve the exact panic marker: recovered = %v", queryKind, recovered)
		}
		var value string
		if queryKind == "query row" {
			if escapedRow == nil {
				t.Fatal("panic callback did not create its intentionally unscanned row")
			}
			if err := escapedRow.Scan(&value); !errors.Is(err, pgx.ErrTxClosed) {
				t.Errorf("unscanned row remained usable after panic: %v", err)
			}
		} else {
			if escapedRows == nil {
				t.Fatal("panic callback did not create its intentionally unclosed rows")
			}
			if escapedRows.Next() {
				t.Error("unclosed rows remained readable after panic")
			}
			if err := escapedRows.Err(); !errors.Is(err, pgx.ErrTxClosed) {
				t.Errorf("unclosed rows retained a live callback after panic: %v", err)
			}
			escapedRows.Close()
		}
		ownedTransactionsExpectAbsent(t, ctx, pool, key)
		ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
	}
}

func TestWithOwnedTxClosesOutstandingRowsOnCallbackExit(t *testing.T) {
	for _, queryKind := range []string{"query", "query row"} {
		for _, reject := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reject=%t", queryKind, reject), func(t *testing.T) {
				ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
				ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
				callbackFailure := errors.New("reject callback with an open result")
				var escapedTx OwnedTx
				var escapedRow OwnedRow
				var escapedRows OwnedRows
				err := ownedTransactionsRun(t, ctx, store, func(tx OwnedTx) error {
					escapedTx = tx
					if _, err := tx.Exec(`INSERT INTO server_settings (key, value)
						VALUES ('owned-transactions-open-result', 'committed')`); err != nil {
						return fmt.Errorf("write before leaving a result open: %w", err)
					}
					if queryKind == "query row" {
						escapedRow = tx.QueryRow("SELECT repeat('r', 16384) FROM generate_series(1, 8)")
					} else {
						var err error
						escapedRows, err = tx.Query("SELECT repeat('r', 16384) FROM generate_series(1, 8)")
						if err != nil {
							return fmt.Errorf("create the intentionally unconsumed rows: %w", err)
						}
					}
					if reject {
						return callbackFailure
					}
					return nil
				})
				if reject {
					if !errors.Is(err, callbackFailure) {
						t.Fatalf("failure with an open result = %v; want callback failure", err)
					}
					ownedTransactionsExpectAbsent(t, ctx, pool, "owned-transactions-open-result")
				} else {
					if err != nil {
						t.Fatalf("commit with an open result: %v", err)
					}
					ownedTransactionsExpectSetting(t, ctx, pool, "owned-transactions-open-result", "committed")
				}
				if escapedTx == nil {
					t.Fatal("the callback did not receive a transaction")
				}
				if _, err := escapedTx.Exec(`INSERT INTO server_settings (key, value)
					VALUES ('owned-transactions-escaped-write', 'must not persist')`); !errors.Is(err, pgx.ErrTxClosed) {
					t.Errorf("escaped Exec error = %v; want a closed transaction", err)
				}
				var value string
				if err := escapedTx.QueryRow("SELECT 'escaped'").Scan(&value); !errors.Is(err, pgx.ErrTxClosed) {
					t.Errorf("escaped QueryRow error = %v; want a closed transaction", err)
				}
				if rows, err := escapedTx.Query("SELECT 'escaped'"); !errors.Is(err, pgx.ErrTxClosed) {
					if rows != nil {
						rows.Close()
					}
					t.Errorf("escaped Query error = %v; want a closed transaction", err)
				}
				if escapedRow != nil {
					if err := escapedRow.Scan(&value); !errors.Is(err, pgx.ErrTxClosed) {
						t.Errorf("unscanned row escaped its callback: %v", err)
					}
				}
				if escapedRows != nil {
					if escapedRows.Next() {
						t.Error("unconsumed rows remained readable after the callback")
					}
					if err := escapedRows.Scan(&value); !errors.Is(err, pgx.ErrTxClosed) {
						t.Errorf("escaped row Scan error = %v; want a closed transaction", err)
					}
					if err := escapedRows.Err(); !errors.Is(err, pgx.ErrTxClosed) {
						t.Errorf("escaped rows Err = %v; want a closed transaction", err)
					}
					escapedRows.Close()
				}
				ownedTransactionsExpectAbsent(t, ctx, pool, "owned-transactions-escaped-write")
				ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
			})
		}
	}
}

func TestWithOwnedTxHandledNoRowsDoesNotFenceOwnership(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
	err := ownedTransactionsRun(t, ctx, store, func(tx OwnedTx) error {
		var value string
		if err := tx.QueryRow("SELECT value FROM server_settings WHERE key = 'owned-transactions-missing'").Scan(&value); !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("missing row error = %v; want pgx.ErrNoRows", err)
		}
		_, err := tx.Exec(`INSERT INTO server_settings (key, value)
			VALUES ('owned-transactions-after-no-rows', 'committed')`)
		return err
	})
	if err != nil {
		t.Fatalf("handled missing row prevented commit: %v", err)
	}
	ownedTransactionsExpectSetting(t, ctx, pool, "owned-transactions-after-no-rows", "committed")
	ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
}

func TestWithOwnedTxRejectsUnavailableOrCancelledAdmission(t *testing.T) {
	for _, state := range []string{"cancelled", "closed", "lost", "unobserved loss"} {
		t.Run(state, func(t *testing.T) {
			ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
			ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
			callerCtx, cancelCaller := context.WithCancel(ctx)
			defer cancelCaller()
			wantErr := ErrUnavailable
			switch state {
			case "cancelled":
				cancelCaller()
				wantErr = context.Canceled
			case "closed":
				closeCtx, cancelClose := context.WithTimeout(ctx, 5*time.Second)
				err := store.Close(closeCtx)
				cancelClose()
				if err != nil {
					t.Fatalf("close the owner before callback admission: %v", err)
				}
			case "lost":
				ownedTransactionsTerminateBackend(t, ctx, store.pool, ownerPID)
				if err := ownedTransactionsCheckOwnership(t, ctx, store); !errors.Is(err, ErrUnavailable) {
					t.Fatalf("detect the terminated owner before admission: %v", err)
				}
			case "unobserved loss":
				ownedTransactionsTerminateBackend(t, ctx, store.pool, ownerPID)
			}
			called := false
			err := ownedTransactionsRun(t, callerCtx, store, func(OwnedTx) error {
				called = true
				return nil
			})
			if !errors.Is(err, wantErr) || called {
				t.Fatalf("admit callback against %s owner: called = %t, error = %v; want %v", state, called, err, wantErr)
			}
			if state == "cancelled" {
				ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
			} else if store.Available() {
				t.Error("unavailable store reported healthy after rejecting admission")
			}
		})
	}
}

func TestWithOwnedTxQueryConnectionLossFencesOldOwner(t *testing.T) {
	ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
	ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
	gatePID := ownedTransactionsLockGate(t, ctx, pool)
	var queryErr error
	var fencedInCallback bool
	finished := make(chan error, 1)
	go func() {
		finished <- store.WithOwnedTx(ctx, func(tx OwnedTx) error {
			if _, err := tx.Exec(`INSERT INTO server_settings (key, value)
				VALUES ('owned-transactions-lost-write', 'must roll back')`); err != nil {
				return err
			}
			rows, err := tx.Query("SELECT value FROM server_settings WHERE key = 'owned-transactions-gate' FOR UPDATE")
			if err != nil {
				queryErr = err
				fencedInCallback = store.ownership.lost.Load()
				return nil
			}
			// Some protocol paths expose RowDescription before the first result.
			// Reading to exhaustion keeps the failure at query admission or its
			// first result, without letting commit be its first observer.
			for rows.Next() {
				var ignored string
				if err := rows.Scan(&ignored); err != nil {
					queryErr = err
					break
				}
			}
			if queryErr == nil {
				queryErr = rows.Err()
			}
			fencedInCallback = store.ownership.lost.Load()
			return nil
		})
	}()
	ownedTransactionsWaitForBlock(t, ctx, pool, ownerPID, gatePID, finished)
	ownedTransactionsTerminateBackend(t, ctx, pool, ownerPID)
	err := ownedTransactionsAwait(t, ctx, finished)
	if !errors.Is(err, ErrUnavailable) || !errors.Is(queryErr, ErrUnavailable) || !fencedInCallback {
		t.Fatalf("ignored query connection loss: callback error = %v, query error = %v, fenced before exit = %t", err, queryErr, fencedInCallback)
	}
	ownedTransactionsAssertTakeover(t, ctx, pool, store, allowedRoot)
}

func TestWithOwnedTxStreamConnectionLossCannotBeIgnored(t *testing.T) {
	for _, terminal := range []string{"next", "close", "callback cleanup", "row scan"} {
		t.Run(terminal, func(t *testing.T) {
			ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
			ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
			gatePID := ownedTransactionsLockGate(t, ctx, pool)
			ready := make(chan struct{})
			resume := make(chan struct{}, 1)
			defer close(resume)
			finished := make(chan error, 1)
			var fencedInCallback bool
			var scanErr error
			go func() {
				finished <- store.WithOwnedTx(ctx, func(tx OwnedTx) error {
					if _, err := tx.Exec(`INSERT INTO server_settings (key, value)
						VALUES ('owned-transactions-lost-write', 'must roll back')`); err != nil {
						return err
					}
					// Two large rows flush a complete first row before the third
					// needs the separately locked, schema-local gate row.
					const statement = `SELECT number, repeat('x', 16384),
						CASE WHEN number = 3 THEN
							(SELECT value FROM server_settings WHERE key = 'owned-transactions-gate' FOR UPDATE)
						ELSE '' END
						FROM generate_series(1, 5) AS numbers(number)`
					var row OwnedRow
					var rows OwnedRows
					if terminal == "row scan" {
						row = tx.QueryRow(statement)
					} else {
						var err error
						rows, err = tx.Query(statement)
						if err != nil {
							return fmt.Errorf("open stream before termination: %w", err)
						}
						if !rows.Next() {
							return fmt.Errorf("stream did not expose its first row: %v", rows.Err())
						}
						var number int
						var payload, gate string
						if err := rows.Scan(&number, &payload, &gate); err != nil {
							return fmt.Errorf("read the first streamed row: %w", err)
						}
						if number != 1 || payload != strings.Repeat("x", 16384) || gate != "" {
							return errors.New("the first streamed row was incomplete")
						}
					}
					close(ready)
					select {
					case <-resume:
					case <-ctx.Done():
						return ctx.Err()
					}
					switch terminal {
					case "next":
						for rows.Next() {
						}
					case "close":
						rows.Close()
					case "row scan":
						var number int
						var payload, gate string
						scanErr = row.Scan(&number, &payload, &gate)
					}
					// Deliberately omit rows.Err and return nil. The wrapper must
					// remember terminal failures independently of callback care.
					fencedInCallback = store.ownership.lost.Load()
					return nil
				})
			}()
			readyCtx, cancelReady := context.WithTimeout(ctx, 5*time.Second)
			select {
			case <-ready:
			case err := <-finished:
				cancelReady()
				t.Fatalf("stream ended before the termination gate: %v", err)
			case <-readyCtx.Done():
				cancelReady()
				t.Fatal("stream did not expose a result before the database gate")
			}
			cancelReady()
			ownedTransactionsWaitForBlock(t, ctx, pool, ownerPID, gatePID, finished)
			ownedTransactionsTerminateBackend(t, ctx, pool, ownerPID)
			resume <- struct{}{}
			err := ownedTransactionsAwait(t, ctx, finished)
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("ignored %s stream error allowed callback success: %v", terminal, err)
			}
			if terminal != "callback cleanup" && !fencedInCallback {
				t.Fatalf("%s did not fence the owner before callback exit", terminal)
			}
			if terminal == "row scan" && !errors.Is(scanErr, ErrUnavailable) {
				t.Fatalf("row Scan lost its cleanup error: %v", scanErr)
			}
			ownedTransactionsAssertTakeover(t, ctx, pool, store, allowedRoot)
		})
	}
}

func ownedTransactionsRun(t *testing.T, ctx context.Context, store *Store, callback func(OwnedTx) error) error {
	t.Helper()
	finished := make(chan error, 1)
	go func() { finished <- store.WithOwnedTx(ctx, callback) }()
	return ownedTransactionsAwait(t, context.WithoutCancel(ctx), finished)
}

func ownedTransactionsCheckOwnership(t *testing.T, ctx context.Context, store *Store) error {
	t.Helper()
	finished := make(chan error, 1)
	go func() { finished <- store.CheckOwnership(ctx) }()
	return ownedTransactionsAwait(t, ctx, finished)
}

func ownedTransactionsAwait(t *testing.T, ctx context.Context, finished <-chan error) error {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	select {
	case err := <-finished:
		return err
	case <-waitCtx.Done():
		t.Fatalf("owned operation did not finish within its transaction and cleanup bounds: %v", waitCtx.Err())
		return waitCtx.Err()
	}
}

func ownedTransactionsExpectSetting(t *testing.T, ctx context.Context, pool *pgxpool.Pool, key, want string) {
	t.Helper()
	var value string
	if err := pool.QueryRow(ctx, "SELECT value FROM server_settings WHERE key = $1", key).Scan(&value); err != nil || value != want {
		t.Fatalf("committed setting %q: value = %q, error = %v; want %q", key, value, err, want)
	}
}

func ownedTransactionsExpectAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, key string) {
	t.Helper()
	var value string
	if err := pool.QueryRow(ctx, "SELECT value FROM server_settings WHERE key = $1", key).Scan(&value); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("rolled-back setting %q remained visible: value = %q, error = %v", key, value, err)
	}
}

func ownedTransactionsAssertReusable(t *testing.T, ctx context.Context, store *Store, ownerPID int32) {
	t.Helper()
	if !store.Available() {
		t.Fatal("ordinary callback completion fenced the catalog owner")
	}
	if err := ownedTransactionsCheckOwnership(t, ctx, store); err != nil {
		t.Fatalf("check the reusable ownership connection: %v", err)
	}
	if err := ownedTransactionsRun(t, ctx, store, func(tx OwnedTx) error {
		var actualPID int32
		if err := tx.QueryRow("SELECT pg_backend_pid()").Scan(&actualPID); err != nil {
			return err
		}
		if actualPID != ownerPID {
			return fmt.Errorf("callback changed the ownership session: got %d, want %d", actualPID, ownerPID)
		}
		_, err := tx.Exec(`INSERT INTO server_settings (key, value)
			VALUES ('owned-transactions-reuse', 'healthy')
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`)
		return err
	}); err != nil {
		t.Fatalf("reuse the owner after callback completion: %v", err)
	}
	ownedTransactionsExpectSetting(t, ctx, store.pool, "owned-transactions-reuse", "healthy")
}

func ownedTransactionsLockGate(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int32 {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO server_settings (key, value)
		VALUES ('owned-transactions-gate', 'held')`); err != nil {
		t.Fatalf("create the schema-local query gate: %v", err)
	}
	gate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin the query gate transaction: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := gate.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("release the schema-local query gate: %v", err)
		}
	})
	var gatePID int32
	if err := gate.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&gatePID); err != nil {
		t.Fatalf("identify the query gate backend: %v", err)
	}
	if _, err := gate.Exec(ctx, `UPDATE server_settings SET value = value
		WHERE key = 'owned-transactions-gate'`); err != nil {
		t.Fatalf("hold the schema-local query gate: %v", err)
	}
	return gatePID
}

func ownedTransactionsWaitForBlock(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerPID, gatePID int32, finished <-chan error) {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(waitCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE pid = $1 AND wait_event_type = 'Lock'
			AND $2::integer = ANY(pg_blocking_pids(pid)))`, ownerPID, gatePID).Scan(&blocked); err != nil {
			t.Fatalf("observe the exact owner waiting for its query gate: %v", err)
		}
		if blocked {
			return
		}
		select {
		case err := <-finished:
			t.Fatalf("owned callback ended before reaching the database gate: %v", err)
		case <-ticker.C:
		case <-waitCtx.Done():
			t.Fatalf("the owner did not block on the held query gate: %v", waitCtx.Err())
		}
	}
}

func ownedTransactionsTerminateBackend(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerPID int32) {
	t.Helper()
	terminateCtx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	var terminated bool
	if err := pool.QueryRow(terminateCtx, "SELECT pg_terminate_backend($1::integer, 5000::bigint)", ownerPID).Scan(&terminated); err != nil || !terminated {
		t.Fatalf("terminate the reserved test owner backend: terminated = %t, error = %v", terminated, err)
	}
}

func ownedTransactionsAssertTakeover(t *testing.T, ctx context.Context, pool *pgxpool.Pool, oldStore *Store, allowedRoot string) {
	t.Helper()
	if oldStore.Available() {
		t.Fatal("terminated owner remained available after a query failure")
	}
	ownedTransactionsExpectAbsent(t, ctx, pool, "owned-transactions-lost-write")
	successorPool, err := pgxpool.NewWithConfig(ctx, pool.Config())
	if err != nil {
		t.Fatal("create a separate pool for the replacement owner")
	}
	libraryIntegrationPoolCleanup(t, successorPool)
	successor, err := New(successorPool, &libraryFixtureProber{}, []string{allowedRoot})
	if err != nil {
		t.Fatalf("acquire ownership after confirmed backend termination: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := successor.Close(cleanupCtx); err != nil {
			t.Errorf("close the replacement owner: %v", err)
		}
	})
	successorPID := int32(successor.ownership.conn.Conn().PgConn().PID())
	ownedTransactionsAssertReusable(t, ctx, successor, successorPID)
	called := false
	err = ownedTransactionsRun(t, ctx, oldStore, func(tx OwnedTx) error {
		called = true
		_, err := tx.Exec("UPDATE server_settings SET value = 'old owner' WHERE key = 'owned-transactions-reuse'")
		return err
	})
	if !errors.Is(err, ErrUnavailable) || called {
		t.Fatalf("old writer escaped its fence after replacement: callback called = %t, error = %v", called, err)
	}
	if err := ownedTransactionsCheckOwnership(t, ctx, oldStore); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("old owner reacquired a healthy session after replacement: %v", err)
	}
	ownedTransactionsExpectSetting(t, ctx, pool, "owned-transactions-reuse", "healthy")
	ownedTransactionsAssertReusable(t, ctx, successor, successorPID)
}

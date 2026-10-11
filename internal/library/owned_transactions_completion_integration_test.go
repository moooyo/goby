package library

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/notificationjournal"
)

// Keep the real pgx transaction and reserved connection while observing the
// exact completion calls and injecting failures at their submission boundaries.
type ownedCompletionObservedTx struct {
	pgx.Tx
	afterExec                       func(string)
	beforeCommit, beforeRollback    func()
	commits, rollbacks              int
	rollbackContextErr, rollbackErr error
	rollbackBounded                 bool
}

func (tx *ownedCompletionObservedTx) Exec(ctx context.Context, statement string, args ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, statement, args...)
	if err == nil && tx.afterExec != nil {
		tx.afterExec(statement)
	}
	return tag, err
}

func (tx *ownedCompletionObservedTx) Commit(ctx context.Context) error {
	tx.commits++
	if tx.beforeCommit != nil {
		tx.beforeCommit()
	}
	return tx.Tx.Commit(ctx)
}

func (tx *ownedCompletionObservedTx) Rollback(ctx context.Context) error {
	tx.rollbacks++
	tx.rollbackContextErr = ctx.Err()
	deadline, bounded := ctx.Deadline()
	tx.rollbackBounded = bounded && time.Until(deadline) > 0 && time.Until(deadline) <= 5*time.Second
	if tx.beforeRollback != nil {
		tx.beforeRollback()
	}
	tx.rollbackErr = tx.Tx.Rollback(ctx)
	return tx.rollbackErr
}

func ownedCompletionObserve(t *testing.T, ctx context.Context, store *Store) (*ownedTx, *ownedCompletionObservedTx) {
	t.Helper()
	tx := beginCatalogTestTransaction(t, ctx, store).(*ownedTx)
	driver := &ownedCompletionObservedTx{Tx: tx.Tx}
	tx.Tx = driver
	return tx, driver
}

func ownedCompletionExpire(t *testing.T, tx *ownedTx) {
	t.Helper()
	// Expire only the protected context after successful SQL. The caller and
	// the actual pgx connection remain live; no statement is in flight.
	expired, cancel := context.WithDeadline(tx.ctx, time.Time{})
	t.Cleanup(cancel)
	tx.ctx = expired
	if !errors.Is(tx.ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("the protected write context did not expire")
	}
}

func ownedCompletionExpectFinalized(t *testing.T, ctx context.Context, tx *ownedTx, driver *ownedCompletionObservedTx) {
	t.Helper()
	commits, rollbacks := driver.commits, driver.rollbacks
	if !tx.finished || len(tx.catalogChanges.changes) != 0 || tx.catalogChanges.resync {
		t.Fatal("transaction completion retained unfinished catalog publication")
	}
	if err := tx.Commit(ctx); !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatalf("a second commit escaped finalization: %v", err)
	}
	if err := tx.Rollback(ctx); !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatalf("a second rollback escaped finalization: %v", err)
	}
	if driver.commits != commits || driver.rollbacks != rollbacks {
		t.Fatal("an already finalized transaction reached the driver again")
	}
	if !tx.store.ownership.mu.TryLock() {
		t.Fatal("transaction completion retained the ownership mutex")
	}
	tx.store.ownership.mu.Unlock()
}

func TestOwnedCommitKnownExpiryRollsBackAndKeepsTheOwner(t *testing.T) {
	for _, boundary := range []string{"no_pending_event", "pending_journal", "already_flushed", "after_event_flush"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, pool, store, libraryID := notificationCollectorFixture(t)
			ownerConn := store.ownership.conn
			ownerPID := int32(ownerConn.Conn().PgConn().PID())
			notifications := catalogChangesTestListener(t, store)
			var beforeSequence int64
			if err := pool.QueryRow(ctx, `SELECT sequence FROM task_system_events WHERE name='LibraryChanged'`).Scan(&beforeSequence); err != nil {
				t.Fatal(err)
			}
			tx, driver := ownedCompletionObserve(t, ctx, store)
			key := "owned-completion-" + boundary
			if _, err := tx.Exec(ctx, `INSERT INTO server_settings(key,value) VALUES($1,'must roll back')`, key); err != nil {
				t.Fatal(err)
			}
			if boundary != "no_pending_event" {
				if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: libraryID,
					LibraryID: libraryID, IsFolder: true, IsCollectionFolder: true}); err != nil {
					t.Fatal(err)
				}
			}
			if boundary == "pending_journal" {
				// The catalog change already wrote its references. A new scope
				// requires another journal write at the completion boundary.
				tx.rememberNotificationReferences(notificationjournal.Reference{Kind: "Library", ID: libraryID, LibraryID: libraryID})
			}
			if boundary == "already_flushed" {
				if err := tx.flushSystemEvent(); err != nil {
					t.Fatalf("flush the pending journal and event before expiration: %v", err)
				}
			}
			if boundary == "after_event_flush" {
				driver.afterExec = func(statement string) {
					if strings.HasPrefix(statement, "UPDATE task_system_events SET sequence=sequence+1") {
						ownedCompletionExpire(t, tx)
					}
				}
			} else {
				ownedCompletionExpire(t, tx)
			}
			err := tx.Commit(ctx)
			if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, pgx.ErrTxCommitRollback) || errors.Is(err, ErrUnavailable) {
				t.Fatalf("known expiration did not report confirmed rollback: %v", err)
			}
			if ctx.Err() != nil || store.ctx.Err() != nil || store.ownership.conn != ownerConn {
				t.Fatal("protected expiration canceled the caller or replaced the reserved owner")
			}
			if driver.commits != 0 || driver.rollbacks != 1 || driver.rollbackContextErr != nil || !driver.rollbackBounded || driver.rollbackErr != nil {
				t.Fatalf("known expiration bypassed independent rollback: commits=%d rollbacks=%d context=%v bounded=%t rollback=%v",
					driver.commits, driver.rollbacks, driver.rollbackContextErr, driver.rollbackBounded, driver.rollbackErr)
			}
			if (boundary == "already_flushed" || boundary == "after_event_flush") && (!tx.systemEventRecorded || !tx.notificationJournalRecorded) {
				t.Fatal("the expiration fixture did not complete both pending event writes")
			}
			if err := rollbackSidecarTransaction(tx, err); err != nil {
				t.Fatalf("a confirmed rollback became unknown sidecar cleanup: %v", err)
			}
			ownedCompletionExpectFinalized(t, ctx, tx, driver)
			ownedTransactionsExpectAbsent(t, ctx, pool, key)
			assertNoCatalogTestNotification(t, notifications)
			var sequence int64
			var journalRows int
			if err := pool.QueryRow(ctx, `SELECT sequence,(SELECT count(*) FROM notification_source_events)
				FROM task_system_events WHERE name='LibraryChanged'`).Scan(&sequence, &journalRows); err != nil || sequence != beforeSequence || journalRows != 0 {
				t.Fatalf("expired completion committed event state: sequence=%d before=%d journal=%d error=%v", sequence, beforeSequence, journalRows, err)
			}
			ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
		})
	}
}

func TestOwnedCommitFlushFailurePreservesRollbackOutcome(t *testing.T) {
	for _, flush := range []string{"journal", "system_event"} {
		for _, failRollback := range []bool{false, true} {
			name := flush + "/confirmed_rollback"
			if failRollback {
				name = flush + "/failed_rollback"
			}
			t.Run(name, func(t *testing.T) {
				ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
				ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
				notifications := catalogChangesTestListener(t, store)
				if flush == "system_event" {
					if _, err := pool.Exec(ctx, `CREATE FUNCTION owned_completion_reject_event() RETURNS trigger LANGUAGE plpgsql AS $$
						BEGIN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='owned completion event failure'; END; $$;
						CREATE TRIGGER owned_completion_event_failure BEFORE UPDATE ON task_system_events
						FOR EACH ROW WHEN (NEW.name='LibraryChanged') EXECUTE FUNCTION owned_completion_reject_event()`); err != nil {
						t.Fatal(err)
					}
				}
				tx, driver := ownedCompletionObserve(t, ctx, store)
				if _, err := tx.Exec(ctx, `INSERT INTO server_settings(key,value) VALUES('owned-completion-flush','must roll back')`); err != nil {
					t.Fatal(err)
				}
				if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: "item", LibraryID: "library"}); err != nil {
					t.Fatal(err)
				}
				if flush == "journal" {
					// Invalidate the previously written prefix so Commit actually
					// attempts the deliberately malformed journal mutation.
					tx.rememberNotificationReferences(notificationjournal.Reference{Kind: "Library", ID: "library"})
					tx.notificationMutationID = "invalid journal identity"
				}
				if failRollback {
					driver.beforeRollback = func() { ownedTransactionsTerminateBackend(t, ctx, pool, ownerPID) }
				}
				err := tx.Commit(ctx)
				if flush == "journal" {
					if !errors.Is(err, notificationjournal.ErrJournal) {
						t.Fatalf("completion lost the original journal failure: %v", err)
					}
				} else {
					var databaseError *pgconn.PgError
					if !errors.As(err, &databaseError) || databaseError.Code != "23514" || databaseError.Message != "owned completion event failure" {
						t.Fatalf("completion lost the original event failure: %v", err)
					}
				}
				if errors.Is(err, pgx.ErrTxCommitRollback) == failRollback || errors.Is(err, ErrUnavailable) != failRollback {
					t.Fatalf("completion mislabeled its rollback outcome: failed=%t error=%v", failRollback, err)
				}
				if driver.commits != 0 || driver.rollbacks != 1 || driver.rollbackContextErr != nil || !driver.rollbackBounded {
					t.Fatalf("flush failure did not independently roll back: commits=%d rollbacks=%d context=%v bounded=%t",
						driver.commits, driver.rollbacks, driver.rollbackContextErr, driver.rollbackBounded)
				}
				cleanupErr := rollbackSidecarTransaction(tx, err)
				if failRollback {
					if driver.rollbackErr == nil || !errors.Is(err, driver.rollbackErr) || !errors.Is(cleanupErr, errSidecarRollbackUnknown) ||
						store.Available() || store.ctx.Err() == nil {
						t.Fatalf("failed cleanup hid uncertainty or ownership loss: commit=%v rollback=%v sidecar=%v", err, driver.rollbackErr, cleanupErr)
					}
				} else if cleanupErr != nil || driver.rollbackErr != nil {
					t.Fatalf("confirmed cleanup became uncertain: rollback=%v sidecar=%v", driver.rollbackErr, cleanupErr)
				}
				ownedCompletionExpectFinalized(t, ctx, tx, driver)
				ownedTransactionsExpectAbsent(t, ctx, pool, "owned-completion-flush")
				assertNoCatalogTestNotification(t, notifications)
				if !failRollback {
					ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
				}
			})
		}
	}
}

func TestOwnedCommitKnownExpiryRetainsFailedCleanup(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
	ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
	tx, driver := ownedCompletionObserve(t, ctx, store)
	if _, err := tx.Exec(ctx, `INSERT INTO server_settings(key,value) VALUES('owned-completion-expired-cleanup','must roll back')`); err != nil {
		t.Fatal(err)
	}
	ownedCompletionExpire(t, tx)
	driver.beforeRollback = func() { ownedTransactionsTerminateBackend(t, ctx, pool, ownerPID) }
	err := tx.Commit(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrUnavailable) || errors.Is(err, pgx.ErrTxCommitRollback) ||
		driver.rollbackErr == nil || !errors.Is(err, driver.rollbackErr) {
		t.Fatalf("expired completion concealed failed rollback: commit=%v rollback=%v", err, driver.rollbackErr)
	}
	if driver.commits != 0 || driver.rollbacks != 1 || store.Available() || store.ctx.Err() == nil || ctx.Err() != nil {
		t.Fatal("failed protected rollback did not fence exactly the lost owner")
	}
	if err := rollbackSidecarTransaction(tx, err); !errors.Is(err, errSidecarRollbackUnknown) {
		t.Fatalf("failed rollback acquired confirmed sidecar cleanup authority: %v", err)
	}
	ownedCompletionExpectFinalized(t, ctx, tx, driver)
	ownedTransactionsExpectAbsent(t, ctx, pool, "owned-completion-expired-cleanup")
}

func TestOwnedCommitAttemptedConnectionFailureRemainsUnknown(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
	ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
	notifications := catalogChangesTestListener(t, store)
	tx, driver := ownedCompletionObserve(t, ctx, store)
	if _, err := tx.Exec(ctx, `INSERT INTO server_settings(key,value) VALUES('owned-completion-failed-commit','must roll back')`); err != nil {
		t.Fatal(err)
	}
	if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: "item", LibraryID: "library"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.flushSystemEvent(); err != nil {
		t.Fatal(err)
	}
	driver.beforeCommit = func() {
		if tx.ctx.Err() != nil {
			t.Fatal("the attempted commit fixture exhausted its protected context")
		}
		ownedTransactionsTerminateBackend(t, ctx, pool, ownerPID)
	}
	err := tx.Commit(ctx)
	if !errors.Is(err, ErrUnavailable) || errors.Is(err, pgx.ErrTxCommitRollback) || driver.commits != 1 || driver.rollbacks != 0 || store.Available() {
		t.Fatalf("an actual driver commit failure became confirmed rollback: commits=%d rollbacks=%d error=%v", driver.commits, driver.rollbacks, err)
	}
	if err := rollbackSidecarTransaction(tx, err); !errors.Is(err, errSidecarRollbackUnknown) {
		t.Fatalf("an attempted commit failure became known sidecar cleanup: %v", err)
	}
	ownedCompletionExpectFinalized(t, ctx, tx, driver)
	ownedTransactionsExpectAbsent(t, ctx, pool, "owned-completion-failed-commit")
	assertNoCatalogTestNotification(t, notifications)
}

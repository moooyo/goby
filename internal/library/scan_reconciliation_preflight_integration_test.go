//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func scanPreflightTestReconcile(fixture rootBindingScanFixture, evidence *scanReconciliationEvidence, staging *scanReconciliationStaging, trace *scanShortProofSQLTracer) error {
	_, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library,
		[]*rootBindingScanCapture{trace.Capture}, evidence, nil, staging)
	return err
}

func scanPreflightTestHookResult(t *testing.T, trace *scanShortProofSQLTracer) {
	t.Helper()
	if err := trace.Snapshot().HookErr; err != nil {
		t.Fatalf("controlled preflight transition failed: %v", err)
	}
}

func TestScanReconciliationPreflightObservationReleasesOwnership(t *testing.T) {
	fixture, adapter, evidence, staging, trace := scanShortProofTestFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	firstProof := adapter.checks + 1
	adapter.revalidate = func(ctx context.Context, count int) error {
		if count == firstProof {
			if !fixture.store.ownership.mu.TryLock() {
				return errors.New("preflight observation retained the ownership gate")
			}
			fixture.store.ownership.mu.Unlock()
			close(entered)
			<-release
		}
		return ctx.Err()
	}
	trace.Reset()
	finished := make(chan error, 1)
	go func() { finished <- scanPreflightTestReconcile(fixture, evidence, staging, trace) }()
	select {
	case <-entered:
	case err := <-finished:
		t.Fatalf("preflight did not reach its lock-free observation: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("preflight did not enter its controlled observation")
	}
	otherCtx, cancelOther := context.WithTimeout(fixture.ctx, time.Second)
	defer cancelOther()
	if err := fixture.store.WithOwnedTx(otherCtx, func(tx OwnedTx) error {
		_, err := tx.Exec(`INSERT INTO server_settings(key,value) VALUES('preflight-owner-progress','committed')`)
		return err
	}); err != nil {
		t.Fatalf("unrelated owner transaction waited for preflight filesystem work: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("reconciliation after owner progress failed: %v", err)
		}
	case <-fixture.ctx.Done():
		t.Fatal("reconciliation did not finish after preflight observation released")
	}
	scanReconciliationCommitAssertItem(t, fixture, "short-proof-missing", false)
	ownedTransactionsExpectSetting(t, fixture.ctx, fixture.pool, "preflight-owner-progress", "committed")
}

func TestScanReconciliationPreflightRechecksCurrentFactsAfterObservation(t *testing.T) {
	for _, scenario := range []string{"candidate_path", "new_cascade_member", "source_reappeared", "root_binding", "cancel_flag"} {
		t.Run(scenario, func(t *testing.T) {
			fixture, _, evidence, staging, trace := scanShortProofTestFixture(t)
			notifications := catalogChangesTestListener(t, fixture.store)
			var changed atomic.Bool
			trace.Reset()
			trace.SetOnBegin(func(ordinal int64) error {
				if ordinal != 2 {
					return nil
				}
				changed.Store(true)
				switch scenario {
				case "candidate_path":
					_, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET path=$2 WHERE id=$1`,
						"short-proof-missing", filepath.Join(fixture.scanRoot.path, "Mismatched.mkv"))
					return err
				case "new_cascade_member":
					_, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
						(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
						VALUES('late-cascade-member',$1,$2,'short-proof-missing','Late','late','Series',true,
						'//series/late','//series/late')`, fixture.library.ID, fixture.scanRoot.id)
					return err
				case "source_reappeared":
					return os.WriteFile(filepath.Join(fixture.scanRoot.path, "Missing.mkv"), []byte("video:late-source"), 0600)
				case "root_binding":
					_, err := fixture.pool.Exec(fixture.ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1`, fixture.scanRoot.id)
					return err
				default:
					_, err := fixture.pool.Exec(fixture.ctx, `UPDATE scan_jobs SET cancel_requested=true WHERE id=$1`, fixture.task.job.ID)
					return err
				}
			})
			err := scanPreflightTestReconcile(fixture, evidence, staging, trace)
			scanPreflightTestHookResult(t, trace)
			want := errScanReconciliationEvidenceUnavailable
			if scenario == "root_binding" {
				want = ErrRootBindingConflict
			} else if scenario == "cancel_flag" {
				want = context.Canceled
			}
			if !changed.Load() || !errors.Is(err, want) {
				t.Fatalf("final transaction accepted changed preflight facts: changed=%t error=%v want=%v", changed.Load(), err, want)
			}
			scanReconciliationCommitAssertItem(t, fixture, "short-proof-missing", true)
			if scenario == "new_cascade_member" {
				scanReconciliationCommitAssertItem(t, fixture, "late-cascade-member", true)
			}
			assertNoCatalogTestNotification(t, notifications)
			if err := fixture.store.CheckOwnership(fixture.ctx); err != nil {
				t.Fatalf("rejected preflight transition destroyed healthy ownership: %v", err)
			}
		})
	}
}

func TestScanReconciliationEmptyPreflightStillRechecksAuthorityWithoutFilesystemWork(t *testing.T) {
	for _, scenario := range []string{"unchanged", "new_candidate", "root_binding", "cancel_flag"} {
		t.Run(scenario, func(t *testing.T) {
			fixture, adapter, evidence, staging, trace := scanShortProofTestFixtureWithPrepare(t, func(fixture rootBindingScanFixture) {
				if _, err := fixture.pool.Exec(fixture.ctx, `DELETE FROM items WHERE id='short-proof-missing'`); err != nil {
					t.Fatal(err)
				}
			})
			var filesystemCalls atomic.Int64
			adapter.revalidate = func(context.Context, int) error {
				filesystemCalls.Add(1)
				return errors.New("empty reconciliation touched filesystem evidence")
			}
			trace.Reset()
			trace.SetOnBegin(func(ordinal int64) error {
				if ordinal != 2 {
					return nil
				}
				switch scenario {
				case "new_candidate":
					_, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
						(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path)
						VALUES('late-physical-candidate',$1,$2,$1,'Late','late','Movie',$3,'Late.mkv')`,
						fixture.library.ID, fixture.scanRoot.id, filepath.Join(fixture.scanRoot.path, "Late.mkv"))
					return err
				case "root_binding":
					_, err := fixture.pool.Exec(fixture.ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1`, fixture.scanRoot.id)
					return err
				case "cancel_flag":
					_, err := fixture.pool.Exec(fixture.ctx, `UPDATE scan_jobs SET cancel_requested=true WHERE id=$1`, fixture.task.job.ID)
					return err
				default:
					return nil
				}
			})
			err := scanPreflightTestReconcile(fixture, evidence, staging, trace)
			scanPreflightTestHookResult(t, trace)
			if filesystemCalls.Load() != 0 {
				t.Fatalf("empty preflight ran %d filesystem proof calls", filesystemCalls.Load())
			}
			want := error(nil)
			if scenario == "new_candidate" {
				want = errScanReconciliationEvidenceUnavailable
			} else if scenario == "root_binding" {
				want = ErrRootBindingConflict
			} else if scenario == "cancel_flag" {
				want = context.Canceled
			}
			if !errors.Is(err, want) {
				t.Fatalf("empty preflight bypassed final current authority: error=%v want=%v", err, want)
			}
			if scenario == "new_candidate" {
				scanReconciliationCommitAssertItem(t, fixture, "late-physical-candidate", true)
			}
			if trace.Snapshot().OwnerTransactions != 2 {
				t.Fatal("empty preflight did not retain both short current-authority transactions")
			}
		})
	}
}

func TestScanReconciliationPreflightFailureIsNotLostWhenFinalPageBecomesEmpty(t *testing.T) {
	fixture, adapter, evidence, staging, trace := scanShortProofTestFixture(t)
	sentinel := errors.New("controlled preflight filesystem failure")
	adapter.revalidate = func(context.Context, int) error { return sentinel }
	trace.Reset()
	trace.SetOnBegin(func(ordinal int64) error {
		if ordinal == 2 {
			_, err := fixture.pool.Exec(fixture.ctx, `DELETE FROM items WHERE id='short-proof-missing'`)
			return err
		}
		return nil
	})
	err := scanPreflightTestReconcile(fixture, evidence, staging, trace)
	scanPreflightTestHookResult(t, trace)
	if !errors.Is(err, sentinel) || !scanReconciliationObservationOnly(err) {
		t.Fatalf("saved preflight failure became an empty successful pass: %v", err)
	}
	snapshot := trace.Snapshot()
	if len(snapshot.OwnerTransactionSpans) != 2 || snapshot.OwnerTransactionSpans[1].Finish != "rollback" {
		t.Fatalf("saved observation did not pass through protected final rollback: %#v", snapshot)
	}
	if err := fixture.store.CheckOwnership(fixture.ctx); err != nil {
		t.Fatalf("observation-only rollback destroyed ownership: %v", err)
	}
}

func TestScanReconciliationPreflightFailureAndOwnerLossRemainFatal(t *testing.T) {
	fixture, adapter, evidence, staging, trace := scanShortProofTestFixture(t, ErrUnavailable)
	sentinel := errors.New("controlled preflight failure before owner loss")
	ownerPID := int32(fixture.store.ownership.conn.Conn().PgConn().PID())
	firstProof := adapter.checks + 1
	adapter.revalidate = func(_ context.Context, count int) error {
		if count == firstProof {
			ownedTransactionsTerminateBackend(t, fixture.ctx, fixture.pool, ownerPID)
		}
		return sentinel
	}
	err := scanPreflightTestReconcile(fixture, evidence, staging, trace)
	if !errors.Is(err, sentinel) || !errors.Is(err, ErrUnavailable) || scanReconciliationObservationOnly(err) {
		t.Fatalf("owner failure was downgraded to retained observation success: %v", err)
	}
	scanReconciliationCommitAssertItem(t, fixture, "short-proof-missing", true)
	if fixture.store.Available() {
		t.Fatal("lost preflight owner remained available")
	}
}

func TestScanReconciliationPreflightRechecksSealedPassAfterOwnershipGap(t *testing.T) {
	fixture, adapter, evidence, staging, trace := scanShortProofTestFixture(t)
	firstProof := adapter.checks + 1
	adapter.revalidate = func(ctx context.Context, count int) error {
		if count == firstProof {
			if err := staging.Close(); err != nil {
				return err
			}
		}
		return ctx.Err()
	}
	err := scanPreflightTestReconcile(fixture, evidence, staging, trace)
	if !errors.Is(err, errScanReconciliationStagingState) || scanReconciliationObservationOnly(err) {
		t.Fatalf("final transaction borrowed a pass closed after preflight: %v", err)
	}
	scanReconciliationCommitAssertItem(t, fixture, "short-proof-missing", true)
}

func TestScanReconciliationStagedPostDeleteCancellationStillRollsBack(t *testing.T) {
	fixture, adapter, evidence, staging, trace := scanShortProofTestFixtureWithPrepare(t, func(fixture rootBindingScanFixture) {
		if _, err := fixture.pool.Exec(fixture.ctx, `CREATE SEQUENCE staged_reconciliation_delete_hits;
			CREATE FUNCTION staged_reconciliation_count_delete() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
				IF OLD.id='short-proof-missing' THEN PERFORM nextval('staged_reconciliation_delete_hits'); END IF;
				RETURN OLD;
			END;
			$$;
			CREATE TRIGGER staged_reconciliation_count_delete BEFORE DELETE ON items
			FOR EACH ROW EXECUTE FUNCTION staged_reconciliation_count_delete()`); err != nil {
			t.Fatal(err)
		}
	})
	var afterDelete atomic.Bool
	adapter.revalidate = func(ctx context.Context, _ int) error {
		var deleted bool
		if err := fixture.pool.QueryRow(ctx, `SELECT is_called FROM staged_reconciliation_delete_hits`).Scan(&deleted); err != nil {
			return err
		}
		if deleted {
			afterDelete.Store(true)
			fixture.task.cancel()
			return nil
		}
		return ctx.Err()
	}
	notifications := catalogChangesTestListener(t, fixture.store)
	err := scanPreflightTestReconcile(fixture, evidence, staging, trace)
	if !afterDelete.Load() || !errors.Is(err, context.Canceled) {
		t.Fatalf("staged proof omitted its authoritative post-DELETE cancellation: deleted=%t error=%v", afterDelete.Load(), err)
	}
	scanReconciliationCommitAssertItem(t, fixture, "short-proof-missing", true)
	assertNoCatalogTestNotification(t, notifications)
	if err := fixture.store.CheckOwnership(fixture.ctx); err != nil {
		t.Fatalf("staged rollback after cancellation destroyed ownership: %v", err)
	}
}

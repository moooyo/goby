package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func TestTaskScanAdmissionLinksOneJobAndPreservesRunAggregates(t *testing.T) {
	prober := taskScanBlockingProber()
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	library := taskScanCreateLibrary(t, ctx, store, root, "owned")
	runID, children := taskScanFixture(t, ctx, pool, library)
	runBefore := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID)
	updates := store.ScanUpdates()
	if updates == nil || cap(updates) != 1 || updates != store.ScanUpdates() {
		t.Fatal("scan updates must expose one stable, buffered notification channel")
	}
	callerCtx, cancelCaller := context.WithCancel(ctx)
	admission, err := store.AdmitTaskScan(callerCtx, children[0])
	cancelCaller()
	if err != nil || admission.Kind != ScanAdmitted || admission.Job.ID == "" || admission.Job.TaskChildID != children[0] {
		t.Fatalf("admit owned scan: admission = %+v, error = %v", admission, err)
	}
	taskScanAwaitSignal(t, ctx, prober.entered, "owned scan did not reach the prober after its caller disconnected")
	running, err := store.GetJob(ctx, admission.Job.ID)
	if err != nil || running.Status != "Running" || running.CancelRequested {
		t.Fatalf("owned scan did not run: job = %+v, error = %v", running, err)
	}
	taskScanAssertChild(t, ctx, pool, children[0], running)
	repeated, err := store.AdmitTaskScan(ctx, children[0])
	if err != nil || repeated.Kind != ScanAlreadyOwned || repeated.Job.ID != running.ID || repeated.Job.TaskChildID != children[0] {
		t.Fatalf("repeated admission lost ownership: admission = %+v, error = %v", repeated, err)
	}
	taskScanExpectCount(t, ctx, pool, "SELECT count(*) FROM scan_jobs", 1)
	taskScanExpectCount(t, ctx, pool, "SELECT count(*) FROM task_run_children", 1)
	taskScanAwaitSignal(t, ctx, updates, "admission and start did not publish a scan update")
	if err := store.CancelTaskScan(ctx, children[0]); err != nil {
		t.Fatalf("cancel the owned scan: %v", err)
	}
	taskScanAwaitSignal(t, ctx, prober.cancelled, "task cancellation did not reach the owned media probe")
	finished := libraryIntegrationWaitJob(t, ctx, store, running.ID, "Cancelled")
	if !finished.CancelRequested {
		t.Error("task cancellation did not retain its durable cancellation flag")
	}
	taskScanAssertChild(t, ctx, pool, children[0], finished)
	taskScanAwaitSignal(t, ctx, updates, "cancellation did not publish a scan update")
	if after := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID); after != runBefore {
		t.Error("the scan bridge changed coordinator-owned run state or aggregate counters")
	}
	if _, err := store.AdmitTaskScan(ctx, children[0]); !errors.Is(err, ErrTaskScanInactive) {
		t.Errorf("admission of a terminal child: got %v, want ErrTaskScanInactive", err)
	}
}

func TestTaskScanDoesNotAdoptOrCancelAnIndependentScan(t *testing.T) {
	prober := taskScanBlockingProber()
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	library := taskScanCreateLibrary(t, ctx, store, root, "independent")
	independent, err := store.StartScan(ctx, library.ID)
	if err != nil {
		t.Fatalf("start the independent scan: %v", err)
	}
	taskScanAwaitSignal(t, ctx, prober.entered, "independent scan did not enter the media probe")
	_, children := taskScanFixture(t, ctx, pool, library)
	before := taskScanRowSnapshot(t, ctx, pool, "scan_jobs", independent.ID)
	childBefore := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0])
	admission, err := store.AdmitTaskScan(ctx, children[0])
	if err != nil || admission.Kind != ScanDuplicate || admission.Job.ID != independent.ID || admission.Job.TaskChildID != "" {
		t.Fatalf("independent scan was not identified as a duplicate: admission = %+v, error = %v", admission, err)
	}
	child := taskScanReadChild(t, ctx, pool, children[0])
	if child.State != "waiting" || child.ScanJobID != "" || child.StartedAt != nil || child.FinishedAt != nil {
		t.Fatalf("duplicate admission adopted independent work: %+v", child)
	}
	if err := store.CancelTaskScan(ctx, children[0]); err != nil {
		t.Fatalf("cancel an unlinked task child: %v", err)
	}
	child = taskScanReadChild(t, ctx, pool, children[0])
	if child.State != "waiting" || child.ScanJobID != "" || child.FinishedAt != nil {
		t.Error("scan cancellation changed an unlinked child owned by the coordinator")
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "scan_jobs", independent.ID); after != before {
		t.Error("cancelling an unlinked child changed the independent scan")
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0]); after != childBefore {
		t.Error("duplicate admission or cancellation changed an unlinked child")
	}
	taskScanExpectCount(t, ctx, pool, "SELECT count(*) FROM scan_jobs", 1)
	// Normal completion proves the task cancellation did not cancel the local
	// worker context before it had a chance to persist that cancellation.
	close(prober.release)
	finished := libraryIntegrationWaitJob(t, ctx, store, independent.ID, "Completed")
	if finished.TaskChildID != "" {
		t.Error("an independent scan acquired a task owner while finishing")
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0]); after != childBefore {
		t.Error("finishing independent work changed the unlinked child")
	}
}

func TestTaskScanRejectsAReverseOnlyChildAssociation(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	library := taskScanCreateLibrary(t, ctx, store, root, "reverse-only-association")
	runID, children := taskScanFixture(t, ctx, pool, library)
	const jobID = "reverse-only-existing-scan"
	// This durable independent scan has no local worker. The schema permits
	// setting its reverse child reference without populating the child's scan
	// ID; public bridge methods must reject that inconsistent ownership.
	if _, err := pool.Exec(ctx, `INSERT INTO scan_jobs (id, library_id, status)
		VALUES ($1, $2, 'Queued')`, jobID, library.ID); err != nil {
		t.Fatalf("insert the independent queued scan: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE scan_jobs SET task_child_id = $2 WHERE id = $1", jobID, children[0]); err != nil {
		t.Fatalf("prepare the reverse-only association: %v", err)
	}
	jobBefore := taskScanRowSnapshot(t, ctx, pool, "scan_jobs", jobID)
	childBefore := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0])
	runBefore := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID)
	assertPreserved := func() {
		t.Helper()
		if after := taskScanRowSnapshot(t, ctx, pool, "scan_jobs", jobID); after != jobBefore {
			t.Error("rejecting inconsistent ownership changed the existing scan")
		}
		if after := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0]); after != childBefore {
			t.Error("rejecting inconsistent ownership changed the waiting child")
		}
		if after := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID); after != runBefore {
			t.Error("rejecting inconsistent ownership changed the parent run")
		}
		taskScanExpectCount(t, ctx, pool, "SELECT count(*) FROM scan_jobs", 1)
	}
	admission, err := store.AdmitTaskScan(ctx, children[0])
	if !errors.Is(err, ErrUnavailable) || admission.Kind != "" || admission.Job.ID != "" {
		t.Errorf("reverse-only association was admitted or classified as a duplicate: admission = %+v, error = %v", admission, err)
	}
	assertPreserved()
	if err := store.CancelTaskScan(ctx, children[0]); !errors.Is(err, ErrUnavailable) {
		t.Errorf("cancel a reverse-only association: got %v, want ErrUnavailable", err)
	}
	assertPreserved()
	if len(prober.calls()) != 0 {
		t.Error("rejecting inconsistent ownership started media work")
	}
}

func TestTaskScanPendingParentCanAdmitItsOwnScan(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	library := taskScanCreateLibrary(t, ctx, store, root, "pending-parent")
	runID, children := taskScanFixture(t, ctx, pool, library)
	if _, err := pool.Exec(ctx, "UPDATE task_runs SET state = 'pending', started_at = NULL WHERE id = $1", runID); err != nil {
		t.Fatalf("prepare the pending parent: %v", err)
	}
	before := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID)
	admission, err := store.AdmitTaskScan(ctx, children[0])
	if err != nil || admission.Kind != ScanAdmitted {
		t.Fatalf("admit a pending parent's child: admission = %+v, error = %v", admission, err)
	}
	completed := libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Completed")
	taskScanAssertChild(t, ctx, pool, children[0], completed)
	if after := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID); after != before {
		t.Error("the scan bridge promoted or finalized the pending parent")
	}
}

func TestTaskScanAdmissionRejectsInactiveOrUnknownChildren(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	library := taskScanCreateLibrary(t, ctx, store, root, "inactive")
	for _, state := range []string{"stopping", "completed", "failed", "cancelled", "interrupted"} {
		t.Run("parent_"+state, func(t *testing.T) {
			runID, children := taskScanFixture(t, ctx, pool, library)
			if _, err := pool.Exec(ctx, `UPDATE task_runs SET state = $2,
				stop_requested_at = CASE WHEN $2 = 'stopping' THEN now() ELSE NULL END,
				stop_reason = CASE WHEN $2 = 'stopping' THEN 'administrator' ELSE '' END,
				finished_at = CASE WHEN $2 = 'stopping' THEN NULL ELSE now() END WHERE id = $1`, runID, state); err != nil {
				t.Fatalf("prepare inactive parent: %v", err)
			}
			before := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0])
			if _, err := store.AdmitTaskScan(ctx, children[0]); !errors.Is(err, ErrTaskScanInactive) {
				t.Fatalf("admit child of %s parent: got %v, want ErrTaskScanInactive", state, err)
			}
			if after := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0]); after != before {
				t.Error("rejected admission changed the waiting child")
			}
		})
	}
	for _, state := range []string{"completed", "failed", "cancelled", "unavailable", "interrupted"} {
		t.Run("child_"+state, func(t *testing.T) {
			_, children := taskScanFixture(t, ctx, pool, library)
			if _, err := pool.Exec(ctx, "UPDATE task_run_children SET state = $2, finished_at = now() WHERE id = $1", children[0], state); err != nil {
				t.Fatalf("prepare terminal child: %v", err)
			}
			before := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0])
			if _, err := store.AdmitTaskScan(ctx, children[0]); !errors.Is(err, ErrTaskScanInactive) {
				t.Fatalf("admit %s child: got %v, want ErrTaskScanInactive", state, err)
			}
			if after := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0]); after != before {
				t.Error("rejected admission changed terminal history")
			}
		})
	}
	unknownID := strings.Repeat("0", 32)
	if _, err := store.AdmitTaskScan(ctx, unknownID); !errors.Is(err, ErrNotFound) {
		t.Errorf("admit unknown child: got %v, want ErrNotFound", err)
	}
	if err := store.CancelTaskScan(ctx, unknownID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cancel unknown child: got %v, want ErrNotFound", err)
	}
	taskScanExpectCount(t, ctx, pool, "SELECT count(*) FROM scan_jobs", 0)
}

func TestTaskScanMissingLibraryPersistsUnavailableHistory(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	library := taskScanCreateLibrary(t, ctx, store, root, "removed-before-admission")
	runID, children := taskScanFixture(t, ctx, pool, library)
	runBefore := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID)
	if err := store.DeleteLibrary(ctx, library.ID); err != nil {
		t.Fatalf("remove the waiting child's library: %v", err)
	}
	if _, err := store.AdmitTaskScan(ctx, children[0]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("admit missing library: got %v, want ErrNotFound", err)
	}
	child := taskScanReadChild(t, ctx, pool, children[0])
	if child.State != "unavailable" || child.LibraryID != library.ID || child.LibraryName != library.Name || child.ScanJobID != "" || child.StartedAt != nil || child.FinishedAt == nil || child.ErrorCode != "library_unavailable" || child.ErrorMessage == "" {
		t.Fatalf("missing library lost its explicit terminal snapshot: %+v", child)
	}
	taskScanExpectCount(t, ctx, pool, "SELECT count(*) FROM scan_jobs", 0)
	if after := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID); after != runBefore {
		t.Error("marking a library unavailable changed coordinator-owned aggregates")
	}
}

func TestTaskScanQueuedCancellationCopiesHistoryBeforeWorkersDrain(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprintf("native=%t", native), func(t *testing.T) {
			prober := taskScanBlockingProber()
			ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
			blockers := taskScanOccupyWorkers(t, ctx, store, root, prober)
			library := taskScanCreateLibrary(t, ctx, store, root, "queued")
			runID, children := taskScanFixture(t, ctx, pool, library)
			runBefore := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID)
			admission, err := store.AdmitTaskScan(ctx, children[0])
			if err != nil || admission.Kind != ScanAdmitted || admission.Job.Status != "Queued" {
				t.Fatalf("admit queued child: admission = %+v, error = %v", admission, err)
			}
			taskScanAssertChild(t, ctx, pool, children[0], admission.Job)
			if native {
				err = store.CancelJob(ctx, admission.Job.ID)
			} else {
				err = store.CancelTaskScan(ctx, children[0])
			}
			if err != nil {
				t.Fatalf("cancel queued scan: %v", err)
			}
			finished := libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Cancelled")
			if finished.StartedAt != nil || finished.Scanned != 0 || finished.Added != 0 || finished.Updated != 0 || !finished.CancelRequested {
				t.Fatalf("queued cancellation started work or lost its flag: %+v", finished)
			}
			taskScanAssertChild(t, ctx, pool, children[0], finished)
			beforeDrain := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0])
			if err := store.DeleteLibrary(ctx, library.ID); err != nil {
				t.Fatalf("delete the terminal queued scan before its worker drains: %v", err)
			}
			if _, err := store.GetJob(ctx, finished.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("deleting the cancelled library did not remove its scan row: %v", err)
			}
			close(prober.release)
			for _, blocker := range blockers {
				libraryIntegrationWaitJob(t, ctx, store, blocker.ID, "Completed")
			}
			taskScanClose(t, ctx, store)
			if len(prober.calls()) != 2 {
				t.Error("a cancelled queued child reached the media prober")
			}
			if after := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0]); after != beforeDrain {
				t.Error("draining an already cancelled job rewrote its terminal child snapshot")
			}
			if after := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID); after != runBefore {
				t.Error("queued cancellation changed coordinator-owned run state or counters")
			}
		})
	}
}

func TestTaskScanStoppingParentPreventsQueuedMediaProbes(t *testing.T) {
	prober := taskScanBlockingProber()
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	blockers := taskScanOccupyWorkers(t, ctx, store, root, prober)
	library := taskScanCreateLibrary(t, ctx, store, root, "parent-stopped")
	runID, children := taskScanFixture(t, ctx, pool, library)
	admission, err := store.AdmitTaskScan(ctx, children[0])
	if err != nil || admission.Kind != ScanAdmitted {
		t.Fatalf("admit child before stopping its parent: admission = %+v, error = %v", admission, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE task_runs SET state = 'stopping',
		stop_requested_at = now(), stop_reason = 'administrator' WHERE id = $1`, runID); err != nil {
		t.Fatalf("persist the parent stop without calling the scanner: %v", err)
	}
	stopping := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID)
	close(prober.release)
	for _, blocker := range blockers {
		libraryIntegrationWaitJob(t, ctx, store, blocker.ID, "Completed")
	}
	finished := libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Cancelled")
	if finished.StartedAt != nil || finished.Scanned != 0 || len(prober.calls()) != 2 {
		t.Fatalf("a child of a stopping parent reached media work: job = %+v, probes = %d", finished, len(prober.calls()))
	}
	taskScanAssertChild(t, ctx, pool, children[0], finished)
	if after := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID); after != stopping {
		t.Error("the scanner finalized the parent instead of leaving aggregation to the coordinator")
	}
}

func TestTaskScanAdmissionRechecksParentAfterDatabaseLockWait(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	library := taskScanCreateLibrary(t, ctx, store, root, "parent-lock")
	runID, children := taskScanFixture(t, ctx, pool, library)
	before := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0])
	gate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin parent gate: %v", err)
	}
	defer rollback(gate)
	if _, err := gate.Exec(ctx, "SELECT id FROM task_runs WHERE id = $1 FOR UPDATE", runID); err != nil {
		t.Fatalf("lock the active parent: %v", err)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := store.AdmitTaskScan(ctx, children[0])
		finished <- err
	}()
	taskScanWaitOwnerBlocked(t, ctx, pool, store, gate.Conn().PgConn().PID())
	if _, err := gate.Exec(ctx, `UPDATE task_runs SET state = 'stopping',
		stop_requested_at = now(), stop_reason = 'administrator' WHERE id = $1`, runID); err != nil {
		t.Fatalf("stop the locked parent: %v", err)
	}
	if err := gate.Commit(ctx); err != nil {
		t.Fatalf("commit the parent stop: %v", err)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, ErrTaskScanInactive) {
			t.Fatalf("admission after waiting on a stopped parent: got %v, want ErrTaskScanInactive", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("admission did not finish after the parent lock was released")
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0]); after != before {
		t.Error("admission based on stale parent state changed the child")
	}
	taskScanExpectCount(t, ctx, pool, "SELECT count(*) FROM scan_jobs", 0)
	if len(prober.calls()) != 0 {
		t.Error("rejected admission reached the media prober")
	}
}

func TestTaskScanTerminalResultCommitsAtomicallyAndSurvivesLibraryDeletion(t *testing.T) {
	prober := taskScanBlockingProber()
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	library := taskScanCreateLibrary(t, ctx, store, root, "history")
	// Prime one item, then change its content and add another item. The task scan
	// must preserve both nonzero update and add counters in its durable history.
	prober.release <- struct{}{}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	for len(prober.entered) > 0 {
		<-prober.entered
	}
	libraryIntegrationFile(t, root, "history/Fixture.mp4", "video:changed content with a different length")
	libraryIntegrationFile(t, root, "history/Second.mp4", "video:new item")
	runID, children := taskScanFixture(t, ctx, pool, library)
	runBefore := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID)
	admission, err := store.AdmitTaskScan(ctx, children[0])
	if err != nil || admission.Kind != ScanAdmitted {
		t.Fatalf("admit the history scan: admission = %+v, error = %v", admission, err)
	}
	taskScanAwaitSignal(t, ctx, prober.entered, "history scan did not enter the controlled media probe")
	// Pause the terminal child UPDATE inside PostgreSQL. A separately committed
	// terminal scan would be visible while this trigger waits, violating the
	// guarantee that deleting a finished library cannot erase its task outcome.
	if _, err := pool.Exec(ctx, `CREATE FUNCTION task_scan_terminal_gate() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.state = 'completed' AND OLD.state <> 'completed' THEN
				PERFORM pg_advisory_xact_lock(hashtextextended(current_schema() || ':task-scan-terminal-gate', 0));
			END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER task_scan_terminal_gate BEFORE UPDATE ON task_run_children
		FOR EACH ROW EXECUTE FUNCTION task_scan_terminal_gate()`); err != nil {
		t.Fatalf("install the owned-schema terminal transaction gate: %v", err)
	}
	gate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin terminal transaction gate: %v", err)
	}
	defer rollback(gate)
	if _, err := gate.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_schema() || ':task-scan-terminal-gate', 0))"); err != nil {
		t.Fatalf("hold the terminal snapshot gate: %v", err)
	}
	close(prober.release)
	taskScanWaitOwnerBlocked(t, ctx, pool, store, gate.Conn().PgConn().PID())
	visible, err := store.GetJob(ctx, admission.Job.ID)
	if err != nil || visible.Status != "Running" || visible.FinishedAt != nil {
		t.Fatalf("terminal scan escaped before its child result committed: job = %+v, error = %v", visible, err)
	}
	child := taskScanReadChild(t, ctx, pool, children[0])
	if child.State != "running" || child.FinishedAt != nil {
		t.Fatalf("terminal child became visible before the scan transaction committed: %+v", child)
	}
	if err := gate.Commit(ctx); err != nil {
		t.Fatalf("release the terminal snapshot gate: %v", err)
	}
	completed := libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Completed")
	if completed.Scanned != 2 || completed.Added != 1 || completed.Updated != 1 {
		t.Fatalf("task scan lost concrete file counters: %+v", completed)
	}
	taskScanAssertChild(t, ctx, pool, children[0], completed)
	childBefore := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0])
	jobBefore := taskScanRowSnapshot(t, ctx, pool, "scan_jobs", completed.ID)
	if _, err := pool.Exec(ctx, "UPDATE task_run_children SET scanned = scanned + 1 WHERE id = $1", children[0]); err != nil {
		t.Fatalf("prepare an inconsistent terminal child: %v", err)
	}
	inconsistentChild := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0])
	if err := store.finishTask(&scanTask{job: completed, ctx: ctx}, "Completed", completed.Error); !errors.Is(err, ErrUnavailable) {
		t.Errorf("terminal finalizer accepted mismatched child counters: got %v, want ErrUnavailable", err)
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0]); after != inconsistentChild {
		t.Error("rejected finalization silently rewrote inconsistent child history")
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "scan_jobs", completed.ID); after != jobBefore {
		t.Error("rejected finalization changed the already terminal scan")
	}
	if _, err := pool.Exec(ctx, "UPDATE task_run_children SET scanned = $2 WHERE id = $1", children[0], completed.Scanned); err != nil {
		t.Fatalf("restore the valid terminal fixture before deletion: %v", err)
	}
	if err := store.DeleteLibrary(ctx, library.ID); err != nil {
		t.Fatalf("delete the completed library: %v", err)
	}
	if _, err := store.GetJob(ctx, completed.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("library deletion retained its scan row: %v", err)
	}
	if err := store.finishTask(&scanTask{job: completed, ctx: ctx}, "Completed", completed.Error); err != nil {
		t.Errorf("late finalizer could not recognize preserved terminal history: %v", err)
	}
	inconsistentJob := completed
	inconsistentJob.Scanned++
	if err := store.finishTask(&scanTask{job: inconsistentJob, ctx: ctx}, "Completed", completed.Error); !errors.Is(err, ErrUnavailable) {
		t.Errorf("late finalizer accepted mismatched cached counters after library deletion: got %v, want ErrUnavailable", err)
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[0]); after != childBefore {
		t.Error("library deletion or a late finalizer rewrote the historical task outcome")
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID); after != runBefore {
		t.Error("the scan bridge changed run aggregates while persisting or retaining history")
	}
}

func TestTaskScanFailedScanCopiesTerminalError(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	library := taskScanCreateLibrary(t, ctx, store, root, "unavailable-root")
	runID, children := taskScanFixture(t, ctx, pool, library)
	runBefore := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID)
	if err := os.Rename(library.Paths[0], filepath.Join(root, "moved-root")); err != nil {
		t.Fatalf("move the owned fixture root before scanning: %v", err)
	}
	admission, err := store.AdmitTaskScan(ctx, children[0])
	if err != nil || admission.Kind != ScanAdmitted {
		t.Fatalf("admit scan with an unavailable root: admission = %+v, error = %v", admission, err)
	}
	failed := libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Failed")
	if failed.Error == "" || failed.StartedAt == nil || len(prober.calls()) != 0 {
		t.Fatalf("failed scan did not retain the root failure: job = %+v, probes = %d", failed, len(prober.calls()))
	}
	taskScanAssertChild(t, ctx, pool, children[0], failed)
	if after := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID); after != runBefore {
		t.Error("a failed scan changed coordinator-owned aggregates")
	}
}

func TestTaskScanCompletedWarningRemainsACompletedChild(t *testing.T) {
	prober := scanProberFunc(func(context.Context, *os.File) (media.Info, error) {
		return media.Info{}, errors.New("fixture media could not be inspected")
	})
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	library := taskScanCreateLibrary(t, ctx, store, root, "warning")
	_, children := taskScanFixture(t, ctx, pool, library)
	admission, err := store.AdmitTaskScan(ctx, children[0])
	if err != nil || admission.Kind != ScanAdmitted {
		t.Fatalf("admit the scan warning fixture: admission = %+v, error = %v", admission, err)
	}
	completed := libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Completed")
	if completed.Error == "" || completed.Added != 0 || completed.Updated != 0 {
		t.Fatalf("scan warning fixture lost its retained warning or published unprobed media: %+v", completed)
	}
	taskScanAssertChild(t, ctx, pool, children[0], completed)
}

func TestTaskScanShutdownInterruptsOwnedWorkAndCancelsIndependentWork(t *testing.T) {
	prober := taskScanBlockingProber()
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	ownedLibrary := taskScanCreateLibrary(t, ctx, store, root, "owned-shutdown")
	independentLibrary := taskScanCreateLibrary(t, ctx, store, root, "independent-shutdown")
	runID, children := taskScanFixture(t, ctx, pool, ownedLibrary)
	runBefore := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID)
	admission, err := store.AdmitTaskScan(ctx, children[0])
	if err != nil || admission.Kind != ScanAdmitted {
		t.Fatalf("admit task scan before shutdown: admission = %+v, error = %v", admission, err)
	}
	independent, err := store.StartScan(ctx, independentLibrary.ID)
	if err != nil {
		t.Fatalf("start independent scan before shutdown: %v", err)
	}
	for range 2 {
		taskScanAwaitSignal(t, ctx, prober.entered, "both shutdown fixtures did not enter their media probes")
	}
	taskScanClose(t, ctx, store)
	interrupted := libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Interrupted")
	taskScanAssertChild(t, ctx, pool, children[0], interrupted)
	cancelled := libraryIntegrationWaitJob(t, ctx, store, independent.ID, "Cancelled")
	if cancelled.TaskChildID != "" {
		t.Error("shutdown attached an independent scan to a task")
	}
	for range 2 {
		taskScanAwaitSignal(t, ctx, prober.cancelled, "shutdown did not cancel both media probe contexts")
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID); after != runBefore {
		t.Error("scanner shutdown changed generic task state before coordinator recovery")
	}
}

func TestTaskScanStartupRecoveryCopiesChildrenBeforeGenericRunRecovery(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	libraries := make([]Library, 0, 4)
	for _, name := range []string{"recover-queued", "recover-running", "keep-completed", "recover-independent"} {
		libraries = append(libraries, taskScanCreateLibrary(t, ctx, store, root, name))
	}
	runID, children := taskScanFixture(t, ctx, pool, libraries[:3]...)
	taskScanClose(t, ctx, store)
	for index, state := range []string{"Queued", "Running", "Completed"} {
		jobID := fmt.Sprintf("task-recovery-%d", index)
		if _, err := pool.Exec(ctx, `INSERT INTO scan_jobs
			(id, library_id, status, task_child_id, scanned, added, updated, started_at, finished_at)
			VALUES ($1, $2, $3, $4, 7, 4, 3,
				CASE WHEN $3 = 'Queued' THEN NULL ELSE now() - interval '1 minute' END,
				CASE WHEN $3 = 'Completed' THEN now() ELSE NULL END)`, jobID, libraries[index].ID, state, children[index]); err != nil {
			t.Fatalf("insert abandoned linked scan: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE task_run_children c SET state = lower(j.status),
			scan_job_id = j.id, started_at = j.started_at, finished_at = j.finished_at,
			scanned = CASE WHEN j.status = 'Completed' THEN j.scanned ELSE 0 END,
			added = CASE WHEN j.status = 'Completed' THEN j.added ELSE 0 END,
			updated = CASE WHEN j.status = 'Completed' THEN j.updated ELSE 0 END
			FROM scan_jobs j WHERE c.id = $1 AND j.id = $2`, children[index], jobID); err != nil {
			t.Fatalf("link the abandoned child snapshot: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scan_jobs (id, library_id, status)
		VALUES ('task-recovery-independent', $1, 'Queued')`, libraries[3].ID); err != nil {
		t.Fatalf("insert abandoned independent scan: %v", err)
	}
	runBefore := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID)
	completedBefore := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[2])
	completedJobBefore := taskScanRowSnapshot(t, ctx, pool, "scan_jobs", "task-recovery-2")
	prober := &libraryFixtureProber{}
	reopened, err := New(pool, prober, []string{root})
	if err != nil {
		t.Fatalf("recover abandoned task scans: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := reopened.Close(cleanupCtx); err != nil {
			t.Errorf("close recovered scanner: %v", err)
		}
	})
	for index := range 2 {
		job := libraryIntegrationWaitJob(t, ctx, reopened, fmt.Sprintf("task-recovery-%d", index), "Interrupted")
		if job.Scanned != 7 || job.Added != 4 || job.Updated != 3 || job.Error == "" {
			t.Fatalf("recovery lost the abandoned scan result: %+v", job)
		}
		taskScanAssertChild(t, ctx, pool, children[index], job)
	}
	independent := libraryIntegrationWaitJob(t, ctx, reopened, "task-recovery-independent", "Interrupted")
	if independent.TaskChildID != "" {
		t.Error("startup recovery fabricated task ownership for an independent scan")
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "task_runs", runID); after != runBefore {
		t.Error("scanner startup recovered generic runs before their coordinator started")
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "task_run_children", children[2]); after != completedBefore {
		t.Error("startup recovery rewrote an already terminal child")
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "scan_jobs", "task-recovery-2"); after != completedJobBefore {
		t.Error("startup recovery rewrote an already terminal scan")
	}
	if len(prober.calls()) != 0 {
		t.Error("startup recovery resumed media work instead of preserving interruption")
	}
}

func TestTaskScanCapacityAndIndependentDuplicateRemainDistinct(t *testing.T) {
	prober := taskScanBlockingProber()
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	blockers := taskScanOccupyWorkers(t, ctx, store, root, prober)
	available := taskScanCreateLibrary(t, ctx, store, root, "capacity-waiting")
	_, duplicateChildren := taskScanFixture(t, ctx, pool, Library{ID: blockers[0].LibraryID, Name: "Independent blocker"})
	_, waitingChildren := taskScanFixture(t, ctx, pool, available)
	duplicateBefore := taskScanRowSnapshot(t, ctx, pool, "task_run_children", duplicateChildren[0])
	waitingBefore := taskScanRowSnapshot(t, ctx, pool, "task_run_children", waitingChildren[0])
	// Both workers are proven inside blocking probes. Fill only the in-memory
	// capacity with inert sentinels and remove every sentinel before either
	// worker can return; no fabricated job is ever sent to a worker or database.
	sentinel := &scanTask{}
	store.mu.Lock()
	if len(store.queue) != 0 || cap(store.queue) == 0 {
		store.mu.Unlock()
		t.Fatal("capacity fixture requires an empty, buffered queue and two occupied workers")
	}
	capacity := cap(store.queue)
	for range capacity {
		store.queue <- sentinel
	}
	store.mu.Unlock()
	defer func() {
		store.mu.Lock()
		defer store.mu.Unlock()
		for range capacity {
			select {
			case task := <-store.queue:
				if task != sentinel {
					t.Error("capacity fixture encountered unexpected queued work")
				}
			default:
				t.Error("a blocked worker consumed a capacity sentinel")
				return
			}
		}
	}()
	duplicate, err := store.AdmitTaskScan(ctx, duplicateChildren[0])
	if err != nil || duplicate.Kind != ScanDuplicate || duplicate.Job.ID != blockers[0].ID {
		t.Fatalf("queue exhaustion obscured independent duplicate ownership: admission = %+v, error = %v", duplicate, err)
	}
	full, err := store.AdmitTaskScan(ctx, waitingChildren[0])
	if err != nil || full.Kind != ScanQueueFull || full.Job.ID != "" {
		t.Fatalf("queue exhaustion admitted or misclassified waiting work: admission = %+v, error = %v", full, err)
	}
	if _, err := store.StartScan(ctx, blockers[0].LibraryID); !errors.Is(err, ErrScanAlreadyActive) || !errors.Is(err, ErrBusy) || errors.Is(err, ErrScanQueueFull) {
		t.Errorf("native duplicate at full capacity lost its specific busy error: %v", err)
	}
	if _, err := store.StartScan(ctx, available.ID); !errors.Is(err, ErrScanQueueFull) || !errors.Is(err, ErrBusy) || errors.Is(err, ErrScanAlreadyActive) {
		t.Errorf("native capacity failure lost its specific busy error: %v", err)
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "task_run_children", duplicateChildren[0]); after != duplicateBefore {
		t.Error("duplicate admission changed its waiting child")
	}
	if after := taskScanRowSnapshot(t, ctx, pool, "task_run_children", waitingChildren[0]); after != waitingBefore {
		t.Error("capacity rejection changed its waiting child")
	}
	taskScanExpectCount(t, ctx, pool, "SELECT count(*) FROM scan_jobs", 2)
}

type taskScanChildSnapshot struct {
	ID, RunID, LibraryID, LibraryName, State, ScanJobID string
	Scanned, Added, Updated                             int
	ErrorCode, ErrorMessage                             string
	StartedAt, FinishedAt                               *time.Time
}

func taskScanFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraries ...Library) (string, []string) {
	t.Helper()
	definitionID, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	runID, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin the task scan fixture: %v", err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `INSERT INTO task_definitions (id, key, emby_key, name)
		VALUES ($1, $2, 'RefreshLibrary', 'Scan bridge fixture')`, definitionID, "scan-bridge-"+definitionID); err != nil {
		t.Fatalf("create the task definition fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO task_runs
		(id, task_id, state, source, task_key, task_emby_key, task_name, started_at, total_children)
		VALUES ($1, $2, 'running', 'manual', $3, 'RefreshLibrary', 'Scan bridge fixture', now(), $4)`,
		runID, definitionID, "scan-bridge-"+definitionID, len(libraries)); err != nil {
		t.Fatalf("create the task run fixture: %v", err)
	}
	children := make([]string, 0, len(libraries))
	for index, library := range libraries {
		childID, err := randomID()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO task_run_children (id, run_id, library_id, library_name, ordinal)
			VALUES ($1, $2, $3, $4, $5)`, childID, runID, library.ID, library.Name, index); err != nil {
			t.Fatalf("create the task child fixture: %v", err)
		}
		children = append(children, childID)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit the task scan fixture: %v", err)
	}
	return runID, children
}

func taskScanCreateLibrary(t *testing.T, ctx context.Context, store *Store, root, name string) Library {
	t.Helper()
	path := libraryIntegrationFile(t, root, name+"/Fixture.mp4", "video:"+name)
	return libraryIntegrationCreate(t, ctx, store, name, "movies", filepath.Dir(path))
}

func taskScanBlockingProber() *libraryFixtureProber {
	return &libraryFixtureProber{
		block: true, entered: make(chan struct{}, 4), cancelled: make(chan struct{}, 4), release: make(chan struct{}, 1),
	}
}

func taskScanOccupyWorkers(t *testing.T, ctx context.Context, store *Store, root string, prober *libraryFixtureProber) []Job {
	t.Helper()
	jobs := make([]Job, 0, 2)
	for index := range 2 {
		library := taskScanCreateLibrary(t, ctx, store, root, fmt.Sprintf("blocker-%d", index))
		job, err := store.StartScan(ctx, library.ID)
		if err != nil {
			t.Fatalf("occupy a scan worker: %v", err)
		}
		taskScanAwaitSignal(t, ctx, prober.entered, "scan worker did not enter its blocking media probe")
		jobs = append(jobs, job)
	}
	return jobs
}

func taskScanAwaitSignal(t *testing.T, ctx context.Context, signal <-chan struct{}, message string) {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	select {
	case <-signal:
	case <-waitCtx.Done():
		t.Fatal(message)
	}
}

func taskScanReadChild(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) taskScanChildSnapshot {
	t.Helper()
	var child taskScanChildSnapshot
	if err := pool.QueryRow(ctx, `SELECT id, run_id, library_id, library_name, state,
		COALESCE(scan_job_id, ''), scanned, added, updated, error_code, error_message,
		started_at, finished_at FROM task_run_children WHERE id = $1`, id).Scan(
		&child.ID, &child.RunID, &child.LibraryID, &child.LibraryName, &child.State,
		&child.ScanJobID, &child.Scanned, &child.Added, &child.Updated, &child.ErrorCode,
		&child.ErrorMessage, &child.StartedAt, &child.FinishedAt); err != nil {
		t.Fatalf("read task child snapshot: %v", err)
	}
	return child
}

func taskScanAssertChild(t *testing.T, ctx context.Context, pool *pgxpool.Pool, childID string, job Job) {
	t.Helper()
	child := taskScanReadChild(t, ctx, pool, childID)
	if child.ID != job.TaskChildID || child.LibraryID != job.LibraryID || child.ScanJobID != job.ID || child.State != strings.ToLower(job.Status) ||
		child.Scanned != job.Scanned || child.Added != job.Added || child.Updated != job.Updated || child.ErrorMessage != job.Error ||
		!taskScanSameTime(child.StartedAt, job.StartedAt) || !taskScanSameTime(child.FinishedAt, job.FinishedAt) {
		t.Fatalf("child snapshot differs from its owning scan: child = %+v, job = %+v", child, job)
	}
	wantCode := map[string]string{
		"Cancelled": "scan_cancelled", "Interrupted": "scan_interrupted", "Failed": "scan_failed",
	}[job.Status]
	if child.ErrorCode != wantCode {
		t.Errorf("child error code = %q, want %q for status %s", child.ErrorCode, wantCode, job.Status)
	}
}

func taskScanSameTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Equal(*right)
}

func taskScanRowSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, id string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(snapshot_row)::text FROM "+pgx.Identifier{table}.Sanitize()+" AS snapshot_row WHERE id = $1", id).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot %s fixture: %v", table, err)
	}
	return snapshot
}

func taskScanExpectCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, query).Scan(&count); err != nil || count != want {
		t.Fatalf("persistent fixture count = %d, want %d; error = %v", count, want, err)
	}
}

func taskScanWaitOwnerBlocked(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, blocker uint32) {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	ownerPID := store.ownership.conn.Conn().PgConn().PID()
	for {
		var blocked bool
		if err := pool.QueryRow(waitCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE pid = $1 AND wait_event_type = 'Lock' AND $2::integer = ANY(pg_blocking_pids(pid)))`,
			ownerPID, blocker).Scan(&blocked); err != nil {
			t.Fatalf("observe the scan owner's database lock wait: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-ticker.C:
		case <-waitCtx.Done():
			t.Fatal("the scan owner did not reach its intended database lock barrier")
		}
	}
}

func taskScanClose(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	closeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := store.Close(closeCtx); err != nil {
		t.Fatalf("close the scan store: %v", err)
	}
}

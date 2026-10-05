//go:build linux

package library

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Synthetic workers do not run concurrently. Hold their elapsed-time window
// open while testing entry boundaries; the pure due tests cover exact deadlines.
func scanProgressBatchHoldWindow(task *scanTask) {
	task.recordSavedProgress(task.job, time.Now().Add(time.Hour))
}

func TestScanProgressBatchDueBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	job := Job{Scanned: 10, Added: 3, Updated: 2}
	checkpoint := scanProgressCheckpoint{scanned: job.Scanned, added: job.Added, updated: job.Updated, savedAt: now}
	for _, test := range []struct {
		name    string
		entries int
		elapsed time.Duration
		want    bool
	}{
		{"same_snapshot", 0, 0, false},
		{"below_both_limits", scanProgressCheckpointItems - 1, scanProgressCheckpointInterval - time.Nanosecond, false},
		{"entry_limit", scanProgressCheckpointItems, 0, true},
		{"elapsed_with_pending", 1, scanProgressCheckpointInterval, true},
		{"elapsed_without_pending", 0, scanProgressCheckpointInterval, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := job
			candidate.Scanned += test.entries
			if got := checkpoint.due(candidate, now.Add(test.elapsed)); got != test.want {
				t.Fatalf("checkpoint due=%t want=%t for entries=%d elapsed=%s", got, test.want, test.entries, test.elapsed)
			}
		})
	}
	if !(scanProgressCheckpoint{}).due(job, now) {
		t.Fatal("a worker without a saved checkpoint skipped its first observation")
	}
}

func TestScanProgressBatchPersistsEntryAndElapsedBoundaries(t *testing.T) {
	ctx, pool, store, task, _ := scanUnchangedProgressFixture(t)
	scanProgressBatchHoldWindow(task)
	saved := task.progress
	before := scanUnchangedProgressSnapshot(t, ctx, pool, task)
	task.job.Scanned += scanProgressCheckpointItems - 1
	for range 2 {
		if err := store.maybePersistProgress(task); err != nil {
			t.Fatal(err)
		}
	}
	if task.progress != saved || scanUnchangedProgressSnapshot(t, ctx, pool, task) != before {
		t.Fatal("a pending batch advanced its saved watermark or durable rows")
	}
	task.job.Scanned++
	if err := store.maybePersistProgress(task); err != nil {
		t.Fatal(err)
	}
	job, err := store.GetJob(ctx, task.job.ID)
	if err != nil || job.Scanned != task.job.Scanned || task.progress.scanned != job.Scanned || task.progress.savedAt == saved.savedAt {
		t.Fatalf("entry boundary did not publish its checkpoint: job=%+v watermark=%+v error=%v", job, task.progress, err)
	}
	taskScanAssertChild(t, ctx, pool, task.job.TaskChildID, job)
	task.recordSavedProgress(task.job, time.Now().Add(-scanProgressCheckpointInterval))
	task.job.Scanned++
	if err := store.maybePersistProgress(task); err != nil {
		t.Fatal(err)
	}
	job, err = store.GetJob(ctx, task.job.ID)
	if err != nil || job.Scanned != task.job.Scanned || task.progress.scanned != job.Scanned {
		t.Fatalf("elapsed boundary omitted its pending entry: job=%+v error=%v", job, err)
	}
	taskScanAssertChild(t, ctx, pool, task.job.TaskChildID, job)
}

func TestScanProgressBatchFailedCheckpointKeepsWatermarkForRetry(t *testing.T) {
	ctx, pool, store, task, _ := scanUnchangedProgressFixture(t)
	scanProgressBatchHoldWindow(task)
	saved := task.progress
	before := scanUnchangedProgressSnapshot(t, ctx, pool, task)
	task.job.Scanned += scanProgressCheckpointItems
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_batch_progress() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected checkpoint failure'; END; $$;
		CREATE TRIGGER reject_batch_progress BEFORE UPDATE ON scan_jobs
		FOR EACH ROW EXECUTE FUNCTION reject_batch_progress()`); err != nil {
		t.Fatal(err)
	}
	if err := store.maybePersistProgress(task); err == nil {
		t.Fatal("failed progress transaction was reported as saved")
	}
	if task.progress != saved || scanUnchangedProgressSnapshot(t, ctx, pool, task) != before {
		t.Fatal("failed progress transaction advanced its watermark or durable rows")
	}
	if _, err := pool.Exec(ctx, "DROP TRIGGER reject_batch_progress ON scan_jobs"); err != nil {
		t.Fatal(err)
	}
	if err := store.maybePersistProgress(task); err != nil {
		t.Fatal(err)
	}
	job, err := store.GetJob(ctx, task.job.ID)
	if err != nil || job.Scanned != task.job.Scanned || task.progress.scanned != job.Scanned || task.progress.savedAt == saved.savedAt {
		t.Fatalf("retry lost its pending batch: job=%+v watermark=%+v error=%v", job, task.progress, err)
	}
	taskScanAssertChild(t, ctx, pool, task.job.TaskChildID, job)
}

func TestScanProgressBatchContextCancellationIsImmediate(t *testing.T) {
	ctx, pool, store, task, _ := scanUnchangedProgressFixture(t)
	scanProgressBatchHoldWindow(task)
	saved := task.progress
	before := scanUnchangedProgressSnapshot(t, ctx, pool, task)
	task.job.Scanned++
	task.cancel()
	if err := store.maybePersistProgress(task); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled worker waited for its batch boundary: %v", err)
	}
	if task.progress != saved || scanUnchangedProgressSnapshot(t, ctx, pool, task) != before {
		t.Fatal("cancelled batch wrote progress before terminal finalization")
	}
}

func TestScanProgressBatchDatabaseCancellationIsCheckedAtBound(t *testing.T) {
	ctx, pool, store, task, _ := scanUnchangedProgressFixture(t)
	scanProgressBatchHoldWindow(task)
	task.job.Scanned += scanProgressCheckpointItems - 1
	if _, err := pool.Exec(ctx, "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", task.job.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.maybePersistProgress(task); err != nil {
		t.Fatalf("database-only cancellation changed the admitted batch policy: %v", err)
	}
	task.job.Scanned++
	if err := store.maybePersistProgress(task); !errors.Is(err, context.Canceled) || !errors.Is(task.ctx.Err(), context.Canceled) {
		t.Fatalf("entry boundary missed durable cancellation: error=%v context=%v", err, task.ctx.Err())
	}
	job, err := store.GetJob(ctx, task.job.ID)
	if err != nil || !job.CancelRequested || job.Scanned != task.job.Scanned {
		t.Fatalf("cancelled checkpoint lost its observed prefix: job=%+v error=%v", job, err)
	}
	taskScanAssertChild(t, ctx, pool, task.job.TaskChildID, job)
}

func TestScanProgressBatchElapsedWithoutChangesChecksDatabaseStop(t *testing.T) {
	ctx, pool, store, task, runID := scanUnchangedProgressFixture(t)
	scanProgressBatchHoldWindow(task)
	before := task.job
	if _, err := pool.Exec(ctx, `UPDATE task_runs SET state='stopping',stop_reason='administrator',
		stop_requested_at=clock_timestamp() WHERE id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	if err := store.maybePersistProgress(task); err != nil {
		t.Fatalf("unchanged worker did not retain its open batch: %v", err)
	}
	task.progress.savedAt = time.Now().Add(-scanProgressCheckpointInterval)
	if err := store.maybePersistProgress(task); !errors.Is(err, context.Canceled) || !errors.Is(task.ctx.Err(), context.Canceled) {
		t.Fatalf("elapsed checkpoint with unchanged counters missed a stopped parent: error=%v context=%v", err, task.ctx.Err())
	}
	job, err := store.GetJob(ctx, task.job.ID)
	if err != nil || !job.CancelRequested || job.Scanned != before.Scanned || job.Added != before.Added || job.Updated != before.Updated {
		t.Fatalf("unchanged elapsed checkpoint changed the accepted prefix: job=%+v error=%v", job, err)
	}
	taskScanAssertChild(t, ctx, pool, task.job.TaskChildID, job)
}

func TestScanProgressBatchDeletedTerminalRetainsExactSavedPrefix(t *testing.T) {
	for _, operation := range []string{"checkpoint", "completion", "finish", "wrong_retained_prefix"} {
		t.Run(operation, func(t *testing.T) {
			ctx, pool, store, task, _ := scanUnchangedProgressFixture(t)
			if _, err := pool.Exec(ctx, `WITH reset AS (
				UPDATE scan_jobs SET scanned=0,added=0,updated=0 WHERE id=$1 RETURNING id)
				UPDATE task_run_children SET scanned=0,added=0,updated=0
				WHERE id=$2 AND scan_job_id IN (SELECT id FROM reset)`, task.job.ID, task.job.TaskChildID); err != nil {
				t.Fatal(err)
			}
			task.job.Scanned, task.job.Added, task.job.Updated = 0, 0, 0
			task.recordSavedProgress(task.job, time.Now().Add(-scanProgressCheckpointInterval))
			task.job.Scanned = 3
			finished, err := scanJob(pool.QueryRow(ctx, `UPDATE scan_jobs SET status='Completed',finished_at=clock_timestamp()
				WHERE id=$1 RETURNING `+jobColumns, task.job.ID))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE task_run_children SET state='completed',finished_at=$2 WHERE id=$1`, task.job.TaskChildID, finished.FinishedAt); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "DELETE FROM scan_jobs WHERE id=$1", task.job.ID); err != nil {
				t.Fatal(err)
			}
			if operation == "wrong_retained_prefix" {
				if _, err := pool.Exec(ctx, "UPDATE task_run_children SET scanned=1 WHERE id=$1", task.job.TaskChildID); err != nil {
					t.Fatal(err)
				}
			}
			before := taskRefreshRowSnapshot(t, ctx, pool, "task_run_children", task.job.TaskChildID)
			want := error(context.Canceled)
			switch operation {
			case "completion":
				err = store.checkCachedTaskScanProgress(task)
			case "finish":
				want = nil
				err = store.finishTask(task, "Failed", "worker observed retained terminal history")
			default:
				if operation == "wrong_retained_prefix" {
					want = ErrUnavailable
				}
				err = store.maybePersistProgress(task)
			}
			if !errors.Is(err, want) {
				t.Fatalf("deleted terminal history used pending counters as committed facts: error=%v want=%v", err, want)
			}
			if task.progress.scanned != 0 || taskRefreshRowSnapshot(t, ctx, pool, "task_run_children", task.job.TaskChildID) != before {
				t.Fatal("retained terminal history or its saved prefix was overwritten")
			}
		})
	}
}

func TestScanProgressBatchTerminalFlushesPendingPrefix(t *testing.T) {
	for _, status := range []string{"Completed", "Cancelled", "Failed"} {
		t.Run(status, func(t *testing.T) {
			ctx, pool, store, task, _ := scanUnchangedProgressFixture(t)
			scanProgressBatchHoldWindow(task)
			task.job.Scanned += scanProgressCheckpointItems - 1
			want := task.job
			if status == "Cancelled" {
				task.cancel()
			}
			message := ""
			if status == "Failed" {
				message = "batch finalization failure"
			}
			if err := store.finishTask(task, status, message); err != nil {
				t.Fatal(err)
			}
			job, err := store.GetJob(ctx, task.job.ID)
			if err != nil || job.Status != status || job.FinishedAt == nil || job.Scanned != task.job.Scanned ||
				job.Scanned != want.Scanned || job.Added != want.Added || job.Updated != want.Updated {
				t.Fatalf("terminal snapshot lost its pending and accepted prefix: job=%+v error=%v", job, err)
			}
			taskScanAssertChild(t, ctx, pool, task.job.TaskChildID, job)
		})
	}
}

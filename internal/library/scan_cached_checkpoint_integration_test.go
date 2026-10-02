//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCachedScanProgressRetainsSnapshotAndRepairsChangedCounters(t *testing.T) {
	ctx, pool, store, task, _ := scanUnchangedProgressFixture(t)
	before := scanUnchangedProgressSnapshot(t, ctx, pool, task)
	if err := store.checkCachedTaskScanProgress(task); err != nil || scanUnchangedProgressSnapshot(t, ctx, pool, task) != before {
		t.Fatalf("combined unchanged check rewrote progress: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE task_run_children SET scanned=scanned+1 WHERE id=$1`, task.job.TaskChildID); err != nil {
		t.Fatal(err)
	}
	if err := store.checkCachedTaskScanProgress(task); err != nil {
		t.Fatalf("changed child snapshot did not use the ordinary checkpoint: %v", err)
	}
	job, err := store.GetJob(ctx, task.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	taskScanAssertChild(t, ctx, pool, task.job.TaskChildID, job)
	task.job.Scanned++
	task.job.Updated++
	if err := store.checkCachedTaskScanProgress(task); err != nil {
		t.Fatal(err)
	}
	job, err = store.GetJob(ctx, task.job.ID)
	if err != nil || job.Scanned != task.job.Scanned || job.Updated != task.job.Updated {
		t.Fatalf("combined check lost changed local counters: job=%+v error=%v", job, err)
	}
	taskScanAssertChild(t, ctx, pool, task.job.TaskChildID, job)
}

func TestCachedScanProgressRejectsChangedAuthority(t *testing.T) {
	for _, scenario := range []struct {
		name, table, statement string
		want                   error
	}{
		{"parent_stop", "task_runs", `UPDATE task_runs SET state='stopping',stop_requested_at=clock_timestamp(),stop_reason='administrator' WHERE id=$1`, context.Canceled},
		{"unknown_mode", "task_runs", `UPDATE task_runs SET task_key='unsupported.scan' WHERE id=$1`, ErrUnavailable},
		{"cancel_flag", "scan_jobs", `UPDATE scan_jobs SET cancel_requested=true WHERE id=$1`, context.Canceled},
		{"changed_job_link", "scan_jobs", `UPDATE scan_jobs SET task_child_id=NULL WHERE id=$1`, ErrUnavailable},
		{"changed_child_link", "task_run_children", `UPDATE task_run_children SET scan_job_id=repeat('1',32) WHERE id=$1`, ErrUnavailable},
		{"conflicting_terminal_child", "task_run_children", `UPDATE task_run_children SET state='completed',finished_at=clock_timestamp() WHERE id=$1`, ErrUnavailable},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, pool, store, task, runID := scanUnchangedProgressFixture(t)
			id := task.job.ID
			if scenario.table == "task_runs" {
				id = runID
			} else if scenario.table == "task_run_children" {
				id = task.job.TaskChildID
			}
			if _, err := pool.Exec(ctx, scenario.statement, id); err != nil {
				t.Fatal(err)
			}
			before := scanUnchangedProgressSnapshot(t, ctx, pool, task)
			if err := store.checkCachedTaskScanProgress(task); !errors.Is(err, scenario.want) {
				t.Fatalf("combined check accepted changed authority: got=%v want=%v", err, scenario.want)
			}
			if errors.Is(scenario.want, context.Canceled) {
				job, err := store.GetJob(ctx, task.job.ID)
				if err != nil || !job.CancelRequested || !errors.Is(task.ctx.Err(), context.Canceled) {
					t.Fatalf("combined cancellation was not retained: job=%+v error=%v context=%v", job, err, task.ctx.Err())
				}
				taskScanAssertChild(t, ctx, pool, task.job.TaskChildID, job)
			} else if scanUnchangedProgressSnapshot(t, ctx, pool, task) != before {
				t.Fatal("rejected combined check rewrote retained progress")
			}
		})
	}
}

func TestCachedScanProgressRetainsTerminalAndDeletedSnapshots(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		name := "terminal"
		if deleted {
			name = "deleted"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, store, task, _ := scanUnchangedProgressFixture(t)
			finished, err := scanJob(pool.QueryRow(ctx, `UPDATE scan_jobs SET status='Completed',finished_at=clock_timestamp()
				WHERE id=$1 RETURNING `+jobColumns, task.job.ID))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE task_run_children SET state='completed',finished_at=$2 WHERE id=$1`, task.job.TaskChildID, finished.FinishedAt); err != nil {
				t.Fatal(err)
			}
			if deleted {
				if _, err := pool.Exec(ctx, `DELETE FROM scan_jobs WHERE id=$1`, task.job.ID); err != nil {
					t.Fatal(err)
				}
			}
			before := taskRefreshRowSnapshot(t, ctx, pool, "task_run_children", task.job.TaskChildID)
			if err := store.checkCachedTaskScanProgress(task); !errors.Is(err, context.Canceled) {
				t.Fatalf("combined check accepted completed or deleted work: %v", err)
			}
			if taskRefreshRowSnapshot(t, ctx, pool, "task_run_children", task.job.TaskChildID) != before {
				t.Fatal("combined terminal check rewrote retained child history")
			}
		})
	}
}

func TestCachedScanProgressRechecksTuplesAfterSharedLockWait(t *testing.T) {
	for _, scenario := range []struct {
		name, table, statement string
		want                   error
	}{
		{"run_stop", "task_runs", `UPDATE task_runs SET state='stopping',stop_requested_at=clock_timestamp(),stop_reason='administrator' WHERE id=$1`, context.Canceled},
		{"child_relink", "task_run_children", `UPDATE task_run_children SET scan_job_id=repeat('2',32) WHERE id=$1`, ErrUnavailable},
		{"job_relink", "scan_jobs", `UPDATE scan_jobs SET task_child_id=NULL WHERE id=$1`, ErrUnavailable},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, pool, store, task, runID := scanUnchangedProgressFixture(t)
			id := task.job.ID
			if scenario.table == "task_runs" {
				id = runID
			} else if scenario.table == "task_run_children" {
				id = task.job.TaskChildID
			}
			gate, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(gate)
			var lockedID string
			if err := gate.QueryRow(ctx, `SELECT id FROM `+scenario.table+` WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID); err != nil {
				t.Fatal(err)
			}
			ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
			gatePID := int32(gate.Conn().PgConn().PID())
			finished := make(chan error, 1)
			go func() { finished <- store.checkCachedTaskScanProgress(task) }()
			ownedTransactionsWaitForBlock(t, ctx, pool, ownerPID, gatePID, finished)
			if _, err := gate.Exec(ctx, scenario.statement, id); err != nil {
				t.Fatal(err)
			}
			if err := gate.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				if !errors.Is(err, scenario.want) {
					t.Fatalf("combined check accepted a stale locked tuple: got=%v want=%v", err, scenario.want)
				}
			case <-ctx.Done():
				t.Fatal("combined check did not finish after the authority gate released")
			}
			ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
		})
	}
}

func TestCachedScanProgressCancellationDuringWaitKeepsOwnershipSession(t *testing.T) {
	ctx, pool, store, task, runID := scanUnchangedProgressFixture(t)
	gate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(gate)
	var id string
	if err := gate.QueryRow(ctx, `SELECT id FROM task_runs WHERE id=$1 FOR UPDATE`, runID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
	finished := make(chan error, 1)
	go func() { finished <- store.checkCachedTaskScanProgress(task) }()
	ownedTransactionsWaitForBlock(t, ctx, pool, ownerPID, int32(gate.Conn().PgConn().PID()), finished)
	task.cancel()
	if err := gate.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("combined check ignored cancelled caller after its shared wait: %v", err)
	}
	ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
}

func TestCachedScanProgressRejectsLostOwnershipSession(t *testing.T) {
	ctx, pool, store, task, _ := scanUnchangedProgressFixture(t, ErrUnavailable)
	ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
	before := scanUnchangedProgressSnapshot(t, ctx, pool, task)
	ownedTransactionsTerminateBackend(t, ctx, pool, ownerPID)
	if err := store.checkCachedTaskScanProgress(task); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("combined check accepted the terminated owner: %v", err)
	}
	if store.Available() || scanUnchangedProgressSnapshot(t, ctx, pool, task) != before {
		t.Fatal("lost combined check remained available or rewrote progress")
	}
}

func scanCachedVisitFixture(t *testing.T) (context.Context, *pgxpool.Pool, *Store, *scanState, *scanPerformanceSQLTracer, string) {
	t.Helper()
	ctx, pool, previous, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	if err := previous.Close(ctx); err != nil {
		t.Fatal(err)
	}
	trace := &scanPerformanceSQLTracer{}
	config := pool.Config()
	config.ConnConfig.Tracer = trace
	tracedPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, tracedPool)
	store, err := New(tracedPool, &libraryFixtureProber{}, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanupCtx); err != nil {
			t.Errorf("close cached visit owner: %v", err)
		}
	})
	path := libraryIntegrationFile(t, root, "combined-visit/Film.mp4", "video:combined-visit")
	library := libraryIntegrationCreate(t, ctx, store, "Combined visit", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	task, _ := scanUnchangedProgressTask(t, ctx, pool, library)
	state := scanClaimLookupState(t, ctx, store, library, filepath.Dir(path))
	state.task, state.themes = task, nil
	info, err := state.opened.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	state.directoryIdentities = map[string]os.FileInfo{".": info}
	return ctx, pool, store, state, trace, root
}

func TestCachedScanImageAbsenceCombinesOneCompletionCheckpoint(t *testing.T) {
	for _, scenario := range []string{"stable_absence", "disabled_images", "valid_candidate", "invalid_candidate", "missing_directory_proof"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, pool, store, state, trace, root := scanCachedVisitFixture(t)
			if scenario == "disabled_images" {
				options := DefaultLibraryOptions()
				options.EnableLocalImages = false
				state.library.Options = &options
			} else if scenario == "valid_candidate" || scenario == "invalid_candidate" {
				if scenario == "valid_candidate" {
					if err := os.WriteFile(filepath.Join(root, "combined-visit", "poster.png"), imageStoreTestPNG(t), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					libraryIntegrationFile(t, root, "combined-visit/poster.png", "invalid-image")
				}
				info, err := state.opened.Stat(".")
				if err != nil {
					t.Fatal(err)
				}
				state.directoryIdentities["."] = info
			} else if scenario == "missing_directory_proof" {
				state.directoryIdentities = nil
			}
			trace.reset()
			if err := state.scanFile("Film.mp4", "video", hierarchy{parentID: state.library.ID}); err != nil {
				t.Fatal(err)
			}
			wantBegins, wantCombined := int64(2), int64(0)
			if scenario == "stable_absence" {
				wantBegins, wantCombined = 1, 1
			} else if scenario == "valid_candidate" || scenario == "invalid_candidate" {
				wantBegins = 3
			}
			if trace.begins.Load() != wantBegins || trace.commits.Load() != wantBegins || trace.cachedCompletionChecks.Load() != wantCombined {
				t.Fatalf("cached visit checkpoint boundaries differ: begin=%d commit=%d combined=%d want_begin=%d want_combined=%d",
					trace.begins.Load(), trace.commits.Load(), trace.cachedCompletionChecks.Load(), wantBegins, wantCombined)
			}
			if scenario == "invalid_candidate" || scenario == "missing_directory_proof" {
				if state.warnings == 0 {
					t.Fatal("incomplete image proof did not retain its warning/fallback")
				}
			}
			job, err := store.GetJob(ctx, state.task.job.ID)
			if err != nil || job.Scanned != state.task.job.Scanned || job.Added != state.task.job.Added || job.Updated != state.task.job.Updated {
				t.Fatalf("cached visit lost persisted counters: job=%+v error=%v", job, err)
			}
			taskScanAssertChild(t, ctx, pool, state.task.job.TaskChildID, job)
		})
	}
}

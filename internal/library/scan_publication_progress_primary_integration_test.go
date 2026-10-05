//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func scanPrimaryPublicationFixture(t *testing.T, update bool) (context.Context, *pgxpool.Pool, *Store, *scanState, *scannedMediaInput, string, string) {
	t.Helper()
	ctx, pool, store, state, _, root := scanCachedVisitFixture(t)
	store.mu.Lock()
	store.active[state.task.job.ID] = state.task
	store.mu.Unlock()
	t.Cleanup(func() {
		state.task.cancel()
		store.mu.Lock()
		delete(store.active, state.task.job.ID)
		store.mu.Unlock()
	})
	options := DefaultLibraryOptions()
	options.EnableLocalImages = false
	state.library.Options = &options
	name := "Added.mp4"
	if update {
		name = "Film.mp4"
		libraryIntegrationFile(t, root, "combined-visit/Film.nfo", "<movie><title>Published primary update</title></movie>")
	} else {
		libraryIntegrationFile(t, root, "combined-visit/"+name, "video:primary-publication")
	}
	info, err := state.opened.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	state.directoryIdentities = map[string]os.FileInfo{".": info}
	input, err := state.prepareScannedMedia(name, "video", scannedRoleOrdinary)
	if err != nil || input == nil {
		t.Fatalf("prepare publication input: input=%v error=%v", input, err)
	}
	t.Cleanup(func() {
		if err := input.close(); err != nil {
			t.Errorf("close publication input: %v", err)
		}
	})
	if input.probe == nil {
		probe, err := input.primary.probe(store.prober, input.file)
		if err != nil {
			t.Fatal(err)
		}
		input.probe = &probe
	}
	if accepted, err := state.acceptScannedMedia(input, "video", nil); err != nil || !accepted {
		t.Fatalf("accept publication input: accepted=%v error=%v", accepted, err)
	}
	var runID string
	if err := pool.QueryRow(ctx, "SELECT run_id FROM task_run_children WHERE id=$1", state.task.job.TaskChildID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, store, state, input, name, runID
}

func scanPrimaryAdvanceDurableProgress(t *testing.T, ctx context.Context, pool *pgxpool.Pool, task *scanTask) Job {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err := lockTaskScanRelation(scanProbeAuthorityTx{ctx: ctx, tx: tx}, task.job.ID, task.job.TaskChildID); err != nil {
		t.Fatal(err)
	}
	job, err := scanJob(tx.QueryRow(ctx, `UPDATE scan_jobs SET scanned=scanned+4,added=added+2,updated=updated+2
		WHERE id=$1 RETURNING `+jobColumns, task.job.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE task_run_children SET scanned=$2,added=$3,updated=$4 WHERE id=$1",
		job.TaskChildID, job.Scanned, job.Added, job.Updated); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return job
}

func TestScanPrimaryPublicationCommitsTaskCountersWithCatalog(t *testing.T) {
	for _, update := range []bool{false, true} {
		name := "added"
		if update {
			name = "updated"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, store, state, input, path, _ := scanPrimaryPublicationFixture(t, update)
			before := state.task.job
			beforeCheckpoint := state.task.progress
			wantAdded, wantUpdated := before.Added, before.Updated
			if update {
				wantUpdated++
			} else {
				wantAdded++
			}
			observed := false
			var observationErr error
			store.SetCatalogChangeListener(func(notification CatalogNotification) {
				for _, change := range notification.Changes {
					if change.IsFolder {
						continue
					}
					var relative string
					if err := pool.QueryRow(ctx, "SELECT relative_path FROM items WHERE id=$1", change.ItemID).Scan(&relative); err != nil {
						observationErr = err
						return
					}
					if relative != filepath.ToSlash(path) {
						continue
					}
					observed = true
					job, err := store.GetJob(ctx, state.task.job.ID)
					if err != nil || job.Scanned != before.Scanned || job.Added != wantAdded || job.Updated != wantUpdated {
						observationErr = fmt.Errorf("catalog commit exposed different task counters: job=%+v error=%v", job, err)
						return
					}
					var matching bool
					if err := pool.QueryRow(ctx, `SELECT c.scanned=j.scanned AND c.added=j.added AND c.updated=j.updated
						AND c.state=lower(j.status) AND c.started_at IS NOT DISTINCT FROM j.started_at
						FROM task_run_children c JOIN scan_jobs j ON j.id=c.scan_job_id
						WHERE c.id=$1 AND j.id=$2`, before.TaskChildID, before.ID).Scan(&matching); err != nil || !matching {
						observationErr = fmt.Errorf("catalog commit exposed a different child snapshot: matching=%v error=%v", matching, err)
						return
					}
					// Commit notifies before the scanner publishes its returned job.
					if state.task.job.Added != before.Added || state.task.job.Updated != before.Updated {
						observationErr = errors.New("publication changed in-memory accepted counters before Commit returned")
					}
					if state.task.progress != beforeCheckpoint {
						observationErr = errors.New("publication advanced its checkpoint watermark before Commit returned")
					}
				}
			})
			t.Cleanup(func() { store.SetCatalogChangeListener(nil) })
			if err := state.publishScannedMedia(path, "video", hierarchy{parentID: state.library.ID}, input); err != nil {
				t.Fatal(err)
			}
			if !observed || observationErr != nil {
				t.Fatalf("atomic catalog/progress observation failed: observed=%v error=%v", observed, observationErr)
			}
			if state.task.job.Added != wantAdded || state.task.job.Updated != wantUpdated {
				t.Fatalf("committed counters were not published to the worker: %+v", state.task.job)
			}
			if state.task.progress.scanned != state.task.job.Scanned || state.task.progress.added != wantAdded ||
				state.task.progress.updated != wantUpdated || state.task.progress.savedAt == beforeCheckpoint.savedAt {
				t.Fatalf("committed publication did not become the saved progress watermark: %+v", state.task.progress)
			}
			job, err := store.GetJob(ctx, before.ID)
			if err != nil {
				t.Fatal(err)
			}
			taskScanAssertChild(t, ctx, pool, before.TaskChildID, job)
		})
	}
}

func TestScanPrimaryPublicationRollsBackCatalogAndAcceptedCounters(t *testing.T) {
	for _, table := range []string{"scan_jobs", "task_run_children"} {
		t.Run(table, func(t *testing.T) {
			ctx, pool, store, state, input, path, _ := scanPrimaryPublicationFixture(t, false)
			before := state.task.job
			beforeCheckpoint := state.task.progress
			snapshot := scanUnchangedProgressSnapshot(t, ctx, pool, state.task)
			if _, err := pool.Exec(ctx, `CREATE FUNCTION fail_primary_publication_progress() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN
					IF NEW.added > OLD.added OR NEW.updated > OLD.updated THEN
						RAISE EXCEPTION 'injected publication progress failure';
					END IF;
					RETURN NEW;
				END; $$;`); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `CREATE TRIGGER fail_primary_publication_progress BEFORE UPDATE ON `+table+
				` FOR EACH ROW EXECUTE FUNCTION fail_primary_publication_progress()`); err != nil {
				t.Fatal(err)
			}
			notifications := 0
			store.SetCatalogChangeListener(func(CatalogNotification) { notifications++ })
			t.Cleanup(func() { store.SetCatalogChangeListener(nil) })
			if err := state.publishScannedMedia(path, "video", hierarchy{parentID: state.library.ID}, input); err == nil {
				t.Fatal("injected progress failure committed the primary publication")
			}
			if state.task.job.Added != before.Added || state.task.job.Updated != before.Updated || notifications != 0 {
				t.Fatalf("rolled-back publication escaped to memory or listeners: job=%+v notifications=%d", state.task.job, notifications)
			}
			if state.task.progress != beforeCheckpoint {
				t.Fatal("rolled-back publication advanced the saved progress watermark")
			}
			if scanUnchangedProgressSnapshot(t, ctx, pool, state.task) != snapshot {
				t.Fatal("rolled-back publication changed the accepted job or child snapshot")
			}
			var count int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM items WHERE library_id=$1 AND relative_path=$2", state.library.ID, filepath.ToSlash(path)).Scan(&count); err != nil || count != 0 {
				t.Fatalf("rolled-back publication left a catalog row: count=%d error=%v", count, err)
			}
		})
	}
}

func TestScanPrimaryPublicationRejectsChangedTaskAuthority(t *testing.T) {
	for _, scenario := range []string{"parent_stop", "cancel_flag", "changed_child_link", "unknown_mode", "terminal", "deleted_terminal", "cancelled_context", "durable_counter_ahead"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, pool, _, state, input, path, runID := scanPrimaryPublicationFixture(t, false)
			before := state.task.job
			want := error(context.Canceled)
			var err error
			switch scenario {
			case "parent_stop":
				_, err = pool.Exec(ctx, `UPDATE task_runs SET state='stopping',stop_requested_at=clock_timestamp(),stop_reason='administrator' WHERE id=$1`, runID)
			case "cancel_flag":
				_, err = pool.Exec(ctx, "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", before.ID)
			case "changed_child_link":
				_, err = pool.Exec(ctx, "UPDATE task_run_children SET scan_job_id=repeat('1',32) WHERE id=$1", before.TaskChildID)
				want = ErrUnavailable
			case "unknown_mode":
				_, err = pool.Exec(ctx, "UPDATE task_runs SET task_key='unsupported.scan' WHERE id=$1", runID)
				want = ErrUnavailable
			case "terminal", "deleted_terminal":
				var finished Job
				finished, err = scanJob(pool.QueryRow(ctx, `UPDATE scan_jobs SET status='Completed',finished_at=clock_timestamp()
					WHERE id=$1 RETURNING `+jobColumns, before.ID))
				if err == nil {
					_, err = pool.Exec(ctx, "UPDATE task_run_children SET state='completed',finished_at=$2 WHERE id=$1", before.TaskChildID, finished.FinishedAt)
				}
				if err == nil && scenario == "deleted_terminal" {
					_, err = pool.Exec(ctx, "DELETE FROM scan_jobs WHERE id=$1", before.ID)
				}
			case "cancelled_context":
				state.task.cancel()
			case "durable_counter_ahead":
				scanPrimaryAdvanceDurableProgress(t, ctx, pool, state.task)
				want = ErrUnavailable
			}
			if err != nil {
				t.Fatal(err)
			}
			childBefore := taskRefreshRowSnapshot(t, ctx, pool, "task_run_children", before.TaskChildID)
			jobBefore := ""
			if scenario == "durable_counter_ahead" {
				jobBefore = taskRefreshRowSnapshot(t, ctx, pool, "scan_jobs", before.ID)
			}
			if err := state.publishScannedMedia(path, "video", hierarchy{parentID: state.library.ID}, input); !errors.Is(err, want) {
				t.Fatalf("changed task authority was accepted: got=%v want=%v", err, want)
			}
			if state.task.job.Added != before.Added || state.task.job.Updated != before.Updated ||
				taskRefreshRowSnapshot(t, ctx, pool, "task_run_children", before.TaskChildID) != childBefore {
				t.Fatal("rejected task authority changed accepted counters or retained child history")
			}
			if jobBefore != "" && taskRefreshRowSnapshot(t, ctx, pool, "scan_jobs", before.ID) != jobBefore {
				t.Fatal("rejected stale publication rewrote the durable scan checkpoint")
			}
			var count int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM items WHERE library_id=$1 AND relative_path=$2", state.library.ID, filepath.ToSlash(path)).Scan(&count); err != nil || count != 0 {
				t.Fatalf("rejected task authority published a primary row: count=%d error=%v", count, err)
			}
		})
	}
}

func TestScanPrimaryPublicationRetainsDurableCountersAfterRejection(t *testing.T) {
	for _, recovery := range []string{"finish", "progress"} {
		t.Run(recovery, func(t *testing.T) {
			ctx, pool, store, state, input, path, _ := scanPrimaryPublicationFixture(t, false)
			before := state.task.job
			durable := scanPrimaryAdvanceDurableProgress(t, ctx, pool, state.task)
			snapshot := scanUnchangedProgressSnapshot(t, ctx, pool, state.task)
			if err := state.publishScannedMedia(path, "video", hierarchy{parentID: state.library.ID}, input); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("publication accepted stale worker counters: %v", err)
			}
			if scanUnchangedProgressSnapshot(t, ctx, pool, state.task) != snapshot ||
				state.task.job.Scanned != before.Scanned || state.task.job.Added != before.Added || state.task.job.Updated != before.Updated {
				t.Fatal("rejected publication changed the durable prefix or old worker counters")
			}
			if recovery == "finish" {
				if err := store.finishTask(state.task, "Failed", "Rejected stale publication"); err != nil {
					t.Fatal(err)
				}
			} else if err := store.persistProgress(state.task); err != nil {
				t.Fatal(err)
			}
			job, err := store.GetJob(ctx, before.ID)
			if err != nil || job.Scanned != durable.Scanned || job.Added != durable.Added || job.Updated != durable.Updated ||
				state.task.job.Scanned != durable.Scanned || state.task.job.Added != durable.Added || state.task.job.Updated != durable.Updated {
				t.Fatalf("recovery regressed the accepted durable prefix: job=%+v memory=%+v error=%v", job, state.task.job, err)
			}
			if recovery == "finish" {
				if job.Status != "Failed" || job.Error != "Rejected stale publication" || job.FinishedAt == nil {
					t.Fatalf("failed recovery lost its terminal snapshot: %+v", job)
				}
			} else if job.Status != "Running" || job.FinishedAt != nil {
				t.Fatalf("progress repair changed the running job state: %+v", job)
			}
			taskScanAssertChild(t, ctx, pool, before.TaskChildID, job)
			var count int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM items WHERE library_id=$1 AND relative_path=$2", state.library.ID, filepath.ToSlash(path)).Scan(&count); err != nil || count != 0 {
				t.Fatalf("recovery published the rejected primary: count=%d error=%v", count, err)
			}
		})
	}
}

func TestScanPrimaryPublicationChecksDatabaseStopAtNextCheckpoint(t *testing.T) {
	ctx, pool, store, state, input, path, runID := scanPrimaryPublicationFixture(t, false)
	before := state.task.job
	var stopErr error
	store.SetCatalogChangeListener(func(notification CatalogNotification) {
		for _, change := range notification.Changes {
			if !change.IsFolder {
				_, stopErr = pool.Exec(ctx, `UPDATE task_runs SET state='stopping',stop_requested_at=clock_timestamp(),stop_reason='administrator' WHERE id=$1`, runID)
				return
			}
		}
	})
	t.Cleanup(func() { store.SetCatalogChangeListener(nil) })
	if err := state.publishScannedMedia(path, "video", hierarchy{parentID: state.library.ID}, input); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("post-publication stop returned an unrelated failure: %v", err)
	}
	if stopErr != nil {
		t.Fatal(stopErr)
	}
	// The committed item already supplies this batch's watermark. A database-
	// only stop must be observed by the next due checkpoint without discarding it.
	state.task.progress.savedAt = time.Time{}
	if err := store.maybePersistProgress(state.task); !errors.Is(err, context.Canceled) {
		t.Fatalf("due checkpoint ignored the committed item's stopped parent: %v", err)
	}
	job, err := store.GetJob(ctx, before.ID)
	if err != nil || job.Added != before.Added+1 || job.Updated != before.Updated || !job.CancelRequested ||
		state.task.job.Added != job.Added || !errors.Is(state.task.ctx.Err(), context.Canceled) {
		t.Fatalf("stopped parent lost the already accepted primary prefix: job=%+v memory=%+v error=%v", job, state.task.job, err)
	}
	taskScanAssertChild(t, ctx, pool, before.TaskChildID, job)
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM items WHERE library_id=$1 AND relative_path=$2", state.library.ID, filepath.ToSlash(path)).Scan(&count); err != nil || count != 1 {
		t.Fatalf("post-commit cancellation removed the accepted primary: count=%d error=%v", count, err)
	}
}

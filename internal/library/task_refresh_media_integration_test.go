package library

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTaskScanModeComesFromTheParentRunSnapshot(t *testing.T) {
	for _, test := range []struct {
		name, key, currentKey string
		force                 bool
	}{
		{"ordinary", TaskLibraryScanKey, TaskLibraryRefreshMediaKey, false},
		{"refresh", TaskLibraryRefreshMediaKey, TaskLibraryScanKey, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			prober := &libraryFixtureProber{}
			ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
			collection := taskScanCreateLibrary(t, ctx, store, root, "snapshot-mode")
			runID, children := taskScanFixtureForKey(t, ctx, pool, test.key, collection)
			// A later definition must not reinterpret a previously admitted run.
			if _, err := pool.Exec(ctx, `UPDATE task_definitions SET key=$2
				WHERE id=(SELECT task_id FROM task_runs WHERE id=$1)`, runID, test.currentKey); err != nil {
				t.Fatal(err)
			}
			before := taskRefreshRowSnapshot(t, ctx, pool, "task_runs", runID)
			admission, err := store.AdmitTaskScan(ctx, children[0])
			if err != nil || admission.Kind != ScanAdmitted || admission.Job.ForceProbe != test.force ||
				admission.Job.TaskChildID != children[0] {
				t.Fatalf("parent snapshot did not select the scan mode: admission=%+v error=%v", admission, err)
			}
			job := libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Completed")
			if job.Error != "" || job.Scanned != 1 || job.Added != 1 || job.ForceProbe != test.force || len(prober.calls()) != 1 {
				t.Fatalf("owned scan did not preserve its admitted mode and real execution: %+v", job)
			}
			taskScanAssertChild(t, ctx, pool, children[0], job)
			if taskRefreshRowSnapshot(t, ctx, pool, "task_runs", runID) != before {
				t.Fatal("the scanner rewrote its coordinator-owned parent snapshot")
			}
		})
	}
}

func TestTaskScanRejectsUnknownParentKeyWithoutCreatingWork(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	collection := taskScanCreateLibrary(t, ctx, store, root, "unknown-parent")
	runID, children := taskScanFixtureForKey(t, ctx, pool, "library.unknown_executor", collection)
	beforeRun := taskRefreshRowSnapshot(t, ctx, pool, "task_runs", runID)
	beforeChild := taskRefreshRowSnapshot(t, ctx, pool, "task_run_children", children[0])
	admission, err := store.AdmitTaskScan(ctx, children[0])
	if !errors.Is(err, ErrUnavailable) || admission.Kind != "" || admission.Job.ID != "" {
		t.Fatalf("unknown parent key obtained scan admission: admission=%+v error=%v", admission, err)
	}
	if err := store.CancelTaskScan(ctx, children[0]); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unknown parent key obtained cancellation authority: %v", err)
	}
	if taskRefreshRowSnapshot(t, ctx, pool, "task_runs", runID) != beforeRun ||
		taskRefreshRowSnapshot(t, ctx, pool, "task_run_children", children[0]) != beforeChild {
		t.Fatal("rejecting an unknown executor modified the parent or waiting child")
	}
	taskScanExpectCount(t, ctx, pool, "SELECT count(*) FROM scan_jobs", 0)
	if len(prober.calls()) != 0 {
		t.Fatal("an unknown executor reached a media probe")
	}
}

func TestTaskScanRejectsLinkedModeMismatchWithoutCancellingOrRecoveringIt(t *testing.T) {
	for _, test := range []struct {
		name, key string
		force     bool
	}{
		{"ordinary-with-forced-job", TaskLibraryScanKey, true},
		{"refresh-with-normal-job", TaskLibraryRefreshMediaKey, false},
		{"unknown-parent-key", "library.unknown_executor", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			prober := &libraryFixtureProber{}
			ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
			collection := taskScanCreateLibrary(t, ctx, store, root, "linked-mode")
			runID, children := taskScanFixtureForKey(t, ctx, pool, test.key, collection)
			jobID, err := randomID()
			if err != nil {
				t.Fatal(err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if _, err := tx.Exec(ctx, `INSERT INTO scan_jobs
				(id,library_id,status,task_child_id,force_probe) VALUES ($1,$2,'Queued',$3,$4)`,
				jobID, collection.ID, children[0], test.force); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `UPDATE task_run_children SET state='queued',scan_job_id=$2 WHERE id=$1`, children[0], jobID); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			ids := map[string]string{"task_runs": runID, "task_run_children": children[0], "scan_jobs": jobID}
			before := make(map[string]string, len(ids))
			for table, id := range ids {
				before[table] = taskRefreshRowSnapshot(t, ctx, pool, table, id)
			}
			assertPreserved := func() {
				t.Helper()
				for table, id := range ids {
					if taskRefreshRowSnapshot(t, ctx, pool, table, id) != before[table] {
						t.Fatalf("rejected scan mode changed %s, including its row version", table)
					}
				}
				taskScanExpectCount(t, ctx, pool, "SELECT count(*) FROM scan_jobs", 1)
			}
			admission, err := store.AdmitTaskScan(ctx, children[0])
			if !errors.Is(err, ErrUnavailable) || admission.Kind != "" || admission.Job.ID != "" {
				t.Fatalf("inconsistent mode was admitted or adopted: admission=%+v error=%v", admission, err)
			}
			assertPreserved()
			if err := store.CancelTaskScan(ctx, children[0]); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("inconsistent mode obtained scan cancellation authority: %v", err)
			}
			assertPreserved()
			if err := store.recoverTaskScans(ctx); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("recovery rewrote an inconsistent linked mode: %v", err)
			}
			assertPreserved()
			if len(prober.calls()) != 0 {
				t.Fatal("rejecting inconsistent durable history started a media probe")
			}
		})
	}
}

func taskRefreshRowSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, id string) string {
	t.Helper()
	var value string
	if err := pool.QueryRow(ctx, `SELECT (to_jsonb(snapshot_row) || jsonb_build_object('row_version',snapshot_row.xmin::text))::text
		FROM `+pgx.Identifier{table}.Sanitize()+` AS snapshot_row WHERE id=$1`, id).Scan(&value); err != nil {
		t.Fatalf("read immutable task fixture snapshot: %v", err)
	}
	return value
}

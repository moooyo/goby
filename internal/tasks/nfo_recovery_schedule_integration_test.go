package tasks

import (
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

func TestNFORecoverySchedulePreservesOverlappingIngestEventsAndSkipsWorkNotDue(t *testing.T) {
	ctx, pool, owner, _, actor, _ := taskRepository(t, 2)
	registry, err := NewExecutorRegistry(
		ExecutorRegistration{Key: library.TaskNFORecoveryKey, Name: "Regenerate local NFO sidecars", Executor: libraryBatchTestExecutor{}},
		ExecutorRegistration{Key: library.TaskLocalMetadataIngestKey, Name: "Import local metadata", Executor: libraryBatchTestExecutor{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, owner, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	recovery, err := store.GetByKey(ctx, library.TaskNFORecoveryKey)
	if err != nil || len(recovery.Triggers) != 3 || recovery.EmbyKey != "" {
		t.Fatalf("recovery did not install its native schedule without a writable root: %+v, %v", recovery, err)
	}
	local, err := store.GetByKey(ctx, library.TaskLocalMetadataIngestKey)
	if err != nil || len(local.Triggers) != 3 {
		t.Fatalf("local import schedule missing: %+v, %v", local, err)
	}
	// This fixture exercises scheduler admission, without opening media or
	// executing a worker. Only library-1 has an accepted source binding.
	if _, err := pool.Exec(ctx, `INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES ('fixture-root','library-1','/fixture/movies','/fixture','movies');
		INSERT INTO items(id,library_id,root_id,name,sort_name,type,path,relative_path,media)
		VALUES ('fixture-movie','library-1','fixture-root','Movie','Movie','Movie','/fixture/movies/movie.mkv','movie.mkv','{}');
		INSERT INTO scan_jobs(id,library_id,status,started_at,finished_at)
		VALUES ('fixture-scan','library-1','Completed',clock_timestamp(),clock_timestamp());
		WITH queued AS (
			INSERT INTO local_metadata_ingest
			(item_id,scan_job_id,directory_identity,source_key,index_number,parent_index_number,parent_number_defined,import_nfo,import_images,enrich_online)
			SELECT i.id,'fixture-scan','directory',jsonb_build_array(i.root_id,i.relative_path,i.path,i.type,i.is_folder,i.parent_id,
				i.file_identity,i.file_size,extract(epoch FROM i.modified_at),i.media_operation_source_revision,r.binding_revision)::text,
				0,0,false,true,false,false FROM items i JOIN library_roots r ON r.id=i.root_id WHERE i.id='fixture-movie'
			RETURNING item_id,source_key,directory_identity
		) INSERT INTO local_nfo_recovery_work(item_id,source_key,directory_identity,root_identity,relative_path,media_change_ns,version,reason)
		SELECT item_id,source_key,directory_identity,'root','movie.nfo',0,'initial','malformed' FROM queued`); err != nil {
		t.Fatal(err)
	}
	get := func(id string) Definition {
		t.Helper()
		current, err := store.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return current
	}
	const token = "00000000000000000000000000000001"
	claim := func(run Run) string {
		t.Helper()
		if _, err := store.BeginRun(ctx, run.ID); err != nil {
			t.Fatal(err)
		}
		page, err := store.ListChildren(ctx, run.ID, Page{})
		if err != nil || len(page.Items) != 1 || page.Items[0].LibraryID != "library-1" {
			t.Fatalf("%s did not snapshot only the due library: %+v, %v", run.TaskKey, page, err)
		}
		child := page.Items[0].ID
		if _, err := store.claimExecution(ctx, run.ID, child, token); err != nil {
			t.Fatal(err)
		}
		return child
	}
	record := func(tx library.OwnedTx, signals ...systemevents.Event) error {
		for _, signal := range signals {
			if err := systemevents.Record(tx.Exec, signal); err != nil {
				return err
			}
		}
		return nil
	}
	started, err := store.Start(ctx, actor, StartRequest{TaskID: local.ID})
	if err != nil {
		t.Fatal(err)
	}
	localChild := claim(started.Run)
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		return record(tx, systemevents.NFORecoveryRequested)
	}); err != nil {
		t.Fatal(err)
	}
	if changed, err := store.DispatchSystemEvents(ctx, 1); !changed || err != nil {
		t.Fatalf("an active local import blocked recovery admission: %t, %v", changed, err)
	}
	active := get(recovery.ID).CurrentRun
	if active == nil || active.TotalChildren != 1 || active.Source != "system_event" {
		t.Fatalf("recovery did not admit the due library: %+v", active)
	}
	recoveryChild := claim(*active)
	// Either worker may signal work outside the other's active snapshot.
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if _, err := tx.Exec(`UPDATE local_nfo_recovery_work SET attempts=1,retry_at=clock_timestamp()+interval '1 hour'`); err != nil {
			return err
		}
		return record(tx, systemevents.NFORecoveryRequested, systemevents.LocalMetadataIngestRequested)
	}); err != nil {
		t.Fatal(err)
	}
	var deferred *AnalysisDeferrals
	if changed, err := store.DispatchSystemEvents(ctx, 2); changed || !errors.Is(err, ErrActiveRunConflict) ||
		!errors.As(err, &deferred) || len(deferred.Items) != 2 {
		t.Fatalf("active workers consumed request overlap: changed=%t, error=%v", changed, err)
	}
	if get(recovery.ID).Triggers[0].LastEventSequence != 1 || get(local.ID).Triggers[0].LastEventSequence != 0 {
		t.Fatal("overlap advanced a committed event cursor")
	}
	if err := store.finishExecution(ctx, active.ID, recoveryChild, token,
		errors.Join(library.ErrNFORecoveryNoSource, errors.New("private storage detail")), false); err != nil {
		t.Fatal(err)
	}
	finished, err := store.ListChildren(ctx, active.ID, Page{})
	if err != nil || len(finished.Items) != 1 || finished.Items[0].ErrorCode != "nfo_recovery_no_source" ||
		finished.Items[0].ErrorMessage != "Some damaged NFOs have no trusted original to regenerate." {
		t.Fatalf("recovery did not show the safe missing-source reason: %+v, %v", finished, err)
	}
	if err := store.finishExecution(ctx, started.Run.ID, localChild, token, nil, false); err != nil {
		t.Fatal(err)
	}
	if changed, err := store.DispatchSystemEvents(ctx, 2); !changed || err != nil {
		t.Fatalf("deferred events were not retried: changed=%t, error=%v", changed, err)
	}
	var runID string
	var sequence int64
	if err := pool.QueryRow(ctx, `SELECT run_id,first_sequence FROM task_system_event_receipts
		WHERE trigger_id=$1 AND disposition='admitted' AND last_sequence=2`, recovery.Triggers[0].ID).Scan(&runID, &sequence); err != nil || sequence != 2 {
		t.Fatalf("deferred recovery event lost its original sequence: %d, %v", sequence, err)
	}
	if run, err := store.GetRun(ctx, runID); err != nil || run.State != RunCompleted || run.TotalChildren != 0 {
		t.Fatalf("recovery work was admitted before its retry time: %+v, %v", run, err)
	}
	if current := get(local.ID).CurrentRun; current == nil || current.ID == started.Run.ID || current.TotalChildren != 1 {
		t.Fatalf("local metadata import did not get a fresh snapshot: %+v", current)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT attempts FROM local_nfo_recovery_work`).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("scheduling mutated the attempt budget: attempts=%d, %v", attempts, err)
	}
}

package tasks

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/systemevents"
)

func TestDolbyVisionVerificationSharesTheWorkerWithoutAnalysisInput(t *testing.T) {
	key := library.TaskDolbyVisionVerificationKey
	rotation := mediaAnalysisExecutionKeys()
	seek := slices.Index(rotation, library.TaskVideoSeekIngestKey)
	if !isMediaAnalysisExecution(key) || seek < 0 || slices.Index(rotation, key) != seek+1 {
		t.Fatalf("seek indexing and Dolby Vision verification do not share the rotation: %v", rotation)
	}
	if input, err := normalizedTaskAnalysis(key, nil); input != nil || err != nil {
		t.Fatalf("verification cannot start without an analysis scope: %v, %v", input, err)
	}
	if _, err := normalizedTaskAnalysis(key, &library.AnalysisSelection{Force: true}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("verification accepted an explicit analysis selection: %v", err)
	}
	if CompatibilityKey(key) != "GobyVerifyDolbyVision" || analysisAutomation(key).event != "" || !preservesOverlappingSystemEvent(key) {
		t.Fatal("verification lost its compatibility key, dedicated schedule, or event retry policy")
	}
	if err := ValidateSchedule(ScheduleRule{Kind: ScheduleSystemEvent, SystemEvent: systemevents.DolbyVisionVerificationRequested}); err != nil {
		t.Fatalf("verification request is not a supported task event: %v", err)
	}
}

func TestDolbyVisionVerificationSerializesWithSeekingAndRetriesEvents(t *testing.T) {
	ctx, pool, owner, _, actor, _ := taskRepository(t, 1)
	registry, err := NewExecutorRegistry(
		ExecutorRegistration{Key: library.TaskDolbyVisionVerificationKey, Name: "Verify Dolby Vision", Executor: libraryBatchTestExecutor{}},
		ExecutorRegistration{Key: library.TaskVideoSeekIngestKey, Name: "Index seek points", AutomaticDisabled: true, Executor: libraryBatchTestExecutor{}},
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
	definition, err := store.GetByKey(ctx, library.TaskDolbyVisionVerificationKey)
	if err != nil || len(definition.Triggers) != 3 {
		t.Fatalf("verification default rules were not installed: %+v, %v", definition, err)
	}
	event := definition.Triggers[0]
	if _, err := pool.Exec(ctx, `INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES ('fixture-root','library-1','/fixture/movies','/fixture','movies');
		INSERT INTO items(id,library_id,root_id,name,sort_name,type,relative_path,media)
		VALUES ('fixture-movie','library-1','fixture-root','Movie','Movie','Movie','movie.mkv','{}');
		INSERT INTO scan_jobs(id,library_id,status,started_at,finished_at)
		VALUES ('fixture-scan','library-1','Completed',clock_timestamp(),clock_timestamp());`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `WITH queued AS (
		INSERT INTO dolby_vision_verifications(item_id,stream_index,scan_job_id,source_revision,verifier_version,tool_identity)
		SELECT i.id,0,'fixture-scan',`+library.MediaOperationSourceRevisionSQL+`,$1,'fixture-tool' FROM items i WHERE i.id='fixture-movie'
		RETURNING item_id,scan_job_id,source_revision
	) INSERT INTO video_seek_ingest(item_id,scan_job_id,source_revision,index_version)
	SELECT item_id,scan_job_id,source_revision,$2 FROM queued`, media.DolbyVisionVerificationVersion, media.VideoSeekIndexVersion); err != nil {
		t.Fatal(err)
	}
	seekDefinition, err := store.GetByKey(ctx, library.TaskVideoSeekIngestKey)
	if err != nil {
		t.Fatal(err)
	}
	seek, err := store.Start(ctx, actor, StartRequest{TaskID: seekDefinition.ID})
	if err != nil {
		t.Fatal(err)
	}
	claim := func(run Run) Child {
		t.Helper()
		if _, err := store.BeginRun(ctx, run.ID); err != nil {
			t.Fatal(err)
		}
		page, err := store.ListChildren(ctx, run.ID, Page{})
		if err != nil || len(page.Items) != 1 || page.Items[0].LibraryID != "library-1" {
			t.Fatalf("expected one eligible library in %s: %+v, %v", run.TaskKey, page, err)
		}
		return page.Items[0]
	}
	seekChild := claim(seek.Run)
	const seekToken = "00000000000000000000000000000001"
	const verificationToken = "00000000000000000000000000000002"
	if _, err := store.claimExecution(ctx, seek.Run.ID, seekChild.ID, seekToken); err != nil {
		t.Fatal(err)
	}
	startupAt, err := store.ScheduleClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeSchedules(ctx, startupAt); err != nil {
		t.Fatal(err)
	}
	current, err := store.Get(ctx, definition.ID)
	if err != nil || current.CurrentRun == nil || current.CurrentRun.Source != "startup" {
		t.Fatalf("startup did not recover pending verification: %+v, %v", current, err)
	}
	run := *current.CurrentRun
	child := claim(run)
	if next, err := store.nextAnalysisRun(ctx, seek.Run.TaskKey); err != nil || next != run.ID {
		t.Fatalf("verification has no turn in the shared rotation: %q, %v", next, err)
	}
	if _, err := store.claimExecution(ctx, run.ID, child.ID, verificationToken); !errors.Is(err, errAnalysisGroupBusy) {
		t.Fatalf("verification did not wait for the seek worker: %v", err)
	}
	if err := store.finishExecution(ctx, seek.Run.ID, seekChild.ID, seekToken, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.claimExecution(ctx, run.ID, child.ID, verificationToken); err != nil {
		t.Fatal(err)
	}
	if err := store.updateExecutionProgress(ctx, run.ID, child.ID, verificationToken, Progress{Processed: 3, Updated: 2}); err != nil {
		t.Fatal(err)
	}
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		for range 2 {
			if err := systemevents.Record(tx.Exec, systemevents.DolbyVisionVerificationRequested); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var deferred *AnalysisDeferrals
	if changed, err := store.DispatchSystemEvents(ctx, 1); changed || !errors.As(err, &deferred) || len(deferred.Items) != 1 || deferred.Items[0].TriggerID != event.ID {
		t.Fatalf("active verification did not defer the unconsumed event: %t, %v", changed, err)
	}
	if _, err := store.Stop(ctx, actor, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.finishExecution(ctx, run.ID, child.ID, verificationToken, context.Canceled, false); err != nil {
		t.Fatal(err)
	}
	if cancelled, err := store.GetRun(ctx, run.ID); err != nil || cancelled.State != RunCancelled || cancelled.Scanned != 3 || cancelled.Updated != 2 {
		t.Fatalf("cancelled verification lost its progress or terminal state: %+v, %v", cancelled, err)
	}
	if changed, err := store.DispatchSystemEvents(ctx, 1); err != nil || !changed {
		t.Fatalf("deferred verification event did not resume: %t, %v", changed, err)
	}
	var first, last int64
	var retryID string
	if err := pool.QueryRow(ctx, `SELECT first_sequence,last_sequence,run_id FROM task_system_event_receipts
		WHERE trigger_id=$1 AND disposition='admitted'`, event.ID).Scan(&first, &last, &retryID); err != nil || first != 1 || last != 2 {
		t.Fatalf("verification event did not preserve its coalesced range: %d..%d, %v", first, last, err)
	}
	if retry, err := store.GetRun(ctx, retryID); err != nil || retry.State != RunCompleted || retry.TotalChildren != 0 {
		t.Fatalf("automatic dispatch reopened a cancelled scan generation: %+v, %v", retry, err)
	}
	if manual, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID}); err != nil || manual.Run.TotalChildren != 1 {
		t.Fatalf("manual verification could not retry the same accepted generation: %+v, %v", manual, err)
	}
}

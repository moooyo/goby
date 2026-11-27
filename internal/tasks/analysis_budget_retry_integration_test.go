//go:build linux

package tasks

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

// Use the real importer and fenced scanner; only the preview admission fault
// is injected. No FFmpeg, online provider, or elapsed budget is needed.
func TestPreviewBudgetFailureDoesNotBlockLocalArtworkImport(t *testing.T) {
	f := newManagerFixture(t, library.WithDeferredLocalMetadata())
	collection := f.addLibraries(t, 1)[0]
	var poster bytes.Buffer
	if err := png.Encode(&poster, image.NewRGBA(image.Rect(0, 0, 8, 12))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(collection.path, "Fixture-poster.png"), poster.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=options||'{"EnablePreviewGeneration":true}'::jsonb WHERE id=$1`, collection.id); err != nil {
		t.Fatal(err)
	}
	scan, err := f.catalog.StartScan(f.ctx, collection.id)
	if err != nil {
		t.Fatal(err)
	}
	managerWait(t, f.ctx, "completed scan with deferred artwork", func(ctx context.Context) bool {
		job, err := f.catalog.GetJob(ctx, scan.ID)
		if err != nil {
			t.Fatal(err)
		}
		return job.Status == "Completed"
	})
	failBudget, attempts := true, 0
	var schedulingContext *analysisBudgetTestContext
	registry, err := NewExecutorRegistry(
		ExecutorRegistration{Key: library.TaskLocalMetadataIngestKey, Name: "Local import", AutomaticDisabled: true, Executor: localArtworkTestExecutor{f.catalog}},
		ExecutorRegistration{Key: library.TaskPreviewGenerationKey, Name: "Seek previews", Executor: &analysisTestExecutor{},
			AnalysisAdmission: func(tx library.OwnedTx, request AnalysisAdmissionRequest) (AnalysisAdmissionBinding, error) {
				actual, err := library.PrepareAnalysis(tx, request.TaskKey, request.Selection, library.AnalysisExecutionProfile{
					Version: library.AnalysisExecutionProfileVersion, UnavailableReason: "disabled",
				})
				if err != nil {
					return AnalysisAdmissionBinding{}, err
				}
				return AnalysisAdmissionBinding{ConfigurationFingerprint: actual.ConfigurationFingerprint, Bind: actual.Bind,
					SnapshotChildren: func(tx library.OwnedTx, runID string) (int64, error) {
						attempts++
						count, err := actual.SnapshotChildren(tx, runID)
						if err == nil && failBudget {
							// Like the production transaction, roll back only after the
							// shorter scheduling context has run out of time.
							schedulingContext.expired = true
							return 0, library.ErrAnalysisAdmissionBudget
						}
						return count, err
					}}, nil
			}},
	)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(f.pool, f.catalog, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	local, err := store.GetByKey(f.ctx, library.TaskLocalMetadataIngestKey)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := store.GetByKey(f.ctx, library.TaskPreviewGenerationKey)
	if err != nil {
		t.Fatal(err)
	}
	// Explicitly record a request so the witness does not depend on scan event
	// ordering; multiple committed signals may coalesce to one admission.
	if err := f.catalog.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error {
		return systemevents.Record(tx.Exec, systemevents.PreviewGenerationRequested)
	}); err != nil {
		t.Fatal(err)
	}
	imported, err := store.Start(f.ctx, f.actor, StartRequest{TaskID: local.ID})
	if err != nil || imported.Run.State != RunPending || imported.Run.TotalChildren != 1 {
		t.Fatalf("scan did not leave a pending local import: %+v %v", imported.Run, err)
	}
	manager := newManagerOwnershipFixture(t, f.ctx, store, f.catalog)
	f.managers = append(f.managers, manager)
	schedule := func() bool {
		t.Helper()
		manager.nextScheduleAttempt = time.Time{}
		ctx, cancel := context.WithTimeout(manager.ctx, managerCycleTimeout)
		defer cancel()
		schedulingContext = &analysisBudgetTestContext{Context: ctx}
		ownerProbed, err := manager.schedulePass(schedulingContext)
		manager.observeFailure(err)
		if err != nil || !manager.Available() {
			t.Fatalf("analysis deferral stopped or degraded the scheduler: %v", err)
		}
		return ownerProbed
	}
	reconcile := func(probed bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(manager.ctx, managerCycleTimeout)
		defer cancel()
		_, err := manager.reconcilePass(ctx, false, probed)
		if err != nil {
			t.Fatal(err)
		}
	}
	checkPreview := func(wantRuns, wantReceipts, wantWorks, wantSources int, consumed bool) {
		t.Helper()
		var runs, receipts, works, sources int
		var cursorAdvanced bool
		err := f.pool.QueryRow(f.ctx, `SELECT
		 (SELECT count(*) FROM task_runs WHERE task_id=$1),
		 (SELECT count(*) FROM task_system_event_receipts WHERE task_id=$1),
		 (SELECT count(*) FROM analysis_work WHERE task_key=$2),
		 (SELECT count(*) FROM analysis_work_sources source JOIN analysis_work work ON work.child_id=source.child_id WHERE work.task_key=$2),
		 (SELECT last_event_sequence>0 FROM task_triggers WHERE task_id=$1 AND kind='system_event' AND retired_at IS NULL)`, preview.ID, preview.Key).
			Scan(&runs, &receipts, &works, &sources, &cursorAdvanced)
		if err != nil || runs != wantRuns || receipts != wantReceipts || works != wantWorks || sources != wantSources || cursorAdvanced != consumed {
			t.Fatalf("preview transaction state: runs=%d receipts=%d work=%d sources=%d cursor=%t error=%v", runs, receipts, works, sources, cursorAdvanced, err)
		}
	}
	probed := schedule()
	if attempts != 1 || len(manager.DeferredAnalyses()) != 1 || !manager.DeferredAnalyses()[0].BudgetExhausted {
		t.Fatalf("expected one isolated preview budget deferral: attempts=%d deferrals=%+v", attempts, manager.DeferredAnalyses())
	}
	checkPreview(0, 0, 0, 0, false)
	reconcile(probed)
	if len(manager.executions) != 1 {
		t.Fatalf("pending artwork import was not dispatched: %d workers", len(manager.executions))
	}
	for _, worker := range manager.executions {
		select {
		case <-worker.done:
		case <-f.ctx.Done():
			t.Fatal("artwork importer did not finish")
		}
		if worker.runID != imported.Run.ID || worker.err != nil {
			t.Fatalf("unexpected worker or import error: %v", worker.err)
		}
	}
	// Give the preview a due timer while the same definition is cooling down
	// from an event. Neither automatic source may retry the expensive snapshot.
	clock, err := store.ScheduleClock(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$2 WHERE task_id=$1 AND kind='interval'`, preview.ID, clock.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	reconcile(schedule())
	if attempts != 1 || manager.nextDue != nil {
		t.Fatalf("cooldown did not exclude the definition across trigger sources: attempts=%d next=%v", attempts, manager.nextDue)
	}
	finished, err := store.GetRun(f.ctx, imported.Run.ID)
	if err != nil || finished.State != RunCompleted || finished.Scanned == 0 || finished.Updated == 0 {
		t.Fatalf("local import did not finish while preview was deferred: %+v %v", finished, err)
	}
	var posters, pending int
	if err := f.pool.QueryRow(f.ctx, `SELECT
		(SELECT count(*) FROM item_images image JOIN items i ON i.id=image.item_id WHERE i.library_id=$1 AND i.type='Movie' AND image.image_type='Primary'),
		(SELECT count(*) FROM local_metadata_ingest pending JOIN items i ON i.id=pending.item_id WHERE i.library_id=$1 AND pending.import_images)`, collection.id).Scan(&posters, &pending); err != nil || posters != 1 || pending != 0 {
		t.Fatalf("artwork did not become visible: posters=%d pending=%d error=%v", posters, pending, err)
	}
	checkPreview(0, 0, 0, 0, false)
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$2 WHERE task_id=$1 AND kind='interval'`, preview.ID, clock.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Advance only the test's monotonic backoff marker. The same event should
	// now commit exactly once, including its originally rolled-back snapshot.
	manager.analysisBudgetRetries[preview.ID] = analysisBudgetRetry{until: time.Now().Add(-time.Second)}
	failBudget = false
	schedule()
	schedule()
	if attempts != 2 || len(manager.DeferredAnalyses()) != 0 {
		t.Fatalf("preview was not retried exactly once: attempts=%d deferrals=%+v", attempts, manager.DeferredAnalyses())
	}
	checkPreview(1, 1, 1, 1, true)
}

type localArtworkTestExecutor struct{ catalog *library.Store }

func (executor localArtworkTestExecutor) Available() bool { return executor.catalog.Available() }

func (executor localArtworkTestExecutor) Execute(ctx context.Context, work Work, report func(Progress) error) error {
	work = work.WithContext(ctx)
	return executor.catalog.ImportLocalScanMetadata(ctx, work.ChildID, work.LibraryID, work.Fence, func(p library.ProviderProgress) error {
		return report(Progress{Processed: p.Processed, Updated: p.Updated})
	})
}

//go:build linux

package tasks

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestPreviewDefaultScheduleInstallsOnceAndPreservesAdministratorDecisions(t *testing.T) {
	testDefaultAnalysisSchedule(t, library.TaskPreviewGenerationKey)
}

func TestPreviewDefaultInstallationPreservesExistingIntroSchedule(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	intro := f.definitions[library.TaskIntroAnalysisKey]
	preview := f.definitions[library.TaskPreviewGenerationKey]
	// Recreate the upgrade state: intro defaults already exist, while preview
	// has never received a schedule or an administrator revision.
	if _, err := f.pool.Exec(f.ctx, `DELETE FROM task_triggers WHERE task_id=$1`, preview.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_definitions SET revision=1 WHERE id=$1`, preview.ID); err != nil {
		t.Fatal(err)
	}
	recordTestAnalysisRequest(t, f, library.TaskPreviewGenerationKey)
	if err := f.store.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if after, err := f.store.Get(f.ctx, intro.ID); err != nil || !reflect.DeepEqual(after, intro) {
		t.Fatalf("preview defaults changed the existing intro identities or plan: %+v %v", after, err)
	}
	after, err := f.store.Get(f.ctx, preview.ID)
	if err != nil || after.ID != preview.ID || after.Revision != 2 || len(after.Triggers) != 2 || after.Triggers[1].LastEventSequence != 0 {
		t.Fatalf("preview defaults did not preserve the pending request: %+v %v", after, err)
	}
	if changed, err := f.store.DispatchSystemEvents(f.ctx, 10); err != nil || !changed {
		t.Fatalf("request preceding default installation was lost: %t %v", changed, err)
	}
}

func TestAutomaticPreviewAdmissionUsesOnlyEnabledEligibleLibraries(t *testing.T) {
	for _, source := range []string{"schedule", "startup", "system_event"} {
		t.Run(source, func(t *testing.T) {
			f := newAnalysisTestFixture(t, 1)
			if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type)
				VALUES('preview-tv','Television','tvshows'),('preview-mixed','Mixed','mixed'),('preview-music','Music','music')`); err != nil {
				t.Fatal(err)
			}
			enableTestAutomaticLibraries(t, f, library.TaskPreviewGenerationKey, "library-1", "preview-tv", "preview-mixed")
			// Enabling intro must not enable preview for the second library.
			enableTestIntroLibraries(t, f, "library-2")
			run := admitTestAutomaticAnalysis(t, f, library.TaskPreviewGenerationKey, source)
			wanted := []string{"library-1", "preview-mixed", "preview-tv"}
			if run.AnalysisInput == nil || !reflect.DeepEqual(run.AnalysisInput.LibraryIDs, wanted) {
				t.Fatalf("automatic preview did not freeze the enabled eligible set: %+v", run.AnalysisInput)
			}
			page, err := f.store.ListChildren(f.ctx, run.ID, Page{Limit: MaxPageLimit})
			if err != nil || page.TotalRecordCount != int64(len(wanted)) || len(page.Items) != len(wanted) {
				t.Fatalf("preview child population is incomplete: %+v %v", page, err)
			}
			ids := make([]string, 0, len(page.Items))
			for _, child := range page.Items {
				ids = append(ids, child.LibraryID)
			}
			if !reflect.DeepEqual(ids, wanted) {
				t.Fatalf("preview included an unselected or ineligible library: %v", ids)
			}
		})
	}
}

func TestAutomaticPreviewEmptySetNeverInvokesAllLibrarySnapshot(t *testing.T) {
	testAutomaticAnalysisEmptySet(t, library.TaskPreviewGenerationKey)
}

func TestPreviewRequestDuringIdenticalRunIsDeferredAndCoalescedAfterCompletion(t *testing.T) {
	testAnalysisRequestDeferral(t, library.TaskPreviewGenerationKey)
}

func TestPreviewCommittedRequestSurvivesSchedulerRestartWithoutCursorCatchup(t *testing.T) {
	testAnalysisRestartCursor(t, library.TaskPreviewGenerationKey)
}

func TestPreviewManualAdmissionCanSelectLibrariesWithAutomationDisabled(t *testing.T) {
	f := newAnalysisTestFixture(t, 1)
	run := f.start(t, library.TaskPreviewGenerationKey, &library.AnalysisSelection{LibraryIDs: []string{"library-2"}})
	children := f.children(t, run)
	if len(children) != 1 || children[0].LibraryID != "library-2" || run.Source != "manual" {
		t.Fatalf("disabled automation rejected an explicit manual preview: %+v %+v", run, children)
	}
}

func TestPreviewRecoveryRequestsFreshAutomaticWorkOnce(t *testing.T) {
	testAnalysisRecoveryRequestsFreshWork(t, library.TaskPreviewGenerationKey)
}

func TestPreviewRecoveryDoesNotReplayManualOrExplicitlyStoppedWork(t *testing.T) {
	testAnalysisRecoveryDoesNotReplayExplicitStops(t, library.TaskPreviewGenerationKey)
}

func TestPreviewGracefulShutdownRetainsRequestForNextScheduler(t *testing.T) {
	testAnalysisGracefulShutdownRetainsRequest(t, library.TaskPreviewGenerationKey)
}

func TestAnalysisRecoveryKeepsIntroAndPreviewReplacementEventsSeparate(t *testing.T) {
	f := newAnalysisTestFixture(t, 1)
	keys := []string{library.TaskIntroAnalysisKey, library.TaskPreviewGenerationKey}
	oldRuns := make(map[string]Run)
	for _, key := range keys {
		enableTestAutomaticLibraries(t, f, key, "library-1")
		oldRuns[key] = admitTestAutomaticAnalysis(t, f, key, "system_event")
	}
	if err := f.store.RecoverRuns(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecoverRuns(f.ctx); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if sequence := analysisRequestSequence(t, f, key); sequence != 2 {
			t.Fatalf("recovery lost or duplicated %s's request: %d", key, sequence)
		}
		old, err := f.store.GetRun(f.ctx, oldRuns[key].ID)
		if err != nil || old.State != RunInterrupted {
			t.Fatalf("recovery did not retain %s's old run: %+v %v", key, old, err)
		}
	}
	if changed, err := f.store.DispatchSystemEvents(f.ctx, 10); err != nil || !changed {
		t.Fatalf("replacement events were not consumed: %t %v", changed, err)
	}
	for _, key := range keys {
		current, err := f.store.Get(f.ctx, oldRuns[key].TaskID)
		if err != nil || current.CurrentRun == nil || current.CurrentRun.ID == oldRuns[key].ID || current.CurrentRun.TaskKey != key {
			t.Fatalf("replacement event selected the wrong analysis: %+v %v", current, err)
		}
	}
}

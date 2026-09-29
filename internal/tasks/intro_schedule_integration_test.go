//go:build linux

package tasks

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

func enableTestIntroLibraries(t *testing.T, f *analysisTestFixture, ids ...string) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET collection_type='tvshows',
		options=options||'{"EnableIntroDetection":true}'::jsonb WHERE id=ANY($1::text[])`, ids); err != nil {
		t.Fatal(err)
	}
}

func recordTestIntroRequest(t *testing.T, f *analysisTestFixture) {
	t.Helper()
	if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error {
		return systemevents.Record(tx.Exec, systemevents.IntroAnalysisRequested)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestIntroDefaultScheduleInstallsOnceAndPreservesAdministratorDecisions(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	definition := f.definitions[library.TaskIntroAnalysisKey]
	if definition.Revision != 2 || len(definition.Triggers) != 2 {
		t.Fatalf("default intro schedule was not installed: %+v", definition)
	}
	interval, event := definition.Triggers[0], definition.Triggers[1]
	if interval.Kind != string(ScheduleInterval) || interval.IntervalTicks == nil ||
		*interval.IntervalTicks != 86400*ScheduleTicksPerSecond || interval.AnchorAt == nil || interval.NextFireAt == nil ||
		interval.NextFireAt.Sub(*interval.AnchorAt) != 24*time.Hour || event.Kind != string(ScheduleSystemEvent) ||
		event.SystemEvent == nil || *event.SystemEvent != string(systemevents.IntroAnalysisRequested) || event.LastEventSequence != 0 {
		t.Fatal("default intro schedule has the wrong interval or event cursor")
	}
	if len(f.definitions[library.TaskPreviewGenerationKey].Triggers) != 0 {
		t.Fatal("intro defaults changed the preview schedule")
	}
	if err := f.store.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if after, err := f.store.Get(f.ctx, definition.ID); err != nil || !reflect.DeepEqual(after, definition) {
		t.Fatalf("reconciliation rewrote the installed schedule: %+v %v", after, err)
	}
	for _, scenario := range []string{"custom", "cleared", "empty_legacy_choice", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAnalysisTestFixture(t, 0)
			definition := f.definitions[library.TaskIntroAnalysisKey]
			switch scenario {
			case "custom", "cleared":
				rules := []ScheduleRule{}
				if scenario == "custom" {
					rules = append(rules, ScheduleRule{Kind: ScheduleStartup})
				}
				if _, err := f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{TaskID: definition.ID,
					Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: rules}); err != nil {
					t.Fatal(err)
				}
			case "empty_legacy_choice", "disabled":
				if _, err := f.pool.Exec(f.ctx, `DELETE FROM task_triggers WHERE task_id=$1`, definition.ID); err != nil {
					t.Fatal(err)
				}
				if scenario == "disabled" {
					if _, err := f.pool.Exec(f.ctx, `UPDATE task_definitions SET enabled=false,revision=1 WHERE id=$1`, definition.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			before, err := f.store.Get(f.ctx, definition.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.store.Reconcile(f.ctx); err != nil {
				t.Fatal(err)
			}
			if after, err := f.store.Get(f.ctx, definition.ID); err != nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("administrator decision was replaced: %+v %v", after, err)
			}
		})
	}
}

func TestAutomaticIntroAdmissionUsesOnlyEnabledTelevisionLibraries(t *testing.T) {
	for _, kind := range []ScheduleKind{ScheduleStartup, ScheduleInterval, ScheduleSystemEvent} {
		t.Run(string(kind), func(t *testing.T) {
			f := newAnalysisTestFixture(t, 1)
			enableTestIntroLibraries(t, f, "library-1")
			if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type)
				VALUES('disabled-tv','Disabled television','tvshows'),('mixed','Mixed','mixed')`); err != nil {
				t.Fatal(err)
			}
			definition := f.definitions[library.TaskIntroAnalysisKey]
			ticks := ScheduleTicksPerSecond
			rule := ScheduleRule{Kind: kind}
			if kind == ScheduleInterval {
				rule.IntervalTicks = &ticks
			}
			if kind == ScheduleSystemEvent {
				rule.SystemEvent = systemevents.IntroAnalysisRequested
			}
			if _, err := f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{TaskID: definition.ID,
				Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{rule}}); err != nil {
				t.Fatal(err)
			}
			now, err := f.store.ScheduleClock(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case ScheduleStartup:
				err = f.store.InitializeSchedules(f.ctx, now)
			case ScheduleInterval:
				if _, err := f.pool.Exec(f.ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE task_id=$1 AND retired_at IS NULL`,
					definition.ID, now.Add(-2*time.Second), now.Add(-time.Second)); err != nil {
					t.Fatal(err)
				}
				_, err = f.store.DispatchDue(f.ctx, 10)
			case ScheduleSystemEvent:
				recordTestIntroRequest(t, f)
				_, err = f.store.DispatchSystemEvents(f.ctx, 10)
			}
			if err != nil {
				t.Fatal(err)
			}
			current, err := f.store.Get(f.ctx, definition.ID)
			if err != nil || current.CurrentRun == nil || current.CurrentRun.AnalysisInput == nil ||
				!reflect.DeepEqual(current.CurrentRun.AnalysisInput.LibraryIDs, []string{"library-1"}) {
				t.Fatalf("automatic admission did not freeze the enabled set: %+v %v", current, err)
			}
			children := f.children(t, *current.CurrentRun)
			if len(children) != 1 || children[0].LibraryID != "library-1" {
				t.Fatalf("automatic admission included a disabled or non-TV library: %+v", children)
			}
		})
	}
}

func TestAutomaticIntroEmptySetNeverInvokesAllLibrarySnapshot(t *testing.T) {
	f := newAnalysisTestFixture(t, 1)
	entry := f.store.executors.entries[library.TaskIntroAnalysisKey]
	entry.Executor = &genericTestExecutor{available: false}
	prepare := entry.AnalysisAdmission
	entry.AnalysisAdmission = func(tx library.OwnedTx, request AnalysisAdmissionRequest) (AnalysisAdmissionBinding, error) {
		binding, err := prepare(tx, request)
		binding.SnapshotChildren = func(library.OwnedTx, string) (int64, error) {
			return 0, errors.New("empty automatic targets reached the all-library snapshot")
		}
		return binding, err
	}
	f.store.executors.entries[library.TaskIntroAnalysisKey] = entry
	recordTestIntroRequest(t, f)
	if changed, err := f.store.DispatchSystemEvents(f.ctx, 10); err != nil || !changed {
		t.Fatalf("empty event was not consumed safely: %t %v", changed, err)
	}
	definition, err := f.store.Get(f.ctx, f.definitions[library.TaskIntroAnalysisKey].ID)
	if err != nil || definition.CurrentRun != nil || definition.LastRun == nil || definition.LastRun.State != RunCompleted || definition.LastRun.TotalChildren != 0 {
		t.Fatalf("empty selection did not finish without work: %+v %v", definition, err)
	}
	if changed, err := f.store.DispatchSystemEvents(f.ctx, 10); err != nil || changed {
		t.Fatalf("event consumption produced another event: %t %v", changed, err)
	}
}

func TestIntroRequestDuringIdenticalRunIsDeferredAndCoalescedAfterCompletion(t *testing.T) {
	f := newAnalysisTestFixture(t, 1)
	enableTestIntroLibraries(t, f, "library-1")
	manual := f.start(t, library.TaskIntroAnalysisKey, &library.AnalysisSelection{LibraryIDs: []string{"library-1"}})
	recordTestIntroRequest(t, f)
	recordTestIntroRequest(t, f)
	changed, err := f.store.DispatchSystemEvents(f.ctx, 10)
	var deferred *AnalysisDeferrals
	if changed || !errors.As(err, &deferred) || len(deferred.Items) != 1 {
		t.Fatalf("identical active scope swallowed the new intro request: %t %v", changed, err)
	}
	var receipts int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM task_system_event_receipts WHERE task_id=$1`, manual.TaskID).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("deferred event was consumed: %d %v", receipts, err)
	}
	if _, err := f.store.Stop(f.ctx, f.actor, manual.ID); err != nil {
		t.Fatal(err)
	}
	if changed, err := f.store.DispatchSystemEvents(f.ctx, 10); err != nil || !changed {
		t.Fatalf("retained intro request did not resume: %t %v", changed, err)
	}
	definition, err := f.store.Get(f.ctx, manual.TaskID)
	if err != nil || definition.CurrentRun == nil || definition.CurrentRun.ID == manual.ID {
		t.Fatalf("retained event reused its old snapshot: %+v %v", definition, err)
	}
	var first, last, sequence int64
	if err := f.pool.QueryRow(f.ctx, `SELECT r.first_sequence,r.last_sequence,e.sequence
		FROM task_system_event_receipts r JOIN task_system_events e ON e.name=r.system_event
		WHERE r.task_id=$1`, manual.TaskID).Scan(&first, &last, &sequence); err != nil || first != 1 || last != 2 || sequence != 2 {
		t.Fatalf("coalesced signal range changed or self-triggered: %d %d %d %v", first, last, sequence, err)
	}
}

func TestIntroCommittedRequestSurvivesSchedulerRestartWithoutCursorCatchup(t *testing.T) {
	f := newAnalysisTestFixture(t, 1)
	recordTestIntroRequest(t, f)
	if changed, err := f.store.DispatchSystemEvents(f.ctx, 10); err != nil || !changed {
		t.Fatalf("consume the earlier empty request: %t %v", changed, err)
	}
	definition := f.definitions[library.TaskIntroAnalysisKey]
	triggerID := definition.Triggers[1].ID
	enableTestIntroLibraries(t, f, "library-1")
	recordTestIntroRequest(t, f)
	reloaded, err := New(f.pool, f.owner, f.store.executors)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	now, err := reloaded.ScheduleClock(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.InitializeSystemEvents(f.ctx, now); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.InitializeSchedules(f.ctx, now); err != nil {
		t.Fatal(err)
	}
	var cursor, sequence int64
	if err := f.pool.QueryRow(f.ctx, `SELECT t.last_event_sequence,e.sequence FROM task_triggers t
		JOIN task_system_events e ON e.name=t.system_event WHERE t.id=$1`, triggerID).Scan(&cursor, &sequence); err != nil || cursor != 1 || sequence != 2 {
		t.Fatalf("startup consumed the offline intro request: cursor=%d sequence=%d error=%v", cursor, sequence, err)
	}
	if changed, err := reloaded.DispatchSystemEvents(f.ctx, 10); err != nil || !changed {
		t.Fatalf("offline intro request was not dispatched after restart: %t %v", changed, err)
	}
	current, err := reloaded.Get(f.ctx, definition.ID)
	if err != nil || current.CurrentRun == nil || current.CurrentRun.TotalChildren != 1 ||
		current.CurrentRun.AnalysisInput == nil || !reflect.DeepEqual(current.CurrentRun.AnalysisInput.LibraryIDs, []string{"library-1"}) {
		t.Fatalf("restart did not snapshot the newly enabled library: %+v %v", current, err)
	}
	var first, last int64
	if err := f.pool.QueryRow(f.ctx, `SELECT first_sequence,last_sequence FROM task_system_event_receipts
		WHERE run_id=$1`, current.CurrentRun.ID).Scan(&first, &last); err != nil || first != 2 || last != 2 {
		t.Fatalf("restart replayed an already consumed event range: %d %d %v", first, last, err)
	}
}

func TestIntroPolicyDoesNotChangeManualOrPreviewSelections(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	manual := f.start(t, library.TaskIntroAnalysisKey, nil)
	if children := f.children(t, manual); len(children) != 2 {
		t.Fatalf("explicit manual analysis lost its original scope: %+v", children)
	}
	if _, err := f.store.Stop(f.ctx, f.actor, manual.ID); err != nil {
		t.Fatal(err)
	}
	preview := f.definitions[library.TaskPreviewGenerationKey]
	if _, err := f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{TaskID: preview.ID, Revision: preview.Revision,
		ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleStartup}}}); err != nil {
		t.Fatal(err)
	}
	now, err := f.store.ScheduleClock(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.InitializeSchedules(f.ctx, now); err != nil {
		t.Fatal(err)
	}
	current, err := f.store.Get(f.ctx, preview.ID)
	if err != nil || current.CurrentRun == nil || len(f.children(t, *current.CurrentRun)) != 2 {
		t.Fatalf("intro policy narrowed preview scheduling: %+v %v", current, err)
	}
}

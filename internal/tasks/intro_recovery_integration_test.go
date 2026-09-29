//go:build linux

package tasks

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

func analysisRequestSequence(t *testing.T, f *analysisTestFixture, key string) int64 {
	t.Helper()
	var sequence int64
	if err := f.pool.QueryRow(f.ctx, `SELECT sequence FROM task_system_events WHERE name=$1`,
		string(analysisAutomation(key).event)).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	return sequence
}

func admitTestAutomaticAnalysis(t *testing.T, f *analysisTestFixture, key, source string) Run {
	t.Helper()
	definition := f.definitions[key]
	now, err := f.store.ScheduleClock(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	switch source {
	case "system_event":
		recordTestAnalysisRequest(t, f, key)
		_, err = f.store.DispatchSystemEvents(f.ctx, 10)
	case "schedule":
		if _, err := f.pool.Exec(f.ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3
			WHERE task_id=$1 AND retired_at IS NULL AND kind='interval'`, definition.ID,
			now.Add(-48*time.Hour), now.Add(-24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		_, err = f.store.DispatchDue(f.ctx, 10)
	case "startup":
		rules := []ScheduleRule{}
		for _, trigger := range definition.Triggers {
			rule := triggerScheduleRule(trigger)
			// Replacement accepts client fields; anchors and per-rule zones
			// are assigned again from the server clock and schedule timezone.
			rule.AnchorAt, rule.Timezone = nil, ""
			rules = append(rules, rule)
		}
		rules = append(rules, ScheduleRule{Kind: ScheduleStartup})
		if _, err := f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{TaskID: definition.ID,
			Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: rules}); err != nil {
			t.Fatal(err)
		}
		now, err = f.store.ScheduleClock(f.ctx)
		if err == nil {
			err = f.store.InitializeSchedules(f.ctx, now)
		}
	default:
		t.Fatal("unsupported automatic analysis fixture source")
	}
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.store.Get(f.ctx, definition.ID)
	if err != nil || current.CurrentRun == nil || current.CurrentRun.Source != source {
		t.Fatalf("automatic analysis fixture was not admitted: %+v %v", current, err)
	}
	run, err := f.store.BeginRun(f.ctx, current.CurrentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func claimTestAnalysisWork(t *testing.T, f *analysisTestFixture, run Run) Child {
	t.Helper()
	children := f.children(t, run)
	if len(children) != 1 {
		t.Fatalf("expected one analysis recovery child: %+v", children)
	}
	token, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.claimExecution(f.ctx, run.ID, children[0].ID, token); err != nil {
		t.Fatal(err)
	}
	return children[0]
}

func TestIntroRecoveryRequestsFreshAutomaticWorkOnce(t *testing.T) {
	testAnalysisRecoveryRequestsFreshWork(t, library.TaskIntroAnalysisKey)
}

func testAnalysisRecoveryRequestsFreshWork(t *testing.T, key string) {
	t.Helper()
	for _, source := range []string{"schedule", "startup", "system_event"} {
		t.Run(source, func(t *testing.T) {
			f := newAnalysisTestFixture(t, 1)
			enableTestAutomaticLibraries(t, f, key, "library-1")
			old := admitTestAutomaticAnalysis(t, f, key, source)
			child := claimTestAnalysisWork(t, f, old)
			before := analysisRequestSequence(t, f, key)
			if err := f.store.RecoverRuns(f.ctx); err != nil {
				t.Fatal(err)
			}
			if err := f.store.RecoverRuns(f.ctx); err != nil {
				t.Fatal(err)
			}
			if after := analysisRequestSequence(t, f, key); after != before+1 {
				t.Fatalf("recovery lost or duplicated its replacement request: %d -> %d", before, after)
			}
			retained, err := f.store.GetRun(f.ctx, old.ID)
			if err != nil || retained.State != RunInterrupted || retained.Source != source || retained.ErrorCode != "server_interrupted" ||
				!reflect.DeepEqual(retained.AnalysisInput, old.AnalysisInput) {
				t.Fatalf("recovery rewrote the old execution instead of retaining it: %+v %v", retained, err)
			}
			children := f.children(t, retained)
			if len(children) != 1 || children[0].ID != child.ID || children[0].State != ChildInterrupted {
				t.Fatal("abandoned child audit was replaced")
			}
			if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=options||jsonb_build_object($1::text,false) WHERE id='library-1'`, analysisAutomation(key).option); err != nil {
				t.Fatal(err)
			}
			enableTestAutomaticLibraries(t, f, key, "library-2")
			if changed, err := f.store.DispatchSystemEvents(f.ctx, 10); err != nil || !changed {
				t.Fatalf("recovery did not admit fresh work: %t %v", changed, err)
			}
			current, err := f.store.Get(f.ctx, old.TaskID)
			if err != nil || current.CurrentRun == nil || current.CurrentRun.ID == old.ID || current.CurrentRun.Source != "system_event" ||
				current.CurrentRun.AnalysisInput == nil || !reflect.DeepEqual(current.CurrentRun.AnalysisInput.LibraryIDs, []string{"library-2"}) {
				t.Fatalf("replacement ignored current library policy: %+v %v", current, err)
			}
		})
	}
}

func TestIntroRecoveryDoesNotReplayManualOrExplicitlyStoppedWork(t *testing.T) {
	testAnalysisRecoveryDoesNotReplayExplicitStops(t, library.TaskIntroAnalysisKey)
}

func testAnalysisRecoveryDoesNotReplayExplicitStops(t *testing.T, key string) {
	t.Helper()
	for _, reason := range []string{"administrator", "max_runtime", "manual_crash", "manual_shutdown"} {
		t.Run(reason, func(t *testing.T) {
			f := newAnalysisTestFixture(t, 1)
			enableTestAutomaticLibraries(t, f, key, "library-1")
			var run Run
			if reason == "manual_crash" || reason == "manual_shutdown" {
				run = f.start(t, key, &library.AnalysisSelection{LibraryIDs: []string{"library-1"}})
			} else {
				run = admitTestAutomaticAnalysis(t, f, key, "system_event")
			}
			claimTestAnalysisWork(t, f, run)
			before := analysisRequestSequence(t, f, key)
			var err error
			switch reason {
			case "administrator":
				_, err = f.store.Stop(f.ctx, f.actor, run.ID)
			case "max_runtime":
				_, err = f.store.SystemStopRun(f.ctx, run.ID, "max_runtime")
			case "manual_shutdown":
				_, err = f.store.SystemStopRun(f.ctx, run.ID, "shutdown")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := f.store.RecoverRuns(f.ctx); err != nil {
				t.Fatal(err)
			}
			if after := analysisRequestSequence(t, f, key); after != before {
				t.Fatalf("an explicit stop or manual run generated automatic work: %d -> %d", before, after)
			}
			if changed, err := f.store.DispatchSystemEvents(f.ctx, 10); err != nil || changed {
				t.Fatalf("an explicit stop or manual run was replayed: %t %v", changed, err)
			}
		})
	}
}

func TestIntroGracefulShutdownRetainsRequestForNextScheduler(t *testing.T) {
	testAnalysisGracefulShutdownRetainsRequest(t, library.TaskIntroAnalysisKey)
}

func testAnalysisGracefulShutdownRetainsRequest(t *testing.T, key string) {
	t.Helper()
	f := newAnalysisTestFixture(t, 1)
	enableTestAutomaticLibraries(t, f, key, "library-1")
	executor := f.executors[key]
	executor.release = make(chan struct{})
	manager, err := NewManager(f.store, f.owner, ManagerOptions{ReconcileInterval: 250 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	recordTestAnalysisRequest(t, f, key)
	manager.Wake()
	work := analysisReceiveWork(t, executor)
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatal(err)
	}
	old, err := f.store.GetRun(f.ctx, work.RunID)
	if err != nil || old.State != RunInterrupted || old.StopReason != "shutdown" {
		t.Fatalf("graceful shutdown lost interrupted audit: %+v %v", old, err)
	}
	if _, err := f.store.SystemStopRun(f.ctx, old.ID, "shutdown"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecoverRuns(f.ctx); err != nil {
		t.Fatal(err)
	}
	var cursor, sequence, runs int64
	if err := f.pool.QueryRow(f.ctx, `SELECT t.last_event_sequence,e.sequence,
		(SELECT count(*) FROM task_runs WHERE task_id=t.task_id)
		FROM task_triggers t JOIN task_system_events e ON e.name=t.system_event
		WHERE t.task_id=$1 AND t.retired_at IS NULL AND t.kind='system_event'`, old.TaskID).Scan(&cursor, &sequence, &runs); err != nil || cursor != 1 || sequence != 2 || runs != 1 {
		t.Fatalf("shutdown consumed or repeated the next-server request: cursor=%d sequence=%d runs=%d error=%v", cursor, sequence, runs, err)
	}
	if changed, err := f.store.DispatchSystemEvents(f.ctx, 10); err != nil || !changed {
		t.Fatalf("next scheduler could not consume the shutdown request: %t %v", changed, err)
	}
	current, err := f.store.Get(f.ctx, old.TaskID)
	if err != nil || current.CurrentRun == nil || current.CurrentRun.ID == old.ID {
		t.Fatalf("next scheduler did not create a fresh run: %+v %v", current, err)
	}
}

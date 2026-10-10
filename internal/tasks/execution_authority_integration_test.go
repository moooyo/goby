//go:build linux

package tasks

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

func TestGlobalWorkerExecutionAuthorizationAcceptsManualAndSystemSources(t *testing.T) {
	for _, scenario := range []struct {
		source string
		legacy bool
	}{
		{"manual", false}, {"startup", false}, {"schedule", false}, {"system_event", false},
		{"startup", true}, {"schedule", true}, {"system_event", true},
	} {
		t.Run(fmt.Sprintf("%s/legacy=%t", scenario.source, scenario.legacy), func(t *testing.T) {
			f := newAnalysisTestFixture(t, 0)
			executor := f.executors[CacheMaintainKey]
			executor.release = make(chan struct{})
			var run Run
			if scenario.source == "manual" {
				run = f.start(t, CacheMaintainKey, nil)
			} else {
				definition := f.definitions[CacheMaintainKey]
				rule := ScheduleRule{Kind: ScheduleStartup}
				switch scenario.source {
				case "schedule":
					ticks := ScheduleTicksPerSecond
					rule = ScheduleRule{Kind: ScheduleInterval, IntervalTicks: &ticks}
				case "system_event":
					rule = ScheduleRule{Kind: ScheduleSystemEvent, SystemEvent: systemevents.ServerStarted}
				}
				saved, err := f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{TaskID: definition.ID,
					Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{rule}})
				if err != nil {
					t.Fatal(err)
				}
				now, err := f.store.ScheduleClock(f.ctx)
				if err != nil {
					t.Fatal(err)
				}
				switch scenario.source {
				case "startup":
					err = f.store.InitializeSchedules(f.ctx, now)
				case "schedule":
					_, err = f.pool.Exec(f.ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE id=$1`,
						saved.Triggers[0].ID, now.Add(-2*time.Second), now.Add(-time.Second))
					if err == nil {
						_, err = f.store.DispatchDue(f.ctx, 10)
					}
				case "system_event":
					err = f.store.InitializeSystemEvents(f.ctx, now)
					if err == nil {
						_, err = f.store.DispatchSystemEvents(f.ctx, 10)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				page, err := f.store.ListRuns(f.ctx, definition.ID, Page{})
				if err != nil || len(page.Items) != 1 {
					t.Fatalf("automatic global admission: runs=%d error=%v", len(page.Items), err)
				}
				run = page.Items[0]
				if run.Source != scenario.source || run.ActorKind != "system" || run.ActorUserID != "" || run.ActorSessionID != "" || run.authority != (executionAuthority{}) {
					t.Fatalf("automatic global admission did not preserve a canonical system identity: %+v", run)
				}
				if scenario.legacy {
					// Previous generic schedulers persisted this valid empty system
					// kind. Leave the row pending to model work queued before upgrade.
					if _, err := f.pool.Exec(f.ctx, `UPDATE task_runs SET actor_kind='' WHERE id=$1 AND state='pending'`, run.ID); err != nil {
						t.Fatal(err)
					}
				}
				run, err = f.store.BeginRun(f.ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				f.invalidateActor(t, "revoked")
			}
			child := f.children(t, run)[0]
			var libraryID, scope *string
			if err := f.pool.QueryRow(f.ctx, `SELECT library_id,analysis_scope_key FROM task_run_children WHERE id=$1`, child.ID).Scan(&libraryID, &scope); err != nil || libraryID == nil || scope == nil || *libraryID != "" || *scope != "" {
				t.Fatalf("global child did not retain its empty non-null scope: library=%v scope=%v error=%v", libraryID, scope, err)
			}
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			manager := &Manager{store: f.store, ctx: ctx, wake: make(chan struct{}, 1), executions: map[string]*workerExecution{}, runtimeDeadlines: map[string]time.Time{}}
			t.Cleanup(func() { manager.drainExecutions() })
			if disposition, err := manager.reconcileExecution(f.ctx, run, child, false); err != nil || disposition != executionContinue {
				t.Fatalf("global dispatch: disposition=%d error=%v", disposition, err)
			}
			execution := manager.executions[child.ID]
			if execution == nil {
				t.Fatal("global worker was not dispatched")
			}
			var work Work
			select {
			case work = <-executor.started:
			case <-execution.done:
				t.Fatalf("global worker failed before Execute: %v", execution.err)
			case <-time.After(5 * time.Second):
				t.Fatal("global worker did not start")
			}
			if work.LibraryID != "" || work.AnalysisScopeKey != "" || work.RunID != run.ID || work.ChildID != child.ID {
				t.Fatalf("global execution changed its durable scope: %+v", work)
			}
			if scenario.source == "manual" {
				f.invalidateActor(t, "revoked")
			}
			if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return work.Fence(tx) }); err != nil {
				t.Fatalf("approved global worker lost publication: %v", err)
			}
			stopped, err := f.store.SystemStopRun(f.ctx, run.ID, "shutdown")
			if err != nil {
				t.Fatal(err)
			}
			if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return work.Fence(tx) }); !errors.Is(err, context.Canceled) {
				t.Fatalf("stopped global worker retained publication: %v", err)
			}
			if _, err := manager.reconcileExecution(f.ctx, stopped, child, false); err != nil {
				t.Fatal(err)
			}
			analysisWaitDone(t, execution.done)
			if err := manager.reapExecutions(f.ctx, false); err != nil {
				t.Fatal(err)
			}
			if terminal, err := f.store.GetRun(f.ctx, run.ID); err != nil || terminal.State != RunInterrupted {
				t.Fatalf("global worker did not persist cancellation: state=%s error=%v", terminal.State, err)
			}
		})
	}
}

//go:build linux

package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

func TestAnalysisAdmissionBudgetDefersStartupAndTimedRules(t *testing.T) {
	for _, kind := range []ScheduleKind{ScheduleStartup, ScheduleInterval} {
		t.Run(string(kind), func(t *testing.T) {
			f := newAnalysisTestFixture(t, 1)
			const providerKey = "z.analysis_test_provider"
			registrations := []ExecutorRegistration{{Key: providerKey, Name: "Later provider", Global: true, Executor: &analysisTestExecutor{}}}
			for _, entry := range f.store.executors.entries {
				registrations = append(registrations, entry)
			}
			registry, err := NewExecutorRegistry(registrations...)
			if err != nil {
				t.Fatal(err)
			}
			f.store, err = New(f.pool, f.owner, registry)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.store.Reconcile(f.ctx); err != nil {
				t.Fatal(err)
			}
			preview := f.definitions[library.TaskPreviewGenerationKey]
			provider, err := f.store.GetByKey(f.ctx, providerKey)
			if err != nil {
				t.Fatal(err)
			}
			enableTestAutomaticLibraries(t, f, preview.Key, "library-1")
			rule := ScheduleRule{Kind: kind}
			if kind == ScheduleInterval {
				ticks := int64(3600) * ScheduleTicksPerSecond
				rule.IntervalTicks = &ticks
			}
			save := func(definition Definition) Trigger {
				t.Helper()
				updated, err := f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{TaskID: definition.ID,
					Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{rule}})
				if err != nil {
					t.Fatal(err)
				}
				return updated.Triggers[0]
			}
			previewTrigger, providerTrigger := save(preview), save(provider)
			now, err := f.store.ScheduleClock(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if kind == ScheduleInterval {
				if _, err := f.pool.Exec(f.ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$2 WHERE id=$1`, previewTrigger.ID, now.Add(-90*time.Minute)); err != nil {
					t.Fatal(err)
				}
				if _, err := f.pool.Exec(f.ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$2 WHERE id=$1`, providerTrigger.ID, now); err != nil {
					t.Fatal(err)
				}
			}
			fail, attempts := true, 0
			registration := f.store.executors.entries[preview.Key]
			prepare := registration.AnalysisAdmission
			registration.AnalysisAdmission = func(tx library.OwnedTx, input AnalysisAdmissionRequest) (AnalysisAdmissionBinding, error) {
				attempts++
				binding, err := prepare(tx, input)
				if err == nil && fail {
					binding.SnapshotChildren = func(library.OwnedTx, string) (int64, error) {
						return 0, library.ErrAnalysisAdmissionBudget
					}
				}
				return binding, err
			}
			f.store.executors.entries[preview.Key] = registration
			manager := newManagerOwnershipFixture(t, f.ctx, f.store, f.owner)
			manager.startupAt = now
			manager.schedulesInitialized = kind != ScheduleStartup
			pass := func() {
				t.Helper()
				manager.nextScheduleAttempt = time.Time{}
				ctx, cancel := context.WithTimeout(manager.ctx, managerCycleTimeout)
				defer cancel()
				if err := manager.schedule(ctx); err != nil || !manager.Available() {
					t.Fatalf("scoped budget error made scheduler unavailable: %v", err)
				}
			}
			state := func() (cursor string, admitted, missed int) {
				t.Helper()
				if err := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(t)::text,
				 (SELECT count(*) FROM task_occurrences WHERE trigger_id=t.id AND disposition='admitted'),
				 (SELECT count(*) FROM task_occurrences WHERE trigger_id=t.id AND disposition='missed')
				 FROM task_triggers t WHERE t.id=$1`, previewTrigger.ID).Scan(&cursor, &admitted, &missed); err != nil {
					t.Fatal(err)
				}
				return
			}
			before, _, _ := state()
			pass()
			pass()
			if after, admitted, missed := state(); after != before || admitted != 0 || missed != 0 || attempts != 1 {
				t.Fatalf("budget failure changed the cursor, wrote receipts, or retried early: admitted=%d missed=%d attempts=%d", admitted, missed, attempts)
			}
			other, err := f.store.Get(f.ctx, provider.ID)
			if err != nil || other.CurrentRun == nil {
				t.Fatalf("unrelated rule did not advance during preview cooldown: %v", err)
			}
			manager.analysisBudgetRetries[preview.ID] = analysisBudgetRetry{until: time.Now().Add(-time.Second)}
			fail = false
			pass()
			pass()
			_, admitted, missed := state()
			wantMissed := 0
			if kind == ScheduleInterval {
				wantMissed = 1
			}
			if attempts != 2 || admitted != 1 || missed != wantMissed || len(manager.DeferredAnalyses()) != 0 {
				t.Fatalf("retry did not commit the exact occurrence once: attempts=%d admitted=%d missed=%d deferrals=%+v", attempts, admitted, missed, manager.DeferredAnalyses())
			}
		})
	}
}

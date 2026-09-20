//go:build linux

package tasks

import (
	"log/slog"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

func TestAnalysisDeferralPreservesEachOccurrenceAndDoesNotBlockOtherScheduledProviders(t *testing.T) {
	for _, kind := range []ScheduleKind{ScheduleStartup, ScheduleInterval, ScheduleSystemEvent} {
		t.Run(string(kind), func(t *testing.T) {
			f := newAnalysisTestFixture(t, 0)
			// This key sorts after analysis during startup initialization, so a
			// provider admitted before the conflict cannot satisfy the witness.
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
			manual := f.start(t, library.TaskIntroAnalysisKey, &library.AnalysisSelection{LibraryIDs: []string{"library-1"}})
			analysis := f.definitions[library.TaskIntroAnalysisKey]
			provider, err := f.store.GetByKey(f.ctx, providerKey)
			if err != nil {
				t.Fatal(err)
			}
			ticks := ScheduleTicksPerSecond
			event := systemevents.LibraryChanged
			rule := ScheduleRule{Kind: kind}
			if kind == ScheduleInterval {
				rule.IntervalTicks = &ticks
			}
			if kind == ScheduleSystemEvent {
				rule.SystemEvent = event
			}
			var analysisTrigger string
			for _, definition := range []Definition{analysis, provider} {
				updated, err := f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{rule}})
				if err != nil {
					t.Fatal(err)
				}
				if definition.ID == analysis.ID {
					analysisTrigger = updated.Triggers[0].ID
				}
			}
			now, err := f.store.ScheduleClock(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if kind == ScheduleInterval {
				if _, err := f.pool.Exec(f.ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE task_id=$1`, analysis.ID, now.Add(-2*time.Second), now.Add(-time.Second)); err != nil {
					t.Fatal(err)
				}
				if _, err := f.pool.Exec(f.ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE task_id=$1`, provider.ID, now.Add(-time.Second), now); err != nil {
					t.Fatal(err)
				}
			}
			if kind == ScheduleSystemEvent {
				if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return systemevents.Record(tx.Exec, systemevents.LibraryChanged) }); err != nil {
					t.Fatal(err)
				}
			}
			var before string
			if err := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(t)::text FROM task_triggers t WHERE id=$1`, analysisTrigger).Scan(&before); err != nil {
				t.Fatal(err)
			}
			manager := &Manager{store: f.store, scans: f.owner, ctx: f.ctx, startupAt: now, schedulesInitialized: kind != ScheduleStartup,
				options: ManagerOptions{ReconcileInterval: 250 * time.Millisecond, Logger: slog.Default()}}
			if err := manager.schedule(f.ctx); err != nil {
				t.Fatalf("expected analysis backpressure poisoned scheduler health: %v", err)
			}
			if !manager.Available() {
				t.Fatal("deferred analysis made all task services unavailable")
			}
			deferred := manager.DeferredAnalyses()
			if len(deferred) != 1 || deferred[0].TaskID != analysis.ID || deferred[0].TriggerID != analysisTrigger {
				t.Fatalf("missing bounded deferral diagnostic: %+v", deferred)
			}
			deferred[0].TaskID = "changed-reader-copy"
			if manager.DeferredAnalyses()[0].TaskID != analysis.ID {
				t.Fatal("deferral snapshot retained mutable caller state")
			}
			current, err := f.store.Get(f.ctx, provider.ID)
			if err != nil || current.CurrentRun == nil {
				t.Fatalf("another provider was starved by the conflicting analysis: %v", err)
			}
			var after string
			var occurrences int
			if err := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(t)::text,(SELECT count(*) FROM task_occurrences WHERE task_id=$2) FROM task_triggers t WHERE id=$1`, analysisTrigger, analysis.ID).Scan(&after, &occurrences); err != nil || after != before || occurrences != 0 {
				t.Fatal("deferred analysis consumed its trigger cursor or invented an overlap receipt")
			}
			if kind == ScheduleSystemEvent {
				var receipts int
				if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM task_system_event_receipts WHERE task_id=$1`, analysis.ID).Scan(&receipts); err != nil || receipts != 0 {
					t.Fatal("deferred system event acquired a false delivery receipt")
				}
			}
			if _, err := f.store.Stop(f.ctx, f.actor, manual.ID); err != nil {
				t.Fatal(err)
			}
			manager.nextScheduleAttempt = time.Time{}
			if err := manager.schedule(f.ctx); err != nil {
				t.Fatalf("next dispatch did not retry deferred analysis: %v", err)
			}
			current, err = f.store.Get(f.ctx, analysis.ID)
			if err != nil || current.CurrentRun == nil || current.CurrentRun.ID == manual.ID || current.CurrentRun.ActorKind != "system" || current.CurrentRun.AnalysisInput == nil || len(current.CurrentRun.AnalysisInput.LibraryIDs) != 0 {
				t.Fatalf("retry lost its original system/all-library request: %+v %v", current.CurrentRun, err)
			}
			if len(manager.DeferredAnalyses()) != 0 {
				t.Fatal("successful retry retained a stale deferral")
			}
		})
	}
}

func TestAnalysisDeferralInventoryHasAnIndependentBound(t *testing.T) {
	values := make([]AnalysisDeferral, 0, MaxAnalysisDeferrals)
	for index := 0; index < MaxAnalysisDeferrals; index++ {
		value := AnalysisDeferral{TaskID: "analysis", TaskKey: library.TaskIntroAnalysisKey, TriggerID: time.Unix(int64(index), 0).String(), Source: "startup"}
		var err error
		values, err = addAnalysisDeferral(values, value)
		if err != nil {
			t.Fatal(err)
		}
	}
	if repeated, err := addAnalysisDeferral(values, values[0]); err != nil || len(repeated) != len(values) {
		t.Fatal("repeated deferral inflated the inventory")
	}
	if _, err := addAnalysisDeferral(values, AnalysisDeferral{TaskID: "overflow"}); err == nil {
		t.Fatal("unbounded deferral inventory was accepted")
	}
}

func TestAnalysisDeferredStartupBacklogPreservesTimedProviderTurn(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	analysis := f.definitions[library.TaskIntroAnalysisKey]
	provider := f.definitions[CacheMaintainKey]
	f.start(t, analysis.Key, &library.AnalysisSelection{LibraryIDs: []string{"library-1"}})
	rules := make([]ScheduleRule, MaxTriggers)
	for index := range rules {
		rules[index] = ScheduleRule{Kind: ScheduleStartup}
	}
	if _, err := f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{TaskID: analysis.ID, Revision: analysis.Revision, ScheduleTimezone: "UTC", Triggers: rules}); err != nil {
		t.Fatal(err)
	}
	entry := f.store.executors.entries[analysis.Key]
	prepare := entry.AnalysisAdmission
	startupPrepares := 0
	providerSeenBeforeRetry := false
	entry.AnalysisAdmission = func(tx library.OwnedTx, request AnalysisAdmissionRequest) (AnalysisAdmissionBinding, error) {
		if request.Source == "startup" {
			startupPrepares++
			if startupPrepares > 1 {
				if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_occurrences WHERE task_id=$1 AND disposition='admitted')`, provider.ID).Scan(&providerSeenBeforeRetry); err != nil {
					return AnalysisAdmissionBinding{}, err
				}
			}
		}
		return prepare(tx, request)
	}
	f.store.executors.entries[analysis.Key] = entry
	manager := &Manager{store: f.store, scans: f.owner, ctx: f.ctx,
		options: ManagerOptions{ReconcileInterval: 250 * time.Millisecond, Logger: slog.Default()}}
	if err := manager.schedule(f.ctx); err != nil {
		t.Fatal(err)
	}
	if startupPrepares != 1 || len(manager.DeferredAnalyses()) != MaxTriggers {
		t.Fatal("one conflicting definition repeatedly prepared its identical startup scope")
	}
	ticks := ScheduleTicksPerSecond
	if _, err := f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{TaskID: provider.ID, Revision: provider.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleInterval, IntervalTicks: &ticks}}}); err != nil {
		t.Fatal(err)
	}
	now, err := f.store.ScheduleClock(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_triggers SET anchor_at=$2,next_fire_at=$3 WHERE task_id=$1`, provider.ID, now.Add(-time.Second), now); err != nil {
		t.Fatal(err)
	}
	manager.nextScheduleAttempt = time.Time{}
	if err := manager.schedule(f.ctx); err != nil {
		t.Fatal(err)
	}
	if startupPrepares != 2 || !providerSeenBeforeRetry || len(manager.DeferredAnalyses()) != MaxTriggers || !manager.Available() {
		t.Fatal("startup backlog ran before another provider or lost its unconsumed rules")
	}
	var consumed int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM task_occurrences WHERE task_id=$1`, analysis.ID).Scan(&consumed); err != nil || consumed != 0 {
		t.Fatal("deferred startup backlog consumed occurrence receipts")
	}
}

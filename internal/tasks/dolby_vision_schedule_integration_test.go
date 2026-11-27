package tasks

import (
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

func TestDolbyVisionScheduleInstallsOnEnableAndPreservesExplicitEmptySchedule(t *testing.T) {
	ctx, pool, owner, _, actor, _ := taskRepository(t, 0)
	key := library.TaskDolbyVisionVerificationKey
	makeStore := func(automatic bool) *Store {
		t.Helper()
		registry, err := NewExecutorRegistry(ExecutorRegistration{Key: key, Name: "Verify Dolby Vision", AutomaticDisabled: !automatic, Executor: libraryBatchTestExecutor{}})
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
		return store
	}
	disabled, err := makeStore(false).GetByKey(ctx, key)
	if err != nil || disabled.Revision != 1 || len(disabled.Triggers) != 0 {
		t.Fatalf("disabled automation acquired a default schedule: %+v, %v", disabled, err)
	}
	store := makeStore(true)
	definition, err := store.GetByKey(ctx, key)
	if err != nil || definition.ID != disabled.ID || definition.Revision != 2 || len(definition.Triggers) != 3 {
		t.Fatalf("enabled automation lost its identity or default schedule: %+v, %v", definition, err)
	}
	event, startup, interval := definition.Triggers[0], definition.Triggers[1], definition.Triggers[2]
	if event.Kind != string(ScheduleSystemEvent) || event.SystemEvent == nil || *event.SystemEvent != string(systemevents.DolbyVisionVerificationRequested) ||
		event.LastEventSequence != 0 || startup.Kind != string(ScheduleStartup) || interval.Kind != string(ScheduleInterval) ||
		interval.AnchorAt == nil || interval.NextFireAt == nil || interval.NextFireAt.Sub(*interval.AnchorAt) != 15*time.Minute ||
		interval.IntervalTicks == nil || *interval.IntervalTicks != 900*ScheduleTicksPerSecond {
		t.Fatalf("default schedule lost its committed request, startup, or periodic recovery: %+v", definition.Triggers)
	}
	for _, trigger := range definition.Triggers {
		if trigger.MaxRuntimeTicks == nil || *trigger.MaxRuntimeTicks != 1200*ScheduleTicksPerSecond {
			t.Fatalf("unbounded or unexpected automatic runtime: %+v", trigger)
		}
	}
	cleared, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{
		TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	actual, err := store.Get(ctx, definition.ID)
	if err != nil || len(actual.Triggers) != 0 || !reflect.DeepEqual(actual, cleared) {
		t.Fatalf("reconciliation replaced a deliberately cleared schedule: %+v, %v", actual, err)
	}
}

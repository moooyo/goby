package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

func TestSystemEventsCommitCoalescingCursorReloadRetirementAndAuthority(t *testing.T) {
	ctx, pool, owner, _, actor, _ := taskRepository(t, 0)
	executor := &genericTestExecutor{available: true}
	registry, err := NewExecutorRegistry(ExecutorRegistration{Key: CacheMaintainKey, Name: "Cache", Global: true, Executor: executor})
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
	definition, err := store.GetByKey(ctx, CacheMaintainKey)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID, Revision: definition.Revision,
		ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleSystemEvent, SystemEvent: systemevents.LibraryChanged}}})
	if err != nil {
		t.Fatal(err)
	}
	emit := func(count int, reject bool) {
		t.Helper()
		sentinel := errors.New("reject source transaction")
		err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
			for range count {
				if err := systemevents.Record(tx.Exec, systemevents.LibraryChanged); err != nil {
					return err
				}
			}
			if reject {
				return sentinel
			}
			return nil
		})
		if reject && !errors.Is(err, sentinel) || !reject && err != nil {
			t.Fatal(err)
		}
	}
	emit(1, true)
	if changed, err := store.DispatchSystemEvents(ctx, 10); err != nil || changed {
		t.Fatal("rolled back source generated task work")
	}
	emit(3, false)
	type dispatchResult struct {
		changed bool
		err     error
	}
	dispatches := make(chan dispatchResult, 2)
	for range 2 {
		go func() {
			changed, err := store.DispatchSystemEvents(ctx, 10)
			dispatches <- dispatchResult{changed, err}
		}()
	}
	admissions := 0
	for range 2 {
		result := <-dispatches
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.changed {
			admissions++
		}
	}
	if admissions != 1 {
		t.Fatal("concurrent dispatch did not consume exactly one committed range")
	}
	var first, last int64
	var runID, disposition string
	if err := pool.QueryRow(ctx, `SELECT first_sequence,last_sequence,run_id,disposition FROM task_system_event_receipts WHERE trigger_id=$1`, saved.Triggers[0].ID).Scan(&first, &last, &runID, &disposition); err != nil {
		t.Fatal(err)
	}
	if first != 1 || last != 3 || disposition != "admitted" {
		t.Fatal("coalescing lost the exact committed sequence range")
	}
	run, err := store.GetRun(ctx, runID)
	if err != nil || run.Source != "system_event" || run.State != RunPending || run.TotalChildren != 1 {
		t.Fatal("event did not admit real executor work")
	}
	emit(1, false)
	if _, err := store.DispatchSystemEvents(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var overlaps int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_system_event_receipts WHERE run_id=$1 AND disposition='overlap' AND first_sequence=4 AND last_sequence=4`, runID).Scan(&overlaps); err != nil || overlaps != 1 {
		t.Fatal("overlap changed the owned run or lost its receipt")
	}
	if stopped, err := store.Stop(ctx, actor, runID); err != nil || stopped.State != RunCancelled {
		t.Fatalf("event run cannot use normal cancellation: %+v %v", stopped, err)
	}
	reloaded, err := New(pool, owner, registry)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := reloaded.DispatchSystemEvents(ctx, 10); err != nil || changed {
		t.Fatal("reloading replayed a consumed or cancelled range")
	}
	emit(1, false)
	saved, err = reloaded.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID, Revision: saved.Revision,
		ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleSystemEvent, SystemEvent: systemevents.LibraryChanged}}})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := reloaded.DispatchSystemEvents(ctx, 10); err != nil || changed {
		t.Fatal("new rule replayed events predating its creation")
	}
	emit(1, false)
	if changed, err := reloaded.DispatchSystemEvents(ctx, 10); err != nil || !changed {
		t.Fatalf("replacement did not consume new events: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT first_sequence,last_sequence FROM task_system_event_receipts WHERE trigger_id=$1`, saved.Triggers[0].ID).Scan(&first, &last); err != nil || first != 6 || last != 6 {
		t.Fatal("retired rule and replacement cursors were confused")
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := reloaded.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID, Revision: saved.Revision, ScheduleTimezone: "UTC"}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("revoked administrator changed event rules: %v", err)
	}
}

func TestSystemEventStartupRetriesAndManagerCancellationUseRealWorker(t *testing.T) {
	ctx, pool, owner, _, actor, _ := taskRepository(t, 0)
	executor := &genericTestExecutor{available: true, started: make(chan struct{}), block: true}
	registry, err := NewExecutorRegistry(ExecutorRegistration{Key: CacheMaintainKey, Name: "Cache", Global: true, Executor: executor})
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
	definition, err := store.GetByKey(ctx, CacheMaintainKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleSystemEvent, SystemEvent: systemevents.ServerStarted}}}); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(store, owner, ManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	select {
	case <-executor.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	page, err := store.ListRuns(ctx, definition.ID, Page{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("startup did not create exactly one run: %v", err)
	}
	run := page.Items[0]
	if _, err := manager.Stop(ctx, actor, run.ID); err != nil {
		t.Fatal(err)
	}
	waitGenericRun(t, ctx, store, run.ID, RunCancelled)
	// Read the lifecycle key from committed storage, not coordinator fields.
	var identityText string
	if err := pool.QueryRow(ctx, `SELECT lifecycle_key FROM task_system_events WHERE name='ServerStarted'`).Scan(&identityText); err != nil {
		t.Fatal(err)
	}
	startup, err := time.Parse(time.RFC3339Nano, identityText)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeSystemEvents(ctx, startup); err != nil {
		t.Fatal(err)
	}
	if changed, err := store.DispatchSystemEvents(ctx, 10); err != nil || changed {
		t.Fatal("startup retry repeated cancelled work")
	}
	if executor.calls.Load() != 1 {
		t.Fatal("system event ran external work more than once")
	}
	if err := manager.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(ctx, actor, StartRequest{TaskID: definition.ID}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed manager admitted work")
	}
}

func TestSystemEventUnavailableExecutorRecordsFailureWithoutFakeSuccess(t *testing.T) {
	ctx, pool, owner, _, actor, _ := taskRepository(t, 0)
	executor := &genericTestExecutor{available: false}
	registry, err := NewExecutorRegistry(ExecutorRegistration{Key: MetadataRefreshKey, Name: "Metadata", Global: true, Executor: executor})
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
	definition, err := store.GetByKey(ctx, MetadataRefreshKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleSystemEvent, SystemEvent: systemevents.ServerStarted}}}); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(store, owner, ManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		current, err := store.Get(ctx, definition.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.LastRun != nil {
			if current.LastRun.State != RunFailed || current.LastRun.UnavailableChildren != 1 || executor.calls.Load() != 0 {
				t.Fatal("unavailable event executor became successful work")
			}
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("unavailable event work did not terminate")
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-tick.C:
		}
	}
}

package tasks

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type genericTestExecutor struct {
	available bool
	started   chan struct{}
	block     bool
	calls     atomic.Int64
}

func (executor *genericTestExecutor) Available() bool { return executor.available }
func (executor *genericTestExecutor) Execute(ctx context.Context, work Work, progress func(Progress) error) error {
	executor.calls.Add(1)
	if work.LibraryID != "" {
		return errors.New("global execution received a library")
	}
	if err := progress(Progress{Processed: 7, Updated: 3}); err != nil {
		return err
	}
	if executor.started != nil {
		close(executor.started)
	}
	if executor.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func waitGenericRun(t *testing.T, ctx context.Context, store *Store, id string, state RunState) Run {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		run, err := store.GetRun(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if run.State == state {
			return run
		}
		select {
		case <-deadline.C:
			t.Fatalf("run state = %s, want %s", run.State, state)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-tick.C:
		}
	}
}

func TestGenericExecutorPersistsProgressReceiptAndCancellation(t *testing.T) {
	for _, cancelWork := range []bool{false, true} {
		name := "complete"
		if cancelWork {
			name = "cancel"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, owner, _, actor, _ := taskRepository(t, 0)
			executor := &genericTestExecutor{available: true, started: make(chan struct{}), block: cancelWork}
			registry, err := NewExecutorRegistry(ExecutorRegistration{Key: CacheMaintainKey, Name: "Maintain cache", Category: "Maintenance", Global: true, Executor: executor})
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
			manager, err := NewManager(store, owner, ManagerOptions{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := manager.Close(closeCtx); err != nil {
					t.Error(err)
				}
			})
			admission, err := manager.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "generic-receipt"})
			if err != nil {
				t.Fatal(err)
			}
			if admission.Run.TotalChildren != 1 {
				t.Fatal("global work needs a real child even without media libraries")
			}
			select {
			case <-executor.started:
			case <-time.After(10 * time.Second):
				t.Fatal("generic executor did not start")
			}
			want := RunCompleted
			if cancelWork {
				if _, err := manager.Stop(ctx, actor, admission.Run.ID); err != nil {
					t.Fatal(err)
				}
				want = RunCancelled
			}
			terminal := waitGenericRun(t, ctx, store, admission.Run.ID, want)
			if terminal.Scanned != 7 || terminal.Updated != 3 || terminal.TerminalChildren != 1 {
				t.Fatal("durable progress was lost")
			}
			replay, err := manager.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "generic-receipt"})
			if err != nil || replay.Admitted || replay.Run.ID != terminal.ID || executor.calls.Load() != 1 {
				t.Fatal("receipt replay repeated external work")
			}
			var scanCount int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM scan_jobs`).Scan(&scanCount); err != nil || scanCount != 0 {
				t.Fatal("generic execution created scanner history")
			}
		})
	}
}

func TestGenericClaimRecoveryInterruptsWithoutRepeatingExternalWork(t *testing.T) {
	ctx, pool, owner, _, actor, _ := taskRepository(t, 0)
	executor := &genericTestExecutor{available: true}
	registry, err := NewExecutorRegistry(ExecutorRegistration{Key: CacheMaintainKey, Name: "Maintain cache", Global: true, Executor: executor})
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
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "recover-generic"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginRun(ctx, admission.Run.ID); err != nil {
		t.Fatal(err)
	}
	children, err := store.ListChildren(ctx, admission.Run.ID, Page{})
	if err != nil || len(children.Items) != 1 {
		t.Fatal("global child missing")
	}
	token, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	childID := children.Items[0].ID
	if _, err := store.claimExecution(ctx, admission.Run.ID, childID, token); err != nil {
		t.Fatal(err)
	}
	if err := store.updateExecutionProgress(ctx, admission.Run.ID, childID, token, Progress{Processed: 4, Added: 6, Updated: 2}); err != nil {
		t.Fatal(err)
	}
	if err := store.updateExecutionProgress(ctx, admission.Run.ID, childID, token, Progress{Processed: 3, Added: 6, Updated: 2}); !errors.Is(err, ErrInconsistent) {
		t.Fatal("progress moved backwards")
	}
	if err := store.RecoverRuns(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.GetRun(ctx, admission.Run.ID)
	if err != nil || recovered.State != RunInterrupted || recovered.TerminalChildren != 1 || recovered.Scanned != 4 || recovered.Added != 6 {
		t.Fatal("abandoned generic claim was not durably interrupted")
	}
	if executor.calls.Load() != 0 {
		t.Fatal("recovery invoked an external executor")
	}
	replay, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "recover-generic"})
	if err != nil || replay.Admitted || replay.Run.ID != recovered.ID {
		t.Fatal("recovery lost the original receipt")
	}
}

func TestUnavailableExecutorRejectsManualAdmissionAndRecordsScheduledFailure(t *testing.T) {
	ctx, pool, owner, _, actor, _ := taskRepository(t, 0)
	executor := &genericTestExecutor{available: false}
	registry, err := NewExecutorRegistry(ExecutorRegistration{Key: MetadataRefreshKey, Name: "Refresh metadata", Executor: executor})
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
	if _, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "unavailable"}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("a missing provider admitted a successful empty manual run")
	}
	if _, err := store.ReplaceTriggers(ctx, actor, ReplaceTriggersRequest{TaskID: definition.ID, Revision: definition.Revision,
		ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleStartup}}}); err != nil {
		t.Fatal(err)
	}
	startup, err := store.ScheduleClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeSchedules(ctx, startup); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeSchedules(ctx, startup); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListRuns(ctx, definition.ID, Page{})
	if err != nil || runs.TotalRecordCount != 1 || runs.Items[0].State != RunFailed || runs.Items[0].UnavailableChildren != 1 || runs.Items[0].Source != "startup" {
		t.Fatal("scheduled unavailable work lost its failure or occurrence receipt")
	}
	if executor.calls.Load() != 0 {
		t.Fatal("an unavailable executor was invoked")
	}
}

type libraryBatchTestExecutor struct{}

func (libraryBatchTestExecutor) Available() bool { return true }
func (libraryBatchTestExecutor) Execute(ctx context.Context, work Work, progress func(Progress) error) error {
	if work.LibraryID == "" {
		return errors.New("library snapshot missing")
	}
	return progress(Progress{Processed: 1})
}

func TestCompletedGenericWorkersAreReapedBeforeWaitingLibraryTail(t *testing.T) {
	ctx, pool, owner, _, actor, _ := taskRepository(t, 20)
	registry, err := NewExecutorRegistry(ExecutorRegistration{Key: MetadataRefreshKey, Name: "Refresh metadata", Executor: libraryBatchTestExecutor{}})
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
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Drive explicit coordination passes so the regression does not rely on
	// machine speed or timer sleeps: the next pass must release both slots.
	manager := &Manager{store: store, scans: owner, ctx: ctx, options: ManagerOptions{BatchSize: MaxPageLimit, MaxConcurrent: func() int { return 2 }},
		childOffsets: map[string]int{}, runtimeDeadlines: map[string]time.Time{}, executions: map[string]*workerExecution{}}
	t.Cleanup(manager.drainExecutions)
	if _, err := manager.reconcile(ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(manager.executions) != 2 {
		t.Fatal("initial executor concurrency was not bounded")
	}
	for _, execution := range manager.executions {
		<-execution.done
	}
	if _, err := manager.reconcile(ctx, false); err != nil {
		t.Fatal(err)
	}
	run, err := store.GetRun(ctx, admission.Run.ID)
	if err != nil || run.CompletedChildren != 2 || len(manager.executions) != 2 {
		t.Fatal("a waiting library tail retained completed workers in its concurrency slots")
	}
}

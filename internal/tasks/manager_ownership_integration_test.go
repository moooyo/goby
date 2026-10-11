package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

func TestManagerIdlePhasesShareOnlyAnExplicitOwnershipProbe(t *testing.T) {
	ctx, _, owner, store, _, _ := taskRepository(t, 0)
	scans := &managerOwnershipScans{ScanExecutor: owner}
	manager := newManagerOwnershipFixture(t, ctx, store, scans)
	for cycle := int64(1); cycle <= 3; cycle++ {
		manager.nextScheduleAttempt = time.Time{}
		scheduleCtx, stopSchedule := context.WithTimeout(manager.ctx, managerCycleTimeout)
		probed, err := manager.schedulePass(scheduleCtx)
		stopSchedule()
		if err != nil || !probed {
			t.Fatalf("idle schedule did not make an explicit observation: probed=%t error=%v", probed, err)
		}
		reconcileCtx, stopReconcile := context.WithTimeout(manager.ctx, managerCycleTimeout)
		idle, err := manager.reconcilePass(reconcileCtx, false, probed)
		stopReconcile()
		if err != nil || !idle || scans.checks.Load() != cycle {
			t.Fatalf("idle cycle repeated or omitted its owner probe: cycle=%d checks=%d idle=%t error=%v", cycle, scans.checks.Load(), idle, err)
		}
	}
	manager.nextScheduleAttempt = time.Now().Add(time.Hour)
	probed, err := manager.schedulePass(manager.ctx)
	if err != nil || probed || scans.checks.Load() != 3 {
		t.Fatalf("backoff changed the idle observation: probed=%t checks=%d error=%v", probed, scans.checks.Load(), err)
	}
	if idle, err := manager.reconcilePass(manager.ctx, false, probed); err != nil || !idle || scans.checks.Load() != 4 {
		t.Fatalf("backoff skipped idle ownership detection: checks=%d idle=%t error=%v", scans.checks.Load(), idle, err)
	}
	if idle, err := manager.reconcilePass(manager.ctx, true, true); err != nil || !idle || scans.checks.Load() != 5 {
		t.Fatalf("idle shutdown reused a schedule probe: checks=%d idle=%t error=%v", scans.checks.Load(), idle, err)
	}
}

func TestManagerScheduleFailureRetainsAnIndependentActiveReconciliation(t *testing.T) {
	ctx, pool, owner, store, actor, definition := taskRepository(t, 1)
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	scans := &managerOwnershipScans{ScanExecutor: owner, admit: func(context.Context, string) (library.ScanAdmission, error) {
		return library.ScanAdmission{Kind: library.ScanQueueFull}, nil
	}}
	manager := newManagerOwnershipFixture(t, ctx, store, scans)
	scheduleCtx, stopSchedule := context.WithCancel(manager.ctx)
	defer stopSchedule()
	observed := &dispatchObservedTransactions{owner: owner}
	observed.before = func(current context.Context) error {
		if current != scheduleCtx {
			t.Fatal("schedule initialization lost its own context")
		}
		stopSchedule()
		return current.Err()
	}
	manager.store, err = New(pool, observed)
	if err != nil {
		t.Fatal(err)
	}
	probed, err := manager.schedulePass(scheduleCtx)
	manager.observeFailure(err)
	if !errors.Is(err, context.Canceled) || !probed || manager.ctx.Err() != nil || manager.Available() {
		t.Fatalf("schedule failure lost the explicit probe or fenced manual work: probed=%t error=%v", probed, err)
	}
	if manager.nextScheduleAttempt.IsZero() || manager.schedulesInitialized {
		t.Fatal("failed initialization lost its retry state")
	}
	reconcileCtx, stopReconcile := context.WithTimeout(manager.ctx, managerCycleTimeout)
	defer stopReconcile()
	observed.before = func(current context.Context) error {
		if current != reconcileCtx || current.Err() != nil {
			return errors.New("reconciliation reused the failed schedule context")
		}
		if _, bounded := current.Deadline(); !bounded {
			return errors.New("reconciliation lost its independent database budget")
		}
		return nil
	}
	if idle, err := manager.reconcilePass(reconcileCtx, false, probed); err != nil || idle || scans.checks.Load() != 2 {
		t.Fatalf("active work skipped its fresh probe or independent retry: idle=%t checks=%d error=%v", idle, scans.checks.Load(), err)
	}
	run, err := store.GetRun(ctx, admission.Run.ID)
	if err != nil || run.State != RunRunning || observed.calls < 2 || manager.Available() {
		t.Fatalf("manual work did not advance while initialization remained unavailable: run=%+v calls=%d error=%v", run, observed.calls, err)
	}
}

func TestManagerIdleOwnerLossStillFencesTheCoordinator(t *testing.T) {
	f := newManagerFixture(t)
	scans := &managerOwnershipScans{ScanExecutor: f.catalog}
	manager, err := NewManager(f.store, scans, ManagerOptions{ReconcileInterval: 250 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	f.managers = append(f.managers, manager)
	managerWait(t, f.ctx, "idle scheduler readiness", func(context.Context) bool { return manager.Available() })
	var ownerPID int32
	if err := f.catalog.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error {
		return tx.QueryRow("SELECT pg_backend_pid()").Scan(&ownerPID)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, "SELECT pg_terminate_backend($1)", ownerPID); err != nil {
		t.Fatal(err)
	}
	managerWait(t, f.ctx, "idle ownership fence", func(context.Context) bool {
		return !f.catalog.Available() && !manager.Available()
	})
	closeCtx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	if err := manager.Close(closeCtx); !errors.Is(err, library.ErrUnavailable) || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("idle coordinator did not retain its ownership failure: %v", err)
	}
}

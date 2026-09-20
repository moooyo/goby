//go:build linux

package transcode

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagerCapturesExecutionBeforeQueueAndRetainsItAtLaunch(t *testing.T) {
	type invocation struct {
		plan    Plan
		threads int
	}
	started := make(chan invocation, 4)
	run := func(ctx context.Context, _, _ string, _ *os.File, plan Plan, threads int, _ func(Progress)) (RunResult, error) {
		started <- invocation{plan, threads}
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}
	options := managerTestOptions(t, run)
	options.Threads, options.MaxJobs = 11, 1
	manager := newTestManager(t, options)
	await := func() invocation {
		t.Helper()
		select {
		case got := <-started:
			return got
		case <-time.After(5 * time.Second):
			t.Fatal("captured job did not launch")
			return invocation{}
		}
	}
	first := managerTestSpec(1)
	first.Plan, _ = CaptureExecution(first.Plan, DefaultExecutionOptions(3))
	if _, err := manager.Ensure(context.Background(), first, managerTestInput(t)); err != nil {
		t.Fatal(err)
	}
	if got := await(); got.threads != 3 || got.plan != first.Plan {
		t.Fatal("running job used manager defaults instead of admitted execution")
	}
	second := managerTestSpec(2)
	second.Plan, _ = CaptureExecution(second.Plan, DefaultExecutionOptions(7))
	queued, err := manager.Ensure(context.Background(), second, managerTestInput(t))
	if err != nil || queued.Spec.Plan != second.Plan || queued.State != "queued" {
		t.Fatalf("queued record did not capture execution: %v", err)
	}
	// Changing the caller's settings object cannot alter the queued value.
	second.Plan.Execution.Threads = 9
	if err := manager.Cancel(context.Background(), first.Scope); err != nil {
		t.Fatal(err)
	}
	if got := await(); got.threads != 7 || got.plan.Execution.Threads != 7 {
		t.Fatal("queued launch lost its immutable execution settings")
	}
	if err := manager.Cancel(context.Background(), second.Scope); err != nil {
		t.Fatal(err)
	}
	legacy := managerTestSpec(3)
	created, err := manager.Ensure(context.Background(), legacy, managerTestInput(t))
	if err != nil || created.Spec.Plan.ExecutionVersion != ExecutionVersion || created.Spec.Plan.Execution.Threads != 11 {
		t.Fatalf("new legacy caller admission was not explicitly captured: %v", err)
	}
	if got := await(); got.threads != 11 || got.plan.Execution.Threads != 11 {
		t.Fatal("legacy admission did not preserve its captured default")
	}
}

func TestManagerRechecksCapturedHardwareAfterQueueBeforeRunner(t *testing.T) {
	var calls atomic.Int32
	started := make(chan int64, 1)
	run := func(ctx context.Context, _, _ string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		calls.Add(1)
		started <- 1
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}
	options := managerTestOptions(t, run)
	options.MaxJobs = 1
	var available atomic.Bool
	available.Store(true)
	var checked atomic.Int32
	options.ValidateHardware = func(ctx context.Context, plan Plan) error {
		checked.Add(1)
		if _, ok := ctx.Deadline(); !ok || plan.ExecutionVersion != ExecutionVersion {
			return errors.New("hardware validation lacks a bound or an execution snapshot")
		}
		if !available.Load() {
			return errors.New("private device identity changed")
		}
		return nil
	}
	manager := newTestManager(t, options)
	first := managerTestSpec(1)
	if _, err := manager.Ensure(context.Background(), first, managerTestInput(t)); err != nil {
		t.Fatal(err)
	}
	managerTestWaitStart(t, started)
	second := managerTestSpec(2)
	input := managerTestInput(t)
	queued, err := manager.Ensure(context.Background(), second, input)
	if err != nil || queued.State != "queued" {
		t.Fatalf("second job did not wait for hardware revalidation: %v", err)
	}
	if checked.Load() != 1 {
		t.Fatal("queued job's execution check ran before its wait ended")
	}
	available.Store(false)
	if err := manager.Cancel(context.Background(), first.Scope); err != nil {
		t.Fatal(err)
	}
	finished := managerTestWaitFinished(t, manager, queued.ID)
	if calls.Load() != 1 || checked.Load() != 2 || finished.State != "failed" || finished.ErrorCode != "hardware_unavailable" {
		t.Fatal("hardware replacement reached the runner or leaked a private error")
	}
	assertManagerInputClosed(t, input)
}

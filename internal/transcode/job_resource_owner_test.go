package transcode

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecutionSlotPoolBoundAndOnceOnlyRelease(t *testing.T) {
	for _, capacity := range []int{-1, 0, maxExecutionSlots + 1} {
		if pool, err := newExecutionSlotPool(capacity, nil); pool != nil || !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("invalid execution capacity %d was accepted: pool=%v error=%v", capacity, pool, err)
		}
	}
	var notifications atomic.Int32
	var pool *executionSlotPool
	pool = jobOwnerTestExecutionPool(t, 1, func() {
		notifications.Add(1)
		if snapshot := pool.snapshot(); snapshot.Used != 0 {
			t.Errorf("execution notification preceded allowance return: %+v", snapshot)
		}
	})
	slot, err := pool.reserve()
	if err != nil {
		t.Fatal(err)
	}
	if rejected, err := pool.reserve(); rejected != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("execution bound admitted an extra slot: slot=%v error=%v", rejected, err)
	}
	released := make(chan bool, 1)
	go func() { released <- slot.release() }()
	if !jobOwnerTestReceive(t, released, "execution release outside pool lock") || slot.release() {
		t.Fatal("execution slot was not returned exactly once")
	}
	if snapshot := pool.snapshot(); snapshot.Used != 0 || snapshot.Available != 1 || notifications.Load() != 1 {
		t.Fatalf("execution allowance or notification was duplicated: %+v notifications=%d", snapshot, notifications.Load())
	}
	reserved, err := pool.reserve()
	if err != nil {
		t.Fatal(err)
	}
	pool.close()
	if rejected, err := pool.reserve(); rejected != nil || !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("closed execution pool admitted work: slot=%v error=%v", rejected, err)
	}
	if !reserved.release() || notifications.Load() != 1 {
		t.Fatal("shutdown either lost its existing slot or notified new admission")
	}
}

func TestJobResourceOwnerReservesPrivateHandlesAndRollsBackOnce(t *testing.T) {
	executions := jobOwnerTestExecutionPool(t, 2, nil)
	completions := jobOwnerTestCompletionPool(t, 2, nil)
	var notifications atomic.Int32
	var first *jobResourceOwner
	first = jobOwnerTestReserve(t, executions, completions, &resourceLifecycle{}, jobRetireAfterAccounting, func() {
		notifications.Add(1)
		first.snapshot()
		executions.snapshot()
		completions.snapshot()
	})
	second := jobOwnerTestReserve(t, executions, completions, &resourceLifecycle{}, jobRetireAfterAccounting, nil)
	if first.execution == second.execution || first.completion == second.completion {
		t.Fatal("independent admissions shared a return authority")
	}
	var rollbacks atomic.Int32
	var contenders sync.WaitGroup
	for range 32 {
		contenders.Add(1)
		go func() {
			defer contenders.Done()
			if first.rollbackBeforeLaunch() {
				rollbacks.Add(1)
			}
		}()
	}
	joined := make(chan struct{})
	go func() { contenders.Wait(); close(joined) }()
	jobOwnerTestReceive(t, joined, "concurrent prelaunch rollback")
	if rollbacks.Load() != 1 || notifications.Load() != 1 {
		t.Fatalf("prelaunch ownership was returned repeatedly: rollbacks=%d notifications=%d", rollbacks.Load(), notifications.Load())
	}
	if snapshot := first.snapshot(); !snapshot.RolledBack || !snapshot.ExecutionReturned || snapshot.TerminalHandled || snapshot.Started {
		t.Fatalf("prelaunch rollback became a terminal or execution transition: %+v", snapshot)
	}
	if executions.snapshot().Used != 1 || completions.snapshot().Reserved != 1 {
		t.Fatal("one rollback returned another admission's execution or completion allowance")
	}
	if first.startedExecution() || first.rollbackBeforeLaunch() || !second.startedExecution() || second.rollbackBeforeLaunch() {
		t.Fatal("rollback admitted a later launch or discarded a started owner")
	}
	if executions.snapshot().Used != 1 || completions.snapshot().Reserved != 1 {
		t.Fatal("rejected postlaunch rollback changed resource counts")
	}
}

func TestJobResourceOwnerCompletionFailureReturnsOnlyNewExecution(t *testing.T) {
	for _, closed := range []bool{false, true} {
		name := "busy"
		want := ErrBusy
		if closed {
			name, want = "closed", ErrManagerClosed
		}
		t.Run(name, func(t *testing.T) {
			var releases, ownershipNotifications atomic.Int32
			var executions *executionSlotPool
			executions = jobOwnerTestExecutionPool(t, 1, func() {
				releases.Add(1)
				if executions.snapshot().Used != 0 {
					t.Error("failed completion reservation retained execution capacity")
				}
			})
			completions := jobOwnerTestCompletionPool(t, 1, nil)
			held, err := completions.reserve()
			if err != nil {
				t.Fatal(err)
			}
			if closed {
				completions.close()
			}
			type admissionResult struct {
				owner *jobResourceOwner
				err   error
			}
			result := make(chan admissionResult, 1)
			go func() {
				owner, err := reserveJobResourceOwner(executions, completions, &resourceLifecycle{}, jobRetireAfterAccounting, func() {
					ownershipNotifications.Add(1)
				})
				result <- admissionResult{owner: owner, err: err}
			}()
			got := jobOwnerTestReceive(t, result, "completion admission rollback")
			if got.owner != nil || !errors.Is(got.err, want) {
				t.Fatalf("completion failure was hidden: owner=%v error=%v", got.owner, got.err)
			}
			if executions.snapshot().Used != 0 || releases.Load() != 1 || ownershipNotifications.Load() != 0 {
				t.Fatalf("failed admission returned wrong ownership: execution=%+v releases=%d ownership=%d", executions.snapshot(), releases.Load(), ownershipNotifications.Load())
			}
			if snapshot := completions.snapshot(); snapshot.Reserved != 1 || snapshot.Available != 0 {
				t.Fatalf("failed admission returned an existing completion reservation: %+v", snapshot)
			}
			if !held.release() || held.release() {
				t.Fatal("existing completion reservation lost its return authority")
			}
		})
	}
}

func TestJobResourceOwnerExecutionFailureDoesNotReserveCompletion(t *testing.T) {
	executions := jobOwnerTestExecutionPool(t, 1, nil)
	completions := jobOwnerTestCompletionPool(t, 2, nil)
	held, err := executions.reserve()
	if err != nil {
		t.Fatal(err)
	}
	if owner, err := reserveJobResourceOwner(executions, completions, &resourceLifecycle{}, jobRetireAfterAccounting, nil); owner != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("execution bound admitted an owner: owner=%v error=%v", owner, err)
	}
	if snapshot := completions.snapshot(); snapshot.Reserved != 0 || snapshot.Available != 2 {
		t.Fatalf("failed execution admission consumed a completion ticket: %+v", snapshot)
	}
	if !held.release() {
		t.Fatal("held execution reservation could not be returned")
	}
}

func TestJobResourceOwnerRejectsInvalidAdmissionWithoutSideEffects(t *testing.T) {
	executions := jobOwnerTestExecutionPool(t, 1, nil)
	completions := jobOwnerTestCompletionPool(t, 1, nil)
	for _, invalid := range []struct {
		executions  *executionSlotPool
		completions *completionTicketPool
		lifecycle   *resourceLifecycle
		policy      jobRetirementPolicy
	}{
		{nil, completions, &resourceLifecycle{}, jobRetireAfterAccounting},
		{executions, nil, &resourceLifecycle{}, jobRetireAfterAccounting},
		{executions, completions, nil, jobRetireAfterAccounting},
		{executions, completions, &resourceLifecycle{}, jobRetireLiveVerified + 1},
	} {
		if owner, err := reserveJobResourceOwner(invalid.executions, invalid.completions, invalid.lifecycle, invalid.policy, nil); owner != nil || !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("invalid admission was accepted: owner=%v error=%v", owner, err)
		}
	}
	if executions.snapshot().Used != 0 || completions.snapshot().Reserved != 0 {
		t.Fatal("invalid admission reserved resources")
	}
}

func TestJobResourceOwnerFiniteRetirementRequiresExactOwnedBarrier(t *testing.T) {
	executions := jobOwnerTestExecutionPool(t, 1, nil)
	completions := jobOwnerTestCompletionPool(t, 1, nil)
	var ownershipNotifications atomic.Int32
	lifecycle := &resourceLifecycle{retire: func(context.Context) error { return nil }}
	owner := jobOwnerTestReserve(t, executions, completions, lifecycle, jobRetireFiniteVerified, func() {
		ownershipNotifications.Add(1)
	})
	foreign := resourceLifecycleEvent{Stage: resourceProcessRetired, At: time.Now(), Verified: true}
	if owner.observeRetirement(foreign) || executions.snapshot().Used != 1 {
		t.Fatal("an event without this job's actual barrier returned execution capacity")
	}
	if !owner.startedExecution() || owner.startedExecution() {
		t.Fatal("execution was not started exactly once")
	}
	if err := lifecycle.retireProcesses(context.Background()); err != nil {
		t.Fatal(err)
	}
	at := lifecycle.snapshot().ProcessRetired
	for _, rejected := range []resourceLifecycleEvent{
		{Stage: resourceWritersDrained, At: at, Verified: true},
		{Stage: resourceProcessRetired, At: at, Verified: false},
		{Stage: resourceProcessRetired, Verified: true},
		{Stage: resourceProcessRetired, At: at.Add(time.Nanosecond), Verified: true},
	} {
		if owner.observeRetirement(rejected) {
			t.Fatalf("unverified or mismatched retirement returned capacity: %+v", rejected)
		}
	}
	actual := resourceLifecycleEvent{Stage: resourceProcessRetired, At: at, Verified: true}
	if !owner.observeRetirement(actual) || owner.observeRetirement(actual) {
		t.Fatal("actual finite retirement did not return execution capacity exactly once")
	}
	if executions.snapshot().Used != 0 || completions.snapshot().Reserved != 1 || ownershipNotifications.Load() != 0 {
		t.Fatal("process retirement returned completion or unfinished user ownership")
	}
	if snapshot := owner.snapshot(); !snapshot.ExecutionReturned || snapshot.RunnerReturned || snapshot.FinalizationQueued || snapshot.TerminalHandled {
		t.Fatalf("early process retirement invented a later lifetime boundary: %+v", snapshot)
	}
	if err := owner.returned(); !errors.Is(err, errJobResourceOwnership) {
		t.Fatalf("process retirement authorized finalization before writer drain: %v", err)
	}
	if err := lifecycle.runnerReturned(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := owner.returned(); err != nil {
		t.Fatal(err)
	}
	if executions.snapshot().Used != 0 || completions.snapshot().Reserved != 1 || ownershipNotifications.Load() != 0 {
		t.Fatal("full runner return duplicated early execution or unfinished ownership release")
	}
}

func TestJobResourceOwnerRetirementCallbackRunsOutsideOwnerLocks(t *testing.T) {
	var owner *jobResourceOwner
	var notifications atomic.Int32
	executions := jobOwnerTestExecutionPool(t, 1, func() {
		notifications.Add(1)
		owner.snapshot()
	})
	completions := jobOwnerTestCompletionPool(t, 1, nil)
	lifecycle := &resourceLifecycle{retire: func(context.Context) error { return nil }}
	lifecycle.onStage = func(event resourceLifecycleEvent) { owner.observeRetirement(event) }
	owner = jobOwnerTestReserve(t, executions, completions, lifecycle, jobRetireFiniteVerified, nil)
	if !owner.startedExecution() {
		t.Fatal("owner could not start execution")
	}
	retired := make(chan error, 1)
	go func() { retired <- lifecycle.retireProcesses(context.Background()) }()
	if err := jobOwnerTestReceive(t, retired, "owned retirement callback"); err != nil {
		t.Fatal(err)
	}
	if notifications.Load() != 1 || executions.snapshot().Used != 0 || completions.snapshot().Reserved != 1 {
		t.Fatal("actual lifecycle callback did not return only execution capacity")
	}
}

func TestJobResourceOwnerRejectsStartAfterOwnedLifecycleRetirement(t *testing.T) {
	for _, stage := range []string{"verified", "unverified", "failed", "drained"} {
		t.Run(stage, func(t *testing.T) {
			executions := jobOwnerTestExecutionPool(t, 1, nil)
			completions := jobOwnerTestCompletionPool(t, 1, nil)
			lifecycle := &resourceLifecycle{}
			switch stage {
			case "verified":
				lifecycle.retire = func(context.Context) error { return nil }
			case "failed":
				lifecycle.retire = func(context.Context) error { return errors.New("domain still populated") }
			}
			owner := jobOwnerTestReserve(t, executions, completions, lifecycle, jobRetireFiniteVerified, nil)
			switch stage {
			case "unverified":
				lifecycle.record(resourceProcessRetired, false)
			case "drained":
				lifecycle.record(resourceWritersDrained, false)
			default:
				lifecycle.retireProcesses(context.Background())
			}
			if owner.startedExecution() {
				t.Fatal("retired lifecycle admitted a subsequent execution launch")
			}
			snapshot := lifecycle.snapshot()
			if owner.observeRetirement(resourceLifecycleEvent{Stage: resourceProcessRetired, At: snapshot.ProcessRetired, Verified: snapshot.RetirementVerified}) {
				t.Fatal("replaying a prelaunch retirement returned a new execution allowance")
			}
			if executions.snapshot().Used != 1 || completions.snapshot().Reserved != 1 || owner.snapshot().Started {
				t.Fatal("stale lifecycle proof changed admission ownership")
			}
			if !owner.rollbackBeforeLaunch() {
				t.Fatal("rejected prelaunch execution lost its rollback authority")
			}
		})
	}
}

func TestJobResourceOwnerFailedBarrierRetainsAllOwnership(t *testing.T) {
	for _, policy := range []jobRetirementPolicy{jobRetireFiniteVerified, jobRetireLiveVerified} {
		t.Run(map[jobRetirementPolicy]string{jobRetireFiniteVerified: "finite", jobRetireLiveVerified: "live"}[policy], func(t *testing.T) {
			executions := jobOwnerTestExecutionPool(t, 1, nil)
			completions := jobOwnerTestCompletionPool(t, 1, nil)
			executor := jobOwnerTestExecutor(t, completions)
			failure := errors.New("domain could not be retired")
			lifecycle := &resourceLifecycle{retire: func(context.Context) error { return failure }}
			var notifications atomic.Int32
			owner := jobOwnerTestReserve(t, executions, completions, lifecycle, policy, func() { notifications.Add(1) })
			if !owner.startedExecution() {
				t.Fatal("owner could not start execution")
			}
			if err := lifecycle.runnerReturned(context.Background()); !errors.Is(err, failure) {
				t.Fatalf("failed barrier was hidden by writer drain: %v", err)
			}
			if owner.observeRetirement(resourceLifecycleEvent{Stage: resourceProcessRetired, At: time.Now(), Verified: true}) {
				t.Fatal("synthetic success overrode a failed actual barrier")
			}
			if err := owner.returned(); !errors.Is(err, errJobResourceOwnership) {
				t.Fatalf("failed barrier permitted runner handoff: %v", err)
			}
			if err := owner.submitFinalization(executor, jobOwnerTestTask(owner)); !errors.Is(err, errJobResourceOwnership) {
				t.Fatalf("failed barrier submitted finalization: %v", err)
			}
			if !errors.Is(owner.accountingComplete(), errJobResourceOwnership) || !errors.Is(owner.terminalComplete(), errJobResourceOwnership) || owner.rollbackBeforeLaunch() {
				t.Fatal("failed retirement escaped through accounting, terminal handling or prelaunch rollback")
			}
			if snapshot := owner.snapshot(); snapshot.RunnerReturned || snapshot.ExecutionReturned || snapshot.FinalizationQueued || snapshot.TerminalHandled || snapshot.RolledBack {
				t.Fatalf("failed barrier discarded owner state: %+v", snapshot)
			}
			if executions.snapshot().Used != 1 || completions.snapshot().Reserved != 1 || notifications.Load() != 0 {
				t.Fatal("failed cleanup returned an execution, completion or user allowance")
			}
		})
	}
}

func TestJobResourceOwnerUnverifiedReturnRetainsEnforcedOwnership(t *testing.T) {
	for _, policy := range []jobRetirementPolicy{jobRetireFiniteVerified, jobRetireLiveVerified} {
		t.Run(map[jobRetirementPolicy]string{jobRetireFiniteVerified: "finite", jobRetireLiveVerified: "live"}[policy], func(t *testing.T) {
			executions := jobOwnerTestExecutionPool(t, 1, nil)
			completions := jobOwnerTestCompletionPool(t, 1, nil)
			lifecycle := &resourceLifecycle{}
			owner := jobOwnerTestReserve(t, executions, completions, lifecycle, policy, nil)
			if !owner.startedExecution() {
				t.Fatal("owner could not start execution")
			}
			if err := lifecycle.runnerReturned(context.Background()); err != nil {
				t.Fatal(err)
			}
			at := lifecycle.snapshot().ProcessRetired
			if owner.observeRetirement(resourceLifecycleEvent{Stage: resourceProcessRetired, At: at, Verified: true}) {
				t.Fatal("event verification flag upgraded an unverified owned lifecycle")
			}
			if err := owner.returned(); !errors.Is(err, errJobResourceOwnership) {
				t.Fatalf("unverified Run return satisfied the enforced barrier: %v", err)
			}
			if executions.snapshot().Used != 1 || completions.snapshot().Reserved != 1 || owner.snapshot().RunnerReturned {
				t.Fatal("unverified return released enforced resource ownership")
			}
		})
	}
}

func TestJobResourceOwnerLiveRetirementWaitsForFullRunnerDrain(t *testing.T) {
	executions := jobOwnerTestExecutionPool(t, 1, nil)
	completions := jobOwnerTestCompletionPool(t, 1, nil)
	lifecycle := &resourceLifecycle{retire: func(context.Context) error { return nil }}
	owner := jobOwnerTestReserve(t, executions, completions, lifecycle, jobRetireLiveVerified, nil)
	if !owner.startedExecution() {
		t.Fatal("owner could not start execution")
	}
	if err := lifecycle.retireProcesses(context.Background()); err != nil {
		t.Fatal(err)
	}
	if owner.observeRetirement(resourceLifecycleEvent{Stage: resourceProcessRetired, At: lifecycle.snapshot().ProcessRetired, Verified: true}) {
		t.Fatal("live process observation returned capacity before observer drain")
	}
	if err := owner.returned(); !errors.Is(err, errJobResourceOwnership) || executions.snapshot().Used != 1 || owner.snapshot().RunnerReturned {
		t.Fatalf("live handoff omitted full writer drain: error=%v owner=%+v", err, owner.snapshot())
	}
	if err := lifecycle.runnerReturned(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := owner.returned(); err != nil {
		t.Fatal(err)
	}
	if err := owner.returned(); !errors.Is(err, errJobResourceOwnership) {
		t.Fatalf("duplicate live runner return was accepted: %v", err)
	}
	if executions.snapshot().Used != 0 || completions.snapshot().Reserved != 1 {
		t.Fatal("live drain failed to return only execution capacity")
	}
}

func TestJobResourceOwnerLegacySeparatesAccountingAndTerminalOwnership(t *testing.T) {
	executions := jobOwnerTestExecutionPool(t, 1, nil)
	var owner *jobResourceOwner
	var ownershipNotifications, completionNotifications atomic.Int32
	completionReleased := make(chan struct{}, 1)
	completions := jobOwnerTestCompletionPool(t, 1, func() {
		completionNotifications.Add(1)
		if snapshot := owner.snapshot(); !snapshot.TerminalHandled || ownershipNotifications.Load() != 1 {
			t.Errorf("completion ticket returned before terminal ownership: %+v", snapshot)
		}
		completionReleased <- struct{}{}
	})
	executor := jobOwnerTestExecutor(t, completions)
	lifecycle := &resourceLifecycle{}
	owner = jobOwnerTestReserve(t, executions, completions, lifecycle, jobRetireAfterAccounting, func() {
		ownershipNotifications.Add(1)
		owner.snapshot()
		completions.snapshot()
		executions.snapshot()
	})
	if !owner.startedExecution() {
		t.Fatal("owner could not start execution")
	}
	if err := lifecycle.runnerReturned(context.Background()); err != nil {
		t.Fatal(err)
	}
	if owner.observeRetirement(resourceLifecycleEvent{Stage: resourceProcessRetired, At: lifecycle.snapshot().ProcessRetired, Verified: true}) {
		t.Fatal("legacy retirement invented an enforced barrier")
	}
	if err := owner.returned(); err != nil {
		t.Fatal(err)
	}
	if executions.snapshot().Used != 1 || !errors.Is(owner.accountingComplete(), errJobResourceOwnership) {
		t.Fatal("legacy runner return or unqueued accounting released execution capacity")
	}
	finalizeEntered, terminalEntered := make(chan struct{}), make(chan struct{})
	allowFinalize, allowTerminal := make(chan struct{}), make(chan struct{})
	var finalizeOnce, terminalOnce sync.Once
	releaseFinalize := func() { finalizeOnce.Do(func() { close(allowFinalize) }) }
	releaseTerminal := func() { terminalOnce.Do(func() { close(allowTerminal) }) }
	t.Cleanup(func() { releaseFinalize(); releaseTerminal() })
	task := finalizationTask{
		Finalize: func(context.Context) error {
			close(finalizeEntered)
			<-allowFinalize
			return owner.accountingComplete()
		},
		Terminal: func(_ context.Context, finalizeErr error) error {
			if finalizeErr != nil {
				return finalizeErr
			}
			close(terminalEntered)
			<-allowTerminal
			return owner.terminalComplete()
		},
	}
	if err := owner.submitFinalization(executor, task); err != nil {
		t.Fatal(err)
	}
	jobOwnerTestReceive(t, finalizeEntered, "legacy finalizer entry")
	if executions.snapshot().Used != 1 || completions.snapshot().Working != 1 || ownershipNotifications.Load() != 0 {
		t.Fatal("active legacy finalization omitted its execution or completion reservation")
	}
	releaseFinalize()
	jobOwnerTestReceive(t, terminalEntered, "legacy terminal entry")
	if executions.snapshot().Used != 0 || completions.snapshot().Working != 1 || ownershipNotifications.Load() != 0 {
		t.Fatal("accounting either retained execution or returned unfinished terminal ownership")
	}
	if snapshot := owner.snapshot(); !snapshot.AccountingHandled || !snapshot.ExecutionReturned || snapshot.TerminalHandled {
		t.Fatalf("legacy accounting did not preserve the terminal boundary: %+v", snapshot)
	}
	if next, err := reserveJobResourceOwner(executions, completions, &resourceLifecycle{}, jobRetireAfterAccounting, nil); next != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("execution release bypassed the unfinished completion bound: owner=%v error=%v", next, err)
	}
	if executions.snapshot().Used != 0 || completions.snapshot().Working != 1 {
		t.Fatal("failed next admission consumed capacity or returned the active completion ticket")
	}
	releaseTerminal()
	jobOwnerTestReceive(t, completionReleased, "completion return after terminal handling")
	if err := jobOwnerTestStopAndWait(t, executor); err != nil {
		t.Fatal(err)
	}
	if ownershipNotifications.Load() != 1 || completionNotifications.Load() != 1 || completions.snapshot().Available != 1 || !owner.snapshot().TerminalHandled {
		t.Fatal("terminal handling did not return unfinished ownership and its completion ticket exactly once")
	}
	if !errors.Is(owner.accountingComplete(), errJobResourceOwnership) || !errors.Is(owner.terminalComplete(), errJobResourceOwnership) || owner.rollbackBeforeLaunch() {
		t.Fatal("terminal owner permitted a second accounting, notification or rollback")
	}
}

func TestJobResourceOwnerRejectedFinalizationKeepsOwnedRetry(t *testing.T) {
	executions := jobOwnerTestExecutionPool(t, 1, nil)
	completions := jobOwnerTestCompletionPool(t, 1, nil)
	executor := jobOwnerTestExecutor(t, completions)
	lifecycle := &resourceLifecycle{}
	owner := jobOwnerTestReserve(t, executions, completions, lifecycle, jobRetireAfterAccounting, nil)
	if !owner.startedExecution() {
		t.Fatal("owner could not start execution")
	}
	if err := lifecycle.runnerReturned(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := owner.returned(); err != nil {
		t.Fatal(err)
	}
	if err := owner.submitFinalization(executor, finalizationTask{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("invalid finalizer was accepted: %v", err)
	}
	foreign := jobOwnerTestExecutor(t, jobOwnerTestCompletionPool(t, 1, nil))
	if err := owner.submitFinalization(foreign, jobOwnerTestTask(owner)); !errors.Is(err, errFinalizationTicket) {
		t.Fatalf("foreign executor accepted this owner's original ticket: %v", err)
	}
	if owner.snapshot().FinalizationQueued || completions.snapshot().Reserved != 1 || executions.snapshot().Used != 1 {
		t.Fatal("rejected handoff lost its original retry reservation")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	task := jobOwnerTestTask(owner)
	task.Finalize = func(context.Context) error {
		close(entered)
		<-release
		return owner.accountingComplete()
	}
	if err := owner.submitFinalization(executor, task); err != nil {
		t.Fatalf("owned handoff could not retry after rejection: %v", err)
	}
	jobOwnerTestReceive(t, entered, "retried finalizer entry")
	if err := owner.submitFinalization(executor, jobOwnerTestTask(owner)); !errors.Is(err, errJobResourceOwnership) {
		t.Fatalf("one owner submitted finalization twice: %v", err)
	}
	unblock()
	if err := jobOwnerTestStopAndWait(t, executor); err != nil {
		t.Fatal(err)
	}
	if !owner.snapshot().TerminalHandled || completions.snapshot().Available != 1 || executions.snapshot().Used != 0 {
		t.Fatal("retried owned handoff did not complete its original reservations")
	}
}

func TestJobResourceOwnerAbnormalOwnershipNotificationIsQuarantined(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			executions := jobOwnerTestExecutionPool(t, 1, nil)
			completions := jobOwnerTestCompletionPool(t, 1, nil)
			executor := jobOwnerTestExecutor(t, completions)
			lifecycle := &resourceLifecycle{retire: func(context.Context) error { return nil }}
			var owner *jobResourceOwner
			var notifications atomic.Int32
			owner = jobOwnerTestReserve(t, executions, completions, lifecycle, jobRetireFiniteVerified, func() {
				notifications.Add(1)
				owner.snapshot()
				if mode == "goexit" {
					runtime.Goexit()
				}
				panic("ownership notification did not return")
			})
			if !owner.startedExecution() {
				t.Fatal("owner could not start execution")
			}
			if err := lifecycle.runnerReturned(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := owner.returned(); err != nil {
				t.Fatal(err)
			}
			quarantined := make(chan error, 1)
			task := jobOwnerTestTask(owner)
			task.OnQuarantined = func(err error) {
				owner.snapshot()
				executor.snapshot()
				quarantined <- err
			}
			if err := owner.submitFinalization(executor, task); err != nil {
				t.Fatal(err)
			}
			if err := jobOwnerTestReceive(t, quarantined, "abnormal ownership quarantine"); err == nil {
				t.Fatal("abnormal terminal ownership returned no quarantine error")
			}
			if err := jobOwnerTestStopAndWait(t, executor); err == nil {
				t.Fatal("executor reported success after abnormal ownership handling")
			}
			if snapshot := owner.snapshot(); !snapshot.OwnershipQuarantined || snapshot.TerminalHandled || !snapshot.AccountingHandled || !snapshot.ExecutionReturned {
				t.Fatalf("partly handled user ownership became a retryable terminal result: %+v", snapshot)
			}
			if snapshot := executor.snapshot(); snapshot.Quarantined != 1 || snapshot.Working != 0 || !snapshot.Done || snapshot.Completion.Working != 1 || snapshot.Completion.Available != 0 {
				t.Fatalf("abnormal terminal handling lost its exact completion reservation: %+v", snapshot)
			}
			if notifications.Load() != 1 || !errors.Is(owner.terminalComplete(), errJobResourceOwnership) || owner.rollbackBeforeLaunch() {
				t.Fatal("partial ownership notification was retried or rolled back")
			}
		})
	}
}

func TestJobResourceOwnerAbnormalPrelaunchRollbackRetainsReservations(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			executions := jobOwnerTestExecutionPool(t, 1, nil)
			completions := jobOwnerTestCompletionPool(t, 1, nil)
			var owner *jobResourceOwner
			var notifications atomic.Int32
			owner = jobOwnerTestReserve(t, executions, completions, &resourceLifecycle{}, jobRetireAfterAccounting, func() {
				notifications.Add(1)
				owner.snapshot()
				if executions.snapshot().Used != 1 || completions.snapshot().Reserved != 1 {
					t.Error("prelaunch notification ran after its execution or completion authority was returned")
				}
				if mode == "goexit" {
					runtime.Goexit()
				}
				panic("prelaunch ownership notification did not return")
			})
			finished := make(chan struct{})
			var normallyReturned atomic.Bool
			go func() {
				defer close(finished)
				defer func() { recover() }()
				owner.rollbackBeforeLaunch()
				normallyReturned.Store(true)
			}()
			jobOwnerTestReceive(t, finished, "abnormal prelaunch notification")
			if normallyReturned.Load() || notifications.Load() != 1 {
				t.Fatal("abnormal rollback was reported as completed or notified repeatedly")
			}
			if snapshot := owner.snapshot(); !snapshot.RolledBack || !snapshot.OwnershipQuarantined || snapshot.ExecutionReturned || snapshot.TerminalHandled || snapshot.Started {
				t.Fatalf("partial prelaunch ownership became a completed rollback: %+v", snapshot)
			}
			if executions.snapshot().Used != 1 || completions.snapshot().Reserved != 1 {
				t.Fatal("abnormal prelaunch notification returned its original reservations")
			}
			if owner.rollbackBeforeLaunch() || owner.startedExecution() || !errors.Is(owner.terminalComplete(), errJobResourceOwnership) || notifications.Load() != 1 {
				t.Fatal("partial prelaunch ownership was retried, launched or terminally released")
			}
		})
	}
}

func jobOwnerTestExecutionPool(t *testing.T, capacity int, notify func()) *executionSlotPool {
	t.Helper()
	pool, err := newExecutionSlotPool(capacity, notify)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func jobOwnerTestCompletionPool(t *testing.T, capacity int, notify func()) *completionTicketPool {
	t.Helper()
	pool, err := newCompletionTicketPool(capacity, notify)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func jobOwnerTestReserve(t *testing.T, executions *executionSlotPool, completions *completionTicketPool, lifecycle *resourceLifecycle, policy jobRetirementPolicy, notify func()) *jobResourceOwner {
	t.Helper()
	owner, err := reserveJobResourceOwner(executions, completions, lifecycle, policy, notify)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func jobOwnerTestExecutor(t *testing.T, pool *completionTicketPool) *finalizationExecutor {
	t.Helper()
	executor, err := newFinalizationExecutor(pool, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		executor.stopAfterDrain()
		if err := executor.wait(ctx); errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			t.Errorf("finalization worker did not stop: %v", err)
		}
	})
	return executor
}

func jobOwnerTestTask(owner *jobResourceOwner) finalizationTask {
	return finalizationTask{
		Finalize: func(context.Context) error { return owner.accountingComplete() },
		Terminal: func(_ context.Context, err error) error {
			if err != nil {
				return err
			}
			return owner.terminalComplete()
		},
	}
}

func jobOwnerTestStopAndWait(t *testing.T, executor *finalizationExecutor) error {
	t.Helper()
	executor.stopAfterDrain()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return executor.wait(ctx)
}

func jobOwnerTestReceive[T any](t *testing.T, values <-chan T, description string) T {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case value := <-values:
		return value
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", description)
		var zero T
		return zero
	}
}

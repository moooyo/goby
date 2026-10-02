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

func TestFinalizationExecutorBoundsReservedQueuedAndWorkingOwnership(t *testing.T) {
	pool := finalizationTestPool(t, 3, nil)
	executor := finalizationTestExecutor(t, pool, 1)
	first := finalizationTestReserve(t, pool)
	second := finalizationTestReserve(t, pool)
	reserved := finalizationTestReserve(t, pool)
	started := make(chan struct{})
	unblock := make(chan struct{})
	var unblockOnce sync.Once
	release := func() { unblockOnce.Do(func() { close(unblock) }) }
	t.Cleanup(release)
	var finalized, terminal atomic.Int32
	held := finalizationTask{
		Finalize: func(context.Context) error {
			finalized.Add(1)
			close(started)
			<-unblock
			return nil
		},
		Terminal: func(context.Context, error) error { terminal.Add(1); return nil },
	}
	if err := executor.submit(first, held); err != nil {
		t.Fatal(err)
	}
	finalizationTestReceive(t, started, "the held finalizer to start")
	queued := finalizationTask{
		Finalize: func(context.Context) error { finalized.Add(1); return nil },
		Terminal: func(context.Context, error) error { terminal.Add(1); return nil },
	}
	if err := executor.submit(second, queued); err != nil {
		t.Fatal(err)
	}
	if snapshot := executor.snapshot(); snapshot.Capacity != 3 || snapshot.Workers != 1 || snapshot.LiveWorkers != 1 ||
		snapshot.Queued != 1 || snapshot.Working != 1 || snapshot.Completion.Reserved != 1 ||
		snapshot.Completion.Queued != 1 || snapshot.Completion.Working != 1 || snapshot.Completion.Available != 0 {
		t.Fatalf("the launch bound does not include the complete finalization lifetime: %+v", snapshot)
	}
	if ticket, err := pool.reserve(); ticket != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("held finalization admitted another launch reservation: ticket=%v error=%v", ticket, err)
	}
	if err := executor.submit(reserved, queued); err != nil {
		t.Fatalf("the original reserved ticket could not enter the bounded backlog: %v", err)
	}
	if snapshot := executor.snapshot(); snapshot.Queued != 2 || snapshot.Working != 1 || snapshot.Completion.Reserved != 0 || snapshot.Completion.Available != 0 {
		t.Fatalf("queue handoff changed the completion allowance: %+v", snapshot)
	}
	executor.stopAfterDrain()
	release()
	if err := finalizationTestWait(t, executor); err != nil {
		t.Fatal(err)
	}
	if finalized.Load() != 3 || terminal.Load() != 3 {
		t.Fatalf("bounded shutdown lost callbacks: finalized=%d terminal=%d", finalized.Load(), terminal.Load())
	}
	if snapshot := executor.snapshot(); !snapshot.Done || snapshot.LiveWorkers != 0 || snapshot.Queued != 0 ||
		snapshot.Working != 0 || snapshot.Quarantined != 0 || snapshot.Completion.Available != 3 {
		t.Fatalf("the finite finalization backlog did not drain: %+v", snapshot)
	}
}

func TestFinalizationExecutorConcurrentDuplicateSubmissionRunsExactlyOnce(t *testing.T) {
	var notifications atomic.Int32
	pool := finalizationTestPool(t, 1, func() { notifications.Add(1) })
	executor := finalizationTestExecutor(t, pool, 1)
	ticket := finalizationTestReserve(t, pool)
	started := make(chan struct{})
	unblock := make(chan struct{})
	var unblockOnce sync.Once
	release := func() { unblockOnce.Do(func() { close(unblock) }) }
	t.Cleanup(release)
	var finalized, terminal atomic.Int32
	task := finalizationTask{
		Finalize: func(context.Context) error {
			finalized.Add(1)
			close(started)
			<-unblock
			return nil
		},
		Terminal: func(context.Context, error) error { terminal.Add(1); return nil },
	}
	const attempts = 32
	results := make(chan error, attempts)
	var submitters sync.WaitGroup
	for range attempts {
		submitters.Add(1)
		go func() {
			defer submitters.Done()
			results <- executor.submit(ticket, task)
		}()
	}
	submissionsDone := make(chan struct{})
	go func() { submitters.Wait(); close(submissionsDone) }()
	finalizationTestReceive(t, submissionsDone, "all concurrent duplicate submissions to return")
	accepted := 0
	for range attempts {
		if err := <-results; err == nil {
			accepted++
		} else if !errors.Is(err, errFinalizationTicket) {
			t.Fatalf("a duplicate submission returned an unrelated error: %v", err)
		}
	}
	if accepted != 1 {
		t.Fatalf("the same reserved ticket was accepted %d times", accepted)
	}
	finalizationTestReceive(t, started, "the accepted finalizer to start")
	if snapshot := executor.snapshot(); snapshot.Queued != 0 || snapshot.Working != 1 || snapshot.Completion.Available != 0 {
		t.Fatalf("duplicate submission created another queued lifetime: %+v", snapshot)
	}
	executor.stopAfterDrain()
	release()
	if err := finalizationTestWait(t, executor); err != nil {
		t.Fatal(err)
	}
	if finalized.Load() != 1 || terminal.Load() != 1 || notifications.Load() != 0 {
		t.Fatalf("duplicate or closed-admission callbacks changed ownership: finalized=%d terminal=%d notified=%d",
			finalized.Load(), terminal.Load(), notifications.Load())
	}
	if snapshot := pool.snapshot(); snapshot.Available != 1 || snapshot.Reserved+snapshot.Queued+snapshot.Working != 0 {
		t.Fatalf("the duplicate ticket did not release exactly one allowance: %+v", snapshot)
	}
}

func TestFinalizationExecutorRejectsStaleForeignAndUnownedTickets(t *testing.T) {
	for _, name := range []string{"nil", "foreign", "released", "queued", "working"} {
		t.Run(name, func(t *testing.T) {
			pool := finalizationTestPool(t, 1, nil)
			executor := finalizationTestExecutor(t, pool, 1)
			var ticket *completionTicket
			switch name {
			case "foreign":
				ticket = finalizationTestReserve(t, finalizationTestPool(t, 1, nil))
			case "released", "queued", "working":
				ticket = finalizationTestReserve(t, pool)
				if name == "released" {
					ticket.release()
				} else {
					if !ticket.queue() {
						t.Fatal("could not establish the existing ticket phase")
					}
					if name == "working" && !ticket.work() {
						t.Fatal("could not establish the working ticket phase")
					}
				}
			}
			if ticket != nil {
				t.Cleanup(func() { ticket.release() })
			}
			before := pool.snapshot()
			var callbacks atomic.Int32
			task := finalizationTask{
				Finalize: func(context.Context) error { callbacks.Add(1); return nil },
				Terminal: func(context.Context, error) error { callbacks.Add(1); return nil },
			}
			if err := executor.submit(ticket, task); !errors.Is(err, errFinalizationTicket) {
				t.Fatalf("invalid %s ticket entered the executor: %v", name, err)
			}
			if after := pool.snapshot(); after != before {
				t.Fatalf("rejected %s ticket changed pool ownership: before=%+v after=%+v", name, before, after)
			}
			executor.stopAfterDrain()
			if err := finalizationTestWait(t, executor); err != nil {
				t.Fatal(err)
			}
			if callbacks.Load() != 0 {
				t.Fatalf("rejected %s ticket ran %d callbacks", name, callbacks.Load())
			}
		})
	}
}

func TestFinalizationExecutorRejectsMissingCallbacksWithoutTakingTicket(t *testing.T) {
	for _, name := range []string{"finalize", "terminal"} {
		t.Run(name, func(t *testing.T) {
			pool := finalizationTestPool(t, 1, nil)
			executor := finalizationTestExecutor(t, pool, 1)
			ticket := finalizationTestReserve(t, pool)
			t.Cleanup(func() { ticket.release() })
			task := finalizationTestTask()
			if name == "finalize" {
				task.Finalize = nil
			} else {
				task.Terminal = nil
			}
			if err := executor.submit(ticket, task); !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("missing %s callback was accepted: %v", name, err)
			}
			if snapshot := executor.snapshot(); snapshot.Queued != 0 || snapshot.Working != 0 ||
				snapshot.Completion.Reserved != 1 || snapshot.Completion.Available != 0 {
				t.Fatalf("invalid callbacks consumed or released the reserved ticket: %+v", snapshot)
			}
		})
	}
}

func TestFinalizationExecutorCallbacksAndReturnNotificationRunOutsideLocks(t *testing.T) {
	type observation struct {
		stage      string
		context    context.Context
		executor   finalizationExecutorSnapshot
		completion completionTicketSnapshot
	}
	observations := make(chan observation, 3)
	var pool *completionTicketPool
	var executor *finalizationExecutor
	pool = finalizationTestPool(t, 1, func() {
		observations <- observation{stage: "available", executor: executor.snapshot(), completion: pool.snapshot()}
	})
	executor = finalizationTestExecutor(t, pool, 1)
	ticket := finalizationTestReserve(t, pool)
	type contextKey struct{}
	finalizeContext := context.WithValue(context.Background(), contextKey{}, "finalize")
	terminalContext := context.WithValue(context.Background(), contextKey{}, "terminal")
	task := finalizationTask{
		FinalizeContext: finalizeContext,
		TerminalContext: terminalContext,
		Finalize: func(ctx context.Context) error {
			observations <- observation{stage: "finalize", context: ctx, executor: executor.snapshot(), completion: pool.snapshot()}
			return nil
		},
		Terminal: func(ctx context.Context, err error) error {
			observations <- observation{stage: "terminal", context: ctx, executor: executor.snapshot(), completion: pool.snapshot()}
			return err
		},
	}
	if err := executor.submit(ticket, task); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"finalize", "terminal", "available"} {
		observed := finalizationTestReceive(t, observations, stage+" callback to inspect both ownership locks")
		if observed.stage != stage {
			t.Fatalf("callback order changed: got %s want %s", observed.stage, stage)
		}
		if stage == "finalize" && observed.context != finalizeContext || stage == "terminal" && observed.context != terminalContext {
			t.Fatalf("%s callback did not receive its owner context", stage)
		}
		if stage == "available" {
			if observed.completion.Available != 1 || observed.executor.Completion.Available != 1 {
				t.Fatalf("scheduler notification preceded the visible ticket return: %+v", observed)
			}
		} else if observed.executor.Working != 1 || observed.completion.Working != 1 {
			t.Fatalf("%s callback lost the original working ticket: %+v", stage, observed)
		}
	}
	executor.stopAfterDrain()
	if err := finalizationTestWait(t, executor); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizationExecutorShutdownKeepsReservedHandoffAndTimeoutOwnership(t *testing.T) {
	pool := finalizationTestPool(t, 3, nil)
	executor := finalizationTestExecutor(t, pool, 1)
	first := finalizationTestReserve(t, pool)
	handoff := finalizationTestReserve(t, pool)
	rollback := finalizationTestReserve(t, pool)
	t.Cleanup(func() { rollback.release() })
	started := make(chan struct{})
	unblock := make(chan struct{})
	var unblockOnce sync.Once
	release := func() { unblockOnce.Do(func() { close(unblock) }) }
	t.Cleanup(release)
	callbackContexts := make(chan error, 4)
	if err := executor.submit(first, finalizationTask{
		Finalize: func(ctx context.Context) error {
			close(started)
			<-unblock
			callbackContexts <- ctx.Err()
			return nil
		},
		Terminal: func(ctx context.Context, err error) error { callbackContexts <- ctx.Err(); return err },
	}); err != nil {
		t.Fatal(err)
	}
	finalizationTestReceive(t, started, "the shutdown finalizer to start")
	pool.close()
	if ticket, err := pool.reserve(); ticket != nil || !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("closed pool admitted another launch: ticket=%v error=%v", ticket, err)
	}
	if err := executor.submit(handoff, finalizationTask{
		Finalize: func(ctx context.Context) error { callbackContexts <- ctx.Err(); return nil },
		Terminal: func(ctx context.Context, err error) error { callbackContexts <- ctx.Err(); return err },
	}); err != nil {
		t.Fatalf("closing launch admission revoked an existing runner reservation: %v", err)
	}
	executor.stopAfterDrain()
	if err := executor.submit(rollback, finalizationTestTask()); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("stopped executor admitted a late ownership transfer: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := executor.wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("held cleanup did not honor the caller's wait deadline: %v", err)
	}
	if snapshot := executor.snapshot(); !snapshot.Stopped || snapshot.Done || snapshot.Queued != 1 || snapshot.Working != 1 ||
		!snapshot.Completion.Closed || snapshot.Completion.Reserved != 1 || snapshot.Completion.Queued != 1 ||
		snapshot.Completion.Working != 1 || snapshot.Completion.Available != 0 {
		t.Fatalf("wait timeout revoked queued, working, or rollback ownership: %+v", snapshot)
	}
	release()
	if err := finalizationTestWait(t, executor); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err := finalizationTestReceive(t, callbackContexts, "callbacks to retain their own contexts"); err != nil {
			t.Fatalf("wait timeout cancelled an owned callback context: %v", err)
		}
	}
	if snapshot := pool.snapshot(); snapshot.Reserved != 1 || snapshot.Available != 2 {
		t.Fatalf("executor drain released a reservation whose handoff was rejected: %+v", snapshot)
	}
	if !rollback.release() || pool.snapshot().Available != 3 {
		t.Fatal("the shutdown owner could not roll back its remaining reservation")
	}
}

func TestFinalizationExecutorFinalizerOutcomesReachTerminalAndRelease(t *testing.T) {
	failure := errors.New("workspace sealing failed")
	for _, test := range []struct {
		name     string
		finalize func(context.Context) error
		want     error
	}{
		{name: "success", finalize: func(context.Context) error { return nil }},
		{name: "failure", finalize: func(context.Context) error { return failure }, want: failure},
		{name: "panic", finalize: func(context.Context) error { panic("finalizer panic") }, want: errFinalizationPanic},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := finalizationTestPool(t, 1, nil)
			executor := finalizationTestExecutor(t, pool, 1)
			ticket := finalizationTestReserve(t, pool)
			terminalErrors := make(chan error, 1)
			contexts := make(chan error, 2)
			task := finalizationTask{
				Finalize: func(ctx context.Context) error {
					contexts <- ctx.Err()
					return test.finalize(ctx)
				},
				Terminal: func(ctx context.Context, err error) error {
					contexts <- ctx.Err()
					terminalErrors <- err
					return nil
				},
			}
			if err := executor.submit(ticket, task); err != nil {
				t.Fatal(err)
			}
			executor.stopAfterDrain()
			if err := finalizationTestWait(t, executor); err != nil {
				t.Fatalf("a handled finalizer outcome became an executor failure: %v", err)
			}
			if err := finalizationTestReceive(t, terminalErrors, "the finalizer outcome to reach terminal handling"); !errors.Is(err, test.want) {
				t.Fatalf("terminal handling received %v, want %v", err, test.want)
			}
			for range 2 {
				if err := finalizationTestReceive(t, contexts, "default callback contexts"); err != nil {
					t.Fatalf("a nil stage context did not default to Background: %v", err)
				}
			}
			if snapshot := executor.snapshot(); snapshot.Quarantined != 0 || snapshot.Completion.Available != 1 || snapshot.Completion.Working != 0 {
				t.Fatalf("normal terminal return did not release the original ticket: %+v", snapshot)
			}
		})
	}
}

func TestFinalizationExecutorRetainsTicketUntilTerminalReturns(t *testing.T) {
	returned := make(chan struct{})
	var notifications atomic.Int32
	pool := finalizationTestPool(t, 1, func() { notifications.Add(1); close(returned) })
	executor := finalizationTestExecutor(t, pool, 1)
	ticket := finalizationTestReserve(t, pool)
	terminalStarted := make(chan struct{})
	unblock := make(chan struct{})
	var unblockOnce sync.Once
	release := func() { unblockOnce.Do(func() { close(unblock) }) }
	t.Cleanup(release)
	var finalized, terminal atomic.Int32
	if err := executor.submit(ticket, finalizationTask{
		Finalize: func(context.Context) error { finalized.Add(1); return nil },
		Terminal: func(context.Context, error) error {
			close(terminalStarted)
			<-unblock
			terminal.Add(1)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	finalizationTestReceive(t, terminalStarted, "terminal handling to hold the completed finalizer's ticket")
	if finalized.Load() != 1 || terminal.Load() != 0 || notifications.Load() != 0 {
		t.Fatal("the held terminal callback did not establish the finalizer-return boundary")
	}
	if snapshot := executor.snapshot(); snapshot.Working != 1 || snapshot.Completion.Working != 1 || snapshot.Completion.Available != 0 {
		t.Fatalf("finalizer return released capacity before terminal handling returned: %+v", snapshot)
	}
	if replacement, err := pool.reserve(); replacement != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("held terminal handling admitted another launch: ticket=%v error=%v", replacement, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := executor.wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting for held terminal handling did not time out: %v", err)
	}
	if snapshot := executor.snapshot(); snapshot.Stopped || snapshot.Done || snapshot.Completion.Working != 1 || snapshot.Completion.Available != 0 {
		t.Fatalf("wait timeout changed terminal ownership or executor admission: %+v", snapshot)
	}
	release()
	finalizationTestReceive(t, returned, "the ticket return after normal terminal completion")
	executor.stopAfterDrain()
	if err := finalizationTestWait(t, executor); err != nil {
		t.Fatal(err)
	}
	if terminal.Load() != 1 || notifications.Load() != 1 || ticket.release() || pool.snapshot().Available != 1 {
		t.Fatal("normal terminal return did not release exactly the original completion allowance")
	}
}

func TestFinalizationExecutorTerminalFailureReleasesTicketAndKeepsStorageOwnership(t *testing.T) {
	pool := finalizationTestPool(t, 2, nil)
	executor := finalizationTestExecutor(t, pool, 1)
	first := finalizationTestReserve(t, pool)
	second := finalizationTestReserve(t, pool)
	finalizeFailure := errors.New("sealing failed before terminal persistence")
	terminalFailure := errors.New("terminal persistence failed")
	laterFailure := errors.New("later terminal persistence failed")
	var storageOwned atomic.Bool
	storageOwned.Store(true)
	var terminalCalls atomic.Int32
	observed := make(chan error, 1)
	if err := executor.submit(first, finalizationTask{
		Finalize: func(context.Context) error { return finalizeFailure },
		Terminal: func(_ context.Context, err error) error {
			terminalCalls.Add(1)
			observed <- err
			return terminalFailure
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := executor.submit(second, finalizationTask{
		Finalize: func(context.Context) error { return nil },
		Terminal: func(context.Context, error) error { terminalCalls.Add(1); return laterFailure },
	}); err != nil {
		t.Fatal(err)
	}
	executor.stopAfterDrain()
	if err := finalizationTestWait(t, executor); !errors.Is(err, terminalFailure) || errors.Is(err, laterFailure) || errors.Is(err, finalizeFailure) {
		t.Fatalf("wait did not retain the first terminal failure: %v", err)
	}
	if err := finalizationTestReceive(t, observed, "the original sealing error to reach terminal handling"); !errors.Is(err, finalizeFailure) {
		t.Fatalf("terminal persistence lost the finalizer error: %v", err)
	}
	if terminalCalls.Load() != 2 || !storageOwned.Load() {
		t.Fatalf("terminal failure stopped the worker or released separate storage ownership: calls=%d storage=%v",
			terminalCalls.Load(), storageOwned.Load())
	}
	if snapshot := executor.snapshot(); snapshot.Quarantined != 0 || snapshot.Completion.Available != 2 ||
		snapshot.Completion.Reserved+snapshot.Completion.Queued+snapshot.Completion.Working != 0 {
		t.Fatalf("normal terminal failures retained completion tickets: %+v", snapshot)
	}
}

func TestFinalizationExecutorTerminalPanicQuarantinesExactTicketAndWorkerSurvives(t *testing.T) {
	pool := finalizationTestPool(t, 2, nil)
	executor := finalizationTestExecutor(t, pool, 1)
	quarantined := finalizationTestReserve(t, pool)
	next := finalizationTestReserve(t, pool)
	type notification struct {
		err        error
		executor   finalizationExecutorSnapshot
		completion completionTicketSnapshot
	}
	notifications := make(chan notification, 1)
	nextFinalized := make(chan struct{})
	var terminals atomic.Int32
	if err := executor.submit(quarantined, finalizationTask{
		Finalize: func(context.Context) error { return nil },
		Terminal: func(context.Context, error) error { panic("terminal panic") },
		OnQuarantined: func(err error) {
			notifications <- notification{err: err, executor: executor.snapshot(), completion: pool.snapshot()}
			panic("admission fence notification panic")
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := executor.submit(next, finalizationTask{
		Finalize: func(context.Context) error { close(nextFinalized); return nil },
		Terminal: func(context.Context, error) error { terminals.Add(1); return nil },
	}); err != nil {
		t.Fatal(err)
	}
	notified := finalizationTestReceive(t, notifications, "quarantine notification to inspect both ownership locks")
	if !errors.Is(notified.err, errFinalizationTerminalPanic) || notified.executor.Quarantined != 1 || notified.completion.Working != 1 {
		t.Fatalf("terminal panic did not preserve its ticket before fencing admission: %+v", notified)
	}
	finalizationTestReceive(t, nextFinalized, "the same fixed worker to continue after both callback panics")
	executor.stopAfterDrain()
	if err := finalizationTestWait(t, executor); !errors.Is(err, errFinalizationTerminalPanic) {
		t.Fatalf("terminal panic was lost or replaced by the notification panic: %v", err)
	}
	if snapshot := executor.snapshot(); !snapshot.Done || snapshot.LiveWorkers != 0 || snapshot.Working != 0 ||
		snapshot.Quarantined != 1 || snapshot.Completion.Working != 1 || snapshot.Completion.Available != 1 {
		t.Fatalf("worker shutdown discarded quarantined completion ownership: %+v", snapshot)
	}
	if len(executor.quarantined) != 2 || executor.quarantineN != 1 || executor.quarantined[0].ticket != quarantined ||
		quarantined.phase != completionWorking || terminals.Load() != 1 {
		t.Fatal("quarantine lost the original submission or prevented subsequent terminal handling")
	}
}

func TestFinalizationExecutorReportsBrokenTicketReleaseInvariant(t *testing.T) {
	pool := finalizationTestPool(t, 2, nil)
	executor := finalizationTestExecutor(t, pool, 1)
	first := finalizationTestReserve(t, pool)
	second := finalizationTestReserve(t, pool)
	var prematureRelease atomic.Bool
	var laterTerminals atomic.Int32
	if err := executor.submit(first, finalizationTask{
		Finalize: func(context.Context) error { return nil },
		Terminal: func(context.Context, error) error {
			prematureRelease.Store(first.release())
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := executor.submit(second, finalizationTask{
		Finalize: func(context.Context) error { return nil },
		Terminal: func(context.Context, error) error { laterTerminals.Add(1); return nil },
	}); err != nil {
		t.Fatal(err)
	}
	executor.stopAfterDrain()
	if err := finalizationTestWait(t, executor); !errors.Is(err, errFinalizationTicket) {
		t.Fatalf("an invalid duplicate terminal release was hidden: %v", err)
	}
	if !prematureRelease.Load() || laterTerminals.Load() != 1 {
		t.Fatal("the ticket invariant failure prevented the fixed worker from continuing")
	}
	if snapshot := executor.snapshot(); snapshot.Quarantined != 0 || snapshot.Completion.Available != 2 ||
		snapshot.Completion.Reserved+snapshot.Completion.Queued+snapshot.Completion.Working != 0 {
		t.Fatalf("an invalid terminal release damaged completion capacity: %+v", snapshot)
	}
}

func TestFinalizationExecutorCallbackGoexitQuarantinesAndReplacesWorkerSlot(t *testing.T) {
	for _, test := range []struct {
		name string
		want error
	}{
		{name: "finalize", want: errFinalizationExit},
		{name: "terminal", want: errFinalizationTerminalExit},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := finalizationTestPool(t, 3, nil)
			executor := finalizationTestExecutor(t, pool, 1)
			original := finalizationTestReserve(t, pool)
			successor := finalizationTestReserve(t, pool)
			handoff := finalizationTestReserve(t, pool)
			exitGate, exit := finalizationTestGate(t)
			successorGate, finishSuccessor := finalizationTestGate(t)
			entered := make(chan struct{})
			successorEntered := make(chan struct{})
			var originalFinalized, originalTerminal, successorTerminal, handoffTerminal atomic.Int32
			type contextKey struct{}
			ownerContext := context.WithValue(context.Background(), contextKey{}, test.name)
			task := finalizationTask{
				FinalizeContext: ownerContext,
				TerminalContext: ownerContext,
				Finalize: func(context.Context) error {
					originalFinalized.Add(1)
					if test.name == "finalize" {
						close(entered)
						<-exitGate
						runtime.Goexit()
					}
					return nil
				},
				Terminal: func(context.Context, error) error {
					originalTerminal.Add(1)
					close(entered)
					<-exitGate
					runtime.Goexit()
					return nil
				},
			}
			if err := executor.submit(original, task); err != nil {
				t.Fatal(err)
			}
			finalizationTestReceive(t, entered, "the callback that will exit its worker")
			if err := executor.submit(successor, finalizationTask{
				Finalize: func(context.Context) error { close(successorEntered); <-successorGate; return nil },
				Terminal: func(context.Context, error) error { successorTerminal.Add(1); return nil },
			}); err != nil {
				t.Fatal(err)
			}
			if snapshot := executor.snapshot(); snapshot.Queued != 1 || snapshot.Completion.Reserved != 1 {
				t.Fatalf("the successor was not queued before the worker exit: %+v", snapshot)
			}
			exit()
			finalizationTestReceive(t, successorEntered, "the replacement worker to process the queued successor")
			if snapshot := executor.snapshot(); snapshot.Workers != 1 || snapshot.LiveWorkers != 1 || snapshot.Done || snapshot.Stopped ||
				snapshot.Working != 1 || snapshot.Quarantined != 1 || !snapshot.Completion.Closed ||
				snapshot.Completion.Working != 2 || snapshot.Completion.Reserved != 1 || snapshot.Completion.Available != 0 {
				t.Fatalf("callback exit lost ownership or changed the configured worker slot bound: %+v", snapshot)
			}
			if ticket, err := pool.reserve(); ticket != nil || !errors.Is(err, ErrManagerClosed) {
				t.Fatalf("callback exit failed to fence new launch admission: ticket=%v error=%v", ticket, err)
			}
			if err := executor.submit(handoff, finalizationTask{
				Finalize: func(context.Context) error { return nil },
				Terminal: func(context.Context, error) error { handoffTerminal.Add(1); return nil },
			}); err != nil {
				t.Fatalf("the exit fence revoked an existing runner reservation: %v", err)
			}
			executor.stopAfterDrain()
			finishSuccessor()
			if err := finalizationTestWait(t, executor); !errors.Is(err, test.want) {
				t.Fatalf("worker exit produced false success or lost the stage error: got %v want %v", err, test.want)
			}
			if snapshot := executor.snapshot(); !snapshot.Done || snapshot.LiveWorkers != 0 || snapshot.Working != 0 || snapshot.Queued != 0 ||
				snapshot.Quarantined != 1 || snapshot.Completion.Working != 1 || snapshot.Completion.Available != 2 {
				t.Fatalf("replacement drain released the exited callback's ticket: %+v", snapshot)
			}
			wantOriginalTerminal := int32(0)
			if test.name == "terminal" {
				wantOriginalTerminal = 1
			}
			if originalFinalized.Load() != 1 || originalTerminal.Load() != wantOriginalTerminal || successorTerminal.Load() != 1 || handoffTerminal.Load() != 1 {
				t.Fatal("worker replacement duplicated the exited callback or lost reserved successors")
			}
			retained := executor.quarantined[0]
			if executor.quarantineN != 1 || retained.ticket != original || original.phase != completionWorking ||
				retained.task.FinalizeContext != ownerContext || retained.task.TerminalContext != ownerContext ||
				retained.task.Finalize == nil || retained.task.Terminal == nil {
				t.Fatal("callback exit did not retain the exact original ticket and task")
			}
		})
	}
}

func TestFinalizationExecutorReleaseNotificationGoexitKeepsReturnedTicketAndReplacesWorkerSlot(t *testing.T) {
	notificationEntered := make(chan struct{})
	fenceErrors := make(chan error, 1)
	var notifications, fenceNotifications atomic.Int32
	var notificationGate <-chan struct{}
	pool := finalizationTestPool(t, 3, func() {
		notifications.Add(1)
		close(notificationEntered)
		<-notificationGate
		runtime.Goexit()
	})
	executor := finalizationTestExecutor(t, pool, 1)
	original := finalizationTestReserve(t, pool)
	successor := finalizationTestReserve(t, pool)
	handoff := finalizationTestReserve(t, pool)
	notificationGate, exit := finalizationTestGate(t)
	successorGate, finishSuccessor := finalizationTestGate(t)
	successorEntered := make(chan struct{})
	var terminals atomic.Int32
	task := finalizationTestTask()
	terminalFailure := errors.New("terminal persistence failed before notification exit")
	task.Terminal = func(context.Context, error) error { return terminalFailure }
	task.OnQuarantined = func(err error) { fenceNotifications.Add(1); fenceErrors <- err }
	if err := executor.submit(original, task); err != nil {
		t.Fatal(err)
	}
	finalizationTestReceive(t, notificationEntered, "ticket-return notification to hold the released ticket")
	if snapshot := executor.snapshot(); snapshot.Working != 1 || snapshot.Quarantined != 0 || snapshot.Completion.Working != 0 || snapshot.Completion.Available != 1 {
		t.Fatalf("ticket return notification ran before completion ownership was returned: %+v", snapshot)
	}
	if err := executor.submit(successor, finalizationTask{
		Finalize: func(context.Context) error { close(successorEntered); <-successorGate; return nil },
		Terminal: func(context.Context, error) error { terminals.Add(1); return nil },
	}); err != nil {
		t.Fatal(err)
	}
	exit()
	finalizationTestReceive(t, successorEntered, "the replacement worker after ticket-return notification exit")
	if err := finalizationTestReceive(t, fenceErrors, "the admission fence notification for the returned ticket"); !errors.Is(err, errFinalizationReleaseExit) {
		t.Fatalf("notification exit did not report the admission fence cause: %v", err)
	}
	if snapshot := executor.snapshot(); snapshot.Workers != 1 || snapshot.LiveWorkers != 1 || snapshot.Done || snapshot.Stopped ||
		snapshot.Working != 1 || snapshot.Quarantined != 0 || !snapshot.Completion.Closed ||
		snapshot.Completion.Working != 1 || snapshot.Completion.Reserved != 1 || snapshot.Completion.Available != 1 {
		t.Fatalf("notification exit double-released, quarantined, or lost the returned ticket: %+v", snapshot)
	}
	if ticket, err := pool.reserve(); ticket != nil || !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("notification exit admitted a new launch using returned capacity: ticket=%v error=%v", ticket, err)
	}
	if err := executor.submit(handoff, finalizationTask{
		Finalize: func(context.Context) error { return nil },
		Terminal: func(context.Context, error) error { terminals.Add(1); return nil },
	}); err != nil {
		t.Fatalf("notification exit revoked an existing reserved handoff: %v", err)
	}
	executor.stopAfterDrain()
	finishSuccessor()
	if err := finalizationTestWait(t, executor); !errors.Is(err, errFinalizationReleaseExit) || !errors.Is(err, terminalFailure) {
		t.Fatalf("ticket-return notification exit produced false success or lost the terminal failure: %v", err)
	}
	if snapshot := executor.snapshot(); !snapshot.Done || snapshot.LiveWorkers != 0 || snapshot.Working != 0 || snapshot.Queued != 0 ||
		snapshot.Quarantined != 0 || !snapshot.Completion.Closed || snapshot.Completion.Available != 3 || snapshot.Completion.Working != 0 {
		t.Fatalf("notification exit left a worker or completion ownership leak: %+v", snapshot)
	}
	if notifications.Load() != 1 || fenceNotifications.Load() != 1 || terminals.Load() != 2 || original.phase != completionReleased || original.release() {
		t.Fatal("notification exit retried terminal release or quarantined an already returned ticket")
	}
}

func TestFinalizationExecutorQuarantineNotificationGoexitRetainsSingleQuarantineAndReplacesWorkerSlot(t *testing.T) {
	pool := finalizationTestPool(t, 3, nil)
	executor := finalizationTestExecutor(t, pool, 1)
	original := finalizationTestReserve(t, pool)
	successor := finalizationTestReserve(t, pool)
	handoff := finalizationTestReserve(t, pool)
	notificationGate, exit := finalizationTestGate(t)
	successorGate, finishSuccessor := finalizationTestGate(t)
	type notification struct {
		err        error
		executor   finalizationExecutorSnapshot
		completion completionTicketSnapshot
	}
	observed := make(chan notification, 1)
	successorEntered := make(chan struct{})
	var notifications, terminals atomic.Int32
	if err := executor.submit(original, finalizationTask{
		Finalize: func(context.Context) error { return nil },
		Terminal: func(context.Context, error) error { panic("terminal panic before notification exit") },
		OnQuarantined: func(err error) {
			notifications.Add(1)
			observed <- notification{err: err, executor: executor.snapshot(), completion: pool.snapshot()}
			<-notificationGate
			runtime.Goexit()
		},
	}); err != nil {
		t.Fatal(err)
	}
	notified := finalizationTestReceive(t, observed, "quarantine notification to inspect ownership before exiting")
	if !errors.Is(notified.err, errFinalizationTerminalPanic) || notified.executor.Quarantined != 1 || notified.executor.Working != 0 ||
		!notified.completion.Closed || notified.completion.Working != 1 || notified.completion.Reserved != 2 {
		t.Fatalf("quarantine notification began before ticket retention and admission fencing: %+v", notified)
	}
	if err := executor.submit(successor, finalizationTask{
		Finalize: func(context.Context) error { close(successorEntered); <-successorGate; return nil },
		Terminal: func(context.Context, error) error { terminals.Add(1); return nil },
	}); err != nil {
		t.Fatalf("quarantine fence revoked a queued successor reservation: %v", err)
	}
	exit()
	finalizationTestReceive(t, successorEntered, "the replacement worker after quarantine notification exit")
	if snapshot := executor.snapshot(); snapshot.Workers != 1 || snapshot.LiveWorkers != 1 || snapshot.Done || snapshot.Stopped ||
		snapshot.Working != 1 || snapshot.Quarantined != 1 || !snapshot.Completion.Closed ||
		snapshot.Completion.Working != 2 || snapshot.Completion.Reserved != 1 || snapshot.Completion.Available != 0 {
		t.Fatalf("quarantine notification exit counted the original submission or worker slot twice: %+v", snapshot)
	}
	if ticket, err := pool.reserve(); ticket != nil || !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("quarantine notification exit reopened launch admission: ticket=%v error=%v", ticket, err)
	}
	if err := executor.submit(handoff, finalizationTask{
		Finalize: func(context.Context) error { return nil },
		Terminal: func(context.Context, error) error { terminals.Add(1); return nil },
	}); err != nil {
		t.Fatalf("quarantine notification exit revoked an existing reserved handoff: %v", err)
	}
	executor.stopAfterDrain()
	finishSuccessor()
	if err := finalizationTestWait(t, executor); !errors.Is(err, errFinalizationQuarantineExit) || !errors.Is(err, errFinalizationTerminalPanic) {
		t.Fatalf("quarantine notification exit lost an observed callback error or produced false success: %v", err)
	}
	if snapshot := executor.snapshot(); !snapshot.Done || snapshot.LiveWorkers != 0 || snapshot.Working != 0 || snapshot.Queued != 0 ||
		snapshot.Quarantined != 1 || snapshot.Completion.Working != 1 || snapshot.Completion.Available != 2 {
		t.Fatalf("quarantine notification exit leaked the worker or discarded the original ticket: %+v", snapshot)
	}
	if notifications.Load() != 1 || terminals.Load() != 2 || executor.quarantineN != 1 || executor.quarantined[0].ticket != original || original.phase != completionWorking {
		t.Fatal("quarantine notification exit duplicated retention or prevented successor terminal handling")
	}
}

func TestFinalizationExecutorRejectsInvalidWorkerBounds(t *testing.T) {
	if executor, err := newFinalizationExecutor(nil, 1); executor != nil || !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("nil completion pool was accepted: executor=%v error=%v", executor, err)
	}
	for _, capacity := range []int{0, maxCompletionTickets + 1} {
		pool := &completionTicketPool{capacity: capacity}
		if executor, err := newFinalizationExecutor(pool, 1); executor != nil || !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("invalid completion pool capacity %d was accepted: executor=%v error=%v", capacity, executor, err)
		}
	}
	pool := finalizationTestPool(t, maxFinalizationWorkers+1, nil)
	for _, workers := range []int{-1, 0, maxFinalizationWorkers + 1} {
		if executor, err := newFinalizationExecutor(pool, workers); executor != nil || !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("invalid fixed worker count %d was accepted: executor=%v error=%v", workers, executor, err)
		}
	}
	if executor, err := newFinalizationExecutor(finalizationTestPool(t, 1, nil), 2); executor != nil || !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("more workers than completion tickets were accepted: executor=%v error=%v", executor, err)
	}
	for _, bounds := range []struct{ capacity, workers int }{{1, 1}, {2, 2}, {maxFinalizationWorkers + 1, maxFinalizationWorkers}} {
		executor := finalizationTestExecutor(t, finalizationTestPool(t, bounds.capacity, nil), bounds.workers)
		if snapshot := executor.snapshot(); snapshot.Capacity != bounds.capacity || snapshot.Workers != bounds.workers || snapshot.LiveWorkers != bounds.workers {
			t.Fatalf("valid constructor bounds were not fixed at creation: %+v", snapshot)
		}
		if len(executor.queue) != bounds.capacity {
			t.Fatalf("the finalization ring has %d slots, want %d", len(executor.queue), bounds.capacity)
		}
	}
}

func TestFinalizationExecutorReleaseNotificationPanicFencesWithoutRestoringTicket(t *testing.T) {
	pool := finalizationTestPool(t, 2, func() { panic("notification interrupted") })
	executor := finalizationTestExecutor(t, pool, 1)
	completed := finalizationTestReserve(t, pool)
	reserved := finalizationTestReserve(t, pool)
	fenced := make(chan error, 1)
	task := finalizationTestTask()
	task.OnQuarantined = func(err error) { fenced <- err }
	if err := executor.submit(completed, task); err != nil {
		t.Fatal(err)
	}
	if err := finalizationTestReceive(t, fenced, "release panic fence"); !errors.Is(err, errFinalizationReleasePanic) {
		t.Fatalf("release failure fence=%v", err)
	}
	if snapshot := executor.snapshot(); !snapshot.Completion.Closed || snapshot.Quarantined != 0 ||
		snapshot.Completion.Working != 0 || snapshot.Completion.Reserved != 1 || snapshot.Completion.Available != 1 {
		t.Fatalf("an already released completion was restored or admission remained open: %+v", snapshot)
	}
	if _, err := pool.reserve(); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("reserve after notification panic=%v", err)
	}
	// The original runner reservation still transfers after the sticky fence.
	if err := executor.submit(reserved, finalizationTestTask()); err != nil {
		t.Fatal(err)
	}
	executor.stopAfterDrain()
	if err := finalizationTestWait(t, executor); !errors.Is(err, errFinalizationReleasePanic) {
		t.Fatalf("release panic was lost: %v", err)
	}
	if completed.release() || reserved.release() {
		t.Fatal("an already completed ticket released twice")
	}
}

func finalizationTestPool(t *testing.T, capacity int, onAvailable func()) *completionTicketPool {
	t.Helper()
	pool, err := newCompletionTicketPool(capacity, onAvailable)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func finalizationTestExecutor(t *testing.T, pool *completionTicketPool, workers int) *finalizationExecutor {
	t.Helper()
	executor, err := newFinalizationExecutor(pool, workers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		finished := make(chan error, 1)
		go func() {
			executor.stopAfterDrain()
			finished <- executor.wait(ctx)
		}()
		select {
		case err := <-finished:
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				t.Errorf("finalization workers did not stop during cleanup: %v", err)
			}
		case <-ctx.Done():
			t.Errorf("finalization executor remained blocked during cleanup: %v", ctx.Err())
		}
	})
	return executor
}

func finalizationTestReserve(t *testing.T, pool *completionTicketPool) *completionTicket {
	t.Helper()
	ticket, err := pool.reserve()
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

func finalizationTestTask() finalizationTask {
	return finalizationTask{
		Finalize: func(context.Context) error { return nil },
		Terminal: func(context.Context, error) error { return nil },
	}
}

func finalizationTestGate(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(open)
	return gate, open
}

func finalizationTestWait(t *testing.T, executor *finalizationExecutor) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return executor.wait(ctx)
}

func finalizationTestReceive[T any](t *testing.T, values <-chan T, description string) T {
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

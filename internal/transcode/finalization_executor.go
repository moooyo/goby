package transcode

import (
	"context"
	"errors"
	"sync"
)

const maxFinalizationWorkers = 64

var (
	errFinalizationTicket         = errors.New("invalid transcode finalization ticket")
	errFinalizationPanic          = errors.New("transcode finalization callback panicked")
	errFinalizationTerminalPanic  = errors.New("transcode terminal callback panicked")
	errFinalizationReleasePanic   = errors.New("transcode completion notification panicked")
	errFinalizationExit           = errors.New("transcode finalization callback exited without returning")
	errFinalizationTerminalExit   = errors.New("transcode terminal callback exited without returning")
	errFinalizationReleaseExit    = errors.New("transcode completion notification exited without returning")
	errFinalizationQuarantineExit = errors.New("transcode quarantine notification exited without returning")
	errFinalizationWorkerExit     = errors.New("transcode finalization worker exited unexpectedly")
)

type finalizationExitKind uint8

const (
	finalizationExitFinalize finalizationExitKind = iota
	finalizationExitTerminal
	finalizationExitRelease
	finalizationExitQuarantine
	finalizationExitWorker
	finalizationExitKinds
)

var finalizationExitErrors = [finalizationExitKinds]error{
	errFinalizationExit, errFinalizationTerminalExit, errFinalizationReleaseExit,
	errFinalizationQuarantineExit, errFinalizationWorkerExit,
}

type finalizationWorkerStage uint8

const (
	finalizationWorkerIdle finalizationWorkerStage = iota
	finalizationWorkerFinalize
	finalizationWorkerTerminal
	finalizationWorkerRelease
	finalizationWorkerNotify
)

type finalizationWorkerState struct {
	submission  finalizationSubmission
	stage       finalizationWorkerStage
	working     bool
	quarantined bool
	normalExit  bool
}

// finalizationTask contains the callbacks owned by one already reserved job.
// The owner establishes writer drain and any required process-retirement proof
// before submit; this executor never authorizes execution or storage release.
// Callbacks must perform I/O with bounded contexts and may create those contexts
// inside the callback. A nil stage context defaults to Background for bookkeeping.
// Terminal must account for a Finalize error before it returns. Its context must
// not depend on a cancelled runner context. OnQuarantined must return promptly
// without I/O. It is the owner's sticky admission fence notification, not a
// claim that a completion ticket is always retained: a notification can exit
// after terminal handling returned and its ticket was already released.
type finalizationTask struct {
	FinalizeContext context.Context
	TerminalContext context.Context
	Finalize        func(context.Context) error
	Terminal        func(context.Context, error) error
	OnQuarantined   func(error)
}

type finalizationSubmission struct {
	ticket *completionTicket
	task   finalizationTask
}

type finalizationExecutorSnapshot struct {
	Capacity    int
	Workers     int
	LiveWorkers int
	Queued      int
	Working     int
	Quarantined int
	Stopped     bool
	Done        bool
	Completion  completionTicketSnapshot
}

// finalizationExecutor owns a finite ring and a fixed set of workers. The
// original launch ticket covers reserved runners, queued tasks, active workers,
// and quarantined terminal handling. Neither submit nor wait spawns goroutines.
// Each fixed worker slot replaces its goroutine only after an abnormal exit;
// its previous owned callback and notification have exited before replacement.
// Queue admission and ticket transitions share the executor-to-pool lock order;
// no owner callback or ticket-return notification runs under either lock.
type finalizationExecutor struct {
	mu          sync.Mutex
	ready       *sync.Cond
	pool        *completionTicketPool
	queue       []finalizationSubmission
	head        int
	queued      int
	working     int
	workers     int
	liveWorkers int
	stopped     bool
	done        chan struct{}
	firstErr    error
	quarantined []finalizationSubmission
	quarantineN int
	exits       [finalizationExitKinds]bool
}

func newFinalizationExecutor(pool *completionTicketPool, workers int) (*finalizationExecutor, error) {
	if pool == nil {
		return nil, ErrInvalidOptions
	}
	capacity := pool.snapshot().Capacity
	if capacity < 1 || capacity > maxCompletionTickets || workers < 1 || workers > maxFinalizationWorkers || workers > capacity {
		return nil, ErrInvalidOptions
	}
	e := &finalizationExecutor{
		pool: pool, queue: make([]finalizationSubmission, capacity), workers: workers,
		liveWorkers: workers, done: make(chan struct{}),
		quarantined: make([]finalizationSubmission, capacity),
	}
	e.ready = sync.NewCond(&e.mu)
	for range workers {
		go e.worker()
	}
	return e, nil
}

// submit transfers an exact reserved ticket after runner ownership has drained.
// Closing pool admission does not revoke reservations already held by runners;
// they can still submit until their owner explicitly calls stopAfterDrain.
func (e *finalizationExecutor) submit(existing *completionTicket, task finalizationTask) error {
	if e == nil || task.Finalize == nil || task.Terminal == nil {
		return ErrInvalidOptions
	}
	if existing == nil || existing.pool != e.pool {
		return errFinalizationTicket
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return ErrManagerClosed
	}
	// Check the current phase before the ring bound, so duplicate or stale
	// submissions are rejected consistently even while every slot is queued.
	e.pool.mu.Lock()
	reserved := existing.phase == completionReserved
	e.pool.mu.Unlock()
	if !reserved {
		return errFinalizationTicket
	}
	// This ring check precedes the transfer, so even a violated ownership
	// invariant cannot leave a reserved ticket stranded in a queue phase.
	if e.queued == len(e.queue) {
		return ErrBusy
	}
	if !existing.queue() {
		return errFinalizationTicket
	}
	index := (e.head + e.queued) % len(e.queue)
	e.queue[index] = finalizationSubmission{ticket: existing, task: task}
	e.queued++
	e.ready.Signal()
	return nil
}

// stopAfterDrain belongs to the shutdown owner after every runner has handed
// off its reserved ticket or completed a proven pre-launch rollback. It closes
// future admission and lets the fixed workers drain the existing finite ring.
func (e *finalizationExecutor) stopAfterDrain() {
	if e == nil {
		return
	}
	e.mu.Lock()
	if !e.stopped {
		e.stopped = true
		e.pool.close()
		e.ready.Broadcast()
	}
	e.mu.Unlock()
}

// wait only limits the caller's observation. A timeout never cancels callbacks,
// drops queued tasks, releases tickets, or closes the executor's admission.
func (e *finalizationExecutor) wait(ctx context.Context) error {
	if e == nil || ctx == nil {
		return ErrInvalidOptions
	}
	select {
	case <-e.done:
		return e.result()
	default:
	}
	select {
	case <-e.done:
		return e.result()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *finalizationExecutor) result() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	err := e.firstErr
	// There are only finitely many stage errors, irrespective of job count.
	// Preserve these admission-fencing failures even after an earlier ordinary
	// terminal error, without retaining an error for every completed job.
	for kind, exited := range e.exits {
		if exited && e.firstErr != finalizationExitErrors[kind] {
			err = errors.Join(err, finalizationExitErrors[kind])
		}
	}
	return err
}

func (e *finalizationExecutor) snapshot() finalizationExecutorSnapshot {
	if e == nil {
		return finalizationExecutorSnapshot{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return finalizationExecutorSnapshot{
		Capacity: len(e.queue), Workers: e.workers, LiveWorkers: e.liveWorkers,
		Queued: e.queued, Working: e.working, Quarantined: e.quarantineN,
		Stopped: e.stopped, Done: e.liveWorkers == 0, Completion: e.pool.snapshot(),
	}
}

func (e *finalizationExecutor) worker() {
	state := finalizationWorkerState{}
	defer e.workerExited(&state)
	for {
		e.mu.Lock()
		for e.queued == 0 && !e.stopped {
			e.ready.Wait()
		}
		if e.queued == 0 {
			state.normalExit = true
			e.mu.Unlock()
			return
		}
		submission := e.queue[e.head]
		e.queue[e.head] = finalizationSubmission{}
		e.head = (e.head + 1) % len(e.queue)
		e.queued--
		if !submission.ticket.work() {
			e.recordErrorLocked(errFinalizationTicket)
			e.mu.Unlock()
			continue
		}
		e.working++
		state = finalizationWorkerState{submission: submission, working: true, stage: finalizationWorkerFinalize}
		e.mu.Unlock()

		finalizeErr := callFinalization(submission.task)
		state.stage = finalizationWorkerTerminal
		terminalErr, returned := callTerminal(submission.task, finalizeErr)
		if terminalErr != nil {
			// Retain a terminal failure before the ticket's availability callback
			// can exit this goroutine without returning from release.
			e.mu.Lock()
			e.recordErrorLocked(terminalErr)
			e.mu.Unlock()
		}
		if !returned {
			// A panic does not prove terminal handling returned. Keep the exact
			// submission and working ticket reachable in a capacity-bounded
			// quarantine, while this same worker services the remaining queue.
			e.mu.Lock()
			e.working--
			state.working = false
			e.quarantineLocked(submission)
			state.quarantined = true
			e.mu.Unlock()
			state.stage = finalizationWorkerNotify
			notifyQuarantined(submission.task.OnQuarantined, terminalErr)
			state = finalizationWorkerState{}
			continue
		}

		// A normal terminal return completes this bounded lifetime even when
		// persistence failed. Workspace cleanup and its storage reservation
		// remain owned by the manager; they are not part of this release.
		state.stage = finalizationWorkerRelease
		releaseErr := releaseFinalizationTicket(submission.ticket)
		e.mu.Lock()
		e.working--
		state.working = false
		e.recordErrorLocked(releaseErr)
		if releaseErr != nil {
			e.pool.close()
		}
		e.mu.Unlock()
		if releaseErr != nil {
			// A partly returned availability notification fences admission just
			// like Goexit. Terminal handling already returned; never restore or
			// double-release its completed ticket to deliver this notification.
			state.stage = finalizationWorkerNotify
			notifyQuarantined(submission.task.OnQuarantined, releaseErr)
		}
		state = finalizationWorkerState{}
	}
}

// workerExited runs even for runtime.Goexit, which callback-local recover
// cannot intercept. The exact submission remains reachable until this guard
// has reconciled its ticket, active-worker count and admission fence.
func (e *finalizationExecutor) workerExited(state *finalizationWorkerState) {
	_ = recover()
	guardNotifyReturned := true
	defer func() {
		// A quarantine notification can itself call Goexit while this guard is
		// handling an earlier interrupted callback. This second defer always
		// records that interruption and completes the fixed worker-slot exit.
		if !guardNotifyReturned {
			e.mu.Lock()
			e.recordExitLocked(finalizationExitQuarantine)
			e.mu.Unlock()
		}
		e.finishWorkerSlot(state.normalExit)
	}()
	if state.normalExit {
		return
	}
	kind := finalizationExitWorker
	switch state.stage {
	case finalizationWorkerFinalize:
		kind = finalizationExitFinalize
	case finalizationWorkerTerminal:
		kind = finalizationExitTerminal
	case finalizationWorkerRelease:
		kind = finalizationExitRelease
	case finalizationWorkerNotify:
		kind = finalizationExitQuarantine
	}
	e.mu.Lock()
	if state.working {
		e.working--
		state.working = false
	}
	e.recordExitLocked(kind)
	if state.submission.ticket != nil && !state.quarantined {
		e.pool.mu.Lock()
		released := state.submission.ticket.phase == completionReleased
		e.pool.mu.Unlock()
		if !released {
			e.quarantineLocked(state.submission)
			state.quarantined = true
		}
	}
	e.mu.Unlock()
	if state.submission.ticket != nil && state.stage != finalizationWorkerNotify {
		guardNotifyReturned = false
		notifyQuarantined(state.submission.task.OnQuarantined, finalizationExitErrors[kind])
		guardNotifyReturned = true
	}
}

func (e *finalizationExecutor) finishWorkerSlot(normalExit bool) {
	e.mu.Lock()
	// An abnormal exit has stopped executing its owned callback before the
	// replacement starts. The logical slot stays live so wait cannot succeed
	// while queued work or a runner's existing handoff can still arrive.
	if !normalExit && (!e.stopped || e.queued != 0) {
		go e.worker()
	} else {
		e.liveWorkers--
	}
	if e.liveWorkers == 0 {
		close(e.done)
	}
	e.mu.Unlock()
}

func (e *finalizationExecutor) quarantineLocked(submission finalizationSubmission) {
	// No quarantined ticket is returned here. Pool capacity therefore bounds
	// the total entries in this fixed array without append or allocation.
	e.quarantined[e.quarantineN] = submission
	e.quarantineN++
	e.pool.close()
}

func (e *finalizationExecutor) recordExitLocked(kind finalizationExitKind) {
	e.exits[kind] = true
	e.recordErrorLocked(finalizationExitErrors[kind])
	// Closing launch admission does not stop handoffs of reservations already
	// owned by runners. Only their shutdown owner can stop the finite queue.
	e.pool.close()
}

func (e *finalizationExecutor) recordErrorLocked(err error) {
	// Retaining only the first error avoids an error backlog proportional to
	// all jobs processed during the manager's lifetime.
	if err != nil && e.firstErr == nil {
		e.firstErr = err
	}
}

func callFinalization(task finalizationTask) (err error) {
	returned := false
	defer func() {
		if !returned {
			_ = recover()
			err = errFinalizationPanic
		}
	}()
	ctx := task.FinalizeContext
	if ctx == nil {
		ctx = context.Background()
	}
	err = task.Finalize(ctx)
	returned = true
	return err
}

func callTerminal(task finalizationTask, finalizeErr error) (err error, returned bool) {
	defer func() {
		if !returned {
			_ = recover()
			err = errFinalizationTerminalPanic
		}
	}()
	ctx := task.TerminalContext
	if ctx == nil {
		ctx = context.Background()
	}
	err = task.Terminal(ctx, finalizeErr)
	returned = true
	return err, returned
}

func releaseFinalizationTicket(ticket *completionTicket) (err error) {
	returned := false
	defer func() {
		if !returned {
			_ = recover()
			err = errFinalizationReleasePanic
		}
	}()
	if !ticket.release() {
		err = errFinalizationTicket
	}
	returned = true
	return err
}

func notifyQuarantined(notify func(error), err error) {
	if notify != nil {
		defer func() { _ = recover() }()
		notify(err)
	}
}

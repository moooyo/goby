package media

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrDiagnosticAuthority  = errors.New("diagnostic_authority_lost")
	ErrDiagnosticAlreadyRun = errors.New("diagnostic_execution_already_run")
)

// DiagnosticExecutionOptions is a server-owned deployment snapshot, never an
// administrator request payload. Administrator and conversion-slot admission
// must precede construction. The execution copies mutable option collections.
type DiagnosticExecutionOptions struct {
	FFmpegPath          string
	CgroupParent        string
	ScratchDirectory    string
	LoaderDirectories   []string
	HardwareEnvironment map[string]string
}

type diagnosticExecutionBackend interface {
	execute(context.Context, DiagnosticSelection, func(context.Context) error, func(DiagnosticReport)) (DiagnosticReport, error)
	interrupt()
	close() error
}

type diagnosticExecutionFactory func(context.Context, diagnosticProcessOptions) (diagnosticExecutionBackend, error)

type diagnosticExecutionClose struct {
	done chan struct{}
	err  error
}

// DiagnosticExecution owns exactly one stage run and its resource session.
// A non-nil owner returned alongside a construction error still owns pending
// cleanup. Retain it and retry Close; it cannot run or release its admission
// slot until Close succeeds. Reports remain provisional after Run returns.
type DiagnosticExecution struct {
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	backend      diagnosticExecutionBackend
	initialError error
	started      bool
	stopping     bool
	closed       bool
	runDone      chan struct{}
	closing      *diagnosticExecutionClose
}

func NewDiagnosticExecution(ctx context.Context, options DiagnosticExecutionOptions) (*DiagnosticExecution, error) {
	return newDiagnosticExecution(ctx, options, openDiagnosticExecution)
}

// The factory is private and per-construction so tests can exercise ownership
// without global hooks, process creation, or a configurable execution backend.
func newDiagnosticExecution(ctx context.Context, options DiagnosticExecutionOptions, open diagnosticExecutionFactory) (*DiagnosticExecution, error) {
	if ctx == nil || open == nil || len(options.LoaderDirectories) > 16 || len(options.HardwareEnvironment) > 4 {
		return nil, ErrDiagnosticResources
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ownedOptions := diagnosticProcessOptions{FFmpegPath: options.FFmpegPath, CgroupParent: options.CgroupParent,
		ScratchDirectory: options.ScratchDirectory, LoaderDirectories: append([]string(nil), options.LoaderDirectories...)}
	if options.HardwareEnvironment != nil {
		ownedOptions.HardwareEnvironment = make(map[string]string, len(options.HardwareEnvironment))
		for key, value := range options.HardwareEnvironment {
			ownedOptions.HardwareEnvironment[key] = value
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	backend, err := open(runCtx, ownedOptions)
	if backend == nil {
		cancel()
		if err == nil {
			err = ErrDiagnosticResources
		}
		return nil, err
	}
	owner := &DiagnosticExecution{ctx: runCtx, cancel: cancel, backend: backend, initialError: err}
	if err != nil {
		owner.Cancel()
	}
	return owner, err
}

// Run may be called once, including a rejected or cancelled attempt. authorize
// is mandatory and is rechecked before the version command and every media
// command. It must honor its context. progress must synchronously store the
// supplied bounded snapshot without blocking on I/O; it must not call Close.
// The caller must Close the owner and confirm success before finalizing any
// report or releasing its conversion slot. Run never claims session closure.
func (owner *DiagnosticExecution) Run(selection DiagnosticSelection, authorize func(context.Context) error, progress func(DiagnosticReport)) (report DiagnosticReport, resultErr error) {
	if owner == nil {
		return diagnosticExecutionRejected(selection, "unavailable", "diagnostic_resources_unavailable"), ErrDiagnosticResources
	}
	owner.mu.Lock()
	if owner.started {
		owner.mu.Unlock()
		return diagnosticExecutionRejected(selection, "unavailable", "diagnostic_execution_already_run"), ErrDiagnosticAlreadyRun
	}
	owner.started = true
	owner.runDone = make(chan struct{})
	done := owner.runDone
	backend, runCtx, initialError, stopping := owner.backend, owner.ctx, owner.initialError, owner.stopping || owner.closed
	owner.mu.Unlock()
	defer close(done)
	if initialError != nil {
		return diagnosticExecutionRejected(selection, "unavailable", "diagnostic_resources_unavailable"), initialError
	}
	if stopping {
		return diagnosticExecutionRejected(selection, "cancelled", "diagnostic_cancelled"), context.Canceled
	}
	if backend == nil || runCtx == nil {
		return diagnosticExecutionRejected(selection, "unavailable", "diagnostic_resources_unavailable"), ErrDiagnosticResources
	}
	var emit func(DiagnosticReport)
	if progress != nil {
		emit = func(snapshot DiagnosticReport) {
			snapshot.SessionClosureRequired = true
			progress(snapshot)
		}
	}
	report, resultErr = backend.execute(runCtx, selection, authorize, emit)
	report.SessionClosureRequired = true
	return report, resultErr
}

// Cancel prevents a future Run and interrupts current work. It does not close
// resources, discard a pending child, or establish permission to release a slot.
func (owner *DiagnosticExecution) Cancel() {
	if owner == nil {
		return
	}
	owner.mu.Lock()
	owner.stopping = true
	cancel, backend := owner.cancel, owner.backend
	owner.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if backend != nil {
		backend.interrupt()
	}
}

// Close cancels and gives the whole call one fixed waiting budget. It never
// closes a session while Run still owns it. A timed-out backend close remains
// attached to this owner; later calls join the same attempt instead of starting
// overlapping cleanup. A completed failed close can be retried explicitly.
func (owner *DiagnosticExecution) Close() error {
	return owner.closeWithin(diagnosticCloseDeadline)
}

func (owner *DiagnosticExecution) closeWithin(wait time.Duration) error {
	if owner == nil {
		return nil
	}
	owner.Cancel()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	owner.mu.Lock()
	if owner.closed {
		owner.mu.Unlock()
		return nil
	}
	done := owner.runDone
	owner.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-timer.C:
			return ErrDiagnosticClosure
		}
	}
	owner.mu.Lock()
	if owner.closed {
		owner.mu.Unlock()
		return nil
	}
	if owner.backend == nil {
		owner.closed = true
		owner.mu.Unlock()
		return nil
	}
	attempt := owner.closing
	if attempt == nil {
		attempt = &diagnosticExecutionClose{done: make(chan struct{})}
		owner.closing = attempt
		backend := owner.backend
		go func() {
			err := backend.close()
			owner.mu.Lock()
			attempt.err = err
			if err == nil {
				owner.closed, owner.backend = true, nil
			}
			close(attempt.done)
			owner.mu.Unlock()
		}()
	}
	owner.mu.Unlock()
	select {
	case <-attempt.done:
		owner.mu.Lock()
		err := attempt.err
		if err != nil && owner.closing == attempt {
			owner.closing = nil
		}
		owner.mu.Unlock()
		if err != nil {
			return ErrDiagnosticClosure
		}
		return nil
	case <-timer.C:
		return ErrDiagnosticClosure
	}
}

func diagnosticExecutionRejected(selection DiagnosticSelection, state, code string) DiagnosticReport {
	now := time.Now().UTC()
	return DiagnosticReport{Version: 1, Selection: selection, StartedAt: now, FinishedAt: now,
		State: state, Code: code, SessionClosureRequired: true}
}

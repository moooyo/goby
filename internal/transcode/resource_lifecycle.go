package transcode

import (
	"context"
	"sync"
	"time"
)

type resourceLifecycleStage uint8

const (
	resourceProcessRetired resourceLifecycleStage = iota
	resourceWritersDrained
	resourceWorkspaceSealed
	resourceTerminalPersisted
	resourceLifecycleStages
)

type resourceLifecycleEvent struct {
	Stage    resourceLifecycleStage
	At       time.Time
	Verified bool
}

type resourceLifecycleSnapshot struct {
	ProcessRetired     time.Time
	WritersDrained     time.Time
	WorkspaceSealed    time.Time
	TerminalPersisted  time.Time
	RetirementVerified bool
	RetirementError    error
}

// resourceLifecycle records separate process, output, accounting and durable
// status boundaries. A retirement barrier must prove that the complete command
// domain is empty after the leader and command-output writers have been joined.
// Sending a process-group signal, observing progress, or reading a directory is
// not such a proof. Callbacks must return promptly and must not perform I/O.
//
// Without a barrier, Run reports an unverified retirement observation only when
// it returns. That observation never authorizes an early execution-slot release.
// A failed barrier records no successful retirement event; its enclosing owner
// must retain the command domain and the reserved capacity for cleanup.
type resourceLifecycle struct {
	mu         sync.Mutex
	stages     [resourceLifecycleStages]time.Time
	verified   bool
	retireErr  error
	retireOnce sync.Once
	retire     func(context.Context) error
	onStage    func(resourceLifecycleEvent)
}

type resourceLifecycleContextKey struct{}

func withResourceLifecycle(ctx context.Context, lifecycle *resourceLifecycle) context.Context {
	return context.WithValue(ctx, resourceLifecycleContextKey{}, lifecycle)
}

func resourceLifecycleFromContext(ctx context.Context) *resourceLifecycle {
	lifecycle, _ := ctx.Value(resourceLifecycleContextKey{}).(*resourceLifecycle)
	return lifecycle
}

// retireProcesses is called early only for finite jobs after Cmd.Wait. Live
// publication can still start caption-extraction commands while observers drain,
// so its complete command-domain barrier belongs at the final Run boundary.
func (l *resourceLifecycle) retireProcesses(ctx context.Context) error {
	if l == nil || l.retire == nil {
		return nil
	}
	l.retireOnce.Do(func() {
		err := l.retire(ctx)
		l.mu.Lock()
		l.retireErr = err
		l.mu.Unlock()
		if err == nil {
			l.record(resourceProcessRetired, true)
		}
	})
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.retireErr
}

// runnerReturned must execute after all runner-owned descriptor-close defers.
// Observer drains and workspace mutation remain distinct from process retirement.
func (l *resourceLifecycle) runnerReturned(ctx context.Context) error {
	if l == nil {
		return nil
	}
	err := l.retireProcesses(ctx)
	if l.retire == nil {
		l.record(resourceProcessRetired, false)
	}
	l.record(resourceWritersDrained, false)
	return err
}

func (l *resourceLifecycle) record(stage resourceLifecycleStage, verified bool) {
	if l == nil || stage >= resourceLifecycleStages {
		return
	}
	l.mu.Lock()
	if !l.stages[stage].IsZero() {
		l.mu.Unlock()
		return
	}
	at := time.Now()
	l.stages[stage] = at
	if stage == resourceProcessRetired {
		l.verified = verified
	}
	notify := l.onStage
	l.mu.Unlock()
	if notify != nil {
		notify(resourceLifecycleEvent{Stage: stage, At: at, Verified: verified})
	}
}

func (l *resourceLifecycle) snapshot() resourceLifecycleSnapshot {
	if l == nil {
		return resourceLifecycleSnapshot{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return resourceLifecycleSnapshot{
		ProcessRetired: l.stages[resourceProcessRetired], WritersDrained: l.stages[resourceWritersDrained],
		WorkspaceSealed: l.stages[resourceWorkspaceSealed], TerminalPersisted: l.stages[resourceTerminalPersisted],
		RetirementVerified: l.verified, RetirementError: l.retireErr,
	}
}

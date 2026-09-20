//go:build linux

package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

type mediaOperationBlockedCleanup struct {
	entered    chan struct{}
	cancelled  chan struct{}
	release    chan struct{}
	executions atomic.Int32
	applies    atomic.Int32
}

func (e *mediaOperationBlockedCleanup) Execute(context.Context, library.MediaOperationWork, func(library.MediaOperationProgress) error) (library.MediaOperationResult, error) {
	e.executions.Add(1)
	return library.MediaOperationResult{}, library.ErrUnavailable
}
func (e *mediaOperationBlockedCleanup) Apply(context.Context, library.MediaOperationWork, func(library.MediaOperationProgress) error) error {
	e.applies.Add(1)
	return library.ErrUnavailable
}
func (e *mediaOperationBlockedCleanup) Discard(ctx context.Context, work library.MediaOperationWork) error {
	if !work.Discard {
		return library.ErrMediaOperationState
	}
	close(e.entered)
	<-ctx.Done()
	close(e.cancelled)
	// Represent an already admitted storage operation that has not returned.
	// Shutdown must retain its worker even after the context was cancelled.
	<-e.release
	return ctx.Err()
}

func TestDisabledMediaOperationCloseJoinsAdmittedCleanup(t *testing.T) {
	f, actor, _ := adminMediaOperationReadyHTTPFixture(t)
	op := readyMediaOperationCancellationFixture(t, f, actor, "shutdown-cleanup")
	r := f.app.mediaOperations
	if r.processingEnabled {
		t.Fatal("fixture unexpectedly enabled media processing")
	}
	executor := &mediaOperationBlockedCleanup{entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(executor.release) }) }
	defer release()
	r.executors[library.MediaOperationRemoveSubtitle] = executor
	if _, err := r.Cancel(f.ctx, actor, op.ID, op.Revision); err != nil {
		t.Fatal(err)
	}
	select {
	case <-executor.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("disabled runtime did not admit explicit cleanup")
	}
	short, cancelShort := context.WithTimeout(context.Background(), 30*time.Millisecond)
	err := r.Close(short)
	cancelShort()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close detached blocked cleanup: %v", err)
	}
	select {
	case <-executor.cancelled:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel cleanup")
	}
	select {
	case <-r.done:
		t.Fatal("runtime completed before the storage worker returned")
	default:
	}
	release()
	joined, cancelJoin := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelJoin()
	if err := r.Close(joined); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Cancel(f.ctx, actor, op.ID, op.Revision); !errors.Is(err, library.ErrUnavailable) {
		t.Fatalf("closed runtime admitted cancellation: %v", err)
	}
	if executor.executions.Load() != 0 || executor.applies.Load() != 0 {
		t.Fatal("cleanup started new media processing or publication")
	}
	current, err := f.app.library.GetMediaOperation(f.ctx, actor, op.ID)
	if err != nil || current.State != "recovery_required" || current.WorkerToken != "" {
		t.Fatalf("uncertain cleanup was reported as complete: %+v, %v", current, err)
	}
}

package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/library"
)

type managerOwnershipScans struct {
	ScanExecutor
	checks      atomic.Int64
	unavailable bool
	probe       func(context.Context) error
	admit       func(context.Context, string) (library.ScanAdmission, error)
}

func (scans *managerOwnershipScans) Available() bool {
	return !scans.unavailable && (scans.ScanExecutor == nil || scans.ScanExecutor.Available())
}

func (scans *managerOwnershipScans) CheckOwnership(ctx context.Context) error {
	scans.checks.Add(1)
	if scans.probe != nil {
		return scans.probe(ctx)
	}
	if scans.ScanExecutor != nil {
		return scans.ScanExecutor.CheckOwnership(ctx)
	}
	return nil
}

func (scans *managerOwnershipScans) ScanUpdates() <-chan struct{} {
	if scans.ScanExecutor == nil {
		return nil
	}
	return scans.ScanExecutor.ScanUpdates()
}

func (scans *managerOwnershipScans) AdmitTaskScan(ctx context.Context, childID string) (library.ScanAdmission, error) {
	if scans.admit != nil {
		return scans.admit(ctx, childID)
	}
	if scans.ScanExecutor == nil {
		return library.ScanAdmission{}, errors.New("unexpected scan admission")
	}
	return scans.ScanExecutor.AdmitTaskScan(ctx, childID)
}

func newManagerOwnershipFixture(t *testing.T, parent context.Context, store *Store, scans ScanExecutor) *Manager {
	t.Helper()
	ctx, cancel := context.WithCancel(parent)
	t.Cleanup(cancel)
	manager := &Manager{store: store, scans: scans, ctx: ctx, cancel: cancel,
		options: ManagerOptions{ReconcileInterval: 250 * time.Millisecond, BatchSize: MaxPageLimit,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
		wake: make(chan struct{}, 1), loopDone: make(chan struct{}), done: make(chan struct{}),
		childOffsets: make(map[string]int), runtimeDeadlines: make(map[string]time.Time), executions: make(map[string]*workerExecution)}
	// Tests drive complete phases directly, so no coordinator goroutine owns
	// these cursors. A triggered ownership fence can still join shutdown.
	close(manager.loopDone)
	return manager
}

func TestManagerScheduleBackoffDoesNotSupplyOwnershipProof(t *testing.T) {
	scans := &managerOwnershipScans{probe: func(context.Context) error { return library.ErrUnavailable }}
	manager := newManagerOwnershipFixture(t, context.Background(), nil, scans)
	manager.nextScheduleAttempt = time.Now().Add(time.Hour)
	probed, err := manager.schedulePass(manager.ctx)
	if err != nil || probed || scans.checks.Load() != 0 {
		t.Fatalf("schedule backoff invented an ownership observation: probed=%t checks=%d error=%v", probed, scans.checks.Load(), err)
	}
	if _, err := manager.reconcilePass(manager.ctx, false, probed); !errors.Is(err, library.ErrUnavailable) || scans.checks.Load() != 1 {
		t.Fatalf("idle backoff omitted its independent ownership probe: checks=%d error=%v", scans.checks.Load(), err)
	}
}

func TestManagerOwnershipReuseRetainsActiveReapingAndShutdownChecks(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		scans := &managerOwnershipScans{probe: func(context.Context) error { return library.ErrUnavailable }}
		manager := newManagerOwnershipFixture(t, context.Background(), nil, scans)
		done := make(chan struct{})
		close(done)
		manager.executions["completed-child"] = &workerExecution{done: done, cancel: func() { t.Fatal("ownership failure reaped a worker") }}
		if _, err := manager.reconcilePass(manager.ctx, shutdown, true); !errors.Is(err, library.ErrUnavailable) || scans.checks.Load() != 1 || len(manager.executions) != 1 {
			t.Fatalf("active reconciliation reused an earlier probe before reaping: shutdown=%t checks=%d error=%v", shutdown, scans.checks.Load(), err)
		}
	}
	scans := &managerOwnershipScans{unavailable: true}
	manager := newManagerOwnershipFixture(t, context.Background(), nil, scans)
	if _, err := manager.reconcilePass(manager.ctx, false, true); !errors.Is(err, library.ErrUnavailable) || scans.checks.Load() != 0 {
		t.Fatalf("idle reuse skipped the cheap availability fence: checks=%d error=%v", scans.checks.Load(), err)
	}
}

func TestManagerManualOwnershipFailureKeepsRequestErrorPrecedence(t *testing.T) {
	for _, operation := range []string{"start", "stop", "stop definition"} {
		for _, cancelled := range []bool{false, true} {
			scans := &managerOwnershipScans{probe: func(context.Context) error { return library.ErrUnavailable }}
			manager := newManagerOwnershipFixture(t, context.Background(), &Store{}, scans)
			ctx, cancel := context.WithCancel(manager.ctx)
			if cancelled {
				cancel()
			}
			var err error
			switch operation {
			case "start":
				_, err = manager.Start(ctx, Actor{}, StartRequest{RequestID: strings.Repeat("x", MaxRequestIDBytes+1)})
			case "stop":
				_, err = manager.Stop(ctx, Actor{}, "")
			case "stop definition":
				_, err = manager.StopByDefinition(ctx, Actor{}, "")
			}
			cancel()
			if !errors.Is(err, library.ErrUnavailable) || scans.checks.Load() != 1 {
				t.Fatalf("manual %s changed ownership error precedence: cancelled=%t checks=%d error=%v", operation, cancelled, scans.checks.Load(), err)
			}
			select {
			case <-manager.done:
			case <-time.After(2 * time.Second):
				t.Fatal("manual ownership failure did not finish fenced shutdown")
			}
		}
	}
}

func TestManagerObservesRollbackClassificationWithoutAnotherOwnershipProbe(t *testing.T) {
	flushFailure := errors.New("pending event flush failed")
	rollbackFailure := errors.New("independent rollback failed")
	for _, test := range []struct {
		name      string
		cause     error
		failure   error
		ownerLost bool
	}{
		{"expired confirmed rollback", context.DeadlineExceeded,
			errors.Join(context.DeadlineExceeded, pgx.ErrTxCommitRollback), false},
		{"expired failed rollback", context.DeadlineExceeded,
			errors.Join(context.DeadlineExceeded, fmt.Errorf("%w: %w", library.ErrUnavailable, rollbackFailure)), true},
		{"flush failed rollback", flushFailure,
			errors.Join(flushFailure, fmt.Errorf("%w: %w", library.ErrUnavailable, rollbackFailure)), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			scans := &managerOwnershipScans{}
			manager := newManagerOwnershipFixture(t, context.Background(), nil, scans)
			manager.scheduleReady = true
			manager.observeFailure(fmt.Errorf("task transaction failed: %w", test.failure))
			manager.mu.Lock()
			closing, closeErr := manager.closing, manager.closeErr
			manager.mu.Unlock()
			if scans.checks.Load() != 0 {
				t.Fatal("rollback classification required another ownership probe")
			}
			if !test.ownerLost {
				if closing || closeErr != nil || manager.ctx.Err() != nil || !manager.Available() {
					t.Fatalf("confirmed rollback unnecessarily fenced a healthy manager: closing=%t error=%v", closing, closeErr)
				}
				if !manager.enter() {
					t.Fatal("confirmed rollback prevented a later task admission")
				}
				manager.operations.Done()
				select {
				case <-manager.done:
					t.Fatal("confirmed rollback began manager shutdown")
				default:
				}
				return
			}
			if !closing || !errors.Is(manager.ctx.Err(), context.Canceled) || manager.Available() ||
				!errors.Is(closeErr, ErrUnavailable) || !errors.Is(closeErr, library.ErrUnavailable) ||
				!errors.Is(closeErr, test.cause) || !errors.Is(closeErr, rollbackFailure) {
				t.Fatalf("classified rollback failure did not immediately fence admission with every cause: closing=%t error=%v", closing, closeErr)
			}
			if manager.enter() {
				manager.operations.Done()
				t.Fatal("classified owner loss admitted more task work")
			}
			closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := manager.Close(closeContext); !errors.Is(err, test.cause) || !errors.Is(err, library.ErrUnavailable) {
				t.Fatalf("fenced shutdown lost its classified transaction cause: %v", err)
			}
		})
	}
}

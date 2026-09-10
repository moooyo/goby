package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestServerCloseCancelsRetentionWithinCallerBudgetAndDrainsLater(t *testing.T) {
	retentionContext, cancelRetention := context.WithCancel(context.Background())
	rollbackGate := make(chan struct{})
	retentionDone := make(chan struct{})
	var releaseOnce sync.Once
	releaseRollback := func() { releaseOnce.Do(func() { close(rollbackGate) }) }
	app := &Server{activityCancel: cancelRetention, activityDone: retentionDone, sockets: newSocketRuntime()}

	// Model a pruning worker already entering its independent rollback phase.
	// The gate preserves that cleanup after the Close caller's budget expires.
	go func() {
		<-retentionContext.Done()
		<-rollbackGate
		close(retentionDone)
	}()
	// Shutdown may already be in progress when another caller invokes Close.
	// Control that existing worker with channels without a database fixture.
	app.sockets.once.Do(func() {
		go func() {
			_ = app.waitActivityRetention(context.Background())
			close(app.sockets.done)
		}()
	})
	t.Cleanup(func() {
		cancelRetention()
		releaseRollback()
		app.sockets.cancel()
		select {
		case <-app.sockets.done:
		case <-time.After(2 * time.Second):
			t.Error("retention shutdown fixture did not drain")
		}
	})

	caller, cancelCaller := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancelCaller()
	closed := make(chan error, 1)
	go func() { closed <- app.Close(caller) }()
	select {
	case <-retentionContext.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel retention while its rollback remained blocked")
	}
	select {
	case err := <-closed:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Close returned before cleanup with the wrong outcome: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("retention rollback blocked Close beyond the caller's budget")
	}
	select {
	case <-retentionDone:
		t.Fatal("caller timeout abandoned the retained cleanup")
	default:
	}
	select {
	case <-app.sockets.done:
		t.Fatal("server shutdown completed before retention cleanup")
	default:
	}

	releaseRollback()
	select {
	case <-app.sockets.done:
	case <-time.After(2 * time.Second):
		t.Fatal("background shutdown did not finish after retention cleanup")
	}
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCleanup()
	if err := app.Close(cleanup); err != nil {
		t.Fatalf("a later Close did not observe the completed cleanup: %v", err)
	}
}

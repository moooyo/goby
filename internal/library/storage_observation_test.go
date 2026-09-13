package library

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStorageObservationTimeoutRetainsDescriptorUntilWorkerFinishes(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var lifetime storageObservationLifetime
	slots := make(chan struct{}, 1)
	entered, unblock, released := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	var descriptorError atomic.Bool
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(unblock) })
		select {
		case <-released:
		case <-time.After(5 * time.Second):
			t.Error("blocked observation did not release its retained descriptor")
		}
	})
	result := make(chan error, 1)
	go func() {
		result <- runStorageObservationWithLimit(context.Background(), slots, 250*time.Millisecond,
			[]*storageObservationLifetime{&lifetime}, func(context.Context) error {
				close(entered)
				<-unblock
				if _, err := root.Stat("."); err != nil {
					descriptorError.Store(true)
				}
				return nil
			})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("observation did not enter its controlled filesystem wait")
	}
	select {
	case err := <-result:
		if !errors.Is(err, errStorageObservationUnavailable) {
			t.Fatalf("storage wait did not fail closed at its deadline: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("caller remained blocked on filesystem observation")
	}
	if err := lifetime.retire(func() error {
		err := root.Close()
		close(released)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
		t.Fatal("caller retirement closed a descriptor still owned by its worker")
	default:
	}
	called := false
	if err := runStorageObservationWithLimit(context.Background(), slots, time.Second, nil, func(context.Context) error {
		called = true
		return nil
	}); !errors.Is(err, errStorageObservationUnavailable) || called {
		t.Fatalf("a timed-out worker did not retain its capacity slot: called=%t err=%v", called, err)
	}
	releaseOnce.Do(func() { close(unblock) })
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("finished observation did not close retired resources")
	}
	if descriptorError.Load() {
		t.Fatal("the timed-out worker lost its retained descriptor")
	}
	if _, err := root.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("retired root remained open after worker completion: %v", err)
	}
}

func TestStorageObservationCancellationAndRetirementDoNotRunLateWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var lifetime storageObservationLifetime
	called := false
	if err := runStorageObservationWithLimit(ctx, make(chan struct{}, 1), time.Second,
		[]*storageObservationLifetime{&lifetime}, func(context.Context) error {
			called = true
			return nil
		}); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("cancelled admission started filesystem work: called=%t err=%v", called, err)
	}
	closes := 0
	closeResources := func() error { closes++; return nil }
	if err := lifetime.retire(closeResources); err != nil {
		t.Fatal(err)
	}
	if err := lifetime.retire(closeResources); err != nil || closes != 1 {
		t.Fatalf("retirement was not idempotent: closes=%d err=%v", closes, err)
	}
	if err := runStorageObservationWithLimit(context.Background(), make(chan struct{}, 1), time.Second,
		[]*storageObservationLifetime{&lifetime}, func(context.Context) error {
			called = true
			return nil
		}); !errors.Is(err, errStorageObservationUnavailable) || called {
		t.Fatalf("retired evidence was used by later work: called=%t err=%v", called, err)
	}
}

func TestStorageObservationPanicReleasesAdmission(t *testing.T) {
	slots := make(chan struct{}, 1)
	var lifetime storageObservationLifetime
	if err := runStorageObservationWithLimit(context.Background(), slots, time.Second,
		[]*storageObservationLifetime{&lifetime}, func(context.Context) error {
			panic("controlled observation failure")
		}); !errors.Is(err, errStorageObservationUnavailable) {
		t.Fatalf("panicking observation was not rejected: %v", err)
	}
	closed := false
	if err := lifetime.retire(func() error { closed = true; return nil }); err != nil || !closed {
		t.Fatalf("panicking worker retained its evidence: closed=%t err=%v", closed, err)
	}
}

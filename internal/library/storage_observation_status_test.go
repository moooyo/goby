package library

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// These tests share the production semaphore and intentionally run serially.
func TestStorageObservationSnapshotDoesNotConsumeFullCapacity(t *testing.T) {
	initial := StorageObservationSnapshot()
	if initial.Active != 0 || initial.Capacity != cap(storageObservationSlots) || initial.Capacity == 0 {
		t.Fatalf("unexpected idle observation inventory: %+v", initial)
	}
	reserved := 0
	snapshotDone := make(chan struct{})
	snapshotStarted := false
	t.Cleanup(func() {
		for reserved > 0 {
			select {
			case <-storageObservationSlots:
				reserved--
			default:
				reserved = 0
			}
		}
		if snapshotStarted {
			select {
			case <-snapshotDone:
			case <-time.After(5 * time.Second):
				t.Error("snapshot did not return after capacity was released")
			}
		}
	})
	for reserved < initial.Capacity {
		select {
		case storageObservationSlots <- struct{}{}:
			reserved++
		default:
			t.Fatal("snapshot consumed an observation slot")
		}
	}
	result := make(chan StorageObservationStatus, 1)
	snapshotStarted = true
	go func() {
		defer close(snapshotDone)
		result <- StorageObservationSnapshot()
	}()
	select {
	case status := <-result:
		if status.Active != initial.Capacity || status.Capacity != initial.Capacity {
			t.Fatalf("full admission inventory = %+v", status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot waited for full observation capacity")
	}
	if len(storageObservationSlots) != reserved {
		t.Fatal("snapshot changed the observation semaphore")
	}
}

func TestStorageObservationSnapshotRetainsCancelledWorker(t *testing.T) {
	initial := StorageObservationSnapshot()
	if initial.Active != 0 || initial.Capacity == 0 {
		t.Fatalf("unexpected idle observation inventory: %+v", initial)
	}
	ctx, cancel := context.WithCancel(context.Background())
	entered, unblock, callerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(unblock) }) }
	waitUntilIdle := func() bool {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			if StorageObservationSnapshot().Active == 0 {
				return true
			}
			select {
			case <-ticker.C:
			case <-timer.C:
				return false
			}
		}
	}
	t.Cleanup(func() {
		cancel()
		release()
		select {
		case <-callerDone:
		case <-time.After(5 * time.Second):
			t.Error("observation caller did not finish after cleanup")
		}
		if !waitUntilIdle() {
			t.Error("observation worker retained a global slot after cleanup")
		}
	})
	result := make(chan error, 1)
	go func() {
		defer close(callerDone)
		result <- runStorageObservationWithLimit(ctx, storageObservationSlots, time.Minute, nil, func(context.Context) error {
			close(entered)
			<-unblock
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("observation worker did not enter its controlled wait")
	}
	if status := StorageObservationSnapshot(); status.Active != 1 || status.Capacity != initial.Capacity {
		t.Fatalf("running worker inventory = %+v", status)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled caller returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled caller waited for its blocked worker")
	}
	if status := StorageObservationSnapshot(); status.Active != 1 || status.Capacity != initial.Capacity {
		t.Fatalf("canceled caller hid its still-running worker: %+v", status)
	}
	release()
	if !waitUntilIdle() {
		t.Fatal("finished worker remained active in the observation snapshot")
	}
}

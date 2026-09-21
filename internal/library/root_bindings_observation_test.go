package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func rootBindingObservationTestStore(t *testing.T) (*Store, libraryRoot) {
	t.Helper()
	path := t.TempDir()
	return &Store{roots: []approvedRoot{{path: path}}}, libraryRoot{allowedPath: path, path: path, relativePath: "."}
}

func waitRootBindingObservationIdle(t *testing.T) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for StorageObservationSnapshot().Active != 0 {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Error("owned observation did not finish its actual cleanup")
			return
		}
	}
}

// These tests use the production semaphore without replacing it or draining
// tokens. Every occupied slot belongs to a real worker released by this helper.
func occupyRootBindingObservationSlots(t *testing.T, store *Store, root libraryRoot) {
	t.Helper()
	status := StorageObservationSnapshot()
	if status.Active != 0 || status.Capacity == 0 {
		t.Fatalf("unexpected preexisting observation state: %+v", status)
	}
	started := make(chan struct{}, status.Capacity)
	returned := make(chan struct{}, status.Capacity)
	release := make(chan struct{})
	t.Cleanup(func() {
		close(release)
		for index := 0; index < status.Capacity; index++ {
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Error("controlled observation caller did not return")
			}
		}
		waitRootBindingObservationIdle(t)
	})
	for index := 0; index < status.Capacity; index++ {
		go func() {
			defer func() { returned <- struct{}{} }()
			_, _ = store.observeRootBindingWith(context.Background(), root, func(context.Context, string, libraryRoot) (RootTopologySnapshot, error) {
				started <- struct{}{}
				<-release
				return RootTopologySnapshot{}, nil
			})
		}()
	}
	for index := 0; index < status.Capacity; index++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("controlled observation did not acquire its slot")
		}
	}
}

func TestRootBindingObservationCallerDeadlineRetainsWorkAndDeferredClose(t *testing.T) {
	if StorageObservationSnapshot().Active != 0 {
		t.Fatal("another observation is active before the isolated lifetime test")
	}
	store, root := rootBindingObservationTestStore(t)
	filePath := filepath.Join(root.path, "held-file")
	if err := os.WriteFile(filePath, []byte("owned descriptor"), 0600); err != nil {
		t.Fatal(err)
	}
	opened := make(chan *os.File, 1)
	finishWork, closeEntered, finishClose, fileClosed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var workOnce, closeOnce sync.Once
	releaseWork := func() { workOnce.Do(func() { close(finishWork) }) }
	releaseClose := func() { closeOnce.Do(func() { close(finishClose) }) }
	type outcome struct {
		snapshot RootTopologySnapshot
		err      error
	}
	returned := make(chan outcome, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(func() { cancel(); releaseWork(); releaseClose(); waitRootBindingObservationIdle(t) })
	wanted := rootBindingReadUnitSnapshot()
	go func() {
		snapshot, err := store.observeRootBindingWith(ctx, root, func(_ context.Context, configured string, observed libraryRoot) (_ RootTopologySnapshot, result error) {
			if configured != root.allowedPath || observed != root {
				return RootTopologySnapshot{}, ErrInvalidInput
			}
			file, err := os.Open(filePath)
			if err != nil {
				return RootTopologySnapshot{}, err
			}
			opened <- file
			defer func() {
				close(closeEntered)
				<-finishClose
				result = errors.Join(result, file.Close())
				close(fileClosed)
			}()
			<-finishWork
			return wanted, nil
		})
		returned <- outcome{snapshot, err}
	}()
	var held *os.File
	select {
	case held = <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("filesystem worker did not open its actual descriptor")
	}
	select {
	case result := <-returned:
		if !errors.Is(result.err, context.DeadlineExceeded) || !reflect.DeepEqual(result.snapshot, RootTopologySnapshot{}) {
			t.Fatal("caller deadline returned a late or partial snapshot")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("caller waited for blocked filesystem work")
	}
	if StorageObservationSnapshot().Active != 1 {
		t.Fatal("caller timeout released the still-running observation")
	}
	if !store.mu.TryLock() {
		t.Fatal("filesystem work retained Store.mu")
	}
	store.mu.Unlock()
	releaseWork()
	select {
	case <-closeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not begin its deferred close")
	}
	if _, err := held.Stat(); err != nil {
		t.Fatal("descriptor closed before controlled cleanup completed")
	}
	if StorageObservationSnapshot().Active != 1 {
		t.Fatal("observation slot was released before deferred close")
	}
	releaseClose()
	select {
	case <-fileClosed:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not close its descriptor")
	}
	waitRootBindingObservationIdle(t)
	if _, err := held.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("finished worker retained the descriptor")
	}
}

func TestRootBindingObservationCapacityRefusesBeforeFilesystemWork(t *testing.T) {
	store, root := rootBindingObservationTestStore(t)
	occupyRootBindingObservationSlots(t, store, root)
	var called atomic.Bool
	observed, err := store.observeRootBindingWith(context.Background(), root, func(context.Context, string, libraryRoot) (RootTopologySnapshot, error) {
		called.Store(true)
		return rootBindingReadUnitSnapshot(), nil
	})
	if !errors.Is(err, errStorageObservationUnavailable) || called.Load() || !reflect.DeepEqual(observed, RootTopologySnapshot{}) {
		t.Fatal("full observation capacity ran filesystem work or claimed a snapshot")
	}
	status := StorageObservationSnapshot()
	if status.Active != status.Capacity {
		t.Fatal("failed admission consumed or released another worker's slot")
	}
}

func TestRootBindingObservationSuccessfulResultWaitsForCleanup(t *testing.T) {
	store, root := rootBindingObservationTestStore(t)
	wanted := rootBindingReadUnitSnapshot()
	var closed atomic.Bool
	observed, err := store.observeRootBindingWith(context.Background(), root, func(context.Context, string, libraryRoot) (RootTopologySnapshot, error) {
		defer closed.Store(true)
		return wanted, nil
	})
	if err != nil || !closed.Load() || !reflect.DeepEqual(observed, wanted) {
		t.Fatal("snapshot was delivered before its worker cleanup")
	}
	if StorageObservationSnapshot().Active != 0 {
		t.Fatal("successful observation retained its slot")
	}
}

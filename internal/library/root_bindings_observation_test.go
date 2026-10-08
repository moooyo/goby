package library

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	return primaryDirectoryUnitStore(t, []approvedRoot{{path: path}}), libraryRoot{allowedPath: path, path: path, relativePath: "."}
}

func waitRootBindingObservationIdle(t *testing.T) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for originalMediaReadGovernor.Stats().Active != 0 {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Error("owned observation did not finish its actual cleanup")
			return
		}
	}
}

// These tests use the production primary governor without replacing it or
// draining tokens. Every occupied phase belongs to an actual retained worker.
func occupyRootBindingObservationSlots(t *testing.T, store *Store, root libraryRoot) {
	t.Helper()
	status := originalMediaReadGovernor.Stats()
	if status.Active != 0 {
		t.Fatalf("unexpected preexisting observation state: %+v", status)
	}
	const capacity = 3
	type worker struct {
		done      <-chan struct{}
		directory *os.File
	}
	started := make(chan worker, capacity)
	returned := make(chan error, capacity)
	caller, cancel := context.WithCancel(context.Background())
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	var workers []worker
	joinedCallers := 0
	release := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		close(release)
		for index := joinedCallers; index < capacity; index++ {
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Error("controlled observation caller did not return")
			}
		}
		for _, worker := range workers {
			select {
			case <-worker.done:
			case <-time.After(5 * time.Second):
				t.Error("actual observation owner did not complete retirement")
			}
			if err := worker.directory.Close(); !errors.Is(err, os.ErrClosed) {
				t.Errorf("retired observation kept its actual descriptor: %v", err)
			}
		}
		if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners {
			t.Errorf("observation cleanup retained worker owners: before=%d after=%d", beforeOwners, owners)
		}
	})
	for index := 0; index < capacity; index++ {
		go func() {
			_, err := store.observeRootBindingWith(caller, root, func(ctx context.Context, path string, _ libraryRoot) (RootTopologySnapshot, error) {
				directory, err := os.Open(path)
				if err != nil {
					return RootTopologySnapshot{}, err
				}
				defer directory.Close()
				if _, err := directory.ReadDir(1); err != nil && !errors.Is(err, io.EOF) {
					return RootTopologySnapshot{}, err
				}
				operation := PrimaryRootIOFromContext(ctx)
				if operation == nil {
					return RootTopologySnapshot{}, ErrUnavailable
				}
				started <- worker{done: operation.handle.state.done, directory: directory}
				<-release
				return RootTopologySnapshot{}, nil
			})
			returned <- err
		}()
	}
	for index := 0; index < capacity; index++ {
		select {
		case worker := <-started:
			workers = append(workers, worker)
		case <-time.After(5 * time.Second):
			t.Fatal("controlled observation did not acquire its slot")
		}
	}
	cancel()
	for index := 0; index < capacity; index++ {
		select {
		case err := <-returned:
			joinedCallers++
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled observation caller returned %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cancelled observation caller did not return")
		}
	}
	if status := originalMediaReadGovernor.Stats(); status.Active != capacity || status.Background != 0 || status.Queued != 0 || status.ActiveRoots != 1 || status.ActiveDomains != 1 {
		t.Fatalf("caller cancellation changed actual saturated foreground phases: %+v", status)
	}
	if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners+capacity {
		t.Fatalf("caller cancellation dropped actual worker owners: before=%d after=%d", beforeOwners, owners)
	}
	for _, worker := range workers {
		if _, err := worker.directory.Stat(); err != nil {
			t.Fatalf("cancelled caller closed a still-owned descriptor: %v", err)
		}
	}
}

func TestRootBindingObservationCallerDeadlineRetainsWorkAndDeferredClose(t *testing.T) {
	if originalMediaReadGovernor.Stats().Active != 0 {
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
	if originalMediaReadGovernor.Stats().Active != 1 {
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
	if originalMediaReadGovernor.Stats().Active != 1 {
		t.Fatal("observation slot was released before deferred close")
	}
	releaseClose()
	select {
	case <-fileClosed:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not close its descriptor")
	}
	waitRootBindingObservationIdle(t)
	if err := held.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("finished worker retained the descriptor")
	}
}

func TestRootBindingObservationCapacityRefusesBeforeFilesystemWork(t *testing.T) {
	store, root := rootBindingObservationTestStore(t)
	occupyRootBindingObservationSlots(t, store, root)
	var called atomic.Bool
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	observed, err := store.observeRootBindingWith(ctx, root, func(context.Context, string, libraryRoot) (RootTopologySnapshot, error) {
		called.Store(true)
		return rootBindingReadUnitSnapshot(), nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || called.Load() || !reflect.DeepEqual(observed, RootTopologySnapshot{}) {
		t.Fatal("full observation capacity ran filesystem work or claimed a snapshot")
	}
	status := originalMediaReadGovernor.Stats()
	if status.Active != 3 {
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
	if originalMediaReadGovernor.Stats().Active != 0 {
		t.Fatal("successful observation retained its slot")
	}
}

func TestRootBindingObservationSaturatedDomainLeavesIndependentDomainReadable(t *testing.T) {
	blocked, blockedRoot := rootBindingObservationTestStore(t)
	occupyRootBindingObservationSlots(t, blocked, blockedRoot)
	healthy, healthyRoot := rootBindingObservationTestStore(t)
	path := filepath.Join(healthyRoot.path, "actual-observation")
	if err := os.WriteFile(path, []byte("healthy storage"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := healthy.observeRootBindingWith(ctx, healthyRoot, func(context.Context, string, libraryRoot) (RootTopologySnapshot, error) {
		file, err := os.Open(path)
		if err != nil {
			return RootTopologySnapshot{}, err
		}
		defer file.Close()
		var data [32]byte
		if _, err := file.Read(data[:]); err != nil {
			return RootTopologySnapshot{}, err
		}
		if status := originalMediaReadGovernor.Stats(); status.Active != 4 || status.ActiveDomains != 2 || status.ActiveRoots != 2 {
			return RootTopologySnapshot{}, fmt.Errorf("independent domain did not retain its own admitted phase: %+v", status)
		}
		return rootBindingReadUnitSnapshot(), nil
	})
	if err != nil {
		t.Fatalf("a saturated configured domain blocked independent actual storage: %v", err)
	}
}

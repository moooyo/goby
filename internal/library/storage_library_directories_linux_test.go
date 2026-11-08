//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type libraryStorageDirectoryTestFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	store   *Store
	actor   identity.Principal
	library Library
	root    RegisteredRootInfo
}

func newLibraryStorageDirectoryTestFixture(t *testing.T) libraryStorageDirectoryTestFixture {
	t.Helper()
	ctx, pool, store, allowed, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "library-storage-directory-admin")
	path := filepath.Dir(libraryIntegrationFile(t, allowed, "registered/marker.txt", "registered directory fixture"))
	library := libraryIntegrationCreate(t, ctx, store, "Directory capacity", "movies", path)
	roots, err := store.ListRegisteredRoots(ctx, actor, library.ID)
	if err != nil || len(roots) != 1 {
		t.Fatalf("discover directory registration: roots=%+v err=%v", roots, err)
	}
	return libraryStorageDirectoryTestFixture{ctx: ctx, pool: pool, store: store, actor: actor, library: library, root: roots[0]}
}

func (fixture libraryStorageDirectoryTestFixture) read(observe libraryStorageDirectoryDescriptorObserver) ([]LibraryStorageDirectory, error) {
	return fixture.store.libraryStorageDirectoriesWithObserver(fixture.ctx,
		&catalogAdministrator{actor: fixture.actor, audience: identity.AdministratorNative}, observe)
}

func requireUnknownLibraryStorageDirectoryCapacity(t *testing.T, item LibraryStorageDirectory) {
	t.Helper()
	if item.TotalBytes != nil || item.UsedBytes != nil || item.AvailableBytes != nil {
		t.Fatalf("uncertain directory published filesystem capacity: %+v", item)
	}
}

func awaitLibraryStorageDirectoryMutation(t *testing.T, changed <-chan error) {
	t.Helper()
	select {
	case err := <-changed:
		if err != nil {
			t.Fatalf("directory observation fixture could not apply its change: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("directory observation did not reach its change boundary")
	}
}

func TestLibraryStorageDirectoriesCancellationRetainsDescriptorPhaseAndOwnerWithoutLocks(t *testing.T) {
	fixture := newLibraryStorageDirectoryTestFixture(t)
	beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
	if beforeIO.Active != 0 {
		t.Fatal("unexpected preexisting primary filesystem phase")
	}
	release, open := directoryLifecycleGate(t)
	caller, cancel := context.WithCancel(fixture.ctx)
	defer cancel()
	type workerState struct {
		directory *os.File
		done      <-chan struct{}
		err       error
	}
	entered := make(chan workerState, 1)
	type requestResult struct {
		items []LibraryStorageDirectory
		err   error
	}
	returned := make(chan requestResult, 1)
	callerDone := make(chan struct{})
	var actualDone <-chan struct{}
	fixture.store.ownership.mu.Lock()
	defer fixture.store.ownership.mu.Unlock()
	defer func() {
		cancel()
		open()
		primaryRootIOTestWait(t, callerDone, "library directory caller cleanup")
		if actualDone != nil {
			primaryRootIOTestWait(t, actualDone, "actual library directory observation retirement")
		}
	}()
	go func() {
		defer close(callerDone)
		items, err := fixture.store.libraryStorageDirectoriesWithObserver(caller,
			&catalogAdministrator{actor: fixture.actor, audience: identity.AdministratorNative},
			func(work context.Context, directory *os.File) (libraryStorageDirectoryObservation, error) {
				value, err := observeLibraryStorageDirectoryDescriptor(work, directory)
				state := workerState{directory: directory, err: err}
				if value.capacity == nil {
					state.err = errors.Join(state.err, errors.New("real held directory has no native filesystem capacity"))
				}
				if operation := PrimaryRootIOFromContext(work); operation != nil {
					state.done = operation.handle.state.done
				} else {
					state.err = errors.Join(state.err, errors.New("filesystem observation has no primary owner"))
				}
				entered <- state
				<-release
				return value, state.err
			})
		returned <- requestResult{items: items, err: err}
	}()
	var held *os.File
	select {
	case state := <-entered:
		held, actualDone = state.directory, state.done
		if state.err != nil {
			t.Fatalf("real descriptor observation failed before cancellation: %v", state.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("directory read waited for catalog ownership during filesystem observation")
	}
	if !fixture.store.mu.TryLock() {
		t.Fatal("directory filesystem observation retained Store.mu")
	}
	fixture.store.mu.Unlock()
	if acquired := fixture.pool.Stat().AcquiredConns(); acquired != 1 {
		t.Fatalf("filesystem observation retained a transaction beyond the reserved owner: acquired=%d", acquired)
	}
	named, namedErr := os.Stat(fixture.root.Path)
	opened, openedErr := held.Stat()
	if namedErr != nil || openedErr != nil || !os.SameFile(named, opened) {
		t.Fatalf("observer did not receive the exact registered directory: named=%v opened=%v", namedErr, openedErr)
	}
	cancel()
	select {
	case result := <-returned:
		if !errors.Is(result.err, context.Canceled) || result.items != nil {
			t.Fatalf("cancelled caller published directory capacity: items=%+v err=%v", result.items, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled directory caller did not return within its bounded wait")
	}
	if status := originalMediaReadGovernor.Stats(); status.Active != beforeIO.Active+1 || status.Background != beforeIO.Background || status.ActiveRoots != beforeIO.ActiveRoots+1 || status.ActiveDomains != beforeIO.ActiveDomains+1 {
		t.Fatalf("caller cancellation dropped its actual foreground filesystem phase: before=%+v after=%+v", beforeIO, status)
	}
	if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners+1 {
		t.Fatalf("caller cancellation retired its actual worker owner: before=%d after=%d", beforeOwners, owners)
	}
	if _, err := held.Stat(); err != nil {
		t.Fatalf("caller cancellation closed a descriptor still held by the worker: %v", err)
	}
	select {
	case <-actualDone:
		t.Fatal("actual observation retired before its filesystem callback exited")
	default:
	}
	open()
	primaryRootIOTestWait(t, actualDone, "released directory filesystem observation")
	if _, err := held.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("actual observation retirement kept its descriptor open: %v", err)
	}
	primaryRootIOTestCondition(t, func() bool {
		status := originalMediaReadGovernor.Stats()
		return status.Active == beforeIO.Active && status.ActiveRoots == beforeIO.ActiveRoots && status.ActiveDomains == beforeIO.ActiveDomains &&
			originalMediaReadOwners.Stats().RegisteredOwners == beforeOwners
	}, "released library directory phase and owner")
}

func TestLibraryStorageDirectoriesRejectRegisteredAndApprovedDirectoryReplacement(t *testing.T) {
	for _, target := range []string{"registered", "approved"} {
		t.Run(target, func(t *testing.T) {
			fixture := newLibraryStorageDirectoryTestFixture(t)
			path := fixture.root.Path
			if target == "approved" {
				path = fixture.root.AllowedPath
			}
			retained := path + "-retained"
			t.Cleanup(func() {
				if _, err := os.Lstat(retained); errors.Is(err, os.ErrNotExist) {
					return
				} else if err != nil {
					t.Errorf("inspect retained directory during restoration: %v", err)
					return
				}
				if target == "approved" {
					if err := os.Remove(fixture.root.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
						t.Errorf("remove replacement registered directory: %v", err)
					}
				}
				if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Errorf("remove replacement directory: %v", err)
				}
				if err := os.Rename(retained, path); err != nil {
					t.Errorf("restore retained fixture directory: %v", err)
				}
			})
			changed := make(chan error, 1)
			items, err := fixture.read(func(work context.Context, directory *os.File) (libraryStorageDirectoryObservation, error) {
				value, err := observeLibraryStorageDirectoryDescriptor(work, directory)
				if err == nil && value.capacity == nil {
					err = errors.New("held directory lacks native capacity before replacement")
				}
				if err == nil {
					err = os.Rename(path, retained)
				}
				if err == nil {
					err = os.Mkdir(path, 0o700)
				}
				if err == nil && target == "approved" {
					err = os.Mkdir(fixture.root.Path, 0o700)
				}
				changed <- err
				return value, err
			})
			awaitLibraryStorageDirectoryMutation(t, changed)
			if err != nil || len(items) != 1 || items[0].Path != fixture.root.Path {
				t.Fatalf("directory replacement lost current registration metadata: items=%+v err=%v", items, err)
			}
			requireUnknownLibraryStorageDirectoryCapacity(t, items[0])
		})
	}
}

func TestLibraryStorageDirectoriesReconcileCurrentRegistrationMetadata(t *testing.T) {
	for _, change := range []string{"revision", "path added", "path removed"} {
		t.Run(change, func(t *testing.T) {
			fixture := newLibraryStorageDirectoryTestFixture(t)
			added := filepath.Join(fixture.root.AllowedPath, "added")
			changed := make(chan error, 1)
			items, err := fixture.read(func(work context.Context, directory *os.File) (libraryStorageDirectoryObservation, error) {
				value, err := observeLibraryStorageDirectoryDescriptor(work, directory)
				if err == nil && value.capacity == nil {
					err = errors.New("held directory lacks native capacity before registration change")
				}
				if err == nil {
					switch change {
					case "revision":
						_, err = fixture.pool.Exec(work, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1`, fixture.root.RootID)
					case "path added":
						_, err = fixture.pool.Exec(work, `INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
							VALUES ('directory-capacity-added-root', $1, $2, $3, 'added')`, fixture.library.ID, added, fixture.root.AllowedPath)
					case "path removed":
						_, err = fixture.pool.Exec(work, `DELETE FROM library_roots WHERE id=$1`, fixture.root.RootID)
					}
				}
				changed <- err
				return value, err
			})
			awaitLibraryStorageDirectoryMutation(t, changed)
			if err != nil {
				t.Fatalf("current registration read failed: %v", err)
			}
			switch change {
			case "revision":
				if len(items) != 1 || items[0].Path != fixture.root.Path {
					t.Fatalf("revision change lost current directory metadata: %+v", items)
				}
				requireUnknownLibraryStorageDirectoryCapacity(t, items[0])
			case "path added":
				byPath := make(map[string]LibraryStorageDirectory, len(items))
				for _, item := range items {
					byPath[item.Path] = item
				}
				current, retained := byPath[added], byPath[fixture.root.Path]
				if len(items) != 2 || len(byPath) != 2 || current.Path != added || retained.Path != fixture.root.Path || retained.TotalBytes == nil {
					t.Fatalf("new registration inherited or displaced an earlier directory observation: %+v", items)
				}
				requireUnknownLibraryStorageDirectoryCapacity(t, current)
			case "path removed":
				if len(items) != 0 {
					t.Fatalf("removed registration retained an earlier capacity observation: %+v", items)
				}
			}
		})
	}
}

func TestLibraryStorageDirectoriesDoNotObserveConflictingExactPathMappings(t *testing.T) {
	fixture := newLibraryStorageDirectoryTestFixture(t)
	second := libraryIntegrationCreate(t, fixture.ctx, fixture.store, "Conflicting directory capacity", "movies", fixture.root.Path)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE library_roots SET allowed_path=path, relative_path='.',
		binding_revision=binding_revision+1 WHERE library_id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	var observed atomic.Int32
	items, err := fixture.read(func(work context.Context, directory *os.File) (libraryStorageDirectoryObservation, error) {
		observed.Add(1)
		return observeLibraryStorageDirectoryDescriptor(work, directory)
	})
	if err != nil || len(items) != 1 || items[0].Path != fixture.root.Path || observed.Load() != 0 {
		t.Fatalf("conflicting exact-path registrations selected a filesystem: items=%+v observed=%d err=%v", items, observed.Load(), err)
	}
	requireUnknownLibraryStorageDirectoryCapacity(t, items[0])
}

func TestLibraryStorageDirectoriesKeepObserverFailuresUnknown(t *testing.T) {
	fixture := newLibraryStorageDirectoryTestFixture(t)
	for _, cause := range []error{os.ErrPermission, errors.New("injected filesystem observation failure")} {
		t.Run(fmt.Sprint(cause), func(t *testing.T) {
			observed := make(chan error, 1)
			items, err := fixture.read(func(work context.Context, directory *os.File) (libraryStorageDirectoryObservation, error) {
				value, readErr := observeLibraryStorageDirectoryDescriptor(work, directory)
				if readErr == nil && value.capacity == nil {
					readErr = errors.New("held directory lacks native capacity before injected failure")
				}
				observed <- readErr
				return value, errors.Join(readErr, cause)
			})
			awaitLibraryStorageDirectoryMutation(t, observed)
			if err != nil || len(items) != 1 || items[0].Path != fixture.root.Path {
				t.Fatalf("observer failure erased current directory metadata: items=%+v err=%v", items, err)
			}
			requireUnknownLibraryStorageDirectoryCapacity(t, items[0])
		})
	}
}

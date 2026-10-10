//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

type storageRootsResult struct {
	items []StorageRoot
	err   error
}

func startStorageRootsRequest(t *testing.T, ctx context.Context, store *Store, actor identity.Principal, reader storageRootReader) <-chan storageRootsResult {
	t.Helper()
	caller, cancel := context.WithCancel(ctx)
	finished := make(chan storageRootsResult, 1)
	done := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		primaryRootIOTestWait(t, done, "storage-root caller cleanup")
	})
	go func() {
		defer close(done)
		defer cancel()
		items, err := store.storageRootsWithReader(caller, &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}, reader)
		finished <- storageRootsResult{items: items, err: err}
	}()
	return finished
}

func awaitStorageRootsResult(t *testing.T, finished <-chan storageRootsResult) storageRootsResult {
	t.Helper()
	select {
	case result := <-finished:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("storage-root caller did not finish within its bounded wait")
		return storageRootsResult{}
	}
}

func setStorageRootsFixture(store *Store, paths ...string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.roots = nil
	store.configuredRoots = nil
	for _, path := range paths {
		store.roots = append(store.roots, approvedRoot{path: path})
		store.configuredRoots = append(store.configuredRoots, configuredStorageRoot{path: path, absolute: path, anchor: path})
	}
}

func TestStorageRootsObserveOneEntryAndKeepConfigurationOrder(t *testing.T) {
	ctx, pool, store, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "storage-roots-order-admin")
	empty := filepath.Join(approved, "empty")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	populated := filepath.Dir(libraryIntegrationFile(t, approved, "populated/first.txt", "first"))
	libraryIntegrationFile(t, populated, "second.txt", "second")
	missing := filepath.Join(approved, "missing")
	file := libraryIntegrationFile(t, approved, "regular.txt", "regular file")
	setStorageRootsFixture(store, populated, missing, empty, file, populated)
	var reads int
	var deadline time.Time
	reader := func(work context.Context, directory *os.File) error {
		reads++
		current, ok := work.Deadline()
		if !ok || !deadline.IsZero() && !current.Equal(deadline) {
			t.Error("storage roots did not share one request deadline")
		}
		deadline = current
		return readStorageRootDirectory(work, directory)
	}
	items, err := store.storageRootsWithReader(ctx, &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}, reader)
	want := []StorageRoot{{Path: populated, Available: true}, {Path: missing}, {Path: empty, Available: true}, {Path: file}, {Path: populated, Available: true}}
	if err != nil || !reflect.DeepEqual(items, want) || reads != 3 {
		t.Fatalf("storage availability changed order, duplicates, or readability: items=%+v reads=%d err=%v", items, reads, err)
	}
	directory, err := os.Open(populated)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := readStorageRootDirectory(ctx, directory); err != nil {
		t.Fatal(err)
	}
	remaining, err := directory.ReadDir(1)
	if err != nil || len(remaining) != 1 {
		t.Fatalf("availability enumerated more than one directory entry: %v", err)
	}
}

func TestStorageRootsCancellationRetainsWorkersPhasesAndRootIsolation(t *testing.T) {
	ctx, pool, store, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "storage-roots-cancel-admin")
	libraryIntegrationFile(t, approved, "marker.txt", "root")
	second := t.TempDir()
	libraryIntegrationFile(t, second, "marker.txt", "second root")
	setStorageRootsFixture(store, approved, second)
	beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
	release, open := directoryLifecycleGate(t)
	entered := make(chan error, 4)
	held := make(chan *os.File, 4)
	var reads atomic.Int32
	reader := func(work context.Context, directory *os.File) error {
		readErr := readStorageRootDirectory(work, directory)
		reads.Add(1)
		held <- directory
		entered <- readErr
		<-release
		return readErr
	}
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make([]<-chan storageRootsResult, 3)
	for index := range finished {
		finished[index] = startStorageRootsRequest(t, requestCtx, store, actor, reader)
	}
	for range finished {
		awaitDirectoryLifecycleRead(t, entered)
	}
	cancel()
	for _, result := range finished {
		if got := awaitStorageRootsResult(t, result); !errors.Is(got.err, context.Canceled) || got.items != nil {
			t.Fatalf("cancelled root request published partial availability: %+v", got)
		}
	}
	if len(serverDirectoryWorkers) != 3 || originalMediaReadGovernor.Stats().Active != beforeIO.Active+3 || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners+3 {
		t.Fatal("cancellation released an actual directory worker or primary phase")
	}
	for range finished {
		if _, err := (<-held).Stat(); err != nil {
			t.Fatalf("cancellation closed a descriptor still owned by the reader: %v", err)
		}
	}
	blocked, cancelBlocked := context.WithTimeout(ctx, 150*time.Millisecond)
	items, err := store.storageRootsWithReader(blocked, &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}, reader)
	cancelBlocked()
	if !errors.Is(err, context.DeadlineExceeded) || items != nil || reads.Load() != 3 {
		t.Fatalf("same-root saturation bypassed admission or became Available=false: items=%+v reads=%d err=%v", items, reads.Load(), err)
	}
	// Reorder only this trusted fixture's configuration. The independent root
	// can still use the fourth worker while the first root's domain is full.
	setStorageRootsFixture(store, second, approved)
	independentCtx, cancelIndependent := context.WithCancel(ctx)
	independent := startStorageRootsRequest(t, independentCtx, store, actor, reader)
	awaitDirectoryLifecycleRead(t, entered)
	cancelIndependent()
	if got := awaitStorageRootsResult(t, independent); !errors.Is(got.err, context.Canceled) || got.items != nil {
		t.Fatalf("independent root caller did not cancel: %+v", got)
	}
	if len(serverDirectoryWorkers) != 4 || originalMediaReadGovernor.Stats().Active != beforeIO.Active+4 {
		t.Fatal("independent root did not retain its separate fourth phase")
	}
	bounded, cancelBounded := context.WithTimeout(ctx, 150*time.Millisecond)
	items, err = store.storageRootsWithReader(bounded, &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}, reader)
	cancelBounded()
	if !errors.Is(err, context.DeadlineExceeded) || items != nil || reads.Load() != 4 || len(serverDirectoryWorkers) != 4 {
		t.Fatalf("global directory worker budget was bypassed: reads=%d err=%v", reads.Load(), err)
	}
	open()
	primaryRootIOTestCondition(t, func() bool {
		return len(serverDirectoryWorkers) == 0 && originalMediaReadGovernor.Stats().Active == beforeIO.Active && originalMediaReadOwners.Stats().RegisteredOwners == beforeOwners
	}, "actual storage-root worker retirement")
	items, err = store.StorageRoots(ctx, actor, identity.AdministratorNative)
	if err != nil || !reflect.DeepEqual(items, []StorageRoot{{Path: second, Available: true}, {Path: approved, Available: true}}) || reads.Load() != 4 {
		t.Fatalf("storage roots did not recover after syscall completion: items=%+v reads=%d err=%v", items, reads.Load(), err)
	}
}

func TestStorageRootsShutdownWaitsForActualDirectoryClose(t *testing.T) {
	ctx, pool, store, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "storage-roots-close-admin")
	libraryIntegrationFile(t, approved, "marker.txt", "root")
	release, open := directoryLifecycleGate(t)
	entered := make(chan error, 1)
	reader := func(work context.Context, directory *os.File) error {
		err := readStorageRootDirectory(work, directory)
		entered <- err
		<-release
		return err
	}
	finished := startStorageRootsRequest(t, ctx, store, actor, reader)
	awaitDirectoryLifecycleRead(t, entered)
	closing, cancelClose := context.WithTimeout(ctx, 150*time.Millisecond)
	closeErr := store.Close(closing)
	cancelClose()
	if !errors.Is(closeErr, context.DeadlineExceeded) {
		t.Fatalf("shutdown abandoned blocked filesystem work: %v", closeErr)
	}
	if got := awaitStorageRootsResult(t, finished); got.err == nil || got.items != nil {
		t.Fatalf("shutdown published storage-root availability: %+v", got)
	}
	select {
	case <-store.done:
		t.Fatal("shutdown completed before the filesystem reader exited")
	default:
	}
	open()
	if err := store.Close(ctx); err != nil || len(serverDirectoryWorkers) != 0 {
		t.Fatalf("shutdown did not drain the actual reader: %v", err)
	}
}

func TestStorageRootsRecheckAdministratorAfterPrimaryAdmissionWait(t *testing.T) {
	ctx, pool, store, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "storage-roots-wait-admin")
	libraryIntegrationFile(t, approved, "marker.txt", "root")
	release, open := directoryLifecycleGate(t)
	entered := make(chan error, 4)
	var reads atomic.Int32
	reader := func(work context.Context, directory *os.File) error {
		err := readStorageRootDirectory(work, directory)
		reads.Add(1)
		entered <- err
		<-release
		return err
	}
	finished := make([]<-chan storageRootsResult, 3)
	for index := range finished {
		finished[index] = startStorageRootsRequest(t, ctx, store, actor, reader)
	}
	for range finished {
		awaitDirectoryLifecycleRead(t, entered)
	}
	beforeQueued := originalMediaReadGovernor.Stats().Queued
	waiting := startStorageRootsRequest(t, ctx, store, actor, reader)
	primaryRootIOTestCondition(t, func() bool {
		return originalMediaReadGovernor.Stats().Queued == beforeQueued+1
	}, "queued storage-root observation")
	if _, err := pool.Exec(ctx, `UPDATE users SET is_administrator=false WHERE id=$1`, actor.User.ID); err != nil {
		t.Fatal(err)
	}
	open()
	for _, request := range append(finished, waiting) {
		if got := awaitStorageRootsResult(t, request); !errors.Is(got.err, ErrForbidden) || got.items != nil {
			t.Fatalf("revoked administrator passed an observation boundary: %+v", got)
		}
	}
	if reads.Load() != 3 {
		t.Fatalf("queued reader observed storage with authority revoked during admission: reads=%d", reads.Load())
	}
}

func TestStorageRootsRecheckAuthorityAndNamedIdentityAfterRead(t *testing.T) {
	for _, change := range []string{"account", "credential", "directory", "symlink"} {
		t.Run(change, func(t *testing.T) {
			ctx, pool, store, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			actor := metadataEditTestActor(t, ctx, pool, "storage-roots-final-admin")
			libraryIntegrationFile(t, approved, "marker.txt", "root")
			release, open := directoryLifecycleGate(t)
			entered := make(chan error, 1)
			reader := func(work context.Context, directory *os.File) error {
				err := readStorageRootDirectory(work, directory)
				entered <- err
				<-release
				return err
			}
			finished := startStorageRootsRequest(t, ctx, store, actor, reader)
			awaitDirectoryLifecycleRead(t, entered)
			want := ErrForbidden
			switch change {
			case "account":
				if _, err := pool.Exec(ctx, `UPDATE users SET is_administrator=false WHERE id=$1`, actor.User.ID); err != nil {
					t.Fatal(err)
				}
			case "credential":
				if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
					t.Fatal(err)
				}
			default:
				want = ErrUnavailable
				if err := os.Rename(approved, approved+"-retained"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_ = os.Remove(approved)
					_ = os.Rename(approved+"-retained", approved)
				})
				if change == "symlink" {
					if err := os.Symlink(t.TempDir(), approved); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Mkdir(approved, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			open()
			if got := awaitStorageRootsResult(t, finished); !errors.Is(got.err, want) || got.items != nil {
				t.Fatalf("a stale authority or replaced anchor published availability: %+v", got)
			}
		})
	}
}

func TestStorageRootsKeepAdmissionAndIOFailuresDistinct(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "storage-roots-error-admin")
	for _, cause := range []error{ErrBusy, syscall.EMFILE, syscall.EIO, context.DeadlineExceeded} {
		items, err := store.storageRootsWithReader(ctx, &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}, func(context.Context, *os.File) error { return cause })
		if items != nil || !errors.Is(err, cause) {
			t.Fatalf("operational failure became a negative storage observation: cause=%v items=%+v err=%v", cause, items, err)
		}
	}
	items, err := store.storageRootsWithReader(ctx, &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}, func(context.Context, *os.File) error { return os.ErrPermission })
	if err != nil || len(items) != 1 || items[0].Available {
		t.Fatalf("read denial lost its explicit unavailable observation: items=%+v err=%v", items, err)
	}
	items, err = store.storageRootsWithReader(ctx, &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}, func(context.Context, *os.File) error { return io.EOF })
	if err != nil || len(items) != 1 || !items[0].Available {
		t.Fatalf("an empty directory was marked unreadable: items=%+v err=%v", items, err)
	}
}

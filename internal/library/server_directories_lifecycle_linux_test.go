//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func startDirectoryLifecycleRequest(t *testing.T, ctx context.Context, store *Store, actor identity.Principal, path string, reader serverDirectoryReader) <-chan serverDirectoryResult {
	t.Helper()
	caller, cancel := context.WithCancel(ctx)
	finished := make(chan serverDirectoryResult, 1)
	done := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("directory caller remained active during fixture cleanup")
		}
	})
	go func() {
		defer close(done)
		defer cancel()
		page, err := store.serverDirectoriesWithReader(caller, &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}, path, 0, 100, false, reader)
		finished <- serverDirectoryResult{page: page, err: err}
	}()
	return finished
}

func awaitDirectoryLifecycleRead(t *testing.T, entered <-chan error) {
	t.Helper()
	select {
	case err := <-entered:
		if err != nil {
			t.Fatalf("real directory read failed before its lifecycle gate: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("directory worker did not reach its read gate")
	}
}

func awaitDirectoryLifecycleResult(t *testing.T, finished <-chan serverDirectoryResult) serverDirectoryResult {
	t.Helper()
	select {
	case result := <-finished:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("directory caller did not finish within its bounded wait")
		return serverDirectoryResult{}
	}
}

func directoryLifecycleGate(t *testing.T) (chan struct{}, func()) {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() {
		open()
		if !imageStoreWaitWorkerCleanup(serverDirectoryWorkers) {
			t.Error("directory lifecycle test retained a filesystem worker slot")
		}
	})
	return release, open
}

func TestServerDirectoryLifecycleRechecksAccountAndCredentialAfterRealRead(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		name := "administrator removed"
		if revoke {
			name = "credential revoked"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, store, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			actor := metadataEditTestActor(t, ctx, pool, "directory-lifecycle-admin")
			libraryIntegrationFile(t, approved, "visible/marker.txt", "directory fixture")
			release, open := directoryLifecycleGate(t)
			entered := make(chan error, 1)
			reader := func(ctx context.Context, roots []string, path string, start, limit int, validate bool) (ServerDirectoryPage, error) {
				// Use the real descriptor-backed read and revalidation before
				// pausing completion. No synthetic page bypasses the filesystem.
				page, err := store.readServerDirectories(ctx, roots, path, start, limit, validate)
				entered <- err
				<-release
				return page, err
			}
			finished := startDirectoryLifecycleRequest(t, ctx, store, actor, approved, reader)
			awaitDirectoryLifecycleRead(t, entered)
			if revoke {
				if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := pool.Exec(ctx, `UPDATE users SET is_administrator=false WHERE id=$1`, actor.User.ID); err != nil {
					t.Fatal(err)
				}
			}
			open()
			result := awaitDirectoryLifecycleResult(t, finished)
			if !errors.Is(result.err, ErrForbidden) || result.page.Path != "" || len(result.page.Items) != 0 {
				t.Fatalf("stale authority exposed a completed directory listing: %v", result.err)
			}
		})
	}
}

func TestServerDirectoryLifecycleCancellationKeepsFourSlotsAndShutdownDrains(t *testing.T) {
	ctx, pool, store, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "directory-cancel-admin")
	libraryIntegrationFile(t, approved, "visible/marker.txt", "directory fixture")
	if cap(serverDirectoryWorkers) != 4 {
		t.Fatal("directory worker capacity contract changed")
	}
	release, open := directoryLifecycleGate(t)
	entered := make(chan error, 5)
	var reads atomic.Int32
	reader := func(ctx context.Context, roots []string, path string, start, limit int, validate bool) (ServerDirectoryPage, error) {
		page, err := store.readServerDirectories(ctx, roots, path, start, limit, validate)
		reads.Add(1)
		entered <- err
		// Model storage completion that cannot be interrupted by a caller's
		// context; resource ownership must last until this callback returns.
		<-release
		return page, err
	}
	requestCtx, cancelRequests := context.WithCancel(ctx)
	defer cancelRequests()
	finished := make([]<-chan serverDirectoryResult, 4)
	for index := range finished {
		finished[index] = startDirectoryLifecycleRequest(t, requestCtx, store, actor, approved, reader)
	}
	for range finished {
		awaitDirectoryLifecycleRead(t, entered)
	}
	cancelRequests()
	for _, call := range finished {
		result := awaitDirectoryLifecycleResult(t, call)
		if !errors.Is(result.err, context.Canceled) || result.page.Path != "" || len(result.page.Items) != 0 {
			t.Fatalf("cancelled caller retained a listing or waited for storage: %v", result.err)
		}
	}
	if len(serverDirectoryWorkers) != 4 {
		t.Fatal("caller cancellation released a still-running directory worker")
	}
	fifthCtx, cancelFifth := context.WithTimeout(ctx, 250*time.Millisecond)
	_, fifthErr := store.serverDirectoriesWithReader(fifthCtx, &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}, approved, 0, 100, false, reader)
	cancelFifth()
	if !errors.Is(fifthErr, context.DeadlineExceeded) || reads.Load() != 4 || len(serverDirectoryWorkers) != 4 {
		t.Fatalf("a fifth request bypassed the occupied worker bound: %v reads=%d", fifthErr, reads.Load())
	}
	closeCtx, cancelClose := context.WithTimeout(ctx, 250*time.Millisecond)
	closeErr := store.Close(closeCtx)
	cancelClose()
	if !errors.Is(closeErr, context.DeadlineExceeded) {
		t.Fatalf("shutdown abandoned admitted directory workers: %v", closeErr)
	}
	select {
	case <-store.done:
		t.Fatal("shutdown completed while directory callbacks were still active")
	default:
	}
	open()
	if err := store.Close(ctx); err != nil {
		t.Fatalf("shutdown did not drain released directory work: %v", err)
	}
	if reads.Load() != 4 || len(serverDirectoryWorkers) != 0 {
		t.Fatal("shutdown leaked a slot or executed an unadmitted reader")
	}
	if _, err := store.BrowseServerDirectories(ctx, actor, identity.AdministratorNative, approved, 0, 100); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed store admitted another directory worker: %v", err)
	}
}

func TestServerDirectoryLifecycleReauthorizesNamedPathBeforeDelayedRead(t *testing.T) {
	ctx, pool, store, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "directory-path-swap-admin")
	requested := filepath.Dir(libraryIntegrationFile(t, approved, "requested/local.txt", "local fixture"))
	outside := t.TempDir()
	libraryIntegrationFile(t, outside, "private/marker.txt", "outside fixture")
	release, open := directoryLifecycleGate(t)
	entered := make(chan error, 1)
	reader := func(ctx context.Context, roots []string, path string, start, limit int, validate bool) (ServerDirectoryPage, error) {
		// Admission and the first credential check have completed. Delay the
		// actual filesystem call, then use its ordinary authorization and
		// descriptor revalidation rather than trusting the earlier path string.
		entered <- nil
		<-release
		return store.readServerDirectories(ctx, roots, path, start, limit, validate)
	}
	finished := startDirectoryLifecycleRequest(t, ctx, store, actor, requested, reader)
	awaitDirectoryLifecycleRead(t, entered)
	if err := os.Rename(requested, filepath.Join(approved, "retained")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, requested); err != nil {
		t.Fatal(err)
	}
	open()
	result := awaitDirectoryLifecycleResult(t, finished)
	if !errors.Is(result.err, ErrForbidden) || result.page.Path != "" || len(result.page.Items) != 0 {
		t.Fatalf("a delayed directory read followed a new outside target: %v", result.err)
	}
}

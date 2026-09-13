package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRootBindingPathsDeletionRetiresOnlyCommittedLibraryAnchors(t *testing.T) {
	ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	firstPath := libraryIntegrationFile(t, allowedRoot, "first/marker.txt", "first-library")
	secondPath := libraryIntegrationFile(t, allowedRoot, "second/marker.txt", "second-library")
	firstLibrary := libraryIntegrationCreate(t, ctx, store, "First root", "movies", filepath.Dir(firstPath))
	secondLibrary := libraryIntegrationCreate(t, ctx, store, "Second root", "movies", filepath.Dir(secondPath))
	readRoot := func(libraryID string) libraryRoot {
		var root libraryRoot
		if err := pool.QueryRow(ctx, `SELECT id, library_id, path, allowed_path, relative_path
			FROM library_roots WHERE library_id = $1`, libraryID).
			Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath); err != nil {
			t.Fatal(err)
		}
		return root
	}
	first, second := readRoot(firstLibrary.ID), readRoot(secondLibrary.ID)
	firstAnchor := rootBindingPathsCandidate(t, allowedRoot)
	secondAnchor := rootBindingPathsCandidate(t, allowedRoot)
	store.mu.Lock()
	store.installRootBindingAnchorLocked(first, firstAnchor)
	store.installRootBindingAnchorLocked(second, secondAnchor)
	store.mu.Unlock()
	lease, err := store.leaseLibraryRoot(first)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	opened, err := store.openLibraryRoot(first)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if err := store.DeleteLibrary(ctx, firstLibrary.ID); err != nil {
		t.Fatal(err)
	}
	store.rootClosures.Wait()
	if _, err := firstAnchor.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("deleted library retained its Store anchor: %v", err)
	}
	store.mu.Lock()
	_, retainedFirst := store.rootBindingAnchors[first.id]
	retainedSecond, foundSecond := store.rootBindingAnchors[second.id]
	store.mu.Unlock()
	if retainedFirst || !foundSecond || retainedSecond.approved != secondAnchor {
		t.Fatal("library deletion retired another library's approval or retained its own")
	}
	rootBindingPathsAssertContents(t, opened, "first-library")
	reopened, err := lease.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	rootBindingPathsAssertContents(t, reopened, "first-library")
	rootBindingPathsAssertOpen(t, store, second, "second-library")
	// A rejected deletion must not retire an anchor while its library still
	// exists. The fixture owns this synthetic active row and its isolated schema.
	jobID := "binding-paths-active-job"
	if _, err := pool.Exec(ctx, `INSERT INTO scan_jobs (id, library_id, status) VALUES ($1, $2, 'Running')`, jobID, secondLibrary.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteLibrary(ctx, secondLibrary.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("active library deletion did not fail: %v", err)
	}
	rootBindingPathsAssertOpen(t, store, second, "second-library")
	if _, err := pool.Exec(ctx, `DELETE FROM scan_jobs WHERE id = $1`, jobID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteLibrary(ctx, secondLibrary.ID); err != nil {
		t.Fatal(err)
	}
	store.rootClosures.Wait()
	if _, err := secondAnchor.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("final deleted library retained its Store anchor: %v", err)
	}
}

func TestRootBindingPathsCancelledMediaWorkerDoesNotBlockStateWrites(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	path := libraryIntegrationFile(t, allowedRoot, "isolated/marker.txt", "retained media")
	collection := libraryIntegrationCreate(t, ctx, store, "Isolated media", "movies", filepath.Dir(path))
	var root libraryRoot
	if err := pool.QueryRow(ctx, `SELECT id, library_id, path, allowed_path, relative_path
		FROM library_roots WHERE library_id = $1`, collection.ID).
		Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath); err != nil {
		t.Fatal(err)
	}
	application := seedCatalogApplicationKey(t, ctx, pool, "root-isolation-application", true)
	caller, cancel := context.WithCancel(ctx)
	defer cancel()
	slots := make(chan struct{}, 1)
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		if !imageStoreWaitWorkerCleanup(slots) {
			t.Error("the cancelled media worker did not release its storage resources")
		}
	})
	finished := make(chan error, 1)
	go func() {
		file, _, err := runMediaSourceWorker(caller, slots, func() (*os.File, MediaFile, error) {
			opened, err := store.withLibraryRootAnchor(root, func(approved *os.Root) (*os.Root, error) {
				close(started)
				<-release
				return openRegisteredRoot(approved, root.relativePath)
			})
			if err != nil {
				return nil, MediaFile{}, err
			}
			defer opened.Close()
			file, err := opened.Open("marker.txt")
			return file, MediaFile{}, err
		})
		if file != nil {
			_ = file.Close()
		}
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("media work did not reach the blocked directory operation")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("the media caller did not observe cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the media caller waited for blocked storage after cancellation")
	}
	if len(slots) != 1 {
		t.Fatal("cancellation released the still-blocked filesystem worker")
	}
	for _, subject := range []Subject{{UserID: userID}, application} {
		stateCtx, cancelState := context.WithTimeout(ctx, time.Second)
		stateDone := make(chan error, 1)
		go func() {
			tx, _, err := store.beginSubjectStateWrite(stateCtx, subject, true)
			if err == nil {
				err = tx.Rollback(stateCtx)
			}
			stateDone <- err
		}()
		select {
		case err := <-stateDone:
			if err != nil {
				t.Errorf("independent user or application state admission failed: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("independent state admission waited for the cancelled media worker")
		}
		cancelState()
	}
	releaseOnce.Do(func() { close(release) })
}

package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
	if _, err := secondAnchor.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("final deleted library retained its Store anchor: %v", err)
	}
}

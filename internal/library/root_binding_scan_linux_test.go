//go:build linux

package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRootBindingScanFilesystemClonesObservedRegisteredRootAcrossTransientReplacement(t *testing.T) {
	parent := rootStorageTestDirectory(t)
	approved, registered := filepath.Join(parent, "approved"), filepath.Join(parent, "approved", "registered")
	libraryIntegrationFile(t, registered, "marker.txt", "observed-original")
	store := &Store{roots: []approvedRoot{{path: approved}}}
	root := libraryRoot{id: "scan-clone-root", libraryID: "scan-clone-library", path: registered, allowedPath: approved, relativePath: "registered"}
	value := rootBindingWriteLiveCapture(t, store, root)
	capture := value.(*rootBindingNamedCapture)
	retained := filepath.Join(approved, "retained-original")
	if err := os.Rename(registered, retained); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationFile(t, registered, "marker.txt", "transient-replacement")
	opened, err := capture.CloneRegisteredRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	rootBindingPathsAssertContents(t, opened, "observed-original")
	if err := os.Rename(registered, filepath.Join(approved, "retained-replacement")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, registered); err != nil {
		t.Fatal(err)
	}
	if err := capture.Revalidate(context.Background()); err != nil {
		t.Fatalf("restored original did not revalidate: %v", err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	rootBindingPathsAssertContents(t, opened, "observed-original")
}

func TestRootBindingScanIntegrationNamedOriginalRecoveryAfterRestartIsRootScoped(t *testing.T) {
	ctx, pool, initial, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	parent := rootStorageTestDirectory(t)
	approved := filepath.Join(parent, "approved")
	firstPath, secondPath := filepath.Join(approved, "first"), filepath.Join(approved, "second")
	libraryIntegrationFile(t, firstPath, "marker.txt", "original-first")
	libraryIntegrationFile(t, secondPath, "marker.txt", "original-second")
	initial.mu.Lock()
	initial.roots = append(initial.roots, approvedRoot{path: approved})
	initial.mu.Unlock()
	firstLibrary := libraryIntegrationCreate(t, ctx, initial, "Scan recovery first", "movies", firstPath)
	secondLibrary := libraryIntegrationCreate(t, ctx, initial, "Scan recovery second", "movies", secondPath)
	readRoot := func(library Library) libraryRoot {
		t.Helper()
		var root libraryRoot
		if err := pool.QueryRow(ctx, `SELECT id, library_id, path, allowed_path, relative_path FROM library_roots WHERE library_id = $1`, library.ID).
			Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath); err != nil {
			t.Fatal(err)
		}
		return root
	}
	first, second := readRoot(firstLibrary), readRoot(secondLibrary)
	if err := initial.Close(ctx); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(parent, "retained-original")
	if err := os.Rename(approved, original); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationFile(t, firstPath, "marker.txt", "replacement-first")
	libraryIntegrationFile(t, secondPath, "marker.txt", "replacement-second")
	store, err := New(pool, &libraryFixtureProber{}, []string{approved})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(ctx); err != nil {
			t.Errorf("close restarted recovery Store: %v", err)
		}
	})
	// Startup with the replacement present caches that directory. Persisted
	// approval still identifies the absent original, including after restart.
	rootBindingPathsAssertOpen(t, store, first, "replacement-first")
	rootBindingPathsAssertOpen(t, store, second, "replacement-second")
	shared := store.roots[0].root
	oldLease, err := store.leaseLibraryRoot(first)
	if err != nil {
		t.Fatal(err)
	}
	defer oldLease.Close()
	oldOpened, err := oldLease.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer oldOpened.Close()
	task := rootBindingScanOwnedTask(t, ctx, pool, store, firstLibrary)
	before := catalogAuditSnapshot(t, ctx, pool)
	for attempt := 0; attempt < 2; attempt++ {
		result, err := store.prepareRootBindingScan(task, first)
		if result != nil {
			defer result.Close()
		}
		if err != nil || result == nil || result.status != RootBindingMismatch || result.opened != nil {
			t.Fatalf("replacement learned an approval: result = %+v, error = %v", result, err)
		}
		_ = result.Close()
		if rootBindingScanAnchor(t, store, first.id) != nil {
			t.Fatal("replacement scan installed an anchor")
		}
	}
	if err := os.Rename(approved, filepath.Join(parent, "retained-replacement")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, approved); err != nil {
		t.Fatal(err)
	}
	result, err := store.prepareRootBindingScan(task, first)
	if result != nil {
		defer result.Close()
	}
	if err != nil || result == nil || result.status != RootBindingVerified || result.row.revision != 1 || result.opened == nil {
		t.Fatalf("original approval did not recover: result = %+v, error = %v", result, err)
	}
	rootBindingPathsAssertContents(t, result.opened, "original-first")
	rootBindingPathsAssertOpen(t, store, first, "original-first")
	rootBindingPathsAssertOpen(t, store, second, "replacement-second")
	rootBindingPathsAssertContents(t, oldOpened, "replacement-first")
	leaseOpened, err := oldLease.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer leaseOpened.Close()
	rootBindingPathsAssertContents(t, leaseOpened, "replacement-first")
	if store.roots[0].root != shared || rootBindingScanAnchor(t, store, second.id) != nil {
		t.Fatal("recovering one root changed the shared anchor or another root")
	}
	if err := result.Revalidate(task.ctx); err != nil {
		t.Fatalf("retained recovery evidence failed validation: %v", err)
	}
	if after := catalogAuditSnapshot(t, ctx, pool); after != before {
		t.Fatal("replacement or original recovery changed approval, audit, task or media rows")
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
	rootBindingPathsAssertOpen(t, store, first, "original-first")
}

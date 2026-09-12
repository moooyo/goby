package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type rootBindingPathsFixture struct {
	store    *Store
	parent   string
	approved string
	first    libraryRoot
	second   libraryRoot
}

func newRootBindingPathsFixture(t *testing.T) rootBindingPathsFixture {
	t.Helper()
	parent := t.TempDir()
	approved := filepath.Join(parent, "approved")
	fixture := rootBindingPathsFixture{store: &Store{roots: []approvedRoot{{path: approved}}}, parent: parent, approved: approved,
		first:  libraryRoot{id: "first-root", libraryID: "first-library", path: filepath.Join(approved, "first"), allowedPath: approved, relativePath: "first"},
		second: libraryRoot{id: "second-root", libraryID: "second-library", path: filepath.Join(approved, "second"), allowedPath: approved, relativePath: "second"}}
	fixture.writeDirectories(t, "original-first", "original-second")
	opened, err := fixture.store.openLibraryRoot(fixture.first)
	if err != nil {
		t.Fatal(err)
	}
	_ = opened.Close()
	shared := fixture.store.roots[0].root
	t.Cleanup(func() {
		fixture.store.mu.Lock()
		defer fixture.store.mu.Unlock()
		fixture.store.retireRootBindingAnchorsLocked("")
		_ = shared.Close()
	})
	return fixture
}

func (fixture rootBindingPathsFixture) writeDirectories(t *testing.T, first, second string) {
	t.Helper()
	for name, content := range map[string]string{"first": first, "second": second} {
		directory := filepath.Join(fixture.approved, name)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "marker.txt"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func (fixture rootBindingPathsFixture) replaceDirectories(t *testing.T, saved, first, second string) {
	t.Helper()
	if err := os.Rename(fixture.approved, filepath.Join(fixture.parent, saved)); err != nil {
		t.Fatal(err)
	}
	fixture.writeDirectories(t, first, second)
}

func rootBindingPathsCandidate(t *testing.T, path string) *os.Root {
	t.Helper()
	approved, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = approved.Close() })
	return approved
}

func rootBindingPathsContents(root *os.Root) (string, error) {
	file, err := root.Open("marker.txt")
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	return string(data), err
}

func rootBindingPathsAssertContents(t *testing.T, root *os.Root, expected string) {
	t.Helper()
	actual, err := rootBindingPathsContents(root)
	if err != nil || actual != expected {
		t.Fatalf("read held root: got %q, %v; want %q", actual, err, expected)
	}
}

func rootBindingPathsAssertOpen(t *testing.T, store *Store, root libraryRoot, expected string) {
	t.Helper()
	opened, err := store.openLibraryRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	rootBindingPathsAssertContents(t, opened, expected)
}

func TestRootBindingPathsReplacementIsScopedAndRetainsIndependentHandles(t *testing.T) {
	fixture := newRootBindingPathsFixture(t)
	store := fixture.store
	shared := store.roots[0].root
	originalLease, err := store.leaseLibraryRoot(fixture.first)
	if err != nil {
		t.Fatal(err)
	}
	defer originalLease.Close()
	originalOpened, err := store.openLibraryRoot(fixture.first)
	if err != nil {
		t.Fatal(err)
	}
	defer originalOpened.Close()
	fixture.replaceDirectories(t, "original", "accepted-first", "unapproved-second")
	firstAnchor := rootBindingPathsCandidate(t, fixture.approved)
	store.mu.Lock()
	store.installRootBindingAnchorLocked(fixture.first, firstAnchor)
	store.mu.Unlock()
	rootBindingPathsAssertOpen(t, store, fixture.first, "accepted-first")
	rootBindingPathsAssertOpen(t, store, fixture.second, "original-second")
	if store.roots[0].root != shared {
		t.Fatal("root approval replaced the shared configured anchor")
	}
	rootBindingPathsAssertContents(t, originalOpened, "original-first")
	originalReopened, err := originalLease.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer originalReopened.Close()
	rootBindingPathsAssertContents(t, originalReopened, "original-first")
	acceptedLease, err := store.leaseLibraryRoot(fixture.first)
	if err != nil {
		t.Fatal(err)
	}
	defer acceptedLease.Close()
	acceptedOpened, err := store.openLibraryRoot(fixture.first)
	if err != nil {
		t.Fatal(err)
	}
	defer acceptedOpened.Close()
	fixture.replaceDirectories(t, "previous-approval", "next-first", "still-unapproved-second")
	nextAnchor := rootBindingPathsCandidate(t, fixture.approved)
	store.mu.Lock()
	store.installRootBindingAnchorLocked(fixture.first, nextAnchor)
	store.mu.Unlock()
	if _, err := firstAnchor.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("retired Store anchor remained open: %v", err)
	}
	rootBindingPathsAssertContents(t, acceptedOpened, "accepted-first")
	acceptedReopened, err := acceptedLease.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer acceptedReopened.Close()
	rootBindingPathsAssertContents(t, acceptedReopened, "accepted-first")
	rootBindingPathsAssertOpen(t, store, fixture.first, "next-first")
	rootBindingPathsAssertOpen(t, store, fixture.second, "original-second")
}

func TestRootBindingPathsInstallationRetainsThePreparedAnchor(t *testing.T) {
	fixture := newRootBindingPathsFixture(t)
	fixture.replaceDirectories(t, "original", "accepted-first", "unapproved-second")
	anchor := rootBindingPathsCandidate(t, fixture.approved)
	fixture.replaceDirectories(t, "prepared-approval", "later-first", "later-second")
	fixture.store.mu.Lock()
	fixture.store.installRootBindingAnchorLocked(fixture.first, anchor)
	fixture.store.mu.Unlock()
	rootBindingPathsAssertOpen(t, fixture.store, fixture.first, "accepted-first")
	rootBindingPathsAssertOpen(t, fixture.store, fixture.second, "original-second")
}

func TestRootBindingPathsRejectMappingAndConfigurationChanges(t *testing.T) {
	fixture := newRootBindingPathsFixture(t)
	store := fixture.store
	anchor := rootBindingPathsCandidate(t, fixture.approved)
	secondAnchor := rootBindingPathsCandidate(t, fixture.approved)
	store.mu.Lock()
	store.installRootBindingAnchorLocked(fixture.first, anchor)
	store.installRootBindingAnchorLocked(fixture.second, secondAnchor)
	store.mu.Unlock()
	for _, test := range []struct {
		name   string
		change func(*libraryRoot)
	}{
		{"root identity", func(root *libraryRoot) { root.id = fixture.second.id }},
		{"library", func(root *libraryRoot) { root.libraryID = fixture.second.libraryID }},
		{"registered path", func(root *libraryRoot) { root.path = fixture.second.path }},
		{"allowed path", func(root *libraryRoot) { root.allowedPath = fixture.parent }},
		{"relative path", func(root *libraryRoot) { root.relativePath = fixture.second.relativePath }},
		{"missing library", func(root *libraryRoot) { root.libraryID = "" }},
		{"missing registered path", func(root *libraryRoot) { root.path = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture.first
			test.change(&root)
			if opened, err := store.openLibraryRoot(root); !errors.Is(err, ErrUnavailable) {
				if opened != nil {
					_ = opened.Close()
				}
				t.Fatalf("changed root mapping borrowed an approval: %v", err)
			}
			if lease, err := store.leaseLibraryRoot(root); !errors.Is(err, ErrUnavailable) {
				if lease != nil {
					_ = lease.Close()
				}
				t.Fatalf("changed root mapping borrowed a publication lease: %v", err)
			}
		})
	}
	configured := store.roots
	store.roots = nil
	if opened, err := store.openLibraryRoot(fixture.first); !errors.Is(err, ErrUnavailable) {
		if opened != nil {
			_ = opened.Close()
		}
		t.Fatalf("unconfigured root retained pathname authority: %v", err)
	}
	if lease, err := store.leaseLibraryRoot(fixture.first); !errors.Is(err, ErrUnavailable) {
		if lease != nil {
			_ = lease.Close()
		}
		t.Fatalf("unconfigured root retained lease authority: %v", err)
	}
	store.roots = configured
	store.closed = true
	if opened, err := store.openLibraryRoot(fixture.first); !errors.Is(err, ErrUnavailable) {
		if opened != nil {
			_ = opened.Close()
		}
		t.Fatalf("closed Store retained new-open authority: %v", err)
	}
	store.closed = false
	rootBindingPathsAssertOpen(t, store, fixture.first, "original-first")
}

func TestRootBindingPathsInstallationKeepsNewOpensBehindAdmission(t *testing.T) {
	fixture := newRootBindingPathsFixture(t)
	fixture.replaceDirectories(t, "original", "accepted-first", "unapproved-second")
	anchor := rootBindingPathsCandidate(t, fixture.approved)
	type openResult struct {
		content string
		err     error
	}
	started := make(chan struct{})
	finished := make(chan openResult, 1)
	func() {
		fixture.store.mu.Lock()
		defer fixture.store.mu.Unlock()
		go func() {
			close(started)
			opened, err := fixture.store.openLibraryRoot(fixture.first)
			if err != nil {
				finished <- openResult{err: err}
				return
			}
			defer opened.Close()
			content, err := rootBindingPathsContents(opened)
			finished <- openResult{content: content, err: err}
		}()
		<-started
		select {
		case result := <-finished:
			t.Fatalf("new open passed admission before anchor installation: %+v", result)
		case <-time.After(25 * time.Millisecond):
		}
		fixture.store.installRootBindingAnchorLocked(fixture.first, anchor)
	}()
	select {
	case result := <-finished:
		if result.err != nil || result.content != "accepted-first" {
			t.Fatalf("admitted open used a stale anchor: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("new open remained blocked after installation")
	}
}

func TestRootBindingPathsStoreCloseRetiresAnchorsAndPreservesLeases(t *testing.T) {
	fixture := newRootBindingPathsFixture(t)
	store := fixture.store
	store.ctx, store.cancel = context.WithCancel(context.Background())
	store.queue = make(chan *scanTask)
	store.done = make(chan struct{})
	anchor := rootBindingPathsCandidate(t, fixture.approved)
	store.mu.Lock()
	store.installRootBindingAnchorLocked(fixture.first, anchor)
	store.mu.Unlock()
	lease, err := store.leaseLibraryRoot(fixture.first)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	opened, err := store.openLibraryRoot(fixture.first)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if len(store.rootBindingAnchors) != 0 {
		t.Fatal("closed Store retained binding anchors")
	}
	if _, err := anchor.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed Store did not retire its binding anchor: %v", err)
	}
	rootBindingPathsAssertContents(t, opened, "original-first")
	reopened, err := lease.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	rootBindingPathsAssertContents(t, reopened, "original-first")
}

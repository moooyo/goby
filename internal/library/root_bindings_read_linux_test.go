//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func rootBindingReadFilesystemSnapshot(t *testing.T, store *Store, root libraryRoot) RootTopologySnapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	snapshot, err := store.observeRootBinding(ctx, root)
	if errors.Is(err, ErrRootStorageIdentityUnsupported) && os.Getenv("GOTMPDIR") == "" {
		t.Skip("the temporary filesystem lacks the strong profile; this is not binding acceptance")
	}
	if err != nil {
		t.Fatalf("observe named root binding: %v", err)
	}
	return snapshot
}

func TestRootBindingReadFilesystemUsesNewNamedAnchorWithoutChangingCache(t *testing.T) {
	parent := rootStorageTestDirectory(t)
	approved := filepath.Join(parent, "approved")
	registered := filepath.Join(approved, "registered")
	if err := os.MkdirAll(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	store := &Store{roots: []approvedRoot{{path: approved}}}
	root := libraryRoot{id: "registered-root", libraryID: "library", path: registered, allowedPath: approved, relativePath: "registered"}
	before := rootBindingReadFilesystemSnapshot(t, store, root)
	if store.roots[0].root != nil {
		t.Fatal("binding observation populated the existing scan root cache")
	}
	if err := openApprovedRoot(&store.roots[0]); err != nil {
		t.Fatal(err)
	}
	cached := store.roots[0].root
	t.Cleanup(func() { _ = cached.Close() })
	if err := os.Rename(approved, filepath.Join(parent, "retained-original")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	after := rootBindingReadFilesystemSnapshot(t, store, root)
	if before.Anchor.Equal(after.Anchor) || before.RegisteredRoot.Equal(after.RegisteredRoot) || before.Mapping != after.Mapping {
		t.Fatal("named replacement was confused with the cached original directory")
	}
	if store.roots[0].root != cached {
		t.Fatal("read replaced the existing scan root cache")
	}
	held, err := cached.Open("registered")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	stillOriginal, err := ObserveRootStorageIdentity(held)
	if err != nil || !before.RegisteredRoot.Equal(stillOriginal.Identity) {
		t.Fatalf("binding observation closed or changed the cached original: %v", err)
	}
}

func TestRootBindingReadFilesystemUsesNewNamedRegisteredRoot(t *testing.T) {
	parent := rootStorageTestDirectory(t)
	registered := filepath.Join(parent, "registered")
	if err := os.Mkdir(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	store := &Store{roots: []approvedRoot{{path: parent}}}
	root := libraryRoot{id: "registered-root", libraryID: "library", path: registered, allowedPath: parent, relativePath: "registered"}
	before := rootBindingReadFilesystemSnapshot(t, store, root)
	if err := os.Rename(registered, filepath.Join(parent, "retained-original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	after := rootBindingReadFilesystemSnapshot(t, store, root)
	if !before.Anchor.Equal(after.Anchor) || before.RegisteredRoot.Equal(after.RegisteredRoot) {
		t.Fatal("current registered pathname did not produce its independent identity")
	}
}

func TestRootBindingReadFilesystemRejectsMissingSymlinkUnconfiguredAndClosedRoots(t *testing.T) {
	parent := t.TempDir()
	registered := filepath.Join(parent, "registered")
	if err := os.Mkdir(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	root := libraryRoot{id: "root", libraryID: "library", path: registered, allowedPath: parent, relativePath: "registered"}
	for _, test := range []struct {
		name  string
		store *Store
	}{
		{"unconfigured", &Store{}},
		{"closed", &Store{roots: []approvedRoot{{path: parent}}, closed: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.store.observeRootBinding(context.Background(), root); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("unavailable configuration was observed: %v", err)
			}
		})
	}
	store := &Store{roots: []approvedRoot{{path: parent}}}
	if err := os.Rename(registered, filepath.Join(parent, "sibling")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.observeRootBinding(context.Background(), root); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing registered directory was observed: %v", err)
	}
	if err := os.Symlink(filepath.Join(parent, "sibling"), registered); err != nil {
		t.Fatal(err)
	}
	if _, err := store.observeRootBinding(context.Background(), root); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("sibling symlink was observed as the registered directory: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.observeRootBinding(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled observation reached storage: %v", err)
	}
	if store.roots[0].root != nil {
		t.Fatal("failed observation populated the existing scan root cache")
	}
}

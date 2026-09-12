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

type rootTopologyFilesystemFixture struct {
	parent  string
	mapping RootTopologyMapping
	lease   *libraryRootLease
	root    *os.Root
}

func newRootTopologyFilesystemFixture(t *testing.T) rootTopologyFilesystemFixture {
	t.Helper()
	parent := rootStorageTestDirectory(t)
	approved, registered := filepath.Join(parent, "approved"), filepath.Join(parent, "approved", "registered")
	if err := os.MkdirAll(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	anchor, err := os.OpenRoot(approved)
	if err != nil {
		t.Fatal(err)
	}
	lease := &libraryRootLease{approved: anchor, relativePath: "registered"}
	t.Cleanup(func() { _ = lease.Close() })
	root, err := lease.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return rootTopologyFilesystemFixture{parent, RootTopologyMapping{ApprovedPath: approved, RegisteredPath: registered}, lease, root}
}

func rootTopologyFilesystemCapture(t *testing.T, fixture rootTopologyFilesystemFixture) *RootTopologyCapture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	capture, err := fixture.lease.CaptureTopology(ctx, fixture.mapping, fixture.root)
	if errors.Is(err, ErrRootStorageIdentityUnsupported) && os.Getenv("GOTMPDIR") == "" {
		t.Skip("the local temporary filesystem lacks the strong profile; this is not topology acceptance")
	}
	if err != nil {
		t.Fatalf("capture the owned filesystem topology: %v", err)
	}
	t.Cleanup(func() { _ = capture.Close() })
	return capture
}

func TestRootTopologyFilesystemCaptureRetainsIndependentWitnesses(t *testing.T) {
	fixture := newRootTopologyFilesystemFixture(t)
	capture := rootTopologyFilesystemCapture(t, fixture)
	snapshot, err := capture.Snapshot()
	if err != nil || snapshot.Anchor.Validate() != nil || snapshot.RegisteredRoot.Validate() != nil || len(snapshot.Boundaries) != 0 {
		t.Fatalf("a new owned directory did not have a complete simple topology (%T)", err)
	}
	live, err := capture.LiveWitness()
	if err != nil || live.Namespace.Inode == 0 || live.Anchor.MountID <= 0 || live.RegisteredRoot.Inode == 0 {
		t.Fatal("the independent live witnesses were not retained")
	}
	before, err := capture.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.mapping.RegisteredPath, "content"), []byte("ordinary directory change"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.root.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := capture.Revalidate(ctx); err != nil {
		t.Fatalf("capture depended on the caller's closed lease or root: %v", err)
	}
	if after, err := capture.Fingerprint(); err != nil || after != before {
		t.Fatal("ordinary content changes rewrote stable topology identity")
	}
	held := append([]rootTopologyHeldPoint(nil), capture.held...)
	namespace := capture.namespace
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	for _, point := range held {
		if _, err := point.file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("topology Close retained an owned directory descriptor")
		}
	}
	if _, err := namespace.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("topology Close retained its namespace descriptor")
	}
}

func TestRootTopologyFilesystemRejectsRegisteredReplacementWithoutRebinding(t *testing.T) {
	fixture := newRootTopologyFilesystemFixture(t)
	capture := rootTopologyFilesystemCapture(t, fixture)
	before, _ := capture.Fingerprint()
	rootPath, saved := fixture.mapping.RegisteredPath, filepath.Join(fixture.mapping.ApprovedPath, "retained-original")
	if err := os.Rename(rootPath, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		if err := capture.Revalidate(ctx); !errors.Is(err, ErrRootTopologyChanged) {
			t.Fatalf("a new empty directory reused the original held topology: %v", err)
		}
		if current, err := fixture.lease.CaptureTopology(ctx, fixture.mapping, fixture.root); !errors.Is(err, ErrRootTopologyChanged) || current != nil {
			if current != nil {
				_ = current.Close()
			}
			t.Fatal("a fresh capture paired the replacement name with the old scan directory")
		}
	}
	newRoot, err := fixture.lease.Open()
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := fixture.lease.CaptureTopology(ctx, fixture.mapping, newRoot)
	_ = newRoot.Close()
	if err != nil {
		t.Fatalf("observe the replacement without authorizing it: %v", err)
	}
	defer replacement.Close()
	if after, err := replacement.Fingerprint(); err != nil || after == before {
		t.Fatal("observing a replacement did not distinguish it from the old binding candidate")
	}
	if retained, err := capture.Fingerprint(); err != nil || retained != before {
		t.Fatal("failed revalidation silently replaced the previous snapshot")
	}
	if err := os.Remove(rootPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, rootPath); err != nil {
		t.Fatal(err)
	}
	if err := capture.Revalidate(ctx); err != nil {
		t.Fatalf("the original topology did not recover after restoring the original directory: %v", err)
	}
	if err := replacement.Revalidate(ctx); !errors.Is(err, ErrRootTopologyChanged) {
		t.Fatal("the replacement's held descriptor was confused with the restored original")
	}
}

func TestRootTopologyFilesystemReopensApprovedAnchorInsteadOfTrustingLease(t *testing.T) {
	fixture := newRootTopologyFilesystemFixture(t)
	capture := rootTopologyFilesystemCapture(t, fixture)
	saved := filepath.Join(fixture.parent, "retained-approved")
	if err := os.Rename(fixture.mapping.ApprovedPath, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fixture.mapping.RegisteredPath, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := capture.Revalidate(ctx); !errors.Is(err, ErrRootTopologyChanged) {
		t.Fatalf("the cached anchor authorized its same-name replacement: %v", err)
	}
	if current, err := fixture.lease.CaptureTopology(ctx, fixture.mapping, fixture.root); !errors.Is(err, ErrRootTopologyChanged) || current != nil {
		if current != nil {
			_ = current.Close()
		}
		t.Fatal("a fresh capture trusted the lease's old unnamed anchor")
	}
	if err := os.Remove(fixture.mapping.RegisteredPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.mapping.ApprovedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, fixture.mapping.ApprovedPath); err != nil {
		t.Fatal(err)
	}
	if err := capture.Revalidate(ctx); err != nil {
		t.Fatalf("restoring the original approved anchor did not restore revalidation: %v", err)
	}
}

func TestRootTopologyFilesystemRejectsSymlinkAndChecksMappingAndCancellation(t *testing.T) {
	fixture := newRootTopologyFilesystemFixture(t)
	capture := rootTopologyFilesystemCapture(t, fixture)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := capture.Revalidate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled revalidation continued")
	}
	if value, err := fixture.lease.CaptureTopology(ctx, fixture.mapping, fixture.root); !errors.Is(err, context.Canceled) || value != nil {
		t.Fatal("cancelled capture returned witnesses")
	}
	wrong := fixture.mapping
	wrong.RegisteredPath = filepath.Join(fixture.mapping.ApprovedPath, "different")
	if value, err := fixture.lease.CaptureTopology(context.Background(), wrong, fixture.root); !errors.Is(err, ErrInvalidRootTopology) || value != nil {
		t.Fatal("mapping was not bound to the lease's exact relative path")
	}
	saved, sibling := filepath.Join(fixture.mapping.ApprovedPath, "saved"), filepath.Join(fixture.mapping.ApprovedPath, "sibling")
	if err := os.Mkdir(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(fixture.mapping.RegisteredPath, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sibling, fixture.mapping.RegisteredPath); err != nil {
		t.Fatal(err)
	}
	if err := capture.Revalidate(context.Background()); !errors.Is(err, ErrRootTopologyUnavailable) {
		t.Fatalf("a symlink became a registered root witness: %v", err)
	}
	if err := os.Remove(fixture.mapping.RegisteredPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, fixture.mapping.RegisteredPath); err != nil {
		t.Fatal(err)
	}
	if err := capture.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRootTopologyLiveComparisonDoesNotConfuseStableAndCurrentIdentity(t *testing.T) {
	base := RootStorageObservation{Identity: rootStorageIdentityFixture(), Live: RootStorageWitness{Device: 1, Inode: 2, MountID: 3}}
	for _, live := range []RootStorageWitness{{Device: 4, Inode: 2, MountID: 3}, {Device: 1, Inode: 4, MountID: 3}, {Device: 1, Inode: 2, MountID: 4}} {
		other := base
		other.Live = live
		if !base.Identity.Equal(other.Identity) || !errors.Is(compareRootTopologyPoint(base, other), ErrRootTopologyChanged) {
			t.Fatal("stable equality bypassed the current capture's exact live witness")
		}
	}
	if err := compareRootTopologyPoint(RootStorageObservation{}, RootStorageObservation{}); !errors.Is(err, ErrRootTopologyChanged) {
		t.Fatal("absent storage identity matched an absent witness")
	}
}

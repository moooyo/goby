//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLocalBackdropLockUsesPhysicalBindMountCoordinates(t *testing.T) {
	mounts := []rootMountInfo{
		{ID: 1, Major: 8, Minor: 1, Root: "/", MountPoint: "/"},
		{ID: 2, Major: 8, Minor: 1, Root: "/srv/media", MountPoint: "/library"},
		{ID: 3, Major: 8, Minor: 1, Root: "/srv/media/Show", MountPoint: "/alias"},
		{ID: 4, Major: 8, Minor: 1, Root: "/srv/media/Show-other", MountPoint: "/similar"},
		{ID: 5, Major: 8, Minor: 2, Root: "/srv/media/Show", MountPoint: "/different-device"},
	}
	aliases, err := backdropLockMountAliases(mounts, "/library/Show", 2, 8, 1)
	if err != nil || !slices.Equal(aliases, []string{"/alias", "/srv/media/Show"}) {
		t.Fatalf("bind aliases = %v, %v", aliases, err)
	}
	if _, err := backdropLockMountAliases(mounts, "/library/Show", 2, 8, 2); !errors.Is(err, ErrRootTopologyUnavailable) {
		t.Fatalf("mount ID/device disagreement was accepted: %v", err)
	}
	for i := 0; i < maxLocalBackdropLockAliases; i++ {
		mounts = append(mounts, rootMountInfo{ID: 10 + i, Major: 8, Minor: 1, Root: "/srv/media/Show", MountPoint: fmt.Sprintf("/alias%d", i)})
	}
	if _, err := backdropLockMountAliases(mounts, "/library/Show", 2, 8, 1); !errors.Is(err, ErrRootTopologyLimit) {
		t.Fatalf("over-budget alias set was accepted: %v", err)
	}
}

// Run only with an operator-created private mount namespace. Mounting directly
// in the host namespace would alter another task's view of its media roots.
func TestLocalBackdropLockPrivateBindMount(t *testing.T) {
	namespace := os.Getenv("GOBY_TEST_BACKDROP_PRIVATE_MOUNT_NS")
	if namespace == "" {
		t.Skip("requires an externally prepared private mount namespace")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	if _, _, err := rootTopologyHelperNamespace(context.Background(), namespace, ""); err != nil {
		t.Fatal(err)
	}
	f := newLocalBackdropFixture(t, true)
	aliasPath := filepath.Join(filepath.Dir(f.dir), "Alias")
	if err := os.Mkdir(aliasPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(f.dir, aliasPath, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(aliasPath, 0); err != nil {
			t.Error(err)
		}
	})
	alias := libraryIntegrationCreate(t, f.ctx, f.store, "Bound source alias", "tvshows", aliasPath)
	lock := acquireBackdropFixtureLock(t, f)
	if _, err := f.store.StartScan(f.ctx, alias.ID); !errors.Is(err, ErrLocalBackdropLocked) {
		t.Fatalf("scan through same-device bind alias = %v", err)
	}
	f.isolateBlocker(t)
	if _, err := f.store.RereadLockedLocalBackdrop(f.ctx, f.actor, f.item.ID, lock.LockID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.ReleaseLocalBackdropLock(f.ctx, f.actor, f.item.ID, lock.LockID); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, f.ctx, f.store, alias.ID, "Completed")
}

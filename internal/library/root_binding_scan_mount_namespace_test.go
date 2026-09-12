//go:build linux

package library

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This test is inert in ordinary runs. The reviewed launcher must set
// GOBY_ROOT_BINDING_SCAN_MOUNT_HELPER=1, provide GOBY_TEST_DATABASE_URL and an
// owned ext4 GOTMPDIR, and launch the binary in a private mount namespace.
// GOBY_ROOT_TOPOLOGY_HOST_MOUNT_NAMESPACE must identify the original host.
// The helper never creates a namespace or changes host mount propagation.
func TestRootBindingScanMountNamespaceHelper(t *testing.T) {
	if os.Getenv("GOBY_ROOT_BINDING_SCAN_MOUNT_HELPER") != "1" {
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if os.Getuid() != 0 || os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("the reviewed isolated mount helper requires root and its owned PostgreSQL fixture")
	}
	host := os.Getenv("GOBY_ROOT_TOPOLOGY_HOST_MOUNT_NAMESPACE")
	if !strings.HasPrefix(host, "mnt:[") || !validRootMountFilesystemRoot(host, "nsfs") {
		t.Fatal("the exact original host mount namespace witness is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	namespace, _, err := rootTopologyHelperNamespace(ctx, host, "")
	if err != nil {
		t.Fatal(err)
	}
	base := os.Getenv("GOTMPDIR")
	canonical, err := filepath.EvalSymlinks(base)
	if err != nil || !filepath.IsAbs(base) || filepath.Clean(base) != base || canonical != base {
		t.Fatal("the reviewed GOTMPDIR must be an existing canonical absolute directory")
	}
	var filesystem unix.Statfs_t
	if err := unix.Statfs(base, &filesystem); err != nil || filesystem.Type != unix.EXT4_SUPER_MAGIC {
		t.Fatal("the mount recovery fixture requires the reviewed ext4 GOTMPDIR")
	}
	directory, err := os.MkdirTemp(base, "goby-root-binding-scan-mount-")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || filepath.Dir(directory) != base {
		t.Fatal("the exclusive mount recovery directory could not be identified")
	}
	helper := &rootTopologyMountHelper{ctx: ctx, host: host, namespace: namespace, directory: directory, identity: info,
		sources: make(map[string]RootStorageIdentity), targets: make(map[string]bool)}
	t.Cleanup(func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := helper.cleanup(); err != nil {
			t.Errorf("owned recovery mount cleanup failed; fixture retained at %s: %v", directory, err)
		} else {
			t.Log("mount_cleanup_completed=true held_descriptors_closed=true owned_mounts_remaining=0 fixture_removed=true")
		}
	})
	one, two, approved := filepath.Join(directory, "source-one"), filepath.Join(directory, "source-two"), filepath.Join(directory, "approved")
	for _, source := range []string{one, two} {
		for _, child := range []string{"first", "second"} {
			libraryIntegrationFile(t, filepath.Join(source, child), "marker.txt", filepath.Base(source)+"-"+child)
		}
		observation, err := rootTopologyHelperDirectory(source)
		if err != nil {
			t.Fatal(err)
		}
		helper.sources[source] = observation.Identity
	}
	if err := os.Mkdir(approved, 0o700); err != nil {
		t.Fatal(err)
	}
	helper.targets[approved] = true
	if err := helper.mount(one, approved); err != nil {
		t.Fatal(err)
	}
	databaseCtx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	store.mu.Lock()
	store.roots = append(store.roots, approvedRoot{path: approved})
	store.mu.Unlock()
	firstLibrary := libraryIntegrationCreate(t, databaseCtx, store, "Mounted recovery first", "movies", filepath.Join(approved, "first"))
	secondLibrary := libraryIntegrationCreate(t, databaseCtx, store, "Mounted recovery second", "movies", filepath.Join(approved, "second"))
	rootFor := func(library Library) libraryRoot {
		t.Helper()
		var root libraryRoot
		if err := pool.QueryRow(databaseCtx, `SELECT id, library_id, path, allowed_path, relative_path FROM library_roots WHERE library_id = $1`, library.ID).
			Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath); err != nil {
			t.Fatal(err)
		}
		return root
	}
	first, second := rootFor(firstLibrary), rootFor(secondLibrary)
	firstAnchor, secondAnchor := rootBindingScanAnchor(t, store, first.id), rootBindingScanAnchor(t, store, second.id)
	oldLease, err := store.leaseLibraryRoot(first)
	if err != nil {
		t.Fatal(err)
	}
	defer oldLease.Close()
	oldRoot, err := oldLease.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer oldRoot.Close()
	observe := func(root *os.Root) RootStorageObservation {
		t.Helper()
		file, err := root.Open(".")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		value, err := ObserveRootStorageIdentity(file)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	originalLive := observe(oldRoot)
	task := rootBindingScanOwnedTask(t, databaseCtx, pool, store, firstLibrary)
	before := catalogAuditSnapshot(t, databaseCtx, pool)
	if err := helper.unmount(approved, unix.MNT_DETACH); err != nil {
		t.Fatal(err)
	}
	unavailable, err := store.prepareRootBindingScan(task, first)
	if unavailable != nil {
		defer unavailable.Close()
	}
	if err != nil || unavailable == nil || unavailable.status != RootBindingUnavailable {
		t.Fatalf("detached storage was not unavailable: result = %+v, error = %v", unavailable, err)
	}
	_ = unavailable.Close()
	if err := helper.mount(two, approved); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		replacement, err := store.prepareRootBindingScan(task, first)
		if replacement != nil {
			defer replacement.Close()
		}
		if err != nil || replacement == nil || replacement.status != RootBindingMismatch {
			t.Fatalf("replacement mount learned an approval: result = %+v, error = %v", replacement, err)
		}
		_ = replacement.Close()
		if rootBindingScanAnchor(t, store, first.id) != firstAnchor {
			t.Fatal("replacement mount changed the original retained anchor")
		}
	}
	if err := helper.unmount(approved, unix.MNT_DETACH); err != nil {
		t.Fatal(err)
	}
	if err := helper.mount(one, approved); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.prepareRootBindingScan(task, first)
	if recovered != nil {
		defer recovered.Close()
	}
	if err != nil || recovered == nil || recovered.status != RootBindingVerified || recovered.opened == nil {
		t.Fatalf("original-source remount did not recover approval: result = %+v, error = %v", recovered, err)
	}
	currentLive := observe(recovered.opened)
	if !originalLive.Identity.Equal(currentLive.Identity) || originalLive.Live.MountID == currentLive.Live.MountID {
		t.Fatal("recovery did not retain the original stable identity on the new live mount")
	}
	if rootBindingScanAnchor(t, store, first.id) == firstAnchor || rootBindingScanAnchor(t, store, second.id) != secondAnchor {
		t.Fatal("remount recovery failed root-specific publication or changed a sibling")
	}
	rootBindingPathsAssertContents(t, oldRoot, "source-one-first")
	rootBindingPathsAssertContents(t, recovered.opened, "source-one-first")
	rootBindingPathsAssertOpen(t, store, first, "source-one-first")
	if err := recovered.Revalidate(task.ctx); err != nil {
		t.Fatalf("recovered live witnesses did not validate: %v", err)
	}
	if after := catalogAuditSnapshot(t, databaseCtx, pool); after != before {
		t.Fatal("mount loss, replacement or original recovery changed persisted approval or catalog rows")
	}
	if _, _, err := rootTopologyHelperNamespace(ctx, host, namespace); err != nil {
		t.Fatal(err)
	}
	t.Log("original_remount_recovered=true replacement_approval_learned=false approval_row_unchanged=true sibling_anchor_unchanged=true old_leases_retained=true fresh_mount_witness=true system_reboot_tested=false")
}

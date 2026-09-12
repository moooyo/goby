//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This helper is intentionally inert in the normal suite. A reviewed operator
// must launch the test binary through unshare in a new private mount namespace:
//
// GOBY_ROOT_TOPOLOGY_MOUNT_HELPER=1
// GOBY_ROOT_TOPOLOGY_HOST_MOUNT_NAMESPACE=mnt:[the-original-host-inode]
// GOTMPDIR=an-existing-owned-ext4-directory
//
// The operator's launcher must share PID 1's mount namespace before unshare.
// No namespace creation or propagation changes are performed by this Go test.
func TestRootTopologyMountNamespaceHelper(t *testing.T) {
	if os.Getenv("GOBY_ROOT_TOPOLOGY_MOUNT_HELPER") != "1" {
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if os.Getuid() != 0 || os.Geteuid() != 0 {
		t.Fatal("the isolated mount helper requires the reviewed root execution scope")
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
		t.Fatal("the real mount helper requires the reviewed ext4 GOTMPDIR, not /tmp or tmpfs")
	}
	directory, err := os.MkdirTemp(base, "goby-root-topology-mount-")
	if err != nil {
		t.Fatal("create the exclusive owned mount fixture")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || filepath.Dir(directory) != base {
		t.Fatal("the exclusive fixture directory could not be identified")
	}
	helper := &rootTopologyMountHelper{ctx: ctx, host: host, namespace: namespace, directory: directory, identity: info,
		sources: make(map[string]RootStorageIdentity), targets: make(map[string]bool)}
	t.Cleanup(func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := helper.cleanup(); err != nil {
			t.Errorf("owned mount cleanup failed; fixture retained at %s: %v", directory, err)
		} else {
			t.Log("mount_cleanup_completed=true held_descriptors_closed=true owned_mounts_remaining=0 fixture_removed=true")
		}
	})
	approved := filepath.Join(directory, "approved")
	registered := filepath.Join(approved, "registered")
	nested, added := filepath.Join(registered, "nested"), filepath.Join(registered, "added")
	one, two := filepath.Join(directory, "source-one"), filepath.Join(directory, "source-two")
	for _, path := range []string{nested, added, one, two} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for index, source := range []string{one, two} {
		if err := os.WriteFile(filepath.Join(source, "owned-content"), []byte(fmt.Sprintf("source-%d", index+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		observation, err := rootTopologyHelperDirectory(source)
		if err != nil {
			t.Fatalf("observe the owned ext4 bind source: %v", err)
		}
		helper.sources[source] = observation.Identity
	}
	if helper.sources[one].Equal(helper.sources[two]) || helper.sources[one].FilesystemUUID != helper.sources[two].FilesystemUUID {
		t.Fatal("the two owned sources must have different directory handles in the same filesystem")
	}
	helper.targets[nested], helper.targets[added] = true, true
	anchor, err := os.OpenRoot(approved)
	if err != nil {
		t.Fatal(err)
	}
	lease := &libraryRootLease{approved: anchor, relativePath: "registered"}
	helper.closers = append(helper.closers, lease.Close)
	root, err := lease.Open()
	if err != nil {
		t.Fatal(err)
	}
	helper.closers = append(helper.closers, root.Close)
	mapping := RootTopologyMapping{ApprovedPath: approved, RegisteredPath: registered}
	capture := func(expected map[string]RootStorageIdentity) *RootTopologyCapture {
		t.Helper()
		value, err := lease.CaptureTopology(ctx, mapping, root)
		if err != nil {
			t.Fatalf("capture real owned bind mounts: %v", err)
		}
		helper.closers = append(helper.closers, value.Close)
		snapshot, err := value.Snapshot()
		if err != nil || len(snapshot.Boundaries) != len(expected) {
			t.Fatal("the actual mount boundary set is incomplete")
		}
		for _, boundary := range snapshot.Boundaries {
			identity, exists := expected[boundary.RelativePath]
			if !exists || !boundary.Identity.Equal(identity) {
				t.Fatal("a mounted boundary has the wrong stable source identity")
			}
		}
		if err := value.Revalidate(ctx); err != nil {
			t.Fatalf("revalidate an unchanged real bind mount: %v", err)
		}
		return value
	}
	fingerprint := func(value *RootTopologyCapture) string {
		t.Helper()
		digest, err := value.Fingerprint()
		if err != nil {
			t.Fatal(err)
		}
		return digest
	}
	assertChanged := func(value *RootTopologyCapture, phase string) {
		t.Helper()
		if err := value.Revalidate(ctx); !errors.Is(err, ErrRootTopologyChanged) {
			t.Fatalf("%s did not invalidate the old held capture: %v", phase, err)
		}
	}
	if err := helper.mount(one, nested); err != nil {
		t.Fatal(err)
	}
	original := capture(map[string]RootStorageIdentity{"nested": helper.sources[one]})
	originalFingerprint := fingerprint(original)
	t.Logf("phase=nested_bind boundaries=1 source_identity_matched=true fingerprint=%s", originalFingerprint)

	// A lazy detach models mount loss while a capture still holds the old mount.
	// It operates only on a tracked bind mount in this private namespace.
	if err := helper.unmount(nested, unix.MNT_DETACH); err != nil {
		t.Fatal(err)
	}
	assertChanged(original, "lost boundary")
	t.Log("phase=boundary_lost old_capture_rejected=true")
	if err := helper.mount(two, nested); err != nil {
		t.Fatal(err)
	}
	assertChanged(original, "replacement boundary")
	replacement := capture(map[string]RootStorageIdentity{"nested": helper.sources[two]})
	if fingerprint(replacement) == originalFingerprint {
		t.Fatal("a replacement source retained the original stable fingerprint")
	}
	t.Log("phase=boundary_replaced old_capture_rejected=true stable_identity_changed=true")

	if err := helper.unmount(nested, unix.MNT_DETACH); err != nil {
		t.Fatal(err)
	}
	if err := helper.mount(one, nested); err != nil {
		t.Fatal(err)
	}
	restored := capture(map[string]RootStorageIdentity{"nested": helper.sources[one]})
	if fingerprint(restored) != originalFingerprint {
		t.Fatal("remounting the original source did not restore the stable fingerprint")
	}
	// Only the new capture is current. Old mount IDs remain live witnesses and
	// are neither required nor permitted to stand in for the stable identity.
	assertChanged(original, "a new mount instance of the original source")
	t.Log("phase=original_source_remounted stable_fingerprint_restored=true new_capture_verified=true")

	if err := helper.mount(two, added); err != nil {
		t.Fatal(err)
	}
	assertChanged(restored, "added boundary")
	withAdded := capture(map[string]RootStorageIdentity{"nested": helper.sources[one], "added": helper.sources[two]})
	if fingerprint(withAdded) == originalFingerprint {
		t.Fatal("the extra boundary was absent from the stable fingerprint")
	}
	t.Log("phase=boundary_added boundaries=2 old_capture_rejected=true")
	if err := helper.unmount(added, unix.MNT_DETACH); err != nil {
		t.Fatal(err)
	}
	afterAdded := capture(map[string]RootStorageIdentity{"nested": helper.sources[one]})
	if fingerprint(afterAdded) != originalFingerprint {
		t.Fatal("removing the added boundary did not recover the original topology")
	}

	if err := helper.mount(two, nested); err != nil {
		t.Fatal(err)
	}
	if err := afterAdded.Revalidate(ctx); !errors.Is(err, ErrRootTopologyAmbiguous) {
		t.Fatalf("an actual same-path stacked mount was not explicitly ambiguous: %v", err)
	}
	if value, err := lease.CaptureTopology(ctx, mapping, root); !errors.Is(err, ErrRootTopologyAmbiguous) || value != nil {
		if value != nil {
			_ = value.Close()
		}
		t.Fatalf("an actual hidden mount produced a complete capture: %v", err)
	}
	t.Log("phase=stacked_mount capture_and_revalidate_ambiguous=true")
	if err := helper.unmount(nested, 0); err != nil {
		t.Fatal(err)
	}
	if err := afterAdded.Revalidate(ctx); err != nil {
		t.Fatalf("unstacking did not restore the still-held original mount: %v", err)
	}
	if _, _, err := rootTopologyHelperNamespace(ctx, host, namespace); err != nil {
		t.Fatal(err)
	}
	t.Log("mount_scenarios_completed=true host_namespace_unchanged=true private_namespace_only=true system_reboot_tested=false")
}

type rootTopologyHelperMount struct {
	source, target string
	identity       RootStorageIdentity
	id             int
	before         map[int]bool
	succeeded      bool
}

type rootTopologyMountHelper struct {
	ctx                        context.Context
	host, namespace, directory string
	identity                   os.FileInfo
	sources                    map[string]RootStorageIdentity
	targets                    map[string]bool
	mounts                     []*rootTopologyHelperMount
	closers                    []func() error
}

func rootTopologyHelperNamespace(ctx context.Context, host, expected string) (string, []rootMountInfo, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	pidOne, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil || pidOne != host {
		return "", nil, errors.New("the original host namespace pin does not match PID 1")
	}
	current, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil || current == host || expected != "" && current != expected {
		return "", nil, errors.New("the helper is not in its independent mount namespace")
	}
	thread, err := os.Readlink("/proc/thread-self/ns/mnt")
	if err != nil || thread != current {
		return "", nil, errors.New("the helper thread is in another mount namespace")
	}
	entries, err := readRootTopologyMountInfo(ctx)
	if err != nil {
		return "", nil, err
	}
	for _, entry := range entries {
		// MS_REC|MS_PRIVATE leaves no propagation tags. Unknown optional
		// semantics are not accepted as proof of private propagation either.
		if len(entry.OptionalFields) != 0 {
			return "", nil, errors.New("mount propagation is not proven private")
		}
	}
	return current, entries, nil
}

func rootTopologyHelperDirectory(path string) (RootStorageObservation, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return RootStorageObservation{}, errors.New("an owned mount path is not a nonsymlink directory")
	}
	file, err := os.Open(path)
	if err != nil {
		return RootStorageObservation{}, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return RootStorageObservation{}, errors.New("the owned mount path changed while opening")
	}
	return ObserveRootStorageIdentity(file)
}

func (helper *rootTopologyMountHelper) records(ctx context.Context) ([]rootMountInfo, error) {
	_, records, err := rootTopologyHelperNamespace(ctx, helper.host, helper.namespace)
	if err != nil {
		return nil, err
	}
	current, err := os.Lstat(helper.directory)
	if err != nil || !current.IsDir() || !os.SameFile(current, helper.identity) {
		return nil, errors.New("the owned fixture directory identity changed")
	}
	return records, nil
}

func (helper *rootTopologyMountHelper) checkOwned(records []rootMountInfo) error {
	seen := make(map[int]bool)
	for _, record := range records {
		if !rootTopologyWithin(helper.directory, record.MountPoint) {
			continue
		}
		found := false
		for _, owned := range helper.mounts {
			if owned.id > 0 && owned.id == record.ID && owned.target == record.MountPoint {
				found, seen[record.ID] = true, true
			}
		}
		if !found {
			return errors.New("the private fixture contains an unowned mount")
		}
	}
	for _, owned := range helper.mounts {
		if owned.id <= 0 || !seen[owned.id] {
			return errors.New("an owned mount disappeared without its recorded unmount")
		}
	}
	return nil
}

func (helper *rootTopologyMountHelper) mount(source, target string) error {
	expected, allowed := helper.sources[source]
	if !allowed || !helper.targets[target] {
		return errors.New("mount target or source is outside the exact owned fixture")
	}
	records, err := helper.records(helper.ctx)
	if err != nil {
		return err
	}
	if err := helper.checkOwned(records); err != nil {
		return err
	}
	observation, err := rootTopologyHelperDirectory(source)
	if err != nil || !expected.Equal(observation.Identity) {
		return errors.New("the owned source identity changed before bind")
	}
	if _, err := rootTopologyHelperDirectory(target); err != nil {
		return err
	}
	before := make(map[int]bool, len(records))
	for _, record := range records {
		before[record.ID] = true
	}
	owned := &rootTopologyHelperMount{source: source, target: target, identity: expected, before: before}
	helper.mounts = append(helper.mounts, owned)
	// Reserve the exact operation before the syscall. Cleanup can reconcile a
	// successful mount even if its first post-syscall observation fails.
	if err := helper.ctx.Err(); err != nil {
		return err
	}
	if err := unix.Mount(source, target, "", uintptr(unix.MS_BIND), ""); err != nil {
		return fmt.Errorf("owned bind mount failed: %w", err)
	}
	owned.succeeded = true
	records, err = helper.records(helper.ctx)
	if err != nil {
		return err
	}
	if err := helper.resolveMount(owned, records); err != nil {
		return err
	}
	return helper.checkOwned(records)
}

func (helper *rootTopologyMountHelper) resolveMount(owned *rootTopologyHelperMount, records []rootMountInfo) error {
	var found []rootMountInfo
	for _, record := range records {
		if record.MountPoint == owned.target && !owned.before[record.ID] {
			found = append(found, record)
		}
	}
	if len(found) != 1 {
		return errors.New("the exact bind operation could not be uniquely reconciled")
	}
	observation, err := rootTopologyHelperDirectory(owned.target)
	if err != nil || observation.Live.MountID != found[0].ID || !owned.identity.Equal(observation.Identity) {
		return errors.New("the new mount does not expose the exact owned source")
	}
	owned.id = found[0].ID
	return nil
}

func (helper *rootTopologyMountHelper) unmount(target string, flags int) error {
	return helper.unmountWithin(helper.ctx, target, flags)
}

func (helper *rootTopologyMountHelper) unmountWithin(ctx context.Context, target string, flags int) error {
	if flags != 0 && flags != unix.MNT_DETACH {
		return errors.New("unexpected unmount mode")
	}
	index := -1
	for candidate, owned := range helper.mounts {
		if owned.target == target {
			index = candidate
		}
	}
	if index < 0 || !helper.targets[target] {
		return errors.New("unmount has no exact owned bind operation")
	}
	records, err := helper.records(ctx)
	if err != nil {
		return err
	}
	if err := helper.checkOwned(records); err != nil {
		return err
	}
	owned := helper.mounts[index]
	observation, err := rootTopologyHelperDirectory(target)
	if err != nil || observation.Live.MountID != owned.id || !owned.identity.Equal(observation.Identity) {
		return errors.New("the top mount changed before its exact owned unmount")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.Unmount(target, flags); err != nil {
		return fmt.Errorf("owned unmount failed: %w", err)
	}
	helper.mounts = append(helper.mounts[:index], helper.mounts[index+1:]...)
	records, err = helper.records(ctx)
	if err != nil {
		return err
	}
	return helper.checkOwned(records)
}

func (helper *rootTopologyMountHelper) cleanup() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var closeErr error
	for index := len(helper.closers) - 1; index >= 0; index-- {
		closeErr = errors.Join(closeErr, helper.closers[index]())
	}
	helper.closers = nil
	records, err := helper.records(ctx)
	if err != nil {
		return errors.Join(closeErr, err)
	}
	for index := len(helper.mounts) - 1; index >= 0; index-- {
		owned := helper.mounts[index]
		if owned.id != 0 {
			continue
		}
		found := false
		for _, record := range records {
			if record.MountPoint == owned.target && !owned.before[record.ID] {
				found = true
			}
		}
		if !owned.succeeded && !found {
			helper.mounts = append(helper.mounts[:index], helper.mounts[index+1:]...)
			continue
		}
		if err := helper.resolveMount(owned, records); err != nil {
			return errors.Join(closeErr, err)
		}
	}
	if err := helper.checkOwned(records); err != nil {
		return errors.Join(closeErr, err)
	}
	for len(helper.mounts) != 0 {
		owned := helper.mounts[len(helper.mounts)-1]
		// Final cleanup closes all held FDs first and uses ordinary unmount.
		// No lazy or force fallback is used to conceal an unexpected busy mount.
		if err := helper.unmountWithin(ctx, owned.target, 0); err != nil {
			return errors.Join(closeErr, err)
		}
	}
	if closeErr != nil {
		return closeErr
	}
	records, err = helper.records(ctx)
	if err != nil {
		return err
	}
	if err := helper.checkOwned(records); err != nil {
		return err
	}
	if err := os.RemoveAll(helper.directory); err != nil {
		return err
	}
	if _, err := os.Lstat(helper.directory); !os.IsNotExist(err) {
		return errors.New("the exact owned mount fixture remains after cleanup")
	}
	return nil
}

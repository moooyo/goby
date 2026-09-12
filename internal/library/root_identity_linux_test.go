//go:build linux

package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestRootStorageLinuxUAPILayoutAndErrorClasses(t *testing.T) {
	if unsafe.Sizeof(rootFSUUID2{}) != 17 || unsafe.Offsetof(rootFSUUID2{}.UUID) != 1 ||
		unsafe.Offsetof(rootDirectoryHandle{}.Handle) != 8 || unsafe.Sizeof(rootDirectoryHandle{}) != 136 {
		t.Fatal("the fixed Linux UAPI buffers have a different layout")
	}
	if rootFSUUIDRequest != uintptr(2<<30|17<<16|0x15<<8) {
		t.Fatal("the supported asm-generic UUID ioctl does not encode the 17-byte read structure")
	}
	for _, errno := range []error{unix.ENOTTY, unix.EOPNOTSUPP, unix.ENOSYS, unix.EOVERFLOW} {
		err := rootStorageOperationError("owned fixture", errno)
		if !errors.Is(err, ErrRootStorageIdentityUnsupported) || !errors.Is(err, errno) || errors.Is(err, ErrRootStorageIdentityUnavailable) {
			t.Errorf("unsupported kernel operation classification = %v", err)
		}
	}
	for _, errno := range []error{unix.EPERM, unix.EACCES, unix.EBADF, unix.ENOENT, unix.ESTALE, unix.ENODEV, unix.EIO, unix.EINVAL} {
		err := rootStorageOperationError("owned fixture", errno)
		if !errors.Is(err, ErrRootStorageIdentityUnavailable) || !errors.Is(err, errno) || errors.Is(err, ErrRootStorageIdentityUnsupported) {
			t.Errorf("unavailable kernel operation classification = %v", err)
		}
	}
}

func TestRootStorageKernelResultIsCompleteBoundedAndCopied(t *testing.T) {
	uuid := rootFSUUID2{Length: 16, UUID: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}}
	handle := rootDirectoryHandle{Bytes: 3, Type: 37, Handle: [MaxRootStorageHandleBytes]byte{9, 8, 7}}
	observation, err := rootStorageObservationFromKernel(uuid, handle, 42)
	if err != nil || observation.Identity.FilesystemUUID != "0102030405060708090a0b0c0d0e0f10" ||
		observation.Identity.HandleType != 37 || len(observation.Identity.Handle) != 3 || observation.Live.MountID != 42 {
		t.Fatalf("the complete kernel profile was not retained (%T)", err)
	}
	handle.Handle[0] = 0
	uuid.UUID[0] = 0
	if observation.Identity.Handle[0] != 9 || observation.Identity.FilesystemUUID[:2] != "01" {
		t.Fatal("the observation aliases a temporary kernel buffer")
	}
	for _, test := range []struct {
		name   string
		change func(*rootFSUUID2, *rootDirectoryHandle, *int32)
		want   error
	}{
		{"short UUID", func(uuid *rootFSUUID2, _ *rootDirectoryHandle, _ *int32) { uuid.Length = 15 }, ErrRootStorageIdentityUnsupported},
		{"eight-byte UUID", func(uuid *rootFSUUID2, _ *rootDirectoryHandle, _ *int32) { uuid.Length = 8 }, ErrRootStorageIdentityUnsupported},
		{"empty UUID length", func(uuid *rootFSUUID2, _ *rootDirectoryHandle, _ *int32) { uuid.Length = 0 }, ErrRootStorageIdentityUnavailable},
		{"long UUID", func(uuid *rootFSUUID2, _ *rootDirectoryHandle, _ *int32) { uuid.Length = 17 }, ErrRootStorageIdentityUnavailable},
		{"zero UUID", func(uuid *rootFSUUID2, _ *rootDirectoryHandle, _ *int32) { uuid.UUID = [16]byte{} }, ErrRootStorageIdentityUnavailable},
		{"empty handle", func(_ *rootFSUUID2, handle *rootDirectoryHandle, _ *int32) { handle.Bytes = 0 }, ErrRootStorageIdentityUnavailable},
		{"long handle", func(_ *rootFSUUID2, handle *rootDirectoryHandle, _ *int32) { handle.Bytes = 129 }, ErrRootStorageIdentityUnsupported},
		{"maximum reported handle", func(_ *rootFSUUID2, handle *rootDirectoryHandle, _ *int32) { handle.Bytes = ^uint32(0) }, ErrRootStorageIdentityUnsupported},
		{"zero mount ID", func(_ *rootFSUUID2, _ *rootDirectoryHandle, mount *int32) { *mount = 0 }, ErrRootStorageIdentityUnavailable},
		{"negative mount ID", func(_ *rootFSUUID2, _ *rootDirectoryHandle, mount *int32) { *mount = -1 }, ErrRootStorageIdentityUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			u, h, mount := uuid, handle, int32(42)
			test.change(&u, &h, &mount)
			value, err := rootStorageObservationFromKernel(u, h, mount)
			if !errors.Is(err, test.want) || value.Identity.Version != 0 || value.Identity.Handle != nil || value.Live != (RootStorageWitness{}) {
				t.Fatalf("an incomplete observation escaped its failure: %T", err)
			}
		})
	}
}

func TestRootStorageIdentityRejectsNonDirectoryAndClosedFiles(t *testing.T) {
	if _, err := ObserveRootStorageIdentity(nil); !errors.Is(err, ErrInvalidRootStorageIdentity) {
		t.Fatalf("nil directory error = %T", err)
	}
	file, err := os.CreateTemp(rootStorageTestDirectory(t), "ordinary-file-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	want := ErrInvalidRootStorageIdentity
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		want = ErrRootStorageIdentityUnsupported
	}
	if _, err := ObserveRootStorageIdentity(file); !errors.Is(err, want) {
		t.Fatalf("regular file error = %T", err)
	}
	if _, err := file.WriteString("the caller still owns this file"); err != nil {
		t.Fatal("the adapter closed a caller-owned descriptor")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if want != ErrRootStorageIdentityUnsupported {
		want = ErrRootStorageIdentityUnavailable
	}
	if _, err := ObserveRootStorageIdentity(file); !errors.Is(err, want) {
		t.Fatalf("closed descriptor error = %T", err)
	}
}

func rootStorageTestObservation(t *testing.T, directory *os.File) RootStorageObservation {
	t.Helper()
	value, err := ObserveRootStorageIdentity(directory)
	if errors.Is(err, ErrRootStorageIdentityUnsupported) {
		if os.Getenv("GOTMPDIR") != "" {
			t.Fatal("the configured GOTMPDIR verification filesystem lacks the required identity profile")
		}
		t.Skip("the owned temporary filesystem or architecture lacks this identity profile; no persistence claim is made")
	}
	if err != nil || value.Identity.Validate() != nil || value.Live.Inode == 0 || value.Live.MountID <= 0 {
		t.Fatalf("observe the owned temporary directory (%T)", err)
	}
	return value
}

// The remote runner deliberately puts GOTMPDIR on its owned ext4 workspace;
// /tmp may be a different filesystem. Do not change TMPDIR for other tests.
func rootStorageTestDirectory(t *testing.T) string {
	t.Helper()
	base := os.Getenv("GOTMPDIR")
	if base == "" {
		return t.TempDir()
	}
	if !filepath.IsAbs(base) {
		t.Fatal("GOTMPDIR must be an absolute owned verification directory")
	}
	base = filepath.Clean(base)
	parent, err := os.Lstat(base)
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 {
		t.Fatal("GOTMPDIR is not an available nonsymlink directory")
	}
	directory, err := os.MkdirTemp(base, "goby-root-identity-")
	if err != nil {
		t.Fatal("create the independent root identity fixture")
	}
	identity, err := os.Lstat(directory)
	if err != nil || !identity.IsDir() || filepath.Dir(directory) != base {
		t.Fatal("the new root identity fixture is not inside GOTMPDIR")
	}
	t.Cleanup(func() {
		current, err := os.Lstat(directory)
		if err != nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(identity, current) {
			t.Error("the owned root identity fixture changed before cleanup; it was retained")
			return
		}
		if err := os.RemoveAll(directory); err != nil {
			t.Errorf("remove the exact owned root identity fixture: %v", err)
		}
	})
	return directory
}

func openRootStorageTestDirectory(t *testing.T, path string) *os.File {
	t.Helper()
	directory, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = directory.Close() })
	return directory
}

func TestRootStorageIdentitySurvivesNewOpenAndOrdinaryContentChanges(t *testing.T) {
	path := rootStorageTestDirectory(t)
	first := openRootStorageTestDirectory(t, path)
	before := rootStorageTestObservation(t, first)
	second := openRootStorageTestDirectory(t, path)
	independent := rootStorageTestObservation(t, second)
	if !before.Identity.Equal(independent.Identity) || before.Live != independent.Live {
		t.Fatal("an independent open of the same directory changed its identity")
	}
	file := filepath.Join(path, "owned-content")
	if err := os.WriteFile(file, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("changed content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, "owned-child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	after := rootStorageTestObservation(t, first)
	if !before.Identity.Equal(after.Identity) || before.Live != after.Live {
		t.Fatal("normal directory content changes altered the stored identity profile")
	}
	if _, err := first.Stat(); err != nil {
		t.Fatal("the adapter closed its caller's directory")
	}
}

func TestRootStorageIdentityDistinguishesNamedReplacementAndOriginalRestoration(t *testing.T) {
	parent := rootStorageTestDirectory(t)
	path, saved := filepath.Join(parent, "bound"), filepath.Join(parent, "retained-original")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	original := openRootStorageTestDirectory(t, path)
	before := rootStorageTestObservation(t, original)
	if err := os.Rename(path, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	replacement := openRootStorageTestDirectory(t, path)
	replaced := rootStorageTestObservation(t, replacement)
	if before.Identity.Equal(replaced.Identity) || before.Identity.FilesystemUUID != replaced.Identity.FilesystemUUID {
		t.Fatal("a new empty directory at the same name was not distinguished within its filesystem")
	}
	// The held original still has its identity. This is deliberately not a
	// check that its former pathname remains bound; callers must prove that.
	held := rootStorageTestObservation(t, original)
	if !before.Identity.Equal(held.Identity) {
		t.Fatal("renaming the held original changed its directory identity")
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, path); err != nil {
		t.Fatal(err)
	}
	restored := rootStorageTestObservation(t, openRootStorageTestDirectory(t, path))
	if !before.Identity.Equal(restored.Identity) {
		t.Fatal("restoring the original directory did not restore its original identity")
	}
}

type rootStorageHelperResult struct {
	Observation RootStorageObservation `json:"observation"`
	Error       string                 `json:"error,omitempty"`
}

func TestRootStorageIdentityAcrossObserverProcesses(t *testing.T) {
	directory := openRootStorageTestDirectory(t, rootStorageTestDirectory(t))
	before := rootStorageTestObservation(t, directory)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestRootStorageIdentityProcessHelper$")
	command.Env = append(os.Environ(), "GOBY_ROOT_STORAGE_IDENTITY_TEST_HELPER=1")
	command.ExtraFiles = []*os.File{directory}
	raw, err := command.Output()
	if err != nil || len(raw) > 4096 {
		t.Fatalf("the bounded identity helper did not complete (%T)", err)
	}
	var result rootStorageHelperResult
	if err := json.Unmarshal(raw, &result); err != nil || result.Error != "" ||
		!before.Identity.Equal(result.Observation.Identity) || before.Live != result.Observation.Live {
		t.Fatal("another observer process did not return the same complete directory identity")
	}
	// This is a separate-process test on the current mount, not a reboot,
	// namespace change, nested-mount, or alternate-filesystem acceptance test.
}

func TestRootStorageIdentityProcessHelper(t *testing.T) {
	if os.Getenv("GOBY_ROOT_STORAGE_IDENTITY_TEST_HELPER") != "1" {
		return
	}
	result := rootStorageHelperResult{}
	// FD 3 is exclusively supplied by the parent test. Reopen that directory
	// in this process without accepting an arbitrary pathname or probing a DB.
	inherited := os.NewFile(3, "owned-root-identity-test")
	directory, err := os.Open("/proc/self/fd/3")
	if err == nil {
		result.Observation, err = ObserveRootStorageIdentity(directory)
		_ = directory.Close()
	}
	_ = inherited.Close()
	if errors.Is(err, ErrRootStorageIdentityUnsupported) {
		result.Error = "unsupported"
	} else if err != nil {
		result.Error = "unavailable"
	}
	raw, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		os.Exit(2)
	}
	_, _ = fmt.Fprintln(os.Stdout, string(raw))
	os.Exit(0)
}

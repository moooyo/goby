//go:build linux

package transcode

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// These actual file checks do not mint a pool, executable preparation or hard
// backend capability. The separate real prepared fixture exercises issuance.
func fixedExecutableFileFixture(t *testing.T) (*os.File, string, string) {
	t.Helper()
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		t.Skip("root-owned immutable executable file checks require root")
	}
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(".", "executable-file-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "approved-input")
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("remove exact fixture file: %v", err)
		}
		if err := os.Remove(directory); err != nil {
			t.Errorf("remove exact empty fixture directory: %v", err)
		}
	})
	if err = os.WriteFile(path, content, 0o555); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	flags, err := unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil {
		_ = file.Close()
		t.Skipf("immutable inode flags are unavailable: %v", err)
	}
	t.Cleanup(func() {
		if err := unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags); err != nil && !errors.Is(err, unix.EBADF) {
			t.Errorf("clear owned fixture immutable flag: %v", err)
		}
		_ = file.Close()
	})
	if err = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags|fixedVolumeImmutableFlag); err != nil {
		t.Skipf("root immutable flag prerequisite is unavailable: %v", err)
	}
	hash := sha256.Sum256(content)
	return file, path, hex.EncodeToString(hash[:])
}

func TestFixedExecutableNativeFileChecks(t *testing.T) {
	file, _, digest := fixedExecutableFileFixture(t)
	stat, allocation, err := fixedExecutableFile(file, digest)
	if err != nil || stat.Mode&0o7777 != 0o555 || allocation != stat.Blocks*512 || allocation <= 0 {
		t.Fatalf("actual strict executable file check = %v, %d", err, allocation)
	}
	for _, invalid := range []string{"", strings.Repeat("0", 64), strings.ToUpper(digest)} {
		if _, _, err := fixedExecutableFile(file, invalid); err == nil {
			t.Fatal("missing, incorrect or noncanonical approval digest passed")
		}
	}
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = unix.FcntlInt(file.Fd(), unix.F_SETFD, flags & ^unix.FD_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	if _, _, err = fixedExecutableFile(file, digest); err == nil {
		t.Fatal("descriptor without CLOEXEC was accepted")
	}
	if _, err = unix.FcntlInt(file.Fd(), unix.F_SETFD, flags); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.FcntlInt(file.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	closed := os.NewFile(uintptr(fd), "closed-executable-negative")
	if err = closed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err = fixedExecutableFile(closed, digest); err == nil {
		t.Fatal("closed descriptor was accepted")
	}
	var private unix.Stat_t
	if fixedVolumeOwnedRegular(file, &private) == nil {
		t.Fatal("executable approval weakened ordinary private control-file admission")
	}
	if _, err = (&fixedPoolExecutablePreparation{}).approve(file, "/not-a-live-pool", digest); err == nil {
		t.Fatal("inactive preparation minted an executable")
	}
	if _, err = (&fixedPoolExecutablePreparation{active: true, issuer: struct{}{}}).approve(file, "/not-a-live-pool", digest); err == nil {
		t.Fatal("foreign issuer minted an executable")
	}
}

func TestFixedExecutableInventoryRequiresExplicitApprovedDigest(t *testing.T) {
	// A metadata comparison is not a pool or executable capability. This pure
	// regression prevents a formerly blank fixture inventory from weakening
	// the explicit digest requirement; real issuance still checks its held FD.
	path, digest := "/owned/tool", strings.Repeat("a", sha256.Size*2)
	stat := unix.Stat_t{Dev: 1, Ino: 2}
	approved := fixedBackingPoolStaticObject{Path: path, SHA256: digest, Device: 1, Inode: 2, AllocatedBytes: 4096}
	if !fixedExecutableListedIdentity([]fixedBackingPoolStaticObject{approved}, path, digest, stat, 4096) {
		t.Fatal("exact explicitly approved inventory did not match")
	}
	for _, test := range []struct {
		name   string
		change func(*fixedBackingPoolStaticObject)
	}{
		{"empty digest", func(object *fixedBackingPoolStaticObject) { object.SHA256 = "" }},
		{"wrong digest", func(object *fixedBackingPoolStaticObject) { object.SHA256 = strings.Repeat("b", sha256.Size*2) }},
		{"foreign path", func(object *fixedBackingPoolStaticObject) { object.Path = "/foreign/tool" }},
		{"foreign device", func(object *fixedBackingPoolStaticObject) { object.Device++ }},
		{"foreign inode", func(object *fixedBackingPoolStaticObject) { object.Inode++ }},
		{"omitted allocation", func(object *fixedBackingPoolStaticObject) { object.AllocatedBytes = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			object := approved
			test.change(&object)
			if fixedExecutableListedIdentity([]fixedBackingPoolStaticObject{object}, path, digest, stat, 4096) {
				t.Fatal("incomplete or foreign executable inventory matched")
			}
		})
	}
	if fixedExecutableListedIdentity([]fixedBackingPoolStaticObject{approved}, path, "", stat, 4096) ||
		fixedExecutableListedIdentity(nil, path, digest, stat, 4096) {
		t.Fatal("empty approval or omitted executable inventory matched")
	}
}

func TestFixedExecutableRejectsPathOnlyDescriptor(t *testing.T) {
	_, path, digest := fixedExecutableFileFixture(t)
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), "path-only-executable-negative")
	defer file.Close()
	if _, _, err = fixedExecutableFile(file, digest); err == nil {
		t.Fatal("path-only descriptor became an executable input")
	}
}

func TestFixedExecutableOrdinaryImmutableObjectStaysPrivate(t *testing.T) {
	file, path, digest := fixedExecutableFileFixture(t)
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		t.Fatal(err)
	}
	pool := &pooledFixedVolumeProvider{}
	snapshot := fixedPoolStaticSnapshot{file: file, path: path, stat: stat, allocatedBytes: stat.Blocks * 512, digest: digest, immutable: true}
	if err := pool.checkStatic(snapshot); err == nil {
		t.Fatal("0555 inventory file acquired execute approval without a live capability")
	}
}

func TestFixedExecutableSymbolicOrReplacedNameDoesNotMatchHeldInode(t *testing.T) {
	file, path, _ := fixedExecutableFileFixture(t)
	link := path + "-symbolic"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(link)
	if err := fixedPoolCheckPath(link, file, false); err == nil {
		t.Fatal("symbolic approval selected a held executable")
	}
	flags, err := unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags & ^fixedVolumeImmutableFlag); err != nil {
		t.Fatal(err)
	}
	oldPath := path + "-old"
	if err = os.Rename(path, oldPath); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(oldPath)
	if err = os.WriteFile(path, []byte("replacement"), 0o555); err != nil {
		t.Fatal(err)
	}
	if err = fixedPoolCheckPath(path, file, false); err == nil {
		t.Fatal("replacement pathname selected a different executable inode")
	}
}

func TestFixedExecutableDirectoryCannotUseExecuteModeException(t *testing.T) {
	file, path, digest := fixedExecutableFileFixture(t)
	_ = file
	directory := filepath.Dir(path) + "/ordinary-control"
	if err := os.Mkdir(directory, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(directory)
	control, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	flags, err := unix.IoctlGetInt(int(control.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.IoctlSetPointerInt(int(control.Fd()), unix.FS_IOC_SETFLAGS, flags|fixedVolumeImmutableFlag); err != nil {
		t.Fatal(err)
	}
	defer unix.IoctlSetPointerInt(int(control.Fd()), unix.FS_IOC_SETFLAGS, flags)
	var stat unix.Stat_t
	if err = unix.Fstat(int(control.Fd()), &stat); err != nil {
		t.Fatal(err)
	}
	pool := &pooledFixedVolumeProvider{}
	snapshot := fixedPoolStaticSnapshot{file: control, path: directory, stat: stat, allocatedBytes: stat.Blocks * 512, digest: digest, immutable: true}
	if err = pool.checkStatic(snapshot); err == nil {
		t.Fatal("0555 ordinary directory acquired executable approval")
	}
}

func TestFixedExecutableRejectsMutableModesAndWritableDescriptor(t *testing.T) {
	for _, mode := range []uint32{0o755, 0o500, 0o4555, 0o2555} {
		t.Run(strconv.FormatUint(uint64(mode), 8), func(t *testing.T) {
			file, _, digest := fixedExecutableFileFixture(t)
			flags, err := unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS)
			if err != nil {
				t.Fatal(err)
			}
			if err = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags & ^fixedVolumeImmutableFlag); err != nil {
				t.Fatal(err)
			}
			if err = unix.Fchmod(int(file.Fd()), mode); err != nil {
				t.Fatal(err)
			}
			if err = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags); err != nil {
				t.Fatal(err)
			}
			if _, _, err = fixedExecutableFile(file, digest); err == nil {
				t.Fatalf("mode %o was approved", mode)
			}
		})
	}
	file, path, digest := fixedExecutableFileFixture(t)
	flags, err := unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags & ^fixedVolumeImmutableFlag); err != nil {
		t.Fatal(err)
	}
	writable, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writable.Close()
	if err = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags); err != nil {
		t.Fatal(err)
	}
	if _, _, err = fixedExecutableFile(writable, digest); err == nil {
		t.Fatal("writable descriptor was approved")
	}
}

func TestFixedExecutableRejectsNonRootAndHardlink(t *testing.T) {
	file, path, digest := fixedExecutableFileFixture(t)
	flags, err := unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags & ^fixedVolumeImmutableFlag); err != nil {
		t.Fatal(err)
	}
	link := path + "-link"
	if err = os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags & ^fixedVolumeImmutableFlag); err != nil {
			t.Errorf("clear exact hardlink fixture flag: %v", err)
		}
		if err := os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("remove exact fixture hardlink: %v", err)
		}
	})
	if err = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags); err != nil {
		t.Fatal(err)
	}
	if _, _, err = fixedExecutableFile(file, digest); err == nil {
		t.Fatal("hardlinked executable was approved")
	}
	if err = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags & ^fixedVolumeImmutableFlag); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err = unix.Fchown(int(file.Fd()), 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if err = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags); err != nil {
		t.Fatal(err)
	}
	if _, _, err = fixedExecutableFile(file, digest); err == nil {
		t.Fatal("non-root executable was approved")
	}
}

func TestFixedExecutableRejectsFileCapabilitiesAndUnsealedInput(t *testing.T) {
	file, _, digest := fixedExecutableFileFixture(t)
	flags, err := unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags & ^fixedVolumeImmutableFlag); err != nil {
		t.Fatal(err)
	}
	if _, _, err = fixedExecutableFile(file, digest); err == nil {
		t.Fatal("mutable executable input was approved")
	}
	capability := make([]byte, 20)
	binary.LittleEndian.PutUint32(capability, 0x02000001)
	binary.LittleEndian.PutUint32(capability[4:], 1)
	if err = unix.Fsetxattr(int(file.Fd()), "security.capability", capability, 0); err != nil {
		t.Skipf("actual file capability fixture is unavailable: %v", err)
	}
	if err = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags); err != nil {
		t.Fatal(err)
	}
	if _, _, err = fixedExecutableFile(file, digest); err == nil {
		t.Fatal("file capabilities were accepted")
	}
}

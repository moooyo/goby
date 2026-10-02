//go:build linux

package commanddomain

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Linux UAPI FS_IMMUTABLE_FL is not exported by the pinned x/sys version.
const executableCapabilityTestImmutableFlag = 0x00000010

type executableCapabilityTestFile struct {
	path   string
	digest string
	source *os.File
}

func TestExecutableCapabilityRequiresActualSupportedRootIssuer(t *testing.T) {
	if runtime.GOARCH == "amd64" && os.Getuid() == 0 && os.Geteuid() == 0 {
		t.Skip("this environment has the supported root issuer identity")
	}
	capability, err := NewExecutableCapability(nil, "/approved/tool", strings.Repeat("a", 64))
	if capability != nil {
		t.Cleanup(func() { _ = capability.Close() })
	}
	if capability != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("an unsupported actual issuer created executable trust: capability=%v error=%v", capability, err)
	}
}

func TestExecutableCapabilityRejectsNonImmutableRealELF(t *testing.T) {
	fixture := executableCapabilityTestCopiedELF(t, nil)
	flags, err := unix.IoctlGetInt(int(fixture.source.Fd()), unix.FS_IOC_GETFLAGS)
	if err == nil && flags&executableCapabilityTestImmutableFlag != 0 {
		t.Fatal("the negative fixture unexpectedly acquired immutable authority")
	}
	executableCapabilityTestRejected(t, fixture.source, fixture.path, fixture.digest)
	if _, err := fixture.source.Stat(); err != nil {
		t.Fatalf("a rejected capability consumed its caller's source descriptor: %v", err)
	}
}

func TestExecutableCapabilityRejectsInvalidSourceFlagsAndApproval(t *testing.T) {
	fixture := executableCapabilityTestCopiedELF(t, nil)
	for _, test := range []struct {
		name   string
		source *os.File
		path   string
		digest string
	}{
		{name: "nil source", path: fixture.path, digest: fixture.digest},
		{name: "relative approval", source: fixture.source, path: "tool", digest: fixture.digest},
		{name: "unclean approval", source: fixture.source, path: fixture.path + "/.", digest: fixture.digest},
		{name: "control in approval", source: fixture.source, path: fixture.path + "\n", digest: fixture.digest},
		{name: "empty hash", source: fixture.source, path: fixture.path},
		{name: "short hash", source: fixture.source, path: fixture.path, digest: fixture.digest[:63]},
		{name: "uppercase hash", source: fixture.source, path: fixture.path, digest: "A" + fixture.digest[1:]},
		{name: "nonhex hash", source: fixture.source, path: fixture.path, digest: "z" + fixture.digest[1:]},
	} {
		t.Run(test.name, func(t *testing.T) {
			executableCapabilityTestRejected(t, test.source, test.path, test.digest)
		})
	}
	for _, test := range []struct {
		name  string
		flags int
	}{
		{name: "read write", flags: unix.O_RDWR | unix.O_CLOEXEC},
		{name: "write only", flags: unix.O_WRONLY | unix.O_CLOEXEC},
		{name: "path only", flags: unix.O_PATH | unix.O_CLOEXEC},
		{name: "missing close on exec", flags: unix.O_RDONLY},
	} {
		t.Run(test.name, func(t *testing.T) {
			fd, err := unix.Open(fixture.path, test.flags, 0)
			if err != nil {
				t.Skipf("the environment cannot provide this actual negative descriptor: %v", err)
			}
			source := os.NewFile(uintptr(fd), test.name)
			defer source.Close()
			executableCapabilityTestRejected(t, source, fixture.path, fixture.digest)
		})
	}
	closed, err := os.Open(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	executableCapabilityTestRejected(t, closed, fixture.path, fixture.digest)
}

func TestExecutableCapabilityImmutableDescriptorDuplicatesAreIndependent(t *testing.T) {
	fixture := executableCapabilityTestImmutableELF(t, nil)
	capability := executableCapabilityTestIssue(t, fixture)
	first := executableCapabilityTestOpen(t, capability)
	second := executableCapabilityTestOpen(t, capability)
	if first.Fd() == second.Fd() || first.Fd() == fixture.source.Fd() || second.Fd() == fixture.source.Fd() {
		t.Fatal("the capability returned a borrowed descriptor instead of independently owned duplicates")
	}
	executableCapabilityTestSameReadOnlyObject(t, fixture.source, first)
	executableCapabilityTestSameReadOnlyObject(t, fixture.source, second)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	executableCapabilityTestELFMagic(t, second)
	executableCapabilityTestELFMagic(t, fixture.source)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("a capability without domain borrows did not close: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("repeated capability close changed ownership: %v", err)
	}
	if file, err := capability.Open(); file != nil || err == nil {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("a closed capability opened another executable descriptor: file=%v error=%v", file, err)
	}
	executableCapabilityTestELFMagic(t, fixture.source)
}

func TestExecutableCapabilityOpenUsesHeldInodeAfterParentRename(t *testing.T) {
	fixture := executableCapabilityTestImmutableELF(t, nil)
	capability := executableCapabilityTestIssue(t, fixture)
	directory := filepath.Dir(fixture.path)
	moved := filepath.Join(filepath.Dir(directory), "moved")
	if err := os.Rename(directory, moved); err != nil {
		t.Skipf("the environment cannot rename the immutable file's containing directory: %v", err)
	}
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.path, []byte("unapproved replacement"), 0555); err != nil {
		t.Fatal(err)
	}
	opened := executableCapabilityTestOpen(t, capability)
	executableCapabilityTestSameReadOnlyObject(t, fixture.source, opened)
	executableCapabilityTestELFMagic(t, opened)
}

func TestExecutableCapabilityImmutableApprovalBindsOriginalDescriptor(t *testing.T) {
	fixture := executableCapabilityTestImmutableELF(t, nil)
	wrongDigest := fixture.digest
	if wrongDigest[0] == '0' {
		wrongDigest = "1" + wrongDigest[1:]
	} else {
		wrongDigest = "0" + wrongDigest[1:]
	}
	executableCapabilityTestRejected(t, fixture.source, fixture.path, wrongDigest)
	other := executableCapabilityTestImmutableELF(t, nil)
	executableCapabilityTestRejected(t, fixture.source, other.path, fixture.digest)
	flags, err := unix.FcntlInt(fixture.source.Fd(), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(fixture.source.Fd(), unix.F_SETFD, flags&^unix.FD_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := unix.FcntlInt(fixture.source.Fd(), unix.F_SETFD, flags); err != nil {
			t.Errorf("restore the original source descriptor flags: %v", err)
		}
	}()
	executableCapabilityTestRejected(t, fixture.source, fixture.path, fixture.digest)
}

func TestExecutableCapabilityImmutableApprovalRejectsLeafAndAncestorSymlinks(t *testing.T) {
	fixture := executableCapabilityTestImmutableELF(t, nil)
	capability := executableCapabilityTestIssue(t, fixture)
	if err := capability.Close(); err != nil {
		t.Fatalf("close the valid direct-path approval before testing symlink rejection: %v", err)
	}
	directory := filepath.Dir(fixture.path)
	leaf := filepath.Join(directory, "alias")
	if err := os.Symlink(fixture.path, leaf); err != nil {
		t.Skipf("the environment cannot provide the actual leaf symlink fixture: %v", err)
	}
	ancestor := filepath.Join(filepath.Dir(directory), "alias-directory")
	if err := os.Symlink(directory, ancestor); err != nil {
		t.Skipf("the environment cannot provide the actual ancestor symlink fixture: %v", err)
	}
	for _, test := range []struct {
		name string
		path string
	}{
		{name: "leaf", path: leaf},
		{name: "ancestor directory", path: filepath.Join(ancestor, filepath.Base(fixture.path))},
	} {
		t.Run(test.name, func(t *testing.T) {
			executableCapabilityTestRejected(t, fixture.source, test.path, fixture.digest)
			if _, err := fixture.source.Stat(); err != nil {
				t.Fatalf("a rejected symlink approval consumed the caller's original descriptor: %v", err)
			}
			executableCapabilityTestELFMagic(t, fixture.source)
		})
	}
}

func TestExecutableCapabilityRejectsImmutableUnsafeMetadata(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(string) error
	}{
		{name: "owner write", prepare: func(path string) error { return os.Chmod(path, 0755) }},
		{name: "missing execute", prepare: func(path string) error { return os.Chmod(path, 0444) }},
		{name: "setuid", prepare: func(path string) error { return os.Chmod(path, 0555|os.ModeSetuid) }},
		{name: "setgid", prepare: func(path string) error { return os.Chmod(path, 0555|os.ModeSetgid) }},
		{name: "nonroot uid", prepare: func(path string) error { return os.Chown(path, 1, 0) }},
		{name: "nonroot gid", prepare: func(path string) error { return os.Chown(path, 0, 1) }},
		{name: "hardlink", prepare: func(path string) error { return os.Link(path, path+".link") }},
		{name: "file capability", prepare: func(path string) error {
			attribute := make([]byte, 20)
			binary.LittleEndian.PutUint32(attribute[0:4], 0x02000001)
			binary.LittleEndian.PutUint32(attribute[4:8], 1<<10)
			return unix.Setxattr(path, "security.capability", attribute, 0)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := executableCapabilityTestImmutableELF(t, test.prepare)
			executableCapabilityTestRejected(t, fixture.source, fixture.path, fixture.digest)
		})
	}
}

func TestExecutableCapabilityIssuedObjectStillRejectsJSON(t *testing.T) {
	fixture := executableCapabilityTestImmutableELF(t, nil)
	capability := executableCapabilityTestIssue(t, fixture)
	for _, value := range []any{*capability, capability} {
		if encoded, err := json.Marshal(value); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("issued executable trust was serialized: bytes=%s error=%v", encoded, err)
		}
	}
	opened := executableCapabilityTestOpen(t, capability)
	executableCapabilityTestSameReadOnlyObject(t, fixture.source, opened)
}

func executableCapabilityTestCopiedELF(t *testing.T, prepare func(string) error) executableCapabilityTestFile {
	t.Helper()
	if runtime.GOARCH != "amd64" || os.Getuid() != 0 || os.Geteuid() != 0 {
		t.Skip("actual native root-issued executable fixtures require Linux amd64 and root")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	image, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if image.Class != elf.ELFCLASS64 || image.Data != elf.ELFDATA2LSB || image.Machine != elf.EM_X86_64 ||
		(image.Type != elf.ET_EXEC && image.Type != elf.ET_DYN) {
		t.Skip("the actual test executable is not a supported native ELF image")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for ancestor := cwd; ; ancestor = filepath.Dir(ancestor) {
		var stat unix.Stat_t
		if unix.Lstat(ancestor, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Mode&0022 != 0 {
			t.Skip("the current directory has no closed root-owned ancestor chain for this fixture")
		}
		if ancestor == filepath.Dir(ancestor) {
			break
		}
	}
	base, err := os.MkdirTemp(".", ".goby-executable-capability-")
	if err != nil {
		t.Fatal(err)
	}
	base, err = filepath.Abs(base)
	if err != nil {
		t.Fatal(err)
	}
	// Registered before the source and immutable-flag cleanups, this runs last.
	// Remove only names created by these fixtures. Unexpected contents keep the
	// private directory intact and report an error instead of recursive deletion.
	t.Cleanup(func() {
		remove := func(path string) {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("remove the exact executable fixture object %s: %v", path, err)
			}
		}
		remove(filepath.Join(base, "alias-directory"))
		for _, name := range []string{"held", "moved"} {
			directory := filepath.Join(base, name)
			var stat unix.Stat_t
			if err := unix.Lstat(directory, &stat); errors.Is(err, unix.ENOENT) {
				continue
			} else if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Mode&0022 != 0 {
				t.Errorf("retain the changed executable fixture directory %s", directory)
				continue
			}
			for _, leaf := range []string{"alias", "tool.link", "tool"} {
				remove(filepath.Join(directory, leaf))
			}
			remove(directory)
		}
		remove(base)
	})
	directory := filepath.Join(base, "held")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "tool")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 0, 0); err != nil {
		t.Skipf("the environment cannot create the actual root-owned fixture: %v", err)
	}
	if err := os.Chmod(path, 0555); err != nil {
		t.Fatal(err)
	}
	if prepare != nil {
		if err := prepare(path); err != nil {
			t.Skipf("the environment cannot prepare this actual kernel metadata case: %v", err)
		}
	}
	source, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	digest := sha256.Sum256(data)
	return executableCapabilityTestFile{path: path, digest: hex.EncodeToString(digest[:]), source: source}
}

func executableCapabilityTestImmutableELF(t *testing.T, prepare func(string) error) executableCapabilityTestFile {
	t.Helper()
	fixture := executableCapabilityTestCopiedELF(t, prepare)
	flags, err := unix.IoctlGetInt(int(fixture.source.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil {
		t.Skipf("this filesystem cannot attest actual immutable flags: %v", err)
	}
	t.Cleanup(func() {
		actual, err := unix.IoctlGetInt(int(fixture.source.Fd()), unix.FS_IOC_GETFLAGS)
		if err != nil {
			t.Errorf("read the temporary executable's inode flags during cleanup: %v", err)
			return
		}
		if actual == flags {
			return
		}
		if err := unix.IoctlSetPointerInt(int(fixture.source.Fd()), unix.FS_IOC_SETFLAGS, flags); err != nil {
			t.Errorf("restore the temporary executable's original inode flags: %v", err)
		}
	})
	if err := unix.IoctlSetPointerInt(int(fixture.source.Fd()), unix.FS_IOC_SETFLAGS, flags|executableCapabilityTestImmutableFlag); err != nil {
		t.Skipf("the environment cannot grant actual immutable fixture authority: %v", err)
	}
	actual, err := unix.IoctlGetInt(int(fixture.source.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil || actual&executableCapabilityTestImmutableFlag == 0 {
		t.Fatalf("immutable setup did not establish the actual kernel flag: flags=%x error=%v", actual, err)
	}
	return fixture
}

func executableCapabilityTestIssue(t *testing.T, fixture executableCapabilityTestFile) *ExecutableCapability {
	t.Helper()
	capability, err := NewExecutableCapability(fixture.source, fixture.path, fixture.digest)
	if capability != nil {
		t.Cleanup(func() {
			if err := capability.Close(); err != nil {
				t.Errorf("close the actual issued executable capability: %v", err)
			}
		})
	}
	if err != nil || capability == nil {
		t.Fatalf("the actual immutable approved executable was rejected: capability=%v error=%v", capability, err)
	}
	return capability
}

func executableCapabilityTestOpen(t *testing.T, capability *ExecutableCapability) *os.File {
	t.Helper()
	file, err := capability.Open()
	if file != nil {
		t.Cleanup(func() { _ = file.Close() })
	}
	if err != nil || file == nil {
		t.Fatalf("open the actual held executable capability: file=%v error=%v", file, err)
	}
	return file
}

func executableCapabilityTestRejected(t *testing.T, source *os.File, path, digest string) {
	t.Helper()
	capability, err := NewExecutableCapability(source, path, digest)
	if capability != nil {
		t.Cleanup(func() { _ = capability.Close() })
	}
	if capability != nil || !errors.Is(err, ErrUnsafe) {
		t.Fatalf("an unapproved actual executable received trust: capability=%v error=%v", capability, err)
	}
}

func executableCapabilityTestSameReadOnlyObject(t *testing.T, source, duplicate *os.File) {
	t.Helper()
	var original, copied unix.Stat_t
	if err := unix.Fstat(int(source.Fd()), &original); err != nil {
		t.Fatal(err)
	}
	if err := unix.Fstat(int(duplicate.Fd()), &copied); err != nil {
		t.Fatal(err)
	}
	flags, err := unix.FcntlInt(duplicate.Fd(), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	access, err := unix.FcntlInt(duplicate.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if original.Dev != copied.Dev || original.Ino != copied.Ino || copied.Mode&unix.S_IFMT != unix.S_IFREG ||
		copied.Uid != 0 || copied.Gid != 0 || copied.Nlink != 1 || copied.Mode&07777 != 0555 ||
		flags&unix.FD_CLOEXEC == 0 || access&unix.O_ACCMODE != unix.O_RDONLY || access&unix.O_PATH != 0 {
		t.Fatalf("the public duplicate lost its held immutable executable identity or descriptor policy: original=%+v duplicate=%+v flags=%x access=%x",
			original, copied, flags, access)
	}
}

func executableCapabilityTestELFMagic(t *testing.T, file *os.File) {
	t.Helper()
	var magic [4]byte
	if _, err := file.ReadAt(magic[:], 0); err != nil || magic != [4]byte{0x7f, 'E', 'L', 'F'} {
		t.Fatalf("an independently owned executable descriptor became unreadable: magic=%x error=%v", magic, err)
	}
}

//go:build linux

package commanddomain

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// These tests exercise real descriptor roles and anonymous memory sealing.
// They do not construct a fake positive hard-storage or command-domain backend.
// Native positive cgroup/launcher tests need a separately reviewed root fixture.
func TestDomainPayloadDescriptorRoles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source")
	writable, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer writable.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(int(writable.Fd()), &stat); err != nil {
		t.Fatal(err)
	}
	readonly, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer readonly.Close()
	role, err := payloadRole(readonly, stat.Dev)
	if err != nil || role.Role != "source" || role.Writable {
		t.Fatalf("read-only source role: %+v %v", role, err)
	}
	role, err = payloadRole(writable, stat.Dev)
	if err != nil || role.Role != "workspace-output" || !role.Writable {
		t.Fatalf("same-domain writer role: %+v %v", role, err)
	}
	if _, err := payloadRole(writable, stat.Dev+1); !errors.Is(err, ErrUnsafe) {
		t.Fatal("an outside-domain writable regular descriptor was accepted")
	}
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readPipe.Close()
	defer writePipe.Close()
	if role, err := payloadRole(readPipe, stat.Dev); err != nil || role.Role != "source" || role.Writable {
		t.Fatalf("read pipe role: %+v %v", role, err)
	}
	if role, err := payloadRole(writePipe, stat.Dev); err != nil || role.Role != "progress-output" || !role.Writable {
		t.Fatalf("write pipe role: %+v %v", role, err)
	}
	null, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if _, err := payloadRole(null, stat.Dev); !errors.Is(err, ErrUnsafe) {
		t.Fatal("a character device was accepted as a payload")
	}
}

func TestDomainBorrowedDirectoryIdentityAndDuplicateLifetime(t *testing.T) {
	borrowed, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Close()
	before, err := directoryIdentity(borrowed)
	if err != nil || before.Device == 0 || before.Inode == 0 || before.MountID == 0 {
		t.Fatalf("actual directory identity: %+v %v", before, err)
	}
	owned, err := duplicate(borrowed)
	if err != nil {
		t.Fatal(err)
	}
	if after, err := directoryIdentity(owned); err != nil || after != before {
		t.Fatalf("duplicate identity: %+v %v", after, err)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	if after, err := directoryIdentity(borrowed); err != nil || after != before {
		t.Fatal("closing the duplicate consumed the borrowed descriptor")
	}
}

func TestDomainAnonymousConfigHasActualCompleteWriteSeals(t *testing.T) {
	data := []byte("bounded immutable wire configuration")
	file, err := sealedConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	seals, err := unix.FcntlInt(file.Fd(), unix.F_GET_SEALS, 0)
	const expected = unix.F_SEAL_SEAL | unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE
	if err != nil || seals&expected != expected {
		t.Fatalf("config seals=%x error=%v", seals, err)
	}
	if _, err := file.WriteAt([]byte("x"), 0); !errors.Is(err, unix.EPERM) {
		t.Fatalf("sealed write error=%v", err)
	}
	if err := file.Truncate(0); !errors.Is(err, unix.EPERM) {
		t.Fatalf("sealed truncate error=%v", err)
	}
	actual, err := io.ReadAll(file)
	if err != nil || string(actual) != string(data) {
		t.Fatalf("sealed content=%q error=%v", actual, err)
	}
}

func TestDomainAcceptsOnlyOrdinaryTemplateProcessAttributes(t *testing.T) {
	if !plainProcessAttributes(&syscall.SysProcAttr{}) || !plainProcessAttributes(&syscall.SysProcAttr{Setpgid: true}) {
		t.Fatal("ordinary template attributes were rejected")
	}
	for _, attributes := range []*syscall.SysProcAttr{
		{UseCgroupFD: true, CgroupFD: 9}, {Pdeathsig: syscall.SIGKILL}, {Cloneflags: unix.CLONE_NEWUSER},
		{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}, {Setpgid: true, Pgid: 1},
	} {
		if plainProcessAttributes(attributes) {
			t.Fatalf("caller-controlled native attributes accepted: %+v", attributes)
		}
	}
}

func TestDomainStandardNullIsNotAnArbitraryCharacterRole(t *testing.T) {
	readonly, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer readonly.Close()
	writable, err := os.OpenFile("/dev/null", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writable.Close()
	if !standardNull(readonly, false) || standardNull(readonly, true) || !standardNull(writable, true) || standardNull(writable, false) {
		t.Fatal("null descriptor access mode was not enforced")
	}
	zero, err := os.Open("/dev/zero")
	if err != nil {
		t.Fatal(err)
	}
	defer zero.Close()
	if standardNull(zero, false) {
		t.Fatal("another character device was accepted as null")
	}
}

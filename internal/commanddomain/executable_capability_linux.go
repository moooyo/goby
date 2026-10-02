//go:build linux

package commanddomain

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	rootExecutableIssuerKind = 1
	executableImmutableFlag  = 0x00000010 // Linux UAPI FS_IMMUTABLE_FL.
)

func executableIssuerNow() (executableCapabilityIssuer, error) {
	if runtime.GOARCH != "amd64" || os.Getuid() != 0 || os.Geteuid() != 0 {
		return executableCapabilityIssuer{}, ErrUnavailable
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || len(boot) != 37 || !launcherBootID(strings.TrimSuffix(string(boot), "\n")) {
		return executableCapabilityIssuer{}, ErrUnsafe
	}
	var stat unix.Stat_t
	var fs unix.Statfs_t
	if unix.Stat("/proc/thread-self/ns/mnt", &stat) != nil || unix.Statfs("/proc/thread-self/ns/mnt", &fs) != nil || fs.Type != unix.NSFS_MAGIC {
		return executableCapabilityIssuer{}, ErrUnsafe
	}
	return executableCapabilityIssuer{kind: rootExecutableIssuerKind, pid: os.Getpid(), boot: strings.TrimSuffix(string(boot), "\n"),
		namespace: LauncherObjectIdentity{Device: stat.Dev, Inode: stat.Ino}}, nil
}

func newExecutableCapability(source *os.File, approval ApprovedExecutable) (*ExecutableCapability, error) {
	issuer, err := executableIssuerNow()
	if err != nil {
		return nil, err
	}
	if source == nil || !filepath.IsAbs(approval.Path) || filepath.Clean(approval.Path) != approval.Path ||
		!launcherPlainText(approval.Path) || !launcherLowerHex(approval.SHA256, 64) {
		return nil, ErrUnsafe
	}
	namedFile, err := openAbsolute(approval.Path, unix.O_PATH)
	if err != nil {
		return nil, ErrUnsafe
	}
	partial := &ExecutableCapability{executableCapabilityState: &executableCapabilityState{named: namedFile, approval: approval, issuer: issuer}}
	var supplied, named unix.Stat_t
	fdFlags, flagErr := unix.FcntlInt(source.Fd(), unix.F_GETFD, 0)
	access, accessErr := unix.FcntlInt(source.Fd(), unix.F_GETFL, 0)
	if flagErr != nil || accessErr != nil || fdFlags&unix.FD_CLOEXEC == 0 || access&unix.O_ACCMODE != unix.O_RDONLY || access&unix.O_PATH != 0 ||
		unix.Fstat(int(source.Fd()), &supplied) != nil || unix.Fstat(int(namedFile.Fd()), &named) != nil || supplied.Dev != named.Dev || supplied.Ino != named.Ino {
		if namedFile.Close() != nil {
			return partial, ErrRetained
		}
		return nil, ErrUnsafe
	}
	if namedFile.Close() != nil {
		return partial, ErrRetained
	}
	file, err := duplicate(source)
	if err != nil {
		return nil, err
	}
	c := &ExecutableCapability{executableCapabilityState: &executableCapabilityState{file: file, approval: approval, issuer: issuer}}
	mountID, err := descriptorMountID(file)
	c.identity = Identity{Device: supplied.Dev, Inode: supplied.Ino, MountID: mountID}
	if err != nil || checkExecutableCapability(c) != nil {
		if file.Close() != nil {
			return c, ErrRetained
		}
		return nil, ErrUnsafe
	}
	return c, nil
}

func checkExecutableCapability(c *ExecutableCapability) error {
	if c == nil || c.executableCapabilityState == nil || c.file == nil || c.named != nil || c.issuer.kind != rootExecutableIssuerKind || c.identity.MountID == 0 {
		return ErrUnsafe
	}
	issuer, err := executableIssuerNow()
	if err != nil || issuer != c.issuer {
		return ErrUnsafe
	}
	return checkExecutableCapabilityFile(c.file, c.approval, c.identity)
}

func checkExecutableCapabilityFile(file *os.File, approval ApprovedExecutable, identity Identity) error {
	if file == nil {
		return ErrUnsafe
	}
	var stat unix.Stat_t
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
	access, accessErr := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	if err != nil || accessErr != nil || flags&unix.FD_CLOEXEC == 0 || access&unix.O_ACCMODE != unix.O_RDONLY || access&unix.O_PATH != 0 ||
		unix.Fstat(int(file.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Dev != identity.Device || stat.Ino != identity.Inode || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 || stat.Mode&07777 != 0555 {
		return ErrUnsafe
	}
	mountID, err := descriptorMountID(file)
	immutable, immutableErr := unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil || mountID != identity.MountID || immutableErr != nil || immutable&executableImmutableFlag == 0 {
		return ErrUnsafe
	}
	var header [20]byte
	if _, err = file.ReadAt(header[:], 0); err != nil || string(header[:4]) != "\x7fELF" || header[4] != 2 || header[5] != 1 || header[6] != 1 ||
		(header[16] != 2 && header[16] != 3) || header[17] != 0 || header[18] != 62 || header[19] != 0 {
		return ErrUnsafe
	}
	if _, err = unix.Fgetxattr(int(file.Fd()), "security.capability", nil); !errors.Is(err, unix.ENODATA) {
		return ErrUnsafe
	}
	return checkExecutable(ownedExecutable{approval: approval, file: file, device: identity.Device, inode: identity.Inode})
}

func duplicateExecutableCapability(c *ExecutableCapability) (*os.File, error) {
	file, err := duplicate(c.file)
	if err != nil {
		return nil, err
	}
	if err = checkExecutableCapabilityFile(file, c.approval, c.identity); err != nil {
		if file.Close() != nil {
			return file, ErrRetained
		}
		return nil, ErrUnsafe
	}
	return file, nil
}

func (c *ExecutableCapability) borrowExecutable(owner *Domain, approval ApprovedExecutable) (ownedExecutable, error) {
	if c == nil || c.executableCapabilityState == nil || owner == nil {
		return ownedExecutable{}, ErrUnsafe
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.approval != approval || checkExecutableCapability(c) != nil {
		return ownedExecutable{}, ErrUnsafe
	}
	slot := -1
	for index, borrowed := range c.borrows {
		if borrowed == nil {
			slot = index
			break
		}
	}
	if slot < 0 {
		return ownedExecutable{}, ErrCapacity
	}
	file, err := duplicateExecutableCapability(c)
	if file == nil {
		return ownedExecutable{}, err
	}
	c.borrows[slot] = owner
	return ownedExecutable{approval: approval, file: file, device: c.identity.Device, inode: c.identity.Inode,
		capability: c, borrowSlot: slot, borrowOwner: owner}, err
}

func checkCapabilityExecutable(tool ownedExecutable) error {
	c := tool.capability
	if c == nil {
		return ErrUnsafe
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if tool.borrowOwner == nil || tool.borrowSlot < 0 || tool.borrowSlot >= len(c.borrows) || c.borrows[tool.borrowSlot] != tool.borrowOwner ||
		c.approval != tool.approval || checkExecutableCapability(c) != nil {
		return ErrUnsafe
	}
	return checkExecutableCapabilityFile(tool.file, tool.approval, c.identity)
}

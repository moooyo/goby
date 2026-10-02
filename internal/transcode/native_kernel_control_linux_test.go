//go:build linux

package transcode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/moooyo/goby/internal/commanddomain"
	"golang.org/x/sys/unix"
)

const nativeKernelControlBytes = 16 << 10

// These records are bounded diagnostic intent in a genuinely reserved control
// filesystem. They cannot mint an executable, join, reader release or lease.
type nativeKernelControlRecord struct {
	Version    int
	Token      string
	Stage      string
	Data       fixedVolumeIdentity
	Control    fixedVolumeIdentity
	Parent     commanddomain.Identity
	ParentPath string
	Launcher   commanddomain.ApprovedExecutable
	Tool       commanddomain.ApprovedExecutable
	Case       string
	Domain     commanddomain.Snapshot
	Pipe       commanddomain.TemplatePipeSnapshot
	Completed  []string
}

type nativeKernelControl struct {
	root     *os.File
	file     *os.File
	identity fixedVolumeIdentity
	stat     unix.Stat_t
	closed   bool
	unknown  bool
}

func nativeKernelEncodeControl(record nativeKernelControlRecord) ([]byte, error) {
	if record.Version != 1 || !fixedVolumeHex(record.Token, 16) || record.Stage == "" || len(record.Stage) > 96 ||
		len(record.Case) > 32 || len(record.Completed) > 4 || len(record.ParentPath) > 4096 {
		return nil, errFixedVolumeUnsafe
	}
	encoded, err := json.Marshal(record)
	if err != nil || len(encoded) >= nativeKernelControlBytes {
		return nil, errors.Join(errFixedVolumeUnsafe, err)
	}
	buffer := bytes.Repeat([]byte{' '}, nativeKernelControlBytes)
	copy(buffer, encoded)
	buffer[len(encoded)] = '\n'
	return buffer, nil
}

func newNativeKernelControl(root *os.File, identity fixedVolumeIdentity) (*nativeKernelControl, error) {
	if root == nil || os.Getuid() != 0 || os.Geteuid() != 0 {
		return nil, errFixedVolumeUnsafe
	}
	if err := nativeKernelCheckWorkspace(root, identity, 0, 0); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(root.Fd()), "native-control.json", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	owner := &nativeKernelControl{root: root, identity: identity, file: os.NewFile(uintptr(fd), "owned-native-control")}
	// A non-nil error result remains the precise owner of a partial file. The
	// enclosing fixture retains both original whole reservations on failure.
	if err = unix.Fstat(fd, &owner.stat); err != nil {
		owner.unknown = true
		return owner, err
	}
	if err = owner.file.Truncate(nativeKernelControlBytes); err != nil {
		owner.unknown = true
		return owner, err
	}
	if err = owner.file.Sync(); err != nil {
		owner.unknown = true
		return owner, err
	}
	if err = root.Sync(); err != nil {
		owner.unknown = true
		return owner, err
	}
	return owner, nil
}

func (owner *nativeKernelControl) persist(record nativeKernelControlRecord) error {
	if owner == nil || owner.file == nil || owner.closed || owner.unknown {
		return errFixedVolumeRetained
	}
	if record.Control != owner.identity || record.Token != owner.identity.ProviderToken {
		return errFixedVolumeUnsafe
	}
	encoded, err := nativeKernelEncodeControl(record)
	if err != nil {
		return err
	}
	if err = nativeKernelCheckWorkspace(owner.root, owner.identity, 0, 0); err != nil {
		owner.unknown = true
		return err
	}
	var stat unix.Stat_t
	if unix.Fstat(int(owner.file.Fd()), &stat) != nil || stat.Dev != owner.stat.Dev || stat.Ino != owner.stat.Ino ||
		stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0o7777 != 0o600 || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Size != nativeKernelControlBytes {
		owner.unknown = true
		return errFixedVolumeRetained
	}
	written := 0
	for written < len(encoded) {
		n, writeErr := owner.file.WriteAt(encoded[written:], int64(written))
		if writeErr != nil || n <= 0 {
			owner.unknown = true
			return errors.Join(writeErr, errFixedVolumeRetained)
		}
		written += n
	}
	if err = owner.file.Sync(); err != nil {
		owner.unknown = true
		return err
	}
	return nil
}

func (owner *nativeKernelControl) closeAfterWriters() error {
	if owner == nil || owner.file == nil || owner.unknown {
		return errFixedVolumeRetained
	}
	if owner.closed {
		return nil
	}
	if err := owner.file.Close(); err != nil {
		owner.unknown = true
		return err
	}
	owner.closed = true
	return nil
}

func nativeKernelCheckWorkspace(file *os.File, identity fixedVolumeIdentity, uid, gid uint32) error {
	if file == nil || identity.RootInode != 2 || identity.MountID == 0 || identity.RootDevice == 0 {
		return errFixedVolumeUnsafe
	}
	var stat unix.Stat_t
	var fs unix.Statfs_t
	var statx unix.Statx_t
	if unix.Fstat(int(file.Fd()), &stat) != nil || stat.Dev != identity.RootDevice || stat.Ino != 2 ||
		stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0o7777 != 0o700 || stat.Uid != uid || stat.Gid != gid ||
		unix.Fstatfs(int(file.Fd()), &fs) != nil || fs.Type != unix.EXT4_SUPER_MAGIC || fs.Bsize != fixedVolumeBlockSize ||
		fs.Flags&unix.ST_RDONLY != 0 || fs.Blocks > math.MaxInt64/uint64(fixedVolumeBlockSize) || fs.Files > math.MaxInt64 ||
		int64(fs.Blocks)*fixedVolumeBlockSize != identity.FilesystemBytes || int64(fs.Files) != identity.FilesystemInodes ||
		unix.Statx(int(file.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID|unix.STATX_INO, &statx) != nil ||
		statx.Mask&unix.STATX_MNT_ID == 0 || statx.Mnt_id != identity.MountID || statx.Ino != 2 {
		return fmt.Errorf("%w: native fixture needs its exact fixed filesystem root", errFixedVolumeUnsafe)
	}
	boot, namespace, err := fixedVolumeKernelDomain()
	if err != nil || boot != identity.BootID || namespace.Dev != identity.MountNamespaceDevice || namespace.Ino != identity.MountNamespaceInode {
		return errFixedVolumeUnsafe
	}
	return nil
}

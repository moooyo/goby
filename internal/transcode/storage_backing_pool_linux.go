//go:build linux

package transcode

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

type fixedPoolStaticSnapshot struct {
	file           *os.File
	path           string
	stat           unix.Stat_t
	allocatedBytes int64
	digest         string
	immutable      bool
	covered        bool
	poolBacking    bool
	executable     *fixedPoolExecutableCapability
}

type pooledFixedVolumeProvider struct {
	mu                    sync.Mutex
	config                fixedBackingPoolConfig
	volumeConfig          fixedVolumeConfig
	root                  *os.File
	device                *os.File
	backing               *os.File
	lock                  *os.File
	primitive             *linuxFixedVolumeProvider
	statics               []fixedPoolStaticSnapshot
	lockStat              unix.Stat_t
	limits                storageReservationLimits
	budget                fixedBackingPoolBudget
	closed                bool
	executablePreparation *fixedPoolExecutablePreparation
	executableCreatorPID  int
	executableClosing     bool
	executables           []*fixedPoolExecutableCapability
	closeErr              error
}

// This factory adds a fixed guest-kernel allocation domain to the existing
// root primitive. It does not create devices, partitions, filesystems or mounts.
// A controlled loop source additionally needs an exact root-prepared, fully
// initialized host allocation map. The factory never prepares or detaches it.
// A verified partition may belong to a VM; lower host/controller allocation is
// outside this contract. An independent privileged owner must not resize, remap
// or repopulate this root-owned domain behind the broker's authority.
func openPooledFixedVolumeProvider(volumeConfig fixedVolumeConfig, config fixedBackingPoolConfig) (_ fixedVolumeProvider, err error) {
	return openPooledFixedVolumeProviderPrepared(volumeConfig, config, nil)
}

// prepare runs only in trusted root Go startup, after the concrete native pool
// identity and lock are established. No wire configuration can supply it.
func openPooledFixedVolumeProviderPrepared(volumeConfig fixedVolumeConfig, config fixedBackingPoolConfig, prepare func(*fixedPoolExecutablePreparation) error) (_ fixedVolumeProvider, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if !volumeConfig.Enabled || volumeConfig.Ledger == nil || config.CoveredMountpoint == nil ||
		len(config.OutsideStatic) > maxFixedBackingPoolStaticObjects || config.PoolRoot == "/" ||
		volumeConfig.ProvisionRoot != config.PoolRoot+"/volumes" || !fixedVolumeHex(volumeConfig.ExpectedRootToken, 16) {
		return nil, errFixedVolumeUnavailable
	}
	borrowedFlags, borrowedErr := unix.FcntlInt(config.CoveredMountpoint.Fd(), unix.F_GETFD, 0)
	if borrowedErr != nil || borrowedFlags&unix.FD_CLOEXEC == 0 {
		return nil, errFixedBackingPoolUnsafe
	}
	p := &pooledFixedVolumeProvider{config: config, volumeConfig: volumeConfig}
	defer func() {
		if err != nil {
			_ = p.Close()
		}
	}()
	if err = p.captureLedger(); err != nil {
		return nil, err
	}
	if p.root, err = fixedVolumeOpenAbsolute(config.PoolRoot, true); err != nil {
		return nil, err
	}
	if p.device, err = fixedVolumeOpenAbsolute(config.DevicePath, false); err != nil {
		return nil, err
	}
	if config.SourceKind == fixedBackingPoolInitializedLoop {
		if p.backing, err = fixedVolumeOpenAbsolute(config.BackingPath, false); err != nil {
			return nil, err
		}
	}
	if err = p.checkDomain(); err != nil {
		return nil, err
	}
	lock, err := fixedVolumeOpenAt(p.root, fixedBackingPoolLockName, unix.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	p.lock = lock
	if err = fixedVolumeOwnedRegular(lock, &p.lockStat); err != nil || p.lockStat.Size != 0 {
		return nil, errFixedBackingPoolUnsafe
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(errFixedVolumeUnavailable, err)
	}
	if err = p.checkPoolMarker(); err != nil {
		return nil, err
	}
	if err = p.checkPoolRootContents(); err != nil {
		return nil, err
	}
	if prepare != nil {
		p.executableCreatorPID = os.Getpid()
		preparation := &fixedPoolExecutablePreparation{issuer: p, active: true}
		p.executablePreparation = preparation
		prepareErr := func() error {
			defer func() { preparation.active = false }()
			return prepare(preparation)
		}()
		if prepareErr != nil {
			return nil, prepareErr
		}
	}
	if err = p.captureStatic(p.device, config.DevicePath, "", false, false); err != nil {
		return nil, err
	}
	if err = p.captureStatic(config.CoveredMountpoint, config.PoolRoot, "", false, true); err != nil {
		return nil, err
	}
	if p.backing != nil {
		if err = p.captureStatic(p.backing, config.BackingPath, "", false, false); err != nil {
			return nil, err
		}
		p.statics[len(p.statics)-1].poolBacking = true
	}
	if pathInsideFixedPool(config.PoolRoot, volumeConfig.Mke2fsPath) {
		return nil, errFixedBackingPoolUnsafe
	}
	formatter, err := fixedVolumeOpenAbsolute(volumeConfig.Mke2fsPath, false)
	if err != nil {
		return nil, err
	}
	formatterErr := p.captureStatic(formatter, volumeConfig.Mke2fsPath, volumeConfig.Mke2fsSHA256, true, false)
	formatterCloseErr := formatter.Close()
	if err = errors.Join(formatterErr, formatterCloseErr); err != nil {
		return nil, err
	}
	for _, object := range config.OutsideStatic {
		if pathInsideFixedPool(config.PoolRoot, object.Path) {
			return nil, errFixedBackingPoolUnsafe
		}
		var file *os.File
		var openErr error
		if executable := p.executable(object.Path); executable != nil {
			fd, duplicateErr := unix.FcntlInt(executable.file.Fd(), unix.F_DUPFD_CLOEXEC, 0)
			openErr = duplicateErr
			if openErr == nil {
				file = os.NewFile(uintptr(fd), "approved-executable-static-input")
			}
		} else {
			file, openErr = fixedVolumeOpenAbsolute(object.Path, false)
		}
		if openErr != nil {
			return nil, openErr
		}
		var stat unix.Stat_t
		statErr := unix.Fstat(int(file.Fd()), &stat)
		allocated, allocationErr := fixedPoolAllocatedBytes(stat)
		if statErr != nil || allocationErr != nil || uint64(stat.Dev) != object.Device || stat.Ino != object.Inode || allocated != object.AllocatedBytes {
			_ = file.Close()
			return nil, errFixedBackingPoolUnsafe
		}
		captureErr := p.captureStatic(file, object.Path, object.SHA256, true, false)
		closeErr := file.Close()
		if err = errors.Join(captureErr, closeErr); err != nil {
			return nil, err
		}
	}
	if err = p.checkStaticAndBudget(); err != nil {
		return nil, err
	}
	volumeConfig.poolPreparation = &fixedPoolPreparation{issuer: p}
	p.volumeConfig = volumeConfig
	// Only after the actual fixed device/mount, complete static budget and
	// prepared pool have been verified may the primitive open inside-pool files.
	primitive, err := openFixedVolumeProvider(volumeConfig)
	if err != nil {
		return nil, err
	}
	var ok bool
	p.primitive, ok = primitive.(*linuxFixedVolumeProvider)
	if !ok {
		_ = primitive.Close()
		return nil, errFixedBackingPoolUnsafe
	}
	if err = p.checkStaticAndBudget(); err != nil {
		return nil, err
	}
	if err = p.checkBaselineInodes(); err != nil {
		return nil, err
	}
	return p, nil
}

func (preparation *fixedPoolPreparation) validate(config fixedVolumeConfig) error {
	// Published callers already hold the issuer's mu, then primitive.mu. Do
	// not acquire issuer.mu again or invert Close's order. Factory startup is
	// single-threaded before publication. Neither primitive nor capability is
	// exposed to a client; native validation itself supplies the proof.
	if preparation == nil {
		return errFixedVolumeUnavailable
	}
	p, ok := preparation.issuer.(*pooledFixedVolumeProvider)
	if !ok || p == nil || p.closed || p.root == nil || p.device == nil || p.lock == nil || len(p.statics) < 3 ||
		config.Ledger != p.volumeConfig.Ledger || config.ProvisionRoot != p.config.PoolRoot+"/volumes" ||
		config.ExpectedRootToken != p.volumeConfig.ExpectedRootToken || config.Mke2fsPath != p.volumeConfig.Mke2fsPath ||
		config.Mke2fsSHA256 != p.volumeConfig.Mke2fsSHA256 {
		return errFixedVolumeUnavailable
	}
	if err := p.checkDomain(); err != nil {
		return err
	}
	if err := p.checkPoolMarker(); err != nil {
		return err
	}
	if err := p.checkPoolRootContents(); err != nil {
		return err
	}
	if err := p.checkPoolLock(); err != nil {
		return err
	}
	return p.checkStaticAndBudget()
}

func (p *pooledFixedVolumeProvider) checkPoolLock() error {
	if p.lock == nil {
		return errFixedBackingPoolUnsafe
	}
	var held unix.Stat_t
	if err := fixedVolumeOwnedRegular(p.lock, &held); err != nil || !fixedVolumeSameInode(held, p.lockStat) || held.Size != 0 {
		return errFixedBackingPoolUnsafe
	}
	lock, err := fixedVolumeOpenAt(p.root, fixedBackingPoolLockName, unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	var stat unix.Stat_t
	statErr := fixedVolumeOwnedRegular(lock, &stat)
	closeErr := lock.Close()
	if statErr != nil || closeErr != nil || !fixedVolumeSameInode(stat, p.lockStat) || stat.Size != 0 {
		return errFixedBackingPoolUnsafe
	}
	return unix.Flock(int(p.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

func (p *pooledFixedVolumeProvider) captureLedger() error {
	l := p.volumeConfig.Ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	j, ok := l.journal.(*fileStorageReservationJournal)
	if !ok || l.closed || l.failed != nil || j.path != p.config.PoolRoot+"/journal" ||
		j.device != p.config.Expected.RootDevice || j.inode != p.config.Expected.JournalInode {
		return errFixedBackingPoolUnsafe
	}
	p.limits = l.state.Limits
	return nil
}

func pathInsideFixedPool(root, path string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}

func (p *pooledFixedVolumeProvider) checkDomain() error {
	expected := p.config.Expected
	if expected.RootDevice == 0 || expected.RootInode != 2 || expected.MountID == 0 ||
		expected.DeviceNumber != expected.RootDevice || expected.DeviceNodeDevice == 0 || expected.DeviceNodeInode == 0 ||
		expected.DeviceBytes < 16<<20 || expected.DeviceBytes%fixedVolumeBlockSize != 0 ||
		expected.JournalInode == 0 || expected.VolumesInode == 0 ||
		!fixedVolumeValidUUID(expected.FilesystemUUID) || !fixedVolumeValidUUID(expected.BootID) {
		return errFixedBackingPoolUnsafe
	}
	boot, mountNS, err := fixedVolumeKernelDomain()
	if err != nil || boot != expected.BootID || uint64(mountNS.Dev) != expected.MountNamespaceDevice || mountNS.Ino != expected.MountNamespaceInode {
		return errFixedBackingPoolUnsafe
	}
	var root, device unix.Stat_t
	if err = unix.Fstat(int(p.root.Fd()), &root); err != nil || root.Uid != 0 || root.Mode&0o777 != 0o700 ||
		uint64(root.Dev) != expected.RootDevice || root.Ino != expected.RootInode {
		return errFixedBackingPoolUnsafe
	}
	if err = unix.Fstat(int(p.device.Fd()), &device); err != nil || device.Uid != 0 || device.Mode&unix.S_IFMT != unix.S_IFBLK ||
		device.Mode&0o077 != 0 || device.Nlink != 1 || uint64(device.Dev) != expected.DeviceNodeDevice ||
		device.Ino != expected.DeviceNodeInode || device.Rdev != expected.DeviceNumber {
		return errFixedBackingPoolUnsafe
	}
	if err = fixedPoolCheckPath(p.config.PoolRoot, p.root, true); err != nil {
		return err
	}
	if err = fixedPoolCheckPath(p.config.DevicePath, p.device, false); err != nil {
		return err
	}
	var bytes uint64
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, p.device.Fd(), uintptr(unix.BLKGETSIZE64), uintptr(unsafe.Pointer(&bytes)))
	if errno != 0 || bytes != uint64(expected.DeviceBytes) {
		return errFixedBackingPoolUnsafe
	}
	switch p.config.SourceKind {
	case fixedBackingPoolPartition:
		if expected.PartitionNumber == 0 || expected.PartitionSectors > math.MaxInt64/512 || int64(expected.PartitionSectors)*512 != expected.DeviceBytes {
			return errFixedBackingPoolUnsafe
		}
		if err = fixedPoolCheckPartition(expected); err != nil {
			return err
		}
	case fixedBackingPoolInitializedLoop:
		if err = p.checkLoopSource(); err != nil {
			return err
		}
	default:
		return errFixedVolumeUnavailable
	}
	superblock, err := fixedVolumeReadSuperblock(p.device)
	if err != nil || superblock.Bytes != expected.DeviceBytes || superblock.UUID != expected.FilesystemUUID ||
		superblock.Inodes != expected.FilesystemInodes || superblock.BlockSize != fixedVolumeBlockSize {
		return errFixedBackingPoolUnsafe
	}
	var fs unix.Statfs_t
	if err = unix.Fstatfs(int(p.root.Fd()), &fs); err != nil || fs.Type != unix.EXT4_SUPER_MAGIC || fs.Bsize != fixedVolumeBlockSize ||
		fs.Blocks > math.MaxInt64/uint64(fixedVolumeBlockSize) || int64(fs.Blocks)*fixedVolumeBlockSize != expected.FilesystemBytes ||
		fs.Files > math.MaxInt64 || int64(fs.Files) != expected.FilesystemInodes ||
		fs.Flags&(unix.ST_NODEV|unix.ST_NOSUID|unix.ST_NOEXEC) != unix.ST_NODEV|unix.ST_NOSUID|unix.ST_NOEXEC || fs.Flags&unix.ST_RDONLY != 0 {
		return errFixedBackingPoolUnsafe
	}
	var statx unix.Statx_t
	if err = unix.Statx(int(p.root.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID|unix.STATX_INO, &statx); err != nil ||
		statx.Mask&unix.STATX_MNT_ID == 0 || statx.Mnt_id != expected.MountID || statx.Ino != expected.RootInode {
		return errFixedBackingPoolUnsafe
	}
	if err = fixedPoolCheckMountInfo(p.config.PoolRoot, expected); err != nil {
		return err
	}
	for name, inode := range map[string]uint64{"journal": expected.JournalInode, "volumes": expected.VolumesInode} {
		child, openErr := fixedVolumeOpenOwnedDirectory(p.root, name, expected.RootDevice, inode)
		if openErr != nil {
			return openErr
		}
		if err = child.Close(); err != nil {
			return err
		}
	}
	return nil
}

func fixedPoolCheckPath(path string, file *os.File, directory bool) error {
	current, err := fixedVolumeOpenAbsolute(path, directory)
	if err != nil {
		return err
	}
	defer current.Close()
	var want, observed unix.Stat_t
	if unix.Fstat(int(file.Fd()), &want) != nil || unix.Fstat(int(current.Fd()), &observed) != nil || !fixedVolumeSameInode(want, observed) {
		return errFixedBackingPoolUnsafe
	}
	return nil
}

func fixedPoolReadSysfsNumber(path string) (uint64, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	var stat unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(int(file.Fd()), &stat) != nil || unix.Fstatfs(int(file.Fd()), &fs) != nil ||
		stat.Uid != 0 || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o222 != 0 || fs.Type != unix.SYSFS_MAGIC {
		return 0, errFixedBackingPoolUnsafe
	}
	content, err := io.ReadAll(io.LimitReader(file, 128))
	if err != nil {
		return 0, err
	}
	return fixedPoolParseSysfsNumber(content)
}

func fixedPoolParseSysfsNumber(content []byte) (uint64, error) {
	if len(content) < 2 || len(content) > 32 || content[len(content)-1] != '\n' {
		return 0, errFixedBackingPoolUnsafe
	}
	value := string(content[:len(content)-1])
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, errFixedBackingPoolUnsafe
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, errFixedBackingPoolUnsafe
	}
	return parsed, nil
}

func fixedPoolCheckPartition(expected fixedBackingPoolIdentity) error {
	base := "/sys/dev/block/" + strconv.FormatUint(uint64(unix.Major(expected.DeviceNumber)), 10) + ":" + strconv.FormatUint(uint64(unix.Minor(expected.DeviceNumber)), 10)
	resolved, err := filepath.EvalSymlinks(base)
	if err != nil || !strings.HasPrefix(resolved, "/sys/devices/") || strings.Contains(resolved, "/virtual/block/") {
		return errFixedVolumeUnavailable
	}
	for suffix, want := range map[string]uint64{"partition": expected.PartitionNumber, "start": expected.PartitionStartSector, "size": expected.PartitionSectors} {
		value, readErr := fixedPoolReadSysfsNumber(base + "/" + suffix)
		if readErr != nil || value != want {
			return errFixedBackingPoolUnsafe
		}
	}
	// A direct partition has one parent block device and no stacked slaves.
	// This is a guest-visible mapping check, not a host hardware assertion.
	parent := filepath.Dir(resolved)
	names, err := os.ReadDir(parent + "/slaves")
	if err != nil || len(names) != 0 {
		return errFixedVolumeUnavailable
	}
	parentSectors, err := fixedPoolReadSysfsNumber(parent + "/size")
	end, ok := checkedFixedPoolSectorEnd(expected.PartitionStartSector, expected.PartitionSectors)
	if err != nil || !ok || end > parentSectors {
		return errFixedBackingPoolUnsafe
	}
	return nil
}

func checkedFixedPoolSectorEnd(start, count uint64) (uint64, bool) {
	if count == 0 || start > math.MaxUint64-count {
		return 0, false
	}
	return start + count, true
}

func (p *pooledFixedVolumeProvider) checkLoopSource() error {
	i := p.config.Expected
	if p.backing == nil || pathInsideFixedPool(p.config.PoolRoot, p.config.BackingPath) ||
		i.BackingDevice == 0 || i.BackingInode == 0 || i.BackingBytes != i.DeviceBytes ||
		i.BackingAllocatedBytes < i.BackingBytes || !fixedVolumeHex(i.BackingMapSHA256, sha256.Size) ||
		unix.Major(i.DeviceNumber) != 7 || unix.Minor(i.DeviceNumber) != i.LoopNumber {
		return errFixedBackingPoolUnsafe
	}
	var stat unix.Stat_t
	if err := fixedVolumeOwnedRegular(p.backing, &stat); err != nil {
		return err
	}
	allocated, err := fixedPoolAllocatedBytes(stat)
	if err != nil || uint64(stat.Dev) != i.BackingDevice || stat.Ino != i.BackingInode || stat.Size != i.BackingBytes || allocated != i.BackingAllocatedBytes {
		return errFixedBackingPoolUnsafe
	}
	if err = fixedPoolCheckPath(p.config.BackingPath, p.backing, false); err != nil {
		return err
	}
	var fs unix.Statfs_t
	if unix.Fstatfs(int(p.backing.Fd()), &fs) != nil || fs.Type != unix.EXT4_SUPER_MAGIC || fs.Bsize != fixedVolumeBlockSize || fs.Flags&unix.ST_RDONLY != 0 {
		return errFixedVolumeUnavailable
	}
	if err = fixedPoolCheckDirectBackingDevice(i.BackingDevice); err != nil {
		return err
	}
	status, err := unix.IoctlLoopGetStatus64(int(p.device.Fd()))
	if err != nil || status.Device != i.BackingDevice || status.Inode != i.BackingInode || status.Number != i.LoopNumber ||
		status.Offset != 0 || status.Sizelimit != uint64(i.DeviceBytes) || status.Flags != 0 || status.Encrypt_type != 0 || status.Encrypt_key_size != 0 {
		return errFixedBackingPoolUnsafe
	}
	mapDigest, err := fixedPoolInitializedMap(p.backing, i.BackingBytes)
	if err != nil || mapDigest != i.BackingMapSHA256 {
		return errFixedBackingPoolUnsafe
	}
	return nil
}

func fixedPoolCheckDirectBackingDevice(device uint64) error {
	base := "/sys/dev/block/" + strconv.FormatUint(uint64(unix.Major(device)), 10) + ":" + strconv.FormatUint(uint64(unix.Minor(device)), 10)
	resolved, err := filepath.EvalSymlinks(base)
	if err != nil || !strings.HasPrefix(resolved, "/sys/devices/") || strings.Contains(resolved, "/virtual/block/") {
		return errFixedVolumeUnavailable
	}
	var stat unix.Stat_t
	if err = unix.Stat(base+"/partition", &stat); err == nil {
		resolved = filepath.Dir(resolved)
	} else if !errors.Is(err, unix.ENOENT) {
		return errFixedVolumeUnavailable
	}
	names, err := os.ReadDir(resolved + "/slaves")
	if err != nil || len(names) != 0 {
		return errFixedVolumeUnavailable
	}
	return nil
}

const (
	fixedPoolFiemapIOCTL = 0xc020660b
	fixedPoolFiemapSync  = 1
	fixedPoolFiemapLast  = 1
	maxFixedPoolExtents  = 4096
)

// These layouts are the Linux fiemap UAPI: a 32-byte header followed by
// 56-byte extents. No path, shell command or user-supplied ioctl is involved.
type fixedPoolFiemapExtent struct {
	Logical    uint64
	Physical   uint64
	Length     uint64
	Reserved64 [2]uint64
	Flags      uint32
	Reserved   [3]uint32
}

type fixedPoolFiemapRequest struct {
	Start    uint64
	Length   uint64
	Flags    uint32
	Mapped   uint32
	Count    uint32
	Reserved uint32
	Extents  [maxFixedPoolExtents]fixedPoolFiemapExtent
}

func fixedPoolInitializedMap(file *os.File, bytes int64) (string, error) {
	if bytes <= 0 || bytes%fixedVolumeBlockSize != 0 || unsafe.Offsetof(fixedPoolFiemapRequest{}.Extents) != 32 ||
		unsafe.Sizeof(fixedPoolFiemapExtent{}) != 56 {
		return "", errFixedBackingPoolUnsafe
	}
	request := &fixedPoolFiemapRequest{Length: uint64(bytes), Flags: fixedPoolFiemapSync, Count: maxFixedPoolExtents}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, file.Fd(), uintptr(fixedPoolFiemapIOCTL), uintptr(unsafe.Pointer(request)))
	if errno != 0 || request.Mapped == 0 || request.Mapped > maxFixedPoolExtents || request.Start != 0 ||
		request.Length != uint64(bytes) || request.Count != maxFixedPoolExtents || request.Reserved != 0 || request.Flags != fixedPoolFiemapSync {
		return "", errFixedBackingPoolUnsafe
	}
	return fixedPoolMapDigest(request.Extents[:request.Mapped], uint64(bytes))
}

func fixedPoolMapDigest(extents []fixedPoolFiemapExtent, bytes uint64) (string, error) {
	if len(extents) == 0 || len(extents) > maxFixedPoolExtents || bytes == 0 || bytes%uint64(fixedVolumeBlockSize) != 0 {
		return "", errFixedBackingPoolUnsafe
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("goby-fixed-backing-fiemap-v1\n"))
	var header [12]byte
	binary.LittleEndian.PutUint64(header[:8], bytes)
	binary.LittleEndian.PutUint32(header[8:], uint32(len(extents)))
	_, _ = hash.Write(header[:])
	var covered uint64
	for index, extent := range extents {
		// Only LAST is accepted. UNKNOWN, DELALLOC, ENCODED, encrypted, inline,
		// tail-packed, UNWRITTEN, MERGED and SHARED mappings are all rejected.
		if extent.Logical != covered || extent.Physical == 0 || extent.Length == 0 || extent.Length%uint64(fixedVolumeBlockSize) != 0 ||
			extent.Logical%uint64(fixedVolumeBlockSize) != 0 || extent.Physical%uint64(fixedVolumeBlockSize) != 0 ||
			extent.Flags&^uint32(fixedPoolFiemapLast) != 0 || extent.Reserved64 != ([2]uint64{}) || extent.Reserved != ([3]uint32{}) ||
			extent.Length > bytes-covered || extent.Physical > math.MaxUint64-extent.Length ||
			(index == len(extents)-1) != (extent.Flags&fixedPoolFiemapLast != 0) {
			return "", errFixedBackingPoolUnsafe
		}
		for _, earlier := range extents[:index] {
			if extent.Physical < earlier.Physical+earlier.Length && earlier.Physical < extent.Physical+extent.Length {
				return "", errFixedBackingPoolUnsafe
			}
		}
		covered += extent.Length
		var encoded [28]byte
		binary.LittleEndian.PutUint64(encoded[:8], extent.Logical)
		binary.LittleEndian.PutUint64(encoded[8:16], extent.Physical)
		binary.LittleEndian.PutUint64(encoded[16:24], extent.Length)
		binary.LittleEndian.PutUint32(encoded[24:], extent.Flags)
		_, _ = hash.Write(encoded[:])
	}
	if covered != bytes {
		return "", errFixedBackingPoolUnsafe
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func fixedPoolCheckMountInfo(root string, expected fixedBackingPoolIdentity) error {
	file, err := os.Open("/proc/thread-self/mountinfo")
	if err != nil {
		return err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || len(content) > 1<<20 {
		return errFixedBackingPoolUnsafe
	}
	return fixedPoolParseMountInfo(content, root, expected)
}

func fixedPoolParseMountInfo(content []byte, root string, expected fixedBackingPoolIdentity) error {
	matches := 0
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		mountID, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil || mountID != expected.MountID {
			continue
		}
		matches++
		majorMinor := strconv.FormatUint(uint64(unix.Major(expected.DeviceNumber)), 10) + ":" + strconv.FormatUint(uint64(unix.Minor(expected.DeviceNumber)), 10)
		separator := -1
		for index := 6; index < len(fields); index++ {
			if fields[index] == "-" {
				separator = index
				break
			}
		}
		if fields[2] != majorMinor || fields[3] != "/" || fields[4] != root || separator < 0 || separator+3 >= len(fields) || fields[separator+1] != "ext4" {
			return errFixedBackingPoolUnsafe
		}
		if !fixedPoolMountOption(fields[5], "nodev") || !fixedPoolMountOption(fields[5], "nosuid") || !fixedPoolMountOption(fields[5], "noexec") ||
			fixedPoolHasDiscard(fields[5]) || fixedPoolHasDiscard(fields[separator+3]) {
			return errFixedBackingPoolUnsafe
		}
	}
	if matches != 1 {
		return errFixedBackingPoolUnsafe
	}
	return nil
}

func fixedPoolHasDiscard(options string) bool {
	for _, option := range strings.Split(options, ",") {
		if option == "discard" || strings.HasPrefix(option, "discard=") {
			return true
		}
	}
	return false
}

func fixedPoolMountOption(options, want string) bool {
	for _, option := range strings.Split(options, ",") {
		if option == want {
			return true
		}
	}
	return false
}

func (p *pooledFixedVolumeProvider) checkPoolMarker() error {
	marker, err := fixedVolumeOpenAt(p.root, fixedBackingPoolMarkerName, unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer marker.Close()
	var stat unix.Stat_t
	if err = fixedVolumeOwnedRegular(marker, &stat); err != nil {
		return err
	}
	want := fixedBackingPoolMarker(p.volumeConfig.ExpectedRootToken, p.config.Expected, p.config.SourceKind)
	content, err := io.ReadAll(io.LimitReader(marker, int64(len(want)+1)))
	if err != nil || string(content) != want {
		return errFixedBackingPoolUnsafe
	}
	return nil
}

func fixedBackingPoolMarker(token string, identity fixedBackingPoolIdentity, kind fixedBackingPoolSourceKind) string {
	source := "guest-kernel-partition"
	if kind == fixedBackingPoolInitializedLoop {
		source = "guest-kernel-initialized-loop"
	}
	marker := fixedBackingPoolVersion + token + "\n" + source + "\n" +
		strconv.FormatUint(identity.DeviceNumber, 10) + ":" + strconv.FormatInt(identity.DeviceBytes, 10) + ":" + strconv.FormatInt(identity.FilesystemInodes, 10) + "\n" +
		identity.FilesystemUUID + "\n" + strconv.FormatUint(identity.JournalInode, 10) + ":" + strconv.FormatUint(identity.VolumesInode, 10) + "\n"
	if kind == fixedBackingPoolInitializedLoop {
		marker += strconv.FormatUint(identity.BackingDevice, 10) + ":" + strconv.FormatUint(identity.BackingInode, 10) + ":" +
			strconv.FormatInt(identity.BackingBytes, 10) + ":" + strconv.FormatInt(identity.BackingAllocatedBytes, 10) + "\n" + identity.BackingMapSHA256 + "\n"
	}
	return marker
}

func (p *pooledFixedVolumeProvider) checkPoolRootContents() error {
	names, err := fixedVolumeDirectoryNames(p.root, 5)
	if err != nil {
		return err
	}
	for _, name := range names {
		switch name {
		case "journal", "volumes", fixedBackingPoolMarkerName, fixedBackingPoolLockName:
		case "lost+found":
			file, openErr := fixedVolumeOpenAt(p.root, name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
			if openErr != nil {
				return openErr
			}
			var stat unix.Stat_t
			statErr := unix.Fstat(int(file.Fd()), &stat)
			emptyErr := fixedVolumeAssertEmpty(file)
			closeErr := file.Close()
			if statErr != nil || uint64(stat.Dev) != p.config.Expected.RootDevice || stat.Uid != 0 || stat.Mode&0o777 != 0o700 ||
				emptyErr != nil || closeErr != nil {
				return errFixedBackingPoolUnsafe
			}
		default:
			return errFixedBackingPoolUnsafe
		}
	}
	return nil
}

func fixedPoolAllocatedBytes(stat unix.Stat_t) (int64, error) {
	if stat.Blocks < 0 || stat.Blocks > math.MaxInt64/512 {
		return 0, errFixedBackingPoolUnsafe
	}
	return stat.Blocks * 512, nil
}

func (p *pooledFixedVolumeProvider) captureStatic(source *os.File, path, digest string, immutable, covered bool) error {
	fd, err := unix.FcntlInt(source.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "owned-pool-static")
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return err
	}
	allocated, err := fixedPoolAllocatedBytes(stat)
	if err != nil {
		_ = file.Close()
		return err
	}
	if immutable && stat.Mode&unix.S_IFMT == unix.S_IFREG && digest == "" {
		if stat.Size < 0 || stat.Size > 128<<20 {
			_ = file.Close()
			return errFixedBackingPoolUnsafe
		}
		hash := sha256.New()
		if _, err = io.Copy(hash, io.NewSectionReader(file, 0, stat.Size)); err != nil {
			_ = file.Close()
			return err
		}
		digest = hex.EncodeToString(hash.Sum(nil))
	}
	snapshot := fixedPoolStaticSnapshot{file: file, path: path, stat: stat, allocatedBytes: allocated, digest: digest,
		immutable: immutable, covered: covered, executable: p.executable(path)}
	if err = p.checkStatic(snapshot); err != nil {
		_ = file.Close()
		return err
	}
	for _, previous := range p.statics {
		if fixedVolumeSameInode(previous.stat, stat) {
			if previous.path != path {
				_ = file.Close()
				return errFixedBackingPoolUnsafe
			}
			return file.Close()
		}
	}
	p.statics = append(p.statics, snapshot)
	return nil
}

func (p *pooledFixedVolumeProvider) checkStatic(snapshot fixedPoolStaticSnapshot) error {
	var stat unix.Stat_t
	if unix.Fstat(int(snapshot.file.Fd()), &stat) != nil || !fixedVolumeSameInode(snapshot.stat, stat) || stat.Uid != 0 || stat.Mode != snapshot.stat.Mode ||
		stat.Nlink != snapshot.stat.Nlink || stat.Size != snapshot.stat.Size || stat.Rdev != snapshot.stat.Rdev {
		return errFixedBackingPoolUnsafe
	}
	allocated, err := fixedPoolAllocatedBytes(stat)
	if err != nil || allocated != snapshot.allocatedBytes {
		return errFixedBackingPoolUnsafe
	}
	if snapshot.covered {
		identity := p.config.Expected
		borrowedFlags, flagErr := unix.FcntlInt(p.config.CoveredMountpoint.Fd(), unix.F_GETFD, 0)
		var borrowed unix.Stat_t
		if flagErr != nil || borrowedFlags&unix.FD_CLOEXEC == 0 || unix.Fstat(int(p.config.CoveredMountpoint.Fd()), &borrowed) != nil ||
			!fixedVolumeSameInode(stat, borrowed) {
			return errFixedBackingPoolUnsafe
		}
		path, linkErr := os.Readlink(fixedVolumeFDPath(snapshot.file))
		if linkErr != nil || path != p.config.PoolRoot || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0o777 != 0o700 ||
			uint64(stat.Dev) != identity.CoveredMountpointDevice || stat.Ino != identity.CoveredMountpointInode ||
			allocated != identity.CoveredMountpointAllocatedBytes || uint64(stat.Dev) == identity.RootDevice {
			return errFixedBackingPoolUnsafe
		}
		return fixedVolumeAssertEmpty(snapshot.file)
	}
	if err = fixedPoolCheckPath(snapshot.path, snapshot.file, stat.Mode&unix.S_IFMT == unix.S_IFDIR); err != nil {
		return err
	}
	if snapshot.immutable {
		if stat.Mode&unix.S_IFMT != unix.S_IFREG && stat.Mode&unix.S_IFMT != unix.S_IFDIR {
			return errFixedBackingPoolUnsafe
		}
		flags, flagErr := unix.IoctlGetInt(int(snapshot.file.Fd()), unix.FS_IOC_GETFLAGS)
		if flagErr != nil || flags&fixedVolumeImmutableFlag == 0 {
			return errFixedBackingPoolUnsafe
		}
		if snapshot.executable != nil {
			if snapshot.executable.check(p, snapshot.file) != nil {
				return errFixedBackingPoolUnsafe
			}
		} else if stat.Mode&0o077 != 0 {
			return errFixedBackingPoolUnsafe
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFREG {
			if stat.Nlink != 1 || stat.Mode&0o222 != 0 || !fixedVolumeHex(snapshot.digest, sha256.Size) || stat.Size > 128<<20 || stat.Size < 0 {
				return errFixedBackingPoolUnsafe
			}
			hash := sha256.New()
			if _, err = io.Copy(hash, io.NewSectionReader(snapshot.file, 0, stat.Size)); err != nil || hex.EncodeToString(hash.Sum(nil)) != snapshot.digest {
				return errFixedBackingPoolUnsafe
			}
		}
	}
	return nil
}

func (p *pooledFixedVolumeProvider) checkStaticAndBudget() error {
	var bytes, inodes int64
	for _, snapshot := range p.statics {
		if err := p.checkStatic(snapshot); err != nil {
			return err
		}
		var ok bool
		if !snapshot.poolBacking {
			bytes, ok = checkedFixedPoolAdd(bytes, snapshot.allocatedBytes)
			if !ok {
				return errFixedBackingPoolUnsafe
			}
		}
		inodes++
	}
	if err := p.checkStaticDirectoryContents(); err != nil {
		return err
	}
	budget, err := fixedBackingPoolCapacity(p.config, p.limits, bytes, inodes)
	if err != nil {
		return err
	}
	p.budget = budget
	return nil
}

func (p *pooledFixedVolumeProvider) checkStaticDirectoryContents() error {
	listed := make(map[string]bool, len(p.statics))
	for _, snapshot := range p.statics {
		listed[snapshot.path] = true
	}
	for _, snapshot := range p.statics {
		if snapshot.covered || snapshot.stat.Mode&unix.S_IFMT != unix.S_IFDIR {
			continue
		}
		names, err := fixedVolumeDirectoryNames(snapshot.file, maxFixedBackingPoolStaticObjects+3)
		if err != nil {
			return err
		}
		for _, name := range names {
			if !listed[filepath.Join(snapshot.path, name)] {
				return errFixedBackingPoolUnsafe
			}
		}
	}
	return nil
}

// Reserved ext4 inodes, lost+found and the complete prepared control inventory
// belong to BaseInodes. Existing lease resources are counted separately. This
// check is accounting, not a promise that a fragmented pool can admit each job.
func (p *pooledFixedVolumeProvider) checkBaselineInodes() error {
	j := p.primitive.journal
	j.mu.Lock()
	journalErr := j.validateRoot()
	var fs unix.Statfs_t
	statErr := unix.Fstatfs(int(p.root.Fd()), &fs)
	j.mu.Unlock()
	if journalErr != nil || statErr != nil || fs.Ffree > fs.Files || fs.Files-fs.Ffree > math.MaxInt64 {
		return errFixedBackingPoolUnsafe
	}
	names, err := fixedVolumeDirectoryNames(p.primitive.root, 2*maxStorageLeases+2)
	if err != nil {
		return err
	}
	var dynamic int64
	for _, name := range names {
		if name == fixedVolumeMarkerName || name == fixedVolumeLockName {
			continue
		}
		if strings.HasSuffix(name, ".next") {
			return errFixedVolumeRetained
		}
		base := strings.TrimSuffix(name, ".json")
		receipt, _, readErr := p.primitive.readReceipt(base)
		if readErr != nil {
			return readErr
		}
		if readErr = p.primitive.checkReceiptLedgerBinding(receipt); readErr != nil {
			return readErr
		}
		dynamic++
		if strings.HasSuffix(name, ".json") {
			continue
		}
		namespace, openErr := fixedVolumeOpenOwnedDirectory(p.primitive.root, name, receipt.Identity.NamespaceDevice, receipt.Identity.NamespaceInode)
		if openErr != nil {
			return openErr
		}
		children, readErr := fixedVolumeDirectoryNames(namespace, 2)
		if readErr == nil {
			readErr = fixedVolumeAssertNamespaceContents(namespace, receipt)
		}
		closeErr := namespace.Close()
		if err = errors.Join(readErr, closeErr); err != nil {
			return err
		}
		dynamic += int64(len(children))
	}
	used := int64(fs.Files - fs.Ffree)
	if dynamic > used || used-dynamic+1 > p.limits.BaseInodes {
		return errFixedBackingPoolUnsafe
	}
	return nil
}

func (p *pooledFixedVolumeProvider) checkLocked() error {
	if p.closed {
		return os.ErrClosed
	}
	if err := p.checkDomain(); err != nil {
		return err
	}
	if err := p.checkPoolMarker(); err != nil {
		return err
	}
	if err := p.checkPoolRootContents(); err != nil {
		return err
	}
	if err := p.checkPoolLock(); err != nil {
		return err
	}
	if err := p.checkStaticAndBudget(); err != nil {
		return err
	}
	return p.checkBaselineInodes()
}

func (p *pooledFixedVolumeProvider) fenceAdmission() {
	l := p.volumeConfig.Ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recovering = true
}

func (p *pooledFixedVolumeProvider) Provision(ctx context.Context, permit *storageProvisionPermit) (fixedVolumeIdentity, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.executableClosing {
		p.fenceAdmission()
		return fixedVolumeIdentity{}, errFixedExecutableRetained
	}
	if err := p.checkLocked(); err != nil {
		p.fenceAdmission()
		return fixedVolumeIdentity{}, err
	}
	identity, err := p.primitive.Provision(ctx, permit)
	if err != nil {
		p.fenceAdmission()
	}
	return identity, err
}

func (p *pooledFixedVolumeProvider) Retire(ctx context.Context, permit *storageRetirePermit) (*storageRetirementReceipt, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkLocked(); err != nil {
		p.fenceAdmission()
		return nil, err
	}
	return p.primitive.Retire(ctx, permit)
}

func (p *pooledFixedVolumeProvider) AcknowledgeRetirement(ctx context.Context, receipt *storageRetirementReceipt) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkLocked(); err != nil {
		p.fenceAdmission()
		return err
	}
	return p.primitive.AcknowledgeRetirement(ctx, receipt)
}

func (p *pooledFixedVolumeProvider) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return p.closeErr
	}
	p.executableClosing = true
	for _, capability := range p.executables {
		for _, use := range capability.uses {
			if use != nil {
				p.fenceAdmission()
				return errFixedExecutableRetained
			}
		}
	}
	p.closed = true
	var err error
	if p.primitive != nil {
		err = p.primitive.Close()
	}
	for _, object := range p.statics {
		err = errors.Join(err, object.file.Close())
	}
	for _, capability := range p.executables {
		err = errors.Join(err, capability.closeOwned())
	}
	for _, file := range []*os.File{p.lock, p.backing, p.device, p.root} {
		if file != nil {
			err = errors.Join(err, file.Close())
		}
	}
	p.closeErr = err
	return err
}

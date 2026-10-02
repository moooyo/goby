//go:build linux

package transcode

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"golang.org/x/sys/unix"
)

// This native file check is independent of capacity/preparation authority.
// File-helper tests may exercise it without minting a pool or executable cap.
func fixedExecutableFile(file *os.File, digest string) (unix.Stat_t, int64, error) {
	var stat unix.Stat_t
	if file == nil || !fixedVolumeHex(digest, sha256.Size) || unix.Fstat(int(file.Fd()), &stat) != nil ||
		stat.Uid != 0 || stat.Gid != 0 || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7777 != 0o555 ||
		stat.Nlink != 1 || stat.Size < 4 || stat.Size > 128<<20 {
		return stat, 0, errFixedBackingPoolUnsafe
	}
	access, accessErr := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	flags, flagsErr := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
	immutable, immutableErr := unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS)
	_, capabilityErr := unix.Fgetxattr(int(file.Fd()), "security.capability", nil)
	if accessErr != nil || access&unix.O_ACCMODE != unix.O_RDONLY || access&unix.O_PATH != 0 || flagsErr != nil || flags&unix.FD_CLOEXEC == 0 ||
		immutableErr != nil || immutable&fixedVolumeImmutableFlag == 0 ||
		(!errors.Is(capabilityErr, unix.ENODATA) && !errors.Is(capabilityErr, unix.EOPNOTSUPP)) {
		return stat, 0, errFixedBackingPoolUnsafe
	}
	var magic [4]byte
	if _, err := file.ReadAt(magic[:], 0); err != nil || magic != [4]byte{0x7f, 'E', 'L', 'F'} {
		return stat, 0, errFixedBackingPoolUnsafe
	}
	image, imageErr := elf.NewFile(file)
	if imageErr != nil || image.Version != elf.EV_CURRENT || image.Type != elf.ET_EXEC && image.Type != elf.ET_DYN {
		return stat, 0, errFixedBackingPoolUnsafe
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.NewSectionReader(file, 0, stat.Size)); err != nil || hex.EncodeToString(hash.Sum(nil)) != digest {
		return stat, 0, errFixedBackingPoolUnsafe
	}
	allocated, err := fixedPoolAllocatedBytes(stat)
	return stat, allocated, err
}

func (preparation *fixedPoolExecutablePreparation) approve(source *os.File, path, digest string) (*fixedPoolExecutableCapability, error) {
	if preparation == nil || !preparation.active || os.Getuid() != 0 || os.Geteuid() != 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errFixedBackingPoolUnsafe
	}
	p, ok := preparation.issuer.(*pooledFixedVolumeProvider)
	if !ok || p == nil || p.closed || p.executableClosing || p.executablePreparation != preparation ||
		p.executableCreatorPID != os.Getpid() || pathInsideFixedPool(p.config.PoolRoot, path) || len(p.executables) >= maxFixedBackingPoolStaticObjects {
		return nil, errFixedBackingPoolUnsafe
	}
	if err := p.checkDomain(); err != nil {
		return nil, err
	}
	if err := p.checkPoolMarker(); err != nil {
		return nil, err
	}
	if err := p.checkPoolLock(); err != nil {
		return nil, err
	}
	stat, allocated, err := fixedExecutableFile(source, digest)
	if err != nil {
		return nil, fmt.Errorf("approved executable descriptor: %w", err)
	}
	if !fixedExecutableListedIdentity(p.config.OutsideStatic, path, digest, stat, allocated) || p.executable(path) != nil {
		return nil, fmt.Errorf("approved executable inventory requires its explicit digest and actual allocation: %w", errFixedBackingPoolUnsafe)
	}
	if err = fixedPoolCheckPath(path, source, false); err != nil {
		return nil, fmt.Errorf("approved executable named inode: %w", err)
	}
	fd, err := unix.FcntlInt(source.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "approved-pool-executable")
	capability := &fixedPoolExecutableCapability{issuer: p, file: file, path: path, digest: digest,
		device: uint64(stat.Dev), inode: stat.Ino, allocated: allocated, creatorPID: os.Getpid(),
		bootID: p.config.Expected.BootID, namespaceDev: p.config.Expected.MountNamespaceDevice,
		namespaceIno: p.config.Expected.MountNamespaceInode, poolDevice: p.config.Expected.RootDevice,
		poolInode: p.config.Expected.RootInode, poolMountID: p.config.Expected.MountID, token: p.volumeConfig.ExpectedRootToken}
	// Register before rechecking or closing. Even a failed Close keeps this
	// precise owned object reachable in the concrete native owner's bounded set.
	p.executables = append(p.executables, capability)
	if err = capability.check(p, file); err != nil {
		return capability, err
	}
	return capability, nil
}

// This comparison never issues a capability. Issuance additionally requires
// the concrete live pool owner and the strict descriptor checks above.
func fixedExecutableListedIdentity(objects []fixedBackingPoolStaticObject, path, digest string, stat unix.Stat_t, allocated int64) bool {
	if !fixedVolumeHex(digest, sha256.Size) {
		return false
	}
	for _, object := range objects {
		if object.Path == path && object.SHA256 == digest && object.Device == uint64(stat.Dev) &&
			object.Inode == stat.Ino && object.AllocatedBytes == allocated {
			return true
		}
	}
	return false
}

func (p *pooledFixedVolumeProvider) executable(path string) *fixedPoolExecutableCapability {
	for _, capability := range p.executables {
		if capability.path == path {
			return capability
		}
	}
	return nil
}

// Caller holds the concrete issuer lock, or is in its unpublished startup.
func (capability *fixedPoolExecutableCapability) check(p *pooledFixedVolumeProvider, file *os.File) error {
	if capability == nil || capability.issuer != p || p == nil || os.Getuid() != 0 || os.Geteuid() != 0 || p.closed || capability.closed || capability.closeTried ||
		capability.creatorPID != os.Getpid() || capability.creatorPID != p.executableCreatorPID || capability.token != p.volumeConfig.ExpectedRootToken ||
		capability.bootID != p.config.Expected.BootID || capability.namespaceDev != p.config.Expected.MountNamespaceDevice ||
		capability.namespaceIno != p.config.Expected.MountNamespaceInode || capability.poolDevice != p.config.Expected.RootDevice ||
		capability.poolInode != p.config.Expected.RootInode || capability.poolMountID != p.config.Expected.MountID || p.executable(capability.path) != capability {
		return errFixedBackingPoolUnsafe
	}
	boot, ns, err := fixedVolumeKernelDomain()
	if err != nil || boot != capability.bootID || uint64(ns.Dev) != capability.namespaceDev || ns.Ino != capability.namespaceIno {
		return errFixedBackingPoolUnsafe
	}
	stat, allocated, err := fixedExecutableFile(file, capability.digest)
	if err != nil || uint64(stat.Dev) != capability.device || stat.Ino != capability.inode || allocated != capability.allocated {
		return errFixedBackingPoolUnsafe
	}
	return fixedPoolCheckPath(capability.path, file, false)
}

func (capability *fixedPoolExecutableCapability) duplicate() (*fixedPoolExecutableUse, error) {
	if capability == nil {
		return nil, errFixedBackingPoolUnsafe
	}
	p, ok := capability.issuer.(*pooledFixedVolumeProvider)
	if !ok || p == nil {
		return nil, errFixedBackingPoolUnsafe
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.executableClosing || p.closed {
		return nil, errFixedExecutableRetained
	}
	if err := p.checkDomain(); err != nil {
		return nil, err
	}
	if err := p.checkPoolMarker(); err != nil {
		return nil, err
	}
	if err := p.checkPoolLock(); err != nil {
		return nil, err
	}
	if err := capability.check(p, capability.file); err != nil {
		return nil, err
	}
	slot := -1
	for index, use := range capability.uses {
		if use == nil {
			slot = index
			break
		}
	}
	if slot < 0 {
		return nil, errFixedExecutableRetained
	}
	fd, err := unix.FcntlInt(capability.file.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	use := &fixedPoolExecutableUse{mu: &sync.Mutex{}, capability: capability, file: os.NewFile(uintptr(fd), "owned-approved-executable-use"), slot: slot}
	capability.uses[slot] = use
	if err = capability.check(p, use.file); err != nil {
		return use, err
	}
	return use, nil
}

// The command is privately owned by the trusted caller and never published.
// Its executable descriptor must already occupy an explicit child ExtraFile.
func (use *fixedPoolExecutableUse) start(command *exec.Cmd) (resultErr error) {
	if use == nil || use.mu == nil {
		return errFixedExecutableRetained
	}
	use.mu.Lock()
	defer use.mu.Unlock()
	if use.capability == nil || command == nil || use.attempted || use.closed || use.unknown || command.Process != nil || command.ProcessState != nil {
		return errFixedExecutableRetained
	}
	p, ok := use.capability.issuer.(*pooledFixedVolumeProvider)
	if !ok || p == nil {
		return errFixedExecutableRetained
	}
	p.mu.Lock()
	validationErr := error(nil)
	if p.closed || p.executableClosing {
		validationErr = errFixedExecutableRetained
	} else {
		validationErr = use.capability.check(p, use.file)
	}
	p.mu.Unlock()
	if validationErr != nil {
		return validationErr
	}
	selected := false
	for index, file := range command.ExtraFiles {
		if file == use.file && command.Path == "/proc/self/fd/"+strconv.Itoa(3+index) {
			selected = true
		}
	}
	if !selected {
		return errFixedExecutableRetained
	}
	use.command, use.attempted = command, true
	returned := false
	defer func() {
		if !returned {
			use.unknown = true
		}
	}()
	resultErr = command.Start()
	if command.Process != nil {
		use.started = true
		if resultErr != nil {
			use.unknown = true
		}
	} else if resultErr == nil {
		use.unknown = true
		resultErr = errFixedExecutableRetained
	}
	use.startReturned = true
	returned = true
	return resultErr
}

func (use *fixedPoolExecutableUse) wait() (resultErr error) {
	if use == nil || use.mu == nil {
		return errFixedExecutableRetained
	}
	use.mu.Lock()
	defer use.mu.Unlock()
	// A non-nil Process, even when Start returned an error, still requires its
	// actual Wait. Unknown start ownership remains fenced after that join.
	if !use.started || use.command == nil || use.waitAttempted {
		return errFixedExecutableRetained
	}
	use.waitAttempted = true
	returned := false
	defer func() {
		if !returned {
			use.unknown = true
		}
	}()
	resultErr = use.command.Wait()
	if use.command.Process == nil || use.command.ProcessState == nil || use.command.Process.Pid != use.command.ProcessState.Pid() {
		use.unknown = true
		return errors.Join(resultErr, errFixedExecutableRetained)
	}
	use.joined = true
	returned = true
	return resultErr
}

func (use *fixedPoolExecutableUse) close() error {
	if use == nil || use.capability == nil || use.mu == nil {
		return errFixedExecutableRetained
	}
	use.mu.Lock()
	defer use.mu.Unlock()
	p, ok := use.capability.issuer.(*pooledFixedVolumeProvider)
	if !ok || p == nil {
		return errFixedExecutableRetained
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if use.closed {
		return nil
	}
	if use.slot < 0 || use.slot >= len(use.capability.uses) || use.capability.uses[use.slot] != use ||
		use.unknown || use.attempted && !use.startReturned || use.started && !use.joined {
		return errFixedExecutableRetained
	}
	if err := use.file.Close(); err != nil {
		use.unknown = true
		return errors.Join(err, errFixedExecutableRetained)
	}
	use.closed = true
	use.capability.uses[use.slot] = nil
	return nil
}

func (capability *fixedPoolExecutableCapability) closeOwned() error {
	if capability.closed {
		return nil
	}
	if capability.closeTried {
		return errors.Join(capability.closeErr, errFixedExecutableRetained)
	}
	for _, use := range capability.uses {
		if use != nil {
			return errFixedExecutableRetained
		}
	}
	capability.closeTried = true
	capability.closeErr = capability.file.Close()
	if capability.closeErr != nil {
		return errors.Join(capability.closeErr, errFixedExecutableRetained)
	}
	capability.closed = true
	return nil
}

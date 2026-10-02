//go:build linux

package transcode

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	fixedVolumeReceiptVersion = 4
	fixedVolumeBackingName    = "backing.ext4"
	fixedVolumeWorkspaceName  = "workspace"
	fixedVolumeImmutableFlag  = 0x10
)

// An intent with an unrecorded resulting identity is deliberately unrecoverable
// without a separate root audit. A crash never turns path discovery into delete
// authority. The owner retains the entire ledger reservation in that case.
type fixedVolumeDiskReceipt struct {
	Version                 int                             `json:"version"`
	ID                      string                          `json:"id"`
	Serial                  uint64                          `json:"serial"`
	Stage                   string                          `json:"stage"`
	Bytes                   int64                           `json:"bytes"`
	Inodes                  int64                           `json:"inodes"`
	Identity                fixedVolumeIdentity             `json:"identity"`
	BackingRemoved          bool                            `json:"backing_removed"`
	MountpointRemoved       bool                            `json:"mountpoint_removed"`
	DeleteRevision          uint64                          `json:"delete_revision"`
	RetirementIdentity      fixedVolumeIdentity             `json:"retirement_identity"`
	CleanupProof            string                          `json:"cleanup_proof"`
	InitialBackingMapSHA256 string                          `json:"initial_backing_map_sha256"`
	Materialization         fixedVolumeMaterializationAudit `json:"materialization"`
}

type linuxFixedVolumeProvider struct {
	mu       sync.Mutex
	config   fixedVolumeConfig
	root     *os.File
	lock     *os.File
	tool     *os.File
	rootStat unix.Stat_t
	lockStat unix.Stat_t
	toolStat unix.Stat_t
	bootID   string
	mountNS  unix.Stat_t
	journal  *fileStorageReservationJournal
	closed   bool
}

func openFixedVolumeProvider(config fixedVolumeConfig) (_ fixedVolumeProvider, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if !config.Enabled {
		return nil, errFixedVolumeUnavailable
	}
	if os.Geteuid() != 0 || !fixedVolumeHex(config.ExpectedRootToken, 16) ||
		!fixedVolumeHex(config.Mke2fsSHA256, sha256.Size) || config.Ledger == nil || config.poolPreparation == nil {
		return nil, fmt.Errorf("%w: root authority and explicit immutable tool approval are required", errFixedVolumeUnavailable)
	}
	if err = config.poolPreparation.validate(config); err != nil {
		return nil, err
	}
	p := &linuxFixedVolumeProvider{config: config}
	defer func() {
		if err != nil {
			_ = p.Close()
		}
	}()
	config.Ledger.mu.Lock()
	p.journal, _ = config.Ledger.journal.(*fileStorageReservationJournal)
	ledgerClosed := config.Ledger.closed || config.Ledger.failed != nil
	config.Ledger.mu.Unlock()
	if p.journal == nil || ledgerClosed || p.journal.expectedToken != config.ExpectedRootToken {
		return nil, fmt.Errorf("%w: one exact durable broker ledger is required", errFixedVolumeUnavailable)
	}
	if err = p.checkJournal(); err != nil {
		return nil, err
	}
	if p.bootID, p.mountNS, err = fixedVolumeKernelDomain(); err != nil {
		return nil, err
	}
	if p.root, err = fixedVolumeOpenAbsolute(config.ProvisionRoot, true); err != nil {
		return nil, err
	}
	if err = unix.Fstat(int(p.root.Fd()), &p.rootStat); err != nil {
		return nil, err
	}
	if p.rootStat.Mode&0o777 != 0o700 {
		return nil, fmt.Errorf("%w: provision root must have mode 0700", errFixedVolumeUnsafe)
	}
	var rootFS unix.Statfs_t
	if err = unix.Fstatfs(int(p.root.Fd()), &rootFS); err != nil || rootFS.Type != unix.EXT4_SUPER_MAGIC || rootFS.Bsize != fixedVolumeBlockSize {
		return nil, fmt.Errorf("%w: owned preallocation root must be on a local 4096-byte ext4 filesystem", errFixedVolumeUnavailable)
	}
	if err = p.checkMarker(); err != nil {
		return nil, err
	}
	if p.lock, err = fixedVolumeOpenAt(p.root, fixedVolumeLockName, unix.O_RDWR|unix.O_CREAT, 0o600); err != nil {
		return nil, err
	}
	if err = fixedVolumeOwnedRegular(p.lock, &p.lockStat); err != nil {
		return nil, err
	}
	if p.lockStat.Size != 0 {
		return nil, fmt.Errorf("%w: unexpected broker lock content", errFixedVolumeUnsafe)
	}
	if err = unix.Flock(int(p.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, fmt.Errorf("%w: dedicated provision root is already locked: %v", errFixedVolumeUnavailable, err)
	}
	if err = p.root.Sync(); err != nil {
		return nil, err
	}
	if p.tool, err = fixedVolumeOpenAbsolute(config.Mke2fsPath, false); err != nil {
		return nil, err
	}
	if err = fixedVolumeOwnedRegular(p.tool, &p.toolStat); err != nil {
		return nil, err
	}
	if err = p.checkTool(); err != nil {
		return nil, err
	}
	if err = p.checkRoot(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *linuxFixedVolumeProvider) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	var err error
	for _, file := range []*os.File{p.tool, p.lock, p.root} {
		if file != nil {
			err = errors.Join(err, file.Close())
		}
	}
	return err
}

func (p *linuxFixedVolumeProvider) checkMarker() error {
	marker, err := fixedVolumeOpenAt(p.root, fixedVolumeMarkerName, unix.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("%w: exact root marker is required: %v", errFixedVolumeUnsafe, err)
	}
	defer marker.Close()
	var stat unix.Stat_t
	if err = fixedVolumeOwnedRegular(marker, &stat); err != nil {
		return err
	}
	want := fixedVolumeRootMarker(p.config.ExpectedRootToken, p.journal.device, p.journal.inode)
	content, err := io.ReadAll(io.LimitReader(marker, int64(len(want)+1)))
	if err != nil || string(content) != want {
		return fmt.Errorf("%w: root marker does not match this broker", errFixedVolumeUnsafe)
	}
	return nil
}

func (p *linuxFixedVolumeProvider) checkRoot() error {
	if p.closed {
		return os.ErrClosed
	}
	if p.config.poolPreparation == nil {
		return errFixedVolumeUnavailable
	}
	if err := p.config.poolPreparation.validate(p.config); err != nil {
		return err
	}
	if err := p.checkJournal(); err != nil {
		return err
	}
	boot, mountNS, domainErr := fixedVolumeKernelDomain()
	if domainErr != nil || boot != p.bootID || !fixedVolumeSameInode(mountNS, p.mountNS) {
		return fmt.Errorf("%w: broker kernel namespace identity changed", errFixedVolumeRetained)
	}
	current, err := fixedVolumeOpenAbsolute(p.config.ProvisionRoot, true)
	if err != nil {
		return err
	}
	defer current.Close()
	var stat unix.Stat_t
	if err = unix.Fstat(int(current.Fd()), &stat); err != nil {
		return err
	}
	if !fixedVolumeSameInode(stat, p.rootStat) || stat.Mode&0o777 != 0o700 {
		return fmt.Errorf("%w: provision root identity changed", errFixedVolumeUnsafe)
	}
	lock, err := fixedVolumeOpenAt(p.root, fixedVolumeLockName, unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = fixedVolumeOwnedRegular(lock, &stat); err != nil || !fixedVolumeSameInode(stat, p.lockStat) || stat.Size != 0 {
		return fmt.Errorf("%w: root lock identity changed", errFixedVolumeUnsafe)
	}
	return p.checkMarker()
}

func fixedVolumeRootMarker(token string, ledgerDevice, ledgerInode uint64) string {
	return fixedVolumeMarkerVersion + token + "\n" + strconv.FormatUint(ledgerDevice, 10) + ":" + strconv.FormatUint(ledgerInode, 10) + "\n"
}

func (p *linuxFixedVolumeProvider) checkJournal() error {
	p.journal.mu.Lock()
	defer p.journal.mu.Unlock()
	return p.journal.validateRoot()
}

func (p *linuxFixedVolumeProvider) checkTool() error {
	var stat unix.Stat_t
	if err := fixedVolumeOwnedRegular(p.tool, &stat); err != nil {
		return err
	}
	if !fixedVolumeSameInode(stat, p.toolStat) || stat.Size != p.toolStat.Size ||
		stat.Mode&0o222 != 0 || stat.Mode&0o111 == 0 || stat.Size <= 0 || stat.Size > 128*1024*1024 {
		return fmt.Errorf("%w: approved formatter identity or permissions changed", errFixedVolumeUnsafe)
	}
	flags, err := unix.IoctlGetInt(int(p.tool.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil || flags&fixedVolumeImmutableFlag == 0 {
		return fmt.Errorf("%w: formatter must have the kernel immutable flag", errFixedVolumeUnavailable)
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, io.NewSectionReader(p.tool, 0, stat.Size)); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != p.config.Mke2fsSHA256 {
		return fmt.Errorf("%w: approved formatter digest changed", errFixedVolumeUnsafe)
	}
	return nil
}

func fixedVolumeOpenAbsolute(path string, directory bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" ||
		len(path) > 4096 || strings.ContainsAny(path, "\x00\r\n") {
		return nil, errFixedVolumeUnsafe
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), "/")
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, component := range parts {
		var stat unix.Stat_t
		if err = unix.Fstat(int(current.Fd()), &stat); err != nil || stat.Uid != 0 || stat.Mode&0o022 != 0 {
			_ = current.Close()
			return nil, fmt.Errorf("%w: every authority path ancestor must be owned and protected by root", errFixedVolumeUnsafe)
		}
		flags := unix.O_RDONLY
		if index != len(parts)-1 || directory {
			flags |= unix.O_DIRECTORY
		}
		next, openErr := fixedVolumeOpenAt(current, component, flags, 0)
		_ = current.Close()
		if openErr != nil {
			return nil, openErr
		}
		current = next
	}
	var stat unix.Stat_t
	if err = unix.Fstat(int(current.Fd()), &stat); err != nil || stat.Uid != 0 || stat.Mode&0o022 != 0 {
		_ = current.Close()
		return nil, fmt.Errorf("%w: authority path is not root protected", errFixedVolumeUnsafe)
	}
	return current, nil
}

func fixedVolumeOpenAt(directory *os.File, name string, flags int, mode uint32) (*os.File, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00\r\n") {
		return nil, errFixedVolumeUnsafe
	}
	fd, err := unix.Openat(int(directory.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, mode)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func fixedVolumeOwnedRegular(file *os.File, stat *unix.Stat_t) error {
	if err := unix.Fstat(int(file.Fd()), stat); err != nil {
		return err
	}
	if stat.Uid != 0 || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o077 != 0 || stat.Nlink != 1 {
		return fmt.Errorf("%w: control file must be root-owned, private and singly linked", errFixedVolumeUnsafe)
	}
	return nil
}

func fixedVolumeSameInode(a, b unix.Stat_t) bool { return a.Dev == b.Dev && a.Ino == b.Ino }

func fixedVolumeNamespaceName(id string, serial uint64) (string, error) {
	if !fixedVolumeValidID(id) || len(id) > 128 || serial == 0 {
		return "", errFixedVolumeUnsafe
	}
	return fixedVolumeLeaseID(id, serial), nil
}

func (p *linuxFixedVolumeProvider) writeReceipt(name string, receipt fixedVolumeDiskReceipt, create bool) error {
	content, err := json.Marshal(receipt)
	if err != nil || len(content) > maxFixedVolumeReceiptBytes {
		return fmt.Errorf("%w: receipt exceeds its reserved metadata bound", errFixedVolumeUnsafe)
	}
	finalName, nextName := name+".json", name+".next"
	if create {
		final, openErr := fixedVolumeOpenAt(p.root, finalName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0o600)
		if openErr != nil {
			return openErr
		}
		writeErr := fixedVolumeWriteSynced(final, content)
		closeErr := final.Close()
		return errors.Join(writeErr, closeErr, p.root.Sync())
	}
	// O_EXCL preserves a failed replacement staging file for the root audit.
	// It is never truncated or deleted on a retry guessed from its pathname.
	next, err := fixedVolumeOpenAt(p.root, nextName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err = fixedVolumeWriteSynced(next, content); err != nil {
		_ = next.Close()
		return err
	}
	if err = next.Close(); err != nil {
		return err
	}
	if err = unix.Renameat(int(p.root.Fd()), nextName, int(p.root.Fd()), finalName); err != nil {
		return err
	}
	return p.root.Sync()
}

func fixedVolumeWriteSynced(file *os.File, content []byte) error {
	written, err := file.Write(content)
	if err != nil {
		return err
	}
	if written != len(content) {
		return io.ErrShortWrite
	}
	return file.Sync()
}

func (p *linuxFixedVolumeProvider) readReceipt(name string) (fixedVolumeDiskReceipt, unix.Stat_t, error) {
	var receipt fixedVolumeDiskReceipt
	var stat unix.Stat_t
	// A staging file means an interrupted durability boundary. Only an explicit
	// root reconciliation may decide which replacement, if either, is current.
	if err := unix.Fstatat(int(p.root.Fd()), name+".next", &stat, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		return receipt, stat, fmt.Errorf("%w: interrupted receipt replacement", errFixedVolumeRetained)
	}
	file, err := fixedVolumeOpenAt(p.root, name+".json", unix.O_RDONLY, 0)
	if err != nil {
		return receipt, stat, fmt.Errorf("%w: no exact durable provisioning receipt: %v", errFixedVolumeRetained, err)
	}
	defer file.Close()
	if err = fixedVolumeOwnedRegular(file, &stat); err != nil || stat.Size <= 0 || stat.Size > maxFixedVolumeReceiptBytes {
		return receipt, stat, errFixedVolumeUnsafe
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxFixedVolumeReceiptBytes+1))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&receipt); err != nil {
		return receipt, stat, errFixedVolumeUnsafe
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return receipt, stat, errFixedVolumeUnsafe
	}
	if receipt.Version != fixedVolumeReceiptVersion || receipt.Identity.ProviderToken != p.config.ExpectedRootToken ||
		receipt.Identity.ProvisionRootDevice != uint64(p.rootStat.Dev) || receipt.Identity.ProvisionRootInode != p.rootStat.Ino ||
		receipt.Identity.LedgerRootDevice != p.journal.device || receipt.Identity.LedgerRootInode != p.journal.inode ||
		receipt.Identity.BootID != p.bootID || receipt.Identity.MountNamespaceDevice != uint64(p.mountNS.Dev) ||
		receipt.Identity.MountNamespaceInode != p.mountNS.Ino {
		return receipt, stat, errFixedVolumeUnsafe
	}
	expectedName, nameErr := fixedVolumeNamespaceName(receipt.ID, receipt.Serial)
	if nameErr != nil || expectedName != name || receipt.Identity.ID != name || !fixedVolumeReceiptStage(receipt.Stage) ||
		!fixedVolumePreparationReceiptValid(receipt) {
		return receipt, stat, errFixedVolumeUnsafe
	}
	return receipt, stat, nil
}

func fixedVolumeReceiptStage(stage string) bool {
	switch stage {
	case "planned", "namespace", "backing-intent", "backing", "allocated", "initialize-intent", "initialized", "format-intent", "materialize-intent", "materialized", "formatted",
		"mountpoint", "loop-intent", "loop-attached", "mount-intent", "mounted", "unmount-intent", "unmounted",
		"detach-intent", "detached", "unlink-intent", "backing-removed", "mountpoint-remove-intent", "mountpoint-removed",
		"namespace-remove-intent", "retired":
		return true
	default:
		return false
	}
}

func (p *linuxFixedVolumeProvider) Provision(ctx context.Context, permit *storageProvisionPermit) (_ fixedVolumeIdentity, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p.mu.Lock()
	defer p.mu.Unlock()
	if permit == nil || permit.ledger != p.config.Ledger || !permit.valid() {
		return fixedVolumeIdentity{}, fmt.Errorf("%w: durable provisioning authorization is required", errFixedVolumeUnsafe)
	}
	if err = ctx.Err(); err != nil {
		return fixedVolumeIdentity{}, err
	}
	if err = p.checkRoot(); err != nil {
		return fixedVolumeIdentity{}, err
	}
	if err = p.checkTool(); err != nil {
		return fixedVolumeIdentity{}, err
	}
	if err = p.checkUnacknowledgedRetirements(); err != nil {
		return fixedVolumeIdentity{}, err
	}
	if permit.ownerBytes < fixedVolumeOwnerBytes || permit.ownerInodes < fixedVolumeOwnerInodes ||
		permit.bytes < 16*1024*1024 || permit.bytes%fixedVolumeBlockSize != 0 ||
		permit.inodes < 16 || permit.inodes > math.MaxUint32 {
		return fixedVolumeIdentity{}, fmt.Errorf("%w: reservation does not cover the fixed domain and bounded owner metadata", errFixedVolumeUnsafe)
	}
	allowance, allowanceOK := addStorageCapacity(permit.bytes, permit.ownerBytes-fixedVolumeOwnerBytes)
	if !allowanceOK {
		return fixedVolumeIdentity{}, errFixedVolumeUnsafe
	}
	name, err := fixedVolumeNamespaceName(permit.id, permit.serial)
	if err != nil {
		return fixedVolumeIdentity{}, err
	}
	if err = permit.claimProvider(uint64(p.rootStat.Dev), p.rootStat.Ino, p.config.ExpectedRootToken); err != nil {
		return fixedVolumeIdentity{}, err
	}
	receipt := fixedVolumeDiskReceipt{Version: fixedVolumeReceiptVersion, ID: permit.id, Serial: permit.serial,
		Stage: "planned", Bytes: permit.bytes, Inodes: permit.inodes,
		Identity: fixedVolumeIdentity{ID: name, ProviderToken: p.config.ExpectedRootToken,
			ProvisionRootDevice: uint64(p.rootStat.Dev), ProvisionRootInode: p.rootStat.Ino,
			LedgerRootDevice: p.journal.device, LedgerRootInode: p.journal.inode,
			BootID: p.bootID, MountNamespaceDevice: uint64(p.mountNS.Dev), MountNamespaceInode: p.mountNS.Ino,
			BackingBytes: permit.bytes, BackingAllowanceBytes: allowance, BlockSize: fixedVolumeBlockSize}}
	defer func() {
		if err != nil {
			err = &fixedVolumeProvisionFailure{Stage: receipt.Stage, Identity: receipt.Identity, Cause: err}
		}
	}()
	if err = p.writeReceipt(name, receipt, true); err != nil {
		return receipt.Identity, err
	}
	if err = unix.Mkdirat(int(p.root.Fd()), name, 0o700); err != nil {
		return receipt.Identity, err
	}
	namespace, err := fixedVolumeOpenAt(p.root, name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return receipt.Identity, err
	}
	defer namespace.Close()
	var stat unix.Stat_t
	if err = unix.Fstat(int(namespace.Fd()), &stat); err != nil {
		return receipt.Identity, err
	}
	receipt.Identity.NamespaceDevice, receipt.Identity.NamespaceInode = uint64(stat.Dev), stat.Ino
	if err = p.root.Sync(); err != nil {
		return receipt.Identity, err
	}
	receipt.Stage = "namespace"
	if err = p.writeReceipt(name, receipt, false); err != nil {
		return receipt.Identity, err
	}
	receipt.Stage = "backing-intent"
	if err = p.writeReceipt(name, receipt, false); err != nil {
		return receipt.Identity, err
	}
	backing, err := fixedVolumeOpenAt(namespace, fixedVolumeBackingName, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL, 0o600)
	if err != nil {
		return receipt.Identity, err
	}
	defer backing.Close()
	if err = fixedVolumeOwnedRegular(backing, &stat); err != nil {
		return receipt.Identity, err
	}
	receipt.Identity.BackingDevice, receipt.Identity.BackingInode = uint64(stat.Dev), stat.Ino
	if err = namespace.Sync(); err != nil {
		return receipt.Identity, err
	}
	receipt.Stage = "backing"
	if err = p.writeReceipt(name, receipt, false); err != nil {
		return receipt.Identity, err
	}
	// Logical device capacity and actual host file allocation are distinct.
	// Metadata is charged to the already durable owner allowance, while all
	// transient allocations are additionally inside the verified hard pool.
	allocateErr := unix.Fallocate(int(backing.Fd()), 0, 0, permit.bytes)
	allocationSyncErr := backing.Sync()
	observeErr := fixedVolumeObserveBacking(backing, &receipt.Identity)
	receipt.Stage = "allocated"
	persistErr := p.writeReceipt(name, receipt, false)
	if err = errors.Join(allocateErr, allocationSyncErr, observeErr, persistErr); err != nil {
		return receipt.Identity, err
	}
	if err = fixedVolumeCheckBacking(backing, receipt.Identity, false); err != nil {
		return receipt.Identity, err
	}
	if receipt.Identity.BackingObservedBytes != permit.bytes || receipt.Identity.BackingAllocatedBytes < permit.bytes {
		return receipt.Identity, errFixedVolumeUnsafe
	}
	receipt.Stage = "initialize-intent"
	if err = p.writeReceipt(name, receipt, false); err != nil {
		return receipt.Identity, err
	}
	initializeErr := fixedVolumeInitializeBacking(ctx, backing, permit.bytes)
	initializationSyncErr := backing.Sync()
	observeErr = fixedVolumeObserveBacking(backing, &receipt.Identity)
	persistErr = p.writeReceipt(name, receipt, false)
	if err = errors.Join(initializeErr, initializationSyncErr, observeErr, persistErr); err != nil {
		return receipt.Identity, err
	}
	if err = fixedVolumeCheckBacking(backing, receipt.Identity, false); err != nil {
		return receipt.Identity, err
	}
	mapDigest, err := fixedPoolInitializedMap(backing, permit.bytes)
	if err != nil {
		return receipt.Identity, err
	}
	receipt.Identity.BackingMapSHA256 = mapDigest
	receipt.InitialBackingMapSHA256 = mapDigest
	receipt.Stage = "initialized"
	if err = p.writeReceipt(name, receipt, false); err != nil {
		return receipt.Identity, err
	}
	if err = fixedVolumeCheckBacking(backing, receipt.Identity, true); err != nil {
		return receipt.Identity, err
	}
	uuid, err := fixedVolumeNewUUID()
	if err != nil {
		return receipt.Identity, err
	}
	receipt.Identity.FilesystemUUID = uuid
	// The formatter is explicitly authorized to alter preparation mappings.
	// Preserve the initial map as audit data, not as a false active/cleanup pin.
	receipt.Identity.BackingMapSHA256 = ""
	receipt.Stage = "format-intent"
	if err = p.writeReceipt(name, receipt, false); err != nil {
		return receipt.Identity, err
	}
	formatErr := p.format(ctx, backing, permit.bytes, permit.inodes, uuid)
	formatSyncErr := backing.Sync()
	observeErr = fixedVolumeObserveBacking(backing, &receipt.Identity)
	persistErr = p.writeReceipt(name, receipt, false)
	if err = errors.Join(formatErr, formatSyncErr, observeErr, persistErr); err != nil {
		return receipt.Identity, err
	}
	if err = fixedVolumeCheckBacking(backing, receipt.Identity, false); err != nil {
		return receipt.Identity, err
	}
	superblock, err := fixedVolumeReadSuperblock(backing)
	if err != nil || superblock.UUID != uuid || superblock.Bytes != permit.bytes ||
		superblock.Inodes <= 0 || superblock.Inodes > permit.inodes || superblock.BlockSize != fixedVolumeBlockSize {
		return receipt.Identity, fmt.Errorf("%w: formatter did not create the reserved ext4 domain: %v", errFixedVolumeUnsafe, err)
	}
	if err = p.checkRoot(); err != nil {
		return receipt.Identity, err
	}
	if err = fixedVolumeCommitPreparation(ctx, backing, &receipt, func(next fixedVolumeDiskReceipt) error {
		return p.writeReceipt(name, next, false)
	}); err != nil {
		return receipt.Identity, err
	}
	if err = ctx.Err(); err != nil {
		return receipt.Identity, err
	}
	if err = fixedVolumeCheckBacking(backing, receipt.Identity, true); err != nil {
		return receipt.Identity, err
	}
	if err = p.checkRoot(); err != nil {
		return receipt.Identity, err
	}
	receipt.Stage = "formatted"
	if err = p.writeReceipt(name, receipt, false); err != nil {
		return receipt.Identity, err
	}
	if err = unix.Mkdirat(int(namespace.Fd()), fixedVolumeWorkspaceName, 0o700); err != nil {
		return receipt.Identity, err
	}
	mountpoint, err := fixedVolumeOpenAt(namespace, fixedVolumeWorkspaceName, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return receipt.Identity, err
	}
	defer mountpoint.Close()
	if err = unix.Fstat(int(mountpoint.Fd()), &stat); err != nil {
		return receipt.Identity, err
	}
	receipt.Identity.MountpointDevice, receipt.Identity.MountpointInode = uint64(stat.Dev), stat.Ino
	if err = namespace.Sync(); err != nil {
		return receipt.Identity, err
	}
	receipt.Stage = "mountpoint"
	if err = p.writeReceipt(name, receipt, false); err != nil {
		return receipt.Identity, err
	}
	loop, number, device, err := fixedVolumeFreeLoop()
	if err != nil {
		return receipt.Identity, err
	}
	defer loop.Close()
	receipt.Identity.LoopNumber, receipt.Identity.LoopDevice = number, device
	receipt.Stage = "loop-intent"
	if err = p.writeReceipt(name, receipt, false); err != nil {
		return receipt.Identity, err
	}
	if err = unix.IoctlLoopConfigure(int(loop.Fd()), &unix.LoopConfig{Fd: uint32(backing.Fd()),
		Size: uint32(fixedVolumeBlockSize), Info: unix.LoopInfo64{Sizelimit: uint64(permit.bytes)}}); err != nil {
		return receipt.Identity, err
	}
	if err = fixedVolumeCheckLoop(loop, receipt.Identity); err != nil {
		return receipt.Identity, err
	}
	receipt.Stage = "loop-attached"
	if err = p.writeReceipt(name, receipt, false); err != nil {
		return receipt.Identity, err
	}
	receipt.Stage = "mount-intent"
	if err = p.writeReceipt(name, receipt, false); err != nil {
		return receipt.Identity, err
	}
	if err = unix.Mount(fixedVolumeFDPath(loop), fixedVolumeChildPath(namespace, fixedVolumeWorkspaceName), "ext4",
		unix.MS_NODEV|unix.MS_NOSUID|unix.MS_NOEXEC, "errors=remount-ro,nodiscard"); err != nil {
		return receipt.Identity, err
	}
	workspace, err := fixedVolumeOpenAt(namespace, fixedVolumeWorkspaceName, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return receipt.Identity, err
	}
	defer workspace.Close()
	if err = fixedVolumeRecordMounted(workspace, &receipt.Identity, superblock); err != nil {
		return receipt.Identity, err
	}
	if err = fixedVolumeCheckBacking(backing, receipt.Identity, true); err != nil {
		return receipt.Identity, err
	}
	if err = receipt.Identity.validateReservation(permit.bytes, permit.inodes, permit.ownerBytes); err != nil {
		return receipt.Identity, err
	}
	receipt.Stage = "mounted"
	if err = p.writeReceipt(name, receipt, false); err != nil {
		return receipt.Identity, err
	}
	return receipt.Identity, nil
}

func fixedVolumeNewUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6], value[8] = value[6]&0x0f|0x40, value[8]&0x3f|0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

// Only this pinned immutable approved executable is run. Arguments are fixed
// broker values and inherited FD roles; no shell, client path, config file or
// inherited environment contributes formatter authority.
func (p *linuxFixedVolumeProvider) format(ctx context.Context, backing *os.File, bytes, inodes int64, uuid string) error {
	if err := p.checkTool(); err != nil {
		return err
	}
	args := []string{"-t", "ext4", "-b", "4096", "-I", "256", "-N", strconv.FormatInt(inodes, 10),
		"-m", "0", "-O", "none,has_journal,extent,filetype,sparse_super,large_file,uninit_bg,64bit",
		"-E", "lazy_itable_init=0,lazy_journal_init=0,nodiscard", "-U", uuid, "-F", "/proc/self/fd/4",
		strconv.FormatInt(bytes/fixedVolumeBlockSize, 10)}
	command := exec.CommandContext(ctx, p.config.Mke2fsPath, args...)
	command.Path = "/proc/self/fd/3"
	command.ExtraFiles = []*os.File{p.tool, backing}
	command.Dir = "/"
	command.Env = []string{"LC_ALL=C", "PATH=/nonexistent", "HOME=/nonexistent", "TMPDIR=/nonexistent", "MKE2FS_CONFIG=/dev/null"}
	output := &fixedVolumeBoundedOutput{}
	command.Stdout, command.Stderr = output, output
	if err := command.Run(); err != nil {
		return fmt.Errorf("approved ext4 formatter failed: %w: %s", err, output.data)
	}
	return p.checkTool()
}

type fixedVolumeBoundedOutput struct{ data []byte }

func (output *fixedVolumeBoundedOutput) Write(value []byte) (int, error) {
	remaining := maxFixedVolumeReceiptBytes - len(output.data)
	if remaining > len(value) {
		remaining = len(value)
	}
	if remaining > 0 {
		output.data = append(output.data, value[:remaining]...)
	}
	return len(value), nil
}

func fixedVolumeFDPath(file *os.File) string { return "/proc/self/fd/" + strconv.Itoa(int(file.Fd())) }

func fixedVolumeChildPath(directory *os.File, name string) string {
	return fixedVolumeFDPath(directory) + "/" + name
}

func fixedVolumeObserveBacking(backing *os.File, identity *fixedVolumeIdentity) error {
	var stat unix.Stat_t
	if err := fixedVolumeOwnedRegular(backing, &stat); err != nil {
		return err
	}
	if uint64(stat.Dev) != identity.BackingDevice || stat.Ino != identity.BackingInode || stat.Size < 0 || stat.Blocks < 0 || stat.Blocks > math.MaxInt64/512 {
		return errFixedVolumeUnsafe
	}
	identity.BackingObservedBytes, identity.BackingAllocatedBytes = stat.Size, stat.Blocks*512
	return nil
}

func fixedVolumeInitializeBacking(ctx context.Context, backing *os.File, bytes int64) error {
	if bytes <= 0 || bytes%fixedVolumeBlockSize != 0 {
		return errFixedVolumeUnsafe
	}
	zeros := make([]byte, 1<<20)
	for offset := int64(0); offset < bytes; {
		if err := ctx.Err(); err != nil {
			return err
		}
		length := int64(len(zeros))
		if bytes-offset < length {
			length = bytes - offset
		}
		written, err := backing.WriteAt(zeros[:int(length)], offset)
		if err != nil {
			return err
		}
		if written != int(length) {
			return io.ErrShortWrite
		}
		offset += int64(written)
	}
	return nil
}

func fixedVolumeCheckBacking(backing *os.File, identity fixedVolumeIdentity, initialized bool) error {
	var stat unix.Stat_t
	if err := fixedVolumeOwnedRegular(backing, &stat); err != nil {
		return err
	}
	if uint64(stat.Dev) != identity.BackingDevice || stat.Ino != identity.BackingInode ||
		stat.Blocks < 0 || stat.Blocks > math.MaxInt64/512 {
		return errFixedVolumeUnsafe
	}
	if stat.Size != identity.BackingObservedBytes || stat.Size < 0 || stat.Size > identity.BackingBytes ||
		stat.Blocks*512 != identity.BackingAllocatedBytes || identity.BackingAllocatedBytes < 0 ||
		identity.BackingAllocatedBytes > identity.BackingAllowanceBytes || identity.BackingAllowanceBytes < identity.BackingBytes {
		return errFixedVolumeUnsafe
	}
	if initialized {
		if stat.Size != identity.BackingBytes || identity.BackingAllocatedBytes < identity.BackingBytes || !fixedVolumeHex(identity.BackingMapSHA256, sha256.Size) {
			return errFixedVolumeUnsafe
		}
		digest, err := fixedPoolInitializedMap(backing, identity.BackingBytes)
		if err != nil || digest != identity.BackingMapSHA256 {
			return fmt.Errorf("%w: initialized backing mapping changed or is unknown", errFixedVolumeUnsafe)
		}
	}
	return nil
}

func fixedVolumeFreeLoop() (*os.File, uint32, uint64, error) {
	controlFD, err := unix.Open("/dev/loop-control", unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("%w: loop control is unavailable: %v", errFixedVolumeUnavailable, err)
	}
	defer unix.Close(controlFD)
	var controlStat unix.Stat_t
	if err = unix.Fstat(controlFD, &controlStat); err != nil || controlStat.Uid != 0 ||
		controlStat.Mode&unix.S_IFMT != unix.S_IFCHR || controlStat.Rdev != unix.Mkdev(10, 237) {
		return nil, 0, 0, errFixedVolumeUnsafe
	}
	number, err := unix.IoctlRetInt(controlFD, unix.LOOP_CTL_GET_FREE)
	if err != nil || number < 0 || uint64(number) > math.MaxUint32 {
		return nil, 0, 0, fmt.Errorf("%w: free loop device unavailable: %v", errFixedVolumeUnavailable, err)
	}
	loop, err := fixedVolumeOpenLoop(uint32(number), 0)
	if err != nil {
		return nil, 0, 0, err
	}
	var stat unix.Stat_t
	if err = unix.Fstat(int(loop.Fd()), &stat); err != nil {
		_ = loop.Close()
		return nil, 0, 0, err
	}
	if _, err = unix.IoctlLoopGetStatus64(int(loop.Fd())); !errors.Is(err, unix.ENXIO) {
		_ = loop.Close()
		return nil, 0, 0, fmt.Errorf("%w: selected loop device is not empty", errFixedVolumeRetained)
	}
	return loop, uint32(number), stat.Rdev, nil
}

func fixedVolumeOpenLoop(number uint32, expected uint64) (*os.File, error) {
	path := "/dev/loop" + strconv.FormatUint(uint64(number), 10)
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	loop := os.NewFile(uintptr(fd), path)
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err != nil || stat.Uid != 0 || stat.Mode&unix.S_IFMT != unix.S_IFBLK ||
		unix.Major(stat.Rdev) != 7 || unix.Minor(stat.Rdev) != number || (expected != 0 && stat.Rdev != expected) {
		_ = loop.Close()
		return nil, errFixedVolumeUnsafe
	}
	return loop, nil
}

func fixedVolumeCheckLoop(loop *os.File, identity fixedVolumeIdentity) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(loop.Fd()), &stat); err != nil || stat.Rdev != identity.LoopDevice || stat.Mode&unix.S_IFMT != unix.S_IFBLK {
		return errFixedVolumeUnsafe
	}
	status, err := unix.IoctlLoopGetStatus64(int(loop.Fd()))
	if err != nil || status.Device != identity.BackingDevice || status.Inode != identity.BackingInode ||
		status.Number != identity.LoopNumber || status.Offset != 0 || status.Sizelimit != uint64(identity.BackingBytes) ||
		status.Flags != 0 || status.Encrypt_type != 0 || status.Encrypt_key_size != 0 {
		return fmt.Errorf("%w: loop backing identity or fixed capacity changed", errFixedVolumeUnsafe)
	}
	var bytes uint64
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, loop.Fd(), uintptr(unix.BLKGETSIZE64), uintptr(unsafe.Pointer(&bytes)))
	if errno != 0 || bytes != uint64(identity.BackingBytes) {
		return fmt.Errorf("%w: exact loop capacity cannot be established", errFixedVolumeUnsafe)
	}
	return nil
}

type fixedVolumeSuperblock struct {
	Bytes     int64
	Inodes    int64
	BlockSize int64
	UUID      string
}

func fixedVolumeReadSuperblock(backing *os.File) (fixedVolumeSuperblock, error) {
	var data [1024]byte
	if _, err := backing.ReadAt(data[:], 1024); err != nil {
		return fixedVolumeSuperblock{}, err
	}
	return fixedVolumeParseSuperblock(data[:])
}

func fixedVolumeParseSuperblock(data []byte) (fixedVolumeSuperblock, error) {
	if len(data) != 1024 || binary.LittleEndian.Uint16(data[56:58]) != 0xef53 {
		return fixedVolumeSuperblock{}, fmt.Errorf("%w: invalid ext4 superblock", errFixedVolumeUnsafe)
	}
	log := binary.LittleEndian.Uint32(data[24:28])
	if log != 2 {
		return fixedVolumeSuperblock{}, errFixedVolumeUnsafe
	}
	blocks := uint64(binary.LittleEndian.Uint32(data[4:8]))
	incompat := binary.LittleEndian.Uint32(data[96:100])
	if incompat&0x80 != 0 {
		blocks |= uint64(binary.LittleEndian.Uint32(data[336:340])) << 32
	}
	if blocks == 0 || blocks > math.MaxInt64/uint64(fixedVolumeBlockSize) {
		return fixedVolumeSuperblock{}, errFixedVolumeUnsafe
	}
	encoded := hex.EncodeToString(data[104:120])
	uuid := encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
	if !fixedVolumeValidUUID(uuid) || binary.LittleEndian.Uint16(data[88:90]) != 256 ||
		incompat&0x40 == 0 || binary.LittleEndian.Uint32(data[92:96])&0x4 == 0 {
		return fixedVolumeSuperblock{}, fmt.Errorf("%w: unexpected ext4 UUID, inode size or feature domain", errFixedVolumeUnsafe)
	}
	return fixedVolumeSuperblock{Bytes: int64(blocks) * fixedVolumeBlockSize,
		Inodes: int64(binary.LittleEndian.Uint32(data[:4])), BlockSize: fixedVolumeBlockSize, UUID: uuid}, nil
}

func fixedVolumeRecordMounted(workspace *os.File, identity *fixedVolumeIdentity, superblock fixedVolumeSuperblock) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(workspace.Fd()), &stat); err != nil || uint64(stat.Dev) != identity.LoopDevice || stat.Ino != 2 {
		return fmt.Errorf("%w: mounted root does not identify the owned loop filesystem", errFixedVolumeUnsafe)
	}
	var fs unix.Statfs_t
	if err := unix.Fstatfs(int(workspace.Fd()), &fs); err != nil || fs.Type != unix.EXT4_SUPER_MAGIC ||
		fs.Bsize != fixedVolumeBlockSize || fs.Blocks == 0 || fs.Blocks > math.MaxInt64/uint64(fixedVolumeBlockSize) ||
		fs.Files == 0 || fs.Files > math.MaxInt64 || int64(fs.Files) != superblock.Inodes ||
		int64(fs.Blocks)*fixedVolumeBlockSize > superblock.Bytes {
		return fmt.Errorf("%w: authoritative mounted byte and inode totals are unknown", errFixedVolumeUnsafe)
	}
	var statx unix.Statx_t
	if err := unix.Statx(int(workspace.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW,
		unix.STATX_INO|unix.STATX_MNT_ID, &statx); err != nil || statx.Mask&unix.STATX_MNT_ID == 0 || statx.Mnt_id == 0 ||
		statx.Ino != stat.Ino || unix.Mkdev(statx.Dev_major, statx.Dev_minor) != uint64(stat.Dev) {
		return fmt.Errorf("%w: exact mount identity is unavailable", errFixedVolumeUnsafe)
	}
	identity.MountID, identity.RootDevice, identity.RootInode = statx.Mnt_id, uint64(stat.Dev), stat.Ino
	identity.FilesystemBytes, identity.FilesystemInodes = int64(fs.Blocks)*fixedVolumeBlockSize, int64(fs.Files)
	return nil
}

func fixedVolumeKernelDomain() (string, unix.Stat_t, error) {
	var stat unix.Stat_t
	file, err := os.Open("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", stat, err
	}
	content, readErr := io.ReadAll(io.LimitReader(file, 38))
	closeErr := file.Close()
	bootID := strings.TrimSuffix(string(content), "\n")
	if readErr != nil || closeErr != nil || !fixedVolumeValidUUID(bootID) || len(content) != 37 {
		return "", stat, errFixedVolumeUnsafe
	}
	// Namespace handles are kernel magic links, rather than filesystem paths
	// received from a client. Their opened dev/inode identity is the authority.
	fd, err := unix.Open("/proc/thread-self/ns/mnt", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", stat, err
	}
	defer unix.Close(fd)
	var fs unix.Statfs_t
	if err = unix.Fstat(fd, &stat); err != nil || stat.Ino == 0 ||
		unix.Fstatfs(fd, &fs) != nil || fs.Type != unix.NSFS_MAGIC {
		return "", stat, errFixedVolumeUnsafe
	}
	return bootID, stat, nil
}

func fixedVolumeDirectoryNames(directory *os.File, maximum int) ([]string, error) {
	fd, err := unix.Openat(int(directory.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	scan := os.NewFile(uintptr(fd), "owned-directory-inventory")
	defer scan.Close()
	names, err := scan.Readdirnames(maximum + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > maximum {
		return nil, fmt.Errorf("%w: dedicated root inventory exceeds its bounded audit", errFixedVolumeRetained)
	}
	return names, nil
}

func (p *linuxFixedVolumeProvider) checkUnacknowledgedRetirements() error {
	names, err := fixedVolumeDirectoryNames(p.root, 2*maxStorageLeases+2)
	if err != nil {
		return err
	}
	for _, name := range names {
		if name == fixedVolumeMarkerName || name == fixedVolumeLockName {
			continue
		}
		if !strings.HasPrefix(name, "volume-") || strings.HasSuffix(name, ".next") {
			return fmt.Errorf("%w: unexpected or interrupted provisioning owner metadata", errFixedVolumeRetained)
		}
		if strings.HasSuffix(name, ".json") {
			receipt, _, readErr := p.readReceipt(strings.TrimSuffix(name, ".json"))
			if readErr != nil {
				return readErr
			}
			if readErr = p.checkReceiptLedgerBinding(receipt); readErr != nil {
				return readErr
			}
			if receipt.Stage == "retired" {
				return fmt.Errorf("%w: retired owner receipt requires ledger acknowledgment", errFixedVolumeRetained)
			}
			continue
		}
		var stat unix.Stat_t
		if !fixedVolumeValidID(name) || unix.Fstatat(int(p.root.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW) != nil ||
			stat.Uid != 0 || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0o777 != 0o700 {
			return errFixedVolumeUnsafe
		}
		receipt, _, readErr := p.readReceipt(name)
		if readErr != nil {
			return readErr
		}
		if readErr = p.checkReceiptLedgerBinding(receipt); readErr != nil {
			return readErr
		}
		namespace, openErr := fixedVolumeOpenOwnedDirectory(p.root, name, receipt.Identity.NamespaceDevice, receipt.Identity.NamespaceInode)
		if openErr != nil {
			return openErr
		}
		contentErr := fixedVolumeAssertNamespaceContents(namespace, receipt)
		closeErr := namespace.Close()
		if openErr = errors.Join(contentErr, closeErr); openErr != nil {
			return openErr
		}
	}
	return nil
}

func (p *linuxFixedVolumeProvider) checkReceiptLedgerBinding(receipt fixedVolumeDiskReceipt) error {
	p.config.Ledger.mu.Lock()
	defer p.config.Ledger.mu.Unlock()
	l := p.config.Ledger
	index := l.recordIndexLocked(receipt.ID, receipt.Serial)
	if index < 0 || l.closed || l.failed != nil {
		return fmt.Errorf("%w: provider receipt has no charged durable ledger owner", errFixedVolumeRetained)
	}
	record := l.state.Records[index]
	if record.Phase < storageProvisioning || record.ProviderRootDevice != uint64(p.rootStat.Dev) ||
		record.ProviderRootInode != p.rootStat.Ino || record.ProviderToken != p.config.ExpectedRootToken ||
		receipt.Bytes != l.state.Limits.VolumeBytes || receipt.Inodes != l.state.Limits.VolumeInodes ||
		(record.Identity != (fixedVolumeIdentity{}) && record.Identity != receipt.Identity) {
		return fmt.Errorf("%w: provider receipt and its full ledger reservation disagree", errFixedVolumeUnsafe)
	}
	return nil
}

func fixedVolumeAssertAbsent(directory *os.File, name string) error {
	var stat unix.Stat_t
	err := unix.Fstatat(int(directory.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("%w: an absent resource cannot be proved", errFixedVolumeRetained)
	}
	return nil
}

func fixedVolumeOpenOwnedDirectory(directory *os.File, name string, device, inode uint64) (*os.File, error) {
	if device == 0 || inode == 0 {
		return nil, errFixedVolumeUnsafe
	}
	file, err := fixedVolumeOpenAt(directory, name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if err = unix.Fstat(int(file.Fd()), &stat); err != nil || uint64(stat.Dev) != device || stat.Ino != inode ||
		stat.Uid != 0 || stat.Mode&0o777 != 0o700 {
		_ = file.Close()
		return nil, fmt.Errorf("%w: exact owned directory identity changed", errFixedVolumeUnsafe)
	}
	return file, nil
}

func fixedVolumeAssertNamespaceContents(namespace *os.File, receipt fixedVolumeDiskReceipt) error {
	names, err := fixedVolumeDirectoryNames(namespace, 2)
	if err != nil {
		return err
	}
	for _, name := range names {
		switch name {
		case fixedVolumeBackingName:
			if receipt.Identity.BackingInode == 0 || receipt.BackingRemoved {
				return errFixedVolumeUnsafe
			}
		case fixedVolumeWorkspaceName:
			if receipt.Identity.MountpointInode == 0 || receipt.MountpointRemoved {
				return errFixedVolumeUnsafe
			}
		default:
			return fmt.Errorf("%w: namespace has an unowned resource", errFixedVolumeUnsafe)
		}
	}
	return nil
}

func fixedVolumeAssertEmpty(directory *os.File) error {
	names, err := fixedVolumeDirectoryNames(directory, 0)
	if err != nil || len(names) != 0 {
		return fmt.Errorf("%w: underlying mountpoint is not empty", errFixedVolumeUnsafe)
	}
	return nil
}

func fixedVolumeCheckMounted(workspace, backing *os.File, identity fixedVolumeIdentity) error {
	superblock, err := fixedVolumeReadSuperblock(backing)
	if err != nil || superblock.UUID != identity.FilesystemUUID || superblock.Bytes != identity.BackingBytes ||
		superblock.Inodes != identity.FilesystemInodes || superblock.BlockSize != identity.BlockSize {
		return fmt.Errorf("%w: mounted filesystem UUID or metadata totals changed", errFixedVolumeUnsafe)
	}
	observed := identity
	if err = fixedVolumeRecordMounted(workspace, &observed, superblock); err != nil {
		return err
	}
	if observed != identity {
		return fmt.Errorf("%w: mounted filesystem identity changed", errFixedVolumeUnsafe)
	}
	return nil
}

func (p *linuxFixedVolumeProvider) Retire(ctx context.Context, permit *storageRetirePermit) (_ *storageRetirementReceipt, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p.mu.Lock()
	defer p.mu.Unlock()
	if permit == nil || permit.ledger != p.config.Ledger || !permit.valid() || permit.providerRootDevice != uint64(p.rootStat.Dev) ||
		permit.providerRootInode != p.rootStat.Ino || permit.providerToken != p.config.ExpectedRootToken {
		return nil, fmt.Errorf("%w: exact durable retirement authority is required", errFixedVolumeUnsafe)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = p.checkRoot(); err != nil {
		return nil, err
	}
	name, err := fixedVolumeNamespaceName(permit.id, permit.serial)
	if err != nil {
		return nil, err
	}
	receipt, _, err := p.readReceipt(name)
	if err != nil {
		// A confirmed ledger proof is a second durable authority after receipt
		// acknowledgment. Bare pathname absence is never enough after a crash.
		proof := permit.confirmedProof()
		cleanupIdentity := permit.confirmedCleanupIdentity()
		if proof == ([32]byte{}) || cleanupIdentity == (fixedVolumeIdentity{}) ||
			fixedVolumeAssertAbsent(p.root, name+".json") != nil || fixedVolumeAssertAbsent(p.root, name+".next") != nil {
			return nil, err
		}
		if err = p.checkRetiredResources(name, cleanupIdentity); err != nil {
			return nil, err
		}
		return p.mintRetirement(permit, cleanupIdentity, proof)
	}
	if receipt.ID != permit.id || receipt.Serial != permit.serial || receipt.Identity.ID != name ||
		receipt.Bytes < 16*1024*1024 || receipt.Bytes%fixedVolumeBlockSize != 0 || receipt.Inodes < 16 ||
		(permit.identity != (fixedVolumeIdentity{}) && receipt.Identity != permit.identity) {
		return nil, errFixedVolumeUnsafe
	}
	if receipt.DeleteRevision != 0 && (receipt.DeleteRevision != permit.revision || receipt.RetirementIdentity != permit.identity) {
		return nil, fmt.Errorf("%w: receipt belongs to a different deletion revision", errFixedVolumeUnsafe)
	}
	if receipt.Stage == "retired" {
		if receipt.DeleteRevision != permit.revision || !receipt.BackingRemoved || !receipt.MountpointRemoved ||
			!fixedVolumeHex(receipt.CleanupProof, sha256.Size) {
			return nil, errFixedVolumeUnsafe
		}
		proof := fixedVolumeCleanupProof(receipt)
		if hex.EncodeToString(proof[:]) != receipt.CleanupProof {
			return nil, errFixedVolumeUnsafe
		}
		if err = p.checkRetiredResources(name, receipt.Identity); err != nil {
			return nil, err
		}
		return p.mintRetirement(permit, receipt.Identity, proof)
	}
	if receipt.DeleteRevision == 0 {
		receipt.DeleteRevision, receipt.RetirementIdentity = permit.revision, permit.identity
		if err = p.writeReceipt(name, receipt, false); err != nil {
			return nil, err
		}
	}
	if receipt.Stage == "planned" {
		if receipt.Identity.NamespaceInode != 0 || receipt.Identity.BackingInode != 0 || receipt.Identity.LoopDevice != 0 ||
			fixedVolumeAssertAbsent(p.root, name) != nil {
			return nil, fmt.Errorf("%w: unrecorded namespace creation requires owner reconciliation", errFixedVolumeRetained)
		}
		receipt.BackingRemoved, receipt.MountpointRemoved = true, true
		return p.finishRetirement(name, receipt, permit)
	}
	if receipt.Stage == "backing-intent" || receipt.Stage == "mount-intent" && receipt.Identity.MountID == 0 {
		return nil, fmt.Errorf("%w: interrupted creation has no exact resulting resource identity", errFixedVolumeRetained)
	}
	if receipt.Stage == "namespace-remove-intent" && fixedVolumeAssertAbsent(p.root, name) == nil {
		if !receipt.BackingRemoved || !receipt.MountpointRemoved {
			return nil, errFixedVolumeUnsafe
		}
		return p.finishRetirement(name, receipt, permit)
	}
	namespace, err := fixedVolumeOpenOwnedDirectory(p.root, name, receipt.Identity.NamespaceDevice, receipt.Identity.NamespaceInode)
	if err != nil {
		return nil, err
	}
	defer namespace.Close()
	if err = fixedVolumeAssertNamespaceContents(namespace, receipt); err != nil {
		return nil, err
	}
	if err = p.retireMount(ctx, name, namespace, &receipt); err != nil {
		return nil, err
	}
	if err = p.retireLoop(ctx, name, &receipt); err != nil {
		return nil, err
	}
	if err = p.retireBacking(ctx, name, namespace, &receipt); err != nil {
		return nil, err
	}
	if err = p.retireMountpoint(ctx, name, namespace, &receipt); err != nil {
		return nil, err
	}
	if err = fixedVolumeAssertEmpty(namespace); err != nil {
		return nil, err
	}
	receipt.Stage = "namespace-remove-intent"
	if err = p.writeReceipt(name, receipt, false); err != nil {
		return nil, err
	}
	// Reopen the name after the durability boundary. The root lock and root-only
	// hierarchy exclude client replacement; this final check detects owner drift.
	recheck, err := fixedVolumeOpenOwnedDirectory(p.root, name, receipt.Identity.NamespaceDevice, receipt.Identity.NamespaceInode)
	if err != nil {
		return nil, err
	}
	if err = fixedVolumeAssertEmpty(recheck); err != nil {
		_ = recheck.Close()
		return nil, err
	}
	if err = recheck.Close(); err != nil {
		return nil, err
	}
	if err = unix.Unlinkat(int(p.root.Fd()), name, unix.AT_REMOVEDIR); err != nil {
		return nil, err
	}
	if err = p.root.Sync(); err != nil {
		return nil, err
	}
	return p.finishRetirement(name, receipt, permit)
}

func (p *linuxFixedVolumeProvider) retireMount(ctx context.Context, name string, namespace *os.File, receipt *fixedVolumeDiskReceipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if receipt.Identity.MountpointInode == 0 || receipt.MountpointRemoved {
		return fixedVolumeAssertAbsent(namespace, fixedVolumeWorkspaceName)
	}
	workspace, err := fixedVolumeOpenAt(namespace, fixedVolumeWorkspaceName, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		if receipt.Stage == "mountpoint-remove-intent" && errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	var stat unix.Stat_t
	if err = unix.Fstat(int(workspace.Fd()), &stat); err != nil {
		_ = workspace.Close()
		return err
	}
	underlying := uint64(stat.Dev) == receipt.Identity.MountpointDevice && stat.Ino == receipt.Identity.MountpointInode
	if underlying {
		err = fixedVolumeAssertEmpty(workspace)
		_ = workspace.Close()
		if err != nil {
			return err
		}
		if receipt.Identity.MountID != 0 && receipt.Stage == "mounted" {
			return fmt.Errorf("%w: mount vanished before authorized retirement", errFixedVolumeRetained)
		}
		if receipt.Stage == "unmount-intent" {
			receipt.Stage = "unmounted"
			return p.writeReceipt(name, *receipt, false)
		}
		return nil
	}
	if receipt.Identity.MountID == 0 || (receipt.Stage != "mounted" && receipt.Stage != "unmount-intent") {
		_ = workspace.Close()
		return fmt.Errorf("%w: unknown mount identity retains its full reservation", errFixedVolumeRetained)
	}
	backing, err := fixedVolumeOpenAt(namespace, fixedVolumeBackingName, unix.O_RDONLY, 0)
	if err != nil {
		_ = workspace.Close()
		return err
	}
	if err = fixedVolumeCheckBacking(backing, receipt.Identity, true); err == nil {
		err = fixedVolumeCheckMounted(workspace, backing, receipt.Identity)
	}
	closeErr := backing.Close()
	workspaceCloseErr := workspace.Close()
	if err = errors.Join(err, closeErr, workspaceCloseErr); err != nil {
		return err
	}
	loop, err := fixedVolumeOpenLoop(receipt.Identity.LoopNumber, receipt.Identity.LoopDevice)
	if err != nil {
		return err
	}
	loopErr := fixedVolumeCheckLoop(loop, receipt.Identity)
	loopCloseErr := loop.Close()
	if err = errors.Join(loopErr, loopCloseErr); err != nil {
		return err
	}
	receipt.Stage = "unmount-intent"
	if err = p.writeReceipt(name, *receipt, false); err != nil {
		return err
	}
	// Flags are exactly zero. A busy filesystem keeps its mount, loop, image,
	// durable ownership receipt and full reservation; no lazy/forced retry exists.
	if err = unix.Unmount(fixedVolumeChildPath(namespace, fixedVolumeWorkspaceName), 0); err != nil {
		return fmt.Errorf("%w: exact nonlazy unmount failed: %v", errFixedVolumeRetained, err)
	}
	underlyingDirectory, err := fixedVolumeOpenOwnedDirectory(namespace, fixedVolumeWorkspaceName,
		receipt.Identity.MountpointDevice, receipt.Identity.MountpointInode)
	if err != nil {
		return err
	}
	err = fixedVolumeAssertEmpty(underlyingDirectory)
	closeErr = underlyingDirectory.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return err
	}
	receipt.Stage = "unmounted"
	return p.writeReceipt(name, *receipt, false)
}

func (p *linuxFixedVolumeProvider) retireLoop(ctx context.Context, name string, receipt *fixedVolumeDiskReceipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if receipt.Identity.LoopDevice == 0 {
		return nil
	}
	loop, err := fixedVolumeOpenLoop(receipt.Identity.LoopNumber, receipt.Identity.LoopDevice)
	if err != nil {
		return err
	}
	defer loop.Close()
	_, statusErr := unix.IoctlLoopGetStatus64(int(loop.Fd()))
	if errors.Is(statusErr, unix.ENXIO) {
		switch receipt.Stage {
		case "loop-intent", "detach-intent":
			receipt.Stage = "detached"
			return p.writeReceipt(name, *receipt, false)
		case "detached", "unlink-intent", "backing-removed", "mountpoint-remove-intent", "mountpoint-removed", "namespace-remove-intent":
			return nil
		default:
			return fmt.Errorf("%w: loop disappeared outside the recorded detach boundary", errFixedVolumeRetained)
		}
	}
	if err = fixedVolumeCheckLoop(loop, receipt.Identity); err != nil {
		return err
	}
	if receipt.Identity.MountID != 0 && receipt.Stage != "unmounted" && receipt.Stage != "detach-intent" {
		return errFixedVolumeRetained
	}
	receipt.Stage = "detach-intent"
	if err = p.writeReceipt(name, *receipt, false); err != nil {
		return err
	}
	if err = unix.IoctlSetInt(int(loop.Fd()), unix.LOOP_CLR_FD, 0); err != nil {
		return fmt.Errorf("%w: exact loop detach failed: %v", errFixedVolumeRetained, err)
	}
	// Linux may defer a busy LOOP_CLR_FD through AUTOCLEAR while returning zero.
	// Only immediate ENXIO proves detachment; deferred detach never releases or
	// unlinks the image and remains an owned, charged cleanup failure.
	if _, err = unix.IoctlLoopGetStatus64(int(loop.Fd())); !errors.Is(err, unix.ENXIO) {
		return fmt.Errorf("%w: loop detach remains busy or unknown", errFixedVolumeRetained)
	}
	receipt.Stage = "detached"
	return p.writeReceipt(name, *receipt, false)
}

func (p *linuxFixedVolumeProvider) retireBacking(ctx context.Context, name string, namespace *os.File, receipt *fixedVolumeDiskReceipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if receipt.Identity.BackingInode == 0 || receipt.BackingRemoved {
		if err := fixedVolumeAssertAbsent(namespace, fixedVolumeBackingName); err != nil {
			return err
		}
		receipt.BackingRemoved = true
		return nil
	}
	backing, err := fixedVolumeOpenAt(namespace, fixedVolumeBackingName, unix.O_RDONLY, 0)
	if errors.Is(err, unix.ENOENT) && receipt.Stage == "unlink-intent" {
		receipt.BackingRemoved, receipt.Stage = true, "backing-removed"
		return p.writeReceipt(name, *receipt, false)
	}
	if err != nil {
		return err
	}
	if err = fixedVolumeCheckBacking(backing, receipt.Identity, receipt.Identity.BackingMapSHA256 != ""); err != nil {
		_ = backing.Close()
		return err
	}
	var stat unix.Stat_t
	if err = unix.Fstat(int(backing.Fd()), &stat); err != nil || stat.Size < 0 || stat.Size > receipt.Bytes ||
		stat.Blocks < 0 || stat.Blocks > math.MaxInt64/512 || stat.Blocks*512 > receipt.Identity.BackingAllowanceBytes {
		_ = backing.Close()
		return errFixedVolumeUnsafe
	}
	receipt.Stage = "unlink-intent"
	if err = p.writeReceipt(name, *receipt, false); err != nil {
		_ = backing.Close()
		return err
	}
	var named unix.Stat_t
	if err = unix.Fstatat(int(namespace.Fd()), fixedVolumeBackingName, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
		!fixedVolumeSameInode(named, stat) || named.Nlink != 1 {
		_ = backing.Close()
		return errFixedVolumeUnsafe
	}
	if err = unix.Unlinkat(int(namespace.Fd()), fixedVolumeBackingName, 0); err != nil {
		_ = backing.Close()
		return err
	}
	closeErr := backing.Close()
	if err = errors.Join(closeErr, namespace.Sync()); err != nil {
		return err
	}
	receipt.BackingRemoved, receipt.Stage = true, "backing-removed"
	return p.writeReceipt(name, *receipt, false)
}

func (p *linuxFixedVolumeProvider) retireMountpoint(ctx context.Context, name string, namespace *os.File, receipt *fixedVolumeDiskReceipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if receipt.Identity.MountpointInode == 0 || receipt.MountpointRemoved {
		if err := fixedVolumeAssertAbsent(namespace, fixedVolumeWorkspaceName); err != nil {
			return err
		}
		receipt.MountpointRemoved = true
		return nil
	}
	mountpoint, err := fixedVolumeOpenOwnedDirectory(namespace, fixedVolumeWorkspaceName,
		receipt.Identity.MountpointDevice, receipt.Identity.MountpointInode)
	if errors.Is(err, unix.ENOENT) && receipt.Stage == "mountpoint-remove-intent" {
		receipt.MountpointRemoved, receipt.Stage = true, "mountpoint-removed"
		return p.writeReceipt(name, *receipt, false)
	}
	if err != nil {
		return err
	}
	if err = fixedVolumeAssertEmpty(mountpoint); err != nil {
		_ = mountpoint.Close()
		return err
	}
	if err = mountpoint.Close(); err != nil {
		return err
	}
	receipt.Stage = "mountpoint-remove-intent"
	if err = p.writeReceipt(name, *receipt, false); err != nil {
		return err
	}
	if err = unix.Unlinkat(int(namespace.Fd()), fixedVolumeWorkspaceName, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	if err = namespace.Sync(); err != nil {
		return err
	}
	receipt.MountpointRemoved, receipt.Stage = true, "mountpoint-removed"
	return p.writeReceipt(name, *receipt, false)
}

func fixedVolumeCleanupProof(receipt fixedVolumeDiskReceipt) [32]byte {
	// Digest all exact pre-deletion identities and the ordered cleanup result.
	// The digest is an audit transcript; the ledger registers the minted receipt
	// pointer instead of treating a nonzero digest as an independent authority.
	receipt.Stage, receipt.CleanupProof = "retired", ""
	content, _ := json.Marshal(receipt)
	return sha256.Sum256(append([]byte("goby-fixed-volume-cleanup-v1\n"), content...))
}

func (p *linuxFixedVolumeProvider) finishRetirement(name string, receipt fixedVolumeDiskReceipt, permit *storageRetirePermit) (*storageRetirementReceipt, error) {
	if !receipt.BackingRemoved || !receipt.MountpointRemoved || fixedVolumeAssertAbsent(p.root, name) != nil {
		return nil, errFixedVolumeRetained
	}
	receipt.Stage = "retired"
	proof := fixedVolumeCleanupProof(receipt)
	receipt.CleanupProof = hex.EncodeToString(proof[:])
	if err := p.writeReceipt(name, receipt, false); err != nil {
		return nil, err
	}
	return p.mintRetirement(permit, receipt.Identity, proof)
}

func (p *linuxFixedVolumeProvider) mintRetirement(permit *storageRetirePermit, cleanupIdentity fixedVolumeIdentity, proof [32]byte) (*storageRetirementReceipt, error) {
	receipt := &storageRetirementReceipt{permit: permit, identity: permit.identity,
		cleanupIdentity: cleanupIdentity, providerToken: p.config.ExpectedRootToken, proof: proof}
	if err := permit.acceptRetirementReceipt(receipt); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (p *linuxFixedVolumeProvider) checkRetiredResources(name string, identity fixedVolumeIdentity) error {
	if identity.ID != name || identity.ProviderToken != p.config.ExpectedRootToken ||
		identity.ProvisionRootDevice != uint64(p.rootStat.Dev) || identity.ProvisionRootInode != p.rootStat.Ino ||
		identity.LedgerRootDevice != p.journal.device || identity.LedgerRootInode != p.journal.inode ||
		identity.BootID != p.bootID || identity.MountNamespaceDevice != uint64(p.mountNS.Dev) || identity.MountNamespaceInode != p.mountNS.Ino {
		return errFixedVolumeUnsafe
	}
	if err := fixedVolumeAssertAbsent(p.root, name); err != nil {
		return err
	}
	if identity.LoopDevice == 0 {
		return nil
	}
	loop, err := fixedVolumeOpenLoop(identity.LoopNumber, identity.LoopDevice)
	if err != nil {
		return err
	}
	defer loop.Close()
	status, err := unix.IoctlLoopGetStatus64(int(loop.Fd()))
	if errors.Is(err, unix.ENXIO) {
		return nil
	}
	if err != nil || status.Device == identity.BackingDevice && status.Inode == identity.BackingInode {
		return fmt.Errorf("%w: original loop allocation remains or cannot be inspected", errFixedVolumeRetained)
	}
	// A completed durable detach permits later reuse by another trusted owner.
	// This provider never detaches or modifies that unrelated backing domain.
	return nil
}

func (p *linuxFixedVolumeProvider) AcknowledgeRetirement(ctx context.Context, receipt *storageRetirementReceipt) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p.mu.Lock()
	defer p.mu.Unlock()
	if receipt == nil || receipt.permit == nil || receipt.permit.ledger != p.config.Ledger || !receipt.permit.completed(receipt) ||
		receipt.providerToken != p.config.ExpectedRootToken || receipt.proof == ([32]byte{}) {
		return fmt.Errorf("%w: durable ledger retirement confirmation is required", errFixedVolumeUnsafe)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.checkRoot(); err != nil {
		return err
	}
	permit := receipt.permit
	if permit.providerRootDevice != uint64(p.rootStat.Dev) || permit.providerRootInode != p.rootStat.Ino ||
		permit.providerToken != p.config.ExpectedRootToken {
		return errFixedVolumeUnsafe
	}
	name, err := fixedVolumeNamespaceName(permit.id, permit.serial)
	if err != nil {
		return err
	}
	disk, stat, readErr := p.readReceipt(name)
	if readErr != nil {
		cleanupIdentity := permit.confirmedCleanupIdentity()
		if cleanupIdentity == (fixedVolumeIdentity{}) || permit.confirmedProof() != receipt.proof ||
			fixedVolumeAssertAbsent(p.root, name+".json") != nil || fixedVolumeAssertAbsent(p.root, name+".next") != nil {
			return readErr
		}
		if err = p.checkRetiredResources(name, cleanupIdentity); err != nil {
			return err
		}
		if err = p.root.Sync(); err != nil {
			return err
		}
		receipt.acknowledged.Store(true)
		return nil
	}
	if disk.ID != permit.id || disk.Serial != permit.serial || disk.Stage != "retired" ||
		disk.DeleteRevision != permit.revision || disk.RetirementIdentity != permit.identity ||
		!disk.BackingRemoved || !disk.MountpointRemoved || fixedVolumeCleanupProof(disk) != receipt.proof ||
		disk.CleanupProof != hex.EncodeToString(receipt.proof[:]) {
		return errFixedVolumeUnsafe
	}
	if err = p.checkRetiredResources(name, disk.Identity); err != nil {
		return err
	}
	var current unix.Stat_t
	if err = unix.Fstatat(int(p.root.Fd()), name+".json", &current, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
		!fixedVolumeSameInode(current, stat) || current.Nlink != 1 {
		return errFixedVolumeUnsafe
	}
	if err = unix.Unlinkat(int(p.root.Fd()), name+".json", 0); err != nil {
		return err
	}
	if err = p.root.Sync(); err != nil {
		return err
	}
	receipt.acknowledged.Store(true)
	return nil
}

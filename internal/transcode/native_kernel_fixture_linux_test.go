//go:build linux

package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/commanddomain"
	"golang.org/x/sys/unix"
)

type nativeKernelPreparedConfig struct {
	Volume fixedVolumeConfig
	Pool   fixedBackingPoolConfig
	Limits storageReservationLimits
	Native nativeKernelPreparedPolicy
}

type nativeKernelPreparedPolicy struct {
	Version        int
	ParentIdentity commanddomain.Identity
	ParentPath     string
	Launcher       commanddomain.ApprovedExecutable
	UID            uint32
	GID            uint32
	MaxCommands    int
	MaxTasks       int
}

type nativeKernelFixture struct {
	provider        fixedVolumeProvider
	ledger          *storageReservationLedger
	config          nativeKernelPreparedConfig
	parent          *os.File
	dataLease       *storageLease
	controlLease    *storageLease
	dataIdentity    fixedVolumeIdentity
	controlIdentity fixedVolumeIdentity
	data            *os.File
	control         *os.File
	dataPin         *storageReaderPin
	controlPin      *storageReaderPin
	store           *nativeKernelControl
	launcher        *fixedPoolNativeExecutable
	tool            *fixedPoolNativeExecutable
	toolApproval    commanddomain.ApprovedExecutable
	source          *os.File
	sourcePin       *storageReaderPin
	record          nativeKernelControlRecord
}

// This is an independent explicitly selected real kernel fixture. Neither this
// entry nor its JSON inventory can prepare a pool, borrow a guessed covered FD,
// mint native readiness or bypass the ordinary shared command owner.
func TestNativeKernelPrivilegedPreparedFixture(t *testing.T) {
	if os.Getenv("GOBY_NATIVE_POOL_KERNEL_TEST") != "1" {
		t.Skip("explicit root-owned native fixed-pool fixture is not enabled")
	}
	if runtime.GOARCH != "amd64" || os.Getuid() != 0 || os.Geteuid() != 0 {
		t.Fatal("native fixture requires actual Linux amd64 root")
	}
	configFile, err := fixedVolumeOpenAbsolute(os.Getenv("GOBY_FIXED_POOL_CONFIG"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer configFile.Close()
	var stat unix.Stat_t
	if err = fixedVolumeOwnedRegular(configFile, &stat); err != nil || stat.Mode&0o7777 != 0o400 || stat.Size != 16<<10 {
		t.Fatal("native fixture config must be its exact private fixed-size file")
	}
	flags, err := unix.IoctlGetInt(int(configFile.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil || flags&fixedVolumeImmutableFlag == 0 {
		t.Fatal("native fixture config must be immutable")
	}
	var config nativeKernelPreparedConfig
	decoder := json.NewDecoder(io.LimitReader(configFile, (16<<10)+1))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&config); err != nil {
		t.Fatal(err)
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatal("native fixture config has trailing data")
	}
	expectedLimits := storageReservationLimits{Bytes: 153288704, Inodes: 8272, VolumeBytes: 64 << 20, VolumeInodes: 4096,
		OwnerBytes: 1114112, OwnerInodes: 8, BaseBytes: 16842752, BaseInodes: 64, MaxLeases: 2, MaxPins: 4}
	if config.Native.Version != 1 || config.Native.UID != 65534 || config.Native.GID != 65534 || config.Native.MaxCommands != 1 || config.Native.MaxTasks != 32 ||
		config.Limits != expectedLimits || config.Pool.Expected.DeviceBytes != 256<<20 {
		t.Fatal("native fixture does not accept a larger book, command or data domain")
	}
	covered, err := nativeKernelBorrowedFD("GOBY_FIXED_POOL_COVERED_FD")
	if err != nil {
		t.Fatal(err)
	}
	defer covered.Close()
	config.Pool.CoveredMountpoint = covered
	parent, err := nativeKernelBorrowedFD("GOBY_NATIVE_CGROUP_PARENT_FD")
	if err != nil {
		t.Fatal(err)
	}
	// The setup owner independently owns this original inherited descriptor.
	// Native construction duplicates it; this test closes its receiver copy only.
	defer parent.Close()
	if err = nativeKernelCheckParent(parent, config.Native); err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, io.NewSectionReader(configFile, 0, stat.Size)); err != nil {
		t.Fatal(err)
	}
	configSHA := hex.EncodeToString(hash.Sum(nil))
	allocated, err := fixedPoolAllocatedBytes(stat)
	if err != nil {
		t.Fatal(err)
	}
	config.Pool.OutsideStatic = append(config.Pool.OutsideStatic, fixedBackingPoolStaticObject{Path: os.Getenv("GOBY_FIXED_POOL_CONFIG"), Device: stat.Dev, Inode: stat.Ino, AllocatedBytes: allocated, SHA256: configSHA})
	binary, binaryPath, binarySHA := fixedPoolPreparedFixtureExecutable(t, config.Pool, config.Volume.ExpectedRootToken, configSHA)
	defer binary.Close()
	launcherFile, err := fixedVolumeOpenAbsolute(config.Native.Launcher.Path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer launcherFile.Close()
	journal, err := openStorageReservationJournal(config.Pool.PoolRoot+"/journal", config.Volume.ExpectedRootToken)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := newStorageReservationLedger(config.Limits, journal)
	if err != nil {
		_ = journal.close()
		t.Fatal(err)
	}
	defer ledger.close()
	config.Volume.Ledger = ledger
	var launcherCapability, toolCapability *fixedPoolExecutableCapability
	provider, err := openPooledFixedVolumeProviderPrepared(config.Volume, config.Pool, func(preparation *fixedPoolExecutablePreparation) error {
		var approveErr error
		toolCapability, approveErr = preparation.approve(binary, binaryPath, binarySHA)
		if approveErr != nil {
			return approveErr
		}
		launcherCapability, approveErr = preparation.approve(launcherFile, config.Native.Launcher.Path, config.Native.Launcher.SHA256)
		return approveErr
	})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	owner := &nativeKernelFixture{provider: provider, ledger: ledger, config: config, parent: parent,
		toolApproval: commanddomain.ApprovedExecutable{Path: binaryPath, SHA256: binarySHA}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err = owner.run(ctx, launcherCapability, toolCapability); err != nil {
		t.Fatalf("actual native fixture failed; original receipts, pins and full leases must be retained: %v", err)
	}
	if err = provider.Close(); err != nil {
		t.Fatal(err)
	}
	if err = nativeKernelCheckParent(parent, config.Native); err != nil {
		t.Fatal(err)
	}
	t.Log("NATIVE_CASES_V1 copier buffered policy cancel: actual joins, exact domain removal and both full lease acknowledgements completed")
}

func nativeKernelBorrowedFD(key string) (*os.File, error) {
	fd, err := strconv.ParseUint(os.Getenv(key), 10, 32)
	if err != nil || fd < 3 {
		return nil, errFixedVolumeUnsafe
	}
	file := os.NewFile(uintptr(fd), "borrowed-native-fixture-input")
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
	if err != nil {
		return nil, err
	}
	// This explicit receiving setup action is independent from any callee's
	// borrowing. Passing the FD to this root process does not pass it to tools.
	if _, err = unix.FcntlInt(file.Fd(), unix.F_SETFD, flags|unix.FD_CLOEXEC); err != nil {
		return nil, err
	}
	return file, nil
}

func nativeKernelCheckParent(parent *os.File, policy nativeKernelPreparedPolicy) error {
	if parent == nil || policy.ParentIdentity.Device == 0 || policy.ParentIdentity.Inode == 0 || policy.ParentIdentity.MountID == 0 ||
		!filepath.IsAbs(policy.ParentPath) || filepath.Clean(policy.ParentPath) != policy.ParentPath {
		return commanddomain.ErrUnsafe
	}
	var stat unix.Stat_t
	var fs unix.Statfs_t
	var statx unix.Statx_t
	access, accessErr := unix.FcntlInt(parent.Fd(), unix.F_GETFL, 0)
	fdFlags, flagErr := unix.FcntlInt(parent.Fd(), unix.F_GETFD, 0)
	if accessErr != nil || access&unix.O_ACCMODE != unix.O_RDONLY || access&unix.O_PATH != 0 || flagErr != nil || fdFlags&unix.FD_CLOEXEC == 0 ||
		unix.Fstat(int(parent.Fd()), &stat) != nil || stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0o7777 != 0o700 || stat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		stat.Dev != policy.ParentIdentity.Device || stat.Ino != policy.ParentIdentity.Inode ||
		unix.Fstatfs(int(parent.Fd()), &fs) != nil || fs.Type != unix.CGROUP2_SUPER_MAGIC || fs.Flags&unix.ST_RDONLY != 0 ||
		unix.Statx(int(parent.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &statx) != nil ||
		statx.Mask&unix.STATX_MNT_ID == 0 || statx.Mnt_id != policy.ParentIdentity.MountID {
		return commanddomain.ErrUnsafe
	}
	named, err := fixedVolumeOpenAbsolute(policy.ParentPath, true)
	if err != nil {
		return err
	}
	defer named.Close()
	var observed unix.Stat_t
	if unix.Fstat(int(named.Fd()), &observed) != nil || !fixedVolumeSameInode(stat, observed) {
		return commanddomain.ErrUnsafe
	}
	version, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION, 0, 0, 0)
	if errno != 0 || version < commanddomain.RequiredLauncherLandlockABI {
		return commanddomain.ErrUnavailable
	}
	fd, err := unix.Openat(int(parent.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	scan := os.NewFile(uintptr(fd), "owned-native-parent-scan")
	defer scan.Close()
	names, err := scan.Readdirnames(4097)
	if err != nil && !errors.Is(err, io.EOF) || len(names) > 4096 {
		return commanddomain.ErrUnsafe
	}
	for _, name := range names {
		var child unix.Stat_t
		if unix.Fstatat(int(parent.Fd()), name, &child, unix.AT_SYMLINK_NOFOLLOW) != nil || child.Mode&unix.S_IFMT == unix.S_IFDIR {
			return commanddomain.ErrRetained
		}
	}
	for name, expected := range map[string]string{"cgroup.type": "domain", "cgroup.procs": ""} {
		file, openErr := fixedVolumeOpenAt(parent, name, unix.O_RDONLY, 0)
		if openErr != nil {
			return openErr
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 4097))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(data) > 4096 || strings.TrimSpace(string(data)) != expected {
			return commanddomain.ErrRetained
		}
	}
	file, err := fixedVolumeOpenAt(parent, "cgroup.subtree_control", unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > 4096 {
		return commanddomain.ErrUnsafe
	}
	pids := false
	for _, controller := range strings.Fields(string(data)) {
		if controller == "pids" {
			pids = true
		}
	}
	if !pids {
		return commanddomain.ErrUnavailable
	}
	for _, name := range []string{"pids.max", "cgroup.kill"} {
		var control unix.Stat_t
		if unix.Fstatat(int(parent.Fd()), name, &control, unix.AT_SYMLINK_NOFOLLOW) != nil || control.Uid != 0 || control.Mode&unix.S_IFMT != unix.S_IFREG {
			return commanddomain.ErrUnavailable
		}
	}
	return nil
}

func (owner *nativeKernelFixture) run(ctx context.Context, launcher, tool *fixedPoolExecutableCapability) error {
	var err error
	if owner.ledger.snapshot().RecoveryRequired {
		return ErrStorageRecoveryRequired
	}
	owner.dataLease, owner.dataIdentity, owner.dataPin, owner.data, err = owner.provision(ctx, "0123456789abcdef0123456789abcdef", 65534, 65534)
	if err != nil {
		return err
	}
	owner.controlLease, owner.controlIdentity, owner.controlPin, owner.control, err = owner.provision(ctx, "abcdef0123456789abcdef0123456789", 0, 0)
	if err != nil {
		return err
	}
	owner.store, err = newNativeKernelControl(owner.control, owner.controlIdentity)
	if err != nil {
		return err
	}
	owner.record = nativeKernelControlRecord{Version: 1, Token: owner.config.Volume.ExpectedRootToken, Stage: "native-reserved-intent",
		Data: owner.dataIdentity, Control: owner.controlIdentity, Parent: owner.config.Native.ParentIdentity, ParentPath: owner.config.Native.ParentPath,
		Launcher: owner.config.Native.Launcher, Tool: owner.toolApproval}
	if err = owner.store.persist(owner.record); err != nil {
		return err
	}
	owner.launcher, err = launcher.nativeExecutable()
	if err != nil {
		return err
	}
	owner.tool, err = tool.nativeExecutable()
	if err != nil {
		return err
	}
	for _, name := range []string{"copier", "buffered", "policy", "cancel"} {
		if err = owner.runCase(ctx, name); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if err = errors.Join(owner.tool.close(), owner.launcher.close()); err != nil {
		return err
	}
	owner.record.Stage = "all-native-and-consumer-owners-retired"
	if err = owner.store.persist(owner.record); err != nil {
		return err
	}
	if err = owner.data.Close(); err != nil {
		return err
	}
	if err = owner.dataPin.closeAfterDescriptors(); err != nil {
		return err
	}
	if err = owner.retire(ctx, owner.dataLease); err != nil {
		return err
	}
	owner.record.Stage = "data-retirement-acknowledged-control-final-intent"
	if err = owner.store.persist(owner.record); err != nil {
		return err
	}
	if err = owner.store.closeAfterWriters(); err != nil {
		return err
	}
	if err = owner.control.Close(); err != nil {
		return err
	}
	if err = owner.controlPin.closeAfterDescriptors(); err != nil {
		return err
	}
	if err = owner.retire(ctx, owner.controlLease); err != nil {
		return err
	}
	snapshot := owner.ledger.snapshot()
	if snapshot.Leases != 0 || snapshot.Readers != 0 || snapshot.ReservedBytes != owner.config.Limits.BaseBytes || snapshot.ReservedInodes != owner.config.Limits.BaseInodes {
		return ErrStorageUnsafe
	}
	return nil
}

func (owner *nativeKernelFixture) provision(ctx context.Context, id string, uid, gid uint32) (*storageLease, fixedVolumeIdentity, *storageReaderPin, *os.File, error) {
	lease, err := owner.ledger.reserve(id, owner.config.Volume.ExpectedRootToken)
	if err != nil {
		return nil, fixedVolumeIdentity{}, nil, nil, err
	}
	permit, err := lease.authorizeProvision()
	if err != nil {
		return lease, fixedVolumeIdentity{}, nil, nil, err
	}
	identity, err := owner.provider.Provision(ctx, permit)
	if err != nil {
		return lease, identity, nil, nil, err
	}
	if err = lease.activate(identity); err != nil {
		return lease, identity, nil, nil, err
	}
	pin, err := lease.pin()
	if err != nil {
		return lease, identity, nil, nil, err
	}
	workspace, err := os.Open(filepath.Join(owner.config.Volume.ProvisionRoot, identity.ID, fixedVolumeWorkspaceName))
	if err != nil {
		return lease, identity, pin, nil, err
	}
	if err = unix.Fchown(int(workspace.Fd()), int(uid), int(gid)); err != nil {
		return lease, identity, pin, workspace, err
	}
	if err = unix.Fchmod(int(workspace.Fd()), 0o700); err != nil {
		return lease, identity, pin, workspace, err
	}
	if err = nativeKernelCheckWorkspace(workspace, identity, uid, gid); err != nil {
		return lease, identity, pin, workspace, err
	}
	return lease, identity, pin, workspace, nil
}

func (owner *nativeKernelFixture) retire(ctx context.Context, lease *storageLease) error {
	if err := lease.writersDrained(); err != nil {
		return err
	}
	if err := lease.terminalHandled(true); err != nil {
		return err
	}
	permit, err := lease.beginRetirement()
	if err != nil {
		return err
	}
	receipt, err := owner.provider.Retire(ctx, permit)
	if err != nil {
		return err
	}
	if err = lease.confirmRetirement(receipt); err != nil {
		return err
	}
	if err = lease.completeRetirement(receipt); !errors.Is(err, ErrStorageUnsafe) {
		return fmt.Errorf("native unacknowledged receipt returned a lease: %w", ErrStorageUnsafe)
	}
	if err = owner.provider.AcknowledgeRetirement(ctx, receipt); err != nil {
		return err
	}
	return lease.completeRetirement(receipt)
}

func (owner *nativeKernelFixture) newDomain(ctx context.Context, name string) (*commanddomain.Domain, error) {
	if err := nativeKernelCheckParent(owner.parent, owner.config.Native); err != nil {
		return nil, err
	}
	owner.record.Case, owner.record.Stage = name, "native-constructor-intent"
	owner.record.Domain, owner.record.Pipe = commanddomain.Snapshot{}, commanddomain.TemplatePipeSnapshot{}
	if err := owner.store.persist(owner.record); err != nil {
		return nil, err
	}
	domain, err := commanddomain.NewWithExecutableCapabilities(commanddomain.Config{Enabled: true, Parent: owner.parent,
		ParentIdentity: owner.config.Native.ParentIdentity, Workspace: owner.data,
		WorkspaceIdentity: commanddomain.Identity{Device: owner.dataIdentity.RootDevice, Inode: 2, MountID: owner.dataIdentity.MountID},
		Launcher:          owner.config.Native.Launcher, Tools: []commanddomain.ApprovedExecutable{owner.toolApproval},
		UID: 65534, GID: 65534, Groups: []uint32{}, MaxCommands: 1, MaxTasks: 32, Hardware: "none"},
		owner.launcher.state.native, []*commanddomain.ExecutableCapability{owner.tool.state.native})
	if domain != nil {
		owner.record.Domain = domain.Snapshot()
	}
	if err != nil {
		return domain, err
	}
	owner.record.Stage = "native-constructor-returned-start-intent"
	if err = owner.store.persist(owner.record); err != nil {
		return domain, err
	}
	return domain, nil
}

func (owner *nativeKernelFixture) runCase(ctx context.Context, name string) error {
	domain, err := owner.newDomain(ctx, name)
	if err != nil {
		return err
	}
	charge := owner.ledger.snapshot()
	switch name {
	case "copier":
		output := &nativeKernelBoundedOutput{}
		command := owner.command("sentinel")
		command.Stdout, command.Stderr = output, output
		if err = domain.Run(ctx, command); err != nil {
			return fmt.Errorf("actual native stream copier Wait: %w: %s", err, output.text())
		}
		if output.text() != nativeKernelToolSentinel {
			return errors.New("actual native copier lost its exact sentinel tail")
		}
	case "buffered":
		err = owner.bufferedCase(ctx, domain, charge)
	case "policy":
		err = owner.policyCase(ctx, domain)
	case "cancel":
		err = owner.cancelCase(ctx, domain, charge)
	default:
		return commanddomain.ErrUnsafe
	}
	if err != nil {
		return err
	}
	owner.record.Stage = "actual-command-and-consumer-joins-before-domain-retirement"
	owner.record.Domain = domain.Snapshot()
	if err = owner.store.persist(owner.record); err != nil {
		return err
	}
	retireCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = domain.Retire(retireCtx); err != nil {
		return err
	}
	snapshot := domain.Snapshot()
	if !snapshot.Removed || snapshot.Active != 0 || snapshot.Quarantined {
		return commanddomain.ErrRetained
	}
	if err = nativeKernelCheckParent(owner.parent, owner.config.Native); err != nil {
		return err
	}
	if owner.source != nil {
		data, readErr := io.ReadAll(io.NewSectionReader(owner.source, 0, 512))
		if readErr != nil || string(data) != nativeKernelToolOutsideInitial {
			return errors.New("native source sentinel changed before complete domain retirement")
		}
		if err = owner.source.Close(); err != nil {
			return err
		}
		if err = owner.sourcePin.closeAfterDescriptors(); err != nil {
			return err
		}
		owner.source, owner.sourcePin = nil, nil
	}
	owner.record.Stage, owner.record.Domain = "native-domain-actually-retired", snapshot
	owner.record.Completed = append(owner.record.Completed, name)
	return owner.store.persist(owner.record)
}

func (owner *nativeKernelFixture) command(mode string, values ...string) *exec.Cmd {
	arguments := []string{"-test.run=^TestNativeKernelToolHelper$", "--", nativeKernelToolSelector, mode}
	arguments = append(arguments, values...)
	return exec.Command(owner.toolApproval.Path, arguments...)
}

func (owner *nativeKernelFixture) bufferedCase(ctx context.Context, domain *commanddomain.Domain, charge storageReservationSnapshot) error {
	command := owner.command("sentinel")
	diagnostic := &nativeKernelBoundedOutput{}
	command.Stderr = diagnostic
	pipes, err := commanddomain.NewTemplatePipes(command, true, false)
	if err != nil {
		return err
	}
	if err = pipes.Start(ctx, domain); err != nil {
		if pipes.Snapshot().Started {
			err = errors.Join(err, pipes.Wait())
		}
		return err
	}
	if err = pipes.Wait(); err != nil {
		return fmt.Errorf("native buffered child Wait: %w: %s", err, diagnostic.text())
	}
	if err = pipes.Close(); !errors.Is(err, commanddomain.ErrRetained) {
		return errors.New("child Wait discarded an unconsumed native pipe")
	}
	if err = owner.tool.close(); !errors.Is(err, errFixedExecutableRetained) {
		return errors.New("native executable borrow returned before actual domain retirement")
	}
	readDone, release, joined := make(chan error, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		joined <- pipes.ConsumeStdout(func(reader io.Reader) error {
			data, readErr := io.ReadAll(io.LimitReader(reader, 513))
			if readErr == nil && string(data) != nativeKernelToolSentinel {
				readErr = errors.New("native buffered tail changed")
			}
			readDone <- readErr
			<-release
			return readErr
		})
	}()
	select {
	case err = <-readDone:
	case <-ctx.Done():
		return commanddomain.ErrRetained
	}
	if err != nil {
		close(release)
		<-joined
		return err
	}
	if err = pipes.Close(); !errors.Is(err, commanddomain.ErrRetained) {
		close(release)
		<-joined
		return errors.New("native callback ownership returned at read EOF")
	}
	if current := owner.ledger.snapshot(); current.ReservedBytes != charge.ReservedBytes || current.ReservedInodes != charge.ReservedInodes || current.Leases != 2 {
		close(release)
		<-joined
		return ErrStorageUnsafe
	}
	close(release)
	if err = <-joined; err != nil {
		return err
	}
	if err = pipes.Close(); err != nil {
		return err
	}
	owner.record.Pipe = pipes.Snapshot()
	return nil
}

func (owner *nativeKernelFixture) policyCase(ctx context.Context, domain *commanddomain.Domain) error {
	fd, err := unix.Openat(int(owner.control.Fd()), "outside-write-sentinel", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	created := os.NewFile(uintptr(fd), "owned-native-policy-sentinel")
	if err = unix.Fchown(fd, 65534, 65534); err != nil {
		return err
	}
	if _, err = created.WriteString(nativeKernelToolOutsideInitial); err != nil {
		return err
	}
	if err = created.Sync(); err != nil {
		return err
	}
	if err = created.Close(); err != nil {
		return err
	}
	if err = owner.control.Sync(); err != nil {
		return err
	}
	pin, err := owner.controlLease.pin()
	if err != nil {
		return err
	}
	file, err := fixedVolumeOpenAt(owner.control, "outside-write-sentinel", unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	owner.source, owner.sourcePin = file, pin
	var stat unix.Stat_t
	if err = unix.Fstat(int(file.Fd()), &stat); err != nil {
		return err
	}
	flags, flagErr := unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS)
	access, accessErr := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	if flagErr != nil || flags&fixedVolumeImmutableFlag != 0 || accessErr != nil || access&unix.O_ACCMODE != unix.O_RDONLY ||
		stat.Dev != owner.controlIdentity.RootDevice || stat.Uid != 65534 || stat.Gid != 65534 || stat.Mode&0o7777 != 0o600 || stat.Nlink != 1 {
		return commanddomain.ErrUnsafe
	}
	values := []string{strconv.FormatUint(owner.dataIdentity.RootDevice, 10), "2", strconv.FormatUint(owner.dataIdentity.MountID, 10),
		strconv.FormatUint(stat.Dev, 10), strconv.FormatUint(stat.Ino, 10), strconv.FormatUint(owner.controlIdentity.MountID, 10)}
	command := owner.command("policy", values...)
	command.ExtraFiles = []*os.File{file}
	output := &nativeKernelBoundedOutput{}
	command.Stdout, command.Stderr = output, output
	if err = domain.Run(ctx, command); err != nil {
		return fmt.Errorf("native fixed-domain policy: %w: %s", err, output.text())
	}
	if output.text() != nativeKernelToolPolicyOK {
		return errors.New("native fixed-domain policy marker changed")
	}
	data, err := io.ReadAll(io.NewSectionReader(file, 0, 512))
	if err != nil || string(data) != nativeKernelToolOutsideInitial {
		return errors.New("native source sentinel was written outside the data domain")
	}
	// runCase retains this source and pin through actual whole-domain removal,
	// including any inherited descriptors held beyond the original leader.
	return nil
}

func (owner *nativeKernelFixture) cancelCase(ctx context.Context, domain *commanddomain.Domain, charge storageReservationSnapshot) error {
	command := owner.command("cancel")
	diagnostic := &nativeKernelBoundedOutput{}
	command.Stderr = diagnostic
	pipes, err := commanddomain.NewTemplatePipes(command, true, false)
	if err != nil {
		return err
	}
	if err = pipes.Start(ctx, domain); err != nil {
		if pipes.Snapshot().Started {
			err = errors.Join(err, pipes.Wait())
		}
		return err
	}
	ready, release, joined := make(chan error, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		joined <- pipes.ConsumeStdout(func(reader io.Reader) error {
			data := make([]byte, len(nativeKernelToolCancelReady))
			_, readErr := io.ReadFull(reader, data)
			if readErr == nil && string(data) != nativeKernelToolCancelReady {
				readErr = errors.New("native cancellation ready marker changed")
			}
			ready <- readErr
			<-release
			return readErr
		})
	}()
	select {
	case err = <-ready:
	case <-ctx.Done():
		return commanddomain.ErrRetained
	}
	if err != nil {
		close(release)
		<-joined
		return err
	}
	if err = pipes.Cancel(); err != nil {
		close(release)
		<-joined
		return err
	}
	waitErr := pipes.Wait()
	if !pipes.Snapshot().Joined || pipes.Snapshot().Unknown || waitErr == nil {
		close(release)
		<-joined
		return errors.New("native cancellation did not actually join its killed child")
	}
	if err = pipes.Close(); !errors.Is(err, commanddomain.ErrRetained) {
		close(release)
		<-joined
		return errors.New("cancellation closed an active native consumer")
	}
	if current := owner.ledger.snapshot(); current.ReservedBytes != charge.ReservedBytes || current.ReservedInodes != charge.ReservedInodes || current.Leases != 2 {
		close(release)
		<-joined
		return ErrStorageUnsafe
	}
	close(release)
	if err = <-joined; err != nil {
		return err
	}
	if err = pipes.Close(); err != nil {
		return err
	}
	owner.record.Pipe = pipes.Snapshot()
	return nil
}

type nativeKernelBoundedOutput struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (output *nativeKernelBoundedOutput) Write(value []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.data.Len() < 4096 {
		_, _ = output.data.Write(value[:min(len(value), 4096-output.data.Len())])
	}
	return len(value), nil
}

func (output *nativeKernelBoundedOutput) text() string {
	output.mu.Lock()
	defer output.mu.Unlock()
	return output.data.String()
}

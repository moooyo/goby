//go:build linux

package transcode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/commanddomain"
	"github.com/moooyo/goby/internal/media"
	"golang.org/x/sys/unix"
)

// The sole fixture owner stays reachable on every incomplete join or receipt.
// This registry supplies no native, storage, cleanup or readiness authority.
var nativeGatewayRetainedFixture struct {
	sync.Mutex
	owner *nativeGatewayKernelFixture
}

type nativeGatewayKernelFixture struct {
	base     *nativeKernelFixture
	scope    *commanddomain.CommandScope
	inputs   []*os.File
	launcher *fixedPoolExecutableCapability
	tool     *fixedPoolExecutableCapability
	pending  *nativeGatewayParserCaller
	legacy   atomic.Int32
}

type nativeGatewayParserCaller struct {
	readDone chan error
	eofDone  chan error
	release  chan struct{}
	result   chan nativeGatewayParserResult
	done     chan struct{}
	joined   bool
}

type nativeGatewayParserResult struct {
	parseErr error
	waitErr  error
	startErr error
	returned bool
}

// Select this entry exactly. The inherited prepared pool and cgroup inputs are
// the old fixture's actual bounded setup, never a request to provision a host.
func TestNativeGatewayPrivilegedPreparedFixture(t *testing.T) {
	if os.Getenv("GOBY_NATIVE_POOL_KERNEL_TEST") != "1" {
		t.Skip("explicit root-owned native fixed-pool fixture is not enabled")
	}
	if runtime.GOARCH != "amd64" || os.Getuid() != 0 || os.Geteuid() != 0 {
		t.Fatal("native gateway fixture requires actual Linux amd64 root")
	}
	owner := &nativeGatewayKernelFixture{base: &nativeKernelFixture{}}
	nativeGatewayRetainedFixture.Lock()
	if nativeGatewayRetainedFixture.owner != nil {
		nativeGatewayRetainedFixture.Unlock()
		t.Fatal("an earlier gateway fixture still retains its exact owners")
	}
	nativeGatewayRetainedFixture.owner = owner
	nativeGatewayRetainedFixture.Unlock()
	if err := owner.prepare(t); err != nil {
		t.Fatalf("native gateway setup failed; exact opened inputs remain retained: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := owner.run(ctx); err != nil {
		if owner.scope != nil {
			owner.scope.CloseGate()
		}
		if owner.base.store != nil {
			owner.base.record.Stage = "gateway-failed-original-owners-and-full-leases-retained"
			_ = owner.base.store.persist(owner.base.record)
		}
		t.Fatalf("actual native gateway fixture failed; original scope, callers, pins and full leases remain retained: %v", err)
	}
	nativeGatewayRetainedFixture.Lock()
	if nativeGatewayRetainedFixture.owner != owner {
		nativeGatewayRetainedFixture.Unlock()
		t.Fatal("native gateway retained-owner membership changed")
	}
	nativeGatewayRetainedFixture.owner = nil
	nativeGatewayRetainedFixture.Unlock()
	t.Log("NATIVE_GATEWAY_CASES_V1 copier buffered policy cancel: actual media joins, scope removal and both full lease acknowledgements completed")
}

func (owner *nativeGatewayKernelFixture) prepare(t *testing.T) error {
	configFile, err := fixedVolumeOpenAbsolute(os.Getenv("GOBY_FIXED_POOL_CONFIG"), false)
	if configFile != nil {
		owner.inputs = append(owner.inputs, configFile)
	}
	if err != nil {
		return err
	}
	var stat unix.Stat_t
	if err = fixedVolumeOwnedRegular(configFile, &stat); err != nil || stat.Mode&0o7777 != 0o400 || stat.Size != 16<<10 {
		return errors.New("native fixture config must be its exact private fixed-size file")
	}
	flags, err := unix.IoctlGetInt(int(configFile.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil || flags&fixedVolumeImmutableFlag == 0 {
		return errors.New("native fixture config must be immutable")
	}
	var config nativeKernelPreparedConfig
	decoder := json.NewDecoder(io.LimitReader(configFile, (16<<10)+1))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&config); err != nil {
		return err
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("native fixture config has trailing data")
	}
	expectedLimits := storageReservationLimits{Bytes: 153288704, Inodes: 8272, VolumeBytes: 64 << 20, VolumeInodes: 4096,
		OwnerBytes: 1114112, OwnerInodes: 8, BaseBytes: 16842752, BaseInodes: 64, MaxLeases: 2, MaxPins: 4}
	if config.Native.Version != 1 || config.Native.UID != 65534 || config.Native.GID != 65534 ||
		config.Native.MaxCommands != 1 || config.Native.MaxTasks != 32 || config.Limits != expectedLimits || config.Pool.Expected.DeviceBytes != 256<<20 {
		return errors.New("native fixture does not accept a larger book, command or data domain")
	}
	covered, err := nativeKernelBorrowedFD("GOBY_FIXED_POOL_COVERED_FD")
	if covered != nil {
		owner.inputs = append(owner.inputs, covered)
	}
	if err != nil {
		return err
	}
	config.Pool.CoveredMountpoint = covered
	parent, err := nativeKernelBorrowedFD("GOBY_NATIVE_CGROUP_PARENT_FD")
	if parent != nil {
		owner.inputs = append(owner.inputs, parent)
		owner.base.parent = parent
	}
	if err != nil {
		return err
	}
	if err = nativeKernelCheckParent(parent, config.Native); err != nil {
		return err
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, io.NewSectionReader(configFile, 0, stat.Size)); err != nil {
		return err
	}
	configSHA := hex.EncodeToString(hash.Sum(nil))
	allocated, err := fixedPoolAllocatedBytes(stat)
	if err != nil {
		return err
	}
	config.Pool.OutsideStatic = append(config.Pool.OutsideStatic, fixedBackingPoolStaticObject{Path: os.Getenv("GOBY_FIXED_POOL_CONFIG"),
		Device: stat.Dev, Inode: stat.Ino, AllocatedBytes: allocated, SHA256: configSHA})
	binary, binaryPath, binarySHA := fixedPoolPreparedFixtureExecutable(t, config.Pool, config.Volume.ExpectedRootToken, configSHA)
	owner.inputs = append(owner.inputs, binary)
	launcherFile, err := fixedVolumeOpenAbsolute(config.Native.Launcher.Path, false)
	if launcherFile != nil {
		owner.inputs = append(owner.inputs, launcherFile)
	}
	if err != nil {
		return err
	}
	journal, err := openStorageReservationJournal(config.Pool.PoolRoot+"/journal", config.Volume.ExpectedRootToken)
	if err != nil {
		return err
	}
	ledger, err := newStorageReservationLedger(config.Limits, journal)
	if err != nil {
		return errors.Join(err, journal.close())
	}
	owner.base.ledger = ledger
	config.Volume.Ledger = ledger
	owner.base.config = config
	owner.base.toolApproval = commanddomain.ApprovedExecutable{Path: binaryPath, SHA256: binarySHA}
	provider, err := openPooledFixedVolumeProviderPrepared(config.Volume, config.Pool, func(preparation *fixedPoolExecutablePreparation) error {
		var approveErr error
		owner.tool, approveErr = preparation.approve(binary, binaryPath, binarySHA)
		if approveErr != nil {
			return approveErr
		}
		owner.launcher, approveErr = preparation.approve(launcherFile, config.Native.Launcher.Path, config.Native.Launcher.SHA256)
		return approveErr
	})
	owner.base.provider = provider
	return err
}

func (owner *nativeGatewayKernelFixture) run(ctx context.Context) error {
	base := owner.base
	var err error
	if base.ledger.snapshot().RecoveryRequired {
		return ErrStorageRecoveryRequired
	}
	base.dataLease, base.dataIdentity, base.dataPin, base.data, err = base.provision(ctx, "0123456789abcdef0123456789abcdef", 65534, 65534)
	if err != nil {
		return err
	}
	base.controlLease, base.controlIdentity, base.controlPin, base.control, err = base.provision(ctx, "abcdef0123456789abcdef0123456789", 0, 0)
	if err != nil {
		return err
	}
	base.store, err = newNativeKernelControl(base.control, base.controlIdentity)
	if err != nil {
		return err
	}
	base.record = nativeKernelControlRecord{Version: 1, Token: base.config.Volume.ExpectedRootToken, Stage: "gateway-whole-reservations-and-scope-constructor-intent",
		Data: base.dataIdentity, Control: base.controlIdentity, Parent: base.config.Native.ParentIdentity, ParentPath: base.config.Native.ParentPath,
		Launcher: base.config.Native.Launcher, Tool: base.toolApproval}
	if err = base.store.persist(base.record); err != nil {
		return err
	}
	base.launcher, err = owner.launcher.nativeExecutable()
	if err != nil {
		return err
	}
	base.tool, err = owner.tool.nativeExecutable()
	if err != nil {
		return err
	}
	owner.scope, err = commanddomain.NewCommandScope(commanddomain.Config{Enabled: true, Parent: base.parent,
		ParentIdentity: base.config.Native.ParentIdentity, Workspace: base.data,
		WorkspaceIdentity: commanddomain.Identity{Device: base.dataIdentity.RootDevice, Inode: 2, MountID: base.dataIdentity.MountID},
		Launcher:          base.config.Native.Launcher, Tools: []commanddomain.ApprovedExecutable{base.toolApproval},
		UID: 65534, GID: 65534, Groups: []uint32{}, MaxCommands: 1, MaxTasks: 32, Hardware: "none"},
		base.launcher.state.native, []*commanddomain.ExecutableCapability{base.tool.state.native})
	if err != nil {
		return err
	}
	bound := commanddomain.WithCommandScope(ctx, owner.scope)
	for _, name := range []string{"copier", "buffered", "policy", "cancel"} {
		if err = owner.runCase(bound, name); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if owner.legacy.Load() != 0 || len(base.record.Completed) != 4 {
		return errors.New("the gateway invoked legacy retirement or lost a case")
	}
	drainCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = owner.scope.Drain(drainCtx)
	cancel()
	if err != nil {
		return err
	}
	if snapshot := owner.scope.Snapshot(); !snapshot.Closed || snapshot.Active != 0 || snapshot.Quarantined {
		return commanddomain.ErrRetained
	}
	if err = nativeKernelCheckParent(base.parent, base.config.Native); err != nil {
		return err
	}
	if err = owner.fullCharge(); err != nil {
		return err
	}
	if err = owner.unchangedSource(); err != nil {
		return err
	}
	// Only actual gateway returns plus whole-scope Drain authorize these closes.
	if err = errors.Join(base.tool.close(), base.launcher.close()); err != nil {
		return err
	}
	if base.source != nil {
		if err = base.source.Close(); err != nil {
			return err
		}
		if err = base.sourcePin.closeAfterDescriptors(); err != nil {
			return err
		}
		base.source, base.sourcePin = nil, nil
	}
	base.record.Stage = "gateway-all-actual-consumers-joined-and-scope-drained"
	if err = base.store.persist(base.record); err != nil {
		return err
	}
	if err = base.data.Close(); err != nil {
		return err
	}
	if err = base.dataPin.closeAfterDescriptors(); err != nil {
		return err
	}
	if err = base.retire(ctx, base.dataLease); err != nil {
		return err
	}
	base.record.Stage = "data-retirement-acknowledged-control-final-intent"
	if err = base.store.persist(base.record); err != nil {
		return err
	}
	if err = base.store.closeAfterWriters(); err != nil {
		return err
	}
	if err = base.control.Close(); err != nil {
		return err
	}
	if err = base.controlPin.closeAfterDescriptors(); err != nil {
		return err
	}
	if err = base.retire(ctx, base.controlLease); err != nil {
		return err
	}
	snapshot := base.ledger.snapshot()
	if snapshot.Leases != 0 || snapshot.Readers != 0 || snapshot.ReservedBytes != base.config.Limits.BaseBytes || snapshot.ReservedInodes != base.config.Limits.BaseInodes {
		return ErrStorageUnsafe
	}
	if err = base.provider.Close(); err != nil {
		return err
	}
	if err = nativeKernelCheckParent(base.parent, base.config.Native); err != nil {
		return err
	}
	if err = base.ledger.close(); err != nil {
		return err
	}
	for index := len(owner.inputs) - 1; index >= 0; index-- {
		if owner.inputs[index] == nil {
			continue
		}
		if err = owner.inputs[index].Close(); err != nil {
			return err
		}
	}
	return nil
}

func (owner *nativeGatewayKernelFixture) runCase(ctx context.Context, name string) error {
	base := owner.base
	if err := nativeGatewayCheckMediaIdle(); err != nil {
		return err
	}
	if err := nativeKernelCheckParent(base.parent, base.config.Native); err != nil {
		return err
	}
	if err := owner.fullCharge(); err != nil {
		return err
	}
	base.record.Case, base.record.Stage = name, "gateway-command-and-actual-consumer-intent"
	// The gateway intentionally exposes no raw private Domain or pipe handle.
	// Zero diagnostics are not minted native join or removal evidence.
	base.record.Domain, base.record.Pipe = commanddomain.Snapshot{}, commanddomain.TemplatePipeSnapshot{}
	if err := base.store.persist(base.record); err != nil {
		return err
	}
	var err error
	switch name {
	case "copier":
		output := &nativeKernelBoundedOutput{}
		command := base.command("sentinel")
		command.Stdout, command.Stderr = output, output
		err = media.RunProcessWithRetirement(ctx, command, owner.legacyRetirement)
		if err == nil && output.text() != nativeKernelToolSentinel {
			err = errors.New("the media copier lost its exact sentinel tail")
		}
	case "buffered":
		err = owner.bufferedCase(ctx)
	case "policy":
		err = owner.policyCase(ctx)
	case "cancel":
		err = owner.cancelCase(ctx)
	default:
		err = commanddomain.ErrUnsafe
	}
	if err != nil {
		return err
	}
	if err = nativeGatewayCheckMediaIdle(); err != nil {
		return err
	}
	if owner.legacy.Load() != 0 {
		return errors.New("the actual gateway invoked a legacy retirement callback")
	}
	if snapshot := owner.scope.Snapshot(); snapshot.Active != 0 || snapshot.Quarantined || snapshot.GateClosed || snapshot.Closed {
		return commanddomain.ErrRetained
	}
	if err = nativeKernelCheckParent(base.parent, base.config.Native); err != nil {
		return err
	}
	if err = owner.fullCharge(); err != nil {
		return err
	}
	if err = owner.unchangedSource(); err != nil {
		return err
	}
	base.record.Stage = "gateway-actual-wrapper-returned-and-parent-checked-empty"
	base.record.Completed = append(base.record.Completed, name)
	return base.store.persist(base.record)
}

func (owner *nativeGatewayKernelFixture) legacyRetirement() error {
	owner.legacy.Add(1)
	return errors.New("legacy retirement must not be invoked for the native shared gateway")
}

func (owner *nativeGatewayKernelFixture) bufferedCase(ctx context.Context) error {
	pending := owner.newParserCaller()
	command := owner.base.command("sentinel")
	command.Stderr = &nativeKernelBoundedOutput{}
	owner.startParserCaller(ctx, command, pending, func(reader io.Reader) error {
		data, err := io.ReadAll(io.LimitReader(reader, 513))
		if err == nil && string(data) != nativeKernelToolSentinel {
			err = errors.New("the actual buffered gateway tail changed")
		}
		// Exact output is shorter than the limit: ReadAll returned only after
		// the actual owned pipe reported EOF, not a synthetic reader boundary.
		pending.readDone <- err
		<-pending.release
		return err
	})
	if err := owner.observeParserRead(ctx, pending.readDone); err != nil {
		return err
	}
	if err := owner.heldParserBoundary(pending); err != nil {
		return err
	}
	close(pending.release)
	result, err := owner.joinParserCaller(ctx, pending)
	if err != nil {
		return err
	}
	return errors.Join(result.parseErr, result.waitErr, result.startErr)
}

func (owner *nativeGatewayKernelFixture) cancelCase(ctx context.Context) error {
	owned, cancel := context.WithCancel(ctx)
	defer cancel()
	pending := owner.newParserCaller()
	command := owner.base.command("cancel")
	command.Stderr = &nativeKernelBoundedOutput{}
	owner.startParserCaller(owned, command, pending, func(reader io.Reader) error {
		marker := make([]byte, len(nativeKernelToolCancelReady))
		_, err := io.ReadFull(reader, marker)
		if err == nil && string(marker) != nativeKernelToolCancelReady {
			err = errors.New("the actual gateway READY marker changed")
		}
		pending.readDone <- err
		if err == nil {
			var tail []byte
			tail, err = io.ReadAll(io.LimitReader(reader, 513))
			if err == nil && len(tail) != 0 {
				err = errors.New("the killed gateway tool produced an unexpected tail")
			}
		}
		pending.eofDone <- err
		<-pending.release
		if err != nil {
			return err
		}
		// The existing wrapper must perform its real SignalCancel/abort path.
		// Context cancellation and observed EOF do not replace callback return.
		return context.Canceled
	})
	if err := owner.observeParserRead(ctx, pending.readDone); err != nil {
		return err
	}
	cancel()
	if err := owner.observeParserRead(ctx, pending.eofDone); err != nil {
		return err
	}
	if err := owner.heldParserBoundary(pending); err != nil {
		return err
	}
	close(pending.release)
	result, err := owner.joinParserCaller(ctx, pending)
	if err != nil {
		return err
	}
	if result.startErr != nil || !errors.Is(result.parseErr, context.Canceled) || result.waitErr == nil ||
		errors.Is(result.waitErr, media.ErrProcessRetirementUnknown) || errors.Is(result.waitErr, commanddomain.ErrRetained) ||
		errors.Is(result.waitErr, commanddomain.ErrWaitOwnership) {
		return fmt.Errorf("actual cancelled parser/child retirement failed: %w", errors.Join(result.parseErr, result.waitErr, result.startErr, commanddomain.ErrRetained))
	}
	return nil
}

func (owner *nativeGatewayKernelFixture) newParserCaller() *nativeGatewayParserCaller {
	pending := &nativeGatewayParserCaller{readDone: make(chan error, 1), eofDone: make(chan error, 1),
		release: make(chan struct{}), result: make(chan nativeGatewayParserResult, 1), done: make(chan struct{})}
	owner.pending = pending
	return pending
}

func (owner *nativeGatewayKernelFixture) startParserCaller(ctx context.Context, command *exec.Cmd, pending *nativeGatewayParserCaller, parse func(io.Reader) error) {
	// One existing fixture caller owns the synchronous media parser, its full
	// return, actual Wait/copiers and Close/leaf retirement. No raw handle leaks.
	go func() {
		defer close(pending.done)
		result := nativeGatewayParserResult{}
		defer func() {
			_ = recover()
			if !result.returned {
				result.waitErr = errors.Join(result.waitErr, media.ErrProcessRetirementUnknown, commanddomain.ErrRetained)
			}
			pending.result <- result
		}()
		result.parseErr, result.waitErr, result.startErr = media.RunProcessStdout(ctx, command, parse)
		result.returned = true
	}()
}

func (owner *nativeGatewayKernelFixture) observeParserRead(ctx context.Context, observed <-chan error) error {
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case err := <-observed:
		return err
	case <-ctx.Done():
		return errors.Join(ctx.Err(), commanddomain.ErrRetained)
	case <-timer.C:
		return commanddomain.ErrRetained
	}
}

func (owner *nativeGatewayKernelFixture) heldParserBoundary(pending *nativeGatewayParserCaller) error {
	select {
	case <-pending.result:
		return errors.New("the gateway returned before its actual parser callback returned")
	default:
	}
	// These counters observe actual media reservations, not process identity,
	// whole-cgroup retirement or storage authority.
	stats := media.GetProcessCapacityStats()
	if stats.Active != 1 || stats.Background != 0 || stats.Queued != 0 || stats.RetirementUnknown != 0 {
		return fmt.Errorf("the held parser lost its actual media reservation: %w", commanddomain.ErrRetained)
	}
	if snapshot := owner.scope.Snapshot(); snapshot.Active != 1 || snapshot.Quarantined || snapshot.GateClosed || snapshot.Closed {
		return commanddomain.ErrRetained
	}
	if err := owner.fullCharge(); err != nil {
		return err
	}
	// The synchronous parser has actually entered through successful native
	// Start and is blocked before returning. The leaf's registered executable
	// borrow cannot retire here; this assertion is not authorized by Snapshot.
	if err := owner.base.tool.close(); !errors.Is(err, errFixedExecutableRetained) {
		return errors.New("the actual native executable borrow returned while its parser remained active")
	}
	return nil
}

func (owner *nativeGatewayKernelFixture) joinParserCaller(ctx context.Context, pending *nativeGatewayParserCaller) (nativeGatewayParserResult, error) {
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case result := <-pending.result:
		select {
		case <-pending.done:
			pending.joined = true
		case <-ctx.Done():
			return result, errors.Join(ctx.Err(), commanddomain.ErrRetained)
		case <-timer.C:
			return result, commanddomain.ErrRetained
		}
		if !result.returned {
			return result, commanddomain.ErrRetained
		}
		return result, nil
	case <-ctx.Done():
		return nativeGatewayParserResult{}, errors.Join(ctx.Err(), commanddomain.ErrRetained)
	case <-timer.C:
		// This timeout publishes no join, releases no slot, and does not drop
		// the retained caller, scope, original source or full reservations.
		return nativeGatewayParserResult{}, commanddomain.ErrRetained
	}
}

func (owner *nativeGatewayKernelFixture) policyCase(ctx context.Context) error {
	base := owner.base
	fd, err := unix.Openat(int(base.control.Fd()), "outside-write-sentinel", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	created := os.NewFile(uintptr(fd), "owned-gateway-policy-sentinel")
	owner.inputs = append(owner.inputs, created)
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
	owner.inputs[len(owner.inputs)-1] = nil
	if err = base.control.Sync(); err != nil {
		return err
	}
	base.sourcePin, err = base.controlLease.pin()
	if err != nil {
		return err
	}
	base.source, err = fixedVolumeOpenAt(base.control, "outside-write-sentinel", unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	var stat unix.Stat_t
	var statx unix.Statx_t
	if err = unix.Fstat(int(base.source.Fd()), &stat); err != nil {
		return err
	}
	flags, flagErr := unix.IoctlGetInt(int(base.source.Fd()), unix.FS_IOC_GETFLAGS)
	access, accessErr := unix.FcntlInt(base.source.Fd(), unix.F_GETFL, 0)
	fdFlags, fdErr := unix.FcntlInt(base.source.Fd(), unix.F_GETFD, 0)
	if flagErr != nil || flags&fixedVolumeImmutableFlag != 0 || accessErr != nil || access&unix.O_ACCMODE != unix.O_RDONLY || access&unix.O_PATH != 0 ||
		fdErr != nil || fdFlags&unix.FD_CLOEXEC == 0 || stat.Dev != base.controlIdentity.RootDevice || stat.Uid != 65534 || stat.Gid != 65534 ||
		stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7777 != 0o600 || stat.Nlink != 1 ||
		unix.Statx(int(base.source.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID|unix.STATX_INO, &statx) != nil ||
		statx.Mask&(unix.STATX_MNT_ID|unix.STATX_INO) != unix.STATX_MNT_ID|unix.STATX_INO ||
		statx.Mnt_id != base.controlIdentity.MountID || statx.Ino != stat.Ino {
		return commanddomain.ErrUnsafe
	}
	values := []string{strconv.FormatUint(base.dataIdentity.RootDevice, 10), "2", strconv.FormatUint(base.dataIdentity.MountID, 10),
		strconv.FormatUint(stat.Dev, 10), strconv.FormatUint(stat.Ino, 10), strconv.FormatUint(base.controlIdentity.MountID, 10)}
	command := base.command("policy", values...)
	command.ExtraFiles = []*os.File{base.source}
	output := &nativeKernelBoundedOutput{}
	command.Stdout, command.Stderr = output, output
	if err = media.RunProcessWithRetirement(ctx, command, owner.legacyRetirement); err != nil {
		return fmt.Errorf("actual gateway fixed-domain policy: %w: %s", err, output.text())
	}
	if output.text() != nativeKernelToolPolicyOK {
		return errors.New("the actual gateway policy marker changed")
	}
	// The readonly source and control pin remain held through final scope Drain,
	// rather than being returned just because this command's wrapper returned.
	return owner.unchangedSource()
}

func (owner *nativeGatewayKernelFixture) unchangedSource() error {
	if owner.base.source == nil {
		return nil
	}
	data, err := io.ReadAll(io.NewSectionReader(owner.base.source, 0, 512))
	if err != nil || string(data) != nativeKernelToolOutsideInitial {
		return errors.New("the actual gateway source sentinel changed")
	}
	return nil
}

func (owner *nativeGatewayKernelFixture) fullCharge() error {
	snapshot := owner.base.ledger.snapshot()
	if snapshot.Leases != 2 || snapshot.ReservedBytes != owner.base.config.Limits.Bytes ||
		snapshot.ReservedInodes != owner.base.config.Limits.Inodes || snapshot.Fenced || snapshot.RecoveryRequired || snapshot.Closed {
		return ErrStorageUnsafe
	}
	return nil
}

func nativeGatewayCheckMediaIdle() error {
	stats := media.GetProcessCapacityStats()
	if stats.Active != 0 || stats.Background != 0 || stats.Queued != 0 || stats.RetirementUnknown != 0 {
		return errors.Join(media.ErrProcessRetirementUnknown, commanddomain.ErrRetained)
	}
	return nil
}

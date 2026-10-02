//go:build linux

package commanddomain

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type ownedExecutable struct {
	approval    ApprovedExecutable
	file        *os.File
	device      uint64
	inode       uint64
	capability  *ExecutableCapability
	borrowSlot  int
	borrowOwner *Domain
}

// Domain owns actual native controls, not a service-visible cgroup descriptor.
// Its root broker retains this object through any failed retirement. The
// enclosing broker must durably bind it to the already reserved volume before
// invoking New or Start; this primitive does not invent that durable binding.
type Domain struct {
	mu              sync.Mutex
	retireMu        sync.Mutex
	parent          *os.File
	group           *os.File
	control         *os.File
	workspace       *os.File
	launcher        ownedExecutable
	tools           []ownedExecutable
	executableUse   *ExecutableUse
	limitsClass     LimitsClass
	hardware        []*os.File
	config          Config
	name            string
	path            string
	identity        Identity
	parentIdentity  Identity
	bootID          string
	brokerPID       uint32
	mountNamespace  LauncherObjectIdentity
	records         []*Process
	attempt         *Process
	active          int
	closed          bool
	removed         bool
	quarantined     bool
	allJoined       chan struct{}
	allJoinedClosed bool
}

// A non-nil result with an error owns partial creation and must be retained.
// There is no privilege escalation, fallback hierarchy, ancestor modification,
// mount creation, numeric PID signaling or ordinary-service compatibility mode.
func New(config Config) (*Domain, error) {
	return newDomain(config, nil, nil, false)
}

// NewWithExecutableCapabilities preserves held-FD identity across packages.
// Approval paths are symbolic selectors only: no FD-mode failure falls back to
// a pathname open. Callers retain capabilities and external consumers until
// actual domain retirement and their own complete consumer joins have returned.
func NewWithExecutableCapabilities(config Config, launcher *ExecutableCapability, tools []*ExecutableCapability) (*Domain, error) {
	if launcher == nil || len(tools) != len(config.Tools) {
		return nil, ErrUnsafe
	}
	return newDomain(config, launcher, tools, true)
}

func newDomain(config Config, launcher *ExecutableCapability, tools []*ExecutableCapability, fromCapabilities bool) (*Domain, error) {
	if !config.Enabled || os.Geteuid() != 0 || runtimeUnsupported() {
		return nil, ErrUnavailable
	}
	abi, _, policyErr := unix.Syscall6(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION, 0, 0, 0)
	if policyErr != 0 || abi < RequiredLauncherLandlockABI {
		return nil, ErrUnavailable
	}
	if config.Parent == nil || config.Workspace == nil || config.MaxCommands < 1 ||
		config.MaxCommands > MaxDomainCommands || config.MaxTasks < config.MaxCommands || config.MaxTasks > 4096 ||
		len(config.Tools) < 1 || len(config.Tools) > 16 {
		return nil, ErrUnsafe
	}
	if config.Hardware == "" {
		config.Hardware = "none"
	}
	// Validate the same immutable wire policy before the first side effect.
	probe := LauncherConfig{Version: 1, ToolSHA256: config.Launcher.SHA256,
		Workspace: LauncherWorkspaceIdentity(config.WorkspaceIdentity), UID: config.UID, GID: config.GID,
		Groups: config.Groups, Hardware: config.Hardware, HardwareDevices: config.HardwareDevices,
		CgroupPath: "/probe", BrokerBootID: "11111111-1111-1111-1111-111111111111",
		MountNamespace: LauncherObjectIdentity{Device: 1, Inode: 1}, BrokerPID: uint32(os.Getpid()), Args: []string{"--help"}}
	if ValidateLauncherConfig(probe) != nil {
		return nil, ErrUnsafe
	}
	d := &Domain{config: config, records: make([]*Process, config.MaxCommands), allJoined: make(chan struct{})}
	d.brokerPID = uint32(os.Getpid())
	d.config.Tools = append([]ApprovedExecutable(nil), config.Tools...)
	d.config.Groups = append([]uint32(nil), config.Groups...)
	d.config.HardwareDevices = append([]LauncherHardwareDevice(nil), config.HardwareDevices...)
	fail := func(cause error) (*Domain, error) {
		if d.group == nil {
			if d.releaseDescriptors() != nil {
				return d, errors.Join(cause, ErrRetained)
			}
			return nil, cause
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.Retire(ctx); err != nil {
			return d, errors.Join(cause, err)
		}
		return nil, cause
	}
	var err error
	d.parent, err = duplicate(config.Parent)
	if err != nil {
		return fail(ErrUnsafe)
	}
	d.parentIdentity, err = directoryIdentity(d.parent)
	var parentStat unix.Stat_t
	var parentFS unix.Statfs_t
	if err != nil || d.parentIdentity != config.ParentIdentity ||
		unix.Fstat(int(d.parent.Fd()), &parentStat) != nil || parentStat.Uid != 0 || parentStat.Mode&0077 != 0 ||
		unix.Fstatfs(int(d.parent.Fd()), &parentFS) != nil || parentFS.Type != unix.CGROUP2_SUPER_MAGIC {
		return fail(ErrUnsafe)
	}
	d.workspace, err = duplicate(config.Workspace)
	if err != nil {
		return fail(ErrUnsafe)
	}
	workspaceIdentity, err := directoryIdentity(d.workspace)
	var workspaceStat unix.Stat_t
	var workspaceFS unix.Statfs_t
	if err != nil || workspaceIdentity != config.WorkspaceIdentity ||
		unix.Fstat(int(d.workspace.Fd()), &workspaceStat) != nil || workspaceStat.Ino != 2 ||
		workspaceStat.Uid != config.UID || workspaceStat.Gid != config.GID || workspaceStat.Mode&0777 != 0700 ||
		unix.Fstatfs(int(d.workspace.Fd()), &workspaceFS) != nil || workspaceFS.Type != unix.EXT4_SUPER_MAGIC ||
		workspaceFS.Flags&unix.ST_RDONLY != 0 {
		return fail(ErrUnsafe)
	}
	// The volume owner has already assigned this exact filesystem's data root.
	// This primitive never changes a deployment or source directory's ownership.
	if fromCapabilities {
		d.launcher, err = launcher.borrowExecutable(d, config.Launcher)
	} else {
		d.launcher, err = openExecutable(config.Launcher)
	}
	if err != nil {
		return fail(err)
	}
	for index, approval := range config.Tools {
		for _, existing := range d.tools {
			if existing.approval.Path == approval.Path {
				return fail(ErrUnsafe)
			}
		}
		var tool ownedExecutable
		var err error
		if fromCapabilities {
			tool, err = tools[index].borrowExecutable(d, approval)
		} else {
			tool, err = openExecutable(approval)
		}
		if tool.file != nil {
			d.tools = append(d.tools, tool)
		}
		if err != nil {
			return fail(err)
		}
	}
	for _, device := range config.HardwareDevices {
		file, err := openAbsolute(device.Path, unix.O_RDONLY)
		if err != nil {
			return fail(ErrUnsafe)
		}
		d.hardware = append(d.hardware, file)
		var stat unix.Stat_t
		mountID, mountErr := descriptorMountID(file)
		if unix.Fstat(int(file.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFCHR ||
			stat.Uid != 0 || stat.Dev != device.Device || stat.Ino != device.Inode || stat.Rdev != device.Rdev ||
			mountErr != nil || mountID != device.MountID {
			return fail(ErrUnsafe)
		}
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return fail(ErrUnsafe)
	}
	d.bootID = strings.TrimSpace(string(boot))
	var ns unix.Stat_t
	if unix.Stat("/proc/self/ns/mnt", &ns) != nil {
		return fail(ErrUnsafe)
	}
	d.mountNamespace = LauncherObjectIdentity{Device: ns.Dev, Inode: ns.Ino}
	parentPath, err := parentCgroupPath(d.parent, d.parentIdentity.MountID)
	if err != nil {
		return fail(err)
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return fail(err)
	}
	d.name = "goby-job-" + hex.EncodeToString(token[:])
	if unix.Mkdirat(int(d.parent.Fd()), d.name, 0700) != nil {
		return fail(ErrUnavailable)
	}
	// Capture the created named identity before opening it. A failed open must
	// retain the exact partial directory rather than pretending creation did not
	// happen or deleting a later path without its original identity.
	var created unix.Stat_t
	if unix.Fstatat(int(d.parent.Fd()), d.name, &created, unix.AT_SYMLINK_NOFOLLOW) != nil {
		d.quarantined, d.closed = true, true
		return d, ErrRetained
	}
	d.identity = Identity{Device: created.Dev, Inode: created.Ino, MountID: d.parentIdentity.MountID}
	fd, err := unix.Openat(int(d.parent.Fd()), d.name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		d.quarantined, d.closed = true, true
		return d, ErrRetained
	}
	d.group = os.NewFile(uintptr(fd), d.name)
	d.path = filepath.Join(parentPath, d.name)
	if d.checkIdentity() != nil {
		return fail(ErrUnsafe)
	}
	typeData, err := d.readControl("cgroup.type")
	if err != nil || strings.TrimSpace(string(typeData)) != "domain" || d.empty() != nil {
		return fail(ErrUnsafe)
	}
	if d.writeControl("pids.max", strconv.Itoa(config.MaxTasks)) != nil {
		return fail(ErrUnavailable)
	}
	maxData, err := d.readControl("pids.max")
	if err != nil || strings.TrimSpace(string(maxData)) != strconv.Itoa(config.MaxTasks) {
		return fail(ErrUnsafe)
	}
	kill, err := d.openControl("cgroup.kill", unix.O_WRONLY)
	if err != nil {
		return fail(ErrUnavailable)
	}
	if d.closeControl(kill) != nil {
		return fail(ErrRetained)
	}
	return d, nil
}

func runtimeUnsupported() bool { return runtime.GOARCH != "amd64" }

// Start serializes the real spawn with closure of the launch gate. Therefore a
// successful empty observation cannot race a registered yet-unstarted command.
// Only the approved launcher sees root credentials; it drops them and installs
// the inherited write/syscall policy before exec. It receives no control FDs.
func (d *Domain) Start(ctx context.Context, command *exec.Cmd) (result *Process, resultErr error) {
	// The caller's command is an immutable template. This owner creates its
	// private Cmd with the explicit context, so no external caller can reap or
	// call Wait on the owned leader before its stream copiers have joined.
	if d == nil || ctx == nil || command == nil || command.Process != nil {
		return nil, ErrUnsafe
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	returned := false
	defer func() {
		if !returned {
			// Keep the exact attempt, command and inherited descriptor objects
			// reachable even when Start's context or stream setup exits
			// abnormally. A partly spawned child is never an ordinary rollback.
			d.closed, d.quarantined = true, true
		}
	}()
	result, resultErr = d.startLocked(ctx, command)
	if d.attempt != nil {
		for _, file := range d.attempt.inherited {
			if file != nil {
				d.closed, d.quarantined = true, true
				resultErr = errors.Join(resultErr, ErrRetained)
				returned = true
				return result, resultErr
			}
		}
		d.attempt.inherited = nil
		d.attempt = nil
	}
	returned = true
	return result, resultErr
}

func (d *Domain) startLocked(ctx context.Context, command *exec.Cmd) (*Process, error) {
	if d.closed || d.removed || d.quarantined {
		return nil, ErrClosed
	}
	if d.checkIdentity() != nil {
		d.closed, d.quarantined = true, true
		return nil, ErrRetained
	}
	slot := -1
	for index, process := range d.records {
		if process == nil {
			slot = index
			break
		}
	}
	if slot < 0 {
		return nil, ErrCapacity
	}
	var tool *ownedExecutable
	if d.executableUse != nil {
		var err error
		tool, err = selectDomainExecutableUse(d, command)
		if err != nil {
			return nil, err
		}
	} else {
		for index := range d.tools {
			if command.Path == d.tools[index].approval.Path {
				tool = &d.tools[index]
				break
			}
		}
	}
	if tool == nil || command.SysProcAttr != nil && !plainProcessAttributes(command.SysProcAttr) ||
		len(command.Args) < 1 || len(command.Args) > 513 || len(command.ExtraFiles) > 32 {
		return nil, ErrUnsafe
	}
	argumentBytes := 0
	for _, argument := range command.Args[1:] {
		if len(argument) > 12*1024-argumentBytes {
			return nil, ErrUnsafe
		}
		argumentBytes += len(argument)
	}
	if checkExecutable(d.launcher) != nil || checkExecutable(*tool) != nil {
		d.closed, d.quarantined = true, true
		return nil, ErrRetained
	}
	process := &Process{domain: d, command: command, slot: slot}
	d.attempt = process
	config := LauncherConfig{Version: 1, ToolSHA256: tool.approval.SHA256,
		Workspace: LauncherWorkspaceIdentity(d.config.WorkspaceIdentity), UID: d.config.UID, GID: d.config.GID,
		Groups: d.config.Groups, Args: append([]string(nil), command.Args[1:]...), Hardware: d.config.Hardware,
		HardwareDevices: d.config.HardwareDevices, CgroupPath: d.path, BrokerPID: d.brokerPID, BrokerBootID: d.bootID, MountNamespace: d.mountNamespace}
	if d.executableUse != nil {
		var err error
		config.Version, config.Limits, err = launcherLimitsForClass(d.limitsClass)
		if err != nil {
			return nil, err
		}
	}
	var inherited []*os.File
	defer func() { process.inherited = inherited }()
	closeInherited := func() {
		for index, file := range inherited {
			if file != nil && file.Close() == nil {
				inherited[index] = nil
			}
		}
	}
	workspace, err := duplicate(d.workspace)
	if err != nil {
		return nil, ErrRetained
	}
	inherited = append(inherited, workspace)
	toolFD, err := duplicate(tool.file)
	if err != nil {
		closeInherited()
		return nil, ErrRetained
	}
	inherited = append(inherited, toolFD)
	for _, borrowed := range command.ExtraFiles {
		owned, err := duplicate(borrowed)
		if err != nil {
			closeInherited()
			return nil, ErrUnsafe
		}
		inherited = append(inherited, owned)
		payload, err := payloadRole(owned, d.config.WorkspaceIdentity.Device)
		if err != nil {
			closeInherited()
			return nil, err
		}
		config.Payload = append(config.Payload, payload)
	}
	for _, borrowed := range d.hardware {
		owned, err := duplicate(borrowed)
		if err != nil {
			closeInherited()
			return nil, ErrRetained
		}
		inherited = append(inherited, owned)
	}
	encoded, err := EncodeLauncherConfig(config)
	if err != nil {
		closeInherited()
		return nil, ErrUnsafe
	}
	configFD, err := sealedConfig(encoded)
	if err != nil {
		if configFD != nil {
			inherited = append(inherited, configFD)
		}
		closeInherited()
		return nil, ErrUnavailable
	}
	inherited = append([]*os.File{configFD}, inherited...)
	if validateStandardStreams(command, d.config.WorkspaceIdentity.Device) != nil {
		closeInherited()
		return nil, ErrUnsafe
	}
	// exec.Cmd remaps inherited files before execve. The executable therefore
	// needs an explicit final child descriptor, never the parent's FD number,
	// which could have become a payload or configuration role in the child.
	launcherFD, err := duplicate(d.launcher.file)
	if err != nil {
		closeInherited()
		return nil, ErrRetained
	}
	launcherChildFD := 3 + len(inherited)
	inherited = append(inherited, launcherFD)
	native := exec.CommandContext(ctx, "/proc/self/fd/"+strconv.Itoa(launcherChildFD))
	native.Args = []string{d.launcher.approval.Path}
	native.ExtraFiles = inherited
	native.Stdin, native.Stdout, native.Stderr = command.Stdin, command.Stdout, command.Stderr
	native.Dir = "/"
	native.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	native.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(d.group.Fd()), Pdeathsig: syscall.SIGKILL}
	native.Cancel = d.cancelCommand
	native.WaitDelay = command.WaitDelay
	if native.WaitDelay <= 0 || native.WaitDelay > 5*time.Second {
		native.WaitDelay = 5 * time.Second
	}
	process.command = native
	d.records[slot], d.active = process, d.active+1
	startErr := native.Start()
	closeInherited()
	if startErr != nil {
		d.records[slot], d.active = nil, d.active-1
		return nil, startErr
	}
	return process, nil
}

func plainProcessAttributes(attributes *syscall.SysProcAttr) bool {
	// Setpgid is the existing ordinary runner's only attribute. Every other
	// launcher attribute belongs to this root owner rather than its caller.
	copy := *attributes
	copy.Setpgid = false
	return reflect.DeepEqual(copy, syscall.SysProcAttr{})
}

func (d *Domain) cancelCommand() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.removed {
		return os.ErrProcessDone
	}
	d.closed = true
	d.closeJoinedIfEmpty()
	if d.checkIdentity() != nil || d.writeControl("cgroup.kill", "1") != nil {
		d.quarantined = true
		return ErrRetained
	}
	return nil
}

func (d *Domain) joined(process *Process) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if process.slot < 0 || process.slot >= len(d.records) || d.records[process.slot] != process {
		d.closed, d.quarantined = true, true
		return
	}
	d.records[process.slot] = nil
	d.active--
	d.closeJoinedIfEmpty()
}

func (d *Domain) abnormalWait(process *Process) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed, d.quarantined = true, true
	// The original record remains active: no successful Wait was observed.
	// An external recovery owner must retain its full durable reservation.
}

func (d *Domain) closeJoinedIfEmpty() {
	if d.closed && d.active == 0 && !d.allJoinedClosed {
		if d.allJoined == nil {
			d.quarantined = true
			return
		}
		close(d.allJoined)
		d.allJoinedClosed = true
	}
}

func (d *Domain) Snapshot() Snapshot {
	if d == nil {
		return Snapshot{Closed: true, Quarantined: true}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return Snapshot{Capacity: len(d.records), Active: d.active, Closed: d.closed,
		Removed: d.removed, Quarantined: d.quarantined, Identity: d.identity}
}

// Retire requires a bounded caller deadline. Failure closes starts and retains
// the same native object and its enclosing full reservation. It never lazy-
// removes a domain, signals an old numeric PID, or abandons an active waiter.
func (d *Domain) Retire(ctx context.Context) error {
	if d == nil {
		return ErrUnavailable
	}
	if ctx == nil {
		return ErrRetained
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return ErrRetained
	}
	if !d.retireMu.TryLock() {
		return ErrRetained
	}
	defer d.retireMu.Unlock()
	d.mu.Lock()
	if d.allJoined == nil || len(d.records) == 0 || d.identity.Device == 0 || d.identity.Inode == 0 ||
		d.identity.MountID == 0 || d.bootID == "" || d.mountNamespace.Device == 0 || d.mountNamespace.Inode == 0 {
		d.closed, d.quarantined = true, true
		d.mu.Unlock()
		return ErrRetained
	}
	d.closed = true
	d.closeJoinedIfEmpty()
	if d.removed {
		d.mu.Unlock()
		return d.releaseDescriptors()
	}
	if d.attempt != nil {
		// Unknown spawn or descriptor retirement needs the broker's durable
		// recovery owner. Never manufacture a successful join from this state.
		d.quarantined = true
		d.mu.Unlock()
		return ErrRetained
	}
	if d.checkIdentity() != nil || d.writeControl("cgroup.kill", "1") != nil {
		d.quarantined = true
		d.mu.Unlock()
		return ErrRetained
	}
	joined := d.allJoined
	d.mu.Unlock()
	select {
	case <-joined:
	case <-ctx.Done():
		return ErrRetained
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		d.mu.Lock()
		if d.checkIdentity() != nil {
			d.quarantined = true
			d.mu.Unlock()
			return ErrRetained
		}
		err := d.empty()
		if err == nil {
			if unix.Unlinkat(int(d.parent.Fd()), d.name, unix.AT_REMOVEDIR) != nil {
				d.quarantined = true
				d.mu.Unlock()
				return ErrRetained
			}
			d.removed = true
			d.mu.Unlock()
			return d.releaseDescriptors()
		}
		d.mu.Unlock()
		if !errors.Is(err, ErrCapacity) {
			return ErrRetained
		}
		select {
		case <-ctx.Done():
			return ErrRetained
		case <-ticker.C:
		}
	}
}

func duplicate(file *os.File) (*os.File, error) {
	if file == nil {
		return nil, ErrUnsafe
	}
	fd, err := unix.FcntlInt(file.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnsafe
	}
	return os.NewFile(uintptr(fd), "command-domain-owned"), nil
}

func openAbsolute(path string, flags int) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
		return nil, ErrUnsafe
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{Flags: uint64(flags | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return nil, ErrUnsafe
	}
	return os.NewFile(uintptr(fd), path), nil
}

func descriptorMountID(file *os.File) (uint64, error) {
	data, err := os.ReadFile("/proc/self/fdinfo/" + strconv.Itoa(int(file.Fd())))
	if err != nil || len(data) > 4096 {
		return 0, ErrUnsafe
	}
	var found uint64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "mnt_id:" {
			if found != 0 {
				return 0, ErrUnsafe
			}
			found, err = strconv.ParseUint(fields[1], 10, 64)
			if err != nil || found == 0 {
				return 0, ErrUnsafe
			}
		}
	}
	if found == 0 {
		return 0, ErrUnsafe
	}
	return found, nil
}

func directoryIdentity(file *os.File) (Identity, error) {
	var stat unix.Stat_t
	if unix.Fstat(int(file.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return Identity{}, ErrUnsafe
	}
	mountID, err := descriptorMountID(file)
	return Identity{Device: stat.Dev, Inode: stat.Ino, MountID: mountID}, err
}

func parentCgroupPath(parent *os.File, mountID uint64) (string, error) {
	path, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(int(parent.Fd())))
	if err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", ErrUnsafe
	}
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil || len(data) > 1024*1024 {
		return "", ErrUnsafe
	}
	found := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		id, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil || id != mountID {
			continue
		}
		separator := -1
		for index, field := range fields {
			if field == "-" {
				separator = index
				break
			}
		}
		if found != "" || separator < 6 || separator+3 >= len(fields) || fields[separator+1] != "cgroup2" ||
			strings.ContainsAny(fields[3]+fields[4], "\\") {
			return "", ErrUnsafe
		}
		relative, err := filepath.Rel(fields[4], path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, "../") {
			return "", ErrUnsafe
		}
		found = filepath.Join(fields[3], relative)
	}
	if found == "" || !filepath.IsAbs(found) {
		return "", ErrUnsafe
	}
	return found, nil
}

func openExecutable(approval ApprovedExecutable) (ownedExecutable, error) {
	var result ownedExecutable
	if len(approval.SHA256) != 64 || strings.ToLower(approval.SHA256) != approval.SHA256 {
		return result, ErrUnsafe
	}
	if _, err := hex.DecodeString(approval.SHA256); err != nil {
		return result, ErrUnsafe
	}
	file, err := openAbsolute(approval.Path, unix.O_RDONLY)
	if err != nil {
		return result, err
	}
	result = ownedExecutable{approval: approval, file: file}
	var stat unix.Stat_t
	if unix.Fstat(int(file.Fd()), &stat) != nil {
		if file.Close() != nil {
			return result, ErrRetained
		}
		return ownedExecutable{}, ErrUnsafe
	}
	result.device, result.inode = stat.Dev, stat.Ino
	if checkExecutable(result) != nil {
		if file.Close() != nil {
			return result, ErrRetained
		}
		return ownedExecutable{}, ErrUnsafe
	}
	return result, nil
}

func checkExecutable(tool ownedExecutable) error {
	if tool.capability != nil {
		return checkCapabilityExecutable(tool)
	}
	if tool.file == nil {
		return ErrUnsafe
	}
	var stat unix.Stat_t
	if unix.Fstat(int(tool.file.Fd()), &stat) != nil || stat.Dev != tool.device || stat.Ino != tool.inode ||
		stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Nlink != 1 || stat.Mode&0022 != 0 ||
		stat.Mode&0111 == 0 || stat.Mode&06000 != 0 || stat.Size <= 0 || stat.Size > 256*1024*1024 {
		return ErrUnsafe
	}
	size, err := unix.Fgetxattr(int(tool.file.Fd()), "security.capability", nil)
	if err != nil && !errors.Is(err, unix.ENODATA) || err == nil && size != 0 {
		return ErrUnsafe
	}
	var magic [4]byte
	if _, err := tool.file.ReadAt(magic[:], 0); err != nil || magic != [4]byte{0x7f, 'E', 'L', 'F'} {
		return ErrUnsafe
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, io.NewSectionReader(tool.file, 0, stat.Size)); err != nil || hex.EncodeToString(sum.Sum(nil)) != tool.approval.SHA256 {
		return ErrUnsafe
	}
	var after unix.Stat_t
	if unix.Fstat(int(tool.file.Fd()), &after) != nil || stat.Dev != after.Dev || stat.Ino != after.Ino ||
		stat.Mode != after.Mode || stat.Uid != after.Uid || stat.Gid != after.Gid || stat.Nlink != after.Nlink ||
		stat.Size != after.Size || stat.Mtim != after.Mtim || stat.Ctim != after.Ctim {
		return ErrUnsafe
	}
	return nil
}

func payloadRole(file *os.File, workspaceDevice uint64) (LauncherPayload, error) {
	var stat unix.Stat_t
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	if err != nil || unix.Fstat(int(file.Fd()), &stat) != nil {
		return LauncherPayload{}, ErrUnsafe
	}
	access := flags & unix.O_ACCMODE
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		if access == unix.O_RDONLY {
			return LauncherPayload{Role: "source", Writable: false}, nil
		}
		if (access == unix.O_WRONLY || access == unix.O_RDWR) && stat.Dev == workspaceDevice {
			return LauncherPayload{Role: "workspace-output", Writable: true}, nil
		}
	case unix.S_IFIFO:
		if access == unix.O_RDONLY {
			return LauncherPayload{Role: "source", Writable: false}, nil
		}
		if access == unix.O_WRONLY {
			return LauncherPayload{Role: "progress-output", Writable: true}, nil
		}
	}
	return LauncherPayload{}, ErrUnsafe
}

func validateStandardStreams(command *exec.Cmd, workspaceDevice uint64) error {
	if file, ok := command.Stdin.(*os.File); ok {
		if standardNull(file, false) {
			goto outputStreams
		}
		role, err := payloadRole(file, workspaceDevice)
		if err != nil || role.Writable {
			return ErrUnsafe
		}
	}
outputStreams:
	for _, writer := range []io.Writer{command.Stdout, command.Stderr} {
		if file, ok := writer.(*os.File); ok {
			if standardNull(file, true) {
				continue
			}
			role, err := payloadRole(file, workspaceDevice)
			if err != nil || !role.Writable {
				return ErrUnsafe
			}
		}
	}
	return nil
}

func standardNull(file *os.File, writable bool) bool {
	var stat unix.Stat_t
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	if err != nil || unix.Fstat(int(file.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFCHR ||
		stat.Uid != 0 || stat.Rdev != unix.Mkdev(1, 3) {
		return false
	}
	access := flags & unix.O_ACCMODE
	if writable {
		return access == unix.O_WRONLY || access == unix.O_RDWR
	}
	return access == unix.O_RDONLY
}

func sealedConfig(data []byte) (*os.File, error) {
	fd, err := unix.MemfdCreate("goby-command-config", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "goby-command-config")
	fail := func(cause error) (*os.File, error) {
		if file.Close() != nil {
			return file, errors.Join(cause, ErrRetained)
		}
		return nil, cause
	}
	if n, err := file.Write(data); err != nil || n != len(data) {
		return fail(ErrUnsafe)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fail(ErrUnsafe)
	}
	if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_SEAL|unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_WRITE); err != nil {
		return fail(ErrUnavailable)
	}
	return file, nil
}

func (d *Domain) checkIdentity() error {
	if d.parent == nil || d.group == nil || d.removed || d.brokerPID == 0 || uint32(os.Getpid()) != d.brokerPID {
		return ErrUnsafe
	}
	parent, err := directoryIdentity(d.parent)
	if err != nil || parent != d.parentIdentity {
		return ErrUnsafe
	}
	group, err := directoryIdentity(d.group)
	var named unix.Stat_t
	if err != nil || group != d.identity || unix.Fstatat(int(d.parent.Fd()), d.name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		named.Dev != d.identity.Device || named.Ino != d.identity.Inode || named.Uid != 0 || named.Mode&0077 != 0 {
		return ErrUnsafe
	}
	var ns unix.Stat_t
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || strings.TrimSpace(string(boot)) != d.bootID || unix.Stat("/proc/self/ns/mnt", &ns) != nil ||
		ns.Dev != d.mountNamespace.Device || ns.Ino != d.mountNamespace.Inode {
		return ErrUnsafe
	}
	return nil
}

func (d *Domain) openControl(name string, flags int) (*os.File, error) {
	if d.group == nil || d.removed || d.control != nil {
		return nil, ErrUnsafe
	}
	fd, err := unix.Openat(int(d.group.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnsafe
	}
	d.control = os.NewFile(uintptr(fd), name)
	return d.control, nil
}

func (d *Domain) closeControl(file *os.File) error {
	if file == nil || d.control != file {
		return ErrRetained
	}
	if file.Close() != nil {
		d.closed, d.quarantined = true, true
		return ErrRetained
	}
	d.control = nil
	return nil
}

func (d *Domain) readControl(name string) ([]byte, error) {
	file, err := d.openControl(name, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := d.closeControl(file)
	if readErr != nil || closeErr != nil || len(data) > 4096 {
		return nil, ErrUnsafe
	}
	return data, nil
}

func (d *Domain) writeControl(name, value string) error {
	file, err := d.openControl(name, unix.O_WRONLY)
	if err != nil {
		return err
	}
	n, writeErr := file.WriteString(value)
	closeErr := d.closeControl(file)
	if writeErr != nil || closeErr != nil || n != len(value) {
		return ErrUnsafe
	}
	return nil
}

func (d *Domain) empty() error {
	data, err := d.readControl("cgroup.events")
	if err != nil {
		return err
	}
	found := false
	var populated uint64
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return ErrUnsafe
		}
		if fields[0] != "populated" {
			continue
		}
		if found {
			return ErrUnsafe
		}
		populated, err = strconv.ParseUint(fields[1], 10, 64)
		if err != nil || populated > 1 {
			return ErrUnsafe
		}
		found = true
	}
	if !found {
		return ErrUnsafe
	}
	if populated != 0 {
		return ErrCapacity
	}
	return nil
}

func (d *Domain) releaseDescriptors() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var result error
	closeFile := func(slot **os.File) {
		if *slot == nil {
			return
		}
		if (*slot).Close() != nil {
			result = ErrRetained
			return
		}
		*slot = nil
	}
	closeFile(&d.group)
	closeFile(&d.control)
	closeFile(&d.parent)
	closeFile(&d.workspace)
	closeFile(&d.launcher.file)
	for index := range d.tools {
		closeFile(&d.tools[index].file)
	}
	for index := range d.hardware {
		closeFile(&d.hardware[index])
	}
	if result == nil {
		// A capability borrow outlives every native descriptor. Keep its owner
		// reference across any close failure, including partial constructor
		// cleanup; only complete retirement or known pre-start rollback returns it.
		release := func(tool *ownedExecutable) error {
			if tool.capability == nil {
				return nil
			}
			if tool.borrowOwner != d || tool.capability.releaseBorrow(d, tool.borrowSlot) != nil {
				return ErrRetained
			}
			tool.capability, tool.borrowOwner = nil, nil
			return nil
		}
		if release(&d.launcher) != nil {
			return ErrRetained
		}
		for index := range d.tools {
			if release(&d.tools[index]) != nil {
				return ErrRetained
			}
		}
	}
	return result
}

// Package commanddomain owns an optional native command domain shared by media
// preparation, conversion, live caption and diagnostic command paths. Its
// primitives do not attest the separate storage ledger or trusted Go writers.
package commanddomain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
)

var (
	ErrUnavailable   = errors.New("native command domain prerequisite is unavailable")
	ErrUnsafe        = errors.New("native command domain identity or policy is unsafe")
	ErrRetained      = errors.New("native command domain ownership must be retained")
	ErrCapacity      = errors.New("native command domain capacity is exhausted")
	ErrClosed        = errors.New("native command domain no longer accepts commands")
	ErrWaitOwnership = errors.New("native command wait ownership is unknown")
)

const MaxDomainCommands = 64

// Identity binds a borrowed directory to its actual kernel object and mount.
// A native constructor rechecks every field; a caller flag is not authority.
type Identity struct {
	Device  uint64 `json:"device"`
	Inode   uint64 `json:"inode"`
	MountID uint64 `json:"mount_id"`
}

type ApprovedExecutable struct {
	Path   string
	SHA256 string
}

// Config is trusted root-broker deployment configuration, never a client RPC.
// Parent and Workspace are borrowed; successful construction duplicates them.
// Parent is an already-owned private cgroup2 directory. Creation, removal and
// all controls remain in the root broker. Workspace must be the exact fixed
// job filesystem root. The enclosing broker owns its durable full reservation.
// Tools and Launcher are immutable approved installed files. Any newly copied
// setup files must be charged by that broker's outside-static inventory.
// NewWithExecutableCapabilities uses these exact approval values as selectors
// for held root-issued FDs; it never reopens the approval paths on that branch.
type Config struct {
	Enabled           bool
	Parent            *os.File
	ParentIdentity    Identity
	Workspace         *os.File
	WorkspaceIdentity Identity
	Launcher          ApprovedExecutable
	Tools             []ApprovedExecutable
	UID               uint32
	GID               uint32
	Groups            []uint32
	MaxCommands       int
	MaxTasks          int
	Hardware          string
	HardwareDevices   []LauncherHardwareDevice
}

// Snapshot is diagnostic. It never attests storage, native readiness, writer
// confinement or durable retirement, including for a zero-value object.
type Snapshot struct {
	Capacity    int
	Active      int
	Closed      bool
	Removed     bool
	Quarantined bool
	Identity    Identity
}

type domainContextKey struct{}

// WithDomain propagates the same actual owner across packages without an
// import cycle. A routed command must not fall back to an unrestricted spawn
// when this owner rejects a start. Installation is not readiness evidence.
func WithDomain(ctx context.Context, domain *Domain) context.Context {
	return context.WithValue(ctx, domainContextKey{}, domain)
}

func FromContext(ctx context.Context) *Domain {
	if ctx == nil {
		return nil
	}
	domain, _ := ctx.Value(domainContextKey{}).(*Domain)
	return domain
}

// Process exclusively owns one actual exec.Cmd from Start through Wait. A
// normal Wait return joins the leader and os/exec's stream copiers. Neither
// caller cancellation nor an observation deadline permits abandoning it.
type Process struct {
	domain      *Domain
	command     *exec.Cmd
	slot        int
	waitMu      sync.Mutex
	waitStarted bool
	joined      bool
	err         error
	inherited   []*os.File
}

func (p *Process) Wait() (result error) {
	if p == nil || p.domain == nil || p.command == nil {
		return ErrWaitOwnership
	}
	p.waitMu.Lock()
	if p.waitStarted {
		p.waitMu.Unlock()
		return ErrWaitOwnership
	}
	p.waitStarted = true
	p.waitMu.Unlock()
	returned := false
	defer func() {
		if !returned {
			// An abnormal waiter exit is not a join. Keep its exact command
			// record and the launcher's original cgroup/storage ownership.
			p.domain.abnormalWait(p)
		}
	}()
	result = p.command.Wait()
	if p.command.Process == nil || p.command.ProcessState == nil || p.command.ProcessState.Pid() != p.command.Process.Pid {
		return errors.Join(result, ErrWaitOwnership)
	}
	p.waitMu.Lock()
	p.joined, p.err = true, result
	p.waitMu.Unlock()
	p.domain.joined(p)
	returned = true
	return result
}

// Run is the synchronous shared gateway. Streaming callers use Start and hold
// the returned Process through their existing actual observer and Wait path.
func (d *Domain) Run(ctx context.Context, command *exec.Cmd) error {
	process, err := d.Start(ctx, command)
	if process == nil {
		return err
	}
	return errors.Join(err, process.Wait())
}

// ExitCode reports only an actually joined owned command. No raw exec.Cmd,
// process or native control descriptor escapes this owner.
func (p *Process) ExitCode() (int, bool) {
	if p == nil {
		return -1, false
	}
	p.waitMu.Lock()
	defer p.waitMu.Unlock()
	if !p.joined || p.command.ProcessState == nil {
		return -1, false
	}
	return p.command.ProcessState.ExitCode(), true
}

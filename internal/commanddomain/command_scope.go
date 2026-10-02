package commanddomain

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

const commandScopeRetireTimeout = 5 * time.Second

// CommandScope is a bounded shared gateway, not a storage or readiness proof.
// Its owner retains the original executable capabilities and source FDs through
// the entire scope lifetime. Strong references here do not prevent the caller
// from explicitly closing an idle capability; a later command then fails closed.
type CommandScope struct{ *commandScopeState }

type commandScopeState struct {
	mu          sync.Mutex
	drainMu     sync.Mutex
	config      Config
	launcher    *ExecutableCapability
	tools       []*ExecutableCapability
	issuer      executableCapabilityIssuer
	slots       []*scopedCommand
	uses        []*ExecutableUse
	metadata    []*scopeMetadataDuplicate
	changed     chan struct{}
	gateClosed  bool
	quarantined bool
	closed      bool
}

type scopedCommand struct {
	mu            sync.Mutex
	opMu          sync.Mutex
	scope         *commandScopeState
	slot          int
	context       context.Context
	cancel        context.CancelFunc
	template      *exec.Cmd
	executable    *ExecutableUse
	limits        LimitsClass
	domain        *Domain
	process       *Process
	pipes         *TemplatePipes
	startErr      error
	waitErr       error
	waitAttempted bool
	joined        bool
	retired       bool
	unknown       bool
}

// ScopedProcess owns one actual command and leaf through Wait and retirement.
// A non-nil handle with a Start error remains an owner and requires actual Wait.
type ScopedProcess struct{ command *scopedCommand }

// ScopedTemplatePipes additionally owns the original template's output ends and
// complete consumer callbacks. Wait never closes readers or returns its slot.
type ScopedTemplatePipes struct{ command *scopedCommand }

type CommandScopeSnapshot struct {
	Capacity    int
	Active      int
	GateClosed  bool
	Quarantined bool
	Closed      bool
}

// NewCommandScope validates live root configuration without creating a command
// cgroup. Each admitted command later owns one real leaf Domain. Config parent
// and workspace are independently duplicated; capabilities remain caller-owned.
// A non-nil result with an error retains a failed descriptor-cleanup owner.
func NewCommandScope(config Config, launcher *ExecutableCapability, tools []*ExecutableCapability) (*CommandScope, error) {
	if !config.Enabled {
		return nil, ErrUnavailable
	}
	if config.MaxCommands < 1 || config.MaxCommands > MaxDomainCommands || config.MaxTasks < config.MaxCommands || config.MaxTasks > 4096 ||
		config.Parent == nil || config.Workspace == nil || len(config.Tools) < 1 || len(config.Tools) > 16 ||
		len(config.Groups) > maxLauncherGroups || len(config.HardwareDevices) > maxLauncherHardwareDevices || launcher == nil || len(tools) != len(config.Tools) {
		return nil, ErrUnsafe
	}
	for _, tool := range tools {
		if tool == nil {
			return nil, ErrUnsafe
		}
	}
	s := &commandScopeState{config: config, launcher: launcher, tools: append([]*ExecutableCapability(nil), tools...),
		slots: make([]*scopedCommand, config.MaxCommands), uses: make([]*ExecutableUse, config.MaxCommands),
		metadata: make([]*scopeMetadataDuplicate, config.MaxCommands), changed: make(chan struct{})}
	s.config.Tools = append([]ApprovedExecutable(nil), config.Tools...)
	s.config.Groups = append([]uint32(nil), config.Groups...)
	s.config.HardwareDevices = append([]LauncherHardwareDevice(nil), config.HardwareDevices...)
	s.config.Parent, s.config.Workspace = nil, nil
	result := &CommandScope{commandScopeState: s}
	if err := initializeCommandScope(s, config); err != nil {
		s.gateClosed, s.quarantined = true, true
		if cleanupErr := s.closeConfiguration(); cleanupErr != nil {
			return result, errors.Join(err, cleanupErr)
		}
		return nil, err
	}
	return result, nil
}

type commandScopeContextKey struct{}

// WithCommandScope records explicit presence, including a typed nil owner. An
// integration must distinguish absence (legacy routing) from present nil (closed
// fail). Nil context is preserved as an explicitly invalid native binding.
func WithCommandScope(ctx context.Context, scope *CommandScope) context.Context {
	if ctx == nil {
		return context.WithValue(context.Background(), commandScopeContextKey{}, (*CommandScope)(nil))
	}
	return context.WithValue(ctx, commandScopeContextKey{}, scope)
}

func CommandScopeFromContext(ctx context.Context) (*CommandScope, bool) {
	if ctx == nil {
		return nil, false
	}
	scope, present := ctx.Value(commandScopeContextKey{}).(*CommandScope)
	if !present {
		_, present = ctx.Value(domainContextKey{}).(*Domain)
	}
	return scope, present
}

func (s *CommandScope) Start(ctx context.Context, template *exec.Cmd) (*ScopedProcess, error) {
	entry, err := s.admit(ctx, template)
	if err != nil {
		return nil, err
	}
	handle := &ScopedProcess{command: entry}
	returned := false
	defer func() {
		if !returned {
			entry.quarantine()
		}
	}()
	err = entry.start(false, false)
	returned = true
	if entry.isRetired() {
		return nil, err
	}
	return handle, err
}

// StartTemplatePipes transfers newly created template pipe ends to the handle.
// The template and its raw stream fields must not be changed or independently
// started, waited or closed after a non-nil handle is returned, including errors.
func (s *CommandScope) StartTemplatePipes(ctx context.Context, template *exec.Cmd, stdout, stderr bool) (*ScopedTemplatePipes, error) {
	if !stdout && !stderr {
		return nil, ErrUnsafe
	}
	entry, err := s.admit(ctx, template)
	if err != nil {
		return nil, err
	}
	handle := &ScopedTemplatePipes{command: entry}
	returned := false
	defer func() {
		if !returned {
			entry.quarantine()
		}
	}()
	err = entry.start(stdout, stderr)
	returned = true
	if entry.isRetired() {
		return nil, err
	}
	return handle, err
}

func (s *CommandScope) Run(ctx context.Context, template *exec.Cmd) error {
	process, err := s.Start(ctx, template)
	if process == nil {
		return err
	}
	return errors.Join(err, process.Wait())
}

func (s *CommandScope) admit(ctx context.Context, template *exec.Cmd) (*scopedCommand, error) {
	if s == nil || s.commandScopeState == nil {
		return nil, ErrUnavailable
	}
	if ctx == nil || template == nil || template.Process != nil || template.ProcessState != nil {
		return nil, ErrUnsafe
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gateClosed || s.closed || s.quarantined {
		return nil, ErrClosed
	}
	index := freeCommandSlot(s.slots)
	if index < 0 {
		return nil, ErrCapacity
	}
	entry := &scopedCommand{scope: s.commandScopeState, slot: index, context: ctx, template: template}
	// Register before context callbacks or the first real native New side
	// effect. Abnormal exits preserve this exact owner in its fixed slot.
	s.slots[index] = entry
	s.notifyLocked()
	return entry, nil
}

func freeCommandSlot(slots []*scopedCommand) int {
	for index, current := range slots {
		if current == nil {
			return index
		}
	}
	return -1
}

func (e *scopedCommand) start(stdout, stderr bool) error {
	returned := false
	defer func() {
		if !returned {
			e.quarantine()
		}
	}()
	err := e.startOwned(stdout, stderr)
	returned = true
	return err
}

func (e *scopedCommand) startOwned(stdout, stderr bool) error {
	if err := e.context.Err(); err != nil {
		e.setStartError(err)
		return e.rollback()
	}
	owned, cancel := context.WithCancel(e.context)
	e.mu.Lock()
	e.context, e.cancel = owned, cancel
	e.mu.Unlock()
	config := e.scope.config
	config.MaxCommands = 1
	// Full native validation and creation run outside the admission mutex.
	// The registered slot keeps configuration alive while Drain can observe its
	// finite deadline. Capability failure never falls back to a pathname open.
	domain, err := NewWithExecutableCapabilities(config, e.scope.launcher, e.scope.tools)
	e.mu.Lock()
	e.domain, e.startErr = domain, err
	e.mu.Unlock()
	if err != nil {
		if domain != nil {
			e.quarantine()
			return errors.Join(err, ErrRetained)
		}
		return e.rollback()
	}
	if e.executable != nil {
		if err := bindDomainExecutableUse(domain, e.executable, e.limits); err != nil {
			e.setStartError(err)
			return e.rollback()
		}
	}
	if err := owned.Err(); err != nil {
		e.setStartError(err)
		return e.rollback()
	}
	if stdout || stderr {
		pipes, pipeErr := NewTemplatePipes(e.template, stdout, stderr)
		e.mu.Lock()
		e.pipes, e.startErr = pipes, pipeErr
		e.mu.Unlock()
		if pipeErr != nil {
			if pipes != nil && pipes.Close() != nil {
				e.quarantine()
				return errors.Join(pipeErr, ErrRetained)
			}
			return e.rollback()
		}
		startErr := pipes.Start(owned, domain)
		e.setStartError(startErr)
		if startErr != nil {
			if errors.Is(startErr, ErrRetained) || errors.Is(startErr, ErrWaitOwnership) {
				e.quarantine()
				return startErr
			}
			if templatePipeProcess(pipes) != nil {
				return startErr
			}
			if pipes.Close() != nil {
				e.quarantine()
				return errors.Join(startErr, ErrRetained)
			}
			return e.rollback()
		}
	} else {
		process, startErr := domain.Start(owned, e.template)
		e.mu.Lock()
		e.process, e.startErr = process, startErr
		e.mu.Unlock()
		if errors.Is(startErr, ErrRetained) || errors.Is(startErr, ErrWaitOwnership) {
			e.quarantine()
			return errors.Join(startErr, ErrRetained)
		}
		if process == nil {
			if startErr == nil {
				e.quarantine()
				return ErrRetained
			}
			return e.rollback()
		}
	}
	e.mu.Lock()
	result := e.startErr
	e.mu.Unlock()
	return result
}

func (e *scopedCommand) setStartError(err error) { e.mu.Lock(); e.startErr = err; e.mu.Unlock() }
func (e *scopedCommand) isRetired() bool         { e.mu.Lock(); defer e.mu.Unlock(); return e.retired }
func templatePipeProcess(pipes *TemplatePipes) *Process {
	if pipes == nil {
		return nil
	}
	pipes.mu.Lock()
	defer pipes.mu.Unlock()
	return pipes.process
}

func (e *scopedCommand) rollback() error {
	e.mu.Lock()
	cancelOwned, domain, startErr := e.cancel, e.domain, e.startErr
	e.mu.Unlock()
	if cancelOwned != nil {
		cancelOwned()
	}
	if domain != nil {
		err := retireScopedDomain(domain)
		if err != nil {
			e.quarantine()
			return errors.Join(startErr, err, ErrRetained)
		}
	}
	return errors.Join(startErr, e.release())
}

func (p *ScopedProcess) Wait() (result error) {
	if p == nil || p.command == nil {
		return ErrWaitOwnership
	}
	e := p.command
	e.opMu.Lock()
	defer e.opMu.Unlock()
	returned := false
	defer func() {
		if !returned {
			e.quarantine()
		}
	}()
	e.mu.Lock()
	retired, process, joined, attempted, startErr, waitErr := e.retired, e.process, e.joined, e.waitAttempted, e.startErr, e.waitErr
	e.mu.Unlock()
	if retired {
		returned = true
		return errors.Join(startErr, waitErr)
	}
	if process == nil || attempted && !joined {
		returned = true
		return errors.Join(startErr, waitErr, ErrRetained)
	}
	if !joined {
		e.mu.Lock()
		e.waitAttempted = true
		e.mu.Unlock()
		waitErr = process.Wait()
		_, joined = process.ExitCode()
		e.mu.Lock()
		e.joined, e.waitErr = joined, waitErr
		e.mu.Unlock()
		if !joined {
			e.quarantine()
			returned = true
			return errors.Join(startErr, waitErr, ErrRetained)
		}
	}
	e.mu.Lock()
	unknown := e.unknown
	e.mu.Unlock()
	if unknown {
		returned = true
		return errors.Join(startErr, waitErr, ErrRetained)
	}
	result = errors.Join(startErr, waitErr, e.retire())
	returned = true
	return result
}

func (p *ScopedProcess) ExitCode() (int, bool) {
	if p == nil || p.command == nil {
		return -1, false
	}
	p.command.mu.Lock()
	process := p.command.process
	p.command.mu.Unlock()
	if process == nil {
		return -1, false
	}
	return process.ExitCode()
}

func (p *ScopedProcess) SignalCancel() error {
	if p == nil || p.command == nil {
		return ErrUnsafe
	}
	return p.command.signalCancel(false)
}

// RetirementComplete reports only this handle's successful actual leaf
// retirement and original slot return. An ExitError does not negate that join.
func (p *ScopedProcess) RetirementComplete() bool {
	if p == nil || p.command == nil {
		return false
	}
	return p.command.retirementComplete()
}

func (p *ScopedTemplatePipes) Wait() (result error) {
	if p == nil || p.command == nil {
		return ErrWaitOwnership
	}
	e := p.command
	e.opMu.Lock()
	defer e.opMu.Unlock()
	returned := false
	defer func() {
		if !returned {
			e.quarantine()
		}
	}()
	e.mu.Lock()
	pipes := e.pipes
	e.mu.Unlock()
	if pipes == nil {
		returned = true
		return ErrRetained
	}
	result = pipes.Wait()
	process := templatePipeProcess(pipes)
	joined := false
	if process != nil {
		_, joined = process.ExitCode()
	}
	e.mu.Lock()
	e.joined, e.waitErr = joined, result
	e.mu.Unlock()
	if !joined {
		e.quarantine()
		result = errors.Join(result, ErrRetained)
	}
	e.mu.Lock()
	unknown := e.unknown
	e.mu.Unlock()
	if unknown {
		result = errors.Join(result, ErrRetained)
	}
	// This actual process join does not consume output or return the leaf slot.
	returned = true
	return result
}

func (p *ScopedTemplatePipes) ConsumeStdout(consume func(io.Reader) error) error {
	return p.consume(false, consume)
}
func (p *ScopedTemplatePipes) ConsumeStderr(consume func(io.Reader) error) error {
	return p.consume(true, consume)
}
func (p *ScopedTemplatePipes) consume(stderr bool, consume func(io.Reader) error) (result error) {
	if p == nil || p.command == nil || consume == nil {
		return ErrUnsafe
	}
	e := p.command
	e.mu.Lock()
	pipes, unknown, retired := e.pipes, e.unknown, e.retired
	e.mu.Unlock()
	if pipes == nil {
		return ErrUnsafe
	}
	if unknown || retired {
		return ErrClosed
	}
	returned := false
	defer func() {
		if !returned {
			e.quarantine()
		}
	}()
	if stderr {
		result = pipes.ConsumeStderr(consume)
	} else {
		result = pipes.ConsumeStdout(consume)
	}
	returned = true
	return result
}

func (p *ScopedTemplatePipes) SignalCancel() error {
	if p == nil || p.command == nil {
		return ErrUnsafe
	}
	return p.command.signalCancel(true)
}

func (p *ScopedTemplatePipes) RetirementComplete() bool {
	if p == nil || p.command == nil {
		return false
	}
	return p.command.retirementComplete()
}

func (p *ScopedTemplatePipes) ExitCode() (int, bool) {
	if p == nil || p.command == nil {
		return -1, false
	}
	p.command.mu.Lock()
	pipes := p.command.pipes
	p.command.mu.Unlock()
	process := templatePipeProcess(pipes)
	if process == nil {
		return -1, false
	}
	return process.ExitCode()
}

func (p *ScopedTemplatePipes) Close() (result error) {
	if p == nil || p.command == nil {
		return ErrUnsafe
	}
	e := p.command
	e.opMu.Lock()
	defer e.opMu.Unlock()
	returned := false
	defer func() {
		if !returned {
			e.quarantine()
		}
	}()
	e.mu.Lock()
	retired, pipes, unknown, joined := e.retired, e.pipes, e.unknown, e.joined
	e.mu.Unlock()
	if retired {
		returned = true
		return nil
	}
	if pipes == nil || unknown || !joined {
		returned = true
		return ErrRetained
	}
	if result = pipes.Close(); result != nil {
		pipes.mu.Lock()
		unknownClose, attemptedClose := pipes.unknown, pipes.closeAdmission
		pipes.mu.Unlock()
		// A normal precondition rejection has made no descriptor close attempt.
		// Callers must join their consumer, or explicitly cancel a failed parser,
		// before retrying. Only actual successful Close permits retirement.
		if unknownClose || attemptedClose {
			e.quarantine()
		}
		returned = true
		return errors.Join(result, ErrRetained)
	}
	// TemplatePipes verifies actual read/consumer return and EOF or explicit
	// cancellation before this leaf can retire and return its original slot.
	result = e.retire()
	returned = true
	return result
}

func (e *scopedCommand) retirementComplete() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.retired && !e.unknown
}

func (e *scopedCommand) signalCancel(withPipes bool) error {
	returned := false
	defer func() {
		if !returned {
			e.quarantine()
		}
	}()
	e.mu.Lock()
	cancel, pipes, retired := e.cancel, e.pipes, e.retired
	e.mu.Unlock()
	if retired {
		returned = true
		return nil
	}
	if cancel == nil {
		e.quarantine()
		returned = true
		return ErrRetained
	}
	cancel()
	if withPipes {
		if pipes == nil {
			e.quarantine()
			returned = true
			return ErrRetained
		}
		// Closing the adapter's owned readers interrupts Read. It does not join
		// a parser callback or permit leaf retirement before actual return.
		if err := pipes.Cancel(); err != nil {
			e.quarantine()
			returned = true
			return errors.Join(err, ErrRetained)
		}
	}
	returned = true
	return nil
}

func (e *scopedCommand) retire() error {
	e.mu.Lock()
	cancelOwned, domain := e.cancel, e.domain
	e.mu.Unlock()
	if domain == nil {
		e.quarantine()
		return ErrRetained
	}
	if cancelOwned != nil {
		cancelOwned()
	}
	err := retireScopedDomain(domain)
	if err != nil {
		e.quarantine()
		return errors.Join(err, ErrRetained)
	}
	return e.release()
}

func retireScopedDomain(domain *Domain) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandScopeRetireTimeout)
	defer cancel()
	return domain.Retire(ctx)
}

func (e *scopedCommand) release() error {
	s := e.scope
	s.mu.Lock()
	defer s.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.unknown || e.slot < 0 || e.slot >= len(s.slots) || s.slots[e.slot] != e {
		s.gateClosed, s.quarantined = true, true
		s.notifyLocked()
		return ErrRetained
	}
	if e.executable != nil {
		use := e.executable
		use.mu.Lock()
		if e.slot >= len(use.commands) || use.commands[e.slot] != e || use.unknown {
			use.mu.Unlock()
			s.gateClosed, s.quarantined = true, true
			s.notifyLocked()
			return ErrRetained
		}
		use.commands[e.slot] = nil
		use.mu.Unlock()
	}
	e.retired = true
	s.slots[e.slot] = nil
	s.notifyLocked()
	return nil
}

func (e *scopedCommand) quarantine() {
	s := e.scope
	s.mu.Lock()
	defer s.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.unknown = true
	s.gateClosed, s.quarantined = true, true
	s.notifyLocked()
}

func (s *CommandScope) CloseGate() {
	if s == nil || s.commandScopeState == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gateClosed = true
	s.notifyLocked()
}

// Drain closes admission and waits for actual handle retirement. It does not
// call an active parser, replace Wait, cancel consumers or erase unknown owners.
// A finite observation deadline never returns a slot or descriptor lifetime.
func (s *CommandScope) Drain(ctx context.Context) error {
	if s == nil || s.commandScopeState == nil || ctx == nil {
		return ErrUnsafe
	}
	returned := false
	defer func() {
		if !returned {
			s.fence()
		}
	}()
	deadline, bounded := ctx.Deadline()
	if !bounded {
		returned = true
		return ErrRetained
	}
	s.CloseGate()
	if !s.drainMu.TryLock() {
		returned = true
		return ErrRetained
	}
	defer s.drainMu.Unlock()
	// Own the actual observation timer. A custom context's deadline value or
	// nil Done channel cannot turn this finite observer into an unbounded wait.
	observation, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	callerDone := ctx.Done()
	for {
		s.mu.Lock()
		active := false
		for _, entry := range s.slots {
			active = active || entry != nil
		}
		for _, use := range s.uses {
			active = active || use != nil
		}
		for _, owner := range s.metadata {
			active = active || owner != nil
		}
		if !active {
			s.mu.Unlock()
			err := s.closeRetiredConfiguration()
			returned = true
			return err
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-changed:
		case <-callerDone:
			err := ctx.Err()
			returned = true
			return errors.Join(err, ErrRetained)
		case <-observation.Done():
			returned = true
			return errors.Join(observation.Err(), ErrRetained)
		}
	}
}

func (s *CommandScope) closeRetiredConfiguration() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	// Recheck actual registry membership while holding admission closed. This
	// performs no join and cannot erase an existing or quarantined handle.
	for _, entry := range s.slots {
		if entry != nil {
			return ErrRetained
		}
	}
	for _, use := range s.uses {
		if use != nil {
			return ErrRetained
		}
	}
	for _, owner := range s.metadata {
		if owner != nil {
			return ErrRetained
		}
	}
	if err := s.closeConfiguration(); err != nil {
		s.quarantined = true
		return err
	}
	s.closed = true
	return nil
}

func (s *CommandScope) fence() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gateClosed, s.quarantined = true, true
	for _, entry := range s.slots {
		if entry == nil {
			continue
		}
		entry.mu.Lock()
		entry.unknown = true
		entry.mu.Unlock()
	}
	s.notifyLocked()
}

func (s *commandScopeState) notifyLocked() { close(s.changed); s.changed = make(chan struct{}) }
func (s *CommandScope) Snapshot() CommandScopeSnapshot {
	if s == nil || s.commandScopeState == nil {
		return CommandScopeSnapshot{GateClosed: true, Quarantined: true}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := CommandScopeSnapshot{Capacity: len(s.slots), GateClosed: s.gateClosed, Quarantined: s.quarantined, Closed: s.closed}
	for _, entry := range s.slots {
		if entry != nil {
			result.Active++
		}
	}
	return result
}

func (s *commandScopeState) closeConfiguration() error {
	for _, slot := range []**os.File{&s.config.Parent, &s.config.Workspace} {
		if *slot == nil {
			continue
		}
		if (*slot).Close() != nil {
			return ErrRetained
		}
		*slot = nil
	}
	return nil
}

func (CommandScope) MarshalJSON() ([]byte, error)        { return nil, ErrUnsafe }
func (*CommandScope) UnmarshalJSON([]byte) error         { return ErrUnsafe }
func (ScopedProcess) MarshalJSON() ([]byte, error)       { return nil, ErrUnsafe }
func (*ScopedProcess) UnmarshalJSON([]byte) error        { return ErrUnsafe }
func (ScopedTemplatePipes) MarshalJSON() ([]byte, error) { return nil, ErrUnsafe }
func (*ScopedTemplatePipes) UnmarshalJSON([]byte) error  { return ErrUnsafe }

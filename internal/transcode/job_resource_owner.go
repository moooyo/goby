package transcode

import (
	"errors"
	"sync"
)

var errJobResourceOwnership = errors.New("transcode resource ownership transition is invalid")

const maxExecutionSlots = 4096

// executionSlotPool counts actual execution capacity independently from
// unfinished user/credential ownership and completion/storage reservations.
// The manager continues to own its existing admission and descriptor bounds.
// onAvailable runs outside every ownership lock and must not perform I/O.
type executionSlotPool struct {
	mu          sync.Mutex
	capacity    int
	used        int
	closed      bool
	onAvailable func()
}

type executionSlot struct {
	pool     *executionSlotPool
	released bool
}

type executionSlotSnapshot struct {
	Capacity  int
	Used      int
	Available int
	Closed    bool
}

func newExecutionSlotPool(capacity int, onAvailable func()) (*executionSlotPool, error) {
	if capacity < 1 || capacity > maxExecutionSlots {
		return nil, ErrInvalidOptions
	}
	return &executionSlotPool{capacity: capacity, onAvailable: onAvailable}, nil
}

func (p *executionSlotPool) reserve() (*executionSlot, error) {
	if p == nil {
		return nil, ErrInvalidOptions
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, ErrManagerClosed
	}
	if p.used == p.capacity {
		return nil, ErrBusy
	}
	p.used++
	return &executionSlot{pool: p}, nil
}

func (p *executionSlotPool) close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
}

func (p *executionSlotPool) snapshot() executionSlotSnapshot {
	if p == nil {
		return executionSlotSnapshot{Closed: true}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return executionSlotSnapshot{Capacity: p.capacity, Used: p.used, Available: p.capacity - p.used, Closed: p.closed}
}

func (slot *executionSlot) release() bool {
	if slot == nil || slot.pool == nil {
		return false
	}
	p := slot.pool
	p.mu.Lock()
	if slot.released {
		p.mu.Unlock()
		return false
	}
	slot.released = true
	p.used--
	notify, closed := p.onAvailable, p.closed
	p.mu.Unlock()
	if notify != nil && !closed {
		notify()
	}
	return true
}

type jobRetirementPolicy uint8

const (
	// Legacy mode observes Run return without claiming a complete domain proof.
	jobRetireAfterAccounting jobRetirementPolicy = iota
	// Only finite jobs may return verified process capacity before Run returns.
	jobRetireFiniteVerified
	// Live observers can launch caption commands until their complete drain.
	jobRetireLiveVerified
)

// jobResourceOwner records the manager's separate resource lifetimes. It does
// not create, confine or verify a command domain. Verified retirement originates
// in this job's actual resourceLifecycle barrier, never in a progress counter,
// directory inventory, completion ticket or storage lease flag. Enforced mode
// must remain unavailable until its native broker prerequisites are complete.
//
// The finalization executor owns completion-ticket transitions and release.
// Storage survives terminal handling and reader retention under its own ledger.
// onOwnershipDone is the manager's once-only user/credential ownership return;
// it runs outside this owner's lock after terminal handling or prelaunch rollback.
type jobResourceOwner struct {
	mu                   sync.Mutex
	execution            *executionSlot
	completion           *completionTicket
	lifecycle            *resourceLifecycle
	policy               jobRetirementPolicy
	started              bool
	runnerReturned       bool
	accountingHandled    bool
	queued               bool
	executionReturned    bool
	terminalHandling     bool
	terminalHandled      bool
	ownershipQuarantined bool
	rolledBack           bool
	onOwnershipDone      func()
}

type jobResourceOwnerSnapshot struct {
	Started              bool
	RunnerReturned       bool
	AccountingHandled    bool
	FinalizationQueued   bool
	ExecutionReturned    bool
	TerminalHandled      bool
	OwnershipQuarantined bool
	RolledBack           bool
}

// reserveJobResourceOwner creates fresh private execution and completion
// reservations as one owned admission. Accepting already reserved handles here
// would permit duplicate owners to return another job's ticket during rollback.
// The manager supplies its bounded non-I/O scheduler notification to both pools.
func reserveJobResourceOwner(executions *executionSlotPool, completions *completionTicketPool, lifecycle *resourceLifecycle, policy jobRetirementPolicy, onOwnershipDone func()) (*jobResourceOwner, error) {
	if executions == nil || completions == nil || lifecycle == nil || policy > jobRetireLiveVerified {
		return nil, ErrInvalidOptions
	}
	execution, err := executions.reserve()
	if err != nil {
		return nil, err
	}
	completion, err := completions.reserve()
	if err != nil {
		execution.release()
		return nil, err
	}
	return &jobResourceOwner{execution: execution, completion: completion, lifecycle: lifecycle,
		policy: policy, onOwnershipDone: onOwnershipDone}, nil
}

// startedExecution closes the prelaunch rollback path before the first owned
// workspace writer, preparation operation or conversion command starts. The
// admission owner must never use rollback while an asset-copy writer is live.
func (o *jobResourceOwner) startedExecution() bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	snapshot := o.lifecycle.snapshot()
	if o.started || o.runnerReturned || o.rolledBack || o.terminalHandled ||
		!snapshot.ProcessRetired.IsZero() || !snapshot.WritersDrained.IsZero() || snapshot.RetirementError != nil {
		return false
	}
	o.started = true
	return true
}

// observeRetirement is wired to this lifecycle's onStage callback. Checking the
// owned snapshot rejects observations from another job and default unverified
// Run returns. This method does not alter unfinished ownership or other leases.
func (o *jobResourceOwner) observeRetirement(event resourceLifecycleEvent) bool {
	if o == nil || event.Stage != resourceProcessRetired || !event.Verified || event.At.IsZero() {
		return false
	}
	o.mu.Lock()
	snapshot := o.lifecycle.snapshot()
	eligible := o.policy == jobRetireFiniteVerified && o.started && !o.rolledBack &&
		!o.executionReturned && snapshot.RetirementVerified && snapshot.RetirementError == nil &&
		snapshot.ProcessRetired.Equal(event.At)
	if eligible {
		o.executionReturned = true
	}
	o.mu.Unlock()
	if !eligible {
		return false
	}
	return o.execution.release()
}

// returned is called only after Run and all runner-owned closes, followed by
// the manager's borrowed source-descriptor closes. It does not itself enqueue
// work, shrink storage or release the legacy execution allowance.
func (o *jobResourceOwner) returned() error {
	if o == nil {
		return errJobResourceOwnership
	}
	o.mu.Lock()
	snapshot := o.lifecycle.snapshot()
	if o.runnerReturned || o.rolledBack || snapshot.WritersDrained.IsZero() {
		o.mu.Unlock()
		return errJobResourceOwnership
	}
	verified := snapshot.RetirementVerified && snapshot.RetirementError == nil && !snapshot.ProcessRetired.IsZero()
	if o.policy != jobRetireAfterAccounting && !verified {
		o.mu.Unlock()
		return errJobResourceOwnership
	}
	o.runnerReturned = true
	release := o.policy != jobRetireAfterAccounting && !o.executionReturned
	if release {
		o.executionReturned = true
	}
	o.mu.Unlock()
	if release {
		o.execution.release()
	}
	return nil
}

// submitFinalization atomically binds the owner handoff to executor.submit.
// The worker may start only after queued ownership is visible. A rejected
// submission leaves the original reservation available for an owned retry.
// Failed native retirement keeps every allowance with the cleanup owner.
func (o *jobResourceOwner) submitFinalization(executor *finalizationExecutor, task finalizationTask) error {
	if o == nil || executor == nil {
		return errJobResourceOwnership
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	snapshot := o.lifecycle.snapshot()
	if !o.runnerReturned || o.queued || o.rolledBack || o.terminalHandled ||
		o.policy != jobRetireAfterAccounting && (!snapshot.RetirementVerified || snapshot.RetirementError != nil) {
		return errJobResourceOwnership
	}
	o.queued = true
	if err := executor.submit(o.completion, task); err != nil {
		o.queued = false
		return err
	}
	return nil
}

// accountingComplete is called by the fixed worker after final inspection and
// accounting have returned, including an explicitly handled failure. It keeps
// user/credential ownership until terminal handling. A failed enforced barrier
// can never fall through to this legacy capacity-return boundary.
func (o *jobResourceOwner) accountingComplete() error {
	if o == nil {
		return errJobResourceOwnership
	}
	o.mu.Lock()
	if !o.queued || o.accountingHandled || o.rolledBack || o.terminalHandled {
		o.mu.Unlock()
		return errJobResourceOwnership
	}
	o.accountingHandled = true
	release := o.policy == jobRetireAfterAccounting && !o.executionReturned
	if release {
		o.executionReturned = true
	}
	o.mu.Unlock()
	if release {
		o.execution.release()
	}
	return nil
}

// terminalComplete follows the terminal persistence attempt and final public
// status handling. A persistence failure remains a failed outcome; it is still
// bounded terminal handling. The executor then returns the completion ticket,
// while guarded storage cleanup/reader ownership remains separate.
func (o *jobResourceOwner) terminalComplete() error {
	if o == nil {
		return errJobResourceOwnership
	}
	o.mu.Lock()
	if !o.accountingHandled || !o.executionReturned || o.terminalHandling || o.terminalHandled || o.rolledBack || o.ownershipQuarantined {
		o.mu.Unlock()
		return errJobResourceOwnership
	}
	o.terminalHandling = true
	notify := o.onOwnershipDone
	o.mu.Unlock()
	returned := false
	defer func() {
		if !returned {
			// A notification may have partly changed manager ownership before
			// panic or Goexit. Preserve its exact owner rather than retrying it.
			// The executor observes the abnormal terminal exit and retains the
			// original completion ticket in its bounded quarantine.
			o.mu.Lock()
			o.terminalHandling = false
			o.ownershipQuarantined = true
			o.mu.Unlock()
		}
	}()
	if notify != nil {
		notify()
	}
	o.mu.Lock()
	o.terminalHandling = false
	o.terminalHandled = true
	o.mu.Unlock()
	returned = true
	return nil
}

// rollbackBeforeLaunch is owned by admission after proving no workspace writer
// or command has started. The storage owner separately performs its
// durable unclaimed rollback; this method never retires a workspace.
func (o *jobResourceOwner) rollbackBeforeLaunch() bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	if o.started || o.runnerReturned || o.queued || o.terminalHandled || o.rolledBack {
		o.mu.Unlock()
		return false
	}
	o.rolledBack = true
	notify := o.onOwnershipDone
	o.mu.Unlock()
	returned := false
	defer func() {
		if !returned {
			// Even before launch, an abnormal ownership notification may have
			// changed only part of the manager's counters. Do not retry it or
			// drop reservations that have not actually been returned.
			o.mu.Lock()
			o.ownershipQuarantined = true
			o.mu.Unlock()
		}
	}()
	if notify != nil {
		notify()
	}
	o.mu.Lock()
	o.executionReturned = true
	o.mu.Unlock()
	o.execution.release()
	o.completion.release()
	returned = true
	return true
}

func (o *jobResourceOwner) snapshot() jobResourceOwnerSnapshot {
	if o == nil {
		return jobResourceOwnerSnapshot{}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return jobResourceOwnerSnapshot{Started: o.started, RunnerReturned: o.runnerReturned,
		AccountingHandled: o.accountingHandled, FinalizationQueued: o.queued, ExecutionReturned: o.executionReturned,
		TerminalHandled: o.terminalHandled, OwnershipQuarantined: o.ownershipQuarantined, RolledBack: o.rolledBack}
}

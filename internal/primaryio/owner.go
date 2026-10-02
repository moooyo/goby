package primaryio

import (
	"context"
	"sync"
)

// OwnerRuntime is registered to one Store lifetime while its Governor can be
// shared across generations. Closing cancels operations and queued admission,
// then waits for explicit actual-owner completion. Deadlines never free leases.
type OwnerRuntime struct {
	mu       sync.Mutex
	governor *Governor
	maximum  int
	closed   bool
	owners   map[*ownerState]struct{}
	drained  chan struct{}
}

func NewOwnerRuntime(governor *Governor, maximumOwners int) (*OwnerRuntime, error) {
	if governor == nil || maximumOwners < 1 || maximumOwners > 8192 {
		return nil, ErrInvalid
	}
	drained := make(chan struct{})
	close(drained)
	return &OwnerRuntime{governor: governor, maximum: maximumOwners, owners: make(map[*ownerState]struct{}), drained: drained}, nil
}

// Register must precede preparation, queueing, descriptor handoff, or actual
// I/O. An owner is never automatically completed by context cancellation.
func (r *OwnerRuntime) Register(ctx context.Context) (*Owner, error) {
	if r == nil || ctx == nil {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrClosed
	}
	if len(r.owners) >= r.maximum {
		return nil, ErrBusy
	}
	if len(r.owners) == 0 {
		r.drained = make(chan struct{})
	}
	work, cancel := context.WithCancel(ctx)
	state := &ownerState{runtime: r, ctx: work, cancel: cancel, generation: 1}
	r.owners[state] = struct{}{}
	return &Owner{state: state, generation: 1}, nil
}

func (r *OwnerRuntime) remove(owner *ownerState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.owners, owner)
	if len(r.owners) == 0 {
		close(r.drained)
	}
}

// Close is safe to retry. Actual readers/processes must observe cancellation,
// stop, join, close owned descriptors, release leases, and call Complete.
func (r *OwnerRuntime) Close(ctx context.Context) error {
	if r == nil || ctx == nil {
		return ErrInvalid
	}
	r.mu.Lock()
	r.closed = true
	owners := make([]*ownerState, 0, len(r.owners))
	for owner := range r.owners {
		owners = append(owners, owner)
	}
	drained := r.drained
	r.mu.Unlock()
	for _, owner := range owners {
		owner.cancel()
	}
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type RuntimeStats struct {
	RegisteredOwners int
	MaximumOwners    int
	Closed           bool
}

// Stats reports retained operation registrations, not a count of descriptors.
// Production adapters need separate finite descriptor and buffer accounting.
func (r *OwnerRuntime) Stats() RuntimeStats {
	if r == nil {
		return RuntimeStats{Closed: true}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return RuntimeStats{len(r.owners), r.maximum, r.closed}
}

type Owner struct {
	state      *ownerState
	generation uint64
}

type ownerState struct {
	mu         sync.Mutex
	runtime    *OwnerRuntime
	ctx        context.Context
	cancel     context.CancelFunc
	acquiring  int
	leases     int
	completed  bool
	generation uint64
}

func (o *Owner) Context() context.Context {
	if o == nil || o.state == nil {
		return nil
	}
	return o.state.ctx
}

// Cancel only signals cancellation. It does not prove that a syscall, HTTP
// copier, child process, publication callback, or descriptor cleanup finished.
func (o *Owner) Cancel() error {
	if o == nil || o.state == nil {
		return ErrInvalid
	}
	state := o.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if o.generation != state.generation {
		return ErrStaleOwner
	}
	state.cancel()
	return nil
}

// Transfer moves an idle retained owner's completion and cancellation rights.
// A stale sender cannot finish registration while the receiver writes a network
// chunk or retains a descriptor between I/O phases. Use TransferLease when an
// active-I/O lease must move with this retained owner.
func (o *Owner) Transfer() (*Owner, error) {
	if o == nil || o.state == nil {
		return nil, ErrInvalid
	}
	state := o.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if o.generation != state.generation {
		return nil, ErrStaleOwner
	}
	if state.completed || state.generation == ^uint64(0) {
		return nil, ErrClosed
	}
	if state.acquiring != 0 || state.leases != 0 {
		return nil, ErrOwnerBusy
	}
	state.generation++
	return &Owner{state: state, generation: state.generation}, nil
}

// TransferLease atomically moves one active phase and its retained owner. The
// common lock order is lease state before owner state, as in Release. Publish
// the returned handles together with the descriptor, then await receiver ACK;
// failed delivery remains the sender's actual cleanup obligation.
func (o *Owner) TransferLease(lease *PrimaryReadLease) (*Owner, *PrimaryReadLease, error) {
	if o == nil || o.state == nil || lease == nil || lease.state == nil {
		return nil, nil, ErrInvalid
	}
	phase := lease.state
	phase.mu.Lock()
	defer phase.mu.Unlock()
	state := o.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if o.generation != state.generation {
		return nil, nil, ErrStaleOwner
	}
	if phase.owner != state || lease.generation != phase.generation {
		return nil, nil, ErrStaleLease
	}
	if state.completed || phase.released || state.generation == ^uint64(0) || phase.generation == ^uint64(0) {
		return nil, nil, ErrClosed
	}
	if state.acquiring != 0 || state.leases != 1 {
		return nil, nil, ErrOwnerBusy
	}
	state.generation++
	phase.generation++
	return &Owner{state: state, generation: state.generation}, &PrimaryReadLease{state: phase, generation: phase.generation}, nil
}

// Acquire charges one operation with all involved roots/domains atomically.
// One Owner cannot hold a lease while waiting for another lease: additional
// roots belong in Route, and concurrent actual operations need separate owners.
func (o *Owner) Acquire(route Route, class Class) (*PrimaryReadLease, error) {
	return o.acquire(o.Context(), route, class, false, false)
}

// AcquireContext applies a phase deadline without canceling the retained owner.
// Owner cancellation still fences admission. A granted lease remains charged
// until actual phase retirement and explicit Release, even after ctx is canceled.
func (o *Owner) AcquireContext(ctx context.Context, route Route, class Class) (*PrimaryReadLease, error) {
	return o.acquire(ctx, route, class, false, true)
}

// TryAcquire charges one available operation without joining the admission
// queue. It returns ErrBusy when capacity or older conflicting work prevents
// immediate admission, and preserves Acquire's retained-owner lifecycle.
func (o *Owner) TryAcquire(route Route, class Class) (*PrimaryReadLease, error) {
	return o.acquire(o.Context(), route, class, true, false)
}

// TryAcquireContext combines a phase deadline with immediate admission. It
// never queues the phase or completes the retained owner after cancellation.
func (o *Owner) TryAcquireContext(ctx context.Context, route Route, class Class) (*PrimaryReadLease, error) {
	return o.acquire(ctx, route, class, true, true)
}

func (o *Owner) acquire(ctx context.Context, route Route, class Class, immediate, phaseContext bool) (*PrimaryReadLease, error) {
	if o == nil || o.state == nil || ctx == nil {
		return nil, ErrInvalid
	}
	state := o.state
	state.mu.Lock()
	if o.generation != state.generation {
		state.mu.Unlock()
		return nil, ErrStaleOwner
	}
	if state.completed {
		state.mu.Unlock()
		return nil, ErrClosed
	}
	if state.leases != 0 || state.acquiring != 0 {
		state.mu.Unlock()
		return nil, ErrOwnerBusy
	}
	state.acquiring++
	state.mu.Unlock()
	if phaseContext {
		phase, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(state.ctx, cancel)
		defer func() { stop(); cancel() }()
		// AfterFunc callbacks run asynchronously. An already canceled owner
		// must fence this acquisition before its callback is scheduled.
		if state.ctx.Err() != nil {
			cancel()
		}
		ctx = phase
	}
	acquire := state.runtime.governor.acquire
	if immediate {
		acquire = state.runtime.governor.tryAcquire
	}
	request, err := acquire(ctx, route, class)
	if err == nil && phaseContext {
		// Owner cancellation may race the merge callback and a grant. No I/O
		// has been handed to the caller yet, so retire that construction charge.
		if ownerErr := state.ctx.Err(); ownerErr != nil {
			state.runtime.governor.release(request)
			err = ownerErr
		}
	}
	state.mu.Lock()
	state.acquiring--
	if err == nil {
		state.leases++
	}
	state.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &PrimaryReadLease{state: &leaseState{owner: state, request: request, generation: 1}, generation: 1}, nil
}

// Complete is an explicit assertion that the actual operation and descriptor
// cleanup finished. It refuses to release registration with outstanding work.
func (o *Owner) Complete() error {
	if o == nil || o.state == nil {
		return ErrInvalid
	}
	state := o.state
	state.mu.Lock()
	if o.generation != state.generation {
		state.mu.Unlock()
		return ErrStaleOwner
	}
	if state.completed {
		state.mu.Unlock()
		return nil
	}
	if state.acquiring != 0 || state.leases != 0 {
		state.mu.Unlock()
		return ErrOwnerBusy
	}
	state.completed = true
	state.mu.Unlock()
	state.cancel()
	state.runtime.remove(state)
	return nil
}

type leaseState struct {
	mu         sync.Mutex
	owner      *ownerState
	request    *request
	generation uint64
	released   bool
}

// PrimaryReadLease follows one actual I/O phase. HTTP releases it after its
// bounded source read returns, before network write, while Owner retains the FD.
// FFmpeg/probe phases include actual process retirement and output-reader join;
// closing one parent *os.File does not prove inherited child readers retired.
type PrimaryReadLease struct {
	state      *leaseState
	generation uint64
}

// Transfer creates the receiver's handle without changing any charge. A stale
// sender/defer cannot release the receiver's lease. Transfer before publication
// of a handoff result; failed delivery remains the sender's cleanup obligation.
func (l *PrimaryReadLease) Transfer() (*PrimaryReadLease, error) {
	if l == nil || l.state == nil {
		return nil, ErrInvalid
	}
	state := l.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if l.generation != state.generation {
		return nil, ErrStaleLease
	}
	if state.released || state.generation == ^uint64(0) {
		return nil, ErrClosed
	}
	state.generation++
	return &PrimaryReadLease{state: state, generation: state.generation}, nil
}

// Release runs after the admitted phase actually retires. Descriptors retained
// between phases remain charged to Owner, not this active-I/O quota. Repeating
// release on the current handle is harmless; Transfer rejects stale release.
// Request cancellation never calls this method.
func (l *PrimaryReadLease) Release() error {
	if l == nil || l.state == nil {
		return ErrInvalid
	}
	state := l.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if l.generation != state.generation {
		return ErrStaleLease
	}
	if state.released {
		return nil
	}
	state.released = true
	owner := state.owner
	owner.runtime.governor.release(state.request)
	owner.mu.Lock()
	owner.leases--
	owner.mu.Unlock()
	return nil
}

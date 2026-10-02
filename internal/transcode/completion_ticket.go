package transcode

import "sync"

const maxCompletionTickets = 4096

type completionTicketPhase uint8

const (
	completionReserved completionTicketPhase = iota
	completionQueued
	completionWorking
	completionReleased
)

type completionTicketSnapshot struct {
	Capacity  int
	Reserved  int
	Queued    int
	Working   int
	Available int
	Closed    bool
}

// completionTicketPool bounds the complete path from launch reservation until
// terminal status handling, including all waiting and active finalizers. A
// process-retirement notification does not release this ticket. The owner must
// acquire before launch and retain the ticket while retirement or cleanup is
// unresolved. The pool complements the manager's admission and metadata limits.
//
// onAvailable must return promptly without I/O. It runs outside the pool lock
// so the scheduler can inspect this pool without reversing an ownership lock.
type completionTicketPool struct {
	mu          sync.Mutex
	capacity    int
	counts      [completionReleased]int
	closed      bool
	onAvailable func()
}

type completionTicket struct {
	pool  *completionTicketPool
	phase completionTicketPhase
}

func newCompletionTicketPool(capacity int, onAvailable func()) (*completionTicketPool, error) {
	if capacity < 1 || capacity > maxCompletionTickets {
		return nil, ErrInvalidOptions
	}
	return &completionTicketPool{capacity: capacity, onAvailable: onAvailable}, nil
}

func (p *completionTicketPool) reserve() (*completionTicket, error) {
	if p == nil {
		return nil, ErrInvalidOptions
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, ErrManagerClosed
	}
	if p.usedLocked() >= p.capacity {
		return nil, ErrBusy
	}
	p.counts[completionReserved]++
	return &completionTicket{pool: p, phase: completionReserved}, nil
}

func (p *completionTicketPool) close() {
	if p != nil {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
	}
}

func (p *completionTicketPool) usedLocked() int {
	return p.counts[completionReserved] + p.counts[completionQueued] + p.counts[completionWorking]
}

func (p *completionTicketPool) snapshot() completionTicketSnapshot {
	if p == nil {
		return completionTicketSnapshot{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return completionTicketSnapshot{Capacity: p.capacity, Reserved: p.counts[completionReserved],
		Queued: p.counts[completionQueued], Working: p.counts[completionWorking],
		Available: p.capacity - p.usedLocked(), Closed: p.closed}
}

// queue is called only after runner-owned writers have drained. A duplicate or
// stale notification cannot enqueue a second finalization using the same ticket.
func (t *completionTicket) queue() bool {
	return t.transition(completionReserved, completionQueued)
}

// work transfers the existing queue reservation to one fixed finalizer worker.
// It neither acquires a new allowance nor authorizes creating another worker.
func (t *completionTicket) work() bool {
	return t.transition(completionQueued, completionWorking)
}

func (t *completionTicket) transition(before, after completionTicketPhase) bool {
	if t == nil || t.pool == nil {
		return false
	}
	p := t.pool
	p.mu.Lock()
	defer p.mu.Unlock()
	if t.phase != before {
		return false
	}
	p.counts[before]--
	p.counts[after]++
	t.phase = after
	return true
}

// release is owned by terminal status handling or a proven pre-launch rollback.
// It is not a process-retirement, writer-drain, directory-deletion, or storage
// reservation release. Failed process cleanup must keep its original ticket.
func (t *completionTicket) release() bool {
	if t == nil || t.pool == nil {
		return false
	}
	p := t.pool
	p.mu.Lock()
	if t.phase == completionReleased {
		p.mu.Unlock()
		return false
	}
	p.counts[t.phase]--
	t.phase = completionReleased
	notify := p.onAvailable
	closed := p.closed
	p.mu.Unlock()
	if !closed && notify != nil {
		notify()
	}
	return true
}

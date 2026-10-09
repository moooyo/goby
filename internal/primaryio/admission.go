// Package primaryio provides an unwired prototype for actual media-root I/O
// admission. It performs no authorization, database access, filesystem work, or
// automatic descriptor retirement. Actual-I/O leases end after the admitted
// phase retires; retained Owner completion follows final descriptor cleanup.
package primaryio

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrInvalid    = errors.New("invalid primary I/O admission input")
	ErrBusy       = errors.New("primary I/O admission queue is full")
	ErrClosed     = errors.New("primary I/O admission is closed")
	ErrStaleLease = errors.New("primary I/O lease ownership was transferred")
	ErrStaleOwner = errors.New("primary I/O retained ownership was transferred")
	ErrOwnerBusy  = errors.New("primary I/O owner still holds acquisitions or leases")
)

// RootKey is stable across Store generations and binding changes. Catalog must
// identify the verified database/schema ownership scope; RootID is catalog data.
type RootKey struct {
	Catalog string
	RootID  string
}

type Class uint8

const (
	Foreground Class = iota
	Background
)

// Route is supplied by a trusted routing adapter after database preparation.
// Domains are opaque configured identities, including all governing ancestors
// and explicitly configured aliases. A route is a charge, never authorization.
// Multiple roots/domains are acquired atomically to avoid nested resource waits.
type Route struct {
	Roots   []RootKey
	Domains []string
}

// preparedRoute owns validated, deduplicated keys. Its slices never leave this
// package and are immutable after preparation, so a retained reader can reuse
// them for each actual-I/O admission without copying caller-owned memory again.
type preparedRoute struct {
	value Route
}

func prepareRoute(route Route) (preparedRoute, error) {
	owned, err := copyRoute(route)
	return preparedRoute{value: owned}, err
}

// Limits are independent of source-open and subprocess concurrency. Background
// limits reserve at least one foreground owner at every governed level. These
// counts bound active owners; they do not promise byte-rate or latency isolation.
type Limits struct {
	Owners                 int
	BackgroundOwners       int
	RootOwners             int
	RootBackgroundOwners   int
	DomainOwners           int
	DomainBackgroundOwners int
	Queued                 int
	RootQueued             int
	DomainQueued           int
}

func (l Limits) valid() bool {
	return l.Owners > 0 && l.Owners <= 4096 &&
		l.BackgroundOwners >= 0 && l.BackgroundOwners < l.Owners &&
		l.RootOwners > 0 && l.RootOwners <= l.Owners &&
		l.RootBackgroundOwners >= 0 && l.RootBackgroundOwners < l.RootOwners && l.RootBackgroundOwners <= l.BackgroundOwners &&
		l.DomainOwners > 0 && l.DomainOwners <= l.Owners &&
		l.DomainBackgroundOwners >= 0 && l.DomainBackgroundOwners < l.DomainOwners && l.DomainBackgroundOwners <= l.BackgroundOwners &&
		l.Queued > 0 && l.Queued <= 4096 &&
		l.RootQueued > 0 && l.RootQueued <= l.Queued &&
		l.DomainQueued > 0 && l.DomainQueued <= l.Queued
}

type laneCount struct{ active, background int }

type request struct {
	ctx      context.Context
	route    Route
	class    Class
	ready    chan struct{}
	granted  bool
	released bool
	err      error
}

// Governor grants every resource dimension under one mutex. Waiting requests
// hold no active quota. Compound waiters fence newer conflicting work so their
// dimensions can drain together. Unrelated domains and foreground reservations
// remain available; no waiting request is charged as an actual I/O owner.
type Governor struct {
	mu            sync.Mutex
	limits        Limits
	closed        bool
	active        laneCount
	roots         map[RootKey]laneCount
	domains       map[string]laneCount
	waiters       []*request
	queuedRoots   map[RootKey]int
	queuedDomains map[string]int
}

func NewGovernor(limits Limits) (*Governor, error) {
	if !limits.valid() {
		return nil, ErrInvalid
	}
	return &Governor{
		limits: limits, roots: make(map[RootKey]laneCount), domains: make(map[string]laneCount),
		queuedRoots: make(map[RootKey]int), queuedDomains: make(map[string]int),
	}, nil
}

// copyRoute bounds and copies trusted keys so mutation of caller-owned slices
// cannot alter queued or granted charges. Repeated keys are counted once.
func copyRoute(route Route) (Route, error) {
	if len(route.Roots) < 1 || len(route.Roots) > 8 || len(route.Domains) < 1 || len(route.Domains) > 16 {
		return Route{}, ErrInvalid
	}
	copy := Route{}
	roots := make(map[RootKey]bool, len(route.Roots))
	domains := make(map[string]bool, len(route.Domains))
	for _, root := range route.Roots {
		if root.Catalog == "" || len(root.Catalog) > 1024 || root.RootID == "" || len(root.RootID) > 256 {
			return Route{}, ErrInvalid
		}
		if !roots[root] {
			roots[root] = true
			copy.Roots = append(copy.Roots, root)
		}
	}
	for _, domain := range route.Domains {
		if domain == "" || len(domain) > 4096 {
			return Route{}, ErrInvalid
		}
		if !domains[domain] {
			domains[domain] = true
			copy.Domains = append(copy.Domains, domain)
		}
	}
	return copy, nil
}

func eligible(count laneCount, active, background int, class Class) bool {
	return count.active < active && (class != Background || count.background < background)
}

func (g *Governor) eligibleLocked(r *request) bool {
	if !eligible(g.active, g.limits.Owners, g.limits.BackgroundOwners, r.class) {
		return false
	}
	for _, root := range r.route.Roots {
		if !eligible(g.roots[root], g.limits.RootOwners, g.limits.RootBackgroundOwners, r.class) {
			return false
		}
	}
	for _, domain := range r.route.Domains {
		if !eligible(g.domains[domain], g.limits.DomainOwners, g.limits.DomainBackgroundOwners, r.class) {
			return false
		}
	}
	return true
}

func routesConflict(first, second Route) bool {
	for _, left := range first.Roots {
		for _, right := range second.Roots {
			if left == right {
				return true
			}
		}
	}
	for _, left := range first.Domains {
		for _, right := range second.Domains {
			if left == right {
				return true
			}
		}
	}
	return false
}

// A background compound claim may fence shared capacity only when its class
// allowance is available. A background owner stuck elsewhere must not prevent
// foreground work from using its reserved capacity on a healthy root/domain.
func (g *Governor) backgroundClassAvailableLocked(r *request) bool {
	if g.active.background >= g.limits.BackgroundOwners {
		return false
	}
	for _, root := range r.route.Roots {
		if g.roots[root].background >= g.limits.RootBackgroundOwners {
			return false
		}
	}
	for _, domain := range r.route.Domains {
		if g.domains[domain].background >= g.limits.DomainBackgroundOwners {
			return false
		}
	}
	return true
}

// Protect foreground reservations rather than treating a background waiter as
// an owner. Existing foreground owners already satisfy a reservation; newer
// foreground demand above it may wait for an older conflicting compound claim.
func (g *Governor) foregroundReserveNeededLocked(older, candidate *request) bool {
	for _, left := range older.route.Roots {
		for _, right := range candidate.route.Roots {
			if left == right {
				count := g.roots[left]
				if count.active-count.background < g.limits.RootOwners-g.limits.RootBackgroundOwners {
					return true
				}
			}
		}
	}
	for _, left := range older.route.Domains {
		for _, right := range candidate.route.Domains {
			if left == right {
				count := g.domains[left]
				if count.active-count.background < g.limits.DomainOwners-g.limits.DomainBackgroundOwners {
					return true
				}
			}
		}
	}
	return false
}

func (g *Governor) blockedByOlderLocked(candidate *request, before int) bool {
	for _, older := range g.waiters[:before] {
		if older.ctx.Err() != nil || len(older.route.Roots) == 1 && len(older.route.Domains) == 1 || !routesConflict(older.route, candidate.route) {
			continue
		}
		if older.class == Foreground || candidate.class == Background {
			return true
		}
		if g.backgroundClassAvailableLocked(older) && !g.foregroundReserveNeededLocked(older, candidate) {
			return true
		}
	}
	return false
}

func (g *Governor) queueAvailableLocked(r *request) bool {
	if len(g.waiters) >= g.limits.Queued {
		return false
	}
	for _, root := range r.route.Roots {
		if g.queuedRoots[root] >= g.limits.RootQueued {
			return false
		}
	}
	for _, domain := range r.route.Domains {
		if g.queuedDomains[domain] >= g.limits.DomainQueued {
			return false
		}
	}
	return true
}

// Queued dimensions are independent of active charges: a compound request may
// still wait after one of its dimensions becomes idle. Zero counts are removed
// so these maps contain only keys charged by the bounded waiting queue.
func (g *Governor) chargeQueuedLocked(r *request, delta int) {
	for _, root := range r.route.Roots {
		count := g.queuedRoots[root] + delta
		if count == 0 {
			delete(g.queuedRoots, root)
		} else {
			g.queuedRoots[root] = count
		}
	}
	for _, domain := range r.route.Domains {
		count := g.queuedDomains[domain] + delta
		if count == 0 {
			delete(g.queuedDomains, domain)
		} else {
			g.queuedDomains[domain] = count
		}
	}
}

func addCount(count laneCount, class Class, delta int) laneCount {
	count.active += delta
	if class == Background {
		count.background += delta
	}
	return count
}

func (g *Governor) chargeLocked(r *request, delta int) {
	g.active = addCount(g.active, r.class, delta)
	for _, root := range r.route.Roots {
		count := addCount(g.roots[root], r.class, delta)
		if count.active == 0 {
			delete(g.roots, root)
		} else {
			g.roots[root] = count
		}
	}
	for _, domain := range r.route.Domains {
		count := addCount(g.domains[domain], r.class, delta)
		if count.active == 0 {
			delete(g.domains, domain)
		} else {
			g.domains[domain] = count
		}
	}
}

func (g *Governor) removeLocked(index int) {
	g.chargeQueuedLocked(g.waiters[index], -1)
	copy(g.waiters[index:], g.waiters[index+1:])
	g.waiters[len(g.waiters)-1] = nil
	g.waiters = g.waiters[:len(g.waiters)-1]
	if len(g.waiters) == 0 && cap(g.waiters) > 64 {
		g.waiters = nil
	}
}

func (g *Governor) dispatchLocked() {
	for index := 0; index < len(g.waiters); {
		r := g.waiters[index]
		if err := r.ctx.Err(); err != nil {
			g.removeLocked(index)
			r.released, r.err = true, err
			close(r.ready)
			continue
		}
		if !g.eligibleLocked(r) || g.blockedByOlderLocked(r, index) {
			index++
			continue
		}
		g.removeLocked(index)
		r.granted = true
		g.chargeLocked(r, 1)
		close(r.ready)
	}
}

// acquire is exposed through Owner.Acquire so every queued/granted request is
// also registered under a Store-compatible runtime close fence.
func (g *Governor) acquire(ctx context.Context, route Route, class Class) (*request, error) {
	if g == nil || ctx == nil || (class != Foreground && class != Background) {
		return nil, ErrInvalid
	}
	prepared, err := prepareRoute(route)
	if err != nil {
		return nil, err
	}
	return g.acquirePrepared(ctx, prepared, class)
}

// The private prepared route changes only key preparation. Every chunk still
// enters the same cancellation, capacity, queue, and fairness checks.
func (g *Governor) acquirePrepared(ctx context.Context, route preparedRoute, class Class) (*request, error) {
	if g == nil || ctx == nil || (class != Foreground && class != Background) || len(route.value.Roots) == 0 || len(route.value.Domains) == 0 {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r := &request{ctx: ctx, route: route.value, class: class}
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return nil, ErrClosed
	}
	g.dispatchLocked()
	queued := false
	if g.eligibleLocked(r) && !g.blockedByOlderLocked(r, len(g.waiters)) {
		r.granted = true
		g.chargeLocked(r, 1)
	} else if !g.queueAvailableLocked(r) {
		g.mu.Unlock()
		return nil, ErrBusy
	} else {
		r.ready = make(chan struct{})
		g.waiters = append(g.waiters, r)
		g.chargeQueuedLocked(r, 1)
		queued = true
	}
	g.mu.Unlock()
	if !queued {
		if err := ctx.Err(); err != nil {
			g.release(r)
			return nil, err
		}
		return r, nil
	}
	select {
	case <-r.ready:
		g.mu.Lock()
		err := r.err
		g.mu.Unlock()
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			g.release(r)
			return nil, err
		}
		return r, nil
	case <-ctx.Done():
		g.release(r)
		return nil, ctx.Err()
	}
}

// tryAcquire uses the same dispatch and compound-waiter fences as acquire, but
// never queues the candidate or waits for a capacity change. Earlier eligible
// requests are dispatched first; unavailable capacity or a fairness fence is busy.
func (g *Governor) tryAcquire(ctx context.Context, route Route, class Class) (*request, error) {
	if g == nil || ctx == nil || (class != Foreground && class != Background) {
		return nil, ErrInvalid
	}
	route, err := copyRoute(route)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r := &request{ctx: ctx, route: route, class: class}
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return nil, ErrClosed
	}
	g.dispatchLocked()
	if err := ctx.Err(); err != nil {
		g.mu.Unlock()
		return nil, err
	}
	if !g.eligibleLocked(r) || g.blockedByOlderLocked(r, len(g.waiters)) {
		g.mu.Unlock()
		return nil, ErrBusy
	}
	r.granted = true
	g.chargeLocked(r, 1)
	g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		g.release(r)
		return nil, err
	}
	return r, nil
}

func (g *Governor) release(r *request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.released {
		return
	}
	r.released = true
	if r.granted {
		g.chargeLocked(r, -1)
	} else {
		for index, queued := range g.waiters {
			if queued == r {
				g.removeLocked(index)
				break
			}
		}
	}
	if !g.closed {
		g.dispatchLocked()
	}
}

// Close fences admission and cancels queued requests. It never releases active
// owners; each OwnerRuntime must cancel and join its own actual operations.
func (g *Governor) Close() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	g.closed = true
	for _, r := range g.waiters {
		r.released, r.err = true, ErrClosed
		close(r.ready)
	}
	for index := range g.waiters {
		g.waiters[index] = nil
	}
	g.waiters = nil
	g.queuedRoots = nil
	g.queuedDomains = nil
}

type Stats struct {
	Active        int
	Background    int
	Queued        int
	ActiveRoots   int
	ActiveDomains int
	Closed        bool
}

func (g *Governor) Stats() Stats {
	if g == nil {
		return Stats{Closed: true}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return Stats{g.active.active, g.active.background, len(g.waiters), len(g.roots), len(g.domains), g.closed}
}

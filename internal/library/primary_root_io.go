package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

// PrimaryRootIO is an opaque, retained operation over committed server-owned
// root mappings. It supplies admission, never authorization. A caller must
// still prove its current task, source, principal and binding before actual I/O.
// Copies share the same handle; only Fork creates another completion right.
type PrimaryRootIO struct {
	handle *primaryRootIOHandle
}

type primaryRootIOHandle struct {
	state  *primaryRootIOState
	closed bool // Protected by state.mu.
}

type primaryRootIORoute struct {
	route  primaryio.Route
	domain string
}

type primaryRootIOState struct {
	mu         sync.Mutex
	retirement sync.Mutex
	owner      *primaryio.Owner
	routes     map[string]primaryRootIORoute
	domains    *originalMediaReadDomainRegistry
	claim      *originalMediaReadDomainClaim
	finish     func()
	handles    int
	pins       int
	observers  int
	active     int
	phase      *primaryRootIOPhase
	unknown    bool
	err        error
	done       chan struct{}
	finalizing bool
	completed  bool
	recovering bool
	faults     uint64
}

type primaryRootIOContextKey struct{}
type primaryRootIOPhaseKey struct{}

type primaryRootIOContext struct {
	operation *PrimaryRootIO
	pins      int // Protected by operation.handle.state.mu.
}

type primaryRootIOPhase struct {
	mu       sync.Mutex
	state    *primaryRootIOState
	route    primaryio.Route
	class    primaryio.Class
	lease    *primaryio.PrimaryReadLease
	claims   []*originalMediaReadDomainClaim
	refs     int
	unknown  bool
	err      error
	finished bool
}

// Failed retirement remains strongly reachable and charged until an independent
// actual retirement proof completes. The finite owner runtimes bound this registry.
var retainedPrimaryRootIO = struct {
	sync.Mutex
	states map[*primaryRootIOState]struct{}
}{states: make(map[*primaryRootIOState]struct{})}

// preparePrimaryRootIO must follow committed database-only preparation. It
// copies at most 256 current root mappings, but each actual phase remains
// bounded to eight roots and sixteen domains. Serial metadata proof therefore
// needs one retained operation, rather than one owner per source or root.
func (s *Store) preparePrimaryRootIO(ctx context.Context, hints []mediaSourceRootHint) (*PrimaryRootIO, error) {
	if ctx == nil || s == nil || len(hints) < 1 || len(hints) > scanReconciliationMaxRoots {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Request cancellation belongs to each Run. An idle handoff or mandatory
	// cleanup can use its own lifetime, while Store shutdown still cancels all
	// phases and retains the Store until actual operation completion.
	work, finish, err := s.beginMediaSourceLifetime(context.WithoutCancel(ctx))
	if err != nil {
		return nil, err
	}
	routes := make(map[string]primaryRootIORoute, len(hints))
	for _, hint := range hints {
		if err := ctx.Err(); err != nil {
			finish()
			return nil, err
		}
		if (rootBindingRow{root: hint.root, revision: hint.bindingRevision}).validateMapping() != nil {
			finish()
			return nil, ErrUnavailable
		}
		if _, exists := routes[hint.root.id]; exists {
			finish()
			return nil, ErrInvalidInput
		}
		route, routeErr := s.primaryReadRouteContext(ctx, hint)
		if routeErr != nil {
			finish()
			return nil, routeErr
		}
		_, domain, routeErr := s.mediaSourceRootLaneContext(ctx, hint)
		if routeErr != nil {
			finish()
			return nil, routeErr
		}
		routes[hint.root.id] = primaryRootIORoute{route: route, domain: domain}
	}
	if err := ctx.Err(); err != nil {
		finish()
		return nil, err
	}
	return newPrimaryRootIO(work, originalMediaReadOwners, &originalMediaReadDomains, routes, finish)
}

// prepareConfiguredPrimaryRootIO accepts only an exact configured Store anchor.
// Its synthetic root key is stable within the catalog and requires no pathname
// resolution. Client paths, canonicalization and filesystem observations never
// choose a new domain or create a routing identity.
func (s *Store) prepareConfiguredPrimaryRootIO(ctx context.Context, configuredAnchor string) (*PrimaryRootIO, error) {
	if ctx == nil || s == nil || configuredAnchor == "" {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	work, finish, err := s.beginMediaSourceLifetime(context.WithoutCancel(ctx))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(configuredAnchor))
	rootID := "configured-" + hex.EncodeToString(digest[:])
	hint := mediaSourceRootHint{root: libraryRoot{id: rootID, path: configuredAnchor,
		allowedPath: configuredAnchor, relativePath: "."}, bindingRevision: 1}
	route, err := s.primaryReadRoute(hint)
	if err != nil {
		finish()
		return nil, err
	}
	_, domain, err := s.mediaSourceRootLane(hint)
	if err != nil {
		finish()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		finish()
		return nil, err
	}
	return newPrimaryRootIO(work, originalMediaReadOwners, &originalMediaReadDomains,
		map[string]primaryRootIORoute{rootID: {route: route, domain: domain}}, finish)
}

func newPrimaryRootIO(ctx context.Context, runtime *primaryio.OwnerRuntime, domains *originalMediaReadDomainRegistry,
	routes map[string]primaryRootIORoute, finish func()) (*PrimaryRootIO, error) {
	return newPrimaryRootIOWithClaimAcquirer(ctx, runtime, domains, routes, finish, nil)
}

func newPrimaryRootIOWithClaimAcquire(ctx context.Context, runtime *primaryio.OwnerRuntime, domains *originalMediaReadDomainRegistry,
	routes map[string]primaryRootIORoute, finish func(), acquire func(string) (*originalMediaReadDomainClaim, error)) (*PrimaryRootIO, error) {
	return newPrimaryRootIOWithClaimAcquirer(ctx, runtime, domains, routes, finish, acquire)
}

// newPrimaryRootIOWithClaimAcquirer is restricted to the bounded source-open
// adapter when acquire is nonnil. Registration precedes exact-domain alias
// acquisition, so a full metadata runtime cannot create additional live aliases.
// The successful acquisition is transferred to the operation; every factory
// failure retires its idle registration and acquired claim before finish.
func newPrimaryRootIOWithClaimAcquirer(ctx context.Context, runtime *primaryio.OwnerRuntime, domains *originalMediaReadDomainRegistry,
	routes map[string]primaryRootIORoute, finish func(), acquire func(string) (*originalMediaReadDomainClaim, error)) (*PrimaryRootIO, error) {
	if ctx == nil || runtime == nil || domains == nil || len(routes) < 1 || len(routes) > scanReconciliationMaxRoots || finish == nil {
		if finish != nil {
			finish()
		}
		return nil, ErrInvalidInput
	}
	if acquire != nil && len(routes) != 1 {
		finish()
		return nil, ErrInvalidInput
	}
	owner, err := runtime.Register(ctx)
	if err != nil {
		finish()
		return nil, primaryRootIOError(err)
	}
	immutable := make(map[string]primaryRootIORoute, len(routes))
	for rootID, prepared := range routes {
		prepared.route.Roots = append([]primaryio.RootKey(nil), prepared.route.Roots...)
		prepared.route.Domains = append([]string(nil), prepared.route.Domains...)
		immutable[rootID] = prepared
	}
	state := &primaryRootIOState{owner: owner, routes: immutable, domains: domains,
		finish: finish, handles: 1, done: make(chan struct{})}
	// A single-source capability retains its canonical mapping while its FD is
	// idle. A serial multi-root metadata operation claims only the actual phase;
	// it must close every temporary FD before that phase's callback returns.
	if len(routes) == 1 {
		for _, route := range routes {
			claimAcquire := acquire
			if claimAcquire == nil {
				claimAcquire = domains.acquire
			}
			state.claim, err = claimAcquire(route.domain)
			if err == nil && (state.claim == nil || state.claim.registry != domains || state.claim.domain != route.domain) {
				err = ErrUnavailable
			}
		}
		if err != nil {
			state.claim.release()
			err = errors.Join(err, owner.Complete())
			finish()
			return nil, err
		}
	}
	return &PrimaryRootIO{handle: &primaryRootIOHandle{state: state}}, nil
}

func primaryRootIOError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, primaryio.ErrBusy) || errors.Is(err, primaryio.ErrOwnerBusy) {
		return fmt.Errorf("%w: primary root I/O admission: %w", ErrBusy, err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w: primary root I/O admission: %w", ErrUnavailable, err)
}

// Context attaches this capability to an actual consumer lifetime. With no
// argument it returns the Store-owned lifetime. It does not acquire a lease.
func (operation *PrimaryRootIO) Context(contexts ...context.Context) context.Context {
	if operation == nil || operation.handle == nil || operation.handle.state == nil || len(contexts) > 1 {
		return nil
	}
	ctx := operation.handle.state.owner.Context()
	if len(contexts) == 1 {
		ctx = contexts[0]
	}
	if ctx == nil {
		return nil
	}
	capability := &primaryRootIOContext{operation: operation}
	ctx = context.WithValue(ctx, primaryRootIOContextKey{}, capability)
	if phase, _ := ctx.Value(primaryRootIOPhaseKey{}).(*primaryRootIOPhase); phase != nil {
		if phase.state != operation.handle.state {
			return withStorageObservationPhaseRetention(ctx, func() (func() error, error) {
				return nil, primaryRootIOError(primaryio.ErrOwnerBusy)
			})
		}
		// Reattaching a capability inside its existing phase must preserve the
		// active lease reference, rather than replacing it with an idle pin.
		return withStorageObservationPhaseRetention(ctx, phase.retainObserver)
	}
	return withStorageObservationPhaseRetention(ctx, func() (func() error, error) {
		state := operation.handle.state
		state.mu.Lock()
		if operation.handle.closed || state.unknown || state.err != nil {
			state.mu.Unlock()
			return nil, ErrUnavailable
		}
		if state.observers != 0 {
			state.mu.Unlock()
			return nil, ErrBusy
		}
		state.observers++
		state.pins++
		capability.pins++
		state.mu.Unlock()
		var once sync.Once
		return func() error {
			once.Do(func() {
				state.mu.Lock()
				state.pins--
				state.observers--
				capability.pins--
				state.mu.Unlock()
			})
			return state.tryComplete()
		}, nil
	})
}

// Fork creates another completion right without registering another owner.
// It grants no new route or authority. The caller closes it only after its
// actual descriptor, syscall, copier or child retirement has completed.
func (operation *PrimaryRootIO) Fork(contexts ...context.Context) (*PrimaryRootIO, error) {
	if operation == nil || operation.handle == nil || operation.handle.state == nil || len(contexts) > 1 {
		return nil, ErrInvalidInput
	}
	if len(contexts) == 1 {
		if contexts[0] == nil {
			return nil, ErrInvalidInput
		}
		if err := contexts[0].Err(); err != nil {
			return nil, err
		}
	}
	state := operation.handle.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if operation.handle.closed || state.unknown || state.err != nil {
		return nil, ErrUnavailable
	}
	state.handles++
	return &PrimaryRootIO{handle: &primaryRootIOHandle{state: state}}, nil
}

func PrimaryRootIOFromContext(ctx context.Context) *PrimaryRootIO {
	if ctx == nil {
		return nil
	}
	capability, _ := ctx.Value(primaryRootIOContextKey{}).(*primaryRootIOContext)
	if capability == nil {
		return nil
	}
	return capability.operation
}

func RetainPrimaryRootIO(ctx context.Context) (*PrimaryRootIO, error) {
	operation := PrimaryRootIOFromContext(ctx)
	if operation == nil {
		return nil, ErrUnavailable
	}
	return operation.Fork(ctx)
}

func RunPrimaryRootIO(ctx context.Context, class primaryio.Class, work func(context.Context) error) error {
	operation := PrimaryRootIOFromContext(ctx)
	if operation == nil {
		return ErrUnavailable
	}
	return operation.Run(ctx, "", class, work)
}

func (operation *PrimaryRootIO) Run(ctx context.Context, rootID string, class primaryio.Class, work func(context.Context) error) error {
	return operation.run(ctx, []string{rootID}, class, work, false)
}

// RunImmediate never waits or queues and performs no SQL, Store locking or
// filesystem work before work. Busy must roll back the caller's transaction;
// any retry and queued admission happen outside database ownership.
func (operation *PrimaryRootIO) RunImmediate(ctx context.Context, rootID string, class primaryio.Class, work func(context.Context) error) error {
	return operation.run(ctx, []string{rootID}, class, work, true)
}

func (operation *PrimaryRootIO) RunRoots(ctx context.Context, rootIDs []string, class primaryio.Class, work func(context.Context) error) error {
	return operation.run(ctx, rootIDs, class, work, false)
}

func (operation *PrimaryRootIO) RunRootsImmediate(ctx context.Context, rootIDs []string, class primaryio.Class, work func(context.Context) error) error {
	return operation.run(ctx, rootIDs, class, work, true)
}

func (state *primaryRootIOState) route(rootIDs []string) (primaryio.Route, []string, error) {
	if len(rootIDs) < 1 || len(rootIDs) > 8 {
		return primaryio.Route{}, nil, ErrInvalidInput
	}
	var combined primaryio.Route
	var domains []string
	seenRoots, seenDomains, seenClaims := map[primaryio.RootKey]bool{}, map[string]bool{}, map[string]bool{}
	for _, rootID := range rootIDs {
		if rootID == "" && len(state.routes) == 1 {
			for selected := range state.routes {
				rootID = selected
			}
		}
		prepared, ok := state.routes[rootID]
		if !ok {
			return primaryio.Route{}, nil, ErrUnavailable
		}
		for _, root := range prepared.route.Roots {
			if !seenRoots[root] {
				seenRoots[root] = true
				combined.Roots = append(combined.Roots, root)
			}
		}
		for _, domain := range prepared.route.Domains {
			if !seenDomains[domain] {
				seenDomains[domain] = true
				combined.Domains = append(combined.Domains, domain)
			}
		}
		if !seenClaims[prepared.domain] {
			seenClaims[prepared.domain] = true
			domains = append(domains, prepared.domain)
		}
	}
	if len(combined.Roots) < 1 || len(combined.Roots) > 8 || len(combined.Domains) < 1 || len(combined.Domains) > 16 {
		return primaryio.Route{}, nil, ErrUnavailable
	}
	return combined, domains, nil
}

func primaryRootIOSubset(subset, complete primaryio.Route) bool {
	for _, root := range subset.Roots {
		found := false
		for _, held := range complete.Roots {
			found = found || root == held
		}
		if !found {
			return false
		}
	}
	for _, domain := range subset.Domains {
		found := false
		for _, held := range complete.Domains {
			found = found || domain == held
		}
		if !found {
			return false
		}
	}
	return true
}

// Run is synchronous with work. work must join every actual reader/process and
// close temporary descriptors before returning, or transfer a worker reference
// through the installed observation hook. A canceled caller never releases an
// active worker's lease. Nested exact/subset phases reuse the existing charge.
func (operation *PrimaryRootIO) run(ctx context.Context, rootIDs []string, class primaryio.Class,
	work func(context.Context) error, immediate bool) (resultErr error) {
	if operation == nil || operation.handle == nil || operation.handle.state == nil || ctx == nil || work == nil ||
		class != primaryio.Foreground && class != primaryio.Background {
		return ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	state := operation.handle.state
	route, domains, err := state.route(rootIDs)
	if err != nil {
		return err
	}
	if held, _ := ctx.Value(primaryRootIOPhaseKey{}).(*primaryRootIOPhase); held != nil {
		if held.state != state || held.class != class || !primaryRootIOSubset(route, held.route) {
			return primaryRootIOError(primaryio.ErrOwnerBusy)
		}
		if err := held.retain(); err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, held.release()) }()
		completed := false
		defer func() {
			if !completed {
				held.markUnknown(ErrUnavailable)
			}
		}()
		resultErr = work(ctx)
		completed = true
		if errors.Is(resultErr, media.ErrProcessRetirementUnknown) {
			held.markUnknown(resultErr)
		}
		return resultErr
	}
	state.mu.Lock()
	capability, _ := ctx.Value(primaryRootIOContextKey{}).(*primaryRootIOContext)
	pinned := capability != nil && capability.operation != nil && capability.operation.handle != nil &&
		capability.operation.handle.state == state && capability.pins > 0
	if operation.handle.closed && !pinned || state.unknown || state.err != nil {
		state.mu.Unlock()
		return ErrUnavailable
	}
	if state.active != 0 {
		state.mu.Unlock()
		return primaryRootIOError(primaryio.ErrOwnerBusy)
	}
	state.active++
	phase := &primaryRootIOPhase{state: state, route: route, class: class, refs: 1}
	state.phase = phase
	state.mu.Unlock()
	defer func() { resultErr = errors.Join(resultErr, phase.release()) }()
	if state.claim == nil {
		for _, domain := range domains {
			claim, err := state.domains.acquire(domain)
			if err != nil {
				return err
			}
			phase.claims = append(phase.claims, claim)
		}
	}
	if immediate {
		phase.lease, err = state.owner.TryAcquireContext(ctx, route, class)
	} else {
		phase.lease, err = state.owner.AcquireContext(ctx, route, class)
	}
	if err != nil {
		return primaryRootIOError(err)
	}
	workCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(state.owner.Context(), cancel)
	defer func() { stop(); cancel() }()
	workCtx = context.WithValue(workCtx, primaryRootIOContextKey{}, &primaryRootIOContext{operation: operation})
	workCtx = context.WithValue(workCtx, primaryRootIOPhaseKey{}, phase)
	workCtx = withStorageObservationPhaseRetention(workCtx, phase.retainObserver)
	completed := false
	defer func() {
		if !completed {
			phase.markUnknown(ErrUnavailable)
		}
	}()
	resultErr = work(workCtx)
	completed = true
	if errors.Is(resultErr, media.ErrProcessRetirementUnknown) {
		phase.markUnknown(resultErr)
	}
	return errors.Join(resultErr, workCtx.Err())
}

func (phase *primaryRootIOPhase) retain() error {
	phase.mu.Lock()
	defer phase.mu.Unlock()
	if phase.refs == 0 || phase.unknown {
		return ErrUnavailable
	}
	phase.refs++
	return nil
}

// One serial operation can transfer only one independent observation worker.
// A copied context does not create unlimited workers behind one retained owner
// or shared active lease. Synchronous nested subset Run calls remain reusable.
func (phase *primaryRootIOPhase) retainObserver() (func() error, error) {
	state := phase.state
	state.mu.Lock()
	if state.unknown || state.completed || state.err != nil {
		state.mu.Unlock()
		return nil, ErrUnavailable
	}
	if state.observers != 0 {
		state.mu.Unlock()
		return nil, ErrBusy
	}
	state.observers++
	state.mu.Unlock()
	if err := phase.retain(); err != nil {
		state.mu.Lock()
		state.observers--
		state.mu.Unlock()
		_ = state.tryComplete()
		return nil, err
	}
	var once sync.Once
	var releaseErr error
	return func() error {
		once.Do(func() {
			releaseErr = phase.release()
			state.mu.Lock()
			state.observers--
			state.mu.Unlock()
			releaseErr = errors.Join(releaseErr, state.tryComplete())
		})
		return releaseErr
	}, nil
}

func (phase *primaryRootIOPhase) markUnknown(err error) {
	phase.state.retirement.Lock()
	defer phase.state.retirement.Unlock()
	phase.markUnknownLocked(err)
}

func (phase *primaryRootIOPhase) markUnknownLocked(err error) {
	phase.mu.Lock()
	phase.unknown = true
	phase.err = errors.Join(phase.err, err)
	phase.mu.Unlock()
	phase.state.markUnknownLocked(err)
}

func (phase *primaryRootIOPhase) release() error {
	phase.mu.Lock()
	phase.refs--
	last, unknown, err := phase.refs == 0, phase.unknown, phase.err
	phase.mu.Unlock()
	if !last || unknown {
		return err
	}
	return phase.finish()
}

func (phase *primaryRootIOPhase) finish() error {
	phase.state.retirement.Lock()
	err := phase.finishLocked()
	phase.state.retirement.Unlock()
	if err != nil {
		return err
	}
	return phase.state.tryComplete()
}

func (phase *primaryRootIOPhase) finishLocked() error {
	phase.mu.Lock()
	if phase.finished {
		phase.mu.Unlock()
		return nil
	}
	if phase.refs != 0 || phase.unknown {
		phase.mu.Unlock()
		return ErrBusy
	}
	phase.finished = true
	phase.mu.Unlock()
	if phase.lease != nil {
		if err := phase.lease.Release(); err != nil {
			phase.markUnknownLocked(err)
			return err
		}
	}
	for _, claim := range phase.claims {
		claim.release()
	}
	phase.state.mu.Lock()
	phase.state.active--
	phase.state.phase = nil
	phase.state.mu.Unlock()
	return nil
}

func (state *primaryRootIOState) markUnknown(err error) {
	state.retirement.Lock()
	defer state.retirement.Unlock()
	state.markUnknownLocked(err)
}

func (state *primaryRootIOState) markUnknownLocked(err error) {
	state.mu.Lock()
	state.unknown = true
	state.faults++
	state.err = errors.Join(state.err, ErrUnavailable, err)
	state.mu.Unlock()
	retainedPrimaryRootIO.Lock()
	retainedPrimaryRootIO.states[state] = struct{}{}
	retainedPrimaryRootIO.Unlock()
}

// MarkUnknown preserves the retained operation when actual FD or process
// retirement cannot be proved. Close cannot turn unknown retirement into idle.
func (operation *PrimaryRootIO) MarkUnknown(err error) error {
	if operation == nil || operation.handle == nil || operation.handle.state == nil || err == nil {
		return ErrInvalidInput
	}
	state := operation.handle.state
	state.retirement.Lock()
	defer state.retirement.Unlock()
	state.mu.Lock()
	if state.completed || state.finalizing {
		state.mu.Unlock()
		return ErrInvalidInput
	}
	phase := state.phase
	state.mu.Unlock()
	if phase != nil {
		phase.markUnknownLocked(err)
	} else {
		state.markUnknownLocked(err)
	}
	return errors.Join(ErrUnavailable, err)
}

// ConfirmRetired performs independent actual retirement after an unknown
// result. proof must join all children, copiers and filesystem workers and
// close the owned descriptors; cancellation or a delivered signal is no proof.
// Historical failure remains observable and this operation admits no new I/O.
func (operation *PrimaryRootIO) ConfirmRetired(proof func() error) error {
	if operation == nil || operation.handle == nil || operation.handle.state == nil || proof == nil {
		return ErrInvalidInput
	}
	state := operation.handle.state
	state.mu.Lock()
	if !state.unknown || state.completed {
		state.mu.Unlock()
		return ErrInvalidInput
	}
	if state.pins != 0 || state.observers != 0 || state.recovering {
		state.mu.Unlock()
		return ErrBusy
	}
	state.recovering = true
	phase, faults := state.phase, state.faults
	state.mu.Unlock()
	defer func() {
		state.mu.Lock()
		state.recovering = false
		state.mu.Unlock()
	}()
	if phase != nil {
		phase.mu.Lock()
		active := phase.refs != 0
		phase.mu.Unlock()
		if active {
			return ErrBusy
		}
	}
	if err := proof(); err != nil {
		state.markUnknown(err)
		return err
	}
	// Fault publication and actual phase release share this fence. A fault
	// appearing during proof cannot be overwritten by successful recovery.
	state.retirement.Lock()
	state.mu.Lock()
	if state.faults != faults {
		state.mu.Unlock()
		state.retirement.Unlock()
		return ErrUnavailable
	}
	state.unknown = false
	state.mu.Unlock()
	if phase != nil {
		phase.mu.Lock()
		phase.unknown = false
		phase.finished = false
		phase.mu.Unlock()
		if err := phase.finishLocked(); err != nil {
			// Historical evidence failure does not invalidate the new actual
			// retirement proof. A new unknown flag does retain ownership.
			state.mu.Lock()
			unknown := state.unknown
			state.mu.Unlock()
			if unknown {
				state.retirement.Unlock()
				return err
			}
		}
	}
	retainedPrimaryRootIO.Lock()
	delete(retainedPrimaryRootIO.states, state)
	retainedPrimaryRootIO.Unlock()
	state.retirement.Unlock()
	completionErr := state.tryComplete()
	state.mu.Lock()
	unknown, currentErr := state.unknown, state.err
	state.mu.Unlock()
	if unknown {
		return errors.Join(completionErr, currentErr)
	}
	return nil
}

func (state *primaryRootIOState) tryComplete() error {
	state.retirement.Lock()
	defer state.retirement.Unlock()
	state.mu.Lock()
	ready := state.handles == 0 && state.pins == 0 && state.observers == 0 && state.active == 0 && !state.unknown && !state.finalizing && !state.completed
	err := state.err
	if ready {
		state.finalizing = true
	}
	state.mu.Unlock()
	if ready {
		if err := state.owner.Complete(); err != nil {
			state.mu.Lock()
			state.finalizing = false
			state.mu.Unlock()
			state.markUnknownLocked(err)
			return err
		}
		state.claim.release()
		state.finish()
		state.mu.Lock()
		state.completed = true
		state.finalizing = false
		state.mu.Unlock()
		close(state.done)
	}
	state.mu.Lock()
	err = state.err
	state.mu.Unlock()
	return err
}

// Close retires this handle only. Worker references, active leases and Forks
// retain the owner, canonical claim and Store until actual cleanup completes.
func (operation *PrimaryRootIO) Close() error {
	if operation == nil {
		return nil
	}
	if operation.handle == nil || operation.handle.state == nil {
		return ErrInvalidInput
	}
	state := operation.handle.state
	state.mu.Lock()
	if !operation.handle.closed {
		operation.handle.closed = true
		state.handles--
	}
	state.mu.Unlock()
	return state.tryComplete()
}

func (operation PrimaryRootIO) MarshalJSON() ([]byte, error) { return nil, ErrInvalidInput }
func (operation *PrimaryRootIO) UnmarshalJSON([]byte) error  { return ErrInvalidInput }

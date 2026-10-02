package primaryio

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// A bounded result wait also detects accidentally routing TryAcquire through
// the queued API, without letting that regression block the test indefinitely.
func primaryIOTryAcquire(t *testing.T, owner *Owner, route Route, class Class) primaryIOAcquireResult {
	t.Helper()
	result := make(chan primaryIOAcquireResult, 1)
	go func() {
		lease, err := owner.TryAcquire(route, class)
		result <- primaryIOAcquireResult{lease, err}
	}()
	return primaryIOAwaitAcquire(t, result)
}

func primaryIOAssertTryBusy(t *testing.T, owner *Owner, route Route, class Class) {
	t.Helper()
	result := primaryIOTryAcquire(t, owner, route, class)
	if !errors.Is(result.err, ErrBusy) || result.lease != nil {
		t.Fatalf("immediate admission = %+v, want busy without a lease", result)
	}
	owner.state.mu.Lock()
	acquiring, leases := owner.state.acquiring, owner.state.leases
	owner.state.mu.Unlock()
	if acquiring != 0 || leases != 0 {
		t.Fatalf("busy immediate admission retained work: acquiring=%d leases=%d", acquiring, leases)
	}
}

func TestPrimaryIOTryAcquireBusyDoesNotQueueOrRetainAcquisition(t *testing.T) {
	g, owners := primaryIOFixture(t, primaryIOLimits())
	route := primaryIORoute("source", "disk")
	holder := primaryIOOwner(t, owners, context.Background())
	first := primaryIOAcquire(t, holder, route, Background)
	candidate := primaryIOOwner(t, owners, context.Background())
	primaryIOAssertTryBusy(t, candidate, route, Background)
	primaryIOAssertCounts(t, g, 1, 1, 0)

	// A rejected phase leaves its retained owner idle and transferable.
	receiver, err := candidate.Transfer()
	if err != nil {
		t.Fatalf("transfer after busy immediate admission: %v", err)
	}
	primaryIORelease(t, first)
	result := primaryIOTryAcquire(t, receiver, route, Background)
	if result.err != nil || result.lease == nil {
		t.Fatalf("immediate admission after actual capacity retires: %+v", result)
	}
	primaryIOAssertCounts(t, g, 1, 1, 0)
	primaryIORelease(t, result.lease)
	primaryIOComplete(t, holder)
	primaryIOComplete(t, receiver)
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

func TestPrimaryIOTryAcquireHonorsOlderCompoundAndQueuedFollower(t *testing.T) {
	limits := primaryIOLimits()
	limits.RootOwners, limits.RootBackgroundOwners = 1, 0
	limits.DomainOwners, limits.DomainBackgroundOwners = 1, 0
	g, owners := primaryIOFixture(t, limits)
	holder := primaryIOOwner(t, owners, context.Background())
	holderLease := primaryIOAcquire(t, holder, primaryIORoute("b", "db"), Foreground)
	compound := primaryIOOwner(t, owners, context.Background())
	compoundResult := primaryIOQueuedAcquire(t, compound, primaryIOAdmissionRoute([]string{"a", "b"}, []string{"da", "db"}), Foreground)
	follower := primaryIOOwner(t, owners, context.Background())
	followerResult := primaryIOQueuedAcquire(t, follower, primaryIORoute("a", "da"), Foreground)
	candidate := primaryIOOwner(t, owners, context.Background())
	primaryIOAssertTryBusy(t, candidate, primaryIORoute("a", "da"), Foreground)
	primaryIOAssertCounts(t, g, 1, 0, 2)
	primaryIOAssertPending(t, compoundResult)
	primaryIOAssertPending(t, followerResult)

	healthy := primaryIOOwner(t, owners, context.Background())
	healthyResult := primaryIOTryAcquire(t, healthy, primaryIORoute("c", "dc"), Foreground)
	if healthyResult.err != nil || healthyResult.lease == nil {
		t.Fatalf("unrelated immediate admission behind a compound fence: %+v", healthyResult)
	}
	primaryIOAssertCounts(t, g, 2, 0, 2)
	primaryIORelease(t, holderLease)
	older := primaryIOAwaitAcquire(t, compoundResult)
	if older.err != nil || older.lease == nil {
		t.Fatalf("older compound admission: %+v", older)
	}
	primaryIOAssertPending(t, followerResult)
	primaryIOAssertTryBusy(t, candidate, primaryIORoute("a", "da"), Foreground)
	primaryIORelease(t, older.lease)
	next := primaryIOAwaitAcquire(t, followerResult)
	if next.err != nil || next.lease == nil {
		t.Fatalf("queued follower admission: %+v", next)
	}
	primaryIOAssertTryBusy(t, candidate, primaryIORoute("a", "da"), Foreground)
	primaryIORelease(t, next.lease)
	later := primaryIOTryAcquire(t, candidate, primaryIORoute("a", "da"), Foreground)
	if later.err != nil || later.lease == nil {
		t.Fatalf("immediate admission after older conflicting claims retire: %+v", later)
	}
	primaryIORelease(t, later.lease)
	primaryIORelease(t, healthyResult.lease)
	for _, owner := range []*Owner{holder, compound, follower, candidate, healthy} {
		primaryIOComplete(t, owner)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

func TestPrimaryIOTryAcquirePreservesBackgroundCompoundForegroundReservation(t *testing.T) {
	g, owners := primaryIOFixture(t, primaryIOLimits())
	b1 := primaryIOOwner(t, owners, context.Background())
	b2 := primaryIOOwner(t, owners, context.Background())
	b1Lease := primaryIOAcquire(t, b1, primaryIORoute("b", "db"), Foreground)
	b2Lease := primaryIOAcquire(t, b2, primaryIORoute("b", "db"), Foreground)
	compound := primaryIOOwner(t, owners, context.Background())
	compoundResult := primaryIOQueuedAcquire(t, compound, primaryIOAdmissionRoute([]string{"a", "b"}, []string{"da", "db"}), Background)
	reserved := primaryIOOwner(t, owners, context.Background())
	reservedResult := primaryIOTryAcquire(t, reserved, primaryIORoute("a", "da"), Foreground)
	if reservedResult.err != nil || reservedResult.lease == nil {
		t.Fatalf("local foreground reservation was fenced by a background waiter: %+v", reservedResult)
	}
	surplus := primaryIOOwner(t, owners, context.Background())
	primaryIOAssertTryBusy(t, surplus, primaryIORoute("a", "da"), Foreground)
	primaryIOAssertTryBusy(t, surplus, primaryIORoute("a", "da"), Background)
	primaryIOAssertCounts(t, g, 3, 0, 1)
	primaryIOAssertPending(t, compoundResult)
	primaryIORelease(t, b1Lease)
	older := primaryIOAwaitAcquire(t, compoundResult)
	if older.err != nil || older.lease == nil {
		t.Fatalf("older background compound after actual capacity retires: %+v", older)
	}
	primaryIORelease(t, older.lease)
	primaryIORelease(t, reservedResult.lease)
	primaryIORelease(t, b2Lease)
	for _, owner := range []*Owner{b1, b2, compound, reserved, surplus} {
		primaryIOComplete(t, owner)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

func TestPrimaryIOTryAcquireHealthyRouteIgnoresFullQueue(t *testing.T) {
	limits := primaryIOLimits()
	limits.RootOwners, limits.RootBackgroundOwners = 1, 0
	limits.DomainOwners, limits.DomainBackgroundOwners = 1, 0
	limits.Queued, limits.RootQueued, limits.DomainQueued = 1, 1, 1
	g, owners := primaryIOFixture(t, limits)
	holder := primaryIOOwner(t, owners, context.Background())
	holderLease := primaryIOAcquire(t, holder, primaryIORoute("b", "db"), Foreground)
	queued := primaryIOOwner(t, owners, context.Background())
	queuedResult := primaryIOQueuedAcquire(t, queued, primaryIOAdmissionRoute([]string{"a", "b"}, []string{"da", "db"}), Foreground)
	healthy := primaryIOOwner(t, owners, context.Background())
	result := primaryIOTryAcquire(t, healthy, primaryIORoute("c", "dc"), Foreground)
	if result.err != nil || result.lease == nil {
		t.Fatalf("available immediate phase consulted queue capacity: %+v", result)
	}
	primaryIOAssertCounts(t, g, 2, 0, 1)
	if err := queued.Cancel(); err != nil {
		t.Fatal(err)
	}
	canceled := primaryIOAwaitAcquire(t, queuedResult)
	if !errors.Is(canceled.err, context.Canceled) || canceled.lease != nil {
		t.Fatalf("canceled queue result: %+v", canceled)
	}
	primaryIORelease(t, result.lease)
	primaryIORelease(t, holderLease)
	for _, owner := range []*Owner{holder, queued, healthy} {
		primaryIOComplete(t, owner)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

func TestPrimaryIOTryAcquireFailuresLeaveOwnerIdle(t *testing.T) {
	for _, test := range []struct {
		name  string
		route Route
		class Class
		setup func(*Governor, *Owner)
		want  error
	}{
		{name: "invalid_route", route: Route{}, class: Foreground, want: ErrInvalid},
		{name: "invalid_class", route: primaryIORoute("source", "disk"), class: Class(255), want: ErrInvalid},
		{name: "canceled", route: primaryIORoute("source", "disk"), class: Foreground, setup: func(_ *Governor, owner *Owner) { _ = owner.Cancel() }, want: context.Canceled},
		{name: "closed_governor", route: primaryIORoute("source", "disk"), class: Foreground, setup: func(g *Governor, _ *Owner) { g.Close() }, want: ErrClosed},
	} {
		t.Run(test.name, func(t *testing.T) {
			g, owners := primaryIOFixture(t, primaryIOLimits())
			owner := primaryIOOwner(t, owners, context.Background())
			if test.setup != nil {
				test.setup(g, owner)
			}
			result := primaryIOTryAcquire(t, owner, test.route, test.class)
			if !errors.Is(result.err, test.want) || result.lease != nil {
				t.Fatalf("rejected immediate admission: %+v, want %v", result, test.want)
			}
			primaryIOAssertCounts(t, g, 0, 0, 0)
			receiver, err := owner.Transfer()
			if err != nil {
				t.Fatalf("rejected acquisition left owner busy: %v", err)
			}
			primaryIOComplete(t, receiver)
		})
	}
	var owner *Owner
	if lease, err := owner.TryAcquire(primaryIORoute("source", "disk"), Foreground); !errors.Is(err, ErrInvalid) || lease != nil {
		t.Fatalf("nil owner immediate admission: lease=%v err=%v", lease, err)
	}
	var g *Governor
	if request, err := g.tryAcquire(context.Background(), primaryIORoute("source", "disk"), Foreground); !errors.Is(err, ErrInvalid) || request != nil {
		t.Fatalf("nil governor immediate admission: request=%v err=%v", request, err)
	}
	g, _ = primaryIOFixture(t, primaryIOLimits())
	if request, err := g.tryAcquire(nil, primaryIORoute("source", "disk"), Foreground); !errors.Is(err, ErrInvalid) || request != nil {
		t.Fatalf("nil context immediate admission: request=%v err=%v", request, err)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

type primaryIOTryContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (ctx *primaryIOTryContext) Err() error {
	err := ctx.Context.Err()
	ctx.once.Do(func() { close(ctx.entered) })
	return err
}

func TestPrimaryIOTryAcquireCanceledAtGovernorMutexLeavesNoCharge(t *testing.T) {
	g, owners := primaryIOFixture(t, primaryIOLimits())
	owner := primaryIOOwner(t, owners, context.Background())
	owner.state.mu.Lock()
	observed := &primaryIOTryContext{Context: owner.state.ctx, entered: make(chan struct{})}
	owner.state.ctx = observed
	owner.state.mu.Unlock()
	// Err announces that Owner has installed its acquiring fence. Keep the
	// governor locked until cancellation, without timing sleeps or polling.
	g.mu.Lock()
	locked := true
	defer func() {
		if locked {
			g.mu.Unlock()
		}
	}()
	result := make(chan primaryIOAcquireResult, 1)
	go func() {
		lease, err := owner.TryAcquire(primaryIORoute("source", "disk"), Foreground)
		result <- primaryIOAcquireResult{lease, err}
	}()
	primaryIOAwaitClosed(t, observed.entered)
	if err := owner.Complete(); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("completion crossed immediate acquisition fence: %v", err)
	}
	if _, err := owner.Transfer(); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("transfer crossed immediate acquisition fence: %v", err)
	}
	if err := owner.Cancel(); err != nil {
		t.Fatal(err)
	}
	g.mu.Unlock()
	locked = false
	value := primaryIOAwaitAcquire(t, result)
	if !errors.Is(value.err, context.Canceled) || value.lease != nil {
		t.Fatalf("immediate admission after mutex-wait cancellation: %+v", value)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
	primaryIOComplete(t, owner)
}

func TestPrimaryIOTryAcquireCopiesAndDeduplicatesRoute(t *testing.T) {
	limits := primaryIOLimits()
	limits.RootOwners, limits.RootBackgroundOwners = 1, 0
	limits.DomainOwners, limits.DomainBackgroundOwners = 1, 0
	g, owners := primaryIOFixture(t, limits)
	owner := primaryIOOwner(t, owners, context.Background())
	root := RootKey{Catalog: "catalog", RootID: "source"}
	route := Route{Roots: []RootKey{root, root}, Domains: []string{"disk", "disk"}}
	first := primaryIOTryAcquire(t, owner, route, Foreground)
	if first.err != nil || first.lease == nil {
		t.Fatalf("immediate admission with repeated trusted keys: %+v", first)
	}
	g.mu.Lock()
	rootCount, domainCount := g.roots[root], g.domains["disk"]
	g.mu.Unlock()
	if rootCount.active != 1 || domainCount.active != 1 {
		t.Fatalf("repeated route keys were charged more than once: root=%+v domain=%+v", rootCount, domainCount)
	}
	for index := range route.Roots {
		route.Roots[index] = RootKey{Catalog: "catalog", RootID: "other"}
	}
	for index := range route.Domains {
		route.Domains[index] = "other-disk"
	}
	candidate := primaryIOOwner(t, owners, context.Background())
	primaryIOAssertTryBusy(t, candidate, primaryIORoute("source", "disk"), Foreground)
	second := primaryIOTryAcquire(t, candidate, primaryIORoute("other", "other-disk"), Foreground)
	if second.err != nil || second.lease == nil {
		t.Fatalf("caller slice mutation altered the admitted route: %+v", second)
	}
	primaryIORelease(t, first.lease)
	primaryIOAssertCounts(t, g, 1, 0, 0)
	primaryIORelease(t, second.lease)
	primaryIOComplete(t, owner)
	primaryIOComplete(t, candidate)
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

func TestPrimaryIOTryAcquireLeaseRetainsCancelAndTransferLifecycle(t *testing.T) {
	g, owners := primaryIOFixture(t, primaryIOLimits())
	sender := primaryIOOwner(t, owners, context.Background())
	route := primaryIORoute("source", "disk")
	result := primaryIOTryAcquire(t, sender, route, Background)
	if result.err != nil || result.lease == nil {
		t.Fatalf("initial immediate admission: %+v", result)
	}
	if lease, err := sender.TryAcquire(route, Background); !errors.Is(err, ErrOwnerBusy) || lease != nil {
		t.Fatalf("nested immediate admission: lease=%v err=%v", lease, err)
	}
	receiver, phase, err := sender.TransferLease(result.lease)
	if err != nil {
		t.Fatal(err)
	}
	if lease, err := sender.TryAcquire(route, Foreground); !errors.Is(err, ErrStaleOwner) || lease != nil {
		t.Fatalf("stale owner immediate admission: lease=%v err=%v", lease, err)
	}
	if err := result.lease.Release(); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("stale immediate lease release: %v", err)
	}
	if err := receiver.Cancel(); err != nil {
		t.Fatal(err)
	}
	primaryIOAssertCounts(t, g, 1, 1, 0)
	if err := receiver.Complete(); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("cancellation completed actual immediate phase: %v", err)
	}
	primaryIORelease(t, phase)
	primaryIORelease(t, phase)
	primaryIOAssertCounts(t, g, 0, 0, 0)
	if owners.Stats().RegisteredOwners != 1 {
		t.Fatal("immediate phase release discarded retained owner")
	}
	primaryIOComplete(t, receiver)
	if lease, err := receiver.TryAcquire(route, Foreground); !errors.Is(err, ErrClosed) || lease != nil {
		t.Fatalf("completed owner immediate admission: lease=%v err=%v", lease, err)
	}
}

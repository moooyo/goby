package primaryio

import (
	"context"
	"errors"
	"testing"
)

func primaryIOAdmissionRoute(roots, domains []string) Route {
	route := Route{Domains: append([]string(nil), domains...)}
	for _, root := range roots {
		route.Roots = append(route.Roots, RootKey{Catalog: "catalog", RootID: root})
	}
	return route
}

func TestPrimaryIOMultiRootAcquireIsAtomicAndAllowsHealthyBypass(t *testing.T) {
	limits := primaryIOLimits()
	limits.RootOwners, limits.RootBackgroundOwners = 1, 0
	g, runtime := primaryIOFixture(t, limits)
	holder := primaryIOOwner(t, runtime, context.Background())
	holderLease := primaryIOAcquire(t, holder, primaryIORoute("b", "db"), Foreground)
	compound := primaryIOOwner(t, runtime, context.Background())
	compoundResult := primaryIOQueuedAcquire(t, compound, primaryIOAdmissionRoute([]string{"a", "b"}, []string{"da", "db"}), Foreground)
	primaryIOAssertCounts(t, g, 1, 0, 1)
	g.mu.Lock()
	aCount, daCount := g.roots[RootKey{Catalog: "catalog", RootID: "a"}], g.domains["da"]
	g.mu.Unlock()
	if aCount.active != 0 || daCount.active != 0 {
		t.Fatalf("queued compound request charged its free dimensions: root=%+v domain=%+v", aCount, daCount)
	}
	primaryIOAssertPending(t, compoundResult)

	healthy := primaryIOOwner(t, runtime, context.Background())
	healthyLease := primaryIOAcquire(t, healthy, primaryIORoute("c", "dc"), Foreground)
	primaryIOAssertCounts(t, g, 2, 0, 1)
	primaryIOAssertPending(t, compoundResult)
	primaryIORelease(t, holderLease)
	result := primaryIOAwaitAcquire(t, compoundResult)
	if result.err != nil || result.lease == nil {
		t.Fatalf("compound acquire after actual retirement: lease=%v err=%v", result.lease, result.err)
	}
	primaryIOAssertCounts(t, g, 2, 0, 0)
	stats := g.Stats()
	if stats.ActiveRoots != 3 || stats.ActiveDomains != 3 {
		t.Fatalf("atomic compound charge and healthy owner: %+v", stats)
	}
	primaryIORelease(t, result.lease)
	primaryIORelease(t, healthyLease)
	primaryIOComplete(t, holder)
	primaryIOComplete(t, compound)
	primaryIOComplete(t, healthy)
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

func TestPrimaryIOCompoundFencePreventsAlternatingRefills(t *testing.T) {
	cases := []struct {
		name  string
		route Route
	}{
		{"roots", primaryIOAdmissionRoute([]string{"a", "b"}, []string{"joined"})},
		{"domains", primaryIOAdmissionRoute([]string{"compound"}, []string{"da", "db"})},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			limits := primaryIOLimits()
			limits.RootOwners, limits.RootBackgroundOwners = 1, 0
			limits.DomainOwners, limits.DomainBackgroundOwners = 1, 0
			g, runtime := primaryIOFixture(t, limits)
			a := primaryIOOwner(t, runtime, context.Background())
			b := primaryIOOwner(t, runtime, context.Background())
			aLease := primaryIOAcquire(t, a, primaryIORoute("a", "da"), Foreground)
			bLease := primaryIOAcquire(t, b, primaryIORoute("b", "db"), Foreground)
			compound := primaryIOOwner(t, runtime, context.Background())
			compoundResult := primaryIOQueuedAcquire(t, compound, test.route, Foreground)

			primaryIORelease(t, aLease)
			refillA := primaryIOOwner(t, runtime, context.Background())
			refillAResult := primaryIOQueuedAcquire(t, refillA, primaryIORoute("a", "da"), Foreground)
			primaryIOAssertCounts(t, g, 1, 0, 2)
			primaryIOAssertPending(t, refillAResult)
			primaryIOAssertPending(t, compoundResult)

			primaryIORelease(t, bLease)
			result := primaryIOAwaitAcquire(t, compoundResult)
			if result.err != nil || result.lease == nil {
				t.Fatalf("oldest compound acquire: lease=%v err=%v", result.lease, result.err)
			}
			refillB := primaryIOOwner(t, runtime, context.Background())
			refillBResult := primaryIOQueuedAcquire(t, refillB, primaryIORoute("b", "db"), Foreground)
			primaryIOAssertCounts(t, g, 1, 0, 2)
			primaryIOAssertPending(t, refillAResult)
			primaryIOAssertPending(t, refillBResult)

			primaryIORelease(t, result.lease)
			resultA := primaryIOAwaitAcquire(t, refillAResult)
			resultB := primaryIOAwaitAcquire(t, refillBResult)
			if resultA.err != nil || resultA.lease == nil || resultB.err != nil || resultB.lease == nil {
				t.Fatalf("refills after compound retirement: a=%+v b=%+v", resultA, resultB)
			}
			primaryIOAssertCounts(t, g, 2, 0, 0)
			primaryIORelease(t, resultA.lease)
			primaryIORelease(t, resultB.lease)
			for _, owner := range []*Owner{a, b, compound, refillA, refillB} {
				primaryIOComplete(t, owner)
			}
			primaryIOAssertCounts(t, g, 0, 0, 0)
		})
	}
}

func TestPrimaryIOBackgroundCompoundFencesRefillAndSurplusForeground(t *testing.T) {
	g, runtime := primaryIOFixture(t, primaryIOLimits())
	b1 := primaryIOOwner(t, runtime, context.Background())
	b2 := primaryIOOwner(t, runtime, context.Background())
	b1Lease := primaryIOAcquire(t, b1, primaryIORoute("b", "db"), Foreground)
	b2Lease := primaryIOAcquire(t, b2, primaryIORoute("b", "db"), Foreground)
	compound := primaryIOOwner(t, runtime, context.Background())
	compoundResult := primaryIOQueuedAcquire(t, compound, primaryIOAdmissionRoute([]string{"a", "b"}, []string{"da", "db"}), Background)

	reserved := primaryIOOwner(t, runtime, context.Background())
	reservedLease := primaryIOAcquire(t, reserved, primaryIORoute("a", "da"), Foreground)
	refill := primaryIOOwner(t, runtime, context.Background())
	refillResult := primaryIOQueuedAcquire(t, refill, primaryIORoute("a", "da"), Background)
	surplus := primaryIOOwner(t, runtime, context.Background())
	surplusResult := primaryIOQueuedAcquire(t, surplus, primaryIORoute("a", "da"), Foreground)
	primaryIOAssertCounts(t, g, 3, 0, 3)
	primaryIOAssertPending(t, compoundResult)
	primaryIOAssertPending(t, refillResult)
	primaryIOAssertPending(t, surplusResult)

	primaryIORelease(t, b1Lease)
	result := primaryIOAwaitAcquire(t, compoundResult)
	if result.err != nil || result.lease == nil {
		t.Fatalf("background compound after capacity drains: %+v", result)
	}
	primaryIOAssertCounts(t, g, 3, 1, 2)
	primaryIOAssertPending(t, refillResult)
	primaryIOAssertPending(t, surplusResult)
	primaryIORelease(t, result.lease)
	refillAcquired := primaryIOAwaitAcquire(t, refillResult)
	if refillAcquired.err != nil || refillAcquired.lease == nil {
		t.Fatalf("background refill after older phase retires: %+v", refillAcquired)
	}
	primaryIOAssertCounts(t, g, 3, 1, 1)
	primaryIOAssertPending(t, surplusResult)
	primaryIORelease(t, refillAcquired.lease)
	surplusAcquired := primaryIOAwaitAcquire(t, surplusResult)
	if surplusAcquired.err != nil || surplusAcquired.lease == nil {
		t.Fatalf("surplus foreground after older claims retire: %+v", surplusAcquired)
	}
	primaryIOAssertCounts(t, g, 3, 0, 0)
	primaryIORelease(t, surplusAcquired.lease)
	primaryIORelease(t, reservedLease)
	primaryIORelease(t, b2Lease)
	for _, owner := range []*Owner{b1, b2, compound, reserved, refill, surplus} {
		primaryIOComplete(t, owner)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

func TestPrimaryIOBackgroundCompoundPreservesLocalForegroundReservations(t *testing.T) {
	cases := []struct {
		name    string
		holders []Route
	}{
		{"root", []Route{primaryIORoute("b", "xb1"), primaryIORoute("b", "xb2"), primaryIORoute("c", "da")}},
		{"domain", []Route{primaryIORoute("b", "xb1"), primaryIORoute("b", "xb2"), primaryIORoute("a", "xa")}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			limits := primaryIOLimits()
			limits.Owners, limits.BackgroundOwners = 6, 5
			g, runtime := primaryIOFixture(t, limits)
			var holders []*Owner
			var leases []*PrimaryReadLease
			for _, route := range test.holders {
				owner := primaryIOOwner(t, runtime, context.Background())
				holders = append(holders, owner)
				leases = append(leases, primaryIOAcquire(t, owner, route, Foreground))
			}
			compound := primaryIOOwner(t, runtime, context.Background())
			compoundResult := primaryIOQueuedAcquire(t, compound, primaryIOAdmissionRoute([]string{"a", "b"}, []string{"da", "db"}), Background)
			reserved := primaryIOOwner(t, runtime, context.Background())
			reservedLease := primaryIOAcquire(t, reserved, primaryIORoute("a", "da"), Foreground)
			primaryIOAssertCounts(t, g, 4, 0, 1)
			primaryIOAssertPending(t, compoundResult)
			if err := compound.Cancel(); err != nil {
				t.Fatalf("cancel compound: %v", err)
			}
			result := primaryIOAwaitAcquire(t, compoundResult)
			if !errors.Is(result.err, context.Canceled) || result.lease != nil {
				t.Fatalf("canceled compound: %+v", result)
			}
			primaryIORelease(t, reservedLease)
			for _, lease := range leases {
				primaryIORelease(t, lease)
			}
			primaryIOComplete(t, reserved)
			primaryIOComplete(t, compound)
			for _, owner := range holders {
				primaryIOComplete(t, owner)
			}
			primaryIOAssertCounts(t, g, 0, 0, 0)
		})
	}
}

func TestPrimaryIOLargeGlobalForegroundReserveDoesNotBypassBackgroundCompoundFence(t *testing.T) {
	limits := primaryIOLimits()
	limits.Owners, limits.BackgroundOwners = 8, 1
	g, runtime := primaryIOFixture(t, limits)
	a1 := primaryIOOwner(t, runtime, context.Background())
	a2 := primaryIOOwner(t, runtime, context.Background())
	b1 := primaryIOOwner(t, runtime, context.Background())
	b2 := primaryIOOwner(t, runtime, context.Background())
	a1Lease := primaryIOAcquire(t, a1, primaryIORoute("a", "da"), Foreground)
	a2Lease := primaryIOAcquire(t, a2, primaryIORoute("a", "da"), Foreground)
	b1Lease := primaryIOAcquire(t, b1, primaryIORoute("b", "db"), Foreground)
	b2Lease := primaryIOAcquire(t, b2, primaryIORoute("b", "db"), Foreground)
	compound := primaryIOOwner(t, runtime, context.Background())
	compoundResult := primaryIOQueuedAcquire(t, compound, primaryIOAdmissionRoute([]string{"a", "b"}, []string{"da", "db"}), Background)

	primaryIORelease(t, a1Lease)
	refill := primaryIOOwner(t, runtime, context.Background())
	refillResult := primaryIOQueuedAcquire(t, refill, primaryIORoute("a", "da"), Foreground)
	// The global foreground reserve is seven, while a/da already has its
	// local reservation occupied. Refilling that surplus must not restart
	// the two-root drain merely because unrelated global slots are unused.
	primaryIOAssertCounts(t, g, 3, 0, 2)
	primaryIOAssertPending(t, compoundResult)
	primaryIOAssertPending(t, refillResult)
	primaryIORelease(t, b1Lease)
	result := primaryIOAwaitAcquire(t, compoundResult)
	if result.err != nil || result.lease == nil {
		t.Fatalf("compound after both roots actually retire capacity: %+v", result)
	}
	primaryIOAssertCounts(t, g, 3, 1, 1)
	primaryIOAssertPending(t, refillResult)
	primaryIORelease(t, result.lease)
	refillAcquired := primaryIOAwaitAcquire(t, refillResult)
	if refillAcquired.err != nil || refillAcquired.lease == nil {
		t.Fatalf("foreground refill after compound retirement: %+v", refillAcquired)
	}
	primaryIOAssertCounts(t, g, 3, 0, 0)
	primaryIORelease(t, refillAcquired.lease)
	primaryIORelease(t, a2Lease)
	primaryIORelease(t, b2Lease)
	for _, owner := range []*Owner{a1, a2, b1, b2, compound, refill} {
		primaryIOComplete(t, owner)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

func TestPrimaryIOBackgroundCompoundWithUnavailableClassAllowsSurplusForeground(t *testing.T) {
	g, runtime := primaryIOFixture(t, primaryIOLimits())
	b := primaryIOOwner(t, runtime, context.Background())
	bLease := primaryIOAcquire(t, b, primaryIORoute("b", "db"), Background)
	compound := primaryIOOwner(t, runtime, context.Background())
	compoundResult := primaryIOQueuedAcquire(t, compound, primaryIOAdmissionRoute([]string{"a", "b"}, []string{"da", "db"}), Background)
	a1 := primaryIOOwner(t, runtime, context.Background())
	a2 := primaryIOOwner(t, runtime, context.Background())
	a1Lease := primaryIOAcquire(t, a1, primaryIORoute("a", "da"), Foreground)
	a2Lease := primaryIOAcquire(t, a2, primaryIORoute("a", "da"), Foreground)
	primaryIOAssertCounts(t, g, 3, 1, 1)
	primaryIOAssertPending(t, compoundResult)

	refill := primaryIOOwner(t, runtime, context.Background())
	refillResult := primaryIOQueuedAcquire(t, refill, primaryIORoute("a", "da"), Background)
	primaryIORelease(t, a1Lease)
	primaryIOAssertCounts(t, g, 2, 1, 2)
	primaryIOAssertPending(t, refillResult)
	primaryIORelease(t, bLease)
	result := primaryIOAwaitAcquire(t, compoundResult)
	if result.err != nil || result.lease == nil {
		t.Fatalf("background compound after class allowance returns: %+v", result)
	}
	primaryIOAssertCounts(t, g, 2, 1, 1)
	primaryIOAssertPending(t, refillResult)
	primaryIORelease(t, result.lease)
	refillAcquired := primaryIOAwaitAcquire(t, refillResult)
	if refillAcquired.err != nil || refillAcquired.lease == nil {
		t.Fatalf("background refill after compound retirement: %+v", refillAcquired)
	}
	primaryIORelease(t, refillAcquired.lease)
	primaryIORelease(t, a2Lease)
	for _, owner := range []*Owner{b, compound, a1, a2, refill} {
		primaryIOComplete(t, owner)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

func TestPrimaryIOCanceledCompoundFenceUnblocksQueuedFollower(t *testing.T) {
	limits := primaryIOLimits()
	limits.RootOwners, limits.RootBackgroundOwners = 1, 0
	g, runtime := primaryIOFixture(t, limits)
	b := primaryIOOwner(t, runtime, context.Background())
	bLease := primaryIOAcquire(t, b, primaryIORoute("b", "db"), Foreground)
	compound := primaryIOOwner(t, runtime, context.Background())
	compoundResult := primaryIOQueuedAcquire(t, compound, primaryIOAdmissionRoute([]string{"a", "b"}, []string{"da", "db"}), Foreground)
	follower := primaryIOOwner(t, runtime, context.Background())
	followerResult := primaryIOQueuedAcquire(t, follower, primaryIORoute("a", "da"), Foreground)
	primaryIOAssertCounts(t, g, 1, 0, 2)
	primaryIOAssertPending(t, followerResult)
	if err := compound.Cancel(); err != nil {
		t.Fatalf("cancel compound: %v", err)
	}
	canceled := primaryIOAwaitAcquire(t, compoundResult)
	if !errors.Is(canceled.err, context.Canceled) || canceled.lease != nil {
		t.Fatalf("canceled compound result: %+v", canceled)
	}
	acquired := primaryIOAwaitAcquire(t, followerResult)
	if acquired.err != nil || acquired.lease == nil {
		t.Fatalf("follower after barrier cancellation: %+v", acquired)
	}
	primaryIOAssertCounts(t, g, 2, 0, 0)
	primaryIORelease(t, acquired.lease)
	primaryIORelease(t, bLease)
	primaryIOComplete(t, compound)
	primaryIOComplete(t, follower)
	primaryIOComplete(t, b)
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

func TestPrimaryIOQueuedRouteIsClonedAndDeduplicated(t *testing.T) {
	limits := primaryIOLimits()
	limits.RootOwners, limits.RootBackgroundOwners = 1, 0
	limits.DomainOwners, limits.DomainBackgroundOwners = 1, 0
	g, runtime := primaryIOFixture(t, limits)
	b := primaryIOOwner(t, runtime, context.Background())
	bLease := primaryIOAcquire(t, b, primaryIORoute("b", "db"), Foreground)
	route := primaryIOAdmissionRoute([]string{"a", "a", "b"}, []string{"da", "da", "db"})
	compound := primaryIOOwner(t, runtime, context.Background())
	compoundResult := primaryIOQueuedAcquire(t, compound, route, Foreground)
	for index := range route.Roots {
		route.Roots[index] = RootKey{Catalog: "mutated", RootID: "mutated"}
	}
	for index := range route.Domains {
		route.Domains[index] = "mutated"
	}
	primaryIORelease(t, bLease)
	result := primaryIOAwaitAcquire(t, compoundResult)
	if result.err != nil || result.lease == nil {
		t.Fatalf("compound using copied route: %+v", result)
	}
	g.mu.Lock()
	aCount := g.roots[RootKey{Catalog: "catalog", RootID: "a"}]
	bCount := g.roots[RootKey{Catalog: "catalog", RootID: "b"}]
	daCount, dbCount := g.domains["da"], g.domains["db"]
	_, mutatedRoot := g.roots[RootKey{Catalog: "mutated", RootID: "mutated"}]
	_, mutatedDomain := g.domains["mutated"]
	g.mu.Unlock()
	if aCount.active != 1 || bCount.active != 1 || daCount.active != 1 || dbCount.active != 1 || mutatedRoot || mutatedDomain {
		t.Fatalf("copied/deduplicated charges: a=%+v b=%+v da=%+v db=%+v mutatedRoot=%v mutatedDomain=%v", aCount, bCount, daCount, dbCount, mutatedRoot, mutatedDomain)
	}
	stats := g.Stats()
	if stats.ActiveRoots != 2 || stats.ActiveDomains != 2 {
		t.Fatalf("deduplicated route dimensions: %+v", stats)
	}
	primaryIORelease(t, result.lease)
	primaryIOComplete(t, b)
	primaryIOComplete(t, compound)
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

func TestPrimaryIOAdmissionQueueLimitsRejectWithoutCharging(t *testing.T) {
	cases := []struct {
		name     string
		limits   Limits
		holder   Route
		waiting  Route
		rejected Route
	}{
		{
			name:   "global",
			limits: Limits{Owners: 1, RootOwners: 1, DomainOwners: 1, Queued: 1, RootQueued: 1, DomainQueued: 1},
			holder: primaryIORoute("a", "da"), waiting: primaryIORoute("a", "da"), rejected: primaryIORoute("b", "db"),
		},
		{
			name:   "root",
			limits: Limits{Owners: 4, BackgroundOwners: 3, RootOwners: 1, DomainOwners: 2, DomainBackgroundOwners: 1, Queued: 4, RootQueued: 1, DomainQueued: 4},
			holder: primaryIORoute("a", "da"), waiting: primaryIORoute("a", "db"), rejected: primaryIORoute("a", "dc"),
		},
		{
			name:   "domain",
			limits: Limits{Owners: 4, BackgroundOwners: 3, RootOwners: 2, RootBackgroundOwners: 1, DomainOwners: 1, Queued: 4, RootQueued: 4, DomainQueued: 1},
			holder: primaryIORoute("a", "shared"), waiting: primaryIORoute("b", "shared"), rejected: primaryIORoute("c", "shared"),
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			g, runtime := primaryIOFixture(t, test.limits)
			holder := primaryIOOwner(t, runtime, context.Background())
			holderLease := primaryIOAcquire(t, holder, test.holder, Foreground)
			waiting := primaryIOOwner(t, runtime, context.Background())
			waitingResult := primaryIOQueuedAcquire(t, waiting, test.waiting, Foreground)
			rejected := primaryIOOwner(t, runtime, context.Background())
			lease, err := rejected.Acquire(test.rejected, Foreground)
			if !errors.Is(err, ErrBusy) || lease != nil {
				t.Fatalf("queue overflow: lease=%v err=%v", lease, err)
			}
			primaryIOAssertCounts(t, g, 1, 0, 1)
			primaryIOComplete(t, rejected)
			if err := waiting.Cancel(); err != nil {
				t.Fatalf("cancel queued owner: %v", err)
			}
			result := primaryIOAwaitAcquire(t, waitingResult)
			if !errors.Is(result.err, context.Canceled) || result.lease != nil {
				t.Fatalf("canceled queue entry: %+v", result)
			}
			primaryIOAssertCounts(t, g, 1, 0, 0)
			primaryIORelease(t, holderLease)
			primaryIOComplete(t, holder)
			primaryIOComplete(t, waiting)
			primaryIOAssertCounts(t, g, 0, 0, 0)
		})
	}
}

func TestPrimaryIOGovernorCloseCancelsQueueButRetainsActiveLease(t *testing.T) {
	limits := Limits{Owners: 1, RootOwners: 1, DomainOwners: 1, Queued: 2, RootQueued: 2, DomainQueued: 2}
	g, runtime := primaryIOFixture(t, limits)
	active := primaryIOOwner(t, runtime, context.Background())
	activeLease := primaryIOAcquire(t, active, primaryIORoute("a", "da"), Foreground)
	queued := primaryIOOwner(t, runtime, context.Background())
	queuedResult := primaryIOQueuedAcquire(t, queued, primaryIORoute("b", "db"), Foreground)
	g.Close()
	result := primaryIOAwaitAcquire(t, queuedResult)
	if !errors.Is(result.err, ErrClosed) || result.lease != nil {
		t.Fatalf("queue at governor close: %+v", result)
	}
	primaryIOAssertCounts(t, g, 1, 0, 0)
	if !g.Stats().Closed {
		t.Fatal("governor did not retain its admission fence")
	}
	if err := active.Complete(); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("active owner complete before retirement: %v", err)
	}
	later := primaryIOOwner(t, runtime, context.Background())
	lease, err := later.Acquire(primaryIORoute("c", "dc"), Foreground)
	if !errors.Is(err, ErrClosed) || lease != nil {
		t.Fatalf("acquire after governor close: lease=%v err=%v", lease, err)
	}
	primaryIORelease(t, activeLease)
	primaryIOComplete(t, active)
	primaryIOComplete(t, queued)
	primaryIOComplete(t, later)
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

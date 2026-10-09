package primaryio

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func primaryIOAssertQueueDimensionsEmpty(t *testing.T, g *Governor) {
	t.Helper()
	g.mu.Lock()
	roots, domains := len(g.queuedRoots), len(g.queuedDomains)
	g.mu.Unlock()
	if roots != 0 || domains != 0 {
		t.Fatalf("retired queue retained dimension keys: roots=%d domains=%d", roots, domains)
	}
}

func primaryIOAssertAcquireBusy(t *testing.T, owner *Owner, route Route) {
	t.Helper()
	result := make(chan primaryIOAcquireResult, 1)
	go func() {
		lease, err := owner.Acquire(route, Foreground)
		result <- primaryIOAcquireResult{lease, err}
	}()
	value := primaryIOAwaitAcquire(t, result)
	if !errors.Is(value.err, ErrBusy) || value.lease != nil {
		t.Fatalf("full queue admitted a request: %+v", value)
	}
}

func TestPrimaryIOQueueCapacityReturnsAfterGrantAndCancellation(t *testing.T) {
	for _, cancelQueued := range []bool{false, true} {
		name := "grant"
		if cancelQueued {
			name = "cancel"
		}
		t.Run(name, func(t *testing.T) {
			limits := Limits{Owners: 1, RootOwners: 1, DomainOwners: 1, Queued: 4, RootQueued: 1, DomainQueued: 1}
			g, owners := primaryIOFixture(t, limits)
			route := primaryIORoute("source", "disk")
			holder := primaryIOOwner(t, owners, context.Background())
			holderLease := primaryIOAcquire(t, holder, route, Foreground)
			waiting := primaryIOOwner(t, owners, context.Background())
			waitingResult := primaryIOQueuedAcquire(t, waiting, route, Foreground)
			next := primaryIOOwner(t, owners, context.Background())
			primaryIOAssertAcquireBusy(t, next, route)
			if cancelQueued {
				if err := waiting.Cancel(); err != nil {
					t.Fatal(err)
				}
				value := primaryIOAwaitAcquire(t, waitingResult)
				if !errors.Is(value.err, context.Canceled) || value.lease != nil {
					t.Fatalf("canceled queue request: %+v", value)
				}
			} else {
				primaryIORelease(t, holderLease)
				value := primaryIOAwaitAcquire(t, waitingResult)
				if value.err != nil || value.lease == nil {
					t.Fatalf("granted queue request: %+v", value)
				}
				holderLease = value.lease
			}
			nextResult := primaryIOQueuedAcquire(t, next, route, Foreground)
			primaryIOAssertCounts(t, g, 1, 0, 1)
			primaryIORelease(t, holderLease)
			value := primaryIOAwaitAcquire(t, nextResult)
			if value.err != nil || value.lease == nil {
				t.Fatalf("replacement queue request: %+v", value)
			}
			primaryIORelease(t, value.lease)
			for _, owner := range []*Owner{holder, waiting, next} {
				primaryIOComplete(t, owner)
			}
			primaryIOAssertCounts(t, g, 0, 0, 0)
			primaryIOAssertQueueDimensionsEmpty(t, g)
		})
	}
}

func TestPrimaryIOQueueLimitSurvivesIdleActiveDimension(t *testing.T) {
	limits := Limits{Owners: 4, BackgroundOwners: 3, RootOwners: 1, DomainOwners: 1, Queued: 4, RootQueued: 1, DomainQueued: 1}
	g, owners := primaryIOFixture(t, limits)
	a := primaryIOOwner(t, owners, context.Background())
	b := primaryIOOwner(t, owners, context.Background())
	aLease := primaryIOAcquire(t, a, primaryIORoute("a", "da"), Foreground)
	bLease := primaryIOAcquire(t, b, primaryIORoute("b", "db"), Foreground)
	compound := primaryIOOwner(t, owners, context.Background())
	compoundResult := primaryIOQueuedAcquire(t, compound, primaryIOAdmissionRoute([]string{"a", "b"}, []string{"da", "db"}), Foreground)
	primaryIORelease(t, aLease)
	stats := g.Stats()
	if stats.ActiveRoots != 1 || stats.ActiveDomains != 1 || stats.Queued != 1 {
		t.Fatalf("queued dimensions changed active statistics: %+v", stats)
	}
	// The compound request still consumes queue capacity on a/da while only
	// b/db has an active lease. Retiring a/da must not erase that queue charge.
	next := primaryIOOwner(t, owners, context.Background())
	primaryIOAssertAcquireBusy(t, next, primaryIORoute("a", "da"))
	if err := compound.Cancel(); err != nil {
		t.Fatal(err)
	}
	value := primaryIOAwaitAcquire(t, compoundResult)
	if !errors.Is(value.err, context.Canceled) || value.lease != nil {
		t.Fatalf("canceled compound request: %+v", value)
	}
	nextLease := primaryIOAcquire(t, next, primaryIORoute("a", "da"), Foreground)
	primaryIORelease(t, nextLease)
	primaryIORelease(t, bLease)
	for _, owner := range []*Owner{a, b, compound, next} {
		primaryIOComplete(t, owner)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
	primaryIOAssertQueueDimensionsEmpty(t, g)
}

func TestPrimaryIOQueuedDuplicatesConsumeOneSlotPerDimension(t *testing.T) {
	limits := Limits{Owners: 1, RootOwners: 1, DomainOwners: 1, Queued: 4, RootQueued: 2, DomainQueued: 2}
	g, owners := primaryIOFixture(t, limits)
	holder := primaryIOOwner(t, owners, context.Background())
	holderLease := primaryIOAcquire(t, holder, primaryIORoute("source", "disk"), Foreground)
	route := primaryIOAdmissionRoute([]string{"source", "source"}, []string{"disk", "disk"})
	first := primaryIOOwner(t, owners, context.Background())
	firstResult := primaryIOQueuedAcquire(t, first, route, Foreground)
	second := primaryIOOwner(t, owners, context.Background())
	secondResult := primaryIOQueuedAcquire(t, second, route, Foreground)
	rejected := primaryIOOwner(t, owners, context.Background())
	primaryIOAssertAcquireBusy(t, rejected, route)
	primaryIOAssertCounts(t, g, 1, 0, 2)
	primaryIORelease(t, holderLease)
	for _, result := range []<-chan primaryIOAcquireResult{firstResult, secondResult} {
		value := primaryIOAwaitAcquire(t, result)
		if value.err != nil || value.lease == nil {
			t.Fatalf("deduplicated queue request: %+v", value)
		}
		primaryIORelease(t, value.lease)
	}
	for _, owner := range []*Owner{holder, first, second, rejected} {
		primaryIOComplete(t, owner)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
	primaryIOAssertQueueDimensionsEmpty(t, g)
}

func TestPrimaryIOQueueCancellationDuringDispatchReturnsCapacity(t *testing.T) {
	limits := Limits{Owners: 1, RootOwners: 1, DomainOwners: 1, Queued: 4, RootQueued: 1, DomainQueued: 1}
	g, owners := primaryIOFixture(t, limits)
	route := primaryIORoute("source", "disk")
	prepared, err := prepareRoute(route)
	if err != nil {
		t.Fatal(err)
	}
	holder := primaryIOOwner(t, owners, context.Background())
	holderLease := primaryIOAcquire(t, holder, route, Foreground)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := &primaryIOQueuedGrantContext{Context: ctx, entered: make(chan struct{}), resume: make(chan struct{})}
	var resumed sync.Once
	resume := func() { resumed.Do(func() { close(observed.resume) }) }
	defer resume()
	result := make(chan primaryIOAdmissionResult, 1)
	go func() {
		request, err := g.acquirePrepared(observed, prepared, Foreground)
		result <- primaryIOAdmissionResult{request, err}
	}()
	primaryIOAwaitClosed(t, observed.entered)
	cancel()
	// The canceled acquisition is paused before its select, so dispatch must
	// retire it before the acquisition can run its own cancellation release.
	g.mu.Lock()
	g.dispatchLocked()
	g.mu.Unlock()
	primaryIOAssertCounts(t, g, 1, 0, 0)
	primaryIOAssertQueueDimensionsEmpty(t, g)
	next := primaryIOOwner(t, owners, context.Background())
	nextResult := primaryIOQueuedAcquire(t, next, route, Foreground)
	resume()
	value := primaryIOAwaitValue(t, result)
	if !errors.Is(value.err, context.Canceled) || value.request != nil {
		t.Fatalf("dispatch-canceled request: %+v", value)
	}
	// The resumed canceled request must not release the replacement's slot.
	rejected := primaryIOOwner(t, owners, context.Background())
	primaryIOAssertAcquireBusy(t, rejected, route)
	primaryIORelease(t, holderLease)
	nextValue := primaryIOAwaitAcquire(t, nextResult)
	if nextValue.err != nil || nextValue.lease == nil {
		t.Fatalf("replacement after dispatch cancellation: %+v", nextValue)
	}
	primaryIORelease(t, nextValue.lease)
	for _, owner := range []*Owner{holder, next, rejected} {
		primaryIOComplete(t, owner)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
	primaryIOAssertQueueDimensionsEmpty(t, g)
}

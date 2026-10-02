package primaryio

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func primaryIOAwaitQueuedPhase(t *testing.T, g *Governor, result <-chan primaryIOAcquireResult) {
	t.Helper()
	// The merged context queries its parent during construction, so the old
	// Done observer cannot mark enqueue. Observe the real synchronized queue
	// condition instead; scheduling yields do not decide the acquisition order.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for g.Stats().Queued != 1 {
		select {
		case value := <-result:
			t.Fatalf("phase admission ended before it queued: %+v", value)
		case <-deadline.C:
			t.Fatal("phase admission did not reach its queue boundary")
		default:
			runtime.Gosched()
		}
	}
}

func TestPrimaryIOPhaseContextCancellationPreservesRetainedOwner(t *testing.T) {
	g, owners := primaryIOFixture(t, primaryIOLimits())
	route := primaryIORoute("source", "disk")
	holder := primaryIOOwner(t, owners, context.Background())
	holderLease := primaryIOAcquire(t, holder, route, Background)
	phase, cancelPhase := context.WithCancel(context.Background())
	t.Cleanup(cancelPhase)
	owner := primaryIOOwner(t, owners, context.WithoutCancel(phase))
	for _, acquire := range []func(context.Context, Route, Class) (*PrimaryReadLease, error){owner.AcquireContext, owner.TryAcquireContext} {
		if lease, err := acquire(nil, route, Background); !errors.Is(err, ErrInvalid) || lease != nil {
			t.Fatalf("nil phase context admission: lease=%v err=%v", lease, err)
		}
	}
	result := make(chan primaryIOAcquireResult, 1)
	go func() {
		lease, err := owner.AcquireContext(phase, route, Background)
		result <- primaryIOAcquireResult{lease, err}
	}()
	primaryIOAwaitQueuedPhase(t, g, result)
	primaryIOAssertCounts(t, g, 1, 1, 1)
	if owners.Stats().RegisteredOwners != 2 {
		t.Fatal("phase admission created another retained registration")
	}
	cancelPhase()
	value := primaryIOAwaitAcquire(t, result)
	if !errors.Is(value.err, context.Canceled) || value.lease != nil {
		t.Fatalf("canceled queued phase: %+v", value)
	}
	primaryIOAssertCounts(t, g, 1, 1, 0)
	if owner.Context().Err() != nil || owners.Stats().RegisteredOwners != 2 {
		t.Fatal("phase cancellation poisoned or completed its retained owner")
	}
	// Reusing the canceled phase must fail before enqueue, while its owner
	// remains available for a different consumer or a later cleanup phase.
	for _, acquire := range []func(context.Context, Route, Class) (*PrimaryReadLease, error){owner.AcquireContext, owner.TryAcquireContext} {
		attempt := make(chan primaryIOAcquireResult, 1)
		go func() {
			lease, err := acquire(phase, route, Background)
			attempt <- primaryIOAcquireResult{lease, err}
		}()
		rejected := primaryIOAwaitAcquire(t, attempt)
		if !errors.Is(rejected.err, context.Canceled) || rejected.lease != nil {
			t.Fatalf("already canceled phase admission: %+v", rejected)
		}
		primaryIOAssertCounts(t, g, 1, 1, 0)
	}
	primaryIORelease(t, holderLease)
	nextPhase, cancelNext := context.WithCancel(context.Background())
	defer cancelNext()
	lease, err := owner.TryAcquireContext(nextPhase, route, Background)
	if err != nil || lease == nil {
		t.Fatalf("later phase could not reuse the retained owner: lease=%v err=%v", lease, err)
	}
	cancelNext()
	primaryIOAssertCounts(t, g, 1, 1, 0)
	if owner.Context().Err() != nil || owners.Stats().RegisteredOwners != 2 {
		t.Fatal("granted phase cancellation changed retained ownership")
	}
	if err := owner.Complete(); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("phase cancellation fabricated actual retirement: %v", err)
	}
	primaryIORelease(t, lease)
	// A healthy consumer phase must still observe its retained owner's close
	// fence. This exercises the owner-to-phase cancellation merge while queued.
	holderLease = primaryIOAcquire(t, holder, route, Background)
	ownerResult := make(chan primaryIOAcquireResult, 1)
	go func() {
		lease, err := owner.AcquireContext(context.Background(), route, Background)
		ownerResult <- primaryIOAcquireResult{lease, err}
	}()
	primaryIOAwaitQueuedPhase(t, g, ownerResult)
	if err := owner.Cancel(); err != nil {
		t.Fatal(err)
	}
	closed := primaryIOAwaitAcquire(t, ownerResult)
	if !errors.Is(closed.err, context.Canceled) || closed.lease != nil {
		t.Fatalf("owner cancellation did not fence a healthy queued phase: %+v", closed)
	}
	primaryIOAssertCounts(t, g, 1, 1, 0)
	if owners.Stats().RegisteredOwners != 2 {
		t.Fatal("owner cancellation completed retained registration")
	}
	for _, acquire := range []func(context.Context, Route, Class) (*PrimaryReadLease, error){owner.AcquireContext, owner.TryAcquireContext} {
		attempt := make(chan primaryIOAcquireResult, 1)
		go func() {
			lease, err := acquire(context.Background(), route, Background)
			attempt <- primaryIOAcquireResult{lease, err}
		}()
		rejected := primaryIOAwaitAcquire(t, attempt)
		if !errors.Is(rejected.err, context.Canceled) || rejected.lease != nil {
			t.Fatalf("healthy phase bypassed canceled retained owner: %+v", rejected)
		}
		primaryIOAssertCounts(t, g, 1, 1, 0)
	}
	primaryIORelease(t, holderLease)
	primaryIOComplete(t, owner)
	primaryIOComplete(t, holder)
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

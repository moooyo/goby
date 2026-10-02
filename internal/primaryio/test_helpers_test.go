package primaryio

import (
	"context"
	"sync"
	"testing"
	"time"
)

func primaryIOLimits() Limits {
	return Limits{Owners: 4, BackgroundOwners: 3, RootOwners: 2, RootBackgroundOwners: 1,
		DomainOwners: 2, DomainBackgroundOwners: 1, Queued: 16, RootQueued: 8, DomainQueued: 8}
}

func primaryIOFixture(t *testing.T, limits Limits) (*Governor, *OwnerRuntime) {
	t.Helper()
	governor, err := NewGovernor(limits)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewOwnerRuntime(governor, 64)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Failure cleanup cancels admission without inventing actual retirement.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = runtime.Close(ctx)
		governor.Close()
	})
	return governor, runtime
}

func primaryIORoute(root, domain string) Route {
	return Route{Roots: []RootKey{{Catalog: "catalog", RootID: root}}, Domains: []string{domain}}
}

func primaryIOOwner(t *testing.T, runtime *OwnerRuntime, ctx context.Context) *Owner {
	t.Helper()
	owner, err := runtime.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func primaryIOAcquire(t *testing.T, owner *Owner, route Route, class Class) *PrimaryReadLease {
	t.Helper()
	lease, err := owner.Acquire(route, class)
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func primaryIORelease(t *testing.T, lease *PrimaryReadLease) {
	t.Helper()
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
}

func primaryIOComplete(t *testing.T, owner *Owner) {
	t.Helper()
	if err := owner.Complete(); err != nil {
		t.Fatal(err)
	}
}

func primaryIOClose(t *testing.T, owners *OwnerRuntime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := owners.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

type primaryIOAcquireResult struct {
	lease *PrimaryReadLease
	err   error
}

// Done is evaluated by acquire's select only after charge/enqueue and unlock.
// This observer is installed after Register, so context.WithCancel cannot
// signal the queue barrier during parent-context setup. No sleep controls order.
type primaryIOQueueContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *primaryIOQueueContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func primaryIOQueuedAcquire(t *testing.T, owner *Owner, route Route, class Class) <-chan primaryIOAcquireResult {
	t.Helper()
	owner.state.mu.Lock()
	observed := &primaryIOQueueContext{Context: owner.state.ctx, entered: make(chan struct{})}
	owner.state.ctx = observed
	owner.state.mu.Unlock()
	result := make(chan primaryIOAcquireResult, 1)
	go func() {
		lease, err := owner.Acquire(route, class)
		result <- primaryIOAcquireResult{lease, err}
	}()
	select {
	case <-observed.entered:
	case value := <-result:
		t.Fatalf("acquisition failed before the queue barrier: %v", value.err)
	case <-time.After(5 * time.Second):
		t.Fatal("acquisition did not reach its charge/queue boundary")
	}
	return result
}

func primaryIOAwaitAcquire(t *testing.T, result <-chan primaryIOAcquireResult) primaryIOAcquireResult {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("acquisition did not complete")
		return primaryIOAcquireResult{}
	}
}

func primaryIOAwaitValue[T any](t *testing.T, result <-chan T) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent operation did not return its result")
		var zero T
		return zero
	}
}

func primaryIOAssertPending(t *testing.T, result <-chan primaryIOAcquireResult) {
	t.Helper()
	select {
	case value := <-result:
		t.Fatalf("waiting acquisition completed early: %v", value.err)
	default:
	}
}

func primaryIOAssertCounts(t *testing.T, governor *Governor, active, background, queued int) {
	t.Helper()
	stats := governor.Stats()
	if stats.Active != active || stats.Background != background || stats.Queued != queued {
		t.Fatalf("counts = %+v; want active=%d background=%d queued=%d", stats, active, background, queued)
	}
}

func primaryIOAwaitClosed(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("actual owner did not reach its completion barrier")
	}
}

func primaryIOAssertOpen(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
		t.Fatal("completion occurred before actual owner retirement")
	default:
	}
}

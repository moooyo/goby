package primaryio

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type primaryIOAdmissionResult struct {
	request *request
	err     error
}

type primaryIOGrantContext struct {
	context.Context
	calls, waitAt int
	entered       chan struct{}
	resume        chan struct{}
}

func (ctx *primaryIOGrantContext) Err() error {
	ctx.calls++
	if ctx.calls == ctx.waitAt {
		close(ctx.entered)
		<-ctx.resume
	}
	return ctx.Context.Err()
}

func TestPrimaryIOImmediateGrantCancellationReturnsCapacity(t *testing.T) {
	route := primaryIORoute("source", "disk")
	prepared, err := prepareRoute(route)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		waitAt  int
		acquire func(*Governor, context.Context) (*request, error)
	}{
		{"prepared", 2, func(g *Governor, ctx context.Context) (*request, error) {
			return g.acquirePrepared(ctx, prepared, Foreground)
		}},
		{"try", 3, func(g *Governor, ctx context.Context) (*request, error) {
			return g.tryAcquire(ctx, route, Foreground)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			g, _ := primaryIOFixture(t, primaryIOLimits())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			observed := &primaryIOGrantContext{Context: ctx, waitAt: test.waitAt, entered: make(chan struct{}), resume: make(chan struct{})}
			var resumed sync.Once
			resume := func() { resumed.Do(func() { close(observed.resume) }) }
			defer resume()
			result := make(chan primaryIOAdmissionResult, 1)
			go func() {
				request, err := test.acquire(g, observed)
				result <- primaryIOAdmissionResult{request, err}
			}()
			primaryIOAwaitClosed(t, observed.entered)
			// The last cancellation observation occurs after unlock and after
			// charging capacity, before ownership can reach an actual reader.
			stats := make(chan Stats, 1)
			go func() { stats <- g.Stats() }()
			if value := primaryIOAwaitValue(t, stats); value.Active != 1 || value.Background != 0 || value.Queued != 0 {
				t.Fatalf("immediate grant was not charged before cancellation: %+v", value)
			}
			cancel()
			resume()
			value := primaryIOAwaitValue(t, result)
			if !errors.Is(value.err, context.Canceled) || value.request != nil {
				t.Fatalf("cancellation after immediate grant escaped compensation: %+v", value)
			}
			primaryIOAssertCounts(t, g, 0, 0, 0)
		})
	}
}

type primaryIOQueuedGrantContext struct {
	context.Context
	entered chan struct{}
	resume  chan struct{}
}

func (ctx *primaryIOQueuedGrantContext) Done() <-chan struct{} {
	close(ctx.entered)
	<-ctx.resume
	return ctx.Context.Done()
}

func TestPrimaryIOQueuedGrantCancellationBeforeDeliveryReturnsCapacity(t *testing.T) {
	limits := Limits{Owners: 1, RootOwners: 1, DomainOwners: 1, Queued: 1, RootQueued: 1, DomainQueued: 1}
	g, _ := primaryIOFixture(t, limits)
	prepared, err := prepareRoute(primaryIORoute("source", "disk"))
	if err != nil {
		t.Fatal(err)
	}
	holder, err := g.acquirePrepared(context.Background(), prepared, Foreground)
	if err != nil {
		t.Fatal(err)
	}
	defer g.release(holder)
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
	primaryIOAssertCounts(t, g, 1, 0, 1)
	g.release(holder)
	primaryIOAssertCounts(t, g, 1, 0, 0)
	primaryIOAssertQueueDimensionsEmpty(t, g)
	// Both select cases are ready before the granted request resumes. Either
	// selection must return its undelivered charge exactly once.
	cancel()
	resume()
	value := primaryIOAwaitValue(t, result)
	if !errors.Is(value.err, context.Canceled) || value.request != nil {
		t.Fatalf("cancellation before queued grant delivery escaped compensation: %+v", value)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
	primaryIOAssertQueueDimensionsEmpty(t, g)
}

func BenchmarkPrimaryIOUncontendedAdmission(b *testing.B) {
	route := primaryIORoute("source", "disk")
	prepared, err := prepareRoute(route)
	if err != nil {
		b.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		acquire func(*Governor) (*request, error)
	}{
		{"prepared", func(g *Governor) (*request, error) {
			return g.acquirePrepared(context.Background(), prepared, Foreground)
		}},
		{"try", func(g *Governor) (*request, error) {
			return g.tryAcquire(context.Background(), route, Foreground)
		}},
	} {
		b.Run(test.name, func(b *testing.B) {
			g, err := NewGovernor(primaryIOLimits())
			if err != nil {
				b.Fatal(err)
			}
			defer g.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				request, err := test.acquire(g)
				if err != nil {
					b.Fatal(err)
				}
				g.release(request)
			}
		})
	}
}

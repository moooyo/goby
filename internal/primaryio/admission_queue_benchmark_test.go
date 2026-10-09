package primaryio

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// The benchmark limits match the original-media runtime. Background holders
// leave foreground capacity available while keeping the mixed queue blocked.
func primaryIOBenchmarkBlockedQueue(b *testing.B, queued, groups int) *Governor {
	b.Helper()
	g, err := NewGovernor(Limits{
		Owners: 4, BackgroundOwners: 2,
		RootOwners: 3, RootBackgroundOwners: 2,
		DomainOwners: 3, DomainBackgroundOwners: 2,
		Queued: 128, RootQueued: 32, DomainQueued: 32,
	})
	if err != nil {
		b.Fatal(err)
	}
	var holders []*request
	var results []<-chan primaryIOAdmissionResult
	b.Cleanup(func() {
		g.Close()
		for _, result := range results {
			select {
			case value := <-result:
				if !errors.Is(value.err, ErrClosed) || value.request != nil {
					b.Errorf("queued request at close: %+v", value)
				}
			case <-time.After(5 * time.Second):
				b.Error("queued request did not retire at close")
			}
		}
		for _, holder := range holders {
			g.release(holder)
		}
	})
	for index := 0; index < 2; index++ {
		route := primaryIORoute(fmt.Sprintf("holder-%d", index), fmt.Sprintf("holder-domain-%d", index))
		holder, err := g.acquire(context.Background(), route, Background)
		if err != nil {
			b.Fatal(err)
		}
		holders = append(holders, holder)
	}
	for index := 0; index < queued; index++ {
		group := index % groups
		route := primaryIOAdmissionRoute(
			[]string{fmt.Sprintf("root-%d-a", group), fmt.Sprintf("root-%d-b", group)},
			[]string{fmt.Sprintf("domain-%d-a", group), fmt.Sprintf("domain-%d-b", group)},
		)
		observed := &primaryIOQueueContext{Context: context.Background(), entered: make(chan struct{})}
		result := make(chan primaryIOAdmissionResult, 1)
		go func() {
			request, err := g.acquire(observed, route, Background)
			result <- primaryIOAdmissionResult{request, err}
		}()
		select {
		case <-observed.entered:
		case value := <-result:
			b.Fatalf("acquisition did not queue: %+v", value)
		case <-time.After(5 * time.Second):
			b.Fatal("acquisition did not reach the queue")
		}
		results = append(results, result)
	}
	return g
}

func BenchmarkPrimaryIOQueueCapacity(b *testing.B) {
	for _, test := range []struct {
		name   string
		queued int
		groups int
		want   bool
	}{
		{"empty", 0, 8, true},
		{"mixed32", 32, 8, true},
		{"mixed96", 96, 8, true},
		{"mixed127", 127, 8, true},
		{"globalFull128", 128, 8, false},
		{"rootFull32", 32, 1, false},
	} {
		for _, parallel := range []bool{false, true} {
			name := test.name + "/serial"
			if parallel {
				name = test.name + "/parallel"
			}
			b.Run(name, func(b *testing.B) {
				g := primaryIOBenchmarkBlockedQueue(b, test.queued, test.groups)
				candidate := &request{route: primaryIOAdmissionRoute(
					[]string{"root-0-a", "root-0-b"}, []string{"domain-0-a", "domain-0-b"},
				)}
				check := func() {
					g.mu.Lock()
					available := g.queueAvailableLocked(candidate)
					g.mu.Unlock()
					if available != test.want {
						b.Errorf("queue available = %v, want %v", available, test.want)
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				if parallel {
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							check()
						}
					})
				} else {
					for index := 0; index < b.N; index++ {
						check()
					}
				}
			})
		}
	}
}

func BenchmarkPrimaryIOBlockedDispatch(b *testing.B) {
	for _, queued := range []int{0, 32, 96, 127} {
		b.Run(fmt.Sprintf("queued%d", queued), func(b *testing.B) {
			g := primaryIOBenchmarkBlockedQueue(b, queued, 8)
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				g.mu.Lock()
				g.dispatchLocked()
				g.mu.Unlock()
			}
		})
	}
}

func BenchmarkPrimaryIOHealthyAdmissionWithBlockedQueue(b *testing.B) {
	for _, queued := range []int{0, 32, 96, 127} {
		b.Run(fmt.Sprintf("queued%d", queued), func(b *testing.B) {
			g := primaryIOBenchmarkBlockedQueue(b, queued, 8)
			route := primaryIORoute("healthy", "healthy-domain")
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				request, err := g.tryAcquire(context.Background(), route, Foreground)
				if err != nil {
					b.Fatal(err)
				}
				g.release(request)
			}
		})
	}
}

type primaryIOBenchmarkCancelAfterQueue struct {
	context.Context
	cancel context.CancelFunc
}

func (ctx *primaryIOBenchmarkCancelAfterQueue) Done() <-chan struct{} {
	ctx.cancel()
	return ctx.Context.Done()
}

func BenchmarkPrimaryIOQueuedAdmissionCancellation(b *testing.B) {
	for _, queued := range []int{0, 32, 96, 127} {
		b.Run(fmt.Sprintf("queued%d", queued), func(b *testing.B) {
			g := primaryIOBenchmarkBlockedQueue(b, queued, 8)
			route, err := prepareRoute(primaryIORoute("candidate", "candidate-domain"))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				ctx, cancel := context.WithCancel(context.Background())
				// acquire evaluates Done only after enqueue and unlock, so each
				// iteration measures a complete queued admission and removal.
				observed := &primaryIOBenchmarkCancelAfterQueue{Context: ctx, cancel: cancel}
				request, err := g.acquirePrepared(observed, route, Background)
				cancel()
				if !errors.Is(err, context.Canceled) || request != nil {
					b.Fatalf("queued admission was not canceled: request=%v err=%v", request, err)
				}
			}
		})
	}
}

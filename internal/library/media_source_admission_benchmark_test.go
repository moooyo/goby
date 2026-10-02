package library

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

// This file can be copied unchanged to a pre-admission checkout. It profiles
// the actual Store worker boundary when available, including its context,
// goroutine and result handoff. The fallback uses the previous helper.
type mediaSourceAdmissionBenchmarkWorker interface {
	runMediaSourceWorker(context.Context, func(context.Context) (*os.File, MediaFile, error)) (*os.File, MediaFile, error)
}

func BenchmarkMediaSourceWorkerBoundary(b *testing.B) {
	for _, parallelism := range []int{1, 8, 32} {
		b.Run("parallelism-"+strconv.Itoa(parallelism), func(b *testing.B) {
			ctx, cancel := context.WithCancel(context.Background())
			store := &Store{ctx: ctx, cancel: cancel, queue: make(chan *scanTask), done: make(chan struct{})}
			b.Cleanup(func() {
				cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
				defer stop()
				if err := store.Close(cleanup); err != nil {
					b.Errorf("source benchmark cleanup failed: %v", err)
				}
			})
			worker, owned := any(store).(mediaSourceAdmissionBenchmarkWorker)
			slots := make(chan struct{}, 4)
			run := func() {
				work := func(context.Context) (*os.File, MediaFile, error) { return nil, MediaFile{}, nil }
				var err error
				if owned {
					_, _, err = worker.runMediaSourceWorker(ctx, work)
				} else {
					_, _, err = runMediaSourceWorker(ctx, slots, func() (*os.File, MediaFile, error) { return work(ctx) })
				}
				if err != nil {
					b.Errorf("source benchmark worker failed: %v", err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			if parallelism == 1 {
				for range b.N {
					run()
				}
			} else {
				// RunParallel uses this as a GOMAXPROCS multiplier, not an exact
				// HTTP caller count. The source budget remains four in both trees.
				b.SetParallelism(parallelism)
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						run()
					}
				})
			}
			// Include final actual-worker cleanup in each measured sample. A
			// successful HTTP-style return can precede owner release slightly.
			if !owned {
				deadline := time.NewTimer(10 * time.Second)
				defer deadline.Stop()
				for range cap(slots) {
					select {
					case slots <- struct{}{}:
					case <-deadline.C:
						b.Fatal("legacy source benchmark owners did not drain")
					}
				}
				for range cap(slots) {
					<-slots
				}
			}
			cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			if err := store.Close(cleanup); err != nil {
				b.Errorf("source benchmark owner drain failed: %v", err)
			}
			b.StopTimer()
		})
	}
}

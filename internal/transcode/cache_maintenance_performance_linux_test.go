//go:build linux

package transcode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkManagerStableCacheMaintenance250ms can be copied unchanged into
// the accepted baseline. One operation represents a production maintenance
// tick; setup and final deletion are excluded from its CPU/allocation cost.
func BenchmarkManagerStableCacheMaintenance250ms(b *testing.B) {
	for _, segments := range []int{1024, 4096} {
		b.Run(fmt.Sprintf("jobs-16-segments-%d", segments), func(b *testing.B) {
			options := Options{Root: filepath.Join(b.TempDir(), "cache"), FFmpegPath: "/not-executed/ffmpeg",
				Repository: &managerTestRepository{}, IdleTimeout: time.Hour, MinFreeBytes: 1,
				pollInterval: 250 * time.Millisecond}
			m, err := NewManager(context.Background(), options)
			if err != nil {
				b.Fatal(err)
			}
			m.cancel()
			<-m.loopDone
			b.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := m.Close(ctx); err != nil {
					b.Error(err)
				}
			})
			var directories []string
			for index := range 16 {
				id := fmt.Sprintf("%032x", index+1)
				if err := m.cache.CreateJob(id); err != nil {
					b.Fatal(err)
				}
				directory := filepath.Join(options.Root, id)
				directories = append(directories, directory)
				for segment := range segments {
					if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("segment-%06d.ts", segment)), make([]byte, 188), 0o600); err != nil {
						b.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(directory, "main.m3u8"), []byte("playlist"), 0o600); err != nil {
					b.Fatal(err)
				}
				spec := managerTestSpec(index + 1)
				var size int64
				var ready bool
				// This optional completion boundary is also present in the real
				// producer path. The baseline uses its full scan as before.
				if finalizer, ok := any(m.cache).(interface {
					FinalizePlanJob(string, Plan) (int64, bool, error)
				}); ok {
					size, ready, err = finalizer.FinalizePlanJob(id, spec.Plan)
				} else {
					size, ready, err = m.cache.ScanPlanJob(id, spec.Plan)
				}
				if err != nil || !ready {
					b.Fatalf("completion boundary: %d, %t, %v", size, ready, err)
				}
				now := time.Now().UTC()
				jobContext, cancel := context.WithCancel(context.Background())
				b.Cleanup(cancel)
				j := &managedJob{record: Record{ID: id, Spec: spec, State: "completed", CreatedAt: now, UpdatedAt: now, LastAccessAt: now, OutputBytes: size},
					ctx: jobContext, cancel: cancel, directory: true, ready: true, finished: true,
					created: make(chan struct{}), launch: make(chan struct{}), done: make(chan struct{}), changed: make(chan struct{})}
				close(j.created)
				close(j.done)
				m.jobs[id], m.bySpec[spec] = j, j
				m.bytes += size
			}
			stopCounting, checks := benchmarkCacheScanCounter(b, directories, fmt.Sprintf("segment-%06d.ts", segments-1))
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				m.maintainJobs(true)
			}
			b.StopTimer()
			stopCounting()
			b.ReportMetric(float64(checks.Load())/float64(b.N), "sentinel-opens/tick")
		})
	}
}

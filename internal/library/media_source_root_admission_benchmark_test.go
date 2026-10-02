package library

import (
	"context"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
)

// This component benchmark measures scheduling and accounting overhead only.
// It performs no database queries, filesystem I/O, decoding or HTTP delivery.
func BenchmarkMediaSourceRootAdmission(b *testing.B) {
	for _, layout := range []string{"unclassified", "one-root", "four-domains"} {
		for _, parallelism := range []int{1, 8, 32} {
			b.Run(layout+"/parallelism-"+strconv.Itoa(parallelism), func(b *testing.B) {
				admission := newMediaSourceAdmission()
				anchor := b.TempDir()
				var roots [4]mediaSourceRootKey
				var domains [4]string
				for index := range roots {
					id := strconv.Itoa(index)
					roots[index] = mediaSourceRootKey{catalog: "benchmark", id: id}
					domains[index] = filepath.Join(anchor, id)
				}
				var callers atomic.Uint64
				run := func(index uint64) {
					var release func()
					var err error
					if layout == "unclassified" {
						release, err = admission.acquire(context.Background(), false)
					} else {
						if layout == "one-root" {
							index = 0
						}
						release, err = admission.acquireRoot(context.Background(), false, roots[index%4], domains[index%4])
					}
					if err != nil {
						b.Errorf("source admission failed: %v", err)
						return
					}
					release()
				}
				b.ReportAllocs()
				b.ResetTimer()
				if parallelism == 1 {
					for range b.N {
						run(0)
					}
				} else {
					b.SetParallelism(parallelism)
					b.RunParallel(func(pb *testing.PB) {
						index := callers.Add(1)
						for pb.Next() {
							run(index)
						}
					})
				}
				b.StopTimer()
				admission.mu.Lock()
				defer admission.mu.Unlock()
				if admission.active != 0 || len(admission.waiters) != 0 || len(admission.roots) != 0 || len(admission.domains) != 0 {
					b.Fatal("benchmark retained source admission ownership")
				}
			})
		}
	}
}

//go:build linux

package transcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func warmRetainedMaintenanceFixture(count int) *Manager {
	m := &Manager{jobs: make(map[string]*managedJob, count), options: Options{IdleTimeout: time.Hour}}
	now := time.Now().UTC()
	for index := range count {
		id := fmt.Sprintf("%032x", index+1)
		m.jobs[id] = &managedJob{record: Record{ID: id, State: "completed", LastAccessAt: now},
			directory: true, ready: true, mediaReady: true, finished: true}
	}
	return m
}

func TestManagerWarmMaintenanceDoesNotAllocateRetainedHistory(t *testing.T) {
	for _, count := range []int{128, 4096} {
		t.Run(fmt.Sprintf("jobs-%d", count), func(t *testing.T) {
			m := warmRetainedMaintenanceFixture(count)
			if allocations := testing.AllocsPerRun(20, func() { m.maintainJobs(false) }); allocations != 0 {
				t.Fatalf("a warm reader wake allocated retained history: %.1f allocations", allocations)
			}
			if len(m.jobs) != count || m.maintenanceCursor != "" {
				t.Fatal("a wake without inspection candidates changed retained history or the audit cursor")
			}
		})
	}
}

func TestManagerEmptyInspectionStillReclaimsExpiredOutput(t *testing.T) {
	m := newJobLockingManager(t)
	job := addJobLockingOutput(t, m, 1, false)
	m.mu.Lock()
	job.record.LastAccessAt = time.Now().UTC().Add(-2 * m.options.IdleTimeout)
	m.mu.Unlock()
	m.maintainJobs(false)
	m.mu.Lock()
	retained := m.jobs[job.record.ID] != nil
	bytes := m.bytes
	m.mu.Unlock()
	if retained || bytes != 0 {
		t.Fatal("an empty inspection set skipped expired output reclamation")
	}
	if _, err := os.Stat(filepath.Join(m.cache.RootPath(), job.record.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired output was not actually removed: %v", err)
	}
}

func TestManagerEmptyInspectionStillStopsFailedAdmission(t *testing.T) {
	m := warmRetainedMaintenanceFixture(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	job := &managedJob{record: Record{ID: "queued", State: "queued", LastAccessAt: time.Now().UTC()},
		ctx: ctx, cancel: cancel, changed: make(chan struct{})}
	m.jobs[job.record.ID] = job
	m.cacheFailed = true
	m.maintainJobs(false)
	if !errors.Is(ctx.Err(), context.Canceled) || job.stopCode != "cache_unavailable" {
		t.Fatal("an empty inspection set skipped the sticky cache failure")
	}
}

// The fixture has no filesystem work; one operation is a processed warm-reader
// maintenance wake, including the final full retained-job lifecycle pass.
func BenchmarkManagerWarmRetainedMaintenance(b *testing.B) {
	for _, count := range []int{128, 4096} {
		b.Run(fmt.Sprintf("jobs-%d", count), func(b *testing.B) {
			m := warmRetainedMaintenanceFixture(count)
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				m.maintainJobs(false)
			}
		})
	}
}

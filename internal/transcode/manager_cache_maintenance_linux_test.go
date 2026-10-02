//go:build linux

package transcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerPendingCacheAccountingFencesAdmissionAndScheduling(t *testing.T) {
	m := newJobLockingManager(t)
	job := addJobLockingOutput(t, m, 1, false)
	queued := addAccountingQueuedJob(t, m, 2)
	for index := range 600 {
		writeCacheTestFile(t, filepath.Join(m.cache.RootPath(), job.record.ID, fmt.Sprintf("segment-%06d.ts", index)), "payload")
	}
	m.maintainJobs(true)
	m.mu.Lock()
	pending := job.accountingPending && m.pendingAccountingJobs == 1 && !job.accountingUnknown
	m.mu.Unlock()
	if !pending {
		t.Fatal("budgeted change backlog did not fence its incomplete accounting")
	}
	input := managerTestInput(t)
	if _, err := m.Ensure(context.Background(), managerTestSpec(3), input); !errors.Is(err, ErrOutputUnavailable) {
		t.Fatalf("unmeasured backlog admitted new work: %v", err)
	}
	assertManagerInputClosed(t, input)
	m.schedule()
	select {
	case <-queued.launch:
		t.Fatal("unmeasured backlog launched queued work")
	default:
	}
	// Pending is distinct from a failed observation. Once all descriptor
	// checks close the backlog, admission/scheduling can resume without cleanup.
	for attempt := 0; attempt < 100; attempt++ {
		m.maintainJobs(true)
		m.mu.Lock()
		known := m.pendingAccountingJobs == 0
		m.mu.Unlock()
		if known {
			break
		}
	}
	m.mu.Lock()
	known := !job.accountingPending && !job.accountingUnknown && m.pendingAccountingJobs == 0 && job.record.OutputBytes == 600*7+15
	m.mu.Unlock()
	if !known {
		t.Fatal("closed change accounting retained a temporary admission fence")
	}
	m.schedule()
	select {
	case <-queued.launch:
	default:
		t.Fatal("closed change accounting did not restore scheduling")
	}
}

func TestManagerFrozenOpenImmediatelyFencesChangedOutput(t *testing.T) {
	for _, mode := range []string{"hls", "progressive"} {
		t.Run(mode, func(t *testing.T) {
			m := newJobLockingManager(t)
			job := addJobLockingOutput(t, m, 1, mode == "progressive")
			if _, _, err := m.cache.FinalizePlanJob(job.record.ID, job.record.Spec.Plan); err != nil {
				t.Fatal(err)
			}
			queued := addAccountingQueuedJob(t, m, 2)
			known := job.record.OutputBytes
			name := "segment-0.ts"
			if mode == "progressive" {
				name = "stream.bin"
			}
			writeCacheTestFile(t, filepath.Join(m.cache.RootPath(), job.record.ID, name), "different frozen payload")
			if mode == "progressive" {
				reader, err := m.OpenProgressive(context.Background(), job.record.Spec.Scope, job.record.ID)
				if reader != nil {
					_ = reader.Close()
				}
				if reader != nil || !errors.Is(err, ErrOutputUnavailable) {
					t.Fatalf("changed frozen progressive output = %v", err)
				}
			} else {
				handle, err := m.TryOpen(job.record.Spec.Scope, job.record.ID, name)
				if handle != nil {
					_ = handle.Close()
				}
				if handle != nil || !errors.Is(err, ErrOutputUnavailable) {
					t.Fatalf("changed frozen HLS output = %v", err)
				}
			}
			assertAccountingFence(t, m, job, known, known, 1)
			assertAccountingAdmissionBlocked(t, m, queued, 3)
			managerAccessAssertReaders(t, m, job.record.ID, 0, 0)
		})
	}
}

func TestManagerCacheMaintenanceFairlyClosesEveryJobBacklog(t *testing.T) {
	m := newJobLockingManager(t)
	const count = 20
	jobs := make([]*managedJob, 0, count)
	for index := range count {
		job := addJobLockingOutput(t, m, index+1, false)
		jobs = append(jobs, job)
		for segment := range 600 {
			writeCacheTestFile(t, filepath.Join(m.cache.RootPath(), job.record.ID, fmt.Sprintf("segment-%06d.ts", segment)), "payload")
		}
	}
	for attempt := 0; attempt < 100; attempt++ {
		m.maintainJobs(true)
		m.mu.Lock()
		known := 0
		for _, job := range jobs {
			if job.record.OutputBytes == 600*7+15 && !job.accountingPending && !job.accountingUnknown {
				known++
			}
		}
		m.mu.Unlock()
		if known == count {
			return
		}
	}
	for _, job := range jobs {
		if _, err := os.Stat(filepath.Join(m.cache.RootPath(), job.record.ID)); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("a retained job starved behind the global maintenance budget")
}

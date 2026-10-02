//go:build linux

package transcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newJobLockingManager(t *testing.T) *Manager {
	t.Helper()
	return newJobLockingManagerWithOptions(t, managerTestOptions(t, nil))
}

func newJobLockingManagerWithOptions(t *testing.T, options Options) *Manager {
	t.Helper()
	options.IdleTimeout = time.Hour
	options.pollInterval = 250 * time.Millisecond
	m := newTestManager(t, options)
	m.cancel()
	managerAccessWait(t, m.loopDone, "the paused maintenance loop")
	return m
}

func addJobLockingOutput(t *testing.T, m *Manager, index int, progressive bool) *managedJob {
	t.Helper()
	id := fmt.Sprintf("%032x", index)
	if err := m.cache.CreateJob(id); err != nil {
		t.Fatal(err)
	}
	spec := managerTestSpec(index)
	if progressive {
		spec.Plan.OutputMode = "progressive"
		writeCacheTestFile(t, filepath.Join(m.cache.RootPath(), id, "stream.bin"), "playable stream")
	} else {
		writeCacheTestFile(t, filepath.Join(m.cache.RootPath(), id, "main.m3u8"), "playlist")
		writeCacheTestFile(t, filepath.Join(m.cache.RootPath(), id, "segment-0.ts"), "segment")
	}
	size, ready, err := m.cache.ScanPlanJob(id, spec.Plan)
	if err != nil || !ready {
		t.Fatalf("initial cache scan = %d, %t, %v", size, ready, err)
	}
	now := time.Now().UTC()
	jobCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	j := &managedJob{record: Record{ID: id, Spec: spec, State: "completed", CreatedAt: now, UpdatedAt: now, LastAccessAt: now, OutputBytes: size},
		ctx: jobCtx, cancel: cancel, directory: true, ready: true, mediaReady: progressive, finished: true,
		created: make(chan struct{}), launch: make(chan struct{}), done: make(chan struct{}), changed: make(chan struct{})}
	close(j.created)
	close(j.done)
	m.mu.Lock()
	m.jobs[id], m.bySpec[spec] = j, j
	m.bytes += size
	m.mu.Unlock()
	return j
}

func TestManagerBlockedJobScanAllowsOtherJobOpenAndCleanup(t *testing.T) {
	for _, output := range []string{"hls", "progressive"} {
		t.Run(output, func(t *testing.T) {
			m := newJobLockingManager(t)
			first := addJobLockingOutput(t, m, 1, false)
			other := addJobLockingOutput(t, m, 2, output == "progressive")
			release := holdCacheJob(t, m.cache, first.record.ID)
			scanned := make(chan struct{})
			go func() { m.maintainJobs(true); close(scanned) }()
			waitCacheJobReferences(t, m.cache, first.record.ID, 2)
			opened := make(chan error, 1)
			go func() {
				var err error
				if output == "progressive" {
					reader, openErr := m.OpenProgressive(context.Background(), other.record.Spec.Scope, other.record.ID)
					err = openErr
					if err == nil {
						err = reader.Close()
					}
				} else {
					handle, openErr := m.TryOpen(other.record.Spec.Scope, other.record.ID, "segment-0.ts")
					err = openErr
					if err == nil {
						err = handle.Close()
					}
				}
				if err == nil && !m.reclaim(other, true) {
					err = errors.New("other job was not reclaimed")
				}
				opened <- err
			}()
			if err := cacheLockingResult(t, opened, "another job's open and cleanup"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(m.cache.RootPath(), other.record.ID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("other job's files were retained: %v", err)
			}
			same := managerAccessStartOpen(t, m, first.record.Spec.Scope, first.record.ID, "segment-0.ts")
			managerAccessWaitReaders(t, m, first.record.ID, 1)
			select {
			case result := <-same:
				if result.handle != nil {
					_ = result.handle.Close()
				}
				t.Fatalf("same-job open escaped its scan: %v", result.err)
			default:
			}
			if err := m.CancelJob(first.record.ID, first.record.Spec.Scope); err != nil {
				t.Fatal(err)
			}
			release()
			managerAccessWait(t, scanned, "the unblocked job scan")
			if handle, err := managerAccessReceiveOpen(t, same); handle != nil || !errors.Is(err, ErrJobCancelled) {
				t.Fatalf("blocked open ignored cancellation: %v", err)
			}
			managerAccessAssertReaders(t, m, first.record.ID, 0, 0)
		})
	}
}

func TestManagerAdmissionRechecksCancellationAfterJobLockWait(t *testing.T) {
	options := managerTestOptions(t, nil)
	options.MaxJobs, options.MaxRetainedJobs = 1, 1
	m := newJobLockingManagerWithOptions(t, options)
	completed := addJobLockingOutput(t, m, 1, false)
	completed.filesMu.Lock()
	var unlockOnce sync.Once
	release := func() { unlockOnce.Do(completed.filesMu.Unlock) }
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		reclaimed, err := m.reclaimForAdmission(ctx, managerTestSpec(2))
		if reclaimed {
			err = errors.New("cancelled admission reclaimed healthy history")
		}
		result <- err
	}()
	// No maintenance worker remains. An occupied shared gate therefore proves
	// that this admission is inside its storage phase, waiting for the job lock.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if !m.filesMu.TryLock() {
			break
		}
		m.filesMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("admission did not reach the job lock")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	release()
	if err := cacheLockingResult(t, result, "cancelled admission"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission = %v", err)
	}
	m.mu.Lock()
	retained, bytes, directory := m.jobs[completed.record.ID] == completed, m.bytes, completed.directory
	m.mu.Unlock()
	if !retained || bytes != completed.record.OutputBytes || !directory {
		t.Fatal("cancelled admission mutated healthy retained output")
	}
}

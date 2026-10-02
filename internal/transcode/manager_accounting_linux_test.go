//go:build linux

package transcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type managerAccountingRepository struct {
	*managerTestRepository
	targetID string
	entered  chan struct{}
	release  <-chan struct{}
	once     sync.Once
}

func (r *managerAccountingRepository) Update(ctx context.Context, record Record) error {
	if record.ID == r.targetID && record.State == "failed" {
		r.once.Do(func() { close(r.entered) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.release:
		}
	}
	return r.managerTestRepository.Update(ctx, record)
}

func newAccountingTestManager(t *testing.T, repository Repository) *Manager {
	t.Helper()
	options := managerAccessOptions(t, nil)
	if repository != nil {
		options.Repository = repository
	}
	m, err := NewManager(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	m.cancel()
	managerAccessWait(t, m.loopDone, "the paused accounting maintenance loop")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := m.Close(ctx)
		m.mu.Lock()
		cacheFailed := m.cacheFailed
		m.mu.Unlock()
		if err != nil && !(cacheFailed && errors.Is(err, ErrCacheUnsafe)) {
			t.Errorf("close accounting manager: %v", err)
		}
	})
	return m
}

func addAccountingQueuedJob(t *testing.T, m *Manager, index int) *managedJob {
	t.Helper()
	job := addJobLockingOutput(t, m, index, false)
	if !m.reclaim(job, true) {
		t.Fatal("could not retire the queued fixture's temporary output")
	}
	job.record.State, job.record.OutputBytes = "queued", 0
	job.directory, job.ready, job.finished, job.durable = false, false, false, true
	job.input = managerTestInput(t)
	job.done = make(chan struct{})
	if err := m.options.Repository.Create(context.Background(), job.record); err != nil {
		t.Fatal(err)
	}
	// This explicit queued fixture owns the same fresh reservation as Ensure;
	// production scheduling never supplies a missing ticket after admission.
	completion, err := m.completions.reserve()
	if err != nil {
		t.Fatal(err)
	}
	job.completion = completion
	m.mu.Lock()
	job.reclaiming = false
	m.jobs[job.record.ID], m.bySpec[job.record.Spec] = job, job
	m.queue = append(m.queue, job)
	m.mu.Unlock()
	t.Cleanup(func() {
		m.mu.Lock()
		finished := job.finished
		if !finished {
			m.stopLocked(job, "cancelled")
		}
		m.mu.Unlock()
		if !finished {
			if err := m.enqueueFinalization(job, context.Canceled, nil); err != nil {
				t.Errorf("enqueue queued cleanup: %v", err)
				return
			}
			managerAccessWait(t, job.done, "the queued fixture's terminal handler")
		}
	})
	return job
}

func assertAccountingFence(t *testing.T, m *Manager, job *managedJob, knownBytes, totalBytes int64, unknown int) {
	t.Helper()
	m.mu.Lock()
	gotBytes, gotTotal, gotUnknown := job.record.OutputBytes, m.bytes, m.unaccountedJobs
	marked := job.accountingUnknown
	m.mu.Unlock()
	if gotBytes != knownBytes || gotTotal != totalBytes || gotUnknown != unknown || !marked {
		t.Fatalf("unknown accounting = job %d, total %d, unknown %d, marked %t; want %d, %d, %d, true",
			gotBytes, gotTotal, gotUnknown, marked, knownBytes, totalBytes, unknown)
	}
}

func assertAccountingAdmissionBlocked(t *testing.T, m *Manager, queued *managedJob, index int) {
	t.Helper()
	input := managerTestInput(t)
	if _, err := m.Ensure(context.Background(), managerTestSpec(index), input); !errors.Is(err, ErrOutputUnavailable) {
		t.Fatalf("unknown directory admitted new work: %v", err)
	}
	assertManagerInputClosed(t, input)
	m.schedule()
	select {
	case <-queued.launch:
		t.Fatal("unknown directory allowed queued work to start")
	default:
	}
}

func TestManagerFinalScanFailurePreservesAccountingUntilGuardedRemoval(t *testing.T) {
	releasePersistence := make(chan struct{})
	repository := &managerAccountingRepository{managerTestRepository: &managerTestRepository{},
		entered: make(chan struct{}), release: releasePersistence}
	m := newAccountingTestManager(t, repository)
	job := addJobLockingOutput(t, m, 1, false)
	other := addJobLockingOutput(t, m, 2, false)
	// Ensure indexes the captured execution plan, while the filesystem helper
	// deliberately creates an uncaptured plan for direct artifact lookups.
	captured, err := CaptureExecution(other.record.Spec.Plan, DefaultExecutionOptions(m.options.Threads))
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	delete(m.bySpec, other.record.Spec)
	other.record.Spec.Plan = captured
	m.bySpec[other.record.Spec] = other
	m.mu.Unlock()
	queued := addAccountingQueuedJob(t, m, 3)
	knownBytes, totalBytes := job.record.OutputBytes, job.record.OutputBytes+other.record.OutputBytes
	initial := job.record
	initial.State = "queued"
	if err := repository.Create(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	job.input = managerTestInput(t)
	m.mu.Lock()
	job.record.State, job.finished, job.running, job.durable = "running", false, true, true
	job.done = make(chan struct{})
	m.running, m.runningUsers[job.record.Spec.Scope.UserID], m.runningAuth[job.record.Spec.Scope.AuthSessionID] = 1, 1, 1
	m.mu.Unlock()
	repository.targetID = job.record.ID
	unknownPath := filepath.Join(m.cache.RootPath(), job.record.ID, "unknown-accounting.txt")
	writeCacheTestFile(t, unknownPath, "owned unknown output larger than the last accounting snapshot")
	finished := make(chan struct{})
	go func() { m.finish(job, nil); close(finished) }()
	t.Cleanup(func() { managerAccessWait(t, finished, "the final accounting worker") })
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releasePersistence) }) }
	t.Cleanup(release)
	managerAccessWait(t, repository.entered, "the failed final status persistence barrier")
	assertManagerInputClosed(t, job.input)
	assertAccountingFence(t, m, job, knownBytes, totalBytes, 1)
	assertAccountingAdmissionBlocked(t, m, queued, 4)
	m.mu.Lock()
	pendingPersistence := !job.finished && m.running == 0
	m.mu.Unlock()
	if !pendingPersistence || m.reclaim(job, false) {
		t.Fatal("pending final persistence became reclaimable")
	}
	if reused, err := m.Ensure(context.Background(), other.record.Spec, managerTestInput(t)); err != nil || reused.ID != other.record.ID {
		t.Fatalf("unknown accounting prevented valid cached reuse: %v", err)
	}
	handle, err := m.TryOpen(other.record.Spec.Scope, other.record.ID, "segment-0.ts")
	if err != nil {
		t.Fatalf("unknown accounting prevented another job's cached read: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(unknownPath); err != nil {
		t.Fatal(err)
	}
	// A later successful scan must not clear the failed final inspection's fence.
	if _, _, err := m.cache.ScanPlanJob(job.record.ID, job.record.Spec.Plan); err != nil {
		t.Fatal(err)
	}
	assertAccountingFence(t, m, job, knownBytes, totalBytes, 1)
	holdRemoval := holdCacheJob(t, m.cache, job.record.ID)
	release()
	managerAccessWait(t, finished, "the completed final persistence attempt")
	removed := make(chan struct{})
	go func() { m.reclaim(job, false); close(removed) }()
	waitCacheJobReferences(t, m.cache, job.record.ID, 2)
	assertAccountingFence(t, m, job, knownBytes, totalBytes, 1)
	assertAccountingAdmissionBlocked(t, m, queued, 5)
	holdRemoval()
	managerAccessWait(t, removed, "the guarded directory removal")
	m.mu.Lock()
	cleared := !job.accountingUnknown && m.unaccountedJobs == 0 && job.record.OutputBytes == 0 && m.bytes == other.record.OutputBytes
	m.mu.Unlock()
	if !cleared {
		t.Fatal("successful guarded removal did not clear its accounting reservation")
	}
	m.schedule()
	select {
	case <-queued.launch:
	default:
		t.Fatal("guarded removal did not restore queue scheduling")
	}
	if _, err := m.Ensure(context.Background(), managerTestSpec(6), managerTestInput(t)); err != nil {
		t.Fatalf("successful cleanup did not restore new admission: %v", err)
	}
}

func TestManagerPeriodicScanFailureRetainsPinnedUnknownAccounting(t *testing.T) {
	m := newAccountingTestManager(t, nil)
	job := addJobLockingOutput(t, m, 1, false)
	queued := addAccountingQueuedJob(t, m, 2)
	knownBytes := job.record.OutputBytes
	handle, err := m.TryOpen(job.record.Spec.Scope, job.record.ID, "segment-0.ts")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	unknownPath := filepath.Join(m.cache.RootPath(), job.record.ID, "unknown-accounting.txt")
	writeCacheTestFile(t, unknownPath, "owned unknown output")
	m.maintainJobs(true)
	assertAccountingFence(t, m, job, knownBytes, knownBytes, 1)
	m.maintainJobs(true)
	assertAccountingFence(t, m, job, knownBytes, knownBytes, 1)
	assertAccountingAdmissionBlocked(t, m, queued, 3)
	if m.reclaim(job, true) {
		t.Fatal("reader pin did not retain the unknown directory")
	}
	if err := os.Remove(unknownPath); err != nil {
		t.Fatal(err)
	}
	// Repairing the unknown entry and shrinking a known artifact still cannot
	// reduce the reservation before the pinned directory is safely removed.
	writeCacheTestFile(t, filepath.Join(m.cache.RootPath(), job.record.ID, "segment-0.ts"), "x")
	m.maintainJobs(true)
	assertAccountingFence(t, m, job, knownBytes, knownBytes, 1)
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if !m.reclaim(job, true) {
		t.Fatal("last reader release did not permit guarded cleanup")
	}
	m.mu.Lock()
	cleared := m.unaccountedJobs == 0 && m.bytes == 0 && !job.accountingUnknown
	m.mu.Unlock()
	if !cleared {
		t.Fatal("guarded cleanup retained unknown accounting after the last reader")
	}
	m.schedule()
	select {
	case <-queued.launch:
	default:
		t.Fatal("last reader cleanup did not restore queue scheduling")
	}
}

func TestManagerPeriodicScanFailureKeepsExecutionSlotUntilFinalization(t *testing.T) {
	m := newAccountingTestManager(t, nil)
	job := addJobLockingOutput(t, m, 1, false)
	queued := addAccountingQueuedJob(t, m, 2)
	knownBytes := job.record.OutputBytes
	job.input = managerTestInput(t)
	m.mu.Lock()
	job.record.State, job.finished, job.running = "running", false, true
	job.done = make(chan struct{})
	m.running, m.runningUsers[job.record.Spec.Scope.UserID], m.runningAuth[job.record.Spec.Scope.AuthSessionID] = 1, 1, 1
	m.mu.Unlock()
	t.Cleanup(func() {
		m.mu.Lock()
		finished := job.finished
		m.mu.Unlock()
		if !finished {
			m.finish(job, context.Canceled)
		}
	})
	unknownPath := filepath.Join(m.cache.RootPath(), job.record.ID, "unknown-accounting.txt")
	writeCacheTestFile(t, unknownPath, "owned unknown running output")
	m.maintainJobs(true)
	assertAccountingFence(t, m, job, knownBytes, knownBytes, 1)
	m.mu.Lock()
	occupied := job.running && !job.finished && m.running == 1 && job.stopCode == "cache_unavailable"
	m.mu.Unlock()
	if !occupied {
		t.Fatal("failed periodic inspection released an unreaped producer's execution slot")
	}
	assertAccountingAdmissionBlocked(t, m, queued, 3)
	m.finish(job, context.Canceled)
	assertAccountingFence(t, m, job, knownBytes, knownBytes, 1)
	m.mu.Lock()
	released := job.finished && !job.running && m.running == 0
	m.mu.Unlock()
	if !released {
		t.Fatal("finalization did not release the stopped producer's execution slot")
	}
	if err := os.Remove(unknownPath); err != nil {
		t.Fatal(err)
	}
	m.reclaim(job, false)
	m.mu.Lock()
	cleared := m.unaccountedJobs == 0 && m.bytes == 0
	m.mu.Unlock()
	if !cleared {
		t.Fatal("final cleanup did not clear the repeated inspection failure")
	}
}

func TestManagerUnknownAccountingCleanupFailureRemainsSticky(t *testing.T) {
	m := newAccountingTestManager(t, nil)
	job := addJobLockingOutput(t, m, 1, false)
	queued := addAccountingQueuedJob(t, m, 2)
	knownBytes := job.record.OutputBytes
	unknownPath := filepath.Join(m.cache.RootPath(), job.record.ID, "unknown-accounting.txt")
	writeCacheTestFile(t, unknownPath, "owned unknown output that guarded cleanup must preserve")
	m.mu.Lock()
	job.readers, m.readers = 1, 1
	m.mu.Unlock()
	m.maintainJobs(true)
	assertAccountingFence(t, m, job, knownBytes, knownBytes, 1)
	m.releaseReader(job)
	if !m.reclaim(job, true) {
		t.Fatal("failed guarded removal did not retire its metadata")
	}
	assertAccountingFence(t, m, job, knownBytes, knownBytes, 1)
	m.mu.Lock()
	failed := m.cacheFailed && errors.Is(m.closeErr, ErrCacheUnsafe)
	m.mu.Unlock()
	if !failed {
		t.Fatal("unknown cleanup failure did not retain the sticky cache failure")
	}
	assertAccountingAdmissionBlocked(t, m, queued, 3)
	if _, err := os.Stat(unknownPath); err != nil {
		t.Fatalf("guarded removal modified unknown output: %v", err)
	}
	m.fail(queued, "cancelled")
	if err := m.enqueueFinalization(queued, context.Canceled, nil); err != nil {
		t.Fatal(err)
	}
	managerAccessWait(t, queued.done, "the cancelled queued fixture's terminal handler")
	// A shutdown attempt may time out, but its retry retains the sticky failure.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Close(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("initial shutdown lost its failure classification: %v", err)
	}
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelClose()
	if err := m.Close(closeCtx); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("shutdown retry lost the sticky cleanup failure: %v", err)
	}
}

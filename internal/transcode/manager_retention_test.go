//go:build linux

package transcode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func retentionTestOptions(t *testing.T) Options {
	t.Helper()
	options := managerTestOptions(t, func(_ context.Context, _, directory string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		return RunResult{}, publishManagerTestOutput(directory, 188)
	})
	options.MaxRetainedJobs = 2
	options.IdleTimeout = time.Hour
	return options
}

func retentionTestComplete(t *testing.T, m *Manager, index int) Record {
	t.Helper()
	input := managerTestInput(t)
	record, err := m.Ensure(context.Background(), managerTestSpec(index), input)
	if err != nil {
		t.Fatalf("admit job %d: %v", index, err)
	}
	completed := managerTestWaitFinished(t, m, record.ID)
	if completed.State != "completed" {
		t.Fatalf("job %d did not complete: %+v", index, completed)
	}
	assertManagerInputClosed(t, input)
	return completed
}

func TestManagerRetentionPressureReclaimsCompletedHistoryBeforeIdleExpiry(t *testing.T) {
	options := retentionTestOptions(t)
	m := newTestManager(t, options)
	repository := options.Repository.(*managerTestRepository)
	first := retentionTestComplete(t, m, 1)
	second := retentionTestComplete(t, m, 2)
	duplicate := managerTestInput(t)
	if reused, err := m.Ensure(context.Background(), first.Spec, duplicate); err != nil || reused.ID != first.ID {
		t.Fatalf("retention pressure prevented exact output reuse: %v, %+v", err, reused)
	}
	assertManagerInputClosed(t, duplicate)
	// Both records are younger than the idle timeout; make the expected victim
	// explicit rather than depending on the filesystem clock's resolution.
	m.mu.Lock()
	m.jobs[first.ID].record.LastAccessAt = time.Now().UTC().Add(-time.Minute)
	m.mu.Unlock()
	third := retentionTestComplete(t, m, 3)
	if _, err := m.Snapshot(first.Spec.Scope, first.ID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("oldest terminal history survived pressure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(options.Root, first.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("evicted output remains on disk: %v", err)
	}
	m.mu.Lock()
	count, cacheBytes := len(m.jobs), m.bytes
	m.mu.Unlock()
	if count != 2 || cacheBytes != second.OutputBytes+third.OutputBytes {
		t.Fatalf("reclamation accounting = %d jobs, %d bytes", count, cacheBytes)
	}
	repository.mu.Lock()
	persisted := repository.records[first.ID]
	repository.mu.Unlock()
	if persisted != first {
		t.Fatalf("pressure rewrote durable terminal history: %+v", persisted)
	}
	// Exercise repeated turnover while maintenance scans the same snapshots.
	// Every accepted source must close, and retention must never grow to mask
	// the admission defect.
	for index := 4; index <= 24; index++ {
		retentionTestComplete(t, m, index)
		m.mu.Lock()
		count := len(m.jobs)
		m.mu.Unlock()
		if count != options.MaxRetainedJobs {
			t.Fatalf("turnover exceeded the retained budget: %d", count)
		}
	}
	if health := m.Health(); !health.Available {
		t.Fatalf("concurrent maintenance invalidated reclaimed history: %+v", health)
	}
}

func TestManagerRetentionPressurePrefersInvalidatedHistory(t *testing.T) {
	options := retentionTestOptions(t)
	m := newTestManager(t, options)
	first := retentionTestComplete(t, m, 1)
	second := retentionTestComplete(t, m, 2)
	m.mu.Lock()
	m.jobs[first.ID].record.LastAccessAt = time.Now().UTC().Add(-time.Minute)
	m.mu.Unlock()
	if err := m.CancelJob(second.ID, second.Spec.Scope); err != nil {
		t.Fatal(err)
	}
	retentionTestComplete(t, m, 3)
	if _, err := m.Snapshot(first.Spec.Scope, first.ID); err != nil {
		t.Fatalf("reusable output was evicted before invalidated history: %v", err)
	}
	if _, err := m.Snapshot(second.Spec.Scope, second.ID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("invalidated terminal history survived pressure: %v", err)
	}
}

func TestManagerRetentionPressurePinsReadersAndUnreapedProcesses(t *testing.T) {
	started, release := make(chan int64, 1), make(chan struct{})
	options := retentionTestOptions(t)
	options.run = func(ctx context.Context, _, directory string, _ *os.File, plan Plan, _ int, _ func(Progress)) (RunResult, error) {
		if plan.StartTicks == 1 {
			started <- 1
			<-ctx.Done()
			<-release
			return RunResult{}, ctx.Err()
		}
		return RunResult{}, publishManagerTestOutput(directory, 188)
	}
	m := newTestManager(t, options)
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	first := retentionTestComplete(t, m, 1)
	reader, err := m.Open(context.Background(), first.Spec.Scope, first.ID, "segment-0.ts")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	secondSpec := managerTestSpec(2)
	secondSpec.Plan.StartTicks = 1
	secondInput := managerTestInput(t)
	second, err := m.Ensure(context.Background(), secondSpec, secondInput)
	if err != nil {
		t.Fatal(err)
	}
	managerTestWaitStart(t, started)
	if err := m.CancelJob(second.ID, second.Spec.Scope); err != nil {
		t.Fatal(err)
	}
	rejected := managerTestInput(t)
	if _, err := m.Ensure(context.Background(), managerTestSpec(3), rejected); !errors.Is(err, ErrBusy) {
		t.Fatalf("pinned output or unreaped process was evicted: %v", err)
	}
	assertManagerInputClosed(t, rejected)
	if _, err := secondInput.Stat(); err != nil {
		t.Fatalf("unreaped producer lost its input: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	if stopped := managerTestWaitFinished(t, m, second.ID); stopped.State != "cancelled" {
		t.Fatalf("cancelled producer finished as %+v", stopped)
	}
	assertManagerInputClosed(t, secondInput)
	retentionTestComplete(t, m, 3)
	if payload, err := io.ReadAll(reader); err != nil || len(payload) != 188 {
		t.Fatalf("pressure damaged the pinned output: %d bytes, %v", len(payload), err)
	}
	if _, err := m.Snapshot(first.Spec.Scope, first.ID); err != nil {
		t.Fatalf("pinned completed history was evicted: %v", err)
	}
}

func TestManagerRetentionPressureDoesNotEvictPendingFinalPersistence(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("failure-%t", fail), func(t *testing.T) {
			release := make(chan struct{})
			repository := &progressiveCompletionRepository{managerTestRepository: &managerTestRepository{}, entered: make(chan struct{}), release: release, fail: fail}
			options := retentionTestOptions(t)
			options.MaxJobs, options.MaxRetainedJobs = 1, 1
			options.Repository = repository
			options.run = func(ctx context.Context, _, directory string, _ *os.File, plan Plan, _ int, _ func(Progress)) (RunResult, error) {
				if plan.StartTicks == 1 {
					<-ctx.Done()
					return RunResult{}, ctx.Err()
				}
				return RunResult{}, publishManagerTestOutput(directory, 188)
			}
			m := newTestManager(t, options)
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			first, err := m.Ensure(context.Background(), managerTestSpec(1), managerTestInput(t))
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-repository.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("completion did not reach final persistence")
			}
			rejected := managerTestInput(t)
			rejectedSpec := managerTestSpec(2)
			rejectedSpec.Scope.UserID, rejectedSpec.Scope.AuthSessionID = "other-viewer", "other-auth"
			if _, err := m.Ensure(context.Background(), rejectedSpec, rejected); !errors.Is(err, ErrBusy) {
				t.Fatalf("uncommitted terminal history was evicted: %v", err)
			}
			assertManagerInputClosed(t, rejected)
			releaseOnce.Do(func() { close(release) })
			finished := managerTestWaitFinished(t, m, first.ID)
			if fail && finished.ErrorCode != "persistence" || !fail && finished.State != "completed" {
				t.Fatalf("final persistence result changed: %+v", finished)
			}
			// Avoid a second completion notification from this deliberately
			// one-shot repository while checking the real admission path.
			secondSpec := managerTestSpec(2)
			secondSpec.Plan.StartTicks = 1
			if _, err := m.Ensure(context.Background(), secondSpec, managerTestInput(t)); err != nil {
				t.Fatalf("finished persistence attempt still blocked admission: %v", err)
			}
		})
	}
}

func TestManagerRetentionPressureRemovesFinishedQueueEntries(t *testing.T) {
	options := retentionTestOptions(t)
	options.MaxJobs, options.MaxRetainedJobs = 1, 1
	options.Repository = &managerTestRepository{createErr: errors.New("controlled create failure")}
	m := newTestManager(t, options)
	// Failed creation finishes without scheduling. Stopping maintenance makes
	// pressure reclamation responsible for releasing its queued pointer too.
	m.cancel()
	<-m.loopDone
	for index := 1; index <= 12; index++ {
		input := managerTestInput(t)
		record, err := m.Ensure(context.Background(), managerTestSpec(index), input)
		if !errors.Is(err, ErrPersistence) {
			t.Fatalf("controlled creation returned %v", err)
		}
		finished := managerTestWaitFinished(t, m, record.ID)
		if finished.ErrorCode != "persistence" {
			t.Fatalf("creation failure was replaced by %+v", finished)
		}
		assertManagerInputClosed(t, input)
		m.mu.Lock()
		retained, queued := len(m.jobs), len(m.queue)
		current := queued == 1 && m.queue[0] == m.jobs[record.ID]
		m.mu.Unlock()
		if retained != 1 || queued != 1 || !current {
			t.Fatalf("failed creation left %d retained and %d queued records", retained, queued)
		}
	}
}

func TestManagerRetentionPressureReservesSlotWhileRemovingFiles(t *testing.T) {
	options := retentionTestOptions(t)
	options.MaxJobs, options.MaxRetainedJobs = 1, 1
	m := newTestManager(t, options)
	completed := retentionTestComplete(t, m, 1)
	m.cancel()
	<-m.loopDone
	m.mu.Lock()
	job := m.jobs[completed.ID]
	m.mu.Unlock()
	// Stop inside the real guarded filesystem operation after reclamation
	// marks the victim, without retaining mu or allowing maintenance to race
	// for this barrier. Other manager APIs can still inspect admission state.
	m.cache.mu.Lock()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { m.cache.mu.Unlock() }) }
	t.Cleanup(release)
	result := make(chan error, 1)
	go func() {
		reclaimed, err := m.reclaimForAdmission(context.Background(), managerTestSpec(2))
		if err == nil && !reclaimed {
			err = errors.New("retention pressure did not reclaim the terminal job")
		}
		result <- err
	}()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		reclaiming := job.reclaiming
		reserved := len(m.jobs) == 1 && !m.admissionAvailableLocked(managerTestSpec(2).Scope)
		unpublished := m.bySpec[completed.Spec] == nil
		m.mu.Unlock()
		if reclaiming {
			if !reserved || !unpublished {
				t.Fatal("in-flight cleanup released its retained slot or remained reusable")
			}
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("reclamation did not reach guarded file removal")
		case <-ticker.C:
		}
	}
	opened := make(chan error, 1)
	go func() {
		handle, err := m.Open(context.Background(), completed.Spec.Scope, completed.ID, "segment-0.ts")
		if handle != nil {
			_ = handle.Close()
		}
		opened <- err
	}()
	select {
	case err := <-opened:
		if !errors.Is(err, ErrJobNotFound) {
			t.Fatalf("file removal allowed a new reader to pin its output: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("new reader waited for output that was already being removed")
	}
	release()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("guarded file removal did not finish")
	}
	if m.reclaim(job, true) {
		t.Fatal("a stale maintenance snapshot reclaimed the same output twice")
	}
	m.maintain()
	m.mu.Lock()
	retained, cacheBytes, readers := len(m.jobs), m.bytes, m.readers
	m.mu.Unlock()
	if retained != 0 || cacheBytes != 0 || readers != 0 || !m.Health().Available {
		t.Fatalf("reclamation left %d jobs, %d bytes, %d readers", retained, cacheBytes, readers)
	}
}

func TestManagerRetentionPressureConcurrentAdmissionKeepsCreatingJobs(t *testing.T) {
	options := retentionTestOptions(t)
	m := newTestManager(t, options)
	retentionTestComplete(t, m, 1)
	retentionTestComplete(t, m, 2)
	repository := options.Repository.(*managerTestRepository)
	gate, entered := make(chan struct{}), make(chan struct{}, 12)
	// The completed jobs establish that all prior Create calls have returned.
	repository.createGate, repository.createEntered = gate, entered
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(gate) }) })
	type result struct {
		record Record
		err    error
		input  *os.File
	}
	results := make(chan result, 12)
	for index := 0; index < cap(results); index++ {
		spec := managerTestSpec(index + 10)
		spec.Scope.UserID = fmt.Sprintf("viewer-%d", index)
		spec.Scope.AuthSessionID = fmt.Sprintf("auth-%d", index)
		input := managerTestInput(t)
		go func() {
			record, err := m.Ensure(context.Background(), spec, input)
			results <- result{record: record, err: err, input: input}
		}()
	}
	for range options.MaxRetainedJobs {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("concurrent admission failed to replace reclaimable history")
		}
	}
	for range cap(results) - options.MaxRetainedJobs {
		select {
		case got := <-results:
			if !errors.Is(got.err, ErrBusy) {
				t.Fatalf("creating records failed to hold their retained slots: %v", got.err)
			}
			assertManagerInputClosed(t, got.input)
		case <-time.After(3 * time.Second):
			t.Fatal("excess concurrent admission did not reject at the hard limit")
		}
	}
	m.mu.Lock()
	count, queued := len(m.jobs), len(m.queue)
	m.mu.Unlock()
	if count != options.MaxRetainedJobs || queued != options.MaxRetainedJobs {
		t.Fatalf("concurrent admission retained %d jobs and %d queued entries", count, queued)
	}
	releaseOnce.Do(func() { close(gate) })
	for range options.MaxRetainedJobs {
		select {
		case got := <-results:
			if got.err != nil {
				t.Fatal(got.err)
			}
			managerTestWaitFinished(t, m, got.record.ID)
			assertManagerInputClosed(t, got.input)
		case <-time.After(3 * time.Second):
			t.Fatal("accepted creation did not finish after persistence release")
		}
	}
}

func TestManagerRetentionPressureCacheFailureFencesAdmissionAndCloses(t *testing.T) {
	options := retentionTestOptions(t)
	options.MaxJobs, options.MaxRetainedJobs = 1, 1
	m, err := NewManager(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	var unknown, id string
	t.Cleanup(func() {
		m.cancel()
		<-m.loopDone
		if unknown != "" {
			_ = os.Remove(unknown)
			m.filesMu.Lock()
			_ = m.cache.RemoveJob(id)
			m.filesMu.Unlock()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil && !errors.Is(err, ErrCacheUnsafe) {
			t.Errorf("close failed cache: %v", err)
		}
	})
	completed := retentionTestComplete(t, m, 1)
	id = completed.ID
	// Freeze maintenance so admission itself must discover guarded removal's
	// failure. Manager context cancellation does not authorize new processes.
	m.cancel()
	<-m.loopDone
	unknown = filepath.Join(options.Root, id, "unknown-retention-fixture.txt")
	if err := os.WriteFile(unknown, []byte("preserve unknown content"), 0o600); err != nil {
		t.Fatal(err)
	}
	rejected := managerTestInput(t)
	if _, err := m.Ensure(context.Background(), managerTestSpec(2), rejected); !errors.Is(err, ErrOutputUnavailable) {
		t.Fatalf("guarded cleanup failure admitted a replacement: %v", err)
	}
	assertManagerInputClosed(t, rejected)
	if content, err := os.ReadFile(unknown); err != nil || string(content) != "preserve unknown content" {
		t.Fatalf("guarded cleanup changed unknown content: %q, %v", content, err)
	}
	m.mu.Lock()
	cacheBytes := m.bytes
	m.mu.Unlock()
	if cacheBytes != completed.OutputBytes || m.Health().Available {
		t.Fatalf("failed cleanup lost accounting or health fence: %d bytes", cacheBytes)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := m.Close(ctx); !errors.Is(err, ErrCacheUnsafe) {
		t.Fatalf("failed reclamation did not finish shutdown with its original error: %v", err)
	}
}

// retentionReuseContext pauses evaluation of the reuse select after Ensure
// captures a completed record and releases mu. The record can then be evicted
// through a real competing admission before Ensure resumes its identity check.
type retentionReuseContext struct {
	context.Context
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (c *retentionReuseContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return c.Context.Done()
}

func TestManagerRetentionPressureRetriesReuseAfterConcurrentEviction(t *testing.T) {
	options := retentionTestOptions(t)
	options.MaxJobs, options.MaxRetainedJobs = 1, 1
	m := newTestManager(t, options)
	first := retentionTestComplete(t, m, 1)
	release := make(chan struct{})
	ctx := &retentionReuseContext{Context: context.Background(), entered: make(chan struct{}), release: release}
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	input := managerTestInput(t)
	type result struct {
		record Record
		err    error
	}
	results := make(chan result, 1)
	go func() {
		record, err := m.Ensure(ctx, first.Spec, input)
		results <- result{record: record, err: err}
	}()
	managerAccessWait(t, ctx.entered, "the completed-record reuse barrier")
	if _, err := input.Stat(); err != nil {
		t.Fatalf("reuse lost its input before resolving the retained record: %v", err)
	}
	retentionTestComplete(t, m, 2)
	if _, err := m.Snapshot(first.Spec.Scope, first.ID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("competing admission did not evict the captured history: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case got := <-results:
		if got.err != nil || got.record.ID == first.ID {
			t.Fatalf("reuse exposed eviction instead of retrying admission: %+v, %v", got.record, got.err)
		}
		if completed := managerTestWaitFinished(t, m, got.record.ID); completed.State != "completed" || completed.Spec != first.Spec {
			t.Fatalf("retried reuse changed the requested output: %+v", completed)
		}
		assertManagerInputClosed(t, input)
	case <-time.After(3 * time.Second):
		t.Fatal("reuse did not retry after its captured output was evicted")
	}
}

func TestManagerRetentionPressureRejectsWorkLimitsWithoutWaitingForStorage(t *testing.T) {
	for _, limit := range []string{"global", "user", "credential"} {
		t.Run(limit, func(t *testing.T) {
			started := make(chan int64, 2)
			options := retentionTestOptions(t)
			options.MaxRetainedJobs, options.MaxQueueJobs = 4, 2
			if limit == "global" {
				options.MaxJobs, options.MaxQueueJobs = 1, 1
			}
			options.run = func(ctx context.Context, _, _ string, _ *os.File, plan Plan, _ int, _ func(Progress)) (RunResult, error) {
				started <- plan.StartTicks
				<-ctx.Done()
				return RunResult{}, ctx.Err()
			}
			m := newTestManager(t, options)
			specFor := func(index int) Spec {
				spec := managerTestSpec(index)
				spec.Plan.StartTicks = int64(index)
				if limit != "user" {
					spec.Scope.UserID = fmt.Sprintf("viewer-%d", index)
				}
				if limit != "credential" {
					spec.Scope.AuthSessionID = fmt.Sprintf("auth-%d", index)
				}
				return spec
			}
			for index := 1; index <= 2; index++ {
				if _, err := m.Ensure(context.Background(), specFor(index), managerTestInput(t)); err != nil {
					t.Fatal(err)
				}
				if index == 1 {
					managerTestWaitStart(t, started)
				}
			}
			m.filesMu.Lock()
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { m.filesMu.Unlock() }) }
			t.Cleanup(release)
			rejected := managerTestInput(t)
			result := make(chan error, 1)
			go func() {
				_, err := m.Ensure(context.Background(), specFor(3), rejected)
				result <- err
			}()
			select {
			case err := <-result:
				if !errors.Is(err, ErrBusy) {
					t.Fatalf("exhausted %s allowance returned %v", limit, err)
				}
				assertManagerInputClosed(t, rejected)
			case <-time.After(3 * time.Second):
				t.Fatalf("exhausted %s allowance waited for the blocked filesystem", limit)
			}
			release()
		})
	}
}

// retentionReclamationContext publishes the first cancellation snapshot only
// after capturing it. Cancellation after checked closes cannot change that
// pre-lock result; safe reclamation must observe it again after taking the locks.
type retentionReclamationContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (c *retentionReclamationContext) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.checked) })
	return err
}

func TestManagerRetentionPressureCancellationPreservesHealthyHistory(t *testing.T) {
	options := retentionTestOptions(t)
	options.MaxJobs, options.MaxRetainedJobs = 1, 1
	m := newTestManager(t, options)
	completed := retentionTestComplete(t, m, 1)
	parentCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &retentionReclamationContext{Context: parentCtx, checked: make(chan struct{})}
	m.filesMu.Lock()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { m.filesMu.Unlock() }) }
	t.Cleanup(release)
	type result struct {
		reclaimed bool
		err       error
	}
	results := make(chan result, 1)
	go func() {
		reclaimed, err := m.reclaimForAdmission(ctx, managerTestSpec(2))
		results <- result{reclaimed: reclaimed, err: err}
	}()
	managerAccessWait(t, ctx.checked, "the uncancelled snapshot before the filesystem barrier")
	// No candidate can be selected until storage is available. Cancellation
	// must be rechecked after that wait, before mutating healthy retained output.
	cancel()
	release()
	select {
	case got := <-results:
		if got.reclaimed || !errors.Is(got.err, context.Canceled) {
			t.Fatalf("cancelled pressure reclaimed output: %t, %v", got.reclaimed, got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled pressure did not return after storage became available")
	}
	if snapshot, err := m.Snapshot(completed.Spec.Scope, completed.ID); err != nil || snapshot != completed {
		t.Fatalf("cancelled pressure changed healthy history: %+v, %v", snapshot, err)
	}
	if _, err := os.Stat(filepath.Join(options.Root, completed.ID)); err != nil {
		t.Fatalf("cancelled pressure removed healthy output: %v", err)
	}
	m.mu.Lock()
	cacheBytes, retained := m.bytes, len(m.jobs)
	m.mu.Unlock()
	if cacheBytes != completed.OutputBytes || retained != 1 {
		t.Fatalf("cancelled pressure changed retained accounting: %d bytes, %d records", cacheBytes, retained)
	}
}

//go:build linux

package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestManagerPlaybackCancellationIncludesUnregisteredProducers(t *testing.T) {
	started := make(chan int64, 4)
	o := managerAccessOptions(t, func(ctx context.Context, _, _ string, _ *os.File, plan Plan, _ int, _ func(Progress)) (RunResult, error) {
		started <- plan.StartTicks
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	o.MaxJobs, o.MaxUserJobs, o.MaxSessionJobs = 4, 4, 4
	m := newTestManager(t, o)
	specs := []Spec{managerTestSpec(1), managerTestSpec(1), managerTestSpec(2), managerTestSpec(1)}
	specs[1].Plan.StartTicks = 1
	specs[2].Plan.StartTicks = 2
	specs[3].Plan.StartTicks = 3
	specs[3].Scope.AuthSessionID = "other-auth"
	var records []Record
	var inputs []*os.File
	for _, spec := range specs {
		input := managerTestInput(t)
		record, err := m.Ensure(context.Background(), spec, input)
		if err != nil {
			t.Fatal(err)
		}
		records, inputs = append(records, record), append(inputs, input)
	}
	for range specs {
		managerTestWaitStart(t, started)
	}
	m.CancelPlayback(specs[0].Scope.AuthSessionID, specs[0].Scope.PlaySessionID)
	for index, record := range records {
		_, err := m.Snapshot(specs[index].Scope, record.ID)
		if index < 2 {
			if !errors.Is(err, ErrJobCancelled) {
				t.Fatalf("retired playback producer %d remains accessible: %v", index, err)
			}
			if finished := managerTestWaitFinished(t, m, record.ID); finished.State != "cancelled" {
				t.Fatalf("producer %d finished as %s", index, finished.State)
			}
			assertManagerInputClosed(t, inputs[index])
		} else if err != nil {
			t.Fatalf("unrelated playback %d was cancelled: %v", index, err)
		}
	}
}

func TestManagerPlaybackCancellationReclaimsCompletedOutputAfterReaderClose(t *testing.T) {
	m := newTestManager(t, retentionTestOptions(t))
	record := retentionTestComplete(t, m, 1)
	handle, err := m.Open(context.Background(), record.Spec.Scope, record.ID, "segment-0.ts")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	m.CancelPlayback(record.Spec.Scope.AuthSessionID, record.Spec.Scope.PlaySessionID)
	if _, err := m.TryOpen(record.Spec.Scope, record.ID, "main.m3u8"); !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("completed output remains accessible: %v", err)
	}
	path := filepath.Join(m.options.Root, record.ID)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cancellation removed pinned output: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("retired playback output was not reclaimed after reader release")
		case <-ticker.C:
		}
	}
}

func TestManagerPlaybackCancellationIsolatesApplicationKeyClients(t *testing.T) {
	started := make(chan int64, 2)
	o := managerAccessOptions(t, func(ctx context.Context, _, dir string, _ *os.File, plan Plan, _ int, _ func(Progress)) (RunResult, error) {
		if err := publishManagerTestOutput(dir, 188); err != nil {
			return RunResult{}, err
		}
		started <- plan.StartTicks
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	o.MaxJobs, o.MaxSessionJobs = 2, 2
	m := newTestManager(t, o)
	first, second := applicationManagerSpec(1, "shared-application-key"), applicationManagerSpec(2, "shared-application-key")
	first.Scope.ApplicationClientID, first.Scope.DeviceID, first.Scope.PlaySessionID = "first-real-client", "first-device", "first-canonical-play"
	second.Scope.ApplicationClientID, second.Scope.DeviceID, second.Scope.PlaySessionID = "second-real-client", "second-device", "second-canonical-play"
	specs := []Spec{first, second}
	var records [2]Record
	var inputs [2]*os.File
	var handles [2]*ReadHandle
	for index, spec := range specs {
		if !validScope(spec.Scope) || !spec.Scope.ApplicationKey || spec.Scope.UserID != "" {
			t.Fatalf("test requires a valid userless application scope: %+v", spec.Scope)
		}
		inputs[index] = managerTestInput(t)
		record, err := m.Ensure(context.Background(), spec, inputs[index])
		if err != nil {
			t.Fatalf("admit application client %d: %v", index, err)
		}
		records[index] = record
		if record.Spec.Scope != spec.Scope {
			t.Fatalf("application scope changed during admission: %+v", record.Spec.Scope)
		}
	}
	for range specs {
		managerTestWaitStart(t, started)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for index, spec := range specs {
		handle, err := m.Open(ctx, spec.Scope, records[index].ID, "segment-0.ts")
		if err != nil {
			t.Fatalf("open application client %d: %v", index, err)
		}
		handles[index] = handle
		t.Cleanup(func() { _ = handle.Close() })
		if foreign, err := m.TryOpen(specs[1-index].Scope, records[index].ID, "segment-0.ts"); foreign != nil || !errors.Is(err, ErrJobNotFound) {
			if foreign != nil {
				_ = foreign.Close()
			}
			t.Fatalf("shared credential exposed client %d output to its sibling: %v", index, err)
		}
	}
	managerAccessAssertReaders(t, m, records[0].ID, 1, 2)
	m.CancelPlayback(first.Scope.AuthSessionID, first.Scope.PlaySessionID)
	if _, err := m.Snapshot(first.Scope, records[0].ID); !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("cancelled application producer remains accessible: %v", err)
	}
	if _, err := m.TryOpen(first.Scope, records[0].ID, "main.m3u8"); !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("cancelled application output admitted a new reader: %v", err)
	}
	if finished := managerTestWaitFinished(t, m, records[0].ID); finished.State != "cancelled" {
		t.Fatalf("application producer cancellation finished as %s", finished.State)
	}
	assertManagerInputClosed(t, inputs[0])
	firstPath, secondPath := filepath.Join(o.Root, records[0].ID), filepath.Join(o.Root, records[1].ID)
	if _, err := os.Stat(firstPath); err != nil {
		t.Fatalf("application cancellation removed output with a pinned reader: %v", err)
	}
	for index, handle := range handles {
		payload, err := io.ReadAll(handle)
		if err != nil || len(payload) != 188 {
			t.Fatalf("pinned application reader %d lost its output: %d bytes, %v", index, len(payload), err)
		}
	}
	if err := handles[0].Close(); err != nil {
		t.Fatal(err)
	}
	managerAccessAssertReaders(t, m, records[0].ID, 0, 1)
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(firstPath); errors.Is(err, os.ErrNotExist) {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("cancelled application cache survived its final reader")
		case <-ticker.C:
		}
	}
	if snapshot, err := m.Snapshot(second.Scope, records[1].ID); err != nil || snapshot.State != "running" {
		t.Fatalf("cancelling one application playback stopped its sibling: %+v, %v", snapshot, err)
	}
	if _, err := inputs[1].Stat(); err != nil {
		t.Fatalf("sibling application's source descriptor was released: %v", err)
	}
	if _, err := os.Stat(secondPath); err != nil {
		t.Fatalf("sibling application's cache was reclaimed: %v", err)
	}
	duplicateInput := managerTestInput(t)
	if reused, err := m.Ensure(context.Background(), second, duplicateInput); err != nil || reused.ID != records[1].ID {
		t.Fatalf("sibling producer stopped being reusable: %+v, %v", reused, err)
	}
	assertManagerInputClosed(t, duplicateInput)
	newReader, err := m.TryOpen(second.Scope, records[1].ID, "segment-0.ts")
	if err != nil {
		t.Fatalf("sibling playback cannot open another artifact: %v", err)
	}
	if err := newReader.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagerReaderWakeDoesNotWaitForCacheIO(t *testing.T) {
	m := newTestManager(t, retentionTestOptions(t))
	retentionTestComplete(t, m, 1)
	m.cancel()
	<-m.loopDone
	// A warmed reader release needs only in-memory leases and reclaim checks.
	// An unrelated long filesystem operation must not block this maintenance.
	m.cache.mu.Lock()
	done := make(chan struct{})
	go func() {
		m.maintainJobs(false)
		close(done)
	}()
	select {
	case <-done:
		m.cache.mu.Unlock()
	case <-time.After(3 * time.Second):
		m.cache.mu.Unlock()
		<-done
		t.Fatal("reader wake initiated a full cache scan")
	}
}

func TestManagerWakeDoesNotApplyTimeoutsBeforePeriodicScan(t *testing.T) {
	started := make(chan int64, 1)
	o := managerAccessOptions(t, func(ctx context.Context, _, _ string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		started <- 1
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	m := newTestManager(t, o)
	spec := managerTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	managerTestWaitStart(t, started)
	// Hold the filesystem barrier so a concurrent periodic scan cannot use
	// these deliberately old timestamps before the wake path is checked.
	m.filesMu.Lock()
	defer m.filesMu.Unlock()
	m.mu.Lock()
	j := m.jobs[record.ID]
	j.started, j.lastProgress = time.Now().UTC().Add(-2*time.Hour), time.Now().UTC().Add(-2*time.Hour)
	m.mu.Unlock()
	m.maintainJobs(false)
	if _, err := m.Snapshot(spec.Scope, record.ID); err != nil {
		t.Fatalf("wake classified a timeout using stale output readiness: %v", err)
	}
}

func TestProgressiveObserverDiscardedOutputDoesNotSync(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "discarded-output-")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	close(done)
	observer := &progressiveObserver{file: file, stop: make(chan struct{}), done: done}
	if err := observer.finish(false); err != nil {
		t.Fatalf("discarded output attempted a flush: %v", err)
	}
}

func TestManagerPayloadReadyWakeDoesNotWaitForPeriodicScan(t *testing.T) {
	producer := newProgressiveTestProducer([]byte("playable output"))
	o := managerAccessOptions(t, producer.run)
	o.pollInterval = time.Second
	m := newTestManager(t, o)
	spec := progressiveTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	managerAccessWait(t, producer.started, "progressive media payload")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := m.WaitReady(ctx, spec.Scope, record.ID); err != nil {
		t.Fatalf("payload readiness waited for the one-second scan tick: %v", err)
	}
}

func TestManagerQueueStartsBeforePreviousFinalPersistence(t *testing.T) {
	finish, release := make(chan struct{}), make(chan struct{})
	started := make(chan int64, 2)
	repository := &progressiveCompletionRepository{managerTestRepository: &managerTestRepository{}, entered: make(chan struct{}), release: release}
	o := managerAccessOptions(t, func(ctx context.Context, _, dir string, _ *os.File, plan Plan, _ int, _ func(Progress)) (RunResult, error) {
		started <- plan.StartTicks
		if plan.StartTicks != 0 {
			<-ctx.Done()
			return RunResult{}, ctx.Err()
		}
		select {
		case <-ctx.Done():
			return RunResult{}, ctx.Err()
		case <-finish:
			return RunResult{}, publishManagerTestOutput(dir, 188)
		}
	})
	o.MaxJobs, o.MaxUserJobs, o.MaxSessionJobs = 1, 1, 1
	o.pollInterval, o.Repository = time.Second, repository
	m := newTestManager(t, o)
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	first, err := m.Ensure(context.Background(), managerTestSpec(1), managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	managerTestWaitStart(t, started)
	secondSpec := managerTestSpec(2)
	secondSpec.Plan.StartTicks = 1
	if _, err := m.Ensure(context.Background(), secondSpec, managerTestInput(t)); err != nil {
		t.Fatal(err)
	}
	close(finish)
	managerAccessWait(t, repository.entered, "blocked final persistence")
	select {
	case <-started:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("released conversion slot waited for final persistence or a scan tick")
	}
	m.mu.Lock()
	job := m.jobs[first.ID]
	finished := job.finished
	m.mu.Unlock()
	if finished || m.reclaim(job, true) {
		t.Fatal("pending final persistence became reclaimable")
	}
	releaseOnce.Do(func() { close(release) })
	managerTestWaitFinished(t, m, first.ID)
}

func TestManagerPeriodicQuotaScanSurvivesContinuousWakes(t *testing.T) {
	started := make(chan int64, 1)
	o := managerAccessOptions(t, func(ctx context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		if err := publishManagerTestOutput(dir, 188); err != nil {
			return RunResult{}, err
		}
		started <- 1
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	o.MaxJobBytes, o.pollInterval = 300, 20*time.Millisecond
	m := newTestManager(t, o)
	spec := managerTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	managerTestWaitStart(t, started)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := m.WaitReady(ctx, spec.Scope, record.ID); err != nil {
		t.Fatal(err)
	}
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			default:
				m.signal()
			}
		}
	}()
	defer func() { close(stop); <-stopped }()
	if err := os.WriteFile(filepath.Join(m.options.Root, record.ID, "segment-1.ts"), make([]byte, 512), 0o600); err != nil {
		t.Fatal(err)
	}
	if finished := managerTestWaitFinished(t, m, record.ID); finished.ErrorCode != "job_quota" {
		t.Fatalf("periodic quota scan lost under wake pressure: %s", finished.ErrorCode)
	}
}

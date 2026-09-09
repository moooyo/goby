//go:build linux

package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

type managerAccessOpenResult struct {
	handle *ReadHandle
	err    error
}

func managerAccessOptions(t *testing.T, run runnerFunc) Options {
	t.Helper()
	o := managerTestOptions(t, run)
	o.IdleTimeout, o.StartupTimeout = time.Hour, time.Hour
	o.NoProgressTimeout, o.MaxRuntime = time.Hour, time.Hour
	return o
}

func managerAccessWait(t *testing.T, event <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func managerAccessStartOpen(t *testing.T, m *Manager, scope Scope, id, name string) <-chan managerAccessOpenResult {
	t.Helper()
	result := make(chan managerAccessOpenResult)
	abandoned := make(chan struct{})
	t.Cleanup(func() { close(abandoned) })
	go func() {
		handle, err := m.TryOpen(scope, id, name)
		select {
		case result <- managerAccessOpenResult{handle: handle, err: err}:
		case <-abandoned:
			if handle != nil {
				_ = handle.Close()
			}
		}
	}()
	return result
}

func managerAccessReceiveOpen(t *testing.T, result <-chan managerAccessOpenResult) (*ReadHandle, error) {
	t.Helper()
	select {
	case received := <-result:
		if received.handle != nil {
			t.Cleanup(func() { _ = received.handle.Close() })
		}
		return received.handle, received.err
	case <-time.After(3 * time.Second):
		t.Fatal("TryOpen waited for output publication")
		return nil, nil
	}
}

func managerAccessTryOpen(t *testing.T, m *Manager, scope Scope, id, name string) (*ReadHandle, error) {
	t.Helper()
	return managerAccessReceiveOpen(t, managerAccessStartOpen(t, m, scope, id, name))
}

func managerAccessCancelJob(t *testing.T, m *Manager, scope Scope, id string) error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- m.CancelJob(id, scope) }()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("CancelJob waited for the runner to finish")
		return nil
	}
}

func managerAccessAssertReaders(t *testing.T, m *Manager, id string, wantJob, wantTotal int) {
	t.Helper()
	m.mu.Lock()
	j, total := m.jobs[id], m.readers
	jobReaders := -1
	if j != nil {
		jobReaders = j.readers
	}
	m.mu.Unlock()
	if jobReaders != wantJob || total != wantTotal {
		t.Fatalf("reader reservations = job %d, total %d; want job %d, total %d", jobReaders, total, wantJob, wantTotal)
	}
}

func managerAccessWaitReaders(t *testing.T, m *Manager, id string, expected int) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		j := m.jobs[id]
		ready := j != nil && j.readers == expected
		m.mu.Unlock()
		if ready {
			return
		}
		select {
		case <-timer.C:
			t.Fatal("TryOpen did not reserve its reader before opening the file")
		case <-ticker.C:
		}
	}
}

func TestManagerTryOpenDoesNotWaitForOutput(t *testing.T) {
	started, publish, published := make(chan struct{}), make(chan struct{}), make(chan struct{})
	publishFuture, futurePublished := make(chan struct{}), make(chan struct{})
	run := func(ctx context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		if err := os.WriteFile(filepath.Join(dir, "segment-0.ts.tmp"), []byte("unpublished segment"), 0o600); err != nil {
			return RunResult{}, err
		}
		close(started)
		select {
		case <-ctx.Done():
			return RunResult{}, ctx.Err()
		case <-publish:
		}
		if err := publishManagerTestOutput(dir, 188); err != nil {
			return RunResult{}, err
		}
		close(published)
		select {
		case <-ctx.Done():
			return RunResult{}, ctx.Err()
		case <-publishFuture:
		}
		if err := os.WriteFile(filepath.Join(dir, "segment-1.ts.tmp"), []byte("future segment"), 0o600); err != nil {
			return RunResult{}, err
		}
		if err := os.Rename(filepath.Join(dir, "segment-1.ts.tmp"), filepath.Join(dir, "segment-1.ts")); err != nil {
			return RunResult{}, err
		}
		close(futurePublished)
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}
	o := managerAccessOptions(t, run)
	o.MaxReaders, o.MaxJobReaders = 1, 1
	m := newTestManager(t, o)
	spec := managerTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	managerAccessWait(t, started, "the unpublished output")
	oldAccess := time.Now().UTC().Add(-time.Minute)
	m.mu.Lock()
	m.jobs[record.ID].record.LastAccessAt = oldAccess
	m.mu.Unlock()
	if handle, err := managerAccessTryOpen(t, m, spec.Scope, record.ID, "main.m3u8"); handle != nil || !errors.Is(err, ErrOutputUnavailable) {
		t.Fatalf("unready job returned a reader: %v", err)
	}
	snapshot, err := m.Snapshot(spec.Scope, record.ID)
	if err != nil || !snapshot.LastAccessAt.After(oldAccess) {
		t.Fatalf("unready access did not refresh the idle deadline: %v", err)
	}
	managerAccessAssertReaders(t, m, record.ID, 0, 0)
	for _, name := range []string{"../main.m3u8", "main.m3u8.tmp", "segment-0.ts.tmp", "segment-list.m3u8", "segment-list.m3u8.tmp", "main.m3u8.publish.tmp", "segment-.ts", "segment-x.ts", "other.ts", "segment-0.ts/../main.m3u8"} {
		if handle, err := managerAccessTryOpen(t, m, spec.Scope, record.ID, name); handle != nil || !errors.Is(err, ErrOutputUnavailable) {
			t.Fatalf("unpublished or unrecognized name %q was accepted: %v", name, err)
		}
	}
	close(publish)
	managerAccessWait(t, published, "the initial playlist")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := m.WaitReady(ctx, spec.Scope, record.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if handle, err := managerAccessTryOpen(t, m, spec.Scope, record.ID, "segment-1.ts"); handle != nil || !errors.Is(err, ErrOutputUnavailable) {
			t.Fatalf("future segment waited for its publication: %v", err)
		}
		managerAccessAssertReaders(t, m, record.ID, 0, 0)
	}
	close(publishFuture)
	managerAccessWait(t, futurePublished, "the future segment")
	handle, err := managerAccessTryOpen(t, m, spec.Scope, record.ID, "segment-1.ts")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(handle)
	if err != nil || string(body) != "future segment" {
		t.Fatalf("published future segment = %q, %v", body, err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	managerAccessAssertReaders(t, m, record.ID, 0, 0)
}

func TestManagerAccessRequiresEveryScopeDimension(t *testing.T) {
	run := func(_ context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		return RunResult{}, publishManagerTestOutput(dir, 188)
	}
	m := newTestManager(t, managerAccessOptions(t, run))
	spec := managerTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	managerTestWaitFinished(t, m, record.ID)
	for _, test := range []struct {
		name   string
		change func(*Scope)
	}{
		{"user", func(scope *Scope) { scope.UserID = "other-user" }},
		{"authentication session", func(scope *Scope) { scope.AuthSessionID = "other-auth" }},
		{"device", func(scope *Scope) { scope.DeviceID = "other-device" }},
		{"play session", func(scope *Scope) { scope.PlaySessionID = "other-play" }},
		{"item", func(scope *Scope) { scope.ItemID = "other-item" }},
		{"source", func(scope *Scope) { scope.SourceID = "other-source" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			foreign := spec.Scope
			test.change(&foreign)
			if snapshot, err := m.Snapshot(foreign, record.ID); snapshot != (Record{}) || !errors.Is(err, ErrJobNotFound) {
				t.Fatalf("foreign snapshot exposed the job: %+v, %v", snapshot, err)
			}
			if handle, err := managerAccessTryOpen(t, m, foreign, record.ID, "segment-0.ts"); handle != nil || !errors.Is(err, ErrJobNotFound) {
				t.Fatalf("foreign output access was accepted: %v", err)
			}
			if err := m.CancelJob(record.ID, foreign); !errors.Is(err, ErrJobNotFound) {
				t.Fatalf("foreign cancellation was accepted: %v", err)
			}
		})
	}
	if snapshot, err := m.Snapshot(spec.Scope, "missing"); snapshot != (Record{}) || !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("missing snapshot = %+v, %v", snapshot, err)
	}
	if handle, err := managerAccessTryOpen(t, m, spec.Scope, "missing", "main.m3u8"); handle != nil || !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("missing output = %v", err)
	}
	if err := m.CancelJob("missing", spec.Scope); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("missing cancellation = %v", err)
	}
	if snapshot, err := m.Snapshot(spec.Scope, record.ID); err != nil || snapshot.State != "completed" {
		t.Fatalf("foreign cancellation changed the original job: %+v, %v", snapshot, err)
	}
	handle, err := managerAccessTryOpen(t, m, spec.Scope, record.ID, "segment-0.ts")
	if err != nil {
		t.Fatal(err)
	}
	if body, err := io.ReadAll(handle); err != nil || len(body) != 188 {
		t.Fatalf("authorized output was changed: %v", err)
	}
}

func TestManagerSnapshotObservesStateWithoutRefreshingAccess(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	run := func(ctx context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		close(started)
		select {
		case <-ctx.Done():
			return RunResult{}, ctx.Err()
		case <-finish:
			return RunResult{}, publishManagerTestOutput(dir, 188)
		}
	}
	m := newTestManager(t, managerAccessOptions(t, run))
	spec := managerTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	managerAccessWait(t, started, "the running job")
	queuedSpec := managerTestSpec(2)
	queued, err := m.Ensure(context.Background(), queuedSpec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		record Record
		state  string
	}{{record, "running"}, {queued, "queued"}} {
		marker := time.Now().UTC().Add(-time.Minute)
		m.mu.Lock()
		m.jobs[test.record.ID].record.LastAccessAt = marker
		m.mu.Unlock()
		for i := 0; i < 4; i++ {
			snapshot, err := m.Snapshot(test.record.Spec.Scope, test.record.ID)
			if err != nil || snapshot.State != test.state || !snapshot.LastAccessAt.Equal(marker) {
				t.Fatalf("%s snapshot changed access time or state: %+v, %v", test.state, snapshot, err)
			}
			snapshot.Spec.Scope.UserID = "modified-copy"
			snapshot.State = "modified-copy"
		}
	}
	if err := m.CancelJob(queued.ID, queuedSpec.Scope); err != nil {
		t.Fatal(err)
	}
	if final := managerTestWaitFinished(t, m, queued.ID); final.State != "cancelled" {
		t.Fatalf("queued cancellation state = %q", final.State)
	}
	close(finish)
	managerTestWaitFinished(t, m, record.ID)
	before, err := m.Snapshot(spec.Scope, record.ID)
	if err != nil || before.State != "completed" {
		t.Fatalf("completed snapshot = %+v, %v", before, err)
	}
	after, err := m.Snapshot(spec.Scope, record.ID)
	if err != nil || after != before {
		t.Fatalf("repeated completed snapshot mutated the record: %+v, %v", after, err)
	}
}

func TestManagerSnapshotReportsFailureAndAvailability(t *testing.T) {
	run := func(context.Context, string, string, *os.File, Plan, int, func(Progress)) (RunResult, error) {
		return RunResult{}, errors.New("private runner failure")
	}
	m := newTestManager(t, managerAccessOptions(t, run))
	spec := managerTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	managerTestWaitFinished(t, m, record.ID)
	if snapshot, err := m.Snapshot(spec.Scope, record.ID); !errors.Is(err, ErrJobFailed) || snapshot.State != "failed" || snapshot.ErrorCode != "process_failed" {
		t.Fatalf("failed snapshot lost the durable projection: %+v, %v", snapshot, err)
	}
	m.mu.Lock()
	m.cacheFailed = true
	m.mu.Unlock()
	if snapshot, err := m.Snapshot(spec.Scope, record.ID); snapshot != (Record{}) || !errors.Is(err, ErrOutputUnavailable) {
		t.Fatalf("failed cache returned a snapshot: %+v, %v", snapshot, err)
	}
	if handle, err := managerAccessTryOpen(t, m, spec.Scope, record.ID, "main.m3u8"); handle != nil || !errors.Is(err, ErrOutputUnavailable) {
		t.Fatalf("failed cache admitted a reader: %v", err)
	}
	m.mu.Lock()
	m.cacheFailed = false
	m.mu.Unlock()
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := m.Snapshot(spec.Scope, record.ID); snapshot != (Record{}) || !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("closed manager returned a snapshot: %+v, %v", snapshot, err)
	}
	if handle, err := managerAccessTryOpen(t, m, spec.Scope, record.ID, "main.m3u8"); handle != nil || !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("closed manager admitted a reader: %v", err)
	}
}

func TestManagerCancelJobStopsOnlyTheRequestedPlan(t *testing.T) {
	started := make(chan int64, 2)
	oldCancelled, releaseOld, finishNew := make(chan struct{}), make(chan struct{}), make(chan struct{})
	newCancelled := make(chan struct{})
	run := func(ctx context.Context, _, dir string, _ *os.File, plan Plan, _ int, _ func(Progress)) (RunResult, error) {
		started <- plan.StartTicks
		if plan.StartTicks == 0 {
			<-ctx.Done()
			close(oldCancelled)
			<-releaseOld
			return RunResult{}, ctx.Err()
		}
		select {
		case <-ctx.Done():
			close(newCancelled)
			return RunResult{}, ctx.Err()
		case <-finishNew:
			return RunResult{}, publishManagerTestOutput(dir, 188)
		}
	}
	o := managerAccessOptions(t, run)
	o.MaxUserJobs, o.MaxSessionJobs = 2, 2
	m := newTestManager(t, o)
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseOld) }) })
	oldSpec := managerTestSpec(1)
	oldInput := managerTestInput(t)
	old, err := m.Ensure(context.Background(), oldSpec, oldInput)
	if err != nil {
		t.Fatal(err)
	}
	if start := managerTestWaitStart(t, started); start != 0 {
		t.Fatalf("old plan start = %d", start)
	}
	newSpec := oldSpec
	newSpec.Plan.StartTicks = ticksPerSecond
	newInput := managerTestInput(t)
	newRecord, err := m.Ensure(context.Background(), newSpec, newInput)
	if err != nil || newRecord.ID == old.ID {
		t.Fatalf("new plan was not isolated from the old plan: %v", err)
	}
	if start := managerTestWaitStart(t, started); start != ticksPerSecond {
		t.Fatalf("new plan start = %d", start)
	}
	if err := managerAccessCancelJob(t, m, oldSpec.Scope, old.ID); err != nil {
		t.Fatal(err)
	}
	managerAccessWait(t, oldCancelled, "the old runner cancellation")
	if snapshot, err := m.Snapshot(oldSpec.Scope, old.ID); !errors.Is(err, ErrJobCancelled) || snapshot.State != "running" {
		t.Fatalf("pending cancellation lost its current record: %+v, %v", snapshot, err)
	}
	if handle, err := managerAccessTryOpen(t, m, oldSpec.Scope, old.ID, "main.m3u8"); handle != nil || !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("pending cancellation allowed output access: %v", err)
	}
	if err := managerAccessCancelJob(t, m, oldSpec.Scope, old.ID); err != nil {
		t.Fatalf("repeated cancellation failed: %v", err)
	}
	if snapshot, err := m.Snapshot(newSpec.Scope, newRecord.ID); err != nil || snapshot.State != "running" {
		t.Fatalf("old cancellation affected the replacement plan: %+v, %v", snapshot, err)
	}
	select {
	case <-newCancelled:
		t.Fatal("the replacement runner was cancelled")
	default:
	}
	releaseOnce.Do(func() { close(releaseOld) })
	if final := managerTestWaitFinished(t, m, old.ID); final.State != "cancelled" {
		t.Fatalf("old plan final state = %q", final.State)
	}
	assertManagerInputClosed(t, oldInput)
	close(finishNew)
	if final := managerTestWaitFinished(t, m, newRecord.ID); final.State != "completed" {
		t.Fatalf("replacement plan final state = %q", final.State)
	}
	assertManagerInputClosed(t, newInput)
	if _, err := managerAccessTryOpen(t, m, newSpec.Scope, newRecord.ID, "segment-0.ts"); err != nil {
		t.Fatalf("replacement output was revoked: %v", err)
	}
}

func TestManagerCancelJobPreservesCompletedHistoryAndExistingReader(t *testing.T) {
	run := func(_ context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		return RunResult{}, publishManagerTestOutput(dir, 188)
	}
	o := managerAccessOptions(t, run)
	repository := o.Repository.(*managerTestRepository)
	m := newTestManager(t, o)
	spec := managerTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	managerTestWaitFinished(t, m, record.ID)
	repository.mu.Lock()
	durable, updates := repository.records[record.ID], len(repository.updates[record.ID])
	repository.mu.Unlock()
	handle, err := managerAccessTryOpen(t, m, spec.Scope, record.ID, "segment-0.ts")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CancelJob(record.ID, spec.Scope); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := m.Snapshot(spec.Scope, record.ID); !errors.Is(err, ErrJobCancelled) || snapshot.State != "completed" || snapshot.ErrorCode != "" {
		t.Fatalf("completed cancellation rewrote history: %+v, %v", snapshot, err)
	}
	if other, err := managerAccessTryOpen(t, m, spec.Scope, record.ID, "main.m3u8"); other != nil || !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("completed cancellation did not revoke new readers: %v", err)
	}
	closeCtx, cancelClose := context.WithCancel(context.Background())
	cancelClose()
	if err := m.Close(closeCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown did not retain the active reader: %v", err)
	}
	managerAccessWait(t, m.loopDone, "the maintenance loop shutdown")
	if _, err := os.Stat(filepath.Join(o.Root, record.ID, "segment-0.ts")); err != nil {
		t.Fatalf("cache was reclaimed while a reader was active: %v", err)
	}
	if body, err := io.ReadAll(handle); err != nil || len(body) != 188 {
		t.Fatalf("the opened reader was invalidated: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(o.Root, record.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("released reader left its cache behind: %v", err)
	}
	repository.mu.Lock()
	current, currentUpdates := repository.records[record.ID], len(repository.updates[record.ID])
	repository.mu.Unlock()
	if current != durable || currentUpdates != updates || current.State != "completed" {
		t.Fatalf("cache cancellation changed durable history: %+v, %d updates", current, currentUpdates)
	}
}

func TestManagerTryOpenEnforcesAndReleasesReaderLimits(t *testing.T) {
	run := func(_ context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		return RunResult{}, publishManagerTestOutput(dir, 188)
	}
	o := managerAccessOptions(t, run)
	o.MaxReaders, o.MaxJobReaders = 2, 1
	m := newTestManager(t, o)
	var records []Record
	for i := 1; i <= 3; i++ {
		record, err := m.Ensure(context.Background(), managerTestSpec(i), managerTestInput(t))
		if err != nil {
			t.Fatal(err)
		}
		managerTestWaitFinished(t, m, record.ID)
		records = append(records, record)
	}
	first, err := managerAccessTryOpen(t, m, records[0].Spec.Scope, records[0].ID, "segment-0.ts")
	if err != nil {
		t.Fatal(err)
	}
	if handle, err := managerAccessTryOpen(t, m, records[0].Spec.Scope, records[0].ID, "main.m3u8"); handle != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("per-job reader limit was bypassed: %v", err)
	}
	second, err := managerAccessTryOpen(t, m, records[1].Spec.Scope, records[1].ID, "segment-0.ts")
	if err != nil {
		t.Fatal(err)
	}
	if handle, err := managerAccessTryOpen(t, m, records[2].Spec.Scope, records[2].ID, "main.m3u8"); handle != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("global reader limit was bypassed: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	managerAccessAssertReaders(t, m, records[0].ID, 0, 1)
	for i := 0; i < 4; i++ {
		if handle, err := managerAccessTryOpen(t, m, records[0].Spec.Scope, records[0].ID, "segment-999.ts"); handle != nil || !errors.Is(err, ErrOutputUnavailable) {
			t.Fatalf("missing output was accepted: %v", err)
		}
		managerAccessAssertReaders(t, m, records[0].ID, 0, 1)
	}
	third, err := managerAccessTryOpen(t, m, records[2].Spec.Scope, records[2].ID, "main.m3u8")
	if err != nil {
		t.Fatalf("failed opens leaked the released reader permit: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
	managerAccessAssertReaders(t, m, records[2].ID, 0, 0)
}

func TestManagerTryOpenRechecksCancellationAndCloseAfterFileOpen(t *testing.T) {
	for _, operation := range []string{"cancel", "close"} {
		t.Run(operation, func(t *testing.T) {
			run := func(_ context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
				return RunResult{}, publishManagerTestOutput(dir, 188)
			}
			m := newTestManager(t, managerAccessOptions(t, run))
			spec := managerTestSpec(1)
			record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
			if err != nil {
				t.Fatal(err)
			}
			managerTestWaitFinished(t, m, record.ID)
			m.filesMu.Lock()
			var unlockOnce sync.Once
			unlock := func() { unlockOnce.Do(m.filesMu.Unlock) }
			defer unlock()
			result := managerAccessStartOpen(t, m, spec.Scope, record.ID, "segment-0.ts")
			managerAccessWaitReaders(t, m, record.ID, 1)
			want := ErrJobCancelled
			if operation == "cancel" {
				if err := managerAccessCancelJob(t, m, spec.Scope, record.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				want = ErrManagerClosed
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if err := m.Close(ctx); !errors.Is(err, context.Canceled) {
					t.Fatalf("close initiation = %v", err)
				}
			}
			unlock()
			if handle, err := managerAccessReceiveOpen(t, result); handle != nil || !errors.Is(err, want) {
				t.Fatalf("%s after reader reservation was ignored: %v", operation, err)
			}
			m.mu.Lock()
			readers := m.readers
			m.mu.Unlock()
			if readers != 0 {
				t.Fatalf("%s barrier leaked %d reader reservations", operation, readers)
			}
		})
	}
}

func TestManagerTryOpenRejectsUnsafeOrInvalidPublishedFiles(t *testing.T) {
	for _, test := range []struct {
		name   string
		create func(string, string) error
	}{
		{"empty", func(path, _ string) error { return os.WriteFile(path, nil, 0o600) }},
		{"oversized", func(path, _ string) error { return os.WriteFile(path, make([]byte, (1<<20)+1), 0o600) }},
		{"symbolic link", func(path, source string) error { return os.Symlink(source, path) }},
		{"hard link", func(path, source string) error { return os.Link(source, path) }},
		{"directory", func(path, _ string) error { return os.Mkdir(path, 0o700) }},
		{"fifo", func(path, _ string) error { return syscall.Mkfifo(path, 0o600) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := func(_ context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
				return RunResult{}, publishManagerTestOutput(dir, 188)
			}
			o := managerAccessOptions(t, run)
			m := newTestManager(t, o)
			spec := managerTestSpec(1)
			record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
			if err != nil {
				t.Fatal(err)
			}
			managerTestWaitFinished(t, m, record.ID)
			// Keep the maintenance scan from classifying deliberately unsafe
			// content before the synchronous output access checks it.
			m.cancel()
			managerAccessWait(t, m.loopDone, "the paused maintenance loop")
			path := filepath.Join(o.Root, record.ID, "segment-1.ts")
			source := filepath.Join(o.Root, record.ID, "segment-0.ts")
			if err := test.create(path, source); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Errorf("remove deliberately invalid test output: %v", err)
				}
			})
			if handle, err := managerAccessTryOpen(t, m, spec.Scope, record.ID, "segment-1.ts"); handle != nil || !errors.Is(err, ErrOutputUnavailable) {
				t.Fatalf("%s output was exposed: %v", test.name, err)
			}
			managerAccessAssertReaders(t, m, record.ID, 0, 0)
		})
	}
}

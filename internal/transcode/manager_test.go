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
	"sync/atomic"
	"testing"
	"time"
)

type managerTestRepository struct {
	mu            sync.Mutex
	records       map[string]Record
	updates       map[string][]string
	createGate    <-chan struct{}
	createEntered chan struct{}
	createErr     error
	updateErr     error
	recovered     bool
}

func (r *managerTestRepository) Recover(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recovered = true
	return nil
}

func (r *managerTestRepository) Create(ctx context.Context, record Record) error {
	if r.createEntered != nil {
		select {
		case r.createEntered <- struct{}{}:
		default:
		}
	}
	if r.createGate != nil {
		select {
		case <-r.createGate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	if record.State != "queued" {
		return errors.New("creation is not queued")
	}
	if r.records == nil {
		r.records = make(map[string]Record)
		r.updates = make(map[string][]string)
	}
	if _, exists := r.records[record.ID]; exists {
		return errors.New("duplicate durable ID")
	}
	r.records[record.ID] = record
	r.updates[record.ID] = []string{record.State}
	return nil
}

func (r *managerTestRepository) Update(ctx context.Context, record Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return r.updateErr
	}
	previous, exists := r.records[record.ID]
	if !exists || previous.Spec != record.Spec || previous.CreatedAt != record.CreatedAt {
		return errors.New("missing or changed durable identity")
	}
	if previous.State == "completed" || previous.State == "failed" || previous.State == "cancelled" {
		if previous.State != record.State {
			return errors.New("terminal state changed")
		}
	}
	r.records[record.ID] = record
	r.updates[record.ID] = append(r.updates[record.ID], record.State)
	return nil
}

func managerTestPlan() Plan {
	return Plan{Container: "ts", VideoCodec: "copy", AudioCodec: "copy", VideoStreamIndex: 0, AudioStreamIndex: 1,
		DurationTicks: 600 * ticksPerSecond, SegmentSeconds: 3}
}

func managerTestSpec(index int) Spec {
	return Spec{Scope: Scope{UserID: "viewer", AuthSessionID: "auth", DeviceID: "device", PlaySessionID: fmt.Sprintf("play-%d", index), ItemID: "movie", SourceID: "source"},
		SourceStamp: "indexed-source-stamp", Plan: managerTestPlan()}
}

func managerTestOptions(t *testing.T, run runnerFunc) Options {
	t.Helper()
	return Options{Root: filepath.Join(t.TempDir(), "cache"), FFmpegPath: "/not-executed/ffmpeg", Repository: &managerTestRepository{},
		MaxJobs: 2, MaxUserJobs: 1, MaxSessionJobs: 1, MaxQueueJobs: 8, MaxRetainedJobs: 32,
		MaxBytes: 8 << 20, MaxJobBytes: 1 << 20, MinFreeBytes: 1,
		IdleTimeout: 5 * time.Second, StartupTimeout: 3 * time.Second, NoProgressTimeout: 3 * time.Second, MaxRuntime: 5 * time.Second,
		run: run, pollInterval: 5 * time.Millisecond}
}

func newTestManager(t *testing.T, o Options) *Manager {
	t.Helper()
	m, err := NewManager(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Errorf("close manager: %v", err)
		}
	})
	return m
}

func managerTestInput(t *testing.T) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "source-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("fake source bytes"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func assertManagerInputClosed(t *testing.T, input *os.File) {
	t.Helper()
	if _, err := input.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("source ownership was not released: %v", err)
	}
}

func publishManagerTestOutput(dir string, size int) error {
	segment := make([]byte, size)
	for i := range segment {
		segment[i] = byte(i)
	}
	if err := os.WriteFile(filepath.Join(dir, "segment-0.ts.tmp"), segment, 0o600); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(dir, "segment-0.ts.tmp"), filepath.Join(dir, "segment-0.ts")); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "main.m3u8.tmp"), []byte("#EXTM3U\n#EXT-X-TARGETDURATION:3\n#EXTINF:3,\nsegment-0.ts\n#EXT-X-ENDLIST\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(filepath.Join(dir, "main.m3u8.tmp"), filepath.Join(dir, "main.m3u8"))
}

func managerTestWaitFinished(t *testing.T, m *Manager, id string) Record {
	t.Helper()
	m.mu.Lock()
	j := m.jobs[id]
	if j == nil {
		m.mu.Unlock()
		t.Fatal("job is missing")
		return Record{}
	}
	done := j.done
	m.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not finish")
	}
	m.mu.Lock()
	record := j.record
	m.mu.Unlock()
	return record
}

func managerTestWaitStart(t *testing.T, started <-chan int64) int64 {
	t.Helper()
	select {
	case value := <-started:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not start")
		return 0
	}
}

func TestManagerReuseScopeAndStartupContext(t *testing.T) {
	var executions atomic.Int32
	run := func(_ context.Context, _, dir string, input *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		executions.Add(1)
		if _, err := input.Stat(); err != nil {
			return RunResult{}, err
		}
		return RunResult{}, publishManagerTestOutput(dir, 188)
	}
	o := managerTestOptions(t, run)
	startupCtx, cancelStartup := context.WithCancel(context.Background())
	m, err := NewManager(startupCtx, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close(context.Background()) })
	cancelStartup()
	spec := managerTestSpec(1)
	input := managerTestInput(t)
	record, err := m.Ensure(context.Background(), spec, input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := m.WaitReady(ctx, spec.Scope, record.ID); err != nil {
		t.Fatal(err)
	}
	final := managerTestWaitFinished(t, m, record.ID)
	if final.State != "completed" || final.OutputBytes <= 188 || final.ErrorCode != "" {
		t.Fatalf("unexpected result: %+v", final)
	}
	assertManagerInputClosed(t, input)
	duplicate := managerTestInput(t)
	reused, err := m.Ensure(ctx, spec, duplicate)
	if err != nil || reused.ID != record.ID || executions.Load() != 1 {
		t.Fatalf("duplicate was not reused: %v %+v", err, reused)
	}
	assertManagerInputClosed(t, duplicate)
	for _, change := range []func(*Scope){func(s *Scope) { s.UserID = "other" }, func(s *Scope) { s.AuthSessionID = "other" }, func(s *Scope) { s.DeviceID = "other" }, func(s *Scope) { s.PlaySessionID = "other" }, func(s *Scope) { s.ItemID = "other" }, func(s *Scope) { s.SourceID = "other" }} {
		foreign := spec.Scope
		change(&foreign)
		if _, err := m.Open(ctx, foreign, record.ID, "main.m3u8"); !errors.Is(err, ErrJobNotFound) {
			t.Fatalf("foreign scope was accepted: %v", err)
		}
	}
	for _, name := range []string{"../main.m3u8", "main.m3u8.tmp", "segment-0.ts.tmp", "file.ts", "segment-0.ts/../main.m3u8"} {
		if _, err := m.Open(ctx, spec.Scope, record.ID, name); !errors.Is(err, ErrOutputUnavailable) {
			t.Fatalf("unsafe name accepted: %q %v", name, err)
		}
	}
	handle, err := m.Open(ctx, spec.Scope, record.ID, "segment-0.ts")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(handle)
	if err != nil || len(data) != 188 {
		t.Fatalf("read output: %v, %d", err, len(data))
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	changed := spec
	changed.SourceStamp = "replacement-source"
	second, err := m.Ensure(ctx, changed, managerTestInput(t))
	if err != nil || second.ID == record.ID {
		t.Fatalf("new source snapshot reused old output: %v", err)
	}
	managerTestWaitFinished(t, m, second.ID)
}

func TestManagerSchedulesWithinUserAndSessionLimits(t *testing.T) {
	started := make(chan int64, 8)
	var active, peak atomic.Int32
	run := func(ctx context.Context, _, _ string, _ *os.File, p Plan, _ int, _ func(Progress)) (RunResult, error) {
		count := active.Add(1)
		for previous := peak.Load(); count > previous && !peak.CompareAndSwap(previous, count); previous = peak.Load() {
		}
		defer active.Add(-1)
		started <- p.StartTicks
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}
	m := newTestManager(t, managerTestOptions(t, run))
	first := managerTestSpec(1)
	first.Plan.StartTicks = 1
	firstInput := managerTestInput(t)
	if _, err := m.Ensure(context.Background(), first, firstInput); err != nil {
		t.Fatal(err)
	}
	if value := managerTestWaitStart(t, started); value != 1 {
		t.Fatalf("first runner = %d", value)
	}
	second := managerTestSpec(2)
	second.Scope.AuthSessionID = "auth-2"
	second.Plan.StartTicks = 2
	secondInput := managerTestInput(t)
	if _, err := m.Ensure(context.Background(), second, secondInput); err != nil {
		t.Fatal(err)
	}
	third := managerTestSpec(3)
	third.Scope.UserID = "viewer-2"
	third.Scope.AuthSessionID = "auth-3"
	third.Plan.StartTicks = 3
	if _, err := m.Ensure(context.Background(), third, managerTestInput(t)); err != nil {
		t.Fatal(err)
	}
	if value := managerTestWaitStart(t, started); value != 3 {
		t.Fatalf("per-user limit was bypassed by runner %d", value)
	}
	select {
	case value := <-started:
		t.Fatalf("unexpected runner %d", value)
	case <-time.After(30 * time.Millisecond):
	}
	if err := m.Cancel(context.Background(), first.Scope); err != nil {
		t.Fatal(err)
	}
	assertManagerInputClosed(t, firstInput)
	if value := managerTestWaitStart(t, started); value != 2 {
		t.Fatalf("queued runner = %d", value)
	}
	if peak.Load() > 2 {
		t.Fatalf("global concurrency exceeded: %d", peak.Load())
	}
	if err := m.Cancel(context.Background(), second.Scope); err != nil {
		t.Fatal(err)
	}
	assertManagerInputClosed(t, secondInput)
}

func TestManagerBoundsQueueAndClosesRejectedInputs(t *testing.T) {
	started := make(chan int64, 4)
	run := func(ctx context.Context, _, _ string, _ *os.File, p Plan, _ int, _ func(Progress)) (RunResult, error) {
		started <- p.StartTicks
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}
	o := managerTestOptions(t, run)
	o.MaxJobs, o.MaxQueueJobs = 1, 1
	m := newTestManager(t, o)
	first := managerTestSpec(1)
	if _, err := m.Ensure(context.Background(), first, managerTestInput(t)); err != nil {
		t.Fatal(err)
	}
	managerTestWaitStart(t, started)
	second := managerTestSpec(2)
	queuedInput := managerTestInput(t)
	queued, err := m.Ensure(context.Background(), second, queuedInput)
	if err != nil {
		t.Fatal(err)
	}
	rejected := managerTestInput(t)
	if _, err := m.Ensure(context.Background(), managerTestSpec(3), rejected); !errors.Is(err, ErrBusy) {
		t.Fatalf("unbounded queue: %v", err)
	}
	assertManagerInputClosed(t, rejected)
	duplicate := managerTestInput(t)
	if reused, err := m.Ensure(context.Background(), second, duplicate); err != nil || reused.ID != queued.ID {
		t.Fatalf("queued reuse failed: %v", err)
	}
	assertManagerInputClosed(t, duplicate)
	if err := m.Cancel(context.Background(), second.Scope); err != nil {
		t.Fatal(err)
	}
	assertManagerInputClosed(t, queuedInput)
	invalid := second
	invalid.Scope.AuthSessionID = ""
	invalidInput := managerTestInput(t)
	if _, err := m.Ensure(context.Background(), invalid, invalidInput); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("invalid scope: %v", err)
	}
	assertManagerInputClosed(t, invalidInput)
}

func TestManagerReaderPinsCacheAcrossCancelledClose(t *testing.T) {
	run := func(_ context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		return RunResult{}, publishManagerTestOutput(dir, 188)
	}
	o := managerTestOptions(t, run)
	m := newTestManager(t, o)
	spec := managerTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	managerTestWaitFinished(t, m, record.ID)
	handle, err := m.Open(context.Background(), spec.Scope, record.ID, "segment-0.ts")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	m.CancelSession(spec.Scope.AuthSessionID)
	if _, err := m.Open(context.Background(), spec.Scope, record.ID, "main.m3u8"); !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("logged-out cache is still accessible: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := m.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close ignored live reader: %v", err)
	}
	if _, err := os.Stat(filepath.Join(o.Root, record.ID, "segment-0.ts")); err != nil {
		t.Fatalf("reader's cache was removed early: %v", err)
	}
	if _, err := NewManager(context.Background(), o); !errors.Is(err, ErrCacheLocked) {
		t.Fatalf("shutdown released lock early: %v", err)
	}
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if data, err := io.ReadAll(handle); err != nil || len(data) != 188 {
		t.Fatalf("retained reader failed: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(o.Root, record.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache not reclaimed: %v", err)
	}
	reopened, err := NewManager(context.Background(), o)
	if err != nil {
		t.Fatalf("cache lock not released: %v", err)
	}
	if err := reopened.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerTimeoutsTerminateOwnedInput(t *testing.T) {
	for _, test := range []struct {
		name, code      string
		ready, progress bool
		configure       func(*Options)
	}{
		{"startup", "startup_timeout", false, false, func(o *Options) { o.StartupTimeout = 50 * time.Millisecond }},
		{"progress", "progress_timeout", true, false, func(o *Options) { o.NoProgressTimeout = 50 * time.Millisecond }},
		{"runtime", "runtime_timeout", true, true, func(o *Options) { o.MaxRuntime = 60 * time.Millisecond }},
		{"idle", "idle_timeout", true, true, func(o *Options) { o.IdleTimeout = 60 * time.Millisecond }},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := func(ctx context.Context, _, dir string, _ *os.File, _ Plan, _ int, progress func(Progress)) (RunResult, error) {
				if test.ready {
					if err := publishManagerTestOutput(dir, 188); err != nil {
						return RunResult{}, err
					}
				}
				ticker := time.NewTicker(5 * time.Millisecond)
				defer ticker.Stop()
				var ticks int64
				for {
					select {
					case <-ctx.Done():
						return RunResult{}, ctx.Err()
					case <-ticker.C:
						if test.progress {
							ticks++
							progress(Progress{OutputTicks: ticks})
						}
					}
				}
			}
			o := managerTestOptions(t, run)
			test.configure(&o)
			m := newTestManager(t, o)
			input := managerTestInput(t)
			record, err := m.Ensure(context.Background(), managerTestSpec(1), input)
			if err != nil {
				t.Fatal(err)
			}
			final := managerTestWaitFinished(t, m, record.ID)
			if final.ErrorCode != test.code {
				t.Fatalf("timeout classification = %q, want %q", final.ErrorCode, test.code)
			}
			assertManagerInputClosed(t, input)
		})
	}
}

func TestManagerRejectsOversizedOutputAndSanitizesFailure(t *testing.T) {
	for _, test := range []struct {
		name, code string
		size       int
		runErr     error
	}{
		{"quota", "job_quota", 4096, nil}, {"process", "process_failed", 188, errors.New("raw stderr with secret-token")}, {"empty", "invalid_output", 0, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := func(_ context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
				if test.size > 0 {
					if err := publishManagerTestOutput(dir, test.size); err != nil {
						return RunResult{}, err
					}
				}
				return RunResult{StderrTail: "secret-token"}, test.runErr
			}
			o := managerTestOptions(t, run)
			o.MaxJobBytes = 1024
			m := newTestManager(t, o)
			record, err := m.Ensure(context.Background(), managerTestSpec(1), managerTestInput(t))
			if err != nil {
				t.Fatal(err)
			}
			final := managerTestWaitFinished(t, m, record.ID)
			if final.State != "failed" || final.ErrorCode != test.code {
				t.Fatalf("failure projection = %+v", final)
			}
		})
	}
}

func TestManagerCreationFailureAndShutdownDuringCreate(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		o := managerTestOptions(t, func(context.Context, string, string, *os.File, Plan, int, func(Progress)) (RunResult, error) {
			t.Error("runner started after create failure")
			return RunResult{}, nil
		})
		o.Repository = &managerTestRepository{createErr: errors.New("private database error")}
		m := newTestManager(t, o)
		input := managerTestInput(t)
		record, err := m.Ensure(context.Background(), managerTestSpec(1), input)
		if !errors.Is(err, ErrPersistence) {
			t.Fatalf("create error = %v", err)
		}
		managerTestWaitFinished(t, m, record.ID)
		assertManagerInputClosed(t, input)
	})
	t.Run("shutdown", func(t *testing.T) {
		gate := make(chan struct{})
		entered := make(chan struct{}, 1)
		o := managerTestOptions(t, func(context.Context, string, string, *os.File, Plan, int, func(Progress)) (RunResult, error) {
			t.Error("runner started during shutdown")
			return RunResult{}, nil
		})
		o.Repository = &managerTestRepository{createGate: gate, createEntered: entered}
		m := newTestManager(t, o)
		input := managerTestInput(t)
		result := make(chan error, 1)
		go func() { _, err := m.Ensure(context.Background(), managerTestSpec(1), input); result <- err }()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("create was not entered")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Fatalf("creation was not cancelled by shutdown: %v", err)
		}
		if err := <-result; !errors.Is(err, ErrPersistence) && !errors.Is(err, ErrManagerClosed) {
			t.Fatalf("Ensure after close = %v", err)
		}
		assertManagerInputClosed(t, input)
	})
}

func TestManagerConcurrentEnsureCreatesOneDurableJob(t *testing.T) {
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	started := make(chan int64, 1)
	run := func(ctx context.Context, _, _ string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		started <- 1
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}
	o := managerTestOptions(t, run)
	o.Repository = &managerTestRepository{createGate: gate, createEntered: entered}
	m := newTestManager(t, o)
	spec := managerTestSpec(1)
	const requests = 24
	inputs := make([]*os.File, requests)
	for i := range inputs {
		inputs[i] = managerTestInput(t)
	}
	results := make(chan Record, requests)
	errorsCh := make(chan error, requests)
	var wg sync.WaitGroup
	for _, input := range inputs {
		wg.Add(1)
		go func(input *os.File) {
			defer wg.Done()
			record, err := m.Ensure(context.Background(), spec, input)
			results <- record
			errorsCh <- err
		}(input)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("creation did not begin")
	}
	close(gate)
	wg.Wait()
	close(results)
	close(errorsCh)
	var id string
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	for record := range results {
		if id == "" {
			id = record.ID
		}
		if record.ID != id {
			t.Fatalf("same spec created %s and %s", id, record.ID)
		}
	}
	managerTestWaitStart(t, started)
	if err := m.Cancel(context.Background(), spec.Scope); err != nil {
		t.Fatal(err)
	}
	for _, input := range inputs {
		assertManagerInputClosed(t, input)
	}
	repo := o.Repository.(*managerTestRepository)
	repo.mu.Lock()
	count := len(repo.records)
	repo.mu.Unlock()
	if count != 1 {
		t.Fatalf("durable jobs=%d", count)
	}
}

func TestManagerWaitsForAtomicOutputsAndBoundsReaders(t *testing.T) {
	temporary := make(chan struct{})
	publish := make(chan struct{})
	run := func(ctx context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		if err := os.WriteFile(filepath.Join(dir, "segment-0.ts.tmp"), make([]byte, 188), 0o600); err != nil {
			return RunResult{}, err
		}
		if err := os.WriteFile(filepath.Join(dir, "main.m3u8.tmp"), []byte("#EXTM3U\nsegment-0.ts\n"), 0o600); err != nil {
			return RunResult{}, err
		}
		close(temporary)
		select {
		case <-ctx.Done():
			return RunResult{}, ctx.Err()
		case <-publish:
		}
		if err := os.Rename(filepath.Join(dir, "segment-0.ts.tmp"), filepath.Join(dir, "segment-0.ts")); err != nil {
			return RunResult{}, err
		}
		if err := os.Rename(filepath.Join(dir, "main.m3u8.tmp"), filepath.Join(dir, "main.m3u8")); err != nil {
			return RunResult{}, err
		}
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}
	o := managerTestOptions(t, run)
	o.MaxReaders, o.MaxJobReaders = 1, 1
	m := newTestManager(t, o)
	spec := managerTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-temporary:
	case <-time.After(time.Second):
		t.Fatal("temporary output was not created")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	if _, err := m.WaitReady(ctx, spec.Scope, record.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("temporary output was advertised: %v", err)
	}
	cancel()
	close(publish)
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := m.WaitReady(ctx, spec.Scope, record.ID); err != nil {
		t.Fatal(err)
	}
	handle, err := m.Open(ctx, spec.Scope, record.ID, "main.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if _, err := m.Open(ctx, spec.Scope, record.ID, "segment-0.ts"); !errors.Is(err, ErrBusy) {
		t.Fatalf("reader quota not enforced: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	missingCtx, cancelMissing := context.WithTimeout(context.Background(), 30*time.Millisecond)
	if _, err := m.Open(missingCtx, spec.Scope, record.ID, "segment-1.ts"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("future segment did not honor deadline: %v", err)
	}
	cancelMissing()
	last, err := m.Open(ctx, spec.Scope, record.ID, "segment-0.ts")
	if err != nil {
		t.Fatalf("failed opens leaked reader permits: %v", err)
	}
	if err := last.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagerGlobalStorageAndFreeSpaceLimits(t *testing.T) {
	t.Run("aggregate", func(t *testing.T) {
		run := func(ctx context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
			if err := publishManagerTestOutput(dir, 4096); err != nil {
				return RunResult{}, err
			}
			<-ctx.Done()
			return RunResult{}, ctx.Err()
		}
		o := managerTestOptions(t, run)
		o.MaxBytes, o.MaxJobBytes = 7000, 6000
		m := newTestManager(t, o)
		first := managerTestSpec(1)
		r1, err := m.Ensure(context.Background(), first, managerTestInput(t))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err = m.WaitReady(ctx, first.Scope, r1.ID); err != nil {
			t.Fatal(err)
		}
		second := managerTestSpec(2)
		second.Scope.UserID = "other"
		second.Scope.AuthSessionID = "other-auth"
		r2, err := m.Ensure(context.Background(), second, managerTestInput(t))
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{r1.ID, r2.ID} {
			final := managerTestWaitFinished(t, m, id)
			if final.State != "failed" || final.ErrorCode != "cache_quota" {
				t.Fatalf("aggregate storage was not stopped: %+v", final)
			}
		}
	})
	t.Run("free-space", func(t *testing.T) {
		o := managerTestOptions(t, func(context.Context, string, string, *os.File, Plan, int, func(Progress)) (RunResult, error) {
			t.Error("runner started without free-space reserve")
			return RunResult{}, nil
		})
		o.MinFreeBytes = 1 << 62
		m := newTestManager(t, o)
		input := managerTestInput(t)
		if _, err := m.Ensure(context.Background(), managerTestSpec(1), input); !errors.Is(err, ErrQuota) {
			t.Fatalf("free-space limit: %v", err)
		}
		assertManagerInputClosed(t, input)
	})
}

func TestManagerDoesNotStartWithoutDurableRunningState(t *testing.T) {
	var called atomic.Bool
	o := managerTestOptions(t, func(context.Context, string, string, *os.File, Plan, int, func(Progress)) (RunResult, error) {
		called.Store(true)
		return RunResult{}, nil
	})
	o.Repository = &managerTestRepository{updateErr: errors.New("private database failure")}
	m := newTestManager(t, o)
	input := managerTestInput(t)
	record, err := m.Ensure(context.Background(), managerTestSpec(1), input)
	if err != nil {
		t.Fatal(err)
	}
	final := managerTestWaitFinished(t, m, record.ID)
	if called.Load() || final.ErrorCode != "persistence" || final.State != "failed" {
		t.Fatalf("non-durable process execution: %+v", final)
	}
	assertManagerInputClosed(t, input)
}

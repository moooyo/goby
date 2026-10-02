//go:build linux

package transcode

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type managerFinalizationPersistObservation struct {
	record      Record
	contextErr  error
	hasDeadline bool
}

type managerFinalizationRepository struct {
	*managerTestRepository
	terminalPlaySession string
	entered             chan managerFinalizationPersistObservation
	release             <-chan struct{}
	once                sync.Once
	terminalUpdates     atomic.Int32
}

func (r *managerFinalizationRepository) Update(ctx context.Context, record Record) error {
	terminal := record.State == "completed" || record.State == "failed" || record.State == "cancelled"
	if terminal {
		r.terminalUpdates.Add(1)
	}
	if terminal && record.Spec.Scope.PlaySessionID == r.terminalPlaySession && r.release != nil {
		_, hasDeadline := ctx.Deadline()
		r.once.Do(func() {
			r.entered <- managerFinalizationPersistObservation{record: record, contextErr: ctx.Err(), hasDeadline: hasDeadline}
		})
		select {
		case <-r.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return r.managerTestRepository.Update(ctx, record)
}

type managerFinalizationLogObservation struct {
	contextErr  error
	finished    bool
	running     bool
	subjectHeld bool
	global      int
	user        int
	auth        int
	ticket      *completionTicket
	executor    finalizationExecutorSnapshot
}

type managerFinalizationLogHandler struct {
	slog.Handler
	manager *Manager
	job     *managedJob
	entered chan managerFinalizationLogObservation
	action  func() error
	calls   atomic.Int32
}

func (h *managerFinalizationLogHandler) Handle(ctx context.Context, record slog.Record) error {
	var jobID string
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "job_id" {
			jobID = attr.Value.String()
		}
		return true
	})
	if record.Message != "transcode progress rejected" || jobID != h.job.record.ID {
		return h.Handler.Handle(ctx, record)
	}
	h.calls.Add(1)
	observed := managerFinalizationLogObservation{contextErr: ctx.Err()}
	h.manager.mu.Lock()
	observed.finished, observed.running = h.job.finished, h.job.running
	observed.subjectHeld, observed.global = h.job.subjectOwnershipHeld, h.manager.running
	observed.user = h.manager.runningUsers[h.job.record.Spec.Scope.UserID]
	observed.auth = h.manager.runningAuth[h.job.record.Spec.Scope.AuthSessionID]
	observed.ticket = h.job.completion
	h.manager.mu.Unlock()
	// Reaching the observation proves that terminal logging holds no file gate.
	h.manager.filesMu.Lock()
	h.manager.filesMu.Unlock()
	observed.executor = h.manager.finalizers.snapshot()
	h.entered <- observed
	return h.action()
}

// These fixtures exercise legacy accounting with the real cache and executor.
// They make no native process-domain or hard storage-enforcement claim.
func newManagerFinalizationFixture(t *testing.T, repository Repository) *Manager {
	t.Helper()
	options := managerAccessOptions(t, nil)
	options.MaxJobs = 1
	if repository != nil {
		options.Repository = repository
	}
	m, err := NewManager(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	m.cancel()
	managerAccessWait(t, m.loopDone, "the paused finalization maintenance loop")
	t.Cleanup(func() {
		m.finalizers.stopAfterDrain()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := m.finalizers.wait(ctx); errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("finalization fixture did not drain: %v", err)
			return
		}
		// Quarantine keeps its job and ticket unchanged through fixture teardown.
		m.filesMu.Lock()
		err := m.cache.Close()
		m.filesMu.Unlock()
		if err != nil {
			t.Errorf("close finalization fixture cache: %v", err)
		}
	})
	return m
}

func addManagerFinalizationRunningJob(t *testing.T, m *Manager) *managedJob {
	t.Helper()
	j := addJobLockingOutput(t, m, 1, false)
	initial := j.record
	initial.State = "queued"
	if err := m.options.Repository.Create(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	ticket, err := m.completions.reserve()
	if err != nil {
		t.Fatal(err)
	}
	input := managerTestInput(t)
	m.mu.Lock()
	j.input, j.completion = input, ticket
	j.record.State = "running"
	j.running, j.finished, j.durable, j.subjectOwnershipHeld = true, false, true, true
	j.done = make(chan struct{})
	m.running = 1
	m.runningUsers[j.record.Spec.Scope.UserID] = 1
	m.runningAuth[j.record.Spec.Scope.AuthSessionID] = 1
	m.mu.Unlock()
	return j
}

func managerFinalizationPhase(ticket *completionTicket) completionTicketPhase {
	ticket.pool.mu.Lock()
	defer ticket.pool.mu.Unlock()
	return ticket.phase
}

func managerFinalizationDrain(t *testing.T, m *Manager) error {
	t.Helper()
	m.finalizers.stopAfterDrain()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return m.finalizers.wait(ctx)
}

func managerFinalizationWaitFence(t *testing.T, m *Manager, j *managedJob) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		m.mu.Lock()
		fenced, changed := m.cacheFailed, j.changed
		m.mu.Unlock()
		if fenced {
			return
		}
		select {
		case <-changed:
		case <-timer.C:
			t.Fatal("abnormal terminal logging did not fence admission")
		}
	}
}

func assertManagerFinalizationHeld(t *testing.T, m *Manager, j *managedJob, ticket *completionTicket, wantGlobal int) {
	t.Helper()
	m.mu.Lock()
	held := !j.finished && j.subjectOwnershipHeld == (wantGlobal != 0) && j.completion == ticket &&
		m.running == wantGlobal && j.running == (wantGlobal != 0) &&
		m.runningUsers[j.record.Spec.Scope.UserID] == wantGlobal && m.runningAuth[j.record.Spec.Scope.AuthSessionID] == wantGlobal
	m.mu.Unlock()
	if !held || managerFinalizationPhase(ticket) != completionWorking {
		t.Fatal("finalization changed execution counters before inspection or returned its original ticket early")
	}
	select {
	case <-j.done:
		t.Fatal("unfinished terminal handling closed the job completion channel")
	default:
	}
}

func TestManagerFinalizationInspectionKeepsLegacyExecutionAndTicket(t *testing.T) {
	m := newManagerFinalizationFixture(t, nil)
	j := addManagerFinalizationRunningJob(t, m)
	ticket, knownBytes := j.completion, j.record.OutputBytes
	release := holdCacheJob(t, m.cache, j.record.ID)
	if err := m.enqueueFinalization(j, nil, nil); err != nil {
		t.Fatal(err)
	}
	waitCacheJobReferences(t, m.cache, j.record.ID, 2)
	assertManagerInputClosed(t, j.input)
	assertManagerFinalizationHeld(t, m, j, ticket, 1)
	m.mu.Lock()
	accounted := j.record.OutputBytes == knownBytes && m.bytes == knownBytes
	m.mu.Unlock()
	if !accounted {
		t.Fatal("blocked complete inspection changed legacy accounting")
	}
	if snapshot := m.finalizers.snapshot(); snapshot.Capacity != m.options.MaxRetainedJobs || snapshot.Workers != 1 ||
		snapshot.LiveWorkers != 1 || snapshot.Working != 1 || snapshot.Queued != 0 ||
		snapshot.Completion.Working != 1 || snapshot.Completion.Available != snapshot.Capacity-1 {
		t.Fatalf("blocked inspection escaped the fixed finalization bound: %+v", snapshot)
	}
	release()
	if final := managerTestWaitFinished(t, m, j.record.ID); final.State != "completed" {
		t.Fatalf("inspection lost the successful legacy outcome: %+v", final)
	}
	if err := managerFinalizationDrain(t, m); err != nil {
		t.Fatal(err)
	}
	if managerFinalizationPhase(ticket) != completionReleased {
		t.Fatal("returned terminal callback did not release its original ticket")
	}
}

func TestManagerFinalizationPersistenceReturnsExecutionAndPreventsReclaim(t *testing.T) {
	gate, release := finalizationTestGate(t)
	defer release()
	repository := &managerFinalizationRepository{managerTestRepository: &managerTestRepository{},
		terminalPlaySession: "play-1", entered: make(chan managerFinalizationPersistObservation, 1), release: gate}
	m := newManagerFinalizationFixture(t, repository)
	j := addManagerFinalizationRunningJob(t, m)
	ticket := j.completion
	if err := m.enqueueFinalization(j, nil, nil); err != nil {
		t.Fatal(err)
	}
	observed := finalizationTestReceive(t, repository.entered, "the terminal persistence barrier")
	if observed.record.State != "completed" || observed.contextErr != nil || !observed.hasDeadline {
		t.Fatalf("terminal persistence lost its bounded independent context: %+v", observed)
	}
	assertManagerFinalizationHeld(t, m, j, ticket, 0)
	if m.reclaim(j, true) {
		t.Fatal("blocked terminal persistence allowed physical reclaim")
	}
	if _, err := os.Stat(filepath.Join(m.cache.RootPath(), j.record.ID, "segment-0.ts")); err != nil {
		t.Fatalf("blocked terminal persistence lost retained output: %v", err)
	}
	release()
	managerTestWaitFinished(t, m, j.record.ID)
	if err := managerFinalizationDrain(t, m); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	returned := !j.subjectOwnershipHeld && m.runningUsers[j.record.Spec.Scope.UserID] == 0 && m.runningAuth[j.record.Spec.Scope.AuthSessionID] == 0
	m.mu.Unlock()
	if !returned || managerFinalizationPhase(ticket) != completionReleased || repository.terminalUpdates.Load() != 1 {
		t.Fatal("terminal persistence changed released execution counters or did not return its original ticket exactly once")
	}
	if !m.reclaim(j, true) {
		t.Fatal("returned completion ticket did not permit physical reclaim")
	}
	if _, err := os.Stat(filepath.Join(m.cache.RootPath(), j.record.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed finalization retained reclaimed files: %v", err)
	}
}

func TestManagerFinalizationReclaimWaitsForActualCompletionReturn(t *testing.T) {
	m := newManagerFinalizationFixture(t, nil)
	j := addManagerFinalizationRunningJob(t, m)
	ticket := j.completion
	gate, release := finalizationTestGate(t)
	defer release()
	terminalReturned := make(chan struct{})
	if err := closeFinalizationInputs(j); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	j.finalizationQueued = true
	m.mu.Unlock()
	if err := m.finalizers.submit(ticket, finalizationTask{
		Finalize: func(context.Context) error { return m.finalizeJobInspection(j, nil) },
		Terminal: func(_ context.Context, inspectionErr error) error {
			err := m.finalizeJobTerminal(j, nil, inspectionErr, nil)
			close(terminalReturned)
			<-gate
			return err
		},
		OnQuarantined: func(err error) { m.quarantineJobFinalization(j, err) },
	}); err != nil {
		t.Fatal(err)
	}
	managerAccessWait(t, terminalReturned, "terminal bookkeeping before executor ticket return")
	m.mu.Lock()
	bookkeepingDone := j.finished && !j.subjectOwnershipHeld && !j.running && m.running == 0 &&
		m.runningUsers[j.record.Spec.Scope.UserID] == 0 && m.runningAuth[j.record.Spec.Scope.AuthSessionID] == 0
	m.mu.Unlock()
	if !bookkeepingDone || managerFinalizationPhase(ticket) != completionWorking {
		t.Fatal("terminal bookkeeping did not remain distinct from the executor ticket return")
	}
	if m.reclaim(j, true) {
		t.Fatal("finished bookkeeping allowed reclaim before the original ticket returned")
	}
	if _, err := os.Stat(filepath.Join(m.cache.RootPath(), j.record.ID)); err != nil {
		t.Fatalf("working completion ticket lost its retained directory: %v", err)
	}
	release()
	if err := managerFinalizationDrain(t, m); err != nil {
		t.Fatal(err)
	}
	if managerFinalizationPhase(ticket) != completionReleased || !m.reclaim(j, true) {
		t.Fatal("actual completion return did not authorize reclaim")
	}
}

func TestManagerFinalizationInspectionPanicRetainsLegacyOwnership(t *testing.T) {
	m := newManagerFinalizationFixture(t, nil)
	j := addManagerFinalizationRunningJob(t, m)
	ticket, knownBytes := j.completion, j.record.OutputBytes
	if err := closeFinalizationInputs(j); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	j.finalizationQueued = true
	m.mu.Unlock()
	if err := m.finalizers.submit(ticket, finalizationTask{
		Finalize: func(context.Context) error { panic("final inspection failure") },
		Terminal: func(_ context.Context, inspectionErr error) error {
			return m.finalizeJobTerminal(j, nil, inspectionErr, nil)
		},
		OnQuarantined: func(err error) { m.quarantineJobFinalization(j, err) },
	}); err != nil {
		t.Fatal(err)
	}
	managerFinalizationWaitFence(t, m, j)
	if err := managerFinalizationDrain(t, m); !errors.Is(err, errFinalizationTerminalPanic) {
		t.Fatalf("interrupted inspection returned terminal ownership normally: %v", err)
	}
	assertManagerFinalizationHeld(t, m, j, ticket, 1)
	m.mu.Lock()
	retained := j.directory && j.accountingUnknown && j.record.OutputBytes == knownBytes && m.bytes == knownBytes && m.unaccountedJobs == 1
	m.mu.Unlock()
	if !retained || m.reclaim(j, true) {
		t.Fatal("interrupted inspection returned legacy execution or accounting ownership")
	}
	if snapshot := m.finalizers.snapshot(); snapshot.Quarantined != 1 || snapshot.Completion.Working != 1 || !snapshot.Completion.Closed {
		t.Fatalf("interrupted inspection lost its original working ticket: %+v", snapshot)
	}
}

func TestManagerFinalizationBlockedLoggerKeepsWorkingTicketAndUnfinishedJob(t *testing.T) {
	m := newManagerFinalizationFixture(t, nil)
	j := addManagerFinalizationRunningJob(t, m)
	ticket := j.completion
	gate, release := finalizationTestGate(t)
	defer release()
	handler := &managerFinalizationLogHandler{Handler: slog.NewJSONHandler(io.Discard, nil), manager: m, job: j,
		entered: make(chan managerFinalizationLogObservation, 1), action: func() error { <-gate; return nil }}
	previous := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(previous) })
	if err := m.enqueueFinalization(j, ErrProgress, &ProgressFailure{Phase: "Write", Reason: "invalid_time"}); err != nil {
		t.Fatal(err)
	}
	observed := finalizationTestReceive(t, handler.entered, "the synchronous terminal logger")
	if observed.contextErr != nil || observed.finished || observed.running || observed.subjectHeld || observed.global != 0 ||
		observed.user != 0 || observed.auth != 0 || observed.ticket != ticket || observed.executor.Workers != 1 ||
		observed.executor.LiveWorkers != 1 || observed.executor.Working != 1 || observed.executor.Completion.Working != 1 {
		t.Fatalf("blocked logger lost the fixed worker or terminal ownership: %+v", observed)
	}
	assertManagerFinalizationHeld(t, m, j, ticket, 0)
	if m.reclaim(j, true) {
		t.Fatal("blocked synchronous logger allowed physical reclaim")
	}
	release()
	if final := managerTestWaitFinished(t, m, j.record.ID); final.ErrorCode != "process_progress" {
		t.Fatalf("terminal logging changed the original run outcome: %+v", final)
	}
	if err := managerFinalizationDrain(t, m); err != nil {
		t.Fatal(err)
	}
	if handler.calls.Load() != 1 || managerFinalizationPhase(ticket) != completionReleased {
		t.Fatal("terminal logger did not complete its original ticket exactly once")
	}
}

func TestManagerFinalizationAbnormalLoggerQuarantinesOriginalTicket(t *testing.T) {
	for _, test := range []struct {
		name    string
		action  func() error
		wantErr error
	}{
		{name: "panic", action: func() error { panic("terminal logger failure") }, wantErr: errFinalizationTerminalPanic},
		{name: "goexit", action: func() error { runtime.Goexit(); return nil }, wantErr: errFinalizationTerminalExit},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := newManagerFinalizationFixture(t, nil)
			j := addManagerFinalizationRunningJob(t, m)
			ticket, knownBytes := j.completion, j.record.OutputBytes
			handler := &managerFinalizationLogHandler{Handler: slog.NewJSONHandler(io.Discard, nil), manager: m, job: j,
				entered: make(chan managerFinalizationLogObservation, 1), action: test.action}
			previous := slog.Default()
			slog.SetDefault(slog.New(handler))
			t.Cleanup(func() { slog.SetDefault(previous) })
			if err := m.enqueueFinalization(j, ErrProgress, &ProgressFailure{Phase: "Write", Reason: "invalid_time"}); err != nil {
				t.Fatal(err)
			}
			finalizationTestReceive(t, handler.entered, "the abnormal terminal logger")
			managerFinalizationWaitFence(t, m, j)
			if err := managerFinalizationDrain(t, m); !errors.Is(err, test.wantErr) {
				t.Fatalf("abnormal logger lost its executor failure: %v", err)
			}
			assertManagerFinalizationHeld(t, m, j, ticket, 0)
			m.mu.Lock()
			retained := m.jobs[j.record.ID] == j && j.directory && j.accountingUnknown &&
				j.record.OutputBytes == knownBytes && m.bytes == knownBytes && m.unaccountedJobs == 1 && m.cacheFailed
			m.mu.Unlock()
			if !retained || m.reclaim(j, true) {
				t.Fatal("abnormal logger released unfinished accounting or retained output")
			}
			if snapshot := m.finalizers.snapshot(); snapshot.Workers != 1 || snapshot.Quarantined != 1 || snapshot.Working != 0 ||
				!snapshot.Done || !snapshot.Completion.Closed || snapshot.Completion.Working != 1 || snapshot.Completion.Available != snapshot.Capacity-1 {
				t.Fatalf("abnormal logger did not retain the original working ticket: %+v", snapshot)
			}
			rejected := managerTestInput(t)
			if _, err := m.Ensure(context.Background(), managerTestSpec(2), rejected); !errors.Is(err, ErrOutputUnavailable) {
				t.Fatalf("abnormal terminal logging admitted new work: %v", err)
			}
			assertManagerInputClosed(t, rejected)
			if handler.calls.Load() != 1 {
				t.Fatal("quarantined terminal logging ran more than once")
			}
		})
	}
}

func TestManagerFinalizationDuplicateEnqueueDoesNotCloseNewDescriptors(t *testing.T) {
	repository := &managerFinalizationRepository{managerTestRepository: &managerTestRepository{}}
	m := newManagerFinalizationFixture(t, repository)
	j := addManagerFinalizationRunningJob(t, m)
	original, ticket := j.input, j.completion
	release := holdCacheJob(t, m.cache, j.record.ID)
	if err := m.enqueueFinalization(j, nil, nil); err != nil {
		t.Fatal(err)
	}
	waitCacheJobReferences(t, m.cache, j.record.ID, 2)
	assertManagerInputClosed(t, original)
	mediaSentinel, bitmapSentinel := managerTestInput(t), managerTestInput(t)
	m.mu.Lock()
	j.input, j.bitmap = mediaSentinel, bitmapSentinel
	m.mu.Unlock()
	const attempts = 16
	results := make(chan error, attempts)
	for range attempts {
		go func() { results <- m.enqueueFinalization(j, ErrProgress, &ProgressFailure{Phase: "Write"}) }()
	}
	for range attempts {
		if err := finalizationTestReceive(t, results, "a duplicate manager handoff"); err != nil {
			t.Fatalf("once-only duplicate handoff failed: %v", err)
		}
	}
	for _, input := range []*os.File{mediaSentinel, bitmapSentinel} {
		if _, err := input.Stat(); err != nil {
			t.Fatalf("duplicate handoff closed a newly observed descriptor: %v", err)
		}
	}
	assertManagerFinalizationHeld(t, m, j, ticket, 1)
	if snapshot := m.finalizers.snapshot(); snapshot.Queued != 0 || snapshot.Working != 1 || snapshot.Completion.Working != 1 {
		t.Fatalf("duplicate handoff added another finalization task: %+v", snapshot)
	}
	release()
	if final := managerTestWaitFinished(t, m, j.record.ID); final.State != "completed" || final.ErrorCode != "" {
		t.Fatalf("duplicate handoff replaced the original outcome: %+v", final)
	}
	if err := managerFinalizationDrain(t, m); err != nil {
		t.Fatal(err)
	}
	if repository.terminalUpdates.Load() != 1 || managerFinalizationPhase(ticket) != completionReleased {
		t.Fatal("duplicate handoff repeated terminal persistence or ticket return")
	}
}

type managerFinalizationEnsureResult struct {
	record Record
	err    error
}

func TestManagerFinalizationCreatingJobsRetainTheirAdmissionTicket(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "create_failure"
		if shutdown {
			name = "shutdown_during_create"
		}
		t.Run(name, func(t *testing.T) {
			gate, release := finalizationTestGate(t)
			defer release()
			repository := &managerTestRepository{createGate: gate, createEntered: make(chan struct{}, 1)}
			if !shutdown {
				repository.createErr = errors.New("creation failed")
			}
			var executions atomic.Int32
			options := managerAccessOptions(t, func(context.Context, string, string, *os.File, Plan, int, func(Progress)) (RunResult, error) {
				executions.Add(1)
				return RunResult{}, nil
			})
			options.Repository = repository
			m := newTestManager(t, options)
			input := managerTestInput(t)
			result := make(chan managerFinalizationEnsureResult, 1)
			go func() {
				record, err := m.Ensure(context.Background(), managerTestSpec(1), input)
				result <- managerFinalizationEnsureResult{record: record, err: err}
			}()
			managerAccessWait(t, repository.createEntered, "the creating job persistence barrier")
			m.mu.Lock()
			var j *managedJob
			for _, accepted := range m.jobs {
				j = accepted
			}
			reserved := j != nil && j.completion != nil && !j.finalizationQueued && !j.running && !j.subjectOwnershipHeld && !j.finished
			m.mu.Unlock()
			if !reserved || managerFinalizationPhase(j.completion) != completionReserved {
				t.Fatal("creating job did not retain its original admission ticket")
			}
			ticket := j.completion
			if snapshot := m.completions.snapshot(); snapshot.Reserved != 1 || snapshot.Queued != 0 || snapshot.Working != 0 {
				t.Fatalf("creating job prematurely handed off or returned its ticket: %+v", snapshot)
			}
			if snapshot := m.finalizers.snapshot(); snapshot.Capacity != options.MaxRetainedJobs || snapshot.Workers != min(2, options.MaxJobs) ||
				snapshot.LiveWorkers != snapshot.Workers || snapshot.Queued != 0 || snapshot.Working != 0 {
				t.Fatalf("creating job escaped the configured fixed executor: %+v", snapshot)
			}
			if _, err := input.Stat(); err != nil {
				t.Fatalf("creating job closed its owned source early: %v", err)
			}
			if !shutdown {
				release()
				outcome := finalizationTestReceive(t, result, "the failed creation result")
				if !errors.Is(outcome.err, ErrPersistence) || outcome.record.ID != j.record.ID {
					t.Fatalf("creation failure changed its accepted identity: %+v", outcome)
				}
				managerTestWaitFinished(t, m, j.record.ID)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := m.Close(ctx); err != nil {
				t.Fatalf("accepted creating job did not drain through shutdown: %v", err)
			}
			if shutdown {
				outcome := finalizationTestReceive(t, result, "the shutdown creation result")
				if !errors.Is(outcome.err, ErrPersistence) && !errors.Is(outcome.err, ErrManagerClosed) {
					t.Fatalf("shutdown creation returned an unrelated error: %v", outcome.err)
				}
			}
			m.mu.Lock()
			handedOff := j.finalizationQueued && j.finished && j.completion == ticket
			m.mu.Unlock()
			if !handedOff || managerFinalizationPhase(ticket) != completionReleased || executions.Load() != 0 {
				t.Fatal("accepted creating job bypassed finalization or launched a runner")
			}
			assertManagerInputClosed(t, input)
			if snapshot := m.finalizers.snapshot(); !snapshot.Done || snapshot.Completion.Available != options.MaxRetainedJobs || snapshot.Quarantined != 0 {
				t.Fatalf("shutdown did not drain the accepted creation ticket: %+v", snapshot)
			}
		})
	}
}

func TestManagerFinalizationQueuedCancellationUsesOriginalTicket(t *testing.T) {
	gate, release := finalizationTestGate(t)
	defer release()
	repository := &managerFinalizationRepository{managerTestRepository: &managerTestRepository{},
		terminalPlaySession: "play-2", entered: make(chan managerFinalizationPersistObservation, 1), release: gate}
	started := make(chan int64, 1)
	var executions atomic.Int32
	options := managerAccessOptions(t, func(ctx context.Context, _, _ string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		executions.Add(1)
		started <- 1
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	options.Repository = repository
	options.MaxJobs, options.MaxQueueJobs, options.MaxRetainedJobs = 1, 1, 2
	options.MaxUserQueueJobs, options.MaxSessionQueueJobs = 1, 1
	m := newTestManager(t, options)
	if _, err := m.Ensure(context.Background(), managerTestSpec(1), managerTestInput(t)); err != nil {
		t.Fatal(err)
	}
	managerTestWaitStart(t, started)
	spec, input := managerTestSpec(2), managerTestInput(t)
	record, err := m.Ensure(context.Background(), spec, input)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	j := m.jobs[record.ID]
	ticket := j.completion
	queued := !j.running && !j.subjectOwnershipHeld && !j.finalizationQueued && ticket != nil
	m.mu.Unlock()
	if !queued || managerFinalizationPhase(ticket) != completionReserved {
		t.Fatal("queued job did not retain a distinct original admission ticket")
	}
	if err := m.CancelJob(record.ID, spec.Scope); err != nil {
		t.Fatal(err)
	}
	observed := finalizationTestReceive(t, repository.entered, "the queued cancellation terminal persistence")
	if observed.record.State != "cancelled" {
		t.Fatalf("queued cancellation lost its terminal outcome: %+v", observed.record)
	}
	m.mu.Lock()
	handedOff := j.finalizationQueued && !j.finished && !j.running && !j.subjectOwnershipHeld && j.completion == ticket && m.running == 1
	m.mu.Unlock()
	if !handedOff || managerFinalizationPhase(ticket) != completionWorking {
		t.Fatal("queued cancellation bypassed its original fixed-worker handoff")
	}
	assertManagerInputClosed(t, input)
	if snapshot := m.finalizers.snapshot(); snapshot.Workers != 1 || snapshot.Working != 1 || snapshot.Completion.Reserved != 1 ||
		snapshot.Completion.Working != 1 || snapshot.Completion.Available != 0 {
		t.Fatalf("queued cancellation did not retain the complete admission bound: %+v", snapshot)
	}
	release()
	managerTestWaitFinished(t, m, record.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := m.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if executions.Load() != 1 || managerFinalizationPhase(ticket) != completionReleased {
		t.Fatal("queued cancellation launched a runner or lost its terminal ticket")
	}
}

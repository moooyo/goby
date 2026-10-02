//go:build linux

package transcode

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type managerPlaybackContextKey struct{}

type managerPlaybackRepository struct {
	*managerTestRepository
	contextObserved atomic.Bool
}

func (repository *managerPlaybackRepository) Create(ctx context.Context, record Record) error {
	if ctx.Value(managerPlaybackContextKey{}) != record.Spec.Scope.PlaySessionID {
		return errors.New("persistence lost the actual admitted operation context")
	}
	repository.contextObserved.Store(true)
	return repository.managerTestRepository.Create(ctx, record)
}

func TestManagerPlaybackStopAfterFirstAdmissionRejectsPostFilesystemInsertion(t *testing.T) {
	var stopped atomic.Bool
	var references, admissions, checks, executions atomic.Int32
	firstCheck := make(chan struct{})
	options := managerAccessOptions(t, func(context.Context, string, string, *os.File, Plan, int, func(Progress)) (RunResult, error) {
		executions.Add(1)
		return RunResult{}, errors.New("a stopped playback reached the runner")
	})
	options.PlaybackAdmission = func(ctx context.Context, _ Spec) (context.Context, func(), error) {
		admissions.Add(1)
		references.Add(1)
		return ctx, func() { references.Add(-1) }, nil
	}
	options.PlaybackStopped = func(Spec) bool {
		observed := stopped.Load()
		if checks.Add(1) == 1 {
			close(firstCheck)
		}
		return observed
	}
	m := newTestManager(t, options)
	// This is the real cache filesystem gate used between the two admission
	// mutex stages, not a hook that waits while holding Manager.mu.
	m.filesMu.Lock()
	var unlockOnce sync.Once
	unlockFiles := func() { unlockOnce.Do(m.filesMu.Unlock) }
	t.Cleanup(unlockFiles)
	input := managerTestInput(t)
	result := managerPlaybackEnsureAsync(m, managerTestSpec(1), input)
	managerAccessWait(t, firstCheck, "the first actual playback admission check")
	if references.Load() != 1 || admissions.Load() != 1 {
		t.Fatal("the filesystem operation did not hold exactly one admission reference")
	}
	stopped.Store(true)
	managerPlaybackAssertPending(t, result)
	unlockFiles()
	observed := managerPlaybackReceive(t, result)
	if !errors.Is(observed.err, ErrJobCancelled) || observed.record.ID != "" {
		t.Fatalf("late Stop admitted a new job: %+v", observed)
	}
	assertManagerInputClosed(t, input)
	if references.Load() != 0 || admissions.Load() != 1 || executions.Load() != 0 || checks.Load() < 2 {
		t.Fatal("post-filesystem rejection released early, reacquired admission or executed a stopped job")
	}
	m.mu.Lock()
	jobs := len(m.jobs)
	m.mu.Unlock()
	if jobs != 0 {
		t.Fatal("a stopped post-filesystem insertion retained a job")
	}
}

func TestManagerPlaybackReferencesCoverPersistenceAndCreatedDedupWait(t *testing.T) {
	gate := make(chan struct{})
	var releaseOnce sync.Once
	releaseCreate := func() { releaseOnce.Do(func() { close(gate) }) }
	repository := &managerPlaybackRepository{managerTestRepository: &managerTestRepository{createGate: gate, createEntered: make(chan struct{}, 1)}}
	var stopped atomic.Bool
	var references, admissions, executions atomic.Int32
	admitted := make(chan struct{}, 2)
	options := managerAccessOptions(t, func(context.Context, string, string, *os.File, Plan, int, func(Progress)) (RunResult, error) {
		executions.Add(1)
		return RunResult{}, errors.New("late-created stopped playback reached the runner")
	})
	options.Repository = repository
	options.PlaybackAdmission = func(ctx context.Context, spec Spec) (context.Context, func(), error) {
		admissions.Add(1)
		references.Add(1)
		admitted <- struct{}{}
		return context.WithValue(ctx, managerPlaybackContextKey{}, spec.Scope.PlaySessionID), func() { references.Add(-1) }, nil
	}
	options.PlaybackStopped = func(Spec) bool { return stopped.Load() }
	m := newTestManager(t, options)
	t.Cleanup(releaseCreate)
	firstInput, duplicateInput := managerTestInput(t), managerTestInput(t)
	first := managerPlaybackEnsureAsync(m, managerTestSpec(1), firstInput)
	managerAccessWait(t, repository.createEntered, "the actual blocked record creation")
	managerAccessWait(t, admitted, "the creator's admission reference")
	duplicateWaiting := make(chan struct{})
	duplicateDeadline, cancelDuplicate := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancelDuplicate)
	duplicateContext := &managerPlaybackDedupContext{Context: duplicateDeadline, waiting: duplicateWaiting}
	duplicate := managerPlaybackEnsureContextAsync(m, duplicateContext, managerTestSpec(1), duplicateInput)
	managerAccessWait(t, admitted, "the deduplicating operation's admission reference")
	managerAccessWait(t, duplicateWaiting, "the actual created-channel dedup wait")
	if references.Load() != 2 || admissions.Load() != 2 || !repository.contextObserved.Load() {
		t.Fatal("persistence or dedup waiting lost its real operation reference/context")
	}
	managerPlaybackAssertPending(t, first)
	managerPlaybackAssertPending(t, duplicate)
	stopped.Store(true)
	releaseCreate()
	created, reused := managerPlaybackReceive(t, first), managerPlaybackReceive(t, duplicate)
	if !errors.Is(created.err, ErrJobCancelled) || !errors.Is(reused.err, ErrJobCancelled) || created.record.ID == "" || reused.record.ID != created.record.ID {
		t.Fatalf("a late Stop survived creation or created-wait reuse: creator=%+v duplicate=%+v", created, reused)
	}
	finished := managerTestWaitFinished(t, m, created.record.ID)
	if finished.State != "cancelled" || executions.Load() != 0 || references.Load() != 0 || admissions.Load() != 2 {
		t.Fatal("late persistence Stop did not cancel before launch or returned operation references incorrectly")
	}
	assertManagerInputClosed(t, firstInput)
	assertManagerInputClosed(t, duplicateInput)
	// Generic observation and cancellation remain available after stopped
	// admission. They do not attempt another playback admission reference.
	if record, err := m.Snapshot(finished.Spec.Scope, finished.ID); record.ID != finished.ID || !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("stopped admission hid generic job lookup: record=%+v error=%v", record, err)
	}
	if err := m.CancelJob(finished.ID, finished.Spec.Scope); err != nil {
		t.Fatal(err)
	}
}

func TestManagerPlaybackOperationContextDoesNotBecomeJobLifetimeAndRetriesOnce(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var finishOnce sync.Once
	releaseRunner := func() { finishOnce.Do(func() { close(finish) }) }
	var executions, references, admissions atomic.Int32
	options := retentionTestOptions(t)
	options.run = func(ctx context.Context, _, directory string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		if ctx.Value(managerPlaybackContextKey{}) != nil {
			return RunResult{}, errors.New("job inherited a short operation context")
		}
		if executions.Add(1) == 1 {
			close(started)
			select {
			case <-finish:
			case <-ctx.Done():
				return RunResult{}, ctx.Err()
			}
		}
		return RunResult{}, publishManagerTestOutput(directory, 188)
	}
	options.PlaybackAdmission = func(ctx context.Context, spec Spec) (context.Context, func(), error) {
		admissions.Add(1)
		references.Add(1)
		return context.WithValue(ctx, managerPlaybackContextKey{}, spec.Scope.PlaySessionID), func() { references.Add(-1) }, nil
	}
	options.PlaybackStopped = func(Spec) bool { return false }
	m := newTestManager(t, options)
	t.Cleanup(releaseRunner)
	request, cancelRequest := context.WithCancel(context.Background())
	t.Cleanup(cancelRequest)
	input := managerTestInput(t)
	first, err := m.Ensure(request, managerTestSpec(1), input)
	if err != nil {
		t.Fatal(err)
	}
	managerAccessWait(t, started, "the manager-owned runner")
	cancelRequest()
	m.mu.Lock()
	job := m.jobs[first.ID]
	jobError := job.ctx.Err()
	marker := job.ctx.Value(managerPlaybackContextKey{})
	m.mu.Unlock()
	if jobError != nil || marker != nil || references.Load() != 0 {
		t.Fatal("normal Ensure return or request cancellation changed the job lifetime")
	}
	if _, err := input.Stat(); err != nil {
		t.Fatal("operation reference release closed an input still owned by the runner")
	}
	releaseRunner()
	managerTestWaitFinished(t, m, first.ID)
	duplicate := managerTestInput(t)
	if reused, err := m.Ensure(context.Background(), first.Spec, duplicate); err != nil || reused.ID != first.ID {
		t.Fatalf("admission hooks broke exact reuse: %+v %v", reused, err)
	}
	assertManagerInputClosed(t, duplicate)
	retentionTestComplete(t, m, 2)
	m.mu.Lock()
	m.jobs[first.ID].record.LastAccessAt = time.Now().UTC().Add(-time.Minute)
	m.mu.Unlock()
	retentionTestComplete(t, m, 3)
	if references.Load() != 0 || admissions.Load() != 4 || executions.Load() != 3 {
		t.Fatalf("filesystem retention retry reacquired its operation: references=%d admissions=%d executions=%d", references.Load(), admissions.Load(), executions.Load())
	}
}

func TestManagerPlaybackSchedulerCancelsStoppedQueuedJobAndLeavesCancelAvailable(t *testing.T) {
	var stopped atomic.Bool
	var references, admissions, checks atomic.Int32
	started := make(chan int64, 2)
	options := managerAccessOptions(t, func(ctx context.Context, _, _ string, _ *os.File, plan Plan, _ int, _ func(Progress)) (RunResult, error) {
		started <- plan.StartTicks
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	options.MaxJobs, options.MaxRetainedJobs, options.MaxQueueJobs = 1, 2, 1
	options.MaxUserJobs, options.MaxSessionJobs = 1, 1
	options.PlaybackAdmission = func(ctx context.Context, _ Spec) (context.Context, func(), error) {
		admissions.Add(1)
		references.Add(1)
		return ctx, func() { references.Add(-1) }, nil
	}
	options.PlaybackStopped = func(Spec) bool { checks.Add(1); return stopped.Load() }
	m := newTestManager(t, options)
	firstSpec, queuedSpec := managerTestSpec(1), managerTestSpec(2)
	firstSpec.Plan.StartTicks, queuedSpec.Plan.StartTicks = 1, 2
	firstInput, queuedInput := managerTestInput(t), managerTestInput(t)
	first, err := m.Ensure(context.Background(), firstSpec, firstInput)
	if err != nil {
		t.Fatal(err)
	}
	if value := managerTestWaitStart(t, started); value != 1 {
		t.Fatal("the first actual runner did not occupy execution")
	}
	queued, err := m.Ensure(context.Background(), queuedSpec, queuedInput)
	if err != nil {
		t.Fatal(err)
	}
	stopped.Store(true)
	m.schedule()
	finished := managerTestWaitFinished(t, m, queued.ID)
	if finished.State != "cancelled" {
		t.Fatalf("scheduler launched or retained a stopped queued job: %+v", finished)
	}
	select {
	case value := <-started:
		t.Fatalf("stopped queued runner started: %d", value)
	default:
	}
	assertManagerInputClosed(t, queuedInput)
	before := checks.Load()
	if record, err := m.Snapshot(first.Spec.Scope, first.ID); err != nil || record.ID != first.ID {
		t.Fatalf("generic lookup was filtered by stopped admission: %+v %v", record, err)
	}
	if err := m.CancelJob(first.ID, first.Spec.Scope); err != nil {
		t.Fatal(err)
	}
	managerTestWaitFinished(t, m, first.ID)
	if checks.Load() != before || references.Load() != 0 || admissions.Load() != 2 {
		t.Fatal("generic cancellation reran admission or leaked operation references")
	}
}

func TestManagerPlaybackInvalidHookConsumesInputAndReleasesKnownReference(t *testing.T) {
	for _, mode := range []string{"nil context", "nil release", "returned rejection"} {
		t.Run(mode, func(t *testing.T) {
			var releases atomic.Int32
			options := managerAccessOptions(t, nil)
			options.PlaybackAdmission = func(ctx context.Context, _ Spec) (context.Context, func(), error) {
				release := func() { releases.Add(1) }
				switch mode {
				case "nil context":
					return nil, release, nil
				case "nil release":
					return ctx, nil, nil
				default:
					return ctx, release, ErrJobCancelled
				}
			}
			m := newTestManager(t, options)
			input := managerTestInput(t)
			_, err := m.Ensure(context.Background(), managerTestSpec(1), input)
			want := ErrInvalidOptions
			if mode == "returned rejection" {
				want = ErrJobCancelled
			}
			if !errors.Is(err, want) {
				t.Fatalf("invalid hook return was admitted: %v", err)
			}
			assertManagerInputClosed(t, input)
			wantReleases := int32(1)
			if mode == "nil release" {
				wantReleases = 0
			}
			if releases.Load() != wantReleases {
				t.Fatal("known hook rejection did not retire its returned operation reference once")
			}
		})
	}
}

func TestManagerPlaybackHookAbnormalExitUnlocksAndRetainsUnknownReference(t *testing.T) {
	for _, stage := range []string{"admission", "stopped", "release"} {
		for _, mode := range []string{"panic", "goexit"} {
			t.Run(stage+"/"+mode, func(t *testing.T) {
				var references, releases atomic.Int32
				options := managerAccessOptions(t, nil)
				options.PlaybackAdmission = func(ctx context.Context, _ Spec) (context.Context, func(), error) {
					references.Add(1)
					if stage == "admission" {
						managerPlaybackAbnormalExit(mode)
					}
					return ctx, func() {
						if stage == "release" {
							managerPlaybackAbnormalExit(mode)
						}
						releases.Add(1)
						references.Add(-1)
					}, nil
				}
				options.PlaybackStopped = func(Spec) bool {
					if stage == "stopped" {
						managerPlaybackAbnormalExit(mode)
					}
					return stage == "release"
				}
				m, err := NewManager(context.Background(), options)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					// A regression that strands the mutex is unknown ownership;
					// never make cleanup itself an unbounded attempt at that lock.
					if !managerPlaybackTryLock(m) {
						t.Error("manager mutex remains owned after callback exit")
						return
					}
					m.mu.Unlock()
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := m.Close(ctx); err != nil {
						t.Errorf("close callback fixture: %v", err)
					}
				})
				input := managerTestInput(t)
				result := make(chan managerPlaybackObservation, 1)
				go func() {
					observed := managerPlaybackObservation{}
					defer func() { observed.panicValue = recover(); result <- observed }()
					observed.record, observed.err = m.Ensure(context.Background(), managerTestSpec(1), input)
					observed.returned = true
				}()
				observed := managerPlaybackReceive(t, result)
				if observed.returned || mode == "panic" && observed.panicValue != "playback hook exit" || mode == "goexit" && observed.panicValue != nil {
					t.Fatalf("abnormal hook became normal completion: %+v", observed)
				}
				if !managerPlaybackTryLock(m) {
					t.Fatal("callback exit stranded the real manager mutex")
				}
				fenced := m.cacheFailed
				m.mu.Unlock()
				if !fenced || references.Load() != 1 || releases.Load() != 0 {
					t.Fatal("abnormal callback returned unknown operation ownership")
				}
				assertManagerInputClosed(t, input)
			})
		}
	}
}

func TestManagerNilPlaybackHooksPreserveLegacyReuse(t *testing.T) {
	options := retentionTestOptions(t)
	m := newTestManager(t, options)
	first := retentionTestComplete(t, m, 1)
	input := managerTestInput(t)
	if record, err := m.Ensure(context.Background(), first.Spec, input); err != nil || record.ID != first.ID {
		t.Fatalf("nil hooks changed legacy reuse: %+v %v", record, err)
	}
	assertManagerInputClosed(t, input)
}

type managerPlaybackObservation struct {
	record     Record
	err        error
	panicValue any
	returned   bool
}

func managerPlaybackEnsureAsync(m *Manager, spec Spec, input *os.File) <-chan managerPlaybackObservation {
	result := make(chan managerPlaybackObservation, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		record, err := m.Ensure(ctx, spec, input)
		result <- managerPlaybackObservation{record: record, err: err, returned: true}
	}()
	return result
}

// Done is first requested by this duplicate's real created-channel select.
// Admission only preserves its actual parent context; it does not call Done or
// use this event as authorization, join or completion evidence.
type managerPlaybackDedupContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (ctx *managerPlaybackDedupContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.waiting) })
	return ctx.Context.Done()
}

func managerPlaybackEnsureContextAsync(m *Manager, ctx context.Context, spec Spec, input *os.File) <-chan managerPlaybackObservation {
	result := make(chan managerPlaybackObservation, 1)
	go func() {
		record, err := m.Ensure(ctx, spec, input)
		result <- managerPlaybackObservation{record: record, err: err, returned: true}
	}()
	return result
}

func managerPlaybackReceive(t *testing.T, result <-chan managerPlaybackObservation) managerPlaybackObservation {
	t.Helper()
	select {
	case observed := <-result:
		return observed
	case <-time.After(6 * time.Second):
		t.Fatal("playback operation did not reach its bounded observation")
		return managerPlaybackObservation{}
	}
}

func managerPlaybackAssertPending(t *testing.T, result <-chan managerPlaybackObservation) {
	t.Helper()
	select {
	case observed := <-result:
		t.Fatalf("blocked ownership operation returned early: %+v", observed)
	default:
	}
}

func managerPlaybackAbnormalExit(mode string) {
	if mode == "panic" {
		panic("playback hook exit")
	}
	runtime.Goexit()
}

func managerPlaybackTryLock(m *Manager) bool {
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for {
		if m.mu.TryLock() {
			return true
		}
		select {
		case <-deadline.C:
			return false
		case <-poll.C:
		}
	}
}

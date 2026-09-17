package media

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// These fakes exercise ownership and synchronization only. They create no
// process, cgroup, tool, media input, or runtime-success evidence.
type diagnosticFakeExecutionBackend struct {
	mu                                sync.Mutex
	executions, interruptions, closes int
	executeFn                         func(context.Context, DiagnosticSelection, func(context.Context) error, func(DiagnosticReport)) (DiagnosticReport, error)
	closeFn                           func() error
}

func (backend *diagnosticFakeExecutionBackend) execute(ctx context.Context, selection DiagnosticSelection, authorize func(context.Context) error, progress func(DiagnosticReport)) (DiagnosticReport, error) {
	backend.mu.Lock()
	backend.executions++
	backend.mu.Unlock()
	if backend.executeFn != nil {
		return backend.executeFn(ctx, selection, authorize, progress)
	}
	return DiagnosticReport{State: "stages_complete"}, nil
}

func (backend *diagnosticFakeExecutionBackend) interrupt() {
	backend.mu.Lock()
	backend.interruptions++
	backend.mu.Unlock()
}

func (backend *diagnosticFakeExecutionBackend) close() error {
	backend.mu.Lock()
	backend.closes++
	backend.mu.Unlock()
	if backend.closeFn != nil {
		return backend.closeFn()
	}
	return nil
}

func (backend *diagnosticFakeExecutionBackend) counts() (int, int, int) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.executions, backend.interruptions, backend.closes
}

func diagnosticTestExecution(t *testing.T, backend *diagnosticFakeExecutionBackend) *DiagnosticExecution {
	t.Helper()
	owner, err := newDiagnosticExecution(context.Background(), DiagnosticExecutionOptions{}, func(context.Context, diagnosticProcessOptions) (diagnosticExecutionBackend, error) {
		return backend, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func diagnosticExecutionWait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("finite ownership fixture did not finish")
	}
}

func TestDiagnosticExecutionRunsOnceAndDoesNotFinalizeClosure(t *testing.T) {
	backend := &diagnosticFakeExecutionBackend{executeFn: func(_ context.Context, _ DiagnosticSelection, _ func(context.Context) error, progress func(DiagnosticReport)) (DiagnosticReport, error) {
		progress(DiagnosticReport{State: "preparing"})
		return DiagnosticReport{State: "stages_complete"}, nil
	}}
	owner := diagnosticTestExecution(t, backend)
	defer owner.Close()
	emissions := 0
	report, err := owner.Run(DiagnosticSelection{}, func(context.Context) error { return nil }, func(snapshot DiagnosticReport) {
		emissions++
		if !snapshot.SessionClosureRequired {
			t.Fatal("progress bypassed the owning session closure")
		}
	})
	if err != nil || report.State != "stages_complete" || !report.SessionClosureRequired || emissions != 1 {
		t.Fatal("run lost its provisional report")
	}
	if _, _, closes := backend.counts(); closes != 0 {
		t.Fatal("Run released the session without explicit owner closure")
	}
	if _, err = owner.Run(DiagnosticSelection{}, nil, nil); !errors.Is(err, ErrDiagnosticAlreadyRun) {
		t.Fatal("completed execution ran again")
	}
	if err = owner.Close(); err != nil {
		t.Fatal(err)
	}
	if !report.SessionClosureRequired {
		t.Fatal("Close rewrote a retained report into final acceptance")
	}
	if err = owner.Close(); err != nil {
		t.Fatal(err)
	}
	executions, _, closes := backend.counts()
	if executions != 1 || closes != 1 {
		t.Fatal("single-run or idempotent-close boundary changed")
	}
}

func TestDiagnosticExecutionConcurrentRunIsRejectedAndCancelInterruptsOwner(t *testing.T) {
	entered, done := make(chan struct{}), make(chan struct{})
	backend := &diagnosticFakeExecutionBackend{executeFn: func(ctx context.Context, _ DiagnosticSelection, _ func(context.Context) error, _ func(DiagnosticReport)) (DiagnosticReport, error) {
		close(entered)
		<-ctx.Done()
		return DiagnosticReport{State: "cancelled"}, ctx.Err()
	}}
	owner := diagnosticTestExecution(t, backend)
	defer owner.Close()
	go func() {
		defer close(done)
		report, err := owner.Run(DiagnosticSelection{}, nil, nil)
		if !errors.Is(err, context.Canceled) || !report.SessionClosureRequired {
			t.Error("cancelled run lost context or closure ownership")
		}
	}()
	diagnosticExecutionWait(t, entered)
	if _, err := owner.Run(DiagnosticSelection{}, nil, nil); !errors.Is(err, ErrDiagnosticAlreadyRun) {
		t.Fatal("concurrent second run was admitted")
	}
	owner.Cancel()
	diagnosticExecutionWait(t, done)
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	executions, interruptions, closes := backend.counts()
	if executions != 1 || interruptions == 0 || closes != 1 {
		t.Fatal("cancellation did not preserve one resource owner")
	}
}

func TestDiagnosticExecutionCancelledBeforeRunNeverDispatches(t *testing.T) {
	backend := &diagnosticFakeExecutionBackend{}
	owner := diagnosticTestExecution(t, backend)
	defer owner.Close()
	owner.Cancel()
	report, err := owner.Run(DiagnosticSelection{}, nil, nil)
	if !errors.Is(err, context.Canceled) || report.State != "cancelled" || !report.SessionClosureRequired {
		t.Fatal("pre-run cancellation was not retained")
	}
	if _, err = owner.Run(DiagnosticSelection{}, nil, nil); !errors.Is(err, ErrDiagnosticAlreadyRun) {
		t.Fatal("a rejected run did not consume the single-use owner")
	}
	if executions, _, _ := backend.counts(); executions != 0 {
		t.Fatal("cancelled owner dispatched stage work")
	}
}

func TestDiagnosticExecutionCloseWaitsForRunAndRetainsTimedOutOwnership(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	backend := &diagnosticFakeExecutionBackend{executeFn: func(context.Context, DiagnosticSelection, func(context.Context) error, func(DiagnosticReport)) (DiagnosticReport, error) {
		close(entered)
		<-release
		return DiagnosticReport{State: "cancelled"}, context.Canceled
	}}
	owner := diagnosticTestExecution(t, backend)
	defer func() { releaseOnce.Do(func() { close(release) }); owner.Close() }()
	go func() { defer close(done); _, _ = owner.Run(DiagnosticSelection{}, nil, nil) }()
	diagnosticExecutionWait(t, entered)
	if err := owner.closeWithin(time.Millisecond); !errors.Is(err, ErrDiagnosticClosure) {
		t.Fatal("blocked Run was incorrectly reported closed")
	}
	if _, _, closes := backend.counts(); closes != 0 {
		t.Fatal("session closed while Run still owned its inputs and command")
	}
	owner.mu.Lock()
	retained := owner.backend != nil && !owner.closed
	owner.mu.Unlock()
	if !retained {
		t.Fatal("timed-out Run lost resource ownership")
	}
	releaseOnce.Do(func() { close(release) })
	diagnosticExecutionWait(t, done)
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnosticExecutionCloseJoinsOnePendingAttempt(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	backend := &diagnosticFakeExecutionBackend{closeFn: func() error {
		close(entered)
		<-release
		return nil
	}}
	owner := diagnosticTestExecution(t, backend)
	defer func() { releaseOnce.Do(func() { close(release) }); owner.Close() }()
	if err := owner.closeWithin(time.Millisecond); !errors.Is(err, ErrDiagnosticClosure) {
		t.Fatal("unfinished backend close was accepted")
	}
	diagnosticExecutionWait(t, entered)
	if err := owner.closeWithin(time.Millisecond); !errors.Is(err, ErrDiagnosticClosure) {
		t.Fatal("retry did not retain the pending backend close")
	}
	if _, _, closes := backend.counts(); closes != 1 {
		t.Fatal("overlapping backend close attempts were started")
	}
	releaseOnce.Do(func() { close(release) })
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, closes := backend.counts(); closes != 1 {
		t.Fatal("completed pending close was not reused")
	}
}

func TestDiagnosticExecutionPartialInitializationRetainsFailedCloseForRetry(t *testing.T) {
	attempts := 0
	backend := &diagnosticFakeExecutionBackend{closeFn: func() error {
		attempts++
		if attempts == 1 {
			return errors.New("private-close-detail")
		}
		return nil
	}}
	owner, err := newDiagnosticExecution(context.Background(), DiagnosticExecutionOptions{}, func(context.Context, diagnosticProcessOptions) (diagnosticExecutionBackend, error) {
		return backend, ErrDiagnosticResources
	})
	if owner == nil || !errors.Is(err, ErrDiagnosticResources) {
		t.Fatal("partial initialization lost its cleanup owner")
	}
	defer owner.Close()
	report, err := owner.Run(DiagnosticSelection{}, nil, nil)
	if !errors.Is(err, ErrDiagnosticResources) || !report.SessionClosureRequired {
		t.Fatal("partially initialized owner became executable")
	}
	if err = owner.Close(); err != ErrDiagnosticClosure {
		t.Fatal("close error leaked detail or claimed closure")
	}
	owner.mu.Lock()
	retained := owner.backend != nil && !owner.closed
	owner.mu.Unlock()
	if !retained {
		t.Fatal("failed close discarded partial initialization resources")
	}
	if err = owner.Close(); err != nil {
		t.Fatal(err)
	}
	executions, interruptions, closes := backend.counts()
	if executions != 0 || interruptions == 0 || closes != 2 {
		t.Fatal("partial initialization did not follow explicit retry closure")
	}
}

func TestDiagnosticExecutionCopiesOptionsAndRejectsMissingOrOversizedInitialization(t *testing.T) {
	options := DiagnosticExecutionOptions{FFmpegPath: "/opt/ffmpeg", CgroupParent: "/delegated", ScratchDirectory: "/readonly",
		LoaderDirectories: []string{"/opt/lib"}, HardwareEnvironment: map[string]string{"LIBVA_DRIVER_NAME": "iHD"}}
	var copied diagnosticProcessOptions
	owner, err := newDiagnosticExecution(context.Background(), options, func(_ context.Context, actual diagnosticProcessOptions) (diagnosticExecutionBackend, error) {
		copied = actual
		return &diagnosticFakeExecutionBackend{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	options.LoaderDirectories[0] = "/changed"
	options.HardwareEnvironment["LIBVA_DRIVER_NAME"] = "changed"
	if copied.FFmpegPath != "/opt/ffmpeg" || copied.CgroupParent != "/delegated" || copied.ScratchDirectory != "/readonly" || copied.LoaderDirectories[0] != "/opt/lib" || copied.HardwareEnvironment["LIBVA_DRIVER_NAME"] != "iHD" {
		t.Fatal("caller mutation changed the admitted process options")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	open := func(context.Context, diagnosticProcessOptions) (diagnosticExecutionBackend, error) {
		calls++
		return nil, ErrDiagnosticTool
	}
	if result, err := newDiagnosticExecution(ctx, DiagnosticExecutionOptions{}, open); result != nil || !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("cancelled initialization reached the process factory")
	}
	if result, err := newDiagnosticExecution(context.Background(), DiagnosticExecutionOptions{LoaderDirectories: make([]string, 17)}, open); result != nil || !errors.Is(err, ErrDiagnosticResources) || calls != 0 {
		t.Fatal("unbounded option collection reached the process factory")
	}
	if result, err := newDiagnosticExecution(context.Background(), DiagnosticExecutionOptions{}, open); result != nil || !errors.Is(err, ErrDiagnosticTool) || calls != 1 {
		t.Fatal("fully closed initialization failure created an owner")
	}
}

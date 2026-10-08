//go:build linux

package transcode

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

type managerSourceReadContextKey struct{}

type managerSourceReadFixture struct {
	input    *os.File
	lifetime context.Context
	active   atomic.Int32
	phases   atomic.Int32
	closes   atomic.Int32
	closeErr error
	badClose atomic.Bool
}

func (read *managerSourceReadFixture) Context(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, managerSourceReadContextKey{}, read)
	return media.WithSourceReadPhase(ctx, func(work context.Context, consume func(context.Context) error) error {
		read.phases.Add(1)
		read.active.Add(1)
		defer read.active.Add(-1)
		return consume(work)
	})
}

func (read *managerSourceReadFixture) Close() error {
	read.closes.Add(1)
	_, err := read.input.Stat()
	if !errors.Is(err, os.ErrClosed) || read.active.Load() != 0 {
		read.badClose.Store(true)
		return errors.New("source owner closed before its actual readers retired")
	}
	return read.closeErr
}

func TestManagerSourceReadRetainsQueuedAndRunningJobsAfterRequest(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(finish) }) }
	prepared := make(chan *managerSourceReadFixture, 2)
	options := managerTestOptions(t, func(ctx context.Context, _, directory string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		read, ok := ctx.Value(managerSourceReadContextKey{}).(*managerSourceReadFixture)
		if !ok || read.lifetime.Err() != nil {
			return RunResult{}, errors.New("runner lost its retained source lifetime")
		}
		err := media.RunSourceReadPhase(ctx, func(work context.Context) error {
			close(started)
			select {
			case <-finish:
				return publishManagerTestOutput(directory, 188)
			case <-work.Done():
				return work.Err()
			}
		})
		return RunResult{}, err
	})
	options.MaxJobs = 1
	options.SourceRead = func(ctx, lifetime context.Context, _ Spec, input *os.File) (SourceReadLifetime, error) {
		if ctx == lifetime {
			return nil, errors.New("source preparation reused the request lifetime")
		}
		read := &managerSourceReadFixture{input: input, lifetime: lifetime}
		prepared <- read
		return read, nil
	}
	m := newTestManager(t, options)
	t.Cleanup(release)
	request, cancelRequest := context.WithCancel(context.Background())
	t.Cleanup(cancelRequest)
	first, err := m.Ensure(request, managerTestSpec(1), managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	firstRead := <-prepared
	managerAccessWait(t, started, "the source-admitted runner")
	second, err := m.Ensure(request, managerTestSpec(2), managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	secondRead := <-prepared
	cancelRequest()
	if firstRead.lifetime.Err() != nil || secondRead.lifetime.Err() != nil || firstRead.active.Load() != 1 ||
		secondRead.phases.Load() != 0 || firstRead.closes.Load() != 0 || secondRead.closes.Load() != 0 {
		t.Fatal("request completion retired a job source or charged an idle queued reader")
	}
	if err := m.CancelJob(second.ID, second.Spec.Scope); err != nil {
		t.Fatal(err)
	}
	if record := managerTestWaitFinished(t, m, second.ID); record.State != "cancelled" {
		t.Fatalf("queued cancellation did not finish: %+v", record)
	}
	if secondRead.phases.Load() != 0 || secondRead.closes.Load() != 1 || secondRead.badClose.Load() || secondRead.lifetime.Err() == nil {
		t.Fatal("queued cancellation did not retire its idle input exactly once")
	}
	if firstRead.active.Load() != 1 || firstRead.closes.Load() != 0 {
		t.Fatal("queued cancellation changed the running source owner")
	}
	release()
	if record := managerTestWaitFinished(t, m, first.ID); record.State != "completed" {
		t.Fatalf("request completion cancelled the independent job: %+v", record)
	}
	if firstRead.phases.Load() != 1 || firstRead.closes.Load() != 1 || firstRead.badClose.Load() || firstRead.lifetime.Err() == nil {
		t.Fatal("the completed source was not closed after its final actual reader")
	}
}

func TestManagerSourceReadClosesUnusedDuplicateAndRejectedInputs(t *testing.T) {
	prepared := make(chan *managerSourceReadFixture, 3)
	preparationErr := errors.New("source preparation rejected")
	options := managerTestOptions(t, func(ctx context.Context, _, directory string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		return RunResult{}, media.RunSourceReadPhase(ctx, func(context.Context) error {
			return publishManagerTestOutput(directory, 188)
		})
	})
	options.SourceRead = func(_ context.Context, lifetime context.Context, spec Spec, input *os.File) (SourceReadLifetime, error) {
		read := &managerSourceReadFixture{input: input, lifetime: lifetime}
		prepared <- read
		if spec.Scope.PlaySessionID == "play-2" {
			return read, preparationErr
		}
		return read, nil
	}
	m := newTestManager(t, options)
	first, err := m.Ensure(context.Background(), managerTestSpec(1), managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	firstRead := <-prepared
	if record := managerTestWaitFinished(t, m, first.ID); record.State != "completed" {
		t.Fatalf("initial job failed: %+v", record)
	}
	duplicate, err := m.Ensure(context.Background(), managerTestSpec(1), managerTestInput(t))
	if err != nil || duplicate.ID != first.ID {
		t.Fatalf("existing job was not reused: %+v %v", duplicate, err)
	}
	duplicateRead := <-prepared
	if _, err := m.Ensure(context.Background(), managerTestSpec(2), managerTestInput(t)); !errors.Is(err, preparationErr) {
		t.Fatalf("preparation failure changed: %v", err)
	}
	rejectedRead := <-prepared
	for _, read := range []*managerSourceReadFixture{firstRead, duplicateRead, rejectedRead} {
		if read.closes.Load() != 1 || read.badClose.Load() || read.lifetime.Err() == nil {
			t.Fatal("an unused or completed source escaped final input retirement")
		}
	}
	if duplicateRead.phases.Load() != 0 || rejectedRead.phases.Load() != 0 {
		t.Fatal("unused inputs acquired actual-reader phases")
	}
}

func TestManagerSourceReadCancellationWaitsForActualReaderRetirement(t *testing.T) {
	started, cancelled, retired := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(retired) }) }
	var read *managerSourceReadFixture
	options := managerTestOptions(t, func(ctx context.Context, _, _ string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		return RunResult{}, media.RunSourceReadPhase(ctx, func(work context.Context) error {
			close(started)
			<-work.Done()
			close(cancelled)
			<-retired
			return work.Err()
		})
	})
	options.SourceRead = func(_ context.Context, lifetime context.Context, _ Spec, input *os.File) (SourceReadLifetime, error) {
		read = &managerSourceReadFixture{input: input, lifetime: lifetime}
		return read, nil
	}
	m := newTestManager(t, options)
	t.Cleanup(release)
	input := managerTestInput(t)
	record, err := m.Ensure(context.Background(), managerTestSpec(1), input)
	if err != nil {
		t.Fatal(err)
	}
	managerAccessWait(t, started, "the active source reader")
	if err := m.CancelJob(record.ID, record.Spec.Scope); err != nil {
		t.Fatal(err)
	}
	managerAccessWait(t, cancelled, "the cancelled source reader")
	if read.active.Load() != 1 || read.closes.Load() != 0 {
		t.Fatal("cancellation released an actual source reader before retirement")
	}
	if _, err := input.Stat(); err != nil {
		t.Fatalf("cancellation closed a descriptor still owned by the runner: %v", err)
	}
	release()
	if finished := managerTestWaitFinished(t, m, record.ID); finished.State != "cancelled" {
		t.Fatalf("retired cancelled job did not finish: %+v", finished)
	}
	if read.active.Load() != 0 || read.closes.Load() != 1 || read.badClose.Load() {
		t.Fatal("actual reader retirement did not release the retained source")
	}
}

func TestManagerSourceReadSkipsDynamicStreams(t *testing.T) {
	var preparations atomic.Int32
	options := managerTestOptions(t, func(ctx context.Context, _, _ string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		if ctx.Value(managerSourceReadContextKey{}) != nil {
			return RunResult{}, errors.New("dynamic input inherited a local-file reader")
		}
		return RunResult{}, nil
	})
	options.SourceRead = func(context.Context, context.Context, Spec, *os.File) (SourceReadLifetime, error) {
		preparations.Add(1)
		return nil, errors.New("dynamic input used local-file admission")
	}
	options.LivePublish = func(context.Context, Spec, string, LiveSegment) error { return nil }
	m := newTestManager(t, options)
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	spec := managerTestSpec(1)
	spec.Plan = liveTestPlan()
	record, err := m.EnsureStream(context.Background(), spec, input)
	if err != nil {
		t.Fatal(err)
	}
	managerTestWaitFinished(t, m, record.ID)
	assertManagerInputClosed(t, input)
	if preparations.Load() != 0 {
		t.Fatal("dynamic stream invoked the local-file SourceRead adapter")
	}
}

func TestManagerSourceReadUnknownRetirementKeepsFinalizationOwnership(t *testing.T) {
	m := newManagerFinalizationFixture(t, nil)
	j := addManagerFinalizationRunningJob(t, m)
	read := &managerSourceReadFixture{input: j.input, lifetime: j.ctx, closeErr: media.ErrProcessRetirementUnknown}
	j.sourceRead = read
	ticket := j.completion
	if err := m.enqueueFinalization(j, media.ErrProcessRetirementUnknown, nil); !errors.Is(err, media.ErrProcessRetirementUnknown) {
		t.Fatalf("unknown source retirement was discarded: %v", err)
	}
	if read.closes.Load() != 1 || read.badClose.Load() {
		t.Fatal("source lifetime closed before its owned input")
	}
	m.mu.Lock()
	held := m.cacheFailed && errors.Is(m.closeErr, ErrCacheUnsafe) && !j.finished && j.running &&
		j.subjectOwnershipHeld && m.running == 1 && j.completion == ticket && j.sourceRead == read
	m.mu.Unlock()
	if !held {
		t.Fatal("unknown source retirement released or detached finalization ownership")
	}
	select {
	case <-j.done:
		t.Fatal("unknown source retirement signalled job completion")
	default:
	}
}

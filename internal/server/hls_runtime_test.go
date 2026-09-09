package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

var errHLSRuntimeTestReleased = errors.New("controlled HLS output release")

type hlsRuntimeOpenCall struct {
	scope transcode.Scope
	id    string
	name  string
}

type hlsRuntimeTestJobs struct {
	mu      sync.Mutex
	ensured []transcode.Spec
	cancels []hlsRuntimeOpenCall
	opens   chan hlsRuntimeOpenCall
	release chan struct{}
	once    sync.Once
}

func newHLSRuntimeTestJobs() *hlsRuntimeTestJobs {
	return &hlsRuntimeTestJobs{opens: make(chan hlsRuntimeOpenCall, 3), release: make(chan struct{})}
}

func (jobs *hlsRuntimeTestJobs) Ensure(_ context.Context, spec transcode.Spec, input *os.File) (transcode.Record, error) {
	// Match the manager's ownership contract even though no encoder is started.
	if err := input.Close(); err != nil {
		return transcode.Record{}, err
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	jobs.ensured = append(jobs.ensured, spec)
	return transcode.Record{ID: fmt.Sprintf("runtime-producer-%d", len(jobs.ensured)), Spec: spec, State: "running"}, nil
}

func (*hlsRuntimeTestJobs) TryOpen(transcode.Scope, string, string) (*transcode.ReadHandle, error) {
	return nil, transcode.ErrOutputUnavailable
}

func (*hlsRuntimeTestJobs) Snapshot(_ transcode.Scope, id string) (transcode.Record, error) {
	return transcode.Record{ID: id, State: "running"}, nil
}

func (jobs *hlsRuntimeTestJobs) Open(ctx context.Context, scope transcode.Scope, id, name string) (*transcode.ReadHandle, error) {
	select {
	case jobs.opens <- hlsRuntimeOpenCall{scope: scope, id: id, name: name}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-jobs.release:
		return nil, errHLSRuntimeTestReleased
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (jobs *hlsRuntimeTestJobs) CancelJob(id string, scope transcode.Scope) error {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	jobs.cancels = append(jobs.cancels, hlsRuntimeOpenCall{id: id, scope: scope})
	return nil
}

func (jobs *hlsRuntimeTestJobs) Close(context.Context) error {
	jobs.releaseAll()
	return nil
}

func (jobs *hlsRuntimeTestJobs) releaseAll() {
	jobs.once.Do(func() { close(jobs.release) })
}

func hlsRuntimeTestFixture(t *testing.T) (*hlsRuntime, *hlsRuntimeTestJobs) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	jobs := newHLSRuntimeTestJobs()
	h := &hlsRuntime{manager: jobs, ctx: ctx, cancel: cancel, sessions: make(map[string]*hlsSession), byKey: make(map[hlsKey]*hlsSession)}
	t.Cleanup(func() { cancel(); jobs.releaseAll() })
	return h, jobs
}

func hlsRuntimeTestSession(t *testing.T, h *hlsRuntime, id string, active bool) *hlsSession {
	t.Helper()
	const segmentTicks = int64(60_000_000)
	scope := transcode.Scope{UserID: id + "-user", AuthSessionID: id + "-auth", DeviceID: id + "-device",
		PlaySessionID: id + "-play", ItemID: id + "-item", SourceID: id + "-source"}
	plan := transcode.Plan{Container: "ts", VideoCodec: "h264", AudioCodec: "aac", VideoStreamIndex: 0, AudioStreamIndex: 1,
		DurationTicks: 4 * segmentTicks, SegmentSeconds: 6}
	timeline := transcode.Timeline{TargetDuration: 6}
	for number := 0; number < 4; number++ {
		timeline.Segments = append(timeline.Segments, transcode.TimelineSegment{Number: number,
			StartTicks: int64(number) * segmentTicks, DurationTicks: segmentTicks})
	}
	ctx, cancel := context.WithCancel(h.ctx)
	session := &hlsSession{id: id, key: hlsKey{scope: scope, stamp: id + "-stamp", plan: plan}, timeline: &timeline,
		accessed: time.Now(), lastAsked: -1, ctx: ctx, cancel: cancel,
		principal: identity.Principal{User: identity.User{ID: scope.UserID}, SessionID: scope.AuthSessionID,
			Client: identity.Client{DeviceID: scope.DeviceID}, Kind: "emby"}}
	if active {
		session.producers = []hlsProducer{{id: id + "-producer", first: 0, last: 3}}
	}
	h.sessions[id], h.byKey[session.key] = session, session
	return session
}

func hlsRuntimeInput(t *testing.T) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "hls-runtime-input-")
	if err != nil {
		t.Fatalf("create controlled HLS input: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func TestHLSRuntimeNearbyOutOfOrderRequestsShareOneProducer(t *testing.T) {
	h, jobs := hlsRuntimeTestFixture(t)
	session := hlsRuntimeTestSession(t, h, "out-of-order", false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		jobs.releaseAll()
		done := make(chan struct{})
		go func() { workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("cancelled segment requests did not exit during cleanup")
		}
	})
	type outcome struct {
		number int
		err    error
	}
	results := make(chan outcome, 3)
	var inputs []*os.File
	for _, number := range []int{0, 2, 1} {
		input := hlsRuntimeInput(t)
		inputs = append(inputs, input)
		workers.Add(1)
		go func(number int, input *os.File) {
			defer workers.Done()
			_, err := h.segment(ctx, session, input, number)
			results <- outcome{number: number, err: err}
		}(number, input)
		// Start the next request only after this request is waiting for output,
		// keeping all three requests concurrent without scheduler-dependent sleeps.
		select {
		case call := <-jobs.opens:
			if call.id != "runtime-producer-1" || call.scope != session.key.scope || call.name != "segment-"+paddedSegmentNumber(number)+".ts" {
				t.Errorf("nearby request %d selected a different producer or scope: %+v", number, call)
			}
		case <-ctx.Done():
			t.Fatalf("segment %d did not reach the output wait: %v", number, ctx.Err())
		}
	}
	jobs.mu.Lock()
	ensured, cancelled := len(jobs.ensured), len(jobs.cancels)
	jobs.mu.Unlock()
	if ensured != 1 || cancelled != 0 {
		t.Errorf("nearby out-of-order reads started %d producers and cancelled %d, want 1 and 0", ensured, cancelled)
	}
	jobs.releaseAll()
	seen := make(map[int]bool)
	for range inputs {
		select {
		case result := <-results:
			if !errors.Is(result.err, errHLSRuntimeTestReleased) || seen[result.number] {
				t.Errorf("unexpected or duplicate segment completion: %+v", result)
			}
			seen[result.number] = true
		case <-ctx.Done():
			t.Fatalf("released HLS output wait did not complete: %v", ctx.Err())
		}
	}
	workers.Wait()
	for _, input := range inputs {
		if _, err := input.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Errorf("segment request retained its input descriptor: %v", err)
		}
	}
}

// hlsRuntimeExpiryContext provides a controllable deadline event. Advancing it
// produces DeadlineExceeded without waiting for an actual wall-clock timeout.
type hlsRuntimeExpiryContext struct {
	context.Context
	done chan struct{}
	once sync.Once
}

func (ctx *hlsRuntimeExpiryContext) Done() <-chan struct{} { return ctx.done }

func (ctx *hlsRuntimeExpiryContext) Err() error {
	select {
	case <-ctx.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (ctx *hlsRuntimeExpiryContext) expire() {
	ctx.once.Do(func() { close(ctx.done) })
}

func TestHLSRuntimeMaintenanceBudgetDoesNotRetireUnverifiedSessions(t *testing.T) {
	h, jobs := hlsRuntimeTestFixture(t)
	first := hlsRuntimeTestSession(t, h, "slow-check", true)
	second := hlsRuntimeTestSession(t, h, "unverified", true)
	cycle := &hlsRuntimeExpiryContext{Context: context.Background(), done: make(chan struct{})}
	entered, finished := make(chan struct{}), make(chan struct{})
	var verified []string
	h.verify = func(ctx context.Context, _ identity.Principal, scope transcode.Scope, _ string, _ transcode.Plan) (*os.File, library.MediaFile, error) {
		verified = append(verified, scope.PlaySessionID)
		if scope == first.key.scope {
			close(entered)
			// Wait for the controlled parent expiration before returning, even
			// if a heavily paused test process lets the child timer fire first.
			<-cycle.Done()
			<-ctx.Done()
			return nil, library.MediaFile{}, ctx.Err()
		}
		return nil, library.MediaFile{}, nil
	}
	go func() {
		defer close(finished)
		h.maintainSessions(cycle, []*hlsSession{first, second})
	}()
	t.Cleanup(func() {
		cycle.expire()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("cancelled maintenance did not exit during cleanup")
		}
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("maintenance did not begin its first verification")
	}
	cycle.expire()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("maintenance did not stop after exhausting its cycle budget")
	}
	if len(verified) != 1 || verified[0] != first.key.scope.PlaySessionID {
		t.Errorf("maintenance continued verification after its cycle expired: %v", verified)
	}
	if first.closed || second.closed || len(h.sessions) != 2 || len(h.byKey) != 2 {
		t.Error("a transient timeout retired a valid or unverified HLS session")
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if len(jobs.cancels) != 0 {
		t.Errorf("cycle expiration cancelled unrelated producers: %+v", jobs.cancels)
	}
}

func TestHLSRuntimeMaintenanceRetiresOnlyTheForbiddenSession(t *testing.T) {
	h, jobs := hlsRuntimeTestFixture(t)
	first := hlsRuntimeTestSession(t, h, "forbidden", true)
	second := hlsRuntimeTestSession(t, h, "permitted", true)
	verifiedFile := hlsRuntimeInput(t)
	var verified []string
	h.verify = func(_ context.Context, _ identity.Principal, scope transcode.Scope, _ string, _ transcode.Plan) (*os.File, library.MediaFile, error) {
		verified = append(verified, scope.PlaySessionID)
		if scope == first.key.scope {
			return nil, library.MediaFile{}, library.ErrForbidden
		}
		return verifiedFile, library.MediaFile{}, nil
	}
	h.maintainSessions(context.Background(), []*hlsSession{first, second})
	if len(verified) != 2 || verified[0] != first.key.scope.PlaySessionID || verified[1] != second.key.scope.PlaySessionID {
		t.Errorf("permanent denial prevented verification of the next session: %v", verified)
	}
	if !first.closed || first.ctx.Err() == nil || second.closed || second.ctx.Err() != nil ||
		h.sessions[first.id] != nil || h.byKey[first.key] != nil || h.sessions[second.id] != second || h.byKey[second.key] != second {
		t.Error("permanent denial retired the wrong session or left its registry entry active")
	}
	if _, err := verifiedFile.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("maintenance retained the successfully verified source descriptor: %v", err)
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if len(jobs.cancels) != 1 || jobs.cancels[0].id != first.producers[0].id || jobs.cancels[0].scope != first.key.scope {
		t.Errorf("permanent denial cancelled the wrong producer scope: %+v", jobs.cancels)
	}
}

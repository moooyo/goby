package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

type hlsAdmissionTestCall struct {
	id       string
	spec     transcode.Spec
	input    *os.File
	closeErr error
	err      error
	release  chan struct{}
	once     sync.Once
}

type hlsAdmissionObservedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (ctx *hlsAdmissionObservedContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.observed) })
	return ctx.Context.Done()
}

func hlsAdmissionObserve(ctx context.Context) *hlsAdmissionObservedContext {
	return &hlsAdmissionObservedContext{Context: ctx, observed: make(chan struct{})}
}

func hlsAdmissionJoined(t *testing.T, ctx *hlsAdmissionObservedContext) {
	t.Helper()
	select {
	case <-ctx.observed:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not enter the admission wait")
	}
}

type hlsAdmissionPausedContext struct {
	*hlsAdmissionObservedContext
	armed   atomic.Bool
	reached chan struct{}
	release chan struct{}
	once    sync.Once
}

func (ctx *hlsAdmissionPausedContext) Err() error {
	if ctx.armed.Load() {
		ctx.once.Do(func() { close(ctx.reached) })
		<-ctx.release
	}
	return ctx.Context.Err()
}

func (call *hlsAdmissionTestCall) finish() { call.once.Do(func() { close(call.release) }) }

type hlsAdmissionTestJobs struct {
	mu            sync.Mutex
	calls         []*hlsAdmissionTestCall
	records       map[string]transcode.Record
	bySpec        map[transcode.Spec]string
	events        []string
	ensures       chan *hlsAdmissionTestCall
	opens         chan hlsRuntimeOpenCall
	cancels       chan hlsRuntimeOpenCall
	closed        chan struct{}
	once          sync.Once
	finishing     bool
	normalize     func(transcode.Spec) transcode.Spec
	snapshotErr   error
	snapshotState string
}

func newHLSAdmissionTestJobs() *hlsAdmissionTestJobs {
	return &hlsAdmissionTestJobs{records: make(map[string]transcode.Record), bySpec: make(map[transcode.Spec]string),
		ensures: make(chan *hlsAdmissionTestCall, 64), opens: make(chan hlsRuntimeOpenCall, 64),
		cancels: make(chan hlsRuntimeOpenCall, 64), closed: make(chan struct{})}
}

func (jobs *hlsAdmissionTestJobs) Ensure(_ context.Context, spec transcode.Spec, input *os.File) (transcode.Record, error) {
	call := &hlsAdmissionTestCall{spec: spec, input: input, closeErr: input.Close(), release: make(chan struct{})}
	jobs.mu.Lock()
	normalized := spec
	if jobs.normalize != nil {
		normalized = jobs.normalize(spec)
	}
	call.id = jobs.bySpec[normalized]
	if call.id == "" {
		call.id = fmt.Sprintf("admission-job-%d", len(jobs.calls)+1)
		jobs.bySpec[normalized] = call.id
		jobs.records[call.id] = transcode.Record{ID: call.id, Spec: normalized, State: "running"}
	}
	jobs.calls = append(jobs.calls, call)
	if jobs.finishing {
		call.finish()
	}
	jobs.events = append(jobs.events, "ensure:"+call.id)
	record := jobs.records[call.id]
	jobs.mu.Unlock()
	jobs.ensures <- call
	// Deliberately ignore cancellation while blocked, as a slow filesystem
	// syscall can do. The runtime must stay responsive and fence the late ID.
	<-call.release
	return record, call.err
}

func (*hlsAdmissionTestJobs) TryOpen(transcode.Scope, string, string) (*transcode.ReadHandle, error) {
	return nil, transcode.ErrOutputUnavailable
}

func (jobs *hlsAdmissionTestJobs) Snapshot(scope transcode.Scope, id string) (transcode.Record, error) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	record, ok := jobs.records[id]
	if !ok || record.Spec.Scope != scope || record.State == "cancelled" {
		return transcode.Record{}, transcode.ErrJobNotFound
	}
	if jobs.snapshotState != "" {
		record.State = jobs.snapshotState
	}
	return record, jobs.snapshotErr
}

func (jobs *hlsAdmissionTestJobs) Open(ctx context.Context, scope transcode.Scope, id, name string) (*transcode.ReadHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := jobs.Snapshot(scope, id); err != nil {
		return nil, err
	}
	jobs.opens <- hlsRuntimeOpenCall{scope: scope, id: id, name: name}
	return nil, errHLSRuntimeTestReleased
}

func (jobs *hlsAdmissionTestJobs) CancelJob(id string, scope transcode.Scope) error {
	jobs.mu.Lock()
	record, ok := jobs.records[id]
	if !ok || record.Spec.Scope != scope || record.State == "cancelled" {
		jobs.mu.Unlock()
		return transcode.ErrJobNotFound
	}
	record.State = "cancelled"
	jobs.records[id] = record
	if jobs.bySpec[record.Spec] == id {
		delete(jobs.bySpec, record.Spec)
	}
	jobs.events = append(jobs.events, "cancel:"+id)
	jobs.mu.Unlock()
	jobs.cancels <- hlsRuntimeOpenCall{scope: scope, id: id}
	return nil
}

func (jobs *hlsAdmissionTestJobs) Close(context.Context) error {
	jobs.once.Do(func() { close(jobs.closed) })
	return nil
}

func (jobs *hlsAdmissionTestJobs) finishAll() {
	jobs.mu.Lock()
	jobs.finishing = true
	calls := append([]*hlsAdmissionTestCall(nil), jobs.calls...)
	jobs.mu.Unlock()
	for _, call := range calls {
		call.finish()
	}
}

func hlsAdmissionFixture(t *testing.T) (*hlsRuntime, *hlsAdmissionTestJobs) {
	t.Helper()
	h, _ := hlsRuntimeTestFixture(t)
	jobs := newHLSAdmissionTestJobs()
	h.manager = jobs
	t.Cleanup(func() {
		h.cancel()
		jobs.finishAll()
		done := make(chan struct{})
		go func() { h.workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("admission workers did not exit during cleanup")
		}
	})
	return h, jobs
}

func hlsAdmissionCall(t *testing.T, jobs *hlsAdmissionTestJobs) *hlsAdmissionTestCall {
	t.Helper()
	select {
	case call := <-jobs.ensures:
		if call.closeErr != nil {
			t.Fatalf("Ensure did not receive an independently owned open input: %v", call.closeErr)
		}
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("admission did not reach Ensure")
		return nil
	}
}

func hlsAdmissionResult(t *testing.T, result <-chan error, expected error) {
	t.Helper()
	select {
	case err := <-result:
		if !errors.Is(err, expected) {
			t.Fatalf("admission result = %v, want %v", err, expected)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admission request did not return")
	}
}

func hlsAdmissionSegmentRequest(t *testing.T, h *hlsRuntime, session *hlsSession, ctx context.Context, number int) (*os.File, <-chan error) {
	t.Helper()
	input := hlsRuntimeInput(t)
	result := make(chan error, 1)
	go func() { _, err := h.segment(ctx, session, input, number); result <- err }()
	return input, result
}

func TestHLSAdmissionSlowEnsureDoesNotBlockStopOrIndependentSession(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	slow := hlsRuntimeTestSession(t, h, "slow-admission", false)
	fast := hlsRuntimeTestSession(t, h, "independent-admission", false)
	_, slowResult := hlsAdmissionSegmentRequest(t, h, slow, context.Background(), 0)
	old := hlsAdmissionCall(t, jobs)
	stopped := make(chan struct{})
	go func() { h.retire(slow); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop waited for slow Ensure")
	}
	hlsAdmissionResult(t, slowResult, transcode.ErrJobNotFound)
	_, fastResult := hlsAdmissionSegmentRequest(t, h, fast, context.Background(), 0)
	next := hlsAdmissionCall(t, jobs)
	if next.spec.Scope != fast.key.scope {
		t.Fatal("independent session did not enter its own admission")
	}
	next.finish()
	hlsAdmissionResult(t, fastResult, errHLSRuntimeTestReleased)
	old.finish()
	select {
	case cancelled := <-jobs.cancels:
		if cancelled.id != old.id || cancelled.scope != slow.key.scope {
			t.Fatalf("late admission cancelled the wrong job: %+v", cancelled)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("late successful admission was not cancelled")
	}
	slow.mu.Lock()
	attached := len(slow.producers)
	slow.mu.Unlock()
	if attached != 0 {
		t.Fatal("retired session attached a late producer")
	}
}

func TestHLSAdmissionNearbyRequestsCoalesceAndCancelWaiters(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "coalesced-admission", false)
	firstInput, firstResult := hlsAdmissionSegmentRequest(t, h, session, context.Background(), 0)
	call := hlsAdmissionCall(t, jobs)
	waitCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := hlsAdmissionObserve(waitCtx)
	waitInput, waitResult := hlsAdmissionSegmentRequest(t, h, session, observed, 2)
	hlsAdmissionJoined(t, observed)
	cancel()
	hlsAdmissionResult(t, waitResult, context.Canceled)
	if _, err := waitInput.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("cancelled waiter retained its consumed input: %v", err)
	}
	nextCtx := hlsAdmissionObserve(context.Background())
	_, nextResult := hlsAdmissionSegmentRequest(t, h, session, nextCtx, 1)
	hlsAdmissionJoined(t, nextCtx)
	call.finish()
	hlsAdmissionResult(t, firstResult, errHLSRuntimeTestReleased)
	hlsAdmissionResult(t, nextResult, errHLSRuntimeTestReleased)
	jobs.mu.Lock()
	ensured := len(jobs.calls)
	jobs.mu.Unlock()
	if ensured != 1 {
		t.Fatalf("nearby requests created %d producers, want one", ensured)
	}
	if _, err := firstInput.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("producer retained its input after the fake manager consumed it: %v", err)
	}
}

func TestHLSAdmissionCancelledCreatorFencesLateRecordBeforeReplacement(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "cancelled-creator", false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, firstResult := hlsAdmissionSegmentRequest(t, h, session, ctx, 0)
	old := hlsAdmissionCall(t, jobs)
	cancel()
	hlsAdmissionResult(t, firstResult, context.Canceled)
	_, replacementResult := hlsAdmissionSegmentRequest(t, h, session, context.Background(), 0)
	old.finish()
	next := hlsAdmissionCall(t, jobs)
	if next.id == old.id {
		t.Fatal("replacement borrowed the cancelled creator's late record")
	}
	next.finish()
	hlsAdmissionResult(t, replacementResult, errHLSRuntimeTestReleased)
	jobs.mu.Lock()
	events := append([]string(nil), jobs.events...)
	jobs.mu.Unlock()
	if len(events) != 3 || events[0] != "ensure:"+old.id || events[1] != "cancel:"+old.id || events[2] != "ensure:"+next.id {
		t.Fatalf("replacement admission crossed the old cancellation fence: %v", events)
	}
}

func TestHLSAdmissionErrorWithRecordCancelsExactlyThatRecord(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "failed-create", false)
	_, result := hlsAdmissionSegmentRequest(t, h, session, context.Background(), 0)
	call := hlsAdmissionCall(t, jobs)
	call.err = transcode.ErrPersistence
	call.finish()
	hlsAdmissionResult(t, result, transcode.ErrPersistence)
	select {
	case cancelled := <-jobs.cancels:
		if cancelled.id != call.id || cancelled.scope != call.spec.Scope {
			t.Fatalf("error cleanup cancelled the wrong record: %+v", cancelled)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a record returned with an admission error was not cancelled")
	}
}

func TestHLSAdmissionReplacementRegistrationRetainsFenceAcrossEmptySession(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	original := hlsRuntimeTestSession(t, h, "registry-fence", false)
	original.principal.User.Policy = []byte("{}")
	original.principal.PeerIP = "127.0.0.1"
	_, oldResult := hlsAdmissionSegmentRequest(t, h, original, context.Background(), 0)
	old := hlsAdmissionCall(t, jobs)
	h.retire(original)
	hlsAdmissionResult(t, oldResult, transcode.ErrJobNotFound)
	source := library.MediaFile{Item: library.Item{ID: original.key.scope.ItemID}, SourceID: original.key.scope.SourceID, ETag: original.key.stamp}
	decision := playback.ConversionDecision{Plan: &original.key.plan}
	empty, err := h.register(original.principal, source, original.key.scope.PlaySessionID, decision, 0)
	if err != nil {
		t.Fatal(err)
	}
	h.retire(empty)
	replacement, err := h.register(original.principal, source, original.key.scope.PlaySessionID, decision, 0)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.key != original.key {
		t.Fatal("test replacement did not reproduce the exact registry key")
	}
	replacement.timeline = original.timeline
	_, replacementResult := hlsAdmissionSegmentRequest(t, h, replacement, context.Background(), 0)
	old.finish()
	next := hlsAdmissionCall(t, jobs)
	if next.id == old.id {
		t.Fatal("replacement session lost the retired session's admission fence")
	}
	next.finish()
	hlsAdmissionResult(t, replacementResult, errHLSRuntimeTestReleased)
}

func TestHLSAdmissionCloseWaitsForLateCreationAndReclaimsGates(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "closing-admission", false)
	_, result := hlsAdmissionSegmentRequest(t, h, session, context.Background(), 0)
	call := hlsAdmissionCall(t, jobs)
	closed := make(chan error, 1)
	go func() { closed <- h.Close(context.Background()) }()
	select {
	case <-jobs.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime Close did not reach the manager")
	}
	select {
	case <-closed:
		t.Fatal("runtime Close forgot an in-flight admission worker")
	default:
	}
	call.finish()
	hlsAdmissionResult(t, result, transcode.ErrJobNotFound)
	hlsAdmissionResult(t, closed, nil)
	h.mu.Lock()
	count, gates := h.admissions, len(h.admissionGates)
	h.mu.Unlock()
	if count != 0 || gates != 0 {
		t.Fatalf("finished runtime retained %d admissions and %d gates", count, gates)
	}
}

func hlsAdmissionWaitPending(t *testing.T, session *hlsSession, first int) *hlsAdmission {
	t.Helper()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		session.mu.Lock()
		pending := session.admission
		session.mu.Unlock()
		if pending != nil && pending.first == first {
			return pending
		}
		select {
		case <-tick.C:
		case <-timeout.C:
			t.Fatalf("request did not install admission for segment %d", first)
			return nil
		}
	}
}

func TestHLSAdmissionChainedSeekCannotBypassEarlierCancellationFence(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "chained-seek", false)
	const ticks = int64(60_000_000)
	for number := 4; number < 600; number++ {
		session.timeline.Segments = append(session.timeline.Segments, transcode.TimelineSegment{Number: number,
			StartTicks: int64(number) * ticks, DurationTicks: ticks})
	}
	session.key.plan.DurationTicks = 600 * ticks
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 1, "Playing", 0))
	_, firstResult := hlsAdmissionSegmentRequest(t, h, session, context.Background(), 0)
	old := hlsAdmissionCall(t, jobs)
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 2, "Playing", 300*ticks))
	_, middleResult := hlsAdmissionSegmentRequest(t, h, session, context.Background(), 300)
	middle := hlsAdmissionWaitPending(t, session, 300)
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 3, "Playing", 0))
	_, finalResult := hlsAdmissionSegmentRequest(t, h, session, context.Background(), 0)
	hlsAdmissionWaitPending(t, session, 0)
	hlsAdmissionResult(t, firstResult, context.Canceled)
	hlsAdmissionResult(t, middleResult, context.Canceled)
	// The cancelled gate waiter may complete before the initial syscall. Its
	// completion must not release the gate still owned by that initial syscall.
	select {
	case <-middle.done:
	case <-time.After(5 * time.Second):
		t.Fatal("superseded gate waiter did not exit")
	}
	select {
	case call := <-jobs.ensures:
		t.Fatalf("chained seek entered Ensure before the old fence: %+v", call.spec)
	default:
	}
	old.finish()
	next := hlsAdmissionCall(t, jobs)
	if next.id == old.id || next.spec != old.spec {
		t.Fatalf("final seek did not retry the old immutable spec with a new job: %+v", next)
	}
	next.finish()
	hlsAdmissionResult(t, finalResult, errHLSRuntimeTestReleased)
	h.workers.Wait()
	jobs.mu.Lock()
	events := append([]string(nil), jobs.events...)
	jobs.mu.Unlock()
	if len(events) != 3 || events[1] != "cancel:"+old.id || events[2] != "ensure:"+next.id {
		t.Fatalf("chained seek crossed the cancellation fence: %v", events)
	}
	if session.key.plan.SegmentMode != "" || session.key.plan.StartTicks != 0 {
		t.Fatal("seek changed the registered immutable output plan")
	}
}

func TestHLSAdmissionBoundsDetachedWorkAndReleasesUnusedReservations(t *testing.T) {
	h, _ := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "bounded-admission", false)
	var gates []*hlsAdmissionGate
	for range maxHLSAdmissions {
		gate, err := h.reserveAdmission(session.key)
		if err != nil {
			t.Fatal(err)
		}
		gates = append(gates, gate)
	}
	if _, err := h.reserveAdmission(session.key); !errors.Is(err, transcode.ErrBusy) {
		t.Fatalf("unbounded detached admission was allowed: %v", err)
	}
	for _, gate := range gates {
		h.releaseAdmission(session.key, gate)
	}
	h.mu.Lock()
	count, retained := h.admissions, len(h.admissionGates)
	h.mu.Unlock()
	if count != 0 || retained != 0 {
		t.Fatalf("released reservations retained %d workers and %d gates", count, retained)
	}
}

func TestHLSAdmissionActiveWaiterRetriesCancelledCreator(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "retrying-waiter", false)
	creatorCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, creatorResult := hlsAdmissionSegmentRequest(t, h, session, creatorCtx, 0)
	old := hlsAdmissionCall(t, jobs)
	waitCtx := hlsAdmissionObserve(context.Background())
	_, waiterResult := hlsAdmissionSegmentRequest(t, h, session, waitCtx, 1)
	hlsAdmissionJoined(t, waitCtx)
	cancel()
	hlsAdmissionResult(t, creatorResult, context.Canceled)
	hlsAdmissionWaitPending(t, session, 1)
	old.finish()
	next := hlsAdmissionCall(t, jobs)
	if next.id == old.id || next.spec.Plan.SegmentStartNumber != 1 {
		t.Fatalf("active waiter borrowed the abandoned record instead of retrying: %+v", next)
	}
	next.finish()
	hlsAdmissionResult(t, waiterResult, errHLSRuntimeTestReleased)
}

func TestHLSAdmissionTransportLocationCannotSplitCancellationFence(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	original := hlsRuntimeTestSession(t, h, "transport-fence", false)
	original.principal.User.Policy = []byte("{}")
	original.principal.PeerIP = "127.0.0.1"
	_, oldResult := hlsAdmissionSegmentRequest(t, h, original, context.Background(), 0)
	old := hlsAdmissionCall(t, jobs)
	h.retire(original)
	hlsAdmissionResult(t, oldResult, transcode.ErrJobNotFound)
	principal := original.principal
	principal.PeerIP = "203.0.113.10"
	source := library.MediaFile{Item: library.Item{ID: original.key.scope.ItemID}, SourceID: original.key.scope.SourceID, ETag: original.key.stamp}
	decision := playback.ConversionDecision{Plan: &original.key.plan}
	replacement, err := h.register(principal, source, original.key.scope.PlaySessionID, decision, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !replacement.key.remote || original.key.remote {
		t.Fatal("test did not change only the transport classification")
	}
	replacement.timeline = original.timeline
	_, nextResult := hlsAdmissionSegmentRequest(t, h, replacement, context.Background(), 0)
	hlsAdmissionWaitPending(t, replacement, 0)
	select {
	case call := <-jobs.ensures:
		t.Fatalf("transport changed the manager cancellation fence: %+v", call.spec)
	default:
	}
	old.finish()
	next := hlsAdmissionCall(t, jobs)
	if next.id == old.id || next.spec != old.spec {
		t.Fatal("transport replacement borrowed a retiring job or changed the spec")
	}
	next.finish()
	hlsAdmissionResult(t, nextResult, errHLSRuntimeTestReleased)
}

func TestHLSAdmissionOldWaiterCannotRetryAgainstSupersedingSeek(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "superseded-waiter", false)
	const ticks = int64(60_000_000)
	for number := 4; number < 600; number++ {
		session.timeline.Segments = append(session.timeline.Segments, transcode.TimelineSegment{Number: number,
			StartTicks: int64(number) * ticks, DurationTicks: ticks})
	}
	session.key.plan.DurationTicks = 600 * ticks
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 1, "Playing", 0))
	_, firstResult := hlsAdmissionSegmentRequest(t, h, session, context.Background(), 0)
	old := hlsAdmissionCall(t, jobs)
	waitCtx := hlsAdmissionObserve(context.Background())
	_, waiterResult := hlsAdmissionSegmentRequest(t, h, session, waitCtx, 1)
	hlsAdmissionJoined(t, waitCtx)
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 2, "Playing", 300*ticks))
	_, seekResult := hlsAdmissionSegmentRequest(t, h, session, context.Background(), 300)
	newDemand := hlsAdmissionWaitPending(t, session, 300)
	hlsAdmissionResult(t, firstResult, context.Canceled)
	hlsAdmissionResult(t, waiterResult, context.Canceled)
	session.mu.Lock()
	current := session.admission
	session.mu.Unlock()
	if current != newDemand || newDemand.ctx.Err() != nil {
		t.Fatal("obsolete coalesced demand cancelled the newer seek")
	}
	old.finish()
	next := hlsAdmissionCall(t, jobs)
	if next.spec.Plan.SegmentStartNumber != 300 {
		t.Fatal("obsolete waiter restarted its old segment instead of the new seek")
	}
	next.finish()
	hlsAdmissionResult(t, seekResult, errHLSRuntimeTestReleased)
}

func TestHLSAdmissionNormalizedPlansCannotSplitCancellationFence(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	// Model the manager's canonicalization of executable plans. Distinct raw
	// registration plans can converge to the same spec and deduplicated ID.
	jobs.normalize = func(spec transcode.Spec) transcode.Spec {
		spec.Plan.Width = 0
		return spec
	}
	original := hlsRuntimeTestSession(t, h, "normalized-plan-fence", false)
	original.principal.User.Policy = []byte("{}")
	original.principal.PeerIP = "127.0.0.1"
	_, oldResult := hlsAdmissionSegmentRequest(t, h, original, context.Background(), 0)
	old := hlsAdmissionCall(t, jobs)
	h.retire(original)
	hlsAdmissionResult(t, oldResult, transcode.ErrJobNotFound)
	plan := original.key.plan
	plan.Width = 640
	source := library.MediaFile{Item: library.Item{ID: original.key.scope.ItemID}, SourceID: original.key.scope.SourceID, ETag: original.key.stamp}
	replacement, err := h.register(original.principal, source, original.key.scope.PlaySessionID,
		playback.ConversionDecision{Plan: &plan}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.key.plan == original.key.plan || replacement.key.scope != original.key.scope {
		t.Fatal("test did not register distinct raw plans for the same playback source")
	}
	replacement.timeline = original.timeline
	_, nextResult := hlsAdmissionSegmentRequest(t, h, replacement, context.Background(), 0)
	hlsAdmissionWaitPending(t, replacement, 0)
	select {
	case call := <-jobs.ensures:
		t.Fatalf("raw plans split the manager cancellation fence: %+v", call.spec)
	default:
	}
	old.finish()
	next := hlsAdmissionCall(t, jobs)
	if next.id == old.id || jobs.normalize(next.spec) != jobs.normalize(old.spec) {
		t.Fatal("normalized replacement reused a retiring record or changed normalized spec")
	}
	next.finish()
	hlsAdmissionResult(t, nextResult, errHLSRuntimeTestReleased)
}

func TestHLSAdmissionCreatorCancellationAndSeekCannotReviveOldWaiter(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "combined-cancellation", false)
	const ticks = int64(60_000_000)
	for number := 4; number < 600; number++ {
		session.timeline.Segments = append(session.timeline.Segments, transcode.TimelineSegment{Number: number,
			StartTicks: int64(number) * ticks, DurationTicks: ticks})
	}
	session.key.plan.DurationTicks = 600 * ticks
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 1, "Playing", 0))
	creatorCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, firstResult := hlsAdmissionSegmentRequest(t, h, session, creatorCtx, 0)
	old := hlsAdmissionCall(t, jobs)
	waitCtx := &hlsAdmissionPausedContext{hlsAdmissionObservedContext: hlsAdmissionObserve(context.Background()),
		reached: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	releaseWaiter := func() { releaseOnce.Do(func() { close(waitCtx.release) }) }
	defer releaseWaiter()
	_, waiterResult := hlsAdmissionSegmentRequest(t, h, session, waitCtx, 1)
	hlsAdmissionJoined(t, waitCtx.hlsAdmissionObservedContext)
	// Stop the retry after it checked the old revision but before it returns
	// to selection. The next iteration must check again under session.mu.
	waitCtx.armed.Store(true)
	cancel()
	select {
	case <-waitCtx.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("active waiter did not reach its creator-cancellation retry")
	}
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 2, "Playing", 300*ticks))
	_, seekResult := hlsAdmissionSegmentRequest(t, h, session, context.Background(), 300)
	newDemand := hlsAdmissionWaitPending(t, session, 300)
	releaseWaiter()
	hlsAdmissionResult(t, firstResult, context.Canceled)
	hlsAdmissionResult(t, waiterResult, context.Canceled)
	session.mu.Lock()
	current := session.admission
	session.mu.Unlock()
	if current != newDemand || newDemand.ctx.Err() != nil {
		t.Fatal("creator cancellation revived obsolete demand against the new seek")
	}
	old.finish()
	next := hlsAdmissionCall(t, jobs)
	if next.spec.Plan.SegmentStartNumber != 300 {
		t.Fatal("old waiter restarted its demand after creator cancellation and a newer seek")
	}
	next.finish()
	hlsAdmissionResult(t, seekResult, errHLSRuntimeTestReleased)
}

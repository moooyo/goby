package server

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

// The fake owner tests server lifetime and request semantics only. It starts no
// process, opens no cgroup and does not establish a media or hardware result.
type diagnosticRuntimeTestOwner struct {
	ctx          context.Context
	runGate      <-chan struct{}
	closeGate    <-chan struct{}
	entered      chan struct{}
	closeAttempt chan struct{}
	runCalls     atomic.Int32
	closeCalls   atomic.Int32
}

func (o *diagnosticRuntimeTestOwner) Run(selection media.DiagnosticSelection, authorize func(context.Context) error, publish func(media.DiagnosticReport)) (media.DiagnosticReport, error) {
	o.runCalls.Add(1)
	report := media.DiagnosticReport{Version: 1, Selection: selection, State: "running", SessionClosureRequired: true,
		Stages: []media.DiagnosticStage{{ID: "already-observed", State: "passed"}}}
	if err := authorize(o.ctx); err != nil {
		report.State = "cancelled"
		return report, err
	}
	publish(report)
	close(o.entered)
	select {
	case <-o.ctx.Done():
		report.State, report.Code = "cancelled", "diagnostic_cancelled"
		return report, o.ctx.Err()
	case <-o.runGate:
		report.State = "stages_complete"
		return report, nil
	}
}

func (o *diagnosticRuntimeTestOwner) Cancel() {}
func (o *diagnosticRuntimeTestOwner) Close() error {
	o.closeCalls.Add(1)
	select {
	case o.closeAttempt <- struct{}{}:
	default:
	}
	select {
	case <-o.closeGate:
		return nil
	default:
		return media.ErrDiagnosticClosure
	}
}

type diagnosticRuntimeTestFixture struct {
	runtime            *mediaDiagnosticRuntime
	actor              identity.Principal
	owner              *diagnosticRuntimeTestOwner
	reserved           atomic.Int32
	opened             atomic.Int32
	clock              atomic.Int64
	allow              atomic.Bool
	runGate            chan struct{}
	closeGate          chan struct{}
	runOnce, closeOnce sync.Once
}

func newDiagnosticRuntimeFixture(t *testing.T, waitForRun, waitForClose bool) *diagnosticRuntimeTestFixture {
	t.Helper()
	s := &Server{cfg: config.Config{MediaDiagnostics: config.MediaDiagnosticsConfig{Enabled: true}}}
	m, err := newMediaDiagnosticRuntime(s)
	if err != nil {
		t.Fatal(err)
	}
	f := &diagnosticRuntimeTestFixture{runtime: m, actor: identity.Principal{Kind: "admin", SessionID: strings.Repeat("a", 32), User: identity.User{ID: strings.Repeat("b", 32), IsAdministrator: true}},
		runGate: make(chan struct{}), closeGate: make(chan struct{})}
	f.allow.Store(true)
	base := time.Now()
	m.born = base
	m.now = func() time.Time { return base.Add(time.Duration(f.clock.Load())) }
	m.authorize = func(ctx context.Context, _ identity.Principal) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !f.allow.Load() {
			return identity.ErrUnauthorized
		}
		return nil
	}
	m.reserve = func() (func(), error) {
		f.reserved.Add(1)
		var once sync.Once
		return func() { once.Do(func() { f.reserved.Add(-1) }) }, nil
	}
	f.owner = &diagnosticRuntimeTestOwner{runGate: f.runGate, closeGate: f.closeGate, entered: make(chan struct{}), closeAttempt: make(chan struct{}, 16)}
	m.open = func(ctx context.Context, _ media.DiagnosticExecutionOptions) (mediaDiagnosticOwner, error) {
		f.opened.Add(1)
		f.owner.ctx = ctx
		return f.owner, nil
	}
	if !waitForRun {
		f.runOnce.Do(func() { close(f.runGate) })
	}
	if !waitForClose {
		f.closeOnce.Do(func() { close(f.closeGate) })
	}
	t.Cleanup(func() {
		f.runOnce.Do(func() { close(f.runGate) })
		f.closeOnce.Do(func() { close(f.closeGate) })
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error("diagnostic fake owner did not close")
		}
	})
	return f
}

func (f *diagnosticRuntimeTestFixture) request(id string) mediaDiagnosticStart {
	token, _ := f.runtime.token(f.actor, f.runtime.now())
	return mediaDiagnosticStart{InstanceId: f.runtime.instance, RequestId: id, StartToken: token, Mode: "software"}
}

func waitDiagnosticRuntime(t *testing.T, m *mediaDiagnosticRuntime) {
	t.Helper()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("diagnostic fake worker did not finish")
	}
}

func TestMediaDiagnosticConcurrentRequestIdempotencyAdmitsOneOwner(t *testing.T) {
	f := newDiagnosticRuntimeFixture(t, true, false)
	request := f.request(strings.Repeat("1", 32))
	var admitted atomic.Int32
	var callers sync.WaitGroup
	for range 24 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			result, first, err := f.runtime.start(context.Background(), f.actor, request)
			if err != nil || result["Id"] != request.RequestId {
				t.Error("duplicate request lost original run")
				return
			}
			if first {
				admitted.Add(1)
			}
		}()
	}
	callers.Wait()
	if admitted.Load() != 1 || f.reserved.Load() != 1 {
		t.Fatal("duplicates created extra admission slots")
	}
	select {
	case <-f.owner.entered:
	case <-time.After(time.Second):
		t.Fatal("fake run did not enter")
	}
	if f.opened.Load() != 1 {
		t.Fatal("duplicates opened multiple resource owners")
	}
	if _, _, err := f.runtime.start(context.Background(), f.actor, f.request(strings.Repeat("2", 32))); !errors.Is(err, errMediaDiagnosticBusy) {
		t.Fatal("parallel diagnostic admitted")
	}
}

func TestMediaDiagnosticRetainedOutcomeSurvivesTokenExpiryButCannotReplayAfterPurge(t *testing.T) {
	f := newDiagnosticRuntimeFixture(t, false, false)
	request := f.request(strings.Repeat("3", 32))
	if _, _, err := f.runtime.start(context.Background(), f.actor, request); err != nil {
		t.Fatal(err)
	}
	waitDiagnosticRuntime(t, f.runtime)
	f.clock.Store(int64(6 * time.Minute))
	result, admitted, err := f.runtime.start(context.Background(), f.actor, request)
	if err != nil || admitted || result["State"] != "passed" || f.opened.Load() != 1 {
		t.Fatal("expired token could not retrieve its retained original result")
	}
	f.clock.Store(int64(31 * time.Minute))
	if _, _, err := f.runtime.start(context.Background(), f.actor, request); !errors.Is(err, errMediaDiagnosticExpired) {
		t.Fatal("expired request replayed after history purge")
	}
	if f.opened.Load() != 1 || f.reserved.Load() != 0 {
		t.Fatal("expired request reopened resources")
	}
}

func TestMediaDiagnosticRequestsBindInstanceActorAndBody(t *testing.T) {
	f := newDiagnosticRuntimeFixture(t, true, false)
	request := f.request(strings.Repeat("4", 32))
	if _, _, err := f.runtime.start(context.Background(), f.actor, request); err != nil {
		t.Fatal(err)
	}
	changed := request
	changed.Mode = "configured"
	if _, _, err := f.runtime.start(context.Background(), f.actor, changed); !errors.Is(err, errMediaDiagnosticConflict) {
		t.Fatal("request body changed under the same id")
	}
	actor := f.actor
	actor.SessionID = strings.Repeat("c", 32)
	if _, _, err := f.runtime.start(context.Background(), actor, request); !errors.Is(err, errMediaDiagnosticConflict) {
		t.Fatal("another credential adopted the request")
	}
	changed = request
	changed.InstanceId = strings.Repeat("d", 32)
	if _, _, err := f.runtime.start(context.Background(), f.actor, changed); !errors.Is(err, errMediaDiagnosticInstance) {
		t.Fatal("old server instance request accepted")
	}
	changed = f.request(strings.Repeat("5", 32))
	changed.StartToken += "0"
	if _, _, err := f.runtime.start(context.Background(), f.actor, changed); !errors.Is(err, errMediaDiagnosticExpired) {
		t.Fatal("tampered token accepted")
	}
}

func TestMediaDiagnosticHistoryCapacityDoesNotEvictUnexpiredReceipts(t *testing.T) {
	f := newDiagnosticRuntimeFixture(t, false, false)
	now := f.runtime.now()
	for index := range mediaDiagnosticMaxRuns {
		id := strings.Repeat("0", 30) + string("0123456789abcdef"[index/16]) + string("0123456789abcdef"[index%16])
		f.runtime.runs[id] = &mediaDiagnosticRun{id: id, state: "failed", created: now, updated: now, finished: &now}
		f.runtime.order = append(f.runtime.order, id)
	}
	if _, _, err := f.runtime.start(context.Background(), f.actor, f.request(strings.Repeat("e", 32))); !errors.Is(err, errMediaDiagnosticRetention) {
		t.Fatal("history capacity silently evicted a live receipt")
	}
	if len(f.runtime.runs) != 32 || f.opened.Load() != 0 || f.reserved.Load() != 0 {
		t.Fatal("capacity rejection changed ownership or retained history")
	}
}

func TestMediaDiagnosticCloseRetainsReservationUntilActualOwnerCloses(t *testing.T) {
	f := newDiagnosticRuntimeFixture(t, false, true)
	request := f.request(strings.Repeat("6", 32))
	if _, _, err := f.runtime.start(context.Background(), f.actor, request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.owner.closeAttempt:
	case <-time.After(time.Second):
		t.Fatal("close not attempted")
	}
	if f.reserved.Load() != 1 {
		t.Fatal("reservation released despite pending owner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := f.runtime.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("server close claimed unfinished owner was gone")
	}
	if f.reserved.Load() != 1 {
		t.Fatal("close timeout released reservation")
	}
	f.closeOnce.Do(func() { close(f.closeGate) })
	waitDiagnosticRuntime(t, f.runtime)
	result, err := f.runtime.get(f.runtime.instance, request.RequestId, false)
	if err != nil || f.reserved.Load() != 0 || result["Report"].(*media.DiagnosticReport).SessionClosureRequired {
		t.Fatal("closed owner did not finalize and release")
	}
}

func TestMediaDiagnosticFinalAuthorityLossKeepsStageFactsButNeverPassesRun(t *testing.T) {
	f := newDiagnosticRuntimeFixture(t, false, true)
	request := f.request(strings.Repeat("7", 32))
	if _, _, err := f.runtime.start(context.Background(), f.actor, request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.owner.closeAttempt:
	case <-time.After(time.Second):
		t.Fatal("close not attempted")
	}
	f.allow.Store(false)
	f.closeOnce.Do(func() { close(f.closeGate) })
	waitDiagnosticRuntime(t, f.runtime)
	result, err := f.runtime.get(f.runtime.instance, request.RequestId, false)
	if err != nil {
		t.Fatal(err)
	}
	report := result["Report"].(*media.DiagnosticReport)
	if result["State"] != "cancelled" || result["Code"] != "diagnostic_authority_lost" || report.State != "cancelled" || report.Code != "diagnostic_authority_lost" || report.Stages[0].State != "passed" || report.SessionClosureRequired {
		t.Fatal("late authority loss either claimed final pass or erased already observed stage facts")
	}
}

func TestMediaDiagnosticRevocationAndCancelAreIdempotentAndScoped(t *testing.T) {
	f := newDiagnosticRuntimeFixture(t, true, false)
	request := f.request(strings.Repeat("8", 32))
	if _, _, err := f.runtime.start(context.Background(), f.actor, request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.owner.entered:
	case <-time.After(time.Second):
		t.Fatal("run not entered")
	}
	f.runtime.cancelActor("unrelated", "unrelated")
	if f.owner.ctx.Err() != nil {
		t.Fatal("unrelated actor cancelled the diagnostic")
	}
	f.runtime.cancelActor("", f.actor.SessionID)
	waitDiagnosticRuntime(t, f.runtime)
	first, err := f.runtime.get(f.runtime.instance, request.RequestId, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.runtime.get(f.runtime.instance, request.RequestId, true)
	if err != nil || first["State"] != "cancelled" || second["Code"] != "diagnostic_authority_lost" || f.reserved.Load() != 0 || f.owner.runCalls.Load() != 1 {
		t.Fatal("revocation or repeated cancel changed run ownership")
	}
}

func TestMediaDiagnosticPartialConstructionCannotReleaseBeforeCleanup(t *testing.T) {
	f := newDiagnosticRuntimeFixture(t, false, true)
	f.runtime.open = func(ctx context.Context, _ media.DiagnosticExecutionOptions) (mediaDiagnosticOwner, error) {
		f.opened.Add(1)
		f.owner.ctx = ctx
		return f.owner, media.ErrDiagnosticResources
	}
	request := f.request(strings.Repeat("9", 32))
	if _, _, err := f.runtime.start(context.Background(), f.actor, request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.owner.closeAttempt:
	case <-time.After(time.Second):
		t.Fatal("partial owner close not attempted")
	}
	if f.owner.runCalls.Load() != 0 || f.reserved.Load() != 1 {
		t.Fatal("failed construction ran or discarded its partial owner")
	}
	f.closeOnce.Do(func() { close(f.closeGate) })
	waitDiagnosticRuntime(t, f.runtime)
	result, err := f.runtime.get(f.runtime.instance, request.RequestId, false)
	if err != nil || result["State"] != "unavailable" || f.reserved.Load() != 0 {
		t.Fatal("partial initialization outcome was lost")
	}
}

func TestMediaDiagnosticExpiredDuringReservationDoesNotAdmitOrRetain(t *testing.T) {
	f := newDiagnosticRuntimeFixture(t, false, false)
	request := f.request(strings.Repeat("f", 32))
	f.runtime.reserve = func() (func(), error) {
		f.reserved.Add(1)
		f.clock.Store(int64(mediaDiagnosticTokenLifetime))
		return func() { f.reserved.Add(-1) }, nil
	}
	if _, _, err := f.runtime.start(context.Background(), f.actor, request); !errors.Is(err, errMediaDiagnosticExpired) {
		t.Fatal("reservation wait extended a consumed request window")
	}
	if f.reserved.Load() != 0 || f.opened.Load() != 0 || len(f.runtime.runs) != 0 {
		t.Fatal("expired admission retained resources or a runnable record")
	}
}

func TestMediaDiagnosticRevisionOrdersUpdatesEvenWhenWallTimeIsUnchanged(t *testing.T) {
	f := newDiagnosticRuntimeFixture(t, true, false)
	request := f.request(strings.Repeat("d", 32))
	queued, _, err := f.runtime.start(context.Background(), f.actor, request)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.owner.entered:
	case <-time.After(time.Second):
		t.Fatal("run not entered")
	}
	running, err := f.runtime.get(f.runtime.instance, request.RequestId, false)
	if err != nil {
		t.Fatal(err)
	}
	cancelling, err := f.runtime.get(f.runtime.instance, request.RequestId, true)
	if err != nil {
		t.Fatal(err)
	}
	waitDiagnosticRuntime(t, f.runtime)
	finished, err := f.runtime.get(f.runtime.instance, request.RequestId, false)
	if err != nil {
		t.Fatal(err)
	}
	var previous uint64
	for _, snapshot := range []map[string]any{queued, running, cancelling, finished} {
		revision, err := strconv.ParseUint(snapshot["Revision"].(string), 10, 64)
		if err != nil || revision <= previous || snapshot["UpdatedAt"] != queued["UpdatedAt"] {
			t.Fatal("visible updates cannot be ordered independently of wall time")
		}
		previous = revision
	}
}

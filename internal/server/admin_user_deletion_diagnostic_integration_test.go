//go:build linux

package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// This synthetic diagnostic owner proves orchestration and closure ordering,
// not diagnostic process, decoded-content, resource-isolation, or GPU results.
// HTTP authorization, user deletion, conversion reservations, queued jobs,
// sockets, and the peer audio FFmpeg processes remain real.
type deletedDiagnosticOwner struct {
	ctx          context.Context
	entered      chan struct{}
	proceed      chan struct{}
	retryEntered chan struct{}
	closePermit  chan struct{}
	permitOnce   sync.Once
	closeCalls   atomic.Int32
	closed       atomic.Bool
	freeze       func()
}

func (o *deletedDiagnosticOwner) Run(selection media.DiagnosticSelection, authorize func(context.Context) error, publish func(media.DiagnosticReport)) (media.DiagnosticReport, error) {
	report := media.DiagnosticReport{Version: 1, Selection: selection, State: "running", SessionClosureRequired: true,
		Stages: []media.DiagnosticStage{{ID: "synthetic-completed-step", State: "passed"}, {ID: "synthetic-pending-step", State: "not_run"}}}
	if err := authorize(o.ctx); err != nil {
		return report, err
	}
	publish(report)
	o.freeze()
	close(o.entered)
	select {
	case <-o.ctx.Done():
		report.State, report.Code = "cancelled", "diagnostic_cancelled"
		return report, o.ctx.Err()
	case <-o.proceed:
		return report, media.ErrDiagnosticResources
	}
}

func (o *deletedDiagnosticOwner) Cancel() {}

func (o *deletedDiagnosticOwner) Close() error {
	if o.closeCalls.Add(1) == 1 {
		return media.ErrDiagnosticClosure
	}
	close(o.retryEntered)
	<-o.closePermit
	o.closed.Store(true)
	return nil
}

type deletedDiagnosticControl struct {
	owner              *deletedDiagnosticOwner
	authorityMu        sync.RWMutex
	authorityFrozen    bool
	opens              atomic.Int32
	reserves           atomic.Int32
	releases           atomic.Int32
	releaseBeforeClose atomic.Bool
}

func installDeletedDiagnosticControl(t *testing.T, f *serverFixture) *deletedDiagnosticControl {
	t.Helper()
	runtime := f.app.mediaDiagnostics
	if runtime == nil {
		t.Fatal("server did not initialize the diagnostic runtime")
	}
	control := &deletedDiagnosticControl{owner: &deletedDiagnosticOwner{
		entered: make(chan struct{}), proceed: make(chan struct{}), retryEntered: make(chan struct{}), closePermit: make(chan struct{}),
	}}
	authorize, reserve := runtime.authorize, runtime.reserve
	runtime.enabled = true
	runtime.authorize = func(ctx context.Context, actor identity.Principal) error {
		control.authorityMu.RLock()
		if control.authorityFrozen {
			control.authorityMu.RUnlock()
			<-ctx.Done()
			return ctx.Err()
		}
		defer control.authorityMu.RUnlock()
		return authorize(ctx, actor)
	}
	control.owner.freeze = func() {
		// Drain every in-flight real authorization before publishing entered.
		// Later watcher calls can only observe cancellation supplied by DELETE;
		// neither its timer nor a stage checkpoint can compensate for a missing hook.
		control.authorityMu.Lock()
		control.authorityFrozen = true
		control.authorityMu.Unlock()
	}
	runtime.reserve = func() (func(), error) {
		release, err := reserve()
		if err != nil {
			return nil, err
		}
		control.reserves.Add(1)
		return func() {
			if !control.owner.closed.Load() {
				control.releaseBeforeClose.Store(true)
			}
			release()
			control.releases.Add(1)
		}, nil
	}
	runtime.open = func(ctx context.Context, _ media.DiagnosticExecutionOptions) (mediaDiagnosticOwner, error) {
		control.opens.Add(1)
		control.owner.ctx = ctx
		return control.owner, nil
	}
	t.Cleanup(func() {
		runtime.BeginClose()
		control.owner.permitOnce.Do(func() { close(control.owner.closePermit) })
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Errorf("close the deleted diagnostic owner: %v", err)
		}
	})
	return control
}

type deletedDiagnosticPendingAudio struct {
	response *http.Response
	err      error
	done     chan struct{}
	cancel   context.CancelFunc
}

func startDeletedDiagnosticPendingAudio(t *testing.T, a *audioHTTPFixture, login clientSessionHTTPLogin, reference string) *deletedDiagnosticPendingAudio {
	t.Helper()
	ctx, cancel := context.WithTimeout(a.f.ctx, 45*time.Second)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.server.URL+a.universal("flac", audioHTTPMP3Query(reference, 0)), nil)
	if err != nil {
		cancel()
		t.Fatal("create the queued audio request")
	}
	request.Header = login.headers.Clone()
	pending := &deletedDiagnosticPendingAudio{done: make(chan struct{}), cancel: cancel}
	go func() {
		pending.response, pending.err = a.server.Client().Do(request)
		close(pending.done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-pending.done:
			if pending.response != nil {
				_ = pending.response.Body.Close()
			}
		case <-time.After(5 * time.Second):
			t.Error("queued audio request did not stop")
		}
	})
	return pending
}

func TestHTTPDeleteDiagnosticOwnerRetainsPeerJobsCachesAndSockets(t *testing.T) {
	a := newAudioHTTPFixture(t, true)
	if a.accounts.viewer.userID == a.accounts.other.userID {
		t.Fatal("queued and running conversions must belong to different users")
	}
	if changed, err := a.f.pool.Exec(a.f.ctx, `UPDATE users SET policy =
		(SELECT policy FROM users WHERE id = $1) WHERE id = $2`, a.accounts.viewer.userID, a.accounts.other.userID); err != nil || changed.RowsAffected() != 1 {
		t.Fatal("grant the surviving peer access to the real audio source")
	}
	csrf := adminSessionHTTPCSRF(t, a.f, a.accounts.cookie)
	headers := mediaDiagnosticHTTPHeaders(a.f, csrf)
	operatorID := managedHTTPCreate(t, a.f, a.accounts.cookie, csrf, "Deleted diagnostic operator", true)
	unrelatedID := managedHTTPCreate(t, a.f, a.accounts.cookie, csrf, "Unrelated deleted administrator", true)
	operatorCookie, operatorCSRF := managedHTTPLogin(t, a.f, "Deleted diagnostic operator", "managed-user-password")
	operator := managedHTTPDetail(t, a.f, a.accounts.cookie, operatorID)
	unrelated := managedHTTPDetail(t, a.f, a.accounts.cookie, unrelatedID)
	control := installDeletedDiagnosticControl(t, a.f)

	// Keep periodic socket authorization from replacing deletion's retirement.
	a.f.app.sockets.revalidateEvery, a.f.app.sockets.pingEvery = time.Hour, time.Hour
	peerSocket := websocketHTTPDial(t, a.server, "/emby/socket", a.accounts.other.headers, http.StatusSwitchingProtocols)
	queuedSocket := websocketHTTPDial(t, a.server, "/emby/socket", a.accounts.viewer.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, a.f, a.accounts.other.id, 1)
	websocketHTTPWaitCount(t, a.f, a.accounts.viewer.id, 1)
	const peerReference, queuedReference = "diagnostic-deletion-live-peer", "diagnostic-deletion-queued-peer"
	// HEAD prepares a genuine scoped playback reference without starting a job.
	head := a.request(t, http.MethodHead, a.universal("flac", audioHTTPMP3Query(queuedReference, 0)), nil, a.accounts.viewer.headers)
	expectHLSHTTPStatus(t, head, http.StatusOK)
	queuedPlayID := a.canonical(t, a.accounts.viewer, queuedReference, "flac")
	if len(a.encoderPIDs(t)) != 0 {
		t.Fatal("preparing queued playback unexpectedly started an encoder")
	}
	body := mediaDiagnosticHTTPStartBody(t, a.f, operatorCookie)
	mediaDiagnosticHTTPRun(t, a.f.request(t, http.MethodPost, mediaDiagnosticHTTPBase+"/runs", body,
		mediaDiagnosticHTTPHeaders(a.f, operatorCSRF), operatorCookie), http.StatusAccepted, body)
	select {
	case <-control.owner.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("diagnostic did not enter its authorized, frozen execution window")
	}
	runtime := a.f.app.mediaDiagnostics
	runtime.mu.Lock()
	run := runtime.runs[body["RequestId"].(string)]
	revision := run.revision
	runtime.mu.Unlock()
	// All administrator and diagnostic setup precedes the finite live-audio window.
	peer := a.openLiveFor(t, a.accounts.other, peerReference)
	peerPlayID := a.canonical(t, a.accounts.other, peerReference, "flac")
	peerSession, peerRecord := a.liveSessionFor(t, a.accounts.other, peerPlayID, 1)
	peerProcess := deletedUserObserveEncoder(t, a, peerRecord.ID, 1)
	pending := startDeletedDiagnosticPendingAudio(t, a, a.accounts.viewer, queuedReference)
	var queuedID string
	audioHTTPWait(t, "the third job was not durably queued behind the diagnostic", func() bool {
		current, snapshotErr := a.f.app.hls.manager.Snapshot(peerSession.key.scope, peerRecord.ID)
		if snapshotErr != nil || current.State != "running" || peerProcess.exited(t) {
			t.Fatal("the live peer finished before the diagnostic capacity window was established")
		}
		var state string
		err := a.f.pool.QueryRow(a.f.ctx, `SELECT id, state FROM encoding_jobs
			WHERE auth_session_id = $1 AND play_session_id = $2`, a.accounts.viewer.id, queuedPlayID).Scan(&queuedID, &state)
		if errors.Is(err, pgx.ErrNoRows) {
			return false
		}
		if err != nil || state != "queued" {
			t.Fatal("the third conversion bypassed the diagnostic reservation")
		}
		return true
	})
	queuedScope := peerRecord.Spec.Scope
	queuedScope.UserID, queuedScope.AuthSessionID, queuedScope.DeviceID, queuedScope.PlaySessionID =
		a.accounts.viewer.userID, a.accounts.viewer.id, a.accounts.viewer.deviceID, queuedPlayID
	reserver, ok := a.f.app.hls.manager.(interface{ ReserveDiagnostic() (func(), error) })
	if !ok {
		t.Fatal("real conversion manager does not expose diagnostic reservations")
	}
	assertPeersHeld := func() {
		t.Helper()
		live, snapshotErr := a.f.app.hls.manager.Snapshot(peerSession.key.scope, peerRecord.ID)
		if snapshotErr != nil || live.State != "running" || peerProcess.exited(t) {
			t.Fatal("the original peer no longer occupies the diagnostic capacity test window")
		}
		_, current := a.liveSessionFor(t, a.accounts.other, peerPlayID, 1)
		if current.ID != peerRecord.ID || peerProcess.exited(t) || len(a.encoderPIDs(t)) != 1 {
			t.Fatal("deletion stopped or replaced the surviving peer producer")
		}
		queued, err := a.f.app.hls.manager.Snapshot(queuedScope, queuedID)
		if err != nil || queued.State != "queued" || a.jobs(t, a.accounts.viewer, queuedPlayID, true) != 1 {
			t.Fatal("deletion cancelled or prematurely launched the queued peer job")
		}
		select {
		case <-pending.done:
			t.Fatal("queued HTTP conversion returned before diagnostic closure")
		default:
		}
		if release, err := reserver.ReserveDiagnostic(); !errors.Is(err, transcode.ErrBusy) || release != nil {
			if release != nil {
				release()
			}
			t.Fatal("diagnostic released its real reservation before owner closure")
		}
		peerSocket.ping(t)
		queuedSocket.ping(t)
		websocketHTTPWaitCount(t, a.f, a.accounts.other.id, 1)
		websocketHTTPWaitCount(t, a.f, a.accounts.viewer.id, 1)
	}
	assertDiagnosticActive := func() {
		t.Helper()
		runtime.mu.Lock()
		active := runtime.active == run.id && run.state == "running" && run.revision == revision &&
			run.authorityCode == "" && run.finished == nil && run.ctx.Err() == nil
		runtime.mu.Unlock()
		if !active || control.owner.ctx.Err() != nil || control.releases.Load() != 0 || control.owner.closeCalls.Load() != 0 {
			t.Fatal("an unrelated or rejected deletion changed the diagnostic owner")
		}
		assertPeersHeld()
	}
	deleteUser := func(id string, value any) {
		t.Helper()
		response := a.f.request(t, http.MethodDelete, "/admin/v1/users/"+url.PathEscape(id), map[string]any{"Revision": value}, headers, a.accounts.cookie)
		expectStatus(t, response, http.StatusOK)
		if outcome := jsonObject(t, response); len(outcome) != 1 || outcome["CurrentSessionRevoked"] != false || len(response.Result().Cookies()) != 0 {
			t.Fatal("deleting another administrator changed the actor response or cookie")
		}
	}
	deleteUser(unrelatedID, unrelated["Revision"])
	assertDiagnosticActive()
	expectAPIError(t, a.f.request(t, http.MethodGet, "/admin/v1/users/"+url.PathEscape(unrelatedID), nil, nil, a.accounts.cookie), http.StatusNotFound, "not_found", false)
	currentRevision, err := strconv.ParseInt(operator["Revision"].(string), 10, 64)
	if err != nil {
		t.Fatal("read the diagnostic owner's current revision")
	}
	expectAPIError(t, a.f.request(t, http.MethodDelete, "/admin/v1/users/"+url.PathEscape(operatorID),
		map[string]any{"Revision": strconv.FormatInt(currentRevision+1, 10)}, headers, a.accounts.cookie), http.StatusConflict, "revision_conflict", false)
	assertDiagnosticActive()
	deleteUser(operatorID, operator["Revision"])
	// The authority barrier prevents every watcher/checkpoint database fallback.
	// Inspect cancellation immediately after DELETE, without a timer allowance.
	runtime.mu.Lock()
	cancelled := run.ctx.Err() == context.Canceled && run.authorityCode == "diagnostic_authority_lost"
	runtime.mu.Unlock()
	if !cancelled || control.owner.ctx.Err() != context.Canceled {
		t.Fatal("committed user deletion did not directly cancel its diagnostic owner")
	}
	expectAPIError(t, a.f.request(t, http.MethodGet, "/admin/v1/users/"+url.PathEscape(operatorID), nil, nil, a.accounts.cookie), http.StatusNotFound, "not_found", false)
	select {
	case <-control.owner.proceed:
		t.Fatal("diagnostic execution advanced beyond its blocked stage")
	default:
	}
	select {
	case <-control.owner.retryEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled diagnostic did not retain and retry its failed owner closure")
	}
	cleanup := mediaDiagnosticHTTPRun(t, a.f.request(t, http.MethodGet, mediaDiagnosticHTTPPath(body), nil, nil, a.accounts.cookie), http.StatusOK, body)
	if cleanup["State"] != "cleanup_pending" || cleanup["Code"] != "diagnostic_process_closure_failed" ||
		objectValue(t, cleanup, "Report")["SessionClosureRequired"] != true || control.owner.closed.Load() ||
		control.owner.closeCalls.Load() != 2 || control.releases.Load() != 0 || control.releaseBeforeClose.Load() {
		t.Fatal("failed diagnostic closure discarded its owner or reservation")
	}
	assertPeersHeld()
	control.owner.permitOnce.Do(func() { close(control.owner.closePermit) })
	finished := make(chan struct{})
	go func() { runtime.wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("closed diagnostic did not finalize and release its reservation")
	}
	response := a.f.request(t, http.MethodGet, mediaDiagnosticHTTPPath(body), nil, nil, a.accounts.cookie)
	final := mediaDiagnosticHTTPRun(t, response, http.StatusOK, body)
	report := objectValue(t, final, "Report")
	stages := report["Stages"].([]any)
	if final["State"] != "cancelled" || final["Code"] != "diagnostic_authority_lost" || final["FinishedAt"] == nil ||
		report["State"] != "cancelled" || report["Code"] != "diagnostic_authority_lost" ||
		report["SessionClosureRequired"] != false || stages[0].(map[string]any)["State"] != "passed" || stages[1].(map[string]any)["State"] != "not_run" ||
		control.opens.Load() != 1 || control.reserves.Load() != 1 || control.releases.Load() != 1 || control.owner.closeCalls.Load() != 2 ||
		!control.owner.closed.Load() || control.releaseBeforeClose.Load() {
		t.Fatal("deleted diagnostic lost its retained facts or released capacity incorrectly")
	}
	mediaDiagnosticHTTPPrivate(t, response, operatorCookie.Value, operatorCSRF, body["StartToken"].(string))
	expectAPIError(t, a.f.request(t, http.MethodGet, mediaDiagnosticHTTPPath(body), nil, nil, operatorCookie), http.StatusUnauthorized, "invalid_credentials", false)
	expectAPIError(t, a.f.request(t, http.MethodPost, mediaDiagnosticHTTPBase+"/runs", body,
		mediaDiagnosticHTTPHeaders(a.f, operatorCSRF), operatorCookie), http.StatusUnauthorized, "invalid_credentials", false)
	select {
	case <-pending.done:
	case <-time.After(8 * time.Second):
		t.Fatal("released diagnostic capacity did not wake the queued HTTP conversion")
	}
	if pending.err != nil || pending.response == nil || pending.response.StatusCode != http.StatusOK {
		t.Fatal("the queued peer conversion did not start normally after owner closure")
	}
	assertAudioHTTPProgressiveHeaders(t, pending.response.Header, "audio/mpeg")
	queuedPrefix := make([]byte, 256)
	if _, err := io.ReadFull(pending.response.Body, queuedPrefix); err != nil {
		t.Fatal("released queued conversion did not publish actual audio")
	}
	queuedSession, queuedRecord := a.liveSessionFor(t, a.accounts.viewer, queuedPlayID, 1)
	queuedProcess := deletedUserObserveEncoder(t, a, queuedID, 2)
	_, survivingRecord := a.liveSessionFor(t, a.accounts.other, peerPlayID, 1)
	if queuedRecord.ID != queuedID || survivingRecord.ID != peerRecord.ID || peerProcess.exited(t) || peerProcess.pid == queuedProcess.pid {
		t.Fatal("diagnostic release replaced a peer job or waited for the original peer to stop")
	}
	readComplete := func(live audioHTTPLive) []byte {
		t.Helper()
		remaining, err := io.ReadAll(io.LimitReader(live.response.Body, 1<<20))
		if err != nil || len(remaining) == 0 || len(remaining) >= 1<<20 {
			t.Fatal("administrator deletion interrupted a surviving complete audio stream")
		}
		_ = live.response.Body.Close()
		live.cancel()
		complete := append(append([]byte(nil), live.prefix...), remaining...)
		a.verifyMP3(t, complete, 6)
		return complete
	}
	peerComplete := readComplete(peer)
	queuedComplete := readComplete(audioHTTPLive{response: pending.response, cancel: pending.cancel, prefix: queuedPrefix})
	for _, completed := range []struct {
		login     clientSessionHTTPLogin
		reference string
		session   *hlsSession
		record    transcode.Record
		process   deletedUserEncoderProcess
		data      []byte
	}{
		{a.accounts.other, peerReference, peerSession, peerRecord, peerProcess, peerComplete},
		{a.accounts.viewer, queuedReference, queuedSession, queuedRecord, queuedProcess, queuedComplete},
	} {
		audioHTTPWait(t, "a surviving original producer did not complete normally", func() bool {
			completed.session.mu.Lock()
			readers, closed := completed.session.progressiveReaders, completed.session.closed
			completed.session.mu.Unlock()
			record, err := a.f.app.hls.manager.Snapshot(completed.session.key.scope, completed.record.ID)
			return readers == 0 && !closed && err == nil && record.State == "completed" && completed.process.exited(t)
		})
		cached := a.request(t, http.MethodGet, a.universal("flac", audioHTTPMP3Query(completed.reference, 0)), nil, completed.login.headers)
		expectHLSHTTPStatus(t, cached, http.StatusOK)
		if !bytes.Equal(cached.body, completed.data) || len(a.encoderPIDs(t)) != 2 {
			t.Fatal("administrator deletion lost a peer cache or started a replacement encoder")
		}
		expectHLSHTTPStatus(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, completed.login.headers), http.StatusOK)
	}
	if len(a.f.app.streamSlots) != 0 {
		t.Fatal("completed peer streams retained HTTP stream capacity")
	}
	peerSocket.ping(t)
	queuedSocket.ping(t)
	websocketHTTPWaitCount(t, a.f, a.accounts.other.id, 1)
	websocketHTTPWaitCount(t, a.f, a.accounts.viewer.id, 1)
	expectStatus(t, a.f.request(t, http.MethodGet, "/admin/v1/session", nil, nil, a.accounts.cookie), http.StatusOK)
}

//go:build linux

package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

type stoppedOwnedHLSMatrixFixture struct {
	real      *playbackStopAliasRealFixture
	principal identity.Principal
	headers   http.Header
	graph     hlsHTTPGraph
	session   *hlsSession
}

func newStoppedOwnedHLSMatrixFixture(t *testing.T) stoppedOwnedHLSMatrixFixture {
	t.Helper()
	runID := os.Getenv("GOBY_STOPPED_USERDATA_LOCK_RUN_ID")
	if runID == "" {
		t.Skip("GOBY_STOPPED_USERDATA_LOCK_RUN_ID explicitly admits the actual owned-HLS stop matrix")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("actual owned-HLS matrix requires a bounded run identity")
	}
	if testing.Short() {
		t.Skip("actual owned-HLS stop matrix requires the real database and media fixtures")
	}
	real := newPlaybackStopAliasRealFixture(t)
	f, h := real.control, real.hls
	// Actual ownership must come from the production constructor defaults.
	if !f.app.correlatedHLSOwnershipEnabled || !f.app.correlatedHLSEarlyStopEnabled {
		t.Fatal("Server.New did not enable the qualified correlated file-HLS owner chain")
	}
	principal, headers := f.principal(t, "normal")
	graph := h.graph(t, clientSessionHTTPLogin{id: principal.SessionID, userID: principal.User.ID,
		deviceID: principal.Client.DeviceID, headers: headers}, 0)
	f.app.hls.mu.Lock()
	session := f.app.hls.sessions[graph.hlsID]
	f.app.hls.mu.Unlock()
	if session == nil || session.playbackReference == nil {
		t.Fatal("natural metadata did not retain a real owned registration")
	}
	expectStatus(t, playbackControlRequest(f.ctx, f.handler, "/emby/Sessions/Playing", map[string]any{
		"PlaySessionId": graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID), "PositionTicks": int64(0)}, headers), http.StatusNoContent)
	return stoppedOwnedHLSMatrixFixture{real: real, principal: principal, headers: headers, graph: graph, session: session}
}

func (fixture stoppedOwnedHLSMatrixFixture) startActualEncoder(t *testing.T) playbackStopAliasEncoder {
	t.Helper()
	f, h := fixture.real.control, fixture.real.hls
	ctx, cancel := context.WithCancel(f.ctx)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.server.URL+fixture.graph.children[0], nil)
		if err != nil {
			return
		}
		request.Header = fixture.headers.Clone()
		response, err := h.server.Client().Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("the actual owned-HLS matrix media consumer did not join")
		}
	})
	return playbackStopAliasObserveEncoder(t, fixture.real, fixture.session.key.scope)
}

func stoppedOwnedHLSMatrixRetired(t *testing.T, process playbackStopAliasEncoder) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var firstUnknown, last stoppedRetirementObservationFacts
	retries := 0
	for time.Now().Before(deadline) {
		exited, reaped, group, known, facts := stoppedEncoderRetirementObservation(process)
		last = facts
		if !known {
			if !facts.retryExitedStatESRCH(exited) {
				t.Fatalf("the exact matrix encoder retirement became unknown: %+v", facts)
			}
			if retries == 0 {
				firstUnknown = facts
				t.Logf("retry the strict same-pidfd retirement observation after stat ESRCH: first_unknown=%+v", firstUnknown)
			}
			retries++
		} else if exited && reaped && group {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the exact actual matrix encoder did not exit/reap its owned group: first_unknown=%+v last_observation=%+v retryable_unknown_observations=%d", firstUnknown, last, retries)
}

func (fixture stoppedOwnedHLSMatrixFixture) stop(t *testing.T, ctx context.Context) int {
	t.Helper()
	f, h := fixture.real.control, fixture.real.hls
	return playbackControlRequest(ctx, f.handler, "/emby/Sessions/Playing/Stopped", map[string]any{
		"PlaySessionId": fixture.graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID)}, fixture.headers).Code
}

func TestHTTPPlaybackStoppedOwnedHLSRejectsDelayedInputAndRegistration(t *testing.T) {
	fixture := newStoppedOwnedHLSMatrixFixture(t)
	f, session := fixture.real.control, fixture.session
	process := fixture.startActualEncoder(t)
	loan, source, err := f.app.hls.authorizePlaybackSource(f.ctx, fixture.principal, session)
	if err != nil || loan == nil || loan.owned == nil {
		t.Fatal("open the actual owned source before delaying its input")
	}
	defer loan.close()
	oldInput, err := loan.owned.duplicate()
	if err != nil {
		t.Fatal("retain the actual old independently owned source FD")
	}
	defer f.app.hls.closePlaybackInput(oldInput)
	var oldMetadata *playbackAdmissionReference
	_, err = f.app.library.GetPlaybackSessionAdmitted(f.ctx, playbackOwner(fixture.principal), fixture.graph.playID,
		func(current library.PlaySession) (func(), error) {
			var err error
			oldMetadata, err = f.app.playbackStopIntents.acquireValidated(playbackScopeForSession(current))
			if err != nil {
				return nil, err
			}
			return oldMetadata.release, nil
		})
	if err != nil {
		t.Fatal("retain the actual old freshly admitted metadata")
	}
	defer oldMetadata.release()
	if status := fixture.stop(t, f.ctx); status != http.StatusNoContent {
		t.Fatalf("standard owned-HLS Stopped returned status=%d", status)
	}
	stoppedOwnedHLSMatrixRetired(t, process)
	if usage := f.app.playbackStopIntents.usage(); usage.Entries != 1 || usage.References < 3 || usage.StopReservations != 0 {
		t.Fatal("durable terminal erased the deliberately retained old capabilities")
	}
	if _, err := oldInput.ensure(context.Background(), f.app.hls.manager,
		transcode.Spec{Scope: session.key.scope, SourceStamp: session.key.stamp, Plan: session.key.plan}); !errors.Is(err, transcode.ErrJobCancelled) {
		t.Fatal("a delayed actual old source input recreated a producer after Stop")
	}
	decision := playback.ConversionDecision{Plan: &session.key.plan, OutputSource: session.output, SubtitleView: session.subtitleView}
	if _, _, err := f.app.hls.registerWithPlaybackOwner(fixture.principal, source, fixture.graph.playID, decision, 0, oldMetadata); !errors.Is(err, transcode.ErrJobCancelled) {
		t.Fatal("old admitted metadata republished a registration after durable Stop")
	}
	if _, err := f.app.hls.freshRegistrationOwner(f.ctx, fixture.principal, source, fixture.graph.playID, decision); !errors.Is(err, library.ErrNotFound) {
		t.Fatal("a new registration bypassed fresh terminal DB rejection")
	}
	oldMetadata.release()
	if err := loan.close(); err != nil {
		t.Fatal("close the actual retained outer source")
	}
	if usage := f.app.playbackStopIntents.usage(); usage != (playbackStopIntentUsage{}) {
		t.Fatal("consumed/rejected old input and metadata did not collect the intent")
	}
	if descriptors := playbackStopAliasSourceFDs(t, fixture.real.source); descriptors != 0 {
		t.Fatal("old source descriptors survived exact process/loan retirement")
	}
	t.Log("actual_old_input_and_metadata_recreation_rejected=true original_emby_player_used=false")
}

func TestHTTPPlaybackStoppedOwnedHLSForeignBodiesDoNotCancel(t *testing.T) {
	fixture := newStoppedOwnedHLSMatrixFixture(t)
	f, h := fixture.real.control, fixture.real.hls
	process := fixture.startActualEncoder(t)
	for _, candidate := range []struct {
		item, source string
		position     int64
		status       int
	}{
		{item: "foreign-item", source: media.SourceID(h.item.ID), status: http.StatusNotFound},
		{item: h.item.ID, source: "foreign-source", status: http.StatusNotFound},
		{item: h.item.ID, source: media.SourceID(h.item.ID), position: -1, status: http.StatusBadRequest},
	} {
		response := playbackControlRequest(f.ctx, f.handler, "/emby/Sessions/Playing/Stopped", map[string]any{
			"PlaySessionId": fixture.graph.playID, "ItemId": candidate.item, "MediaSourceId": candidate.source, "PositionTicks": candidate.position}, fixture.headers)
		expectStatus(t, response, candidate.status)
		if f.app.playbackStopIntents.blocked(fixture.session.key.scope) || f.app.playbackStopIntents.usage().StopReservations != 0 {
			t.Fatal("a rejected body installed a stop intent")
		}
		if state, err := f.app.hls.manager.Snapshot(fixture.session.key.scope, process.jobID); err != nil || state.State != "running" {
			t.Fatal("a foreign/invalid Stopped body cancelled the exact actual producer")
		}
	}
	foreign, foreignHeaders := f.principal(t, "normal")
	if foreign.SessionID == fixture.principal.SessionID {
		t.Fatal("foreign-body fixture reused the original authentication session")
	}
	expectStatus(t, playbackControlRequest(f.ctx, f.handler, "/emby/Sessions/Playing/Stopped", map[string]any{
		"PlaySessionId": fixture.graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID)}, foreignHeaders), http.StatusNotFound)
	if state, err := f.app.hls.manager.Snapshot(fixture.session.key.scope, process.jobID); err != nil || state.State != "running" ||
		f.app.playbackStopIntents.usage().StopReservations != 0 {
		t.Fatal("a distinct authenticated session cancelled the owned actual producer")
	}
	if status := fixture.stop(t, f.ctx); status != http.StatusNoContent {
		t.Fatalf("valid standard stop after foreign bodies returned status=%d", status)
	}
	stoppedOwnedHLSMatrixRetired(t, process)
}

func TestHTTPPlaybackStoppedOwnedHLSExpiredAuthorityDoesNotMint(t *testing.T) {
	fixture := newStoppedOwnedHLSMatrixFixture(t)
	f, h := fixture.real.control, fixture.real.hls
	before := f.app.playbackStopIntents.usage()
	updated, err := f.control.Exec(f.ctx, `UPDATE sessions
		SET expires_at=created_at+interval '1 microsecond'
		WHERE id=$1 AND created_at+interval '1 microsecond'<clock_timestamp()`, fixture.principal.SessionID)
	if err != nil || updated.RowsAffected() != 1 {
		t.Fatal("expire the exact actual playback credential")
	}
	called := false
	_, err = f.app.library.GetPlaybackSessionAdmitted(f.ctx, playbackOwner(fixture.principal), fixture.graph.playID,
		func(library.PlaySession) (func(), error) {
			called = true
			return nil, nil
		})
	if err == nil || called || f.app.playbackStopIntents.usage() != before {
		t.Fatal("expired database authority minted a new cancellation lifetime")
	}
	expectStatus(t, playbackControlRequest(f.ctx, f.handler, "/emby/Sessions/Playing/Stopped", map[string]any{
		"PlaySessionId": fixture.graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID)}, fixture.headers), http.StatusUnauthorized)
	if usage := f.app.playbackStopIntents.usage(); usage.StopReservations != 0 {
		t.Fatal("expired control-route authority installed a stop intent")
	}
}

func TestHTTPPlaybackStoppedOwnedHLSFailedCommitRetainsIntent(t *testing.T) {
	fixture := newStoppedOwnedHLSMatrixFixture(t)
	f, h := fixture.real.control, fixture.real.hls
	process := fixture.startActualEncoder(t)
	barrier, err := f.control.Begin(f.ctx)
	if err != nil {
		t.Fatal("begin the actual failed-commit userdata barrier")
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := barrier.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Error("the actual failed-commit userdata barrier did not release")
			}
		})
	}
	defer release()
	t.Cleanup(release)
	if err := barrier.QueryRow(f.ctx, "SELECT user_id FROM user_item_data WHERE user_id=$1 AND item_id=$2 FOR UPDATE", fixture.principal.User.ID, h.item.ID).Scan(new(string)); err != nil {
		t.Fatal("hold the exact actual userdata row before failed Stop")
	}
	ctx, cancel := context.WithTimeout(f.ctx, 750*time.Millisecond)
	status := fixture.stop(t, ctx)
	cancel()
	if status != http.StatusServiceUnavailable {
		t.Fatalf("held actual report did not expose its failed durable completion: status=%d", status)
	}
	stoppedOwnedHLSMatrixRetired(t, process)
	if usage := f.app.playbackStopIntents.usage(); usage.StopReservations != 1 || usage.StopOwners != 0 || !f.app.playbackStopIntents.blocked(fixture.session.key.scope) {
		t.Fatal("failed durable completion cleared the accepted restrictive intent")
	}
	var state string
	if err := f.control.QueryRow(f.ctx, "SELECT state FROM play_sessions WHERE id=$1", fixture.graph.playID).Scan(&state); err != nil || state != "Playing" {
		t.Fatal("failed report invented a durable terminal state")
	}
	release()
	if status := fixture.stop(t, f.ctx); status != http.StatusNoContent {
		t.Fatalf("complete fresh same-play retry did not reuse its Stop lane: status=%d", status)
	}
	if usage := f.app.playbackStopIntents.usage(); usage.StopReservations != 0 || usage.StopOwners != 0 {
		t.Fatal("successful same-play retry retained its undurable reservation")
	}
}

func TestHTTPPlaybackStoppedOwnedHLSShareWaitExpiryDoesNotMint(t *testing.T) {
	fixture := newStoppedOwnedHLSMatrixFixture(t)
	f := fixture.real.control
	before := f.app.playbackStopIntents.usage()
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	barrier, err := f.control.Begin(ctx)
	if err != nil {
		t.Fatal("begin the actual canonical SHARE deadline barrier")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := barrier.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error("canonical deadline barrier did not release")
		}
	}()
	changed, err := barrier.Exec(ctx, "UPDATE play_sessions SET expires_at=clock_timestamp()+interval '500 milliseconds' WHERE id=$1", fixture.graph.playID)
	if err != nil || changed.RowsAffected() != 1 {
		t.Fatal("hold the exact canonical play until its real database deadline")
	}
	var called atomic.Bool
	result := make(chan error, 1)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		_, err := f.app.library.GetPlaybackSessionAdmitted(ctx, playbackOwner(fixture.principal), fixture.graph.playID,
			func(library.PlaySession) (func(), error) { called.Store(true); return nil, nil })
		result <- err
	}()
	t.Cleanup(func() {
		cancel()
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancelCleanup()
		_ = barrier.Rollback(cleanup)
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("canonical SHARE deadline validation did not join")
		}
	})
	observed := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var waiters int64
		if err := f.control.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity activity WHERE activity.datname=current_database()
			AND $1::integer=ANY(pg_blocking_pids(activity.pid)) AND activity.wait_event_type='Lock'
			AND activity.query LIKE '%play_sessions%' AND activity.query LIKE '%FOR SHARE%'`, barrier.Conn().PgConn().PID()).Scan(&waiters); err != nil {
			t.Fatal("observe the actual canonical SHARE row waiter")
		}
		if waiters == 1 {
			observed = true
			break
		}
		select {
		case <-joined:
			t.Fatal("admitted validation returned before its actual SHARE wait")
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !observed || called.Load() {
		t.Fatal("the deadline fixture did not hold one real SHARE validation before its mint")
	}
	for {
		var expired bool
		if err := barrier.QueryRow(ctx, "SELECT expires_at<=clock_timestamp() FROM play_sessions WHERE id=$1", fixture.graph.playID).Scan(&expired); err != nil {
			t.Fatal("observe the exact database deadline while its row remains held")
		}
		if expired {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the held play did not reach its database deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := barrier.Commit(ctx); err != nil {
		t.Fatal("release the actual expired canonical row")
	}
	select {
	case err := <-result:
		if !errors.Is(err, library.ErrNotFound) || called.Load() || f.app.playbackStopIntents.usage() != before {
			t.Fatal("post-SHARE fresh clock checking minted an expired lifetime")
		}
	case <-ctx.Done():
		t.Fatal("the actual SHARE expiry validation did not finish")
	}
	<-joined
	t.Log("actual_canonical_share_wait_expiry_rejected_before_mint=true producer_acceptance_claimed=false")
}

// Four abandoned durable reports retain their existing restrictive reservations.
// A full early lane must still permit the fifth report's normal durable Stop.
func TestHTTPPlaybackStoppedOwnedHLSFourFailedStopsKeepFifthFallbackAvailable(t *testing.T) {
	first := newStoppedOwnedHLSMatrixFixture(t)
	f, h := first.real.control, first.real.hls
	fixtures := []stoppedOwnedHLSMatrixFixture{first}
	for len(fixtures) < 5 {
		principal, headers := f.principal(t, "normal")
		graph := h.graph(t, clientSessionHTTPLogin{id: principal.SessionID, userID: principal.User.ID,
			deviceID: principal.Client.DeviceID, headers: headers}, 0)
		f.app.hls.mu.Lock()
		session := f.app.hls.sessions[graph.hlsID]
		f.app.hls.mu.Unlock()
		if session == nil || session.playbackReference == nil {
			t.Fatal("each failed Stop requires its independent actual owned registration")
		}
		for _, prior := range fixtures {
			if prior.graph.playID == graph.playID || prior.principal.SessionID == principal.SessionID {
				t.Fatal("Stop lane fixture reused a canonical play or credential")
			}
		}
		expectStatus(t, playbackControlRequest(f.ctx, f.handler, "/emby/Sessions/Playing", map[string]any{
			"PlaySessionId": graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID)}, headers), http.StatusNoContent)
		fixtures = append(fixtures, stoppedOwnedHLSMatrixFixture{real: first.real, principal: principal, headers: headers, graph: graph, session: session})
	}
	barrier, err := f.control.Begin(f.ctx)
	if err != nil {
		t.Fatal("begin the actual four-abandoned-report userdata barrier")
	}
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := barrier.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Error("four-abandoned-report barrier did not release")
			}
		})
	}
	defer release()
	t.Cleanup(release)
	if err := barrier.QueryRow(f.ctx, "SELECT user_id FROM user_item_data WHERE user_id=$1 AND item_id=$2 FOR UPDATE", first.principal.User.ID, h.item.ID).Scan(new(string)); err != nil {
		t.Fatal("hold the actual shared userdata row for four abandoned Stops")
	}
	for index := 0; index < 4; index++ {
		ctx, cancel := context.WithTimeout(f.ctx, 500*time.Millisecond)
		status := fixtures[index].stop(t, ctx)
		cancel()
		usage := f.app.playbackStopIntents.usage()
		if status != http.StatusServiceUnavailable || usage.StopReservations != index+1 || usage.StopOwners != 0 {
			t.Fatal("an actual abandoned durable report did not retain its one Stop reservation")
		}
	}
	ctx, cancel := context.WithTimeout(f.ctx, 500*time.Millisecond)
	status := fixtures[4].stop(t, ctx)
	cancel()
	if status != http.StatusServiceUnavailable || f.app.playbackStopIntents.usage().StopReservations != 4 ||
		f.app.playbackStopIntents.blocked(fixtures[4].session.key.scope) {
		t.Fatalf("a failed fifth normal Stop invented early intent or exhausted the bounded lane: status=%d reservations=%d blocked=%t",
			status, f.app.playbackStopIntents.usage().StopReservations,
			f.app.playbackStopIntents.blocked(fixtures[4].session.key.scope))
	}
	var state string
	if err := f.control.QueryRow(f.ctx, "SELECT state FROM play_sessions WHERE id=$1", fixtures[4].graph.playID).Scan(&state); err != nil || state != "Playing" {
		t.Fatal("a failed full-lane fallback invented a terminal fifth playback report")
	}
	release()
	if status := fixtures[4].stop(t, f.ctx); status != http.StatusNoContent {
		t.Fatalf("a full early lane denied the fifth normal Stop after userdata release: status=%d", status)
	}
	if usage := f.app.playbackStopIntents.usage(); usage.StopReservations != 4 || usage.StopOwners != 0 {
		t.Fatal("a successful fifth fallback changed the four uncommitted reservations")
	}
	for _, fixture := range fixtures[:4] {
		if status := fixture.stop(t, f.ctx); status != http.StatusNoContent {
			t.Fatal("fresh same-play retry did not drain the actual reserved Stop lane after release")
		}
	}
	if usage := f.app.playbackStopIntents.usage(); usage != (playbackStopIntentUsage{}) {
		t.Fatal("all terminal retries and registry owners did not drain the bounded lane")
	}
	t.Log("actual_failed_stops=4 fifth_normal_commit204=true bounded_lane_unchanged=true producer_acceptance_claimed=false")
}

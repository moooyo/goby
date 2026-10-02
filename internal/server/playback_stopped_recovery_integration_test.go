//go:build linux

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

func stoppedRecoveryUserDataBarrier(t *testing.T, fixture stoppedOwnedHLSMatrixFixture) (pgx.Tx, func()) {
	t.Helper()
	f := fixture.real.control
	ctx, cancel := context.WithTimeout(f.ctx, 20*time.Second)
	t.Cleanup(cancel)
	barrier, err := f.control.Begin(ctx)
	if err != nil {
		t.Fatal("begin the real recovery userdata barrier")
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := barrier.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Error("the real recovery userdata barrier did not release")
			}
		})
	}
	t.Cleanup(release)
	if err := barrier.QueryRow(ctx, "SELECT user_id FROM user_item_data WHERE user_id=$1 AND item_id=$2 FOR UPDATE",
		fixture.principal.User.ID, fixture.real.hls.item.ID).Scan(new(string)); err != nil {
		t.Fatal("hold the exact real recovery userdata row")
	}
	return barrier, release
}

func stoppedRecoveryWaitLockedQuery(t *testing.T, fixture stoppedOwnedHLSMatrixFixture, barrier pgx.Tx, relation, lock string) {
	t.Helper()
	f := fixture.real.control
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		var count int64
		if err := f.control.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity activity WHERE activity.datname=current_database()
			AND $1::integer=ANY(pg_blocking_pids(activity.pid)) AND activity.wait_event_type='Lock'
			AND activity.query LIKE $2 AND activity.query LIKE $3`, barrier.Conn().PgConn().PID(), "%"+relation+"%", "%"+lock+"%").Scan(&count); err != nil {
			t.Fatal("observe the exact real recovery row wait")
		}
		if count == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the recovery request did not enter its actual database row wait")
}

func stoppedRecoveryLateOwners(t *testing.T, fixture stoppedOwnedHLSMatrixFixture) (*hlsPlaybackSource, *playbackOwnedInput, *playbackAdmissionReference, library.MediaFile) {
	t.Helper()
	f := fixture.real.control
	var metadata *playbackAdmissionReference
	_, err := f.app.library.GetPlaybackSessionAdmitted(f.ctx, playbackOwner(fixture.principal), fixture.graph.playID,
		func(current library.PlaySession) (func(), error) {
			var err error
			metadata, err = f.app.playbackStopIntents.acquireValidated(playbackScopeForSession(current))
			if err != nil {
				return nil, err
			}
			return metadata.release, nil
		})
	if err != nil || metadata == nil {
		t.Fatal("mint the actual late fresh Get owner during userdata wait")
	}
	t.Cleanup(metadata.release)
	loan, source, err := f.app.hls.authorizePlaybackSource(f.ctx, fixture.principal, fixture.session)
	if err != nil || loan == nil || loan.owned == nil {
		t.Fatal("open the real late independently owned source")
	}
	t.Cleanup(func() { _ = loan.close() })
	input, err := loan.owned.duplicate()
	if err != nil {
		t.Fatal("duplicate the actual late owned descriptor before Stop commit")
	}
	t.Cleanup(func() { _ = f.app.hls.closePlaybackInput(input) })
	return loan, input, metadata, source
}

func stoppedRecoveryRejectLateOwners(t *testing.T, fixture stoppedOwnedHLSMatrixFixture, loan *hlsPlaybackSource, input *playbackOwnedInput, metadata *playbackAdmissionReference, source library.MediaFile) {
	t.Helper()
	f, session := fixture.real.control, fixture.session
	if _, err := input.ensure(context.Background(), f.app.hls.manager, transcode.Spec{Scope: session.key.scope,
		SourceStamp: session.key.stamp, Plan: session.key.plan}); !errors.Is(err, transcode.ErrJobCancelled) {
		t.Fatal("a delayed actual old input recreated work after fallback commit")
	}
	decision := playback.ConversionDecision{Plan: &session.key.plan, OutputSource: session.output, SubtitleView: session.subtitleView}
	if _, _, err := f.app.hls.registerWithPlaybackOwner(fixture.principal, source, fixture.graph.playID, decision, 0, metadata); !errors.Is(err, transcode.ErrJobCancelled) {
		t.Fatal("late admitted metadata republished a registration after fallback commit")
	}
	if _, err := f.app.hls.freshRegistrationOwner(f.ctx, fixture.principal, source, fixture.graph.playID, decision); !errors.Is(err, library.ErrNotFound) {
		t.Fatal("a new registration bypassed fresh terminal DB validation")
	}
	metadata.release()
	if err := loan.close(); err != nil {
		t.Fatal("close the actual delayed fallback source loan")
	}
}

func stoppedRecoveryFiveFixtures(t *testing.T) []stoppedOwnedHLSMatrixFixture {
	t.Helper()
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
			t.Fatal("each recovery scope requires its real registration owner")
		}
		for _, prior := range fixtures {
			if graph.playID == prior.graph.playID || principal.SessionID == prior.principal.SessionID {
				t.Fatal("recovery scopes reused a play or credential")
			}
		}
		expectStatus(t, playbackControlRequest(f.ctx, f.handler, "/emby/Sessions/Playing", map[string]any{
			"PlaySessionId": graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID)}, headers), http.StatusNoContent)
		fixtures = append(fixtures, stoppedOwnedHLSMatrixFixture{real: first.real, principal: principal, headers: headers, graph: graph, session: session})
	}
	return fixtures
}

func stoppedRecoveryStartProgressive(t *testing.T, fixture stoppedOwnedHLSMatrixFixture) playbackStopAliasEncoder {
	t.Helper()
	f, h := fixture.real.control, fixture.real.hls
	body := videoHTTPBody(true, videoHTTPProfile("http", true, true))
	body["CurrentPlaySessionId"] = fixture.graph.playID
	response := h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", body, fixture.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	var decoded struct {
		PlayID  string           `json:"PlaySessionId"`
		Sources []map[string]any `json:"MediaSources"`
		Error   string           `json:"ErrorCode"`
	}
	if json.Unmarshal(response.body, &decoded) != nil || decoded.PlayID != fixture.graph.playID || decoded.Error != "" || len(decoded.Sources) != 1 ||
		decoded.Sources[0]["TranscodingSubProtocol"] != "http" || decoded.Sources[0]["TranscodingContainer"] != "mp4" {
		t.Fatal("natural progressive negotiation did not preserve the same canonical play")
	}
	target, ok := decoded.Sources[0]["TranscodingUrl"].(string)
	if !ok || target == "" {
		t.Fatal("natural progressive negotiation omitted its actual delivery URI")
	}
	ctx, cancel := context.WithCancel(f.ctx)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.server.URL+target, nil)
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
			t.Error("the actual progressive recovery consumer did not join")
		}
	})
	process := playbackStopAliasObserveEncoder(t, fixture.real, fixture.session.key.scope)
	record, err := f.app.hls.manager.Snapshot(fixture.session.key.scope, process.jobID)
	if err != nil || record.Spec.Plan.OutputMode != "progressive" || record.Spec.Plan.SourceMode == "stream" {
		t.Fatal("the observed real producer is not the naturally negotiated same-play progressive output")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, err := hlsWallPauseReadProcess(process.pid, process.startTick)
		if err != nil {
			t.Fatal("the exact progressive producer disappeared before its work witness")
		}
		if current.UserTicks+current.SystemTicks > process.first.UserTicks+process.first.SystemTicks &&
			(current.ReadChars > process.first.ReadChars || current.WriteChars > process.first.WriteChars) {
			return process
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the actual same-play progressive producer did not show CPU and character-IO growth")
	return playbackStopAliasEncoder{}
}

func TestHTTPPlaybackStoppedRecoveryFullLaneCommitsAndFencesLateOwners(t *testing.T) {
	fixtures := stoppedRecoveryFiveFixtures(t)
	fifth, f := fixtures[4], fixtures[0].real.control
	process := stoppedRecoveryStartProgressive(t, fifth)
	barrier, release := stoppedRecoveryUserDataBarrier(t, fixtures[0])
	defer release()
	for index := 0; index < 4; index++ {
		ctx, cancel := context.WithTimeout(f.ctx, 500*time.Millisecond)
		status := fixtures[index].stop(t, ctx)
		cancel()
		if status != http.StatusServiceUnavailable || f.app.playbackStopIntents.usage().StopReservations != index+1 {
			t.Fatal("a real abandoned report did not retain its restrictive reservation")
		}
	}
	// A full lane and a failed fifth report cannot invent accepted intent.
	failed, cancelFailed := context.WithTimeout(f.ctx, 500*time.Millisecond)
	failedStatus := fifth.stop(t, failed)
	cancelFailed()
	if failedStatus != http.StatusServiceUnavailable || f.app.playbackStopIntents.blocked(fifth.session.key.scope) || f.app.playbackStopIntents.usage().StopReservations != 4 {
		t.Fatal("failed full-lane fallback pretended to accept a fifth early intent")
	}
	stopCtx, cancelStop := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancelStop()
	response := make(chan *httptest.ResponseRecorder, 1)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		response <- playbackControlRequest(stopCtx, f.handler, "/emby/Sessions/Playing/Stopped", map[string]any{
			"PlaySessionId": fifth.graph.playID, "ItemId": fifth.real.hls.item.ID, "MediaSourceId": media.SourceID(fifth.real.hls.item.ID)}, fifth.headers)
	}()
	t.Cleanup(func() {
		release()
		cancelStop()
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("the fallback Stop did not join")
		}
	})
	stoppedRecoveryWaitLockedQuery(t, fifth, barrier, "user_item_data", "FOR UPDATE")
	loan, input, metadata, source := stoppedRecoveryLateOwners(t, fifth)
	if f.app.playbackStopIntents.blocked(fifth.session.key.scope) {
		t.Fatal("the fifth fallback cancelled its old owners before actual commit")
	}
	select {
	case <-response:
		t.Fatal("full-lane fallback returned instead of waiting for userdata")
	default:
	}
	state, start, err := hlsColdPID(process.pid)
	record, recordErr := f.app.hls.manager.Snapshot(fifth.session.key.scope, process.jobID)
	if err != nil || start != process.startTick || state == 'Z' || recordErr != nil || record.State != "running" {
		t.Fatal("full-lane fallback cancelled the actual same-play progressive producer before commit")
	}
	release()
	select {
	case result := <-response:
		expectStatus(t, result, http.StatusNoContent)
	case <-stopCtx.Done():
		t.Fatal("the fallback Stop did not complete after userdata release")
	}
	select {
	case <-joined:
	case <-stopCtx.Done():
		t.Fatal("the fallback Stop response did not join")
	}
	stoppedOwnedHLSMatrixRetired(t, process)
	if !f.app.playbackStopIntents.blocked(fifth.session.key.scope) {
		t.Fatal("actual commit did not fence the current late-owned entry before post-commit cancellation")
	}
	stoppedRecoveryRejectLateOwners(t, fifth, loan, input, metadata, source)
	for _, fixture := range fixtures[:4] {
		expectStatus(t, playbackControlRequest(f.ctx, f.handler, "/emby/Sessions/Playing/Ping?PlaySessionId="+url.QueryEscape(fixture.graph.playID), nil, fixture.headers), http.StatusNoContent)
	}
	// Tag the actual single-query maintenance calls, excluding background work.
	check, cancelCheck := context.WithTimeout(f.ctx, 4*time.Second)
	traced, counts := f.traceAcquisitions(check)
	f.app.hls.recoverPlaybackStopIntents(traced)
	cancelCheck()
	if counts.dataSuccesses.Load() != 4 || counts.controlAttempts.Load() != 0 || counts.unexpectedAttempts.Load() != 0 || f.app.playbackStopIntents.usage().StopReservations != 4 {
		t.Fatal("live/Ping recovery used another lane or forgot the four undurable intents")
	}
	for _, fixture := range fixtures[:4] {
		play, _, err := f.app.library.ReportPlayback(f.ctx, playbackOwner(fixture.principal), library.PlaybackReport{Event: "Stopped",
			PlaySessionID: fixture.graph.playID, ItemID: fixture.real.hls.item.ID, MediaSourceID: media.SourceID(fixture.real.hls.item.ID)})
		if err != nil || play.State != "Stopped" {
			t.Fatal("commit actual terminal rows through the existing fully validated vanilla report")
		}
	}
	f.app.hls.recoverPlaybackStopIntents(f.ctx)
	if usage := f.app.playbackStopIntents.usage(); usage != (playbackStopIntentUsage{}) {
		t.Fatal("actual terminal witnesses and all old holders did not recover the bounded lane")
	}
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancelCleanup()
	if err := f.app.hls.Close(cleanup); err != nil {
		t.Fatal("join the actual progressive producer and all HLS owners")
	}
	resources, ok := f.app.hls.manager.(interface {
		ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
	})
	if !ok {
		t.Fatal("actual recovery cleanup lacks scoped resource evidence")
	}
	usage, err := resources.ResourceUsage(cleanup, fifth.session.key.scope)
	if err != nil || usage != (transcode.ResourceUsage{}) || playbackStopAliasSourceFDs(t, fifth.real.source) != 0 {
		t.Fatal("actual recovery process/source/scoped resources survived join")
	}
	if _, err := os.Stat(filepath.Join(f.app.cfg.Transcoding.CacheDirectory, process.jobID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the actual progressive fallback cache survived join")
	}
	t.Log("actual_failed_stops=4 fifth_normal_commit204=true late_input_and_registration_fenced=true same_play_progressive_running_before_commit=true terminal_witness_data_queries=4 original_emby_player_used=false universal_early_stop_claimed=false")
}

func TestHTTPPlaybackStoppedRecoveryNoInitialEntryFencesOwnersMintedDuringWait(t *testing.T) {
	fixture := newStoppedOwnedHLSMatrixFixture(t)
	f := fixture.real.control
	old := fixture.session
	f.app.hls.retire(old)
	if f.app.playbackStopIntents.usage() != (playbackStopIntentUsage{}) {
		t.Fatal("the no-entry fixture retained an initial owner")
	}
	barrier, release := stoppedRecoveryUserDataBarrier(t, fixture)
	defer release()
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	result, joined := make(chan int, 1), make(chan struct{})
	go func(current stoppedOwnedHLSMatrixFixture) { defer close(joined); result <- current.stop(t, ctx) }(fixture)
	t.Cleanup(func() {
		release()
		cancel()
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("no-entry fallback did not join")
		}
	})
	stoppedRecoveryWaitLockedQuery(t, fixture, barrier, "user_item_data", "FOR UPDATE")
	// Register naturally after the factory has observed no entry. The fresh
	// owned Get and unchanged final AUTH/source checks are still mandatory.
	file, source, err := f.app.hls.verify(f.ctx, fixture.principal, old.key.scope, old.key.stamp, old.key.plan)
	if err != nil {
		t.Fatal("freshly open the authorized no-entry planning source")
	}
	if err := file.Close(); err != nil {
		t.Fatal("close the actual no-entry planning descriptor")
	}
	decision := playback.ConversionDecision{Plan: &old.key.plan, OutputSource: old.output, SubtitleView: old.subtitleView}
	late, err := f.app.hls.registerVerified(f.ctx, fixture.principal, source, fixture.graph.playID, decision, 0)
	if err != nil || late == nil || late.playbackReference == nil {
		t.Fatal("fresh metadata did not establish its actual late owner during userdata wait")
	}
	fixture.session = late
	loan, input, metadata, current := stoppedRecoveryLateOwners(t, fixture)
	if f.app.playbackStopIntents.blocked(old.key.scope) || f.app.playbackStopIntents.usage().StopReservations != 0 {
		t.Fatal("a no-entry early snapshot invented a restrictive intent")
	}
	release()
	select {
	case status := <-result:
		if status != http.StatusNoContent {
			t.Fatal("no-entry fallback did not commit its standard report")
		}
	case <-ctx.Done():
		t.Fatal("no-entry fallback did not finish")
	}
	select {
	case <-joined:
	case <-ctx.Done():
		t.Fatal("no-entry fallback did not join")
	}
	stoppedRecoveryRejectLateOwners(t, fixture, loan, input, metadata, current)
	if usage := f.app.playbackStopIntents.usage(); usage != (playbackStopIntentUsage{}) {
		t.Fatal("the no-entry late owners did not collect after actual commit/rejection")
	}
	t.Log("actual_factory_initial_entry_absent=true actual_late_get_and_source_owners_fenced_at_commit=true original_emby_player_used=false")
}

func TestHTTPPlaybackStoppedRecoveryWitnessRechecksLocksScopeAndRealOwners(t *testing.T) {
	fixture := newStoppedOwnedHLSMatrixFixture(t)
	f, h := fixture.real.control, fixture.real.hls
	loan, input, metadata, _ := stoppedRecoveryLateOwners(t, fixture)
	_, release := stoppedRecoveryUserDataBarrier(t, fixture)
	ctx, cancel := context.WithTimeout(f.ctx, 500*time.Millisecond)
	status := fixture.stop(t, ctx)
	cancel()
	release()
	if status != http.StatusServiceUnavailable || f.app.playbackStopIntents.usage().StopReservations != 1 {
		t.Fatal("the witness fixture did not retain an actual failed-report intent")
	}
	scope := fixture.session.key.scope
	confirm := func(ctx context.Context) (bool, error) {
		return f.app.library.ConfirmOwnedPlaybackTerminal(ctx, playbackOwner(fixture.principal), fixture.graph.playID, h.item.ID, media.SourceID(h.item.ID))
	}
	if terminal, err := confirm(f.ctx); err != nil || terminal {
		t.Fatal("a live Playing row became terminal evidence")
	}
	expectStatus(t, playbackControlRequest(f.ctx, f.handler, "/emby/Sessions/Playing/Ping?PlaySessionId="+url.QueryEscape(fixture.graph.playID), nil, fixture.headers), http.StatusNoContent)
	if terminal, err := confirm(f.ctx); err != nil || terminal {
		t.Fatal("Ping or elapsed cancellation state became terminal evidence")
	}
	play, _, err := f.app.library.ReportPlayback(f.ctx, playbackOwner(fixture.principal), library.PlaybackReport{Event: "Stopped",
		PlaySessionID: fixture.graph.playID, ItemID: h.item.ID, MediaSourceID: media.SourceID(h.item.ID)})
	if err != nil || play.State != "Stopped" {
		t.Fatal("commit an independently fully validated real terminal row")
	}
	traced, counts := f.traceAcquisitions(f.ctx)
	if terminal, err := confirm(traced); err != nil || !terminal || counts.dataSuccesses.Load() != 1 || counts.controlAttempts.Load() != 0 {
		t.Fatal("the real terminal witness did not use exactly ordinary Data")
	}
	gate := &f.app.playbackStopIntents
	gate.mu.Lock()
	candidate := playbackStopRecoveryCandidate{entry: gate.entries[playbackStopIntentKeyFor(scope)], scope: scope}
	gate.mu.Unlock()
	if gate.recoverTerminal(candidate) || gate.recoveryCandidates()[0].entry != nil {
		t.Fatal("real held source/metadata owners were collected by terminal evidence alone")
	}
	foreign := playbackOwner(fixture.principal)
	foreign.SessionID = "foreign-authentication"
	if terminal, err := f.app.library.ConfirmOwnedPlaybackTerminal(f.ctx, foreign, fixture.graph.playID, h.item.ID, media.SourceID(h.item.ID)); err != nil || terminal {
		t.Fatal("foreign credential identity received terminal proof")
	}
	if terminal, err := f.app.library.ConfirmOwnedPlaybackTerminal(f.ctx, playbackOwner(fixture.principal), fixture.graph.playID, "foreign-item", media.SourceID("foreign-item")); err != nil || terminal {
		t.Fatal("foreign catalog/source identity received terminal proof")
	}
	if terminal, _ := f.app.library.ConfirmOwnedPlaybackTerminal(f.ctx, playbackOwner(fixture.principal), fixture.graph.playID, h.item.ID, "foreign-source"); terminal {
		t.Fatal("a mismatched source received terminal proof")
	}
	// First require a real SHARE-lock timeout, then update the actual tuple's
	// state and immutable scope while a separate SHARE waiter is blocked.
	for _, change := range []string{"state", "scope"} {
		locked, err := f.control.Begin(f.ctx)
		if err != nil {
			t.Fatal("begin the actual terminal recheck barrier")
		}
		closeLock := func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := locked.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Error("terminal recheck barrier did not release")
			}
		}
		t.Cleanup(closeLock)
		var command string
		if change == "state" {
			command = "UPDATE play_sessions SET state='Playing' WHERE id=$1"
		} else {
			command = "UPDATE play_sessions SET device_id='different-device' WHERE id=$1"
		}
		updated, err := locked.Exec(f.ctx, command, fixture.graph.playID)
		if err != nil || updated.RowsAffected() != 1 {
			t.Fatal("change the exact actual locked terminal tuple")
		}
		short, cancelShort := context.WithTimeout(f.ctx, 150*time.Millisecond)
		terminal, timeoutErr := confirm(short)
		cancelShort()
		if terminal || timeoutErr == nil {
			t.Fatal("an actual locked/timeout witness claimed terminal proof")
		}
		waiting, cancelWaiting := context.WithTimeout(f.ctx, 5*time.Second)
		type outcome struct {
			terminal bool
			err      error
		}
		result, joined := make(chan outcome, 1), make(chan struct{})
		go func() {
			defer close(joined)
			terminal, err := confirm(waiting)
			result <- outcome{terminal: terminal, err: err}
		}()
		t.Cleanup(func() {
			closeLock()
			cancelWaiting()
			select {
			case <-joined:
			case <-time.After(5 * time.Second):
				t.Error("the actual terminal SHARE witness did not join")
			}
		})
		stoppedRecoveryWaitLockedQuery(t, fixture, locked, "play_sessions", "FOR SHARE")
		if err := locked.Commit(f.ctx); err != nil {
			t.Fatal("publish the adversarial actual tuple change after the SHARE wait")
		}
		select {
		case observed := <-result:
			if observed.err != nil || observed.terminal {
				t.Fatal("post-lock tuple recheck accepted changed state/scope")
			}
		case <-waiting.Done():
			t.Fatal("the actual terminal recheck did not finish")
		}
		select {
		case <-joined:
		case <-waiting.Done():
			t.Fatal("the actual terminal recheck did not join")
		}
		cancelWaiting()
		if change == "state" {
			play, _, err := f.app.library.ReportPlayback(f.ctx, playbackOwner(fixture.principal), library.PlaybackReport{Event: "Stopped",
				PlaySessionID: fixture.graph.playID, ItemID: h.item.ID, MediaSourceID: media.SourceID(h.item.ID)})
			if err != nil || play.State != "Stopped" && play.State != "Expired" {
				t.Fatalf("restore an actual durable terminal row for the next tuple recheck: state=%s error_type=%T", play.State, err)
			}
		} else {
			updated, err := f.control.Exec(f.ctx, "UPDATE play_sessions SET device_id=$2 WHERE id=$1", fixture.graph.playID, fixture.principal.Client.DeviceID)
			if err != nil || updated.RowsAffected() != 1 {
				t.Fatal("restore the deliberately changed fixture identity")
			}
		}
	}
	if terminal, err := confirm(f.ctx); err != nil || !terminal {
		t.Fatal("the restored complete actual terminal identity lost its proof")
	}
	if err := f.app.hls.closePlaybackInput(input); err != nil {
		t.Fatal("close the actual retained duplicate before recovery")
	}
	metadata.release()
	if err := loan.close(); err != nil {
		t.Fatal("close the actual outer source before recovery")
	}
	f.app.hls.recoverPlaybackStopIntents(f.ctx)
	if usage := gate.usage(); usage != (playbackStopIntentUsage{}) {
		t.Fatal("a matching actual terminal row plus real owner join did not collect")
	}
	t.Log("actual_terminal_witness_data_only=true share_timeout_and_changed_state_scope_rejected=true live_ping_foreign_no_witness=true actual_held_fd_and_metadata_join_required=true producer_qualification_claimed=false")
}

func TestHTTPPlaybackStoppedRecoveryDeletedRowDoesNotProveTerminal(t *testing.T) {
	// Deleted rows provide no evidence. A held real owner keeps the deliberately
	// retained entry alive while its independently committed terminal row is deleted.
	deleted := newStoppedOwnedHLSMatrixFixture(t)
	df, dh := deleted.real.control, deleted.real.hls
	dloan, dinput, dmetadata, _ := stoppedRecoveryLateOwners(t, deleted)
	_, drelease := stoppedRecoveryUserDataBarrier(t, deleted)
	dctx, dcancel := context.WithTimeout(df.ctx, 500*time.Millisecond)
	dstatus := deleted.stop(t, dctx)
	dcancel()
	drelease()
	if dstatus != http.StatusServiceUnavailable {
		t.Fatal("deleted-row fixture did not fail a real durable report")
	}
	terminalPlay, _, err := df.app.library.ReportPlayback(df.ctx, playbackOwner(deleted.principal), library.PlaybackReport{Event: "Stopped",
		PlaySessionID: deleted.graph.playID, ItemID: dh.item.ID, MediaSourceID: media.SourceID(dh.item.ID)})
	if err != nil || terminalPlay.State != "Stopped" {
		t.Fatal("commit the real terminal row before testing deletion")
	}
	removed, err := df.control.Exec(df.ctx, "DELETE FROM play_sessions WHERE id=$1", deleted.graph.playID)
	if err != nil || removed.RowsAffected() != 1 {
		t.Fatal("delete the exact terminal fixture row")
	}
	if err := df.app.hls.closePlaybackInput(dinput); err != nil {
		t.Fatal("close the actual deleted-row duplicate")
	}
	dmetadata.release()
	if err := dloan.close(); err != nil {
		t.Fatal("close the actual deleted-row outer source")
	}
	if terminal, err := df.app.library.ConfirmOwnedPlaybackTerminal(df.ctx, playbackOwner(deleted.principal), deleted.graph.playID, dh.item.ID, media.SourceID(dh.item.ID)); err != nil || terminal {
		t.Fatal("NotFound/deletion was inferred to be terminal evidence")
	}
	df.app.hls.recoverPlaybackStopIntents(df.ctx)
	if df.app.playbackStopIntents.usage().StopReservations != 1 {
		t.Fatal("a deleted row caused undurable intent to be forgotten")
	}
	t.Log("actual_deleted_row_no_terminal_witness=true undurable_intent_retained=true producer_qualification_claimed=false")
}

func TestHTTPPlaybackStoppedRecoveryMaintenanceReservesTerminalWitnessBudget(t *testing.T) {
	fixture := newStoppedOwnedHLSMatrixFixture(t)
	f, h := fixture.real.control, fixture.real.hls
	_, release := stoppedRecoveryUserDataBarrier(t, fixture)
	ctx, cancel := context.WithTimeout(f.ctx, 500*time.Millisecond)
	status := fixture.stop(t, ctx)
	cancel()
	release()
	if status != http.StatusServiceUnavailable || f.app.playbackStopIntents.usage().StopReservations != 1 {
		t.Fatal("the scheduling fixture did not retain a failed durable Stop")
	}
	// Stop only this fixture's original maintenance loop. The real terminal
	// row must be collected by the cycle under test, not by a concurrent tick.
	f.app.hls.cancel()
	runtime, _ := hlsRuntimeTestFixture(t)
	sessions := make([]*hlsSession, 0, 25)
	for index := 0; index < 24; index++ {
		session := hlsRuntimeTestSession(t, runtime, fmt.Sprintf("slow-recovery-check-%02d", index), true)
		previous := session.key
		// These controlled callbacks test scheduling without claiming actual
		// encoder or fresh-AUTH qualification for their synthetic registrations.
		session.key.plan.OutputMode = "progressive"
		delete(runtime.byKey, previous)
		runtime.byKey[session.key] = session
		sessions = append(sessions, session)
	}
	idle := hlsRuntimeTestSession(t, runtime, "idle-before-recovery-check", false)
	idle.accessed = time.Now().Add(-hlsIdleTTL - time.Second)
	sessions = append(sessions, idle)
	runtime.server = f.app
	entered := make(chan struct{}, len(sessions))
	var active, maximum, attempts atomic.Int32
	runtime.verify = func(ctx context.Context, _ identity.Principal, _ transcode.Scope, _ string, _ transcode.Plan) (*os.File, library.MediaFile, error) {
		attempts.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		for before := maximum.Load(); current > before && !maximum.CompareAndSwap(before, current); before = maximum.Load() {
		}
		entered <- struct{}{}
		<-ctx.Done()
		return nil, library.MediaFile{}, ctx.Err()
	}
	cycle, cancelCycle := context.WithTimeout(f.ctx, 4*time.Second)
	defer cancelCycle()
	traced, counts := f.traceAcquisitions(cycle)
	joined := make(chan struct{})
	go func() { defer close(joined); runtime.maintainPlaybackCycle(traced, sessions) }()
	t.Cleanup(func() {
		cancelCycle()
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("the bounded recovery maintenance cycle did not join")
		}
	})
	select {
	case <-entered:
	case <-cycle.Done():
		t.Fatal("the scheduling fixture did not begin its controlled slow checks")
	}
	// Idle sweeping must finish before the first authority callback, even when
	// the remaining active checks would otherwise exhaust the whole cycle.
	if !idle.closed || idle.ctx.Err() == nil {
		t.Fatal("terminal recovery budgeting delayed the existing idle sweep")
	}
	play, _, err := f.app.library.ReportPlayback(f.ctx, playbackOwner(fixture.principal), library.PlaybackReport{Event: "Stopped",
		PlaySessionID: fixture.graph.playID, ItemID: h.item.ID, MediaSourceID: media.SourceID(h.item.ID)})
	if err != nil || play.State != "Stopped" {
		t.Fatal("commit the real terminal row while the controlled maintenance checks wait")
	}
	select {
	case <-joined:
	case <-cycle.Done():
		t.Fatal("ordinary slow maintenance exhausted the terminal witness opportunity")
	}
	if attempts.Load() < int32(hlsMaintenanceWorkers) || maximum.Load() > int32(hlsMaintenanceWorkers) || active.Load() != 0 {
		t.Fatal("the controlled maintenance checks escaped their existing worker bound or did not join")
	}
	for _, session := range sessions[:24] {
		if session.closed || session.ctx.Err() != nil {
			t.Fatal("a transient scheduling timeout retired an unverified registration")
		}
	}
	if counts.dataSuccesses.Load() != 1 || counts.controlAttempts.Load() != 0 || counts.unexpectedAttempts.Load() != 0 ||
		f.app.playbackStopIntents.usage() != (playbackStopIntentUsage{}) {
		t.Fatal("the shared cycle did not collect its actual terminal witness using ordinary Data")
	}
	t.Log("actual_terminal_witness_after_slow_maintenance=true synthetic_authority_callbacks=true idle_sweep_first=true existing_worker_bound_preserved=true producer_qualification_claimed=false")
}

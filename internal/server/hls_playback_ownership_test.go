package server

import (
	"context"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

type hlsPlaybackOwnershipTestJobs struct {
	*hlsRuntimeTestJobs
	fileCancels []hlsRuntimeOpenCall
}

func (jobs *hlsPlaybackOwnershipTestJobs) CancelFileHLSPlayback(auth, play string) {
	jobs.fileCancels = append(jobs.fileCancels, hlsRuntimeOpenCall{scope: transcode.Scope{AuthSessionID: auth, PlaySessionID: play}})
}

// Registry/context/policy tests make no AUTH or actual-encoder claim. The
// separately frozen actual row/matrix fixtures exercise that boundary.
func TestHLSPlaybackEarlyCancellationRetainsOtherTransportsAndSharedPolicy(t *testing.T) {
	h, legacyJobs := hlsRuntimeTestFixture(t)
	jobs := &hlsPlaybackOwnershipTestJobs{hlsRuntimeTestJobs: legacyJobs}
	h.manager = jobs
	app := &Server{hls: h, correlatedHLSOwnershipEnabled: true, correlatedHLSEarlyStopEnabled: true}
	h.server = app
	principal := mediaPolicyTestPrincipal(1)
	scope := mediaPolicyTestScope(principal, "canonical-play")
	work, done, err := app.acquireMediaPolicy(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	defer app.stopMediaPolicy()
	app.touchMediaPolicy(work, principal, scope)
	var sessions []*hlsSession
	for index, plan := range []transcode.Plan{{}, {OutputMode: "hls"}, {OutputMode: "progressive"},
		{SourceMode: "stream", OutputMode: "hls"}, {SourceMode: "stream"}} {
		plan.StartTicks = int64(index)
		ctx, cancel := context.WithCancel(h.ctx)
		session := &hlsSession{id: string(rune('a' + index)), key: hlsKey{scope: scope, plan: plan}, ctx: ctx, cancel: cancel}
		t.Cleanup(cancel)
		if correlatedFileHLSPlan(plan) {
			session.playbackReference, err = app.playbackStopIntents.acquireValidated(scope)
			if err != nil {
				t.Fatal(err)
			}
		}
		h.sessions[session.id], h.byKey[session.key] = session, session
		hlsRuntimeTestOwnProducer(t, h, session)
		sessions = append(sessions, session)
	}
	intent, err := app.playbackStopIntents.acceptValidatedStop(scope)
	if err != nil {
		t.Fatal(err)
	}
	defer intent.finish(true)
	if err := h.cancelFileHLSPlayback(scope.AuthSessionID, scope.PlaySessionID); err != nil {
		t.Fatal(err)
	}
	for _, session := range sessions {
		stopped := correlatedFileHLSPlan(session.key.plan)
		if session.closed != stopped || (session.ctx.Err() != nil) != stopped || (h.sessions[session.id] == nil) != stopped {
			t.Fatal("the early scoped retirement crossed a file-HLS transport boundary")
		}
	}
	if len(jobs.fileCancels) != 1 || jobs.fileCancels[0].scope.AuthSessionID != scope.AuthSessionID ||
		jobs.fileCancels[0].scope.PlaySessionID != scope.PlaySessionID || len(legacyJobs.playbackCancels) != 0 || len(legacyJobs.cancels) != 2 {
		t.Fatal("early retirement used generic playback cancellation or missed a file-HLS owner")
	}
	if work.Err() != nil || app.checkMediaPolicy(principal, scope) != nil {
		t.Fatal("the early intent revoked the lease shared with other transports")
	}
	app.touchMediaPolicy(work, principal, scope)
	play := library.PlaySession{ID: scope.PlaySessionID, UserID: scope.UserID, AuthSessionID: scope.AuthSessionID,
		DeviceID: scope.DeviceID, ItemID: scope.ItemID, MediaSourceID: scope.SourceID, State: "Playing", ExpiresAt: time.Now().Add(time.Hour)}
	if !app.heartbeatPlaybackMediaPolicy(principal, play) {
		t.Fatal("early HLS Stop blocked an uncommitted shared-policy heartbeat")
	}
	next, release, err := app.acquireMediaPolicy(context.Background(), principal, scope)
	if err != nil || next.Err() != nil {
		t.Fatal("early HLS Stop denied another transport's same-play policy admission")
	}
	release()
	for _, session := range sessions {
		h.retire(session)
	}
}

func TestHLSPlaybackEarlyCancellationNeverFallsBackToGenericManager(t *testing.T) {
	h, jobs := hlsRuntimeTestFixture(t)
	if err := h.cancelFileHLSPlayback("auth", "play"); err == nil {
		t.Fatal("an adapter without the narrow sweep claimed complete early cancellation")
	}
	if len(jobs.playbackCancels) != 0 {
		t.Fatal("missing narrow adapter fell back to cross-transport cancellation")
	}
}

func TestHLSPlaybackStoppedRequestFailureDoesNotRetireSharedPolicy(t *testing.T) {
	h, _ := hlsRuntimeTestFixture(t)
	app := &Server{hls: h, correlatedHLSOwnershipEnabled: true}
	h.server = app
	principal := mediaPolicyTestPrincipal(1)
	scope := mediaPolicyTestScope(principal, "canonical-play")
	work, done, err := app.acquireMediaPolicy(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	defer app.stopMediaPolicy()
	intent, err := app.playbackStopIntents.acceptValidatedStop(scope)
	if err != nil {
		t.Fatal(err)
	}
	defer intent.finish(true)
	session := &hlsSession{key: hlsKey{scope: scope, plan: transcode.Plan{OutputMode: "hls"}}}
	app.failHLSMediaPolicy(work, session)
	if work.Err() != nil || len(app.playbackPolicyGate().leases) != 1 {
		t.Fatal("a scoped Stop failure retired an undelivered shared lease")
	}
	// An uncovered transport retains the original startup failure semantics.
	session.key.plan.OutputMode = "progressive"
	app.failHLSMediaPolicy(work, session)
	lease := mediaPolicyContextLease(work)
	if !lease.failed || !lease.complete {
		t.Fatal("the scoped guard changed progressive startup failure handling")
	}
	done()
	if len(app.playbackPolicyGate().leases) != 0 {
		t.Fatal("the established unshared startup rollback stopped retiring its lease")
	}
}

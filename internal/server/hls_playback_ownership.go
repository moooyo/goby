package server

import (
	"context"
	"errors"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

// The first scoped stop cut is file HLS only. Dynamic/progressive and
// unnamed legacy transports retain their existing cancellation ordering.
func correlatedFileHLSPlan(plan transcode.Plan) bool {
	return plan.SourceMode != "stream" && (plan.OutputMode == "" || plan.OutputMode == "hls")
}

func (h *hlsRuntime) usesPlaybackOwnership(plan transcode.Plan) bool {
	return h != nil && h.server != nil && h.server.correlatedHLSOwnershipEnabled && correlatedFileHLSPlan(plan)
}

func (s *Server) holdCorrelatedHLSAdmission(ctx context.Context, spec transcode.Spec) (context.Context, func(), error) {
	if !s.correlatedHLSOwnershipEnabled || !correlatedFileHLSPlan(spec.Plan) {
		return ctx, func() {}, nil
	}
	return s.playbackStopIntents.holdContext(ctx, spec.Scope)
}

func (s *Server) correlatedHLSPlaybackStopped(spec transcode.Spec) bool {
	return s.correlatedHLSOwnershipEnabled && correlatedFileHLSPlan(spec.Plan) && s.playbackStopIntents.blocked(spec.Scope)
}

// A request cancelled by the scoped early intent must not turn its startup
// failure into retirement of a policy lease shared with another transport.
// Other startup failures retain the established generic policy behavior.
func (s *Server) failHLSMediaPolicy(ctx context.Context, session *hlsSession) {
	if session == nil {
		return
	}
	if s.correlatedHLSPlaybackStopped(transcode.Spec{Scope: session.key.scope, Plan: session.key.plan}) {
		return
	}
	s.failMediaPolicy(ctx, session.key.scope)
}

func playbackScopeForSession(play library.PlaySession) transcode.Scope {
	return transcode.Scope{UserID: play.UserID, AuthSessionID: play.AuthSessionID, DeviceID: play.DeviceID,
		ApplicationKey: play.ApplicationKey, ApplicationClientID: play.ApplicationClientID,
		PlaySessionID: play.ID, ItemID: play.ItemID, SourceID: play.MediaSourceID}
}

func playbackScopeForValidatedStop(stop library.ValidatedPlaybackStop) transcode.Scope {
	owner := stop.Owner()
	return transcode.Scope{UserID: owner.UserID, AuthSessionID: owner.SessionID, DeviceID: owner.DeviceID,
		ApplicationKey: owner.ApplicationKey, ApplicationClientID: owner.ApplicationClientID,
		PlaySessionID: stop.PlaySessionID(), ItemID: stop.ItemID(), SourceID: stop.MediaSourceID()}
}

// This factory is intentionally inactive until the production owner chain is
// qualified. Early retirement requires an owned correlated file-HLS scope.
// A committed normal report also fences current owners that arrived while its
// userdata write was waiting, without minting a new entry or reservation.
func (s *Server) correlatedHLSValidatedStop(stop library.ValidatedPlaybackStop) (library.PlaybackValidatedStopAction, error) {
	if !s.correlatedHLSOwnershipEnabled || !s.correlatedHLSEarlyStopEnabled || stop.IsDynamic() {
		return library.PlaybackValidatedStopAction{}, nil
	}
	scope := playbackScopeForValidatedStop(stop)
	gate := &s.playbackStopIntents
	// A successful complete report fences whichever exact entry exists at
	// commit, including owners minted while userdata was waiting. This action
	// does not mint an entry or claim an early intent on a failed report.
	action := library.PlaybackValidatedStopAction{Finish: func(committed bool) {
		if committed {
			gate.publishCommittedTerminal(scope)
		}
	}}
	gate.mu.Lock()
	entry := gate.entries[playbackStopIntentKeyFor(scope)]
	owned := entry != nil && entry.scope == scope && (entry.references > 0 || entry.stopReserved || entry.stopOwners > 0)
	gate.mu.Unlock()
	if !owned {
		return action, nil
	}
	reference, err := gate.acceptValidatedStop(scope)
	if err != nil {
		if errors.Is(err, transcode.ErrBusy) {
			// Pressure withholds the early optimization, never the ordinary
			// fully validated durable Stop and its post-commit cancellation.
			return action, nil
		}
		return library.PlaybackValidatedStopAction{}, library.ErrUnavailable
	}
	committedFinish := action.Finish
	action.Cancel = func(context.Context) error {
		// Cancellation is restrictive and belongs to the accepted intent,
		// even when the originating HTTP context has been canceled.
		return s.hls.cancelFileHLSPlayback(scope.AuthSessionID, scope.PlaySessionID)
	}
	action.Finish = func(committed bool) {
		committedFinish(committed)
		reference.finish(committed)
	}
	return action, nil
}

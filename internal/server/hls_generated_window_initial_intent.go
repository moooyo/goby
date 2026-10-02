package server

import (
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

// The negotiated start is an initial request intention, not watched progress.
// Its one-shot fence belongs to this output registration's committed Prepared
// revision. Ordinary media GETs and registry reuse cannot refresh the intention.
type hlsGeneratedWindowInitialIntent struct {
	position int64
	revision int64
	valid    bool
	captured bool
}

// Only the verified PlaybackInfo publication path calls this explicit hook.
// Keep the committed row intact and reject a snapshot superseded while source
// validation was in flight. A paused or already playing registration has no
// initial intention to seed, even when another negotiation reuses its key.
func (h *hlsRuntime) seedGeneratedWindowInitialIntent(session *hlsSession, prepared library.PlaySession, start int64) {
	if h == nil || session == nil || prepared.IsDynamic || prepared.State != "Prepared" || prepared.PlaybackRevision < 0 ||
		start < 0 || start >= session.key.plan.DurationTicks || session.key.plan.OutputMode != "" || session.key.plan.SourceMode != "" ||
		!transcode.GeneratedHLS(session.key.plan) {
		return
	}
	scope := session.key.scope
	if prepared.ID != scope.PlaySessionID || prepared.AuthSessionID != scope.AuthSessionID || prepared.UserID != scope.UserID ||
		prepared.DeviceID != scope.DeviceID || prepared.ApplicationKey != scope.ApplicationKey ||
		prepared.ApplicationClientID != scope.ApplicationClientID || prepared.ItemID != scope.ItemID || prepared.MediaSourceID != scope.SourceID {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	current := &session.demand
	if session.closed || session.ctx == nil || session.ctx.Err() != nil || !current.initialized || current.state != "Prepared" ||
		current.revision != prepared.PlaybackRevision || current.position != prepared.PositionTicks || current.paused || current.initialIntent.captured {
		return
	}
	current.initialIntent = hlsGeneratedWindowInitialIntent{position: start, revision: prepared.PlaybackRevision, valid: true, captured: true}
}

// The initial comparison uses the negotiated position once. Prepared wall time
// does not establish playback advancement. This copy never changes the actual
// current position, revision, state or timestamp supplied by the committed row.
func hlsGeneratedWindowInitialComparison(previous, current hlsPlaybackDemand) hlsPlaybackDemand {
	intent := previous.initialIntent
	if previous.initialized && previous.state == "Prepared" && current.state == "Playing" && intent.valid &&
		intent.revision == previous.revision && current.revision > previous.revision {
		previous.position = intent.position
		previous.updated = current.updated
	}
	return previous
}

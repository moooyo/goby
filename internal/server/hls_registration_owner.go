package server

import (
	"context"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func requireLivePlaybackReference(reference *playbackAdmissionReference, scope transcode.Scope) error {
	if reference == nil || reference.gate == nil || reference.entry == nil || reference.released == nil {
		return transcode.ErrInvalidScope
	}
	gate := reference.gate
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if reference.entry.scope != scope {
		return transcode.ErrInvalidScope
	}
	if *reference.released || reference.entry.stopped || gate.closing || gate.entries[playbackStopIntentKeyFor(scope)] != reference.entry {
		return transcode.ErrJobCancelled
	}
	return nil
}

// freshRegistrationOwner rejects an old planning snapshot before it can insert
// a real owned registration. The parent is minted inside fresh canonical SHARE
// validation and retained through registry insertion/final source verification.
// This does not modify the P5 source pipeline or reuse its authority snapshots.
func (h *hlsRuntime) freshRegistrationOwner(ctx context.Context, principal identity.Principal, source library.MediaFile, playID string, decision playback.ConversionDecision) (*playbackAdmissionReference, error) {
	if decision.Plan == nil {
		return nil, transcode.ErrInvalidPlan
	}
	if !h.usesPlaybackOwnership(*decision.Plan) {
		return nil, nil
	}
	var reference *playbackAdmissionReference
	_, err := h.server.library.GetPlaybackSessionAdmitted(ctx, playbackOwner(principal), playID, func(current library.PlaySession) (func(), error) {
		if current.ItemID != source.Item.ID || current.MediaSourceID != source.SourceID || current.IsDynamic {
			return nil, library.ErrNotFound
		}
		var err error
		reference, err = h.server.playbackStopIntents.acquireValidated(playbackScopeForSession(current))
		if err != nil {
			return nil, err
		}
		return reference.release, nil
	})
	if err != nil {
		// The admitted Get already performs failed-commit cleanup. Shared
		// release state makes this exact caller error path idempotent as well.
		reference.release()
		return nil, err
	}
	if reference == nil {
		return nil, transcode.ErrInvalidScope
	}
	return reference, nil
}

// holdSessionSource precedes every unchanged fresh source authorization. The
// descendant is lifetime ownership only; the caller still performs all AUTH,
// queue-readback, filesystem, source and delivery checks before consuming it.
func (h *hlsRuntime) holdSessionSource(session *hlsSession) (*playbackAdmissionReference, error) {
	if h == nil || session == nil {
		return nil, transcode.ErrJobNotFound
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.ctx.Err() != nil {
		return nil, transcode.ErrJobNotFound
	}
	if !h.usesPlaybackOwnership(session.key.plan) {
		return nil, nil
	}
	return session.playbackReference.fork(session.key.scope)
}

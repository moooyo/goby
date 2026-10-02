package server

import (
	"context"
	"errors"
	"os"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// retainUnknownPlaybackInput keeps an actual unknown loan, not an inferred FD
// identity. Its gate reference already consumes the live-owner bound. Recovery
// requires an actual consumer/descriptor join and is not guessed here.
func (s *Server) retainUnknownPlaybackInput(input *playbackOwnedInput) {
	if s == nil || input == nil {
		return
	}
	s.playbackUnknownInputMu.Lock()
	defer s.playbackUnknownInputMu.Unlock()
	if s.playbackUnknownInputs == nil {
		s.playbackUnknownInputs = new([maxPlaybackAdmissionReferences]*playbackOwnedInput)
	}
	for _, prior := range s.playbackUnknownInputs {
		if prior == input {
			return
		}
	}
	for index, prior := range s.playbackUnknownInputs {
		if prior == nil {
			s.playbackUnknownInputs[index] = input
			return
		}
	}
	panic("unknown playback input escaped its existing live-owner bound")
}

func (h *hlsRuntime) closePlaybackInput(input *playbackOwnedInput) error {
	if input == nil {
		return nil
	}
	err := input.close()
	if err != nil && h != nil && h.server != nil {
		h.server.retainUnknownPlaybackInput(input)
	}
	return err
}

// duplicateAdmissionInputLocked runs with session.mu. FD and worker owners
// exist before the pending operation/goroutine is published.
func (h *hlsRuntime) duplicateAdmissionInputLocked(session *hlsSession, source *os.File) (*os.File, *playbackOwnedInput, *playbackAdmissionReference, error) {
	if !h.usesPlaybackOwnership(session.key.plan) {
		file, err := transcode.DuplicateInput(source)
		return file, nil, nil, err
	}
	loan, err := session.playbackReference.fork(session.key.scope)
	if err != nil {
		return nil, nil, nil, err
	}
	file, err := transcode.DuplicateInput(source)
	if err != nil {
		loan.release()
		return nil, nil, nil, err
	}
	owned, err := newPlaybackOwnedInput(file, loan)
	if err != nil {
		if owned != nil {
			h.server.retainUnknownPlaybackInput(owned)
		}
		return nil, nil, nil, err
	}
	worker, err := owned.reference.fork(session.key.scope)
	if err != nil {
		return nil, nil, nil, errors.Join(err, h.closePlaybackInput(owned))
	}
	return file, owned, worker, nil
}

func (pending *hlsAdmission) installPlaybackInput(input *playbackOwnedInput, worker *playbackAdmissionReference) {
	pending.playbackInput, pending.playbackReference = input, worker
	if worker != nil {
		pending.ctx = context.WithValue(pending.ctx, playbackAdmissionContextKey{}, worker)
	}
}

func (h *hlsRuntime) closeAdmissionInput(pending *hlsAdmission, file *os.File, consumed bool) {
	var err error
	if pending.playbackInput != nil {
		err = h.closePlaybackInput(pending.playbackInput)
	} else if !consumed {
		err = file.Close()
	}
	if pending.sourceRead != nil {
		if err != nil {
			err = media.SourceReadRetirementError(err, file)
			_ = pending.sourceRead.MarkUnknown(err)
		}
		_ = pending.sourceRead.Close()
	}
}

// ensureGeneratedWindowInput duplicates the borrowed proof source under the
// actual worker's live owner. The outer source/endpoint/closure observations
// remain unchanged and retain their separate source descriptor and owner.
func (h *hlsRuntime) ensureGeneratedWindowInput(ctx context.Context, session *hlsSession, source *os.File, plan transcode.Plan) (transcode.Record, error) {
	spec := transcode.Spec{Scope: session.key.scope, SourceStamp: session.key.stamp, Plan: plan}
	if !h.usesPlaybackOwnership(plan) {
		file, err := transcode.DuplicateInput(source)
		if err != nil {
			return transcode.Record{}, err
		}
		return h.manager.Ensure(ctx, spec, file)
	}
	parent, _ := ctx.Value(playbackAdmissionContextKey{}).(*playbackAdmissionReference)
	if parent == nil || parent.gate != &h.server.playbackStopIntents {
		return transcode.Record{}, transcode.ErrInvalidScope
	}
	loan, err := parent.fork(spec.Scope)
	if err != nil {
		return transcode.Record{}, err
	}
	file, err := transcode.DuplicateInput(source)
	if err != nil {
		loan.release()
		return transcode.Record{}, err
	}
	owned, err := newPlaybackOwnedInput(file, loan)
	if err != nil {
		if owned != nil {
			h.server.retainUnknownPlaybackInput(owned)
		}
		return transcode.Record{}, err
	}
	defer h.closePlaybackInput(owned)
	return owned.ensure(ctx, h.manager, spec)
}

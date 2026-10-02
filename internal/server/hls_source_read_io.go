package server

import (
	"context"
	"errors"
	"os"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func closePrimaryMediaSource(file *os.File, read *library.MediaSourceReadIO) error {
	var err error
	if file != nil {
		err = file.Close()
		if errors.Is(err, os.ErrClosed) {
			err = nil
		}
	}
	if read != nil {
		if err != nil {
			err = media.SourceReadRetirementError(err, file)
			err = errors.Join(err, read.MarkUnknown(err))
		}
		err = errors.Join(err, read.Close())
	}
	return err
}

// The consuming GET/HEAD path records its first descriptor-close failure before
// the outer HTTP loan can observe ErrClosed. The operation's unknown state is
// sticky, so a later duplicate Close cannot discard this exact resource owner.
func closeHLSConsumedSource(ctx context.Context, file *os.File) {
	if file == nil {
		return
	}
	err := file.Close()
	if err == nil {
		return
	}
	if read := library.MediaSourceReadIOFromContext(ctx); read != nil {
		_ = read.MarkUnknown(media.SourceReadRetirementError(err, file))
	}
}

// prepareTranscodeSourceRead resolves an actual registered principal and fresh
// committed source authority. Neither Spec strings nor the supplied descriptor
// classify a storage route. The verified source and consumed input must still
// identify the same held object before its opaque lifetime enters the queue.
func (h *hlsRuntime) prepareTranscodeSourceRead(ctx, lifetime context.Context, spec transcode.Spec, input *os.File) (transcode.SourceReadLifetime, error) {
	if h == nil || h.server == nil || h.server.library == nil || input == nil {
		return nil, library.ErrUnavailable
	}
	h.mu.Lock()
	var selected *hlsSession
	if !h.closing {
		for _, session := range h.byScope[spec.Scope] {
			if session.key.stamp == spec.SourceStamp && session.ctx != nil && session.ctx.Err() == nil {
				selected = session
				break
			}
		}
	}
	h.mu.Unlock()
	if selected == nil {
		return nil, transcode.ErrJobCancelled
	}
	// The registered principal and context are immutable. A progressive caller
	// can already hold session.mu while Ensure prepares its input lifetime.
	principal := selected.principal
	if selected.ctx.Err() != nil {
		return nil, transcode.ErrJobCancelled
	}
	verified, source, err := h.verify(ctx, principal, spec.Scope, spec.SourceStamp, spec.Plan)
	if err != nil {
		return nil, err
	}
	if verified == nil {
		return nil, library.ErrUnavailable
	}
	before, sourceErr := verified.Stat()
	current, inputErr := input.Stat()
	closeErr := verified.Close()
	if sourceErr != nil || inputErr != nil || closeErr != nil || before == nil || current == nil ||
		!before.Mode().IsRegular() || !current.Mode().IsRegular() || !os.SameFile(before, current) ||
		before.Size() != current.Size() || !before.ModTime().Equal(current.ModTime()) || media.FileChangeTime(before) != media.FileChangeTime(current) {
		return nil, errors.Join(library.ErrSourceChanged, sourceErr, inputErr, closeErr)
	}
	return h.server.library.PrepareMediaSourceIO(lifetime, source)
}

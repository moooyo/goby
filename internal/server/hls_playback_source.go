package server

import (
	"context"
	"errors"
	"os"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// A source loan preserves the existing raw-file APIs for timeline/byte proofs,
// while owning the actual cancellation lifetime until descriptor Close.
type hlsPlaybackSource struct {
	runtime     *hlsRuntime
	file        *os.File
	owned       *playbackOwnedInput
	read        *library.MediaSourceReadIO
	work        context.Context
	cancel      context.CancelFunc
	stopSession func() bool
}

func (source *hlsPlaybackSource) context(ctx context.Context) context.Context {
	if source != nil && source.owned != nil {
		ctx = context.WithValue(source.work, playbackAdmissionContextKey{}, source.owned.reference)
	}
	if source != nil && source.read != nil {
		ctx = source.read.Context(ctx)
	}
	return ctx
}

func (source *hlsPlaybackSource) close() error {
	if source == nil {
		return nil
	}
	if source.stopSession != nil {
		source.stopSession()
	}
	if source.cancel != nil {
		source.cancel()
	}
	var err error
	if source.owned != nil {
		err = source.runtime.closePlaybackInput(source.owned)
	} else if source.file != nil {
		err = source.file.Close()
		if errors.Is(err, os.ErrClosed) {
			err = nil
		}
	}
	if source.read != nil {
		if err != nil {
			err = media.SourceReadRetirementError(err, source.file)
			err = errors.Join(err, source.read.MarkUnknown(err))
		}
		err = errors.Join(err, source.read.Close())
	}
	return err
}

// authorizePlaybackSource forks an existing actual registration parent before
// unchanged fresh AUTH/queue/readback/filesystem work. It does not mint from a
// post-commit snapshot or relax any authority/source/delivery check.
func (h *hlsRuntime) authorizePlaybackSource(ctx context.Context, principal identity.Principal, session *hlsSession) (*hlsPlaybackSource, library.MediaFile, error) {
	reference, err := h.holdSessionSource(session)
	if err != nil {
		return nil, library.MediaFile{}, err
	}
	loan := &hlsPlaybackSource{runtime: h, work: ctx}
	if reference != nil {
		loan.work, loan.cancel = context.WithCancel(ctx)
		loan.stopSession = context.AfterFunc(session.ctx, loan.cancel)
		if session.ctx.Err() != nil {
			loan.cancel()
		}
	}
	file, current, err := h.verify(loan.work, principal, session.key.scope, session.key.stamp, session.key.plan)
	loan.file = file
	if reference != nil {
		if file == nil {
			reference.release()
		} else {
			var ownershipErr error
			loan.owned, ownershipErr = newPlaybackOwnedInput(file, reference)
			err = errors.Join(err, ownershipErr)
		}
	}
	if err != nil {
		return nil, library.MediaFile{}, errors.Join(err, loan.close())
	}
	if h.server != nil && h.server.library != nil {
		loan.read, err = h.server.library.PrepareMediaSourceIO(loan.work, current)
		if err != nil {
			return nil, library.MediaFile{}, errors.Join(err, loan.close())
		}
	}
	return loan, current, nil
}

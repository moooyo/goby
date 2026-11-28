package server

import (
	"bytes"
	"context"
	"errors"
	"os"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
)

// A queued job carries no HTTP context, media descriptor, token or path to
// the source. It must reopen the indexed source after independent admission.
type subtitleExtractionWork struct {
	key       subtitleExtractionKey
	tool      os.FileInfo
	command   string
	actor     identity.Principal
	bound     bool
	requestID string
}

func subtitleExtractionActor(principal identity.Principal) identity.Principal {
	return identity.Principal{Kind: principal.Kind, SessionID: principal.SessionID, PeerIP: principal.PeerIP,
		ClientSessionID: principal.ClientSessionID, ApplicationKeyID: principal.ApplicationKeyID,
		User: identity.User{ID: principal.User.ID}}
}

func embeddedSubtitleStream(source library.MediaFile, index int) (media.Stream, bool) {
	if source.Item.Media != nil {
		for _, stream := range source.Item.Media.Streams {
			if stream.Index == index && !stream.IsExternal {
				return stream, true
			}
		}
	}
	return media.Stream{}, false
}

func (s *Server) recheckSubtitleExtractionAuthority(ctx context.Context, previous identity.Principal) error {
	current, err := s.identity.RevalidateSessionAuthority(ctx, previous)
	if err != nil {
		return err
	}
	if current.User.IsAdministrator != previous.User.IsAdministrator || !bytes.Equal(current.User.Policy, previous.User.Policy) {
		return library.ErrForbidden
	}
	return nil
}

func (s *Server) openSubtitleExtractionSource(ctx context.Context, work subtitleExtractionWork, background bool) (*os.File, library.MediaFile, media.Stream, error) {
	fresh, err := s.identity.RevalidateSessionAuthority(ctx, work.actor)
	if err != nil {
		return nil, library.MediaFile{}, media.Stream{}, err
	}
	if !work.bound && !s.currentSettingsSnapshot().Transcoding.EnableSubtitleExtraction {
		return nil, library.MediaFile{}, media.Stream{}, subtitle.ErrUnsupportedFormat
	}
	key := work.key
	var file *os.File
	var source library.MediaFile
	subject := librarySubject(fresh, fresh.User.ID)
	if background {
		file, source, err = s.library.RevalidateBackgroundMediaSourceFor(ctx, subject, key.item, key.source)
	} else {
		file, source, err = s.library.RevalidateMediaSourceFor(ctx, subject, key.item, key.source, false)
	}
	if err != nil {
		return nil, library.MediaFile{}, media.Stream{}, err
	}
	// The source open can block after its account and catalog checks. Reject
	// a revoked credential or a policy change instead of using a stale grant.
	if err := s.recheckSubtitleExtractionAuthority(ctx, fresh); err != nil {
		return nil, library.MediaFile{}, media.Stream{}, errors.Join(err, file.Close())
	}
	if !work.bound && !s.currentSettingsSnapshot().Transcoding.EnableSubtitleExtraction {
		return nil, library.MediaFile{}, media.Stream{}, errors.Join(subtitle.ErrUnsupportedFormat, file.Close())
	}
	stream, found := embeddedSubtitleStream(source, key.index)
	if source.ReadVersion() != key.version || !found || stream.CodecType != "subtitle" || stream.Codec != key.codec {
		return nil, library.MediaFile{}, media.Stream{}, errors.Join(library.ErrSourceChanged, file.Close())
	}
	return file, source, stream, nil
}

func (s *Server) extractBackgroundSubtitle(ctx context.Context, work subtitleExtractionWork) (_ []byte, resultErr error) {
	if work.requestID != "" {
		ctx = context.WithValue(ctx, requestIDKey, work.requestID)
	}
	stage := "subtitle_input"
	defer func() { s.logPlaybackFailure(ctx, stage, resultErr) }()
	if s.serverPolicy != nil {
		watched, release, allowed := s.serverPolicy.Watch(ctx, work.actor.PeerIP)
		if !allowed {
			return nil, library.ErrForbidden
		}
		defer release()
		ctx = watched
	}
	matchesTool := func() bool {
		path, info := subtitleExtractionTool(work.command)
		return path == work.key.tool && sameSubtitleExtractionTool(info, work.tool)
	}
	if !matchesTool() {
		return nil, library.ErrUnavailable
	}
	file, source, stream, err := s.openSubtitleExtractionSource(ctx, work, true)
	if err != nil {
		return nil, err
	}
	read, err := s.library.PrepareMediaSourceIO(ctx, source)
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	var data []byte
	err = func() (result error) {
		defer func() {
			if file != nil {
				result = errors.Join(result, closePrimaryMediaSource(file, read))
			}
		}()
		return read.Run(ctx, func(execution context.Context) (result error) {
			// Close the descriptor before returning the active background lease.
			defer func() {
				result = errors.Join(result, closePrimaryMediaSource(file, read))
				file = nil
			}()
			stage = "subtitle_extraction"
			data, result = media.ExtractBackgroundSubtitle(execution, work.command, file, stream, work.key.format)
			return result
		})
	}()
	if err != nil {
		return nil, err
	}
	stage = "subtitle_render"
	if _, err := subtitle.Parse(data, subtitle.Format(work.key.format)); err != nil {
		return nil, err
	}
	stage = "subtitle_input"
	// Verify the current catalog entry after closing the original descriptor.
	// The same still-open inode alone would not detect a replaced directory entry.
	file, _, _, err = s.openSubtitleExtractionSource(ctx, work, true)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if !matchesTool() {
		return nil, library.ErrUnavailable
	}
	return data, ctx.Err()
}

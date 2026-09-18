package server

import (
	"context"
	"errors"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
	"github.com/moooyo/goby/internal/transcode"
)

func (s *Server) readPlannedExternalSubtitle(ctx context.Context, principal identity.Principal, scope transcode.Scope, plan transcode.Plan) (library.SubtitleContent, error) {
	if plan.Subtitle.ExternalTag == "" || plan.Subtitle.Mode != "burn" && plan.Subtitle.Mode != "hls" {
		return library.SubtitleContent{}, library.ErrInvalidInput
	}
	content, err := s.library.ReadSubtitleFor(ctx, librarySubject(principal, principal.User.ID), scope.ItemID, scope.SourceID, plan.Subtitle.StreamIndex)
	if err != nil {
		return library.SubtitleContent{}, err
	}
	actual, actualErr := subtitle.NormalizeFormat(content.Info.Codec)
	expected, expectedErr := subtitle.NormalizeFormat(plan.Subtitle.Codec)
	if actualErr != nil || expectedErr != nil || actual != expected || content.Info.Tag != plan.Subtitle.ExternalTag {
		return library.SubtitleContent{}, library.ErrSourceChanged
	}
	return content, nil
}

// readBurnSubtitleAsset resolves the session principal only after a queued job
// starts. Both the primary media stamp and indexed subtitle hash are checked
// again before immutable bytes enter the runner's private cache directory.
func (s *Server) readBurnSubtitleAsset(ctx context.Context, spec transcode.Spec) ([]byte, error) {
	if s.hls == nil || spec.Plan.Subtitle.Mode != "burn" || spec.Plan.Subtitle.ExternalTag == "" {
		return nil, library.ErrNotFound
	}
	var principal identity.Principal
	found := false
	s.hls.mu.Lock()
	for _, session := range s.hls.sessions {
		if session.key.scope != spec.Scope || session.key.stamp != spec.SourceStamp || session.key.plan.Subtitle != spec.Plan.Subtitle {
			continue
		}
		session.mu.Lock()
		if !session.closed {
			principal, found = session.principal, true
		}
		session.mu.Unlock()
		if found {
			break
		}
	}
	s.hls.mu.Unlock()
	if !found {
		return nil, library.ErrNotFound
	}
	input, _, err := s.authorizeHLS(ctx, principal, spec.Scope, spec.SourceStamp, spec.Plan)
	if err != nil {
		return nil, err
	}
	_ = input.Close()
	content, err := s.readPlannedExternalSubtitle(ctx, principal, spec.Scope, spec.Plan)
	if err != nil {
		return nil, err
	}
	doc, err := subtitle.Parse(content.Data, subtitle.Format(content.Info.Codec))
	if err != nil {
		return nil, err
	}
	format := subtitle.FormatASS
	if doc.Format == subtitle.FormatSSA {
		// libass accepts legacy SSA script headers in the fixed .ass asset.
		// Keeping that native script preserves its full styles and font data.
		format = subtitle.FormatSSA
	}
	rendered, err := subtitle.Render(doc, subtitle.Options{Format: format, PreserveSource: true, CopyTimestamps: true})
	if err != nil || len(rendered.Data) > media.MaxSubtitleExtractionBytes {
		return nil, errors.Join(subtitle.ErrLimitExceeded, err)
	}
	// Reading and rendering may cross a rescan or permission revision. A fresh
	// authorization fences those changes before the prepared result is returned.
	current, _, err := s.authorizeHLS(ctx, principal, spec.Scope, spec.SourceStamp, spec.Plan)
	if err != nil {
		return nil, err
	}
	_ = current.Close()
	return rendered.Data, nil
}

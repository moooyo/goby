package server

import (
	"context"
	"time"

	"github.com/moooyo/goby/internal/library"
)

// Single-item and playback responses resolve detected evidence against actual
// source/support files. Catalog lists retain their inexpensive manual/chapter
// projection and never imply that a database stamp proves current source bytes.
func (s *Server) resolveItemAnalysisIntro(ctx context.Context, subject library.Subject, item *library.Item, sourceID string) {
	if s.library == nil || item == nil || item.Type != "Episode" || item.IsFolder || item.Media == nil {
		return
	}
	if item.Intro != nil && item.Intro.Provenance == "Detected" {
		item.Intro = nil
	}
	if item.AnalysisSourceRevision == "" {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	interval, err := s.library.ResolveAnalysisIntroForRevision(bounded, subject, item.ID, sourceID, item.AnalysisSourceRevision)
	if err == nil && interval != nil && interval.Provenance == "Detected" && interval.StartTicks >= 0 && interval.StartTicks < interval.EndTicks && interval.EndTicks <= item.Media.DurationTicks {
		item.Intro = interval
	}
	// A missing, stale or unprovable automatic result leaves the existing
	// manual/chapter projection intact and does not turn playback into a 500.
}

func (s *Server) resolvePlaybackAnalysisIntro(ctx context.Context, subject library.Subject, source *library.MediaFile) {
	s.resolveItemAnalysisIntro(ctx, subject, &source.Item, source.SourceID)
}

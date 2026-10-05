package server

import (
	"context"
	"sort"
	"time"

	"github.com/moooyo/goby/internal/library"
)

func (s *Server) resolveItemAnalysisCredits(ctx context.Context, subject library.Subject, item *library.Item, sourceID string) {
	if s.library == nil || item == nil || item.IsFolder || item.Media == nil || item.Type != "Movie" && item.Type != "Episode" {
		return
	}
	item.DetectedCredits = nil
	if item.Credits != nil || item.AnalysisSourceRevision == "" {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	segments, err := s.library.ResolveDetectedCreditsForRevision(bounded, subject, item.ID, sourceID, item.AnalysisSourceRevision)
	if err == nil {
		item.DetectedCredits = segments
	}
}

// Goby extends the standard single CreditsStart marker with a complete interval
// set. Unioning overlapping native adjustments preserves their covered times
// while supplying an unambiguous, ordered, non-overlapping wire representation.
func itemCreditsIntervalsDTO(item library.Item) []map[string]any {
	result := []map[string]any{}
	if item.Media == nil || item.IsFolder || item.Type != "Movie" && item.Type != "Episode" || item.Media.DurationTicks <= 0 {
		return result
	}
	duration := item.Media.DurationTicks
	if item.Credits != nil {
		if item.Credits.StartTicks >= 0 && item.Credits.StartTicks < duration {
			result = append(result, map[string]any{"StartPositionTicks": item.Credits.StartTicks, "EndPositionTicks": duration, "Source": item.Credits.Provenance})
		}
		return result
	}
	segments := append([]library.CreditsInterval(nil), item.DetectedCredits...)
	sort.SliceStable(segments, func(i, j int) bool { return segments[i].StartTicks < segments[j].StartTicks })
	merged := []library.CreditsInterval{}
	for _, segment := range segments {
		if segment.StartTicks < 0 || segment.EndTicks <= segment.StartTicks || segment.EndTicks > duration {
			return result
		}
		if len(merged) > 0 && segment.StartTicks < merged[len(merged)-1].EndTicks {
			last := &merged[len(merged)-1]
			last.EndTicks = max(last.EndTicks, segment.EndTicks)
			if last.Source != segment.Source {
				last.Source = "Combined"
			}
		} else {
			merged = append(merged, segment)
		}
	}
	for _, segment := range merged {
		result = append(result, map[string]any{"StartPositionTicks": segment.StartTicks, "EndPositionTicks": segment.EndTicks, "Source": segment.Source})
	}
	return result
}

package transcode

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/moooyo/goby/internal/media"
)

const MaxHLSSubtitleTracks = 8

// HLSSubtitlePlan binds the complete advertised track set to one immutable
// media producer. Selection and caption delay belong to the delivery view.
// The fixed array keeps Plan comparable for session and job deduplication.
type HLSSubtitlePlan struct {
	Count  int                                    `json:"Count,omitempty"`
	Tracks [MaxHLSSubtitleTracks]HLSSubtitleTrack `json:"Tracks,omitempty"`
}

type HLSSubtitleTrack struct {
	StreamIndex int    `json:"StreamIndex"`
	Codec       string `json:"Codec"`
	// Finite sources bind catalog bytes; stream sources bind an authorized
	// lease and subtitle definition. Neither case carries a path or URL.
	ExternalTag string `json:"ExternalTag,omitempty"`
}

// HasHLSSubtitles includes the original one-track representation so retained
// plans remain readable while new planners use the immutable track set.
func HasHLSSubtitles(p Plan) bool {
	return p.HLS.Subtitles.Count > 0 || p.Subtitle.Mode == "hls"
}

// PlanHLSSubtitles resolves the legacy representation without mutating Plan.
// Callers must validate the plan before authorizing or opening a track.
func PlanHLSSubtitles(p Plan) HLSSubtitlePlan {
	if p.HLS.Subtitles.Count != 0 || p.Subtitle.Mode != "hls" {
		return p.HLS.Subtitles
	}
	var result HLSSubtitlePlan
	result.Count = 1
	result.Tracks[0] = HLSSubtitleTrack{StreamIndex: p.Subtitle.StreamIndex, Codec: p.Subtitle.Codec, ExternalTag: p.Subtitle.ExternalTag}
	return result
}

func HLSSubtitleTrackAt(p Plan, slot int) (HLSSubtitleTrack, bool) {
	tracks := PlanHLSSubtitles(p)
	if slot < 0 || slot >= tracks.Count || slot >= MaxHLSSubtitleTracks {
		return HLSSubtitleTrack{}, false
	}
	return tracks.Tracks[slot], true
}

// ValidateHLSSubtitlePlan accepts only source-bound text tracks with stable
// identities. It does not open sources or infer client subtitle capabilities.
func ValidateHLSSubtitlePlan(p Plan) error {
	invalid := func(field string) error { return fmt.Errorf("%w: HLS subtitles %s", ErrInvalidPlan, field) }
	tracks := p.HLS.Subtitles
	if tracks.Count < 0 || tracks.Count > MaxHLSSubtitleTracks {
		return invalid("track count")
	}
	if tracks.Count > 0 && (p.OutputMode != "" || p.Subtitle != (SubtitlePlan{})) {
		return invalid("mixed delivery modes")
	}
	previous := -1
	for slot, track := range tracks.Tracks {
		if slot >= tracks.Count {
			if track != (HLSSubtitleTrack{}) {
				return invalid("unused track")
			}
			continue
		}
		limit := maxStreamIndex
		if track.ExternalTag != "" {
			decoded, err := hex.DecodeString(track.ExternalTag)
			if err != nil || len(decoded) != 32 || track.ExternalTag != strings.ToLower(track.ExternalTag) {
				return invalid("external identity")
			}
			limit = 1<<31 - 1
		}
		if track.StreamIndex <= previous || track.StreamIndex > limit || track.StreamIndex == p.VideoStreamIndex || track.StreamIndex == p.AudioStreamIndex {
			return invalid("source stream index")
		}
		if track.Codec != strings.ToLower(track.Codec) || media.SubtitleExtractFormat(track.Codec) == "" {
			return invalid("text codec")
		}
		previous = track.StreamIndex
	}
	return nil
}

package playback

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// HLSSubtitleView describes a manifest or subtitle response, never an encoder
// input. Keeping it outside Plan lets track switching, off, and caption delay
// share the same immutable audio/video producer.
type HLSSubtitleView struct {
	SelectedStreamIndex int
	SelectionSet        bool
	OffsetTicks         int64
}

// HLSSubtitleViewFor validates a delivery view against the complete bound set.
// A nil selection retains the original explicit-selection behavior: no default
// track is enabled unless a legacy one-track plan already carries one.
func HLSSubtitleViewFor(plan transcode.Plan, selected *int, offsetTicks int64) (HLSSubtitleView, error) {
	view := HLSSubtitleView{SelectedStreamIndex: -1, OffsetTicks: offsetTicks}
	if offsetTicks < -24*60*60*media.TicksPerSecond || offsetTicks > 24*60*60*media.TicksPerSecond {
		return view, fmt.Errorf("%w: HLS subtitle offset", ErrInvalidRequest)
	}
	if selected != nil {
		view.SelectedStreamIndex, view.SelectionSet = *selected, true
	} else if plan.Subtitle.Mode == "hls" {
		view.SelectedStreamIndex, view.SelectionSet = plan.Subtitle.StreamIndex, true
	}
	if view.SelectedStreamIndex < -1 || !transcode.HasHLSSubtitles(plan) && (view.SelectedStreamIndex != -1 || offsetTicks != 0) {
		return view, fmt.Errorf("%w: HLS subtitle selection", ErrInvalidRequest)
	}
	if view.SelectedStreamIndex >= 0 {
		tracks := transcode.PlanHLSSubtitles(plan)
		for slot := 0; slot < min(tracks.Count, transcode.MaxHLSSubtitleTracks); slot++ {
			if tracks.Tracks[slot].StreamIndex == view.SelectedStreamIndex {
				return view, nil
			}
		}
		return view, fmt.Errorf("%w: HLS subtitle track is not bound", ErrInvalidRequest)
	}
	return view, nil
}

// HLSSubtitleMetadata returns a detached catalog description only when its
// current identity still matches the bound track. The caller authorizes Source
// and escapes presentation labels for its output format.
func HLSSubtitleMetadata(source Source, plan transcode.Plan, slot int) (media.Stream, bool) {
	track, ok := transcode.HLSSubtitleTrackAt(plan, slot)
	if !ok {
		return media.Stream{}, false
	}
	var found media.Stream
	matched := false
	for _, stream := range source.Info.Streams {
		if stream.Index != track.StreamIndex {
			continue
		}
		candidate, valid := hlsSubtitleTrack(stream)
		if matched || !valid || candidate != track {
			return media.Stream{}, false
		}
		found, matched = stream, true
	}
	return found, matched
}

func hlsSubtitleTrack(stream media.Stream) (transcode.HLSSubtitleTrack, bool) {
	if !strings.EqualFold(stream.CodecType, "subtitle") || !stream.IsTextSubtitleStream || stream.Index < 0 || stream.Index > 1<<31-1 {
		return transcode.HLSSubtitleTrack{}, false
	}
	codec := strings.ToLower(stream.Codec)
	if media.SubtitleExtractFormat(codec) == "" {
		return transcode.HLSSubtitleTrack{}, false
	}
	track := transcode.HLSSubtitleTrack{StreamIndex: stream.Index, Codec: codec}
	if stream.IsExternal {
		tag, err := hex.DecodeString(stream.SubtitleTag)
		if err != nil || len(tag) != 32 || stream.SubtitleTag != strings.ToLower(stream.SubtitleTag) {
			return transcode.HLSSubtitleTrack{}, false
		}
		track.ExternalTag = stream.SubtitleTag
	} else if stream.Index > 4095 || stream.SubtitleTag != "" {
		return transcode.HLSSubtitleTrack{}, false
	}
	return track, true
}

func clientAcceptsHLSSubtitle(stream media.Stream, container string, request Request, profile TranscodingProfile) bool {
	if _, valid := hlsSubtitleTrack(stream); !valid {
		return false
	}
	if strings.EqualFold(profile.ManifestSubtitles, "vtt") || strings.EqualFold(profile.ManifestSubtitles, "webvtt") {
		return true
	}
	if request.DeviceProfile == nil {
		return false
	}
	for _, candidate := range request.DeviceProfile.SubtitleProfiles {
		if candidate.Method == SubtitleDeliveryMethodHls && matchesList(candidate.Language, stream.Language) && matchesList(candidate.Container, container) &&
			(candidate.Protocol == "" || strings.EqualFold(candidate.Protocol, "hls")) &&
			(matchesList(candidate.Format, "vtt") || matchesList(candidate.Format, "webvtt")) {
			return true
		}
	}
	return false
}

func configureHLSSubtitleTracks(plan *transcode.Plan, source Source, request Request, profile TranscodingProfile, selected *media.Stream) *Reason {
	maximum := transcode.MaxHLSSubtitleTracks
	if profile.MaxManifestSubtitles != nil {
		if *profile.MaxManifestSubtitles < 0 {
			return conversionReason("conversion_manifest_subtitle_limit", "MaxManifestSubtitles", "The subtitle rendition limit must be nonnegative.")
		}
		maximum = min(maximum, *profile.MaxManifestSubtitles)
	}
	var eligible []transcode.HLSSubtitleTrack
	for _, stream := range source.Info.Streams {
		if clientAcceptsHLSSubtitle(stream, plan.Container, request, profile) {
			track, _ := hlsSubtitleTrack(stream)
			eligible = append(eligible, track)
		}
	}
	sort.Slice(eligible, func(left, right int) bool { return eligible[left].StreamIndex < eligible[right].StreamIndex })
	var tracks transcode.HLSSubtitlePlan
	for index, track := range eligible {
		if index > 0 && eligible[index-1].StreamIndex == track.StreamIndex {
			return conversionReason("conversion_subtitle_identity_invalid", "SubtitleStreamIndex", "Subtitle stream identities must be unique.")
		}
		if tracks.Count < maximum {
			tracks.Tracks[tracks.Count] = track
			tracks.Count++
		}
	}
	if selected != nil {
		found := false
		for slot := 0; slot < tracks.Count; slot++ {
			found = found || tracks.Tracks[slot].StreamIndex == selected.Index
		}
		if !found {
			return conversionReason("conversion_manifest_subtitle_limit", "SubtitleStreamIndex", "The selected subtitle is not in the bounded client-compatible rendition set.")
		}
	}
	proposal := *plan
	proposal.HLS.Subtitles = tracks
	if err := transcode.ValidateHLSSubtitlePlan(proposal); err != nil {
		return conversionReason("conversion_subtitle_identity_invalid", "SubtitleStreamIndex", "The subtitle rendition set has incompatible or unbound stream identities.")
	}
	plan.HLS.Subtitles = tracks
	return nil
}

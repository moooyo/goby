package server

import (
	"encoding/hex"
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type hlsGeneratedWindowInitializationEpoch struct {
	digests [transcode.MaxHLSRenditions][32]byte
}

// Logical MAP URLs outlive individual retained producers. The first closed
// initialization bytes for each source slot therefore remain immutable across
// cache eviction and producer replacement. This history contains only bounded
// hashes, never file handles or permission to start or publish a producer.
// The graph owner serializes all calls with its session mutex and retains the
// history until the presentation is invalidated, including while paused.
type hlsGeneratedWindowInitializationHistory struct {
	graphID            string
	sourceIdentity     string
	basePlan           transcode.Plan
	nativeClockVersion uint8
	slots              map[int]hlsGeneratedWindowInitializationEpoch
}

func newHLSGeneratedWindowInitializationHistory(graphID, sourceIdentity string, basePlan transcode.Plan, nativeClockVersion uint8) (hlsGeneratedWindowInitializationHistory, error) {
	var empty hlsGeneratedWindowInitializationHistory
	count := basePlan.HLS.RenditionCount
	if graphID == "" || len(sourceIdentity) != 64 || strings.ToLower(sourceIdentity) != sourceIdentity ||
		transcode.ValidatePlan(basePlan) != nil || basePlan.HLS.SegmentType != "fmp4" || basePlan.StartTicks != 0 ||
		basePlan.HLS.Window != (transcode.HLSWindow{}) || basePlan.DurationTicks <= 0 || basePlan.SegmentSeconds < 1 ||
		count < 0 || count == 1 || count > transcode.MaxHLSRenditions {
		return empty, transcode.ErrInvalidTimeline
	}
	if _, err := hex.DecodeString(sourceIdentity); err != nil {
		return empty, transcode.ErrInvalidTimeline
	}
	period := int64(basePlan.SegmentSeconds) * media.TicksPerSecond
	if (basePlan.DurationTicks+period-1)/period > transcode.MaxTimelineSegments {
		return empty, transcode.ErrUnsupportedTimeline
	}
	return hlsGeneratedWindowInitializationHistory{graphID: graphID, sourceIdentity: sourceIdentity,
		basePlan: basePlan, nativeClockVersion: nativeClockVersion}, nil
}

// Remember only after independent complete native media closure, actual byte
// digests and the final identity fence have succeeded for every rendition.
// The structural artifact table does not establish those facts. A replacement
// producer may change disk identity but cannot change initialization bytes
// behind a MAP already cached by the client. Any changed sibling rejects the
// entire set before an entry or a binding can be replaced.
func (history *hlsGeneratedWindowInitializationHistory) validateAndRemember(graphID, sourceIdentity string, artifacts hlsGeneratedWindowArtifactSet,
	digests [transcode.MaxHLSRenditions][32]byte) error {
	if history == nil || graphID == "" || graphID != history.graphID || sourceIdentity != history.sourceIdentity ||
		!artifacts.valid(artifacts.producerID, artifacts.plan) || artifacts.plan.HLS.SegmentType != "fmp4" ||
		artifacts.plan.HLS.Window.NativeClockVersion != history.nativeClockVersion {
		return transcode.ErrInvalidTimeline
	}
	plan := artifacts.plan
	base := plan
	base.StartTicks, base.HLS.Window = 0, transcode.HLSWindow{}
	if base != history.basePlan || plan.StartTicks < 0 || plan.HLS.Window.StartNumber < 0 ||
		plan.HLS.Window.StartNumber >= transcode.MaxTimelineSegments {
		return transcode.ErrInvalidTimeline
	}
	period := int64(base.SegmentSeconds) * media.TicksPerSecond
	start := int64(plan.HLS.Window.StartNumber) * period
	if start >= base.DurationTicks || plan.StartTicks != start || plan.HLS.Window.EndTicks != start+min(period, base.DurationTicks-start) {
		return transcode.ErrInvalidTimeline
	}
	count := max(1, base.HLS.RenditionCount)
	for variant, digest := range digests {
		if variant < count && digest == ([32]byte{}) || variant >= count && digest != ([32]byte{}) {
			return transcode.ErrInvalidTimeline
		}
	}
	number := plan.HLS.Window.StartNumber
	if known, found := history.slots[number]; found {
		if known.digests != digests {
			return transcode.ErrOutputUnavailable
		}
		return nil
	}
	if len(history.slots) >= transcode.MaxTimelineSegments {
		return transcode.ErrBusy
	}
	if history.slots == nil {
		history.slots = make(map[int]hlsGeneratedWindowInitializationEpoch)
	}
	history.slots[number] = hlsGeneratedWindowInitializationEpoch{digests: digests}
	return nil
}

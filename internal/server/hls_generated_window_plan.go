package server

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// Each immutable source slot selects its independently calibrated container
// clock before publication. Admission still needs actual input, source and
// media closure; planning never promotes a metadata duration into evidence.
func hlsGeneratedWindowTimeline(plan transcode.Plan, info media.Info, endpoint transcode.GeneratedSourceEndpointCertificate) (transcode.Timeline, error) {
	unsupported := func() (transcode.Timeline, error) { return transcode.Timeline{}, transcode.ErrUnsupportedTimeline }
	if transcode.ValidatePlan(plan) != nil || !transcode.GeneratedWindowClosureEligible(plan) ||
		plan.HLS.Window != (transcode.HLSWindow{}) || plan.StartTicks != 0 ||
		!info.FormatStartKnown || endpoint.StreamIndex != plan.VideoStreamIndex || endpoint.TrackID == 0 || endpoint.SampleCount <= 0 ||
		!endpoint.DurationTicksExact || plan.DurationTicks != info.DurationTicks || endpoint.DurationTicks <= 0 ||
		endpoint.MetadataSHA256 == ([32]byte{}) || endpoint.SampleExtentsSHA256 == ([32]byte{}) ||
		len(endpoint.SourceIdentity) != 64 || strings.ToLower(endpoint.SourceIdentity) != endpoint.SourceIdentity {
		return unsupported()
	}
	if _, err := hex.DecodeString(endpoint.SourceIdentity); err != nil {
		return unsupported()
	}
	rational := func(value transcode.GeneratedRational) (*big.Rat, bool) {
		if value.Den <= 0 {
			return nil, false
		}
		return new(big.Rat).SetFrac64(value.Num, value.Den), true
	}
	origin, originOK := rational(endpoint.Origin)
	last, lastOK := rational(endpoint.Last)
	end, endOK := rational(endpoint.End)
	period, periodOK := rational(endpoint.FrameDuration)
	if !originOK || !lastOK || !endOK || !periodOK || period.Sign() <= 0 ||
		origin.Cmp(new(big.Rat).SetFrac64(info.FormatStartTicks, media.TicksPerSecond)) != 0 ||
		period.Cmp(new(big.Rat).SetFrac64(1, int64(plan.FrameRate))) != 0 {
		return unsupported()
	}
	duration := new(big.Rat).SetFrac64(endpoint.DurationTicks, media.TicksPerSecond)
	if new(big.Rat).Sub(end, origin).Cmp(duration) != 0 || new(big.Rat).Sub(end, last).Cmp(period) != 0 ||
		new(big.Rat).Mul(new(big.Rat).SetInt64(endpoint.SampleCount), period).Cmp(duration) != 0 {
		return unsupported()
	}
	if plan.DurationTicks != endpoint.DurationTicks {
		// Some ordinary MP4 probes expose the absolute presentation endpoint
		// as format.duration. Only the independently proved exact endpoint may
		// qualify that convention; arbitrary metadata offsets remain unsupported.
		absoluteTicks := new(big.Rat).Mul(new(big.Rat).Set(end), new(big.Rat).SetInt64(media.TicksPerSecond))
		if !absoluteTicks.IsInt() || !absoluteTicks.Num().IsInt64() || absoluteTicks.Num().Int64() != plan.DurationTicks {
			return unsupported()
		}
	}
	haveSource := false
	for _, stream := range info.Streams {
		if stream.Index == endpoint.StreamIndex {
			if haveSource || stream.CodecType != "video" || stream.Codec != "h264" || stream.IsAttachedPicture || stream.IsExternal {
				return unsupported()
			}
			haveSource = true
		}
	}
	if !haveSource {
		return unsupported()
	}
	timeline, err := transcode.BuildTimeline(endpoint.DurationTicks, plan.SegmentSeconds, nil, false)
	if err != nil {
		return transcode.Timeline{}, err
	}
	for _, slot := range timeline.Segments {
		frames := new(big.Int).Mul(big.NewInt(slot.DurationTicks), big.NewInt(int64(plan.FrameRate)))
		if new(big.Int).Mod(frames, big.NewInt(media.TicksPerSecond)).Sign() != 0 {
			return unsupported()
		}
		candidate := hlsGeneratedWindowProductionBase(plan, endpoint)
		candidate.StartTicks = slot.StartTicks
		candidate.HLS.Window = transcode.HLSWindow{EndTicks: slot.StartTicks + slot.DurationTicks, StartNumber: slot.Number,
			RequireInputEvidence: true}
		// Check the entire declared movie before publication. A future tail
		// cannot discover an unrepresentable clock after clients cached the
		// immutable full-source timeline.
		if hlsGeneratedWindowNativeClockVersion(candidate) == 0 {
			return unsupported()
		}
	}
	return timeline, nil
}

func hlsGeneratedWindowSlotPlan(base transcode.Plan, timeline transcode.Timeline, number int) (transcode.Plan, error) {
	if transcode.ValidatePlan(base) != nil || base.StartTicks != 0 || number < 0 || number >= transcode.MaxTimelineSegments || number >= len(timeline.Segments) || base.HLS.Window != (transcode.HLSWindow{}) ||
		!transcode.GeneratedWindowClosureEligible(base) {
		return transcode.Plan{}, transcode.ErrInvalidTimeline
	}
	nominal := int64(base.SegmentSeconds) * media.TicksPerSecond
	count := (base.DurationTicks + nominal - 1) / nominal
	start := int64(number) * nominal
	duration := min(nominal, base.DurationTicks-start)
	slot := timeline.Segments[number]
	if count > transcode.MaxTimelineSegments || int64(len(timeline.Segments)) != count ||
		timeline.TargetDuration != int((min(base.DurationTicks, nominal)+media.TicksPerSecond-1)/media.TicksPerSecond) ||
		slot.Number != number || slot.StartTicks != start || slot.DurationTicks != duration || duration <= 0 ||
		duration*int64(base.FrameRate)%media.TicksPerSecond != 0 {
		return transcode.Plan{}, transcode.ErrInvalidTimeline
	}
	window := base
	window.StartTicks = slot.StartTicks
	window.HLS.Window = transcode.HLSWindow{EndTicks: slot.StartTicks + slot.DurationTicks, StartNumber: number,
		RequireInputEvidence: true}
	window.HLS.Window.NativeClockVersion = hlsGeneratedWindowNativeClockVersion(window)
	if window.HLS.Window.NativeClockVersion == 0 {
		return transcode.Plan{}, transcode.ErrUnsupportedTimeline
	}
	if err := transcode.ValidatePlan(window); err != nil {
		return transcode.Plan{}, err
	}
	return window, nil
}

func hlsGeneratedWindowNativeClockVersion(plan transcode.Plan) uint8 {
	switch plan.HLS.SegmentType {
	case "mpegts":
		plan.HLS.Window.NativeClockVersion = transcode.GeneratedWindowNativeClockV1
		if transcode.GeneratedWindowNativeClockEligible(plan) {
			return transcode.GeneratedWindowNativeClockV1
		}
	case "fmp4":
		plan.HLS.Window.NativeClockVersion = transcode.GeneratedWindowNativeClockV2
		if transcode.GeneratedWindowFMP4NativeClockEligible(plan) {
			return transcode.GeneratedWindowNativeClockV2
		}
	}
	return 0
}

func hlsGeneratedWindowProductionBase(plan transcode.Plan, endpoint transcode.GeneratedSourceEndpointCertificate) transcode.Plan {
	plan.DurationTicks = endpoint.DurationTicks
	return plan
}

func hlsGeneratedWindowVariantName(number, variant, count int) string {
	name := hlsGeneratedWindowLogicalName(number)
	if name == "" || variant < 0 || variant >= max(1, count) {
		return ""
	}
	if count == 0 {
		return name
	}
	return fmt.Sprintf("v%d-%s", variant, name)
}

func hlsGeneratedWindowVariantNumber(name string, count int) (number, variant int, valid bool) {
	for index := 0; index < max(1, count); index++ {
		candidate := name
		if count != 0 {
			var prefixed bool
			candidate, prefixed = strings.CutPrefix(name, fmt.Sprintf("v%d-", index))
			if !prefixed {
				continue
			}
		}
		if parsed, found := hlsGeneratedWindowLogicalNumber(candidate); found && hlsGeneratedWindowVariantName(parsed, index, count) == name {
			return parsed, index, true
		}
	}
	return 0, 0, false
}

func hlsGeneratedWindowLogicalName(number int) string {
	if number < 0 || number >= transcode.MaxPlaylistSegments {
		return ""
	}
	return fmt.Sprintf("window-segment-%06d.ts", number)
}

func hlsGeneratedWindowLogicalNumber(name string) (int, bool) {
	value, prefixed := strings.CutPrefix(name, "window-segment-")
	value, suffixed := strings.CutSuffix(value, ".ts")
	if !prefixed || !suffixed || len(value) != 6 {
		return 0, false
	}
	number, err := strconv.Atoi(value)
	return number, err == nil && hlsGeneratedWindowLogicalName(number) == name
}

package transcode

import (
	"fmt"
	"math"
	"math/big"
)

func hasHLSWindow(p Plan) bool {
	return p.HLS.Window != (HLSWindow{})
}

func validateHLSWindow(p Plan) error {
	if !hasHLSWindow(p) {
		return nil
	}
	invalid := func() error { return fmt.Errorf("%w: generated HLS window", ErrInvalidPlan) }
	w := p.HLS.Window
	if p.OutputMode != "" || p.SourceMode != "" || p.SegmentMode != "" ||
		p.SegmentStartNumber != 0 || p.EndTicks != 0 || p.SegmentTimes != "" || p.ReferenceStartTicks != 0 ||
		p.DurationTicks <= 0 || p.DurationTicks > maxDurationTicks || p.StartTicks < 0 ||
		w.EndTicks <= p.StartTicks || w.EndTicks > p.DurationTicks ||
		w.StartNumber < 0 || w.StartNumber >= MaxPlaylistSegments || p.SegmentSeconds < 1 || p.SegmentSeconds > 10 {
		return invalid()
	}
	// This is the requested nominal segment budget, not evidence of actual
	// packet boundaries. Published playlists retain their independent parser
	// and publication limits; copy preroll cannot establish a fixed timeline.
	nominal := int64(p.SegmentSeconds) * ticksPerSecond
	count := (w.EndTicks - p.StartTicks + nominal - 1) / nominal
	if count > int64(MaxPlaylistSegments-w.StartNumber) {
		return invalid()
	}
	if w.RequireInputEvidence && !GeneratedWindowClosureEligible(p) {
		return invalid()
	}
	if err := validateGeneratedWindowNativeClock(p); err != nil {
		return invalid()
	}
	return nil
}

// GeneratedWindowClosureEligible is the initial source-range proof subset.
// Average source frame rate does not establish source CFR. Actual input
// association and closed output packets must independently qualify each job.
// Unsupported transforms retain the complete-source generated path.
func GeneratedWindowClosureEligible(p Plan) bool {
	decode, encode := hardwareSelection(p.Hardware)
	return p.OutputMode == "" && p.SourceMode == "" && p.SegmentMode == "" && GeneratedHLS(p) &&
		p.VideoStreamIndex >= 0 && p.VideoCodec == "h264" && p.AudioStreamIndex == -1 && p.AudioCodec == "" &&
		p.FrameRate >= 1 && p.FrameRate <= 60 && math.Trunc(p.FrameRate) == p.FrameRate &&
		decode == "software" && encode == "software" && p.Subtitle.Mode == "" && !HasHLSSubtitles(p) && p.VideoFilters == (VideoFilters{}) &&
		(p.HLS.SegmentType == "mpegts" || p.HLS.SegmentType == "fmp4") && !p.AudioSampleSeek
}

func generatedHLSEndTicks(p Plan) int64 {
	if hasHLSWindow(p) {
		return p.HLS.Window.EndTicks
	}
	return p.DurationTicks
}

func generatedHLSStartNumber(p Plan) int {
	if hasHLSWindow(p) {
		return p.HLS.Window.StartNumber
	}
	return p.SegmentStartNumber
}

// generatedWindowSourceOriginTicks retains the exact source sample origin for
// sample-domain seeking. Resetting the filter's local PTS does not restore the
// fractional requested position that was rounded up to a whole source sample.
func generatedWindowSourceOriginTicks(p Plan) *big.Rat {
	if !p.AudioSampleSeek {
		return new(big.Rat).SetInt64(p.StartTicks)
	}
	start := progressiveSampleLimit(p.StartTicks, p.AudioSourceSampleRate)
	return new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(start), big.NewInt(ticksPerSecond)), big.NewInt(int64(p.AudioSourceSampleRate)))
}

// HLSWindowSourceAnchorTicks maps the observed first pre-container packet into
// the window's source epoch without rounding its rational clock. The result is
// private affine timing evidence, not a terminal coverage, restart, gapless
// audio, or fixed source-segment proof. The publisher must independently pair
// that packet with the actual media and initialization from this same job.
// Native clock v1 accepts only its observed local-zero premux epoch and maps
// the explicit output offset once; its actual media clock is proved separately.
func HLSWindowSourceAnchorTicks(p Plan, clock HLSMuxClock) (*big.Rat, error) {
	if !hasHLSWindow(p) || ValidatePlan(p) != nil || clock.Rendition < 0 || clock.Rendition >= max(1, p.HLS.RenditionCount) {
		return nil, ErrInvalidTimeline
	}
	if _, err := clock.Ticks(); err != nil {
		return nil, err
	}
	if p.HLS.Window.NativeClockVersion == GeneratedWindowNativeClockV1 {
		return generatedWindowNativeSourceAnchor(p, clock)
	}
	if p.HLS.Window.NativeClockVersion == GeneratedWindowNativeClockV2 {
		return generatedWindowFMP4NativeSourceAnchor(p, clock)
	}
	packet := new(big.Int).Mul(big.NewInt(clock.PTS), big.NewInt(clock.TimeBaseNumerator))
	packet.Mul(packet, big.NewInt(ticksPerSecond))
	anchor := new(big.Rat).SetFrac(packet, big.NewInt(clock.TimeBaseDenominator))
	anchor.Add(anchor, generatedWindowSourceOriginTicks(p))
	if anchor.Cmp(new(big.Rat).SetInt64(-maxDurationTicks)) < 0 || anchor.Cmp(new(big.Rat).SetInt64(maxDurationTicks)) > 0 {
		return nil, ErrInvalidTimeline
	}
	return anchor, nil
}

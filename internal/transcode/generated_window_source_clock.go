package transcode

import "math/big"

// GeneratedWindowNativeClockV1 binds one silent encoded TS window to the
// source-relative presentation clock. Version zero retains generic mux epochs.
const GeneratedWindowNativeClockV1 uint8 = 1

const (
	generatedWindowNativeClockRate     = int64(90_000)
	generatedWindowNativeClockHalfWrap = int64(1) << 32
)

// GeneratedWindowNativeClockEligible describes a candidate emission contract,
// not observed source cadence or output coverage. The conservative full-source
// bound avoids the MPEG-TS half-cycle ambiguity without inventing an unwrap
// contract for future slots of an already published presentation.
func GeneratedWindowNativeClockEligible(p Plan) bool {
	if !GeneratedWindowClosureEligible(p) || !p.HLS.Window.RequireInputEvidence ||
		p.HLS.SegmentType != "mpegts" || p.CopyTimestamps || p.SourceFormatStartKnown || p.SourceFormatStartTicks != 0 ||
		p.DurationTicks <= 0 || p.DurationTicks > maxDurationTicks || p.StartTicks < 0 ||
		p.HLS.Window.EndTicks <= p.StartTicks || p.HLS.Window.EndTicks > p.DurationTicks || p.DurationTicks%1000 != 0 ||
		p.StartTicks%1000 != 0 || p.HLS.Window.EndTicks%1000 != 0 {
		return false
	}
	rate := int64(p.FrameRate)
	if generatedWindowNativeClockRate%rate != 0 {
		return false
	}
	// Existing duration bounds keep this multiplication within int64.
	return p.DurationTicks*generatedWindowNativeClockRate < generatedWindowNativeClockHalfWrap*ticksPerSecond
}

func validateGeneratedWindowNativeClock(p Plan) error {
	if p.HLS.Window.NativeClockVersion == 0 {
		return nil
	}
	if p.HLS.Window.NativeClockVersion == GeneratedWindowNativeClockV2 {
		if !GeneratedWindowFMP4NativeClockEligible(p) {
			return ErrInvalidPlan
		}
		return nil
	}
	if p.HLS.Window.NativeClockVersion != GeneratedWindowNativeClockV1 || !GeneratedWindowNativeClockEligible(p) {
		return ErrInvalidPlan
	}
	return nil
}

// The v1 command shifts packets only inside libavformat, after stats_mux_pre.
// Its actual encoder-backed first mux record must therefore retain the local
// zero epoch. Nonzero records are rejected rather than selecting whichever
// epoch happens to agree with a nominal presentation. Actual media calibration
// independently checks this association and the emitted source-global bounds.
func generatedWindowNativeSourceAnchor(p Plan, clock HLSMuxClock) (*big.Rat, error) {
	if p.HLS.Window.NativeClockVersion != GeneratedWindowNativeClockV1 || !GeneratedWindowNativeClockEligible(p) ||
		clock.Rendition < 0 || clock.Rendition >= max(1, p.HLS.RenditionCount) {
		return nil, ErrInvalidTimeline
	}
	if _, err := clock.Ticks(); err != nil {
		return nil, err
	}
	if generatedClockSeconds(clock.PTS, clock.TimeBaseNumerator, clock.TimeBaseDenominator).Sign() != 0 {
		return nil, ErrInvalidTimeline
	}
	return new(big.Rat).SetInt64(p.StartTicks), nil
}

func validateGeneratedWindowSourceClock(p Plan, clock HLSMuxClock, first, end *big.Rat) error {
	if _, err := generatedWindowNativeSourceAnchor(p, clock); err != nil {
		return err
	}
	if first == nil || end == nil || first.Cmp(generatedTicksSeconds(p.StartTicks)) != 0 ||
		end.Cmp(generatedTicksSeconds(p.HLS.Window.EndTicks)) != 0 {
		return ErrInvalidTimeline
	}
	return nil
}

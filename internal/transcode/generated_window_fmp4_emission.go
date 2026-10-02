package transcode

import (
	"math"
	"math/big"
)

// GeneratedWindowNativeClockV2 is the independently observed silent AVC fMP4
// emission contract. TS version one and generic version zero remain separate.
const GeneratedWindowNativeClockV2 uint8 = 2

// GeneratedWindowFMP4NativeClockEligible describes a command candidate, not
// source CFR, output closure or a publication decision. Only the calibrated
// software frame rates are admitted; audio, edits and copied clocks require
// independent contracts. Microsecond-representable boundaries match the actual
// outer muxer's output offset, without rounding a source or sample endpoint.
func GeneratedWindowFMP4NativeClockEligible(p Plan) bool {
	return GeneratedWindowClosureEligible(p) && p.HLS.Window.RequireInputEvidence && p.HLS.SegmentType == "fmp4" &&
		(p.HLS.RenditionCount == 0 || p.HLS.RenditionCount == 2) &&
		(p.FrameRate == 24 || p.FrameRate == 25) && !p.CopyTimestamps && !p.SourceFormatStartKnown && p.SourceFormatStartTicks == 0 &&
		p.DurationTicks > 0 && p.DurationTicks <= maxDurationTicks && p.StartTicks >= 0 && p.HLS.Window.EndTicks > p.StartTicks &&
		p.HLS.Window.EndTicks <= p.DurationTicks && p.DurationTicks%1000 == 0 && p.StartTicks%1000 == 0 && p.HLS.Window.EndTicks%1000 == 0
}

func generatedWindowFMP4NativeSourceAnchor(p Plan, clock HLSMuxClock) (*big.Rat, error) {
	if p.HLS.Window.NativeClockVersion != GeneratedWindowNativeClockV2 || !GeneratedWindowFMP4NativeClockEligible(p) ||
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

func validateGeneratedWindowFMP4SourceClock(p Plan, clock HLSMuxClock, first, end *big.Rat) error {
	if _, err := generatedWindowFMP4NativeSourceAnchor(p, clock); err != nil {
		return err
	}
	if first == nil || end == nil || first.Cmp(generatedTicksSeconds(p.StartTicks)) != 0 || end.Cmp(generatedTicksSeconds(p.HLS.Window.EndTicks)) != 0 {
		return ErrInvalidTimeline
	}
	return nil
}

// GeneratedFMP4NativeEmission is a compact result after every native fragment
// has been paired with actual packet decode order. Hashes bind the independently
// held initialization and media. This result still needs the caller's complete
// source/input closure, source identity fence and ownership before publication.
type GeneratedFMP4NativeEmission struct {
	StartTicks, EndTicks int64
	RenditionCount       int
	TrackIDs             [MaxHLSRenditions]uint32
	MediaTimeScales      [MaxHLSRenditions]int64
	InitializationSHA256 [MaxHLSRenditions][32]byte
	SegmentSHA256        [MaxHLSRenditions][32]byte
}

func ValidateGeneratedFMP4NativeEmission(p Plan, mux [MaxHLSRenditions]HLSMuxClock, outputs [MaxHLSRenditions]GeneratedSegmentBounds,
	native [MaxHLSRenditions]GeneratedFMP4NativeClock) (GeneratedFMP4NativeEmission, error) {
	var empty GeneratedFMP4NativeEmission
	if ValidatePlan(p) != nil || p.HLS.Window.NativeClockVersion != GeneratedWindowNativeClockV2 || !GeneratedWindowFMP4NativeClockEligible(p) {
		return empty, ErrInvalidPlan
	}
	span := p.HLS.Window.EndTicks - p.StartTicks
	rate := int64(p.FrameRate)
	if span <= 0 || span > int64(p.SegmentSeconds)*ticksPerSecond || span*rate%ticksPerSecond != 0 {
		return empty, ErrInvalidTimeline
	}
	frames := span * rate / ticksPerSecond
	count := max(1, p.HLS.RenditionCount)
	result := GeneratedFMP4NativeEmission{StartTicks: p.StartTicks, EndTicks: p.HLS.Window.EndTicks, RenditionCount: count}
	for index := 0; index < MaxHLSRenditions; index++ {
		if index >= count {
			if mux[index] != (HLSMuxClock{}) || outputs[index] != (GeneratedSegmentBounds{}) || native[index] != (GeneratedFMP4NativeClock{}) {
				return empty, ErrInvalidTimeline
			}
			continue
		}
		bounds, clock := outputs[index], native[index]
		video := bounds.Video
		if clock.TrackID != 1 || clock.MediaTimeScale <= 0 || clock.EditPresent || clock.HeaderDurationKnown || clock.HeaderDurationUnits != 0 ||
			clock.FragmentCount < 1 || clock.FragmentCount > maxGeneratedFMP4NativeFragments || clock.TotalSamples != frames ||
			clock.InitializationSHA256 == ([32]byte{}) || clock.SegmentSHA256 == ([32]byte{}) ||
			clock.InitializationSHA256 != bounds.InitializationSHA256 || clock.SegmentSHA256 != bounds.SegmentSHA256 ||
			!video.Present || video.Kind != "video" || video.Codec != "h264" || bounds.Audio.Present || video.PacketCount != frames ||
			!video.FirstKey || video.HasCorrupt || video.HasDiscard || video.SkipSamples != 0 || video.DiscardPadding != 0 ||
			!video.PresentationDecodeAligned || video.TimeBase.Num <= 0 || video.TimeBase.Den <= 0 ||
			video.MinPacketDuration <= 0 || video.MinPacketDuration != video.MaxPacketDuration ||
			video.FirstPTS != video.FirstDTS || video.LastPTS != video.LastDTS || video.EndPTS != video.EndDTS ||
			video.FirstPacketPTS != video.FirstPTS || video.LastPacketPTS != video.LastPTS ||
			video.TotalPresentationGapTicks != 0 || video.TotalDecodeGapTicks != 0 || video.MaxPresentationGapTicks != 0 || video.MaxDecodeGapTicks != 0 {
			return empty, ErrInvalidTimeline
		}
		unit := new(big.Rat).SetFrac64(1, clock.MediaTimeScale)
		if unit.Cmp(generatedClockSeconds(1, video.TimeBase.Num, video.TimeBase.Den)) != 0 ||
			generatedClockSeconds(video.MinPacketDuration, video.TimeBase.Num, video.TimeBase.Den).Cmp(new(big.Rat).SetFrac64(1, rate)) != 0 {
			return empty, ErrInvalidTimeline
		}
		first := generatedClockSeconds(video.FirstPTS, video.TimeBase.Num, video.TimeBase.Den)
		end := generatedClockSeconds(video.EndPTS, video.TimeBase.Num, video.TimeBase.Den)
		if validateGeneratedWindowFMP4SourceClock(p, mux[index], first, end) != nil || mux[index].Rendition != index ||
			new(big.Rat).Sub(end, generatedClockSeconds(video.LastPTS, video.TimeBase.Num, video.TimeBase.Den)).Cmp(new(big.Rat).SetFrac64(1, rate)) != 0 {
			return empty, ErrInvalidTimeline
		}
		var samples int64
		var sequence uint32
		for fragmentIndex, fragment := range clock.Fragments {
			if fragmentIndex >= clock.FragmentCount {
				if fragment != (GeneratedFMP4FragmentClock{}) {
					return empty, ErrInvalidTimeline
				}
				continue
			}
			if fragment.TrackID != clock.TrackID || fragment.SequenceNumber <= sequence || fragment.DecodeUnits > math.MaxInt64 || fragment.SampleCount < 1 ||
				fragment.SampleCount > frames-samples || int64(fragment.DecodeUnits) != video.FirstDTS+samples*video.MinPacketDuration {
				return empty, ErrInvalidTimeline
			}
			samples += fragment.SampleCount
			sequence = fragment.SequenceNumber
		}
		if samples != frames {
			return empty, ErrInvalidTimeline
		}
		result.TrackIDs[index], result.MediaTimeScales[index] = clock.TrackID, clock.MediaTimeScale
		result.InitializationSHA256[index], result.SegmentSHA256[index] = clock.InitializationSHA256, clock.SegmentSHA256
	}
	return result, nil
}

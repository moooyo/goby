package transcode

import (
	"math/big"
)

// GeneratedWindowClosure binds one normally completed output slot to observed
// source input, mux packets and immutable media bytes. Its current proof subset
// excludes audio priming, copied access units and noninteger output cadence.
type GeneratedWindowClosure struct {
	StartTicks, EndTicks int64
	Number               int
	RenditionCount       int
	NativeClockVersion   uint8
	Source               GeneratedSourceRange
	Input                [MaxHLSRenditions]GeneratedInputEvidence
	Output               [MaxHLSRenditions]GeneratedSegmentBounds
}

func generatedClockSeconds(value, numerator, denominator int64) *big.Rat {
	return new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(value), big.NewInt(numerator)), big.NewInt(denominator))
}

func generatedTicksSeconds(value int64) *big.Rat {
	return new(big.Rat).SetFrac(big.NewInt(value), big.NewInt(ticksPerSecond))
}

func generatedRatAbs(value *big.Rat) *big.Rat {
	if value.Sign() < 0 {
		return new(big.Rat).Neg(value)
	}
	return new(big.Rat).Set(value)
}

// ValidateGeneratedWindowClosure consumes observed facts, not nominal packet
// bounds. A full-source admission timeline still needs its independent source
// duration contract; this proof never upgrades metadata into a source EOF fact.
func ValidateGeneratedWindowClosure(plan Plan, source GeneratedSourceRange, input [MaxHLSRenditions]GeneratedInputEvidence, mux [MaxHLSRenditions]HLSMuxClock, output [MaxHLSRenditions]GeneratedSegmentBounds, lists [MaxHLSRenditions]MediaPlaylist) (GeneratedWindowClosure, error) {
	invalid := func() (GeneratedWindowClosure, error) { return GeneratedWindowClosure{}, ErrInvalidTimeline }
	if ValidatePlan(plan) != nil || !plan.HLS.Window.RequireInputEvidence || !GeneratedWindowClosureEligible(plan) {
		return invalid()
	}
	duration := plan.HLS.Window.EndTicks - plan.StartTicks
	rate := int64(plan.FrameRate)
	if duration <= 0 || duration > int64(plan.SegmentSeconds)*ticksPerSecond || duration*rate%ticksPerSecond != 0 {
		return invalid()
	}
	frames := duration * rate / ticksPerSecond
	if source.StartTicks != plan.StartTicks || source.EndTicks != plan.HLS.Window.EndTicks || source.FrameCount != frames ||
		source.First.Den <= 0 || source.Last.Den <= 0 || source.End.Den <= 0 || source.FrameDuration.Den <= 0 ||
		generatedClockSeconds(1, source.First.Num, source.First.Den).Cmp(generatedTicksSeconds(plan.StartTicks)) != 0 ||
		generatedClockSeconds(1, source.End.Num, source.End.Den).Cmp(generatedTicksSeconds(plan.HLS.Window.EndTicks)) != 0 ||
		generatedClockSeconds(1, source.FrameDuration.Num, source.FrameDuration.Den).Cmp(new(big.Rat).SetFrac64(1, rate)) != 0 ||
		generatedClockSeconds(1, source.Last.Num, source.Last.Den).Cmp(new(big.Rat).Sub(generatedTicksSeconds(plan.HLS.Window.EndTicks), new(big.Rat).SetFrac64(1, rate))) != 0 {
		return invalid()
	}
	count := max(1, plan.HLS.RenditionCount)
	closure := GeneratedWindowClosure{StartTicks: plan.StartTicks, EndTicks: plan.HLS.Window.EndTicks,
		Number: plan.HLS.Window.StartNumber, RenditionCount: count, NativeClockVersion: plan.HLS.Window.NativeClockVersion,
		Source: source, Input: input, Output: output}
	var referenceSourceFirst, referenceSourceLast *big.Rat
	var referenceOutputFirst, referenceOutputEnd *big.Rat
	var referenceFirstFrame, referenceLastFrame int64
	for index := 0; index < count; index++ {
		observed := input[index]
		if ValidateGeneratedInputEvidence(plan, observed) != nil || observed.Rendition != index || observed.Frames != frames ||
			!observed.SourceSequential || !observed.InputCadenceAligned || !observed.InputCadenceExact || observed.FirstInputPTS < 0 || observed.FirstEncoderPTS != 0 {
			return invalid()
		}
		sourceFirst := generatedClockSeconds(observed.FirstInputPTS, observed.InputTimeBaseNumerator, observed.InputTimeBaseDenominator)
		sourceLast := generatedClockSeconds(observed.LastInputPTS, observed.InputTimeBaseNumerator, observed.InputTimeBaseDenominator)
		// A one-quantum diagnostic allowance cannot authorize a source-epoch
		// shift. Exact cumulative cadence connects every observed record with
		// the independently decoded source-frame grid.
		if new(big.Rat).Add(generatedTicksSeconds(plan.StartTicks), sourceFirst).Cmp(generatedClockSeconds(1, source.First.Num, source.First.Den)) != 0 ||
			new(big.Rat).Add(generatedTicksSeconds(plan.StartTicks), sourceLast).Cmp(generatedClockSeconds(1, source.Last.Num, source.Last.Den)) != 0 {
			return invalid()
		}
		if index == 0 {
			referenceSourceFirst, referenceSourceLast = sourceFirst, sourceLast
			referenceFirstFrame, referenceLastFrame = observed.FirstInputFrame, observed.LastInputFrame
		} else if sourceFirst.Cmp(referenceSourceFirst) != 0 || sourceLast.Cmp(referenceSourceLast) != 0 ||
			observed.FirstInputFrame != referenceFirstFrame || observed.LastInputFrame != referenceLastFrame {
			return invalid()
		}
		list := lists[index]
		if !list.Ended || list.Type != "EVENT" || list.Sequence != int64(closure.Number) || len(list.Segments) != 1 || list.Segments[0].Number != int64(closure.Number) ||
			!list.Segments[0].Discontinuity || list.Segments[0].DurationTicks != duration ||
			plan.HLS.SegmentType == "fmp4" && list.InitName == "" || plan.HLS.SegmentType == "mpegts" && list.InitName != "" {
			return invalid()
		}
		bounds := output[index]
		video := bounds.Video
		if !video.Present || video.Kind != "video" || video.Codec != "h264" || bounds.Audio.Present || video.PacketCount != frames ||
			!video.FirstKey || video.HasCorrupt || video.HasDiscard || video.SkipSamples != 0 || video.DiscardPadding != 0 ||
			video.TimeBase.Num <= 0 || video.TimeBase.Den <= 0 || video.FirstPTS != video.FirstDTS || video.EndPTS != video.EndDTS ||
			!video.PresentationDecodeAligned || video.MinPacketDuration <= 0 || video.MinPacketDuration != video.MaxPacketDuration ||
			video.FirstPacketPTS != video.FirstPTS || video.LastPacketPTS != video.LastPTS ||
			video.LastPTS < video.FirstPTS || video.EndPTS <= video.LastPTS || video.LastDTS != video.LastPTS ||
			video.TotalPresentationGapTicks != 0 || video.TotalDecodeGapTicks != 0 || video.MaxPresentationGapTicks != 0 || video.MaxDecodeGapTicks != 0 {
			return invalid()
		}
		outputFirst := generatedClockSeconds(video.FirstPTS, video.TimeBase.Num, video.TimeBase.Den)
		outputLast := generatedClockSeconds(video.LastPTS, video.TimeBase.Num, video.TimeBase.Den)
		outputEnd := generatedClockSeconds(video.EndPTS, video.TimeBase.Num, video.TimeBase.Den)
		period := generatedClockSeconds(video.MinPacketDuration, video.TimeBase.Num, video.TimeBase.Den)
		if new(big.Rat).Sub(outputEnd, outputFirst).Cmp(generatedTicksSeconds(duration)) != 0 || period.Cmp(new(big.Rat).SetFrac64(1, rate)) != 0 ||
			new(big.Rat).Sub(outputEnd, outputLast).Cmp(period) != 0 || bounds.SegmentSHA256 == ([32]byte{}) ||
			plan.HLS.SegmentType == "fmp4" && bounds.InitializationSHA256 == ([32]byte{}) {
			return invalid()
		}
		if index == 0 {
			referenceOutputFirst, referenceOutputEnd = outputFirst, outputEnd
		} else if outputFirst.Cmp(referenceOutputFirst) != 0 || outputEnd.Cmp(referenceOutputEnd) != 0 {
			return invalid()
		}
		firstMux := mux[index]
		if firstMux.Rendition != index || firstMux.TimeBaseNumerator <= 0 || firstMux.TimeBaseDenominator <= 0 ||
			generatedClockSeconds(firstMux.PTS, firstMux.TimeBaseNumerator, firstMux.TimeBaseDenominator).Sign() != 0 {
			return invalid()
		}
		if plan.HLS.Window.NativeClockVersion == GeneratedWindowNativeClockV1 &&
			validateGeneratedWindowSourceClock(plan, firstMux, outputFirst, outputEnd) != nil {
			return invalid()
		}
		if plan.HLS.Window.NativeClockVersion == GeneratedWindowNativeClockV2 &&
			validateGeneratedWindowFMP4SourceClock(plan, firstMux, outputFirst, outputEnd) != nil {
			return invalid()
		}
	}
	return closure, nil
}

package transcode

import (
	"errors"
	"math"
	"testing"
)

type generatedClosureFixture struct {
	plan   Plan
	source GeneratedSourceRange
	input  [MaxHLSRenditions]GeneratedInputEvidence
	mux    [MaxHLSRenditions]HLSMuxClock
	output [MaxHLSRenditions]GeneratedSegmentBounds
	lists  [MaxHLSRenditions]MediaPlaylist
}

func generatedClosureTestFixture(adaptive, fragmented bool) generatedClosureFixture {
	fixture := generatedClosureFixture{}
	fixture.plan = Plan{Container: "ts", VideoStreamIndex: 0, AudioStreamIndex: -1, VideoCodec: "h264",
		Width: 160, Height: 96, VideoBitrate: 256000, FrameRate: 30, SegmentSeconds: 3,
		StartTicks: 90 * ticksPerSecond, DurationTicks: 120 * ticksPerSecond,
		HLS: HLSPlan{SegmentType: "mpegts", Window: HLSWindow{EndTicks: 93 * ticksPerSecond, StartNumber: 300, RequireInputEvidence: true}}}
	if fragmented {
		fixture.plan.Container, fixture.plan.HLS.SegmentType = "mp4", "fmp4"
	}
	if adaptive {
		fixture.plan.HLS.RenditionCount = 2
		fixture.plan.HLS.Renditions[0] = HLSRendition{Width: 160, Height: 96, VideoBitrate: 256000}
		fixture.plan.HLS.Renditions[1] = HLSRendition{Width: 80, Height: 48, VideoBitrate: 96000}
	}
	fixture.source = GeneratedSourceRange{StartTicks: fixture.plan.StartTicks, EndTicks: fixture.plan.HLS.Window.EndTicks, FrameCount: 90,
		First: GeneratedRational{Num: 90, Den: 1}, Last: GeneratedRational{Num: 2789, Den: 30},
		End: GeneratedRational{Num: 93, Den: 1}, FrameDuration: GeneratedRational{Num: 1, Den: 30}}
	for index := 0; index < max(1, fixture.plan.HLS.RenditionCount); index++ {
		fixture.input[index] = GeneratedInputEvidence{Rendition: index, Frames: 90, FirstInputFrame: 0, LastInputFrame: 89,
			InputTimeBaseNumerator: 1, InputTimeBaseDenominator: 90000, FirstInputPTS: 0, LastInputPTS: 267000,
			EncoderTimeBaseNumerator: 1, EncoderTimeBaseDenominator: 30, FirstEncoderPTS: 0, LastEncoderPTS: 89,
			SourceSequential: true, InputCadenceAligned: true, InputCadenceExact: true}
		fixture.mux[index] = HLSMuxClock{Rendition: index, PTS: 0, TimeBaseNumerator: 1, TimeBaseDenominator: 30}
		first := int64(126000)
		if fragmented {
			first = 0
		}
		fixture.output[index] = GeneratedSegmentBounds{Video: GeneratedTrackBounds{Present: true, Kind: "video", Codec: "h264",
			TimeBase: GeneratedRational{Num: 1, Den: 90000}, FirstPTS: first, LastPTS: first + 89*3000, EndPTS: first + 90*3000,
			FirstDTS: first, LastDTS: first + 89*3000, EndDTS: first + 90*3000, FirstPacketPTS: first, LastPacketPTS: first + 89*3000,
			FirstPacketDuration: 3000, LastPacketDuration: 3000, MinPacketDuration: 3000, MaxPacketDuration: 3000,
			PresentationDecodeAligned: true, FirstKey: true, PacketCount: 90}, SegmentSHA256: [32]byte{1}}
		fixture.lists[index] = MediaPlaylist{Type: "EVENT", Ended: true, Independent: true, Sequence: 300, TargetDuration: 3,
			Segments: []MediaSegment{{Number: 300, Name: "segment-000300.ts", DurationTicks: 3 * ticksPerSecond, Discontinuity: true}}}
		if fragmented {
			fixture.output[index].InitializationSHA256 = [32]byte{2}
			fixture.lists[index].InitName = "init.mp4"
			fixture.lists[index].Segments[0].Name = "segment-000300.m4s"
		}
	}
	return fixture
}

func (f generatedClosureFixture) validate() (GeneratedWindowClosure, error) {
	return ValidateGeneratedWindowClosure(f.plan, f.source, f.input, f.mux, f.output, f.lists)
}

func TestGeneratedWindowClosureRequiresObservedSourceAndClosedCFRSlot(t *testing.T) {
	for _, adaptive := range []bool{false, true} {
		for _, fragmented := range []bool{false, true} {
			fixture := generatedClosureTestFixture(adaptive, fragmented)
			closure, err := fixture.validate()
			if err != nil || closure.StartTicks != 90*ticksPerSecond || closure.EndTicks != 93*ticksPerSecond ||
				closure.Number != 300 || closure.Source != fixture.source || closure.RenditionCount != max(1, fixture.plan.HLS.RenditionCount) {
				t.Fatalf("closed observed slot: adaptive=%v, fragmented=%v, %+v, %v", adaptive, fragmented, closure, err)
			}
		}
	}
}

func TestGeneratedWindowClosureRejectsEndpointOnlyAndNominalFacts(t *testing.T) {
	for name, mutate := range map[string]func(*generatedClosureFixture){
		"missing source range":         func(f *generatedClosureFixture) { f.source = GeneratedSourceRange{} },
		"short final source frame":     func(f *generatedClosureFixture) { f.source.End = GeneratedRational{Num: 9299, Den: 100} },
		"source output grid differs":   func(f *generatedClosureFixture) { f.source.FrameDuration = GeneratedRational{Num: 1, Den: 24} },
		"intermediate input duplicate": func(f *generatedClosureFixture) { f.input[0].SourceSequential = false },
		"intermediate input cadence":   func(f *generatedClosureFixture) { f.input[0].InputCadenceAligned = false },
		"intermediate quantization":    func(f *generatedClosureFixture) { f.input[0].InputCadenceExact = false },
		"entire input epoch shifted": func(f *generatedClosureFixture) {
			f.input[0].FirstInputPTS++
			f.input[0].LastInputPTS++
		},
		"coarse input clock": func(f *generatedClosureFixture) {
			f.input[0].InputTimeBaseDenominator, f.input[0].LastInputPTS = 1, 3
		},
		"intermediate unequal duration": func(f *generatedClosureFixture) {
			f.output[0].Video.MinPacketDuration, f.output[0].Video.MaxPacketDuration = 1500, 4500
		},
		"intermediate output reorder": func(f *generatedClosureFixture) { f.output[0].Video.PresentationDecodeAligned = false },
		"rounded output cadence":      func(f *generatedClosureFixture) { f.output[0].Video.TimeBase.Den = 89999 },
		"output gap":                  func(f *generatedClosureFixture) { f.output[0].Video.TotalDecodeGapTicks = 1 },
		"output digest missing":       func(f *generatedClosureFixture) { f.output[0].SegmentSHA256 = [32]byte{} },
		"unfinished playlist":         func(f *generatedClosureFixture) { f.lists[0].Ended = false },
		"missing discontinuity":       func(f *generatedClosureFixture) { f.lists[0].Segments[0].Discontinuity = false },
		"wrong segment duration":      func(f *generatedClosureFixture) { f.lists[0].Segments[0].DurationTicks++ },
		"different input identity": func(f *generatedClosureFixture) {
			f.input[1].FirstInputFrame, f.input[1].LastInputFrame = 1, 90
		},
		"different mux epoch": func(f *generatedClosureFixture) {
			v := &f.output[1].Video
			v.FirstPTS++
			v.LastPTS++
			v.EndPTS++
			v.FirstDTS++
			v.LastDTS++
			v.EndDTS++
			v.FirstPacketPTS++
			v.LastPacketPTS++
		},
		"overflowing output span": func(f *generatedClosureFixture) {
			f.output[0].Video.FirstPTS, f.output[0].Video.FirstDTS, f.output[0].Video.FirstPacketPTS = math.MinInt64, math.MinInt64, math.MinInt64
			f.output[0].Video.EndPTS, f.output[0].Video.EndDTS = math.MaxInt64, math.MaxInt64
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := generatedClosureTestFixture(true, false)
			mutate(&fixture)
			if _, err := fixture.validate(); !errors.Is(err, ErrInvalidTimeline) {
				t.Fatalf("unsupported closure admitted: %v", err)
			}
		})
	}
}

func TestGeneratedWindowClosureDoesNotEnableUnsupportedPlans(t *testing.T) {
	for name, mutate := range map[string]func(*Plan){
		"audio priming":          func(p *Plan) { p.AudioStreamIndex, p.AudioCodec = 1, "aac" },
		"stream copy":            func(p *Plan) { p.VideoCodec, p.VideoCopyCodec = "copy", "h264" },
		"fractional output rate": func(p *Plan) { p.FrameRate = 30000.0 / 1001.0 },
		"live source":            func(p *Plan) { p.SourceMode = "stream" },
		"source missing":         func(p *Plan) { p.VideoStreamIndex = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := generatedClosureTestFixture(false, false)
			mutate(&fixture.plan)
			if GeneratedWindowClosureEligible(fixture.plan) {
				t.Fatal("unsupported transformation acquired the strict closure contract")
			}
		})
	}
}

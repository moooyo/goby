package transcode

import (
	"errors"
	"testing"
)

func generatedFMP4EmissionFixture(count int) (Plan, [MaxHLSRenditions]HLSMuxClock, [MaxHLSRenditions]GeneratedSegmentBounds, [MaxHLSRenditions]GeneratedFMP4NativeClock) {
	plan := generatedClosureMediaPlanForTest(count)
	var mux [MaxHLSRenditions]HLSMuxClock
	var outputs [MaxHLSRenditions]GeneratedSegmentBounds
	var native [MaxHLSRenditions]GeneratedFMP4NativeClock
	for index := 0; index < max(1, count); index++ {
		mux[index] = HLSMuxClock{Rendition: index, TimeBaseNumerator: 1, TimeBaseDenominator: 12_288}
		video := GeneratedTrackBounds{Present: true, Kind: "video", Codec: "h264", TimeBase: GeneratedRational{Num: 1, Den: 12_288},
			FirstPTS: 1_105_920, FirstDTS: 1_105_920, FirstPacketPTS: 1_105_920, LastPTS: 1_179_136, LastDTS: 1_179_136, LastPacketPTS: 1_179_136,
			EndPTS: 1_179_648, EndDTS: 1_179_648, PacketCount: 144, FirstKey: true,
			FirstPacketDuration: 512, LastPacketDuration: 512, MinPacketDuration: 512, MaxPacketDuration: 512, PresentationDecodeAligned: true}
		outputs[index].Video = video
		outputs[index].InitializationSHA256[0], outputs[index].SegmentSHA256[0] = byte(index+1), byte(index+11)
		native[index] = GeneratedFMP4NativeClock{TrackID: 1, MediaTimeScale: 12_288, FragmentCount: 1, TotalSamples: 144,
			InitializationSHA256: outputs[index].InitializationSHA256, SegmentSHA256: outputs[index].SegmentSHA256}
		native[index].Fragments[0] = GeneratedFMP4FragmentClock{TrackID: 1, SequenceNumber: 1, DecodeUnits: 1_105_920, SampleCount: 144}
	}
	return plan, mux, outputs, native
}

func generatedClosureMediaPlanForTest(count int) Plan {
	plan := Plan{Container: "mp4", VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: -1, Width: 160, Height: 96,
		FrameRate: 24, VideoBitrate: 256_000, DurationTicks: 100 * ticksPerSecond, StartTicks: 90 * ticksPerSecond, SegmentSeconds: 6,
		HLS: HLSPlan{SegmentType: "fmp4", Window: HLSWindow{EndTicks: 96 * ticksPerSecond, StartNumber: 15, RequireInputEvidence: true, NativeClockVersion: GeneratedWindowNativeClockV2}}}
	if count > 0 {
		plan.HLS.RenditionCount = count
		for index := 0; index < count; index++ {
			plan.HLS.Renditions[index] = HLSRendition{Width: 160 >> index, Height: 96 >> index, VideoBitrate: 256_000 >> index}
		}
	}
	return plan
}

func TestGeneratedFMP4EmissionBindsNativeTrackEveryFragmentAndWholeBytes(t *testing.T) {
	for _, count := range []int{0, 2} {
		plan, mux, outputs, native := generatedFMP4EmissionFixture(count)
		proof, err := ValidateGeneratedFMP4NativeEmission(plan, mux, outputs, native)
		if err != nil || proof.StartTicks != plan.StartTicks || proof.EndTicks != plan.HLS.Window.EndTicks || proof.RenditionCount != max(1, count) {
			t.Fatalf("complete native packet association rejected: %v", err)
		}
		for index := 0; index < max(1, count); index++ {
			if proof.MediaTimeScales[index] != native[index].MediaTimeScale || proof.TrackIDs[index] != native[index].TrackID ||
				proof.InitializationSHA256[index] != outputs[index].InitializationSHA256 || proof.SegmentSHA256[index] != outputs[index].SegmentSHA256 {
				t.Fatal("compact emission proof lost its actual initialization/media association")
			}
		}
	}
}

func TestGeneratedFMP4EmissionRejectsEpochEditsAliasesAndIncompleteSibling(t *testing.T) {
	for name, mutate := range map[string]func(*[MaxHLSRenditions]HLSMuxClock, *[MaxHLSRenditions]GeneratedSegmentBounds, *[MaxHLSRenditions]GeneratedFMP4NativeClock){
		"premux source epoch": func(mux *[MaxHLSRenditions]HLSMuxClock, _ *[MaxHLSRenditions]GeneratedSegmentBounds, _ *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			mux[1].PTS = 1_105_920
		},
		"movie edit": func(_ *[MaxHLSRenditions]HLSMuxClock, _ *[MaxHLSRenditions]GeneratedSegmentBounds, raw *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			raw[1].EditPresent = true
		},
		"known zero duration": func(_ *[MaxHLSRenditions]HLSMuxClock, _ *[MaxHLSRenditions]GeneratedSegmentBounds, raw *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			raw[1].HeaderDurationKnown = true
		},
		"different initialized track": func(_ *[MaxHLSRenditions]HLSMuxClock, _ *[MaxHLSRenditions]GeneratedSegmentBounds, raw *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			raw[1].TrackID = 2
		},
		"native scale mismatch": func(_ *[MaxHLSRenditions]HLSMuxClock, _ *[MaxHLSRenditions]GeneratedSegmentBounds, raw *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			raw[1].MediaTimeScale++
		},
		"unpaired init bytes": func(_ *[MaxHLSRenditions]HLSMuxClock, _ *[MaxHLSRenditions]GeneratedSegmentBounds, raw *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			raw[1].InitializationSHA256[0]++
		},
		"unpaired media bytes": func(_ *[MaxHLSRenditions]HLSMuxClock, _ *[MaxHLSRenditions]GeneratedSegmentBounds, raw *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			raw[1].SegmentSHA256[0]++
		},
		"local tfdt": func(_ *[MaxHLSRenditions]HLSMuxClock, _ *[MaxHLSRenditions]GeneratedSegmentBounds, raw *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			raw[1].Fragments[0].DecodeUnits = 0
		},
		"missing sibling": func(_ *[MaxHLSRenditions]HLSMuxClock, _ *[MaxHLSRenditions]GeneratedSegmentBounds, raw *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			raw[1] = GeneratedFMP4NativeClock{}
		},
		"unconsumed sample": func(_ *[MaxHLSRenditions]HLSMuxClock, _ *[MaxHLSRenditions]GeneratedSegmentBounds, raw *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			raw[1].Fragments[0].SampleCount--
		},
		"unused native fragment": func(_ *[MaxHLSRenditions]HLSMuxClock, _ *[MaxHLSRenditions]GeneratedSegmentBounds, raw *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			raw[1].Fragments[1] = raw[1].Fragments[0]
		},
		"unused rendition": func(_ *[MaxHLSRenditions]HLSMuxClock, _ *[MaxHLSRenditions]GeneratedSegmentBounds, raw *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			raw[2] = raw[1]
		},
		"audio packet": func(_ *[MaxHLSRenditions]HLSMuxClock, output *[MaxHLSRenditions]GeneratedSegmentBounds, _ *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			output[1].Audio.Present = true
		},
		"middle packet gap": func(_ *[MaxHLSRenditions]HLSMuxClock, output *[MaxHLSRenditions]GeneratedSegmentBounds, _ *[MaxHLSRenditions]GeneratedFMP4NativeClock) {
			output[1].Video.TotalDecodeGapTicks = 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			plan, mux, outputs, native := generatedFMP4EmissionFixture(2)
			mutate(&mux, &outputs, &native)
			proof, err := ValidateGeneratedFMP4NativeEmission(plan, mux, outputs, native)
			if !errors.Is(err, ErrInvalidTimeline) || proof != (GeneratedFMP4NativeEmission{}) {
				t.Fatal("incomplete or differently timed output acquired a native emission proof")
			}
		})
	}
}

func TestGeneratedFMP4EmissionEligibilityAndCommandStaySeparateFromTS(t *testing.T) {
	plan, _, _, _ := generatedFMP4EmissionFixture(0)
	for _, rate := range []float64{23, 23.976, 30, 60} {
		other := plan
		other.FrameRate = rate
		if GeneratedWindowFMP4NativeClockEligible(other) || ValidatePlan(other) == nil {
			t.Fatal("uncalibrated source/output frame rate acquired version two")
		}
	}
	args, err := BuildArgs(plan, 1)
	if err != nil {
		t.Fatal(err)
	}
	find := func(name, value string) bool {
		for index := 0; index+1 < len(args); index++ {
			if args[index] == name && args[index+1] == value {
				return true
			}
		}
		return false
	}
	if !find("-output_ts_offset", "90.0000000") || !find("-avoid_negative_ts", "disabled") ||
		!find("-hls_segment_options", "use_editlist=0:avoid_negative_ts=disabled:movflags=+frag_discont") {
		t.Fatal("version-two production command differs from its observed emission contract")
	}
	plan.HLS.SegmentType, plan.Container, plan.HLS.Window.NativeClockVersion = "mpegts", "ts", GeneratedWindowNativeClockV1
	if !GeneratedWindowNativeClockEligible(plan) || GeneratedWindowFMP4NativeClockEligible(plan) || ValidatePlan(plan) != nil {
		t.Fatal("version-two dispatch changed the established TS eligibility")
	}
}

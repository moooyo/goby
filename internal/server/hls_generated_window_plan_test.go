package server

import (
	"errors"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func generatedWindowGraphPlanFixture() (transcode.Plan, media.Info, transcode.GeneratedSourceEndpointCertificate) {
	plan := transcode.Plan{Container: "ts", VideoStreamIndex: 0, AudioStreamIndex: -1, VideoCodec: "h264",
		Width: 160, Height: 96, FrameRate: 24, VideoBitrate: 256000, SegmentSeconds: 6,
		DurationTicks: 100 * media.TicksPerSecond, HLS: transcode.HLSPlan{SegmentType: "mpegts"}}
	info := media.Info{FormatStartKnown: true, FormatStartTicks: 2 * media.TicksPerSecond, DurationTicks: plan.DurationTicks,
		Streams: []media.Stream{{Index: 0, CodecType: "video", Codec: "h264"}}}
	certificate := transcode.GeneratedSourceEndpointCertificate{SourceIdentity: strings.Repeat("a", 64), StreamIndex: 0, TrackID: 1,
		SampleCount: 2400, FrameDuration: transcode.GeneratedRational{Num: 1, Den: 24}, Origin: transcode.GeneratedRational{Num: 2, Den: 1},
		Last: transcode.GeneratedRational{Num: 2447, Den: 24}, End: transcode.GeneratedRational{Num: 102, Den: 1},
		DurationTicks: plan.DurationTicks, DurationTicksExact: true, MetadataSHA256: [32]byte{1}, SampleExtentsSHA256: [32]byte{2}}
	return plan, info, certificate
}

func TestGeneratedWindowGraphPlanRetainsFullSourceClockAndExactTail(t *testing.T) {
	plan, info, certificate := generatedWindowGraphPlanFixture()
	timeline, err := hlsGeneratedWindowTimeline(plan, info, certificate)
	if err != nil || len(timeline.Segments) != 17 || timeline.TargetDuration != 6 {
		t.Fatalf("certified source graph: %+v, %v", timeline, err)
	}
	slot, found := timeline.SegmentAt(90 * media.TicksPerSecond)
	if !found || slot.Number != 15 || slot.StartTicks != 90*media.TicksPerSecond || slot.DurationTicks != 6*media.TicksPerSecond {
		t.Fatalf("seek lost cumulative source time: %+v, %v", slot, found)
	}
	window, err := hlsGeneratedWindowSlotPlan(plan, timeline, slot.Number)
	if err != nil || window.DurationTicks != plan.DurationTicks || window.StartTicks != slot.StartTicks ||
		window.HLS.Window.EndTicks != 96*media.TicksPerSecond || window.HLS.Window.StartNumber != 15 || !window.HLS.Window.RequireInputEvidence ||
		window.HLS.Window.NativeClockVersion != transcode.GeneratedWindowNativeClockV1 {
		t.Fatalf("source slot became a rebased whole-film producer: %+v, %v", window, err)
	}
	tail, err := hlsGeneratedWindowSlotPlan(plan, timeline, 16)
	if err != nil || tail.StartTicks != 96*media.TicksPerSecond || tail.HLS.Window.EndTicks != 100*media.TicksPerSecond {
		t.Fatalf("sample-set endpoint was replaced by a nominal full segment: %+v, %v", tail, err)
	}
	if _, found := timeline.SegmentAt(100 * media.TicksPerSecond); found {
		t.Fatal("the exclusive certified endpoint became a media slot")
	}
}

func TestGeneratedWindowGraphNativeClockRejectsUnsupportedMovieBeforePublication(t *testing.T) {
	for _, mode := range []string{"nonintegral TS cadence", "half-wrap source duration"} {
		plan, info, certificate := generatedWindowGraphPlanFixture()
		switch mode {
		case "nonintegral TS cadence":
			plan.FrameRate = 31
			certificate.FrameDuration = transcode.GeneratedRational{Num: 1, Den: 31}
			certificate.SampleCount = 3100
			certificate.Last = transcode.GeneratedRational{Num: 3161, Den: 31}
		case "half-wrap source duration":
			plan.FrameRate = 1
			plan.DurationTicks, info.DurationTicks, certificate.DurationTicks = 50_000*media.TicksPerSecond, 50_000*media.TicksPerSecond, 50_000*media.TicksPerSecond
			certificate.SampleCount = 50_000
			certificate.FrameDuration = transcode.GeneratedRational{Num: 1, Den: 1}
			certificate.Last, certificate.End = transcode.GeneratedRational{Num: 50_001, Den: 1}, transcode.GeneratedRational{Num: 50_002, Den: 1}
		}
		if _, err := hlsGeneratedWindowTimeline(plan, info, certificate); !errors.Is(err, transcode.ErrUnsupportedTimeline) {
			t.Fatalf("a future unsupported source clock was advertised before proof: mode=%s error=%v", mode, err)
		}
	}
}

func TestGeneratedWindowGraphRecognizesOnlyProvedAbsoluteFormatDuration(t *testing.T) {
	plan, info, certificate := generatedWindowGraphPlanFixture()
	plan.DurationTicks, info.DurationTicks = 102*media.TicksPerSecond, 102*media.TicksPerSecond
	timeline, err := hlsGeneratedWindowTimeline(plan, info, certificate)
	if err != nil {
		t.Fatal(err)
	}
	base := hlsGeneratedWindowProductionBase(plan, certificate)
	window, err := hlsGeneratedWindowSlotPlan(base, timeline, 15)
	if err != nil || plan.DurationTicks != 102*media.TicksPerSecond || info.DurationTicks != plan.DurationTicks ||
		base.DurationTicks != 100*media.TicksPerSecond || window.StartTicks != 90*media.TicksPerSecond || window.HLS.Window.EndTicks != 96*media.TicksPerSecond {
		t.Fatalf("native source duration rewrote catalog metadata or manufactured its endpoint: base=%+v, window=%+v, %v", base, window, err)
	}
	plan.DurationTicks, info.DurationTicks = 101*media.TicksPerSecond, 101*media.TicksPerSecond
	if _, err := hlsGeneratedWindowTimeline(plan, info, certificate); !errors.Is(err, transcode.ErrUnsupportedTimeline) {
		t.Fatal("an arbitrary metadata offset was treated as a proved duration convention")
	}
}

func TestGeneratedWindowGraphAdaptiveSlotsShareSourceCutsAndVariantNames(t *testing.T) {
	plan, info, certificate := generatedWindowGraphPlanFixture()
	plan.HLS.RenditionCount = 2
	plan.HLS.Renditions[0] = transcode.HLSRendition{Width: 160, Height: 96, VideoBitrate: 256000}
	plan.HLS.Renditions[1] = transcode.HLSRendition{Width: 80, Height: 48, VideoBitrate: 96000}
	timeline, err := hlsGeneratedWindowTimeline(plan, info, certificate)
	if err != nil {
		t.Fatal(err)
	}
	window, err := hlsGeneratedWindowSlotPlan(plan, timeline, 15)
	if err != nil || window.HLS.RenditionCount != 2 || window.StartTicks != 90*media.TicksPerSecond {
		t.Fatalf("adaptive source slot did not retain one output graph: %+v, %v", window, err)
	}
	for variant := 0; variant < 2; variant++ {
		name := hlsGeneratedWindowVariantName(15, variant, 2)
		number, parsedVariant, valid := hlsGeneratedWindowVariantNumber(name, 2)
		if !valid || number != 15 || parsedVariant != variant {
			t.Fatalf("variant admission mixed source slots: %q, %d, %d, %v", name, number, parsedVariant, valid)
		}
	}
	if _, _, valid := hlsGeneratedWindowVariantNumber("window-segment-000015.ts", 2); valid {
		t.Fatal("unqualified admission bypassed the adaptive variant binding")
	}
}

func TestGeneratedWindowGraphPlanRejectsMetadataOnlyOrUnsupportedCertificates(t *testing.T) {
	for name, mutate := range map[string]func(*transcode.Plan, *media.Info, *transcode.GeneratedSourceEndpointCertificate){
		"absent table evidence": func(_ *transcode.Plan, _ *media.Info, certificate *transcode.GeneratedSourceEndpointCertificate) {
			certificate.MetadataSHA256 = [32]byte{}
		},
		"absent extents": func(_ *transcode.Plan, _ *media.Info, certificate *transcode.GeneratedSourceEndpointCertificate) {
			certificate.SampleExtentsSHA256 = [32]byte{}
		},
		"metadata endpoint differs": func(_ *transcode.Plan, info *media.Info, _ *transcode.GeneratedSourceEndpointCertificate) {
			info.DurationTicks++
		},
		"sample count differs": func(_ *transcode.Plan, _ *media.Info, certificate *transcode.GeneratedSourceEndpointCertificate) {
			certificate.SampleCount++
		},
		"sample period differs": func(_ *transcode.Plan, _ *media.Info, certificate *transcode.GeneratedSourceEndpointCertificate) {
			certificate.FrameDuration.Den = 25
		},
		"unknown origin": func(_ *transcode.Plan, info *media.Info, _ *transcode.GeneratedSourceEndpointCertificate) {
			info.FormatStartKnown = false
		},
		"different origin": func(_ *transcode.Plan, info *media.Info, _ *transcode.GeneratedSourceEndpointCertificate) {
			info.FormatStartTicks++
		},
		"rounded duration": func(_ *transcode.Plan, _ *media.Info, certificate *transcode.GeneratedSourceEndpointCertificate) {
			certificate.DurationTicksExact = false
		},
		"fractional rate": func(plan *transcode.Plan, _ *media.Info, _ *transcode.GeneratedSourceEndpointCertificate) {
			plan.FrameRate = 30000.0 / 1001.0
		},
		"uncalibrated fMP4 cadence": func(plan *transcode.Plan, _ *media.Info, certificate *transcode.GeneratedSourceEndpointCertificate) {
			plan.Container, plan.HLS.SegmentType = "mp4", "fmp4"
			plan.FrameRate = 30
			certificate.FrameDuration = transcode.GeneratedRational{Num: 1, Den: 30}
			certificate.SampleCount = 3000
			certificate.Last = transcode.GeneratedRational{Num: 3059, Den: 30}
		},
		"audio priming": func(plan *transcode.Plan, _ *media.Info, _ *transcode.GeneratedSourceEndpointCertificate) {
			plan.AudioStreamIndex, plan.AudioCodec = 1, "aac"
		},
		"external source": func(_ *transcode.Plan, info *media.Info, _ *transcode.GeneratedSourceEndpointCertificate) {
			info.Streams[0].IsExternal = true
		},
		"unbound source identity": func(_ *transcode.Plan, _ *media.Info, certificate *transcode.GeneratedSourceEndpointCertificate) {
			certificate.SourceIdentity = "metadata-path"
		},
	} {
		t.Run(name, func(t *testing.T) {
			plan, info, certificate := generatedWindowGraphPlanFixture()
			mutate(&plan, &info, &certificate)
			if _, err := hlsGeneratedWindowTimeline(plan, info, certificate); !errors.Is(err, transcode.ErrUnsupportedTimeline) {
				t.Fatalf("unsupported graph replaced the complete-source path: %v", err)
			}
		})
	}
}

func TestGeneratedWindowGraphLogicalNamesKeepAdmissionSeparateFromExactBytes(t *testing.T) {
	for _, number := range []int{0, 15, transcode.MaxPlaylistSegments - 1} {
		name := hlsGeneratedWindowLogicalName(number)
		parsed, valid := hlsGeneratedWindowLogicalNumber(name)
		if !valid || parsed != number {
			t.Fatalf("canonical logical admission URI: %q, %d, %v", name, parsed, valid)
		}
		if _, valid := transcode.HLSArtifact(name); valid {
			t.Fatalf("logical admission URI also names exact job bytes: %q", name)
		}
	}
	for _, name := range []string{"window-segment-15.ts", "window-segment-+00015.ts", "window-segment-016384.ts", "window-segment-000015.m4s", "segment-000015.ts", "../window-segment-000015.ts"} {
		if _, valid := hlsGeneratedWindowLogicalNumber(name); valid {
			t.Fatalf("noncanonical admission URI was accepted: %q", name)
		}
	}
}

func TestGeneratedWindowGraphSlotPlanRejectsForgedIntervals(t *testing.T) {
	for name, mutate := range map[string]func(*transcode.Plan, *transcode.Timeline){
		"offset base":  func(plan *transcode.Plan, _ *transcode.Timeline) { plan.StartTicks = 6 * media.TicksPerSecond },
		"invalid base": func(plan *transcode.Plan, _ *transcode.Timeline) { plan.SegmentSeconds = 0 },
		"oversized slot": func(_ *transcode.Plan, timeline *transcode.Timeline) {
			timeline.Segments[0].DurationTicks = 12 * media.TicksPerSecond
		},
		"offgrid slot": func(_ *transcode.Plan, timeline *transcode.Timeline) { timeline.Segments[0].DurationTicks-- },
		"shifted slot": func(_ *transcode.Plan, timeline *transcode.Timeline) {
			timeline.Segments[0].StartTicks = 6 * media.TicksPerSecond
		},
		"wrong number": func(_ *transcode.Plan, timeline *transcode.Timeline) { timeline.Segments[0].Number = 1 },
		"missing tail": func(_ *transcode.Plan, timeline *transcode.Timeline) {
			timeline.Segments = timeline.Segments[:len(timeline.Segments)-1]
		},
		"changed target": func(_ *transcode.Plan, timeline *transcode.Timeline) { timeline.TargetDuration++ },
	} {
		t.Run(name, func(t *testing.T) {
			plan, info, certificate := generatedWindowGraphPlanFixture()
			timeline, err := hlsGeneratedWindowTimeline(plan, info, certificate)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&plan, &timeline)
			if _, err := hlsGeneratedWindowSlotPlan(plan, timeline, 0); !errors.Is(err, transcode.ErrInvalidTimeline) {
				t.Fatalf("forged source interval reached a producer: %v", err)
			}
		})
	}
}

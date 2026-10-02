package server

import (
	"errors"
	"fmt"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func generatedWindowFMP4GraphPlanFixture(rate, count int) (transcode.Plan, media.Info, transcode.GeneratedSourceEndpointCertificate) {
	plan, info, endpoint := generatedWindowGraphPlanFixture()
	plan.Container, plan.HLS.SegmentType, plan.FrameRate = "mp4", "fmp4", float64(rate)
	plan.HLS.RenditionCount = count
	for variant := 0; variant < count; variant++ {
		plan.HLS.Renditions[variant] = transcode.HLSRendition{Width: 160 >> variant, Height: 96 >> variant, VideoBitrate: int64(max(64000, 256000>>variant))}
	}
	endpoint.SampleCount = 100 * int64(rate)
	endpoint.FrameDuration = transcode.GeneratedRational{Num: 1, Den: int64(rate)}
	endpoint.Last = transcode.GeneratedRational{Num: 102*int64(rate) - 1, Den: int64(rate)}
	return plan, info, endpoint
}

func TestGeneratedWindowFMP4GraphPlanPreservesEveryCertifiedSourceSlot(t *testing.T) {
	for _, rate := range []int{24, 25} {
		for _, count := range []int{0, 2} {
			t.Run(fmt.Sprintf("rate%d-renditions%d", rate, count), func(t *testing.T) {
				plan, info, endpoint := generatedWindowFMP4GraphPlanFixture(rate, count)
				originalPlan, originalEndpoint := plan, endpoint
				timeline, err := hlsGeneratedWindowTimeline(plan, info, endpoint)
				if err != nil || len(timeline.Segments) != 17 || timeline.TargetDuration != 6 {
					t.Fatalf("certified fMP4 timeline rejected: %+v, %v", timeline, err)
				}
				for number, slot := range timeline.Segments {
					window, err := hlsGeneratedWindowSlotPlan(plan, timeline, number)
					if err != nil || window.DurationTicks != 100*media.TicksPerSecond || window.StartTicks != slot.StartTicks ||
						window.HLS.Window.EndTicks != slot.StartTicks+slot.DurationTicks || window.HLS.Window.StartNumber != number ||
						!window.HLS.Window.RequireInputEvidence || window.HLS.Window.NativeClockVersion != transcode.GeneratedWindowNativeClockV2 ||
						!transcode.GeneratedWindowFMP4NativeClockEligible(window) || transcode.GeneratedWindowNativeClockEligible(window) ||
						window.HLS.RenditionCount != count || window.HLS.Renditions != plan.HLS.Renditions || transcode.ValidatePlan(window) != nil {
						t.Fatalf("slot %d lost its fMP4 source clock or adaptive outputs: %+v, %v", number, window, err)
					}
				}
				for _, start := range []int64{0, 6, 90, 96} {
					slot, found := timeline.SegmentAt(start * media.TicksPerSecond)
					if !found || slot.StartTicks != start*media.TicksPerSecond || slot.Number != int(start/6) {
						t.Fatalf("source-global slot %d was rebased: %+v, %v", start, slot, found)
					}
				}
				if timeline.Segments[16].DurationTicks != 4*media.TicksPerSecond || plan != originalPlan || endpoint != originalEndpoint ||
					info.FormatStartTicks != 2*media.TicksPerSecond || !info.FormatStartKnown {
					t.Fatal("planning rewrote the independently certified source origin or exact tail")
				}
				if _, found := timeline.SegmentAt(endpoint.DurationTicks); found {
					t.Fatal("the certified exclusive endpoint became a media slot")
				}
			})
		}
	}
}

func TestGeneratedWindowFMP4GraphPlanChecksFutureTailWithoutTSWrapLimits(t *testing.T) {
	plan, info, endpoint := generatedWindowFMP4GraphPlanFixture(24, 0)
	plan.DurationTicks, info.DurationTicks, endpoint.DurationTicks = 50000*media.TicksPerSecond, 50000*media.TicksPerSecond, 50000*media.TicksPerSecond
	endpoint.SampleCount = 50000 * 24
	endpoint.Last = transcode.GeneratedRational{Num: 50002*24 - 1, Den: 24}
	endpoint.End = transcode.GeneratedRational{Num: 50002, Den: 1}
	timeline, err := hlsGeneratedWindowTimeline(plan, info, endpoint)
	if err != nil || len(timeline.Segments) != 8334 {
		t.Fatalf("fMP4 inherited a TS-only half-wrap restriction: %+v, %v", timeline, err)
	}
	for number := range timeline.Segments {
		window, err := hlsGeneratedWindowSlotPlan(plan, timeline, number)
		if err != nil || window.HLS.Window.NativeClockVersion != transcode.GeneratedWindowNativeClockV2 {
			t.Fatalf("advertised future slot %d lacks a calibrated fMP4 clock: %+v, %v", number, window, err)
		}
	}
	tail := timeline.Segments[len(timeline.Segments)-1]
	if tail.StartTicks != 49998*media.TicksPerSecond || tail.DurationTicks != 2*media.TicksPerSecond {
		t.Fatalf("future tail lost its sample-set endpoint: %+v", tail)
	}
}

func TestGeneratedWindowFMP4GraphPlanRetainsExactFractionalSampleTail(t *testing.T) {
	plan, info, endpoint := generatedWindowFMP4GraphPlanFixture(24, 2)
	plan.DurationTicks += media.TicksPerSecond / 8
	info.DurationTicks, endpoint.DurationTicks = plan.DurationTicks, plan.DurationTicks
	endpoint.SampleCount = 2403
	endpoint.Last = transcode.GeneratedRational{Num: 2450, Den: 24}
	endpoint.End = transcode.GeneratedRational{Num: 2451, Den: 24}
	timeline, err := hlsGeneratedWindowTimeline(plan, info, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	tail, err := hlsGeneratedWindowSlotPlan(plan, timeline, 16)
	if err != nil || tail.StartTicks != 96*media.TicksPerSecond || tail.HLS.Window.EndTicks != plan.DurationTicks ||
		timeline.Segments[16].DurationTicks != 4*media.TicksPerSecond+media.TicksPerSecond/8 {
		t.Fatalf("exact sample tail was rounded to metadata or nominal duration: %+v, %v", tail, err)
	}
}

func TestGeneratedWindowFMP4GraphPlanRejectsUncalibratedClockAndMetadataTail(t *testing.T) {
	for _, rate := range []int{1, 23, 30, 60} {
		plan, info, endpoint := generatedWindowFMP4GraphPlanFixture(rate, 0)
		if _, err := hlsGeneratedWindowTimeline(plan, info, endpoint); !errors.Is(err, transcode.ErrUnsupportedTimeline) {
			t.Fatalf("uncalibrated fMP4 rate %d replaced the complete-source path: %v", rate, err)
		}
	}
	for _, count := range []int{3, transcode.MaxHLSRenditions} {
		plan, info, endpoint := generatedWindowFMP4GraphPlanFixture(24, count)
		if _, err := hlsGeneratedWindowTimeline(plan, info, endpoint); !errors.Is(err, transcode.ErrUnsupportedTimeline) {
			t.Fatalf("uncalibrated fMP4 rendition count %d acquired a graph: %v", count, err)
		}
	}
	for name, mutate := range map[string]func(*transcode.Plan, *media.Info, *transcode.GeneratedSourceEndpointCertificate){
		"fractional rate": func(plan *transcode.Plan, _ *media.Info, _ *transcode.GeneratedSourceEndpointCertificate) {
			plan.FrameRate = 24000.0 / 1001.0
		},
		"metadata-only endpoint": func(_ *transcode.Plan, _ *media.Info, endpoint *transcode.GeneratedSourceEndpointCertificate) {
			endpoint.MetadataSHA256, endpoint.SampleExtentsSHA256 = [32]byte{}, [32]byte{}
		},
		"rounded future sample": func(plan *transcode.Plan, info *media.Info, endpoint *transcode.GeneratedSourceEndpointCertificate) {
			plan.DurationTicks += media.TicksPerSecond / 24
			info.DurationTicks, endpoint.DurationTicks = plan.DurationTicks, plan.DurationTicks
			endpoint.SampleCount++
			endpoint.Last = transcode.GeneratedRational{Num: 2448, Den: 24}
			endpoint.End = transcode.GeneratedRational{Num: 2449, Den: 24}
		},
		"missing last sample": func(_ *transcode.Plan, _ *media.Info, endpoint *transcode.GeneratedSourceEndpointCertificate) {
			endpoint.Last = transcode.GeneratedRational{}
		},
		"arbitrary format duration": func(plan *transcode.Plan, info *media.Info, _ *transcode.GeneratedSourceEndpointCertificate) {
			plan.DurationTicks, info.DurationTicks = 101*media.TicksPerSecond, 101*media.TicksPerSecond
		},
	} {
		t.Run(name, func(t *testing.T) {
			plan, info, endpoint := generatedWindowFMP4GraphPlanFixture(24, 0)
			mutate(&plan, &info, &endpoint)
			if _, err := hlsGeneratedWindowTimeline(plan, info, endpoint); !errors.Is(err, transcode.ErrUnsupportedTimeline) {
				t.Fatalf("unsupported source evidence published an immutable fMP4 timeline: %v", err)
			}
		})
	}
}

func TestGeneratedWindowFMP4GraphSlotPlanRejectsUncalibratedCandidate(t *testing.T) {
	plan, info, endpoint := generatedWindowFMP4GraphPlanFixture(24, 0)
	timeline, err := hlsGeneratedWindowTimeline(plan, info, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	plan.FrameRate = 30
	for _, number := range []int{0, 1, 15, 16} {
		if _, err := hlsGeneratedWindowSlotPlan(plan, timeline, number); !errors.Is(err, transcode.ErrUnsupportedTimeline) {
			t.Fatalf("slot %d selected a generic or TS clock for unsupported fMP4: %v", number, err)
		}
	}
}

func TestGeneratedWindowFMP4GraphPlanUsesOnlyProvedAbsoluteFormatDuration(t *testing.T) {
	plan, info, endpoint := generatedWindowFMP4GraphPlanFixture(25, 2)
	plan.DurationTicks, info.DurationTicks = 102*media.TicksPerSecond, 102*media.TicksPerSecond
	timeline, err := hlsGeneratedWindowTimeline(plan, info, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	base := hlsGeneratedWindowProductionBase(plan, endpoint)
	window, err := hlsGeneratedWindowSlotPlan(base, timeline, 15)
	if err != nil || base.DurationTicks != 100*media.TicksPerSecond || plan.DurationTicks != 102*media.TicksPerSecond ||
		info.DurationTicks != plan.DurationTicks || endpoint.Origin != (transcode.GeneratedRational{Num: 2, Den: 1}) ||
		window.StartTicks != 90*media.TicksPerSecond || window.HLS.Window.EndTicks != 96*media.TicksPerSecond ||
		window.HLS.Window.NativeClockVersion != transcode.GeneratedWindowNativeClockV2 {
		t.Fatalf("fMP4 used metadata as EOF or rebased the proved source origin: base=%+v, window=%+v, %v", base, window, err)
	}
}

func TestGeneratedWindowFMP4DispatchKeepsEstablishedTSClockEligibility(t *testing.T) {
	plan, info, endpoint := generatedWindowFMP4GraphPlanFixture(30, 3)
	plan.Container, plan.HLS.SegmentType = "ts", "mpegts"
	timeline, err := hlsGeneratedWindowTimeline(plan, info, endpoint)
	if err != nil {
		t.Fatal("V2 admission restrictions narrowed the established TS graph", err)
	}
	for _, number := range []int{0, 1, 15, 16} {
		window, err := hlsGeneratedWindowSlotPlan(plan, timeline, number)
		if err != nil || window.HLS.Window.NativeClockVersion != transcode.GeneratedWindowNativeClockV1 ||
			!transcode.GeneratedWindowNativeClockEligible(window) || window.HLS.RenditionCount != 3 {
			t.Fatalf("TS slot %d inherited V2 frame-rate or rendition limits: %+v, %v", number, window, err)
		}
	}
}

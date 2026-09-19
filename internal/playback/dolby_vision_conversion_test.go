package playback

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func dolbyVisionSource(profile, compatibility int) media.Stream {
	return media.Stream{Index: 0, Codec: "hevc", CodecType: "video", BitDepth: 10, PixelFormat: "yuv420p10le", InterlaceKnown: true,
		VideoRange: "DOVI", VideoRangeKnown: true, DolbyVision: &media.DolbyVisionMetadata{Profile: profile, Level: 6, RPUPresent: true,
			BLPresent: true, ELPresent: profile == 7, CompatibilityID: compatibility, RPUVerified: true, ResidualDisabled: profile != 7, RPUProfile: profile, RPUFrameCount: 48}}
}

func TestDolbyVisionPlansVerifiedProfilesAndPreservesCPUDecodeMetadata(t *testing.T) {
	for _, profile := range [][2]int{{5, 0}, {7, 6}, {8, 1}, {8, 2}, {8, 4}} {
		for _, outputRange := range []string{"SDR", "HDR10"} {
			source := dolbyVisionSource(profile[0], profile[1])
			before := *source.DolbyVision
			filters, reason := videoProcessingPlanForRange(source, outputRange)
			if reason != nil || filters.ToneMap != "dolbyvision" || filters.DVProfile != profile[0] || filters.Backend != "vulkan" {
				t.Fatalf("verified profile declined before strict rendering: %+v, %+v", filters, reason)
			}
			plan := transcode.Plan{VideoCodec: "hevc", VideoBitDepth: 10, VideoFilters: filters, Hardware: transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD129"}}
			selectVideoProcessingHardware(&plan)
			if plan.Hardware.Decode != "software" || plan.Hardware.Encode != "vaapi" || plan.Hardware.Device != "/dev/dri/renderD129" {
				t.Fatalf("RPU metadata or configured adapter was lost: %+v", plan)
			}
			output := source
			videoEncodedOutput(&output, plan)
			if output.VideoRange != outputRange || !output.VideoRangeKnown || output.DolbyVision != nil || output.BitDepth != 10 || output.PixelFormat != "yuv420p10le" {
				t.Fatalf("output retained source DV facts or lost requested bit depth: %+v", output)
			}
			if !reflect.DeepEqual(*source.DolbyVision, before) {
				t.Fatal("projection changed the original RPU evidence")
			}
		}
	}
}

func TestDolbyVisionRejectsMissingRPUAndContradictoryLayers(t *testing.T) {
	for _, change := range []func(*media.Stream){
		func(s *media.Stream) { s.DolbyVision = nil },
		func(s *media.Stream) { s.DolbyVision.RPUVerified = false },
		func(s *media.Stream) { s.DolbyVision.ResidualDisabled = true },
		func(s *media.Stream) { s.DolbyVision.RPUProfile = 8 },
		func(s *media.Stream) { s.DolbyVision.RPUResidualMixed = true },
		func(s *media.Stream) { s.DolbyVision.RPUFrameCount = 0 },
		func(s *media.Stream) { s.DolbyVision.RPUPresent = false },
		func(s *media.Stream) { s.DolbyVision.BLPresent = false },
		func(s *media.Stream) { s.DolbyVision.Profile = 4 },
		func(s *media.Stream) {
			s.DolbyVision.Profile, s.DolbyVision.CompatibilityID, s.DolbyVision.ELPresent = 8, 1, true
		},
		func(s *media.Stream) { s.DolbyVision.Profile, s.DolbyVision.CompatibilityID = 5, 6 },
		func(s *media.Stream) { s.Codec = "av1" },
		func(s *media.Stream) { s.BitDepth, s.PixelFormat = 8, "yuv420p" },
	} {
		source := dolbyVisionSource(7, 6)
		change(&source)
		if _, reason := videoProcessingPlan(source); reason == nil {
			t.Fatalf("unverified or contradictory Dolby Vision accepted: %+v", source)
		}
	}
}

func TestDolbyVisionSoftwareEncodingRetainsExplicitAMDProcessingDevice(t *testing.T) {
	filters, reason := videoProcessingPlan(dolbyVisionSource(5, 0))
	if reason != nil {
		t.Fatal(reason)
	}
	plan := transcode.Plan{VideoFilters: filters, Hardware: transcode.Hardware{Decode: "vaapi", Encode: "software", Device: "/dev/dri/renderD129"}}
	selectVideoProcessingHardware(&plan)
	plan.Subtitle.Mode = "burn"
	selectVideoProcessingHardware(&plan)
	if plan.Hardware.Decode != "software" || plan.Hardware.Encode != "software" || plan.Hardware.Device != "/dev/dri/renderD129" || plan.VideoFilters.Backend != "vulkan" {
		t.Fatalf("composition changed encoder selection or discarded the adapter: %+v", plan)
	}
}

func TestDolbyVisionUsesDecodedDepthWhenRawSampleDepthIsAbsent(t *testing.T) {
	for _, profile := range [][2]int{{5, 0}, {7, 6}, {8, 1}, {8, 2}, {8, 4}} {
		source := dolbyVisionSource(profile[0], profile[1])
		source.BitDepth = 0
		filters, reason := videoProcessingPlanForRange(source, "HDR10")
		if reason != nil || filters.SourceBitDepth != 10 || filters.ToneMap != "dolbyvision" || filters.DVProfile != profile[0] {
			t.Fatalf("explicit decoded 10-bit pixels were treated as unknown: %+v, %+v", filters, reason)
		}
		if source.BitDepth != 0 || source.PixelFormat != "yuv420p10le" {
			t.Fatal("planning changed the reported source depth or decoded format")
		}
		for _, depth := range []int{8, 12} {
			conflicting := source
			conflicting.BitDepth = depth
			if _, reason := videoProcessingPlanForRange(conflicting, "HDR10"); reason == nil {
				t.Fatalf("reported depth %d contradicted decoded 10-bit pixels without rejection", depth)
			}
		}
		conflicting := source
		conflicting.BitDepth, conflicting.PixelFormat = 10, "yuv420p"
		if _, reason := videoProcessingPlanForRange(conflicting, "HDR10"); reason == nil {
			t.Fatal("reported 10-bit depth overrode decoded 8-bit pixels")
		}
	}
}

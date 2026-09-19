package playback

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestProgressiveVideoHDRToSDRUsesAMDAndProjectsActualColor(t *testing.T) {
	for _, transfer := range []string{"smpte2084", "arib-std-b67"} {
		t.Run(transfer, func(t *testing.T) {
			source := progressiveVideoTestSource()
			video := &source.Info.Streams[0]
			video.Codec, video.PixelFormat, video.BitDepth = "hevc", "yuv420p10le", 10
			video.VideoRange, video.VideoRangeKnown = "", false
			video.ColorTransfer, video.ColorPrimaries, video.ColorSpace, video.ColorRange = transfer, "bt2020", "bt2020nc", "tv"
			before := *video
			limits := conversionTestLimits()
			limits.Hardware = transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD128"}
			request := progressiveVideoTestRequest()
			request.MaxWidth, request.MaxHeight = profileTestPtr(640), profileTestPtr(360)
			decision := progressiveVideoTestPlan(t, source, request, limits)
			output := decision.OutputSource.Info.Streams[0]
			if decision.Method != "Transcode" || decision.Plan.VideoCodec != "h264" || decision.Plan.VideoFilters.SourceTransfer != transfer || decision.Plan.VideoFilters.Backend != "vulkan" || decision.Plan.Hardware != limits.Hardware {
				t.Fatalf("HDR conversion did not select the configured AMD path: %+v", decision.Plan)
			}
			if output.VideoRange != "SDR" || !output.VideoRangeKnown || output.ColorTransfer != "bt709" || output.ColorPrimaries != "bt709" || output.ColorSpace != "bt709" || output.ColorRange != "tv" || output.BitDepth != 8 || output.Width > 640 || output.Height > 360 {
				t.Fatalf("SDR projection does not describe the pixel transform: %+v", output)
			}
			if !reflect.DeepEqual(before, *video) {
				t.Fatal("conversion changed original HDR source facts")
			}
			limits.AllowVideoTranscode = false
			progressiveVideoTestDeclined(t, source, request, limits)
		})
	}
}

func TestProgressiveVideoDeinterlaceUsesDisplayFieldOrder(t *testing.T) {
	for _, test := range []struct{ field, parity string }{{"tt", "tff"}, {"bt", "tff"}, {"bb", "bff"}, {"tb", "bff"}, {"unknown", "auto"}} {
		t.Run(test.field, func(t *testing.T) {
			source := progressiveVideoTestSource()
			source.Info.Streams[0].IsInterlaced, source.Info.Streams[0].InterlaceKnown, source.Info.Streams[0].FieldOrder = true, true, test.field
			decision := progressiveVideoTestPlan(t, source, progressiveVideoTestRequest(), conversionTestLimits())
			video := decision.OutputSource.Info.Streams[0]
			if decision.Plan.VideoFilters.Deinterlace != test.parity || decision.Plan.VideoCodec != "h264" || decision.Plan.FrameRate != 0 || !video.InterlaceKnown || video.IsInterlaced || video.FieldOrder != "progressive" {
				t.Fatalf("field order or frame cadence was not retained: %+v, %+v", decision.Plan, video)
			}
		})
	}
}

func TestAMDDeinterlaceSelectsVulkanWithoutChangingCodecBackends(t *testing.T) {
	source := progressiveVideoTestSource()
	source.Info.Streams[0].IsInterlaced, source.Info.Streams[0].FieldOrder = true, "tt"
	limits := conversionTestLimits()
	limits.Hardware = transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD128"}
	decision := progressiveVideoTestPlan(t, source, progressiveVideoTestRequest(), limits)
	if decision.Plan.VideoFilters.Backend != "vulkan" || decision.Plan.VideoFilters.Deinterlace != "tff" || decision.Plan.Hardware != limits.Hardware {
		t.Fatalf("AMD processing selected unusable VPP or discarded codec acceleration: %+v", decision.Plan)
	}
}

func TestProgressiveVideoRejectsIncompleteAndConflictingHDRMetadata(t *testing.T) {
	for _, field := range []string{"primaries", "matrix", "range", "transfer", "declaration", "dolby-vision"} {
		t.Run(field, func(t *testing.T) {
			source := progressiveVideoTestSource()
			video := &source.Info.Streams[0]
			video.VideoRange, video.VideoRangeKnown = "HDR10", true
			video.ColorTransfer, video.ColorPrimaries, video.ColorSpace, video.ColorRange = "smpte2084", "bt2020", "bt2020nc", "tv"
			switch field {
			case "primaries":
				video.ColorPrimaries = ""
			case "matrix":
				video.ColorSpace = "bt709"
			case "range":
				video.ColorRange = "unknown"
			case "transfer":
				video.ColorTransfer = "bt709"
			case "declaration":
				video.VideoRange = "HLG"
			case "dolby-vision":
				video.VideoRange = "DOVI"
			}
			progressiveVideoTestDeclined(t, source, progressiveVideoTestRequest(), conversionTestLimits())
		})
	}
}

func TestProgressiveVideoUnknownInterlaceEncodingAppliesRealFilter(t *testing.T) {
	source := progressiveVideoTestSource()
	source.Info.Streams[0].InterlaceKnown = false
	request := progressiveVideoTestRequest()
	request.AllowInterlacedVideoStreamCopy = profileTestPtr(false)
	decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.VideoFilters.Deinterlace != "auto" || decision.Plan.Hardware != (transcode.Hardware{}) {
		t.Fatalf("unknown field order was declared progressive without deinterlacing: %+v", decision.Plan)
	}
}

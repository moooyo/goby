package playback

import (
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestHLSVideoProcessingProjectsSDRAfterRealSoftwareFilters(t *testing.T) {
	for _, container := range []string{"ts", "mp4"} {
		t.Run(container, func(t *testing.T) {
			source, request := conversionTestSource(), conversionTestRequest()
			video := &source.Info.Streams[0]
			video.Codec, video.BitDepth, video.PixelFormat = "hevc", 10, "yuv420p10le"
			video.IsInterlaced, video.InterlaceKnown, video.FieldOrder = true, true, "bb"
			video.VideoRange, video.VideoRangeKnown = "HDR10", true
			video.ColorTransfer, video.ColorPrimaries, video.ColorSpace, video.ColorRange = "smpte2084", "bt2020", "bt2020nc", "tv"
			request.DeviceProfile.TranscodingProfiles[0].Container = container
			limits := conversionTestLimits()
			limits.Hardware = transcode.Hardware{Decode: "vaapi", Encode: "vaapi"}
			decision := conversionTestPlan(t, source, request, limits)
			plan, output := decision.Plan, decision.OutputSource.Info.Streams[0]
			if plan.VideoCodec != "h264" || plan.VideoFilters.ToneMap != "hdr10" || plan.VideoFilters.Deinterlace != "bff" || plan.Hardware != (transcode.Hardware{}) {
				t.Fatalf("HLS did not select the software source transform: %+v", plan)
			}
			if output.VideoRange != "SDR" || !output.VideoRangeKnown || output.ColorTransfer != "bt709" || output.ColorPrimaries != "bt709" || output.ColorSpace != "bt709" || output.ColorRange != "tv" || output.IsInterlaced || !output.InterlaceKnown {
				t.Fatalf("HLS projected source HDR or interlace metadata after conversion: %+v", output)
			}
			limits.AllowVideoTranscode = false
			conversionTestDeclined(t, source, request, limits)
		})
	}
}

func TestHLSVideoInterlaceConversionAndFragmentedMP4AreConstructible(t *testing.T) {
	source, request := conversionTestSource(), conversionTestRequest()
	source.Info.Streams[0].IsInterlaced, source.Info.Streams[0].FieldOrder = true, "tt"
	request.AllowInterlacedVideoStreamCopy = profileTestPtr(true)
	decision := conversionTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.VideoCodec != "h264" || decision.Plan.VideoFilters.Deinterlace != "tff" || decision.OutputSource.Info.Streams[0].IsInterlaced {
		t.Fatalf("interlaced source did not receive deinterlacing: %+v", decision)
	}
	source = conversionTestSource()
	request.DeviceProfile.TranscodingProfiles[0].Container = "mp4"
	decision = conversionTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.Container != "mp4" || decision.Plan.HLS.SegmentType != "fmp4" || decision.Plan.VideoCodec != "copy" || decision.Plan.AudioCodec != "copy" {
		t.Fatalf("fragmented MP4 HLS did not preserve compatible stream copies: %+v", decision.Plan)
	}
}

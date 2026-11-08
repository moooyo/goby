package playback

import (
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestTranscodingHevcGateFallsThroughOfferedFormatsAndPreservesCopy(t *testing.T) {
	for _, protocol := range []string{"hls", "http"} {
		t.Run(protocol, func(t *testing.T) {
			container := "ts"
			if protocol == "http" {
				container = "mp4"
			}
			profile := videoProfilesTestProfile(protocol, container)
			profile.VideoCodec = "hevc,h264"
			request := videoProfilesTestRequest(profile)
			request.AllowVideoStreamCopy = profileTestPtr(false)
			limits := conversionTestLimits()
			limits.DisableHevcEncoding = true
			decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, limits, protocol, 0)
			if decision.Plan.VideoCodec != "h264" {
				t.Fatalf("disabled HEVC hid an offered H.264 encoder: %+v", decision)
			}
			profile.VideoCodec = "hevc"
			request = videoProfilesTestRequest(profile)
			request.AllowVideoStreamCopy = profileTestPtr(false)
			videoProfilesTestDeclined(t, videoProfilesTestSource(), request, limits)
		})
	}
	source := progressiveVideoTestSource()
	source.Info.Streams[0].Codec, source.Info.Streams[0].Profile = "hevc", "Main"
	request := progressiveVideoTestRequest()
	request.VideoCodec = "hevc"
	decision := progressiveVideoTestPlan(t, source, request, ConversionLimits{AllowRemux: true, DisableHevcEncoding: true})
	if decision.Plan.VideoCodec != "copy" || decision.Plan.VideoCopyCodec != "hevc" {
		t.Fatalf("HEVC admission gate removed a copied source: %+v", decision)
	}
}

func TestTranscodingHardwareGateDependsOnTheSelectedProcessingGraph(t *testing.T) {
	request := progressiveVideoTestRequest()
	request.AllowVideoStreamCopy = profileTestPtr(false)
	limits := conversionTestLimits()
	limits.Hardware = transcode.Hardware{Decode: "software", Encode: "vaapi"}
	limits.HardwareUnavailable, limits.DisableHardwareEncoding = true, true
	decision := progressiveVideoTestPlan(t, progressiveVideoTestSource(), request, limits)
	if decision.Plan.Hardware.Encode != "software" || conversionHardwareRequired(*decision.Plan) {
		t.Fatalf("disabling an unavailable hardware encoder did not permit independent CPU encoding: %+v", decision)
	}
	limits.Hardware.Decode = "vaapi"
	progressiveVideoTestDeclined(t, progressiveVideoTestSource(), request, limits)
	limits.Hardware = transcode.Hardware{Decode: "software", Encode: "vaapi", Device: "/dev/dri/renderD128"}
	limits.HardwareUnavailable = false
	source := progressiveVideoTestSource()
	source.Info.Streams[0].IsInterlaced, source.Info.Streams[0].FieldOrder = true, "tt"
	decision = progressiveVideoTestPlan(t, source, request, limits)
	if decision.Plan.Hardware.Encode != "software" || decision.Plan.Hardware.Device != limits.Hardware.Device || decision.Plan.VideoFilters.Backend != "vulkan" {
		t.Fatalf("encoder policy removed the independently required filtering device: %+v", decision)
	}
	limits.HardwareUnavailable = true
	progressiveVideoTestDeclined(t, source, request, limits)
}

func TestTranscodingDownmixCapturesOriginalChannelFactsAcrossPlanners(t *testing.T) {
	limits := conversionTestLimits()
	limits.Execution = transcode.DefaultExecutionOptions(2)
	limits.Execution.DownMixAudioBoost = 2
	limits.MaxAudioChannels = 2
	source, request := conversionTestSource(), conversionTestRequest()
	source.Info.Streams[1].Channels = 6
	request.AllowAudioStreamCopy = profileTestPtr(false)
	hls := conversionTestPlan(t, source, request, limits)
	source.Info.DurationTicks = 0
	dynamic, err := PlanDynamicConversion(source, request, limits)
	if err != nil || dynamic.Plan == nil {
		t.Fatalf("dynamic surround downmix was not constructible: %+v, %v", dynamic, err)
	}
	videoSource, videoRequest := progressiveVideoTestSource(), progressiveVideoTestRequest()
	videoSource.Info.Streams[1].Channels = 6
	videoRequest.AllowAudioStreamCopy = profileTestPtr(false)
	video := progressiveVideoTestPlan(t, videoSource, videoRequest, limits)
	audioSource := progressiveAudioTestSource()
	audioSource.Info.Streams[1].Channels = 6
	audio := progressiveAudioTestPlan(t, audioSource, ProgressiveAudioRequest{OutputContainer: "m4a", AudioCodec: "aac", AllowAudioStreamCopy: profileTestPtr(false)}, limits)
	for _, plan := range []*transcode.Plan{hls.Plan, dynamic.Plan, video.Plan, audio.Plan} {
		if plan.SourceAudioChannels != 6 || plan.AudioChannels != 2 || plan.Execution.DownMixAudioBoost != 2 {
			t.Fatalf("downmix lost the actual source channels or effective multiplier: %+v", plan)
		}
	}
	limits.Execution.DownMixAudioBoost = 1
	neutral := progressiveVideoTestPlan(t, videoSource, videoRequest, limits)
	if neutral.Plan.SourceAudioChannels != 0 || neutral.Plan.Execution.DownMixAudioBoost != 0 {
		t.Fatal("inactive downmix facts split the immutable plan identity")
	}
}

func TestTranscodingSoftwareToneSizeLimitIsAppliedBeforeOutputProjection(t *testing.T) {
	source := progressiveVideoTestSource()
	video := &source.Info.Streams[0]
	video.Codec, video.PixelFormat, video.BitDepth = "hevc", "yuv420p10le", 10
	video.VideoRange, video.VideoRangeKnown = "HDR10", true
	video.ColorTransfer, video.ColorPrimaries, video.ColorSpace, video.ColorRange = "smpte2084", "bt2020", "bt2020nc", "tv"
	limits := conversionTestLimits()
	limits.Execution = transcode.DefaultExecutionOptions(2)
	limits.Execution.SoftwareToneMapOptions = transcode.SoftwareToneMapOptions{ToneMapSizeLimit: "Limit420", SoftwareToneMapAlgorithm: "Mobius"}
	request := conversionTestRequest()
	request.AllowVideoStreamCopy = profileTestPtr(false)
	hls := conversionTestPlan(t, source, request, limits)
	progressiveRequest := progressiveVideoTestRequest()
	progressiveRequest.AllowVideoStreamCopy = profileTestPtr(false)
	progressive := progressiveVideoTestPlan(t, source, progressiveRequest, limits)
	for _, plan := range []*transcode.Plan{hls.Plan, progressive.Plan} {
		if plan.Height > 420 || plan.VideoFilters.Backend != "" || plan.Execution.SoftwareToneMapOptions.SoftwareToneMapAlgorithm != "Mobius" {
			t.Fatalf("CPU tone mapping policy was applied after immutable output selection: %+v", plan)
		}
	}
	if hls.OutputSource.Info.Streams[0].Height != hls.Plan.Height || progressive.OutputSource.Info.Streams[0].Height != progressive.Plan.Height {
		t.Fatal("tone mapping size limit changed execution without its advertised output")
	}
	limits.Hardware = transcode.Hardware{Decode: "software", Encode: "software", Device: "/dev/dri/renderD128"}
	gpu := progressiveVideoTestPlan(t, source, progressiveRequest, limits)
	if gpu.Plan.Height != 1080 || gpu.Plan.VideoFilters.Backend != "vulkan" {
		t.Fatal("software tone mapping size limit changed an independent GPU graph")
	}
	progressiveRequest.Height = profileTestPtr(720)
	limits.Hardware = transcode.Hardware{}
	progressiveVideoTestDeclined(t, source, progressiveRequest, limits)
}

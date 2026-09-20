package playback

import (
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestExecutionLegacyPlannerDoesNotInventManagerDefaults(t *testing.T) {
	decision := conversionTestPlan(t, conversionTestSource(), conversionTestRequest(), conversionTestLimits())
	if decision.Plan.ExecutionVersion != 0 || decision.Plan.Execution != (transcode.ExecutionOptions{}) {
		t.Fatal("legacy planner invented execution settings before manager admission")
	}
	limits := conversionTestLimits()
	limits.Execution.Threads = 3
	if _, err := PlanConversion(conversionTestSource(), conversionTestRequest(), limits); err == nil {
		t.Fatal("partially resolved execution settings were accepted")
	}
}

func TestExecutionCaptureAcrossLocalAndDynamicPlanners(t *testing.T) {
	options := transcode.DefaultExecutionOptions(7)
	options.H264 = transcode.CPUQuality{Preset: "slow", RateControl: "capped_crf", CRF: 19}
	limits := conversionTestLimits()
	limits.Execution, limits.MaxBitrate = options, 1_500_000
	source, request := conversionTestSource(), conversionTestRequest()
	request.AllowVideoStreamCopy = profileTestPtr(false)
	request.MaxStreamingBitrate = profileTestPtr(int64(1_000_000))
	hls := conversionTestPlan(t, source, request, limits)
	source.Info.DurationTicks = 0
	dynamic, err := PlanDynamicConversion(source, request, limits)
	if err != nil || dynamic.Plan == nil {
		t.Fatalf("dynamic settings plan unavailable: %v, %+v", err, dynamic.Reasons)
	}
	videoRequest := progressiveVideoTestRequest()
	videoRequest.AllowVideoStreamCopy = profileTestPtr(false)
	videoRequest.MaxBitrate = profileTestPtr(int64(1_000_000))
	progressive := progressiveVideoTestPlan(t, progressiveVideoTestSource(), videoRequest, limits)
	for _, plan := range []*transcode.Plan{hls.Plan, dynamic.Plan, progressive.Plan} {
		if plan.ExecutionVersion != transcode.ExecutionVersion || plan.Execution.Threads != 7 || plan.Execution.H264 != options.H264 {
			t.Fatal("a planner lost the admitted execution settings")
		}
		if plan.VideoBitrate <= 0 || plan.VideoBitrate+plan.AudioBitrate > 1_000_000 {
			t.Fatal("capped CRF escaped the existing client/server output budget")
		}
	}
	if hls.OutputSource.Info.Bitrate > 1_000_000 || dynamic.OutputSource.Info.Bitrate > 1_000_000 || progressive.OutputSource.Info.Bitrate > 1_000_000 {
		t.Fatal("quality settings raised the projected transport budget")
	}
	options.Threads, options.H264.Preset = 9, "fast"
	if hls.Plan.Execution.Threads != 7 || hls.Plan.Execution.H264.Preset != "slow" {
		t.Fatal("a mutable settings value changed the admitted plan")
	}
}

func TestExecutionToneGatesRejectRequiredBackendWithoutRelabelingHDR(t *testing.T) {
	for _, backend := range []string{"software", "vulkan"} {
		t.Run(backend, func(t *testing.T) {
			source := progressiveVideoTestSource()
			video := &source.Info.Streams[0]
			video.Codec, video.PixelFormat, video.BitDepth = "hevc", "yuv420p10le", 10
			video.VideoRange, video.VideoRangeKnown = "HDR10", true
			video.ColorTransfer, video.ColorPrimaries, video.ColorSpace, video.ColorRange = "smpte2084", "bt2020", "bt2020nc", "tv"
			limits := conversionTestLimits()
			limits.Execution = transcode.DefaultExecutionOptions(3)
			if backend == "software" {
				limits.Execution.SoftwareToneMapping = false
			} else {
				limits.Hardware = transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD128"}
				limits.Execution.VulkanToneMapping = false
			}
			request := conversionTestRequest()
			request.AllowVideoStreamCopy = profileTestPtr(false)
			hls := conversionTestDeclined(t, source, request, limits)
			progressiveRequest := progressiveVideoTestRequest()
			progressiveRequest.AllowVideoStreamCopy = profileTestPtr(false)
			progressive := progressiveVideoTestDeclined(t, source, progressiveRequest, limits)
			source.Info.DurationTicks = 0
			dynamic, err := PlanDynamicConversion(source, request, limits)
			if err != nil || dynamic.Plan != nil {
				t.Fatal("dynamic conversion ignored a disabled tone backend")
			}
			for _, reasons := range [][]Reason{hls.Reasons, progressive.Reasons, dynamic.Reasons} {
				found := false
				for _, reason := range reasons {
					found = found || reason.Code == "conversion_tone_mapping_disabled"
				}
				if !found {
					t.Fatal("disabled filter has no explicit unsupported reason")
				}
			}
			if video.VideoRange != "HDR10" || video.ColorTransfer != "smpte2084" || video.BitDepth != 10 {
				t.Fatal("disabled processing relabeled the source as SDR")
			}
		})
	}
}

func TestExecutionInactivePreferencesPreservePlanIdentity(t *testing.T) {
	source, request := conversionTestSource(), conversionTestRequest()
	request.AllowVideoStreamCopy = profileTestPtr(false)
	limits := conversionTestLimits()
	limits.Execution = transcode.DefaultExecutionOptions(2)
	before := *conversionTestPlan(t, source, request, limits).Plan
	limits.Execution.HEVC = transcode.CPUQuality{Preset: "slow", RateControl: "capped_crf", CRF: 35}
	limits.Execution.H264.CRF = 18
	limits.Execution.SoftwareToneMapping, limits.Execution.VulkanToneMapping = false, false
	after := *conversionTestPlan(t, source, request, limits).Plan
	if before != after {
		t.Fatal("inactive execution preferences invalidated the output identity")
	}
	limits.Execution.H264.Preset = "medium"
	if updated := *conversionTestPlan(t, source, request, limits).Plan; updated == before {
		t.Fatal("active preset was omitted from plan equality")
	}
}

func TestExecutionUnavailableHardwareDoesNotAuthorizeDefaultDevice(t *testing.T) {
	limits := conversionTestLimits()
	limits.Hardware = transcode.Hardware{Decode: "vaapi", Encode: "vaapi"}
	limits.HardwareUnavailable = true
	request := conversionTestRequest()
	request.AllowVideoStreamCopy = profileTestPtr(false)
	conversionTestDeclined(t, conversionTestSource(), request, limits)
	progressiveRequest := progressiveVideoTestRequest()
	progressiveRequest.AllowVideoStreamCopy = profileTestPtr(false)
	progressiveVideoTestDeclined(t, progressiveVideoTestSource(), progressiveRequest, limits)
	progressiveRequest.AllowVideoStreamCopy = nil
	copy := progressiveVideoTestPlan(t, progressiveVideoTestSource(), progressiveRequest, limits)
	if copy.Plan.VideoCodec != "copy" || copy.Plan.Hardware != (transcode.Hardware{}) {
		t.Fatal("missing managed device blocked or altered an authorized copy path")
	}
	limits.Execution = transcode.DefaultExecutionOptions(5)
	limits.Execution.SoftwareToneMapping, limits.Execution.VulkanToneMapping = false, false
	audio := progressiveAudioTestPlan(t, progressiveAudioTestSource(), ProgressiveAudioRequest{OutputContainer: "m4a", AudioCodec: "aac"}, limits)
	if audio.Plan.Execution.Threads != 5 || audio.Plan.Hardware != (transcode.Hardware{}) {
		t.Fatal("audio conversion lost threads or acquired an inactive hardware dependency")
	}
}

func TestExecutionUnavailableSoftwareFilterProfileCannotSwitchToCPU(t *testing.T) {
	source := progressiveVideoTestSource()
	video := &source.Info.Streams[0]
	video.Codec, video.PixelFormat, video.BitDepth = "hevc", "yuv420p10le", 10
	video.VideoRange, video.VideoRangeKnown = "HDR10", true
	video.ColorTransfer, video.ColorPrimaries, video.ColorSpace, video.ColorRange = "smpte2084", "bt2020", "bt2020nc", "tv"
	limits := conversionTestLimits()
	limits.Hardware = transcode.Hardware{Decode: "software", Encode: "software"}
	limits.Execution = transcode.DefaultExecutionOptions(3)
	request := conversionTestRequest()
	request.AllowVideoStreamCopy = profileTestPtr(false)
	progressiveRequest := progressiveVideoTestRequest()
	progressiveRequest.AllowVideoStreamCopy = profileTestPtr(false)
	// A real CPU choice is available without a device and has a supported CPU
	// tone map. The missing-device selection must not silently become it.
	control := progressiveVideoTestPlan(t, source, progressiveRequest, limits)
	if control.Plan.VideoFilters.Backend != "" || control.Plan.VideoFilters.ToneMap != "hdr10" {
		t.Fatal("control did not establish the otherwise available software tone map")
	}
	for _, device := range []string{"", "/dev/dri/renderD129"} {
		name := "missing-id-path-unresolved"
		if device != "" {
			name = "known-filter-device-unavailable"
		}
		t.Run(name, func(t *testing.T) {
			selected := limits
			selected.Hardware.Device, selected.HardwareUnavailable = device, true
			hls := conversionTestDeclined(t, source, request, selected)
			progressive := progressiveVideoTestDeclined(t, source, progressiveRequest, selected)
			dynamicSource := source
			dynamicSource.Info.DurationTicks = 0
			dynamic, err := PlanDynamicConversion(dynamicSource, request, selected)
			if err != nil || dynamic.Plan != nil {
				t.Fatal("dynamic encoding replaced the unavailable selected execution profile")
			}
			for _, reasons := range [][]Reason{hls.Reasons, progressive.Reasons, dynamic.Reasons} {
				found := false
				for _, reason := range reasons {
					found = found || reason.Code == "conversion_hardware_unavailable"
				}
				if !found {
					t.Fatal("selected-profile refusal lost its explicit unavailable reason")
				}
			}
			if video.VideoRange != "HDR10" || video.ColorTransfer != "smpte2084" || video.BitDepth != 10 {
				t.Fatal("refused execution profile relabeled the HDR source")
			}
			copy := progressiveVideoTestPlan(t, progressiveVideoTestSource(), progressiveVideoTestRequest(), selected)
			if copy.Plan.VideoCodec != "copy" || copy.Plan.Hardware != (transcode.Hardware{}) {
				t.Fatal("unavailable video execution profile blocked a compatible copied stream")
			}
			audio := progressiveAudioTestPlan(t, progressiveAudioTestSource(), ProgressiveAudioRequest{OutputContainer: "m4a", AudioCodec: "aac"}, selected)
			if audio.Plan.Execution.Threads != 3 || audio.Plan.Hardware != (transcode.Hardware{}) {
				t.Fatal("unavailable video execution profile blocked independent audio encoding")
			}
		})
	}
}

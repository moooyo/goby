package playback

import (
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func inactiveVideoDeviceDecision(t *testing.T, transport string, source Source, limits ConversionLimits, burn bool) (*transcode.Plan, []Reason) {
	t.Helper()
	if transport == "progressive" {
		request := progressiveVideoTestRequest()
		request.AllowVideoStreamCopy = profileTestPtr(false)
		request.MaxWidth = profileTestPtr(1280)
		if burn {
			request.BurnSubtitles, request.SubtitleStreamIndex = true, profileTestPtr(12)
		}
		decision, err := PlanProgressiveVideo(source, request, limits)
		if err != nil {
			t.Fatal(err)
		}
		return decision.Plan, decision.Reasons
	}
	request := conversionTestRequest()
	request.AllowVideoStreamCopy = profileTestPtr(false)
	request.DeviceProfile.TranscodingProfiles[0].MaxWidth = profileTestPtr(1280)
	if burn {
		request.SubtitleStreamIndex = profileTestPtr(12)
		request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{{Method: SubtitleDeliveryMethodEncode, Format: source.Info.Streams[3].Codec, Container: "ts", Protocol: "hls"}}
	}
	var decision ConversionDecision
	var err error
	if transport == "dynamic" {
		source.Info.DurationTicks = 0
		decision, err = PlanDynamicConversion(source, request, limits)
	} else {
		decision, err = PlanConversion(source, request, limits)
	}
	if err != nil {
		t.Fatal(err)
	}
	return decision.Plan, decision.Reasons
}

func TestInactiveCPUDeviceDoesNotRejectOrSplitPlainSDREncoding(t *testing.T) {
	for _, transport := range []string{"hls", "dynamic", "progressive"} {
		for _, capture := range []bool{false, true} {
			name := transport + "/legacy"
			if capture {
				name = transport + "/captured"
			}
			t.Run(name, func(t *testing.T) {
				limits := conversionTestLimits()
				limits.Hardware = transcode.Hardware{Decode: "software", Encode: "software", Device: "/dev/dri/renderD129"}
				if capture {
					limits.Execution = transcode.DefaultExecutionOptions(5)
				}
				selected := limits.Hardware
				plan, reasons := inactiveVideoDeviceDecision(t, transport, progressiveVideoTestSource(), limits, false)
				if plan == nil || plan.VideoCodec != "h264" || plan.Width != 1280 || plan.Hardware.Device != "" || plan.VideoFilters != (transcode.VideoFilters{}) {
					t.Fatalf("inactive CPU device prevented ordinary SDR encoding: plan=%+v reasons=%+v", plan, reasons)
				}
				if err := transcode.ValidatePlan(*plan); err != nil {
					t.Fatalf("CPU plan retained an invalid device dependency: %v", err)
				}
				if limits.Hardware != selected {
					t.Fatal("planning changed the administrator's retained device selection")
				}
				limits.Hardware.Device = ""
				withoutDevice, _ := inactiveVideoDeviceDecision(t, transport, progressiveVideoTestSource(), limits, false)
				if withoutDevice == nil || *plan != *withoutDevice {
					t.Fatal("inactive device choice split an otherwise identical CPU output identity")
				}
			})
		}
	}
}

func TestCPUDeviceSurvivesFinalVulkanProcessingAndLateSubtitleSelection(t *testing.T) {
	for _, transport := range []string{"hls", "dynamic", "progressive"} {
		for _, processing := range []string{"hdr", "ten-bit", "deinterlace", "subtitle"} {
			t.Run(transport+"/"+processing, func(t *testing.T) {
				source := progressiveVideoTestSource()
				video := &source.Info.Streams[0]
				switch processing {
				case "hdr", "ten-bit":
					video.Codec, video.PixelFormat, video.BitDepth = "hevc", "yuv420p10le", 10
					if processing == "hdr" {
						video.VideoRange = "HDR10"
						video.ColorTransfer, video.ColorPrimaries, video.ColorSpace, video.ColorRange = "smpte2084", "bt2020", "bt2020nc", "tv"
					}
				case "deinterlace":
					video.IsInterlaced, video.FieldOrder = true, "tt"
				case "subtitle":
					// Dynamic sources admit bitmap burn-in without inventing a
					// finite-file text extraction path. All three use the same graph.
					source.Info.Streams[3].Codec, source.Info.Streams[3].IsTextSubtitleStream = "hdmv_pgs_subtitle", false
				}
				limits := conversionTestLimits()
				limits.Hardware = transcode.Hardware{Decode: "software", Encode: "software", Device: "/dev/dri/renderD129"}
				limits.Execution = transcode.DefaultExecutionOptions(5)
				plan, reasons := inactiveVideoDeviceDecision(t, transport, source, limits, processing == "subtitle")
				if plan == nil || plan.Hardware != limits.Hardware || plan.VideoFilters.Backend != "vulkan" {
					t.Fatalf("required Vulkan device was discarded: plan=%+v reasons=%+v", plan, reasons)
				}
				if processing == "subtitle" && plan.Subtitle.Mode != "burn" {
					t.Fatal("late subtitle composition did not retain its actual burn graph")
				}
				if err := transcode.ValidatePlan(*plan); err != nil {
					t.Fatalf("Vulkan plan is not executable: %v", err)
				}
			})
		}
	}
}

func TestInactiveCPUDeviceCannotBypassSelectedProfileUnavailability(t *testing.T) {
	limits := conversionTestLimits()
	limits.Hardware = transcode.Hardware{Decode: "software", Encode: "software", Device: "/dev/dri/renderD129"}
	limits.HardwareUnavailable = true
	limits.Execution = transcode.DefaultExecutionOptions(5)
	for _, transport := range []string{"hls", "dynamic", "progressive"} {
		t.Run(transport, func(t *testing.T) {
			plan, reasons := inactiveVideoDeviceDecision(t, transport, progressiveVideoTestSource(), limits, false)
			if plan != nil {
				t.Fatal("inactive-device normalization bypassed unavailable-profile admission")
			}
			found := false
			for _, reason := range reasons {
				found = found || reason.Code == "conversion_hardware_unavailable"
			}
			if !found {
				t.Fatal("unavailable selected profile lost its explicit refusal reason")
			}
		})
	}
	copy := progressiveVideoTestPlan(t, progressiveVideoTestSource(), progressiveVideoTestRequest(), limits)
	if copy.Plan.VideoCodec != "copy" || copy.Plan.Hardware != (transcode.Hardware{}) {
		t.Fatal("unavailable video device changed an independent copy path")
	}
	audio := progressiveAudioTestPlan(t, progressiveAudioTestSource(), ProgressiveAudioRequest{OutputContainer: "m4a", AudioCodec: "aac"}, limits)
	if audio.Plan.Hardware != (transcode.Hardware{}) || audio.Plan.Execution.Threads != 5 {
		t.Fatal("unavailable video device changed independent audio encoding")
	}
}

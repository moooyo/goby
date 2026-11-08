package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

func TestManagedTranscodingPolicyUsesTheCapturedRequestRevision(t *testing.T) {
	server := &Server{cfg: config.Config{Transcoding: config.TranscodingConfig{Enabled: true, Threads: 2}}}
	policy := settings.DefaultTranscodingPolicy()
	policy.EnableHevcEncoding, policy.EnableHardwareEncoding = false, false
	policy.EnableSubtitleExtraction, policy.EnableOnTheFlyAttachmentExtraction = false, false
	snapshot := requestSettingsSnapshot{Revision: 9, Effective: settings.Values{MaxBitrate: 4_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 6},
		Transcoding: policy, Execution: transcode.DefaultExecutionOptions(2), CodecConfigurations: transcode.DefaultCodecConfigurations(),
		hardwareFallback: transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD129"}}
	snapshot.CodecConfigurations[1].Priority = 200
	request := managedExecutionRequest(httptest.NewRequest(http.MethodGet, "/", nil), snapshot)
	snapshot.Transcoding = settings.DefaultTranscodingPolicy()
	snapshot.CodecConfigurations[1].Priority = 0
	planning := server.requestPlanningConfig(request)
	if !planning.DisableHevcEncoding || !planning.DisableHardwareEncoding || !planning.DisableSubtitleExtraction || !planning.DisableAttachmentExtraction ||
		planning.Hardware.Encode != "software" || planning.Hardware.Decode != "vaapi" || planning.Hardware.Device != "/dev/dri/renderD129" || planning.CodecConfigurations[1].Priority != 200 {
		t.Fatalf("native policy mixed current settings with the captured admission: %+v", planning)
	}
	limits := hlsServerLimits(planning)
	if !limits.DisableHevcEncoding || !limits.DisableHardwareEncoding || !limits.DisableSubtitleExtraction || !limits.DisableAttachmentExtraction || limits.CodecConfigurations != planning.CodecConfigurations {
		t.Fatal("native policy was lost between request configuration and conversion limits")
	}
}

func TestManagedHardwareEncodingDisableDoesNotRequireAnUnusedMissingDevice(t *testing.T) {
	server := &Server{cfg: config.Config{Transcoding: config.TranscodingConfig{Enabled: true, Threads: 2}}}
	policy := settings.DefaultTranscodingPolicy()
	policy.EnableHardwareEncoding = false
	snapshot := requestSettingsSnapshot{Effective: settings.Values{MaxBitrate: 4_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 8},
		Transcoding: policy, Execution: transcode.DefaultExecutionOptions(2),
		hardwareFallback: transcode.Hardware{Decode: "software", Encode: "vaapi"}, hardwareFallbackUnavailable: true}
	request := managedExecutionRequest(httptest.NewRequest(http.MethodGet, "/", nil), snapshot)
	limits := hlsServerLimits(server.requestPlanningConfig(request))
	source := playback.Source{ItemID: "item", MediaSourceID: media.SourceID("item"), Path: "/media/input.mkv", ItemType: "Movie",
		Info: media.Info{Container: "matroska", DurationTicks: 60 * media.TicksPerSecond, FormatStartKnown: true, Bitrate: 1_000_000,
			Streams: []media.Stream{{Index: 0, CodecType: "video", Codec: "h264", Profile: "High", BitDepth: 8, PixelFormat: "yuv420p",
				Width: 320, Height: 180, Bitrate: 1_000_000, InterlaceKnown: true, AverageFrameRate: "24/1"}}}}
	copyDisabled := false
	videoRequest := playback.ProgressiveVideoRequest{OutputContainer: "mp4", VideoCodec: "h264", AllowVideoStreamCopy: &copyDisabled}
	decision, err := playback.PlanProgressiveVideo(source, videoRequest, limits)
	if err != nil || decision.Plan == nil || planUsesHardware(*decision.Plan) {
		t.Fatalf("missing unused encoder device blocked CPU admission: %+v, %v", decision, err)
	}
	if _, err := server.resolveHardwareEncoding(context.Background(), limits, playback.ConversionDecision{Plan: decision.Plan}, false); err != nil {
		t.Fatalf("second admission gate still required an unused device: %v", err)
	}
	snapshot.hardwareFallback.Decode = "vaapi"
	request = managedExecutionRequest(httptest.NewRequest(http.MethodGet, "/", nil), snapshot)
	limits = hlsServerLimits(server.requestPlanningConfig(request))
	decision, err = playback.PlanProgressiveVideo(source, videoRequest, limits)
	if err != nil || decision.Plan != nil {
		t.Fatal("disabling encoding bypassed the independently required unavailable decoder")
	}
}

func TestManagedSoftwareCodecConfigurationBlocksHardwareFallback(t *testing.T) {
	runtime := hardwareEncodingTestRuntime(t)
	runtime.probe = func(context.Context, string, string, media.HardwareEncodingRequest) (media.HardwareEncodingResult, error) {
		return media.HardwareEncodingResult{Code: "hardware_encoding_output_mismatch"}, nil
	}
	server := &Server{hardwareEncoding: runtime}
	plan := hardwareEncodingTestPlan(t)
	configurations := transcode.DefaultCodecConfigurations()
	configurations[1].IsEnabled = false
	limits := playback.ConversionLimits{AllowVideoTranscode: true, CodecConfigurations: configurations}
	if !transcode.ConfiguredVideoEncodingAllowed(configurations, plan) {
		t.Fatal("native software encoder policy incorrectly disabled the hardware encoder")
	}
	_, err := server.resolveHardwareEncoding(context.Background(), limits, playback.ConversionDecision{Plan: &plan}, true)
	if !errors.Is(err, errHLSRequestUnsupported) {
		t.Fatalf("hardware failure bypassed a disabled software fallback encoder: %v", err)
	}
}

func TestManagedTranscodingTempPathStateDoesNotRetargetTheActiveCache(t *testing.T) {
	server := &Server{activeTranscodingTempPath: "/private/active-transcodes"}
	state := server.ManagedTranscodingTempPathState("/private/next-transcodes")
	if state.Active == nil || *state.Active != server.activeTranscodingTempPath || !state.RestartRequired || state.Desired != "/private/next-transcodes" {
		t.Fatal("saved restart path replaced the captured active location")
	}
	*state.Active = "/private/caller-edited"
	if server.activeTranscodingTempPath != "/private/active-transcodes" {
		t.Fatal("returned active path exposed mutable process state")
	}
	if server.ManagedTranscodingTempPathState(server.activeTranscodingTempPath).RestartRequired {
		t.Fatal("equal active and desired directories required a restart")
	}
}

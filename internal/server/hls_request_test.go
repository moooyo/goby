package server

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func hlsRequestTestSource() playback.Source {
	return playback.Source{
		ItemID: "hls-item", MediaSourceID: media.SourceID("hls-item"), ItemType: "Movie", Path: "/media/source.mp4",
		Info: media.Info{
			Container: "mov,mp4", DurationTicks: 120 * media.TicksPerSecond, Bitrate: 5_000_000,
			Streams: []media.Stream{
				{Index: 2, CodecType: "video", Codec: "h264", Width: 1920, Height: 1080, Bitrate: 4_000_000,
					AverageFrameRate: "30/1", BitDepth: 8, PixelFormat: "yuv420p", InterlaceKnown: true, VideoRangeKnown: true, VideoRange: "SDR"},
				{Index: 5, CodecType: "audio", Codec: "aac", Channels: 2, SampleRate: 48_000, Bitrate: 192_000, IsDefault: true},
				{Index: 9, CodecType: "audio", Codec: "ac3", Channels: 6, SampleRate: 48_000, Bitrate: 640_000},
			},
		},
	}
}

func hlsRequestTestLimits() playback.ConversionLimits {
	return playback.ConversionLimits{
		MaxBitrate: 20_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 8,
		AllowRemux: true, AllowAudioTranscode: true, AllowVideoTranscode: true,
		Hardware: transcode.Hardware{Decode: "software", Encode: "software"},
	}
}

func hlsRequestTestPlan(t *testing.T, values map[string]string, source playback.Source, limits playback.ConversionLimits) playback.ConversionDecision {
	t.Helper()
	decision, err := hlsRequestConversion(values, source, limits)
	if err != nil || decision.Plan == nil {
		t.Fatalf("expected a constructible manual HLS request: %v", err)
	}
	if !decision.Output.OriginalCompatible || !decision.Output.ProfileMatched {
		t.Error("manual HLS output did not satisfy the explicit output constraints")
	}
	return decision
}

func TestHLSRequestDefaultsRemainExplicitHLSOutputWithoutMutatingInputs(t *testing.T) {
	source, limits := hlsRequestTestSource(), hlsRequestTestLimits()
	values := map[string]string{
		"DeviceId": "client-device", "PlaySessionId": "play-correlation", "api_key": "opaque-token",
		"UnrecognizedClientHint": "ignored", "HardwareEncoder": "nvenc", "HardwareDevice": "untrusted-device",
	}
	beforeSource, _ := json.Marshal(source)
	beforeValues := make(map[string]string, len(values))
	for key, value := range values {
		beforeValues[key] = value
	}
	decision := hlsRequestTestPlan(t, values, source, limits)
	plan := decision.Plan
	if plan.Container != "ts" || plan.VideoCodec != "copy" || plan.AudioCodec != "copy" || plan.SegmentSeconds != 6 ||
		plan.StartTicks != 0 || plan.DurationTicks != source.Info.DurationTicks || plan.VideoStreamIndex != 2 || plan.AudioStreamIndex != 5 {
		t.Error("default manual HLS request did not preserve the bounded remux plan")
	}
	if decision.Original.ProfileMatched {
		t.Error("HLS defaults invented generic original-file client compatibility")
	}
	afterSource, _ := json.Marshal(source)
	if string(beforeSource) != string(afterSource) || !reflect.DeepEqual(beforeValues, values) || limits != hlsRequestTestLimits() {
		t.Error("manual HLS planning mutated caller-owned facts, query fields, or limits")
	}
}

func TestHLSRequestMapsCapsAliasesAndConfiguredHardwareToConstructibleOutput(t *testing.T) {
	limits := hlsRequestTestLimits()
	limits.Hardware = transcode.Hardware{Decode: "cuda", Encode: "nvenc", Device: "1"}
	values := map[string]string{
		"Container": "MPEGTS", "SegmentContainer": "ts", "SegmentLength": "3",
		"VideoCodec": "HEVC,H264", "AudioCodec": "AAC,MP3", "AudioStreamIndex": "5", "SubtitleStreamIndex": "-1",
		"VideoBitrate": "2000000", "AudioBitrate": "96000", "AudioSampleRate": "44100",
		"MaxWidth": "1280", "Width": "1280", "MaxHeight": "720", "Height": "720",
		"MaxAudioChannels": "1", "TranscodingMaxAudioChannels": "1", "AudioChannels": "1",
		"Framerate": "24", "MaxFramerate": "24.0", "StartTimeTicks": "100000000",
		"EnableAutoStreamCopy": "false", "AllowVideoStreamCopy": "true", "AllowAudioStreamCopy": "true",
		"HardwareEncoder": "software", "Hwaccel": "software", "Device": "client-chosen",
	}
	decision := hlsRequestTestPlan(t, values, hlsRequestTestSource(), limits)
	plan := decision.Plan
	if plan.VideoCodec != "h264" || plan.AudioCodec != "aac" || plan.Width != 1280 || plan.Height != 720 ||
		plan.FrameRate != 24 || plan.VideoBitrate != 2_000_000 || plan.AudioBitrate != 96_000 ||
		plan.AudioChannels != 1 || plan.AudioSampleRate != 44_100 || plan.StartTicks != 10*media.TicksPerSecond ||
		plan.DurationTicks != 120*media.TicksPerSecond || plan.SegmentSeconds != 3 || plan.Hardware != limits.Hardware {
		t.Error("explicit manual HLS constraints were not reflected in the physical output plan")
	}
	capped := hlsRequestTestPlan(t, map[string]string{"MaxStreamingBitrate": "1000000"}, hlsRequestTestSource(), limits)
	if capped.OutputSource.Info.Bitrate > 1_000_000 {
		t.Error("manual HLS planning ignored the total streaming bitrate constraint")
	}
}

func TestHLSRequestCopyFlagsSelectedAudioAndAudioOnlyCandidates(t *testing.T) {
	for _, test := range []struct {
		name, video, audio string
		values             map[string]string
	}{
		{"copy-default", "copy", "copy", nil},
		{"video-copy-disabled", "h264", "copy", map[string]string{"AllowVideoStreamCopy": "false"}},
		{"audio-copy-disabled", "copy", "aac", map[string]string{"AllowAudioStreamCopy": "false"}},
		{"auto-copy-disabled", "h264", "aac", map[string]string{"EnableAutoStreamCopy": "false"}},
		{"specific-copy-denial", "h264", "copy", map[string]string{"EnableAutoStreamCopy": "true", "AllowVideoStreamCopy": "false"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := hlsRequestTestPlan(t, test.values, hlsRequestTestSource(), hlsRequestTestLimits()).Plan
			if plan.VideoCodec != test.video || plan.AudioCodec != test.audio {
				t.Error("manual HLS copy flags selected the wrong physical conversion")
			}
		})
	}
	selected := hlsRequestTestPlan(t, map[string]string{"AudioStreamIndex": "9", "TranscodingMaxAudioChannels": "2"}, hlsRequestTestSource(), hlsRequestTestLimits())
	if selected.Plan.AudioStreamIndex != 9 || selected.Plan.AudioCodec != "aac" || selected.Plan.AudioChannels != 2 {
		t.Error("manual HLS did not select and downmix the requested source audio")
	}
	source := hlsRequestTestSource()
	source.ItemType = "Audio"
	source.Info.Streams = []media.Stream{source.Info.Streams[1]}
	decision := hlsRequestTestPlan(t, map[string]string{"AudioCodec": "mp3", "AudioBitrate": "128000"}, source, hlsRequestTestLimits())
	if decision.Plan.VideoStreamIndex != -1 || decision.Plan.VideoCodec != "" || decision.Plan.AudioCodec != "mp3" ||
		decision.Plan.AudioBitrate != 128_000 || decision.Plan.AudioChannels != 2 {
		t.Error("audio-only HLS failed to apply the selected MP3 candidate")
	}
}

func TestHLSRequestRejectsInvalidOrUnsupportedExplicitConstraints(t *testing.T) {
	for _, test := range []struct {
		name   string
		values map[string]string
		want   error
	}{
		{"width-conflict", map[string]string{"Width": "640", "MaxWidth": "1280"}, errHLSRequestInvalid},
		{"height-conflict", map[string]string{"Height": "360", "MaxHeight": "720"}, errHLSRequestInvalid},
		{"channels-conflict", map[string]string{"MaxAudioChannels": "2", "TranscodingMaxAudioChannels": "6"}, errHLSRequestInvalid},
		{"framerate-conflict", map[string]string{"Framerate": "24", "MaxFramerate": "30"}, errHLSRequestInvalid},
		{"field-case-conflict", map[string]string{"VideoCodec": "h264", "videocodec": "hevc"}, errHLSRequestInvalid},
		{"segment-zero", map[string]string{"SegmentLength": "0"}, errHLSRequestInvalid},
		{"segment-too-long", map[string]string{"SegmentLength": "11"}, errHLSRequestInvalid},
		{"negative-start", map[string]string{"StartTimeTicks": "-1"}, errHLSRequestInvalid},
		{"full-duration-start", map[string]string{"StartTimeTicks": "1200000000"}, errHLSRequestInvalid},
		{"overflow-start", map[string]string{"StartTimeTicks": "9223372036854775808"}, errHLSRequestInvalid},
		{"invalid-audio-index", map[string]string{"AudioStreamIndex": "-1"}, errHLSRequestInvalid},
		{"missing-audio-index", map[string]string{"AudioStreamIndex": "99"}, errHLSRequestInvalid},
		{"overflow-audio-index", map[string]string{"AudioStreamIndex": "2147483648"}, errHLSRequestInvalid},
		{"invalid-subtitle-index", map[string]string{"SubtitleStreamIndex": "-2"}, errHLSRequestInvalid},
		{"selected-subtitle", map[string]string{"SubtitleStreamIndex": "12"}, errHLSRequestUnsupported},
		{"invalid-copy-flag", map[string]string{"EnableAutoStreamCopy": "perhaps"}, errHLSRequestInvalid},
		{"non-finite-fps", map[string]string{"Framerate": "NaN"}, errHLSRequestInvalid},
		{"infinite-fps", map[string]string{"MaxFramerate": "+Inf"}, errHLSRequestInvalid},
		{"zero-bitrate", map[string]string{"VideoBitrate": "0"}, errHLSRequestInvalid},
		{"overflow-bitrate", map[string]string{"AudioBitrate": "9223372036854775808"}, errHLSRequestInvalid},
		{"unsupported-sample-rate", map[string]string{"AudioSampleRate": "48001"}, errHLSRequestUnsupported},
		{"empty-codec", map[string]string{"VideoCodec": ""}, errHLSRequestInvalid},
		{"unsupported-video", map[string]string{"VideoCodec": "hevc"}, errHLSRequestUnsupported},
		{"unsupported-audio", map[string]string{"AudioCodec": "opus,ac3"}, errHLSRequestUnsupported},
		{"too-many-codecs", map[string]string{"AudioCodec": strings.Repeat("aac,", 8) + "aac"}, errHLSRequestInvalid},
		{"unsupported-container", map[string]string{"Container": "mp4"}, errHLSRequestUnsupported},
		{"unsupported-segment-container", map[string]string{"SegmentContainer": "webm"}, errHLSRequestUnsupported},
		{"unsupported-protocol", map[string]string{"Protocol": "dash"}, errHLSRequestUnsupported},
		{"byte-seeking", map[string]string{"TranscodeSeekInfo": "Bytes"}, errHLSRequestUnsupported},
		{"burn-in", map[string]string{"SubtitleMethod": "Encode"}, errHLSRequestUnsupported},
		{"tone-mapping", map[string]string{"EnableToneMapping": "true"}, errHLSRequestUnsupported},
		{"HDR", map[string]string{"VideoRangeType": "HDR10"}, errHLSRequestUnsupported},
		{"copy-timestamps", map[string]string{"CopyTimestamps": "true"}, errHLSRequestUnsupported},
		{"extra-manifest-subtitles", map[string]string{"MaxManifestSubtitles": "1"}, errHLSRequestUnsupported},
		{"oversized-hint", map[string]string{"ClientHint": strings.Repeat("x", maxHLSQueryText+1)}, errHLSRequestInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision, err := hlsRequestConversion(test.values, hlsRequestTestSource(), hlsRequestTestLimits())
			if !errors.Is(err, test.want) || decision.Plan != nil {
				t.Errorf("invalid manual HLS request error = %v, want %v without a plan", err, test.want)
			}
		})
	}
	source := hlsRequestTestSource()
	source.Info.DurationTicks = 30 * 24 * 60 * 60 * media.TicksPerSecond
	last := strconv.FormatInt(source.Info.DurationTicks-1, 10)
	decision := hlsRequestTestPlan(t, map[string]string{"StartTimeTicks": last}, source, hlsRequestTestLimits())
	if decision.Plan.StartTicks != source.Info.DurationTicks-1 || decision.Plan.DurationTicks != source.Info.DurationTicks {
		t.Error("manual HLS parsing truncated the full source timeline beyond int32 ticks")
	}
	source = hlsRequestTestSource()
	source.Info.Streams[0].VideoRange = "HDR10"
	if decision, err := hlsRequestConversion(nil, source, hlsRequestTestLimits()); !errors.Is(err, errHLSRequestUnsupported) || decision.Plan != nil {
		t.Error("manual HLS planning accepted unsupported source HDR conversion")
	}
	limits := hlsRequestTestLimits()
	limits.AllowAudioTranscode, limits.AllowVideoTranscode = false, false
	if decision, err := hlsRequestConversion(map[string]string{"EnableAutoStreamCopy": "false"}, hlsRequestTestSource(), limits); !errors.Is(err, errHLSRequestUnsupported) || decision.Plan != nil {
		t.Error("explicit conversion flags bypassed user conversion permissions")
	}
}

func TestHLSUserLimitsUseCurrentPolicyWithoutAdministratorOverrides(t *testing.T) {
	cfg := config.TranscodingConfig{
		Enabled: true, MaxBitrate: 9_000_000, MaxWidth: 1280, MaxHeight: 720, MaxAudioChannels: 6,
		Hardware: transcode.Hardware{Decode: "cuda", Encode: "nvenc", Device: "1"},
	}
	for _, test := range []struct {
		name, policy        string
		admin, disabled     bool
		remux, audio, video bool
	}{
		{"absent-flags", "{}", false, false, true, true, true},
		{"explicit-flags", "{\"EnablePlaybackRemuxing\":false,\"EnableAudioPlaybackTranscoding\":true,\"EnableVideoPlaybackTranscoding\":false}", false, false, false, true, false},
		{"administrator-denied", "{\"EnablePlaybackRemuxing\":false,\"EnableAudioPlaybackTranscoding\":false,\"EnableVideoPlaybackTranscoding\":false}", true, false, false, false, false},
		{"media-playback-disabled", "{\"EnableMediaPlayback\":false}", true, false, false, false, false},
		{"media-playback-null", "{\"EnableMediaPlayback\":null}", false, false, false, false, false},
		{"media-playback-invalid", "{\"EnableMediaPlayback\":\"true\"}", false, false, false, false, false},
		{"null-remux", "{\"EnablePlaybackRemuxing\":null}", false, false, false, true, true},
		{"invalid-audio", "{\"EnableAudioPlaybackTranscoding\":1}", false, false, true, false, true},
		{"invalid-video", "{\"EnableVideoPlaybackTranscoding\":\"true\"}", true, false, true, true, false},
		{"disabled-user", "{}", true, true, false, false, false},
		{"empty-policy", "", false, false, false, false, false},
		{"invalid-policy", "{", false, false, false, false, false},
		{"null-policy", "null", false, false, false, false, false},
		{"array-policy", "[]", false, false, false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			user := identity.User{Policy: json.RawMessage(test.policy), IsAdministrator: test.admin, IsDisabled: test.disabled}
			limits := hlsUserLimits(cfg, user)
			if limits.AllowRemux != test.remux || limits.AllowAudioTranscode != test.audio || limits.AllowVideoTranscode != test.video {
				t.Error("conversion grants did not follow the current user policy")
			}
			if limits.MaxBitrate != cfg.MaxBitrate || limits.MaxWidth != cfg.MaxWidth || limits.MaxHeight != cfg.MaxHeight ||
				limits.MaxAudioChannels != cfg.MaxAudioChannels || limits.Hardware != cfg.Hardware {
				t.Error("user policy projection changed configured execution limits")
			}
		})
	}
	cfg.Enabled = false
	if limits := hlsUserLimits(cfg, identity.User{Policy: json.RawMessage("{}")}); limits.AllowRemux || limits.AllowAudioTranscode || limits.AllowVideoTranscode {
		t.Error("user defaults re-enabled disabled server transcoding")
	}
}

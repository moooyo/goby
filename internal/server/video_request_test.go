package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func videoRequestTestSource() playback.Source {
	return playback.Source{
		ItemID: "video-query-item", MediaSourceID: media.SourceID("video-query-item"), ItemType: "Movie", Path: "/media/movie.mkv",
		Info: media.Info{Container: "matroska", DurationTicks: 120 * media.TicksPerSecond, Bitrate: 3_692_000, Size: 55_380_000,
			FormatStartKnown: true, FormatStartTicks: 14_000_000,
			Streams: []media.Stream{
				{Index: 0, CodecType: "video", Codec: "mjpeg", IsAttachedPicture: true},
				{Index: 2, CodecType: "video", Codec: "h264", Width: 1920, Height: 1080, Bitrate: 3_500_000,
					BitDepth: 8, PixelFormat: "yuv420p", Profile: "High", AverageFrameRate: "30000/1001",
					IsDefault: true, InterlaceKnown: true, VideoRange: "SDR", VideoRangeKnown: true},
				{Index: 5, CodecType: "audio", Codec: "aac", Bitrate: 192_000, Channels: 2, SampleRate: 48_000, Profile: "LC", IsDefault: true},
				{Index: 9, CodecType: "audio", Codec: "aac", Bitrate: 64_000, Channels: 1, SampleRate: 24_000, Profile: "LC"},
				{Index: 12, CodecType: "subtitle", Codec: "srt", IsTextSubtitleStream: true},
				{Index: 15, CodecType: "subtitle", Codec: "srt", IsTextSubtitleStream: true, IsExternal: true},
			}},
	}
}

func videoRequestTestLimits() playback.ConversionLimits {
	return playback.ConversionLimits{MaxBitrate: 20_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 8,
		AllowRemux: true, AllowAudioTranscode: true, AllowVideoTranscode: true}
}

func videoRequestTestPlan(t *testing.T, source playback.Source, values map[string]string, suffix string, limits playback.ConversionLimits) videoRequestResult {
	t.Helper()
	result, err := videoRequestDecision(source, values, suffix, limits)
	if err != nil || result.Original || result.Conversion.Plan == nil {
		t.Fatalf("expected a video conversion, original=%t plan=%t error=%v", result.Original, result.Conversion.Plan != nil, err)
	}
	if err := transcode.ValidatePlan(*result.Conversion.Plan); err != nil {
		t.Fatalf("video query selected an unexecutable plan: %v", err)
	}
	return result
}

func TestVideoRequestPreservesUnconstrainedAndStaticOriginalDelivery(t *testing.T) {
	for _, test := range []struct {
		name, suffix string
		values       map[string]string
	}{
		{"bare legacy", "", nil},
		{"native suffix", "mkv", nil},
		{"native container", "", map[string]string{"Container": "mkv"}},
		{"client-side seek", "", map[string]string{"StartTimeTicks": "123456789"}},
		{"legacy position hint", "", map[string]string{"StartPositionTicks": "123456789"}},
		{"static explicit output hints", "mkv", map[string]string{"Static": "true", "VideoCodec": "unsupported", "MaxWidth": "320", "CopyTimestamps": "true", "SubtitleMethod": "Encode"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := videoRequestTestSource()
			source.Info.FormatStartKnown = false
			result, err := videoRequestDecision(source, test.values, test.suffix, playback.ConversionLimits{})
			if err != nil || !result.Original || result.Conversion.Plan != nil {
				t.Fatalf("original-file delivery unexpectedly required conversion: original=%t error=%v", result.Original, err)
			}
			if test.name == "client-side seek" && result.StartTicks != 123456789 {
				t.Fatal("original seeking changed the client's source position")
			}
		})
	}
	for _, container := range []string{"mp4", "m4v"} {
		source := videoRequestTestSource()
		source.Info.Container, source.Path = "mov,mp4,m4a,3gp,3g2,mj2", "/media/movie."+container
		result, err := videoRequestDecision(source, nil, container, playback.ConversionLimits{})
		if err != nil || !result.Original {
			t.Fatalf("native %s video was normalized to an audio-only container: %v", container, err)
		}
		result, err = videoRequestDecision(source, nil, "m4a", videoRequestTestLimits())
		if !errors.Is(err, errVideoRequestUnsupported) || result.Original {
			t.Fatal("the audio M4A alias incorrectly authorized raw MP4 video delivery")
		}
	}
}

func TestVideoRequestExactTargetsAndIndependentCeilings(t *testing.T) {
	values := map[string]string{
		"Static": "false", "VideoCodec": "h264", "AudioCodec": "aac", "Width": "640", "Height": "360",
		"MaxWidth": "1280", "MaxHeight": "720", "Framerate": "24", "MaxFramerate": "30",
		"VideoBitrate": "1000000", "MaxVideoBitrate": "1200000", "AudioBitrate": "96000", "MaxAudioBitrate": "128000",
		"AudioChannels": "1", "MaxAudioChannels": "2", "TranscodingMaxAudioChannels": "1",
		"AudioSampleRate": "24000", "MaxSampleRate": "48000", "MaxStreamingBitrate": "1200000",
	}
	result := videoRequestTestPlan(t, videoRequestTestSource(), values, "mp4", videoRequestTestLimits())
	plan := result.Conversion.Plan
	if plan.VideoCodec != "h264" || plan.AudioCodec != "aac" || plan.Width != 640 || plan.Height != 360 || plan.FrameRate != 24 ||
		plan.VideoBitrate != 1_000_000 || plan.AudioBitrate != 96_000 || plan.AudioChannels != 1 || plan.AudioSampleRate != 24_000 {
		t.Fatal("video query targets were weakened into ceilings or silently ignored")
	}
	for _, conflict := range []map[string]string{
		{"Width": "640", "MaxWidth": "320"}, {"Height": "720", "MaxHeight": "360"},
		{"Framerate": "30", "MaxFramerate": "24"}, {"VideoBitrate": "1000000", "MaxVideoBitrate": "800000"},
		{"AudioBitrate": "192000", "MaxAudioBitrate": "96000"}, {"AudioChannels": "2", "TranscodingMaxAudioChannels": "1"},
		{"AudioChannels": "2", "MaxAudioChannels": "1", "TranscodingMaxAudioChannels": "6"},
		{"AudioSampleRate": "48000", "MaxSampleRate": "24000"},
	} {
		conflict["Static"] = "false"
		result, err := videoRequestDecision(videoRequestTestSource(), conflict, "mp4", videoRequestTestLimits())
		if !errors.Is(err, errVideoRequestUnsupported) || result.Original || result.Conversion.Plan != nil {
			t.Fatalf("conflicting exact targets and ceilings produced playable output: %v", err)
		}
	}
}

func TestVideoRequestCeilingsNeverFallThroughToRawBytes(t *testing.T) {
	for _, test := range []struct {
		name, value string
		check       func(transcode.Plan) bool
	}{
		{"MaxWidth", "640", func(p transcode.Plan) bool { return p.VideoCodec == "h264" && p.Width <= 640 }},
		{"MaxHeight", "360", func(p transcode.Plan) bool { return p.VideoCodec == "h264" && p.Height <= 360 }},
		{"MaxFramerate", "24", func(p transcode.Plan) bool { return p.VideoCodec == "h264" && p.FrameRate > 0 && p.FrameRate <= 24 }},
		{"MaxVideoBitrate", "800000", func(p transcode.Plan) bool { return p.VideoCodec == "h264" && p.VideoBitrate <= 800_000 }},
		{"MaxAudioBitrate", "64000", func(p transcode.Plan) bool { return p.AudioCodec == "aac" && p.AudioBitrate <= 64_000 }},
		{"MaxAudioChannels", "1", func(p transcode.Plan) bool { return p.AudioCodec == "aac" && p.AudioChannels == 1 }},
		{"TranscodingMaxAudioChannels", "1", func(p transcode.Plan) bool { return p.AudioCodec == "aac" && p.AudioChannels == 1 }},
		{"MaxSampleRate", "24000", func(p transcode.Plan) bool { return p.AudioCodec == "aac" && p.AudioSampleRate <= 24_000 }},
		{"MaxStreamingBitrate", "1000000", func(p transcode.Plan) bool { return p.VideoCodec == "h264" && p.VideoBitrate <= 1_000_000 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := videoRequestTestPlan(t, videoRequestTestSource(), map[string]string{test.name: test.value}, "", videoRequestTestLimits())
			if !test.check(*result.Conversion.Plan) {
				t.Fatal("a declared video ceiling did not constrain the produced representation")
			}
			if test.name == "MaxStreamingBitrate" && result.Conversion.OutputSource.Info.Bitrate > 1_000_000 {
				t.Fatal("the video budget excluded the selected audio stream")
			}
		})
	}
}

func TestVideoRequestUnsupportedTransformsCannotBecomeOriginalDelivery(t *testing.T) {
	for _, query := range []struct{ name, value string }{
		{"CopyTimestamps", "true"}, {"Deinterlace", "true"}, {"EnableToneMapping", "true"}, {"HDR", "true"},
		{"VideoCodec", "hevc"}, {"AudioCodec", "opus"}, {"TranscodingProtocol", "hls"}, {"SegmentContainer", "ts"},
		{"VideoFilter", "scale=640:360"}, {"AudioFilter", "volume=0.5"}, {"VideoProfile", "baseline"},
		{"LiveStreamId", "unimplemented-live-source"}, {"VideoRange", "HDR10"}, {"VideoRangeType", "HLG"},
		{"SubtitleMethod", "Encode"}, {"SubtitleDeliveryMethod", "Encode"}, {"SubtitleStreamIndex", "12"}, {"SubtitleStreamIndex", "99"},
	} {
		t.Run(query.name+"/"+query.value, func(t *testing.T) {
			result, err := videoRequestDecision(videoRequestTestSource(), map[string]string{query.name: query.value}, "", videoRequestTestLimits())
			if !errors.Is(err, errVideoRequestUnsupported) || result.Original || result.Conversion.Plan != nil {
				t.Fatalf("an unsupported explicit transformation became playable output: %v", err)
			}
		})
	}
}

func TestVideoRequestRejectsMalformedAndConflictingParameters(t *testing.T) {
	for _, values := range []map[string]string{
		{"Static": "invalid"}, {"StartTimeTicks": "-1"}, {"StartTimeTicks": "9223372036854775808"},
		{"StartPositionTicks": "-1"},
		{"VideoStreamIndex": "-1"}, {"VideoStreamIndex": "2147483648"}, {"VideoStreamIndex": "99"},
		{"AudioStreamIndex": "99"}, {"Width": "0"}, {"MaxWidth": "8193"}, {"Width": "640", "width": "320"},
		{"Framerate": "NaN"}, {"MaxFramerate": "Inf"}, {"Framerate": "0.5"}, {"Framerate": "30000/1001"},
		{"VideoBitrate": "-1"}, {"AudioBitrate": "9223372036854775808"}, {"AllowVideoStreamCopy": "sometimes"},
		{"VideoCodec": "h264;cmd"}, {"VideoCodec": "a,b,c,d,e,f,g,h,i"}, {"AudioCodec": strings.Repeat("a", 4097)},
		{"Container": "mkv"}, {"MaxWidth": ""}, {"VideoBitrate": ""}, {"Framerate": ""},
	} {
		result, err := videoRequestDecision(videoRequestTestSource(), values, "mp4", videoRequestTestLimits())
		if !errors.Is(err, errVideoRequestInvalid) || result.Original || result.Conversion.Plan != nil {
			t.Fatalf("malformed or conflicting video parameters were accepted: %v", err)
		}
	}
	for _, field := range []string{"MaxWidth", "VideoBitrate", "Framerate"} {
		result, err := videoRequestDecision(videoRequestTestSource(), map[string]string{field: ""}, "", videoRequestTestLimits())
		if !errors.Is(err, errVideoRequestInvalid) || result.Original {
			t.Fatalf("empty %s bypassed its parser through original delivery: %v", field, err)
		}
	}
}

func TestVideoRequestCodecCandidatesAndCaseInsensitiveFields(t *testing.T) {
	for _, values := range []map[string]string{
		{"vIdEoCoDeC": "hevc,h264", "AuDiOcOdEc": "opus,aac"},
		{"VideoCodec": "future-codec,h264", "AudioCodec": "future-codec,aac"},
		{"Static": "false", "Width": "640", "width": "640"},
	} {
		videoRequestTestPlan(t, videoRequestTestSource(), values, "mp4", videoRequestTestLimits())
	}
	result, err := videoRequestDecision(videoRequestTestSource(), map[string]string{"VideoCodec": "future-codec"}, "mp4", videoRequestTestLimits())
	if !errors.Is(err, errVideoRequestUnsupported) || result.Original || result.Conversion.Plan != nil {
		t.Fatalf("a valid unknown codec was not an unsupported representation: %v", err)
	}
}

func TestVideoRequestCopyAndEncodingPermissions(t *testing.T) {
	for _, test := range []struct {
		name         string
		values       map[string]string
		limits       playback.ConversionLimits
		video, audio string
		decline      bool
	}{
		{"remux only", nil, playback.ConversionLimits{AllowRemux: true}, "copy", "copy", false},
		{"audio only", nil, playback.ConversionLimits{AllowAudioTranscode: true}, "copy", "aac", false},
		{"video only", nil, playback.ConversionLimits{AllowVideoTranscode: true}, "h264", "copy", false},
		{"no permissions", nil, playback.ConversionLimits{}, "", "", true},
		{"copy off", map[string]string{"EnableAutoStreamCopy": "false"}, videoRequestTestLimits(), "h264", "aac", false},
		{"video copy off", map[string]string{"AllowVideoStreamCopy": "false"}, videoRequestTestLimits(), "h264", "copy", false},
		{"audio copy off", map[string]string{"AllowAudioStreamCopy": "false"}, videoRequestTestLimits(), "copy", "aac", false},
		{"copy cannot grant video encode", map[string]string{"AllowVideoStreamCopy": "false"}, playback.ConversionLimits{AllowRemux: true}, "", "", true},
		{"copy cannot grant audio encode", map[string]string{"AllowAudioStreamCopy": "false"}, playback.ConversionLimits{AllowRemux: true}, "", "", true},
		{"explicit video copy seek", map[string]string{"StartTimeTicks": "10000000", "VideoCodec": "copy"}, videoRequestTestLimits(), "", "", true},
		{"explicit copy plus copy denial", map[string]string{"VideoCodec": "copy", "AllowVideoStreamCopy": "false"}, videoRequestTestLimits(), "", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := videoRequestDecision(videoRequestTestSource(), test.values, "mp4", test.limits)
			if test.decline {
				if !errors.Is(err, errVideoRequestUnsupported) || result.Original || result.Conversion.Plan != nil {
					t.Fatalf("denied conversion became available: %v", err)
				}
				return
			}
			if err != nil || result.Original || result.Conversion.Plan == nil || result.Conversion.Plan.VideoCodec != test.video || result.Conversion.Plan.AudioCodec != test.audio {
				t.Fatalf("independent copy and encoding decisions changed: %v", err)
			}
		})
	}
}

func TestVideoRequestUsesOnlyVerifiedSourceClockAndLeavesInputsUnchanged(t *testing.T) {
	source := videoRequestTestSource()
	values := map[string]string{"Static": "false", "StartTimeTicks": "12345678", "VideoStreamIndex": "2", "AudioStreamIndex": "9",
		"SourceFormatStartKnown": "false", "SourceFormatStartTicks": "-999999", "FormatStartTicks": "0",
		"Hardware": "nvenc", "HardwareDevice": "/dev/dri/renderD129", "UserId": "untrusted", "DeviceId": "untrusted"}
	before, err := json.Marshal(struct {
		Source playback.Source
		Query  map[string]string
	}{source, values})
	if err != nil {
		t.Fatal(err)
	}
	result := videoRequestTestPlan(t, source, values, "mp4", videoRequestTestLimits())
	plan := result.Conversion.Plan
	if !plan.SourceFormatStartKnown || plan.SourceFormatStartTicks != source.Info.FormatStartTicks || plan.StartTicks != 12345678 ||
		plan.VideoStreamIndex != 2 || plan.AudioStreamIndex != 9 || plan.Hardware != (transcode.Hardware{}) {
		t.Fatal("query values replaced trusted source facts, stream indexes, or execution policy")
	}
	after, err := json.Marshal(struct {
		Source playback.Source
		Query  map[string]string
	}{source, values})
	if err != nil || string(before) != string(after) {
		t.Fatal("video query negotiation mutated caller-owned input")
	}
	limits := videoRequestTestLimits()
	limits.Hardware = transcode.Hardware{Decode: "software", Encode: "vaapi", Device: "/dev/dri/renderD128"}
	configured := videoRequestTestPlan(t, source, values, "mp4", limits)
	if configured.Conversion.Plan.Hardware != limits.Hardware {
		t.Fatal("query-provided hardware replaced the authorized execution configuration")
	}
	source.Info.FormatStartKnown = false
	values["SourceFormatStartKnown"] = "true"
	result, err = videoRequestDecision(source, values, "mp4", videoRequestTestLimits())
	if !errors.Is(err, errVideoRequestUnsupported) || result.Original || result.Conversion.Plan != nil {
		t.Fatalf("a client injected proof for an unknown source clock: %v", err)
	}
}

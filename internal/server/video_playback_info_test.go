package server

import (
	"net/url"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
)

func TestVideoPlaybackURLReconstructsConcreteProgressivePlan(t *testing.T) {
	integer := func(value int) *int { return &value }
	integer64 := func(value int64) *int64 { return &value }
	number := func(value float64) *float64 { return &value }
	disabled := false
	for _, test := range []struct {
		name    string
		request playback.ProgressiveVideoRequest
		mutate  func(*playback.Source)
	}{
		{"remux", playback.ProgressiveVideoRequest{}, nil},
		{"encode video copy audio", playback.ProgressiveVideoRequest{AllowVideoStreamCopy: &disabled, Width: integer(640), Height: integer(360), VideoBitrate: integer64(1_000_000)}, nil},
		{"copy video encode audio", playback.ProgressiveVideoRequest{AllowAudioStreamCopy: &disabled, AudioChannels: integer(1), AudioSampleRate: integer(24_000), AudioBitrate: integer64(96_000)}, nil},
		{"encode both and seek", playback.ProgressiveVideoRequest{StartTimeTicks: 12_345_678, AllowAudioStreamCopy: &disabled,
			Width: integer(640), Height: integer(360), FrameRate: number(23.976), VideoBitrate: integer64(1_000_000),
			AudioChannels: integer(1), AudioSampleRate: integer(24_000), AudioBitrate: integer64(96_000)}, nil},
		{"alternate source audio", playback.ProgressiveVideoRequest{AudioStreamIndex: integer(9)}, nil},
		{"negative source clock", playback.ProgressiveVideoRequest{StartTimeTicks: 10_000_000}, func(source *playback.Source) { source.Info.FormatStartTicks = -5_000_000 }},
		{"zero source clock", playback.ProgressiveVideoRequest{}, func(source *playback.Source) { source.Info.FormatStartTicks = 0 }},
		{"video without audio", playback.ProgressiveVideoRequest{AudioCodec: "none", StartTimeTicks: 10_000_000}, func(source *playback.Source) {
			source.Info.Streams = source.Info.Streams[:2]
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := videoRequestTestSource()
			if test.mutate != nil {
				test.mutate(&source)
			}
			request := test.request
			request.OutputContainer, request.VideoCodec = "mp4", "h264"
			request.VideoStreamIndex = integer(2)
			if request.AudioCodec == "" {
				request.AudioCodec = "aac"
			}
			selected, err := playback.PlanProgressiveVideo(source, request, videoRequestTestLimits())
			if err != nil || selected.Plan == nil {
				t.Fatalf("fixture has no progressive video output: %v", err)
			}
			target, err := url.Parse(videoPlaybackURL(source.ItemID, source.MediaSourceID, "play_owned", "video device", "test+token", *selected.Plan))
			if err != nil || target.Scheme != "" || target.Host != "" || target.Path != "/emby/Videos/"+source.ItemID+"/stream.mp4" {
				t.Fatal("video URL is not a standard relative stream route")
			}
			query := target.Query()
			if query.Get("DeviceId") != "video device" || query.Get("MediaSourceId") != source.MediaSourceID || query.Get("PlaySessionId") != "play_owned" ||
				query.Get("api_key") != "test+token" || query.Get("Static") != "false" || query.Get("SubtitleStreamIndex") != "-1" || query.Get("VideoStreamIndex") != "2" {
				t.Fatal("video URL lost its concrete selection or playback ownership fields")
			}
			for key := range query {
				name := strings.ToLower(key)
				if strings.Contains(name, "formatstart") || strings.Contains(name, "hardware") || name == "durationticks" {
					t.Fatal("video URL exposed trusted source-clock or execution configuration")
				}
			}
			if strings.Contains(target.String(), "/media/") {
				t.Fatal("video URL exposed an original source path")
			}
			if selected.Plan.AudioStreamIndex < 0 && (query.Has("AudioStreamIndex") || query.Has("AudioCodec") || query.Has("AudioBitrate")) {
				t.Fatal("a video-only plan acquired invented audio settings")
			}
			values := make(map[string]string, len(query))
			for key := range query {
				values[key] = query.Get(key)
			}
			values["SourceFormatStartKnown"], values["SourceFormatStartTicks"], values["FormatStartTicks"] = "false", "999999", "0"
			values["Hardware"], values["HardwareDevice"] = "cuda", "/dev/dri/renderD129"
			rebuilt := videoRequestTestPlan(t, source, values, "mp4", videoRequestTestLimits())
			if *rebuilt.Conversion.Plan != *selected.Plan || rebuilt.Conversion.Method != selected.Method {
				t.Fatal("URL round trip changed the source clock, selection, output settings, or copy decisions")
			}
			if selected.Plan.VideoCodec == "copy" {
				if query.Get("AllowVideoStreamCopy") != "true" || query.Has("Width") || query.Has("VideoBitrate") || query.Has("Framerate") {
					t.Fatal("copied video received invented encoder parameters")
				}
			} else if query.Get("AllowVideoStreamCopy") != "false" {
				t.Fatal("an encoded video URL permitted an accidental copy")
			}
			if selected.Plan.AudioCodec == "copy" && (query.Get("AllowAudioStreamCopy") != "true" || query.Has("AudioChannels") || query.Has("AudioSampleRate")) {
				t.Fatal("copied audio received invented encoder parameters")
			}
			if selected.Plan.AudioCodec == "aac" && query.Get("AllowAudioStreamCopy") != "false" {
				t.Fatal("an encoded audio URL permitted an accidental copy")
			}
			if selected.Plan.VideoCodec != "copy" && selected.Plan.FrameRate == 0 && query.Has("Framerate") {
				t.Fatal("timestamp-preserving encoding acquired an invented CFR target")
			}
		})
	}
}

func TestVideoPlaybackURLChangedSeekUsesCurrentSourceClockAndPolicy(t *testing.T) {
	source := videoRequestTestSource()
	disabled := false
	selected, err := playback.PlanProgressiveVideo(source, playback.ProgressiveVideoRequest{
		OutputContainer: "mp4", VideoCodec: "h264", AudioCodec: "aac", AllowVideoStreamCopy: &disabled,
		AllowAudioStreamCopy: &disabled, StartTimeTicks: 2 * media.TicksPerSecond}, videoRequestTestLimits())
	if err != nil || selected.Plan == nil {
		t.Fatalf("seek fixture has no video output: %v", err)
	}
	target, err := url.Parse(videoPlaybackURL(source.ItemID, source.MediaSourceID, "play_owned", "video device", "test-token", *selected.Plan))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for key, entry := range target.Query() {
		values[key] = entry[0]
	}
	values["StartTimeTicks"] = "10000000"
	result := videoRequestTestPlan(t, source, values, "mp4", videoRequestTestLimits())
	want := *selected.Plan
	want.StartTicks = media.TicksPerSecond
	if *result.Conversion.Plan != want || result.StartTicks != want.StartTicks {
		t.Fatal("changing only the seek changed unrelated concrete output settings")
	}
	limits := videoRequestTestLimits()
	limits.AllowVideoTranscode = false
	result, err = videoRequestDecision(source, values, "mp4", limits)
	if err == nil || result.Original || result.Conversion.Plan != nil {
		t.Fatal("a previously advertised URL bypassed the current video encoding policy")
	}
	source.Info.FormatStartKnown = false
	values["SourceFormatStartKnown"] = "true"
	result, err = videoRequestDecision(source, values, "mp4", videoRequestTestLimits())
	if err == nil || result.Original || result.Conversion.Plan != nil {
		t.Fatal("an old URL replaced missing current source-clock proof")
	}
}

package server

import (
	"net/url"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
)

func TestAudioPlaybackURLReconstructsConcreteProgressivePlan(t *testing.T) {
	integer := func(value int) *int { return &value }
	integer64 := func(value int64) *int64 { return &value }
	noCopy := false
	for _, test := range []struct {
		name, sourceContainer, sourceCodec string
		request                            playback.ProgressiveAudioRequest
	}{
		{"encoded-mp3", "flac", "flac", playback.ProgressiveAudioRequest{
			OutputContainer: "mp3", AudioCodec: "mp3", StartTimeTicks: 2 * media.TicksPerSecond,
			AudioChannels: integer(1), AudioSampleRate: integer(24_000), AudioBitrate: integer64(96_000), AllowAudioStreamCopy: &noCopy}},
		{"encoded-flac", "flac", "flac", playback.ProgressiveAudioRequest{
			OutputContainer: "flac", AudioCodec: "flac", StartTimeTicks: 2 * media.TicksPerSecond,
			AudioChannels: integer(2), AudioSampleRate: integer(48_000), AudioBitDepth: integer(16), AllowAudioStreamCopy: &noCopy}},
		{"encoded-wav", "aac", "aac", playback.ProgressiveAudioRequest{
			OutputContainer: "wav", AudioCodec: "pcm_s16le", AudioChannels: integer(1), AudioSampleRate: integer(48_000)}},
		{"copied-aac-m4a", "aac", "aac", playback.ProgressiveAudioRequest{OutputContainer: "m4a", AudioCodec: "copy"}},
		{"copied-mp3", "mp3", "mp3", playback.ProgressiveAudioRequest{OutputContainer: "mp3", AudioCodec: "copy"}},
		{"ogg-sample-seek", "ogg", "vorbis", playback.ProgressiveAudioRequest{
			OutputContainer: "mp3", AudioCodec: "mp3", StartTimeTicks: 12_378_912,
			AudioChannels: integer(1), AudioSampleRate: integer(24_000), AudioBitrate: integer64(96_000)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := audioRequestTestSource(test.sourceContainer, test.sourceCodec)
			source.Info.AudioDurationExact = true
			stream := &source.Info.Streams[1]
			stream.AudioTiming = &media.AudioTiming{Exact: true, SampleCount: 120 * int64(stream.SampleRate), PacketCount: 300,
				EndTicks: source.Info.DurationTicks}
			request := test.request
			request.AudioStreamIndex = integer(stream.Index)
			selected, err := playback.PlanProgressiveAudio(source, request, audioRequestTestLimits())
			if err != nil || selected.Plan == nil {
				t.Fatalf("fixture has no progressive output: %v", err)
			}
			target, err := url.Parse(audioPlaybackURL(source.ItemID, source.MediaSourceID, "play_owned", "audio device", "test+token", *selected.Plan))
			if err != nil || target.Scheme != "" || target.Host != "" || target.Path != "/emby/Audio/"+source.ItemID+"/stream."+selected.Plan.Container {
				t.Fatal("progressive URL is not a standard relative audio stream route")
			}
			query := target.Query()
			if query.Get("MediaSourceId") != source.MediaSourceID || query.Get("PlaySessionId") != "play_owned" ||
				query.Get("DeviceId") != "audio device" || query.Get("api_key") != "test+token" || query.Get("Static") != "false" {
				t.Fatal("progressive URL lost its ownership or concrete-output parameters")
			}
			if query.Has("AudioSourceSampleCount") || query.Has("DurationTicks") || query.Has("AudioSampleSeek") || strings.Contains(target.String(), "/media/") {
				t.Fatal("progressive URL exposed internal source facts")
			}
			values := make(map[string]string, len(query))
			for key := range query {
				values[key] = query.Get(key)
			}
			// A query can express desired output, but cannot replace source
			// duration or sample measurements from the authorized catalog.
			values["AudioSourceSampleCount"], values["DurationTicks"] = "1", "1"
			values["AudioSampleSeek"] = "false"
			rebuilt, err := audioRequestDecision(source, values, selected.Plan.Container, false, audioRequestTestLimits())
			if err != nil || rebuilt.Original || rebuilt.Progressive == nil || rebuilt.Progressive.Plan == nil {
				t.Fatalf("concrete progressive URL could not be replanned: %v", err)
			}
			if *rebuilt.Progressive.Plan != *selected.Plan || rebuilt.Progressive.Method != selected.Method {
				t.Fatal("URL round trip changed the selected concrete media plan")
			}
			if selected.Plan.AudioCodec == "copy" && query.Get("AudioCodec") != "copy" ||
				selected.Plan.AudioCodec != "copy" && query.Get("AllowAudioStreamCopy") != "false" {
				t.Fatal("URL permitted a different encode/copy decision")
			}
		})
	}
}

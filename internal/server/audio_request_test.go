package server

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
)

func audioRequestTestSource(container, codec string) playback.Source {
	depth := 0
	if codec == "flac" || codec == "alac" {
		depth = 24
	} else if codec == "pcm_f32le" {
		depth = 32
	}
	return playback.Source{
		ItemID: "audio-query-item", MediaSourceID: media.SourceID("audio-query-item"),
		ItemType: "Audio", Path: "/media/track." + container,
		Info: media.Info{
			Container: container, DurationTicks: 120 * media.TicksPerSecond, Bitrate: 128_000, Size: 1_920_000,
			Streams: []media.Stream{
				{Index: 0, CodecType: "video", Codec: "mjpeg", IsAttachedPicture: true},
				{Index: 5, CodecType: "audio", Codec: codec, IsDefault: true, Channels: 2, SampleRate: 44_100, Bitrate: 128_000, BitDepth: depth},
			},
		},
	}
}

func audioRequestTestLimits() playback.ConversionLimits {
	return playback.ConversionLimits{
		MaxBitrate: 20_000_000, MaxAudioChannels: 8,
		AllowRemux: true, AllowAudioTranscode: true, AllowVideoTranscode: true,
	}
}

func audioRequestTestResult(t *testing.T, source playback.Source, values map[string]string, suffix string, universal bool) audioRequestResult {
	t.Helper()
	result, err := audioRequestDecision(source, values, suffix, universal, audioRequestTestLimits())
	if err != nil {
		t.Fatalf("audio query was unexpectedly rejected: %v", err)
	}
	count := 0
	if result.Original {
		count++
	}
	if result.Progressive != nil && result.Progressive.Plan != nil {
		count++
	}
	if result.HLS != nil && result.HLS.Plan != nil {
		count++
	}
	if count != 1 {
		t.Fatal("audio query did not select exactly one usable delivery mode")
	}
	return result
}

func TestAudioUniversalPrioritizesOriginalContainersWithoutInventingCodecRestrictions(t *testing.T) {
	for _, test := range []struct {
		name, container, codec, capability string
	}{
		{"MP3", "mp3", "mp3", "mp3"},
		{"FLAC-list-order", "flac", "flac", "MP3,FLAC"},
		{"FLAC-reversed-list", "flac", "flac", "flac,mp3"},
		{"M4A-ALAC", "m4a", "alac", "mp4"},
		{"WAV-float", "wav", "pcm_f32le", "wave"},
		{"Ogg-Speex", "ogg", "speex", "oga"},
		{"Ogg-Opus", "ogg", "opus", "opus"},
		{"ADTS-AAC", "aac", "aac", "adts"},
		{"ASF-WMA", "asf", "wmav2", "wma"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := audioRequestTestSource(test.container, test.codec)
			result := audioRequestTestResult(t, source, map[string]string{
				"Container": test.capability, "AudioCodec": "unimplemented-output-codec",
				"AudioBitrate": "64000", "AudioChannels": "1", "AudioSampleRate": "22050",
				"TranscodingProtocol": "hls", "TranscodingContainer": "ts", "StartTimeTicks": "20000000",
				"MaxStreamingBitrate": "256000", "MaxSampleRate": "48000", "MaxAudioChannels": "2",
			}, "", true)
			if !result.Original || result.StartTicks != 2*media.TicksPerSecond || result.AudioStreamIndex != 5 {
				t.Error("compatible original audio was replaced by unused conversion settings")
			}
		})
	}
	minimal := audioRequestTestResult(t, audioRequestTestSource("mp3", "mp3"), nil, "", true)
	if !minimal.Original {
		t.Error("minimal MP3 universal request did not preserve observed original delivery")
	}
	source := audioRequestTestSource("mov,mp4,m4a", "alac")
	source.Path = "/media/track.m4b"
	m4b := audioRequestTestResult(t, source, map[string]string{"Container": "mp4"}, ".m4a", true)
	if !m4b.Original || m4b.SourceContainer != "m4a" {
		t.Error("M4B source facts were not normalized to their MP4 audio container family")
	}
	speex := audioRequestTestResult(t, audioRequestTestSource("ogg", "speex"), map[string]string{"Container": "opus"}, "", true)
	if speex.Original {
		t.Error("an Opus capability incorrectly claimed support for original Ogg/Speex")
	}
	unknown := audioRequestTestResult(t, audioRequestTestSource("flac", "flac"), map[string]string{"Container": "unknown-format"}, "", true)
	if unknown.Original || unknown.Progressive == nil || unknown.Progressive.Plan.Container != "mp3" || unknown.Progressive.Plan.AudioCodec != "mp3" {
		t.Error("unknown container capability invented original support or bypassed Goby's explicit fallback")
	}
}

func TestAudioUniversalCeilingsAndTrackSelectionCanRejectOriginalDelivery(t *testing.T) {
	source := audioRequestTestSource("flac", "flac")
	source.Info.Streams[1].SampleRate = 96_000
	for _, values := range []map[string]string{
		{"Container": "flac", "MaxSampleRate": "48000"},
		{"Container": "flac", "MaxAudioChannels": "1"},
		{"Container": "flac", "MaxStreamingBitrate": "96000"},
	} {
		result := audioRequestTestResult(t, source, values, "", true)
		if result.Original || result.Progressive == nil {
			t.Error("universal audio copied original bytes despite a declared capability ceiling")
		}
	}
	withTrack := audioRequestTestSource("mp4", "alac")
	withTrack.Info.Streams = append(withTrack.Info.Streams, media.Stream{
		Index: 9, CodecType: "audio", Codec: "aac", Channels: 1, SampleRate: 48_000, Bitrate: 96_000,
	})
	result := audioRequestTestResult(t, withTrack, map[string]string{"Container": "m4a", "AudioStreamIndex": "9"}, "", true)
	if result.Original || result.AudioStreamIndex != 9 || result.Progressive.Plan.AudioStreamIndex != 9 {
		t.Error("explicit nondefault audio selection was hidden behind original-file delivery")
	}
	unknownFacts := audioRequestTestSource("mp3", "mp3")
	unknownFacts.Info.Streams[1].SampleRate = 0
	if result, err := audioRequestDecision(unknownFacts, map[string]string{"Container": "mp3", "MaxSampleRate": "48000"}, "", true, audioRequestTestLimits()); !errors.Is(err, errAudioRequestUnsupported) || result.Original {
		t.Error("unknown source sample rate was treated as proof of compatibility")
	}
	allowed, err := audioRequestDecision(audioRequestTestSource("mp3", "mp3"), map[string]string{"Container": "mp3"}, "", true, playback.ConversionLimits{})
	if err != nil || !allowed.Original {
		t.Error("conversion permissions incorrectly disabled independently authorized original bytes")
	}
}

func TestAudioUniversalBareFormatsAllowOriginalButRespectBitrateCeilings(t *testing.T) {
	for _, format := range []struct{ container, codec string }{
		{"mp3", "mp3"}, {"flac", "flac"}, {"aac", "aac"}, {"wav", "pcm_s16le"},
	} {
		t.Run(format.container, func(t *testing.T) {
			source := audioRequestTestSource(format.container, format.codec)
			for _, values := range []map[string]string{
				nil,
				{"TranscodingProtocol": "hls", "TranscodingContainer": "ts", "AudioCodec": "aac", "AudioSampleRate": "22050"},
			} {
				if result := audioRequestTestResult(t, source, values, "", true); !result.Original {
					t.Error("bare Universal request treated conversion fallback settings as original-format restrictions")
				}
			}
			limited := audioRequestTestResult(t, source, map[string]string{"MaxStreamingBitrate": "96000"}, "", true)
			if limited.Original || limited.Progressive == nil || limited.Progressive.OutputSource.Info.Bitrate > 96_000 {
				t.Error("unconstrained original formats bypassed the explicit low-bitrate ceiling")
			}
		})
	}
}

func TestAudioProgressiveTargetsRemainIndependentFromCeilingsAndOutputAliases(t *testing.T) {
	source := audioRequestTestSource("flac", "flac")
	source.Info.Streams[1].Channels, source.Info.Streams[1].SampleRate = 1, 96_000
	values := map[string]string{
		"Container": "mp3", "TranscodingContainer": "adts", "AudioCodec": "aac",
		"AudioBitrate": "192000", "MaxStreamingBitrate": "256000",
		"AudioChannels": "2", "MaxAudioChannels": "6", "AudioSampleRate": "48000", "MaxSampleRate": "96000",
		"StartTimeTicks": "20000000",
	}
	result := audioRequestTestResult(t, source, values, "", true)
	plan := result.Progressive.Plan
	if plan.Container != "aac" || plan.AudioCodec != "aac" || plan.AudioBitrate != 192_000 ||
		plan.AudioChannels != 2 || plan.AudioSampleRate != 48_000 || plan.StartTicks != 2*media.TicksPerSecond ||
		result.Progressive.OutputSource.Info.DurationTicks != 118*media.TicksPerSecond {
		t.Error("progressive exact targets were reduced to aliases or ceilings")
	}
	for _, output := range []struct{ selector, container, codec string }{
		{"mp3", "mp3", "mp3"}, {"aac", "aac", "aac"}, {"mp4", "m4a", "aac"}, {"m4b", "m4a", "aac"},
		{"flac", "flac", "flac"}, {"oga", "ogg", "vorbis"}, {"opus", "ogg", "opus"}, {"wave", "wav", "pcm_s16le"},
	} {
		t.Run(output.selector, func(t *testing.T) {
			result := audioRequestTestResult(t, source, map[string]string{
				"Container": "mp3", "TranscodingContainer": output.selector, "EnableAutoStreamCopy": "false",
			}, "", true)
			if result.Progressive.Plan.Container != output.container || result.Progressive.Plan.AudioCodec != output.codec {
				t.Error("explicit output container did not select its closed progressive codec mapping")
			}
		})
	}
	for _, depth := range []string{"16", "24"} {
		result := audioRequestTestResult(t, source, map[string]string{
			"Container": "mp3", "TranscodingContainer": "flac", "AudioBitDepth": depth, "AllowAudioStreamCopy": "false",
		}, "", true)
		if result.Progressive.Plan.AudioBitDepth != 16 && depth == "16" || result.Progressive.Plan.AudioBitDepth != 24 && depth == "24" {
			t.Error("FLAC precision target did not reach the progressive plan")
		}
	}
	for _, mismatch := range []map[string]string{
		{"Container": "mp3", "TranscodingContainer": "aac", "AudioChannels": "2", "MaxAudioChannels": "1"},
		{"Container": "mp3", "TranscodingContainer": "aac", "AudioSampleRate": "96000", "MaxSampleRate": "48000"},
		{"Container": "mp3", "TranscodingContainer": "aac", "AudioBitrate": "192000", "MaxStreamingBitrate": "128000"},
		{"Container": "mp3", "TranscodingContainer": "mp3", "AudioCodec": "aac"},
	} {
		if result, err := audioRequestDecision(source, mismatch, "", true, audioRequestTestLimits()); !errors.Is(err, errAudioRequestUnsupported) || result.Original {
			t.Error("incompatible progressive targets were silently replaced by a different format or dimension")
		}
	}
}

func TestAudioLegacyAndStaticKeepTheirOriginalContractSeparateFromUniversal(t *testing.T) {
	mp3 := audioRequestTestSource("mp3", "mp3")
	original := audioRequestTestResult(t, mp3, nil, "mp3", false)
	if !original.Original {
		t.Error("unconstrained same-format legacy stream stopped serving original bytes")
	}
	converted := audioRequestTestResult(t, mp3, map[string]string{"Container": "adts"}, ".aac", false)
	if converted.Original || converted.Progressive.Plan.Container != "aac" {
		t.Error("legacy suffix and query container were interpreted as original-format capabilities")
	}
	if _, err := audioRequestDecision(mp3, map[string]string{"AudioCodec": "aac"}, "mp3", false, audioRequestTestLimits()); !errors.Is(err, errAudioRequestUnsupported) {
		t.Error("legacy output MP3/AAC mismatch returned incompatible original bytes")
	}
	static := audioRequestTestResult(t, mp3, map[string]string{
		"Static": "true", "AudioCodec": "aac", "AudioSampleRate": "22050", "AudioChannels": "1",
		"MaxStreamingBitrate": "1", "MaxSampleRate": "1", "MaxAudioChannels": "1", "StartTimeTicks": "20000000",
	}, "mp3", false)
	if !static.Original || static.Progressive != nil || static.HLS != nil {
		t.Error("Static=true stopped meaning complete original-file delivery")
	}
	seek := audioRequestTestResult(t, mp3, map[string]string{"StartTimeTicks": "20000000"}, "mp3", false)
	if seek.Original || seek.Progressive.Plan.StartTicks != 2*media.TicksPerSecond {
		t.Error("legacy conversion seeking returned an unchanged full original file")
	}
}

func TestAudioHLSKeepsExactTargetsAndCeilingsDistinctAndRejectsPackedAudio(t *testing.T) {
	source := audioRequestTestSource("flac", "flac")
	source.Info.Streams[1].SampleRate = 96_000
	values := map[string]string{
		"Container": "mp3", "TranscodingProtocol": "hls", "TranscodingContainer": "ts", "AudioCodec": "aac",
		"AudioChannels": "1", "MaxAudioChannels": "6", "AudioSampleRate": "44100", "MaxSampleRate": "48000",
		"AudioBitrate": "96000", "MaxStreamingBitrate": "128000", "StartTimeTicks": "20000000", "SegmentLength": "3",
	}
	result := audioRequestTestResult(t, source, values, "", true)
	plan := result.HLS.Plan
	if plan.Container != "ts" || plan.AudioCodec != "aac" || plan.AudioChannels != 1 || plan.AudioSampleRate != 44_100 ||
		plan.AudioBitrate != 96_000 || plan.StartTicks != 2*media.TicksPerSecond || plan.DurationTicks != source.Info.DurationTicks {
		t.Error("HLS audio changed exact targets or confused them with independent ceilings")
	}
	for _, changes := range []map[string]string{
		{"AudioChannels": "2", "MaxAudioChannels": "1"},
		{"AudioSampleRate": "48000", "MaxSampleRate": "44100"},
		{"AudioBitrate": "128000", "MaxStreamingBitrate": "128000"},
		{"TranscodingContainer": "aac"}, {"TranscodingContainer": "mp3"}, {"SegmentContainer": "aac"},
	} {
		request := make(map[string]string, len(values))
		for key, value := range values {
			request[key] = value
		}
		for key, value := range changes {
			request[key] = value
		}
		if result, err := audioRequestDecision(source, request, "", true, audioRequestTestLimits()); !errors.Is(err, errAudioRequestUnsupported) || result.Original || result.HLS != nil && result.HLS.Plan != nil {
			t.Errorf("unsupported HLS audio request returned a usable plan: %v", err)
		}
	}
	copyOnly := audioRequestTestResult(t, audioRequestTestSource("mp3", "mp3"), map[string]string{"Static": "false", "TranscodingProtocol": "hls", "AudioCodec": "copy"}, "", true)
	if copyOnly.HLS.Plan.AudioCodec != "copy" {
		t.Error("explicit HLS audio copy unexpectedly selected an encoder")
	}
}

func TestAudioRequestBoundsConflictsAndPureOwnership(t *testing.T) {
	source := audioRequestTestSource("mp3", "mp3")
	for _, test := range []struct {
		values map[string]string
		suffix string
		want   error
	}{
		{map[string]string{"AudioChannels": "1", "audiochannels": "2"}, "", errAudioRequestInvalid},
		{map[string]string{"Container": "m4a|aac"}, "", errAudioRequestInvalid},
		{map[string]string{"Container": "flac"}, "mp3", errAudioRequestInvalid},
		{map[string]string{"Container": "mp3,,flac"}, "", errAudioRequestInvalid},
		{map[string]string{"Container": "flac", "TranscodingContainer": "unknown-format"}, "", errAudioRequestUnsupported},
		{map[string]string{"AudioStreamIndex": "-1"}, "", errAudioRequestInvalid},
		{map[string]string{"AudioStreamIndex": "999"}, "", errAudioRequestInvalid},
		{map[string]string{"AudioChannels": "0"}, "", errAudioRequestInvalid},
		{map[string]string{"MaxAudioChannels": "2147483648"}, "", errAudioRequestInvalid},
		{map[string]string{"AudioSampleRate": "NaN"}, "", errAudioRequestInvalid},
		{map[string]string{"AudioBitrate": "9223372036854775808"}, "", errAudioRequestInvalid},
		{map[string]string{"StartTimeTicks": "-1"}, "", errAudioRequestInvalid},
		{map[string]string{"CopyTimestamps": "true"}, "", errAudioRequestUnsupported},
		{map[string]string{"CopyTimestamps": "perhaps"}, "", errAudioRequestInvalid},
		{map[string]string{"SubtitleStreamIndex": "2"}, "", errAudioRequestUnsupported},
		{map[string]string{"AudioFilter": "arbitrary-filter"}, "", errAudioRequestUnsupported},
		{map[string]string{"ClientHint": strings.Repeat("x", 4097)}, "", errAudioRequestInvalid},
	} {
		if result, err := audioRequestDecision(source, test.values, test.suffix, true, audioRequestTestLimits()); !errors.Is(err, test.want) || result.Original {
			t.Errorf("invalid audio request error = %v, want %v without original delivery", err, test.want)
		}
	}
	before := source
	before.Info.Streams = append([]media.Stream(nil), source.Info.Streams...)
	values := map[string]string{"Container": "mp3", "UserId": "other-user", "DeviceId": "other-device", "PlaySessionId": "other-session"}
	expected := make(map[string]string, len(values))
	for key, value := range values {
		expected[key] = value
	}
	result := audioRequestTestResult(t, source, values, "", true)
	if !result.Original || !reflect.DeepEqual(source, before) || !reflect.DeepEqual(values, expected) {
		t.Error("pure audio negotiation changed input facts or used identity hints as authority")
	}
}

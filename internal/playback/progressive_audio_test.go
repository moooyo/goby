package playback

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func progressiveAudioTestSource() Source {
	return Source{ItemID: "audio-item", MediaSourceID: media.SourceID("audio-item"), ItemType: "Audio", Path: "/media/audio.mka",
		Info: media.Info{Container: "matroska", DurationTicks: 120 * media.TicksPerSecond, Bitrate: 640_000, Size: 9_600_000,
			Streams: []media.Stream{
				{Index: 0, CodecType: "video", Codec: "mjpeg", IsAttachedPicture: true},
				{Index: 5, CodecType: "audio", Codec: "flac", IsDefault: true, Channels: 2, SampleRate: 48_000, BitDepth: 24, Bitrate: 512_000, TimeBase: "1/48000", Profile: "source-profile", CodecTag: "source-tag"},
				{Index: 9, CodecType: "audio", Codec: "aac", Channels: 1, SampleRate: 44_100, Bitrate: 64_000, TimeBase: "1/44100", Profile: "LC"},
			}}}
}

func progressiveAudioTestExactSource(samples int64, rate int) Source {
	source := progressiveAudioTestSource()
	stream := source.Info.Streams[1]
	stream.Index, stream.Codec, stream.Profile, stream.BitDepth, stream.SampleRate = 0, "aac", "LC", 0, rate
	stream.Bitrate = 128_000
	product := samples * media.TicksPerSecond
	duration := product / int64(rate)
	if product%int64(rate) != 0 {
		duration++
	}
	stream.AudioTiming = &media.AudioTiming{Exact: true, SampleCount: samples, PacketCount: (samples + 1023) / 1024, EndTicks: duration}
	source.Info.Streams, source.Info.Container, source.Path = []media.Stream{stream}, "aac", "/media/exact.aac"
	source.Info.DurationTicks, source.Info.AudioDurationExact = duration, true
	return source
}

func progressiveAudioTestPlan(t *testing.T, source Source, request ProgressiveAudioRequest, limits ConversionLimits) ProgressiveAudioDecision {
	t.Helper()
	decision, err := PlanProgressiveAudio(source, request, limits)
	if err != nil || decision.Plan == nil {
		t.Fatalf("expected a progressive audio plan, got %+v, %v", decision, err)
	}
	if err := transcode.ValidatePlan(*decision.Plan); err != nil {
		t.Fatalf("planner produced an invalid runner plan: %v", err)
	}
	return decision
}

func progressiveAudioTestDeclined(t *testing.T, source Source, request ProgressiveAudioRequest, limits ConversionLimits) ProgressiveAudioDecision {
	t.Helper()
	decision, err := PlanProgressiveAudio(source, request, limits)
	if err != nil || decision.Plan != nil || len(decision.Reasons) == 0 {
		t.Fatalf("expected an explained unsupported decision, got %+v, %v", decision, err)
	}
	return decision
}

func TestPlanProgressiveAudioSupportsClosedEncoderMatrix(t *testing.T) {
	for _, format := range []struct{ container, codec string }{
		{"mp3", "mp3"}, {"aac", "aac"}, {"m4a", "aac"}, {"flac", "flac"},
		{"ogg", "vorbis"}, {"ogg", "opus"}, {"ogg", "flac"}, {"wav", "pcm_s16le"},
	} {
		t.Run(format.container+"-"+format.codec, func(t *testing.T) {
			source := progressiveAudioTestSource()
			before := source
			before.Info.Streams = append([]media.Stream(nil), source.Info.Streams...)
			request := ProgressiveAudioRequest{OutputContainer: format.container, AudioCodec: format.codec, AllowAudioStreamCopy: profileTestPtr(false)}
			decision := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
			plan := decision.Plan
			if !reflect.DeepEqual(source, before) || decision.Method != "Transcode" || plan.OutputMode != "progressive" || plan.VideoStreamIndex != -1 ||
				plan.VideoCodec != "" || plan.AudioStreamIndex != 5 || plan.AudioCodec != format.codec || plan.Container != format.container ||
				plan.SegmentSeconds != 0 || plan.SegmentMode != "" || plan.SegmentTimes != "" || plan.Hardware != (transcode.Hardware{}) {
				t.Fatalf("unexpected progressive plan or source mutation: %+v", decision)
			}
			output := decision.OutputSource
			if output.Path != "output."+format.container || output.Info.Size != 0 || len(output.Info.Streams) != 1 || output.Info.DurationTicks != source.Info.DurationTicks {
				t.Fatalf("unexpected output source projection: %+v", output)
			}
			audio := output.Info.Streams[0]
			if audio.Index != 0 || audio.CodecType != "audio" || audio.Codec != format.codec || audio.TimeBase != "" || audio.CodecTag != "" || audio.Profile == "source-profile" {
				t.Fatalf("input container facts leaked into encoded output: %+v", audio)
			}
			if format.codec == "flac" && (plan.AudioBitDepth != 24 || audio.BitDepth != 24 || plan.AudioBitrate != 0 || audio.Bitrate != 0) {
				t.Fatalf("FLAC invented a compressed bitrate or discarded source precision: %+v", decision)
			}
			if format.codec != "flac" && format.codec != "pcm_s16le" && audio.BitDepth != 0 {
				t.Fatalf("lossy output claimed an unverified integer bit depth: %+v", audio)
			}
		})
	}
}

func TestPlanProgressiveAudioKeepsExactTargetsSeparateFromCeilings(t *testing.T) {
	request := ProgressiveAudioRequest{OutputContainer: "aac", AudioCodec: "aac", AudioStreamIndex: profileTestPtr(9),
		AudioChannels: profileTestPtr(2), MaxAudioChannels: profileTestPtr(6), AudioSampleRate: profileTestPtr(96_000), MaxSampleRate: profileTestPtr(96_000),
		AudioBitrate: profileTestPtr(int64(192_000)), MaxBitrate: profileTestPtr(int64(256_000))}
	decision := progressiveAudioTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits())
	if decision.Plan.AudioStreamIndex != 9 || decision.Plan.AudioChannels != 2 || decision.Plan.AudioSampleRate != 96_000 || decision.Plan.AudioBitrate != 192_000 {
		t.Fatalf("precise upmix/resample/bitrate targets were treated as ceilings or aliases: %+v", decision.Plan)
	}
	if decision.OutputSource.Info.Streams[0].Profile != "LC" || decision.OutputSource.Info.Bitrate > *request.MaxBitrate {
		t.Fatalf("AAC output facts or planning budget are inconsistent: %+v", decision.OutputSource)
	}
	request.MaxSampleRate = profileTestPtr(48_000)
	progressiveAudioTestDeclined(t, progressiveAudioTestSource(), request, conversionTestLimits())
	request.MaxSampleRate = profileTestPtr(96_000)
	request.MaxAudioChannels = profileTestPtr(1)
	progressiveAudioTestDeclined(t, progressiveAudioTestSource(), request, conversionTestLimits())
}

func TestPlanProgressiveAudioCopyRequiresMatchingSourceAndRemuxPermission(t *testing.T) {
	source := progressiveAudioTestSource()
	request := ProgressiveAudioRequest{OutputContainer: "flac", AudioCodec: "flac"}
	decision := progressiveAudioTestPlan(t, source, request, ConversionLimits{AllowRemux: true})
	if decision.Method != "DirectStream" || decision.Plan.AudioCodec != "copy" || decision.Plan.AudioChannels != 0 || decision.Plan.AudioSampleRate != 0 ||
		decision.OutputSource.Info.Streams[0].BitDepth != 24 || decision.OutputSource.Info.Streams[0].Index != 0 {
		t.Fatalf("copy was confused with encoded output: %+v", decision)
	}
	progressiveAudioTestDeclined(t, source, request, ConversionLimits{})
	request.AllowAudioStreamCopy = profileTestPtr(false)
	progressiveAudioTestDeclined(t, source, request, ConversionLimits{AllowRemux: true})
	decision = progressiveAudioTestPlan(t, source, request, ConversionLimits{AllowAudioTranscode: true})
	if decision.Method != "Transcode" || decision.Plan.AudioCodec != "flac" {
		t.Fatalf("copy restriction did not force authorized encoding: %+v", decision)
	}
	request.AudioCodec = "copy"
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
	request.AllowAudioStreamCopy = nil
	request.OutputContainer = "mp3"
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
}

func TestPlanProgressiveAudioExactChangesPreventAutomaticCopy(t *testing.T) {
	for _, mutate := range []func(*ProgressiveAudioRequest){
		func(request *ProgressiveAudioRequest) { request.AudioChannels = profileTestPtr(2) },
		func(request *ProgressiveAudioRequest) { request.AudioSampleRate = profileTestPtr(48_000) },
		func(request *ProgressiveAudioRequest) { request.AudioBitrate = profileTestPtr(int64(96_000)) },
	} {
		source := progressiveAudioTestSource()
		source.Info.Container, source.Path = "aac", "/media/audio.aac"
		source.Info.Streams = []media.Stream{source.Info.Streams[2]}
		request := ProgressiveAudioRequest{OutputContainer: "aac", AudioCodec: "aac", AudioStreamIndex: profileTestPtr(9)}
		baseline := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
		if baseline.Plan.AudioCodec != "copy" {
			t.Fatalf("copy was already unavailable before changing an exact target: %+v", baseline.Plan)
		}
		mutate(&request)
		decision := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
		if decision.Plan.AudioCodec != "aac" || decision.Method != "Transcode" {
			t.Fatalf("exact output change incorrectly retained copy: %+v", decision)
		}
	}
}

func TestPlanProgressiveAudioFLACPreservesKnownPrecision(t *testing.T) {
	for _, test := range []struct {
		name        string
		sourceDepth int
		target      *int
		want        int
		decline     bool
	}{
		{"16 bit", 16, nil, 16, false}, {"24 bit", 24, nil, 24, false}, {"unknown precision", 0, nil, 24, false},
		{"explicit quantization", 24, profileTestPtr(16), 16, false}, {"explicit promotion", 16, profileTestPtr(24), 24, false},
		{"unrequested reduction from 32 bit", 32, nil, 0, true}, {"explicit reduction from 32 bit", 32, profileTestPtr(24), 24, false},
		{"unsupported FLAC depth", 24, profileTestPtr(20), 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := progressiveAudioTestSource()
			source.Info.Streams[1].BitDepth = test.sourceDepth
			request := ProgressiveAudioRequest{OutputContainer: "flac", AudioCodec: "flac", AudioBitDepth: test.target, AllowAudioStreamCopy: profileTestPtr(false)}
			if test.decline {
				progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
				return
			}
			decision := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
			if decision.Plan.AudioBitDepth != test.want || decision.OutputSource.Info.Streams[0].BitDepth != test.want {
				t.Fatalf("wrong FLAC precision: %+v", decision)
			}
		})
	}
}

func TestPlanProgressiveAudioLosslessBudgetUsesFrameBoundsNotCompressionRatio(t *testing.T) {
	source := progressiveAudioTestSource()
	request := ProgressiveAudioRequest{OutputContainer: "flac", AudioCodec: "flac", AudioSampleRate: profileTestPtr(48_000),
		AudioChannels: profileTestPtr(2), AllowAudioStreamCopy: profileTestPtr(false), MaxBitrate: profileTestPtr(int64(1_000_000))}
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
	request.MaxBitrate = profileTestPtr(int64(3_000_000))
	decision := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	if decision.OutputSource.Info.Bitrate <= 48_000*2*24 || decision.OutputSource.Info.Bitrate > *request.MaxBitrate || decision.OutputSource.Info.Streams[0].Bitrate != 0 {
		t.Fatalf("FLAC budget assumed compression or exceeded its bound: %+v", decision.OutputSource)
	}
	request.AudioBitrate = profileTestPtr(int64(128_000))
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
	request = ProgressiveAudioRequest{OutputContainer: "wav", AudioCodec: "pcm_s16le", AudioChannels: profileTestPtr(2), AudioSampleRate: profileTestPtr(48_000),
		AudioBitrate: profileTestPtr(int64(1_536_000)), MaxBitrate: profileTestPtr(int64(1_600_000))}
	decision = progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.AudioBitrate != 0 || decision.OutputSource.Info.Streams[0].Bitrate != 1_536_000 || decision.OutputSource.Info.Streams[0].BitDepth != 16 {
		t.Fatalf("PCM used a nonexistent bitrate control or incorrect payload rate: %+v", decision)
	}
	request.MaxBitrate = profileTestPtr(int64(1_536_000))
	decision = progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	if decision.OutputSource.Info.Bitrate != 1_536_000 {
		t.Fatalf("container headers were mixed into the PCM media bitrate: %+v", decision.OutputSource)
	}
}

func TestPlanProgressiveAudioMP3UsesExactSupportedCBRRates(t *testing.T) {
	for _, test := range []struct {
		rate    int
		bitrate int64
		decline bool
	}{
		{44_100, 150_000, true}, {44_100, 160_000, false}, {12_000, 96_000, true}, {12_000, 64_000, false},
		{24_000, 144_000, false}, {24_000, 192_000, true}, {48_000, 320_000, false},
	} {
		request := ProgressiveAudioRequest{OutputContainer: "mp3", AudioCodec: "mp3", AudioSampleRate: &test.rate, AudioBitrate: &test.bitrate}
		if test.decline {
			progressiveAudioTestDeclined(t, progressiveAudioTestSource(), request, conversionTestLimits())
			continue
		}
		decision := progressiveAudioTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits())
		if decision.Plan.AudioBitrate != test.bitrate || decision.Plan.AudioSampleRate != test.rate {
			t.Fatalf("MP3 silently rounded an exact bitrate: %+v", decision.Plan)
		}
	}
	request := ProgressiveAudioRequest{OutputContainer: "mp3", AudioCodec: "mp3", AudioSampleRate: profileTestPtr(44_100), MaxBitrate: profileTestPtr(int64(175_000))}
	decision := progressiveAudioTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits())
	if decision.Plan.AudioBitrate != 160_000 || decision.OutputSource.Info.Bitrate > *request.MaxBitrate {
		t.Fatalf("MP3 budget was rounded upward: %+v", decision)
	}
}

func TestPlanProgressiveAudioAACContainerAndBitrateLimits(t *testing.T) {
	request := ProgressiveAudioRequest{OutputContainer: "aac", AudioCodec: "aac", AudioChannels: profileTestPtr(8), AudioSampleRate: profileTestPtr(48_000)}
	progressiveAudioTestDeclined(t, progressiveAudioTestSource(), request, conversionTestLimits())
	request.OutputContainer = "m4a"
	decision := progressiveAudioTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits())
	if decision.Plan.AudioChannels != 8 || decision.OutputSource.Info.Streams[0].Profile != "LC" {
		t.Fatalf("M4A incorrectly inherited the ADTS channel limit: %+v", decision)
	}
	request.AudioChannels, request.AudioSampleRate, request.AudioBitrate = profileTestPtr(1), profileTestPtr(48_000), profileTestPtr(int64(300_000))
	progressiveAudioTestDeclined(t, progressiveAudioTestSource(), request, conversionTestLimits())
}

func TestPlanProgressiveAudioOpusAndVorbisUseActualOutputCapabilities(t *testing.T) {
	source := progressiveAudioTestSource()
	source.Info.Streams[1].SampleRate = 24_000
	request := ProgressiveAudioRequest{OutputContainer: "ogg", AudioCodec: "opus"}
	decision := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.AudioSampleRate != 48_000 || decision.OutputSource.Info.Streams[0].SampleRate != 48_000 {
		t.Fatalf("Opus input sampling was mistaken for its playback clock: %+v", decision)
	}
	request.MaxSampleRate = profileTestPtr(24_000)
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
	request = ProgressiveAudioRequest{OutputContainer: "ogg", AudioCodec: "vorbis", AudioSampleRate: profileTestPtr(96_000), AudioBitrate: profileTestPtr(int64(192_000))}
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
	request.AudioSampleRate = profileTestPtr(8_000)
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
	request.AudioBitrate = profileTestPtr(int64(48_000))
	progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	request.AudioBitDepth = profileTestPtr(16)
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
}

func TestPlanProgressiveAudioFLACSeekRequiresEncoding(t *testing.T) {
	for _, container := range []string{"flac", "ogg"} {
		request := ProgressiveAudioRequest{OutputContainer: container, AudioCodec: "flac", StartTimeTicks: 30 * media.TicksPerSecond}
		decision := progressiveAudioTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits())
		if decision.Plan.AudioCodec != "flac" || decision.Plan.StartTicks != request.StartTimeTicks || decision.Plan.DurationTicks != 120*media.TicksPerSecond ||
			decision.OutputSource.Info.DurationTicks != 90*media.TicksPerSecond {
			t.Fatalf("FLAC seek retained stale copied stream information or wrong timeline: %+v", decision)
		}
		request.AudioCodec = "copy"
		progressiveAudioTestDeclined(t, progressiveAudioTestSource(), request, conversionTestLimits())
	}
}

func TestPlanProgressiveAudioCopyKeepsMediaBitrateSeparateFromContainerOverhead(t *testing.T) {
	request := ProgressiveAudioRequest{OutputContainer: "aac", AudioCodec: "copy", AudioStreamIndex: profileTestPtr(9),
		StartTimeTicks: 60 * media.TicksPerSecond, MaxBitrate: profileTestPtr(int64(64_000))}
	source := progressiveAudioTestSource()
	source.Info.Container, source.Path = "aac", "/media/audio.aac"
	decision := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	if decision.Method != "DirectStream" || decision.OutputSource.Info.Bitrate != 64_000 {
		t.Fatalf("source container size was substituted for the declared media rate: %+v", decision)
	}
	source.Info.Size = 0
	progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	request.MaxBitrate = profileTestPtr(int64(63_999))
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
}

func TestPlanProgressiveAudioShortMP3RetainsRequested128KMediaRate(t *testing.T) {
	for _, start := range []int64{0, 2 * media.TicksPerSecond} {
		for _, exact := range []*int64{nil, profileTestPtr(int64(128_000))} {
			source := progressiveAudioTestSource()
			source.Info.DurationTicks = 6 * media.TicksPerSecond
			request := ProgressiveAudioRequest{OutputContainer: "mp3", AudioCodec: "mp3", StartTimeTicks: start,
				AudioBitrate: exact, MaxBitrate: profileTestPtr(int64(128_000))}
			decision := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
			if decision.Plan.AudioBitrate != 128_000 || decision.OutputSource.Info.Bitrate != 128_000 ||
				decision.OutputSource.Info.DurationTicks != 6*media.TicksPerSecond-start || decision.OutputSource.Info.Size != 0 {
				t.Fatalf("short audio lost bitrate to an invented startup-bandwidth charge: %+v", decision)
			}
		}
	}
}

func TestPlanProgressiveAudioWAVCopyRequiresNativePCMFormat(t *testing.T) {
	source := progressiveAudioTestSource()
	source.Info.Container, source.Path = "wav", "/media/audio.wav"
	source.Info.Streams = []media.Stream{source.Info.Streams[1]}
	source.Info.Streams[0].Index, source.Info.Size = 0, 23_040_044
	source.Info.Streams[0].Codec, source.Info.Streams[0].BitDepth, source.Info.Streams[0].Bitrate = "pcm_s16le", 16, 1_536_000
	request := ProgressiveAudioRequest{OutputContainer: "wav", AudioCodec: "copy"}
	decision := progressiveAudioTestPlan(t, source, request, ConversionLimits{AllowRemux: true})
	if decision.Plan.AudioCodec != "copy" || decision.Plan.AudioSourceSampleRate != 48_000 || decision.OutputSource.Info.Bitrate != 1_536_000 {
		t.Fatalf("native WAV copy lost its source-format facts: %+v", decision)
	}
	source.Info.Streams[0].Bitrate, source.Info.Streams[0].BitDepth = 0, 0
	request.AudioBitrate, request.AudioBitDepth = profileTestPtr(int64(1_536_000)), profileTestPtr(16)
	decision = progressiveAudioTestPlan(t, source, request, ConversionLimits{AllowRemux: true})
	if decision.Plan.AudioCodec != "copy" || decision.OutputSource.Info.Streams[0].Bitrate != 1_536_000 || decision.OutputSource.Info.Streams[0].BitDepth != 16 {
		t.Fatalf("known PCM codec dimensions were not used to satisfy exact copy targets: %+v", decision)
	}
	source.Info.Size, source.Info.DurationTicks = 5_529_600_068, 8*60*60*media.TicksPerSecond
	request.StartTimeTicks = source.Info.DurationTicks - 60*media.TicksPerSecond
	progressiveAudioTestPlan(t, source, request, ConversionLimits{AllowRemux: true})
	request.StartTimeTicks = 0
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
	source.Info.Size, source.Info.DurationTicks = 23_040_044, 120*media.TicksPerSecond
	source.Info.Container, source.Path = "matroska", "/media/audio.mka"
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
	request.AudioCodec = "pcm_s16le"
	decision = progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.AudioCodec != "pcm_s16le" {
		t.Fatalf("unverified WAV framing was copied: %+v", decision)
	}
}

func TestPlanProgressiveAudioADTSRequiresVerifiedCopyFraming(t *testing.T) {
	source := progressiveAudioTestSource()
	source.Info.Container, source.Path = "mov,mp4,m4a", "/media/audio.m4a"
	request := ProgressiveAudioRequest{OutputContainer: "aac", AudioCodec: "aac", AudioStreamIndex: profileTestPtr(9)}
	decision := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.AudioCodec != "aac" {
		t.Fatalf("AAC profile name was incorrectly treated as proof of ADTS framing: %+v", decision.Plan)
	}
	request.AudioCodec = "copy"
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
	source.Info.Container, source.Path = "aac", "/media/audio.aac"
	decision = progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.AudioCodec != "copy" {
		t.Fatalf("verified ADTS framing was not accepted for a matching copy: %+v", decision.Plan)
	}
}

func TestPlanProgressiveAudioWAVDeclinesOutputBeyondRIFFLength(t *testing.T) {
	source := progressiveAudioTestSource()
	source.Info.DurationTicks = 24 * 60 * 60 * media.TicksPerSecond
	request := ProgressiveAudioRequest{OutputContainer: "wav", AudioCodec: "pcm_s16le", AudioSampleRate: profileTestPtr(48_000), AudioChannels: profileTestPtr(2)}
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
}

func TestPlanProgressiveAudioMalformedAndUnsupportedInputsRemainDistinct(t *testing.T) {
	for _, mutate := range []func(*ProgressiveAudioRequest){
		func(request *ProgressiveAudioRequest) { request.OutputContainer = "" },
		func(request *ProgressiveAudioRequest) { request.AudioCodec = "aac,mp3" },
		func(request *ProgressiveAudioRequest) { request.OutputContainer = "../mp3" },
		func(request *ProgressiveAudioRequest) { request.StartTimeTicks = -1 },
		func(request *ProgressiveAudioRequest) { request.AudioChannels = profileTestPtr(0) },
		func(request *ProgressiveAudioRequest) { request.MaxBitrate = profileTestPtr(int64(0)) },
	} {
		request := ProgressiveAudioRequest{OutputContainer: "mp3", AudioCodec: "mp3"}
		mutate(&request)
		decision, err := PlanProgressiveAudio(progressiveAudioTestSource(), request, conversionTestLimits())
		if !errors.Is(err, ErrInvalidRequest) || decision.Plan != nil {
			t.Fatalf("malformed normalized input was not an invalid request: %+v, %v", decision, err)
		}
	}
	for _, request := range []ProgressiveAudioRequest{
		{OutputContainer: "mp3", AudioCodec: "aac"}, {OutputContainer: "m4a", AudioCodec: "alac"},
		{OutputContainer: "aac", AudioCodec: "aac", AudioSampleRate: profileTestPtr(7350)},
		{OutputContainer: "aac", AudioCodec: "aac", StartTimeTicks: 120 * media.TicksPerSecond},
	} {
		progressiveAudioTestDeclined(t, progressiveAudioTestSource(), request, conversionTestLimits())
	}
}

func TestPlanProgressiveAudioSourceFactsAndExecutionBounds(t *testing.T) {
	for _, mutate := range []func(*Source){
		func(source *Source) { source.ItemType = "Movie" },
		func(source *Source) { source.Info.Streams[1].IsExternal = true },
		func(source *Source) { source.Info.Streams[1].Channels = 0 },
		func(source *Source) { source.Info.Streams[1].SampleRate = 0 },
		func(source *Source) { source.Info.DurationTicks = 0 },
		func(source *Source) { source.Info.DurationTicks = progressiveMaxDuration + 1 },
	} {
		source := progressiveAudioTestSource()
		mutate(&source)
		progressiveAudioTestDeclined(t, source, ProgressiveAudioRequest{OutputContainer: "mp3", AudioCodec: "mp3"}, conversionTestLimits())
	}
	source := progressiveAudioTestSource()
	source.Info.DurationTicks = progressiveMaxDuration
	request := ProgressiveAudioRequest{OutputContainer: "ogg", AudioCodec: "flac", AudioChannels: profileTestPtr(8), AudioSampleRate: profileTestPtr(96_000), AllowAudioStreamCopy: profileTestPtr(false)}
	limits := conversionTestLimits()
	limits.Hardware = transcode.Hardware{Decode: "cuda", Encode: "nvenc", Device: "0"}
	decision := progressiveAudioTestPlan(t, source, request, limits)
	if decision.OutputSource.Info.Bitrate <= 0 || decision.OutputSource.Info.Bitrate > 20_000_000 || decision.Plan.Hardware != (transcode.Hardware{}) {
		t.Fatalf("long multichannel audio overflowed its budget or requested video hardware: %+v", decision)
	}
}

func TestPlanProgressiveAudioRejectsNegativeSourceFacts(t *testing.T) {
	for _, mutate := range []func(*media.Stream){
		func(stream *media.Stream) { stream.Bitrate = -1 },
		func(stream *media.Stream) { stream.BitDepth = -1 },
	} {
		source := progressiveAudioTestSource()
		mutate(&source.Info.Streams[1])
		decision, err := PlanProgressiveAudio(source, ProgressiveAudioRequest{OutputContainer: "flac", AudioCodec: "flac"}, conversionTestLimits())
		if !errors.Is(err, ErrInvalidSource) || decision.Plan != nil {
			t.Fatalf("negative source facts were silently projected: %+v, %v", decision, err)
		}
	}
}

func TestPlanProgressiveAudioCarriesExactSamplesWithoutTickRoundTrip(t *testing.T) {
	source := progressiveAudioTestExactSource(289_792, 48_000)
	if source.Info.DurationTicks != 60_373_334 {
		t.Fatalf("unexpected regression fixture duration: %d", source.Info.DurationTicks)
	}
	request := ProgressiveAudioRequest{OutputContainer: "m4a", AudioCodec: "aac", AudioSampleRate: profileTestPtr(48_000),
		AudioBitrate: profileTestPtr(int64(128_000)), MaxBitrate: profileTestPtr(int64(128_000)), AllowAudioStreamCopy: profileTestPtr(false)}
	decision := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	count, err := transcode.ProgressiveOutputSamples(*decision.Plan, 48_000)
	if err != nil || count != 289_792 || decision.Plan.AudioSourceSampleCount != 289_792 || decision.Plan.AudioSourceSampleRate != 48_000 {
		t.Fatalf("exact ADTS sample count gained a sample after ticks rounding: %+v, count=%d, err=%v", decision.Plan, count, err)
	}
	source.Info.Streams[0].AudioTiming.SampleCount++
	if decision.Plan.AudioSourceSampleCount != 289_792 {
		t.Fatal("the immutable plan retained caller-owned measurement state")
	}
}

func TestPlanProgressiveAudioExactSeekQuantizesInSourceSampleDomain(t *testing.T) {
	for _, test := range []struct {
		start int64
		rate  int
		want  int64
	}{
		{0, 48_000, 289_792}, {1, 48_000, 289_791},
		{0, 44_100, 266_247}, {1, 44_100, 266_246}, {12_345, 44_100, 266_192},
	} {
		source := progressiveAudioTestExactSource(289_792, 48_000)
		request := ProgressiveAudioRequest{OutputContainer: "m4a", AudioCodec: "aac", StartTimeTicks: test.start,
			AudioSampleRate: &test.rate, AllowAudioStreamCopy: profileTestPtr(false)}
		decision := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
		count, err := transcode.ProgressiveOutputSamples(*decision.Plan, test.rate)
		if err != nil || count != test.want {
			t.Fatalf("start=%d rate=%d produced %d samples, want %d: %v", test.start, test.rate, count, test.want, err)
		}
	}
	source := progressiveAudioTestExactSource(289_792, 48_000)
	request := ProgressiveAudioRequest{OutputContainer: "m4a", AudioCodec: "aac", StartTimeTicks: source.Info.DurationTicks - 1}
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
}

func TestPlanProgressiveAudioWAVRIFFLimitUsesExactSamples(t *testing.T) {
	const samples = int64((math.MaxUint32 - progressiveWAVHeaderBytes) / 4)
	source := progressiveAudioTestExactSource(samples, 48_000)
	source.Info.Container, source.Path = "mov,mp4,m4a", "/media/exact.m4a"
	request := ProgressiveAudioRequest{OutputContainer: "wav", AudioCodec: "pcm_s16le", AudioChannels: profileTestPtr(2), AudioSampleRate: profileTestPtr(48_000)}
	decision := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	count, err := transcode.ProgressiveOutputSamples(*decision.Plan, 48_000)
	if err != nil || count != samples || count*4 > math.MaxUint32-progressiveWAVHeaderBytes {
		t.Fatalf("exact PCM output incorrectly exceeded RIFF capacity: count=%d, err=%v", count, err)
	}
	// The legacy trusted-duration fallback really does round up one more
	// sample here; it cannot use an unproven sample count to bypass its bound.
	source.Info.Streams[0].AudioTiming.Exact, source.Info.AudioDurationExact = false, false
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
}

func TestPlanProgressiveAudioFLACFrameBudgetUsesExactSamples(t *testing.T) {
	source := progressiveAudioTestExactSource(4096, 48_000)
	request := ProgressiveAudioRequest{OutputContainer: "flac", AudioCodec: "flac", AudioBitDepth: profileTestPtr(16),
		AudioChannels: profileTestPtr(2), AudioSampleRate: profileTestPtr(48_000), MaxBitrate: profileTestPtr(int64(1_586_250))}
	decision := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.AudioSourceSampleCount != 4096 || decision.OutputSource.Info.Bitrate != 1_586_250 {
		t.Fatalf("a full FLAC block was inflated by the ticks-to-samples round trip: %+v", decision)
	}
	source.Info.Streams[0].AudioTiming.Exact, source.Info.AudioDurationExact = false, false
	progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
}

func TestPlanProgressiveAudioRejectsInconsistentExactTiming(t *testing.T) {
	for _, mutate := range []func(*Source){
		func(source *Source) { source.Info.AudioDurationExact = false },
		func(source *Source) { source.Info.Streams[0].AudioTiming.SampleCount = 0 },
		func(source *Source) { source.Info.Streams[0].AudioTiming.PacketCount = 0 },
		func(source *Source) { source.Info.Streams[0].AudioTiming.StartTicks = 1 },
		func(source *Source) { source.Info.Streams[0].AudioTiming.EndTicks-- },
		func(source *Source) { source.Info.Streams[0].AudioTiming.SampleCount++ },
		func(source *Source) { source.Info.Streams[0].AudioTiming.SampleCount = math.MaxInt64 },
	} {
		source := progressiveAudioTestExactSource(289_792, 48_000)
		mutate(&source)
		progressiveAudioTestDeclined(t, source, ProgressiveAudioRequest{OutputContainer: "m4a", AudioCodec: "aac"}, conversionTestLimits())
	}
}

func TestPlanProgressiveAudioProjectionDoesNotInheritSourceMeasurements(t *testing.T) {
	source := progressiveAudioTestExactSource(289_792, 48_000)
	source.Info.AudioDurationReason, source.Info.PresentationOriginTicks = "source measurement", 17_000
	request := ProgressiveAudioRequest{OutputContainer: "aac", AudioCodec: "copy"}
	decision := progressiveAudioTestPlan(t, source, request, ConversionLimits{AllowRemux: true})
	output := decision.OutputSource.Info
	if output.AudioDurationExact || output.AudioDurationReason != "" || output.PresentationOriginTicks != 0 || output.Streams[0].AudioTiming != nil {
		t.Fatalf("planned output falsely inherited exact source measurements: %+v", output)
	}
	if source.Info.Streams[0].AudioTiming == nil || source.Info.Streams[0].AudioTiming.SampleCount != 289_792 || !source.Info.AudioDurationExact {
		t.Fatal("clearing the output measurement also changed the input")
	}
}

func TestPlanProgressiveAudioTrustedDurationWithoutMeasurementsKeepsFallback(t *testing.T) {
	source := progressiveAudioTestSource()
	source.Info.DurationTicks = 60_373_334
	request := ProgressiveAudioRequest{OutputContainer: "mp3", AudioCodec: "mp3", AudioSampleRate: profileTestPtr(48_000)}
	decision := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
	count, err := transcode.ProgressiveOutputSamples(*decision.Plan, 48_000)
	if err != nil || decision.Plan.AudioSourceSampleCount != 0 || count != 289_793 {
		t.Fatalf("the unmeasured low-level caller silently invented exact samples: count=%d plan=%+v err=%v", count, decision.Plan, err)
	}
}

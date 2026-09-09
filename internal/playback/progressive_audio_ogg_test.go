package playback

import (
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestPlanProgressiveOggSeeksUseDecodedSamplesAndPreserveCopyPermissions(t *testing.T) {
	for _, codec := range []string{"vorbis", "opus", "flac"} {
		t.Run(codec, func(t *testing.T) {
			source := progressiveAudioTestExactSource(288_000, 48_000)
			source.Info.Container, source.Path = "ogg", "/media/source.ogg"
			source.Info.Streams[0].Codec = codec
			source.Info.Streams[0].Profile = ""
			if codec == "flac" {
				source.Info.Streams[0].BitDepth = 24
			}
			request := ProgressiveAudioRequest{OutputContainer: "ogg", AudioCodec: codec}
			whole := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
			if whole.Method != "DirectStream" || whole.Plan.AudioCodec != "copy" || whole.Plan.AudioSampleSeek {
				t.Fatal("a complete compatible Ogg copy was replaced by sample filtering")
			}
			request.StartTimeTicks = 12_378_912
			seek := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
			if seek.Method != "Transcode" || seek.Plan.AudioCodec != codec || !seek.Plan.AudioSampleSeek ||
				seek.Plan.AudioSourceSampleCount != 288_000 || seek.Plan.StartTicks != request.StartTimeTicks ||
				seek.Plan.DurationTicks != 6*media.TicksPerSecond {
				t.Fatal("Ogg conversion used an unreliable page seek or lost its exact source samples")
			}
			progressiveAudioTestDeclined(t, source, request, ConversionLimits{AllowRemux: true})
			request.AudioCodec = "copy"
			progressiveAudioTestDeclined(t, source, request, conversionTestLimits())
			request.AudioCodec, request.StartTimeTicks = codec, 0
			request.AllowAudioStreamCopy = profileTestPtr(false)
			encoded := progressiveAudioTestPlan(t, source, request, conversionTestLimits())
			if !encoded.Plan.AudioSampleSeek || encoded.Plan.AudioCodec != codec {
				t.Fatal("complete Ogg encoding failed to normalize the decoded sample origin")
			}
		})
	}
}

func TestPlanAudioOggHLSTransfersExactSampleWindowToExecution(t *testing.T) {
	source := progressiveAudioTestExactSource(288_000, 48_000)
	source.Info.Container, source.Path = "ogg", "/media/source.ogg"
	source.Info.Streams[0].Codec, source.Info.Streams[0].Profile = "vorbis", ""
	request := audioProfilesTestRequest(audioProfilesTestProfile("hls", "ts", "aac"))
	request.StartTimeTicks = profileTestPtr(int64(12_378_912))
	decision := audioProfilesTestPlan(t, source, request, conversionTestLimits(), "hls", 0)
	if !decision.Plan.AudioSampleSeek || decision.Plan.AudioCodec != "aac" || decision.Plan.AudioSourceSampleCount != 288_000 ||
		decision.Plan.AudioSourceSampleRate != 48_000 || decision.Plan.StartTicks != *request.StartTimeTicks {
		t.Fatal("Ogg HLS negotiation lost the measured input sample window")
	}
	source.Info.Streams[0].AudioTiming.SampleCount--
	audioProfilesTestDeclined(t, source, request, conversionTestLimits())
	source.Info.Streams[0].AudioTiming = nil
	audioProfilesTestDeclined(t, source, request, conversionTestLimits())
}

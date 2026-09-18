package playback

import (
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestDynamicConversionKeepsUnknownDurationAndOriginalTrackIndexes(t *testing.T) {
	for _, container := range []string{"ts", "mp4"} {
		source, request := conversionTestSource(), conversionTestRequest()
		source.Info.DurationTicks, source.Info.Size, source.Info.FileChangeTimeNs = 0, 0, 0
		request.LiveStreamID = "live_owned"
		request.DeviceProfile.TranscodingProfiles[0].Container = container
		decision, err := PlanDynamicConversion(source, request, conversionTestLimits())
		if err != nil || decision.Plan == nil {
			t.Fatalf("dynamic %s planning failed: %+v, %v", container, decision, err)
		}
		plan := *decision.Plan
		if plan.SourceMode != "stream" || plan.DurationTicks != 0 || plan.StartTicks != 0 || plan.SegmentMode != "" || plan.VideoStreamIndex != 2 || plan.AudioStreamIndex != 5 || plan.VideoCodec != "copy" || plan.AudioCodec != "copy" {
			t.Fatalf("dynamic planning invented a source timeline or changed original tracks: %+v", plan)
		}
		if container == "mp4" && plan.HLS.SegmentType != "fmp4" {
			t.Fatal("MP4 profile did not select fragmented HLS")
		}
		if transcode.ValidatePlan(plan) != nil || decision.OutputSource.Info.DurationTicks != 0 || decision.OutputSource.Info.Size != 0 {
			t.Fatal("dynamic output is not executable or projects finite input facts")
		}
		if request.LiveStreamID != "live_owned" {
			t.Fatal("planning mutated caller-owned lease metadata")
		}
	}
}

func TestDynamicConversionRejectsSeekAndEnforcesPermissionAndProfileLimits(t *testing.T) {
	source, request := conversionTestSource(), conversionTestRequest()
	source.Info.DurationTicks = 0
	seek := int64(1)
	request.StartTimeTicks = &seek
	if _, err := PlanDynamicConversion(source, request, conversionTestLimits()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("nonseekable stream accepted a source offset")
	}
	request.StartTimeTicks = nil
	subtitle := 12
	request.SubtitleStreamIndex = &subtitle
	if _, err := PlanDynamicConversion(source, request, conversionTestLimits()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("stream accepted a subtitle extraction pass over its sole input")
	}
	request.SubtitleStreamIndex = nil
	illegalSubtitle := -2
	request.SubtitleStreamIndex = &illegalSubtitle
	if _, err := PlanDynamicConversion(source, request, conversionTestLimits()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("invalid negative subtitle index bypassed validation")
	}
	request.SubtitleStreamIndex = nil
	if decision, err := PlanDynamicConversion(source, request, ConversionLimits{}); err != nil || decision.Plan != nil {
		t.Fatal("dynamic source bypassed user conversion policy")
	}
	request.DeviceProfile = nil
	if decision, err := PlanDynamicConversion(source, request, conversionTestLimits()); err != nil || decision.Plan != nil {
		t.Fatal("dynamic source invented client compatibility without a profile")
	}
}

func TestDynamicConversionPlansEncodingWithinRequestedBitrate(t *testing.T) {
	source, request := conversionTestSource(), conversionTestRequest()
	source.Info.DurationTicks = 0
	request.MaxStreamingBitrate = profileTestPtr(int64(1_000_000))
	request.AllowVideoStreamCopy, request.AllowAudioStreamCopy = profileTestPtr(false), profileTestPtr(false)
	decision, err := PlanDynamicConversion(source, request, conversionTestLimits())
	if err != nil || decision.Plan == nil {
		t.Fatalf("bounded dynamic encoding was rejected: %+v, %v", decision, err)
	}
	if decision.Plan.VideoCodec != "h264" || decision.Plan.AudioCodec != "aac" || decision.OutputSource.Info.Bitrate > 1_000_000 {
		t.Fatal("dynamic encoder ignored track-copy or bitrate constraints")
	}
}

func TestDynamicOggEncodingDoesNotRequireFiniteSampleCoverage(t *testing.T) {
	source := Source{ItemID: "ogg-stream", ItemType: "Audio", Info: media.Info{Container: "ogg", Streams: []media.Stream{
		{Index: 7, CodecType: "audio", Codec: "opus", Channels: 2, SampleRate: 48000, Bitrate: 128000},
	}}}
	request := Request{DeviceProfile: &DeviceProfile{TranscodingProfiles: []TranscodingProfile{{Type: DlnaProfileTypeAudio, Container: "mp4", Protocol: "hls", AudioCodec: "aac"}}}}
	decision, err := PlanDynamicConversion(source, request, conversionTestLimits())
	if err != nil || decision.Plan == nil {
		t.Fatalf("nonseekable Ogg encoding failed: %+v, %v", decision, err)
	}
	if decision.Plan.AudioCodec != "aac" || decision.Plan.AudioStreamIndex != 7 || decision.Plan.SourceMode != "stream" || decision.Plan.AudioSampleSeek || decision.Plan.AudioSourceSampleCount != 0 {
		t.Fatal("Ogg stream plan required or invented a finite sample timeline")
	}
}

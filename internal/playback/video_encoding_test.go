package playback

import (
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestProgressiveVideoEncodingEstablishesCodecProfileAndDepth(t *testing.T) {
	for _, test := range []struct {
		codec, profile, projectedProfile, tag string
		depth                                 int
	}{
		{"hevc", "main", "Main", "hvc1", 8},
		{"hevc", "main10", "Main 10", "hvc1", 10},
		{"av1", "main", "Main", "av01", 8},
		{"av1", "main", "Main", "av01", 10},
		{"h264", "baseline", "Baseline", "avc1", 8},
	} {
		t.Run(test.codec+test.projectedProfile+strconv.Itoa(test.depth), func(t *testing.T) {
			request := progressiveVideoTestRequest()
			request.VideoCodec, request.VideoProfile, request.VideoBitDepth = test.codec, test.profile, &test.depth
			request.AllowVideoStreamCopy = profileTestPtr(false)
			decision := progressiveVideoTestPlan(t, progressiveVideoTestSource(), request, conversionTestLimits())
			plan, output := decision.Plan, decision.OutputSource.Info.Streams[0]
			if plan.VideoCodec != test.codec || plan.VideoProfile != test.profile || plan.VideoBitDepth != test.depth || plan.VideoCopyCodec != "" ||
				output.Codec != test.codec || output.Profile != test.projectedProfile || output.BitDepth != test.depth || output.CodecTagString != test.tag ||
				output.IsAVC != (test.codec == "h264") || output.Level != 0 || output.RefFrames != 0 {
				t.Fatalf("encoder requirements and projected media disagree: plan=%+v output=%+v", plan, output)
			}
			if test.depth == 10 && output.PixelFormat != "yuv420p10le" {
				t.Fatalf("10-bit output was described as an 8-bit pixel format: %+v", output)
			}
		})
	}
}

func TestProgressiveVideoRejectsContradictoryEncodingAndUnauthorizedConversion(t *testing.T) {
	request := progressiveVideoTestRequest()
	request.VideoCodec, request.VideoProfile, request.VideoBitDepth = "hevc", "main", profileTestPtr(10)
	progressiveVideoTestDeclined(t, progressiveVideoTestSource(), request, conversionTestLimits())
	request.VideoCodec, request.VideoProfile = "h264", "high"
	progressiveVideoTestDeclined(t, progressiveVideoTestSource(), request, conversionTestLimits())
	request.VideoCodec, request.VideoProfile, request.VideoBitDepth = "av1", "main", profileTestPtr(8)
	progressiveVideoTestDeclined(t, progressiveVideoTestSource(), request, ConversionLimits{AllowRemux: true})
}

func TestProgressiveVideoCopyPreservesHEVCAndAV1Facts(t *testing.T) {
	for _, codec := range []string{"hevc", "av1"} {
		source, request := progressiveVideoTestSource(), progressiveVideoTestRequest()
		source.Info.Streams[0].Codec, source.Info.Streams[0].Profile = codec, "Main"
		request.VideoCodec = codec
		decision := progressiveVideoTestPlan(t, source, request, ConversionLimits{AllowRemux: true})
		if decision.Plan.VideoCodec != "copy" || decision.Plan.VideoCopyCodec != codec || decision.Plan.VideoProfile != "" || decision.Plan.VideoBitDepth != 0 ||
			decision.OutputSource.Info.Streams[0].Codec != codec || decision.OutputSource.Info.Streams[0].IsAVC {
			t.Fatalf("copy was relabeled as H.264 or acquired encoder facts: %+v", decision)
		}
	}
}

func TestHLSNegotiationUsesSupportedContainerAndEncoderProfiles(t *testing.T) {
	for _, test := range []struct {
		codec, containers, profile, expectedContainer string
		depth                                         int
	}{
		{"hevc", "ts", "Main 10", "ts", 10},
		{"hevc", "mp4", "Main", "mp4", 8},
		{"av1", "ts,mp4", "Main", "mp4", 10},
		{"h264", "ts", "Baseline", "ts", 8},
	} {
		t.Run(test.codec+test.containers, func(t *testing.T) {
			request := conversionTestRequest()
			request.DeviceProfile.TranscodingProfiles[0].VideoCodec = test.codec
			request.DeviceProfile.TranscodingProfiles[0].Container = test.containers
			request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: test.codec, Conditions: []ProfileCondition{
				conversionRequired(ProfileConditionValueVideoBitDepth, ProfileConditionTypeEquals, map[int]string{8: "8", 10: "10"}[test.depth]),
				conversionRequired(ProfileConditionValueVideoProfile, ProfileConditionTypeEquals, test.profile),
			}}}
			decision := conversionTestPlan(t, conversionTestSource(), request, conversionTestLimits())
			if decision.Plan.VideoCodec != test.codec || decision.Plan.Container != test.expectedContainer || decision.Plan.VideoBitDepth != test.depth ||
				decision.OutputSource.Info.Streams[0].Profile != test.profile {
				t.Fatalf("profile constraints were not applied to the actual encoder: %+v", decision)
			}
		})
	}
	request := conversionTestRequest()
	request.DeviceProfile.TranscodingProfiles[0].VideoCodec = "av1"
	conversionTestDeclined(t, conversionTestSource(), request, conversionTestLimits())
}

func TestHTTPVideoProfileNegotiationSelectsTenBitAndCodecSpecificConstraints(t *testing.T) {
	profile := videoProfilesTestProfile("http", "mp4")
	profile.VideoCodec = "hevc,av1"
	request := videoProfilesTestRequest(profile)
	request.AllowVideoStreamCopy = profileTestPtr(false)
	request.DeviceProfile.CodecProfiles = []CodecProfile{
		{Type: CodecTypeVideo, Codec: "hevc", Conditions: []ProfileCondition{conversionRequired(ProfileConditionValueVideoProfile, ProfileConditionTypeEquals, "UnsupportedProfile")}},
		{Type: CodecTypeVideo, Codec: "av1", Conditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueVideoBitDepth, ProfileConditionTypeEquals, "10"),
			conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeLessThanEqual, "1280"),
		}},
	}
	decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, conversionTestLimits(), "http", 0)
	if decision.Plan.VideoCodec != "av1" || decision.Plan.VideoBitDepth != 10 || decision.Plan.Width > 1280 {
		t.Fatalf("an earlier rejected codec displaced the compatible AV1 output: %+v", decision)
	}
}

func TestHEVCProfileAliasesAndVideoRangeTypeUseMeasuredFacts(t *testing.T) {
	source := progressiveVideoTestSource()
	source.Info.Streams[0].Codec, source.Info.Streams[0].Profile = "hevc", "Main 10"
	source.Info.Streams[0].BitDepth, source.Info.Streams[0].PixelFormat = 0, "yuv420p10le"
	streams, err := selectStreams(source, Request{})
	if err != nil {
		t.Fatal(err)
	}
	facts := conditionFacts{source: source, streams: streams}
	for _, profile := range []string{"main10", "Main 10"} {
		if got := evaluateCondition(conversionRequired(ProfileConditionValueVideoProfile, ProfileConditionTypeEquals, profile), facts); got != conditionPass {
			t.Fatalf("equivalent HEVC profile %q did not match: %v", profile, got)
		}
	}
	condition := conversionRequired(ProfileConditionValueVideoRangeType, ProfileConditionTypeEquals, "SDR")
	if got := evaluateCondition(condition, facts); got != conditionPass {
		t.Fatalf("known SDR range did not satisfy VideoRangeType: %v", got)
	}
	source.Info.Streams[0].VideoRangeKnown = false
	if got := evaluateCondition(condition, facts); got != conditionUnknown {
		t.Fatalf("an unknown range acquired a fabricated SDR declaration: %v", got)
	}
}

func TestProgressiveVideoBurnUsesSelectedTrackIdentityAndOffset(t *testing.T) {
	source, request := progressiveVideoTestSource(), progressiveVideoTestRequest()
	source.Info.Streams[3].IsExternal, source.Info.Streams[3].Codec, source.Info.Streams[3].SubtitleTag = true, "ass", strings.Repeat("a", 64)
	request.VideoCodec, request.BurnSubtitles, request.SubtitleStreamIndex = "hevc", true, profileTestPtr(12)
	request.SubtitleOffsetTicks = media.TicksPerSecond
	decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.Subtitle.Mode != "burn" || decision.Plan.Subtitle.StreamIndex != 12 || decision.Plan.Subtitle.ExternalTag != source.Info.Streams[3].SubtitleTag ||
		decision.Plan.Subtitle.OffsetTicks != media.TicksPerSecond || !transcode.VideoEncodingSupported(decision.Plan.VideoCodec) {
		t.Fatalf("progressive subtitle identity or offset was lost: %+v", decision.Plan)
	}
	progressiveVideoTestDeclined(t, source, request, ConversionLimits{AllowRemux: true, AllowAudioTranscode: true})
	source.Info.Streams[3].SubtitleTag = ""
	progressiveVideoTestDeclined(t, source, request, conversionTestLimits())
}

func TestVideoProfileCopySeekUsesSourceTimestampsWithoutCustomClientFlags(t *testing.T) {
	source := progressiveVideoCopySeekTestSource()
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.StartTimeTicks = profileTestPtr(int64(25_000_000))
	decision := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	if decision.Plan.VideoCodec != "copy" || decision.Plan.StartTicks != 20_000_000 || !decision.Plan.CopyTimestamps || decision.Plan.VideoCopySeekCandidate == "" {
		t.Fatalf("default client negotiation failed to preserve the aligned source clock: %+v", decision.Plan)
	}
	if decision.OutputSource.Info.DurationTicks != source.Info.DurationTicks {
		t.Fatal("source-timestamp output lost the original clock endpoint")
	}
	request.AllowVideoSeekAlignment = profileTestPtr(false)
	precise := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	if precise.Plan.VideoCodec != "h264" || precise.Plan.StartTicks != *request.StartTimeTicks || precise.Plan.CopyTimestamps {
		t.Fatal("an explicit exact seek restriction was ignored")
	}
	request.AllowVideoSeekAlignment = nil
	request.DeviceProfile.TranscodingProfiles[0].CopyTimestamps = profileTestPtr(false)
	precise = videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	if precise.Plan.VideoCodec != "h264" || precise.Plan.StartTicks != *request.StartTimeTicks || precise.Plan.CopyTimestamps {
		t.Fatal("a client refusing source timestamps received an aligned response")
	}
}

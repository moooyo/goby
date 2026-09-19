package playback

import (
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestModernVideoDepthConditionsUseProbeShapedDecodedFormats(t *testing.T) {
	for _, codec := range []string{"hevc", "av1"} {
		for _, depth := range []int{8, 10} {
			t.Run(codec+"/"+strconv.Itoa(depth), func(t *testing.T) {
				source := progressiveVideoTestSource()
				video := &source.Info.Streams[0]
				video.Codec, video.BitDepth, video.PixelFormat, video.Profile = codec, 0, "yuv420p", "Main"
				if depth == 10 {
					video.PixelFormat = "yuv420p10le"
					if codec == "hevc" {
						video.Profile = "Main 10"
					}
				}
				profile := videoProfilesTestProfile("http", "mp4")
				profile.VideoCodec = codec
				request := videoProfilesTestRequest(profile)
				request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: codec, Conditions: []ProfileCondition{
					conversionRequired(ProfileConditionValueVideoBitDepth, ProfileConditionTypeEquals, strconv.Itoa(depth)),
				}}}
				decision := videoProfilesTestPlan(t, source, request, ConversionLimits{AllowRemux: true}, "http", 0)
				if decision.Plan.VideoCodec != "copy" || decision.Output.ClientMustValidate || decision.OutputSource.Info.Streams[0].BitDepth != 0 || video.BitDepth != 0 {
					t.Fatal("copy negotiation lost proved pixel precision or fabricated a sample report")
				}
				direct := progressiveVideoTestRequest()
				direct.VideoCodec, direct.VideoBitDepth = codec, &depth
				copied := progressiveVideoTestPlan(t, source, direct, ConversionLimits{AllowRemux: true})
				if copied.Plan.VideoCodec != "copy" || copied.Plan.VideoBitDepth != 0 {
					t.Fatal("an exact copy precision request became an encoder operation")
				}
				direct.VideoBitDepth = profileTestPtr(18 - depth)
				progressiveVideoTestDeclined(t, source, direct, ConversionLimits{AllowRemux: true})
				request.DeviceProfile.TranscodingProfiles[0].Protocol = "hls"
				hls := conversionTestPlan(t, source, request, ConversionLimits{AllowRemux: true})
				if hls.Plan.VideoCodec != "copy" || hls.OutputSource.Info.Streams[0].BitDepth != 0 {
					t.Fatal("HLS copy did not use the same reported-versus-effective depth contract")
				}
			})
		}
	}
}

func TestModernVideoConflictingDepthCannotAuthorizeCopyOrProcessing(t *testing.T) {
	for _, codec := range []string{"hevc", "av1"} {
		source := progressiveVideoTestSource()
		video := &source.Info.Streams[0]
		video.Codec, video.BitDepth, video.PixelFormat = codec, 8, "yuv420p10le"
		streams, err := selectStreams(source, Request{})
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range []string{"8", "10"} {
			condition := conversionRequired(ProfileConditionValueVideoBitDepth, ProfileConditionTypeEquals, required)
			if state := evaluateCondition(condition, conditionFacts{source: source, streams: streams}); state != conditionUnknown {
				t.Fatalf("contradictory precision was treated as a verified value: %s: %v", required, state)
			}
		}
		request := progressiveVideoTestRequest()
		request.VideoCodec, request.VideoBitDepth = codec, profileTestPtr(8)
		progressiveVideoTestDeclined(t, source, request, conversionTestLimits())
		if filters, reason := videoProcessingPlan(*video); reason == nil || filters != (transcode.VideoFilters{}) {
			t.Fatal("GPU processing silently selected one contradictory precision declaration")
		}
		if video.BitDepth != 8 || media.EffectiveVideoBitDepth(*video) != 8 {
			t.Fatal("reported precision was overwritten while rejecting its conflict")
		}
	}
}

func TestModernMissingSampleDepthStillSelectsTenBitGPUInput(t *testing.T) {
	for _, codec := range []string{"hevc", "av1"} {
		for _, format := range []string{"yuv420p10le", "p010le"} {
			source := media.Stream{CodecType: "video", Codec: codec, PixelFormat: format, VideoRange: "SDR", VideoRangeKnown: true, InterlaceKnown: true}
			filters, reason := videoProcessingPlan(source)
			if reason != nil || filters.SourceBitDepth != 10 || filters.ToneMap != "" || source.BitDepth != 0 {
				t.Fatalf("explicit decoder format did not select the ten-bit GPU transfer: %+v, %+v", filters, reason)
			}
			source.VideoRangeKnown = false
			if _, reason := videoProcessingPlan(source); reason == nil {
				t.Fatal("ten-bit pixel precision was incorrectly promoted to a known SDR range")
			}
		}
	}
	vision := dolbyVisionSource(5, 0)
	vision.BitDepth = 0
	if filters, reason := videoProcessingPlan(vision); reason != nil || filters.SourceBitDepth != 10 || filters.ToneMap != "dolbyvision" {
		t.Fatalf("verified Dolby Vision relied on an absent bits_per_raw_sample report: %+v, %+v", filters, reason)
	}
}

package playback

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestCodecConfigurationsPrioritizeOfferedSoftwareFormatsAcrossProtocols(t *testing.T) {
	for _, protocol := range []string{"hls", "http"} {
		t.Run(protocol, func(t *testing.T) {
			container := "ts"
			if protocol == "http" {
				container = "mp4"
			}
			profile := videoProfilesTestProfile(protocol, container)
			profile.VideoCodec = "h264,hevc"
			request := videoProfilesTestRequest(profile)
			request.AllowVideoStreamCopy = profileTestPtr(false)
			limits := conversionTestLimits()
			limits.CodecConfigurations = transcode.DefaultCodecConfigurations()
			limits.CodecConfigurations[1].Priority = 100
			decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, limits, protocol, 0)
			if decision.Plan.VideoCodec != "hevc" || decision.OutputSource.Info.Streams[0].Codec != "hevc" {
				t.Fatalf("the higher-priority offered encoder was not selected and projected: %+v", decision)
			}
			limits.CodecConfigurations[1].IsEnabled = false
			decision = videoProfilesTestPlan(t, videoProfilesTestSource(), request, limits, protocol, 0)
			if decision.Plan.VideoCodec != "h264" || decision.OutputSource.Info.Streams[0].Codec != "h264" {
				t.Fatalf("a disabled preferred encoder hid the next offered encoder: %+v", decision)
			}
		})
	}
}

func TestCodecConfigurationsPreserveCopyAndCannotIntroduceUnofferedEncoder(t *testing.T) {
	profile := videoProfilesTestProfile("hls", "ts")
	request := videoProfilesTestRequest(profile)
	limits := conversionTestLimits()
	limits.CodecConfigurations = transcode.DefaultCodecConfigurations()
	limits.CodecConfigurations[0].IsEnabled = false
	limits.CodecConfigurations[1].Priority = 1000
	decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, limits, "hls", 0)
	if decision.Plan.VideoCodec != "copy" || decision.Method != "DirectStream" {
		t.Fatalf("codec configuration removed an authorized copy candidate: %+v", decision)
	}
	request.AllowVideoStreamCopy = profileTestPtr(false)
	decision = videoProfilesTestDeclined(t, videoProfilesTestSource(), request, limits)
	found := false
	for _, reason := range decision.Reasons {
		found = found || reason.Code == "conversion_encoder_disabled"
	}
	if !found {
		t.Fatalf("unoffered HEVC bypassed disabled H.264 instead of an explained refusal: %+v", decision)
	}
}

func TestCodecConfigurationsPreserveOutputGroupsAndCallerOrdering(t *testing.T) {
	profile := videoProfilesTestProfile("hls", "ts,mp4")
	profile.VideoCodec, profile.AudioCodec = "h264,hevc", "aac,mp3"
	configurations := transcode.DefaultCodecConfigurations()
	configurations[1].Priority = 100
	candidates := conversionEncodingCandidates(profile, DlnaProfileTypeVideo, false, false)
	before := append([]conversionEncodingCandidate(nil), candidates...)
	ordered := orderConversionEncodingCandidates(candidates, configurations, transcode.Hardware{})
	if !reflect.DeepEqual(candidates, before) {
		t.Fatal("encoder priority mutated caller-owned candidates")
	}
	for index, candidate := range ordered {
		if candidate.profile.Container != before[index].profile.Container || candidate.profile.AudioCodec != before[index].profile.AudioCodec {
			t.Fatal("encoder priority reordered container or audio preference")
		}
		if index == 0 || candidate.profile.Container != ordered[index-1].profile.Container || candidate.profile.AudioCodec != ordered[index-1].profile.AudioCodec {
			if candidate.video.codec != "hevc" {
				t.Fatalf("encoder priority was not applied within the output group: %+v", candidate)
			}
		}
	}
	formats := videoEncodingFormats("h264,av1,hevc", "mp4")
	orderedFormats := orderVideoEncodingFormats(formats, configurations, transcode.Hardware{})
	for index, format := range formats {
		if format.codec == "av1" && orderedFormats[index] != format {
			t.Fatal("native encoder priority moved an unmanaged format")
		}
	}
	if orderedHardware := orderConversionEncodingCandidates(candidates, configurations, transcode.Hardware{Encode: "vaapi"}); !reflect.DeepEqual(orderedHardware, candidates) {
		t.Fatal("software encoder priority reordered hardware encoders")
	}
}

package playback

import (
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestCodecProfileLimitsApplyUpperBoundsWithoutForcingClientProfiles(t *testing.T) {
	for _, protocol := range []string{"hls", "http"} {
		t.Run(protocol, func(t *testing.T) {
			container := "ts"
			if protocol == "http" {
				container = "mp4"
			}
			request := videoProfilesTestRequest(videoProfilesTestProfile(protocol, container))
			request.AllowVideoStreamCopy = profileTestPtr(false)
			for _, maximum := range []string{"baseline", "main", "high"} {
				t.Run(maximum, func(t *testing.T) {
					limits := conversionTestLimits()
					limits.CodecProfileLimits = transcode.CodecProfileLimits{H264: maximum, HEVC: "main10"}
					selected := videoProfilesTestPlan(t, videoProfilesTestSource(), request, limits, protocol, 0)
					if selected.Plan.VideoProfile != maximum || selected.Plan.VideoBitDepth != 8 ||
						selected.OutputSource.Info.Streams[0].Profile != videoEncodingProfileName(maximum) {
						t.Fatalf("the selected and projected profile exceeded or ignored the new admission limit: %+v", selected)
					}
				})
			}
			limits := conversionTestLimits()
			limits.CodecProfileLimits = transcode.DefaultCodecProfileLimits()
			request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{
				conversionRequired(ProfileConditionValueVideoProfile, ProfileConditionTypeEquals, "Baseline"),
			}}}
			selected := videoProfilesTestPlan(t, videoProfilesTestSource(), request, limits, protocol, 0)
			if selected.Plan.VideoProfile != "baseline" {
				t.Fatal("an upper bound forced its highest profile instead of the lower client-compatible profile")
			}
			baseline := *selected.Plan
			limits.CodecProfileLimits = transcode.CodecProfileLimits{H264: "main", HEVC: "main"}
			selected = videoProfilesTestPlan(t, videoProfilesTestSource(), request, limits, protocol, 0)
			if !reflect.DeepEqual(*selected.Plan, baseline) {
				t.Fatal("inactive profile preferences changed the identity of the same accepted Baseline output")
			}
			request.DeviceProfile.CodecProfiles[0].Conditions[0].Value = "High"
			limits.CodecProfileLimits.H264 = "baseline"
			videoProfilesTestDeclined(t, videoProfilesTestSource(), request, limits)
		})
	}
}

func TestCodecProfileLimitsRestrictTenBitHEVCWithoutDiscardingMain(t *testing.T) {
	for _, protocol := range []string{"hls", "http"} {
		t.Run(protocol, func(t *testing.T) {
			container := "ts"
			if protocol == "http" {
				container = "mp4"
			}
			profile := videoProfilesTestProfile(protocol, container)
			profile.VideoCodec = "hevc"
			request := videoProfilesTestRequest(profile)
			request.AllowVideoStreamCopy = profileTestPtr(false)
			request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "hevc", Conditions: []ProfileCondition{
				conversionRequired(ProfileConditionValueVideoProfile, ProfileConditionTypeEquals, "Main 10"),
				conversionRequired(ProfileConditionValueVideoBitDepth, ProfileConditionTypeEquals, "10"),
			}}}
			limits := conversionTestLimits()
			limits.CodecProfileLimits = transcode.DefaultCodecProfileLimits()
			selected := videoProfilesTestPlan(t, videoProfilesTestSource(), request, limits, protocol, 0)
			if selected.Plan.VideoProfile != "main10" || selected.Plan.VideoBitDepth != 10 ||
				selected.OutputSource.Info.Streams[0].Profile != "Main 10" || selected.OutputSource.Info.Streams[0].BitDepth != 10 {
				t.Fatal("Main 10 was allowed without its concrete ten-bit encoded output")
			}
			limits.CodecProfileLimits.HEVC = "main"
			videoProfilesTestDeclined(t, videoProfilesTestSource(), request, limits)
			limits.CodecProfileLimits.HEVC = "main10"
			request.DeviceProfile.CodecProfiles[0].Conditions = []ProfileCondition{
				conversionRequired(ProfileConditionValueVideoProfile, ProfileConditionTypeEquals, "Main"),
			}
			selected = videoProfilesTestPlan(t, videoProfilesTestSource(), request, limits, protocol, 0)
			if selected.Plan.VideoProfile != "main" || selected.Plan.VideoBitDepth != 8 {
				t.Fatal("the Main 10 upper bound removed a lower, client-required Main output")
			}
		})
	}
}

func TestCodecProfileLimitsPreserveHigherProfileSourceCopies(t *testing.T) {
	for _, protocol := range []string{"hls", "http"} {
		for _, codec := range []string{"h264", "hevc"} {
			t.Run(protocol+"/"+codec, func(t *testing.T) {
				container := "ts"
				if protocol == "http" {
					container = "mp4"
				}
				source := videoProfilesTestSource()
				maximum, sourceProfile := "baseline", "High"
				if codec == "hevc" {
					maximum, sourceProfile = "main", "Main 10"
					video := &source.Info.Streams[0]
					video.Codec, video.Profile, video.BitDepth, video.PixelFormat = "hevc", sourceProfile, 10, "yuv420p10le"
					video.IsAVC, video.IsAVCKnown = false, true
					video.VideoRange, video.VideoRangeKnown = "SDR", true
				}
				profile := videoProfilesTestProfile(protocol, container)
				profile.VideoCodec = codec
				request := videoProfilesTestRequest(profile)
				request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: codec, Conditions: []ProfileCondition{
					conversionRequired(ProfileConditionValueVideoProfile, ProfileConditionTypeEquals, sourceProfile),
				}}}
				limits := conversionTestLimits()
				limits.CodecProfileLimits = transcode.DefaultCodecProfileLimits()
				if codec == "h264" {
					limits.CodecProfileLimits.H264 = maximum
				} else {
					limits.CodecProfileLimits.HEVC = maximum
				}
				selected := videoProfilesTestPlan(t, source, request, limits, protocol, 0)
				if selected.Plan.VideoCodec != "copy" || selected.Plan.VideoProfile != "" || selected.Method != "DirectStream" ||
					selected.OutputSource.Info.Streams[0].Profile != sourceProfile {
					t.Fatalf("an encoding upper bound altered an authorized higher-profile source copy: %+v", selected)
				}
			})
		}
	}
}

func TestCodecProfileLimitsResolveIndependentProgressiveEncodingWithoutChangingCopy(t *testing.T) {
	request := progressiveVideoTestRequest()
	request.AllowVideoStreamCopy = profileTestPtr(false)
	limits := conversionTestLimits()
	limits.CodecProfileLimits = transcode.CodecProfileLimits{H264: "baseline", HEVC: "main"}
	selected := progressiveVideoTestPlan(t, progressiveVideoTestSource(), request, limits)
	if selected.Plan.VideoProfile != "baseline" || selected.OutputSource.Info.Streams[0].Profile != "Baseline" {
		t.Fatal("an omitted progressive profile did not resolve within the encoding limit")
	}
	request.VideoProfile = "high"
	progressiveVideoTestDeclined(t, progressiveVideoTestSource(), request, limits)
	request.AllowVideoStreamCopy = nil
	selected = progressiveVideoTestPlan(t, progressiveVideoTestSource(), request, limits)
	if selected.Plan.VideoCodec != "copy" || selected.OutputSource.Info.Streams[0].Profile != "High" {
		t.Fatal("a profile limit was reinterpreted as a restriction on the progressive source copy")
	}
	request.AllowVideoStreamCopy = profileTestPtr(false)
	request.VideoCodec, request.VideoProfile, request.VideoBitDepth = "hevc", "main10", profileTestPtr(10)
	progressiveVideoTestDeclined(t, progressiveVideoTestSource(), request, limits)
	limits.CodecProfileLimits.HEVC = "main10"
	selected = progressiveVideoTestPlan(t, progressiveVideoTestSource(), request, limits)
	if selected.Plan.VideoProfile != "main10" || selected.Plan.VideoBitDepth != 10 {
		t.Fatal("an allowed exact Main 10 progressive output did not retain its pixel depth")
	}
	limits.CodecProfileLimits.HEVC = "main"
	request.VideoCodec, request.VideoProfile = "av1", "main"
	selected = progressiveVideoTestPlan(t, progressiveVideoTestSource(), request, limits)
	if selected.Plan.VideoCodec != "av1" || selected.Plan.VideoBitDepth != 10 {
		t.Fatal("limits for H.264 and HEVC removed an independent AV1 encoding path")
	}
}

func TestCodecProfileLimitsRetainAdmittedTupleWhileCheckingCurrentCeilings(t *testing.T) {
	source := videoProfilesTestSource()
	source.Info.DurationTicks = 0
	profile := videoProfilesTestProfile("hls", "mp4")
	request := videoProfilesTestRequest(profile)
	request.AllowVideoStreamCopy = profileTestPtr(false)
	limits := conversionTestLimits()
	limits.CodecProfileLimits = transcode.DefaultCodecProfileLimits()
	initial, err := PlanDynamicConversion(source, request, limits)
	if err != nil || initial.Plan == nil || initial.Plan.VideoProfile != "high" {
		t.Fatalf("the fixture did not admit a concrete high-profile dynamic output: %+v, %v", initial, err)
	}
	plan := *initial.Plan
	limits.CodecProfileLimits.H264 = "baseline"
	limits.AdmittedVideoEncoding = AdmittedVideoEncoding{Codec: plan.VideoCodec, Profile: plan.VideoProfile, BitDepth: plan.VideoBitDepth,
		Container: plan.Container, Width: plan.Width, Height: plan.Height}
	retained, err := PlanDynamicConversion(source, request, limits)
	if err != nil || retained.Plan == nil || !reflect.DeepEqual(*retained.Plan, plan) {
		t.Fatalf("a profile edit changed the output tuple of a retained revision: %+v, %v", retained, err)
	}
	limits.AdmittedVideoEncoding = AdmittedVideoEncoding{}
	fresh, err := PlanDynamicConversion(source, request, limits)
	if err != nil || fresh.Plan == nil || fresh.Plan.VideoProfile != "baseline" {
		t.Fatalf("a new dynamic admission did not use the changed profile limit: %+v, %v", fresh, err)
	}
	limits.AdmittedVideoEncoding = AdmittedVideoEncoding{Codec: plan.VideoCodec, Profile: plan.VideoProfile, BitDepth: plan.VideoBitDepth,
		Container: plan.Container, Width: plan.Width, Height: plan.Height}
	limits.MaxWidth = plan.Width - 2
	retained, err = PlanDynamicConversion(source, request, limits)
	if err != nil || retained.Plan != nil {
		t.Fatal("retained profile policy bypassed a stricter current output ceiling")
	}
	limits.MaxWidth = plan.Width
	limits.AllowVideoTranscode = false
	retained, err = PlanDynamicConversion(source, request, limits)
	if err != nil || retained.Plan != nil {
		t.Fatal("retained profile policy bypassed revoked video encoding permission")
	}
}

func TestCodecProfileLimitsRejectIncompletePlannerPolicy(t *testing.T) {
	limits := conversionTestLimits()
	limits.CodecProfileLimits = transcode.CodecProfileLimits{H264: "baseline"}
	request := videoProfilesTestRequest(videoProfilesTestProfile("hls", "ts"))
	if _, err := PlanVideoConversion(videoProfilesTestSource(), request, limits); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("a partially supplied internal profile limit became a silent default")
	}
}

package playback

import (
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func videoEncodingReprojectionFixture(t *testing.T, protocol string, profile *DeviceProfile, adaptive bool) (ConversionDecision, Request) {
	t.Helper()
	source := videoProfilesTestSource()
	request := videoProfilesTestRequest(videoProfilesTestProfile("hls", "mp4"))
	if protocol == "http" {
		request.DeviceProfile.TranscodingProfiles[0].Protocol = "http"
	}
	request.DeviceProfile.TranscodingProfiles[0].VideoCodec = "hevc"
	request.DeviceProfile.TranscodingProfiles[0].EnableAdaptiveBitrate = profileTestPtr(adaptive)
	if profile != nil {
		request.DeviceProfile.CodecProfiles = profile.CodecProfiles
	}
	request.AllowVideoStreamCopy = profileTestPtr(false)
	limits := conversionTestLimits()
	limits.Hardware = transcode.Hardware{Encode: "vaapi", Device: "/dev/dri/renderD128"}
	planner := PlanVideoConversion
	if protocol == "dynamic" {
		planner = PlanDynamicConversion
		source.Info.DurationTicks, source.Info.Size = 0, 0
	}
	decision, err := planner(source, request, limits)
	if err != nil || decision.Plan == nil || decision.Plan.Hardware.Encode != "vaapi" || decision.OutputSource.Info.Streams[0].CodecTag != "hev1" {
		t.Fatalf("fixture did not establish an accepted VAAPI HEVC output: %+v, %v", decision, err)
	}
	return decision, request
}

func TestReprojectVideoEncodingOutputRechecksHEVCTagForEveryProtocol(t *testing.T) {
	for _, protocol := range []string{"http", "hls", "dynamic"} {
		for _, constrained := range []bool{false, true} {
			name := protocol + "/unconstrained"
			if constrained {
				name = protocol + "/hev1-only"
			}
			t.Run(name, func(t *testing.T) {
				profile := &DeviceProfile{}
				if constrained {
					profile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "hevc", Container: "mp4",
						Conditions: []ProfileCondition{conversionRequired(ProfileConditionValueVideoCodecTag, ProfileConditionTypeEquals, "hev1")}}}
				}
				decision, request := videoEncodingReprojectionFixture(t, protocol, profile, false)
				before := decision.OutputSource.Info.Streams[0]
				plan := *decision.Plan
				plan.Hardware.Encode, plan.Hardware.Device = "software", ""
				if constrained {
					// Mutation after planning must not rewrite the original agreement.
					request.DeviceProfile.CodecProfiles[0].Conditions[0].Value = "hvc1"
					*request.DeviceProfile.CodecProfiles[0].Conditions[0].IsRequired = false
				}
				resolved, err := ReprojectVideoEncodingOutput(decision, plan)
				if err != nil {
					t.Fatal(err)
				}
				if decision.OutputSource.Info.Streams[0] != before {
					t.Fatal("reprojection mutated the caller-owned stream facts")
				}
				if constrained {
					if resolved.Plan != nil || resolved.Output.OriginalCompatible || len(resolved.Reasons) == 0 {
						t.Fatal("a client requiring hev1 received an hvc1 fallback")
					}
					return
				}
				video := resolved.OutputSource.Info.Streams[0]
				if resolved.Plan == nil || !resolved.Output.OriginalCompatible || !resolved.Output.ProfileMatched ||
					video.CodecTag != "hvc1" || video.CodecTagString != "hvc1" || video.Codec != before.Codec || video.BitDepth != before.BitDepth ||
					video.Width != before.Width || video.Height != before.Height || video.Profile != before.Profile {
					t.Fatal("software framing was not truthfully reprojected and accepted without changing the media contract")
				}
			})
		}
	}
}

func TestReprojectVideoEncodingOutputRechecksTagDependentAdaptiveConditions(t *testing.T) {
	profile := &DeviceProfile{CodecProfiles: []CodecProfile{{Type: CodecTypeVideo, Codec: "hevc", Container: "mp4",
		ApplyConditions: []ProfileCondition{conversionRequired(ProfileConditionValueVideoCodecTag, ProfileConditionTypeEquals, "hvc1")},
		Conditions:      []ProfileCondition{conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeGreaterThanEqual, "1280")}}}}
	decision, _ := videoEncodingReprojectionFixture(t, "hls", profile, true)
	if decision.Plan.HLS.RenditionCount < 3 || decision.Plan.Width < 1280 || decision.Plan.HLS.Renditions[decision.Plan.HLS.RenditionCount-1].Width >= 1280 {
		t.Fatal("fixture must include both allowed and disallowed widths after the tag change")
	}
	plan := *decision.Plan
	plan.Hardware.Encode, plan.Hardware.Device = "software", ""
	resolved, err := ReprojectVideoEncodingOutput(decision, plan)
	if err != nil || resolved.Plan != nil || resolved.Output.OriginalCompatible {
		t.Fatal("a lower adaptive rendition bypassed newly applicable hvc1 profile constraints")
	}
}

func TestOutputValidationSnapshotRetainsOnlyDetachedEvaluationInputs(t *testing.T) {
	request := Request{MaxStreamingBitrate: profileTestPtr(int64(8_000_000)), DeviceProfile: &DeviceProfile{
		Name: "unused-client-label", ResponseProfiles: []ResponseProfile{{MimeType: "unused-response-type"}},
		TranscodingProfiles: []TranscodingProfile{{Protocol: "hls"}},
		DirectPlayProfiles:  []DirectPlayProfile{{Type: DlnaProfileTypeVideo, VideoCodec: "hevc", Container: "mp4"}},
		CodecProfiles:       []CodecProfile{{Type: CodecTypeVideo, Conditions: []ProfileCondition{conversionRequired(ProfileConditionValueVideoCodecTag, ProfileConditionTypeEquals, "hev1")}}}}}
	snapshot := snapshotConversionOutputRequest(request)
	*request.MaxStreamingBitrate = 1
	request.DeviceProfile.DirectPlayProfiles[0].VideoCodec = "av1"
	request.DeviceProfile.CodecProfiles[0].Conditions[0].Value = "hvc1"
	*request.DeviceProfile.CodecProfiles[0].Conditions[0].IsRequired = false
	if *snapshot.MaxStreamingBitrate != 8_000_000 || snapshot.DeviceProfile.DirectPlayProfiles[0].VideoCodec != "hevc" ||
		snapshot.DeviceProfile.CodecProfiles[0].Conditions[0].Value != "hev1" || !*snapshot.DeviceProfile.CodecProfiles[0].Conditions[0].IsRequired ||
		snapshot.DeviceProfile.Name != "" || len(snapshot.DeviceProfile.ResponseProfiles) != 0 || len(snapshot.DeviceProfile.TranscodingProfiles) != 0 {
		t.Fatal("output validation retained mutable or irrelevant client request state")
	}
}

func TestReprojectVideoEncodingOutputKeepsMultitrackSubtitleView(t *testing.T) {
	for _, selected := range []int{18, -1} {
		for _, adaptive := range []bool{false, true} {
			source, request := hlsSubtitleFixture()
			request.SubtitleStreamIndex = profileTestPtr(selected)
			request.AllowVideoStreamCopy = profileTestPtr(false)
			request.DeviceProfile.TranscodingProfiles[0].Container = "mp4"
			request.DeviceProfile.TranscodingProfiles[0].VideoCodec = "hevc"
			request.DeviceProfile.TranscodingProfiles[0].EnableAdaptiveBitrate = profileTestPtr(adaptive)
			request.DeviceProfile.SubtitleProfiles[0].Container = "mp4"
			limits := conversionTestLimits()
			limits.Hardware = transcode.Hardware{Encode: "vaapi", Device: "/dev/dri/renderD128"}
			decision := conversionTestPlan(t, source, request, limits)
			decision.SubtitleView.OffsetTicks = media.TicksPerSecond
			before := decision.OutputSource.Info.Streams[0]
			plan := *decision.Plan
			plan.Hardware.Encode, plan.Hardware.Device = "software", ""
			resolved, err := ReprojectVideoEncodingOutput(decision, plan)
			if err != nil || resolved.Plan == nil || resolved.SubtitleView != decision.SubtitleView ||
				resolved.Plan.HLS.Subtitles != decision.Plan.HLS.Subtitles || resolved.Output.DefaultSubtitleStreamIndex == nil ||
				*resolved.Output.DefaultSubtitleStreamIndex != selected || resolved.Output.SubtitleMethod != decision.Output.SubtitleMethod ||
				resolved.Output.SubtitleFormat != decision.Output.SubtitleFormat {
				t.Fatalf("framing fallback changed the subtitle group or active view: selected=%d adaptive=%t resolved=%+v err=%v", selected, adaptive, resolved, err)
			}
			after := resolved.OutputSource.Info.Streams[0]
			if before.CodecTagString != "hev1" || after.CodecTagString != "hvc1" || after.Codec != before.Codec || after.Width != before.Width ||
				after.Height != before.Height || after.BitDepth != before.BitDepth || after.Profile != before.Profile || decision.OutputSource.Info.Streams[0] != before {
				t.Fatal("subtitle preservation changed the codec contract or mutated the previous projection")
			}
		}
	}
}

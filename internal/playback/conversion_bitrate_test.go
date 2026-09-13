package playback

import "testing"

type conversionBitrateCase struct {
	name        string
	source      Source
	request     Request
	property    ProfileConditionValue
	codecType   CodecType
	codec       string
	bitrateText string
	payloadCost int64
	encodedRate int64
	encodedText string
}

func conversionUnknownBitrateCases() []conversionBitrateCase {
	videoSource, videoRequest := conversionTestSource(), conversionTestRequest()
	videoSource.Info.Streams[0].Bitrate = 0
	audioSource, audioRequest := conversionTestSource(), conversionTestRequest()
	audioSource.ItemType = "Audio"
	audioSource.Info.Streams = audioSource.Info.Streams[1:2]
	audioSource.Info.Streams[0].Bitrate = 0
	audioSource.Info.Bitrate = 256_000
	audioRequest.DeviceProfile.DirectPlayProfiles = []DirectPlayProfile{{Type: DlnaProfileTypeAudio, Container: "mp4,m4a", AudioCodec: "aac"}}
	audioRequest.DeviceProfile.TranscodingProfiles = []TranscodingProfile{{Type: DlnaProfileTypeAudio, Container: "ts", Protocol: "hls", AudioCodec: "aac"}}
	return []conversionBitrateCase{
		{"video", videoSource, videoRequest, ProfileConditionValueVideoBitrate, CodecTypeVideo, "h264", "4000000", 4_192_000, 4_000_000, "4000000"},
		{"audio", audioSource, audioRequest, ProfileConditionValueAudioBitrate, CodecTypeAudio, "aac", "256000", 256_000, 192_000, "192000"},
	}
}

func TestPlanConversionCopiedUnknownBitratesUseSeparatePlanningBudgets(t *testing.T) {
	for _, test := range conversionUnknownBitrateCases() {
		t.Run(test.name, func(t *testing.T) {
			limits := ConversionLimits{AllowRemux: true}
			decision := conversionTestPlan(t, test.source, test.request, limits)
			wantBudget := (test.payloadCost*10 + 8) / 9
			if decision.Method != "DirectStream" || decision.OutputSource.Info.Streams[0].Bitrate != 0 ||
				decision.OutputSource.Info.Bitrate != wantBudget || test.source.Info.Streams[0].Bitrate != 0 {
				t.Fatalf("a copied stream acquired its source's aggregate planning bitrate: %+v", decision)
			}
			for _, owner := range []string{"request", "profile", "server"} {
				t.Run(owner, func(t *testing.T) {
					request, bounded := test.request, limits
					profile := *request.DeviceProfile
					request.DeviceProfile = &profile
					ceiling := wantBudget
					switch owner {
					case "request":
						request.MaxStreamingBitrate = &ceiling
					case "profile":
						profile.MaxStreamingBitrate = &ceiling
					case "server":
						bounded.MaxBitrate = ceiling
					}
					conversionTestPlan(t, test.source, request, bounded)
					ceiling--
					if owner == "server" {
						bounded.MaxBitrate = ceiling
					}
					conversionTestDeclined(t, test.source, request, bounded)
				})
			}
			test.source.Info.Bitrate = 0
			conversionTestDeclined(t, test.source, test.request, limits)
		})
	}
}

func TestPlanConversionRequiredCopiedBitrateConditionsNeedStreamFacts(t *testing.T) {
	for _, operator := range []ProfileConditionType{ProfileConditionTypeEquals, ProfileConditionTypeGreaterThanEqual, ProfileConditionTypeLessThanEqual} {
		for _, test := range conversionUnknownBitrateCases() {
			t.Run(test.name+"/"+string(operator), func(t *testing.T) {
				test.request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: test.codecType, Codec: test.codec, Conditions: []ProfileCondition{
					conversionRequired(test.property, operator, test.bitrateText),
				}}}
				limits := ConversionLimits{AllowRemux: true}
				decision := conversionTestDeclined(t, test.source, test.request, limits)
				conversionRequireBitrateReason(t, decision.Reasons, test.property, false)
				conversionRequireBitrateReason(t, decision.Original.Reasons, test.property, false)

				// A measured stream declaration can satisfy the same predicate.
				// Restoring that fact must preserve the lower-cost copy candidate.
				test.source.Info.Streams[0].Bitrate = test.source.Info.Bitrate
				known := conversionTestPlan(t, test.source, test.request, limits)
				if known.Method != "DirectStream" || known.Output.ClientMustValidate || known.OutputSource.Info.Streams[0].Bitrate != test.source.Info.Bitrate {
					t.Fatalf("a known copied bitrate lost its verified predicate: %+v", known)
				}
			})
		}
	}
}

func TestPlanConversionOptionalCopiedBitrateConditionsStayUnverified(t *testing.T) {
	for _, operator := range []ProfileConditionType{ProfileConditionTypeEquals, ProfileConditionTypeGreaterThanEqual, ProfileConditionTypeLessThanEqual} {
		for _, test := range conversionUnknownBitrateCases() {
			for _, required := range []*bool{nil, profileTestPtr(false)} {
				t.Run(test.name+"/"+string(operator), func(t *testing.T) {
					test.request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: test.codecType, Codec: test.codec, Conditions: []ProfileCondition{{
						Property: test.property, Condition: operator, Value: test.bitrateText, IsRequired: required,
					}}}}
					decision := conversionTestPlan(t, test.source, test.request, ConversionLimits{AllowRemux: true})
					if decision.Method != "DirectStream" || !decision.Output.ClientMustValidate || decision.OutputSource.Info.Streams[0].Bitrate != 0 {
						t.Fatalf("an optional unknown copied bitrate was claimed as verified: %+v", decision)
					}
					conversionRequireBitrateReason(t, decision.Output.Reasons, test.property, true)
				})
			}
		}
	}
}

func TestPlanConversionCopiedBitrateCannotEstablishRequiredApplicability(t *testing.T) {
	for _, test := range conversionUnknownBitrateCases() {
		t.Run(test.name, func(t *testing.T) {
			test.request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: test.codecType, Codec: test.codec, ApplyConditions: []ProfileCondition{
				conversionRequired(test.property, ProfileConditionTypeEquals, test.bitrateText),
			}, Conditions: []ProfileCondition{
				conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeLessThanEqual, "2"),
			}}}
			decision := conversionTestDeclined(t, test.source, test.request, ConversionLimits{AllowRemux: true})
			conversionRequireBitrateReason(t, decision.Reasons, test.property, false)
		})
	}
}

func TestPlanConversionEncodingEstablishesRequiredOutputBitrates(t *testing.T) {
	for _, operator := range []ProfileConditionType{ProfileConditionTypeEquals, ProfileConditionTypeGreaterThanEqual, ProfileConditionTypeLessThanEqual} {
		for _, test := range conversionUnknownBitrateCases() {
			t.Run(test.name+"/"+string(operator), func(t *testing.T) {
				test.request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: test.codecType, Codec: test.codec, Conditions: []ProfileCondition{
					conversionRequired(test.property, operator, test.encodedText),
				}}}
				decision := conversionTestPlan(t, test.source, test.request, conversionTestLimits())
				if decision.Method != "Transcode" || decision.Output.ClientMustValidate || decision.OutputSource.Info.Streams[0].Bitrate != test.encodedRate {
					t.Fatalf("a required bitrate was not established by the selected encoder: %+v", decision)
				}
				if test.name == "video" && (decision.Plan.VideoCodec != "h264" || decision.Plan.VideoBitrate != test.encodedRate) ||
					test.name == "audio" && (decision.Plan.AudioCodec != "aac" || decision.Plan.AudioBitrate != test.encodedRate) {
					t.Fatalf("projected bitrate differs from the actual encoder target: %+v", decision.Plan)
				}
			})
		}
	}
}

func TestPlanConversionMixedEncodingReservesUnknownCopiedBitrateCosts(t *testing.T) {
	t.Run("copied video", func(t *testing.T) {
		source, request := conversionTestSource(), conversionTestRequest()
		source.Info.Streams[0].Bitrate = 0
		request.AllowAudioStreamCopy = profileTestPtr(false)
		request.MaxStreamingBitrate = profileTestPtr(int64(4_600_000))
		decision := conversionTestPlan(t, source, request, ConversionLimits{AllowAudioTranscode: true})
		if decision.Plan.VideoCodec != "copy" || decision.Plan.AudioCodec != "aac" || decision.Plan.AudioBitrate != 140_000 ||
			decision.OutputSource.Info.Streams[0].Bitrate != 0 || decision.OutputSource.Info.Streams[1].Bitrate != 140_000 || decision.OutputSource.Info.Bitrate != 4_600_000 {
			t.Fatalf("audio encoding did not reserve the unknown copied video's planning cost: %+v", decision)
		}
	})
	t.Run("copied audio", func(t *testing.T) {
		source, request := conversionTestSource(), conversionTestRequest()
		source.Info.Bitrate = 400_000
		source.Info.Streams[0].Bitrate, source.Info.Streams[1].Bitrate = 300_000, 0
		request.AllowVideoStreamCopy = profileTestPtr(false)
		request.MaxStreamingBitrate = profileTestPtr(int64(600_000))
		decision := conversionTestPlan(t, source, request, ConversionLimits{AllowVideoTranscode: true})
		if decision.Plan.VideoCodec != "h264" || decision.Plan.AudioCodec != "copy" || decision.Plan.VideoBitrate != 140_000 ||
			decision.OutputSource.Info.Streams[0].Bitrate != 140_000 || decision.OutputSource.Info.Streams[1].Bitrate != 0 || decision.OutputSource.Info.Bitrate != 600_000 {
			t.Fatalf("video encoding did not reserve the unknown copied audio's planning cost: %+v", decision)
		}
	})
}

func conversionRequireBitrateReason(t *testing.T, reasons []Reason, property ProfileConditionValue, optional bool) {
	t.Helper()
	code := "unknown_required_condition"
	if optional {
		code = "unverified_condition"
	}
	for _, reason := range reasons {
		if reason.Code == code && reason.Property == string(property) && reason.Unverified == optional {
			return
		}
	}
	t.Fatalf("missing %s for unknown %s: %+v", code, property, reasons)
}

package playback

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func profileTestPtr[T any](value T) *T {
	return &value
}

func profileTestSource() Source {
	return Source{
		ItemID:        "item1",
		MediaSourceID: media.SourceID("item1"),
		Path:          "/media/movie.mp4",
		ItemType:      "Movie",
		Info: media.Info{
			Container:     "mov,mp4,m4a,3gp,3g2,mj2",
			DurationTicks: 90 * media.TicksPerSecond,
			Bitrate:       4_000_000,
			Streams: []media.Stream{
				{
					Index:            2,
					Codec:            "h264",
					CodecType:        "video",
					Width:            1920,
					Height:           1080,
					Level:            41,
					Profile:          "High",
					AverageFrameRate: "30000/1001",
					Bitrate:          3_500_000,
				},
				{
					Index:      5,
					Codec:      "aac",
					CodecType:  "audio",
					IsDefault:  true,
					Channels:   2,
					SampleRate: 48_000,
					Bitrate:    192_000,
				},
				{
					Index:     9,
					Codec:     "ac3",
					CodecType: "audio",
					Channels:  6,
					Bitrate:   384_000,
				},
				{
					Index:                12,
					Codec:                "subrip",
					CodecType:            "subtitle",
					IsTextSubtitleStream: true,
				},
			},
		},
	}
}

func profileTestRequest() Request {
	return Request{
		DeviceProfile: &DeviceProfile{
			DirectPlayProfiles: []DirectPlayProfile{{
				Type:       DlnaProfileTypeVideo,
				Container:  "mp4",
				VideoCodec: "h264",
				AudioCodec: "aac",
			}},
		},
	}
}

func profileTestDecision(t *testing.T, source Source, request Request, wantDirectPlay bool) Decision {
	t.Helper()
	decision, err := Evaluate(source, request)
	if err != nil {
		t.Fatalf("Evaluate() returned an unexpected error: %v", err)
	}
	if decision.DirectPlay != wantDirectPlay {
		t.Fatalf("Evaluate().DirectPlay = %v, want %v; decision: %+v", decision.DirectPlay, wantDirectPlay, decision)
	}
	if request.EnableDirectPlay == nil && request.EnableDirectStream == nil && decision.DirectStream != wantDirectPlay {
		t.Fatalf("Evaluate().DirectStream = %v, want %v; decision: %+v", decision.DirectStream, wantDirectPlay, decision)
	}
	if !decision.DirectPlay && !decision.DirectStream && len(decision.Reasons) == 0 {
		t.Fatal("a rejected playback decision must explain why it was rejected")
	}
	return decision
}

func TestEvaluateWithoutProfileRequiresClientValidation(t *testing.T) {
	decision := profileTestDecision(t, profileTestSource(), Request{}, true)
	if decision.ProfileEvaluated || !decision.ClientMustValidate {
		t.Fatalf("missing client capabilities must remain unverified: %+v", decision)
	}
	if decision.DefaultAudioStreamIndex == nil || *decision.DefaultAudioStreamIndex != 5 {
		t.Fatalf("default audio selection must use stream index 5: %+v", decision)
	}
	if decision.DefaultSubtitleStreamIndex == nil || *decision.DefaultSubtitleStreamIndex != -1 {
		t.Fatalf("subtitles must be disabled by default: %+v", decision)
	}
	if decision.RequiresAudioTrackSelection {
		t.Fatalf("the default audio track must not require a track change: %+v", decision)
	}
}

func TestEvaluateExplicitDirectPlayDisableCannotBeOverridden(t *testing.T) {
	for _, withProfile := range []bool{false, true} {
		name := "without profile"
		request := Request{}
		if withProfile {
			name = "with profile"
			request = profileTestRequest()
		}
		t.Run(name, func(t *testing.T) {
			request.EnableDirectPlay = profileTestPtr(false)
			request.EnableDirectStream = profileTestPtr(true)
			request.EnableTranscoding = profileTestPtr(true)
			decision := profileTestDecision(t, profileTestSource(), request, false)
			if !decision.DirectStream {
				t.Fatalf("disabling DirectPlay must not disable independent DirectStream support: %+v", decision)
			}
		})
	}
}

func TestEvaluateDirectPlaybackFlagsAreIndependent(t *testing.T) {
	for _, test := range []struct {
		name             string
		directPlay       *bool
		directStream     *bool
		wantDirectPlay   bool
		wantDirectStream bool
	}{
		{"both default to enabled", nil, nil, true, true},
		{"only direct play disabled", profileTestPtr(false), nil, false, true},
		{"only direct stream disabled", nil, profileTestPtr(false), true, false},
		{"only direct play enabled", profileTestPtr(true), profileTestPtr(false), true, false},
		{"only direct stream enabled", profileTestPtr(false), profileTestPtr(true), false, true},
		{"both disabled", profileTestPtr(false), profileTestPtr(false), false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := profileTestRequest()
			request.EnableDirectPlay, request.EnableDirectStream = test.directPlay, test.directStream
			decision := profileTestDecision(t, profileTestSource(), request, test.wantDirectPlay)
			if decision.DirectStream != test.wantDirectStream {
				t.Fatalf("DirectStream = %v, want %v; decision: %+v", decision.DirectStream, test.wantDirectStream, decision)
			}
		})
	}
}

func TestEvaluateMatchesTypeContainerAndSelectedCodecs(t *testing.T) {
	for _, test := range []struct {
		name    string
		profile DirectPlayProfile
		want    bool
	}{
		{"matching video", DirectPlayProfile{Type: DlnaProfileTypeVideo, Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"}, true},
		{"container and codec lists", DirectPlayProfile{Type: DlnaProfileTypeVideo, Container: "mkv,mp4", VideoCodec: "hevc,h264", AudioCodec: "ac3,aac"}, true},
		{"wrong media type", DirectPlayProfile{Type: DlnaProfileTypeAudio, Container: "mp4", AudioCodec: "aac"}, false},
		{"wrong container", DirectPlayProfile{Type: DlnaProfileTypeVideo, Container: "mkv", VideoCodec: "h264", AudioCodec: "aac"}, false},
		{"wrong video codec", DirectPlayProfile{Type: DlnaProfileTypeVideo, Container: "mp4", VideoCodec: "hevc", AudioCodec: "aac"}, false},
		{"only unselected audio is supported", DirectPlayProfile{Type: DlnaProfileTypeVideo, Container: "mp4", VideoCodec: "h264", AudioCodec: "ac3"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := profileTestRequest()
			request.DeviceProfile.DirectPlayProfiles = []DirectPlayProfile{test.profile}
			decision := profileTestDecision(t, profileTestSource(), request, test.want)
			if !decision.ProfileEvaluated || decision.ClientMustValidate {
				t.Fatalf("a supplied device profile must be evaluated: %+v", decision)
			}
		})
	}

	t.Run("empty capabilities are not unrestricted", func(t *testing.T) {
		profileTestDecision(t, profileTestSource(), Request{DeviceProfile: &DeviceProfile{}}, false)
	})
	t.Run("any direct-play profile may match", func(t *testing.T) {
		request := profileTestRequest()
		request.DeviceProfile.DirectPlayProfiles = append([]DirectPlayProfile{{
			Type: DlnaProfileTypeVideo, Container: "mkv", VideoCodec: "hevc", AudioCodec: "opus",
		}}, request.DeviceProfile.DirectPlayProfiles...)
		profileTestDecision(t, profileTestSource(), request, true)
	})
}

func TestEvaluateAudioSelectionUsesStreamIndex(t *testing.T) {
	for _, test := range []struct {
		name        string
		index       *int
		audioCodecs string
		want        bool
		wantIndex   int
		wantChange  bool
	}{
		{"omitted uses default", nil, "aac", true, 5, false},
		{"explicit default", profileTestPtr(5), "aac", true, 5, false},
		{"supported alternate", profileTestPtr(9), "ac3", true, 9, true},
		{"alternate is not covered by default codec", profileTestPtr(9), "aac", false, 9, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := profileTestRequest()
			request.AudioStreamIndex = test.index
			request.DeviceProfile.DirectPlayProfiles[0].AudioCodec = test.audioCodecs
			decision := profileTestDecision(t, profileTestSource(), request, test.want)
			if decision.DefaultAudioStreamIndex == nil || *decision.DefaultAudioStreamIndex != test.wantIndex {
				t.Fatalf("selected audio index = %v, want %d", decision.DefaultAudioStreamIndex, test.wantIndex)
			}
			if decision.RequiresAudioTrackSelection != test.wantChange {
				t.Fatalf("RequiresAudioTrackSelection = %v, want %v", decision.RequiresAudioTrackSelection, test.wantChange)
			}
		})
	}

	t.Run("stream order does not replace default flag", func(t *testing.T) {
		source := profileTestSource()
		source.Info.Streams[1], source.Info.Streams[2] = source.Info.Streams[2], source.Info.Streams[1]
		decision := profileTestDecision(t, source, profileTestRequest(), true)
		if decision.DefaultAudioStreamIndex == nil || *decision.DefaultAudioStreamIndex != 5 {
			t.Fatalf("reordering streams changed the default selection: %+v", decision)
		}
	})
}

func TestEvaluateRejectsInvalidStreamIndices(t *testing.T) {
	for _, test := range []struct {
		name     string
		audio    *int
		subtitle *int
	}{
		{"negative audio", profileTestPtr(-1), nil},
		{"audio array position", profileTestPtr(1), nil},
		{"video index as audio", profileTestPtr(2), nil},
		{"subtitle index as audio", profileTestPtr(12), nil},
		{"missing audio", profileTestPtr(99), nil},
		{"invalid subtitle disable value", nil, profileTestPtr(-2)},
		{"subtitle array position", nil, profileTestPtr(3)},
		{"audio index as subtitle", nil, profileTestPtr(5)},
		{"missing subtitle", nil, profileTestPtr(99)},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := profileTestRequest()
			request.AudioStreamIndex = test.audio
			request.SubtitleStreamIndex = test.subtitle
			if _, err := Evaluate(profileTestSource(), request); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Evaluate() error = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestEvaluateSubtitleDelivery(t *testing.T) {
	for _, test := range []struct {
		name    string
		index   *int
		profile *SubtitleProfile
		want    bool
	}{
		{"omitted subtitles", nil, nil, true},
		{"explicitly disabled subtitles", profileTestPtr(-1), nil, true},
		{"embedded subtitles", profileTestPtr(12), &SubtitleProfile{Format: "subrip", Container: "mp4", Method: SubtitleDeliveryMethodEmbed}, true},
		{"unadvertised subtitle support", profileTestPtr(12), nil, false},
		{"wrong subtitle format", profileTestPtr(12), &SubtitleProfile{Format: "ass", Container: "mp4", Method: SubtitleDeliveryMethodEmbed}, false},
		{"wrong subtitle container", profileTestPtr(12), &SubtitleProfile{Format: "subrip", Container: "mkv", Method: SubtitleDeliveryMethodEmbed}, false},
		{"subtitle burning is unavailable", profileTestPtr(12), &SubtitleProfile{Format: "subrip", Method: SubtitleDeliveryMethodEncode}, false},
		{"embedded subtitle extraction is unavailable", profileTestPtr(12), &SubtitleProfile{Format: "subrip", Method: SubtitleDeliveryMethodExternal}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := profileTestRequest()
			request.SubtitleStreamIndex = test.index
			if test.profile != nil {
				request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{*test.profile}
			}
			decision := profileTestDecision(t, profileTestSource(), request, test.want)
			if test.want {
				wantIndex := -1
				if test.index != nil {
					wantIndex = *test.index
				}
				if decision.DefaultSubtitleStreamIndex == nil || *decision.DefaultSubtitleStreamIndex != wantIndex {
					t.Fatalf("selected subtitle index = %v, want %d", decision.DefaultSubtitleStreamIndex, wantIndex)
				}
				if wantIndex == 12 && decision.SubtitleMethod != SubtitleDeliveryMethodEmbed {
					t.Fatalf("selected subtitle method = %q, want Embed", decision.SubtitleMethod)
				}
			}
		})
	}
	t.Run("an embed candidate remains usable after unsupported methods", func(t *testing.T) {
		request := profileTestRequest()
		request.SubtitleStreamIndex = profileTestPtr(12)
		request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{
			{Format: "subrip", Method: SubtitleDeliveryMethodEncode},
			{Format: "subrip", Method: SubtitleDeliveryMethodExternal},
			{Format: "subrip", Method: SubtitleDeliveryMethodEmbed},
		}
		decision := profileTestDecision(t, profileTestSource(), request, true)
		if decision.SubtitleMethod != SubtitleDeliveryMethodEmbed {
			t.Fatalf("selected subtitle method = %q, want Embed", decision.SubtitleMethod)
		}
	})
}

func TestEvaluateRequestLimitBoundaries(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Source, *Request)
		want   bool
	}{
		{"bitrate at the cap", func(_ *Source, r *Request) { r.MaxStreamingBitrate = profileTestPtr(int64(4_000_000)) }, true},
		{"bitrate below the cap", func(_ *Source, r *Request) { r.MaxStreamingBitrate = profileTestPtr(int64(4_000_001)) }, true},
		{"bitrate above the cap", func(_ *Source, r *Request) { r.MaxStreamingBitrate = profileTestPtr(int64(3_999_999)) }, false},
		{"unknown bitrate with a cap", func(s *Source, r *Request) {
			s.Info.Bitrate = 0
			r.MaxStreamingBitrate = profileTestPtr(int64(4_000_000))
		}, false},
		{"audio channels at the cap", func(_ *Source, r *Request) { r.MaxAudioChannels = profileTestPtr(2) }, true},
		{"audio channels above the cap", func(_ *Source, r *Request) { r.MaxAudioChannels = profileTestPtr(1) }, false},
		{"unknown audio channels with a cap", func(s *Source, r *Request) { s.Info.Streams[1].Channels = 0; r.MaxAudioChannels = profileTestPtr(2) }, false},
		{"alternate audio channels above the cap", func(_ *Source, r *Request) {
			r.AudioStreamIndex = profileTestPtr(9)
			r.MaxAudioChannels = profileTestPtr(2)
			r.DeviceProfile.DirectPlayProfiles[0].AudioCodec = "ac3"
		}, false},
		{"alternate audio channels at the cap", func(_ *Source, r *Request) {
			r.AudioStreamIndex = profileTestPtr(9)
			r.MaxAudioChannels = profileTestPtr(6)
			r.DeviceProfile.DirectPlayProfiles[0].AudioCodec = "ac3"
		}, true},
		{"device bitrate cap", func(_ *Source, r *Request) { r.DeviceProfile.MaxStreamingBitrate = profileTestPtr(int64(3_999_999)) }, false},
		{"request cannot relax the device cap", func(_ *Source, r *Request) {
			r.MaxStreamingBitrate = profileTestPtr(int64(8_000_000))
			r.DeviceProfile.MaxStreamingBitrate = profileTestPtr(int64(3_999_999))
		}, false},
		{"device cannot relax the request cap", func(_ *Source, r *Request) {
			r.MaxStreamingBitrate = profileTestPtr(int64(3_999_999))
			r.DeviceProfile.MaxStreamingBitrate = profileTestPtr(int64(8_000_000))
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, request := profileTestSource(), profileTestRequest()
			test.change(&source, &request)
			profileTestDecision(t, source, request, test.want)
		})
	}
}

func TestEvaluateRejectsInvalidRequestLimitsAndSourceSelection(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Request)
	}{
		{"zero bitrate cap", func(r *Request) { r.MaxStreamingBitrate = profileTestPtr(int64(0)) }},
		{"negative bitrate cap", func(r *Request) { r.MaxStreamingBitrate = profileTestPtr(int64(-1)) }},
		{"zero audio channel cap", func(r *Request) { r.MaxAudioChannels = profileTestPtr(0) }},
		{"negative audio channel cap", func(r *Request) { r.MaxAudioChannels = profileTestPtr(-1) }},
		{"negative start", func(r *Request) { r.StartTimeTicks = profileTestPtr(int64(-1)) }},
		{"start beyond duration", func(r *Request) { r.StartTimeTicks = profileTestPtr(90*media.TicksPerSecond + 1) }},
		{"different media source", func(r *Request) { r.MediaSourceID = media.SourceID("other-item") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := profileTestRequest()
			test.change(&request)
			if _, err := Evaluate(profileTestSource(), request); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Evaluate() error = %v, want ErrInvalidRequest", err)
			}
		})
	}

	for _, start := range []int64{0, 90 * media.TicksPerSecond} {
		request := profileTestRequest()
		request.StartTimeTicks = profileTestPtr(start)
		request.MediaSourceID = media.SourceID("item1")
		profileTestDecision(t, profileTestSource(), request, true)
	}
	t.Run("unknown duration is not zero duration", func(t *testing.T) {
		source, request := profileTestSource(), profileTestRequest()
		source.Info.DurationTicks = 0
		request.StartTimeTicks = profileTestPtr(media.TicksPerSecond)
		profileTestDecision(t, source, request, true)
	})
	t.Run("missing source is invalid", func(t *testing.T) {
		if _, err := Evaluate(Source{}, Request{}); !errors.Is(err, ErrInvalidSource) {
			t.Fatalf("Evaluate() error = %v, want ErrInvalidSource", err)
		}
	})
}

func TestEvaluateInvalidRequestsCannotBypassValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		request Request
	}{
		{"no device profile", Request{AudioStreamIndex: profileTestPtr(0)}},
		{"both delivery methods disabled", Request{AudioStreamIndex: profileTestPtr(0), EnableDirectPlay: profileTestPtr(false), EnableDirectStream: profileTestPtr(false)}},
		{"zero bitrate without a profile", Request{MaxStreamingBitrate: profileTestPtr(int64(0))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Evaluate(profileTestSource(), test.request); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Evaluate() error = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestEvaluateContainerAndCodecConditionsAreCumulative(t *testing.T) {
	for _, test := range []struct {
		name     string
		width    string
		channels string
		want     bool
	}{
		{"all conditions hold", "1920", "2", true},
		{"container condition fails", "1919", "2", false},
		{"codec condition fails", "1920", "1", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := profileTestRequest()
			request.DeviceProfile.ContainerProfiles = []ContainerProfile{{
				Type: DlnaProfileTypeVideo, Container: "mp4",
				Conditions: []ProfileCondition{{Property: ProfileConditionValueWidth, Condition: ProfileConditionTypeLessThanEqual, Value: test.width}},
			}}
			request.DeviceProfile.CodecProfiles = []CodecProfile{{
				Type: CodecTypeVideoAudio, Codec: "aac", Container: "mp4",
				Conditions: []ProfileCondition{{Property: ProfileConditionValueAudioChannels, Condition: ProfileConditionTypeLessThanEqual, Value: test.channels}},
			}}
			profileTestDecision(t, profileTestSource(), request, test.want)
		})
	}

	t.Run("a later matching profile cannot erase an earlier failure", func(t *testing.T) {
		request := profileTestRequest()
		request.DeviceProfile.ContainerProfiles = []ContainerProfile{
			{Type: DlnaProfileTypeVideo, Container: "mp4", Conditions: []ProfileCondition{{Property: ProfileConditionValueWidth, Condition: ProfileConditionTypeLessThanEqual, Value: "1280"}}},
			{Type: DlnaProfileTypeVideo, Container: "mp4", Conditions: []ProfileCondition{{Property: ProfileConditionValueHeight, Condition: ProfileConditionTypeLessThanEqual, Value: "1080"}}},
		}
		profileTestDecision(t, profileTestSource(), request, false)
	})
	t.Run("all matching codec profiles must hold", func(t *testing.T) {
		request := profileTestRequest()
		request.DeviceProfile.CodecProfiles = []CodecProfile{
			{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{{Property: ProfileConditionValueWidth, Condition: ProfileConditionTypeLessThanEqual, Value: "1280"}}},
			{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{{Property: ProfileConditionValueHeight, Condition: ProfileConditionTypeLessThanEqual, Value: "1080"}}},
		}
		profileTestDecision(t, profileTestSource(), request, false)
	})
	t.Run("unrelated media and codec profiles do not restrict the source", func(t *testing.T) {
		request := profileTestRequest()
		failure := []ProfileCondition{{Property: ProfileConditionValueWidth, Condition: ProfileConditionTypeLessThanEqual, Value: "1"}}
		request.DeviceProfile.ContainerProfiles = []ContainerProfile{
			{Type: DlnaProfileTypeAudio, Container: "mp4", Conditions: failure},
			{Type: DlnaProfileTypeVideo, Container: "mkv", Conditions: failure},
		}
		request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "hevc", Conditions: failure}}
		profileTestDecision(t, profileTestSource(), request, true)
	})
}

func TestEvaluateKnownProfileConditions(t *testing.T) {
	for _, test := range []struct {
		name      string
		property  ProfileConditionValue
		condition ProfileConditionType
		value     string
		want      bool
	}{
		{"width equals", ProfileConditionValueWidth, ProfileConditionTypeEquals, "1920", true},
		{"width not equals rejects equality", ProfileConditionValueWidth, ProfileConditionTypeNotEquals, "1920", false},
		{"width at maximum", ProfileConditionValueWidth, ProfileConditionTypeLessThanEqual, "1920", true},
		{"width exceeds maximum", ProfileConditionValueWidth, ProfileConditionTypeLessThanEqual, "1919", false},
		{"width at minimum", ProfileConditionValueWidth, ProfileConditionTypeGreaterThanEqual, "1920", true},
		{"width below minimum", ProfileConditionValueWidth, ProfileConditionTypeGreaterThanEqual, "1921", false},
		{"width comma alternatives", ProfileConditionValueWidth, ProfileConditionTypeEqualsAny, "1280,1920", true},
		{"width pipe alternatives", ProfileConditionValueWidth, ProfileConditionTypeEqualsAny, "1280|1920", true},
		{"width absent from alternatives", ProfileConditionValueWidth, ProfileConditionTypeEqualsAny, "640,1280", false},
		{"height equals", ProfileConditionValueHeight, ProfileConditionTypeEquals, "1080", true},
		{"height not equals", ProfileConditionValueHeight, ProfileConditionTypeNotEquals, "720", true},
		{"video bitrate uses stream bitrate", ProfileConditionValueVideoBitrate, ProfileConditionTypeLessThanEqual, "3500000", true},
		{"video bitrate exceeds cap", ProfileConditionValueVideoBitrate, ProfileConditionTypeLessThanEqual, "3499999", false},
		{"audio channels equals", ProfileConditionValueAudioChannels, ProfileConditionTypeEquals, "2", true},
		{"audio channels not equals", ProfileConditionValueAudioChannels, ProfileConditionTypeNotEquals, "6", true},
		{"video profile equals", ProfileConditionValueVideoProfile, ProfileConditionTypeEquals, "High", true},
		{"video profile does not match", ProfileConditionValueVideoProfile, ProfileConditionTypeEquals, "Baseline", false},
		{"video profile alternatives", ProfileConditionValueVideoProfile, ProfileConditionTypeEqualsAny, "Main|High", true},
		{"video level uses raw probe value", ProfileConditionValueVideoLevel, ProfileConditionTypeEquals, "41", true},
		{"video level at maximum", ProfileConditionValueVideoLevel, ProfileConditionTypeLessThanEqual, "41", true},
		{"video level exceeds maximum", ProfileConditionValueVideoLevel, ProfileConditionTypeLessThanEqual, "40", false},
		{"rational frame rate below thirty", ProfileConditionValueVideoFramerate, ProfileConditionTypeLessThanEqual, "30", true},
		{"rational frame rate above twenty nine", ProfileConditionValueVideoFramerate, ProfileConditionTypeGreaterThanEqual, "29", true},
		{"rational frame rate is not exactly thirty", ProfileConditionValueVideoFramerate, ProfileConditionTypeEquals, "30", false},
		{"rational frame rate exceeds lower maximum", ProfileConditionValueVideoFramerate, ProfileConditionTypeLessThanEqual, "29.9", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := profileTestRequest()
			profileType, codec := CodecTypeVideo, "h264"
			if test.property == ProfileConditionValueAudioChannels {
				profileType, codec = CodecTypeVideoAudio, "aac"
			}
			request.DeviceProfile.CodecProfiles = []CodecProfile{{
				Type: profileType, Codec: codec,
				Conditions: []ProfileCondition{{Property: test.property, Condition: test.condition, Value: test.value}},
			}}
			profileTestDecision(t, profileTestSource(), request, test.want)
		})
	}
}

func TestEvaluateUnknownConditionsRespectRequiredFlag(t *testing.T) {
	for _, test := range []struct {
		name       string
		isRequired *bool
		want       bool
	}{
		{"optional by default", nil, true},
		{"explicitly required", profileTestPtr(true), false},
		{"explicitly optional", profileTestPtr(false), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := profileTestRequest()
			request.DeviceProfile.CodecProfiles = []CodecProfile{{
				Type: CodecTypeVideo, Codec: "h264",
				Conditions: []ProfileCondition{{Property: ProfileConditionValue("FutureMediaFact"), Condition: ProfileConditionTypeEquals, Value: "true", IsRequired: test.isRequired}},
			}}
			decision := profileTestDecision(t, profileTestSource(), request, test.want)
			if test.want {
				unverified := false
				for _, reason := range decision.Reasons {
					unverified = unverified || reason.Unverified
				}
				if !unverified {
					t.Fatalf("ignored unknown capabilities must be marked unverified: %+v", decision)
				}
			}
		})
	}

	t.Run("missing required media fact is unknown", func(t *testing.T) {
		source, request := profileTestSource(), profileTestRequest()
		source.Info.Streams[0].Width = 0
		request.DeviceProfile.CodecProfiles = []CodecProfile{{
			Type: CodecTypeVideo, Codec: "h264",
			Conditions: []ProfileCondition{{Property: ProfileConditionValueWidth, Condition: ProfileConditionTypeLessThanEqual, Value: "1920", IsRequired: profileTestPtr(true)}},
		}}
		profileTestDecision(t, source, request, false)
	})
	t.Run("optional conditions still reject known failures", func(t *testing.T) {
		request := profileTestRequest()
		request.DeviceProfile.CodecProfiles = []CodecProfile{{
			Type: CodecTypeVideo, Codec: "h264",
			Conditions: []ProfileCondition{{Property: ProfileConditionValueWidth, Condition: ProfileConditionTypeLessThanEqual, Value: "1280", IsRequired: profileTestPtr(false)}},
		}}
		profileTestDecision(t, profileTestSource(), request, false)
	})
}

func TestEvaluateApplyConditionsDoNotHideUnknownRequirements(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply ProfileCondition
		want  bool
	}{
		{"known false skips codec restrictions", ProfileCondition{Property: ProfileConditionValueVideoProfile, Condition: ProfileConditionTypeEquals, Value: "Baseline"}, true},
		{"known true applies codec restrictions", ProfileCondition{Property: ProfileConditionValueVideoProfile, Condition: ProfileConditionTypeEquals, Value: "High"}, false},
		{"unknown optional by default still applies restrictions", ProfileCondition{Property: ProfileConditionValue("FutureMediaFact"), Condition: ProfileConditionTypeEquals, Value: "true"}, false},
		{"unknown explicitly required cannot skip", ProfileCondition{Property: ProfileConditionValue("FutureMediaFact"), Condition: ProfileConditionTypeEquals, Value: "true", IsRequired: profileTestPtr(true)}, false},
		{"ignored optional unknown still applies restrictions", ProfileCondition{Property: ProfileConditionValue("FutureMediaFact"), Condition: ProfileConditionTypeEquals, Value: "true", IsRequired: profileTestPtr(false)}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := profileTestRequest()
			request.DeviceProfile.CodecProfiles = []CodecProfile{{
				Type: CodecTypeVideo, Codec: "h264",
				ApplyConditions: []ProfileCondition{test.apply},
				Conditions:      []ProfileCondition{{Property: ProfileConditionValueWidth, Condition: ProfileConditionTypeLessThanEqual, Value: "1280"}},
			}}
			profileTestDecision(t, profileTestSource(), request, test.want)
		})
	}
}

func TestRequestJSONPreservesOmittedAndExplicitZeroValues(t *testing.T) {
	var omitted Request
	if err := json.Unmarshal([]byte(`{}`), &omitted); err != nil {
		t.Fatal(err)
	}
	var nulls Request
	if err := json.Unmarshal([]byte(`{"DeviceProfile":null,"MaxStreamingBitrate":null,"StartTimeTicks":null,"AudioStreamIndex":null,"EnableDirectPlay":null}`), &nulls); err != nil {
		t.Fatal(err)
	}
	if nulls.DeviceProfile != nil || nulls.MaxStreamingBitrate != nil || nulls.StartTimeTicks != nil ||
		nulls.AudioStreamIndex != nil || nulls.EnableDirectPlay != nil {
		t.Fatalf("null optional values must not become explicit zero values: %+v", nulls)
	}
	var explicit Request
	if err := json.Unmarshal([]byte(`{
		"Id":"item1","UserId":"user1","MediaSourceId":"mediasource_item1",
		"LiveStreamId":"live1","CurrentPlaySessionId":"play1",
		"MaxStreamingBitrate":0,"StartTimeTicks":0,"AudioStreamIndex":0,
		"SubtitleStreamIndex":0,"MaxAudioChannels":0,
		"EnableDirectPlay":false,"EnableDirectStream":false,"EnableTranscoding":false,
		"AllowInterlacedVideoStreamCopy":false,"AllowVideoStreamCopy":false,
		"AllowAudioStreamCopy":false,"IsPlayback":false,"AutoOpenLiveStream":false,
		"DeviceProfile":{
			"MaxStreamingBitrate":0,"MusicStreamingTranscodingBitrate":0,
			"DirectPlayProfiles":[{"Type":"Video","Container":"mp4","AudioCodec":"aac","VideoCodec":"h264"}],
			"CodecProfiles":[{"Type":"Video","Conditions":[{"Property":"Width","Condition":"Equals","Value":"0","IsRequired":false}]}],
			"SubtitleProfiles":[{"Format":"subrip","Method":"Embed","AllowChunkedResponse":false}]
		}
	}`), &explicit); err != nil {
		t.Fatal(err)
	}
	if explicit.ID != "item1" || explicit.UserID != "user1" || explicit.MediaSourceID != "mediasource_item1" ||
		explicit.LiveStreamID != "live1" || explicit.CurrentPlaySessionID != "play1" {
		t.Fatalf("official ID fields were not preserved: %+v", explicit)
	}
	for _, test := range []struct {
		name     string
		omitted  *bool
		explicit *bool
	}{
		{"EnableDirectPlay", omitted.EnableDirectPlay, explicit.EnableDirectPlay},
		{"EnableDirectStream", omitted.EnableDirectStream, explicit.EnableDirectStream},
		{"EnableTranscoding", omitted.EnableTranscoding, explicit.EnableTranscoding},
		{"AllowInterlacedVideoStreamCopy", omitted.AllowInterlacedVideoStreamCopy, explicit.AllowInterlacedVideoStreamCopy},
		{"AllowVideoStreamCopy", omitted.AllowVideoStreamCopy, explicit.AllowVideoStreamCopy},
		{"AllowAudioStreamCopy", omitted.AllowAudioStreamCopy, explicit.AllowAudioStreamCopy},
		{"IsPlayback", omitted.IsPlayback, explicit.IsPlayback},
		{"AutoOpenLiveStream", omitted.AutoOpenLiveStream, explicit.AutoOpenLiveStream},
	} {
		if test.omitted != nil || test.explicit == nil || *test.explicit {
			t.Errorf("%s lost the distinction between omitted and false", test.name)
		}
	}
	for _, test := range []struct {
		name     string
		omitted  *int
		explicit *int
	}{
		{"AudioStreamIndex", omitted.AudioStreamIndex, explicit.AudioStreamIndex},
		{"SubtitleStreamIndex", omitted.SubtitleStreamIndex, explicit.SubtitleStreamIndex},
		{"MaxAudioChannels", omitted.MaxAudioChannels, explicit.MaxAudioChannels},
	} {
		if test.omitted != nil || test.explicit == nil || *test.explicit != 0 {
			t.Errorf("%s lost the distinction between omitted and zero", test.name)
		}
	}
	for _, test := range []struct {
		name     string
		omitted  *int64
		explicit *int64
	}{
		{"MaxStreamingBitrate", omitted.MaxStreamingBitrate, explicit.MaxStreamingBitrate},
		{"StartTimeTicks", omitted.StartTimeTicks, explicit.StartTimeTicks},
	} {
		if test.omitted != nil || test.explicit == nil || *test.explicit != 0 {
			t.Errorf("%s lost the distinction between omitted and zero", test.name)
		}
	}
	if omitted.DeviceProfile != nil || explicit.DeviceProfile == nil {
		t.Fatal("DeviceProfile lost the distinction between omitted and supplied")
	}
	profile := explicit.DeviceProfile
	if profile.MaxStreamingBitrate == nil || *profile.MaxStreamingBitrate != 0 ||
		profile.MusicStreamingTranscodingBitrate == nil || *profile.MusicStreamingTranscodingBitrate != 0 ||
		len(profile.CodecProfiles) != 1 || len(profile.CodecProfiles[0].Conditions) != 1 ||
		profile.CodecProfiles[0].Conditions[0].IsRequired == nil || *profile.CodecProfiles[0].Conditions[0].IsRequired ||
		len(profile.SubtitleProfiles) != 1 || profile.SubtitleProfiles[0].AllowChunkedResponse == nil ||
		*profile.SubtitleProfiles[0].AllowChunkedResponse {
		t.Fatalf("nested explicit zero or false values were not preserved: %+v", profile)
	}

	encoded, err := json.Marshal(explicit)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"MaxStreamingBitrate", "StartTimeTicks", "AudioStreamIndex", "SubtitleStreamIndex", "MaxAudioChannels",
		"EnableDirectPlay", "EnableDirectStream", "EnableTranscoding", "AllowInterlacedVideoStreamCopy", "AllowVideoStreamCopy",
		"AllowAudioStreamCopy", "IsPlayback", "AutoOpenLiveStream"} {
		if _, ok := roundTrip[name]; !ok {
			t.Errorf("marshaling omitted explicit field %s", name)
		}
	}
}

func TestPlaybackSourceHelpersUseCanonicalContainer(t *testing.T) {
	if got := media.SourceID("item1"); got != "mediasource_item1" {
		t.Fatalf("SourceID(item1) = %q, want mediasource_item1", got)
	}
	if media.SourceID("item1") == media.SourceID("item2") {
		t.Fatal("different items must have different original media source identifiers")
	}
	for _, test := range []struct {
		name      string
		format    string
		path      string
		want      string
		wantMIME  string
		audioOnly bool
	}{
		{"mp4 format aliases", "mov,mp4,m4a,3gp,3g2,mj2", "/media/movie.mp4", "mp4", "video/mp4", false},
		{"uppercase known extension", "mov,mp4,m4a,3gp,3g2,mj2", "/media/movie.MP4", "mp4", "video/mp4", false},
		{"quicktime extension", "mov,mp4,m4a,3gp,3g2,mj2", "/media/movie.mov", "mov", "video/quicktime", false},
		{"audio mp4 extension", "mov,mp4,m4a,3gp,3g2,mj2", "/media/song.m4a", "m4a", "audio/mp4", true},
		{"matroska aliases", "matroska,webm", "/media/movie.mkv", "mkv", "video/x-matroska", false},
		{"webm compatible extension", "matroska,webm", "/media/movie.webm", "webm", "video/webm", false},
		{"audio matroska extension", "matroska,webm", "/media/song.mka", "mka", "audio/x-matroska", true},
		{"extension cannot replace container", "matroska,webm", "/media/movie.mp4", "mkv", "video/x-matroska", false},
		{"transport stream default", "mpegts", "/media/movie.ts", "ts", "video/mp2t", false},
		{"transport stream known extension", "mpegts", "/media/movie.m2ts", "m2ts", "video/mp2t", false},
		{"unknown format is not inferred from extension", "unknown-format", "/media/movie.mp4", "unknown-format", "application/octet-stream", false},
		{"missing format is not inferred from extension", "", "/media/movie.mp4", "", "application/octet-stream", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := media.Info{Container: test.format, Streams: []media.Stream{{CodecType: "video"}}}
			if test.audioOnly {
				info.Streams = []media.Stream{{CodecType: "audio"}}
			}
			if got := media.CanonicalContainer(info, test.path); got != test.want {
				t.Errorf("CanonicalContainer() = %q, want %q", got, test.want)
			}
			if got := media.SourceMIMEType(info, test.path); got != test.wantMIME {
				t.Errorf("SourceMIMEType() = %q, want %q", got, test.wantMIME)
			}
		})
	}
}

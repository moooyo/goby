package playback

import (
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

const externalSubtitleTestIndex = 41

func externalSubtitleTestSource(codec string) Source {
	source := profileTestSource()
	source.Info.Streams = append(source.Info.Streams, media.Stream{
		Index: externalSubtitleTestIndex, Codec: codec, CodecType: "subtitle", Language: "eng",
		IsExternal: true, IsTextSubtitleStream: true, IsDefault: true, IsForced: true,
	})
	return source
}

func externalSubtitleTestRequest(profiles ...SubtitleProfile) Request {
	request := profileTestRequest()
	request.SubtitleStreamIndex = profileTestPtr(externalSubtitleTestIndex)
	request.DeviceProfile.SubtitleProfiles = profiles
	return request
}

func assertExternalSubtitleDecision(t *testing.T, decision Decision, format string) {
	t.Helper()
	if decision.SubtitleMethod != SubtitleDeliveryMethodExternal || decision.SubtitleFormat != format ||
		decision.DefaultSubtitleStreamIndex == nil || *decision.DefaultSubtitleStreamIndex != externalSubtitleTestIndex {
		t.Fatalf("selected external subtitle facts are incorrect: %+v", decision)
	}
}

func TestEvaluateIndexedExternalTextSubtitleNativeAndConversionSelection(t *testing.T) {
	for _, test := range []struct {
		name, codec, format, want string
	}{
		{"native SRT", "srt", "srt", "srt"},
		{"SRT to VTT", "srt", "vtt", "vtt"},
		{"native WebVTT", "webvtt", "vtt", "vtt"},
		{"WebVTT to SRT", "webvtt", "srt", "srt"},
		{"SRT format list prefers native", "srt", "vtt,srt", "srt"},
		{"WebVTT format list prefers native", "webvtt", "srt,vtt", "vtt"},
		{"empty selector preserves native", "webvtt", "", "vtt"},
		{"case insensitive format selectors", "srt", "VTT", "vtt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := externalSubtitleTestRequest(SubtitleProfile{Format: test.format, Method: SubtitleDeliveryMethodExternal})
			decision := profileTestDecision(t, externalSubtitleTestSource(test.codec), request, true)
			assertExternalSubtitleDecision(t, decision, test.want)
			if !decision.ProfileMatched || !decision.OriginalCompatible || decision.ClientMustValidate || len(decision.Reasons) != 0 {
				t.Fatalf("supported external text delivery produced an incompatible decision: %+v", decision)
			}
			// Subtitle text conversion is separate from media transcoding and
			// remains possible when the client disables media transcoding.
			request.EnableTranscoding = profileTestPtr(false)
			assertExternalSubtitleDecision(t, profileTestDecision(t, externalSubtitleTestSource(test.codec), request, true), test.want)
		})
	}
}

func TestEvaluateExternalSubtitlesPreferAnyApplicableNativeCandidate(t *testing.T) {
	for _, codec := range []string{"srt", "webvtt"} {
		native, other := "srt", "vtt"
		if codec == "webvtt" {
			native, other = other, native
		}
		request := externalSubtitleTestRequest(
			SubtitleProfile{Format: other, Method: SubtitleDeliveryMethodExternal, Language: "eng", Container: "mp4"},
			SubtitleProfile{Format: native, Method: SubtitleDeliveryMethodEncode},
			SubtitleProfile{Format: native, Method: SubtitleDeliveryMethodExternal, Language: "ENG", Container: "mkv,MP4"},
		)
		decision := profileTestDecision(t, externalSubtitleTestSource(codec), request, true)
		assertExternalSubtitleDecision(t, decision, native)
	}
}

func TestEvaluateExternalSubtitleLanguageContainerAndProtocolRestrictions(t *testing.T) {
	for _, test := range []struct {
		name     string
		profiles []SubtitleProfile
		want     bool
		format   string
	}{
		{"wrong language", []SubtitleProfile{{Format: "srt", Method: SubtitleDeliveryMethodExternal, Language: "fra"}}, false, ""},
		{"wrong container", []SubtitleProfile{{Format: "srt", Method: SubtitleDeliveryMethodExternal, Container: "mkv"}}, false, ""},
		{"conversion still checks language", []SubtitleProfile{{Format: "vtt", Method: SubtitleDeliveryMethodExternal, Language: "fra"}}, false, ""},
		{"conversion still checks container", []SubtitleProfile{{Format: "vtt", Method: SubtitleDeliveryMethodExternal, Container: "mkv"}}, false, ""},
		{"native language mismatch may use a matching conversion", []SubtitleProfile{
			{Format: "srt", Method: SubtitleDeliveryMethodExternal, Language: "fra"},
			{Format: "vtt", Method: SubtitleDeliveryMethodExternal, Language: "eng", Container: "mp4"},
		}, true, "vtt"},
		{"native container mismatch may use a matching conversion", []SubtitleProfile{
			{Format: "srt", Method: SubtitleDeliveryMethodExternal, Container: "mkv"},
			{Format: "vtt", Method: SubtitleDeliveryMethodExternal, Language: "eng", Container: "mp4"},
		}, true, "vtt"},
		{"HTTP delivery", []SubtitleProfile{{Format: "srt", Method: SubtitleDeliveryMethodExternal, Protocol: "http"}}, true, "srt"},
		{"HTTPS delivery", []SubtitleProfile{{Format: "srt", Method: SubtitleDeliveryMethodExternal, Protocol: "https"}}, true, "srt"},
		{"HLS protocol is unavailable", []SubtitleProfile{{Format: "srt", Method: SubtitleDeliveryMethodExternal, Protocol: "hls"}}, false, ""},
		{"local filesystem delivery is unavailable", []SubtitleProfile{{Format: "srt", Method: SubtitleDeliveryMethodExternal, Protocol: "file"}}, false, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision := profileTestDecision(t, externalSubtitleTestSource("srt"), externalSubtitleTestRequest(test.profiles...), test.want)
			if test.want {
				assertExternalSubtitleDecision(t, decision, test.format)
			} else if decision.SubtitleMethod != "" || decision.SubtitleFormat != "" || decision.ProfileMatched {
				t.Fatalf("unmatched constraints advertised external delivery: %+v", decision)
			}
		})
	}
}

func TestEvaluateExternalSubtitleWithoutProfileKeepsDeviceCapabilityUnknown(t *testing.T) {
	for _, test := range []struct{ codec, native string }{{"srt", "srt"}, {"webvtt", "vtt"}} {
		request := Request{SubtitleStreamIndex: profileTestPtr(externalSubtitleTestIndex)}
		decision := profileTestDecision(t, externalSubtitleTestSource(test.codec), request, true)
		assertExternalSubtitleDecision(t, decision, test.native)
		if !decision.ClientMustValidate || decision.ProfileEvaluated || decision.ProfileMatched || !decision.OriginalCompatible {
			t.Fatalf("native sidecar facts falsely established unknown client capabilities: %+v", decision)
		}
	}
	request := externalSubtitleTestRequest()
	decision := profileTestDecision(t, externalSubtitleTestSource("srt"), request, false)
	if decision.SubtitleMethod != "" || decision.SubtitleFormat != "" {
		t.Fatal("an explicitly supplied profile without subtitle support was treated as absent")
	}
}

func TestEvaluateExternalSubtitlesStayDisabledUntilSelected(t *testing.T) {
	for _, selected := range []*int{nil, profileTestPtr(-1)} {
		for _, withProfile := range []bool{false, true} {
			request := Request{SubtitleStreamIndex: selected}
			if withProfile {
				request = externalSubtitleTestRequest(SubtitleProfile{Format: "vtt", Method: SubtitleDeliveryMethodExternal})
				request.SubtitleStreamIndex = selected
			}
			decision := profileTestDecision(t, externalSubtitleTestSource("srt"), request, true)
			if decision.DefaultSubtitleStreamIndex == nil || *decision.DefaultSubtitleStreamIndex != -1 ||
				decision.SubtitleMethod != "" || decision.SubtitleFormat != "" {
				t.Fatalf("omitted or disabled subtitles selected a default/forced sidecar: %+v", decision)
			}
		}
	}
}

func TestExternalSubtitleCandidateFormatsPreserveOffAndSourceFacts(t *testing.T) {
	const second = externalSubtitleTestIndex + 1
	source := externalSubtitleTestSource("srt")
	source.Info.Streams = append(source.Info.Streams,
		media.Stream{Index: second, Codec: "webvtt", CodecType: "subtitle", Language: "eng", IsExternal: true, IsTextSubtitleStream: true},
		media.Stream{Index: second + 1, Codec: "ass", CodecType: "subtitle", Language: "eng", IsExternal: true, IsTextSubtitleStream: true},
		media.Stream{Index: second + 2, Codec: "pgs", CodecType: "subtitle", Language: "eng", IsExternal: true},
		media.Stream{Index: second + 3, Codec: "srt", CodecType: "subtitle", Language: "eng", IsTextSubtitleStream: true},
	)
	original := source
	original.Info.Streams = append([]media.Stream(nil), source.Info.Streams...)
	profile := func(subtitles ...SubtitleProfile) *DeviceProfile {
		request := profileTestRequest()
		request.DeviceProfile.SubtitleProfiles = subtitles
		return request.DeviceProfile
	}
	for _, test := range []struct {
		name    string
		profile *DeviceProfile
		want    map[int]string
	}{
		{"VTT external candidates", profile(SubtitleProfile{Format: "vtt", Method: SubtitleDeliveryMethodExternal}),
			map[int]string{externalSubtitleTestIndex: "vtt", second: "vtt"}},
		{"both formats prefer each native codec", profile(SubtitleProfile{Format: "vtt,srt", Method: SubtitleDeliveryMethodExternal}),
			map[int]string{externalSubtitleTestIndex: "srt", second: "vtt"}},
		{"SRT external candidates", profile(SubtitleProfile{Format: "srt", Method: SubtitleDeliveryMethodExternal}),
			map[int]string{externalSubtitleTestIndex: "srt", second: "srt"}},
		{"absent profile", nil, map[int]string{}},
		{"no subtitle declarations", profile(), map[int]string{}},
		{"HLS does not imply external", profile(SubtitleProfile{Format: "vtt", Method: SubtitleDeliveryMethodHls}), map[int]string{}},
		{"unsupported ASS remains unsupported", profile(SubtitleProfile{Format: "ass", Method: SubtitleDeliveryMethodExternal}), map[int]string{}},
		{"language restriction", profile(SubtitleProfile{Format: "vtt", Method: SubtitleDeliveryMethodExternal, Language: "fra"}), map[int]string{}},
		{"container restriction", profile(SubtitleProfile{Format: "vtt", Method: SubtitleDeliveryMethodExternal, Container: "mkv"}), map[int]string{}},
		{"protocol restriction", profile(SubtitleProfile{Format: "vtt", Method: SubtitleDeliveryMethodExternal, Protocol: "file"}), map[int]string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var copied *DeviceProfile
			if test.profile != nil {
				value := *test.profile
				value.SubtitleProfiles = append([]SubtitleProfile(nil), test.profile.SubtitleProfiles...)
				copied = &value
			}
			for _, selected := range []*int{nil, profileTestPtr(-1)} {
				request := Request{DeviceProfile: test.profile, SubtitleStreamIndex: selected}
				before, err := Evaluate(source, request)
				if err != nil {
					t.Fatalf("candidate test source or request is invalid: %v", err)
				}
				got := ExternalSubtitleCandidateFormats(source, test.profile)
				if !reflect.DeepEqual(got, test.want) {
					t.Errorf("candidate formats = %v, want %v", got, test.want)
				}
				after, err := Evaluate(source, request)
				if err != nil || !reflect.DeepEqual(before, after) || after.DefaultSubtitleStreamIndex == nil ||
					*after.DefaultSubtitleStreamIndex != -1 || after.SubtitleMethod != "" || after.SubtitleFormat != "" {
					t.Error("candidate projection changed Off, selected delivery, or playback compatibility")
				}
				if !reflect.DeepEqual(source, original) || !reflect.DeepEqual(test.profile, copied) {
					t.Error("candidate projection mutated source or profile facts")
				}
			}
		})
	}
}

func TestEvaluateExternalSubtitleSelectionUsesGlobalStreamIndices(t *testing.T) {
	for _, index := range []int{-2, 4, 5, 40, 99} {
		request := externalSubtitleTestRequest(SubtitleProfile{Format: "srt", Method: SubtitleDeliveryMethodExternal})
		request.SubtitleStreamIndex = profileTestPtr(index)
		decision, err := Evaluate(externalSubtitleTestSource("srt"), request)
		if !errors.Is(err, ErrInvalidRequest) || !reflect.DeepEqual(decision, Decision{}) {
			t.Errorf("invalid global subtitle index %d returned decision=%+v error=%v", index, decision, err)
		}
	}
	source := externalSubtitleTestSource("srt")
	source.Info.Streams[len(source.Info.Streams)-1].Index = 12
	if _, err := Evaluate(source, externalSubtitleTestRequest()); !errors.Is(err, ErrInvalidSource) {
		t.Errorf("external subtitle duplicate index did not invalidate the source: %v", err)
	}
}

func TestEvaluateExternalDeliveryDoesNotEnableBurnInBitmapOrExtraction(t *testing.T) {
	for _, method := range []SubtitleDeliveryMethod{SubtitleDeliveryMethodEmbed, SubtitleDeliveryMethodEncode, SubtitleDeliveryMethodHls, SubtitleDeliveryMethodVideoSideData} {
		request := externalSubtitleTestRequest(SubtitleProfile{Format: "srt", Method: method})
		decision := profileTestDecision(t, externalSubtitleTestSource("srt"), request, false)
		if decision.SubtitleMethod != "" || decision.SubtitleFormat != "" {
			t.Fatalf("unsupported sidecar delivery method %s was advertised", method)
		}
	}
	for _, test := range []struct {
		codec string
		text  bool
	}{{"hdmv_pgs_subtitle", false}, {"dvd_subtitle", false}, {"ass", true}, {"srt", false}, {"webvtt", false}} {
		source := externalSubtitleTestSource(test.codec)
		source.Info.Streams[len(source.Info.Streams)-1].IsTextSubtitleStream = test.text
		for _, request := range []Request{
			externalSubtitleTestRequest(SubtitleProfile{Format: "srt,vtt", Method: SubtitleDeliveryMethodExternal}),
			{SubtitleStreamIndex: profileTestPtr(externalSubtitleTestIndex)},
		} {
			decision := profileTestDecision(t, source, request, false)
			if decision.SubtitleMethod != "" || decision.SubtitleFormat != "" {
				t.Errorf("unsupported codec or nontext sidecar was advertised: %+v", decision)
			}
		}
	}
	// Embedded subtitle behavior stays unchanged: native Embed is supported,
	// but External would require an extraction operation this phase lacks.
	request := profileTestRequest()
	request.SubtitleStreamIndex = profileTestPtr(12)
	request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{{Format: "subrip", Method: SubtitleDeliveryMethodEmbed}}
	decision := profileTestDecision(t, profileTestSource(), request, true)
	if decision.SubtitleMethod != SubtitleDeliveryMethodEmbed || decision.SubtitleFormat != "" {
		t.Fatalf("external text support changed native embedded subtitle behavior: %+v", decision)
	}
	request.DeviceProfile.SubtitleProfiles[0].Method = SubtitleDeliveryMethodExternal
	profileTestDecision(t, profileTestSource(), request, false)
}

func TestEvaluateOriginalFallbackCannotBypassUnsupportedExternalSubtitles(t *testing.T) {
	for _, test := range []struct {
		name    string
		profile SubtitleProfile
		want    bool
	}{
		{"supported conversion retains original fallback", SubtitleProfile{Format: "vtt", Method: SubtitleDeliveryMethodExternal}, true},
		{"embed cannot deliver a sidecar", SubtitleProfile{Format: "srt", Method: SubtitleDeliveryMethodEmbed}, false},
		{"burn-in remains unsupported", SubtitleProfile{Format: "srt", Method: SubtitleDeliveryMethodEncode}, false},
		{"language restriction remains enforced", SubtitleProfile{Format: "srt", Method: SubtitleDeliveryMethodExternal, Language: "fra"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := externalSubtitleTestRequest(test.profile)
			request.DeviceProfile.DirectPlayProfiles[0].VideoCodec = "hevc"
			request.EnableTranscoding = profileTestPtr(false)
			decision := profileTestDecision(t, externalSubtitleTestSource("srt"), request, test.want)
			fallback, blocked := false, false
			for _, reason := range decision.Reasons {
				fallback = fallback || reason.Code == "original_fallback"
				blocked = blocked || reason.Code == "subtitle_delivery_unsupported" && !reason.ProfileOnly && !reason.Unverified
			}
			if fallback != test.want || blocked == test.want || decision.ProfileMatched || decision.OriginalCompatible {
				t.Fatalf("original fallback bypassed subtitle delivery or erased codec mismatch: %+v", decision)
			}
			if test.want {
				assertExternalSubtitleDecision(t, decision, "vtt")
			}
		})
	}
}

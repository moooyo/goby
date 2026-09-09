package playback

import "testing"

func TestEvaluateExplicitTranscodingDisablePermitsObservedOriginalFallback(t *testing.T) {
	for _, test := range []struct {
		name        string
		transcoding *bool
		isPlayback  *bool
		want        bool
	}{
		{"omitted transcoding is not disabled", nil, nil, false},
		{"enabled transcoding does not force original", profileTestPtr(true), profileTestPtr(true), false},
		{"disabled transcoding with playback", profileTestPtr(false), profileTestPtr(true), true},
		{"disabled transcoding without playback", profileTestPtr(false), profileTestPtr(false), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := profileTestRequest()
			request.DeviceProfile.DirectPlayProfiles[0].VideoCodec = "hevc"
			request.EnableTranscoding, request.IsPlayback = test.transcoding, test.isPlayback
			decision := profileTestDecision(t, profileTestSource(), request, test.want)
			if decision.OriginalCompatible || decision.ProfileMatched {
				t.Fatalf("client fallback falsely established codec compatibility: %+v", decision)
			}
			mismatch, fallback := false, false
			for _, reason := range decision.Reasons {
				mismatch = mismatch || reason.Code == "no_direct_play_profile"
				fallback = fallback || reason.Code == "original_fallback"
			}
			if !mismatch || fallback != test.want || decision.ClientMustValidate != test.want {
				t.Fatalf("fallback lost the observed decision or its original mismatch: %+v", decision)
			}
		})
	}
}

func TestEvaluateOriginalFallbackCannotOverrideRequestRestrictions(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Request)
	}{
		{"request bitrate", func(r *Request) { r.MaxStreamingBitrate = profileTestPtr(int64(3_999_999)) }},
		{"request audio channels", func(r *Request) { r.MaxAudioChannels = profileTestPtr(1) }},
		{"selected subtitle needs burn-in", func(r *Request) {
			r.SubtitleStreamIndex = profileTestPtr(12)
			r.DeviceProfile.SubtitleProfiles = []SubtitleProfile{{Format: "subrip", Method: SubtitleDeliveryMethodEncode}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := profileTestRequest()
			request.DeviceProfile.DirectPlayProfiles[0].VideoCodec = "hevc"
			request.EnableTranscoding = profileTestPtr(false)
			test.change(&request)
			decision := profileTestDecision(t, profileTestSource(), request, false)
			for _, reason := range decision.Reasons {
				if reason.Code == "original_fallback" {
					t.Fatalf("hard request restriction was bypassed: %+v", decision)
				}
			}
		})
	}
}

func TestEvaluateOriginalFallbackRespectsIndependentDeliverySwitches(t *testing.T) {
	for _, test := range []struct {
		name   string
		play   bool
		stream bool
	}{
		{"direct play only", true, false},
		{"HTTP original only", false, true},
		{"no delivery", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := profileTestRequest()
			request.DeviceProfile.DirectPlayProfiles[0].VideoCodec = "hevc"
			request.EnableTranscoding = profileTestPtr(false)
			request.EnableDirectPlay, request.EnableDirectStream = profileTestPtr(test.play), profileTestPtr(test.stream)
			decision := profileTestDecision(t, profileTestSource(), request, test.play)
			if decision.DirectStream != test.stream || decision.OriginalCompatible || decision.ProfileMatched || !decision.ClientMustValidate {
				t.Fatalf("fallback altered strict compatibility or delivery switches: %+v", decision)
			}
		})
	}
}

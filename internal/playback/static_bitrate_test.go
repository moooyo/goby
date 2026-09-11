package playback

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestEvaluateStaticBitrateConstrainsCompleteOriginalFile(t *testing.T) {
	for _, test := range []struct {
		name     string
		mutate   func(*Source, *Request)
		want     bool
		property string
	}{
		{"omitted static limit", func(_ *Source, _ *Request) {}, true, ""},
		{"exact static limit", func(_ *Source, r *Request) { r.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(4_000_000)) }, true, ""},
		{"below static limit", func(_ *Source, r *Request) { r.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(4_000_001)) }, true, ""},
		{"above static limit", func(_ *Source, r *Request) { r.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(3_999_999)) }, false, "DeviceProfile.MaxStaticBitrate"},
		{"complete file includes unselected streams", func(_ *Source, r *Request) { r.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(3_800_000)) }, false, "DeviceProfile.MaxStaticBitrate"},
		{"unknown original bitrate", func(s *Source, r *Request) {
			s.Info.Bitrate = 0
			r.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(4_000_000))
		}, false, "DeviceProfile.MaxStaticBitrate"},
		{"streaming limit cannot relax static limit", func(_ *Source, r *Request) {
			r.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(3_999_999))
			r.DeviceProfile.MaxStreamingBitrate = profileTestPtr(int64(8_000_000))
		}, false, "DeviceProfile.MaxStaticBitrate"},
		{"static limit cannot relax streaming limit", func(_ *Source, r *Request) {
			r.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(8_000_000))
			r.DeviceProfile.MaxStreamingBitrate = profileTestPtr(int64(3_999_999))
		}, false, "DeviceProfile.MaxStreamingBitrate"},
		{"request cannot relax static limit", func(_ *Source, r *Request) {
			r.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(3_999_999))
			r.MaxStreamingBitrate = profileTestPtr(int64(8_000_000))
		}, false, "DeviceProfile.MaxStaticBitrate"},
		{"static limit cannot relax request limit", func(_ *Source, r *Request) {
			r.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(8_000_000))
			r.MaxStreamingBitrate = profileTestPtr(int64(3_999_999))
		}, false, "MaxStreamingBitrate"},
		{"music limit does not constrain video", func(_ *Source, r *Request) {
			r.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(4_000_000))
			r.DeviceProfile.MaxStaticMusicBitrate = profileTestPtr(1)
		}, true, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, request := profileTestSource(), profileTestRequest()
			test.mutate(&source, &request)
			decision := profileTestDecision(t, source, request, test.want)
			if decision.DirectStream != test.want || decision.OriginalCompatible != test.want {
				t.Fatalf("the original-file static decision lost strict compatibility: %+v", decision)
			}
			if test.property != "" {
				staticBitrateRequireReason(t, decision, test.property, source.Info.Bitrate == 0)
			}
		})
	}
}

func staticBitrateAudioRequest() Request {
	request := audioProfilesTestRequest()
	request.DeviceProfile.DirectPlayProfiles = []DirectPlayProfile{{Type: DlnaProfileTypeAudio, Container: "aac", AudioCodec: "aac"}}
	return request
}

func TestEvaluateStaticBitrateIntersectsMusicLimits(t *testing.T) {
	for _, test := range []struct {
		name              string
		static, streaming int64
		music             int
		want              bool
		property          string
	}{
		{"all exact", 64_000, 64_000, 64_000, true, ""},
		{"general static is strictest", 63_999, 128_000, 128_000, false, "DeviceProfile.MaxStaticBitrate"},
		{"music static is strictest", 128_000, 128_000, 63_999, false, "DeviceProfile.MaxStaticMusicBitrate"},
		{"streaming is strictest", 128_000, 63_999, 128_000, false, "DeviceProfile.MaxStreamingBitrate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := staticBitrateAudioRequest()
			request.DeviceProfile.MaxStaticBitrate = &test.static
			request.DeviceProfile.MaxStreamingBitrate = &test.streaming
			request.DeviceProfile.MaxStaticMusicBitrate = &test.music
			decision := profileTestDecision(t, audioProfilesTestNativeAAC(), request, test.want)
			if decision.ProfileMatched != test.want || decision.OriginalCompatible != test.want {
				t.Fatalf("audio static limits did not preserve the strict profile result: %+v", decision)
			}
			if test.property != "" {
				staticBitrateRequireReason(t, decision, test.property, false)
			}
		})
	}
}

func TestEvaluateStaticBitrateValidatesPositiveInt64WithoutFloatRounding(t *testing.T) {
	for _, value := range []int64{0, -1} {
		request := profileTestRequest()
		request.DeviceProfile.MaxStaticBitrate = &value
		if _, err := Evaluate(profileTestSource(), request); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("nonpositive static limit %d was accepted: %v", value, err)
		}
	}
	var profile DeviceProfile
	if err := json.Unmarshal([]byte(`{"MaxStaticBitrate":9007199254740993}`), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.MaxStaticBitrate == nil || *profile.MaxStaticBitrate != 9_007_199_254_740_993 {
		t.Fatalf("the static bitrate field lost its exact int64 value: %+v", profile.MaxStaticBitrate)
	}
	source, request := profileTestSource(), profileTestRequest()
	request.DeviceProfile.MaxStaticBitrate = profile.MaxStaticBitrate
	source.Info.Bitrate = 9_007_199_254_740_993
	profileTestDecision(t, source, request, true)
	source.Info.Bitrate++
	decision := profileTestDecision(t, source, request, false)
	staticBitrateRequireReason(t, decision, "DeviceProfile.MaxStaticBitrate", false)
}

func TestEvaluateDeviceBitrateLimitsCannotUseOriginalFallback(t *testing.T) {
	for _, kind := range []string{"video", "audio"} {
		for _, cap := range []string{"static", "streaming", "music"} {
			if kind == "video" && cap == "music" {
				continue
			}
			for _, unknown := range []bool{false, true} {
				for _, mismatch := range []bool{false, true} {
					name := kind + "/" + cap
					if unknown {
						name += "/unknown"
					}
					if mismatch {
						name += "/codec mismatch"
					}
					t.Run(name, func(t *testing.T) {
						source, request := profileTestSource(), profileTestRequest()
						if kind == "audio" {
							source, request = audioProfilesTestNativeAAC(), staticBitrateAudioRequest()
						}
						if mismatch {
							request.DeviceProfile.DirectPlayProfiles[0].VideoCodec = "hevc"
							request.DeviceProfile.DirectPlayProfiles[0].AudioCodec = "flac"
						}
						limit, property := source.Info.Bitrate-1, "DeviceProfile.MaxStaticBitrate"
						switch cap {
						case "static":
							request.DeviceProfile.MaxStaticBitrate = &limit
						case "streaming":
							request.DeviceProfile.MaxStreamingBitrate = &limit
							property = "DeviceProfile.MaxStreamingBitrate"
						case "music":
							request.DeviceProfile.MaxStaticMusicBitrate = profileTestPtr(int(limit))
							property = "DeviceProfile.MaxStaticMusicBitrate"
						}
						if unknown {
							source.Info.Bitrate = 0
						}
						request.EnableTranscoding = profileTestPtr(false)
						decision := profileTestDecision(t, source, request, false)
						if decision.DirectStream || decision.OriginalCompatible || decision.ProfileMatched || decision.ClientMustValidate {
							t.Fatalf("disabled transcoding bypassed a device bitrate restriction: %+v", decision)
						}
						staticBitrateRequireReason(t, decision, property, unknown)
						for _, reason := range decision.Reasons {
							if reason.Code == "original_fallback" {
								t.Fatalf("a bitrate-limited original was advertised as a fallback: %+v", decision)
							}
						}
					})
				}
			}
		}
	}
}

func TestEvaluateSatisfiedStaticLimitPreservesCodecFallback(t *testing.T) {
	request := profileTestRequest()
	request.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(4_000_000))
	request.DeviceProfile.DirectPlayProfiles[0].VideoCodec = "hevc"
	request.EnableTranscoding = profileTestPtr(false)
	decision := profileTestDecision(t, profileTestSource(), request, true)
	if !decision.DirectStream || decision.OriginalCompatible || decision.ProfileMatched || !decision.ClientMustValidate {
		t.Fatalf("a satisfied static ceiling changed the existing codec fallback contract: %+v", decision)
	}
	for _, reason := range decision.Reasons {
		if reason.Code == "original_fallback" {
			return
		}
	}
	t.Fatalf("the original-file codec fallback was not explained: %+v", decision)
}

type staticBitrateConversionCase struct {
	name    string
	source  func() Source
	request func() Request
	plan    func(Source, Request, ConversionLimits) (ConversionDecision, error)
	audio   bool
}

func staticBitrateConversionCases() []staticBitrateConversionCase {
	return []staticBitrateConversionCase{
		{"audio HTTP", audioProfilesTestNativeAAC, func() Request {
			return audioProfilesTestRequest(audioProfilesTestProfile("http", "aac", "aac"))
		}, PlanAudioConversion, true},
		{"audio HLS", audioProfilesTestNativeAAC, func() Request {
			return audioProfilesTestRequest(audioProfilesTestProfile("hls", "ts", "aac"))
		}, PlanAudioConversion, true},
		{"video HTTP", videoProfilesTestSource, func() Request {
			return videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
		}, PlanVideoConversion, false},
		{"video HLS", videoProfilesTestSource, func() Request {
			return videoProfilesTestRequest(videoProfilesTestProfile("hls", "ts"))
		}, PlanVideoConversion, false},
	}
}

func TestStaticBitrateDoesNotConstrainConvertedDelivery(t *testing.T) {
	for _, test := range staticBitrateConversionCases() {
		for _, encode := range []bool{false, true} {
			name := test.name + "/remux"
			if encode {
				name = test.name + "/encode"
			}
			t.Run(name, func(t *testing.T) {
				source, request := test.source(), test.request()
				request.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(32_000))
				request.DeviceProfile.MaxStaticMusicBitrate = profileTestPtr(16_000)
				request.DeviceProfile.MaxStreamingBitrate = profileTestPtr(int64(5_000_000))
				request.MaxStreamingBitrate = profileTestPtr(int64(6_000_000))
				limits := conversionTestLimits()
				if encode {
					request.AllowAudioStreamCopy, request.AllowVideoStreamCopy = profileTestPtr(false), profileTestPtr(false)
				} else {
					request.EnableTranscoding = profileTestPtr(false)
					limits.AllowAudioTranscode, limits.AllowVideoTranscode = false, false
				}
				before, err := json.Marshal(struct {
					Source  Source
					Request Request
				}{source, request})
				if err != nil {
					t.Fatal(err)
				}
				original, err := Evaluate(source, request)
				if err != nil {
					t.Fatal(err)
				}
				decision, err := test.plan(source, request, limits)
				if err != nil || decision.Plan == nil {
					t.Fatalf("a static-only restriction prevented converted delivery: %+v, %v", decision, err)
				}
				if original.DirectPlay || original.DirectStream || !reflect.DeepEqual(decision.Original, original) {
					t.Fatalf("converted delivery replaced the refused original-file result: %+v", decision)
				}
				property := "DeviceProfile.MaxStaticBitrate"
				if test.audio {
					property = "DeviceProfile.MaxStaticMusicBitrate"
				}
				staticBitrateRequireReason(t, original, property, false)
				if !decision.Output.OriginalCompatible || !decision.Output.ProfileMatched || decision.OutputSource.Info.Bitrate <= 32_000 ||
					decision.OutputSource.Info.Bitrate > 5_000_000 {
					t.Fatalf("converted delivery did not honor its independent streaming budget: %+v", decision)
				}
				wantMethod, wantAudio, wantVideo := "DirectStream", "copy", "copy"
				if encode {
					wantMethod, wantAudio, wantVideo = "Transcode", "aac", "h264"
				}
				if test.audio {
					wantVideo = ""
				}
				if decision.Method != wantMethod || decision.Plan.AudioCodec != wantAudio || decision.Plan.VideoCodec != wantVideo {
					t.Fatalf("the static ceiling changed independently authorized copy or encoding: %+v", decision)
				}
				after, err := json.Marshal(struct {
					Source  Source
					Request Request
				}{source, request})
				if err != nil || string(before) != string(after) {
					t.Fatalf("output evaluation mutated caller-owned static limits or media facts: %v", err)
				}
			})
		}
	}
}

func TestStaticBitrateOutputStillHonorsEveryStreamingCeiling(t *testing.T) {
	for _, test := range staticBitrateConversionCases() {
		for _, owner := range []string{"request", "profile", "server"} {
			t.Run(test.name+"/"+owner, func(t *testing.T) {
				source, request := test.source(), test.request()
				ceiling := int64(1_000_000)
				if test.audio {
					ceiling = 70_000
				}
				request.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(32_000))
				request.DeviceProfile.MaxStaticMusicBitrate = profileTestPtr(16_000)
				request.MaxStreamingBitrate = profileTestPtr(ceiling * 3)
				request.DeviceProfile.MaxStreamingBitrate = profileTestPtr(ceiling * 3)
				limits := conversionTestLimits()
				limits.MaxBitrate = ceiling * 3
				switch owner {
				case "request":
					request.MaxStreamingBitrate = &ceiling
				case "profile":
					request.DeviceProfile.MaxStreamingBitrate = &ceiling
				case "server":
					limits.MaxBitrate = ceiling
				}
				request.AllowAudioStreamCopy, request.AllowVideoStreamCopy = profileTestPtr(false), profileTestPtr(false)
				decision, err := test.plan(source, request, limits)
				if err != nil || decision.Plan == nil || decision.Method != "Transcode" {
					t.Fatalf("an authorized bounded encoding was unavailable: %+v, %v", decision, err)
				}
				if decision.OutputSource.Info.Bitrate <= 32_000 || decision.OutputSource.Info.Bitrate > ceiling ||
					!decision.Output.OriginalCompatible || !decision.Output.ProfileMatched {
					t.Fatalf("clearing static limits removed the %s streaming ceiling: %+v", owner, decision)
				}
			})
		}
	}
}

func TestStaticBitrateDoesNotGrantOverBudgetRemux(t *testing.T) {
	for _, test := range staticBitrateConversionCases() {
		t.Run(test.name, func(t *testing.T) {
			source, request := test.source(), test.request()
			request.DeviceProfile.MaxStaticBitrate = profileTestPtr(int64(1))
			ceiling := int64(1_000_000)
			if test.audio {
				ceiling = 32_000
			}
			request.DeviceProfile.MaxStreamingBitrate = &ceiling
			request.EnableTranscoding = profileTestPtr(false)
			decision, err := test.plan(source, request, ConversionLimits{AllowRemux: true})
			if err != nil || decision.Plan != nil || len(decision.Reasons) == 0 {
				t.Fatalf("a static refusal authorized an over-budget remux: %+v, %v", decision, err)
			}
			if decision.Original.DirectPlay || decision.Original.DirectStream {
				t.Fatalf("an over-budget remux fell back to the forbidden original: %+v", decision.Original)
			}
		})
	}
}

func staticBitrateRequireReason(t *testing.T, decision Decision, property string, unknown bool) {
	t.Helper()
	for _, reason := range decision.Reasons {
		if reason.Property != property {
			continue
		}
		wantCode := "device_bitrate_limit"
		if property == "MaxStreamingBitrate" {
			wantCode = "bitrate_limit"
		}
		if unknown {
			wantCode = "unknown_source_bitrate"
		}
		if reason.Code == wantCode && !reason.Unverified && reason.ProfileOnly == (property != "MaxStreamingBitrate") {
			return
		}
	}
	t.Fatalf("missing enforced bitrate reason for %s: %+v", property, decision)
}

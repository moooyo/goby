package playback

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func audioProfilesTestProfile(protocol, container, codec string) TranscodingProfile {
	return TranscodingProfile{Type: DlnaProfileTypeAudio, Protocol: protocol, Context: EncodingContextStreaming,
		Container: container, AudioCodec: codec}
}

func audioProfilesTestRequest(profiles ...TranscodingProfile) Request {
	return Request{DeviceProfile: &DeviceProfile{SupportedMediaTypes: "Audio", TranscodingProfiles: profiles}}
}

func audioProfilesTestPlan(t *testing.T, source Source, request Request, limits ConversionLimits, protocol string, index int) ConversionDecision {
	t.Helper()
	original, err := Evaluate(source, request)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := PlanAudioConversion(source, request, limits)
	if err != nil || decision.Plan == nil {
		t.Fatalf("expected an audio conversion, got %+v, %v", decision, err)
	}
	if !reflect.DeepEqual(decision.Original, original) {
		t.Fatalf("conversion changed the original-file decision: got %+v, want %+v", decision.Original, original)
	}
	if decision.SelectedProtocol != protocol || decision.SelectedProfileIndex == nil || *decision.SelectedProfileIndex != index {
		t.Fatalf("wrong ordered profile selection: protocol=%q index=%v, want %q/%d", decision.SelectedProtocol, decision.SelectedProfileIndex, protocol, index)
	}
	if !decision.Output.OriginalCompatible || !decision.Output.ProfileMatched {
		t.Fatalf("the selected output does not satisfy the client's profile: %+v", decision.Output)
	}
	if err := transcode.ValidatePlan(*decision.Plan); err != nil {
		t.Fatalf("the selected output cannot be executed by the runner: %v", err)
	}
	if decision.Plan.VideoStreamIndex != -1 || decision.Plan.VideoCodec != "" {
		t.Fatalf("audio conversion retained a video or attached-picture track: %+v", decision.Plan)
	}
	return decision
}

func audioProfilesTestDeclined(t *testing.T, source Source, request Request, limits ConversionLimits) ConversionDecision {
	t.Helper()
	original, err := Evaluate(source, request)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := PlanAudioConversion(source, request, limits)
	if err != nil || decision.Plan != nil || len(decision.Reasons) == 0 || decision.SelectedProfileIndex != nil {
		t.Fatalf("expected an explained unsupported decision without a selected profile, got %+v, %v", decision, err)
	}
	if !reflect.DeepEqual(decision.Original, original) {
		t.Fatal("an unsupported conversion changed the original-file decision")
	}
	return decision
}

func audioProfilesTestNativeAAC() Source {
	source := progressiveAudioTestSource()
	source.Info.Streams = []media.Stream{source.Info.Streams[2]}
	source.Info.Streams[0].IsDefault = true
	source.Info.Container, source.Path, source.Info.Bitrate = "aac", "/media/native.aac", 64_000
	return source
}

func TestPlanAudioConversionPreservesMixedProtocolProfileOrder(t *testing.T) {
	http := audioProfilesTestProfile("http", "mp3", "mp3")
	hls := audioProfilesTestProfile("hls", "ts", "aac")
	for _, test := range []struct {
		name     string
		profiles []TranscodingProfile
		protocol string
		codec    string
	}{
		{"progressive first", []TranscodingProfile{http, hls}, "http", "mp3"},
		{"HLS first", []TranscodingProfile{hls, http}, "hls", "aac"},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), audioProfilesTestRequest(test.profiles...), conversionTestLimits(), test.protocol, 0)
			if decision.Plan.AudioCodec != test.codec || decision.Method != "Transcode" {
				t.Fatalf("a later profile replaced the first constructible output: %+v", decision)
			}
			if (decision.Plan.OutputMode == "progressive") != (test.protocol == "http") {
				t.Fatalf("the execution mode disagrees with the selected delivery protocol: %+v", decision.Plan)
			}
		})
	}
}

func TestPlanAudioConversionUsesReferenceAudioDeliveryDefaults(t *testing.T) {
	for _, test := range []struct {
		name string
		json string
	}{
		{"omitted protocol", `{"Type":"Audio","Container":"mp3","AudioCodec":"mp3","Context":"Streaming"}`},
		{"empty protocol", `{"Type":"Audio","Container":"mp3","AudioCodec":"mp3","Protocol":"","Context":"Streaming"}`},
		{"omitted context", `{"Type":"Audio","Container":"mp3","AudioCodec":"mp3","Protocol":"http"}`},
		{"empty context", `{"Type":"Audio","Container":"mp3","AudioCodec":"mp3","Protocol":"http","Context":""}`},
		{"both omitted", `{"Type":"Audio","Container":"mp3","AudioCodec":"mp3"}`},
		{"both empty", `{"Type":"Audio","Container":"mp3","AudioCodec":"mp3","Protocol":"","Context":""}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var profile TranscodingProfile
			if err := json.Unmarshal([]byte(test.json), &profile); err != nil {
				t.Fatal(err)
			}
			request := audioProfilesTestRequest(profile)
			decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits(), "http", 0)
			if decision.Plan.OutputMode != "progressive" || decision.Plan.Container != "mp3" || request.DeviceProfile.TranscodingProfiles[0] != profile {
				t.Fatalf("audio delivery defaults changed the requested profile or chose a different protocol: %+v", decision)
			}
		})
	}
	for _, protocol := range []string{"", "http", "hls"} {
		t.Run("static context "+protocol, func(t *testing.T) {
			profile := audioProfilesTestProfile(protocol, "mp3", "mp3")
			if protocol == "hls" {
				profile.Container = "ts"
			}
			profile.Context = EncodingContextStatic
			audioProfilesTestDeclined(t, progressiveAudioTestSource(), audioProfilesTestRequest(profile), conversionTestLimits())
		})
	}
}

func TestPlanAudioConversionSkipsUnconstructibleLeadingProfiles(t *testing.T) {
	for _, test := range []struct {
		name    string
		profile TranscodingProfile
	}{
		{"unsupported progressive container", audioProfilesTestProfile("http", "asf", "wmav2")},
		{"unsupported progressive codec", audioProfilesTestProfile("http", "m4a", "alac")},
		{"unsupported HLS codec", audioProfilesTestProfile("hls", "ts", "opus")},
		{"unsupported protocol", audioProfilesTestProfile("dash", "mp4", "aac")},
		{"video profile", TranscodingProfile{Type: DlnaProfileTypeVideo, Protocol: "http", Context: EncodingContextStreaming, Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"}},
		{"malformed channel limit", TranscodingProfile{Type: DlnaProfileTypeAudio, Protocol: "http", Context: EncodingContextStreaming, Container: "mp3", AudioCodec: "mp3", MaxAudioChannels: "two"}},
		{"unsupported copy timestamps", TranscodingProfile{Type: DlnaProfileTypeAudio, Protocol: "http", Context: EncodingContextStreaming, Container: "mp3", AudioCodec: "mp3", CopyTimestamps: profileTestPtr(true)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := audioProfilesTestRequest(test.profile, audioProfilesTestProfile("http", "mp3", "mp3"))
			decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits(), "http", 1)
			if decision.Plan.Container != "mp3" || decision.Plan.AudioCodec != "mp3" {
				t.Fatalf("unsupported leading profile changed the usable fallback: %+v", decision.Plan)
			}
		})
	}
}

func TestPlanAudioConversionBoundsSearchAcrossProfilesPerRequest(t *testing.T) {
	requestWithLeadingProfiles := func(count int) Request {
		var profiles []TranscodingProfile
		for index := 0; index < count; index++ {
			profiles = append(profiles, audioProfilesTestProfile("http", "m4a", "aac"))
		}
		profiles = append(profiles, audioProfilesTestProfile("http", "mp3", "mp3"))
		request := audioProfilesTestRequest(profiles...)
		request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "aac", Conditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeLessThanEqual, "8"),
			conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeLessThanEqual, "96000"),
			conversionRequired(ProfileConditionValueAudioBitrate, ProfileConditionTypeLessThanEqual, "768000"),
			conversionRequired("FutureCapability", ProfileConditionTypeEquals, "supported"),
		}}}
		return request
	}
	// Both attempts force a new request budget after exhaustion. An implementation
	// that resets the budget per profile would incorrectly reach the MP3 fallback.
	for _, name := range []string{"first request", "independent later request"} {
		t.Run(name, func(t *testing.T) {
			decision := audioProfilesTestDeclined(t, progressiveAudioTestSource(), requestWithLeadingProfiles(17), conversionTestLimits())
			foundLimit := false
			for _, reason := range decision.Reasons {
				foundLimit = foundLimit || reason.Code == "audio_profile_search_limit"
			}
			if !foundLimit {
				t.Fatalf("candidate work across profiles exceeded its request budget without an explicit refusal: %+v", decision)
			}
			decision = audioProfilesTestPlan(t, progressiveAudioTestSource(), requestWithLeadingProfiles(2), conversionTestLimits(), "http", 2)
			if decision.Plan.AudioCodec != "mp3" || decision.Plan.Container != "mp3" {
				t.Fatalf("an exhausted earlier request or a rejected leading profile blocked a valid fallback: %+v", decision)
			}
		})
	}
}

func TestPlanAudioConversionDoesNotInventHLSForProgressiveProfiles(t *testing.T) {
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "mp3", "mp3"))
	decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits(), "http", 0)
	if decision.Plan.OutputMode != "progressive" || decision.Plan.Container != "mp3" || decision.Plan.SegmentSeconds != 0 {
		t.Fatalf("an HTTP profile was silently translated into segmented HLS: %+v", decision.Plan)
	}
	request.DeviceProfile.TranscodingProfiles[0].Container = "ts"
	audioProfilesTestDeclined(t, progressiveAudioTestSource(), request, conversionTestLimits())
}

func TestPlanAudioConversionProgressiveCopyAndEncodingPermissions(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*Request, *ConversionLimits)
		codec   string
		decline bool
	}{
		{"authorized copy", func(_ *Request, _ *ConversionLimits) {}, "copy", false},
		{"copy does not need encoding permission", func(_ *Request, limits *ConversionLimits) { limits.AllowAudioTranscode = false }, "copy", false},
		{"transcoding delivery permits authorized copy", func(request *Request, limits *ConversionLimits) {
			request.EnableDirectPlay, request.EnableDirectStream = profileTestPtr(false), profileTestPtr(false)
			request.EnableTranscoding = profileTestPtr(true)
			limits.AllowAudioTranscode = false
		}, "copy", false},
		{"disabled transcoding still permits direct-stream copy", func(request *Request, _ *ConversionLimits) { request.EnableTranscoding = profileTestPtr(false) }, "copy", false},
		{"remux denial forces authorized encoding", func(_ *Request, limits *ConversionLimits) { limits.AllowRemux = false }, "aac", false},
		{"explicit copy denial forces encoding", func(request *Request, _ *ConversionLimits) { request.AllowAudioStreamCopy = profileTestPtr(false) }, "aac", false},
		{"no conversion permissions", func(_ *Request, limits *ConversionLimits) { *limits = ConversionLimits{} }, "", true},
		{"both delivery modes disabled", func(request *Request, _ *ConversionLimits) {
			request.EnableDirectStream, request.EnableTranscoding = profileTestPtr(false), profileTestPtr(false)
		}, "", true},
		{"copy denial cannot bypass encoding policy", func(request *Request, limits *ConversionLimits) {
			request.AllowAudioStreamCopy = profileTestPtr(false)
			limits.AllowAudioTranscode = false
		}, "", true},
		{"copy denial survives disabled transcoding", func(request *Request, _ *ConversionLimits) {
			request.AllowAudioStreamCopy, request.EnableTranscoding = profileTestPtr(false), profileTestPtr(false)
		}, "", true},
		{"remux denial survives disabled transcoding", func(request *Request, limits *ConversionLimits) {
			request.EnableTranscoding = profileTestPtr(false)
			limits.AllowRemux = false
		}, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, limits := audioProfilesTestRequest(audioProfilesTestProfile("http", "aac", "aac")), conversionTestLimits()
			test.mutate(&request, &limits)
			if test.decline {
				audioProfilesTestDeclined(t, audioProfilesTestNativeAAC(), request, limits)
				return
			}
			decision := audioProfilesTestPlan(t, audioProfilesTestNativeAAC(), request, limits, "http", 0)
			wantMethod := "Transcode"
			if test.codec == "copy" {
				wantMethod = "DirectStream"
			}
			if decision.Plan.AudioCodec != test.codec || decision.Method != wantMethod {
				t.Fatalf("conversion flags or permissions changed the physical operation: %+v", decision)
			}
		})
	}
}

func TestPlanAudioConversionIntersectsContainerAndCodecCeilings(t *testing.T) {
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "mp3", "mp3"))
	request.DeviceProfile.ContainerProfiles = []ContainerProfile{{Type: DlnaProfileTypeAudio, Container: "mp3", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeLessThanEqual, "22050"),
	}}}
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "mp3", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeLessThanEqual, "1"),
		conversionRequired(ProfileConditionValueAudioBitrate, ProfileConditionTypeLessThanEqual, "96000"),
	}}}
	decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits(), "http", 0)
	output := decision.OutputSource.Info.Streams[0]
	if decision.Plan.AudioCodec != "mp3" || output.Channels != 1 || output.SampleRate <= 0 || output.SampleRate > 22_050 || output.Bitrate <= 0 || output.Bitrate > 96_000 {
		t.Fatalf("joint client limits were checked only against source facts or one profile class: %+v", decision)
	}
}

func TestPlanAudioConversionConstructsExactAudioConditions(t *testing.T) {
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "mp3", "mp3"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "mp3", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeEquals, "2"),
		conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeEquals, "44100"),
		conversionRequired(ProfileConditionValueAudioBitrate, ProfileConditionTypeEquals, "128000"),
	}}}
	decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits(), "http", 0)
	if decision.Plan.AudioChannels != 2 || decision.Plan.AudioSampleRate != 44_100 || decision.Plan.AudioBitrate != 128_000 {
		t.Fatalf("exact client requirements were treated as upper bounds: %+v", decision.Plan)
	}
}

func TestPlanAudioConversionConstructsMinimumAudioConditions(t *testing.T) {
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "m4a", "aac"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "aac", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeGreaterThanEqual, "4"),
		conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeGreaterThanEqual, "48000"),
		conversionRequired(ProfileConditionValueAudioBitrate, ProfileConditionTypeGreaterThanEqual, "256000"),
	}}}
	decision := audioProfilesTestPlan(t, audioProfilesTestNativeAAC(), request, conversionTestLimits(), "http", 0)
	output := decision.OutputSource.Info.Streams[0]
	if decision.Plan.AudioCodec != "aac" || output.Channels < 4 || output.SampleRate < 48_000 || output.Bitrate < 256_000 {
		t.Fatalf("constructible lower bounds were ignored or confused with ceilings: %+v", decision)
	}
}

func TestPlanAudioConversionConstructsAllowedAudioAlternatives(t *testing.T) {
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "mp3", "mp3"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "mp3", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeEqualsAny, "1,2"),
		conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeEqualsAny, "22050|44100"),
		conversionRequired(ProfileConditionValueAudioBitrate, ProfileConditionTypeEqualsAny, "64000,128000"),
	}}}
	decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits(), "http", 0)
	output := decision.OutputSource.Info.Streams[0]
	if output.Channels != 1 && output.Channels != 2 || output.SampleRate != 22_050 && output.SampleRate != 44_100 || output.Bitrate != 64_000 && output.Bitrate != 128_000 {
		t.Fatalf("the encoder selected values outside the client's declared alternatives: %+v", output)
	}
}

func TestPlanAudioConversionReducesFLACPrecisionOnlyWithEncodingPermission(t *testing.T) {
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "flac", "flac"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "flac", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioBitDepth, ProfileConditionTypeLessThanEqual, "16"),
	}}}
	decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits(), "http", 0)
	if decision.Plan.AudioCodec != "flac" || decision.Plan.AudioBitDepth != 16 || decision.OutputSource.Info.Streams[0].BitDepth != 16 || decision.Method != "Transcode" {
		t.Fatalf("24-bit input was mislabeled as compatible 16-bit output without quantization: %+v", decision)
	}
	audioProfilesTestDeclined(t, progressiveAudioTestSource(), request, ConversionLimits{AllowRemux: true})
}

func TestPlanAudioConversionChoosesFLACPrecisionWithinStreamingBudget(t *testing.T) {
	for _, test := range []struct {
		name        string
		sourceDepth int
		maxDepth    string
		budget      int64
		wantDepth   int
	}{
		{"24-bit source fits only 16-bit output", 24, "24", 3_200_000, 16},
		{"larger budget retains source precision", 24, "24", 5_000_000, 24},
		{"8-bit source uses supported 16-bit encoding", 8, "16", 3_200_000, 16},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := progressiveAudioTestSource()
			source.Info.Streams[1].SampleRate, source.Info.Streams[1].BitDepth = 96_000, test.sourceDepth
			source.Info.Streams[1].TimeBase = "1/96000"
			request := audioProfilesTestRequest(audioProfilesTestProfile("http", "flac", "flac"))
			request.MaxStreamingBitrate = &test.budget
			request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "flac", Conditions: []ProfileCondition{
				conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeEquals, "2"),
				conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeEquals, "96000"),
				conversionRequired(ProfileConditionValueAudioBitDepth, ProfileConditionTypeLessThanEqual, test.maxDepth),
			}}}
			limits := conversionTestLimits()
			limits.AllowRemux = false
			decision := audioProfilesTestPlan(t, source, request, limits, "http", 0)
			output := decision.OutputSource.Info.Streams[0]
			if decision.Method != "Transcode" || decision.Plan.AudioCodec != "flac" || decision.Plan.AudioBitDepth != test.wantDepth || output.BitDepth != test.wantDepth ||
				output.Channels != 2 || output.SampleRate != 96_000 || decision.OutputSource.Info.Bitrate <= 0 || decision.OutputSource.Info.Bitrate > test.budget {
				t.Fatalf("FLAC precision was not constructed within all client requirements and the streaming budget: %+v", decision)
			}
			if source.Info.Streams[1].BitDepth != test.sourceDepth || source.Info.Streams[1].SampleRate != 96_000 {
				t.Fatal("choosing a supported output precision changed the original audio facts")
			}
		})
	}
}

func TestPlanAudioConversionCanonicalizesContainerAliasesWithoutDroppingConditions(t *testing.T) {
	for _, test := range []struct {
		alias, canonical, codec string
	}{
		{"mp4", "m4a", "aac"},
		{"m4b", "m4a", "aac"},
		{"adts", "aac", "aac"},
		{"wave", "wav", "pcm_s16le"},
		{"oga", "ogg", "vorbis"},
	} {
		t.Run(test.alias, func(t *testing.T) {
			request := audioProfilesTestRequest(audioProfilesTestProfile("http", test.alias, test.codec))
			request.DeviceProfile.ContainerProfiles = []ContainerProfile{{Type: DlnaProfileTypeAudio, Container: test.alias, Conditions: []ProfileCondition{
				conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeEquals, "1"),
			}}}
			request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: test.codec, Container: test.alias, Conditions: []ProfileCondition{
				conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeEquals, "22050"),
			}}}
			before, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits(), "http", 0)
			output := decision.OutputSource.Info.Streams[0]
			if decision.Plan.Container != test.canonical || decision.OutputSource.Info.Container != test.canonical || output.Channels != 1 || output.SampleRate != 22_050 {
				t.Fatalf("container alias matching dropped codec or container conditions: %+v", decision)
			}
			after, err := json.Marshal(request)
			if err != nil || string(before) != string(after) {
				t.Fatalf("container alias normalization changed caller-owned selectors or conditions: %v", err)
			}
		})
	}
}

func TestPlanAudioConversionRetriesEncodingWhenCopiedFactsFailRequiredProfile(t *testing.T) {
	source := audioProfilesTestNativeAAC()
	source.Info.Streams[0].Profile = ""
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "aac", "aac"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "aac", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioProfile, ProfileConditionTypeEquals, "LC"),
	}}}
	decision := audioProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	if decision.Plan.AudioCodec != "aac" || decision.OutputSource.Info.Streams[0].Profile != "LC" || decision.Method != "Transcode" {
		t.Fatalf("an unverifiable copy prevented a constructible same-profile encoding: %+v", decision)
	}
	audioProfilesTestDeclined(t, source, request, ConversionLimits{AllowRemux: true})
	request.EnableTranscoding = profileTestPtr(false)
	audioProfilesTestDeclined(t, source, request, conversionTestLimits())
}

func TestPlanAudioConversionPreservesOptionalUnknownConditions(t *testing.T) {
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "mp3", "mp3"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "mp3", Conditions: []ProfileCondition{{
		Property: "FutureAudioCapability", Condition: ProfileConditionTypeEquals, Value: "supported", IsRequired: profileTestPtr(false),
	}}}}
	decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits(), "http", 0)
	if !decision.Output.ClientMustValidate {
		t.Fatal("an optional unknown output condition was silently claimed to be verified")
	}
	condition := &request.DeviceProfile.CodecProfiles[0].Conditions[0]
	condition.IsRequired = profileTestPtr(true)
	audioProfilesTestDeclined(t, progressiveAudioTestSource(), request, conversionTestLimits())
	condition.IsRequired, condition.Condition = profileTestPtr(false), "UnrecognizedComparison"
	audioProfilesTestDeclined(t, progressiveAudioTestSource(), request, conversionTestLimits())
}

func TestPlanAudioConversionDoesNotDiscardUnknownApplicability(t *testing.T) {
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "mp3", "mp3"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "mp3", ApplyConditions: []ProfileCondition{
		conversionRequired("FutureAudioCapability", ProfileConditionTypeEquals, "supported"),
	}, Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeLessThanEqual, "2"),
	}}}
	audioProfilesTestDeclined(t, progressiveAudioTestSource(), request, conversionTestLimits())
	request.DeviceProfile.CodecProfiles[0].ApplyConditions[0].IsRequired = nil
	decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits(), "http", 0)
	if !decision.Output.ClientMustValidate {
		t.Fatal("optional unknown applicability was silently represented as verified output compatibility")
	}
}

func TestPlanAudioConversionChecksEncodedFactsInsteadOfSourceMetadata(t *testing.T) {
	for _, condition := range []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioProfile, ProfileConditionTypeEquals, "source-profile"),
		conversionRequired(ProfileConditionValueAudioBitDepth, ProfileConditionTypeGreaterThanEqual, "16"),
	} {
		t.Run(string(condition.Property), func(t *testing.T) {
			request := audioProfilesTestRequest(audioProfilesTestProfile("http", "m4a", "aac"))
			request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "aac", Conditions: []ProfileCondition{condition}}}
			audioProfilesTestDeclined(t, progressiveAudioTestSource(), request, conversionTestLimits())
		})
	}
}

func TestPlanAudioConversionRejectsContradictoryOutputConditions(t *testing.T) {
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "m4a", "aac"))
	request.DeviceProfile.ContainerProfiles = []ContainerProfile{{Type: DlnaProfileTypeAudio, Container: "m4a", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeLessThanEqual, "32000"),
	}}}
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "aac", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeGreaterThanEqual, "44100"),
	}}}
	audioProfilesTestDeclined(t, progressiveAudioTestSource(), request, conversionTestLimits())
}

func TestPlanAudioConversionRejectsProfileThenTriesLaterProtocol(t *testing.T) {
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "mp3", "mp3"), audioProfilesTestProfile("hls", "ts", "aac"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "mp3", Container: "mp3", Conditions: []ProfileCondition{
		conversionRequired("FutureAudioCapability", ProfileConditionTypeEquals, "supported"),
	}}}
	decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), request, conversionTestLimits(), "hls", 1)
	if decision.Plan.AudioCodec != "aac" {
		t.Fatalf("a profile-specific required condition blocked an unrelated valid fallback: %+v", decision)
	}
}

func TestPlanAudioConversionResetsOutputTrackAndSeekCoordinates(t *testing.T) {
	source := progressiveAudioTestSource()
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "mp3", "mp3"))
	request.AudioStreamIndex, request.StartTimeTicks = profileTestPtr(9), profileTestPtr(90*media.TicksPerSecond)
	request.DeviceProfile.ContainerProfiles = []ContainerProfile{{Type: DlnaProfileTypeAudio, Container: "mp3", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueNumAudioStreams, ProfileConditionTypeEquals, "1"),
		conversionRequired(ProfileConditionValueNumVideoStreams, ProfileConditionTypeEquals, "0"),
	}}}
	decision := audioProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	if decision.Plan.AudioStreamIndex != 9 || decision.Plan.StartTicks != 90*media.TicksPerSecond || decision.Plan.DurationTicks != source.Info.DurationTicks {
		t.Fatalf("input track and timeline were replaced by output coordinates: %+v", decision.Plan)
	}
	if len(decision.OutputSource.Info.Streams) != 1 || decision.OutputSource.Info.Streams[0].Index != 0 ||
		decision.OutputSource.Info.DurationTicks != 30*media.TicksPerSecond || decision.Output.DefaultAudioStreamIndex == nil ||
		*decision.Output.DefaultAudioStreamIndex != 0 || decision.Output.RequiresAudioTrackSelection {
		t.Fatalf("output validation used source track indices or applied the seek twice: %+v", decision)
	}
}

func TestPlanAudioConversionUsesStreamingBudgetWithoutStaticMusicCeiling(t *testing.T) {
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "mp3", "mp3"))
	request.DeviceProfile.MaxStaticMusicBitrate = profileTestPtr(32_000)
	request.DeviceProfile.MusicStreamingTranscodingBitrate = profileTestPtr(170_000)
	request.DeviceProfile.MaxStreamingBitrate = profileTestPtr(int64(180_000))
	request.MaxStreamingBitrate = profileTestPtr(int64(190_000))
	limits := conversionTestLimits()
	limits.MaxBitrate = 200_000
	decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), request, limits, "http", 0)
	if decision.Plan.AudioBitrate != 160_000 || decision.OutputSource.Info.Bitrate > 170_000 || decision.OutputSource.Info.Bitrate <= 32_000 {
		t.Fatalf("static music limits restricted encoding or the streaming ceiling was ignored: %+v", decision)
	}
	limits.MaxBitrate = 100_000
	decision = audioProfilesTestPlan(t, progressiveAudioTestSource(), request, limits, "http", 0)
	if decision.Plan.AudioBitrate > 100_000 || decision.OutputSource.Info.Bitrate > 100_000 {
		t.Fatalf("client streaming settings bypassed the server bitrate ceiling: %+v", decision)
	}
}

func TestPlanAudioConversionIntersectsAllChannelLimits(t *testing.T) {
	source := progressiveAudioTestSource()
	source.Info.Streams[1].Channels = 6
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "m4a", "aac"))
	request.MaxAudioChannels = profileTestPtr(4)
	request.DeviceProfile.TranscodingProfiles[0].MaxAudioChannels = "2"
	limits := conversionTestLimits()
	limits.MaxAudioChannels = 3
	decision := audioProfilesTestPlan(t, source, request, limits, "http", 0)
	if decision.Plan.AudioChannels != 2 || decision.OutputSource.Info.Streams[0].Channels != 2 {
		t.Fatalf("profile, request, and server channel ceilings were not intersected: %+v", decision)
	}
	limits.MaxAudioChannels = 1
	decision = audioProfilesTestPlan(t, source, request, limits, "http", 0)
	if decision.Plan.AudioChannels != 1 || decision.OutputSource.Info.Streams[0].Channels != 1 {
		t.Fatalf("client settings replaced the stricter server channel ceiling: %+v", decision)
	}
}

func TestPlanAudioConversionDoesNotAttachVideoHardwarePolicy(t *testing.T) {
	for _, profile := range []TranscodingProfile{
		audioProfilesTestProfile("http", "m4a", "aac"),
		audioProfilesTestProfile("hls", "ts", "aac"),
	} {
		t.Run(profile.Protocol, func(t *testing.T) {
			limits := conversionTestLimits()
			limits.Hardware = transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD128"}
			decision := audioProfilesTestPlan(t, progressiveAudioTestSource(), audioProfilesTestRequest(profile), limits, profile.Protocol, 0)
			if decision.Plan.Hardware != (transcode.Hardware{}) {
				t.Fatalf("audio-only conversion requested unrelated video hardware: %+v", decision.Plan)
			}
		})
	}
}

func TestPlanAudioConversionPreservesCallerMetadataAndExactSamples(t *testing.T) {
	source := progressiveAudioTestExactSource(289_792, 48_000)
	source.Info.Streams[0].Index = 9
	source.Info.Streams[0].Language, source.Info.Streams[0].Title = "eng", "Original track"
	source.Info.AudioDurationReason = "source presentation measured"
	request := audioProfilesTestRequest(audioProfilesTestProfile("http", "m4a", "aac"))
	request.AudioStreamIndex, request.StartTimeTicks = profileTestPtr(9), profileTestPtr(int64(1))
	request.AllowAudioStreamCopy = profileTestPtr(false)
	request.DeviceProfile.DirectPlayProfiles = []DirectPlayProfile{{Type: DlnaProfileTypeAudio, Container: "aac", AudioCodec: "aac"}}
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeAudio, Codec: "aac", Container: "m4a", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeEquals, "44100"),
		conversionRequired(ProfileConditionValueAudioBitrate, ProfileConditionTypeEquals, "128000"),
	}}}
	before, err := json.Marshal(struct {
		Source  Source
		Request Request
	}{source, request})
	if err != nil {
		t.Fatal(err)
	}
	decision := audioProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	after, err := json.Marshal(struct {
		Source  Source
		Request Request
	}{source, request})
	if err != nil || string(before) != string(after) {
		t.Fatalf("candidate planning mutated caller-owned source or profile metadata: %v", err)
	}
	count, err := transcode.ProgressiveOutputSamples(*decision.Plan, 44_100)
	if err != nil || count != 266_246 || decision.Plan.AudioSourceSampleCount != 289_792 || decision.Plan.AudioSourceSampleRate != 48_000 {
		t.Fatalf("profile adaptation lost exact input samples before seek and resampling: plan=%+v count=%d err=%v", decision.Plan, count, err)
	}
	output := decision.OutputSource.Info
	if output.AudioDurationExact || output.AudioDurationReason != "" || output.Streams[0].AudioTiming != nil || output.Streams[0].Language != "" || output.Streams[0].Title != "" {
		t.Fatalf("projected output inherited measurements or labels from the original file: %+v", output)
	}
	source.Info.Streams[0].AudioTiming.SampleCount++
	request.DeviceProfile.CodecProfiles[0].Conditions[0].Value = "22050"
	if decision.Plan.AudioSourceSampleCount != 289_792 || decision.Plan.AudioSampleRate != 44_100 || decision.OutputSource.Info.Streams[0].SampleRate != 44_100 {
		t.Fatal("the completed plan retained mutable caller-owned source or condition state")
	}
}

func TestPlanAudioConversionRejectsTopLevelStructuralErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Source, *Request, *ConversionLimits)
		want   error
	}{
		{"invalid source identity", func(source *Source, _ *Request, _ *ConversionLimits) { source.ItemID = "" }, ErrInvalidSource},
		{"duplicate stream index", func(source *Source, _ *Request, _ *ConversionLimits) { source.Info.Streams[2].Index = 5 }, ErrInvalidSource},
		{"wrong item", func(_ *Source, request *Request, _ *ConversionLimits) { request.ID = "another-item" }, ErrInvalidRequest},
		{"missing selected audio", func(_ *Source, request *Request, _ *ConversionLimits) { request.AudioStreamIndex = profileTestPtr(999) }, ErrInvalidRequest},
		{"negative start", func(_ *Source, request *Request, _ *ConversionLimits) {
			request.StartTimeTicks = profileTestPtr(int64(-1))
		}, ErrInvalidRequest},
		{"invalid server budget", func(_ *Source, _ *Request, limits *ConversionLimits) { limits.MaxBitrate = -1 }, ErrInvalidRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, request, limits := progressiveAudioTestSource(), audioProfilesTestRequest(audioProfilesTestProfile("http", "mp3", "mp3")), conversionTestLimits()
			test.mutate(&source, &request, &limits)
			decision, err := PlanAudioConversion(source, request, limits)
			if !errors.Is(err, test.want) || decision.Plan != nil || decision.SelectedProfileIndex != nil {
				t.Fatalf("top-level structural failure became a conversion candidate: %+v, %v", decision, err)
			}
		})
	}
}

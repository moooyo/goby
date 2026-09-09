package playback

import (
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func conversionTestSource() Source {
	source := profileTestSource()
	source.Info.Streams[0].BitDepth = 8
	source.Info.Streams[0].PixelFormat = "yuv420p"
	source.Info.Streams[0].InterlaceKnown = true
	source.Info.Streams[0].TimeBase = "1/15360"
	source.Info.Streams[1].TimeBase = "1/48000"
	return source
}

func conversionTestRequest() Request {
	request := profileTestRequest()
	request.DeviceProfile.TranscodingProfiles = []TranscodingProfile{{
		Type: DlnaProfileTypeVideo, Container: "ts", Protocol: "hls", Context: EncodingContextStreaming,
		VideoCodec: "h264", AudioCodec: "aac,mp3",
	}}
	return request
}

func conversionTestLimits() ConversionLimits {
	return ConversionLimits{AllowRemux: true, AllowAudioTranscode: true, AllowVideoTranscode: true}
}

func conversionTestPlan(t *testing.T, source Source, request Request, limits ConversionLimits) ConversionDecision {
	t.Helper()
	decision, err := PlanConversion(source, request, limits)
	if err != nil || decision.Plan == nil {
		t.Fatalf("expected an HLS conversion, got %+v, %v", decision, err)
	}
	if err := transcode.ValidatePlan(*decision.Plan); err != nil {
		t.Fatalf("planner returned a plan the runner cannot execute: %v", err)
	}
	if !decision.Output.OriginalCompatible || !decision.Output.ProfileMatched {
		t.Fatalf("projected output did not satisfy the client profile: %+v", decision.Output)
	}
	return decision
}

func conversionTestDeclined(t *testing.T, source Source, request Request, limits ConversionLimits) ConversionDecision {
	t.Helper()
	decision, err := PlanConversion(source, request, limits)
	if err != nil || decision.Plan != nil || len(decision.Reasons) == 0 {
		t.Fatalf("expected an explained conversion refusal, got %+v, %v", decision, err)
	}
	return decision
}

func conversionRequired(property ProfileConditionValue, operator ProfileConditionType, value string) ProfileCondition {
	return ProfileCondition{Property: property, Condition: operator, Value: value, IsRequired: profileTestPtr(true)}
}

func TestPlanConversionRemuxPreservesOriginalEvaluationAndInputs(t *testing.T) {
	source, request := conversionTestSource(), conversionTestRequest()
	wantOriginal, err := Evaluate(source, request)
	if err != nil {
		t.Fatal(err)
	}
	sourceCopy := source
	sourceCopy.Info.Streams = append([]media.Stream(nil), source.Info.Streams...)
	profileCopy := *request.DeviceProfile
	profileCopy.DirectPlayProfiles = append([]DirectPlayProfile(nil), profileCopy.DirectPlayProfiles...)
	profileCopy.TranscodingProfiles = append([]TranscodingProfile(nil), profileCopy.TranscodingProfiles...)
	decision := conversionTestPlan(t, source, request, conversionTestLimits())
	if !reflect.DeepEqual(decision.Original, wantOriginal) || !reflect.DeepEqual(source, sourceCopy) || !reflect.DeepEqual(*request.DeviceProfile, profileCopy) {
		t.Fatal("conversion planning changed the original decision or caller-owned inputs")
	}
	plan := decision.Plan
	if decision.Method != "DirectStream" || plan.VideoCodec != "copy" || plan.AudioCodec != "copy" ||
		plan.VideoStreamIndex != 2 || plan.AudioStreamIndex != 5 || plan.SegmentSeconds != 6 || plan.Width != 0 || plan.AudioBitrate != 0 {
		t.Fatalf("unexpected remux plan: %+v", decision)
	}
	video := decision.OutputSource.Info.Streams[0]
	if video.Width != 1920 || video.IsAVC || !video.IsAVCKnown || len(decision.OutputSource.Info.Streams) != 2 || video.CodecTag != "" ||
		video.TimeBase != "" || decision.OutputSource.Info.Streams[1].TimeBase != "" {
		t.Fatalf("output projection retained original container or unselected streams: %+v", decision.OutputSource)
	}
}

func TestPlanConversionAuthorizationsAndCopyFlags(t *testing.T) {
	for _, test := range []struct {
		name         string
		mutate       func(*Request, *ConversionLimits)
		video, audio string
		decline      bool
	}{
		{"all permissions absent", func(_ *Request, limits *ConversionLimits) { *limits = ConversionLimits{} }, "", "", true},
		{"transcoding disabled still permits remux", func(request *Request, _ *ConversionLimits) { request.EnableTranscoding = profileTestPtr(false) }, "copy", "copy", false},
		{"direct stream disabled requires an encoder", func(request *Request, _ *ConversionLimits) { request.EnableDirectStream = profileTestPtr(false) }, "copy", "aac", false},
		{"both conversion modes disabled", func(request *Request, _ *ConversionLimits) {
			request.EnableDirectStream = profileTestPtr(false)
			request.EnableTranscoding = profileTestPtr(false)
		}, "", "", true},
		{"video copy disabled", func(request *Request, _ *ConversionLimits) { request.AllowVideoStreamCopy = profileTestPtr(false) }, "h264", "copy", false},
		{"audio copy disabled", func(request *Request, _ *ConversionLimits) { request.AllowAudioStreamCopy = profileTestPtr(false) }, "copy", "aac", false},
		{"all stream copy disabled", func(request *Request, _ *ConversionLimits) {
			request.AllowVideoStreamCopy = profileTestPtr(false)
			request.AllowAudioStreamCopy = profileTestPtr(false)
		}, "h264", "aac", false},
		{"video transcode forbidden", func(request *Request, limits *ConversionLimits) {
			request.AllowVideoStreamCopy = profileTestPtr(false)
			limits.AllowVideoTranscode = false
		}, "", "", true},
		{"audio transcode forbidden", func(request *Request, limits *ConversionLimits) {
			request.AllowAudioStreamCopy = profileTestPtr(false)
			limits.AllowAudioTranscode = false
		}, "", "", true},
		{"copy prohibition survives disabled transcoding", func(request *Request, _ *ConversionLimits) {
			request.AllowAudioStreamCopy = profileTestPtr(false)
			request.EnableTranscoding = profileTestPtr(false)
		}, "", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, limits := conversionTestRequest(), conversionTestLimits()
			test.mutate(&request, &limits)
			if test.decline {
				conversionTestDeclined(t, conversionTestSource(), request, limits)
				return
			}
			decision := conversionTestPlan(t, conversionTestSource(), request, limits)
			if decision.Plan.VideoCodec != test.video || decision.Plan.AudioCodec != test.audio {
				t.Fatalf("got %+v, expected video=%s audio=%s", decision.Plan, test.video, test.audio)
			}
		})
	}
}

func TestPlanConversionSelectedAudioAndChannelLimits(t *testing.T) {
	for _, channels := range []int{0, 2} {
		t.Run(map[int]string{0: "preserve surround", 2: "requested downmix"}[channels], func(t *testing.T) {
			request := conversionTestRequest()
			request.AudioStreamIndex = profileTestPtr(9)
			if channels != 0 {
				request.MaxAudioChannels = &channels
			}
			decision := conversionTestPlan(t, conversionTestSource(), request, conversionTestLimits())
			wantChannels := channels
			if channels == 0 {
				wantChannels = 6
			}
			if decision.Plan.VideoCodec != "copy" || decision.Plan.AudioCodec != "aac" || decision.Plan.AudioStreamIndex != 9 || decision.Plan.AudioChannels != wantChannels {
				t.Fatalf("selected audio was not converted with the requested channel policy: %+v", decision.Plan)
			}
		})
	}
}

func TestPlanConversionTriesEverySupportedAudioCodecBeforeEncodingVideo(t *testing.T) {
	request := conversionTestRequest()
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideoAudio, Codec: "aac", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioProfile, ProfileConditionTypeEquals, "HE-AAC"),
	}}}
	decision := conversionTestPlan(t, conversionTestSource(), request, conversionTestLimits())
	if decision.Plan.VideoCodec != "copy" || decision.Plan.AudioCodec != "mp3" {
		t.Fatalf("a feasible MP3 output was not considered after AAC failed: %+v", decision.Plan)
	}
}

func TestPlanConversionFitsEncodedAudioIntoRemainingTotalBudget(t *testing.T) {
	request := conversionTestRequest()
	request.MaxStreamingBitrate = profileTestPtr(int64(4_000_000))
	decision := conversionTestPlan(t, conversionTestSource(), request, conversionTestLimits())
	if decision.Plan.VideoCodec != "copy" || decision.Plan.AudioCodec != "aac" || decision.Plan.AudioBitrate != 100_000 {
		t.Fatalf("a feasible audio-only conversion was not budgeted: %+v", decision.Plan)
	}
}

func TestPlanConversionAppliesConditionsAfterDownmixAndResize(t *testing.T) {
	request := conversionTestRequest()
	request.AudioStreamIndex = profileTestPtr(9)
	request.MaxAudioChannels = profileTestPtr(2)
	request.DeviceProfile.TranscodingProfiles[0].MaxWidth = profileTestPtr(1280)
	request.DeviceProfile.CodecProfiles = []CodecProfile{
		{Type: CodecTypeVideoAudio, Codec: "aac", ApplyConditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeLessThanEqual, "2"),
		}, Conditions: []ProfileCondition{conversionRequired(ProfileConditionValueAudioBitrate, ProfileConditionTypeLessThanEqual, "128000")}},
		{Type: CodecTypeVideo, Codec: "h264", ApplyConditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeLessThanEqual, "1280"),
		}, Conditions: []ProfileCondition{conversionRequired(ProfileConditionValueVideoBitrate, ProfileConditionTypeLessThanEqual, "1000000")}},
	}
	decision := conversionTestPlan(t, conversionTestSource(), request, conversionTestLimits())
	if decision.Plan.VideoCodec != "h264" || decision.Plan.VideoBitrate != 1_000_000 || decision.Plan.AudioBitrate != 128_000 {
		t.Fatalf("conditions newly applicable to the output were not used to construct it: %+v", decision.Plan)
	}
}

func TestPlanConversionNormalizesContainerAliasesForEveryConstraint(t *testing.T) {
	request := conversionTestRequest()
	request.DeviceProfile.TranscodingProfiles[0].Container = "mpegts"
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Container: "mpegts", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeLessThanEqual, "640"),
	}}}
	request.DeviceProfile.ContainerProfiles = []ContainerProfile{{Type: DlnaProfileTypeVideo, Container: "MPEGTS", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueHeight, ProfileConditionTypeLessThanEqual, "180"),
	}}}
	decision := conversionTestPlan(t, conversionTestSource(), request, conversionTestLimits())
	if decision.Plan.VideoCodec != "h264" || decision.Plan.Width > 640 || decision.Plan.Height > 180 {
		t.Fatalf("MPEG-TS alias skipped an applicable output constraint: %+v", decision.Plan)
	}
	if request.DeviceProfile.CodecProfiles[0].Container != "mpegts" || request.DeviceProfile.TranscodingProfiles[0].Container != "mpegts" {
		t.Fatal("normalizing output aliases mutated the caller's device profile")
	}
}

func TestPlanConversionRejectsUnverifiedInterlacedCopyWhenDisabled(t *testing.T) {
	for _, profileFlag := range []bool{false, true} {
		source, request := conversionTestSource(), conversionTestRequest()
		source.Info.Streams[0].InterlaceKnown = false
		if profileFlag {
			request.DeviceProfile.TranscodingProfiles[0].AllowInterlacedVideoStreamCopy = profileTestPtr(false)
		} else {
			request.AllowInterlacedVideoStreamCopy = profileTestPtr(false)
		}
		decision := conversionTestPlan(t, source, request, conversionTestLimits())
		if decision.Plan.VideoCodec != "h264" {
			t.Fatalf("unverified source was copied despite an explicit interlace restriction: %+v", decision.Plan)
		}
	}
}

func TestPlanConversionProjectsEncodedFactsBeforeCheckingClientConditions(t *testing.T) {
	source, request := conversionTestSource(), conversionTestRequest()
	source.Info.Streams[0].Codec = "hevc"
	source.Info.Streams[0].BitDepth = 10
	source.Info.Streams[0].VideoRange, source.Info.Streams[0].VideoRangeKnown = "SDR", true
	source.Info.Streams[0].ColorRange, source.Info.Streams[0].ColorTransfer = "tv", "bt709"
	request.AudioStreamIndex = profileTestPtr(9)
	request.MaxStreamingBitrate = profileTestPtr(int64(2_000_000))
	request.DeviceProfile.CodecProfiles = []CodecProfile{
		{Type: CodecTypeVideo, Codec: "h264", Container: "ts", Conditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueVideoBitDepth, ProfileConditionTypeLessThanEqual, "8"),
			conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeLessThanEqual, "1280"),
			conversionRequired(ProfileConditionValueHeight, ProfileConditionTypeLessThanEqual, "720"),
			conversionRequired(ProfileConditionValueVideoFramerate, ProfileConditionTypeLessThanEqual, "24"),
			conversionRequired(ProfileConditionValueVideoBitrate, ProfileConditionTypeLessThanEqual, "1000000"),
		}},
		{Type: CodecTypeVideoAudio, Codec: "aac", Container: "ts", Conditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeLessThanEqual, "2"),
			conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeLessThanEqual, "44100"),
			conversionRequired(ProfileConditionValueAudioBitrate, ProfileConditionTypeLessThanEqual, "128000"),
			conversionRequired(ProfileConditionValueAudioProfile, ProfileConditionTypeEquals, "LC"),
		}},
	}
	decision := conversionTestPlan(t, source, request, conversionTestLimits())
	plan := decision.Plan
	if plan.VideoCodec != "h264" || plan.AudioCodec != "aac" || plan.Width != 1280 || plan.Height != 720 || plan.FrameRate != 24 ||
		plan.VideoBitrate != 1_000_000 || plan.AudioChannels != 2 || plan.AudioSampleRate != 44_100 || plan.AudioBitrate != 128_000 {
		t.Fatalf("output constraints were not applied to encoder parameters: %+v", plan)
	}
	if decision.Original.OriginalCompatible || decision.OutputSource.Info.Streams[0].BitDepth != 8 {
		t.Fatalf("original mismatch and encoded output were conflated: %+v", decision)
	}
	video := decision.OutputSource.Info.Streams[0]
	if video.TimeBase != "" || video.ColorRange != "" || video.ColorTransfer != "" {
		t.Fatalf("encoding retained unverified container or color metadata: %+v", video)
	}
}

func TestPlanConversionRechecksOutputPredicatesInsteadOfDiscardingInputConstraints(t *testing.T) {
	for _, condition := range []ProfileCondition{
		conversionRequired(ProfileConditionValueVideoBitDepth, ProfileConditionTypeGreaterThanEqual, "10"),
		conversionRequired(ProfileConditionValueVideoProfile, ProfileConditionTypeEquals, "Baseline"),
		conversionRequired(ProfileConditionValueVideoLevel, ProfileConditionTypeLessThanEqual, "41"),
		conversionRequired(ProfileConditionValueRefFrames, ProfileConditionTypeLessThanEqual, "4"),
		conversionRequired(ProfileConditionValue("UnknownOutputFact"), ProfileConditionTypeEquals, "true"),
		conversionRequired(ProfileConditionValueVideoRange, ProfileConditionTypeEquals, "SDR"),
		conversionRequired(ProfileConditionValueVideoBitDepth, ProfileConditionType("Regex"), "8"),
	} {
		t.Run(string(condition.Property)+string(condition.Condition), func(t *testing.T) {
			request := conversionTestRequest()
			request.AllowVideoStreamCopy = profileTestPtr(false)
			request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{condition}}}
			conversionTestDeclined(t, conversionTestSource(), request, conversionTestLimits())
		})
	}
	request := conversionTestRequest()
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{
		{Property: "UnknownOptionalFact", Condition: ProfileConditionTypeEquals, Value: "true"},
	}}}
	decision := conversionTestPlan(t, conversionTestSource(), request, conversionTestLimits())
	if !decision.Output.ClientMustValidate {
		t.Fatal("optional unknown conditions must remain visible as unverified")
	}
}

func TestPlanConversionProfileScopeAndUnsupportedFeatures(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Source, *Request)
	}{
		{"missing profile", func(_ *Source, request *Request) { request.DeviceProfile = nil }},
		{"missing HLS profile", func(_ *Source, request *Request) { request.DeviceProfile.TranscodingProfiles = nil }},
		{"static context", func(_ *Source, request *Request) {
			request.DeviceProfile.TranscodingProfiles[0].Context = EncodingContextStatic
		}},
		{"DASH", func(_ *Source, request *Request) { request.DeviceProfile.TranscodingProfiles[0].Protocol = "dash" }},
		{"fragmented MP4", func(_ *Source, request *Request) { request.DeviceProfile.TranscodingProfiles[0].Container = "mp4" }},
		{"HEVC output only", func(_ *Source, request *Request) { request.DeviceProfile.TranscodingProfiles[0].VideoCodec = "hevc" }},
		{"AV1 output only", func(_ *Source, request *Request) { request.DeviceProfile.TranscodingProfiles[0].VideoCodec = "av1" }},
		{"Opus output only", func(_ *Source, request *Request) { request.DeviceProfile.TranscodingProfiles[0].AudioCodec = "opus" }},
		{"HDR PQ", func(source *Source, _ *Request) { source.Info.Streams[0].ColorTransfer = "smpte2084" }},
		{"HDR HLG", func(source *Source, _ *Request) { source.Info.Streams[0].ColorTransfer = "arib-std-b67" }},
		{"unclassified 10 bit", func(source *Source, _ *Request) { source.Info.Streams[0].BitDepth = 10 }},
		{"unclassified 10 bit pixel format", func(source *Source, _ *Request) {
			source.Info.Streams[0].BitDepth = 0
			source.Info.Streams[0].PixelFormat = "p010le"
		}},
		{"interlaced even if client permits copy", func(source *Source, request *Request) {
			source.Info.Streams[0].IsInterlaced = true
			request.AllowInterlacedVideoStreamCopy = profileTestPtr(true)
		}},
		{"embedded text subtitle", func(_ *Source, request *Request) { request.SubtitleStreamIndex = profileTestPtr(12) }},
		{"external audio", func(source *Source, _ *Request) { source.Info.Streams[1].IsExternal = true }},
		{"zero duration", func(source *Source, _ *Request) { source.Info.DurationTicks = 0 }},
		{"start at end", func(source *Source, request *Request) { request.StartTimeTicks = &source.Info.DurationTicks }},
		{"timestamp copy", func(_ *Source, request *Request) {
			request.DeviceProfile.TranscodingProfiles[0].CopyTimestamps = profileTestPtr(true)
		}},
		{"minimum buffer unsupported", func(_ *Source, request *Request) {
			request.DeviceProfile.TranscodingProfiles[0].MinSegments = profileTestPtr(3)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, request := conversionTestSource(), conversionTestRequest()
			test.mutate(&source, &request)
			conversionTestDeclined(t, source, request, conversionTestLimits())
		})
	}
}

func TestPlanConversionExternalTextSubtitleUsesOutputContainer(t *testing.T) {
	source, request := conversionTestSource(), conversionTestRequest()
	source.Info.Streams[3].Codec, source.Info.Streams[3].IsExternal = "srt", true
	request.SubtitleStreamIndex = profileTestPtr(12)
	request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{{Format: "vtt", Container: "mpegts", Method: SubtitleDeliveryMethodExternal, Protocol: "http"}}
	decision := conversionTestPlan(t, source, request, conversionTestLimits())
	if decision.Output.SubtitleFormat != "vtt" || decision.Output.SubtitleMethod != SubtitleDeliveryMethodExternal || decision.Original.OriginalCompatible {
		t.Fatalf("external subtitle capability was not evaluated against the HLS container: %+v", decision)
	}
	request.DeviceProfile.SubtitleProfiles[0].Container = "mp4"
	conversionTestDeclined(t, source, request, conversionTestLimits())
	request.DeviceProfile.SubtitleProfiles[0].Container = "ts"
	source.Info.Streams[3].Codec, source.Info.Streams[3].IsTextSubtitleStream = "hdmv_pgs_subtitle", false
	conversionTestDeclined(t, source, request, conversionTestLimits())
}

func TestPlanConversionAudioOnlyAndMP3(t *testing.T) {
	source := conversionTestSource()
	source.ItemType = "Audio"
	source.Info.Streams = []media.Stream{source.Info.Streams[2]}
	source.Info.Streams[0].Codec = "flac"
	source.Info.Bitrate = 600_000
	request := conversionTestRequest()
	request.DeviceProfile.TranscodingProfiles[0].Type = DlnaProfileTypeAudio
	request.DeviceProfile.TranscodingProfiles[0].AudioCodec = "mp3"
	request.DeviceProfile.MaxStaticMusicBitrate = profileTestPtr(32_000)
	request.DeviceProfile.MusicStreamingTranscodingBitrate = profileTestPtr(170_000)
	decision := conversionTestPlan(t, source, request, conversionTestLimits())
	plan := decision.Plan
	if plan.VideoStreamIndex != -1 || plan.VideoCodec != "" || plan.AudioStreamIndex != 9 || plan.AudioCodec != "mp3" ||
		plan.AudioChannels != 2 || plan.AudioBitrate != 160_000 || decision.Method != "Transcode" {
		t.Fatalf("unexpected audio-only HLS plan: %+v", plan)
	}
	if decision.OutputSource.Info.Bitrate > 200_000 || len(decision.OutputSource.Info.Streams) != 1 {
		t.Fatalf("audio output retained unrelated streams or bitrate facts: %+v", decision.OutputSource)
	}
}

func TestPlanConversionHardwarePolicyOnlyAppliesToEncodedVideo(t *testing.T) {
	request, limits := conversionTestRequest(), conversionTestLimits()
	limits.Hardware = transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD128"}
	decision := conversionTestPlan(t, conversionTestSource(), request, limits)
	if decision.Plan.Hardware != (transcode.Hardware{}) {
		t.Fatal("remux unexpectedly requested a hardware decoder or encoder")
	}
	request.AllowVideoStreamCopy = profileTestPtr(false)
	decision = conversionTestPlan(t, conversionTestSource(), request, limits)
	if decision.Plan.Hardware != limits.Hardware {
		t.Fatal("encoded video lost the requested hardware policy")
	}
}

func TestPlanConversionDurationSegmentAndServerCaps(t *testing.T) {
	request, limits := conversionTestRequest(), conversionTestLimits()
	request.StartTimeTicks = profileTestPtr(17 * media.TicksPerSecond)
	request.DeviceProfile.TranscodingProfiles[0].SegmentLength = profileTestPtr(3)
	limits.MaxWidth, limits.MaxHeight, limits.MaxBitrate, limits.MaxAudioChannels = 640, 480, 900_000, 1
	decision := conversionTestPlan(t, conversionTestSource(), request, limits)
	plan := decision.Plan
	if plan.StartTicks != 17*media.TicksPerSecond || plan.DurationTicks != 90*media.TicksPerSecond || plan.SegmentSeconds != 3 ||
		plan.Width > 640 || plan.Height > 480 || plan.AudioChannels != 1 || decision.OutputSource.Info.Bitrate > 900_000 {
		t.Fatalf("server caps or timeline were ignored: %+v", decision)
	}
}

func TestPlanConversionRejectsMalformedInputWithoutPlan(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Source, *Request, *ConversionLimits)
	}{
		{"invalid server limit", func(_ *Source, _ *Request, limits *ConversionLimits) { limits.MaxBitrate = -1 }},
		{"invalid channels string", func(_ *Source, request *Request, _ *ConversionLimits) {
			request.DeviceProfile.TranscodingProfiles[0].MaxAudioChannels = "two"
		}},
		{"invalid output dimensions", func(_ *Source, request *Request, _ *ConversionLimits) {
			request.DeviceProfile.TranscodingProfiles[0].MaxWidth = profileTestPtr(-1)
		}},
		{"invalid selected stream", func(_ *Source, request *Request, _ *ConversionLimits) { request.AudioStreamIndex = profileTestPtr(999) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, request, limits := conversionTestSource(), conversionTestRequest(), conversionTestLimits()
			test.mutate(&source, &request, &limits)
			decision, err := PlanConversion(source, request, limits)
			if !errors.Is(err, ErrInvalidRequest) || decision.Plan != nil {
				t.Fatalf("got %+v, %v", decision, err)
			}
		})
	}
}

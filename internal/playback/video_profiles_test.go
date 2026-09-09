package playback

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func videoProfilesTestSource() Source {
	source := conversionTestSource()
	source.Info.FormatStartKnown = true
	source.Info.Streams[0].IsAVCKnown, source.Info.Streams[0].IsAVC = true, true
	source.Info.Streams[1].Profile = "LC"
	source.Info.Streams[2].SampleRate = 48_000
	return source
}

func videoProfilesTestProfile(protocol, container string) TranscodingProfile {
	return TranscodingProfile{Type: DlnaProfileTypeVideo, Protocol: protocol, Context: EncodingContextStreaming,
		Container: container, VideoCodec: "h264", AudioCodec: "aac"}
}

func videoProfilesTestRequest(profiles ...TranscodingProfile) Request {
	return Request{DeviceProfile: &DeviceProfile{SupportedMediaTypes: "Video", TranscodingProfiles: profiles}}
}

func videoProfilesTestPlan(t *testing.T, source Source, request Request, limits ConversionLimits, protocol string, index int) ConversionDecision {
	t.Helper()
	original, err := Evaluate(source, request)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := PlanVideoConversion(source, request, limits)
	if err != nil || decision.Plan == nil {
		t.Fatalf("expected a video conversion, got %+v, %v", decision, err)
	}
	if !reflect.DeepEqual(decision.Original, original) {
		t.Fatalf("conversion changed original-file evaluation: got %+v, want %+v", decision.Original, original)
	}
	if decision.SelectedProtocol != protocol || decision.SelectedProfileIndex == nil || *decision.SelectedProfileIndex != index {
		t.Fatalf("wrong ordered selection: protocol=%q index=%v, want %q/%d", decision.SelectedProtocol, decision.SelectedProfileIndex, protocol, index)
	}
	if !decision.Output.OriginalCompatible || !decision.Output.ProfileMatched {
		t.Fatalf("selected output does not satisfy the client profile: %+v", decision.Output)
	}
	if err := transcode.ValidatePlan(*decision.Plan); err != nil {
		t.Fatalf("selected output cannot be executed: %v", err)
	}
	return decision
}

func videoProfilesTestDeclined(t *testing.T, source Source, request Request, limits ConversionLimits) ConversionDecision {
	t.Helper()
	original, err := Evaluate(source, request)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := PlanVideoConversion(source, request, limits)
	if err != nil || decision.Plan != nil || len(decision.Reasons) == 0 || decision.SelectedProfileIndex != nil {
		t.Fatalf("expected an explained unsupported decision without a selected profile, got %+v, %v", decision, err)
	}
	if !reflect.DeepEqual(decision.Original, original) {
		t.Fatal("an unsupported conversion changed original-file evaluation")
	}
	return decision
}

func TestPlanVideoConversionPreservesMixedProtocolOrder(t *testing.T) {
	http, hls := videoProfilesTestProfile("http", "mp4"), videoProfilesTestProfile("hls", "ts")
	for _, test := range []struct {
		name, protocol string
		profiles       []TranscodingProfile
	}{
		{"progressive first", "http", []TranscodingProfile{http, hls}},
		{"HLS first", "hls", []TranscodingProfile{hls, http}},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision := videoProfilesTestPlan(t, videoProfilesTestSource(), videoProfilesTestRequest(test.profiles...), conversionTestLimits(), test.protocol, 0)
			if (decision.Plan.OutputMode == "progressive") != (test.protocol == "http") {
				t.Fatalf("execution mode differs from the selected protocol: %+v", decision.Plan)
			}
			if decision.Plan.VideoCodec != "copy" || decision.Plan.AudioCodec != "copy" || decision.Method != "DirectStream" {
				t.Fatalf("a constructible remux was replaced by unnecessary encoding: %+v", decision)
			}
		})
	}
}

func TestPlanVideoConversionUsesReferenceDeliveryDefaults(t *testing.T) {
	for _, text := range []string{
		`{"Type":"Video","Container":"mp4","VideoCodec":"h264","AudioCodec":"aac"}`,
		`{"Type":"Video","Container":"mp4","VideoCodec":"h264","AudioCodec":"aac","Protocol":"","Context":""}`,
		`{"Type":"Video","Container":"mp4","VideoCodec":"h264","AudioCodec":"aac","Protocol":"http"}`,
		`{"Type":"Video","Container":"MP4","VideoCodec":"H264","AudioCodec":"AAC","Protocol":"HTTP","Context":"streaming"}`,
	} {
		t.Run(text, func(t *testing.T) {
			var profile TranscodingProfile
			if err := json.Unmarshal([]byte(text), &profile); err != nil {
				t.Fatal(err)
			}
			request := videoProfilesTestRequest(profile)
			decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, conversionTestLimits(), "http", 0)
			if decision.Plan.Container != "mp4" || request.DeviceProfile.TranscodingProfiles[0] != profile {
				t.Fatal("delivery defaults changed the requested profile or selected container")
			}
		})
	}
}

func TestPlanVideoConversionRequiresLocalVideoAndDeclaredCapabilities(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Source, *Request)
	}{
		{"no device profile", func(_ *Source, request *Request) { request.DeviceProfile = nil }},
		{"audio-only media", func(source *Source, _ *Request) { source.ItemType = "Audio" }},
		{"live source", func(_ *Source, request *Request) { request.LiveStreamID = "live-source" }},
		{"external video", func(source *Source, _ *Request) { source.Info.Streams[0].IsExternal = true }},
		{"external audio", func(source *Source, _ *Request) { source.Info.Streams[1].IsExternal = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, request := videoProfilesTestSource(), videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
			test.mutate(&source, &request)
			videoProfilesTestDeclined(t, source, request, conversionTestLimits())
		})
	}
}

func TestPlanVideoConversionRequiresKnownProgressiveSourceClock(t *testing.T) {
	source := videoProfilesTestSource()
	source.Info.FormatStartKnown = false
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	videoProfilesTestDeclined(t, source, request, conversionTestLimits())
	request.DeviceProfile.TranscodingProfiles = append(request.DeviceProfile.TranscodingProfiles, videoProfilesTestProfile("hls", "ts"))
	videoProfilesTestPlan(t, source, request, conversionTestLimits(), "hls", 1)
}

func TestPlanVideoConversionPreservesUnrequestedVideoTiming(t *testing.T) {
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.AllowVideoStreamCopy = profileTestPtr(false)
	decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, conversionTestLimits(), "http", 0)
	if decision.Plan.FrameRate != 0 || decision.OutputSource.Info.Streams[0].AverageFrameRate != "" || decision.OutputSource.Info.Streams[0].RealFrameRate != "" {
		t.Fatalf("a profile without frame-rate constraints invented constant-rate output: %+v", decision)
	}
	request.AllowVideoStreamCopy = nil
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueVideoFramerate, ProfileConditionTypeEquals, "30000/1001"),
	}}}
	decision = videoProfilesTestPlan(t, videoProfilesTestSource(), request, conversionTestLimits(), "http", 0)
	if decision.Plan.VideoCodec != "copy" || decision.Plan.FrameRate != 0 {
		t.Fatalf("verified original rational timing was unnecessarily replaced: %+v", decision.Plan)
	}
	request.AllowVideoStreamCopy = profileTestPtr(false)
	videoProfilesTestDeclined(t, videoProfilesTestSource(), request, conversionTestLimits())
}

func TestPlanVideoConversionSkipsUnusableLeadingProfiles(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*TranscodingProfile)
	}{
		{"wrong media type", func(profile *TranscodingProfile) { profile.Type = DlnaProfileTypeAudio }},
		{"unknown protocol", func(profile *TranscodingProfile) { profile.Protocol = "dash" }},
		{"empty container", func(profile *TranscodingProfile) { profile.Container = "" }},
		{"empty video codec", func(profile *TranscodingProfile) { profile.VideoCodec = "" }},
		{"empty audio codec", func(profile *TranscodingProfile) { profile.AudioCodec = "" }},
		{"unsupported output", func(profile *TranscodingProfile) { profile.Container = "webm" }},
		{"unsupported video codec", func(profile *TranscodingProfile) { profile.VideoCodec = "hevc" }},
		{"unsupported audio codec", func(profile *TranscodingProfile) { profile.AudioCodec = "opus" }},
		{"static context", func(profile *TranscodingProfile) { profile.Context = EncodingContextStatic }},
		{"malformed channels", func(profile *TranscodingProfile) { profile.MaxAudioChannels = "two" }},
		{"copy timestamps", func(profile *TranscodingProfile) { profile.CopyTimestamps = profileTestPtr(true) }},
		{"estimated size", func(profile *TranscodingProfile) { profile.EstimateContentLength = profileTestPtr(true) }},
		{"byte seek", func(profile *TranscodingProfile) { profile.TranscodeSeekInfo = TranscodeSeekInfoBytes }},
		{"manifest subtitles", func(profile *TranscodingProfile) { profile.ManifestSubtitles = "vtt" }},
		{"segment duration", func(profile *TranscodingProfile) { profile.SegmentLength = profileTestPtr(6) }},
		{"minimum segments", func(profile *TranscodingProfile) { profile.MinSegments = profileTestPtr(2) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			leading := videoProfilesTestProfile("http", "mp4")
			test.mutate(&leading)
			request := videoProfilesTestRequest(leading, videoProfilesTestProfile("hls", "ts"))
			videoProfilesTestPlan(t, videoProfilesTestSource(), request, conversionTestLimits(), "hls", 1)
		})
	}
}

func TestPlanVideoConversionDoesNotUseMPEGTSFactsForMP4(t *testing.T) {
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{
		{Type: CodecTypeVideo, Container: "mp4", Codec: "h264", Conditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueIsAvc, ProfileConditionTypeEquals, "true"),
			conversionRequired(ProfileConditionValueVideoCodecTag, ProfileConditionTypeEquals, "avc1"),
		}},
		{Type: CodecTypeVideo, Container: "ts", Codec: "h264", Conditions: []ProfileCondition{
			conversionRequired("UnimplementedTransportCapability", ProfileConditionTypeEquals, "supported"),
		}},
	}
	decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, conversionTestLimits(), "http", 0)
	if decision.OutputSource.Info.Container != "mp4" || decision.Plan.Container != "mp4" || decision.Plan.SegmentSeconds != 0 {
		t.Fatalf("HTTP profile was projected or executed using a different container: %+v", decision)
	}
	request.DeviceProfile.CodecProfiles[0].Conditions = append(request.DeviceProfile.CodecProfiles[0].Conditions,
		conversionRequired("UnimplementedMP4Capability", ProfileConditionTypeEquals, "supported"))
	videoProfilesTestDeclined(t, videoProfilesTestSource(), request, conversionTestLimits())
}

func TestPlanVideoConversionPreservesRemuxAndEncodingPermissions(t *testing.T) {
	for _, test := range []struct {
		name         string
		mutate       func(*Request, *ConversionLimits)
		video, audio string
		decline      bool
	}{
		{"authorized remux", func(_ *Request, _ *ConversionLimits) {}, "copy", "copy", false},
		{"no encoder permissions", func(_ *Request, limits *ConversionLimits) {
			limits.AllowAudioTranscode, limits.AllowVideoTranscode = false, false
		}, "copy", "copy", false},
		{"transcoding delivery permits remux", func(request *Request, limits *ConversionLimits) {
			request.EnableDirectPlay, request.EnableDirectStream = profileTestPtr(false), profileTestPtr(false)
			limits.AllowAudioTranscode, limits.AllowVideoTranscode = false, false
		}, "copy", "copy", false},
		{"transcoding disabled permits remux", func(request *Request, _ *ConversionLimits) { request.EnableTranscoding = profileTestPtr(false) }, "copy", "copy", false},
		{"video copy disabled", func(request *Request, _ *ConversionLimits) { request.AllowVideoStreamCopy = profileTestPtr(false) }, "h264", "copy", false},
		{"audio copy disabled", func(request *Request, _ *ConversionLimits) { request.AllowAudioStreamCopy = profileTestPtr(false) }, "copy", "aac", false},
		{"remux disabled permits authorized audio conversion", func(_ *Request, limits *ConversionLimits) { limits.AllowRemux = false }, "copy", "aac", false},
		{"all permissions absent", func(_ *Request, limits *ConversionLimits) { *limits = ConversionLimits{} }, "", "", true},
		{"both conversion modes disabled", func(request *Request, _ *ConversionLimits) {
			request.EnableDirectStream, request.EnableTranscoding = profileTestPtr(false), profileTestPtr(false)
		}, "", "", true},
		{"video copy cannot bypass encoding denial", func(request *Request, limits *ConversionLimits) {
			request.AllowVideoStreamCopy = profileTestPtr(false)
			limits.AllowVideoTranscode = false
		}, "", "", true},
		{"audio copy cannot bypass encoding denial", func(request *Request, limits *ConversionLimits) {
			request.AllowAudioStreamCopy = profileTestPtr(false)
			limits.AllowAudioTranscode = false
		}, "", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, limits := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4")), conversionTestLimits()
			test.mutate(&request, &limits)
			if test.decline {
				videoProfilesTestDeclined(t, videoProfilesTestSource(), request, limits)
				return
			}
			decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, limits, "http", 0)
			if decision.Plan.VideoCodec != test.video || decision.Plan.AudioCodec != test.audio {
				t.Fatalf("conversion permissions or stream-copy flags were not applied: %+v", decision.Plan)
			}
		})
	}
}

func TestPlanVideoConversionConstructsNumericOutputRequirements(t *testing.T) {
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.DeviceProfile.ContainerProfiles = []ContainerProfile{{Type: DlnaProfileTypeVideo, Container: "mp4", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeEquals, "1280"),
		conversionRequired(ProfileConditionValueHeight, ProfileConditionTypeEquals, "720"),
		conversionRequired(ProfileConditionValueVideoFramerate, ProfileConditionTypeEquals, "25"),
	}}}
	request.DeviceProfile.CodecProfiles = []CodecProfile{
		{Type: CodecTypeVideo, Codec: "h264", Container: "mp4", Conditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueVideoBitrate, ProfileConditionTypeEquals, "1200000"),
		}},
		{Type: CodecTypeVideoAudio, Codec: "aac", Container: "mp4", Conditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeEquals, "1"),
			conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeEquals, "44100"),
			conversionRequired(ProfileConditionValueAudioBitrate, ProfileConditionTypeEquals, "64000"),
		}},
	}
	decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, conversionTestLimits(), "http", 0)
	plan := decision.Plan
	if plan.VideoCodec != "h264" || plan.Width != 1280 || plan.Height != 720 || plan.FrameRate != 25 || plan.VideoBitrate != 1_200_000 ||
		plan.AudioCodec != "aac" || plan.AudioChannels != 1 || plan.AudioSampleRate != 44_100 || plan.AudioBitrate != 64_000 {
		t.Fatalf("profile requirements were not constructed as concrete encoder targets: %+v", plan)
	}
	if plan.AudioSourceSampleCount != 0 || plan.AudioSourceSampleRate != 0 || plan.AudioSampleSeek {
		t.Fatal("video conversion inherited audio-only presentation coordinates")
	}
}

func TestPlanVideoConversionConstructsMinimumSupportedVideoBitrate(t *testing.T) {
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.AllowVideoStreamCopy = profileTestPtr(false)
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueVideoBitrate, ProfileConditionTypeEquals, "64000"),
	}}}
	decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, conversionTestLimits(), "http", 0)
	if decision.Plan.VideoCodec != "h264" || decision.Plan.VideoBitrate != 64_000 {
		t.Fatalf("the profile search omitted the runner's supported minimum video bitrate: %+v", decision.Plan)
	}
}

func TestPlanVideoConversionKeepsAspectRatioForMaximumDimensions(t *testing.T) {
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeLessThanEqual, "1280"),
		conversionRequired(ProfileConditionValueHeight, ProfileConditionTypeLessThanEqual, "1080"),
	}}}
	decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, conversionTestLimits(), "http", 0)
	if decision.Plan.Width != 1280 || decision.Plan.Height != 720 {
		t.Fatalf("maximum geometry constraints unnecessarily stretched the picture: %+v", decision.Plan)
	}
}

func TestPlanVideoConversionFiltersNumericConditionsBeforeBoundingDomain(t *testing.T) {
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	var conditions []ProfileCondition
	for value := 10; value <= 210; value += 10 {
		conditions = append(conditions, conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeNotEquals, strconv.Itoa(value)))
	}
	conditions = append(conditions, conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeEquals, "1280"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: conditions}}
	decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, conversionTestLimits(), "http", 0)
	if decision.Plan.Width != 1280 || decision.Plan.Height != 720 {
		t.Fatalf("rejected numeric neighbors displaced a later feasible exact target: %+v", decision.Plan)
	}
}

func TestPlanVideoConversionConstructsFractionalFrameRateIntervals(t *testing.T) {
	source := videoProfilesTestSource()
	source.Info.Streams[0].AverageFrameRate = "30"
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueVideoFramerate, ProfileConditionTypeGreaterThanEqual, "24"),
		conversionRequired(ProfileConditionValueVideoFramerate, ProfileConditionTypeLessThanEqual, "24.5"),
		conversionRequired(ProfileConditionValueVideoFramerate, ProfileConditionTypeNotEquals, "24"),
		conversionRequired(ProfileConditionValueVideoFramerate, ProfileConditionTypeNotEquals, "24.5"),
	}}}
	decision := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	if decision.Plan.VideoCodec != "h264" || decision.Plan.FrameRate <= 24 || decision.Plan.FrameRate >= 24.5 {
		t.Fatalf("the bounded FPS domain omitted a constructible fractional interval: %+v", decision.Plan)
	}
}

func TestPlanVideoConversionConstructsEvenDimensionIntervals(t *testing.T) {
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeGreaterThanEqual, "1000"),
		conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeLessThanEqual, "1004"),
		conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeNotEquals, "1000"),
		conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeNotEquals, "1004"),
	}}}
	decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, conversionTestLimits(), "http", 0)
	if decision.Plan.Width != 1002 {
		t.Fatalf("the bounded geometry domain omitted its feasible even neighbor: %+v", decision.Plan)
	}
}

func TestPlanVideoConversionInterleavesAudioDomainCandidates(t *testing.T) {
	source := videoProfilesTestSource()
	source.Info.Streams[1].Channels, source.Info.Streams[1].SampleRate = 8, 96_000
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideoAudio, Codec: "aac", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeLessThanEqual, "8"),
		conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeLessThanEqual, "96000"),
		conversionRequired(ProfileConditionValueAudioBitrate, ProfileConditionTypeEquals, "8000"),
	}}}
	decision := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	if decision.Plan.AudioCodec != "aac" || decision.Plan.AudioChannels != 1 || decision.Plan.AudioBitrate != 8_000 {
		t.Fatalf("earlier multichannel variants displaced a constructible mono encoding: %+v", decision.Plan)
	}
}

func TestPlanVideoConversionSearchesAcrossGeometryAndAudioDomains(t *testing.T) {
	source := videoProfilesTestSource()
	source.Info.Streams[1].Channels, source.Info.Streams[1].SampleRate = 8, 96_000
	var widths []string
	for value := 1280; len(widths) < 63; value -= 2 {
		widths = append(widths, strconv.Itoa(value))
	}
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.DeviceProfile.ContainerProfiles = []ContainerProfile{{Type: DlnaProfileTypeVideo, Container: "mp4", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeEqualsAny, strings.Join(widths, ",")),
		conversionRequired(ProfileConditionValueAudioChannels, ProfileConditionTypeLessThanEqual, "8"),
		conversionRequired(ProfileConditionValueAudioBitrate, ProfileConditionTypeEquals, "8000"),
	}}}
	decision := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	if decision.Plan.Width != 1280 || decision.Plan.AudioChannels != 1 || decision.Plan.AudioBitrate != 8_000 {
		t.Fatalf("geometry alternatives displaced a feasible audio combination within the request budget: %+v", decision.Plan)
	}
}

func TestVideoProfileRequestsBoundsUnsuccessfulCombinationNodes(t *testing.T) {
	source := videoProfilesTestSource()
	streams, err := selectStreams(source, Request{})
	if err != nil {
		t.Fatal(err)
	}
	limits, err := normalizeConversionLimits(conversionTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	var widths, heights []string
	for value := 0; value < 63; value++ {
		widths = append(widths, strconv.Itoa(1280-value*2))
		heights = append(heights, strconv.Itoa(720-value*2))
	}
	conditions := []ProfileCondition{
		conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeEqualsAny, strings.Join(widths, ",")),
		conversionRequired(ProfileConditionValueHeight, ProfileConditionTypeEqualsAny, strings.Join(heights, ",")),
		conversionRequired(ProfileConditionValueVideoFramerate, ProfileConditionTypeLessThanEqual, "0.5"),
	}
	search := videoProfileBudget{audioProfileBudget: audioProfileBudget{remaining: maxVideoProfileAttempts}, nodes: 1024}
	count := 0
	for range videoProfileRequests(ProgressiveVideoRequest{OutputContainer: "mp4", VideoCodec: "h264", AudioCodec: "aac"}, streams, limits, conditions, &search) {
		count++
	}
	if count != 1 || !search.exhausted || search.nodes != 0 {
		t.Fatalf("empty later domains traversed an unbounded Cartesian tree: candidates=%d budget=%+v", count, search)
	}
}

func TestVideoProfileRequestsDeduplicatesNumericNeighbors(t *testing.T) {
	source := videoProfilesTestSource()
	streams, err := selectStreams(source, Request{})
	if err != nil {
		t.Fatal(err)
	}
	limits, err := normalizeConversionLimits(conversionTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	var conditions []ProfileCondition
	text := strings.TrimSuffix(strings.Repeat("1280,", 64), ",")
	for index := 0; index < 64; index++ {
		conditions = append(conditions, conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeEqualsAny, text))
	}
	search := videoProfileBudget{audioProfileBudget: audioProfileBudget{remaining: maxVideoProfileAttempts}, nodes: maxVideoProfileNodes}
	count := 0
	for range videoProfileRequests(ProgressiveVideoRequest{OutputContainer: "mp4", VideoCodec: "h264", AudioCodec: "aac"}, streams, limits, conditions, &search) {
		count++
	}
	if count != 2 || search.exhausted || search.nodes < maxVideoProfileNodes-4200 {
		t.Fatalf("duplicate neighbors consumed repeated predicate work: candidates=%d budget=%+v", count, search)
	}
}

func TestPlanVideoConversionPrefersVerifiedOptionalFrameRateOutput(t *testing.T) {
	source := videoProfilesTestSource()
	source.Info.Streams[0].Codec, source.Info.Streams[0].AverageFrameRate = "hevc", "60"
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{{
		Property: ProfileConditionValueVideoFramerate, Condition: ProfileConditionTypeLessThanEqual, Value: "30",
	}}}}
	decision := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	if decision.Output.ClientMustValidate || decision.Plan.FrameRate <= 0 || decision.Plan.FrameRate > 30 {
		t.Fatalf("unverified preserved timing displaced a constructible verified frame rate: %+v", decision)
	}
}

func TestPlanVideoConversionReevaluatesConditionalOutputConstraints(t *testing.T) {
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.DeviceProfile.TranscodingProfiles[0].MaxWidth = profileTestPtr(1280)
	request.DeviceProfile.CodecProfiles = []CodecProfile{
		{Type: CodecTypeVideo, Codec: "h264", Container: "mp4", ApplyConditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeLessThanEqual, "1280"),
		}, Conditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueVideoFramerate, ProfileConditionTypeLessThanEqual, "24"),
		}},
		{Type: CodecTypeVideoAudio, Codec: "aac", Container: "mp4", ApplyConditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeLessThanEqual, "1280"),
		}, Conditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueAudioSampleRate, ProfileConditionTypeEquals, "32000"),
		}},
	}
	decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, conversionTestLimits(), "http", 0)
	if decision.Plan.Width > 1280 || decision.Plan.FrameRate <= 0 || decision.Plan.FrameRate > 24 || decision.Plan.AudioSampleRate != 32_000 {
		t.Fatalf("constraints activated by resized output were not applied: %+v", decision.Plan)
	}
}

func TestPlanVideoConversionTriesEncodingWhenCopiedFactsAreUnverified(t *testing.T) {
	source := videoProfilesTestSource()
	source.Info.Streams[0].BitDepth, source.Info.Streams[1].Profile = 0, ""
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{
		{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueVideoBitDepth, ProfileConditionTypeEquals, "8"),
		}},
		{Type: CodecTypeVideoAudio, Codec: "aac", Conditions: []ProfileCondition{
			conversionRequired(ProfileConditionValueAudioProfile, ProfileConditionTypeEquals, "LC"),
		}},
	}
	decision := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	if decision.Plan.VideoCodec != "h264" || decision.Plan.AudioCodec != "aac" {
		t.Fatalf("a failed copy blocked authorized encoding with verifiable output facts: %+v", decision.Plan)
	}
	videoProfilesTestDeclined(t, source, request, ConversionLimits{AllowRemux: true})
}

func TestPlanVideoConversionPreservesUnknownAndRequiredConditions(t *testing.T) {
	for _, condition := range []ProfileCondition{
		conversionRequired("FutureVideoCapability", ProfileConditionTypeEquals, "supported"),
		conversionRequired(ProfileConditionValueVideoProfile, ProfileConditionTypeEquals, "High"),
		conversionRequired(ProfileConditionValueVideoLevel, ProfileConditionTypeEquals, "41"),
		conversionRequired(ProfileConditionValueRefFrames, ProfileConditionTypeEquals, "4"),
	} {
		t.Run(string(condition.Property), func(t *testing.T) {
			request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
			request.AllowVideoStreamCopy = profileTestPtr(false)
			request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{condition}}}
			videoProfilesTestDeclined(t, videoProfilesTestSource(), request, conversionTestLimits())
		})
	}
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{{
		Property: "FutureVideoCapability", Condition: ProfileConditionTypeEquals, Value: "supported",
	}}}}
	decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, conversionTestLimits(), "http", 0)
	if !decision.Output.ClientMustValidate {
		t.Fatal("optional unknown conditions were claimed as verified")
	}
	request.DeviceProfile.CodecProfiles[0].ApplyConditions = []ProfileCondition{conversionRequired("FutureApplicability", ProfileConditionTypeEquals, "supported")}
	videoProfilesTestDeclined(t, videoProfilesTestSource(), request, conversionTestLimits())
}

func TestPlanVideoConversionRejectsContradictoryNumericRequirements(t *testing.T) {
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.DeviceProfile.ContainerProfiles = []ContainerProfile{{Type: DlnaProfileTypeVideo, Container: "mp4", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeLessThanEqual, "640"),
	}}}
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueWidth, ProfileConditionTypeGreaterThanEqual, "1280"),
	}}}
	videoProfilesTestDeclined(t, videoProfilesTestSource(), request, conversionTestLimits())
}

func TestPlanVideoConversionResetsOutputTrackAndSeekCoordinates(t *testing.T) {
	source := videoProfilesTestSource()
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.AudioStreamIndex, request.StartTimeTicks = profileTestPtr(9), profileTestPtr(60*media.TicksPerSecond)
	request.DeviceProfile.ContainerProfiles = []ContainerProfile{{Type: DlnaProfileTypeVideo, Container: "mp4", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueNumAudioStreams, ProfileConditionTypeEquals, "1"),
		conversionRequired(ProfileConditionValueNumVideoStreams, ProfileConditionTypeEquals, "1"),
	}}}
	decision := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	if decision.Plan.VideoStreamIndex != 2 || decision.Plan.AudioStreamIndex != 9 || decision.Plan.StartTicks != 60*media.TicksPerSecond ||
		decision.Plan.DurationTicks != source.Info.DurationTicks || decision.Plan.VideoCodec != "h264" {
		t.Fatalf("source selection and seek coordinates were lost: %+v", decision.Plan)
	}
	if len(decision.OutputSource.Info.Streams) != 2 || decision.OutputSource.Info.Streams[0].Index != 0 || decision.OutputSource.Info.Streams[1].Index != 1 ||
		decision.OutputSource.Info.DurationTicks != 30*media.TicksPerSecond || decision.Output.DefaultAudioStreamIndex == nil ||
		*decision.Output.DefaultAudioStreamIndex != 1 || decision.Output.RequiresAudioTrackSelection {
		t.Fatalf("output evaluation reused input stream indices or applied seek twice: %+v", decision)
	}
}

func TestPlanVideoConversionKeepsTheSelectedDefaultVideoInput(t *testing.T) {
	source := videoProfilesTestSource()
	alternate := source.Info.Streams[0]
	alternate.Index, alternate.IsDefault = 15, true
	source.Info.Streams = append(source.Info.Streams, alternate)
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.DeviceProfile.ContainerProfiles = []ContainerProfile{{Type: DlnaProfileTypeVideo, Container: "mp4", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueNumVideoStreams, ProfileConditionTypeEquals, "1"),
	}}}
	decision := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	if decision.Plan.VideoStreamIndex != 15 || decision.OutputSource.Info.Streams[0].Index != 0 {
		t.Fatalf("default video selection used output ordinals as original input indexes: %+v", decision.Plan)
	}
}

func TestPlanVideoConversionSupportsSilentVideoWithoutDroppingExistingAudio(t *testing.T) {
	source := videoProfilesTestSource()
	source.Info.Streams = source.Info.Streams[:1]
	for _, codec := range []string{"", "none", "aac"} {
		t.Run(codec, func(t *testing.T) {
			profile := videoProfilesTestProfile("http", "mp4")
			profile.AudioCodec = codec
			request := videoProfilesTestRequest(profile)
			decision := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
			if decision.Plan.AudioStreamIndex != -1 || decision.Plan.AudioCodec != "" || len(decision.OutputSource.Info.Streams) != 1 {
				t.Fatalf("silent video acquired an invented audio stream: %+v", decision)
			}
			if codec != "aac" {
				videoProfilesTestDeclined(t, videoProfilesTestSource(), request, conversionTestLimits())
			}
		})
	}
}

func TestPlanVideoConversionDoesNotPromiseUnsupportedProgressiveSubtitles(t *testing.T) {
	source := videoProfilesTestSource()
	source.Info.Streams[3].IsExternal = true
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.SubtitleStreamIndex = profileTestPtr(12)
	request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{{Format: "srt", Method: SubtitleDeliveryMethodExternal}}
	videoProfilesTestDeclined(t, source, request, conversionTestLimits())
}

func TestPlanVideoConversionEnforcesRequestServerAndProfileCaps(t *testing.T) {
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.AudioStreamIndex, request.MaxAudioChannels = profileTestPtr(9), profileTestPtr(4)
	request.MaxStreamingBitrate, request.DeviceProfile.MaxStreamingBitrate = profileTestPtr(int64(2_500_000)), profileTestPtr(int64(2_000_000))
	request.DeviceProfile.TranscodingProfiles[0].MaxWidth = profileTestPtr(1280)
	request.DeviceProfile.TranscodingProfiles[0].MaxAudioChannels = "2"
	limits := conversionTestLimits()
	limits.MaxBitrate, limits.MaxWidth, limits.MaxAudioChannels = 1_500_000, 960, 1
	decision := videoProfilesTestPlan(t, videoProfilesTestSource(), request, limits, "http", 0)
	if decision.OutputSource.Info.Bitrate > 1_500_000 || decision.Plan.Width > 960 || decision.Plan.AudioChannels != 1 {
		t.Fatalf("client capabilities replaced stricter authorization limits: %+v", decision)
	}
}

func TestPlanVideoConversionBoundsWorkAcrossHLSProfiles(t *testing.T) {
	requestWithProfiles := func(count int) Request {
		var profiles []TranscodingProfile
		for index := 0; index < count; index++ {
			profiles = append(profiles, videoProfilesTestProfile("hls", "ts"))
		}
		profiles = append(profiles, videoProfilesTestProfile("http", "mp4"))
		request := videoProfilesTestRequest(profiles...)
		request.DeviceProfile.ContainerProfiles = []ContainerProfile{{Type: DlnaProfileTypeVideo, Container: "ts", Conditions: []ProfileCondition{
			conversionRequired("UnavailableHLSTransportFact", ProfileConditionTypeEquals, "supported"),
		}}}
		return request
	}
	for _, name := range []string{"first request", "independent later request"} {
		t.Run(name, func(t *testing.T) {
			decision := videoProfilesTestDeclined(t, videoProfilesTestSource(), requestWithProfiles(228), conversionTestLimits())
			found := false
			for _, reason := range decision.Reasons {
				found = found || reason.Code == "video_profile_search_limit"
			}
			if !found {
				t.Fatalf("candidate work exceeded the per-request bound without an explicit refusal: %+v", decision.Reasons)
			}
			videoProfilesTestPlan(t, videoProfilesTestSource(), requestWithProfiles(2), conversionTestLimits(), "http", 2)
		})
	}
}

func TestPlanVideoConversionPreservesCallerOwnedState(t *testing.T) {
	source, request := videoProfilesTestSource(), videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	source.Info.FormatStartTicks = -3 * media.TicksPerSecond
	request.AllowVideoStreamCopy = profileTestPtr(false)
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{
		conversionRequired(ProfileConditionValueVideoFramerate, ProfileConditionTypeEquals, "23.976"),
	}}}
	before, err := json.Marshal(struct {
		Source  Source
		Request Request
	}{source, request})
	if err != nil {
		t.Fatal(err)
	}
	decision := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	after, err := json.Marshal(struct {
		Source  Source
		Request Request
	}{source, request})
	if err != nil || string(before) != string(after) {
		t.Fatalf("profile negotiation changed caller-owned source or request state: %v", err)
	}
	if !decision.Plan.SourceFormatStartKnown || decision.Plan.SourceFormatStartTicks != -3*media.TicksPerSecond {
		t.Fatal("a known negative container clock was discarded")
	}
	request.DeviceProfile.CodecProfiles[0].Conditions[0].Value = "30"
	source.Info.Streams[0].Width = 320
	if decision.Plan.FrameRate != 23.976 || decision.OutputSource.Info.Streams[0].Width != 1920 {
		t.Fatal("a completed plan retained mutable caller-owned facts")
	}
}

func TestPlanVideoConversionRejectsStructuralErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Source, *Request, *ConversionLimits)
		want   error
	}{
		{"empty identity", func(source *Source, _ *Request, _ *ConversionLimits) { source.ItemID = "" }, ErrInvalidSource},
		{"duplicate stream index", func(source *Source, _ *Request, _ *ConversionLimits) { source.Info.Streams[1].Index = 2 }, ErrInvalidSource},
		{"wrong source", func(_ *Source, request *Request, _ *ConversionLimits) { request.MediaSourceID = "another-source" }, ErrInvalidRequest},
		{"missing audio", func(_ *Source, request *Request, _ *ConversionLimits) { request.AudioStreamIndex = profileTestPtr(999) }, ErrInvalidRequest},
		{"negative seek", func(_ *Source, request *Request, _ *ConversionLimits) {
			request.StartTimeTicks = profileTestPtr(int64(-1))
		}, ErrInvalidRequest},
		{"invalid server limits", func(_ *Source, _ *Request, limits *ConversionLimits) { limits.MaxWidth = -1 }, ErrInvalidRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, request, limits := videoProfilesTestSource(), videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4")), conversionTestLimits()
			test.mutate(&source, &request, &limits)
			decision, err := PlanVideoConversion(source, request, limits)
			if !errors.Is(err, test.want) || decision.Plan != nil || decision.SelectedProfileIndex != nil {
				t.Fatalf("structural failure became a valid conversion candidate: %+v, %v", decision, err)
			}
		})
	}
}

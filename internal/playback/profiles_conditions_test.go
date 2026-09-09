package playback

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestEvaluateExtendedProbeFacts(t *testing.T) {
	for _, test := range []struct {
		name     string
		property ProfileConditionValue
		value    string
		want     bool
	}{
		{"video bit depth", ProfileConditionValueVideoBitDepth, "10", true},
		{"video bit depth mismatch", ProfileConditionValueVideoBitDepth, "8", false},
		{"audio bit depth", ProfileConditionValueAudioBitDepth, "24", true},
		{"audio sample rate", ProfileConditionValueAudioSampleRate, "48000", true},
		{"audio bitrate", ProfileConditionValueAudioBitrate, "192000", true},
		{"audio profile", ProfileConditionValueAudioProfile, "LC", true},
		{"reference frames", ProfileConditionValueRefFrames, "4", true},
		{"progressive video", ProfileConditionValueIsInterlaced, "false", true},
		{"interlaced mismatch", ProfileConditionValueIsInterlaced, "true", false},
		{"AVC fact", ProfileConditionValueIsAvc, "true", true},
		{"raw codec tag", ProfileConditionValueVideoCodecTag, "0x31637661", true},
		{"known video range", ProfileConditionValueVideoRange, "SDR", true},
		{"video range mismatch", ProfileConditionValueVideoRange, "HDR", false},
		{"embedded audio", ProfileConditionValueIsExternalAudio, "false", true},
		{"audio stream count", ProfileConditionValueNumAudioStreams, "2", true},
		{"video stream count", ProfileConditionValueNumVideoStreams, "1", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, request := profileTestSource(), profileTestRequest()
			video, audio := &source.Info.Streams[0], &source.Info.Streams[1]
			video.BitDepth, video.RefFrames = 10, 4
			video.InterlaceKnown, video.IsAVCKnown, video.IsAVC = true, true, true
			video.CodecTag = "0x31637661"
			video.VideoRange, video.VideoRangeKnown = "SDR", true
			audio.BitDepth, audio.Profile = 24, "LC"
			request.DeviceProfile.ContainerProfiles = []ContainerProfile{{
				Type: DlnaProfileTypeVideo, Container: "mp4",
				Conditions: []ProfileCondition{{Property: test.property, Condition: ProfileConditionTypeEquals, Value: test.value, IsRequired: profileTestPtr(true)}},
			}}
			profileTestDecision(t, source, request, test.want)
		})
	}
}

func TestEvaluateUnknownProbeFactsAreNotInvented(t *testing.T) {
	for _, test := range []struct {
		name     string
		property ProfileConditionValue
		value    string
		change   func(*media.Stream)
	}{
		{"pixel format does not establish bit depth", ProfileConditionValueVideoBitDepth, "10", func(s *media.Stream) { s.PixelFormat = "yuv420p10le" }},
		{"false interlace needs an explicit known fact", ProfileConditionValueIsInterlaced, "false", func(s *media.Stream) { s.IsInterlaced = false }},
		{"AVC codec name is not the container flag", ProfileConditionValueIsAvc, "true", func(s *media.Stream) { s.Codec = "h264" }},
		{"ten bits do not establish HDR", ProfileConditionValueVideoRange, "HDR", func(s *media.Stream) { s.BitDepth = 10 }},
		{"unknown range string is not authoritative", ProfileConditionValueVideoRange, "SDR", func(s *media.Stream) { s.VideoRange = "SDR" }},
		{"color transfer has no unverified range mapping", ProfileConditionValueVideoRange, "SDR", func(s *media.Stream) { s.ColorTransfer = "bt709" }},
		{"zero reference frames are unknown", ProfileConditionValueRefFrames, "0", func(s *media.Stream) { s.RefFrames = 0 }},
		{"missing frame rates are unknown", ProfileConditionValueVideoFramerate, "0", func(s *media.Stream) { s.AverageFrameRate = "0/0" }},
		{"probe profile placeholder remains unknown", ProfileConditionValueVideoProfile, "unknown", func(s *media.Stream) { s.Profile = "unknown" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, request := profileTestSource(), profileTestRequest()
			test.change(&source.Info.Streams[0])
			condition := ProfileCondition{Property: test.property, Condition: ProfileConditionTypeEquals, Value: test.value, IsRequired: profileTestPtr(true)}
			request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{condition}}}
			decision := profileTestDecision(t, source, request, false)
			if len(decision.Reasons) != 1 || decision.Reasons[0].Code != "unknown_required_condition" {
				t.Fatalf("missing fact was not identified as an unknown requirement: %+v", decision)
			}
			request.DeviceProfile.CodecProfiles[0].Conditions[0].IsRequired = nil
			decision = profileTestDecision(t, source, request, true)
			if len(decision.Reasons) != 1 || !decision.Reasons[0].Unverified || !decision.ClientMustValidate {
				t.Fatalf("optional missing fact did not preserve its uncertainty: %+v", decision)
			}
		})
	}
}

func TestEvaluateFrameRateFallbackRemainsExact(t *testing.T) {
	source, request := profileTestSource(), profileTestRequest()
	source.Info.Streams[0].AverageFrameRate = "0/0"
	source.Info.Streams[0].RealFrameRate = "24000/1001"
	request.DeviceProfile.CodecProfiles = []CodecProfile{{
		Type: CodecTypeVideo, Codec: "h264",
		Conditions: []ProfileCondition{{Property: ProfileConditionValueVideoFramerate, Condition: ProfileConditionTypeEquals, Value: "24000/1001", IsRequired: profileTestPtr(true)}},
	}}
	profileTestDecision(t, source, request, true)
	request.DeviceProfile.CodecProfiles[0].Conditions[0].Value = "23.976"
	profileTestDecision(t, source, request, false)
}

func TestEvaluateRejectsMalformedConditionsWithoutNumericExpansion(t *testing.T) {
	for _, value := range []string{"NaN", "Inf", "-Inf", "0/0", "0x1p10", "1e1000000000", "1e-1000000000", strings.Repeat("9", 129)} {
		t.Run(value, func(t *testing.T) {
			request := profileTestRequest()
			request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{{
				Property: ProfileConditionValueWidth, Condition: ProfileConditionTypeLessThanEqual, Value: value, IsRequired: profileTestPtr(false),
			}}}}
			decision := profileTestDecision(t, profileTestSource(), request, false)
			if len(decision.Reasons) != 1 || decision.Reasons[0].Code != "invalid_profile_condition" {
				t.Fatalf("invalid numeric condition did not produce a precise reason: %+v", decision)
			}
		})
	}
	for _, condition := range []ProfileCondition{
		{Property: ProfileConditionValueWidth, Condition: ProfileConditionType("UnknownComparison"), Value: "1920"},
		{Property: ProfileConditionValueVideoProfile, Condition: ProfileConditionTypeLessThanEqual, Value: "High"},
		{Property: ProfileConditionValueIsInterlaced, Condition: ProfileConditionTypeEquals, Value: "not-a-boolean"},
	} {
		request := profileTestRequest()
		request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: []ProfileCondition{condition}}}
		profileTestDecision(t, profileTestSource(), request, false)
	}
}

func TestEvaluateApplicabilityIsIndependentOfConditionOrder(t *testing.T) {
	falseCondition := ProfileCondition{Property: ProfileConditionValueWidth, Condition: ProfileConditionTypeEquals, Value: "1280"}
	unknownCondition := ProfileCondition{Property: ProfileConditionValueIsInterlaced, Condition: ProfileConditionTypeEquals, Value: "true", IsRequired: profileTestPtr(true)}
	for _, conditions := range [][]ProfileCondition{{falseCondition, unknownCondition}, {unknownCondition, falseCondition}} {
		request := profileTestRequest()
		request.DeviceProfile.CodecProfiles = []CodecProfile{{
			Type: CodecTypeVideo, Codec: "h264", ApplyConditions: conditions,
			Conditions: []ProfileCondition{{Property: ProfileConditionValueWidth, Condition: ProfileConditionTypeLessThanEqual, Value: "1"}},
		}}
		profileTestDecision(t, profileTestSource(), request, true)
	}
}

func TestEvaluateAudioFilesIgnoreAttachedArtwork(t *testing.T) {
	source := Source{ItemID: "song", Path: "/music/song.m4a", ItemType: "Audio", Info: media.Info{
		Container: "mov,mp4,m4a,3gp,3g2,mj2", Bitrate: 256_000,
		Streams: []media.Stream{
			{Index: 0, CodecType: "video", Codec: "mjpeg", IsDefault: true, IsAttachedPicture: true},
			{Index: 3, CodecType: "audio", Codec: "aac", IsDefault: true, Channels: 2},
		},
	}}
	request := Request{DeviceProfile: &DeviceProfile{
		DirectPlayProfiles: []DirectPlayProfile{{Type: DlnaProfileTypeAudio, Container: "m4a", AudioCodec: "aac"}},
		ContainerProfiles: []ContainerProfile{{Type: DlnaProfileTypeAudio, Container: "m4a", Conditions: []ProfileCondition{
			{Property: ProfileConditionValueNumVideoStreams, Condition: ProfileConditionTypeEquals, Value: "0", IsRequired: profileTestPtr(true)},
		}}},
	}}
	decision := profileTestDecision(t, source, request, true)
	if decision.DefaultAudioStreamIndex == nil || *decision.DefaultAudioStreamIndex != 3 || media.SourceMIMEType(source.Info, source.Path) != "audio/mp4" {
		t.Fatalf("attached artwork was treated as playable video: %+v", decision)
	}
	request.DeviceProfile.MaxStaticMusicBitrate = profileTestPtr(255_999)
	profileTestDecision(t, source, request, false)
	request.DeviceProfile.MaxStaticMusicBitrate = profileTestPtr(256_000)
	profileTestDecision(t, source, request, true)
}

func TestEvaluateDoesNotMutateInputs(t *testing.T) {
	source, request := profileTestSource(), profileTestRequest()
	streamsBefore := append([]media.Stream(nil), source.Info.Streams...)
	profileBefore := *request.DeviceProfile
	request.AudioStreamIndex = profileTestPtr(9)
	profileTestDecision(t, source, request, false)
	if !reflect.DeepEqual(source.Info.Streams, streamsBefore) || !reflect.DeepEqual(*request.DeviceProfile, profileBefore) || *request.AudioStreamIndex != 9 {
		t.Fatal("pure playback evaluation changed its source or request")
	}
}

func TestEvaluateRejectsUnboundedProfilesAndAmbiguousSources(t *testing.T) {
	request := profileTestRequest()
	request.DeviceProfile.DirectPlayProfiles = make([]DirectPlayProfile, 257)
	if _, err := Evaluate(profileTestSource(), request); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unbounded profiles returned %v, want ErrInvalidRequest", err)
	}
	request = profileTestRequest()
	request.DeviceProfile.CodecProfiles = []CodecProfile{{Type: CodecTypeVideo, Codec: "h264", Conditions: make([]ProfileCondition, 1025)}}
	if _, err := Evaluate(profileTestSource(), request); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unbounded conditions returned %v, want ErrInvalidRequest", err)
	}
	source := profileTestSource()
	source.Info.Streams[1].Index = source.Info.Streams[0].Index
	if _, err := Evaluate(source, Request{}); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("duplicate stream indices returned %v, want ErrInvalidSource", err)
	}
	source = profileTestSource()
	source.MediaSourceID = "another-source"
	if _, err := Evaluate(source, Request{}); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("inconsistent original source identity returned %v, want ErrInvalidSource", err)
	}
}

package server

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func TestVideoEncodingQueriesRejectMalformedPrecisionAndProfiles(t *testing.T) {
	for _, values := range []map[string]string{
		{"videobitdepth": ""}, {"videobitdepth": "9"}, {"videobitdepth": "010"}, {"videobitdepth": "+8"},
		{"videobitdepth": " 8"}, {"videobitdepth": "8.0"}, {"videobitdepth": "2147483648"},
		{"videoprofile": ""}, {"videoprofile": "main;cmd"}, {"videoprofile": strings.Repeat("a", 33)},
	} {
		if _, _, err := videoQueryEncoding(values); !errors.Is(err, errHLSRequestInvalid) {
			t.Fatalf("malformed encoder query was accepted: %v: %v", values, err)
		}
	}
	if _, _, err := videoQueryEncoding(map[string]string{"videoprofile": "professional"}); !errors.Is(err, errHLSRequestUnsupported) {
		t.Fatal("an unsupported profile was not distinguished from malformed syntax")
	}
	depth, profile, err := videoQueryEncoding(map[string]string{"videobitdepth": "10", "videoprofile": "Main 10"})
	if err != nil || depth == nil || *depth != 10 || profile != "main10" || videoProfileCondition(profile) != "Main 10" {
		t.Fatal("HEVC precision and profile aliases were not normalized")
	}
	for _, values := range []map[string]string{
		{"videorange": ""}, {"videorange": "SDR", "videorangetype": "HDR10"},
	} {
		if _, err := videoQueryRange(values); !errors.Is(err, errHLSRequestInvalid) {
			t.Fatal("an invalid or conflicting range was accepted")
		}
	}
	if value, err := videoQueryRange(map[string]string{"videorange": "hdr10", "videorangetype": "HDR10"}); err != nil || value != "HDR10" {
		t.Fatal("matching HDR10 range aliases were rejected")
	}
}

func TestProgressiveModernEncodingQueriesPreserveOutputAcrossURLRoundTrip(t *testing.T) {
	for _, test := range []struct {
		codec, profile string
		depth          int
	}{
		{"hevc", "main", 8}, {"hevc", "main10", 10}, {"av1", "main", 8}, {"av1", "main", 10},
	} {
		t.Run(test.codec+"/"+strconv.Itoa(test.depth), func(t *testing.T) {
			source := videoRequestTestSource()
			values := map[string]string{"VideoCodec": test.codec, "VideoProfile": test.profile, "VideoBitDepth": strconv.Itoa(test.depth), "AllowVideoStreamCopy": "false"}
			result := videoRequestTestPlan(t, source, values, "mp4", videoRequestTestLimits())
			plan := *result.Conversion.Plan
			if plan.VideoCodec != test.codec || plan.VideoBitDepth != test.depth || plan.VideoProfile != test.profile {
				t.Fatalf("query was weakened into a different encoding: %+v", plan)
			}
			parsed, err := url.Parse(videoPlaybackURL(source.ItemID, source.MediaSourceID, "play", "device", "token", plan))
			if err != nil {
				t.Fatal(err)
			}
			query := parsed.Query()
			if query.Get("VideoCodec") != test.codec || query.Get("VideoProfile") != test.profile || query.Get("VideoBitDepth") != strconv.Itoa(test.depth) {
				t.Fatal("progressive URL omitted concrete encoding settings")
			}
			values = map[string]string{}
			for key := range query {
				values[key] = query.Get(key)
			}
			rebuilt := videoRequestTestPlan(t, source, values, "mp4", videoRequestTestLimits())
			if *rebuilt.Conversion.Plan != plan {
				t.Fatal("progressive URL round trip changed its encoding contract")
			}
		})
	}
}

func TestHLSModernEncodingQueriesConstrainActualOutput(t *testing.T) {
	for _, test := range []struct {
		codec, container, profile string
		depth                     int
	}{
		{"hevc", "ts", "main", 8}, {"hevc", "mp4", "main10", 10},
		{"av1", "mp4", "main", 8}, {"av1", "mp4", "main", 10},
	} {
		t.Run(test.codec+"/"+test.container+"/"+strconv.Itoa(test.depth), func(t *testing.T) {
			decision := hlsRequestTestPlan(t, map[string]string{
				"VideoCodec": test.codec, "VideoProfile": test.profile, "VideoBitDepth": strconv.Itoa(test.depth),
				"SegmentContainer": test.container, "AllowVideoStreamCopy": "false",
			}, hlsRequestTestSource(), hlsRequestTestLimits())
			if decision.Plan.VideoCodec != test.codec || decision.Plan.VideoBitDepth != test.depth || decision.Plan.VideoProfile != test.profile {
				t.Fatalf("HLS query accepted precision without producing it: %+v", decision.Plan)
			}
		})
	}
	for _, values := range []map[string]string{
		{"VideoCodec": "av1", "SegmentContainer": "ts"},
		{"VideoCodec": "h264", "VideoBitDepth": "10", "AllowVideoStreamCopy": "false"},
		{"VideoCodec": "hevc", "VideoBitDepth": "8", "VideoProfile": "main10", "AllowVideoStreamCopy": "false"},
		{"VideoCodec": "av1", "VideoProfile": "high", "SegmentContainer": "mp4", "AllowVideoStreamCopy": "false"},
	} {
		if decision, err := hlsRequestConversion(values, hlsRequestTestSource(), hlsRequestTestLimits()); !errors.Is(err, errHLSRequestUnsupported) || decision.Plan != nil {
			t.Fatalf("an incompatible codec, container or precision was advertised: %v: %v", values, err)
		}
	}
}

func TestProgressiveHDR10URLRetainsRangeAndPrecision(t *testing.T) {
	source := videoRequestTestSource()
	video := &source.Info.Streams[1]
	video.Codec, video.Profile, video.BitDepth, video.PixelFormat = "hevc", "Main 10", 10, "yuv420p10le"
	video.VideoRange, video.VideoRangeKnown = "HDR10", true
	video.ColorTransfer, video.ColorPrimaries, video.ColorSpace, video.ColorRange = "smpte2084", "bt2020", "bt2020nc", "tv"
	limits := videoRequestTestLimits()
	limits.Hardware = transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD128"}
	values := map[string]string{"VideoCodec": "hevc", "VideoProfile": "main10", "VideoBitDepth": "10", "VideoRangeType": "HDR10", "AllowVideoStreamCopy": "false"}
	result := videoRequestTestPlan(t, source, values, "mp4", limits)
	plan := *result.Conversion.Plan
	if plan.VideoFilters.OutputRange != "hdr10" || plan.VideoBitDepth != 10 {
		t.Fatal("HDR10 request was converted to an SDR representation")
	}
	parsed, err := url.Parse(videoPlaybackURL(source.ItemID, source.MediaSourceID, "play", "device", "token", plan))
	if err != nil || parsed.Query().Get("VideoRange") != "HDR10" || parsed.Query().Get("VideoBitDepth") != "10" {
		t.Fatal("HDR10 output URL omitted its range or precision")
	}
	values = map[string]string{}
	for key := range parsed.Query() {
		values[key] = parsed.Query().Get(key)
	}
	rebuilt := videoRequestTestPlan(t, source, values, "mp4", limits)
	if *rebuilt.Conversion.Plan != plan {
		t.Fatal("HDR10 output URL reconstructed a different color conversion")
	}
}

func TestHLSRevisionURLsDescribeImmutableEncodingWithoutInventedLevels(t *testing.T) {
	plan := transcode.Plan{VideoCodec: "hevc", VideoBitDepth: 10, VideoProfile: "main10", Container: "mp4", Width: 1920, Height: 1080}
	session := &hlsSession{id: "revision", key: hlsKey{plan: plan, scope: transcode.Scope{ItemID: "item", SourceID: "source", DeviceID: "device", PlaySessionID: "play"}},
		output: playback.Source{Info: media.Info{Bitrate: 5_000_000}}}
	parsed, err := url.Parse(hlsSessionURL(session, "Videos", "master.m3u8", "token", 0))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for key := range parsed.Query() {
		values[strings.ToLower(key)] = parsed.Query().Get(key)
	}
	if values["videocodec"] != "hevc" || values["videobitdepth"] != "10" || values["videoprofile"] != "main10" || !hlsVideoEncodingMatches(session, values) {
		t.Fatal("HLS URL did not preserve its encoding revision")
	}
	for _, key := range []string{"videocodec", "videobitdepth", "videoprofile", "videorange", "videorangetype"} {
		changed := map[string]string{}
		for name, value := range values {
			changed[name] = value
		}
		changed[key] = "different"
		if hlsVideoEncodingMatches(session, changed) {
			t.Fatalf("HLS revision allowed a changed %s", key)
		}
	}
	master, err := hlsGeneratedMaster(session, "Videos", "token", 0)
	if err != nil || strings.Contains(string(master), "CODECS=") || strings.Contains(string(master), "VideoLevel=") {
		t.Fatalf("HLS manifest invented an unmeasured codec level: %v", err)
	}
	session.key.plan.VideoCodec = "copy"
	session.output.Info.Streams = []media.Stream{{CodecType: "video", Codec: "hevc", Profile: "Main 10", BitDepth: 10}}
	query := hlsVideoEncodingQuery(session)
	if query.Get("VideoCodec") != "hevc" || query.Get("VideoProfile") != "main10" || query.Get("VideoBitDepth") != "10" {
		t.Fatal("copied output was described as an encoder operation instead of source facts")
	}
}

func TestVideoSeekAlignmentReportsActualStartWithoutChangingDefaultConsent(t *testing.T) {
	request := playback.Request{}
	if err := mergePlaybackQuery(&request, map[string]string{"allowvideoseekalignment": "true"}); err != nil || request.AllowVideoSeekAlignment == nil || !*request.AllowVideoSeekAlignment {
		t.Fatal("explicit PlaybackInfo seek alignment was not retained")
	}
	if err := mergePlaybackQuery(&request, map[string]string{"allowvideoseekalignment": "false"}); err == nil {
		t.Fatal("conflicting body and query alignment preferences were accepted")
	}
	if err := mergePlaybackQuery(&playback.Request{}, map[string]string{"allowvideoseekalignment": "perhaps"}); err == nil {
		t.Fatal("malformed seek alignment was accepted")
	}
	if result, err := videoRequestDecision(videoRequestTestSource(), map[string]string{"AllowVideoSeekAlignment": "perhaps"}, "mp4", videoRequestTestLimits()); !errors.Is(err, errVideoRequestInvalid) || result.Conversion.Plan != nil {
		t.Fatal("malformed progressive seek alignment was accepted")
	}
	header := http.Header{}
	header.Set("Access-Control-Expose-Headers", "Content-Type")
	setVideoStartHeaders(header, 20*media.TicksPerSecond, 23*media.TicksPerSecond)
	if header.Get("X-Goby-Start-Time-Ticks") != "200000000" || header.Get("X-Goby-Seek-Aligned") != "true" {
		t.Fatal("aligned response reported the requested start instead of the selected keyframe")
	}
	setVideoStartHeaders(header, 23*media.TicksPerSecond, 23*media.TicksPerSecond)
	if header.Get("X-Goby-Start-Time-Ticks") != "230000000" || header.Get("X-Goby-Seek-Aligned") != "" {
		t.Fatal("an exact response inherited the alignment marker")
	}
	if header.Get("Access-Control-Expose-Headers") != "Content-Type, X-Goby-Start-Time-Ticks, X-Goby-Seek-Aligned" {
		t.Fatal("actual seek headers were not exposed to the browser or replaced unrelated exposed headers")
	}
	plan := transcode.Plan{VideoCodec: "copy", VideoCopyCodec: "hevc", VideoCopySeekCandidate: "private-candidate", StartTicks: 20 * media.TicksPerSecond}
	parsed, err := url.Parse(videoPlaybackURL("item", "source", "play", "device", "token", plan))
	if err != nil || parsed.Query().Get("StartTimeTicks") != "200000000" || parsed.Query().Has("AllowVideoSeekAlignment") || strings.Contains(parsed.String(), "private-candidate") {
		t.Fatal("copy URL changed its actual start, inferred alignment consent, or leaked seek evidence")
	}
}

func TestProgressiveTimestampModeSurvivesURLReconstruction(t *testing.T) {
	for _, test := range []struct {
		name   string
		values map[string]string
		copy   bool
	}{
		{"copied absolute timeline", map[string]string{"CopyTimestamps": "true"}, true},
		{"encoded absolute timeline", map[string]string{"CopyTimestamps": "true", "VideoCodec": "hevc", "StartTimeTicks": "12345678"}, true},
		{"absolute timeline without alignment", map[string]string{"CopyTimestamps": "true", "AllowVideoSeekAlignment": "false", "StartTimeTicks": "12345678"}, true},
		{"legacy rebased timeline", map[string]string{"StartTimeTicks": "12345678"}, false},
		{"explicit rebased timeline", map[string]string{"CopyTimestamps": "false", "StartTimeTicks": "12345678"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := videoRequestTestSource()
			result := videoRequestTestPlan(t, source, test.values, "mp4", videoRequestTestLimits())
			plan := *result.Conversion.Plan
			if plan.CopyTimestamps != test.copy {
				t.Fatal("the requested timestamp mode did not reach the media plan")
			}
			parsed, err := url.Parse(videoPlaybackURL(source.ItemID, source.MediaSourceID, "play", "device", "token", plan))
			if err != nil || (parsed.Query().Get("CopyTimestamps") == "true") != test.copy {
				t.Fatal("the URL did not describe the plan's actual timestamp mode")
			}
			values := map[string]string{}
			for key := range parsed.Query() {
				values[key] = parsed.Query().Get(key)
			}
			rebuilt := videoRequestTestPlan(t, source, values, "mp4", videoRequestTestLimits())
			if *rebuilt.Conversion.Plan != plan {
				t.Fatal("URL reconstruction changed the output timestamp mode or start")
			}
		})
	}
	if result, err := videoRequestDecision(videoRequestTestSource(), map[string]string{"CopyTimestamps": "perhaps"}, "mp4", videoRequestTestLimits()); !errors.Is(err, errVideoRequestInvalid) || result.Conversion.Plan != nil {
		t.Fatal("malformed timestamp mode was accepted")
	}
}

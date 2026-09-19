package server

import (
	"net/url"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func TestGeneratedHLSMasterUsesDistinctScopedVariantURLs(t *testing.T) {
	plan := transcode.Plan{Container: "mp4", HLS: transcode.HLSPlan{SegmentType: "fmp4", RenditionCount: 2}, VideoBitrate: 2000000, AudioBitrate: 128000, FrameRate: 24}
	plan.HLS.Renditions[0] = transcode.HLSRendition{Width: 1280, Height: 720, VideoBitrate: 2000000}
	plan.HLS.Renditions[1] = transcode.HLSRendition{Width: 640, Height: 360, VideoBitrate: 500000}
	plan.Subtitle = transcode.SubtitlePlan{Mode: "hls", StreamIndex: 3, Codec: "subrip"}
	session := &hlsSession{id: "revision", key: hlsKey{plan: plan, scope: transcode.Scope{ItemID: "movie", SourceID: "source", PlaySessionID: "play", DeviceID: "device"}}, output: playback.Source{Info: media.Info{Bitrate: 2400000}}}
	data, err := hlsGeneratedMaster(session, "Videos", "A/B+C", 1234)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "#EXT-X-STREAM-INF:") != 2 || !strings.Contains(string(data), "#EXT-X-MEDIA:TYPE=SUBTITLES") || !strings.Contains(string(data), "/subtitles-0.m3u8?") {
		t.Fatalf("missing HLS graph: %s", data)
	}
	var children []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" && line[0] != '#' {
			children = append(children, line)
			parsed, err := url.Parse(line)
			if err != nil || parsed.Query().Get("api_key") != "A/B+C" || parsed.Query().Get("PlaySessionId") != "play" || parsed.Query().Get("GobyHlsId") != "revision" {
				t.Fatalf("lost scope: %s", line)
			}
		}
	}
	if len(children) != 2 || children[0] == children[1] || !strings.Contains(children[0], "v0.m3u8") || !strings.Contains(children[1], "v1.m3u8") {
		t.Fatalf("aliased variants: %v", children)
	}
	for _, name := range []string{"v0.m3u8", "v1-init.mp4", "v0-segment-000000.m4s"} {
		if !hlsPlanArtifact(plan, name) {
			t.Errorf("missing generated artifact %s", name)
		}
	}
	for _, name := range []string{"v2.m3u8", "main.m3u8", "init.mp4", "v0-segment-000000.ts", "../v0.m3u8", "subtitle.ass"} {
		if hlsPlanArtifact(plan, name) {
			t.Errorf("accepted unrelated artifact %s", name)
		}
	}
}

func TestManualHLSRequestsConstructAdaptiveFragmentedAndSubtitleOutputs(t *testing.T) {
	decision := hlsRequestTestPlan(t, map[string]string{"SegmentContainer": "fmp4", "EnableAdaptiveBitrate": "true"}, hlsRequestTestSource(), hlsRequestTestLimits())
	if decision.Plan.Container != "mp4" || decision.Plan.HLS.RenditionCount < 2 {
		t.Fatalf("manual ladder = %+v", decision.Plan)
	}
	source := hlsRequestTestSource()
	source.Info.Streams = append(source.Info.Streams, media.Stream{Index: 12, CodecType: "subtitle", Codec: "subrip", IsTextSubtitleStream: true})
	decision = hlsRequestTestPlan(t, map[string]string{"SubtitleStreamIndex": "12", "ManifestSubtitles": "vtt", "MaxManifestSubtitles": "10"}, source, hlsRequestTestLimits())
	if !transcode.HasHLSSubtitles(*decision.Plan) || decision.SubtitleView.SelectedStreamIndex != 12 {
		t.Fatalf("manual subtitle = %+v", decision.Plan)
	}
}

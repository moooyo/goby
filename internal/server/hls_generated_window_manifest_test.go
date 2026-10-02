package server

import (
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestGeneratedWindowManifestPreservesSourceTimelineAndAdmissionIdentity(t *testing.T) {
	plan, info, endpoint := generatedWindowGraphPlanFixture()
	timeline, err := hlsGeneratedWindowTimeline(plan, info, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	session := &hlsSession{id: "fixed-output-revision", key: hlsKey{plan: plan, scope: transcode.Scope{ItemID: "movie", SourceID: "source", PlaySessionID: "play"}}}
	graph := &hlsGeneratedWindowGraph{endpoint: endpoint, timeline: timeline}
	body, err := hlsGeneratedWindowManifest(session, graph, "Videos", "controlled-token", 90*media.TicksPerSecond)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "#EXT-X-PLAYLIST-TYPE:VOD\n") || !strings.Contains(text, "#EXT-X-MEDIA-SEQUENCE:0\n") ||
		!strings.Contains(text, "#EXT-X-START:TIME-OFFSET=90.0000000,PRECISE=YES\n") ||
		strings.Count(text, "#EXT-X-DISCONTINUITY\n") != 17 || strings.Count(text, "#EXTINF:6.0000000,\n") != 16 ||
		strings.Count(text, "#EXTINF:4.0000000,\n") != 1 || strings.Count(text, "GobyHlsWindowGraphId=fixed-output-revision") != 17 ||
		!strings.Contains(text, "/window-segment-000015.ts?") || !strings.Contains(text, "/window-segment-000016.ts?") ||
		!strings.HasSuffix(text, "#EXT-X-ENDLIST\n") || strings.Contains(text, "#EXT-X-GAP") || strings.Contains(text, "GobyHlsProducerId") || strings.Contains(text, "#EXT-X-MAP") {
		t.Fatalf("logical presentation mixed rebased output, incomplete resources or unsupported tags: %s", text)
	}
}

func TestGeneratedWindowManifestRejectsAnUncertifiedEndpoint(t *testing.T) {
	plan, info, endpoint := generatedWindowGraphPlanFixture()
	timeline, err := hlsGeneratedWindowTimeline(plan, info, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	session := &hlsSession{id: "output", key: hlsKey{plan: plan}}
	graph := &hlsGeneratedWindowGraph{endpoint: endpoint, timeline: timeline}
	graph.endpoint.DurationTicks++
	if _, err := hlsGeneratedWindowManifest(session, graph, "Videos", "token", 0); err == nil {
		t.Fatal("a mismatching endpoint was promoted into public ENDLIST")
	}
}

package server

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

var generatedWindowFMP4ManifestSlots = regexp.MustCompile(`(?m)^#EXT-X-DISCONTINUITY\n#EXT-X-MAP:URI="([^"\n]+)"\n#EXTINF:([0-9]+\.[0-9]{7}),\n([^#\n]+)\n`)

func TestGeneratedWindowFMP4ManifestPreservesFullTimelineAndSlotMaps(t *testing.T) {
	for _, rate := range []int{24, 25} {
		for _, count := range []int{0, 2} {
			for variant := 0; variant < max(1, count); variant++ {
				t.Run(fmt.Sprintf("rate%d-renditions%d-variant%d", rate, count, variant), func(t *testing.T) {
					plan, info, endpoint := generatedWindowFMP4GraphPlanFixture(rate, count)
					timeline, err := hlsGeneratedWindowTimeline(plan, info, endpoint)
					if err != nil {
						t.Fatal(err)
					}
					session := &hlsSession{id: "fmp4-output-revision", key: hlsKey{plan: plan, scope: transcode.Scope{ItemID: "movie", SourceID: "source", PlaySessionID: "play"}}}
					graph := &hlsGeneratedWindowGraph{endpoint: endpoint, timeline: timeline}
					body, err := hlsGeneratedWindowManifest(session, graph, "Videos", "controlled-token", 90*media.TicksPerSecond, variant)
					if err != nil {
						t.Fatal(err)
					}
					text := string(body)
					prefix := "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-INDEPENDENT-SEGMENTS\n#EXT-X-TARGETDURATION:6\n#EXT-X-START:TIME-OFFSET=90.0000000,PRECISE=YES\n"
					if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, "#EXT-X-ENDLIST\n") ||
						strings.Contains(text, "#EXT-X-GAP") || strings.Contains(text, "GobyHlsProducerId") || strings.Contains(text, ".ts?") {
						t.Fatalf("fMP4 presentation lost its immutable full-source VOD contract: %s", text)
					}
					matches := generatedWindowFMP4ManifestSlots.FindAllStringSubmatch(text, -1)
					if len(matches) != 17 || strings.Count(text, "#EXT-X-DISCONTINUITY\n") != 17 || strings.Count(text, "#EXT-X-MAP:URI=") != 17 ||
						strings.Count(text, "#EXTINF:") != 17 || strings.Count(text, "GobyHlsWindowGraphId=fmp4-output-revision") != 34 {
						t.Fatalf("each source slot needs its own ordered map and media admission: %s", text)
					}
					seen := make(map[string]bool)
					for number, match := range matches {
						duration := "6.0000000"
						if number == 16 {
							duration = "4.0000000"
						}
						if match[2] != duration {
							t.Fatalf("slot %d advertised a metadata or nominal tail: %s", number, match[2])
						}
						namePrefix := ""
						if count > 0 {
							namePrefix = fmt.Sprintf("v%d-", variant)
						}
						for index, name := range []string{fmt.Sprintf("%swindow-init-%06d.mp4", namePrefix, number), fmt.Sprintf("%swindow-segment-%06d.m4s", namePrefix, number)} {
							raw := match[1]
							if index == 1 {
								raw = match[3]
							}
							parsed, err := url.Parse(raw)
							if err != nil || parsed.Path != "/emby/Videos/movie/hls2/fmp4-output-revision/"+name {
								t.Fatalf("slot %d map/media crossed a source slot or rendition: %q, %v", number, raw, err)
							}
							query := parsed.Query()
							if query.Get(hlsGeneratedWindowGraphQuery) != session.id || query.Get("GobyHlsId") != session.id || query.Get("StartTimeTicks") != "900000000" ||
								query.Get("MediaSourceId") != "source" || query.Get("PlaySessionId") != "play" || query.Get("api_key") != "controlled-token" {
								t.Fatalf("logical map/media admission lost its graph or source binding: %q", raw)
							}
							if seen[raw] {
								t.Fatalf("neighboring source slots reused a logical initialization or media URI: %q", raw)
							}
							seen[raw] = true
						}
					}
				})
			}
		}
	}
}

func TestGeneratedWindowFMP4ManifestKeepsIndependentGraphAdmission(t *testing.T) {
	plan, info, endpoint := generatedWindowFMP4GraphPlanFixture(24, 2)
	timeline, err := hlsGeneratedWindowTimeline(plan, info, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	graph := &hlsGeneratedWindowGraph{endpoint: endpoint, timeline: timeline}
	seen := make(map[string]bool)
	for _, id := range []string{"fmp4-graph-a", "fmp4-graph-b"} {
		session := &hlsSession{id: id, key: hlsKey{plan: plan, scope: transcode.Scope{ItemID: "movie", SourceID: "source", PlaySessionID: "play"}}}
		for variant := 0; variant < 2; variant++ {
			body, err := hlsGeneratedWindowManifest(session, graph, "Videos", "token", 0, variant)
			if err != nil {
				t.Fatal(err)
			}
			matches := generatedWindowFMP4ManifestSlots.FindAllStringSubmatch(string(body), -1)
			if len(matches) != 17 {
				t.Fatal("an independent graph lost its complete source-slot maps")
			}
			for _, match := range matches {
				for _, raw := range []string{match[1], match[3]} {
					parsed, err := url.Parse(raw)
					if err != nil || parsed.Query().Get(hlsGeneratedWindowGraphQuery) != id || seen[raw] {
						t.Fatalf("independent graphs or renditions shared an admission URI: %q, %v", raw, err)
					}
					seen[raw] = true
				}
			}
		}
	}
}

func TestGeneratedWindowManifestTSKeepsExactTextAndURLs(t *testing.T) {
	for _, count := range []int{0, 2} {
		plan, info, endpoint := generatedWindowGraphPlanFixture()
		plan.HLS.RenditionCount = count
		for variant := 0; variant < count; variant++ {
			plan.HLS.Renditions[variant] = transcode.HLSRendition{Width: 160 >> variant, Height: 96 >> variant, VideoBitrate: 256000 >> variant}
		}
		timeline, err := hlsGeneratedWindowTimeline(plan, info, endpoint)
		if err != nil {
			t.Fatal(err)
		}
		session := &hlsSession{id: "fixed-output-revision", key: hlsKey{plan: plan, scope: transcode.Scope{ItemID: "movie", SourceID: "source", PlaySessionID: "play"}}}
		graph := &hlsGeneratedWindowGraph{endpoint: endpoint, timeline: timeline}
		for variant := 0; variant < max(1, count); variant++ {
			body, err := hlsGeneratedWindowManifest(session, graph, "Videos", "controlled-token", 90*media.TicksPerSecond, variant)
			if err != nil {
				t.Fatal(err)
			}
			var expected strings.Builder
			expected.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-INDEPENDENT-SEGMENTS\n#EXT-X-TARGETDURATION:6\n#EXT-X-START:TIME-OFFSET=90.0000000,PRECISE=YES\n")
			for number := 0; number < 17; number++ {
				duration := 6
				if number == 16 {
					duration = 4
				}
				prefix := ""
				if count > 0 {
					prefix = fmt.Sprintf("v%d-", variant)
				}
				fmt.Fprintf(&expected, "#EXT-X-DISCONTINUITY\n#EXTINF:%d.0000000,\n/emby/Videos/movie/hls2/fixed-output-revision/%swindow-segment-%06d.ts?DeviceId=&GobyHlsId=fixed-output-revision&MediaSourceId=source&PlaySessionId=play&StartTimeTicks=900000000&VideoBitDepth=8&VideoCodec=h264&VideoProfile=high&api_key=controlled-token&GobyHlsWindowGraphId=fixed-output-revision\n", duration, prefix, number)
			}
			expected.WriteString("#EXT-X-ENDLIST\n")
			if string(body) != expected.String() {
				t.Fatalf("TS manifest text or URLs changed for rendition count %d variant %d:\n%s", count, variant, body)
			}
		}
	}
}

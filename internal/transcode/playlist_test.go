package transcode

import (
	"fmt"
	"strings"
	"testing"
)

const measuredPlaylist = "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:7\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:EVENT\n#EXTINF:6.006000,\nsegment-000000.ts\n#EXTINF:6.993000,\nsegment-000001.ts\n"

const fragmentedPlaylist = "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:7\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:EVENT\n#EXT-X-MAP:URI=\"v0-init.mp4\"\n#EXTINF:6.006000,\nv0-segment-000000.m4s\n#EXTINF:6.993000,\nv0-segment-000001.m4s\n"

func TestMeasuredPlaylistPreservesDurationAndEventState(t *testing.T) {
	playlist, err := ParseMediaPlaylist([]byte(measuredPlaylist))
	if err != nil || playlist.Ended || playlist.Independent || len(playlist.Segments) != 2 || playlist.Segments[1].DurationTicks != 69_930_000 {
		t.Fatalf("measured event playlist = %+v, error = %v", playlist, err)
	}
	rewritten, err := RewriteMediaPlaylist([]byte(measuredPlaylist), func(segment MediaSegment) string {
		return fmt.Sprintf("/emby/Videos/item/hls1/job/%d.ts?api_key=synthetic-test-token", segment.Number)
	})
	if err != nil || !strings.Contains(string(rewritten), "#EXTINF:6.0060000,") ||
		!strings.Contains(string(rewritten), "/1.ts?api_key=synthetic-test-token") ||
		strings.Contains(string(rewritten), "ENDLIST") || strings.Contains(string(rewritten), "INDEPENDENT-SEGMENTS") {
		t.Fatalf("rewritten measured playlist = %q, error = %v", rewritten, err)
	}
	finished := measuredPlaylist + "#EXT-X-ENDLIST\n"
	playlist, err = ParseMediaPlaylist([]byte(finished))
	if err != nil || !playlist.Ended {
		t.Fatalf("finished event playlist = %+v, error = %v", playlist, err)
	}
}

func TestGeneratedPlaylistRejectsIncompleteAndForeignResources(t *testing.T) {
	for _, bad := range []string{
		strings.Replace(measuredPlaylist, "segment-000000.ts", "https://external.example/0.ts", 1),
		strings.Replace(measuredPlaylist, "segment-000000.ts", "../segment-000000.ts", 1),
		strings.Replace(measuredPlaylist, "segment-000000.ts", "segment-000000.ts.tmp", 1),
		strings.Replace(measuredPlaylist, "segment-000001.ts", "segment-000000.ts", 1),
		strings.Replace(measuredPlaylist, "#EXTINF:6.006000,", "#EXT-X-KEY:METHOD=AES-128,URI=\"key\"\n#EXTINF:6.006000,", 1),
		strings.Replace(measuredPlaylist, "#EXTINF:6.006000,", "#EXTINF:NaN,", 1),
		strings.Replace(measuredPlaylist, "#EXTINF:6.006000,", "#EXTINF:0,", 1),
		strings.Replace(measuredPlaylist, "TARGETDURATION:7", "TARGETDURATION:6", 1),
		strings.Replace(measuredPlaylist, "PLAYLIST-TYPE:EVENT", "PLAYLIST-TYPE:VOD", 1),
		measuredPlaylist + "#EXTINF:1.000000,\n",
		measuredPlaylist + "#EXT-X-ENDLIST\nsegment-000002.ts\n",
		"\xef\xbb\xbf" + measuredPlaylist,
		strings.Repeat("x", MaxPlaylistBytes+1),
	} {
		if _, err := ParseMediaPlaylist([]byte(bad)); err == nil {
			t.Errorf("accepted invalid generated playlist with %d bytes", len(bad))
		}
	}
}

func TestPlaylistChildRewritingCannotInjectURLsOrTags(t *testing.T) {
	for _, bad := range []string{"", "https://external.example/0.ts", "//external.example/0.ts", "../0.ts", "/%2e%2e/0.ts", "/0.ts#fragment", "/0.ts\n#EXT-X-ENDLIST", `\external\0.ts`, "/0.ts?x=\"unsafe\""} {
		if _, err := RewriteMediaPlaylist([]byte(measuredPlaylist), func(MediaSegment) string { return bad }); err == nil {
			t.Errorf("accepted invalid child URL %q", bad)
		}
	}
}

func TestGeneratedPlaylistParsesAllMediaKinds(t *testing.T) {
	for _, prefix := range []string{"", "v0-", "v1-", "v2-", "v3-"} {
		for _, extension := range []string{"ts", "m4s", "aac", "mp3", "vtt"} {
			t.Run(prefix+extension, func(t *testing.T) {
				data := strings.ReplaceAll(measuredPlaylist, "segment-", prefix+"segment-")
				data = strings.ReplaceAll(data, ".ts", "."+extension)
				initName := ""
				if extension == "m4s" {
					initName = prefix + "init.mp4"
					data = strings.Replace(data, "VERSION:3", "VERSION:7", 1)
					data = strings.Replace(data, "#EXTINF:", "#EXT-X-MAP:URI=\""+initName+"\"\n#EXTINF:", 1)
				}
				playlist, err := ParseMediaPlaylist([]byte(data))
				if err != nil || playlist.InitName != initName || playlist.Ended || len(playlist.Segments) != 2 {
					t.Fatalf("generated media playlist = %+v, error = %v", playlist, err)
				}
				if playlist.Segments[1].Name != prefix+"segment-000001."+extension || playlist.Segments[1].DurationTicks != 69_930_000 {
					t.Fatalf("second media segment = %+v", playlist.Segments[1])
				}
			})
		}
	}
}

func TestGeneratedPlaylistRejectsInvalidArtifactGraphs(t *testing.T) {
	for _, invalidMap := range []string{
		"#EXT-X-MAP", "#EXT-X-MAP:URI=\"", "#EXT-X-MAP:URI=\"\"", "#EXT-X-MAP:URI=v0-init.mp4",
		"#EXT-X-MAP:URI=\"../v0-init.mp4\"", "#EXT-X-MAP:URI=\"https://example.test/init.mp4\"",
		"#EXT-X-MAP:URI=\"v0-init.mp4?token=value\"", "#EXT-X-MAP:URI=\"v0-init.mp4.tmp\"",
		"#EXT-X-MAP:URI=\"v0-init.mp4\",BYTERANGE=\"100@0\"",
		"#EXT-X-MAP:URI=\"v0-init.mp4\",URI=\"v0-init.mp4\"",
		"#EXT-X-MAP:URI=\"v0-init.mp4\"\n#EXT-X-MAP:URI=\"v0-init.mp4\"",
		"#EXT-X-MAP:URI=\"v1-init.mp4\"", "#EXT-X-MAP:URI=\"init.mp4\"",
	} {
		data := strings.Replace(fragmentedPlaylist, "#EXT-X-MAP:URI=\"v0-init.mp4\"", invalidMap, 1)
		if _, err := ParseMediaPlaylist([]byte(data)); err == nil {
			t.Errorf("accepted invalid map %q", invalidMap)
		}
	}
	for _, data := range []string{
		strings.Replace(fragmentedPlaylist, "#EXT-X-MAP:URI=\"v0-init.mp4\"\n", "", 1),
		strings.Replace(fragmentedPlaylist, "v0-segment-000001.m4s", "v1-segment-000001.m4s", 1),
		strings.Replace(fragmentedPlaylist, "v0-segment-000001.m4s", "segment-000001.m4s", 1),
		strings.Replace(fragmentedPlaylist, "v0-segment-000001.m4s", "v0-segment-000001.aac", 1),
		strings.ReplaceAll(fragmentedPlaylist, ".m4s", ".ts"),
		strings.Replace(measuredPlaylist, "segment-000001.ts", "segment-000001.aac", 1),
		strings.Replace(measuredPlaylist, "segment-000000.ts", "segment-0.ts", 1),
		strings.Replace(measuredPlaylist, "#EXTINF:6.993000,", "#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:6.993000,", 1),
		strings.Replace(fragmentedPlaylist, "#EXT-X-MAP:", "#EXT-X-DISCONTINUITY\n#EXT-X-MAP:", 1),
	} {
		if _, err := ParseMediaPlaylist([]byte(data)); err == nil {
			t.Errorf("accepted invalid artifact graph %q", data)
		}
	}
}

func TestFragmentedPlaylistRewritesAuthorizedInitializationAndMedia(t *testing.T) {
	var mappedNames []string
	rewritten, err := RewriteMediaPlaylistWithMap([]byte(fragmentedPlaylist+"#EXT-X-ENDLIST\n"), func(segment MediaSegment) string {
		mappedNames = append(mappedNames, segment.Name)
		return "/media/" + segment.Name + "?api_key=synthetic-test-token"
	}, func(name string) string {
		mappedNames = append(mappedNames, name)
		return "/media/" + name + "?api_key=synthetic-test-token"
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(mappedNames, ",") != "v0-init.mp4,v0-segment-000000.m4s,v0-segment-000001.m4s" {
		t.Fatalf("rewritten artifact names = %v", mappedNames)
	}
	for _, expected := range []string{
		"#EXT-X-MAP:URI=\"/media/v0-init.mp4?api_key=synthetic-test-token\"\n",
		"#EXTINF:6.0060000,\n/media/v0-segment-000000.m4s?api_key=synthetic-test-token\n",
		"#EXTINF:6.9930000,\n/media/v0-segment-000001.m4s?api_key=synthetic-test-token\n",
		"#EXT-X-ENDLIST\n",
	} {
		if !strings.Contains(string(rewritten), expected) {
			t.Errorf("rewritten playlist is missing %q: %s", expected, rewritten)
		}
	}
	if _, err := RewriteMediaPlaylist([]byte(fragmentedPlaylist), func(segment MediaSegment) string { return segment.Name }); err == nil {
		t.Fatal("legacy rewrite accepted a map without an initialization callback")
	}
	if _, err := RewriteMediaPlaylistWithMap([]byte(fragmentedPlaylist), nil, func(name string) string { return name }); err == nil {
		t.Fatal("rewrite accepted a missing media callback")
	}
}

func TestInitializationRewritingCannotInjectURLsOrTags(t *testing.T) {
	for _, bad := range []string{
		"", "https://example.test/init.mp4", "//example.test/init.mp4", "../init.mp4", "/%2e%2e/init.mp4",
		"/init.mp4#fragment", "/init.mp4\n#EXT-X-ENDLIST", `\external\init.mp4`, "/init.mp4?x=\"unsafe\"",
	} {
		_, err := RewriteMediaPlaylistWithMap([]byte(fragmentedPlaylist), func(segment MediaSegment) string {
			return segment.Name
		}, func(string) string { return bad })
		if err == nil {
			t.Errorf("accepted invalid initialization URL %q", bad)
		}
	}
}

func TestMediaPlaylistWithoutMapDoesNotInvokeInitializationCallback(t *testing.T) {
	rewritten, err := RewriteMediaPlaylistWithMap([]byte(measuredPlaylist), func(segment MediaSegment) string {
		return segment.Name
	}, func(string) string {
		t.Fatal("initialization callback invoked for a playlist without a map")
		return ""
	})
	if err != nil || strings.Contains(string(rewritten), "#EXT-X-MAP") {
		t.Fatalf("playlist without map = %q, error = %v", rewritten, err)
	}
}

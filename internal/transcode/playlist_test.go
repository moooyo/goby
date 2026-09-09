package transcode

import (
	"fmt"
	"strings"
	"testing"
)

const measuredPlaylist = "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:7\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:EVENT\n#EXTINF:6.006000,\nsegment-000000.ts\n#EXTINF:6.993000,\nsegment-000001.ts\n"

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

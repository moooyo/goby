package transcode

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const generatedWindowInitialMapPlaylist = "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:3\n#EXT-X-MEDIA-SEQUENCE:71\n#EXT-X-PLAYLIST-TYPE:EVENT\n" +
	"#EXT-X-DISCONTINUITY\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:3.000000,\nsegment-000071.m4s\n#EXTINF:1.000000,\nsegment-000072.m4s\n#EXT-X-ENDLIST\n"

func TestGeneratedWindowInitialEpochMarkerBeforeMapRetainsItsFragment(t *testing.T) {
	before, err := ParseMediaPlaylist([]byte(generatedWindowInitialMapPlaylist))
	if err != nil || before.Sequence != 71 || before.InitName != "init.mp4" || len(before.Segments) != 2 ||
		!before.Segments[0].Discontinuity || before.Segments[1].Discontinuity {
		t.Fatalf("first map lost its encoder epoch: %+v: %v", before, err)
	}
	rewritten, err := RewriteMediaPlaylistWithMap([]byte(generatedWindowInitialMapPlaylist),
		func(segment MediaSegment) string { return segment.Name }, func(name string) string { return name })
	if err != nil {
		t.Fatal(err)
	}
	after, err := ParseMediaPlaylist(rewritten)
	if err != nil || !reflect.DeepEqual(before, after) ||
		!strings.Contains(string(rewritten), "#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-DISCONTINUITY\n#EXTINF:3.0000000,\nsegment-000071.m4s") {
		t.Fatalf("rewriting moved the epoch to another fragment: %+v: %v: %s", after, err, rewritten)
	}
}

func TestGeneratedWindowInitialMapStillRejectsAmbiguousEpochs(t *testing.T) {
	for name, data := range map[string]string{
		"duplicate before map":    strings.Replace(generatedWindowInitialMapPlaylist, "#EXT-X-DISCONTINUITY\n", "#EXT-X-DISCONTINUITY\n#EXT-X-DISCONTINUITY\n", 1),
		"duplicate after map":     strings.Replace(generatedWindowInitialMapPlaylist, "#EXT-X-MAP:URI=\"init.mp4\"\n", "#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-DISCONTINUITY\n", 1),
		"second map":              strings.Replace(generatedWindowInitialMapPlaylist, "#EXT-X-MAP:URI=\"init.mp4\"\n", "#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-MAP:URI=\"init.mp4\"\n", 1),
		"map after media":         strings.Replace(generatedWindowInitialMapPlaylist, "segment-000071.m4s\n", "segment-000071.m4s\n#EXT-X-MAP:URI=\"init.mp4\"\n", 1),
		"map inside duration":     strings.Replace(generatedWindowInitialMapPlaylist, "#EXTINF:3.000000,\n", "#EXTINF:3.000000,\n#EXT-X-MAP:URI=\"init.mp4\"\n", 1),
		"unattached final marker": strings.Replace(generatedWindowInitialMapPlaylist, "#EXT-X-ENDLIST\n", "#EXT-X-DISCONTINUITY\n#EXT-X-ENDLIST\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseMediaPlaylist([]byte(data)); !errors.Is(err, ErrInvalidPlaylist) {
				t.Fatalf("ambiguous first map or epoch accepted: %v", err)
			}
		})
	}
}

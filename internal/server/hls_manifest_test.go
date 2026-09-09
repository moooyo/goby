package server

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestHLSMasterPlaylistSingleVariantPreservesAuthorizedURL(t *testing.T) {
	child := "main.m3u8?api_key=A%2FB%2BC%3D&PlaySessionId=session&StartTimeTicks=12345678"
	playlist, err := hlsMasterPlaylist(child, 4_128_000, 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	want := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-STREAM-INF:BANDWIDTH=4128000,RESOLUTION=1920x1080\n" + child + "\n"
	if string(playlist) != want {
		t.Fatalf("master playlist = %q", playlist)
	}
	if !utf8.Valid(playlist) || bytes.HasPrefix(playlist, []byte{0xef, 0xbb, 0xbf}) || strings.Contains(string(playlist), "CODECS") || strings.Contains(string(playlist), "INDEPENDENT-SEGMENTS") {
		t.Fatalf("master made an unsupported claim or used invalid text: %q", playlist)
	}
	audio, err := hlsMasterPlaylist("/emby/Videos/item/main.m3u8?api_key=token", 128_000, 0, 0)
	if err != nil || strings.Contains(string(audio), "RESOLUTION") {
		t.Fatalf("master without dimensions = (%q, %v)", audio, err)
	}
	for _, input := range []struct {
		bandwidth     int64
		width, height int
	}{{0, 0, 0}, {-1, 0, 0}, {1, -1, 1}, {1, 1, -1}, {1, 1920, 0}, {1, 0, 1080}} {
		if _, err := hlsMasterPlaylist("main.m3u8", input.bandwidth, input.width, input.height); !errors.Is(err, errInvalidHLSManifest) {
			t.Errorf("invalid variant %+v = %v", input, err)
		}
	}
}

func TestHLSVODPlaylistKeepsWholeTimelineAndPreciseOffsetHint(t *testing.T) {
	timeline := transcode.Timeline{TargetDuration: 5, Segments: []transcode.TimelineSegment{
		{Number: 0, StartTicks: 0, DurationTicks: 30_000_001},
		{Number: 1, StartTicks: 30_000_001, DurationTicks: 23_333_333},
		{Number: 2, StartTicks: 53_333_334, DurationTicks: 46_666_666},
	}}
	var requested []int
	playlist, err := hlsVODPlaylist(timeline, 34_567_890, func(segment transcode.TimelineSegment) string {
		requested = append(requested, segment.Number)
		return fmt.Sprintf("segment-%06d.ts?api_key=A%%2FB%%2BC&PlaySessionId=session", segment.Number)
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:5\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n" +
		"#EXT-X-START:TIME-OFFSET=3.4567890,PRECISE=YES\n" +
		"#EXTINF:3.0000001,\nsegment-000000.ts?api_key=A%2FB%2BC&PlaySessionId=session\n" +
		"#EXT-X-DISCONTINUITY\n" +
		"#EXTINF:2.3333333,\nsegment-000001.ts?api_key=A%2FB%2BC&PlaySessionId=session\n" +
		"#EXT-X-DISCONTINUITY\n" +
		"#EXTINF:4.6666666,\nsegment-000002.ts?api_key=A%2FB%2BC&PlaySessionId=session\n#EXT-X-ENDLIST\n"
	if string(playlist) != want {
		t.Fatalf("VOD playlist = %q", playlist)
	}
	if fmt.Sprint(requested) != "[0 1 2]" {
		t.Fatalf("seek truncated the source timeline: %v", requested)
	}
	if !utf8.Valid(playlist) || bytes.HasPrefix(playlist, []byte{0xef, 0xbb, 0xbf}) || strings.Contains(string(playlist), "INDEPENDENT-SEGMENTS") {
		t.Fatal("VOD has invalid text or an unsupported segment claim")
	}
	fromStart, err := hlsVODPlaylist(timeline, 0, hlsTestChildURL)
	if err != nil || strings.Contains(string(fromStart), "#EXT-X-START:") || strings.Count(string(fromStart), "#EXTINF:") != 3 ||
		strings.Count(string(fromStart), "#EXT-X-DISCONTINUITY\n") != 2 {
		t.Fatalf("VOD from zero = (%q, %v)", fromStart, err)
	}
	nearEnd, err := hlsVODPlaylist(timeline, 99_999_999, hlsTestChildURL)
	if err != nil || !strings.Contains(string(nearEnd), "TIME-OFFSET=9.9999999") || strings.Count(string(nearEnd), "#EXTINF:") != 3 ||
		strings.Count(string(nearEnd), "#EXT-X-DISCONTINUITY\n") != 2 {
		t.Fatalf("VOD near the end = (%q, %v)", nearEnd, err)
	}
}

func TestHLSVODPlaylistRetainsIrregularMeasuredSpans(t *testing.T) {
	timeline := transcode.Timeline{TargetDuration: 7, Segments: []transcode.TimelineSegment{
		{Number: 0, DurationTicks: 61_234_567},
		{Number: 1, StartTicks: 61_234_567, DurationTicks: 39_876_544},
		{Number: 2, StartTicks: 101_111_111, DurationTicks: 1},
	}}
	playlist, err := hlsVODPlaylist(timeline, 0, hlsTestChildURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, duration := range []string{"#EXTINF:6.1234567,", "#EXTINF:3.9876544,", "#EXTINF:0.0000001,"} {
		if !strings.Contains(string(playlist), duration+"\n") {
			t.Fatalf("measured duration %q was changed: %s", duration, playlist)
		}
	}
	// RFC 8216 uses nearest-integer comparison, not a requirement that the
	// target exceed the exact decimal value of every EXTINF.
	rounded := transcode.Timeline{TargetDuration: 4, Segments: []transcode.TimelineSegment{{DurationTicks: 44_999_999}}}
	if _, err := hlsVODPlaylist(rounded, 0, hlsTestChildURL); err != nil {
		t.Fatalf("valid rounded target duration was rejected: %v", err)
	}
	rounded.Segments[0].DurationTicks = 45_000_000
	if _, err := hlsVODPlaylist(rounded, 0, hlsTestChildURL); !errors.Is(err, errInvalidHLSManifest) {
		t.Fatalf("insufficient target duration = %v", err)
	}
}

func TestHLSVODPlaylistRejectsIncompleteOrInvalidTimelines(t *testing.T) {
	valid := func() transcode.Timeline {
		return transcode.Timeline{TargetDuration: 2, Segments: []transcode.TimelineSegment{
			{Number: 0, DurationTicks: media.TicksPerSecond},
			{Number: 1, StartTicks: media.TicksPerSecond, DurationTicks: media.TicksPerSecond},
		}}
	}
	tests := []struct {
		name   string
		change func(*transcode.Timeline)
		start  int64
	}{
		{"empty", func(value *transcode.Timeline) { value.Segments = nil }, 0},
		{"zero target", func(value *transcode.Timeline) { value.TargetDuration = 0 }, 0},
		{"negative target", func(value *transcode.Timeline) { value.TargetDuration = -1 }, 0},
		{"initial number", func(value *transcode.Timeline) { value.Segments[0].Number = 1 }, 0},
		{"number gap", func(value *transcode.Timeline) { value.Segments[1].Number = 2 }, 0},
		{"initial position", func(value *transcode.Timeline) { value.Segments[0].StartTicks = 1 }, 0},
		{"negative position", func(value *transcode.Timeline) { value.Segments[0].StartTicks = -1 }, 0},
		{"gap", func(value *transcode.Timeline) { value.Segments[1].StartTicks++ }, 0},
		{"overlap", func(value *transcode.Timeline) { value.Segments[1].StartTicks-- }, 0},
		{"zero duration", func(value *transcode.Timeline) { value.Segments[0].DurationTicks = 0 }, 0},
		{"negative duration", func(value *transcode.Timeline) { value.Segments[0].DurationTicks = -1 }, 0},
		{"negative seek", func(*transcode.Timeline) {}, -1},
		{"exclusive end seek", func(*transcode.Timeline) {}, 2 * media.TicksPerSecond},
		{"past end seek", func(*transcode.Timeline) {}, 3 * media.TicksPerSecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			timeline := valid()
			test.change(&timeline)
			calls := 0
			if _, err := hlsVODPlaylist(timeline, test.start, func(segment transcode.TimelineSegment) string {
				calls++
				return hlsTestChildURL(segment)
			}); !errors.Is(err, errInvalidHLSManifest) {
				t.Fatalf("invalid timeline = %v", err)
			}
			if calls != 0 {
				t.Fatalf("invalid timeline called the child URL generator %d times", calls)
			}
		})
	}
	if _, err := hlsVODPlaylist(valid(), 0, nil); !errors.Is(err, errInvalidHLSManifest) {
		t.Fatalf("nil child URL generator = %v", err)
	}
	overflow := transcode.Timeline{TargetDuration: math.MaxInt, Segments: []transcode.TimelineSegment{
		{DurationTicks: math.MaxInt64}, {Number: 1, StartTicks: math.MaxInt64, DurationTicks: 1},
	}}
	if _, err := hlsVODPlaylist(overflow, 0, hlsTestChildURL); !errors.Is(err, errInvalidHLSManifest) {
		t.Fatalf("overflowing total duration = %v", err)
	}
	tooMany := transcode.Timeline{TargetDuration: 1, Segments: make([]transcode.TimelineSegment, transcode.MaxTimelineSegments+1)}
	if _, err := hlsVODPlaylist(tooMany, 0, hlsTestChildURL); !errors.Is(err, errHLSManifestLimit) {
		t.Fatalf("excessive segment count = %v", err)
	}
}

func TestHLSManifestURLsRejectInjectionExternalPathsAndTraversal(t *testing.T) {
	timeline := transcode.Timeline{TargetDuration: 1, Segments: []transcode.TimelineSegment{{DurationTicks: media.TicksPerSecond}}}
	for _, unsafe := range []string{
		"", "https://example.com/main.m3u8", "http:main.m3u8", "//example.com/main.m3u8", "///example.com/main.m3u8",
		"#EXT-X-ENDLIST", "main.m3u8#fragment", "main.m3u8#", "main.m3u8\n#EXT-X-ENDLIST", "main.m3u8\r\nsegment.ts",
		" main.m3u8", "main.m3u8 ", "main\t.m3u8", "main.m3u8\x00", "main\x7f.m3u8", "\ufeffmain.m3u8",
		"../main.m3u8", "./main.m3u8", "parent/../main.m3u8", "/parent/./main.m3u8", "parent\\main.m3u8",
		"%2e%2e/main.m3u8", "parent/%2E%2E/main.m3u8", "parent/%2e%2e%2fmain.m3u8", "%2f%2fexample.com/main.m3u8",
		"parent/%5c..%5cmain.m3u8", "%252e%252e/main.m3u8", "https%3a%2f%2fexample.com/main.m3u8",
		"main%0a.m3u8", "main%00.m3u8", "main.m3u8?api_key=token%0D%0A%23EXT-X-ENDLIST", "main.m3u8?api_key=%00",
		"main.m3u8?api_key=%ff", "main.m3u8?api_key=%", "main%ZZ.m3u8", "?api_key=token", `main".m3u8`, "main<.m3u8",
	} {
		t.Run(fmt.Sprintf("%q", unsafe), func(t *testing.T) {
			if _, err := hlsMasterPlaylist(unsafe, 128_000, 0, 0); !errors.Is(err, errInvalidHLSManifest) {
				t.Fatalf("unsafe master URI = %v", err)
			}
			if _, err := hlsVODPlaylist(timeline, 0, func(transcode.TimelineSegment) string { return unsafe }); !errors.Is(err, errInvalidHLSManifest) {
				t.Fatalf("unsafe segment URI = %v", err)
			}
		})
	}
	for _, safe := range []string{
		"main.m3u8", "/emby/Videos/item/main.m3u8?api_key=A%2FB%2BC%3D", "hls/main.m3u8?api_key=a+b&empty=&flag",
		"main.m3u8?api_key=%252F", "%E7%89%87%E6%AE%B5.ts?api_key=token", "main%20file.m3u8?api_key=token",
	} {
		playlist, err := hlsMasterPlaylist(safe, 128_000, 0, 0)
		if err != nil || !strings.HasSuffix(string(playlist), safe+"\n") {
			t.Errorf("safe URI was rejected or rewritten: (%q, %v)", playlist, err)
		}
	}
}

func TestHLSManifestBoundsEachURLAndCompleteOutput(t *testing.T) {
	single := transcode.Timeline{TargetDuration: 1, Segments: []transcode.TimelineSegment{{DurationTicks: media.TicksPerSecond}}}
	const prefix = "segment.ts?api_key="
	longest := prefix + strings.Repeat("t", maxHLSManifestURLBytes-len(prefix))
	if _, err := hlsMasterPlaylist(longest, 128_000, 0, 0); err != nil {
		t.Fatalf("maximum-length URI = %v", err)
	}
	firstOnly, err := hlsVODPlaylist(single, 0, func(transcode.TimelineSegment) string { return longest })
	if err != nil || strings.Contains(string(firstOnly), "#EXT-X-DISCONTINUITY") {
		t.Fatalf("single-segment playlist = (%q, %v)", firstOnly, err)
	}
	if _, err := hlsMasterPlaylist(longest+"t", 128_000, 0, 0); !errors.Is(err, errInvalidHLSManifest) {
		t.Fatalf("oversized URI = %v", err)
	}
	timeline := transcode.Timeline{TargetDuration: 1, Segments: make([]transcode.TimelineSegment, 1024)}
	for number := range timeline.Segments {
		timeline.Segments[number] = transcode.TimelineSegment{Number: number, StartTicks: int64(number) * media.TicksPerSecond, DurationTicks: media.TicksPerSecond}
	}
	base, err := hlsVODPlaylist(timeline, 0, func(transcode.TimelineSegment) string { return prefix })
	if err != nil {
		t.Fatal(err)
	}
	padding := (maxHLSManifestBytes - len(base)) / len(timeline.Segments)
	remainder := (maxHLSManifestBytes - len(base)) % len(timeline.Segments)
	urlAtBoundary := func(segment transcode.TimelineSegment) string {
		extra := padding
		if segment.Number == 0 {
			extra += remainder
		}
		return prefix + strings.Repeat("t", extra)
	}
	boundary, err := hlsVODPlaylist(timeline, 0, urlAtBoundary)
	if err != nil || len(boundary) != maxHLSManifestBytes || !strings.HasSuffix(string(boundary), "#EXT-X-ENDLIST\n") ||
		strings.Count(string(boundary), "#EXT-X-DISCONTINUITY\n") != len(timeline.Segments)-1 {
		t.Fatalf("manifest at output boundary = (%d bytes, %v)", len(boundary), err)
	}
	oversized, err := hlsVODPlaylist(timeline, 0, func(segment transcode.TimelineSegment) string {
		child := urlAtBoundary(segment)
		if segment.Number == len(timeline.Segments)-1 {
			child += "t"
		}
		return child
	})
	if !errors.Is(err, errHLSManifestLimit) || oversized != nil {
		t.Fatalf("oversized manifest = (%d bytes, %v)", len(oversized), err)
	}
}

func TestHLSVODPlaylistUsesValidatedTimelineSnapshot(t *testing.T) {
	timeline := transcode.Timeline{TargetDuration: 2, Segments: []transcode.TimelineSegment{
		{DurationTicks: media.TicksPerSecond},
		{Number: 1, StartTicks: media.TicksPerSecond, DurationTicks: 2 * media.TicksPerSecond},
	}}
	playlist, err := hlsVODPlaylist(timeline, 0, func(segment transcode.TimelineSegment) string {
		if segment.Number == 0 {
			timeline.Segments[1].DurationTicks = -1
		}
		return hlsTestChildURL(segment)
	})
	if err != nil || !strings.Contains(string(playlist), "#EXTINF:2.0000000,\nsegment-000001.ts") {
		t.Fatalf("callback changed the validated timeline: (%q, %v)", playlist, err)
	}
}

func hlsTestChildURL(segment transcode.TimelineSegment) string {
	return fmt.Sprintf("segment-%06d.ts?api_key=token", segment.Number)
}

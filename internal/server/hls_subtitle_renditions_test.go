package server

import (
	"bytes"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func hlsSubtitleTestSession() *hlsSession {
	plan := transcode.Plan{Container: "mp4", VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: 1, AudioCodec: "aac", DurationTicks: 12 * media.TicksPerSecond,
		Width: 320, Height: 192, VideoBitrate: 500000, AudioBitrate: 64000, SegmentSeconds: 3, HLS: transcode.HLSPlan{SegmentType: "fmp4"}}
	plan.HLS.Subtitles.Count = 2
	plan.HLS.Subtitles.Tracks[0] = transcode.HLSSubtitleTrack{StreamIndex: 4, Codec: "subrip"}
	plan.HLS.Subtitles.Tracks[1] = transcode.HLSSubtitleTrack{StreamIndex: 9, Codec: "ass"}
	return &hlsSession{id: "revision", key: hlsKey{plan: plan, scope: transcode.Scope{ItemID: "movie", SourceID: "source", PlaySessionID: "play", DeviceID: "device"}},
		output: playback.Source{Info: media.Info{Bitrate: 564000}}, subtitleView: playback.HLSSubtitleView{SelectedStreamIndex: 4, SelectionSet: true},
		subtitleSource: playback.Source{Info: media.Info{Streams: []media.Stream{
			{Index: 4, Codec: "subrip", CodecType: "subtitle", Language: "en", Title: "English \"caption\"\nURI=\"injected\"", IsTextSubtitleStream: true},
			{Index: 9, Codec: "ass", CodecType: "subtitle", Language: "fr", IsTextSubtitleStream: true},
		}}}}
}

func TestHLSSubtitleViewsPreserveProducerAndEveryPublishedURL(t *testing.T) {
	session := hlsSubtitleTestSession()
	originalPlan, originalView := session.key.plan, session.subtitleView
	for _, selection := range []int{4, 9, -1} {
		view, err := playback.HLSSubtitleViewFor(session.key.plan, &selection, 2500000)
		if err != nil {
			t.Fatal(err)
		}
		body, err := hlsGeneratedMasterView(session, "Videos", "A/B+C", 65000000, view)
		if err != nil || bytes.Count(body, []byte("#EXT-X-MEDIA:TYPE=SUBTITLES")) != 2 {
			t.Fatalf("master lost a bound rendition: %s, %v", body, err)
		}
		if bytes.Contains(body, []byte("\nURI=")) || bytes.Contains(body, []byte("NAME=\"English \"")) {
			t.Fatal("untrusted subtitle labels escaped the HLS attribute")
		}
		wantDefaults := 1
		if selection == -1 {
			wantDefaults = 0
		}
		if bytes.Count(body, []byte("DEFAULT=YES")) != wantDefaults || bytes.Count(body, []byte("AUTOSELECT=YES")) != wantDefaults {
			t.Fatal("off or selected view changed automatic track selection")
		}
		for _, line := range strings.Split(string(body), "\n") {
			child := line
			if strings.HasPrefix(line, "#EXT-X-MEDIA:") {
				_, value, ok := strings.Cut(line, "URI=\"")
				if !ok {
					t.Fatal("subtitle URL was omitted")
				}
				child = strings.TrimSuffix(value, "\"")
			} else if line == "" || line[0] == '#' {
				continue
			}
			parsed, err := url.Parse(child)
			if err != nil || parsed.Query().Get("api_key") != "A/B+C" || parsed.Query().Get("SubtitleOffsetTicks") != "2500000" || parsed.Query().Get("GobyHlsId") != session.id {
				t.Fatalf("child lost its immutable view or authority: %s", child)
			}
			values := make(map[string]string)
			for key, items := range parsed.Query() {
				values[strings.ToLower(key)] = items[0]
			}
			actual, err := hlsSubtitleRequestView(values, session)
			if err != nil || actual.SelectedStreamIndex != selection || actual.OffsetTicks != view.OffsetTicks {
				t.Fatalf("published view cannot round trip: %+v, %v", actual, err)
			}
		}
	}
	if session.key.plan != originalPlan || session.subtitleView != originalView {
		t.Fatal("presenting a subtitle view mutated the shared producer or prior default")
	}
	if _, err := hlsSubtitleRequestView(map[string]string{"subtitlestreamindex": "7"}, session); err == nil {
		t.Fatal("an unbound subtitle was selected")
	}
}

func TestHLSSubtitleArtifactSlotsAreCanonicalAndIndependentOfSelection(t *testing.T) {
	session := hlsSubtitleTestSession()
	view := playback.HLSSubtitleView{SelectedStreamIndex: -1, SelectionSet: true}
	for _, test := range []struct {
		name     string
		slot     int
		sequence int64
		playlist bool
	}{{"subtitles-0.m3u8", 0, -1, true}, {"subtitles-1-segment-000019.vtt", 1, 19, false}} {
		slot, sequence, playlist, ok := hlsSubtitleArtifact(session.key.plan, test.name, view)
		if !ok || slot != test.slot || sequence != test.sequence || playlist != test.playlist {
			t.Fatalf("bound artifact did not retain identity: %s", test.name)
		}
	}
	for _, name := range []string{"subtitles-2.m3u8", "subtitles--1.m3u8", "subtitles-01.m3u8", "subtitles-0-segment-1.vtt", "subtitles-0-segment--00001.vtt", "../subtitles-0.m3u8", "subtitles.vtt"} {
		if _, _, _, ok := hlsSubtitleArtifact(session.key.plan, name, view); ok {
			t.Fatalf("unbound or ambiguous subtitle path accepted: %s", name)
		}
	}
}

func TestHLSSubtitleWindowsRetainMeasuredRollingEpoch(t *testing.T) {
	var timeline hlsSubtitleTimeline
	list := transcode.MediaPlaylist{Sequence: 0, TargetDuration: 4, Segments: []transcode.MediaSegment{
		{Number: 0, DurationTicks: 25000000}, {Number: 1, DurationTicks: 12500000}, {Number: 2, DurationTicks: 37500000},
	}}
	windows, err := timeline.observe("job", 800000, list)
	if err != nil || windows[1].Start != 25800000 || windows[2].End != 75800000 {
		t.Fatalf("window was inferred from nominal durations: %+v, %v", windows, err)
	}
	list.Sequence, list.Segments = 2, []transcode.MediaSegment{{Number: 2, DurationTicks: 37500000}, {Number: 3, DurationTicks: 22000000}}
	windows, err = timeline.observe("job", 800000, list)
	if err != nil || windows[2].Start != 38300000 || windows[3].End != 97800000 {
		t.Fatalf("rolling window lost the earlier epoch anchor: %+v, %v", windows, err)
	}
	before := make(map[int64]hlsSubtitleWindow)
	for number, window := range timeline.Windows {
		before[number] = window
	}
	list.Segments[0].DurationTicks++
	if _, err := timeline.observe("job", 800000, list); err == nil || !reflect.DeepEqual(before, timeline.Windows) {
		t.Fatal("a published media boundary changed under an existing subtitle URL")
	}
	list.Sequence, list.Segments = 8, []transcode.MediaSegment{{Number: 8, DurationTicks: 30000000}}
	if _, err := timeline.observe("job", 800000, list); err == nil {
		t.Fatal("an unknown rolling gap used sequence times nominal duration")
	}
	if _, err := timeline.observe("replacement", 800000, list); err == nil {
		t.Fatal("a replacement producer inherited another epoch's anchor")
	}
}

func TestHLSSubtitleManifestMirrorsActualDurationsAndCompletion(t *testing.T) {
	session := hlsSubtitleTestSession()
	list := transcode.MediaPlaylist{Sequence: 7, TargetDuration: 4, Segments: []transcode.MediaSegment{
		{Number: 7, DurationTicks: 35000000}, {Number: 8, DurationTicks: 12500000, Discontinuity: true},
	}}
	view := playback.HLSSubtitleView{SelectedStreamIndex: 9, SelectionSet: true, OffsetTicks: -2500000}
	body, err := hlsSubtitleManifest(session, "Videos", "token", 0, 1, view, list)
	if err != nil || !bytes.Contains(body, []byte("#EXT-X-MEDIA-SEQUENCE:7\n")) || !bytes.Contains(body, []byte("#EXTINF:3.5000000,")) ||
		!bytes.Contains(body, []byte("#EXTINF:1.2500000,")) || !bytes.Contains(body, []byte("#EXT-X-DISCONTINUITY\n")) || bytes.Contains(body, []byte("#EXT-X-ENDLIST")) {
		t.Fatalf("subtitle manifest invented a media duration or completion: %s, %v", body, err)
	}
	list.Ended = true
	body, err = hlsSubtitleManifest(session, "Videos", "token", 0, 1, view, list)
	if err != nil || !bytes.HasSuffix(body, []byte("#EXT-X-ENDLIST\n")) {
		t.Fatal("completed media did not close the subtitle window")
	}
}

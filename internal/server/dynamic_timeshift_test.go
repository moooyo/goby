package server

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/timeshift"
	"github.com/moooyo/goby/internal/transcode"
)

func dynamicWindowTestID(number uint64) string { return fmt.Sprintf("a_%032x", number) }

func dynamicWindowTestSnapshot() timeshift.WindowSnapshot {
	const start = 100 * media.TicksPerSecond
	durations := []int64{24 * media.TicksPerSecond / 10, 11 * media.TicksPerSecond / 10, 425 * media.TicksPerSecond / 100,
		2 * media.TicksPerSecond, 3 * media.TicksPerSecond, 4 * media.TicksPerSecond}
	generations := []uint64{4, 4, 9, 9, 9, 11}
	discontinuities := []uint64{7, 7, 8, 8, 8, 9}
	snapshot := timeshift.WindowSnapshot{PresentationID: "window", NextSequence: 37, EarliestTicks: start, LiveStartTicks: start, TargetDurationTicks: 5 * media.TicksPerSecond, DiscontinuitySequence: 7,
		Variants: []timeshift.Variant{{ID: "r0", Kind: "video", Format: "fmp4"}, {ID: "r1", Kind: "video", Format: "fmp4"}}}
	for _, epoch := range []struct{ generation, sequence, discontinuity uint64 }{{4, 30, 7}, {9, 33, 8}, {11, 36, 9}} {
		snapshot.Epochs = append(snapshot.Epochs, timeshift.Epoch{Generation: epoch.generation, FirstSequence: epoch.sequence, DiscontinuitySequence: epoch.discontinuity,
			Initializations: []timeshift.Artifact{{ID: dynamicWindowTestID(1000 + epoch.generation*2), VariantID: "r0", Size: 80}, {ID: dynamicWindowTestID(1001 + epoch.generation*2), VariantID: "r1", Size: 80}}})
	}
	position := int64(start)
	for index, duration := range durations {
		generation := generations[index]
		segment := timeshift.Segment{Sequence: uint64(31 + index), Generation: generation, StartTicks: position, DurationTicks: duration,
			Discontinuity: index == 2 || index == 5, DiscontinuitySequence: discontinuities[index]}
		for variant := 0; variant < 2; variant++ {
			segment.Artifacts = append(segment.Artifacts, timeshift.Artifact{ID: dynamicWindowTestID(uint64(100 + index*2 + variant)), VariantID: "r" + strconv.Itoa(variant),
				InitID: dynamicWindowTestID(1000 + generation*2 + uint64(variant)), Size: 128})
		}
		snapshot.Segments = append(snapshot.Segments, segment)
		position += duration
	}
	snapshot.LiveEdgeTicks = position
	return snapshot
}

func dynamicWindowTestResource(id, format string, initialization bool) string {
	return "/media/" + dynamicArtifactName(id, format, initialization) + "?SubtitleStreamIndex=-1&SubtitleOffsetTicks=0"
}

// This small independent observer implements the RFC 8216 section 6.2.1
// discontinuity-number rule: base plus preceding discontinuity tags. It does
// not reuse the product playlist parser or assume one initialization per file.
func dynamicWindowObservePlaylist(t *testing.T, data []byte) (sequences, discontinuities []uint64, initializations, mediaURLs, durations []string) {
	t.Helper()
	var sequence, discontinuity uint64
	currentInit := ""
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			value, err := strconv.ParseUint(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			sequence = value
		case strings.HasPrefix(line, "#EXT-X-DISCONTINUITY-SEQUENCE:"):
			value, err := strconv.ParseUint(strings.TrimPrefix(line, "#EXT-X-DISCONTINUITY-SEQUENCE:"), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			discontinuity = value
		case line == "#EXT-X-DISCONTINUITY":
			discontinuity++
		case strings.HasPrefix(line, "#EXT-X-MAP:URI=\""):
			currentInit = strings.TrimSuffix(strings.TrimPrefix(line, "#EXT-X-MAP:URI=\""), "\"")
		case strings.HasPrefix(line, "#EXTINF:"):
			durations = append(durations, strings.TrimSuffix(strings.TrimPrefix(line, "#EXTINF:"), ","))
		case line != "" && !strings.HasPrefix(line, "#"):
			sequences = append(sequences, sequence)
			sequence++
			discontinuities = append(discontinuities, discontinuity)
			initializations = append(initializations, currentInit)
			mediaURLs = append(mediaURLs, line)
		}
	}
	return
}

func TestDynamicWindowPlaylistPreservesObservedDurationsEpochsAndVariantIsolation(t *testing.T) {
	snapshot := dynamicWindowTestSnapshot()
	requested := snapshot.EarliestTicks + 56*media.TicksPerSecond/10
	view := dynamicPlaybackView{StartTicks: &requested}
	for variant := 0; variant < 2; variant++ {
		variantID := "r" + strconv.Itoa(variant)
		var called []string
		data, err := dynamicWindowPlaylist(snapshot, variantID, view, func(id, format string, init bool) string {
			called = append(called, id)
			return dynamicWindowTestResource(id, format, init)
		})
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, expected := range []string{"#EXT-X-TARGETDURATION:5\n", "#EXT-X-MEDIA-SEQUENCE:31\n", "#EXT-X-DISCONTINUITY-SEQUENCE:7\n", "#EXT-X-START:TIME-OFFSET=3.5000000,PRECISE=NO\n"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("playlist lost an observed boundary: %s", expected)
			}
		}
		if strings.Contains(text, "#EXT-X-ENDLIST") || strings.Contains(text, "#EXT-X-PLAYLIST-TYPE") || strings.Contains(text, "#EXT-X-INDEPENDENT-SEGMENTS") {
			t.Fatal("a rolling window invented completion, immutability, or independent decoding")
		}
		sequences, discontinuities, inits, urls, durations := dynamicWindowObservePlaylist(t, data)
		if !reflect.DeepEqual(sequences, []uint64{31, 32, 33, 34, 35, 36}) || !reflect.DeepEqual(discontinuities, []uint64{7, 7, 8, 8, 8, 9}) ||
			!reflect.DeepEqual(durations, []string{"2.4000000", "1.1000000", "4.2500000", "2.0000000", "3.0000000", "4.0000000"}) {
			t.Fatal("variable-duration media was replaced by nominal timing or a reset sequence")
		}
		allowed := make(map[string]bool)
		for index, segment := range snapshot.Segments {
			artifact := segment.Artifacts[variant]
			allowed[artifact.ID], allowed[artifact.InitID] = true, true
			if urls[index] != dynamicWindowTestResource(artifact.ID, "fmp4", false) || inits[index] != dynamicWindowTestResource(artifact.InitID, "fmp4", true) {
				t.Fatal("media or initialization referred to a different variant or epoch")
			}
		}
		for _, id := range called {
			if !allowed[id] {
				t.Fatal("the resource callback saw an artifact outside the selected variant")
			}
		}
		if strings.Count(text, "#EXT-X-MAP:") != 3 {
			t.Fatal("initialization was not replaced exactly at each retained epoch")
		}
	}
}

func TestDynamicWindowLeadingDiscontinuityPreservesAbsoluteSequenceNumbers(t *testing.T) {
	full := dynamicWindowTestSnapshot()
	data, err := dynamicWindowPlaylist(full, "r0", dynamicPlaybackView{}, dynamicWindowTestResource)
	if err != nil {
		t.Fatal(err)
	}
	_, prior, _, _, _ := dynamicWindowObservePlaylist(t, data)
	trimmed := full
	trimmed.Segments = append([]timeshift.Segment(nil), full.Segments[2:]...)
	trimmed.Epochs = append([]timeshift.Epoch(nil), full.Epochs[1:]...)
	trimmed.EarliestTicks = trimmed.Segments[0].StartTicks
	trimmed.LiveStartTicks = trimmed.EarliestTicks
	trimmed.DiscontinuitySequence = trimmed.Segments[0].DiscontinuitySequence
	trimmed.Ended = true
	if !trimmed.Segments[0].Discontinuity {
		t.Fatal("fixture must begin at the retained reconnect boundary")
	}
	data, err = dynamicWindowPlaylist(trimmed, "r0", dynamicPlaybackView{}, dynamicWindowTestResource)
	if err != nil {
		t.Fatal(err)
	}
	sequences, current, _, _, _ := dynamicWindowObservePlaylist(t, data)
	if !reflect.DeepEqual(current, prior[2:]) || !reflect.DeepEqual(sequences, []uint64{33, 34, 35, 36}) ||
		strings.Count(string(data), "#EXT-X-DISCONTINUITY\n") != 1 || !strings.Contains(string(data), "#EXT-X-DISCONTINUITY-SEQUENCE:8\n") {
		t.Fatal("removing a leading discontinuity double-counted or reset retained segment discontinuity numbers")
	}
}

func TestDynamicViewStartUsesOnlyRetainedPositionsAndSafeLiveStart(t *testing.T) {
	snapshot := dynamicWindowTestSnapshot()
	for _, test := range []struct {
		name string
		view dynamicPlaybackView
		want *int64
		err  error
	}{
		{name: "unspecified"},
		{name: "live", view: dynamicPlaybackView{Live: true}, want: dynamicWindowTestTickPointer(0)},
		{name: "aligned seek", view: dynamicPlaybackView{StartTicks: dynamicWindowTestTickPointer(snapshot.EarliestTicks + 56*media.TicksPerSecond/10)}, want: dynamicWindowTestTickPointer(35 * media.TicksPerSecond / 10)},
		{name: "first", view: dynamicPlaybackView{StartTicks: dynamicWindowTestTickPointer(snapshot.EarliestTicks)}, want: dynamicWindowTestTickPointer(0)},
		{name: "expired", view: dynamicPlaybackView{StartTicks: dynamicWindowTestTickPointer(snapshot.EarliestTicks - 1)}, err: timeshift.ErrWindowExpired},
		{name: "edge", view: dynamicPlaybackView{StartTicks: dynamicWindowTestTickPointer(snapshot.LiveEdgeTicks)}, err: timeshift.ErrNotBuffered},
		{name: "future", view: dynamicPlaybackView{StartTicks: dynamicWindowTestTickPointer(snapshot.LiveEdgeTicks + 1)}, err: timeshift.ErrNotBuffered},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := dynamicViewStart(snapshot, test.view)
			if !errors.Is(err, test.err) || (got == nil) != (test.want == nil) || got != nil && *got != *test.want {
				t.Fatalf("retained view mismatch: %v, %v", got, err)
			}
		})
	}
	empty := timeshift.WindowSnapshot{}
	if _, err := dynamicViewStart(empty, dynamicPlaybackView{Live: true}); !errors.Is(err, timeshift.ErrNotBuffered) {
		t.Fatal("an empty window manufactured a live position")
	}
}

func dynamicWindowTestTickPointer(value int64) *int64 { return &value }

func dynamicWindowTestSession() *dynamicStreamSession {
	plan := transcode.Plan{VideoStreamIndex: 2, AudioStreamIndex: 5, Container: "mp4"}
	plan.HLS.Subtitles.Count = 2
	plan.HLS.Subtitles.Tracks[0] = transcode.HLSSubtitleTrack{StreamIndex: 12, Codec: "subrip"}
	plan.HLS.Subtitles.Tracks[1] = transcode.HLSSubtitleTrack{StreamIndex: 18, Codec: "ass"}
	return &dynamicStreamSession{id: "presentation", key: dynamicStreamKey{liveID: "live-owned", plan: plan},
		scope:        transcode.Scope{UserID: "viewer", AuthSessionID: "auth", DeviceID: "device", PlaySessionID: "play-owned", ItemID: "item", SourceID: "source"},
		subtitleView: playback.HLSSubtitleView{SelectedStreamIndex: 12, SelectionSet: true, OffsetTicks: 2 * media.TicksPerSecond}}
}

func TestDynamicRequestViewKeepsOffOffsetAndSeekExplicitWithoutMutatingSession(t *testing.T) {
	session := dynamicWindowTestSession()
	before := session.subtitleView
	view, err := dynamicRequestView(map[string]string{"subtitlestreamindex": "-1", "subtitleoffsetticks": "-15000000", "starttimeticks": "1056000000"}, session)
	if err != nil || view.Subtitles.SelectedStreamIndex != -1 || !view.Subtitles.SelectionSet || view.Subtitles.OffsetTicks != -15*media.TicksPerSecond/10 || view.StartTicks == nil || *view.StartTicks != 1056000000 || session.subtitleView != before {
		t.Fatal("off, offset or retained position changed the shared session view")
	}
	for _, values := range []map[string]string{
		{"subtitlestreamindex": "99"}, {"subtitlestreamindex": "-2"}, {"subtitleoffsetticks": "864000000001"},
		{"starttimeticks": "-1"}, {"starttimeticks": "9223372036854775808"}, {"live": "yes"}, {"live": "true", "starttimeticks": "0"},
	} {
		if _, err := dynamicRequestView(values, session); err == nil {
			t.Fatalf("invalid view was accepted: %v", values)
		}
	}
	for _, manifest := range []bool{false, true} {
		address := dynamicArtifactURLView(session, "main.m3u8", "owned + & token", view, manifest)
		parsed, err := url.Parse(address)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		if parsed.Host != "" || query.Get("api_key") != "owned + & token" || query.Get("SubtitleStreamIndex") != "-1" || query.Get("SubtitleOffsetTicks") != "-15000000" ||
			query.Get("GobyLiveId") != session.id || query.Get("PlaySessionId") != session.scope.PlaySessionID || query.Get("DeviceId") != session.scope.DeviceID || query.Get("MediaSourceId") != session.scope.SourceID {
			t.Fatal("child URL lost its exact owner or explicit subtitle state")
		}
		if manifest != (query.Get("StartTimeTicks") == "1056000000") {
			t.Fatal("seek view leaked onto an immutable artifact or disappeared from a manifest")
		}
	}
	view.StartTicks = nil
	view.Live = true
	for _, manifest := range []bool{false, true} {
		parsed, err := url.Parse(dynamicArtifactURLView(session, "main.m3u8", "token", view, manifest))
		if err != nil {
			t.Fatal(err)
		}
		if manifest != (parsed.Query().Get("Live") == "true") {
			t.Fatal("live selection was not confined to manifest URLs")
		}
	}
}

func TestDynamicZeroSubtitleViewRoundTripsAsExplicitOff(t *testing.T) {
	session := dynamicWindowTestSession()
	session.key.plan.HLS.Subtitles = transcode.HLSSubtitlePlan{}
	session.subtitleView = playback.HLSSubtitleView{}
	parsed, err := url.Parse(dynamicArtifactURLView(session, "master.m3u8", "token", dynamicPlaybackView{Subtitles: session.subtitleView}, true))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("SubtitleStreamIndex") != "-1" || parsed.Query().Get("SubtitleOffsetTicks") != "0" {
		t.Fatal("a presentation without HLS subtitle tracks advertised track zero instead of Off")
	}
	values := make(map[string]string)
	for name, items := range parsed.Query() {
		values[strings.ToLower(name)] = items[0]
	}
	view, err := dynamicRequestView(values, session)
	if err != nil || view.Subtitles.SelectedStreamIndex != -1 {
		t.Fatal("the ordinary no-subtitle playback URL cannot pass its own view validation")
	}
}

func TestDynamicSnapshotArtifactRejectsOtherWindowsExpiredNamesAndWrongResourceKinds(t *testing.T) {
	snapshot := dynamicWindowTestSnapshot()
	mediaArtifact := snapshot.Segments[0].Artifacts[0]
	initialization := snapshot.Epochs[0].Initializations[0]
	for _, test := range []struct {
		name string
		want timeshift.Artifact
	}{
		{dynamicArtifactName(mediaArtifact.ID, "fmp4", false), mediaArtifact}, {dynamicArtifactName(initialization.ID, "fmp4", true), initialization},
	} {
		got, kind, ok := dynamicSnapshotArtifact(snapshot, test.name)
		if !ok || got != test.want || kind != "video" {
			t.Fatal("a committed owned artifact was not identified exactly")
		}
	}
	for _, name := range []string{dynamicArtifactName(dynamicWindowTestID(999999), "fmp4", false), mediaArtifact.ID + ".mp4", initialization.ID + ".m4s",
		mediaArtifact.ID + ".ts", "../" + dynamicArtifactName(mediaArtifact.ID, "fmp4", false), dynamicArtifactName(mediaArtifact.ID, "fmp4", false) + "?other=true", "init.mp4", "segment-000000.m4s"} {
		if _, _, ok := dynamicSnapshotArtifact(snapshot, name); ok {
			t.Fatalf("an uncommitted or differently typed artifact was exposed: %s", name)
		}
	}
	trimmed := snapshot
	trimmed.Segments = trimmed.Segments[2:]
	trimmed.Epochs = trimmed.Epochs[1:]
	if _, _, ok := dynamicSnapshotArtifact(trimmed, dynamicArtifactName(mediaArtifact.ID, "fmp4", false)); ok {
		t.Fatal("an artifact absent from this committed snapshot leaked into its lookup")
	}
	if _, _, ok := dynamicSnapshotArtifact(trimmed, dynamicArtifactName(initialization.ID, "fmp4", true)); ok {
		t.Fatal("an unrelated epoch initialization leaked into its lookup")
	}
}

func TestDynamicWindowPlaylistRejectsUnavailableResourcesAndMarksOnlyActualEnd(t *testing.T) {
	snapshot := dynamicWindowTestSnapshot()
	if _, err := dynamicWindowPlaylist(snapshot, "foreign", dynamicPlaybackView{}, dynamicWindowTestResource); !errors.Is(err, timeshift.ErrNotFound) {
		t.Fatal("an unknown rendition was rendered")
	}
	empty := snapshot
	empty.Segments, empty.Epochs = nil, nil
	empty.EarliestTicks, empty.LiveStartTicks = empty.LiveEdgeTicks, empty.LiveEdgeTicks
	empty.DiscontinuitySequence = snapshot.Segments[len(snapshot.Segments)-1].DiscontinuitySequence
	if _, err := dynamicWindowPlaylist(empty, "r0", dynamicPlaybackView{}, dynamicWindowTestResource); !errors.Is(err, timeshift.ErrNotBuffered) {
		t.Fatal("an empty live window with a known rendition became a playlist")
	}
	empty.Ended = true
	const terminal = "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:5\n#EXT-X-MEDIA-SEQUENCE:37\n#EXT-X-DISCONTINUITY-SEQUENCE:9\n#EXT-X-ENDLIST\n"
	for _, view := range []dynamicPlaybackView{{}, {Live: true}} {
		resources := 0
		body, err := dynamicWindowPlaylist(empty, "r0", view, func(id, format string, initialization bool) string {
			resources++
			return dynamicWindowTestResource(id, format, initialization)
		})
		if err != nil || string(body) != terminal || resources != 0 {
			t.Fatal("an empty ended window lost its canonical ENDLIST or monotonic next sequence")
		}
	}
	for _, seek := range []struct {
		ticks int64
		want  error
	}{{empty.EarliestTicks - 1, timeshift.ErrWindowExpired}, {empty.LiveEdgeTicks, timeshift.ErrNotBuffered}, {empty.LiveEdgeTicks + 1, timeshift.ErrNotBuffered}} {
		if _, err := dynamicWindowPlaylist(empty, "r0", dynamicPlaybackView{StartTicks: &seek.ticks}, dynamicWindowTestResource); !errors.Is(err, seek.want) {
			t.Fatalf("empty ended window changed explicit seek semantics at %d: %v", seek.ticks, err)
		}
	}
	for _, unsafe := range []string{"https://untrusted.invalid/media", "//untrusted.invalid/media", "../outside.m4s", "/media\n#EXT-X-ENDLIST"} {
		if _, err := dynamicWindowPlaylist(snapshot, "r0", dynamicPlaybackView{}, func(string, string, bool) string { return unsafe }); !errors.Is(err, errInvalidHLSManifest) {
			t.Fatal("an unsafe resource URI entered the playlist")
		}
	}
	snapshot.Stalled = true
	data, err := dynamicWindowPlaylist(snapshot, "r0", dynamicPlaybackView{}, dynamicWindowTestResource)
	if err != nil || strings.Contains(string(data), "#EXT-X-ENDLIST") {
		t.Fatal("a temporarily stalled source was declared ended")
	}
	snapshot.Stalled = false
	snapshot.Ended = true
	data, err = dynamicWindowPlaylist(snapshot, "r0", dynamicPlaybackView{}, dynamicWindowTestResource)
	if err != nil || !strings.HasSuffix(string(data), "#EXT-X-ENDLIST\n") {
		t.Fatal("an actual ended presentation did not terminate its playlist")
	}
}

func TestTimeshiftScopeProjectionPreservesEveryOwnershipDimension(t *testing.T) {
	for _, scope := range []transcode.Scope{
		{UserID: "user", AuthSessionID: "auth", DeviceID: "device", PlaySessionID: "play", ItemID: "item", SourceID: "source"},
		{ApplicationKey: true, ApplicationClientID: "application-client", AuthSessionID: "application-credential", DeviceID: "device", PlaySessionID: "play", ItemID: "item", SourceID: "source"},
	} {
		got := timeshiftScope(scope)
		want := timeshift.Scope{UserID: scope.UserID, AuthSessionID: scope.AuthSessionID, DeviceID: scope.DeviceID, PlaySessionID: scope.PlaySessionID,
			ItemID: scope.ItemID, SourceID: scope.SourceID, ApplicationKey: scope.ApplicationKey, ApplicationClientID: scope.ApplicationClientID}
		if got != want {
			t.Fatal("timeshift scope dropped or invented an ownership dimension")
		}
	}
}

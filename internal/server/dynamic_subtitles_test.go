package server

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/subtitle"
	"github.com/moooyo/goby/internal/timeshift"
	"github.com/moooyo/goby/internal/transcode"
)

func dynamicSubtitleTestRuntime(t *testing.T) *dynamicSubtitleRuntime {
	t.Helper()
	plan := transcode.Plan{}
	plan.HLS.Subtitles.Count = 2
	plan.HLS.Subtitles.Tracks[0] = transcode.HLSSubtitleTrack{StreamIndex: 3, Codec: "subrip"}
	plan.HLS.Subtitles.Tracks[1] = transcode.HLSSubtitleTrack{StreamIndex: 7, Codec: "webvtt"}
	runtime, err := newDynamicSubtitleRuntime(plan)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if _, _, err := runtime.beginEpoch(ctx, 1, media.TicksPerSecond); err != nil {
		t.Fatal(err)
	}
	return runtime
}

func dynamicSubtitleTestDocument(text string, start, end int64) subtitle.Document {
	return subtitle.Document{Format: subtitle.FormatWebVTT, Cues: []subtitle.Cue{{StartTicks: start, EndTicks: end, Text: text}}}
}

func dynamicSubtitleTestSnapshot(sequence uint64, generation uint64, start, duration int64, id string) timeshift.WindowSnapshot {
	return timeshift.WindowSnapshot{PresentationID: "window", Revision: sequence + 1, EarliestTicks: start,
		LiveEdgeTicks: start + duration, LiveStartTicks: start, TargetDurationTicks: duration,
		Segments: []timeshift.Segment{{Sequence: sequence, Generation: generation, StartTicks: start,
			DurationTicks: duration, Artifacts: []timeshift.Artifact{{ID: id, VariantID: "r0"}}}}}
}

func TestDynamicSubtitleRequiresTrackCompletenessAndKeepsCrossingCues(t *testing.T) {
	runtime := dynamicSubtitleTestRuntime(t)
	second := media.TicksPerSecond
	if err := runtime.PublishMedia(1, 0, 0, 2*second, second); err != nil {
		t.Fatal(err)
	}
	if err := runtime.PublishMedia(1, 1, 2*second, 4*second, second); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Append(1, 3, dynamicSubtitleTestDocument("crosses both media segments", second, 3*second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.render(3, 0, 0); !errors.Is(err, subtitle.ErrLiveNotReady) {
		t.Fatal("AV publication or a received cue sealed subtitle completeness")
	}
	if err := runtime.CompleteTrack(1, 3, 4*second); err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []uint64{0, 1} {
		result, _, err := runtime.render(3, sequence, 0)
		if err != nil {
			t.Fatalf("render crossing cue in segment %d: %v", sequence, err)
		}
		parsed, err := subtitle.Parse(result.Data, subtitle.FormatWebVTT)
		if err != nil || len(parsed.Cues) != 1 || parsed.Cues[0].StartTicks != second || parsed.Cues[0].EndTicks != 3*second ||
			parsed.Cues[0].Text != "crosses both media segments" || !strings.Contains(string(result.Data), "\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:90000\n") {
			t.Fatalf("crossing cue or measured clock was lost in segment %d: cues=%+v, error=%v, output=%q", sequence, parsed.Cues, err, result.Data)
		}
	}
	if _, _, err := runtime.render(7, 0, 0); !errors.Is(err, subtitle.ErrLiveNotReady) {
		t.Fatal("one completed track sealed another sparse track")
	}
	if err := runtime.CompleteTrack(1, 7, 4*second); err != nil {
		t.Fatal(err)
	}
	result, _, err := runtime.render(7, 0, 0)
	if err != nil || strings.Contains(string(result.Data), "-->") {
		t.Fatal("explicitly completed sparse input did not produce a valid empty segment")
	}
	if _, _, err := runtime.render(3, 1, -second); !errors.Is(err, subtitle.ErrLiveNotReady) {
		t.Fatal("negative caption delay bypassed future input completeness")
	}
}

func TestDynamicSubtitleMediaClocksPreservePrimingAndRejectRebinding(t *testing.T) {
	runtime := dynamicSubtitleTestRuntime(t)
	second := media.TicksPerSecond
	if err := runtime.PublishMedia(1, 4, -second/10, 19*second/10, second); err != nil {
		t.Fatal(err)
	}
	if err := runtime.CompleteTrack(1, 3, 2*second); err != nil {
		t.Fatal(err)
	}
	_, clock, err := runtime.render(3, 4, 0)
	if err != nil || clock.StartTicks != -second/10 || clock.EndTicks-clock.StartTicks != 2*second {
		t.Fatal("rendering rewrote the observed media interval")
	}
	if err := runtime.PublishMedia(1, 4, 0, 2*second, second); !errors.Is(err, subtitle.ErrLiveConflict) {
		t.Fatal("a published logical sequence accepted different media coordinates")
	}
	if err := runtime.PublishMedia(1, 5, math.MinInt64, 1, 0); !errors.Is(err, subtitle.ErrInvalidRange) {
		t.Fatal("an overflowing source interval was accepted")
	}
	for sequence := uint64(10); len(runtime.media) < dynamicSubtitleClockLimit; sequence++ {
		if err := runtime.PublishMedia(1, sequence, 0, second, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.PublishMedia(1, 99999, 0, second, 0); !errors.Is(err, subtitle.ErrLimitExceeded) {
		t.Fatal("media-clock bookkeeping exceeded its bound")
	}
}

func TestDynamicSubtitleRetentionUsesActualArtifactGraceAndKeepsOldEpoch(t *testing.T) {
	runtime := dynamicSubtitleTestRuntime(t)
	second := media.TicksPerSecond
	if err := runtime.Append(1, 3, dynamicSubtitleTestDocument("old generation", second, 3*second)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.CompleteTrack(1, 3, 4*second); err != nil {
		t.Fatal(err)
	}
	if err := runtime.PublishMedia(1, 0, 0, 2*second, second); err != nil {
		t.Fatal(err)
	}
	old := dynamicSubtitleTestSnapshot(0, 1, 0, 2*second, "retained-old-media")
	if err := runtime.retain(old, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, _, err := runtime.beginEpoch(ctx, 2, second); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Append(2, 3, dynamicSubtitleTestDocument("new generation", 0, second)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.CompleteTrack(2, 3, 2*second); err != nil {
		t.Fatal(err)
	}
	if err := runtime.PublishMedia(2, 1, 0, 2*second, 2*second); err != nil {
		t.Fatal(err)
	}
	current := dynamicSubtitleTestSnapshot(1, 2, 2*second, 2*second, "current-media")
	if err := runtime.retain(current, func(id string) bool { return id == "retained-old-media" }); err != nil {
		t.Fatal(err)
	}
	result, _, err := runtime.render(3, 0, 0)
	if err != nil || !strings.Contains(string(result.Data), "old generation") {
		t.Fatal("rolling the media window prematurely discarded advertised subtitle grace")
	}
	if err := runtime.retain(current, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.render(3, 0, 0); !errors.Is(err, subtitle.ErrLiveWindowExpired) {
		t.Fatal("expired AV grace retained a subtitle-only old window")
	}
	if err := runtime.Append(1, 3, dynamicSubtitleTestDocument("late old generation", 4*second, 5*second)); !errors.Is(err, subtitle.ErrLiveEpoch) {
		t.Fatal("an obsolete reader appended to a new generation")
	}
	runtime.StopEpoch(2)
	result, _, err = runtime.render(3, 1, 0)
	if err != nil || !strings.Contains(string(result.Data), "new generation") {
		t.Fatal("stopping a producer removed previously sealed captions")
	}
	if err := runtime.CompleteTrack(2, 3, 3*second); !errors.Is(err, subtitle.ErrLiveEpoch) {
		t.Fatal("a stopped producer advanced caption completeness")
	}
}

func TestDynamicSubtitleExternalMappingRequiresMeasuredEpochAndPreservesBias(t *testing.T) {
	runtime := dynamicSubtitleTestRuntime(t)
	second := media.TicksPerSecond
	delta, err := runtime.externalDelta(context.Background(), 1, dynamicSubtitleSourceUpdate{Clock: "media", OffsetTicks: second / 2})
	if err != nil || delta != 3*second/2 {
		t.Fatal("external media-clock captions did not use the producer's explicit headroom")
	}
	anchor := subtitle.LiveClockAnchor{Known: true, Generation: 1, MPEGTS: 900_000, SourceTicks: second, MaxDistance90k: 900_000}
	if err := runtime.BindSourceAnchor(1, anchor); err != nil {
		t.Fatal(err)
	}
	update := dynamicSubtitleSourceUpdate{Clock: "mpegts", OffsetTicks: second / 2,
		Batch: subtitle.LiveBatch{Header: subtitle.LiveHeader{TimestampMap: &subtitle.LiveTimestampMap{LocalTicks: 0, MPEGTS: 990_000}}}}
	delta, err = runtime.externalDelta(context.Background(), 1, update)
	if err != nil || delta != 5*second/2 {
		t.Fatal("MPEGTS captions received the source-origin bias twice or lost their declared offset")
	}
	next := anchor
	next.MPEGTS += 90_000
	next.SourceTicks += second
	if err := runtime.BindSourceAnchor(1, next); err != nil {
		t.Fatal(err)
	}
	if err := runtime.BindSourceAnchor(1, anchor); err != nil {
		t.Fatal("an earlier observation of the same affine clock was rejected")
	}
	if runtime.epochs[1].anchor != next {
		t.Fatal("an earlier observation moved the active clock anchor backward")
	}
	shifted := next
	shifted.SourceTicks += second
	if err := runtime.BindSourceAnchor(1, shifted); !errors.Is(err, subtitle.ErrLiveConflict) {
		t.Fatal("a rolling anchor changed the epoch's affine clock")
	}
	if _, err := runtime.externalDelta(context.Background(), 1, dynamicSubtitleSourceUpdate{Clock: "mpegts"}); !errors.Is(err, subtitle.ErrLiveTimestampMap) {
		t.Fatal("an absent source map was treated as zero")
	}
	runtime.CancelEpoch(1)
	if _, err := runtime.externalDelta(context.Background(), 1, update); !errors.Is(err, subtitle.ErrLiveEpoch) {
		t.Fatal("a failed producer kept an external reader's generation alive")
	}
}

func TestDynamicSubtitleCompanionCompletionRequiresOrderedClosedInputs(t *testing.T) {
	runtime := dynamicSubtitleTestRuntime(t)
	second := media.TicksPerSecond
	if err := runtime.PublishMedia(1, 0, 0, 2*second, 0); err != nil {
		t.Fatal(err)
	}
	if err := runtime.completeCaption(1, 3, 1, 2*second, 4*second); !errors.Is(err, subtitle.ErrLiveConflict) {
		t.Fatal("a missing first companion silently sealed its absent input")
	}
	if _, _, err := runtime.render(3, 0, 0); !errors.Is(err, subtitle.ErrLiveNotReady) {
		t.Fatal("an out-of-order companion advanced its watermark")
	}
	if err := runtime.Append(1, 3, dynamicSubtitleTestDocument("long copied cue", second, 5*second)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.completeCaption(1, 3, 0, 0, 2*second); err != nil {
		t.Fatal(err)
	}
	if err := runtime.completeCaption(1, 3, 0, 0, 2*second); err != nil {
		t.Fatal("an exact closed-companion retry was not idempotent")
	}
	if err := runtime.completeCaption(1, 3, 2, 4*second, 6*second); !errors.Is(err, subtitle.ErrLiveConflict) {
		t.Fatal("a companion sequence gap manufactured subtitle completeness")
	}
	if err := runtime.PublishMedia(1, 1, 2*second, 4*second, 0); err != nil {
		t.Fatal(err)
	}
	if err := runtime.completeCaption(1, 3, 1, 2*second, 4*second); err != nil {
		t.Fatal(err)
	}
	result, _, err := runtime.render(3, 1, 0)
	if err != nil {
		t.Fatalf("render an earlier cue after a later empty companion: %v", err)
	}
	parsed, err := subtitle.Parse(result.Data, subtitle.FormatWebVTT)
	if err != nil || len(parsed.Cues) != 1 || parsed.Cues[0].StartTicks != second || parsed.Cues[0].EndTicks != 5*second || parsed.Cues[0].Text != "long copied cue" {
		t.Fatalf("a later empty companion dropped an earlier crossing cue: cues=%+v, error=%v, output=%q", parsed.Cues, err, result.Data)
	}
	if _, _, err := runtime.render(3, 1, -second); !errors.Is(err, subtitle.ErrLiveNotReady) {
		t.Fatal("the last closed interval pretended that finite future captions were already known")
	}
	if err := runtime.completeCaptionSource(1, 3, 3); !errors.Is(err, subtitle.ErrLiveConflict) {
		t.Fatal("a premature EOF marker skipped unfinished companion work")
	}
	if err := runtime.completeCaptionSource(1, 3, 2); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.render(3, 1, -second); err != nil {
		t.Fatal("verified complete input did not release the finite negative-offset tail")
	}
}

func TestDynamicSubtitleExplicitIntervalsDoNotInventMissingPrefixOrGaps(t *testing.T) {
	runtime := dynamicSubtitleTestRuntime(t)
	second := media.TicksPerSecond
	if err := runtime.PublishMedia(1, 0, 0, second, 0); err != nil {
		t.Fatal(err)
	}
	if err := runtime.PublishMedia(1, 1, second, 2*second, 0); err != nil {
		t.Fatal(err)
	}
	if err := runtime.completeInterval(1, 3, second, 2*second, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.render(3, 0, 0); !errors.Is(err, subtitle.ErrLiveWindowExpired) {
		t.Fatal("a closed HLS suffix invented a complete earlier interval")
	}
	if _, _, err := runtime.render(3, 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := runtime.completeInterval(1, 3, 3*second, 4*second, false); !errors.Is(err, subtitle.ErrLiveConflict) {
		t.Fatal("an unobserved subtitle interval gap was silently sealed")
	}
	if err := runtime.completeInterval(1, 3, 2*second, 3*second, true); !errors.Is(err, subtitle.ErrLiveConflict) {
		t.Fatal("an external discontinuity reused the previous media epoch")
	}
	if err := runtime.completeInterval(1, 3, 2*second, 3*second, false); err != nil {
		t.Fatal(err)
	}
	if err := runtime.completeInterval(1, 7, -2*second, -second, false); err != nil {
		t.Fatal("a declared negative source offset lost its prezero interval")
	}
	if _, _, err := runtime.render(7, 0, 0); !errors.Is(err, subtitle.ErrLiveNotReady) {
		t.Fatal("a prezero completion sealed a future positive interval")
	}
	if err := runtime.completeInterval(1, 7, -second, second, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.render(7, 0, 0); err != nil {
		t.Fatal("an explicitly complete interval crossing logical zero was rejected")
	}
}

func TestDynamicSubtitlePlaylistUsesCommittedDurationsAndKeepsOffViewTracks(t *testing.T) {
	second := media.TicksPerSecond
	snapshot := timeshift.WindowSnapshot{EarliestTicks: 10 * second, LiveEdgeTicks: 15 * second, LiveStartTicks: 10 * second, DiscontinuitySequence: 4, TargetDurationTicks: 4 * second,
		Segments: []timeshift.Segment{{Sequence: 20, Generation: 5, StartTicks: 10 * second, DurationTicks: 3 * second / 2, Discontinuity: true},
			{Sequence: 21, Generation: 6, StartTicks: 23 * second / 2, DurationTicks: 7 * second / 2, Discontinuity: true}}}
	view := dynamicPlaybackView{Subtitles: playback.HLSSubtitleView{SelectionSet: true, SelectedStreamIndex: -1, OffsetTicks: -second}}
	body, err := dynamicRenderedSubtitlePlaylist(snapshot, func(sequence uint64) string {
		return "/captions/" + hlsSubtitleSegmentName(0, int64(sequence)) + "?SubtitleStreamIndex=-1"
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, expected := range []string{"#EXT-X-TARGETDURATION:4", "#EXT-X-MEDIA-SEQUENCE:20", "#EXT-X-DISCONTINUITY-SEQUENCE:4", "#EXTINF:1.5000000", "#EXTINF:3.5000000", "subtitles-0-segment-000020.vtt", "subtitles-0-segment-000021.vtt"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("subtitle playlist lost actual media or fixed-slot information: %s", expected)
		}
	}
	if strings.Count(text, "#EXT-X-DISCONTINUITY\n") != 1 || strings.Contains(text, "#EXT-X-ENDLIST") {
		t.Fatal("live subtitle discontinuities or end state differ from committed media")
	}
	if _, err := dynamicRenderedSubtitlePlaylist(snapshot, func(uint64) string { return "invalid\nurl" }, view); !errors.Is(err, errInvalidHLSManifest) {
		t.Fatal("unsafe subtitle resource URL entered a playlist")
	}
	snapshot.Segments = snapshot.Segments[:1]
	body, err = dynamicRenderedSubtitlePlaylist(snapshot, func(uint64) string { return "/one.vtt" }, view)
	if err != nil || !strings.Contains(string(body), "#EXT-X-TARGETDURATION:4") {
		t.Fatal("a rolling subtitle window changed its fixed target duration")
	}
	snapshot.Segments, snapshot.Ended, snapshot.NextSequence = nil, true, 77
	body, err = dynamicRenderedSubtitlePlaylist(snapshot, func(uint64) string { t.Fatal("empty ended playlist asked for a resource"); return "" }, view)
	if err != nil || !strings.Contains(string(body), "#EXT-X-MEDIA-SEQUENCE:77") || !strings.Contains(string(body), "#EXT-X-ENDLIST") {
		t.Fatal("empty terminal subtitles lost the global next sequence or ENDLIST")
	}
}

func TestDynamicSubtitleNegativeOffsetPublishesCompleteLivePrefixWithoutEOF(t *testing.T) {
	runtime := dynamicSubtitleTestRuntime(t)
	second := media.TicksPerSecond
	snapshot := timeshift.WindowSnapshot{TargetDurationTicks: 2 * second, DiscontinuitySequence: 9}
	for sequence := uint64(0); sequence < 5; sequence++ {
		start := int64(sequence) * 2 * second
		if err := runtime.PublishMedia(1, sequence, start, start+2*second, 0); err != nil {
			t.Fatal(err)
		}
		snapshot.Segments = append(snapshot.Segments, timeshift.Segment{Sequence: sequence, Generation: 1, StartTicks: start,
			DurationTicks: 2 * second, DiscontinuitySequence: 9})
	}
	snapshot.LiveEdgeTicks = 10 * second
	if err := runtime.CompleteTrack(1, 3, 8*second); err != nil {
		t.Fatal(err)
	}
	ready, err := runtime.readyWindow(snapshot, 3, -2*second)
	if err != nil || len(ready.Segments) != 3 || ready.LiveEdgeTicks != 6*second || ready.Ended || ready.DiscontinuitySequence != 9 {
		t.Fatal("a complete retained prefix waited for the perpetually moving negative-offset tail")
	}
	if err := runtime.PublishMedia(1, 5, 10*second, 12*second, 0); err != nil {
		t.Fatal(err)
	}
	snapshot.Segments = append(snapshot.Segments, timeshift.Segment{Sequence: 5, Generation: 1, StartTicks: 10 * second, DurationTicks: 2 * second, DiscontinuitySequence: 9})
	snapshot.LiveEdgeTicks = 12 * second
	if err := runtime.CompleteTrack(1, 3, 10*second); err != nil {
		t.Fatal(err)
	}
	ready, err = runtime.readyWindow(snapshot, 3, -2*second)
	if err != nil || len(ready.Segments) != 4 || ready.Segments[3].Sequence != 3 || ready.Ended {
		t.Fatal("continued live input did not advance only the completed subtitle prefix")
	}
}

func TestDynamicSubtitlePositiveOffsetDropsOnlyExpiredLeadingIntervals(t *testing.T) {
	runtime := dynamicSubtitleTestRuntime(t)
	second := media.TicksPerSecond
	snapshot := timeshift.WindowSnapshot{TargetDurationTicks: 2 * second, NextSequence: 7}
	for sequence := uint64(0); sequence < 7; sequence++ {
		start := int64(sequence) * 2 * second
		if err := runtime.PublishMedia(1, sequence, start, start+2*second, 0); err != nil {
			t.Fatal(err)
		}
		snapshot.Segments = append(snapshot.Segments, timeshift.Segment{Sequence: sequence, Generation: 1, StartTicks: start,
			DurationTicks: 2 * second, DiscontinuitySequence: 11, Artifacts: []timeshift.Artifact{{ID: hlsSubtitleSegmentName(0, int64(sequence)), VariantID: "r0"}}})
	}
	if err := runtime.CompleteTrack(1, 3, 14*second); err != nil {
		t.Fatal(err)
	}
	if err := runtime.retain(snapshot, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	snapshot.Segments = snapshot.Segments[2:]
	snapshot.EarliestTicks, snapshot.LiveEdgeTicks = 4*second, 14*second
	if err := runtime.retain(snapshot, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	ready, err := runtime.readyWindow(snapshot, 3, 2*second)
	if err != nil || len(ready.Segments) != 4 || ready.Segments[0].Sequence != 3 || ready.EarliestTicks != 6*second || ready.DiscontinuitySequence != 11 {
		t.Fatal("positive offset either invented pruned captions or lost later complete media intervals")
	}
}

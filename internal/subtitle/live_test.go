package subtitle

import (
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
)

func newLiveJournalForTest(t *testing.T, options LiveJournalOptions, generation uint64, streams ...int) *LiveJournal {
	t.Helper()
	journal, err := NewLiveJournal(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.BeginEpoch(generation, streams); err != nil {
		t.Fatal(err)
	}
	return journal
}

func liveTestDocument(cues ...Cue) Document { return Document{Format: FormatWebVTT, Cues: cues} }

func TestLiveJournalPreservesCrossingCuesAndIndependentViews(t *testing.T) {
	journal := newLiveJournalForTest(t, LiveJournalOptions{}, 1, 3, 1<<20+7)
	long := Cue{StartTicks: TicksPerSecond, EndTicks: 9 * TicksPerSecond, Text: "crossing <00:03.000>caption", Identifier: "long"}
	if err := journal.Append(1, 3, liveTestDocument(long)); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(1, 1<<20+7, liveTestDocument(Cue{StartTicks: 2 * TicksPerSecond, EndTicks: 8 * TicksPerSecond, Text: "other language"})); err != nil {
		t.Fatal(err)
	}
	if err := journal.Advance(1, 12*TicksPerSecond); err != nil {
		t.Fatal(err)
	}
	for _, interval := range [][2]int64{{2, 4}, {4, 6}} {
		result, err := journal.Window(1, 3, interval[0]*TicksPerSecond, interval[1]*TicksPerSecond, 0, TicksPerSecond)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := Parse(result.Data, FormatWebVTT)
		if err != nil || len(parsed.Cues) != 1 || parsed.Cues[0] != long || !strings.Contains(string(result.Data), "MPEGTS:90000") {
			t.Fatalf("cross-segment cue or output clock changed: %+v, %v", parsed.Cues, err)
		}
	}
	delayed, err := journal.Window(1, 3, 4*TicksPerSecond, 6*TicksPerSecond, TicksPerSecond, 0)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(delayed.Data, FormatWebVTT)
	if err != nil || len(parsed.Cues) != 1 || parsed.Cues[0].StartTicks != 2*TicksPerSecond || parsed.Cues[0].EndTicks != 10*TicksPerSecond || !strings.Contains(parsed.Cues[0].Text, "<00:04.000>") {
		t.Fatalf("delayed view changed the stored cue or inline clock: %+v, %v", parsed.Cues, err)
	}
	other, err := journal.Window(1, 1<<20+7, 4*TicksPerSecond, 6*TicksPerSecond, 0, 0)
	if err != nil || !strings.Contains(string(other.Data), "other language") || strings.Contains(string(other.Data), "crossing") {
		t.Fatalf("track selection leaked: %q, %v", other.Data, err)
	}
	off, err := journal.Window(1, -1, 4*TicksPerSecond, 6*TicksPerSecond, 0, 0)
	if err != nil || strings.Contains(string(off.Data), "-->") {
		t.Fatalf("off view retained a cue: %q, %v", off.Data, err)
	}
	if err := journal.Prune(1, 4*TicksPerSecond); err != nil {
		t.Fatal(err)
	}
	retained, err := journal.Window(1, 3, 4*TicksPerSecond, 6*TicksPerSecond, 0, 0)
	if err != nil || !strings.Contains(string(retained.Data), "00:01.000 --> 00:09.000") {
		t.Fatalf("retention clipped a surviving crossing cue: %q, %v", retained.Data, err)
	}
	if _, err := journal.Window(1, 3, 3*TicksPerSecond, 5*TicksPerSecond, 0, 0); !errors.Is(err, ErrLiveWindowExpired) {
		t.Fatalf("expired media interval returned %v", err)
	}
	if _, err := journal.Window(1, 3, 4*TicksPerSecond, 6*TicksPerSecond, 2*TicksPerSecond, 0); !errors.Is(err, ErrLiveWindowExpired) {
		t.Fatalf("offset recovered pruned source data: %v", err)
	}
}

func TestLiveJournalRequiresObservedCompletenessAndRejectsLateChanges(t *testing.T) {
	journal := newLiveJournalForTest(t, LiveJournalOptions{}, 1, 0)
	if _, err := journal.Window(1, 0, 0, TicksPerSecond, 0, 0); !errors.Is(err, ErrLiveNotReady) {
		t.Fatalf("unobserved sparse input became empty output: %v", err)
	}
	if err := journal.Advance(1, 5*TicksPerSecond); err != nil {
		t.Fatal(err)
	}
	empty, err := journal.Window(1, 0, 0, 5*TicksPerSecond, 0, 0)
	if err != nil || strings.Contains(string(empty.Data), "-->") {
		t.Fatalf("observed empty interval failed: %q, %v", empty.Data, err)
	}
	if err := journal.Append(1, 0, liveTestDocument(Cue{StartTicks: 2 * TicksPerSecond, EndTicks: 6 * TicksPerSecond, Text: "late"})); !errors.Is(err, ErrLiveConflict) {
		t.Fatalf("sealed empty output accepted a late change: %v", err)
	}
	cue := Cue{StartTicks: 5 * TicksPerSecond, EndTicks: 8 * TicksPerSecond, Text: "known next cue"}
	if err := journal.Append(1, 0, liveTestDocument(cue)); err != nil {
		t.Fatal(err)
	}
	if err := journal.Advance(1, 6*TicksPerSecond); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(1, 0, liveTestDocument(cue)); err != nil {
		t.Fatalf("sealed exact retry failed: %v", err)
	}
	if _, err := journal.Window(1, 0, 4*TicksPerSecond, 5*TicksPerSecond, -2*TicksPerSecond, 0); !errors.Is(err, ErrLiveNotReady) {
		t.Fatalf("negative offset bypassed future source completeness: %v", err)
	}
	if _, err := journal.Window(1, -1, 4*TicksPerSecond, 5*TicksPerSecond, -2*TicksPerSecond, 0); err != nil {
		t.Fatalf("off unnecessarily required future subtitles: %v", err)
	}
	if err := journal.Advance(1, 5*TicksPerSecond); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("watermark moved backward: %v", err)
	}
	if err := journal.Prune(1, 7*TicksPerSecond); !errors.Is(err, ErrLiveNotReady) {
		t.Fatalf("retention invented unobserved progress: %v", err)
	}
}

func TestLiveJournalAppendIsAtomicAndRepeatedHLSCuesAreBounded(t *testing.T) {
	journal := newLiveJournalForTest(t, LiveJournalOptions{MaxCues: 2}, 1, 4)
	first := Cue{StartTicks: TicksPerSecond, EndTicks: 4 * TicksPerSecond, Identifier: "same", Text: "original"}
	if err := journal.Append(1, 4, liveTestDocument(first, first)); err != nil {
		t.Fatal(err)
	}
	initialBytes := journal.bytes
	for range 30 {
		if err := journal.Append(1, 4, liveTestDocument(first)); err != nil {
			t.Fatal(err)
		}
	}
	if journal.bytes != initialBytes || journal.cues != 1 {
		t.Fatal("repeated full cues grew journal retention")
	}
	second := Cue{StartTicks: 4 * TicksPerSecond, EndTicks: 5 * TicksPerSecond, Text: "must not partially commit"}
	changed := first
	changed.Text = "conflicting replacement"
	if err := journal.Append(1, 4, liveTestDocument(second, changed)); !errors.Is(err, ErrLiveConflict) {
		t.Fatalf("conflicting retry returned %v", err)
	}
	if journal.bytes != initialBytes || journal.cues != 1 {
		t.Fatal("failed append partially committed")
	}
	third := Cue{StartTicks: 5 * TicksPerSecond, EndTicks: 6 * TicksPerSecond, Text: "over limit"}
	if err := journal.Append(1, 4, liveTestDocument(second, third)); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("cue quota returned %v", err)
	}
	if journal.cues != 1 {
		t.Fatal("quota rejection partially committed")
	}
	if err := journal.Append(1, 4, liveTestDocument(second)); err != nil {
		t.Fatal(err)
	}
	if err := journal.Advance(1, 10*TicksPerSecond); err != nil {
		t.Fatal(err)
	}
	if err := journal.Prune(1, 4*TicksPerSecond); err != nil {
		t.Fatal(err)
	}
	if journal.cues != 1 || journal.bytes >= initialBytes+liveCueBytes(second) {
		t.Fatal("expired cues did not release retained charge")
	}
	if err := journal.Append(1, 4, liveTestDocument(first)); err != nil || journal.cues != 1 {
		t.Fatalf("expired repetition recreated history: %v", err)
	}
}

func TestLiveJournalGenerationAndByteBounds(t *testing.T) {
	journal := newLiveJournalForTest(t, LiveJournalOptions{MaxEpochs: 1, MaxBytes: 2048}, 1, 0)
	if err := journal.BeginEpoch(1, []int{0}); err != nil {
		t.Fatal(err)
	}
	if err := journal.BeginEpoch(1, []int{1}); !errors.Is(err, ErrLiveConflict) {
		t.Fatalf("generation changed track set: %v", err)
	}
	if err := journal.BeginEpoch(2, []int{0}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("generation limit returned %v", err)
	}
	large := liveTestDocument(Cue{StartTicks: 0, EndTicks: TicksPerSecond, Text: strings.Repeat("x", 2048)})
	if err := journal.Append(1, 0, large); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("byte quota returned %v", err)
	}
	if journal.cues != 0 {
		t.Fatal("byte quota installed oversized state")
	}
	if err := journal.Prune(2, 0); err != nil {
		t.Fatal(err)
	}
	if journal.bytes != 0 || len(journal.epochs) != 0 {
		t.Fatal("pruned generation retained its state")
	}
	if err := journal.BeginEpoch(2, []int{1<<31 - 1}); err != nil {
		t.Fatal(err)
	}
	if err := journal.BeginEpoch(1, []int{0}); !errors.Is(err, ErrLiveEpoch) {
		t.Fatalf("retired generation was resurrected: %v", err)
	}
	if _, err := journal.Window(1, 0, 0, 1, 0, 0); !errors.Is(err, ErrLiveWindowExpired) {
		t.Fatalf("retired generation returned %v", err)
	}
	if err := journal.Prune(1, 0); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("retention moved backward: %v", err)
	}
}

func TestLiveJournalHeaderMappingAndMetadataAreExplicit(t *testing.T) {
	document, err := Parse([]byte("WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:90000\n\nSTYLE\n::cue { color: lime; }\n\n00:01.000 --> 00:04.000\nText <00:02.000>continues\n"), FormatWebVTT)
	if err != nil {
		t.Fatal(err)
	}
	journal := newLiveJournalForTest(t, LiveJournalOptions{}, 7, 0)
	if err := journal.Append(7, 0, document); !errors.Is(err, ErrLiveTimestampMap) {
		t.Fatalf("unmapped transport clock was silently discarded: %v", err)
	}
	mapped, err := MapLiveDocument(document, TicksPerSecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(7, 0, mapped); err != nil {
		t.Fatal(err)
	}
	mapped.Cues[0].Text = "caller mutation"
	mapped.header[0] = "WEBVTT changed by caller"
	if err := journal.Advance(7, 6*TicksPerSecond); err != nil {
		t.Fatal(err)
	}
	result, err := journal.Window(7, 0, 2*TicksPerSecond, 4*TicksPerSecond, 0, 2*TicksPerSecond)
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"STYLE\n::cue { color: lime; }", "00:02.000 --> 00:05.000", "Text <00:03.000>continues", "MPEGTS:180000"} {
		if !strings.Contains(string(result.Data), wanted) {
			t.Fatalf("missing detached metadata/clock %q: %s", wanted, result.Data)
		}
	}
	if strings.Contains(string(result.Data), "caller mutation") || strings.Count(string(result.Data), "X-TIMESTAMP-MAP") != 1 {
		t.Fatal("caller or source mapping mutated the journal view")
	}
}

func TestLiveJournalConcurrentReadAndIdempotentAppend(t *testing.T) {
	journal := newLiveJournalForTest(t, LiveJournalOptions{}, 1, 0)
	document := liveTestDocument(Cue{StartTicks: 0, EndTicks: 10 * TicksPerSecond, Text: "stable"})
	if err := journal.Append(1, 0, document); err != nil {
		t.Fatal(err)
	}
	if err := journal.Advance(1, 10*TicksPerSecond); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 12 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 20 {
				if err := journal.Append(1, 0, document); err != nil {
					t.Error(err)
					return
				}
				result, err := journal.Window(1, 0, TicksPerSecond, 2*TicksPerSecond, 0, 0)
				if err != nil || !strings.Contains(string(result.Data), "stable") {
					t.Errorf("concurrent snapshot failed: %v", err)
					return
				}
			}
		}()
	}
	workers.Wait()
	if journal.cues != 1 {
		t.Fatal("concurrent retries duplicated retained cues")
	}
}

func TestLiveClockRequiresMatchingEpochAnchorAcrossMPEGTSWrap(t *testing.T) {
	const period = int64(1 << 33)
	anchor := LiveClockAnchor{Known: true, Generation: 2, MPEGTS: period + 45_000, SourceTicks: 2 * TicksPerSecond, MaxDistance90k: 180_000}
	for _, test := range []struct {
		mapping LiveTimestampMap
		want    int64
	}{
		{LiveTimestampMap{LocalTicks: TicksPerSecond, MPEGTS: 90_000}, 15 * TicksPerSecond / 10},
		{LiveTimestampMap{MPEGTS: uint64(period - 45_000)}, TicksPerSecond},
		{LiveTimestampMap{MPEGTS: 45_001}, 2*TicksPerSecond + 111},
	} {
		got, err := ResolveLiveTimestampMap(2, test.mapping, anchor)
		if err != nil || got != test.want {
			t.Fatalf("mapping %+v = %d, %v; want %d", test.mapping, got, err, test.want)
		}
	}
	badAnchors := []LiveClockAnchor{anchor, anchor, anchor, anchor}
	badAnchors[0].Known = false
	badAnchors[1].Generation = 1
	badAnchors[2].MaxDistance90k = 0
	badAnchors[3].MaxDistance90k = period / 2
	for _, invalid := range badAnchors {
		if _, err := ResolveLiveTimestampMap(2, LiveTimestampMap{MPEGTS: 45_000}, invalid); !errors.Is(err, ErrLiveTimestampMap) {
			t.Fatalf("unverified anchor accepted: %+v, %v", invalid, err)
		}
	}
	for _, pts := range []uint64{uint64(period), uint64(45_000 + period/2), 900_000} {
		if _, err := ResolveLiveTimestampMap(2, LiveTimestampMap{MPEGTS: pts}, anchor); !errors.Is(err, ErrLiveTimestampMap) {
			t.Fatalf("invalid or distant mapping accepted: %d, %v", pts, err)
		}
	}
	zeroAnchor := LiveClockAnchor{Known: true, Generation: 1, MaxDistance90k: 10}
	if _, err := ResolveLiveTimestampMap(1, LiveTimestampMap{MPEGTS: uint64(period - 1)}, zeroAnchor); !errors.Is(err, ErrLiveTimestampMap) {
		t.Fatalf("mapping invented a negative transport epoch: %v", err)
	}
}

func TestMapLiveDocumentRejectsClockOverflowAndPreservesZeroCrossing(t *testing.T) {
	document := liveTestDocument(Cue{StartTicks: TicksPerSecond, EndTicks: 4 * TicksPerSecond, Text: "before <00:02.000>after"})
	mapped, err := MapLiveDocument(document, -2*TicksPerSecond)
	if err != nil || len(mapped.Cues) != 1 || mapped.Cues[0].StartTicks != 0 || mapped.Cues[0].EndTicks != 2*TicksPerSecond || strings.Contains(mapped.Cues[0].Text, "<00:") {
		t.Fatalf("zero crossing = %+v, %v", mapped.Cues, err)
	}
	if _, err := MapLiveDocument(document, math.MaxInt64); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("mapping clock overflow returned %v", err)
	}
	journal := newLiveJournalForTest(t, LiveJournalOptions{}, 1, 0)
	if err := journal.Advance(1, math.MaxInt64); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Window(1, 0, math.MaxInt64-2, math.MaxInt64, -1, 0); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("window clock overflow returned %v", err)
	}
}

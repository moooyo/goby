package introdetect

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"testing"
)

func clockIntersectionFixture() (map[int]Interval, map[int]int64, map[int]Interval) {
	second := TicksPerSecond
	caps := map[int]Interval{2: {second, 41 * second}, 5: {6 * second, 34 * second}, 7: {13 * second, 42 * second}}
	clocks := map[int]int64{2: -3 * second, 5: 4 * second, 7: 10 * second}
	want := map[int]Interval{2: {second, 27 * second}, 5: {8 * second, 34 * second}, 7: {14 * second, 40 * second}}
	return caps, clocks, want
}

func TestClockIntersectionPreservesDirectionOrderingAndCoordinateTranslations(t *testing.T) {
	for _, translation := range []struct {
		name             string
		capShift, clocks int64
	}{
		{"negative_source_clock", 0, 0},
		{"shift_source_clocks_and_caps", 20 * TicksPerSecond, 20 * TicksPerSecond},
		{"negative_reference_gauge", 0, 20 * TicksPerSecond},
	} {
		t.Run(translation.name, func(t *testing.T) {
			for _, selected := range [][]int{{7, 2, 5}, {2, 5, 7}, {5, 7, 2}} {
				caps, clocks, want := clockIntersectionFixture()
				for source := range caps {
					cap := caps[source]
					caps[source] = Interval{cap.StartTicks + translation.capShift, cap.EndTicks + translation.capShift}
					clocks[source] += translation.clocks
					interval := want[source]
					want[source] = Interval{interval.StartTicks + translation.capShift, interval.EndTicks + translation.capShift}
				}
				beforeCaps, beforeClocks, beforeSelected := maps.Clone(caps), maps.Clone(clocks), slices.Clone(selected)
				budget := audioEvidenceBudget()
				got, err := intersectClockRanges(selected, caps, clocks, DefaultOptions(), budget)
				if err != nil || !reflect.DeepEqual(got, want) || budget.used != 2*int64(len(selected)) {
					t.Fatalf("common interval lost its clock direction or translation: %#v, %v, used %d", got, err, budget.used)
				}
				if !reflect.DeepEqual(caps, beforeCaps) || !reflect.DeepEqual(clocks, beforeClocks) || !slices.Equal(selected, beforeSelected) {
					t.Fatal("intersection mutated its immutable caps, clock map, or member order")
				}
				got[2] = Interval{}
				if !reflect.DeepEqual(caps, beforeCaps) {
					t.Fatal("returned intervals alias the caller's source caps")
				}
			}
		})
	}
}

func TestClockIntersectionRejectsOnlyEmptyOrTooShortCommonPeriods(t *testing.T) {
	o := DefaultOptions()
	for _, example := range []struct {
		name    string
		cap     Interval
		present bool
	}{
		{"exact_minimum", Interval{6 * TicksPerSecond, 23 * TicksPerSecond}, true},
		{"one_tick_short", Interval{6 * TicksPerSecond, 23*TicksPerSecond - 1}, false},
		{"touching_reference_caps", Interval{36 * TicksPerSecond, 64 * TicksPerSecond}, false},
		{"disjoint_reference_caps", Interval{37 * TicksPerSecond, 65 * TicksPerSecond}, false},
	} {
		t.Run(example.name, func(t *testing.T) {
			caps, clocks, _ := clockIntersectionFixture()
			caps[5] = example.cap
			got, err := intersectClockRanges([]int{2, 5, 7}, caps, clocks, o, audioEvidenceBudget())
			if err != nil || (got != nil) != example.present {
				t.Fatalf("empty/short reference period was padded or rejected as an input error: %#v, %v", got, err)
			}
			if example.present {
				want := map[int]Interval{
					2: {TicksPerSecond, 16 * TicksPerSecond},
					5: {8 * TicksPerSecond, 23 * TicksPerSecond},
					7: {14 * TicksPerSecond, 29 * TicksPerSecond},
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("exact minimum reference interval was rounded or extended: %#v", got)
				}
			}
		})
	}
}

func TestClockIntersectionRejectsMalformedWitnessesWithoutPartialRanges(t *testing.T) {
	for _, kind := range []string{"empty_members", "duplicate_member", "negative_member", "missing_cap", "wrong_cap_key", "extra_cap", "missing_clock", "wrong_clock_key", "negative_cap", "reversed_cap", "zero_cap"} {
		t.Run(kind, func(t *testing.T) {
			caps, clocks, _ := clockIntersectionFixture()
			selected := []int{2, 5, 7}
			switch kind {
			case "empty_members":
				selected = nil
			case "duplicate_member":
				selected = []int{2, 2, 7}
			case "negative_member":
				selected[0] = -2
				caps[-2], clocks[-2] = caps[2], clocks[2]
				delete(caps, 2)
				delete(clocks, 2)
			case "missing_cap":
				delete(caps, 5)
			case "wrong_cap_key":
				caps[6] = caps[5]
				delete(caps, 5)
			case "extra_cap":
				caps[6] = caps[5]
			case "missing_clock":
				delete(clocks, 5)
			case "wrong_clock_key":
				clocks[6] = clocks[5]
				delete(clocks, 5)
			case "negative_cap":
				caps[5] = Interval{-1, 34 * TicksPerSecond}
			case "reversed_cap":
				caps[5] = Interval{34 * TicksPerSecond, 6 * TicksPerSecond}
			case "zero_cap":
				caps[5] = Interval{6 * TicksPerSecond, 6 * TicksPerSecond}
			}
			got, err := intersectClockRanges(selected, caps, clocks, DefaultOptions(), audioEvidenceBudget())
			if got != nil || !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("malformed clock witness returned evidence or hid an error: %#v, %v", got, err)
			}
		})
	}
}

func TestClockIntersectionChecksSignedArithmeticWithoutClampingReferenceTime(t *testing.T) {
	for _, example := range []struct {
		name  string
		cap   Interval
		clock int64
		valid bool
	}{
		{"negative_reference_near_limit", Interval{0, 20 * TicksPerSecond}, 1<<63 - 1, true},
		{"source_end_near_limit", Interval{1<<63 - 1 - 20*TicksPerSecond, 1<<63 - 1}, 1<<63 - 1, true},
		{"reference_end_overflow", Interval{0, 20 * TicksPerSecond}, -1<<63 + 1, false},
		{"reference_start_overflow", Interval{1, 20 * TicksPerSecond}, -1 << 63, false},
		{"positive_end_overflow", Interval{1<<63 - 1 - 20*TicksPerSecond, 1<<63 - 1}, -1, false},
	} {
		t.Run(example.name, func(t *testing.T) {
			caps := map[int]Interval{0: example.cap}
			got, err := intersectClockRanges([]int{0}, caps, map[int]int64{0: example.clock}, DefaultOptions(), audioEvidenceBudget())
			if example.valid {
				if err != nil || !reflect.DeepEqual(got, caps) {
					t.Fatalf("representable translated interval was clamped or lost: %#v, %v", got, err)
				}
			} else if got != nil || !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("overflow returned a partial or wrapped interval: %#v, %v", got, err)
			}
		})
	}
}

func TestClockIntersectionChargesBothPassesAndCancellationBeforePublication(t *testing.T) {
	caps, clocks, want := clockIntersectionFixture()
	selected := []int{2, 5, 7}
	for _, limit := range []int64{0, 2, 3, 5, 6} {
		budget := &workBudget{ctx: context.Background(), limit: limit}
		got, err := intersectClockRanges(selected, caps, clocks, DefaultOptions(), budget)
		if limit == 6 {
			if err != nil || !reflect.DeepEqual(got, want) || budget.used != 6 {
				t.Fatalf("two complete linear passes did not fit their exact budget: %#v, %v", got, err)
			}
		} else if got != nil || !errors.Is(err, ErrLimit) {
			t.Fatalf("budget %d exposed a partial common interval: %#v, %v", limit, got, err)
		}
	}
	for _, after := range []int{1, 2, 3} {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		controlled := &cancelDuringContext{Context: ctx, cancel: cancel, after: after}
		got, err := intersectClockRanges(selected, caps, clocks, DefaultOptions(), &workBudget{ctx: controlled, limit: DefaultOptions().MaxComparisons})
		if got != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation at check %d published geometry: %#v, %v", after, got, err)
		}
	}
	shortCaps := maps.Clone(caps)
	shortCaps[5] = Interval{6 * TicksPerSecond, 23*TicksPerSecond - 1}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	controlled := &cancelDuringContext{Context: ctx, cancel: cancel, after: 2}
	got, err := intersectClockRanges(selected, shortCaps, clocks, DefaultOptions(), &workBudget{ctx: controlled, limit: DefaultOptions().MaxComparisons})
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("short-period absence hid cancellation: %#v, %v", got, err)
	}
}

func TestGroupProjectionMeasuresOneSharedClockPeriodInsideAllSourceCaps(t *testing.T) {
	episodes, edges := v2ProjectionFixture(t)
	second := TicksPerSecond
	caps := map[int]Interval{0: {3 * second, 36 * second}, 1: {4 * second, 37 * second}, 2: {35 * second / 10, 36 * second}}
	before := maps.Clone(caps)
	group, err := projectGroup(episodes, []int{2, 0, 1}, caps, edges, DefaultOptions(), audioEvidenceBudget())
	if err != nil || group == nil || group.Status != Qualified || group.Metrics.PairCount != 3 ||
		group.Metrics.AudioSamples != 128 || group.Metrics.AudioAgreementPermille != 1000 || group.Metrics.VisualMatchedTimePermille != 1000 {
		t.Fatalf("the group did not measure the actual common 32-second window: %#v, %v", group, err)
	}
	requireClockProjectionRanges(t, group, episodes, map[int]Interval{0: {4 * second, 36 * second}, 1: {4 * second, 36 * second}, 2: {4 * second, 36 * second}})
	if !reflect.DeepEqual(caps, before) {
		t.Fatal("common-window projection rewrote the discovery caps")
	}
	caps[2] = Interval{35 * second / 10, 25 * second}
	short, err := projectGroup(episodes, []int{0, 1, 2}, caps, edges, DefaultOptions(), audioEvidenceBudget())
	if err != nil || short == nil || short.Status != Review || !slices.Contains(short.Reasons, ShortInterval) {
		t.Fatalf("a measurable period below the automatic minimum was not retained for review: %#v, %v", short, err)
	}
	requireClockProjectionRanges(t, short, episodes, map[int]Interval{0: {4 * second, 25 * second}, 1: {4 * second, 25 * second}, 2: {4 * second, 25 * second}})
	caps[2] = Interval{35 * second / 10, 18 * second}
	missing, err := projectGroup(episodes, []int{0, 1, 2}, caps, edges, DefaultOptions(), audioEvidenceBudget())
	if err != nil || missing != nil {
		t.Fatalf("a shared period below the hard minimum produced partial evidence: %#v, %v", missing, err)
	}
}

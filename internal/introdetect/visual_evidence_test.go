package introdetect

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func v2TestBudget() *workBudget {
	return &workBudget{ctx: context.Background(), limit: DefaultOptions().MaxComparisons}
}

func TestVisualTimeBandsRetainEdgeFragmentsWithoutInventingAFullBand(t *testing.T) {
	o := DefaultOptions()
	value, err := measureVisualTimeline(Interval{2 * TicksPerSecond, 12 * TicksPerSecond},
		[]visualSpan{{Interval{2 * TicksPerSecond, 115 * TicksPerSecond / 10}, 1}}, o, v2TestBudget())
	if err != nil || value.minimumBand != 1000 || value.matched != 950 || value.unobservable != 50 ||
		value.endGap != TicksPerSecond/2 || !value.allBandsAnchored {
		t.Fatalf("edge fragment was dropped from the denominator or imposed an impossible full-band anchor: %#v, %v", value, err)
	}
	short, err := measureVisualTimeline(Interval{2 * TicksPerSecond, 4 * TicksPerSecond},
		[]visualSpan{{Interval{2 * TicksPerSecond, 4 * TicksPerSecond}, 1}}, o, v2TestBudget())
	if err != nil || short.minimumBand != 0 || short.allBandsAnchored {
		t.Fatalf("an absent complete band was reported as confirmed: %#v, %v", short, err)
	}
}

func TestCompleteVisualBandCannotBorrowOnlyTheTipOfAnOutsideAnchor(t *testing.T) {
	second := TicksPerSecond
	spans := []visualSpan{{Interval{41 * second / 10, 51 * second / 10}, 1}}
	for _, start := range []int64{55, 65, 75, 85, 95} {
		spans = append(spans, visualSpan{Interval{start * second / 10, (start + 5) * second / 10}, 1})
	}
	value, err := measureVisualTimeline(Interval{4 * second, 10 * second}, spans, DefaultOptions(), v2TestBudget())
	if err != nil || value.minimumBand != 520 || value.allBandsAnchored {
		t.Fatalf("a 0.1-second intersection plus separated flashes became a full-band one-second anchor: %#v, %v", value, err)
	}
}

func TestVisualStatesWithinContinuousMatchedAnchorsNeedNotRemainStationary(t *testing.T) {
	states := []uint64{0x0000ffff0000ffff, 0xffff0000ffff0000, 0xff00ff00ff00ff00, 0x00ff00ff00ff00ff}
	a := Episode{DurationTicks: 30 * TicksPerSecond}
	for i := 0; i <= 40; i++ {
		state := min(i/12, 2)
		if i == 4 || i == 5 || i == 16 || i == 17 || i == 28 || i == 29 {
			state = 3
		}
		a.Visual = append(a.Visual, VisualSample{int64(i) * TicksPerSecond / 2, states[state], 200})
	}
	b := a
	evidence, err := measureVisualV2(a, b, audioMatch{a: Interval{0, 20 * TicksPerSecond}, b: Interval{0, 20 * TicksPerSecond}}, 0, DefaultOptions(), v2TestBudget())
	if err != nil || evidence.metrics.VisualDistinctStates != 4 || evidence.metrics.VisualMatchedTimePermille != 1000 || evidence.metrics.VisualAnchorCount != 1 {
		t.Fatalf("a genuinely matched state inside a continuous anchor was incorrectly required to remain stationary: %#v, %v", evidence.metrics, err)
	}
}

func TestVisualDominanceCountsTransitionObservationsConsistently(t *testing.T) {
	states := []uint64{0x0000ffff0000ffff, 0xffff0000ffff0000, 0xff00ff00ff00ff00, 0x00ff00ff00ff00ff}
	a := Episode{DurationTicks: 30 * TicksPerSecond}
	for i := 0; i <= 24; i++ {
		state := 0
		if i%6 >= 4 {
			state = 1 + i/6%3
		}
		a.Visual = append(a.Visual, VisualSample{int64(i) * TicksPerSecond, states[state], 200})
	}
	evidence, err := measureVisualV2(a, a, audioMatch{a: Interval{0, 24 * TicksPerSecond}, b: Interval{0, 24 * TicksPerSecond}}, 0, DefaultOptions(), v2TestBudget())
	if err != nil || evidence.metrics.VisualDistinctStates != 4 || evidence.metrics.VisualDominantStatePermille != 680 {
		t.Fatalf("short state changes diluted the 17-of-25 dominant observation count: %#v, %v", evidence.metrics, err)
	}
}

func TestVisualAlignmentChargesSkippedSourcePrefix(t *testing.T) {
	a := testEpisode(1, 200*TicksPerSecond, testOpening{100 * TicksPerSecond, 50 * TicksPerSecond, 778899})
	b := testEpisode(2, 200*TicksPerSecond, testOpening{100 * TicksPerSecond, 50 * TicksPerSecond, 778899})
	budget := &workBudget{ctx: context.Background(), limit: 10}
	match, _, err := visualConfirm(a, b, audioMatch{a: Interval{100 * TicksPerSecond, 145 * TicksPerSecond}, b: Interval{100 * TicksPerSecond, 145 * TicksPerSecond}}, 0, DefaultOptions(), budget)
	if match != nil || !errors.Is(err, ErrLimit) {
		t.Fatalf("walking a long target prefix escaped the work budget: %#v, %v", match, err)
	}
}

func v2ProjectionFixture(t *testing.T) ([]Episode, map[[2]int]pairMatch) {
	t.Helper()
	states := []uint64{0x0000ffff0000ffff, 0xffff0000ffff0000, 0xff00ff00ff00ff00, 0x00ff00ff00ff00ff, 0xaaaaaaaaaaaaaaaa}
	var episodes []Episode
	for i := 1; i <= 3; i++ {
		e := testEpisode(i, 100*TicksPerSecond, testOpening{0, 50 * TicksPerSecond, 778899})
		for j := range e.Visual {
			if e.Visual[j].Ticks < 50*TicksPerSecond {
				e.Visual[j].Hash = states[e.Visual[j].Ticks/(10*TicksPerSecond)]
			}
		}
		episodes = append(episodes, e)
	}
	edges := make(map[[2]int]pairMatch)
	o := DefaultOptions()
	budget := v2TestBudget()
	for left := 0; left < 3; left++ {
		for right := left + 1; right < 3; right++ {
			runs, _, err := alignedAudio(episodes[left], episodes[right], 0, o, budget)
			if err != nil || len(runs) != 1 {
				t.Fatalf("full acoustic fixture: %#v, %v", runs, err)
			}
			match, reason, err := visualConfirm(episodes[left], episodes[right], runs[0], 0, o, budget)
			if err != nil || match == nil || len(match.reasons) != 0 || !qualifiedEvidence(match.metrics, o) {
				t.Fatalf("full visual fixture: %#v, %s, %v", match, reason, err)
			}
			match.left, match.right = left, right
			edges[[2]int{left, right}] = *match
		}
	}
	return episodes, edges
}

func TestFinalGroupProjectionRechecksStatesWithoutReapplyingTheAudioGuard(t *testing.T) {
	episodes, edges := v2ProjectionFixture(t)
	initial := edges[[2]int{0, 1}].a
	ranges := map[int]Interval{0: initial, 1: initial, 2: initial}
	group, err := projectGroup(episodes, []int{0, 1, 2}, ranges, edges, DefaultOptions(), v2TestBudget())
	if err != nil || group == nil || group.Status != Qualified {
		t.Fatalf("unchanged complete witness failed projection: %#v, %v", group, err)
	}
	for _, member := range group.Members {
		if member.Interval != initial {
			t.Fatalf("final projection applied the extraction guard again: %#v, want %#v", member.Interval, initial)
		}
	}
	for source := range ranges {
		ranges[source] = Interval{22 * TicksPerSecond, 38 * TicksPerSecond}
	}
	cropped, err := projectGroup(episodes, []int{0, 1, 2}, ranges, edges, DefaultOptions(), v2TestBudget())
	if err != nil || cropped == nil || cropped.Status != Review || cropped.Metrics.VisualDistinctStates != 2 || !slices.Contains(cropped.Reasons, LowVisualDiversity) {
		t.Fatalf("cropping did not retain an honest two-state review after losing the original diversity: %#v, %v", cropped, err)
	}
}

func TestVisualCorroborationRejectsConcentratedGapsDespiteStrongGlobalCoverage(t *testing.T) {
	for _, test := range []struct {
		name             string
		first, last      int64
		wantGap          int64
		wantBandPermille int
	}{
		{"six_second_gap_across_two_confirmed_bands", 8, 12, 6, 400},
		{"sparse_band_with_a_real_one_second_anchor", 11, 13, 4, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			episodes, edges := v2ProjectionFixture(t)
			for index := range episodes[2].Visual {
				frame := &episodes[2].Visual[index]
				if frame.Ticks >= test.first*TicksPerSecond && frame.Ticks <= test.last*TicksPerSecond {
					frame.Hash = ^frame.Hash
				}
			}
			o := DefaultOptions()
			audio := edges[[2]int{0, 2}].audio
			evidence, err := measureVisualV2(episodes[0], episodes[2], audio, 0, o, v2TestBudget())
			if err != nil || !evidence.allBandsAnchored || evidence.metrics.VisualMatchedTimePermille < o.MinVisualAgreement ||
				evidence.metrics.VisualMaxUnconfirmedGapTicks != test.wantGap*TicksPerSecond ||
				evidence.metrics.VisualMinBandMatchedPermille != test.wantBandPermille {
				t.Fatalf("the fixture lost its strong global coverage or actual distributed anchors: %#v, %v", evidence, err)
			}
			match, reason, err := visualConfirm(episodes[0], episodes[2], audio, 0, o, v2TestBudget())
			if err != nil || match == nil || reason != "" || !slices.Equal(match.reasons, []Reason{InsufficientVisualAnchors}) ||
				match.metrics.VisualAgreementPermille < o.MinVisualAgreement || match.metrics.VisualSimilarityPermille < o.MinVisualSimilarity ||
				qualifiedEvidence(match.metrics, o) {
				t.Fatalf("strong aggregate evidence hid a concentrated unconfirmed region: %#v, %s, %v", match, reason, err)
			}
		})
	}
}

func TestFixedPairRangeClassesSurviveGroupDeduplication(t *testing.T) {
	o := DefaultOptions()
	values := []pairMatch{hypothesisPhasePair(0, 950), hypothesisPhasePair(0, 990), hypothesisPhasePair(0, 970)}
	for index := range values {
		shift := int64(2*index) * TicksPerSecond
		values[index].a.StartTicks += shift
		values[index].a.EndTicks += shift
		values[index].b = values[index].a
	}
	winners, err := distinctPairHypotheses(values, o, v2TestBudget())
	if err != nil || len(winners) != 2 {
		t.Fatalf("immutable pair classes: %#v, %v", winners, err)
	}
	var groups []Group
	for _, winner := range winners {
		group := hypothesisTestGroup()
		for index := range group.Members {
			group.Members[index].Interval = winner.a
		}
		pair := [2]string{group.Members[0].SourceKey, group.Members[1].SourceKey}
		group.alignmentOffsets = []int64{0, 0, 0}
		group.phaseAnchors = map[[2]string]int64{pair: winner.phaseAnchorOffset}
		group.phaseClasses = map[[2]string]string{pair: winner.phaseClass}
		groups = mergeGroup(groups, group, o)
	}
	if len(groups) != 2 {
		t.Fatalf("strong 2-second winner bridged original 0- and 4-second range classes downstream: %#v", groups)
	}
	if err := markGroupConflicts(groups, o, v2TestBudget()); err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if !slices.Contains(group.Reasons, CompetingIntervals) {
			t.Fatalf("original incompatible range class lost its competing evidence: %#v", group)
		}
	}
}

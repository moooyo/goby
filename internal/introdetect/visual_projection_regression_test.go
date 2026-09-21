package introdetect

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
)

func visualCostGateFixture(t *testing.T) (Episode, Episode, audioMatch) {
	t.Helper()
	a, b, matched := audioEvidenceFixture(124, 0)
	const d = (uint64(1) << 16) - 1
	const e = ((uint64(1) << 25) - 1) << 16
	const f = ((uint64(1) << 25) - 1) << 39
	for i := 0; i <= 30; i++ {
		var right uint64
		if i == 30 {
			right = e ^ f
		} else {
			if i%2 == 1 {
				right ^= d
			}
			if i%5 == 4 {
				right ^= e
			}
		}
		tick := int64(i) * TicksPerSecond
		a.Visual = append(a.Visual, VisualSample{tick, right ^ d, 200})
		b.Visual = append(b.Visual, VisualSample{tick, right, 200})
	}
	audio, reason, err := measureAudioRun(a, b, matched, 0, 123, DefaultOptions(), v2TestBudget())
	if err != nil || audio == nil || reason != "" || len(audio.reasons) != 0 || audio.metrics.AudioAgreementPermille != 1000 ||
		audio.metrics.AudioInformativePermille != 1000 || audio.metrics.AudioSimilarityPermille != 1000 {
		t.Fatalf("strong actual acoustic fixture: %#v, %s, %v", audio, reason, err)
	}
	return a, b, *audio
}

func TestVisualConfirmationRetainsQualifiedZeroWhenCostWinnerFailsHardGate(t *testing.T) {
	a, b, audio := visualCostGateFixture(t)
	o := DefaultOptions()
	winner, err := alignVisual(a, b, audio, 0, o, v2TestBudget())
	if err != nil || winner.phase != TicksPerSecond/2 {
		t.Fatalf("fixture lost its nonzero cost winner: %#v, %v", winner, err)
	}
	alternate, reason, err := measureVisualMatch(a, b, audio, 0, winner, true, o, v2TestBudget())
	if err != nil || alternate != nil || reason != AudioWithoutVisual {
		t.Fatalf("the 18-of-30 cost winner escaped the hard agreement gate: %#v, %s, %v", alternate, reason, err)
	}
	zero, err := materializeVisualPhase(a, b, audio, 0, 0, o, v2TestBudget())
	if err != nil {
		t.Fatal(err)
	}
	baseline, reason, err := measureVisualMatch(a, b, audio, 0, zero, true, o, v2TestBudget())
	if err != nil || baseline == nil || reason != "" || len(baseline.reasons) != 0 || !qualifiedEvidence(baseline.metrics, o) ||
		baseline.metrics.VisualSimilarityPermille != 750 || baseline.metrics.VisualMatchedTimePermille != 967 || baseline.metrics.VisualDistinctStates != 5 {
		t.Fatalf("actual zero witness was not qualified: %#v, %s, %v", baseline, reason, err)
	}
	got, reason, err := visualConfirm(a, b, audio, 0, o, v2TestBudget())
	if err != nil || reason != "" || !reflect.DeepEqual(got, baseline) {
		t.Fatalf("the failed cost winner erased or mixed the complete zero witness: got %#v, want %#v, %s, %v", got, baseline, reason, err)
	}
}

func visualProjectionCycleFixture(t *testing.T) ([]Episode, map[[2]int]pairMatch, map[int]Interval) {
	t.Helper()
	a, b, _ := audioEvidenceFixture(200, 0)
	c := a
	c.Audio = slices.Clone(a.Audio)
	for index := 200; index < len(c.Audio); index++ {
		c.Audio[index].Fingerprint ^= 0xffff0000
	}
	episodes := []Episode{a, b, c}
	for index, key := range []string{"a", "b", "c"} {
		episodes[index].EpisodeKey = "cycle-episode-" + key
		episodes[index].SourceKey = "cycle-source-" + key
		episodes[index].ContentIdentity = "cycle-content-" + key
		episodes[index].AlgorithmProfile = "synthetic-mechanics-v1"
	}
	for i := 0; i <= 50; i++ {
		left, right := stateAnchorTestHash(i), stateAnchorTestHash((i+63)%64)
		middle := left&0xffffffff00000000 | right&0x00000000ffffffff
		for source, hash := range []uint64{left, middle, right} {
			episodes[source].Visual = append(episodes[source].Visual, VisualSample{int64(i) * TicksPerSecond, hash, 200})
		}
	}
	o, budget := DefaultOptions(), v2TestBudget()
	edges := make(map[[2]int]pairMatch)
	for left := 0; left < 3; left++ {
		for right := left + 1; right < 3; right++ {
			runs, _, err := alignedAudio(episodes[left], episodes[right], 0, o, budget)
			if err != nil || len(runs) != 1 || runs[0].a != (Interval{0, 50 * TicksPerSecond}) || runs[0].b != runs[0].a {
				t.Fatalf("actual cycle audio %d/%d: %#v, %v", left, right, runs, err)
			}
			pair, reason, err := visualConfirm(episodes[left], episodes[right], runs[0], 0, o, budget)
			if err != nil || pair == nil || reason != "" || len(pair.reasons) != 0 || !qualifiedEvidence(pair.metrics, o) {
				t.Fatalf("actual cycle edge %d/%d: %#v, %s, %v", left, right, pair, reason, err)
			}
			pair.left, pair.right = left, right
			edges[[2]int{left, right}] = *pair
		}
	}
	if edges[[2]int{0, 1}].visualPhase != 0 || edges[[2]int{1, 2}].visualPhase != 0 || edges[[2]int{0, 2}].visualPhase != TicksPerSecond/2 {
		t.Fatalf("fixture lost its nontransitive visual correspondence: %#v", edges)
	}
	ranges := map[int]Interval{
		0: intersect(edges[[2]int{0, 1}].a, edges[[2]int{0, 2}].a),
		1: intersect(edges[[2]int{0, 1}].b, edges[[2]int{1, 2}].a),
		2: intersect(edges[[2]int{0, 2}].b, edges[[2]int{1, 2}].b),
	}
	return episodes, edges, ranges
}

func TestVisualProjectionDoesNotErodeACompleteCorrespondenceCycle(t *testing.T) {
	episodes, edges, ranges := visualProjectionCycleFixture(t)
	want := map[int]Interval{0: {0, 49 * TicksPerSecond}, 1: {0, 50 * TicksPerSecond}, 2: {TicksPerSecond, 50 * TicksPerSecond}}
	if !reflect.DeepEqual(ranges, want) {
		t.Fatalf("incorrect initial confirmed caps: %#v", ranges)
	}
	group, err := projectGroup(episodes, []int{0, 1, 2}, ranges, edges, DefaultOptions(), v2TestBudget())
	if err != nil || group == nil || group.Status != Qualified || len(group.Members) != 3 || len(group.Reasons) != 0 || group.Metrics.PairCount != 3 {
		t.Fatalf("repeated endpoint feedback erased the complete witness: %#v, %v", group, err)
	}
	for source, member := range group.Members {
		if member.Interval != want[source] {
			t.Fatalf("final interval escaped or eroded its initial confirmed cap: %#v, want %#v", member, want[source])
		}
	}
	if group.Metrics.AudioAgreementPermille != 979 || group.Metrics.VisualMatchedTimePermille != 980 ||
		group.Metrics.VisualMinBandMatchedPermille != 800 || group.Metrics.VisualSimilarityPermille != 750 || group.Metrics.VisualMaxUnconfirmedGapTicks != TicksPerSecond {
		t.Fatalf("final-window lost mates were hidden or discovery metrics were reused: %#v", group.Metrics)
	}
}

func TestVisualProjectionFilteringRetainsRawPositionsAndBothClocks(t *testing.T) {
	second := TicksPerSecond
	a := Episode{Visual: []VisualSample{{0, 1, 200}, {second / 5, 2, 200}, {second / 2, 3, 200}, {second, 4, 200}}}
	b := Episode{Visual: []VisualSample{{0, 1, 200}, {second / 2, 3, 200}, {second, 4, 200}}}
	full := audioMatch{a: Interval{0, second}, b: Interval{0, second}}
	o := DefaultOptions()
	original, err := materializeVisualPhase(a, b, full, 0, 0, o, v2TestBudget())
	if err != nil || !reflect.DeepEqual(original.targets, []int{0, -1, 1, 2}) {
		t.Fatalf("raw one-to-one fixture: %#v, %v", original, err)
	}
	cropped := audioMatch{a: Interval{second / 10, second}, b: full.b}
	filtered, err := filterVisualAlignment(a, b, cropped, 0, original, o, v2TestBudget())
	if err != nil || filtered.start != 1 || !reflect.DeepEqual(filtered.targets, []int{-1, 1, 2}) {
		t.Fatalf("cropping released a target or compressed unpaired positions: %#v, %v", filtered, err)
	}
	// An ordinary uniform map makes each side's missing edge time explicit.
	a = b
	original, err = materializeVisualPhase(a, b, full, 0, 0, o, v2TestBudget())
	if err != nil {
		t.Fatal(err)
	}
	for _, window := range []audioMatch{
		{a: full.a, b: Interval{0, second / 2}},
		{a: Interval{second / 2, second}, b: full.b},
	} {
		filtered, err := filterVisualAlignment(a, b, window, 0, original, o, v2TestBudget())
		if err != nil {
			t.Fatal(err)
		}
		evidence, err := measureAlignedVisualV2(a, b, window, filtered, o, v2TestBudget())
		if err != nil || evidence.metrics.VisualMatchedTimePermille != 500 || evidence.metrics.VisualUnobservableTimePermille != 500 || evidence.metrics.VisualContradictedTimePermille != 0 {
			t.Fatalf("an excluded mate supplied time on the other clock: %#v, %v", evidence, err)
		}
	}
	filtered, err = filterVisualAlignment(a, b, full, -second, original, o, v2TestBudget())
	if err != nil || filtered.maximumResidual != second || !reflect.DeepEqual(filtered.targets, original.targets) {
		t.Fatalf("the exact current acoustic corridor boundary changed: %#v, %v", filtered, err)
	}
	filtered, err = filterVisualAlignment(a, b, full, -second-1, original, o, v2TestBudget())
	if err != nil || !reflect.DeepEqual(filtered.targets, []int{-1, -1, -1}) {
		t.Fatalf("projection expanded the current acoustic corridor: %#v, %v", filtered, err)
	}
}

func TestVisualProjectionRetainsWitnessCapsClockAndNonintervalReasons(t *testing.T) {
	episodes, edges, _ := visualProjectionCycleFixture(t)
	o := DefaultOptions()
	original := edges[[2]int{0, 1}]
	original.reasons = []Reason{WeakVisualEvidence, PeriodicVisualEvidence, CandidateSearchLimited}
	before := slices.Clone(original.reasons)
	interval := Interval{10 * TicksPerSecond, 40 * TicksPerSecond}
	currentOffset := TicksPerSecond / 10
	audio, err := projectAudioEvidence(episodes[0], episodes[1], interval, interval, currentOffset, original.audio, o, v2TestBudget())
	if err != nil || audio == nil {
		t.Fatalf("actual projected audio: %#v, %v", audio, err)
	}
	value, reason, err := projectVisualEvidence(episodes[0], episodes[1], *audio, currentOffset, original, o, v2TestBudget())
	if err != nil || value == nil || reason != "" || value.a != interval || value.b != interval ||
		!qualifiedEvidence(value.metrics, o) || slices.Contains(value.reasons, WeakVisualEvidence) ||
		!slices.Contains(value.reasons, PeriodicVisualEvidence) || !slices.Contains(value.reasons, CandidateSearchLimited) ||
		!reflect.DeepEqual(value.audio, original.audio) || value.offset != original.offset || value.visualPhase != original.visualPhase || !slices.Equal(original.reasons, before) {
		t.Fatalf("projection changed immutable provenance or reused old interval quality: %#v, %s, %v", value, reason, err)
	}
	// A later crop still rebuilds from the discovery context, rather than
	// applying the original phase to the previous measurement's new clock.
	narrow := Interval{12 * TicksPerSecond, 38 * TicksPerSecond}
	narrowAudio, err := projectAudioEvidence(episodes[0], episodes[1], narrow, narrow, -currentOffset, value.audio, o, v2TestBudget())
	if err != nil || narrowAudio == nil {
		t.Fatalf("nested actual acoustic projection: %#v, %v", narrowAudio, err)
	}
	nested, reason, err := projectVisualEvidence(episodes[0], episodes[1], *narrowAudio, -currentOffset, *value, o, v2TestBudget())
	if err != nil || nested == nil || reason != "" || nested.a != narrow || nested.b != narrow || !slices.Contains(nested.reasons, ShortInterval) ||
		nested.visualPhase != original.visualPhase || nested.offset != original.offset || !reflect.DeepEqual(nested.audio, original.audio) {
		t.Fatalf("a later crop reinterpreted the frozen correspondence clock: %#v, %s, %v", nested, reason, err)
	}
	// The audio window is wider than this pair's confirmed visual caps.
	shifted := edges[[2]int{0, 2}]
	for _, expanded := range []audioMatch{
		{a: shifted.audio.a, b: shifted.b, metrics: shifted.audio.metrics},
		{a: shifted.a, b: shifted.audio.b, metrics: shifted.audio.metrics},
	} {
		value, _, err := projectVisualEvidence(episodes[0], episodes[2], expanded, 0, shifted, o, v2TestBudget())
		if value != nil || !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("an acoustic window was mistaken for confirmed visual bounds: %#v, %v", value, err)
		}
	}
}

func TestVisualProjectionRemeasuresConcentratedFailuresAndHardGates(t *testing.T) {
	for _, kind := range []string{"concentrated_mismatch", "all_mismatch", "long_dark_gap"} {
		t.Run(kind, func(t *testing.T) {
			episodes, edges, _ := visualProjectionCycleFixture(t)
			original := edges[[2]int{0, 1}]
			for index := range episodes[1].Visual {
				if kind == "all_mismatch" || kind == "concentrated_mismatch" && index >= 11 && index <= 13 {
					episodes[1].Visual[index].Hash = ^episodes[1].Visual[index].Hash
				}
				if kind == "long_dark_gap" && index >= 10 && index <= 13 {
					episodes[1].Visual[index].Contrast = 0
				}
			}
			value, reason, err := projectVisualEvidence(episodes[0], episodes[1], original.audio, 0, original, DefaultOptions(), v2TestBudget())
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "concentrated_mismatch":
				if value == nil || reason != "" || value.a != original.a || value.b != original.b ||
					value.metrics.VisualMinBandMatchedPermille != 200 || !slices.Contains(value.reasons, InsufficientVisualAnchors) || qualifiedEvidence(value.metrics, DefaultOptions()) {
					t.Fatalf("fixed bounds hid a real concentrated failure: %#v, %s", value, reason)
				}
			case "all_mismatch":
				if value != nil || reason != AudioWithoutVisual {
					t.Fatalf("projection bypassed the hard agreement gate: %#v, %s", value, reason)
				}
			case "long_dark_gap":
				if value != nil || reason != InsufficientVisual {
					t.Fatalf("projection bypassed the maximum usable gap: %#v, %s", value, reason)
				}
			}
		})
	}
}

func TestVisualWitnessSelectionAndProjectionFailClosedOnBudgetAndCancellation(t *testing.T) {
	a, b, audio := visualCostGateFixture(t)
	o := DefaultOptions()
	complete := v2TestBudget()
	if value, _, err := visualConfirm(a, b, audio, 0, o, complete); err != nil || value == nil || value.visualPhase != 0 {
		t.Fatalf("complete zero witness: %#v, %v", value, err)
	}
	value, _, err := visualConfirm(a, b, audio, 0, o, &workBudget{ctx: context.Background(), limit: complete.used - 1})
	if value != nil || !errors.Is(err, ErrLimit) {
		t.Fatalf("an alternate's late budget failure returned the successful zero witness: %#v, %v", value, err)
	}
	episodes, edges, _ := visualProjectionCycleFixture(t)
	original := edges[[2]int{0, 1}]
	complete = v2TestBudget()
	if value, _, err := projectVisualEvidence(episodes[0], episodes[1], original.audio, 0, original, o, complete); err != nil || value == nil {
		t.Fatalf("complete projected witness: %#v, %v", value, err)
	}
	value, _, err = projectVisualEvidence(episodes[0], episodes[1], original.audio, 0, original, o,
		&workBudget{ctx: context.Background(), limit: complete.used - 1})
	if value != nil || !errors.Is(err, ErrLimit) {
		t.Fatalf("projection returned incomplete metrics after exhaustion: %#v, %v", value, err)
	}
	base, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	during := &cancelDuringContext{Context: base, cancel: stop, after: 3}
	value, _, err = projectVisualEvidence(episodes[0], episodes[1], original.audio, 0, original, o, &workBudget{ctx: during, limit: o.MaxComparisons})
	if value != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled projection returned visual evidence: %#v, %v", value, err)
	}
}

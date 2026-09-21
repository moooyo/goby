package introdetect

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func hypothesisPhasePair(offset int64, agreement int) pairMatch {
	value := hypothesisTestPair()
	value.offset = offset
	value.b = Interval{value.a.StartTicks + offset, value.a.EndTicks + offset}
	value.metrics.AudioAgreementPermille = agreement
	value.audio = audioMatch{a: value.a, b: value.b, metrics: value.metrics}
	return value
}

func TestDistinctPairHypothesesCannotBridgePhasesThroughAStrongerWinner(t *testing.T) {
	o := DefaultOptions()
	first := hypothesisPhasePair(0, 950)
	first.reasons = []Reason{WeakVisualEvidence}
	bridge := hypothesisPhasePair(3*TicksPerSecond/10, 990)
	last := hypothesisPhasePair(6*TicksPerSecond/10, 970)
	if !equivalentPair(first, bridge, o) || !equivalentPair(bridge, last, o) || equivalentPair(first, last, o) {
		t.Fatal("fixture does not contain the nontransitive phase bridge")
	}
	values := []pairMatch{first, bridge, last}
	want := []pairMatch{bridge, last}
	want[0].phaseAnchorOffset, want[0].phaseGrouped = first.offset, true
	want[1].phaseAnchorOffset, want[1].phaseGrouped = last.offset, true
	want[0].phaseClass, want[1].phaseClass = pairHypothesisClass(first), pairHypothesisClass(last)
	for _, permutation := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		input := []pairMatch{values[permutation[0]], values[permutation[1]], values[permutation[2]]}
		before := append([]pairMatch(nil), input...)
		budget := &workBudget{ctx: context.Background(), limit: o.MaxComparisons}
		got, err := distinctPairHypotheses(input, o, budget)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("winner replacement moved an anchor or changed the complete witness: order=%v got=%#v err=%v", permutation, got, err)
		}
		if !reflect.DeepEqual(input, before) {
			t.Fatal("canonical sorting modified the caller's candidate order")
		}
	}
}

func TestDistinctPairHypothesesUsesFixedActualRangesAsWellAsPhase(t *testing.T) {
	o := DefaultOptions()
	values := []pairMatch{hypothesisPhasePair(0, 950), hypothesisPhasePair(0, 990), hypothesisPhasePair(0, 970)}
	for index := range values {
		shift := int64(index*2) * TicksPerSecond
		values[index].a.StartTicks += shift
		values[index].a.EndTicks += shift
		values[index].b.StartTicks += shift
		values[index].b.EndTicks += shift
		values[index].audio.a, values[index].audio.b = values[index].a, values[index].b
	}
	budget := &workBudget{ctx: context.Background(), limit: o.MaxComparisons}
	got, err := distinctPairHypotheses(values, o, budget)
	if err != nil || len(got) != 2 || got[0].a != values[1].a || got[1].a != values[2].a {
		t.Fatalf("a stronger interval moved the immutable range anchor: %#v, %v", got, err)
	}
}

func TestDistinctPairHypothesesPreservesDeterministicCompleteAudioWitness(t *testing.T) {
	o := DefaultOptions()
	a := hypothesisPhasePair(0, 950)
	a.audio.a.StartTicks--
	a.audio.b.StartTicks--
	b := a
	b.audio.a.StartTicks++
	b.audio.b.StartTicks++
	var baseline []pairMatch
	for _, input := range [][]pairMatch{{a, b}, {b, a}} {
		budget := &workBudget{ctx: context.Background(), limit: o.MaxComparisons}
		got, err := distinctPairHypotheses(input, o, budget)
		if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0].audio, a.audio) {
			t.Fatalf("equal final metrics lost the stable original audio witness: %#v, %v", got, err)
		}
		if baseline != nil && !reflect.DeepEqual(got, baseline) {
			t.Fatal("an evidence tie depended on raw candidate input order")
		}
		baseline = got
	}
}

func TestDistinctPairHypothesesFailsClosedAtCandidateAndWorkLimits(t *testing.T) {
	values := []pairMatch{hypothesisPhasePair(0, 950), hypothesisPhasePair(3*TicksPerSecond/10, 990), hypothesisPhasePair(6*TicksPerSecond/10, 970)}
	for _, kind := range []string{"candidate_limit", "comparison_limit", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			o := DefaultOptions()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			budget := &workBudget{ctx: ctx, limit: o.MaxComparisons}
			want := ErrLimit
			switch kind {
			case "candidate_limit":
				o.MaxCandidatesPerPair = 1
			case "comparison_limit":
				budget.limit = 1
			case "cancelled":
				cancel()
				want = context.Canceled
			}
			got, err := distinctPairHypotheses(values, o, budget)
			if got != nil || !errors.Is(err, want) {
				t.Fatalf("partial hypotheses escaped a bounded operation: got=%#v err=%v", got, err)
			}
		})
	}
}

func hypothesisTestMetrics() Metrics {
	return Metrics{
		AudioAgreementPermille: 950, AudioSimilarityPermille: 940, AudioInformativePermille: 930,
		AudioSamples: 180, AudioDistinct: 90,
		VisualAgreementPermille: 1000, VisualSimilarityPermille: 1000, VisualCoveragePermille: 1000,
		VisualSamples: 46, VisualTransitions: 5, VisualChangeCoveragePermille: 108, VisualDominancePermille: 200,
		VisualMinBandMatchedPermille: 800, VisualMatchedTimePermille: 900,
		VisualContradictedTimePermille: 50, VisualUnobservableTimePermille: 50,
		VisualAnchorCount: 9, VisualDistinctStates: 6, VisualDominantStatePermille: 200,
		BoundaryUncertaintyTicks: 2 * TicksPerSecond, PairCount: 1,
	}
}

func hypothesisTestPair() pairMatch {
	return pairMatch{
		left: 0, right: 1, offset: 25 * TicksPerSecond,
		a:       Interval{10 * TicksPerSecond, 60 * TicksPerSecond},
		b:       Interval{35 * TicksPerSecond, 85 * TicksPerSecond},
		metrics: hypothesisTestMetrics(),
	}
}

func hypothesisTestGroup() Group {
	metrics := hypothesisTestMetrics()
	metrics.PairCount = 3
	return Group{
		ID: "complete-group", AlgorithmProfile: "synthetic-hypothesis-profile", Status: Qualified, Metrics: metrics,
		Members: []Support{
			{EpisodeKey: "episode-a", SourceKey: "source-a", ContentIdentity: "content-a", Interval: Interval{10 * TicksPerSecond, 60 * TicksPerSecond}},
			{EpisodeKey: "episode-b", SourceKey: "source-b", ContentIdentity: "content-b", Interval: Interval{35 * TicksPerSecond, 85 * TicksPerSecond}},
			{EpisodeKey: "episode-c", SourceKey: "source-c", ContentIdentity: "content-c", Interval: Interval{65 * TicksPerSecond, 115 * TicksPerSecond}},
		},
	}
}

func TestEquivalentPairPreservesDifferentOffsetHypotheses(t *testing.T) {
	o := DefaultOptions()
	base := hypothesisTestPair()
	near := base
	near.offset += o.AudioAlignmentTicks
	if !equivalentPair(base, near, o) || !equivalentPair(near, base, o) {
		t.Fatal("inclusive audio alignment tolerance did not preserve one hypothesis")
	}
	near.offset++
	if equivalentPair(base, near, o) || equivalentPair(near, base, o) {
		t.Fatal("an offset outside audio alignment tolerance was deduplicated")
	}
	phase := base
	phase.offset += 2 * TicksPerSecond
	phase.b.StartTicks += 2 * TicksPerSecond
	phase.b.EndTicks += 2 * TicksPerSecond
	if !compatibleInterval(base.b, phase.b, o) {
		t.Fatal("fixture must have compatible ranges but a distinct actual phase")
	}
	if equivalentPair(base, phase, o) {
		t.Fatal("range compatibility erased a distinct two-second phase")
	}
	far := base
	far.a.StartTicks += 4 * TicksPerSecond
	far.a.EndTicks += 4 * TicksPerSecond
	if equivalentPair(base, far, o) {
		t.Fatal("matching offsets erased an incompatible source range")
	}
	minimum, maximum := base, base
	minimum.offset, maximum.offset = -1<<63, 1<<63-1
	if equivalentPair(minimum, maximum, o) {
		t.Fatal("overflow made distant offsets equivalent")
	}
}

func TestPreferPairChoosesCompleteEligibleWitness(t *testing.T) {
	o := DefaultOptions()
	strong := hypothesisTestPair()
	if !qualifiedEvidence(strong.metrics, o) {
		t.Fatal("strong fixture is missing joint evidence")
	}
	for _, kind := range []string{"missing_visual", "review_reason", "short_left", "short_right"} {
		t.Run(kind, func(t *testing.T) {
			weak := strong
			weak.metrics.AudioAgreementPermille = 1000
			switch kind {
			case "missing_visual":
				weak.metrics.VisualMatchedTimePermille = 840
				weak.metrics.VisualContradictedTimePermille = 110
			case "review_reason":
				weak.reasons = []Reason{CandidateSearchLimited}
			case "short_left":
				weak.a.EndTicks = weak.a.StartTicks + o.AutoMinDurationTicks - 1
			case "short_right":
				weak.b.EndTicks = weak.b.StartTicks + o.AutoMinDurationTicks - 1
			}
			before := weak
			before.reasons = append([]Reason(nil), weak.reasons...)
			if !preferPair(strong, weak, o) || preferPair(weak, strong, o) {
				t.Fatal("one strong metric outranked a complete eligible observation")
			}
			selected := weak
			if preferPair(strong, selected, o) {
				selected = strong
			}
			if !reflect.DeepEqual(selected, strong) || !reflect.DeepEqual(weak, before) {
				t.Fatal("selection combined witnesses or mutated the rejected observation")
			}
		})
	}
}

func TestPreferPairUsesLexicographicEvidenceThenActualTimeline(t *testing.T) {
	o := DefaultOptions()
	base := hypothesisTestPair()
	for _, kind := range []string{"audio_agreement", "audio_similarity", "audio_information", "minimum_band", "matched_time", "contradiction", "unobservable", "uncertainty"} {
		t.Run(kind, func(t *testing.T) {
			better := base
			switch kind {
			case "audio_agreement":
				better.metrics.AudioAgreementPermille++
				better.metrics.VisualMinBandMatchedPermille = 500
			case "audio_similarity":
				better.metrics.AudioSimilarityPermille++
				better.metrics.AudioInformativePermille--
			case "audio_information":
				better.metrics.AudioInformativePermille++
				better.metrics.VisualMinBandMatchedPermille--
			case "minimum_band":
				better.metrics.VisualMinBandMatchedPermille++
				better.metrics.VisualMatchedTimePermille--
				better.metrics.VisualUnobservableTimePermille++
			case "matched_time":
				better.metrics.VisualMatchedTimePermille++
				better.metrics.VisualUnobservableTimePermille--
			case "contradiction":
				better.metrics.VisualContradictedTimePermille--
				better.metrics.VisualUnobservableTimePermille++
			case "unobservable":
				better.metrics.VisualUnobservableTimePermille--
				better.metrics.BoundaryUncertaintyTicks++
			case "uncertainty":
				better.metrics.BoundaryUncertaintyTicks--
			}
			if !qualifiedEvidence(better.metrics, o) || !preferPair(better, base, o) || preferPair(base, better, o) {
				t.Fatalf("lexicographic evidence order changed at %s", kind)
			}
		})
	}
	later := base
	later.a.StartTicks++
	later.a.EndTicks++
	later.b.StartTicks++
	later.b.EndTicks++
	if !preferPair(base, later, o) || preferPair(later, base, o) || preferPair(base, base, o) {
		t.Fatal("equal-strength timeline tie is unstable")
	}
	phase := base
	phase.offset++
	if !preferPair(base, phase, o) || preferPair(phase, base, o) {
		t.Fatal("equal-strength actual offset tie is unstable")
	}
}

func TestPreferGroupPreservesCompleteWitnessAndMemberOrder(t *testing.T) {
	o := DefaultOptions()
	strong := hypothesisTestGroup()
	weak := hypothesisTestGroup()
	weak.Metrics.AudioAgreementPermille = 1000
	weak.Metrics.VisualMinBandMatchedPermille = 499
	weak.Reasons = []Reason{WeakVisualEvidence}
	if !preferGroup(strong, weak, o) || preferGroup(weak, strong, o) {
		t.Fatal("group selection favored strong audio without complete visual proof")
	}
	selected := weak
	if preferGroup(strong, selected, o) {
		selected = strong
	}
	if !reflect.DeepEqual(selected, strong) {
		t.Fatal("group selection combined separate witnesses")
	}
	short := hypothesisTestGroup()
	short.Metrics.AudioAgreementPermille = 1000
	short.Members[2].Interval.EndTicks = short.Members[2].Interval.StartTicks + o.AutoMinDurationTicks - 1
	if !preferGroup(strong, short, o) || preferGroup(short, strong, o) {
		t.Fatal("a short group member escaped the joint qualification gate")
	}
	permuted := hypothesisTestGroup()
	permuted.Members[0], permuted.Members[2] = permuted.Members[2], permuted.Members[0]
	before := append([]Support(nil), permuted.Members...)
	if preferGroup(strong, permuted, o) || preferGroup(permuted, strong, o) {
		t.Fatal("member input order changed an otherwise identical witness")
	}
	if !reflect.DeepEqual(permuted.Members, before) {
		t.Fatal("preference sorting mutated caller-owned members")
	}
}

func hypothesisTestEdges() map[[2]int]pairMatch {
	return map[[2]int]pairMatch{
		{2, 5}: {left: 2, right: 5, offset: 12 * TicksPerSecond},
		{2, 7}: {left: 7, right: 2, offset: 8 * TicksPerSecond},
		{5, 7}: {left: 5, right: 7, offset: -20 * TicksPerSecond},
	}
}

func TestConsistentOffsetsUsesActualDirectedEdgesWithoutMutation(t *testing.T) {
	o := DefaultOptions()
	selected := []int{7, 2, 5}
	edges := hypothesisTestEdges()
	beforeSelected, beforeEdges := append([]int(nil), selected...), hypothesisTestEdges()
	want := map[int]int64{2: 0, 5: 12 * TicksPerSecond, 7: -8 * TicksPerSecond}
	clocks, ok := consistentOffsets(selected, edges, o)
	if !ok || !reflect.DeepEqual(clocks, want) {
		t.Fatalf("directed offsets did not produce the reference clocks: %#v, %v", clocks, ok)
	}
	permuted, ok := consistentOffsets([]int{5, 7, 2}, edges, o)
	if !ok || !reflect.DeepEqual(permuted, want) {
		t.Fatal("selected member order changed the reference or inferred clocks")
	}
	if !reflect.DeepEqual(selected, beforeSelected) || !reflect.DeepEqual(edges, beforeEdges) {
		t.Fatal("clock consistency mutated the selected members or pair edges")
	}
}

func TestConsistentOffsetsRejectsContradictoryTriangleAndIncompleteClique(t *testing.T) {
	o := DefaultOptions()
	edges := hypothesisTestEdges()
	edge := edges[[2]int{5, 7}]
	edge.offset += o.AudioAlignmentTicks
	edges[[2]int{5, 7}] = edge
	if _, ok := consistentOffsets([]int{2, 5, 7}, edges, o); !ok {
		t.Fatal("inclusive residual tolerance rejected the actual reference clocks")
	}
	edge.offset++
	edges[[2]int{5, 7}] = edge
	if clocks, ok := consistentOffsets([]int{2, 5, 7}, edges, o); ok || clocks != nil {
		t.Fatal("fitting or pairwise range overlap concealed a contradictory triangle")
	}
	for _, kind := range []string{"missing_reference", "missing_cross_edge", "wrong_endpoints", "duplicate_member", "empty_members"} {
		t.Run(kind, func(t *testing.T) {
			selected := []int{2, 5, 7}
			edges := hypothesisTestEdges()
			switch kind {
			case "missing_reference":
				delete(edges, [2]int{2, 7})
			case "missing_cross_edge":
				delete(edges, [2]int{5, 7})
			case "wrong_endpoints":
				edges[[2]int{5, 7}] = pairMatch{left: 2, right: 7, offset: -20 * TicksPerSecond}
			case "duplicate_member":
				selected = []int{2, 5, 5}
			case "empty_members":
				selected = nil
			}
			if clocks, ok := consistentOffsets(selected, edges, o); ok || clocks != nil {
				t.Fatal("an incomplete or malformed clock hypothesis was accepted")
			}
		})
	}
}

func TestConsistentOffsetsRejectsTickArithmeticOverflow(t *testing.T) {
	edges := map[[2]int]pairMatch{
		{0, 1}: {left: 0, right: 1, offset: 1<<63 - 1},
		{0, 2}: {left: 0, right: 2, offset: -1 << 63},
		{1, 2}: {left: 1, right: 2, offset: 1},
	}
	if clocks, ok := consistentOffsets([]int{0, 1, 2}, edges, DefaultOptions()); ok || clocks != nil {
		t.Fatal("signed subtraction overflow repaired an inconsistent triangle")
	}
	edges[[2]int{0, 1}] = pairMatch{left: 1, right: 0, offset: -1 << 63}
	if clocks, ok := consistentOffsets([]int{0, 1}, edges, DefaultOptions()); ok || clocks != nil {
		t.Fatal("signed negation overflow produced an oriented edge")
	}
}

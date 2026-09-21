package introdetect

import (
	"context"
	"errors"
	"maps"
	"math/bits"
	"reflect"
	"slices"
	"testing"
)

type clockProjectionConfig struct {
	alternating  bool
	weakBaseline bool
	coherent     bool
	middleShift  int64
}

type clockProjectionFixtureData struct {
	episodes []Episode
	edges    map[[2]int]pairMatch
	ranges   map[int]Interval
	admitted map[int]int64
	refined  map[int]int64
}

// This is a projection-stage mechanics fixture. Real audio/visual functions
// construct every pair; it makes no claim about discovery votes or real media.
func clockProjectionFixture(t *testing.T, config clockProjectionConfig) clockProjectionFixtureData {
	t.Helper()
	episodes, _ := v2ProjectionFixture(t)
	step := TicksPerSecond / 4
	body := []uint32{0, 0xffffffff, 0xaaaaaaaa}
	ranges := make(map[int]Interval)
	for source := range episodes {
		episodes[source].Audio = nil
		shift := int64(0)
		if source == 1 {
			shift = config.middleShift
			episodes[source].DurationTicks += shift
			for index := range episodes[source].Visual {
				episodes[source].Visual[index].Ticks += shift
			}
		}
		for index := 0; index < 400; index++ {
			word := body[source]
			if index < 200 {
				word = audioEvidenceWord(index)
				if config.alternating && index%2 == 1 {
					word = ^word
				}
				if config.weakBaseline && source == 2 && index%8 == 0 {
					word ^= 0xff
				}
			}
			episodes[source].Audio = append(episodes[source].Audio, AudioSample{
				StartTicks: shift + int64(index)*step, EndTicks: shift + int64(index+1)*step, Fingerprint: word,
			})
		}
		ranges[source] = Interval{5*TicksPerSecond + shift, 45*TicksPerSecond + shift}
	}
	for index := 0; index < 199; index++ {
		word, next := episodes[0].Audio[index].Fingerprint, episodes[0].Audio[index+1].Fingerprint
		if bits.OnesCount32(word) != 16 || config.alternating && bits.OnesCount32(word^next) <= DefaultOptions().MaxAudioHamming {
			t.Fatal("fixture words do not prove the intended nearest-bin distinction")
		}
	}
	edges := make(map[[2]int]pairMatch)
	o := DefaultOptions()
	for _, key := range [][2]int{{0, 1}, {0, 2}, {1, 2}} {
		offset := int64(1_125_000)
		if key == [2]int{1, 2} {
			offset = -offset
		}
		if config.coherent {
			offset = 0
		}
		runs, _, err := alignedAudio(episodes[key[0]], episodes[key[1]], offset, o, audioEvidenceBudget())
		if err != nil || len(runs) != 1 {
			t.Fatalf("actual direct audio witness %v: %#v, %v", key, runs, err)
		}
		match, reason, err := visualConfirm(episodes[key[0]], episodes[key[1]], runs[0], offset, o, audioEvidenceBudget())
		if err != nil || match == nil || reason != "" {
			t.Fatalf("actual direct audiovisual witness %v: %#v, %s, %v", key, match, reason, err)
		}
		if !config.weakBaseline && (len(match.reasons) != 0 || !qualifiedEvidence(match.metrics, o)) {
			t.Fatalf("direct witness %v must qualify before projection: %#v", key, match)
		}
		match.left, match.right = key[0], key[1]
		match.phaseGrouped, match.phaseAnchorOffset = true, offset
		match.phaseClass = pairHypothesisClass(*match)
		edges[key] = *match
	}
	admitted, ok := consistentOffsets([]int{0, 1, 2}, edges, o)
	if !ok {
		t.Fatal("fixture must satisfy the original complete-clique clock admission")
	}
	refined, err := refineCliqueClocks([]int{0, 1, 2}, edges, admitted, o, audioEvidenceBudget())
	if err != nil {
		t.Fatal(err)
	}
	if !config.coherent && (!reflect.DeepEqual(admitted, map[int]int64{0: 0, 1: 1_125_000, 2: 1_125_000}) ||
		!reflect.DeepEqual(refined, map[int]int64{0: 0, 1: 1_500_000, 2: 750_000})) {
		t.Fatalf("fixture must cross the half-bin boundary under a geometry-valid refinement: %#v, %#v", admitted, refined)
	}
	return clockProjectionFixtureData{episodes, edges, ranges, admitted, refined}
}

func cloneClockProjectionFixture(value clockProjectionFixtureData) clockProjectionFixtureData {
	value.episodes = slices.Clone(value.episodes)
	for index := range value.episodes {
		value.episodes[index].Audio = slices.Clone(value.episodes[index].Audio)
		value.episodes[index].Visual = slices.Clone(value.episodes[index].Visual)
	}
	value.edges = maps.Clone(value.edges)
	for key, edge := range value.edges {
		edge.reasons = slices.Clone(edge.reasons)
		edge.audio.reasons = slices.Clone(edge.audio.reasons)
		value.edges[key] = edge
	}
	value.ranges, value.admitted, value.refined = maps.Clone(value.ranges), maps.Clone(value.admitted), maps.Clone(value.refined)
	return value
}

func TestGroupProjectionPreservesQualifiedAdmittedWitnessAfterClockRefinement(t *testing.T) {
	for _, example := range []struct {
		name        string
		alternating bool
	}{
		{"refined_audio_missing", true},
		{"refined_audio_weak", false},
	} {
		t.Run(example.name, func(t *testing.T) {
			fixture := clockProjectionFixture(t, clockProjectionConfig{alternating: example.alternating})
			before := cloneClockProjectionFixture(fixture)
			o := DefaultOptions()
			for _, key := range [][2]int{{0, 1}, {0, 2}, {1, 2}} {
				pair, err := projectAudioEvidence(fixture.episodes[key[0]], fixture.episodes[key[1]], fixture.ranges[key[0]], fixture.ranges[key[1]],
					fixture.admitted[key[1]]-fixture.admitted[key[0]], fixture.edges[key].audio, o, audioEvidenceBudget())
				if err != nil || pair == nil || pair.metrics.AudioSamples != 160 || pair.metrics.AudioAgreementPermille != 1000 || pair.metrics.AudioSimilarityPermille != 1000 {
					t.Fatalf("baseline pair %v must have complete actual audio: %#v, %v", key, pair, err)
				}
			}
			pair, err := projectAudioEvidence(fixture.episodes[0], fixture.episodes[1], fixture.ranges[0], fixture.ranges[1],
				fixture.refined[1], fixture.edges[[2]int{0, 1}].audio, o, audioEvidenceBudget())
			if err != nil || example.alternating && pair != nil {
				t.Fatalf("the refined nearest-bin witness must be absent: %#v, %v", pair, err)
			}
			if !example.alternating && (pair == nil || pair.metrics.AudioSamples != 139 || pair.metrics.AudioAgreementPermille != 868 || !slices.Contains(pair.reasons, WeakAudioEvidence)) {
				t.Fatalf("the refined nearest-bin witness must retain actual weakness: %#v", pair)
			}
			baselineBudget := audioEvidenceBudget()
			baseline, err := projectGroupWithClocks(fixture.episodes, []int{0, 1, 2}, fixture.ranges, fixture.edges, fixture.admitted, o, baselineBudget)
			if err != nil || baseline == nil || baseline.Status != Qualified || len(baseline.Members) != 3 || baseline.Metrics.PairCount != 3 {
				t.Fatalf("the complete original mapping must qualify: %#v, %v", baseline, err)
			}
			refined, err := projectGroupWithClocks(fixture.episodes, []int{0, 1, 2}, fixture.ranges, fixture.edges, fixture.refined, o, audioEvidenceBudget())
			if err != nil || example.alternating && refined != nil || !example.alternating && (refined == nil || refined.Status != Review || !slices.Contains(refined.Reasons, WeakAudioEvidence)) {
				t.Fatalf("the complete refined mapping must preserve its real failure: %#v, %v", refined, err)
			}
			// Exactly enough for admission, one complete projection, and its
			// qualification check: attempting a refinement would exhaust it.
			limit := int64(3+len(baseline.Members)) + baselineBudget.used
			for _, selected := range [][]int{{0, 1, 2}, {2, 0, 1}, {1, 2, 0}} {
				originalOrder := slices.Clone(selected)
				edges := make(map[[2]int]pairMatch)
				for _, key := range [][2]int{{1, 2}, {0, 2}, {0, 1}} {
					edges[key] = fixture.edges[key]
				}
				budget := &workBudget{ctx: context.Background(), limit: limit}
				got, err := projectGroup(fixture.episodes, selected, fixture.ranges, edges, o, budget)
				if err != nil || !reflect.DeepEqual(got, baseline) || budget.used != limit {
					t.Fatalf("qualified baseline was replaced or a second mapping was attempted: %#v, %v, used %d/%d", got, err, budget.used, limit)
				}
				if !slices.Equal(selected, originalOrder) || !reflect.DeepEqual(edges, fixture.edges) {
					t.Fatal("projection mutated the selected member order or pair witnesses")
				}
			}
			if !reflect.DeepEqual(fixture, before) {
				t.Fatal("projection mutated source features, initial intervals, clocks, or phase evidence")
			}
		})
	}
}

func TestGroupProjectionRetainsCompleteReviewWhenRefinementLosesEvidence(t *testing.T) {
	for _, alternating := range []bool{true, false} {
		fixture := clockProjectionFixture(t, clockProjectionConfig{alternating: alternating, weakBaseline: true})
		o := DefaultOptions()
		baseline, err := projectGroupWithClocks(fixture.episodes, []int{0, 1, 2}, fixture.ranges, fixture.edges, fixture.admitted, o, audioEvidenceBudget())
		if err != nil || baseline == nil || baseline.Status != Review || baseline.Metrics.AudioAgreementPermille != 875 || !slices.Contains(baseline.Reasons, WeakAudioEvidence) {
			t.Fatalf("baseline fixture must be a complete actual Review: %#v, %v", baseline, err)
		}
		refined, err := projectGroupWithClocks(fixture.episodes, []int{0, 1, 2}, fixture.ranges, fixture.edges, fixture.refined, o, audioEvidenceBudget())
		if err != nil || alternating && refined != nil || !alternating && (refined == nil || refined.Status != Review || refined.Metrics.AudioAgreementPermille >= baseline.Metrics.AudioAgreementPermille) {
			t.Fatalf("refined fixture must lose complete evidence or actual agreement: %#v, %v", refined, err)
		}
		got, err := projectGroup(fixture.episodes, []int{0, 1, 2}, fixture.ranges, fixture.edges, o, audioEvidenceBudget())
		if err != nil || !reflect.DeepEqual(got, baseline) {
			t.Fatalf("the original Review was lost or mixed with another witness: %#v, %v", got, err)
		}
	}
}

func TestGroupProjectionCanUseCompleteRefinementWhenBaselineHasNoEvidence(t *testing.T) {
	fixture := clockProjectionFixture(t, clockProjectionConfig{alternating: true, middleShift: 1_800_000})
	before := cloneClockProjectionFixture(fixture)
	o := DefaultOptions()
	baseline, err := projectGroupWithClocks(fixture.episodes, []int{0, 1, 2}, fixture.ranges, fixture.edges, fixture.admitted, o, audioEvidenceBudget())
	if err != nil || baseline != nil {
		t.Fatalf("original cross-edge must select a rejected adjacent bin: %#v, %v", baseline, err)
	}
	refined, err := projectGroupWithClocks(fixture.episodes, []int{0, 1, 2}, fixture.ranges, fixture.edges, fixture.refined, o, audioEvidenceBudget())
	if err != nil || refined == nil || refined.Status != Qualified || refined.Metrics.PairCount != 3 {
		t.Fatalf("one refined mapping must independently prove the full clique: %#v, %v", refined, err)
	}
	got, err := projectGroup(fixture.episodes, []int{2, 1, 0}, fixture.ranges, fixture.edges, o, audioEvidenceBudget())
	if err != nil || !reflect.DeepEqual(got, refined) || !reflect.DeepEqual(fixture, before) {
		t.Fatalf("complete refinement was lost, mixed, or changed its inputs: %#v, %v", got, err)
	}
}

func TestGroupProjectionSkipsAnIdenticalClockMappingForReview(t *testing.T) {
	fixture := clockProjectionFixture(t, clockProjectionConfig{alternating: true, weakBaseline: true, coherent: true})
	o := DefaultOptions()
	budget := audioEvidenceBudget()
	clocks, valid, err := checkOffsetBudget([]int{0, 1, 2}, fixture.edges, o, budget)
	if err != nil || !valid {
		t.Fatalf("clock admission: %v, %v", valid, err)
	}
	baseline, err := projectGroupWithClocks(fixture.episodes, []int{0, 1, 2}, fixture.ranges, fixture.edges, clocks, o, budget)
	if err != nil || baseline == nil || baseline.Status != Review {
		t.Fatalf("identical-map fixture must remain Review: %#v, %v", baseline, err)
	}
	for range baseline.Members {
		if err := budget.spend(); err != nil {
			t.Fatal(err)
		}
	}
	refined, err := refineCliqueClocks([]int{0, 1, 2}, fixture.edges, clocks, o, budget)
	if err != nil || !maps.Equal(clocks, refined) {
		t.Fatalf("fixture clocks must be identical: %#v, %v", refined, err)
	}
	limit := budget.used + 3 // Comparing the three clocks must finish the call.
	got, err := projectGroup(fixture.episodes, []int{0, 1, 2}, fixture.ranges, fixture.edges, o, &workBudget{ctx: context.Background(), limit: limit})
	if err != nil || !reflect.DeepEqual(got, baseline) {
		t.Fatalf("an identical mapping was projected again: %#v, %v", got, err)
	}
}

func clockProjectionSecondAttemptStart(t *testing.T, fixture clockProjectionFixtureData) int64 {
	t.Helper()
	o := DefaultOptions()
	budget := audioEvidenceBudget()
	clocks, valid, err := checkOffsetBudget([]int{0, 1, 2}, fixture.edges, o, budget)
	if err != nil || !valid {
		t.Fatalf("clock admission: %v, %v", valid, err)
	}
	baseline, err := projectGroupWithClocks(fixture.episodes, []int{0, 1, 2}, fixture.ranges, fixture.edges, clocks, o, budget)
	if err != nil || baseline == nil || baseline.Status != Review {
		t.Fatalf("late-error fixture must first produce a complete Review: %#v, %v", baseline, err)
	}
	for range baseline.Members {
		if err := budget.spend(); err != nil {
			t.Fatal(err)
		}
	}
	refined, err := refineCliqueClocks([]int{0, 1, 2}, fixture.edges, clocks, o, budget)
	if err != nil || maps.Equal(clocks, refined) {
		t.Fatalf("late-error fixture needs a distinct second mapping: %#v, %v", refined, err)
	}
	return budget.used + 3
}

func TestGroupProjectionSecondAttemptBudgetFailureDiscardsEarlierReview(t *testing.T) {
	fixture := clockProjectionFixture(t, clockProjectionConfig{alternating: true, weakBaseline: true})
	start := clockProjectionSecondAttemptStart(t, fixture)
	budget := &workBudget{ctx: context.Background(), limit: start + int64(len(fixture.ranges)) + 1}
	group, err := projectGroup(fixture.episodes, []int{0, 1, 2}, fixture.ranges, fixture.edges, DefaultOptions(), budget)
	if !errors.Is(err, ErrLimit) || group != nil || budget.used <= start {
		t.Fatalf("second-attempt exhaustion returned a prior Review or reset its budget: %#v, %v, used %d", group, err, budget.used)
	}
}

type cancelAtClockProjectionBudget struct {
	context.Context
	budget *workBudget
	after  int64
	cancel context.CancelFunc
}

func (c *cancelAtClockProjectionBudget) Err() error {
	if c.budget != nil && c.budget.used > c.after {
		c.cancel()
	}
	return c.Context.Err()
}

func TestGroupProjectionSecondAttemptCancellationDiscardsEarlierReview(t *testing.T) {
	fixture := clockProjectionFixture(t, clockProjectionConfig{alternating: true, weakBaseline: true})
	start := clockProjectionSecondAttemptStart(t, fixture)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	controlled := &cancelAtClockProjectionBudget{Context: ctx, after: start, cancel: cancel}
	budget := &workBudget{ctx: controlled, limit: DefaultOptions().MaxComparisons}
	controlled.budget = budget
	group, err := projectGroup(fixture.episodes, []int{0, 1, 2}, fixture.ranges, fixture.edges, DefaultOptions(), budget)
	if !errors.Is(err, context.Canceled) || group != nil || budget.used <= start {
		t.Fatalf("second-attempt cancellation returned a prior Review or reset its context: %#v, %v, used %d", group, err, budget.used)
	}
}

func TestGroupProjectionCacheKeyRetainsVisualPhase(t *testing.T) {
	edges := hypothesisTestEdges()
	ranges := map[int]Interval{2: {0, 40 * TicksPerSecond}, 5: {0, 40 * TicksPerSecond}, 7: {0, 40 * TicksPerSecond}}
	before, err := groupProjectionKey([]int{2, 5, 7}, ranges, edges, audioEvidenceBudget())
	if err != nil {
		t.Fatal(err)
	}
	edge := edges[[2]int{5, 7}]
	edge.visualPhase++
	edges[[2]int{5, 7}] = edge
	after, err := groupProjectionKey([]int{2, 5, 7}, ranges, edges, audioEvidenceBudget())
	if err != nil || before == after {
		t.Fatalf("distinct complete visual mappings shared a projection cache entry: %q, %q, %v", before, after, err)
	}
	permuted, err := groupProjectionKey([]int{7, 2, 5}, ranges, edges, audioEvidenceBudget())
	if err != nil || permuted != after {
		t.Fatalf("member order changed the complete visual witness identity: %q, %q, %v", after, permuted, err)
	}
}

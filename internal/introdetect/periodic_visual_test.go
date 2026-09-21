package introdetect

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func periodicVisualTestSamples(stateAt func(int) int) ([]VisualSample, []int) {
	states := [...]uint64{0xaaaaaaaaaaaaaaaa, 0xcccccccccccccccc, 0xf0f0f0f0f0f0f0f0, 0xff00ff00ff00ff00}
	samples, ids := make([]VisualSample, 49), make([]int, 49)
	for second := range samples {
		state := stateAt(second)
		samples[second] = VisualSample{Ticks: int64(second) * TicksPerSecond, Hash: states[state], Contrast: 200}
		ids[second] = state
	}
	return samples, ids
}

func periodicVisualTest(t *testing.T, samples []VisualSample, ids []int) bool {
	t.Helper()
	o := DefaultOptions()
	budget := &workBudget{ctx: context.Background(), limit: o.MaxComparisons}
	beforeSamples, beforeIDs := append([]VisualSample(nil), samples...), append([]int(nil), ids...)
	repeated, err := periodicVisualEvidence(samples, ids, 0, len(samples), o, budget)
	if err != nil {
		t.Fatalf("inspect visual repetition: %v", err)
	}
	if !reflect.DeepEqual(samples, beforeSamples) || !reflect.DeepEqual(ids, beforeIDs) {
		t.Fatal("period detection mutated the caller's actual observations")
	}
	return repeated
}

func TestPeriodicVisualEvidenceRetainsFourStateCycleAcrossMissingObservations(t *testing.T) {
	for _, kind := range []string{"complete", "missing_frame", "low_contrast", "unknown_state"} {
		t.Run(kind, func(t *testing.T) {
			samples, ids := periodicVisualTestSamples(func(second int) int { return second / 2 % 4 })
			switch kind {
			case "missing_frame":
				// PTS remains 24s -> 26s; removing a row must not compress time.
				samples = append(samples[:25], samples[26:]...)
				ids = append(ids[:25], ids[26:]...)
			case "low_contrast":
				samples[25].Hash, samples[25].Contrast = 0, 0
				ids[25] = -1
			case "unknown_state":
				ids[25] = -1
			}
			if !periodicVisualTest(t, samples, ids) {
				t.Fatal("an observation hole erased the repeated four-state sequence")
			}
		})
	}
}

func TestPeriodicVisualEvidenceDoesNotInventPeriodForSlowDistinctScenes(t *testing.T) {
	samples, ids := periodicVisualTestSamples(func(second int) int { return min(second/12, 3) })
	if periodicVisualTest(t, samples, ids) {
		t.Fatal("four slow nonrepeating scenes were called a temporal cycle")
	}
}

func TestPeriodicVisualEvidenceRequiresOneOffsetForTheWholeObservedSequence(t *testing.T) {
	for _, kind := range []string{"reordered_states", "observable_contradiction"} {
		t.Run(kind, func(t *testing.T) {
			samples, ids := periodicVisualTestSamples(func(second int) int { return second / 2 % 4 })
			if kind == "reordered_states" {
				order := [...]int{0, 1, 2, 3, 0, 2, 1, 3, 0, 3, 2, 1}
				samples, ids = periodicVisualTestSamples(func(second int) int { return order[min(second/4, len(order)-1)] })
			} else {
				// This is an observed different state, not a missing comparison.
				ids[25], samples[25].Hash = 2, 0xf0f0f0f0f0f0f0f0
			}
			if periodicVisualTest(t, samples, ids) {
				t.Fatal("choosing different state recurrences concealed an observed contradiction")
			}
		})
	}
}

func TestPeriodicVisualEvidenceRequiresTheFullStateSetAndRepeatedSupport(t *testing.T) {
	samples, ids := periodicVisualTestSamples(func(second int) int { return second / 2 % 3 })
	if periodicVisualTest(t, samples, ids) {
		t.Fatal("a three-state sequence satisfied the four-state evidence policy")
	}
	// One traversal, even with all four states, cannot prove their recurrence.
	samples, ids = periodicVisualTestSamples(func(second int) int { return second / 2 % 4 })
	if periodicVisualTest(t, samples[:8], ids[:8]) {
		t.Fatal("one traversal was promoted to repeated visual evidence")
	}
}

func TestPeriodicVisualEvidencePropagatesTheComparisonBudget(t *testing.T) {
	samples, ids := periodicVisualTestSamples(func(second int) int { return second / 2 % 4 })
	o := DefaultOptions()
	budget := &workBudget{ctx: context.Background(), limit: 1}
	repeated, err := periodicVisualEvidence(samples, ids, 0, len(samples), o, budget)
	if repeated || !errors.Is(err, ErrLimit) {
		t.Fatalf("comparison exhaustion was silently treated as no cycle: repeated=%v err=%v", repeated, err)
	}
}

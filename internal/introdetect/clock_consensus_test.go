package introdetect

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func clockConsensusFixture(crossOffset int64) map[[2]int]pairMatch {
	return map[[2]int]pairMatch{
		{0, 1}: {left: 0, right: 1, offset: 10, phaseGrouped: true, phaseAnchorOffset: 10, phaseClass: "phase-01"},
		{0, 2}: {left: 0, right: 2, offset: -10, phaseGrouped: true, phaseAnchorOffset: -10, phaseClass: "phase-02"},
		{0, 3}: {left: 0, right: 3, offset: 20, phaseGrouped: true, phaseAnchorOffset: 20, phaseClass: "phase-03"},
		// Store this edge in the reverse direction, including its phase anchor.
		{1, 2}: {left: 2, right: 1, offset: -crossOffset, phaseGrouped: true, phaseAnchorOffset: -crossOffset, phaseClass: "phase-12"},
		{1, 3}: {left: 1, right: 3, offset: 10, phaseGrouped: true, phaseAnchorOffset: 10, phaseClass: "phase-13"},
		{2, 3}: {left: 2, right: 3, offset: 30, phaseGrouped: true, phaseAnchorOffset: 30, phaseClass: "phase-23"},
	}
}

func TestCliqueClockConsensusUsesEveryDirectedEdgeDeterministically(t *testing.T) {
	o := DefaultOptions()
	o.AudioAlignmentTicks = 2
	for _, example := range []struct {
		name  string
		cross int64
		want  map[int]int64
	}{
		{"exact_clocks", -20, map[int]int64{0: 0, 1: 10, 2: -10, 3: 20}},
		{"half_ticks_away_from_zero", -22, map[int]int64{0: 0, 1: 11, 2: -11, 3: 20}},
		// Rounding only the correction would incorrectly produce 9 and -9.
		{"opposite_sign_half_corrections", -18, map[int]int64{0: 0, 1: 10, 2: -10, 3: 20}},
	} {
		t.Run(example.name, func(t *testing.T) {
			for _, selected := range [][]int{{0, 1, 2, 3}, {3, 1, 0, 2}, {2, 0, 3, 1}} {
				edges := clockConsensusFixture(example.cross)
				beforeEdges := clockConsensusFixture(example.cross)
				beforeSelected := append([]int(nil), selected...)
				admitted, ok := consistentOffsets(selected, edges, o)
				if !ok {
					t.Fatal("fixture must satisfy the original reference-clock admission")
				}
				beforeClocks := map[int]int64{0: 0, 1: 10, 2: -10, 3: 20}
				got, err := refineCliqueClocks(selected, edges, admitted, o, &workBudget{ctx: context.Background(), limit: o.MaxComparisons})
				if err != nil || !reflect.DeepEqual(got, example.want) {
					t.Fatalf("whole-clique clocks = %#v, %v; want %#v", got, err, example.want)
				}
				if !reflect.DeepEqual(selected, beforeSelected) || !reflect.DeepEqual(edges, beforeEdges) || !reflect.DeepEqual(admitted, beforeClocks) {
					t.Fatal("refinement mutated its admitted witness or immutable phase data")
				}
			}
		})
	}
}

func TestCliqueClockConsensusDoesNotUseQualificationOrVisualMetrics(t *testing.T) {
	o := DefaultOptions()
	o.AudioAlignmentTicks = 2
	selected := []int{0, 1, 2, 3}
	edges := clockConsensusFixture(-22)
	for key, edge := range edges {
		edge.metrics = Metrics{AudioAgreementPermille: 1, VisualMinBandMatchedPermille: 0, VisualMatchedTimePermille: 0}
		edge.reasons = []Reason{WeakAudioEvidence, InsufficientVisualAnchors, PeriodicVisualEvidence}
		edge.a, edge.b = Interval{7, 19}, Interval{101, 113}
		edges[key] = edge
	}
	admitted, ok := consistentOffsets(selected, edges, o)
	if !ok {
		t.Fatal("fixture must satisfy the original clock admission")
	}
	got, err := refineCliqueClocks(selected, edges, admitted, o, &workBudget{ctx: context.Background(), limit: o.MaxComparisons})
	want := map[int]int64{0: 0, 1: 11, 2: -11, 3: 20}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("quality facts altered the constant-clock consensus: %#v, %v", got, err)
	}
	for _, edge := range edges {
		if !reflect.DeepEqual(edge.reasons, []Reason{WeakAudioEvidence, InsufficientVisualAnchors, PeriodicVisualEvidence}) {
			t.Fatal("clock refinement discarded the witness's eligibility facts")
		}
	}
}

func TestCliqueClockConsensusPreservesInclusivePhaseAnchorBoundary(t *testing.T) {
	o := DefaultOptions()
	o.AudioAlignmentTicks = 2
	selected := []int{0, 1, 2, 3}
	for _, example := range []struct {
		name   string
		anchor int64
		want   map[int]int64
	}{
		{"at_tolerance", 9, map[int]int64{0: 0, 1: 11, 2: -11, 3: 20}},
		{"outside_tolerance", 8, map[int]int64{0: 0, 1: 10, 2: -10, 3: 20}},
	} {
		t.Run(example.name, func(t *testing.T) {
			edges := clockConsensusFixture(-22)
			edge := edges[[2]int{0, 1}]
			edge.phaseAnchorOffset = example.anchor
			edges[[2]int{0, 1}] = edge
			admitted, ok := consistentOffsets(selected, edges, o)
			if !ok {
				t.Fatal("fixture must satisfy the original clock admission")
			}
			got, err := refineCliqueClocks(selected, edges, admitted, o, &workBudget{ctx: context.Background(), limit: o.MaxComparisons})
			if err != nil || !reflect.DeepEqual(got, example.want) {
				t.Fatalf("phase-constrained clocks = %#v, %v; want %#v", got, err, example.want)
			}
			if !reflect.DeepEqual(edges[[2]int{0, 1}], edge) {
				t.Fatal("refinement moved the phase anchor instead of preserving it")
			}
		})
	}
}

func TestCliqueClockConsensusKeepsOriginalClocksWhenAnEdgeWouldExceedTolerance(t *testing.T) {
	o := DefaultOptions()
	o.AudioAlignmentTicks = 5
	selected := []int{0, 1, 2, 3, 4}
	edges := make(map[[2]int]pairMatch)
	for left := 0; left < len(selected); left++ {
		for right := left + 1; right < len(selected); right++ {
			edges[[2]int{left, right}] = pairMatch{left: left, right: right}
		}
	}
	for key, offset := range map[[2]int]int64{{1, 2}: 5, {1, 3}: -5, {1, 4}: -5, {2, 3}: 5, {2, 4}: 5} {
		edge := edges[key]
		edge.offset = offset
		edges[key] = edge
	}
	admitted, ok := consistentOffsets(selected, edges, o)
	if !ok {
		t.Fatal("every original edge residual is within the admitted tolerance")
	}
	// The one equal-weight solution moves clocks 1 and 2 to +1 and -1. Its
	// edge 1->2 residual is 7, so it cannot replace the admitted zero clocks.
	got, err := refineCliqueClocks(selected, edges, admitted, o, &workBudget{ctx: context.Background(), limit: o.MaxComparisons})
	if err != nil || !reflect.DeepEqual(got, admitted) {
		t.Fatalf("out-of-bound edge was averaged away: %#v, %v", got, err)
	}
}

func TestCliqueClockConsensusCannotAdmitAContradictoryOrIncompleteWitness(t *testing.T) {
	o := DefaultOptions()
	selected := []int{2, 5, 7}
	for _, kind := range []string{"contradictory_triangle", "missing_edge", "wrong_direction"} {
		t.Run(kind, func(t *testing.T) {
			edges := hypothesisTestEdges()
			switch kind {
			case "contradictory_triangle":
				edge := edges[[2]int{5, 7}]
				edge.offset += o.AudioAlignmentTicks + 1
				edges[[2]int{5, 7}] = edge
			case "missing_edge":
				delete(edges, [2]int{5, 7})
			case "wrong_direction":
				edge := edges[[2]int{5, 7}]
				edge.left = 2
				edges[[2]int{5, 7}] = edge
			}
			if _, ok := consistentOffsets(selected, edges, o); ok {
				t.Fatal("fixture must fail the original complete-clique admission")
			}
			admitted := map[int]int64{2: 0, 5: 12 * TicksPerSecond, 7: -8 * TicksPerSecond}
			got, err := refineCliqueClocks(selected, edges, admitted, o, &workBudget{ctx: context.Background(), limit: o.MaxComparisons})
			if !errors.Is(err, ErrInvalidInput) || got != nil {
				t.Fatalf("refinement admitted an invalid witness: %#v, %v", got, err)
			}
			group, err := projectGroup(nil, selected, nil, edges, o, &workBudget{ctx: context.Background(), limit: o.MaxComparisons})
			if err != nil || group != nil {
				t.Fatalf("projection bypassed its original admission gate: %#v, %v", group, err)
			}
		})
	}
}

func TestCliqueClockConsensusUsesCheckedArithmeticAtSignedEndpoints(t *testing.T) {
	o := DefaultOptions()
	selected := []int{0, 1, 2}
	for _, clocks := range []map[int]int64{
		{0: 0, 1: 1<<63 - 3, 2: 1<<63 - 2},
		{0: 0, 1: -1<<63 + 2, 2: -1<<63 + 3},
	} {
		edges := map[[2]int]pairMatch{
			{0, 1}: {left: 0, right: 1, offset: clocks[1]},
			{0, 2}: {left: 0, right: 2, offset: clocks[2]},
			{1, 2}: {left: 1, right: 2, offset: 1},
		}
		if _, ok := consistentOffsets(selected, edges, o); !ok {
			t.Fatal("coherent endpoint fixture must be admitted")
		}
		got, err := refineCliqueClocks(selected, edges, clocks, o, &workBudget{ctx: context.Background(), limit: o.MaxComparisons})
		if err != nil || !reflect.DeepEqual(got, clocks) {
			t.Fatalf("large absolute offsets changed despite zero residuals: %#v, %v", got, err)
		}
	}
	for _, example := range []struct {
		base, numerator, divisor int64
		want                     int64
		valid                    bool
	}{
		{1<<63 - 1, 1, 2, 0, false},
		{-1 << 63, -1, 2, 0, false},
		{1<<63 - 1, -1, 2, 1<<63 - 1, true},
		{-1 << 63, 1, 2, -1 << 63, true},
	} {
		got, valid := roundConsensusClock(example.base, example.numerator, example.divisor)
		if valid != example.valid || valid && got != example.want {
			t.Fatalf("signed-endpoint rounding = %d, %v; want %d, %v", got, valid, example.want, example.valid)
		}
	}
	clocks := map[int]int64{0: 0, 1: 1<<63 - 2, 2: 1<<63 - 1}
	edges := map[[2]int]pairMatch{
		{0, 1}: {left: 0, right: 1, offset: clocks[1]},
		{0, 2}: {left: 0, right: 2, offset: clocks[2]},
		{1, 2}: {left: 1, right: 2, offset: -5},
	}
	if _, ok := consistentOffsets(selected, edges, o); !ok {
		t.Fatal("small endpoint residual must pass original admission")
	}
	got, err := refineCliqueClocks(selected, edges, clocks, o, &workBudget{ctx: context.Background(), limit: o.MaxComparisons})
	if err != nil || !reflect.DeepEqual(got, clocks) {
		t.Fatalf("overflowing refinement did not retain the complete admitted mapping: %#v, %v", got, err)
	}
}

func TestCliqueClockConsensusCancellationAndBudgetReturnNoMapping(t *testing.T) {
	o := DefaultOptions()
	o.AudioAlignmentTicks = 2
	selected := []int{0, 1, 2, 3}
	edges := clockConsensusFixture(-22)
	admitted, ok := consistentOffsets(selected, edges, o)
	if !ok {
		t.Fatal("fixture must satisfy the original clock admission")
	}
	for _, limit := range []int64{0, 5, 15, 19} {
		got, err := refineCliqueClocks(selected, edges, admitted, o, &workBudget{ctx: context.Background(), limit: limit})
		if !errors.Is(err, ErrLimit) || got != nil {
			t.Fatalf("budget %d exposed a partial or fallback mapping: %#v, %v", limit, got, err)
		}
	}
	for _, after := range []int{1, 2} {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		controlled := &cancelDuringContext{Context: ctx, cancel: cancel, after: after}
		got, err := refineCliqueClocks(selected, edges, admitted, o, &workBudget{ctx: controlled, limit: o.MaxComparisons})
		if !errors.Is(err, context.Canceled) || got != nil {
			t.Fatalf("cancellation at check %d exposed a mapping: %#v, %v", after, got, err)
		}
	}
}

package introdetect

import (
	"context"
	"reflect"
	"testing"
)

func TestCalibratedBoundsPreserveExhaustiveGeometryEstimate(t *testing.T) {
	cohort, _ := calibratedSequenceFixture()
	identity := calibratedGeometry{ScaleYPermille: 1000}
	viewBudget := &workBudget{ctx: context.Background(), limit: DefaultOptions().MaxComparisons}
	anchor, err := calibratedView(cohort.Episodes[0], identity, viewBudget)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range cohort.Episodes[1:] {
		neutral, err := calibratedView(source, identity, viewBudget)
		if err != nil {
			t.Fatal(err)
		}
		baselineBudget := &workBudget{ctx: context.Background(), limit: DefaultOptions().MaxComparisons}
		baseline, baselineOK, err := calibratedExhaustiveEstimateForTest(anchor, source, neutral, baselineBudget)
		if err != nil {
			t.Fatal(err)
		}
		boundedBudget := &workBudget{ctx: context.Background(), limit: DefaultOptions().MaxComparisons}
		cache := newCalibratedViewCache()
		bounded, boundedOK, err := calibratedBoundedEstimate(anchor, source, neutral, boundedBudget, cache)
		if err != nil {
			t.Fatal(err)
		}
		if baselineOK != boundedOK || baselineOK && !reflect.DeepEqual(baseline, bounded) {
			t.Fatalf("bounds changed the winning whole-prefix estimate: exhaustive=%+v bounded=%+v", baseline, bounded)
		}
		before := boundedBudget.used
		reused, reusedOK, err := calibratedBoundedEstimate(anchor, source, neutral, boundedBudget, cache)
		if err != nil || boundedOK != reusedOK || !reflect.DeepEqual(bounded, reused) {
			t.Fatal("view cache changed a measured estimate", err)
		}
		if boundedBudget.used-before >= before {
			t.Fatal("repeated source view construction was not avoided")
		}
	}
}

func TestCalibratedBoundsNeverExcludeAnActualGridMatch(t *testing.T) {
	cohort, _ := calibratedSequenceFixture()
	budget := &workBudget{ctx: context.Background(), limit: DefaultOptions().MaxComparisons}
	set, err := newCalibratedViewCache().get(cohort.Episodes[0], budget)
	if err != nil {
		t.Fatal(err)
	}
	// The query view belongs to another source and uses a geometry between
	// grid points, so it is not itself one of the values in the target box.
	query, err := calibratedView(cohort.Episodes[1], calibratedGeometry{ScaleYPermille: 970, ShiftXPermille: -15, ShiftYPermille: 15}, budget)
	if err != nil {
		t.Fatal(err)
	}
	confirmed := 0
	for i, bound := range set.bounds {
		if i+7 >= len(query.Visual) {
			break
		}
		sample := query.Visual[i+7]
		for _, view := range set.views {
			if !sequenceClose(sample, view.Visual[i]) {
				continue
			}
			confirmed++
			possible, lower := calibratedPotentialMatch(sample, bound)
			actual, _ := sequenceDistance(sample, view.Visual[i])
			if !possible || lower > int64(actual) {
				t.Fatal("a cross-source geometry match was pruned or overestimated")
			}
		}
	}
	if confirmed == 0 {
		t.Fatal("fixture supplied no actual cross-source geometry matches")
	}
}

// This estimates one geometry for the complete source prefix, not separate
// views for pairs, frames, states, or candidate interiors. All grid points and
// nominated phases are scored before the deterministic winner is retained.
func calibratedExhaustiveEstimateForTest(anchor, source, neutral Episode, budget *workBudget) (calibratedEstimate, bool, error) {
	anchors := calibratedAnchors(anchor)
	offsets, err := calibratedOffsets(anchors, calibratedAnchors(neutral), budget)
	if err != nil {
		return calibratedEstimate{}, false, err
	}
	if len(offsets) == 0 {
		return calibratedEstimate{}, false, nil
	}
	phases := calibratedPhases(offsets)
	best := calibratedEstimate{distance: 1 << 62}
	for _, geometry := range calibratedGeometryGrid() {
		view, err := calibratedView(source, geometry, budget)
		if err != nil {
			return calibratedEstimate{}, false, err
		}
		for _, offset := range phases {
			votes, distance, j := 0, int64(0), 0
			for i, sample := range anchors.Visual {
				if i%2 != 0 {
					continue
				}
				if !calibratedMoving(anchors.Visual, i) {
					continue
				}
				if err := budget.spend(); err != nil {
					return calibratedEstimate{}, false, err
				}
				target := sample.Ticks + offset
				for j+1 < len(view.Visual) && absolute(view.Visual[j+1].Ticks-target) <= absolute(view.Visual[j].Ticks-target) {
					j++
				}
				if j >= len(view.Visual) || absolute(view.Visual[j].Ticks-target) > visualSequenceResidual {
					continue
				}
				if sequenceClose(sample, view.Visual[j]) {
					votes++
					sum, _ := sequenceDistance(sample, view.Visual[j])
					distance += int64(sum)
				}
			}
			if votes > best.votes || votes == best.votes && distance < best.distance {
				best = calibratedEstimate{geometry, view, votes, distance, offset}
			}
		}
	}
	return best, best.votes >= 3, nil
}

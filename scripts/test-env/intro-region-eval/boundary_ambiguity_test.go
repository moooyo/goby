package main

import (
	"reflect"
	"testing"
)

func makeBoundaryClosureFixture(t *testing.T, ranges [][2]int) ([3]view, appearanceMatrix, appearanceMatrix, appearanceMatrix, clockHypothesis, *budget) {
	t.Helper()
	work := &budget{}
	var views [3]view
	clock := clockHypothesis{Scale: 1, Mapping: make([]int, 420)}
	for source := range views {
		views[source] = view{ID: []string{"A", "B", "C"}[source], Geometry: neutral(), Frames: make([]frame, 420)}
		for i := range views[source].Frames {
			clock.Mapping[i] = i
			f := frame{PTS: float64(i) / 10, Previous: i - 5}
			if i < 5 {
				f.Previous = -1
			}
			shared := false
			for _, interval := range ranges {
				shared = shared || i >= interval[0] && i < interval[1]
			}
			for patch := range f.Patches {
				seed := uint32(0x17351 + i*991 + patch*337)
				if !shared {
					seed ^= uint32(source+1) * 0x13517
				}
				f.Patches[patch] = boundaryDescriptor(seed)
			}
			views[source].Frames[i] = f
		}
	}
	matrix := func(a, b view) appearanceMatrix {
		result, err := makeMatrix(a, b, work)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	return views, matrix(views[0], views[1]), matrix(views[0], views[2]), matrix(views[1], views[2]), clock, work
}

func TestObservedBoundaryLongComponentIsNotSplitByNominalSubwindows(t *testing.T) {
	views, ab, ac, bc, clock, work := makeBoundaryClosureFixture(t, [][2]int{{50, 250}})
	groups, summary, err := closeGroups(views, ab, ac, bc, []clockHypothesis{clock}, []clockHypothesis{clock}, work)
	if err != nil || summary.CohortBoundaryAmbiguous || len(groups) != 1 {
		t.Fatalf("one continuous component was mistaken for multiple windows: groups=%d summary=%+v err=%v", len(groups), summary, err)
	}
	if groups[0].AnchorStart < 5 || groups[0].AnchorEnd > 24.9 || groups[0].AnchorEnd-groups[0].AnchorStart < 8 {
		t.Fatalf("long component lost containment: %+v", groups[0])
	}
}

func TestObservedBoundaryKeepsDisjointCounterevidenceAcrossClocks(t *testing.T) {
	views, ab, ac, bc, clock, work := makeBoundaryClosureFixture(t, [][2]int{{50, 170}, {230, 350}})
	// The second legal stage-level mapping exposes only the later component.
	// Dropping the first clock's ambiguity must not make that result adoptable.
	alternative := clockHypothesis{Scale: 1, Offset: .05, Mapping: append([]int(nil), clock.Mapping...)}
	for i := 0; i < 200; i++ {
		alternative.Mapping[i] = -1
	}
	groups, summary, err := closeGroups(views, ab, ac, bc, []clockHypothesis{clock, alternative}, []clockHypothesis{clock}, work)
	if err != nil || len(groups) != 0 || !summary.CohortBoundaryAmbiguous || summary.BoundaryAmbiguousClocks < 1 || summary.BoundaryAmbiguity == nil {
		t.Fatalf("disjoint complete counterevidence was dropped: groups=%d summary=%+v err=%v", len(groups), summary, err)
	}
	if len(summary.RejectedAmbiguousGroups) == 0 {
		t.Fatal("the alternative clock did not expose the intended otherwise-adoptable witness")
	}
	if summary.BoundaryAmbiguity.Witnesses[0].ComponentIndices[1] >= summary.BoundaryAmbiguity.Witnesses[1].ComponentIndices[0] {
		t.Fatal("ambiguity record does not retain both disjoint components")
	}
}

func TestObservedBoundaryDominanceMatchesCompleteAudit(t *testing.T) {
	views, ab, ac, bc, clock, initialWork := makeBoundaryClosureFixture(t, [][2]int{{50, 170}})
	baselineWork, optimizedWork := *initialWork, *initialWork
	baseline, baselineSummary, err := closeGroupsWithBoundaryDominance(views, ab, ac, bc, []clockHypothesis{clock}, []clockHypothesis{clock}, &baselineWork, false)
	if err != nil {
		t.Fatal(err)
	}
	optimized, optimizedSummary, err := closeGroupsWithBoundaryDominance(views, ab, ac, bc, []clockHypothesis{clock}, []clockHypothesis{clock}, &optimizedWork, true)
	if err != nil {
		t.Fatal(err)
	}
	for i := range baseline {
		baseline[i].PassingWindows = 0
	}
	for i := range optimized {
		optimized[i].PassingWindows = 0
	}
	if len(baseline) == 0 || !reflect.DeepEqual(baseline, optimized) || baselineSummary.CohortBoundaryAmbiguous != optimizedSummary.CohortBoundaryAmbiguous || optimizedSummary.BoundaryDominatedWindows == 0 || optimizedWork.ClockLookups >= baselineWork.ClockLookups {
		t.Fatalf("dominance changed witnesses or failed to avoid repeated audits: baseline=%+v optimized=%+v", baselineSummary, optimizedSummary)
	}
}

func TestObservedBoundaryKeepsNonAnchorSourceCounterevidence(t *testing.T) {
	views, _, ac, _, clock, work := makeBoundaryClosureFixture(t, [][2]int{{50, 170}})
	for i := 50; i < 170; i++ {
		views[1].Frames[i+180].Patches = views[0].Frames[i].Patches
	}
	ab, err := makeMatrix(views[0], views[1], work)
	if err != nil {
		t.Fatal(err)
	}
	bc, err := makeMatrix(views[1], views[2], work)
	if err != nil {
		t.Fatal(err)
	}
	alternative := clockHypothesis{Scale: 1, Offset: 18}
	alternative.Mapping, err = affineMapping(views[0], views[1], 1, 18, work)
	if err != nil {
		t.Fatal(err)
	}
	groups, summary, err := closeGroups(views, ab, ac, bc, []clockHypothesis{clock, alternative}, []clockHypothesis{clock}, work)
	if err != nil || len(groups) != 0 || !summary.CohortBoundaryAmbiguous || summary.BoundaryAmbiguity == nil {
		t.Fatalf("non-anchor source alternatives were silently chosen: groups=%d summary=%+v err=%v", len(groups), summary, err)
	}
	left, right := summary.BoundaryAmbiguity.Witnesses[0], summary.BoundaryAmbiguity.Witnesses[1]
	if summary.BoundaryAmbiguity.SourceIndex != 1 || left.ComponentSourceBounds[0] != right.ComponentSourceBounds[0] || left.SourceBounds[1][1] > right.SourceBounds[1][0] && right.SourceBounds[1][1] > left.SourceBounds[1][0] {
		t.Fatalf("counterexample did not retain equal anchor component and disjoint B intervals: left=%v right=%v source=%d", left.ComponentSourceBounds, right.ComponentSourceBounds, summary.BoundaryAmbiguity.SourceIndex)
	}
}

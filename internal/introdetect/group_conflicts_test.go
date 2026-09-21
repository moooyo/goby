package introdetect

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

func conflictTestGroup(phase int64) Group {
	group := hypothesisTestGroup()
	group.ID = fmt.Sprintf("phase-%d", phase)
	group.alignmentOffsets = []int64{0, 25*TicksPerSecond + phase, 55*TicksPerSecond + 2*phase}
	group.phaseAnchors = map[[2]string]int64{
		{"source-a", "source-b"}: group.alignmentOffsets[1],
		{"source-a", "source-c"}: group.alignmentOffsets[2],
		{"source-b", "source-c"}: group.alignmentOffsets[2] - group.alignmentOffsets[1],
	}
	return group
}

func TestMarkGroupConflictsFreezesEligibilityForAllThreePhases(t *testing.T) {
	o := DefaultOptions()
	for _, permutation := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		groups := []Group{
			conflictTestGroup(int64(permutation[0]) * TicksPerSecond),
			conflictTestGroup(int64(permutation[1]) * TicksPerSecond),
			conflictTestGroup(int64(permutation[2]) * TicksPerSecond),
		}
		beforeIDs := []string{groups[0].ID, groups[1].ID, groups[2].ID}
		for _, group := range groups {
			if !hypothesisQualifiedGroup(group, o) {
				t.Fatal("phase fixture must begin with complete qualified evidence")
			}
		}
		budget := &workBudget{ctx: context.Background(), limit: o.MaxComparisons}
		if err := markGroupConflicts(groups, o, budget); err != nil {
			t.Fatalf("mark competing phases: %v", err)
		}
		for index, group := range groups {
			if !slices.Contains(group.Reasons, CompetingIntervals) || hypothesisQualifiedGroup(group, o) {
				t.Fatalf("an earlier conflict concealed a later phase: order=%v group=%#v", permutation, group)
			}
			if group.Status != Qualified || group.ID != beforeIDs[index] {
				t.Fatal("conflict marking finalized status or changed group identity")
			}
		}
	}
}

func TestMarkGroupConflictsDoesNotLetAWeakPhaseContaminateStrongEvidence(t *testing.T) {
	o := DefaultOptions()
	strong, weak := conflictTestGroup(0), conflictTestGroup(TicksPerSecond)
	weak.Status, weak.Reasons = Review, []Reason{WeakVisualEvidence}
	weak.Metrics.VisualMatchedTimePermille = 840
	weak.Metrics.VisualContradictedTimePermille = 110
	groups := []Group{strong, weak}
	budget := &workBudget{ctx: context.Background(), limit: o.MaxComparisons}
	if err := markGroupConflicts(groups, o, budget); err != nil {
		t.Fatalf("compare weak and strong phases: %v", err)
	}
	if !reflect.DeepEqual(groups, []Group{strong, weak}) || !hypothesisQualifiedGroup(groups[0], o) {
		t.Fatal("an ineligible alternate phase added an unsupported conflict")
	}
}

func TestMarkGroupConflictsRetainsRealIntervalConflictsDespiteWeakEvidence(t *testing.T) {
	o := DefaultOptions()
	strong, weak := conflictTestGroup(0), conflictTestGroup(0)
	weak.Status, weak.Reasons = Review, []Reason{WeakVisualEvidence}
	for index := range weak.Members {
		weak.Members[index].Interval.StartTicks += 100 * TicksPerSecond
		weak.Members[index].Interval.EndTicks += 100 * TicksPerSecond
	}
	groups := []Group{strong, weak}
	budget := &workBudget{ctx: context.Background(), limit: o.MaxComparisons}
	if err := markGroupConflicts(groups, o, budget); err != nil {
		t.Fatalf("mark different repeated intervals: %v", err)
	}
	for _, group := range groups {
		if !slices.Contains(group.Reasons, CompetingIntervals) {
			t.Fatal("a real interval conflict was suppressed by the phase eligibility gate")
		}
	}
	if !slices.Contains(groups[1].Reasons, WeakVisualEvidence) || groups[0].Status != Qualified || groups[1].Status != Review {
		t.Fatal("conflict marking erased an existing reason or changed status")
	}
}

func TestMarkGroupConflictsPropagatesBudgetFailure(t *testing.T) {
	groups := []Group{conflictTestGroup(0), conflictTestGroup(TicksPerSecond)}
	budget := &workBudget{ctx: context.Background(), limit: 1}
	if err := markGroupConflicts(groups, DefaultOptions(), budget); !errors.Is(err, ErrLimit) {
		t.Fatalf("conflict comparisons ignored their budget: %v", err)
	}
}

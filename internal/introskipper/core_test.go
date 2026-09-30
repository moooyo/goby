// SPDX-License-Identifier: GPL-3.0-only

package introskipper

import (
	"context"
	"math"
	"reflect"
	"testing"
)

func TestSampleDurationBinary64(t *testing.T) {
	// Independent evaluation of the pinned two-division expression on test-env
	// yields this binary64 word. This is not a claim of a C# reflection oracle.
	if math.Float64bits(sampleDuration) != 0x3fbfb3f65f17ec9f {
		t.Fatalf("unexpected sample-duration bits: %016x", math.Float64bits(sampleDuration))
	}
}

func filled(count int, point uint32) []uint32 {
	values := make([]uint32, count)
	for i := range values {
		values[i] = point
	}
	return values
}

func TestIndexKeepsFirstKeyOrderAndLastPosition(t *testing.T) {
	a := newAnalyzer(defaultConfiguration())
	index := a.createInvertedIndex(episode{ID: "a", Fingerprint: []uint32{7, 3, 7, 9, 3}})
	if !reflect.DeepEqual(index.keys, []uint32{7, 3, 9}) || index.positions[7] != 2 || index.positions[3] != 4 || index.positions[9] != 3 {
		t.Fatalf("unexpected ordered index: %#v", index)
	}
	// The native per-ID cache reuses the original index even for a later value.
	cached := a.createInvertedIndex(episode{ID: "a", Fingerprint: []uint32{99}})
	if !reflect.DeepEqual(index, cached) {
		t.Fatalf("cache was not reused: %#v", cached)
	}
}

func TestMinimumDurationUsesPointDistanceWithoutExtraHop(t *testing.T) {
	a := newAnalyzer(defaultConfiguration())
	for _, count := range []int{122, 123} {
		left, right := a.findContiguous(filled(count, 0), filled(count, 63), 0)
		if count == 122 && (left.End != 0 || right.End != 0) {
			t.Fatalf("accepted less than 15 seconds: %#v %#v", left, right)
		}
		if count == 123 && (left.Start != 0 || left.End != 122*sampleDuration || right != left) {
			t.Fatalf("did not accept exactly six differing bits: %#v %#v", left, right)
		}
	}
	left, right := a.findContiguous(filled(200, 0), filled(200, 127), 0)
	if left.End != 0 || right.End != 0 {
		t.Fatalf("accepted seven differing bits: %#v %#v", left, right)
	}
}

func TestMaximumTimeSkipUsesGapBetweenMatchedPoints(t *testing.T) {
	a := newAnalyzer(defaultConfiguration())
	for _, gap := range []int{28, 29} {
		leftPoints, rightPoints := filled(180, 0), filled(180, ^uint32(0))
		for position := 0; position < len(rightPoints); position += gap {
			rightPoints[position] = 0
		}
		left, right := a.findContiguous(leftPoints, rightPoints, 0)
		if gap == 28 && (left.Start != 0 || left.End != 168*sampleDuration || right != left) {
			t.Fatalf("28-point gap should remain contiguous: %#v %#v", left, right)
		}
		if gap == 29 && (left.End != 0 || right.End != 0) {
			t.Fatalf("29-point gap must split short runs: %#v %#v", left, right)
		}
	}
}

func TestContiguousTieKeepsEarlierRun(t *testing.T) {
	a := newAnalyzer(defaultConfiguration())
	right := filled(320, ^uint32(0))
	for i := 0; i <= 122; i++ {
		right[i], right[i+180] = 0, 0
	}
	leftRange, rightRange := a.findContiguous(filled(320, 0), right, 0)
	if leftRange.Start != 0 || leftRange.End != 122*sampleDuration || rightRange != leftRange {
		t.Fatalf("equal-length later run replaced the first: %#v %#v", leftRange, rightRange)
	}
}

func TestUnequalLengthsPreserveUpstreamUpperLimit(t *testing.T) {
	a := newAnalyzer(defaultConfiguration())
	left, right := a.findContiguous(filled(200, 0), filled(300, 0), 50)
	if left != (timeRange{0, 149 * sampleDuration}) || right != (timeRange{50 * sampleDuration, 199 * sampleDuration}) {
		t.Fatalf("positive shift changed upstream upper limit: %#v %#v", left, right)
	}
	left, right = a.findContiguous(filled(300, 0), filled(200, 0), -50)
	if left != (timeRange{50 * sampleDuration, 199 * sampleDuration}) || right != (timeRange{0, 149 * sampleDuration}) {
		t.Fatalf("negative shift changed upstream upper limit: %#v %#v", left, right)
	}
}

func TestSelectSharedRegionUsesLeftDurationAndFirstTieBeforeSnap(t *testing.T) {
	lhs, rhs := selectSharedRegion("a", []timeRange{{5, 25}, {50, 70}}, "b", []timeRange{{9, 29}, {0, 100}})
	if lhs != (segment{"a", 0, 25}) || rhs != (segment{"b", 9, 29}) {
		t.Fatalf("tie selection or <=5-second snap changed: %#v %#v", lhs, rhs)
	}
	lhs, rhs = selectSharedRegion("a", []timeRange{{5.000001, 25.000001}}, "b", []timeRange{{-1, 19}})
	if lhs.Start != 5.000001 || rhs.Start != 0 {
		t.Fatalf("snap threshold changed: %#v %#v", lhs, rhs)
	}
}

func TestCandidateReplacementIsStrictlyLonger(t *testing.T) {
	saved := segment{"a", 0, 20}
	if isBetterCandidate(segment{"a", 30, 50}, saved) {
		t.Fatal("equal-duration later candidate must not replace saved candidate")
	}
	if !isBetterCandidate(segment{"a", 30, 51}, saved) {
		t.Fatal("longer candidate must replace saved candidate")
	}
}

func TestIndexShiftWrapsUint32(t *testing.T) {
	a := newAnalyzer(defaultConfiguration())
	// The last exact key contributes shift +1 only through unchecked uint wrap.
	// Hamming comparison still uses the original, unshifted 32-bit words.
	lhs := episode{ID: "a", Fingerprint: filled(200, 0)}
	rhs := episode{ID: "b", Fingerprint: filled(200, 0)}
	lhs.Fingerprint[198] = ^uint32(0)
	rhs.Fingerprint[199] = 1
	lhsRanges, rhsRanges, err := a.searchInvertedIndex(context.Background(), lhs, rhs)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i := range lhsRanges {
		if lhsRanges[i].Start == 0 && rhsRanges[i].Start == sampleDuration {
			found = true
		}
	}
	if !found {
		t.Fatalf("wrapped uint32 key did not contribute shift +1: %#v %#v", lhsRanges, rhsRanges)
	}
}

func TestIndexShiftWrapsUint32BelowZero(t *testing.T) {
	a := newAnalyzer(defaultConfiguration())
	lhs := episode{ID: "a", Fingerprint: filled(200, 17)}
	rhs := episode{ID: "b", Fingerprint: filled(200, 17)}
	lhs.Fingerprint[198] = 0
	rhs.Fingerprint[199] = ^uint32(0)
	lhsRanges, rhsRanges, err := a.searchInvertedIndex(context.Background(), lhs, rhs)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i := range lhsRanges {
		if lhsRanges[i].Start == 0 && rhsRanges[i].Start == sampleDuration {
			found = true
		}
	}
	if !found {
		t.Fatalf("negative uint32 wrap did not contribute shift +1: %#v %#v", lhsRanges, rhsRanges)
	}
}

func TestEqualDurationShiftsKeepFirstInsertion(t *testing.T) {
	left, right := filled(400, 0x55555555), filled(400, 0xaaaaaaaa)
	for i := 0; i <= 150; i++ {
		left[100+i], right[40+i], right[210+i] = 8, 7, 9
	}
	a := newAnalyzer(defaultConfiguration())
	lhsRanges, rhsRanges, err := a.searchInvertedIndex(context.Background(), episode{ID: "a", Fingerprint: left}, episode{ID: "b", Fingerprint: right})
	if err != nil {
		t.Fatal(err)
	}
	if len(lhsRanges) != 2 || len(rhsRanges) != 2 || rhsRanges[0].Start != 40*sampleDuration || rhsRanges[1].Start != 210*sampleDuration {
		t.Fatalf("shift insertion order changed: %#v %#v", lhsRanges, rhsRanges)
	}
	lhs, rhs := selectSharedRegion("a", lhsRanges, "b", rhsRanges)
	if lhs.Start != 100*sampleDuration || lhs.End != 250*sampleDuration || rhs.Start != 0 || rhs.End != 190*sampleDuration {
		t.Fatalf("equal shift replaced earlier inserted candidate: %#v %#v", lhs, rhs)
	}
}

func TestFirstValidPairBreakAndSavedInsertionOrder(t *testing.T) {
	a := newAnalyzer(defaultConfiguration())
	points := filled(200, 17)
	candidates, trace, err := a.findCandidates(context.Background(), []episode{
		{ID: "first", Fingerprint: points}, {ID: "second", Fingerprint: points}, {ID: "third", Fingerprint: points},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(trace) != 2 || trace[0].LHS.ID != "first" || trace[0].RHS.ID != "second" || trace[1].LHS.ID != "second" || trace[1].RHS.ID != "third" {
		t.Fatalf("unexpected pair order/break behavior: %#v", trace)
	}
	if len(candidates) != 3 || candidates[0].ID != "first" || candidates[1].ID != "second" || candidates[2].ID != "third" {
		t.Fatalf("unexpected candidate insertion order: %#v", candidates)
	}
}

func TestMaximumDurationRejectsOnlyAfterStartSnap(t *testing.T) {
	a := newAnalyzer(defaultConfiguration())
	// A full shared run lasts over the default maximum and must not settle a pair.
	candidates, trace, err := a.findCandidates(context.Background(), []episode{
		{ID: "a", Fingerprint: filled(1000, 17)}, {ID: "b", Fingerprint: filled(1000, 17)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 || len(trace) != 1 || trace[0].Reason != "rhs-exceeds-maximum-duration" {
		t.Fatalf("maximum introduction duration not preserved: %#v %#v", candidates, trace)
	}
}

func TestMaximumDurationPreservesRHSOnlyCheck(t *testing.T) {
	left, right := filled(1100, 0), filled(1100, ^uint32(0))
	for i := 0; i < 965; i++ {
		point := uint32(i+1)*4096 + 0x555
		left[30+i], right[60+i] = point, point
	}
	candidates, trace, err := newAnalyzer(defaultConfiguration()).findCandidates(context.Background(), []episode{
		{ID: "a", Fingerprint: left}, {ID: "b", Fingerprint: right},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || len(trace) != 1 || !trace[0].Accepted || candidates[0].duration() <= 120 || candidates[1].duration() > 120 {
		t.Fatalf("upstream RHS-only maximum check changed: %#v %#v", candidates, trace)
	}
	if candidates[0].Start != 0 || candidates[0].End != 994*sampleDuration || candidates[1].Start != 60*sampleDuration || candidates[1].End != 1024*sampleDuration {
		t.Fatalf("unexpected asymmetric snapped candidates: %#v", candidates)
	}
}

func TestEmptySingleAndCanceledQueues(t *testing.T) {
	for _, episodes := range [][]episode{nil, {{ID: "a", Fingerprint: filled(200, 17)}}, {{ID: "a", Fingerprint: []uint32{}}, {ID: "b", Fingerprint: []uint32{}}}} {
		candidates, _, err := newAnalyzer(defaultConfiguration()).findCandidates(context.Background(), episodes)
		if err != nil || len(candidates) != 0 {
			t.Fatalf("invalid empty/single outcome: %#v %v", candidates, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := newAnalyzer(defaultConfiguration()).findCandidates(ctx, []episode{{ID: "a"}, {ID: "b"}})
	if err != context.Canceled {
		t.Fatalf("cancellation not returned: %v", err)
	}
}

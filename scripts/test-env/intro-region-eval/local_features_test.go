package main

import (
	"math/bits"
	"testing"
)

func TestBRIEFPatternIsBoundedAndNonDuplicate(t *testing.T) {
	seen := map[[2]int]bool{}
	for _, c := range comparisons {
		if c.A < 0 || c.A >= 256 || c.B < 0 || c.B >= 256 || absInt(c.A%16-c.B%16)+absInt(c.A/16-c.B/16) < 4 {
			t.Fatal("unbounded or neighboring comparison")
		}
		key := [2]int{min(c.A, c.B), max(c.A, c.B)}
		if seen[key] {
			t.Fatal("duplicate comparison")
		}
		seen[key] = true
	}
}

func TestPatchBrightnessOffsetAndUnreliableEvidence(t *testing.T) {
	var a, b [256]float64
	for i := range a {
		a[i] = float64((i * 71) % 170)
		b[i] = a[i] + 30
	}
	da, db := describe(a), describe(b)
	close, _, err := closePatch(da, db, &budget{})
	if err != nil || !close {
		t.Fatal("identical local ordering did not survive brightness offset")
	}
	flat := describe([256]float64{})
	close, _, err = closePatch(flat, flat, &budget{})
	if err != nil || close {
		t.Fatal("flat patch supplied evidence")
	}
	da.Reliable = [2]uint64{^uint64(0), 0}
	close, _, err = closePatch(da, db, &budget{})
	if err != nil || close {
		t.Fatal("fewer than 80 reliable bits supplied evidence")
	}
}

func TestMotionNeedsSharedReliableSynchronousChanges(t *testing.T) {
	all := [2]uint64{^uint64(0), ^uint64(0)}
	prev := descriptor{Reliable: all, Usable: true}
	now := descriptor{Bits: [2]uint64{0xffff, 0}, Reliable: all, Usable: true}
	pass, err := synchronousPatch(now, prev, now, prev, &budget{})
	if err != nil || !pass {
		t.Fatal("shared motion rejected")
	}
	other := descriptor{Bits: [2]uint64{0, 0xffff}, Reliable: all, Usable: true}
	pass, err = synchronousPatch(now, prev, other, prev, &budget{})
	if err != nil || pass {
		t.Fatal("unrelated changes supplied motion")
	}
	prev.Reliable = [2]uint64{0, 0xffff}
	pass, err = synchronousPatch(now, prev, now, prev, &budget{})
	if err != nil || pass {
		t.Fatal("unreliable temporal bits supplied motion")
	}
}

func TestOppositeFlipsCannotSupplyPairOrGroupMotion(t *testing.T) {
	all := [2]uint64{^uint64(0), ^uint64(0)}
	low := descriptor{Reliable: all, Usable: true}
	high := descriptor{Bits: [2]uint64{0xfff, 0}, Reliable: all, Usable: true}
	pass, err := synchronousPatch(high, low, low, high, &budget{})
	if err != nil || pass {
		t.Fatal("opposite pair flips supplied synchronous motion")
	}
	pass, err = synchronousGroup([3]descriptor{high, low, high}, [3]descriptor{low, high, low}, &budget{})
	if err != nil || pass {
		t.Fatal("opposite group flips supplied synchronous motion")
	}
	pass, err = synchronousGroup([3]descriptor{high, high, high}, [3]descriptor{low, low, low}, &budget{})
	if err != nil || !pass {
		t.Fatal("same-direction group flips rejected")
	}
}

func TestPairwiseReliableMotionCannotBeBorrowedByGroup(t *testing.T) {
	var reliable [3][2]uint64
	var motion [3][2]uint64
	for bit := 0; bit < 128; bit++ {
		for source := 0; source < 3; source++ {
			shared := bit < 56
			pairAB, pairAC, pairBC := bit >= 56 && bit < 80, bit >= 80 && bit < 104, bit >= 104
			owns := shared || pairAB && source != 2 || pairAC && source != 1 || pairBC && source != 0
			if owns {
				reliable[source][bit/64] |= 1 << uint(bit%64)
				if !shared {
					motion[source][bit/64] |= 1 << uint(bit%64)
				}
			}
		}
	}
	var current, previous [3]descriptor
	for source := range current {
		current[source] = descriptor{Bits: motion[source], Reliable: reliable[source], Usable: true}
		previous[source] = descriptor{Reliable: reliable[source], Usable: true}
	}
	for a := 0; a < 3; a++ {
		for b := a + 1; b < 3; b++ {
			pass, err := synchronousPatch(current[a], previous[a], current[b], previous[b], &budget{})
			if err != nil || !pass {
				t.Fatal("counterexample did not pass each pair motion gate")
			}
		}
	}
	pass, err := synchronousGroup(current, previous, &budget{})
	if err != nil || pass {
		t.Fatal("pairwise reliable evidence was borrowed across the group")
	}
}

func TestSpatialSupportNeedsCenterAndDistribution(t *testing.T) {
	var mask uint32
	for _, p := range []int{0, 2, 4, 6, 8, 12, 16, 18} {
		mask |= 1 << uint(p)
	}
	if !spatial(mask) {
		t.Fatal("distributed foreground evidence rejected")
	}
	mask = (1 << 10) - 1
	if spatial(mask) {
		t.Fatal("two rows supplied spatial evidence")
	}
	mask = 0
	for _, p := range []int{0, 1, 2, 3, 4, 5, 9, 10, 14, 15, 19, 20, 21, 22, 23, 24} {
		mask |= 1 << uint(p)
	}
	if spatial(mask) {
		t.Fatal("frame border supplied foreground evidence")
	}
}

func TestFrozenGeometriesHaveInterpolationMargin(t *testing.T) {
	raw := make([]byte, frameBytes)
	for i := range raw {
		raw[i] = byte((i * 71) % 256)
	}
	grid := geometryGrid()
	if len(grid) != 81 {
		t.Fatal("geometry family changed")
	}
	for _, g := range grid {
		if _, err := render(raw, 0, g, &budget{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := render(raw, 0, geometry{1, 1, 20, 0}, &budget{}); err == nil {
		t.Fatal("out-of-bounds geometry was clamped")
	}
}

func TestBudgetFailureDoesNotReturnMatches(t *testing.T) {
	all := descriptor{Bits: [2]uint64{0xffff, 0}, Reliable: [2]uint64{^uint64(0), ^uint64(0)}, Usable: true}
	work := &budget{PatchComparisons: maxPatchComparisons}
	close, _, err := closePatch(all, all, work)
	if err == nil || close {
		t.Fatal("budget overflow returned evidence")
	}
}

func TestDescriptorBitsAreNotDegenerate(t *testing.T) {
	var values [256]float64
	for i := range values {
		values[i] = float64((i * 71) % 170)
	}
	d := describe(values)
	active := bits.OnesCount64(d.Reliable[0]) + bits.OnesCount64(d.Reliable[1])
	ones := bits.OnesCount64(d.Bits[0]) + bits.OnesCount64(d.Bits[1])
	if active < 80 || ones < 20 || ones > 108 {
		t.Fatal("synthetic descriptor lacks bounded comparison diversity")
	}
}

func TestPairUpperBoundRetainsValidSixDescriptorMotion(t *testing.T) {
	all := [2]uint64{^uint64(0), ^uint64(0)}
	common80 := [2]uint64{^uint64(0), 0xffff}
	var current, previous [3]descriptor
	for source := range current {
		current[source] = descriptor{Reliable: all, Usable: true}
		previous[source] = descriptor{Reliable: all, Usable: true}
		current[source].Bits[0] = 0x3f | uint64(0x3f)<<uint(6+source*6)
	}
	current[0].Bits[1] = uint64(0xfffff) << 16
	current[1].Bits[1] = uint64(0xfffff) << 36
	current[2].Reliable = common80
	previous[2].Reliable = common80
	group, err := synchronousGroup(current, previous, &budget{})
	if err != nil || !group {
		t.Fatal("valid group counterexample rejected")
	}
	pair, err := synchronousPatch(current[0], previous[0], current[1], previous[1], &budget{})
	if err != nil || pair {
		t.Fatal("counterexample did not fail stricter pair ratio")
	}
	possible, err := possibleGroupPairMotion(current[0], previous[0], current[1], previous[1], &budget{})
	if err != nil || !possible {
		t.Fatal("necessary pair bound removed valid group motion")
	}
}

func TestAffineMappingOwnsEachSourceFrameOnce(t *testing.T) {
	a := view{Frames: []frame{{PTS: 0}, {PTS: .04}, {PTS: .1}}}
	b := view{Frames: []frame{{PTS: .02}, {PTS: .1}}}
	mapping, err := affineMapping(a, b, 1, 0, &budget{})
	if err != nil {
		t.Fatal(err)
	}
	if mapping[0] != 0 || mapping[1] != -1 || mapping[2] != 1 {
		t.Fatalf("unexpected ownership: %v", mapping)
	}
	views := [3]view{a, a, a}
	mappings := [3][]int{{0, 1, 2}, {0, 1, 2}, {0, 1, 2}}
	if insideSourceWindow(views, mappings, 0, [3][2]float64{{.01, .2}, {0, .2}, {0, .2}}) {
		t.Fatal("outside-window source sample accepted")
	}
}

func TestUnknownSelfEvidenceCannotCreateAState(t *testing.T) {
	var a, b frame
	comparable, close, different, err := selfEvidence(a, b, 0xff, &budget{})
	if err != nil || comparable || close || different {
		t.Fatal("unknown patches supplied comparable or different state evidence")
	}
	all := descriptor{Reliable: [2]uint64{^uint64(0), ^uint64(0)}, Usable: true}
	for p := 0; p < 8; p++ {
		a.Patches[p] = all
		b.Patches[p] = all
	}
	b.Patches[0] = descriptor{}
	b.Patches[1].Bits = [2]uint64{^uint64(0), ^uint64(0)}
	_, _, different, err = selfEvidence(a, b, 0xff, &budget{})
	if err != nil || different {
		t.Fatal("one unknown plus one different patch proved a distinct eight-patch state")
	}
	b.Patches[2].Bits = [2]uint64{^uint64(0), ^uint64(0)}
	_, _, different, err = selfEvidence(a, b, 0xff, &budget{})
	if err != nil || !different {
		t.Fatal("two known differences did not establish state difference")
	}
}

func TestCohortHypothesisBudgetIsShared(t *testing.T) {
	work := &budget{}
	for i := 0; i < maxHypotheses; i++ {
		if err := work.retain(); err != nil {
			t.Fatal(err)
		}
	}
	if work.retain() == nil {
		t.Fatal("combined hypothesis budget was not enforced")
	}
}

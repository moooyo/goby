package main

import (
	"math/bits"
	"testing"
)

// This is a gate-level counterexample, not an end-to-end image fixture. It
// proves that per-patch distributed final motion does not imply four moving
// patches at one nomination instant. State/period gates are outside its claim.
func TestDistributedFinalMotionDoesNotImplyInstantaneousFourPatchSeed(t *testing.T) {
	var views [3]view
	all := descriptor{Reliable: [2]uint64{^uint64(0), ^uint64(0)}, Usable: true}
	movingPatches := []int{6, 8, 12, 16}
	states := []uint64{0, 0xffffffff, 0xffffffff00000000, ^uint64(0)}
	for source := range views {
		views[source].Frames = make([]frame, 80)
		for index := 0; index < 80; index++ {
			f := frame{PTS: float64(index) / 10, Previous: index - 5}
			for patch := range f.Patches {
				f.Patches[patch] = all
			}
			for phase, patch := range movingPatches {
				state := 0
				for _, base := range []int{10, 30, 50} {
					if index >= base+phase*2 {
						state++
					}
				}
				f.Patches[patch].Bits[0] = states[state]
			}
			views[source].Frames[index] = f
		}
	}
	maximumInstantaneous := 0
	var counts [patchCount]int
	var last [patchCount]float64
	for patch := range last {
		last[patch] = -1e9
	}
	for index := 5; index < 80; index++ {
		mask, err := groupMotion(views, [3]int{index, index, index}, [3]int{index - 5, index - 5, index - 5}, (1<<25)-1, &budget{})
		if err != nil {
			t.Fatal(err)
		}
		maximumInstantaneous = max(maximumInstantaneous, bits.OnesCount32(mask))
		for patch := range counts {
			if mask&(1<<uint(patch)) != 0 && float64(index)/10-last[patch] >= 1-1e-7 {
				counts[patch]++
				last[patch] = float64(index) / 10
			}
		}
		m, err := compareFrames(views[0], index, views[1], index, &budget{})
		if err != nil || !spatial(m.Appearance) {
			t.Fatal("shared appearance did not pass")
		}
		if index%10 == 0 && bits.OnesCount32(m.Dynamic) >= 4 {
			t.Fatal("old instantaneous seed unexpectedly passed")
		}
	}
	if maximumInstantaneous >= 4 {
		t.Fatal("events were not distributed as the counterexample requires")
	}
	for _, patch := range movingPatches {
		if counts[patch] < 3 {
			t.Fatalf("patch %d did not meet final distributed motion condition", patch)
		}
	}
}

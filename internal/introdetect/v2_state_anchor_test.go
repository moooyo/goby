package introdetect

import (
	"math/bits"
	"slices"
	"sort"
	"testing"
)

// The first 128 signed Walsh rows are distinct 64-bit hashes separated by at
// least 32 bits. These mechanical fixtures therefore have genuine separate
// feature states without relying on random collision probabilities.
func stateAnchorTestHash(index int) uint64 {
	var hash uint64
	for bit := 0; bit < 64; bit++ {
		parity := bits.OnesCount64(uint64(index%64)&uint64(bit)) & 1
		if parity^(index/64&1) != 0 {
			hash |= uint64(1) << uint(bit)
		}
	}
	return hash
}

func stateAnchorTestCohort(step int64, masks [3]uint64) Cohort {
	cohort := testCohort()
	starts := [...]int64{
		10*TicksPerSecond + 1200000,
		35*TicksPerSecond + 3700000,
		63*TicksPerSecond + 8100000,
	}
	for index := range cohort.Episodes {
		episode := &cohort.Episodes[index]
		var visual []VisualSample
		for _, sample := range episode.Visual {
			if sample.Ticks < starts[index] || sample.Ticks >= starts[index]+50*TicksPerSecond {
				visual = append(visual, sample)
			}
		}
		for relative := int64(0); relative < 50*TicksPerSecond; relative += step {
			visual = append(visual, VisualSample{
				Ticks: starts[index] + relative, Hash: stateAnchorTestHash(int(relative/step)) ^ masks[index], Contrast: 200,
			})
		}
		sort.Slice(visual, func(i, j int) bool { return visual[i].Ticks < visual[j].Ticks })
		episode.Visual = visual
	}
	return cohort
}

func TestAnalyzeV2DynamicStatesUseMatchedAnchorsWithoutStateDwell(t *testing.T) {
	for _, cadence := range []struct {
		name string
		step int64
	}{{"half_second", TicksPerSecond / 2}, {"one_second", TicksPerSecond}} {
		t.Run(cadence.name, func(t *testing.T) {
			// Every opening sample has a different state. There are no same-state
			// adjacent samples, while the complete cross-source anchor lasts 50s.
			cohort := stateAnchorTestCohort(cadence.step, [3]uint64{})
			result := analyzeTest(t, cohort)
			if len(result.Groups) != 1 || len(result.Groups[0].Members) != 3 {
				t.Fatalf("dynamic states lost their complete witness group: %#v", result)
			}
			for _, episode := range result.Episodes {
				if episode.Status != Qualified || len(episode.Candidates) != 1 {
					t.Fatalf("continuous matched anchors required stationary states: %#v", episode)
				}
				metrics := episode.Candidates[0].Metrics
				if metrics.VisualDistinctStates < 4 || metrics.VisualMatchedTimePermille < 850 {
					t.Fatalf("dynamic observations did not supply anchored state evidence: %#v", metrics)
				}
			}
		})
	}
}

func stateAnchorTestPair() (Episode, Episode, audioMatch) {
	a := Episode{DurationTicks: 40 * TicksPerSecond}
	for tick := int64(0); tick <= 30*TicksPerSecond; tick += TicksPerSecond / 2 {
		state := min(int(tick/(10*TicksPerSecond)), 2)
		if tick == 15*TicksPerSecond {
			state = 3
		}
		a.Visual = append(a.Visual, VisualSample{Ticks: tick, Hash: stateAnchorTestHash(state), Contrast: 200})
	}
	b := Episode{DurationTicks: a.DurationTicks, Visual: append([]VisualSample(nil), a.Visual...)}
	interval := Interval{0, 30 * TicksPerSecond}
	return a, b, audioMatch{a: interval, b: interval}
}

func TestVisualStateAnchorsCannotBorrowRemoteOrOneSidedSupport(t *testing.T) {
	a, b, audio := stateAnchorTestPair()
	baseline, err := measureVisualV2(a, b, audio, 0, DefaultOptions(), v2TestBudget())
	if err != nil || baseline.metrics.VisualDistinctStates != 4 {
		t.Fatalf("one real observation inside a complete anchor did not count as a state: %#v, %v", baseline, err)
	}
	for _, kind := range []string{"unmatched_neighbors", "dark_neighbors", "missing_neighbors", "short_anchor_on_one_side"} {
		t.Run(kind, func(t *testing.T) {
			a, b, audio := stateAnchorTestPair()
			switch kind {
			case "unmatched_neighbors":
				b.Visual[29].Hash = ^a.Visual[29].Hash
				b.Visual[31].Hash = ^a.Visual[31].Hash
			case "dark_neighbors":
				b.Visual[29].Hash, b.Visual[29].Contrast = 0, 0
				b.Visual[31].Hash, b.Visual[31].Contrast = 0, 0
			case "missing_neighbors":
				var retained []VisualSample
				for _, frame := range a.Visual {
					if frame.Ticks < 14*TicksPerSecond || frame.Ticks > 16*TicksPerSecond || frame.Ticks == 15*TicksPerSecond {
						retained = append(retained, frame)
					}
				}
				a.Visual = retained
			case "short_anchor_on_one_side":
				// The isolated matched run lasts 1s in A but only 0.4s in B.
				// Both sources still have long, ordinary anchors elsewhere.
				b.Visual[28].Hash, b.Visual[28].Contrast = 0, 0
				b.Visual[32].Hash, b.Visual[32].Contrast = 0, 0
				b.Visual[29].Ticks = 148 * TicksPerSecond / 10
				b.Visual[31].Ticks = 152 * TicksPerSecond / 10
			}
			evidence, err := measureVisualV2(a, b, audio, 0, DefaultOptions(), v2TestBudget())
			if err != nil {
				t.Fatalf("measure isolated state evidence: %v", err)
			}
			if evidence.metrics.VisualAnchorCount == 0 || evidence.metrics.VisualMatchedTimePermille < 850 {
				t.Fatalf("fixture lost its surrounding matched anchor evidence: %#v", evidence.metrics)
			}
			if evidence.metrics.VisualDistinctStates != 3 {
				t.Fatalf("the fourth state borrowed an unrelated or short anchor: %#v", evidence.metrics)
			}
		})
	}
}

func TestVisualStateAnchorHammingBoundaryRemainsSeparateFromQualification(t *testing.T) {
	for _, distance := range []int{24, 28} {
		name := "hd24"
		if distance == 28 {
			name = "hd28"
		}
		t.Run(name, func(t *testing.T) {
			mask := uint64(1)<<uint(distance) - 1
			a, b := Episode{DurationTicks: 40 * TicksPerSecond}, Episode{DurationTicks: 40 * TicksPerSecond}
			for index := 0; index <= 60; index++ {
				tick, hash := int64(index)*TicksPerSecond/2, stateAnchorTestHash(index)
				a.Visual = append(a.Visual, VisualSample{Ticks: tick, Hash: hash, Contrast: 200})
				b.Visual = append(b.Visual, VisualSample{Ticks: tick, Hash: hash ^ mask, Contrast: 200})
			}
			interval := Interval{0, 30 * TicksPerSecond}
			evidence, err := measureVisualV2(a, b, audioMatch{a: interval, b: interval}, 0, DefaultOptions(), v2TestBudget())
			if err != nil {
				t.Fatalf("measure Hamming boundary: %v", err)
			}
			if distance == 24 {
				if evidence.metrics.VisualMatchedTimePermille != 1000 || evidence.metrics.VisualDistinctStates < 4 {
					t.Fatalf("the inclusive HD24 boundary did not provide real matched anchors: %#v", evidence.metrics)
				}
			} else if evidence.metrics.VisualMatchedTimePermille != 0 || evidence.metrics.VisualContradictedTimePermille != 1000 || evidence.metrics.VisualDistinctStates != 0 {
				t.Fatalf("HD28 differences were admitted as visual matches: %#v", evidence.metrics)
			}

			// Full audio, many states and high contrast still cannot compensate
			// for the low average similarity of an all-HD24 visual repetition.
			result := analyzeTest(t, stateAnchorTestCohort(TicksPerSecond/2, [3]uint64{0, mask, mask}))
			requireNoAutomatic(t, result)
			if distance == 24 {
				for _, episode := range result.Episodes {
					if episode.Status != Review || len(episode.Candidates) == 0 {
						t.Fatalf("matched but weak HD24 evidence did not remain reviewable: %#v", episode)
					}
					for _, candidate := range episode.Candidates {
						if candidate.Metrics.VisualSimilarityPermille >= DefaultOptions().MinVisualSimilarity || !slices.Contains(candidate.Reasons, WeakVisualEvidence) {
							t.Fatalf("near hashes alone erased the similarity gate: %#v", candidate)
						}
					}
				}
			}
		})
	}
}

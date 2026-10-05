// SPDX-FileCopyrightText: 2022 ConfusedPolarBear
// SPDX-FileCopyrightText: 2024-2026 rlauuzo
// SPDX-FileCopyrightText: 2024-2026 AbandonedCart
// SPDX-FileCopyrightText: 2024-2026 Kilian von Pflugk
// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

import (
	"math"
	"reflect"
	"testing"
)

func TestPinnedNormalizationDistribution(t *testing.T) {
	for _, tc := range []struct{ floor, want int }{{2, 85}, {45, 89}} {
		frames := make([]BlackFrame, 200)
		for i := range frames {
			frames[i] = BlackFrame{i, tc.floor, float64(i)}
			if i >= 190 {
				frames[i].Percentage = 95
			}
		}
		minimum, _ := NormalizeThreshold(frames, 85)
		if minimum != tc.want {
			t.Fatalf("floor=%d minimum=%d want=%d", tc.floor, minimum, tc.want)
		}
	}
}
func TestPinnedSparseBlackIntervalRecoveryAndFallback(t *testing.T) {
	frames := []BlackFrame{{0, 15, 366.45}, {1, 96, 376.46}, {2, 96, 386.47}, {3, 98, 396.48}, {4, 99, 406.49}, {5, 20, 416.5}}
	options := DefaultOptions()
	plan := PlanBlackFrames(frames, Range{2356.27, 2806.27}, options)
	if len(plan.IntervalProbeRanges) != 1 {
		t.Fatalf("missing targeted recovery: %+v", plan)
	}
	scenes := FinalizeBlackScenes(frames, []Range{{367.827, 376.002}}, plan, options)
	if len(scenes) != 1 || math.Abs(scenes[0].StartTime+2356.27-2724.097) > 1e-9 || math.Abs(scenes[0].EndTime+2356.27-2762.760) > 1e-9 {
		t.Fatalf("wrong interval recovery: %+v", scenes)
	}
	frames = []BlackFrame{{0, 10, 0}, {1, 96, 10}, {2, 96, 20}, {3, 96, 30}, {4, 96, 40}, {5, 10, 50}}
	options.RefineCreditsBoundary = false
	plan = PlanBlackFrames(frames, Range{0, 50}, options)
	scenes = FinalizeBlackScenes(frames, nil, plan, options)
	if len(plan.IntervalProbeRanges) != 1 || len(scenes) != 1 || scenes[0].StartTime != 10 || scenes[0].EndTime != 40 {
		t.Fatalf("sparse valid scene wrongly rejected: %+v %+v", plan, scenes)
	}
}
func TestPinnedDensityRejectsMergeAcrossContent(t *testing.T) {
	var frames []BlackFrame
	for i := 0; i <= 69; i++ {
		time := float64(i) * 0.5
		p := 10
		if time <= 7.5 || time >= 27 {
			p = 95
		}
		frames = append(frames, BlackFrame{i, p, time})
	}
	got := DetectCreditScenes(frames, 85, 96, 5, false)
	want := []Scene{{0, 15, 0, 7.5}, {54, 69, 27, 34.5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}
func visualSequence(start, end, step float64, card bool) []KeyframeVisual {
	var result []KeyframeVisual
	for time := start; time <= end; time += step {
		v := KeyframeVisual{time, 0.55, 108}
		if card {
			v.Entropy, v.Saturation = 0.12, 30
		}
		result = append(result, v)
	}
	return result
}
func TestPinnedEntropyGoldenRuns(t *testing.T) {
	cases := []struct {
		name    string
		values  []KeyframeVisual
		minimum int
		want    *Range
	}{
		{"uniformly sparse", visualSequence(0, 63, 21, true), 60, &Range{0, 63}},
		{"sparse trailing cards", append(visualSequence(0, 20, 2, true), visualSequence(28, 196, 8, true)...), 15, &Range{0, 20}},
		{"latest separated run", append(append(visualSequence(0, 20, 2, true), visualSequence(22, 58, 2, false)...), visualSequence(60, 96, 12, true)...), 15, &Range{60, 96}},
		{"busy content", visualSequence(0, 90, 2, false), 15, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FindEntropyRange(tc.values, tc.minimum)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
	var sparse []KeyframeVisual
	for time := 0; time <= 54; time += 2 {
		card := time <= 6 || time == 14 || time == 22 || time == 30 || time == 38 || time == 46 || time == 54
		sparse = append(sparse, visualSequence(float64(time), float64(time), 1, card)...)
	}
	if got := FindEntropyRange(sparse, 15); got != nil {
		t.Fatalf("sparse cards crossed busy content: %+v", got)
	}
	for _, tc := range []struct {
		entropy, saturation float64
		want                bool
	}{{0.349, 95, true}, {0.35, 30, false}, {0.12, 96, false}} {
		if got := IsCreditCardKeyframe(KeyframeVisual{0, tc.entropy, tc.saturation}); got != tc.want {
			t.Fatalf("exclusive threshold %+v", tc)
		}
	}
}
func TestPinnedBoundaryAndProbeRangeRules(t *testing.T) {
	if value, ok := TryRefineBoundaryTime(0, 10, 20); ok || value != 10 {
		t.Fatal(value, ok)
	}
	if value, ok := TryRefineBoundaryTime(0.1, 10, 20); !ok || value != 10.1 {
		t.Fatal(value, ok)
	}
	if _, ok := TryRefineBoundaryTime(10.1, 10, 20); ok {
		t.Fatal("accepted outside boundary")
	}
	got := BuildIntervalProbeRanges([]Scene{{0, 1, 0, 10}, {2, 3, 35, 50}, {4, 5, 95, 100}}, 15, Range{100, 200})
	want := []Range{{100, 165}, {180, 200}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

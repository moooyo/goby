// SPDX-FileCopyrightText: 2025-2026 rlauuzo
// SPDX-FileCopyrightText: 2026 AbandonedCart
// SPDX-FileCopyrightText: 2026 Kilian von Pflugk
// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

import "testing"

func TestPinnedStartAndEndOffsets(t *testing.T) {
	cases := []struct {
		startOffset                                         float64
		include                                             bool
		endOffset, duration, start, end, wantStart, wantEnd float64
	}{
		{2, true, 0, 60, 1.2, 10, 2, 10}, {2, false, 0, 60, 0, 10, 0, 10}, {2, true, 0, 60, 1, 2, 0, 0},
		{2, false, 0, 60, 5, 12, 7, 12}, {0, false, 100, 30, -5, 200, 0, 30},
	}
	for _, tc := range cases {
		options := DefaultOptions()
		options.AdjustIntroBasedOnChapters, options.AdjustIntroBasedOnSilence, options.SnapToKeyframe = false, false, false
		options.IntroStartOffset, options.IntroEndOffset, options.IncludeIntroStartOffsetWhenSnapping = tc.startOffset, tc.endOffset, tc.include
		got := FinishTimeAdjustment(PlanTimeAdjustment(Segment{tc.start, tc.end, ChromaprintSource}, tc.duration, nil, options), nil, nil)
		if got.Start != tc.wantStart || got.End != tc.wantEnd {
			t.Fatalf("case %+v got %+v", tc, got)
		}
	}
}

func TestNativeAdjustmentRunsAfterCombinationHardBoundary(t *testing.T) {
	candidates := []Segment{{350, 438, ChapterSource}, {400, 449.5, BlackFrameSource}, {300, 436, ChromaprintSource}}
	combined := Combine(candidates, 450, 15)
	if len(combined) != 1 || combined[0].End != 438 {
		t.Fatalf("missing combination cap: %+v", combined)
	}
	plan := PlanTimeAdjustment(combined[0], 450, []Chapter{{"Ending", 350}, {"Preview", 438}}, DefaultOptions())
	// Native nearest-keyframe adjustment happens after combination. A frame at
	// 439 may therefore move the final end beyond the authored boundary at 438.
	got := FinishTimeAdjustment(plan, nil, []float64{435, 439})
	if got.End != 439 {
		t.Fatalf("native post-combination adjustment changed: %+v", got)
	}
}

func TestNativeSilencePrecedesKeyframeAndExcludesTouchingRanges(t *testing.T) {
	plan := PlanTimeAdjustment(Segment{20, 40, BlackFrameSource}, 100, nil, DefaultOptions())
	got := FinishTimeAdjustment(plan, []Range{{30, 35}, {42, 43}, {36, 36.5}}, []float64{35.5, 36.5, 40})
	if got.End != 35.5 {
		t.Fatalf("silence ordering or strict intersection changed: %+v", got)
	}
	// The equal-distance keyframe tie keeps the first in decode order.
	if SelectNearest([]float64{39, 41}, 40) != 39 {
		t.Fatal("nearest tie changed")
	}
}

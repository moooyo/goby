// SPDX-FileCopyrightText: 2025-2026 rlauuzo
// SPDX-FileCopyrightText: 2026 AbandonedCart
// SPDX-FileCopyrightText: 2026 Kilian von Pflugk
// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

import "math"

const adjustmentEpsilon = 1e-3

type AdjustmentPlan struct {
	Segment                  Segment
	SearchRange              Range
	ProbeEnd                 bool
	UseSilence, UseKeyframes bool
	MinimumSilenceDuration   float64
}

func SearchRange(time, duration, windowStart, windowEnd float64) Range {
	return Range{max(time-windowStart, 0), min(time+windowEnd, duration)}
}
func SelectNearest(candidates []float64, reference float64) float64 {
	nearest, best := reference, math.MaxFloat64
	for _, value := range candidates {
		if distance := math.Abs(value - reference); distance < best {
			best, nearest = distance, value
		}
	}
	return nearest
}
func chapterBoundary(chapters []Chapter, reference float64, search Range) float64 {
	var candidates []float64
	for _, chapter := range chapters {
		if chapter.StartSeconds+adjustmentEpsilon >= search.Start && chapter.StartSeconds-adjustmentEpsilon <= search.End {
			candidates = append(candidates, chapter.StartSeconds)
		}
	}
	return SelectNearest(candidates, reference)
}

// PlanTimeAdjustment preserves the default TimeAdjustmentHelper operation order.
// A pure chapter candidate does not snap to chapters a second time.
func PlanTimeAdjustment(original Segment, duration float64, chapters []Chapter, options Options) AdjustmentPlan {
	plan := AdjustmentPlan{Segment: original, UseSilence: options.AdjustIntroBasedOnSilence, UseKeyframes: options.SnapToKeyframe, MinimumSilenceDuration: options.SilenceDetectionMinimumDuration}
	if options.EndSnapThreshold < 0 || options.AdjustWindowInward < 0 || options.AdjustWindowOutward < 0 {
		return plan
	}
	useChapters := options.AdjustIntroBasedOnChapters && original.Source != ChapterSource
	start := original.Start
	snap := start < 0 || start <= options.EndSnapThreshold+adjustmentEpsilon
	if snap {
		start = 0
	} else if useChapters {
		start = chapterBoundary(chapters, start, SearchRange(start, duration, options.AdjustWindowOutward, options.AdjustWindowInward))
	}
	if !snap || options.IncludeIntroStartOffsetWhenSnapping {
		start = min(duration, max(0, start+options.IntroStartOffset))
	}
	end := original.End
	if end >= duration-options.EndSnapThreshold-adjustmentEpsilon {
		end = duration
	} else {
		if useChapters {
			end = chapterBoundary(chapters, end, SearchRange(end, duration, options.AdjustWindowInward, options.AdjustWindowOutward))
		}
		end = min(duration, max(0, end-options.IntroEndOffset))
		plan.SearchRange = SearchRange(end, duration, options.AdjustWindowInward, options.AdjustWindowOutward)
		plan.ProbeEnd = true
	}
	plan.Segment = Segment{start, end, original.Source}
	return plan
}

func FinishTimeAdjustment(plan AdjustmentPlan, silence []Range, keyframes []float64) Segment {
	segment := plan.Segment
	if plan.ProbeEnd {
		if plan.UseSilence {
			for _, r := range silence {
				if r.Start >= plan.SearchRange.End || r.End <= plan.SearchRange.Start || r.Duration() < plan.MinimumSilenceDuration || r.Start < plan.SearchRange.Start {
					continue
				}
				segment.End = r.Start
				break
			}
		}
		if plan.UseKeyframes {
			segment.End = SelectNearest(keyframes, segment.End)
		}
	}
	if segment.Start >= segment.End {
		return Segment{Source: segment.Source}
	}
	return segment
}

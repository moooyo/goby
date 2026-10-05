// SPDX-FileCopyrightText: 2026 rlauuzo
// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

import "sort"

func FindRawScenes(frames []BlackFrame, minimum int) []Scene {
	var scenes []Scene
	gap := maximumInRunGap(frames)
	var first, last *BlackFrame
	for i := range frames {
		frame := &frames[i]
		if frame.Percentage < minimum {
			continue
		}
		if first == nil {
			first, last = frame, frame
			continue
		}
		if frame.Time-last.Time > gap {
			scenes = append(scenes, Scene{first.Frame, last.Frame, first.Time, last.Time})
			first = frame
		}
		last = frame
	}
	if first != nil {
		scenes = append(scenes, Scene{first.Frame, last.Frame, first.Time, last.Time})
	}
	return scenes
}

func DetectCreditScenes(frames []BlackFrame, minimum, sceneChange, minimumDuration int, allowRefinement bool) []Scene {
	var dense []Scene
	for _, scene := range FindRawScenes(frames, minimum) {
		if CalculateMetrics(frames, scene, minimum).MeetsDensity(0.5) {
			dense = append(dense, scene)
		}
	}
	var merged []Scene
	for _, scene := range dense {
		if len(merged) > 0 {
			current := &merged[len(merged)-1]
			union := Scene{current.StartFrame, scene.EndFrame, current.StartTime, scene.EndTime}
			if scene.StartTime-current.EndTime <= MaximumSceneMergeGapSeconds && CalculateMetrics(frames, union, minimum).MeetsDensity(0.5) {
				*current = union
				continue
			}
		}
		merged = append(merged, scene)
	}
	var result []Scene
	for _, scene := range merged {
		for i := sort.Search(len(frames), func(i int) bool { return frames[i].Frame >= scene.StartFrame }); i < len(frames); i++ {
			frame := frames[i]
			if frame.Frame > scene.EndFrame {
				break
			}
			if frame.Percentage >= sceneChange {
				scene.StartFrame, scene.StartTime = frame.Frame, frame.Time
				break
			}
		}
		last, _, exists := FindBoundaryKeyframeTimes(frames, scene)
		if scene.EndTime-scene.StartTime >= float64(minimumDuration) || (allowRefinement && exists && ShouldRefineBoundary(scene, last, minimumDuration)) {
			result = append(result, scene)
		}
	}
	return result
}

func DetectIntervalSupportedCreditScenes(frames []BlackFrame, intervals []Range, minimum, minimumDuration int) []Scene {
	var result []Scene
	for _, candidate := range FindRawScenes(frames, minimum) {
		best := -1
		bestSpan := -1.0
		for i, interval := range intervals {
			if interval.Start <= candidate.EndTime && interval.End >= candidate.StartTime-2 {
				span := max(candidate.EndTime, interval.End) - interval.Start
				if span > bestSpan {
					best, bestSpan = i, span
				}
			}
		}
		if best < 0 {
			continue
		}
		start, end := intervals[best].Start, max(candidate.EndTime, intervals[best].End)
		if end-start < float64(minimumDuration) {
			continue
		}
		startFrame, endFrame := candidate.EndFrame, candidate.StartFrame
		foundStart := false
		for i := sort.Search(len(frames), func(i int) bool { return frames[i].Frame >= candidate.StartFrame }); i < len(frames); i++ {
			frame := frames[i]
			if frame.Frame > candidate.EndFrame {
				break
			}
			if frame.Percentage < minimum {
				continue
			}
			if !foundStart && frame.Time >= start {
				startFrame = frame.Frame
				foundStart = true
			}
			if frame.Time <= end {
				endFrame = frame.Frame
			}
		}
		result = append(result, Scene{startFrame, endFrame, start, end})
	}
	return result
}

func BuildIntervalProbeRanges(candidates []Scene, minimumDuration int, window Range) []Range {
	var ranges []Range
	for _, c := range candidates {
		r := Range{max(window.Start, window.Start+c.StartTime-float64(minimumDuration)), min(window.End, window.Start+c.EndTime+float64(minimumDuration))}
		if r.Duration() > 0 {
			ranges = append(ranges, r)
		}
	}
	sort.SliceStable(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	var result []Range
	for _, r := range ranges {
		if len(result) > 0 && r.Start <= result[len(result)-1].End {
			result[len(result)-1].End = max(result[len(result)-1].End, r.End)
		} else {
			result = append(result, r)
		}
	}
	return result
}

func HasIntervalSupport(scene Scene, intervals []Range) bool {
	for _, r := range intervals {
		if min(scene.EndTime, r.End)-max(scene.StartTime, r.Start) >= MinimumIntervalOverlapSeconds {
			return true
		}
	}
	return false
}

// RankCreditCandidates prefers interval support, then the later input scene.
func RankCreditCandidates(scenes []Scene, intervals []Range) []Scene {
	result := append([]Scene(nil), scenes...)
	// Reverse before stable sorting to preserve the upstream descending index tie.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	sort.SliceStable(result, func(i, j int) bool {
		return HasIntervalSupport(result[i], intervals) && !HasIntervalSupport(result[j], intervals)
	})
	return result
}

func FindBoundaryKeyframeTimes(frames []BlackFrame, scene Scene) (last, first float64, ok bool) {
	hasLast := false
	for _, f := range frames {
		if f.Time < scene.StartTime {
			last = f.Time
			hasLast = true
		}
		if f.Time >= scene.StartTime {
			return last, f.Time, hasLast
		}
	}
	return 0, 0, false
}
func ShouldRefineBoundary(scene Scene, lastKeyframeTime float64, minimumDuration int) bool {
	maximum := scene.StartTime - lastKeyframeTime
	return maximum > MinimumBoundaryProbeWindow && scene.EndTime-scene.StartTime+maximum >= float64(minimumDuration)
}
func SelectProbeMinimum(frames []BlackFrame, scene Scene, sceneChange int) int {
	for _, frame := range frames {
		if frame.Frame == scene.StartFrame {
			return min(frame.Percentage, sceneChange)
		}
	}
	return sceneChange
}
func TryRefineBoundaryTime(probeTime, lastKeyframeTime, sceneStartTime float64) (float64, bool) {
	value := probeTime + lastKeyframeTime
	return value, value > lastKeyframeTime && value <= sceneStartTime
}

type BlackPlan struct {
	Minimum, SceneChange int
	Scenes               []Scene
	IntervalProbeRanges  []Range
}

func PlanBlackFrames(frames []BlackFrame, window Range, options Options) BlackPlan {
	minimum, change := NormalizeThreshold(frames, options.BlackFrameMinimumPercentage)
	scenes := DetectCreditScenes(frames, minimum, change, options.MinimumCreditsDuration, options.RefineCreditsBoundary)
	plan := BlackPlan{Minimum: minimum, SceneChange: change, Scenes: scenes}
	if len(scenes) == 0 {
		plan.IntervalProbeRanges = BuildIntervalProbeRanges(FindRawScenes(frames, minimum), options.MinimumCreditsDuration, window)
	} else if len(scenes) == 1 && CalculateMetrics(frames, scenes[0], minimum).IsSparse(scenes[0], options.MinimumCreditsDuration) {
		plan.IntervalProbeRanges = BuildIntervalProbeRanges(scenes, options.MinimumCreditsDuration, window)
	}
	return plan
}
func FinalizeBlackScenes(frames []BlackFrame, intervals []Range, plan BlackPlan, options Options) []Scene {
	scenes := plan.Scenes
	if len(plan.IntervalProbeRanges) > 0 {
		supported := DetectIntervalSupportedCreditScenes(frames, intervals, plan.Minimum, options.MinimumCreditsDuration)
		if len(scenes) == 0 || len(supported) > 0 {
			scenes = supported
		}
	}
	return RankCreditCandidates(scenes, intervals)
}

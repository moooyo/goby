// SPDX-FileCopyrightText: 2026 rlauuzo
// SPDX-FileCopyrightText: 2026 AbandonedCart
// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

import "sort"

type SceneMetrics struct{ TotalFrameCount, BlackFrameCount int }

func (m SceneMetrics) MeetsDensity(minimum float64) bool {
	return m.TotalFrameCount > 0 && float64(m.BlackFrameCount)/float64(m.TotalFrameCount) >= minimum
}
func (m SceneMetrics) IsSparse(scene Scene, minimumDuration int) bool {
	return m.BlackFrameCount <= 1 || (scene.EndTime-scene.StartTime)/float64(m.BlackFrameCount-1) > float64(minimumDuration)*0.5
}
func CalculateMetrics(frames []BlackFrame, scene Scene, minimum int) SceneMetrics {
	var m SceneMetrics
	for i := sort.Search(len(frames), func(i int) bool { return frames[i].Time >= scene.StartTime }); i < len(frames); i++ {
		frame := frames[i]
		if frame.Time > scene.EndTime {
			break
		}
		m.TotalFrameCount++
		if frame.Percentage >= minimum {
			m.BlackFrameCount++
		}
	}
	return m
}

// NormalizeThreshold ports the capped first-percentile histogram, including
// integer arithmetic. The caller must provide at least one frame.
func NormalizeThreshold(frames []BlackFrame, minimumPercentage int) (minimum, sceneChange int) {
	if len(frames) == 0 {
		return minimumPercentage, 95
	}
	var counts [31]int
	for _, f := range frames {
		counts[min(30, max(0, f.Percentage))]++
	}
	remaining, floor := min(len(frames)-1, int(float64(len(frames))*0.01)), 0
	for remaining >= counts[floor] {
		remaining -= counts[floor]
		floor++
	}
	return minimumPercentage*(100-floor)/100 + floor, 95*(100-floor)/100 + floor
}

func maximumInRunGap(frames []BlackFrame) float64 {
	var gaps []float64
	for i := 1; i < len(frames); i++ {
		if gap := frames[i].Time - frames[i-1].Time; gap > 0 {
			gaps = append(gaps, gap)
		}
	}
	if len(gaps) == 0 {
		return MaximumSceneMergeGapSeconds
	}
	sort.Float64s(gaps)
	return min(MaximumSceneMergeGapSeconds, gaps[len(gaps)/2]*5)
}

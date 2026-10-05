// SPDX-FileCopyrightText: 2026 rlauuzo
// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

import "sort"

const boundaryTolerance = 0.5

func ReachesWindowEnd(candidateEnd, windowEnd float64, minimumDuration int) bool {
	return candidateEnd > windowEnd-float64(minimumDuration)
}

// HardBoundaries are authored credits-to-content chapter ends. Adjacent or
// overlapping credits chapters do not create a boundary between themselves.
func HardBoundaries(candidates []Segment, windowEnd float64) []float64 {
	var chapters []Segment
	for _, c := range candidates {
		if c.Source == ChapterSource && c.Valid() {
			chapters = append(chapters, c)
		}
	}
	var result []float64
	for _, c := range chapters {
		b := c.End
		if b >= windowEnd-boundaryTolerance {
			continue
		}
		inside := false
		for _, other := range chapters {
			if b >= other.Start-boundaryTolerance && b < other.End-boundaryTolerance {
				inside = true
				break
			}
		}
		if !inside {
			result = append(result, b)
		}
	}
	sort.Float64s(result)
	return result
}

// Combine ports CreditsCandidateCombiner. Gaps remain separate segments; this
// neither labels intervening content as a coda nor assumes it is safe to skip.
func Combine(candidates []Segment, windowEnd float64, minimumDuration int) []Segment {
	boundaries := HardBoundaries(candidates, windowEnd)
	var ordered []Segment
	for _, c := range candidates {
		if !c.Valid() {
			continue
		}
		if c.Source != ChapterSource {
			for _, b := range boundaries {
				if b > c.Start+boundaryTolerance {
					if c.End > b {
						c.End = b
						if c.End-c.Start < float64(minimumDuration) {
							c = Segment{}
						}
					}
					break
				}
			}
		}
		if !c.Valid() {
			continue
		}
		blocked := false
		for _, b := range boundaries {
			if b >= c.End-boundaryTolerance {
				blocked = true
				break
			}
		}
		if ReachesWindowEnd(c.End, windowEnd, minimumDuration) && c.End < windowEnd && !blocked {
			c.End = windowEnd
		}
		ordered = append(ordered, c)
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Start < ordered[j].Start })
	var result []Segment
	for _, candidate := range ordered {
		if len(result) > 0 {
			current := &result[len(result)-1]
			blocked := false
			for _, b := range boundaries {
				if b >= current.End-boundaryTolerance && b <= candidate.Start+boundaryTolerance {
					blocked = true
					break
				}
			}
			if candidate.Start <= current.End+MaximumSceneMergeGapSeconds && !blocked {
				current.End = max(current.End, candidate.End)
				current.Source = CombinedSource
				continue
			}
		}
		result = append(result, candidate)
	}
	return result
}

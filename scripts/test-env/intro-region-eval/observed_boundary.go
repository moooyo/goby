package main

import (
	"errors"
	"math"
	"math/bits"
	"sort"
)

const boundaryAuditCacheEntries = 4096

type boundaryAudit struct {
	Begin           float64
	Finish          float64
	Bounds          [3][2]float64
	ObservedBounds  [3][2]float64
	Support         uint32
	Moving          uint32
	MinimumCoverage int
	Reason          string
	ComponentFirst  int
	ComponentLast   int
}

type boundaryAuditKey struct {
	Begin   float64
	Finish  float64
	Support uint32
}

type boundaryComponentKey struct {
	Support uint32
	First   int
	Last    int
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validateBoundarySource(s source, work *budget) error {
	if len(s.PTS) != 1200 || len(s.Raw) != 1200*frameBytes {
		return errors.New("incomplete boundary source slots")
	}
	for i, pts := range s.PTS {
		if err := work.clock(); err != nil {
			return err
		}
		nominal := float64(i) / 10
		if !finite(pts) || pts < nominal || pts >= float64(i+1)/10 || i > 0 && pts <= s.PTS[i-1] {
			return errors.New("uncertain or invalid boundary source PTS")
		}
	}
	return nil
}

func validateBoundaryViews(views [3]view, work *budget) error {
	for _, v := range views {
		if len(v.Frames) == 0 {
			return errors.New("empty boundary view")
		}
		for i, f := range v.Frames {
			if err := work.clock(); err != nil {
				return err
			}
			if !finite(f.PTS) || f.PTS < 0 || i > 0 && f.PTS <= v.Frames[i-1].PTS {
				return errors.New("invalid actual boundary PTS")
			}
		}
	}
	return nil
}

func validateBoundaryMappings(views [3]view, mappings [3][]int, clocks [2]clockHypothesis, work *budget) error {
	for _, clock := range clocks {
		if !finite(clock.Scale) || !finite(clock.Offset) || clock.Scale <= 0 {
			return errors.New("invalid boundary clock")
		}
	}
	for source, mapping := range mappings {
		if len(mapping) != len(views[0].Frames) {
			return errors.New("invalid boundary mapping length")
		}
		previous := -1
		for _, index := range mapping {
			if err := work.clock(); err != nil {
				return err
			}
			if index < -1 || index >= len(views[source].Frames) || index >= 0 && index <= previous {
				return errors.New("invalid or reused boundary source ownership")
			}
			if index >= 0 {
				previous = index
			}
		}
	}
	return nil
}

func strictlyInsideBoundary(views [3]view, mappings [3][]int, ai int, bounds [3][2]float64) bool {
	if ai < 0 || ai >= len(views[0].Frames) {
		return false
	}
	for source := 0; source < 3; source++ {
		index := mappings[source][ai]
		if index < 0 || index >= len(views[source].Frames) {
			return false
		}
		pts := views[source].Frames[index].PTS
		if !finite(pts) || pts < bounds[source][0] || pts >= bounds[source][1] {
			return false
		}
	}
	return true
}

// The original support is immutable. Interior tolerance does not grant any
// evidence before the first or after the last full-clique support observation.
func observedCommonComponents(views [3]view, mappings [3][]int, clocks [2]clockHypothesis, appearance []uint32, start, end int, support uint32, original [3][2]float64, work *budget) ([]boundaryAudit, error) {
	result := []boundaryAudit{}
	first, last, runStart := -1, -1, -1
	finishComponent := func() {
		if first >= 0 && last > first {
			boundary := projectObservedComponent(views, mappings, clocks, first, last, support, original)
			if boundary.Reason == "" {
				result = append(result, boundary)
			}
		}
		first, last, runStart = -1, -1, -1
	}
	for ai := start; ai < end; ai++ {
		if err := work.clock(); err != nil {
			return nil, err
		}
		if appearance[ai]&support != support || !strictlyInsideBoundary(views, mappings, ai, original) {
			finishComponent()
			continue
		}
		if runStart >= 0 {
			for source := 0; source < 3; source++ {
				if mappings[source][ai] != mappings[source][ai-1]+1 {
					finishComponent()
					break
				}
			}
		}
		if runStart < 0 {
			runStart = ai
			continue
		}
		previous := ai - 1
		for ; previous >= runStart; previous-- {
			if err := work.clock(); err != nil {
				return nil, err
			}
			span := true
			for source := 0; source < 3; source++ {
				span = span && views[source].Frames[mappings[source][ai]].PTS-views[source].Frames[mappings[source][previous]].PTS >= .5
			}
			if span {
				break
			}
		}
		if previous < runStart {
			continue
		}
		moving, err := groupMotion(views, [3]int{ai, mappings[1][ai], mappings[2][ai]}, [3]int{previous, mappings[1][previous], mappings[2][previous]}, support, work)
		if err != nil {
			return nil, err
		}
		if bits.OnesCount32(moving) < 4 {
			continue
		}
		if first < 0 {
			first = runStart
		}
		last = ai
	}
	finishComponent()
	for i := range result {
		// This extent identifies the complete fixed-support component only.
		// It never supplies published endpoints outside the original window.
		first := sort.Search(len(views[0].Frames), func(j int) bool { return views[0].Frames[j].PTS >= result[i].Begin })
		last := sort.Search(len(views[0].Frames), func(j int) bool { return views[0].Frames[j].PTS >= result[i].Finish })
		if last >= len(appearance) {
			last = len(appearance) - 1
		}
		connected := func(left, right int) bool {
			if left < 0 || right >= len(appearance) || appearance[left]&support != support || appearance[right]&support != support {
				return false
			}
			for source := range mappings {
				if mappings[source][left] < 0 || mappings[source][right] != mappings[source][left]+1 {
					return false
				}
			}
			return true
		}
		for connected(first-1, first) {
			if err := work.clock(); err != nil {
				return nil, err
			}
			first--
		}
		for connected(last, last+1) {
			if err := work.clock(); err != nil {
				return nil, err
			}
			last++
		}
		result[i].ComponentFirst, result[i].ComponentLast = first, last
	}
	return result, nil
}

func projectObservedComponent(views [3]view, mappings [3][]int, clocks [2]clockHypothesis, first, last int, support uint32, original [3][2]float64) boundaryAudit {
	result := boundaryAudit{Begin: original[0][0], Finish: original[0][1], Support: support}
	for source := 0; source < 3; source++ {
		scale, offset := 1.0, 0.0
		if source > 0 {
			scale, offset = clocks[source-1].Scale, clocks[source-1].Offset
		}
		begin := views[source].Frames[mappings[source][first]].PTS
		finish := views[source].Frames[mappings[source][last]].PTS
		result.ObservedBounds[source] = [2]float64{begin, finish}
		result.Begin = math.Max(result.Begin, (begin-offset)/scale)
		result.Finish = math.Min(result.Finish, (finish-offset)/scale)
	}
	for source := 0; source < 3; source++ {
		scale, offset := 1.0, 0.0
		if source > 0 {
			scale, offset = clocks[source-1].Scale, clocks[source-1].Offset
		}
		begin, finish := result.Begin*scale+offset, result.Finish*scale+offset
		// Correct floating-point round trips inward only. No tolerance grants
		// additional content outside an actual observation.
		for begin < result.ObservedBounds[source][0] {
			result.Begin = math.Nextafter(result.Begin, math.Inf(1))
			begin = result.Begin*scale + offset
		}
		for finish > result.ObservedBounds[source][1] {
			result.Finish = math.Nextafter(result.Finish, math.Inf(-1))
			finish = result.Finish*scale + offset
		}
	}
	for source := 0; source < 3; source++ {
		scale, offset := 1.0, 0.0
		if source > 0 {
			scale, offset = clocks[source-1].Scale, clocks[source-1].Offset
		}
		result.Bounds[source] = [2]float64{result.Begin*scale + offset, result.Finish*scale + offset}
		duration := result.Bounds[source][1] - result.Bounds[source][0]
		if duration < 8 || duration > 90 {
			result.Reason = "duration"
			return result
		}
	}
	result.Reason = ""
	return result
}

// Recompute every appearance edge and six-descriptor motion after trimming.
// Unknown mapped samples remain in the anchor-sample coverage denominator.
func auditObservedBoundary(views [3]view, mappings [3][]int, boundary boundaryAudit, cache *posteriorEvidenceCache, work *budget) (boundaryAudit, error) {
	result := boundary
	start := sort.Search(len(views[0].Frames), func(i int) bool { return views[0].Frames[i].PTS >= result.Begin })
	end := sort.Search(len(views[0].Frames), func(i int) bool { return views[0].Frames[i].PTS >= result.Finish })
	if start >= end || !spatial(result.Support) {
		result.Reason = "appearance"
		return result, nil
	}
	appearance := make([]uint32, end-start)
	var counts [patchCount]int
	for ai := start; ai < end; ai++ {
		if err := work.clock(); err != nil {
			return boundaryAudit{}, err
		}
		if !strictlyInsideBoundary(views, mappings, ai, result.Bounds) {
			continue
		}
		mask := result.Support
		for left := 0; left < 3; left++ {
			for right := left + 1; right < 3; right++ {
				for patch := 0; patch < patchCount; patch++ {
					bit := uint32(1) << uint(patch)
					if result.Support&bit == 0 {
						continue
					}
					close, _, err := closePatch(views[left].Frames[mappings[left][ai]].Patches[patch], views[right].Frames[mappings[right][ai]].Patches[patch], work)
					if err != nil {
						return boundaryAudit{}, err
					}
					if !close {
						mask &^= bit
					}
				}
			}
		}
		appearance[ai-start] = mask
		for patch := 0; patch < patchCount; patch++ {
			if mask&(1<<uint(patch)) != 0 {
				counts[patch]++
			}
		}
	}
	result.MinimumCoverage = 1000
	for patch, count := range counts {
		if result.Support&(1<<uint(patch)) != 0 {
			if count*1000 < (end-start)*850 {
				result.Reason = "appearance"
				return result, nil
			}
			result.MinimumCoverage = min(result.MinimumCoverage, count*1000/(end-start))
		}
	}
	var events [patchCount]int
	var last [patchCount]float64
	for patch := range last {
		last[patch] = math.Inf(-1)
	}
	for ai := start; ai < end; ai++ {
		previous := views[0].Frames[ai].Previous
		if previous < start || previous >= end || !strictlyInsideBoundary(views, mappings, ai, result.Bounds) || !strictlyInsideBoundary(views, mappings, previous, result.Bounds) {
			continue
		}
		mask := appearance[ai-start] & appearance[previous-start] & result.Support
		moving, err := groupMotion(views, [3]int{ai, mappings[1][ai], mappings[2][ai]}, [3]int{previous, mappings[1][previous], mappings[2][previous]}, mask, work)
		if err != nil {
			return boundaryAudit{}, err
		}
		for patch := 0; patch < patchCount; patch++ {
			if moving&(1<<uint(patch)) != 0 && views[0].Frames[ai].PTS-last[patch] >= 1-1e-7 {
				events[patch]++
				last[patch] = views[0].Frames[ai].PTS
			}
		}
	}
	for patch, count := range events {
		if count >= 3 {
			result.Moving |= 1 << uint(patch)
		}
	}
	if bits.OnesCount32(result.Moving) < 4 {
		result.Reason = "motion"
		return result, nil
	}
	reason, err := stateAndPeriodAudit(views, mappings, start, end, result.Support, result.Bounds, cache, work)
	if err != nil {
		return boundaryAudit{}, err
	}
	result.Reason = reason
	return result, nil
}

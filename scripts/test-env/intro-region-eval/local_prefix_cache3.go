package main

import (
	"errors"
	"math"
	"math/bits"
	"sort"
)

type geometryTrial struct {
	Geometry               geometry `json:"geometry"`
	IndependentAnchorVotes int      `json:"independentAnchorVotes"`
	MatchingPairs          int      `json:"matchingPairs"`
	Distance               int64    `json:"distance"`
}

type geometryResult struct {
	Anchor    string          `json:"anchor"`
	Source    string          `json:"source"`
	Selected  geometryTrial   `json:"selected"`
	Completed bool            `json:"completed"`
	Qualified bool            `json:"qualified"`
	Trials    []geometryTrial `json:"trials"`
}

func nominateGeometry(a source, b source, work *budget) (geometryResult, error) {
	result := geometryResult{Anchor: a.Info.ID, Source: b.Info.ID}
	indices := make([]int, 240)
	for i := range indices {
		indices[i] = i * 5
	}
	av, err := makeView(a, neutral(), indices, work)
	if err != nil {
		return result, err
	}
	penalty := func(g geometry) float64 {
		return math.Abs(g.ScaleX-1) + math.Abs(g.ScaleY-1) + float64(absInt(g.ShiftX)+absInt(g.ShiftY))/100
	}
	for _, g := range geometryGrid() {
		bv, err := makeView(b, g, indices, work)
		if err != nil {
			return result, err
		}
		trial := geometryTrial{Geometry: g}
		for ai := 0; ai < len(av.Frames); ai += 2 {
			voted := false
			for bi := 0; bi < len(bv.Frames); bi += 2 {
				m, err := compareFrames(av, ai, bv, bi, work)
				if err != nil {
					return result, err
				}
				if spatial(m.Appearance) {
					voted = true
					trial.MatchingPairs++
					trial.Distance += int64(m.Distance)
				}
			}
			if voted {
				trial.IndependentAnchorVotes++
			}
		}
		result.Trials = append(result.Trials, trial)
		best := result.Selected
		if len(result.Trials) == 1 || trial.IndependentAnchorVotes > best.IndependentAnchorVotes || trial.IndependentAnchorVotes == best.IndependentAnchorVotes && (trial.Distance < best.Distance || trial.Distance == best.Distance && penalty(g) < penalty(best.Geometry)) {
			result.Selected = trial
		}
	}
	result.Completed = true
	result.Qualified = result.Selected.IndependentAnchorVotes >= 3
	return result, nil
}

type appearanceMatrix struct {
	Width int
	Masks []uint32
}

func makeMatrix(a, b view, work *budget) (appearanceMatrix, error) {
	matrix := appearanceMatrix{Width: len(b.Frames), Masks: make([]uint32, len(a.Frames)*len(b.Frames))}
	for ai, aFrame := range a.Frames {
		for bi, bFrame := range b.Frames {
			var mask uint32
			for patch := 0; patch < patchCount; patch++ {
				close, _, err := closePatch(aFrame.Patches[patch], bFrame.Patches[patch], work)
				if err != nil {
					return appearanceMatrix{}, err
				}
				if close {
					mask |= 1 << uint(patch)
				}
			}
			matrix.Masks[ai*matrix.Width+bi] = mask
		}
	}
	return matrix, nil
}

func (m appearanceMatrix) at(a, b int) uint32 {
	if a < 0 || b < 0 {
		return 0
	}
	return m.Masks[a*m.Width+b]
}

type clockHypothesis struct {
	Scale             float64 `json:"scale"`
	Offset            float64 `json:"offset"`
	PrefixSupportMask uint32  `json:"prefixSupportMask"`
	PrefixDynamicMask uint32  `json:"prefixDynamicMask"`
	Mapping           []int   `json:"-"`
}

func affineMapping(a, b view, scale, offset float64, work *budget) ([]int, error) {
	pts := make([]float64, len(b.Frames))
	for i, f := range b.Frames {
		pts[i] = f.PTS
	}
	mapping := make([]int, len(a.Frames))
	owners := make([]int, len(b.Frames))
	residual := make([]float64, len(b.Frames))
	for i := range owners {
		owners[i] = -1
	}
	for ai, frame := range a.Frames {
		if err := work.clock(); err != nil {
			return nil, err
		}
		target := frame.PTS*scale + offset
		bi := nearest(pts, target, .060001)
		mapping[ai] = bi
		if bi < 0 {
			continue
		}
		distance := math.Abs(pts[bi] - target)
		if owners[bi] < 0 {
			owners[bi] = ai
			residual[bi] = distance
			continue
		}
		if distance < residual[bi] {
			mapping[owners[bi]] = -1
			owners[bi] = ai
			residual[bi] = distance
		} else {
			mapping[ai] = -1
		}
	}
	return mapping, nil
}

func pairClockMotion(a view, ai, ap int, b view, bi, bp int, mask uint32, work *budget) (uint32, error) {
	var result uint32
	for patch := 0; patch < patchCount; patch++ {
		if mask&(1<<uint(patch)) != 0 {
			moving, err := possibleGroupPairMotion(a.Frames[ai].Patches[patch], a.Frames[ap].Patches[patch], b.Frames[bi].Patches[patch], b.Frames[bp].Patches[patch], work)
			if err != nil {
				return 0, err
			}
			if moving {
				result |= 1 << uint(patch)
			}
		}
	}
	return result, nil
}

// This is only a necessary bound for a later six-descriptor motion mask. The
// pair-only change denominator cannot safely stand in for the group mask.
func possibleGroupPairMotion(a, ap, b, bp descriptor, work *budget) (bool, error) {
	if err := work.patch(); err != nil {
		return false, err
	}
	if !a.Usable || !ap.Usable || !b.Usable || !bp.Usable {
		return false, nil
	}
	active, changeA, changeB, common := 0, 0, 0, 0
	for i := 0; i < 2; i++ {
		mask := a.Reliable[i] & ap.Reliable[i] & b.Reliable[i] & bp.Reliable[i]
		x, y := (a.Bits[i]^ap.Bits[i])&mask, (b.Bits[i]^bp.Bits[i])&mask
		active += bits.OnesCount64(mask)
		changeA += bits.OnesCount64(x)
		changeB += bits.OnesCount64(y)
		common += bits.OnesCount64(x & y & ^(a.Bits[i] ^ b.Bits[i]) & ^(ap.Bits[i] ^ bp.Bits[i]))
	}
	return active >= 80 && changeA >= 12 && changeB >= 12 && common >= 6, nil
}

func discoverClocks(a, b view, matrix appearanceMatrix, work *budget) ([]clockHypothesis, error) {
	result := []clockHypothesis{}
	for _, scale := range []float64{.98, 1, 1.02} {
		for offsetIndex := -2400; offsetIndex <= 2400; offsetIndex++ {
			offset := float64(offsetIndex) / 20
			mapping, err := affineMapping(a, b, scale, offset, work)
			if err != nil {
				return nil, err
			}
			var counts [patchCount]int
			for ai, bi := range mapping {
				mask := matrix.at(ai, bi)
				for patch := 0; patch < patchCount; patch++ {
					if mask&(1<<uint(patch)) != 0 {
						counts[patch]++
					}
				}
			}
			var support uint32
			for patch, count := range counts {
				if count >= 68 {
					support |= 1 << uint(patch)
				}
			}
			if !spatial(support) {
				continue
			}
			var events [patchCount]int
			var last [patchCount]float64
			for patch := range last {
				last[patch] = -1e9
			}
			for ai, bi := range mapping {
				ap := a.Frames[ai].Previous
				if bi < 0 || ap < 0 || mapping[ap] < 0 {
					continue
				}
				mask := matrix.at(ai, bi) & matrix.at(ap, mapping[ap]) & support
				moving, err := pairClockMotion(a, ai, ap, b, bi, mapping[ap], mask, work)
				if err != nil {
					return nil, err
				}
				for patch := 0; patch < patchCount; patch++ {
					if moving&(1<<uint(patch)) != 0 && a.Frames[ai].PTS-last[patch] >= 1-1e-7 {
						events[patch]++
						last[patch] = a.Frames[ai].PTS
					}
				}
			}
			var dynamic uint32
			for patch, count := range events {
				if count >= 3 {
					dynamic |= 1 << uint(patch)
				}
			}
			if bits.OnesCount32(dynamic) < 4 {
				continue
			}
			if err := work.retain(); err != nil {
				return nil, err
			}
			result = append(result, clockHypothesis{scale, offset, support, dynamic, mapping})
		}
	}
	return result, nil
}

type groupWitness struct {
	SourceIDs               [3]string          `json:"sourceIDs"`
	Geometries              [3]geometry        `json:"geometries"`
	Clocks                  [2]clockHypothesis `json:"clocks"`
	AnchorStart             float64            `json:"anchorStart"`
	AnchorEnd               float64            `json:"anchorEnd"`
	SupportMask             uint32             `json:"supportMask"`
	DynamicMask             uint32             `json:"dynamicMask"`
	MinimumCoveragePermille int                `json:"minimumCoveragePermille"`
	PassingWindows          int                `json:"passingWindows"`
	SourceBounds            [3][2]float64      `json:"sourceBounds"`
	ObservedEdgeBounds      [3][2]float64      `json:"observedEdgeBounds"`
	BoundaryPolicy          string             `json:"boundaryPolicy"`
	ComponentAnchorBounds   [2]float64         `json:"componentAnchorBounds"`
}

type boundaryWitnessEvidence struct {
	Clocks                [2]clockHypothesis `json:"clocks"`
	SourceBounds          [3][2]float64      `json:"sourceBounds"`
	ComponentIndices      [2]int             `json:"componentIndices"`
	Support               uint32             `json:"supportMask"`
	ComponentSourceBounds [3][2]float64      `json:"componentSourceBounds"`
}

type boundaryAmbiguityEvidence struct {
	Reason      string                     `json:"reason"`
	Witnesses   [2]boundaryWitnessEvidence `json:"witnesses"`
	SourceIndex int                        `json:"sourceIndex"`
}

type closureSummary struct {
	ClockCombinations        int                        `json:"clockCombinations"`
	AggregateAppearancePass  int                        `json:"aggregateAppearancePass"`
	AggregateGroupMotionPass int                        `json:"aggregateGroupMotionPass"`
	WindowAppearancePass     int                        `json:"windowAppearancePass"`
	WindowMotionPass         int                        `json:"windowMotionPass"`
	WindowStateRejected      int                        `json:"windowStateRejected"`
	WindowPeriodicRejected   int                        `json:"windowPeriodicRejected"`
	BoundaryCandidates       int                        `json:"boundaryCandidates"`
	BoundaryRejected         int                        `json:"boundaryRejected"`
	BoundaryAuditCacheHits   int                        `json:"boundaryAuditCacheHits"`
	BoundaryRemeasured       int                        `json:"boundaryRemeasured"`
	BoundaryDominatedWindows int                        `json:"boundaryDominatedWindows"`
	BoundaryAmbiguousClocks  int                        `json:"boundaryAmbiguousClocks"`
	CohortBoundaryAmbiguous  bool                       `json:"cohortBoundaryAmbiguous"`
	BoundaryAmbiguity        *boundaryAmbiguityEvidence `json:"boundaryAmbiguity,omitempty"`
	RejectedAmbiguousGroups  []groupWitness             `json:"rejectedAmbiguousGroups,omitempty"`
	PosteriorCache           *posteriorCacheStats       `json:"posteriorCache,omitempty"`
}

func groupMotion(views [3]view, current, previous [3]int, mask uint32, work *budget) (uint32, error) {
	var result uint32
	for patch := 0; patch < patchCount; patch++ {
		if mask&(1<<uint(patch)) != 0 {
			var now, before [3]descriptor
			for source := 0; source < 3; source++ {
				now[source] = views[source].Frames[current[source]].Patches[patch]
				before[source] = views[source].Frames[previous[source]].Patches[patch]
			}
			moving, err := synchronousGroup(now, before, work)
			if err != nil {
				return 0, err
			}
			if moving {
				result |= 1 << uint(patch)
			}
		}
	}
	return result, nil
}

func selfEvidence(a, b frame, support uint32, work *budget) (bool, bool, bool, error) {
	matched, comparable, total := 0, 0, bits.OnesCount32(support)
	for patch := 0; patch < patchCount; patch++ {
		if support&(1<<uint(patch)) != 0 {
			x, y := a.Patches[patch], b.Patches[patch]
			close, _, err := closePatch(x, y, work)
			if err != nil {
				return false, false, false, err
			}
			if close {
				matched++
			}
			active := bits.OnesCount64(x.Reliable[0]&y.Reliable[0]) + bits.OnesCount64(x.Reliable[1]&y.Reliable[1])
			if x.Usable && y.Usable && active >= 80 {
				comparable++
			}
		}
	}
	return comparable*1000 >= total*850, matched*1000 >= total*850, (matched+total-comparable)*1000 < total*850, nil
}

func insideSourceWindow(views [3]view, mappings [3][]int, ai int, bounds [3][2]float64) bool {
	if ai < 0 {
		return false
	}
	for source := 0; source < 3; source++ {
		i := mappings[source][ai]
		if i < 0 {
			return false
		}
		pts := views[source].Frames[i].PTS
		if pts < bounds[source][0]-1e-7 || pts >= bounds[source][1]-1e-7 {
			return false
		}
	}
	return true
}

func stateAndPeriodAudit(views [3]view, mappings [3][]int, start, end int, support uint32, bounds [3][2]float64, cache *posteriorEvidenceCache, work *budget) (string, error) {
	for source := range views {
		states := []int{}
		for ai := start; ai < end && len(states) < 6; ai++ {
			i := mappings[source][ai]
			if !insideSourceWindow(views, mappings, ai, bounds) {
				continue
			}
			comparable, _, _, err := posteriorSelfEvidence(cache, views, source, i, i, support, work)
			if err != nil {
				return "", err
			}
			if !comparable {
				continue
			}
			newState := true
			for _, old := range states {
				_, _, different, err := posteriorSelfEvidence(cache, views, source, i, old, support, work)
				if err != nil {
					return "", err
				}
				if !different {
					newState = false
					break
				}
			}
			if newState {
				states = append(states, i)
			}
		}
		if len(states) < 6 {
			return "state", nil
		}
		for lag := 5; lag <= 40; lag++ {
			matched, denominator := 0, end-start-lag
			if denominator <= 0 {
				continue
			}
			for ai := start + lag; ai < end; ai++ {
				if err := work.clock(); err != nil {
					return "", err
				}
				i, j := mappings[source][ai], mappings[source][ai-lag]
				if !insideSourceWindow(views, mappings, ai, bounds) || !insideSourceWindow(views, mappings, ai-lag, bounds) {
					matched++
					continue
				}
				_, _, different, err := posteriorSelfEvidence(cache, views, source, i, j, support, work)
				if err != nil {
					return "", err
				}
				if !different {
					matched++
				}
			}
			if matched*1000 >= denominator*850 {
				return "periodic", nil
			}
		}
	}
	return "", nil
}

func closeGroups(views [3]view, ab, ac, bc appearanceMatrix, bs, cs []clockHypothesis, work *budget) ([]groupWitness, closureSummary, error) {
	return closeGroupsWithBoundaryDominance(views, ab, ac, bc, bs, cs, work, true)
}

func closeGroupsWithBoundaryDominance(views [3]view, ab, ac, bc appearanceMatrix, bs, cs []clockHypothesis, work *budget, useDominance bool) ([]groupWitness, closureSummary, error) {
	result := []groupWitness{}
	summary := closureSummary{}
	var earliestComponentEnd, latestComponentStart [3]*boundaryWitnessEvidence
	if err := validateBoundaryViews(views, work); err != nil {
		return nil, summary, err
	}
	anchor := views[0]
	count := len(anchor.Frames)
	identity := make([]int, count)
	for i := range identity {
		identity[i] = i
	}
	for _, b := range bs {
		for _, c := range cs {
			summary.ClockCombinations++
			mappings := [3][]int{identity, b.Mapping, c.Mapping}
			if err := validateBoundaryMappings(views, mappings, [2]clockHypothesis{b, c}, work); err != nil {
				return nil, summary, err
			}
			appearance := make([]uint32, count)
			dynamic := make([]uint32, count)
			prefix := make([][patchCount]int, count+1)
			var aggregate uint32
			for ai := 0; ai < count; ai++ {
				if err := work.clock(); err != nil {
					return nil, summary, err
				}
				bi, ci := b.Mapping[ai], c.Mapping[ai]
				if bi >= 0 && ci >= 0 {
					appearance[ai] = ab.at(ai, bi) & ac.at(ai, ci) & bc.at(bi, ci)
				}
				prefix[ai+1] = prefix[ai]
				for patch := 0; patch < patchCount; patch++ {
					if appearance[ai]&(1<<uint(patch)) != 0 {
						prefix[ai+1][patch]++
					}
				}
			}
			for patch, n := range prefix[count] {
				if n >= 68 {
					aggregate |= 1 << uint(patch)
				}
			}
			if !spatial(aggregate) {
				continue
			}
			summary.AggregateAppearancePass++
			var eventPTS [patchCount][]float64
			for ai := 0; ai < count; ai++ {
				ap := anchor.Frames[ai].Previous
				if ap < 0 || b.Mapping[ai] < 0 || c.Mapping[ai] < 0 || b.Mapping[ap] < 0 || c.Mapping[ap] < 0 {
					continue
				}
				mask := appearance[ai] & appearance[ap] & aggregate
				moving, err := groupMotion(views, [3]int{ai, b.Mapping[ai], c.Mapping[ai]}, [3]int{ap, b.Mapping[ap], c.Mapping[ap]}, mask, work)
				if err != nil {
					return nil, summary, err
				}
				dynamic[ai] = moving
				for patch := 0; patch < patchCount; patch++ {
					if moving&(1<<uint(patch)) != 0 {
						eventPTS[patch] = append(eventPTS[patch], anchor.Frames[ai].PTS)
					}
				}
			}
			var aggregateDynamic uint32
			for patch, events := range eventPTS {
				previous := -1e9
				n := 0
				for _, pts := range events {
					if pts-previous >= 1-1e-7 {
						n++
						previous = pts
					}
				}
				if n >= 3 {
					aggregateDynamic |= 1 << uint(patch)
				}
			}
			if bits.OnesCount32(aggregateDynamic&aggregate) < 4 {
				continue
			}
			summary.AggregateGroupMotionPass++
			best := groupWitness{SourceIDs: [3]string{views[0].ID, views[1].ID, views[2].ID}, Geometries: [3]geometry{views[0].Geometry, views[1].Geometry, views[2].Geometry}, Clocks: [2]clockHypothesis{b, c}}
			minimumDuration := 8 / math.Min(1, math.Min(b.Scale, c.Scale))
			maximumDuration := 90 / math.Max(1, math.Max(b.Scale, c.Scale))
			if summary.PosteriorCache == nil {
				summary.PosteriorCache = &posteriorCacheStats{}
			}
			cache := newPosteriorEvidenceCache(views, [2]clockHypothesis{b, c}, work.ctx, summary.PosteriorCache)
			boundaryCache := make(map[boundaryAuditKey]boundaryAudit)
			dominantAudits := make(map[boundaryComponentKey]boundaryAudit)
			ambiguousBoundary := false
			var clockEarliestEnd, clockLatestStart *boundaryWitnessEvidence
			for start := 0; start < count; start++ {
				for end := start + 1; end <= count; end++ {
					duration := float64(end-start) / 10
					if duration < minimumDuration-1e-7 {
						continue
					}
					if duration > maximumDuration+1e-7 {
						break
					}
					if err := work.clock(); err != nil {
						return nil, summary, err
					}
					begin, finish := float64(start)/10, float64(end)/10
					bounds := [3][2]float64{{begin, finish}, {begin*b.Scale + b.Offset, finish*b.Scale + b.Offset}, {begin*c.Scale + c.Offset, finish*c.Scale + c.Offset}}
					// With first-at-or-after 100 ms source slots and a 60 ms affine
					// residual, only the first and last anchor slots can cross a
					// nominal window boundary. Retain their denominator contribution.
					var excluded uint32
					if !insideSourceWindow(views, mappings, start, bounds) {
						excluded |= appearance[start]
					}
					var excludedLast uint32
					if end-1 != start && !insideSourceWindow(views, mappings, end-1, bounds) {
						excludedLast = appearance[end-1]
					}
					var support uint32
					minimumCoverage := 1000
					for patch := 0; patch < patchCount; patch++ {
						n := prefix[end][patch] - prefix[start][patch]
						if excluded&(1<<uint(patch)) != 0 {
							n--
						}
						if excludedLast&(1<<uint(patch)) != 0 {
							n--
						}
						if n*1000 >= (end-start)*850 {
							support |= 1 << uint(patch)
							minimumCoverage = min(minimumCoverage, n*1000/(end-start))
						}
					}
					if !spatial(support) {
						continue
					}
					summary.WindowAppearancePass++
					var moving uint32
					for patch := 0; patch < patchCount; patch++ {
						if support&(1<<uint(patch)) == 0 {
							continue
						}
						last := -1e9
						n := 0
						for ai := start; ai < end; ai++ {
							previous := anchor.Frames[ai].Previous
							if previous < start || dynamic[ai]&(1<<uint(patch)) == 0 || !insideSourceWindow(views, mappings, ai, bounds) || !insideSourceWindow(views, mappings, previous, bounds) {
								continue
							}
							pts := anchor.Frames[ai].PTS
							if pts-last >= 1-1e-7 {
								n++
								last = pts
							}
						}
						if n >= 3 {
							moving |= 1 << uint(patch)
						}
					}
					if bits.OnesCount32(moving) < 4 {
						continue
					}
					summary.WindowMotionPass++
					summary.BoundaryCandidates++
					boundaries, err := observedCommonComponents(views, mappings, [2]clockHypothesis{b, c}, appearance, start, end, support, bounds, work)
					if err != nil {
						return nil, summary, err
					}
					if len(boundaries) == 0 {
						summary.BoundaryRejected++
						continue
					}
					for _, boundary := range boundaries {
						componentKey := boundaryComponentKey{support, boundary.ComponentFirst, boundary.ComponentLast}
						if err := work.clock(); err != nil {
							return nil, summary, err
						}
						priorDominant, dominantFound := dominantAudits[componentKey]
						if useDominance && dominantFound && priorDominant.Begin <= boundary.Begin && priorDominant.Finish >= boundary.Finish {
							// A fully audited containing interval in this identical
							// component has already supplied both the best-result
							// and ambiguity evidence. Its contained subwindow cannot
							// replace either. Never reuse its metrics for the child.
							summary.BoundaryDominatedWindows++
							continue
						}
						key := boundaryAuditKey{boundary.Begin, boundary.Finish, support}
						if err := work.clock(); err != nil {
							return nil, summary, err
						}
						if prior, found := boundaryCache[key]; found {
							summary.BoundaryAuditCacheHits++
							boundary.Moving, boundary.MinimumCoverage, boundary.Reason = prior.Moving, prior.MinimumCoverage, prior.Reason
						} else {
							summary.BoundaryRemeasured++
							boundary, err = auditObservedBoundary(views, mappings, boundary, cache, work)
							if err != nil {
								return nil, summary, err
							}
							if len(boundaryCache) < boundaryAuditCacheEntries {
								boundaryCache[key] = boundary
							}
						}
						if boundary.Reason == "state" {
							summary.WindowStateRejected++
							continue
						}
						if boundary.Reason == "periodic" {
							summary.WindowPeriodicRejected++
							continue
						}
						if boundary.Reason != "" {
							summary.BoundaryRejected++
							continue
						}
						best.PassingWindows++
						if dominantFound {
							if boundary.Finish-boundary.Begin > priorDominant.Finish-priorDominant.Begin {
								dominantAudits[componentKey] = boundary
							}
						} else if len(dominantAudits) < boundaryAuditCacheEntries {
							dominantAudits[componentKey] = boundary
						}
						evidence := boundaryWitnessEvidence{Clocks: [2]clockHypothesis{b, c}, SourceBounds: boundary.Bounds, ComponentIndices: [2]int{boundary.ComponentFirst, boundary.ComponentLast}, Support: support}
						for source := range views {
							evidence.ComponentSourceBounds[source] = [2]float64{views[source].Frames[mappings[source][boundary.ComponentFirst]].PTS, views[source].Frames[mappings[source][boundary.ComponentLast]].PTS}
						}
						if clockEarliestEnd == nil || evidence.ComponentIndices[1] < clockEarliestEnd.ComponentIndices[1] {
							copy := evidence
							clockEarliestEnd = &copy
						}
						if clockLatestStart == nil || evidence.ComponentIndices[0] > clockLatestStart.ComponentIndices[0] {
							copy := evidence
							clockLatestStart = &copy
						}
						ambiguousBoundary = ambiguousBoundary || clockEarliestEnd.ComponentIndices[1] <= clockLatestStart.ComponentIndices[0]
						for source := range views {
							if earliestComponentEnd[source] == nil || evidence.ComponentSourceBounds[source][1] < earliestComponentEnd[source].ComponentSourceBounds[source][1] {
								copy := evidence
								earliestComponentEnd[source] = &copy
							}
							if latestComponentStart[source] == nil || evidence.ComponentSourceBounds[source][0] > latestComponentStart[source].ComponentSourceBounds[source][0] {
								copy := evidence
								latestComponentStart[source] = &copy
							}
							if earliestComponentEnd[source].ComponentSourceBounds[source][1] <= latestComponentStart[source].ComponentSourceBounds[source][0] {
								summary.CohortBoundaryAmbiguous = true
								if summary.BoundaryAmbiguity == nil {
									summary.BoundaryAmbiguity = &boundaryAmbiguityEvidence{Reason: "disjoint-fully-audited-source-components", Witnesses: [2]boundaryWitnessEvidence{*earliestComponentEnd[source], *latestComponentStart[source]}, SourceIndex: source}
								}
							}
						}
						trimmedDuration := boundary.Finish - boundary.Begin
						if trimmedDuration > best.AnchorEnd-best.AnchorStart || trimmedDuration == best.AnchorEnd-best.AnchorStart && boundary.Begin < best.AnchorStart {
							best.AnchorStart = boundary.Begin
							best.AnchorEnd = boundary.Finish
							best.SupportMask = support
							best.DynamicMask = boundary.Moving
							best.MinimumCoveragePermille = boundary.MinimumCoverage
							best.SourceBounds = boundary.Bounds
							best.ObservedEdgeBounds = boundary.ObservedBounds
							best.BoundaryPolicy = "observed-common-component-v2"
							best.ComponentAnchorBounds = [2]float64{views[0].Frames[boundary.ComponentFirst].PTS, views[0].Frames[boundary.ComponentLast].PTS}
						}
					}
				}
			}
			if ambiguousBoundary {
				summary.BoundaryAmbiguousClocks++
				continue
			}
			if best.PassingWindows > 0 {
				if err := work.retain(); err != nil {
					return nil, summary, err
				}
				result = append(result, best)
			}
		}
	}
	for i, left := range result {
		for _, right := range result[i+1:] {
			for source := range views {
				if left.SourceBounds[source][1] <= right.SourceBounds[source][0] || right.SourceBounds[source][1] <= left.SourceBounds[source][0] {
					summary.CohortBoundaryAmbiguous = true
					if summary.BoundaryAmbiguity == nil {
						summary.BoundaryAmbiguity = &boundaryAmbiguityEvidence{Reason: "disjoint-returned-source-clock-witnesses", SourceIndex: source, Witnesses: [2]boundaryWitnessEvidence{{Clocks: left.Clocks, SourceBounds: left.SourceBounds, Support: left.SupportMask}, {Clocks: right.Clocks, SourceBounds: right.SourceBounds, Support: right.SupportMask}}}
					}
				}
			}
		}
	}
	if summary.CohortBoundaryAmbiguous {
		summary.RejectedAmbiguousGroups = result
		return []groupWitness{}, summary, nil
	}
	return result, summary, nil
}

func completePrefix(sources map[string]source, work *budget) (map[string]any, error) {
	work.stage = "source-boundary-validation"
	result := map[string]any{"complete": false, "productionResult": false, "geometries": []geometryResult{}, "groups": []groupWitness{}, "searchScope": "One heuristic fixed geometry per source; finite affine clocks; maximal fixed support per nominal window; separate observed full-support components and complete fixed-support remeasurement. Disjoint passing source components or returned source intervals across any clocks make the whole cohort abstain. Not an exhaustive geometry/support-subset search.", "nominationPolicy": "appearance-spatial-v2", "boundaryPolicy": "observed-common-component-v2", "descriptorMatrixLogicalBoundBytes": 24 << 20, "boundaryAuditCacheEntryBound": boundaryAuditCacheEntries, "rawInputBytes": 3 * 1200 * frameBytes}
	ids := []string{}
	for id := range sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) != 3 {
		return result, errors.New("complete-prefix cohort must contain exactly three sources")
	}
	for _, id := range ids {
		if err := validateBoundarySource(sources[id], work); err != nil {
			return result, err
		}
	}
	if sources[ids[0]].Info.SourceSHA256 == sources[ids[1]].Info.SourceSHA256 || sources[ids[0]].Info.SourceSHA256 == sources[ids[2]].Info.SourceSHA256 || sources[ids[1]].Info.SourceSHA256 == sources[ids[2]].Info.SourceSHA256 {
		return result, errors.New("duplicate source content")
	}
	geometries := [3]geometry{neutral(), neutral(), neutral()}
	work.stage = "fixed-geometry-nomination"
	nominations := []geometryResult{}
	qualified := true
	for target := 1; target < 3; target++ {
		nomination, err := nominateGeometry(sources[ids[0]], sources[ids[target]], work)
		nominations = append(nominations, nomination)
		result["geometries"] = nominations
		if err != nil {
			return result, err
		}
		qualified = qualified && nomination.Qualified
		geometries[target] = nomination.Selected.Geometry
	}
	if !qualified {
		result["complete"] = true
		result["stopStage"] = "fixed-geometry-nomination"
		return result, nil
	}
	var views [3]view
	work.stage = "full-prefix-render"
	for i, id := range ids {
		v, err := makeView(sources[id], geometries[i], nil, work)
		if err != nil {
			return result, err
		}
		views[i] = v
	}
	work.stage = "anchor-appearance-matrices"
	ab, err := makeMatrix(views[0], views[1], work)
	if err != nil {
		return result, err
	}
	ac, err := makeMatrix(views[0], views[2], work)
	if err != nil {
		return result, err
	}
	work.stage = "affine-clock-discovery"
	bs, err := discoverClocks(views[0], views[1], ab, work)
	if err != nil {
		return result, err
	}
	result["firstSourceClocks"] = bs
	cs, err := discoverClocks(views[0], views[2], ac, work)
	if err != nil {
		return result, err
	}
	result["secondSourceClocks"] = cs
	if len(bs) == 0 || len(cs) == 0 {
		result["complete"] = true
		result["stopStage"] = "single-clock-prefix-support"
		return result, nil
	}
	work.stage = "third-pair-appearance-matrix"
	bc, err := makeMatrix(views[1], views[2], work)
	if err != nil {
		return result, err
	}
	work.stage = "clique-closure-and-observed-boundaries"
	groups, summary, err := closeGroups(views, ab, ac, bc, bs, cs, work)
	result["closure"] = summary
	if err != nil {
		return result, err
	}
	result["groups"] = groups
	result["complete"] = true
	result["stopStage"] = "complete-clique-window-search"
	return result, nil
}

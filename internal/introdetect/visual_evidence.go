package introdetect

import (
	"fmt"
	"math/bits"
	"sort"
)

// No isolated observation fills a long hole. A one-second ceiling also admits
// the public matcher's one-Hz mechanical fixtures; production samples at 2 Hz.
const maxVisualEvidenceEdgeTicks = TicksPerSecond
const maxVisualStateRepresentatives = 256

type visualSpan struct {
	Interval
	kind int // 1: matched, 2: contradicted; unassigned time is unobservable.
}

type visualTimeline struct {
	matched, contradicted, unobservable int
	minimumBand                         int
	maximumGap, startGap, endGap        int64
	anchors                             []Interval
	allBandsAnchored                    bool
}

type visualStates struct {
	ids      []int
	counts   []int
	total    int
	periodic bool
}

type visualV2Evidence struct {
	metrics          Metrics
	firstA, lastA    int64
	firstB, lastB    int64
	allBandsAnchored bool
	periodic         bool
}

func buildVisualStates(samples []VisualSample, interval Interval, o Options, budget *workBudget) (visualStates, error) {
	value := visualStates{ids: make([]int, len(samples))}
	for i := range value.ids {
		value.ids[i] = -1
	}
	start := sort.Search(len(samples), func(i int) bool { return samples[i].Ticks >= interval.StartTicks })
	end := sort.Search(len(samples), func(i int) bool { return samples[i].Ticks > interval.EndTicks })
	var representatives []uint64
	for i := start; i < end; i++ {
		if err := budget.spend(); err != nil {
			return value, err
		}
		if samples[i].Contrast < o.MinVisualContrast {
			continue
		}
		nearest, distance := -1, 65
		for state, representative := range representatives {
			if err := budget.spend(); err != nil {
				return value, err
			}
			current := bits.OnesCount64(samples[i].Hash ^ representative)
			if current <= o.VisualStateRadius && current < distance {
				nearest, distance = state, current
			}
		}
		if nearest < 0 {
			if len(representatives) == maxVisualStateRepresentatives {
				return value, fmt.Errorf("%w: visual state representatives", ErrLimit)
			}
			nearest = len(representatives)
			representatives = append(representatives, samples[i].Hash)
			value.counts = append(value.counts, 0)
		}
		value.ids[i] = nearest
		value.counts[nearest]++
		value.total++
	}
	// Fixed first representatives never move or union through a near-neighbor
	// chain. Dominance uses the full informative source window, not selected hits.
	periodic, err := periodicVisualEvidence(samples, value.ids, start, end, o, budget)
	value.periodic = periodic
	return value, err
}

func appendVisualSpan(spans []visualSpan, interval Interval, kind int) []visualSpan {
	if interval.EndTicks <= interval.StartTicks {
		return spans
	}
	if len(spans) > 0 && spans[len(spans)-1].kind == kind && spans[len(spans)-1].EndTicks == interval.StartTicks {
		spans[len(spans)-1].EndTicks = interval.EndTicks
		return spans
	}
	return append(spans, visualSpan{interval, kind})
}

func measureVisualTimeline(interval Interval, spans []visualSpan, o Options, budget *workBudget) (visualTimeline, error) {
	value := visualTimeline{minimumBand: 1000, startGap: interval.EndTicks - interval.StartTicks,
		endGap: interval.EndTicks - interval.StartTicks, allBandsAnchored: true}
	var matched, contradicted int64
	lastMatched := interval.StartTicks
	for _, span := range spans {
		if err := budget.spend(); err != nil {
			return value, err
		}
		if span.kind == 1 {
			matched += span.EndTicks - span.StartTicks
			value.maximumGap = max(value.maximumGap, span.StartTicks-lastMatched)
			lastMatched = span.EndTicks
			if span.EndTicks-span.StartTicks >= o.MinVisualAnchorTicks {
				value.anchors = append(value.anchors, span.Interval)
			}
		} else if span.kind == 2 {
			contradicted += span.EndTicks - span.StartTicks
		}
	}
	value.maximumGap = max(value.maximumGap, interval.EndTicks-lastMatched)
	duration := interval.EndTicks - interval.StartTicks
	value.matched, value.contradicted = int(matched*1000/duration), int(contradicted*1000/duration)
	// Integer-rounding residue is conservatively assigned to unobservable time.
	value.unobservable = 1000 - value.matched - value.contradicted
	if len(value.anchors) > 0 {
		value.startGap = value.anchors[0].StartTicks - interval.StartTicks
		value.endGap = interval.EndTicks - value.anchors[len(value.anchors)-1].EndTicks
	}
	// The absolute source clock defines the grid. Cropping never slides a band
	// until a convenient title or scene happens to occupy it.
	fullBands := 0
	for start := interval.StartTicks; start < interval.EndTicks; {
		if err := budget.spend(); err != nil {
			return value, err
		}
		end := min(interval.EndTicks, (start/o.VisualBandTicks+1)*o.VisualBandTicks)
		if start%o.VisualBandTicks != 0 || end-start != o.VisualBandTicks {
			// Edge fragments remain in the full time partition and gap checks.
			// The separate edge-anchor contract governs their discrete samples.
			start = end
			continue
		}
		fullBands++
		var confirmed int64
		for _, span := range spans {
			if err := budget.spend(); err != nil {
				return value, err
			}
			if span.kind == 1 {
				confirmed += max(int64(0), min(end, span.EndTicks)-max(start, span.StartTicks))
			}
		}
		value.minimumBand = min(value.minimumBand, int(confirmed*1000/(end-start)))
		anchored := false
		for _, anchor := range value.anchors {
			if err := budget.spend(); err != nil {
				return value, err
			}
			anchored = anchored || min(end, anchor.EndTicks)-max(start, anchor.StartTicks) >= o.MinVisualAnchorTicks
		}
		value.allBandsAnchored = value.allBandsAnchored && anchored
		start = end
	}
	if fullBands == 0 {
		value.minimumBand, value.allBandsAnchored = 0, false
	}
	return value, nil
}

func measureVisualV2(a, b Episode, audio audioMatch, offset int64, o Options, budget *workBudget) (visualV2Evidence, error) {
	value := visualV2Evidence{firstA: -1, firstB: -1, lastA: -1, lastB: -1}
	statesA, err := buildVisualStates(a.Visual, audio.a, o, budget)
	if err != nil {
		return value, err
	}
	statesB, err := buildVisualStates(b.Visual, audio.b, o, budget)
	if err != nil {
		return value, err
	}
	matchedStatesA, matchedStatesB := make([]int64, len(statesA.counts)), make([]int64, len(statesB.counts))
	start := sort.Search(len(a.Visual), func(i int) bool { return a.Visual[i].Ticks >= audio.a.StartTicks })
	end := sort.Search(len(a.Visual), func(i int) bool { return a.Visual[i].Ticks > audio.a.EndTicks })
	var spansA, spansB []visualSpan
	previousI, previousJ, previousKind := -1, -1, 0
	chainA, chainB := -1, -1
	var chainTicksA, chainTicksB int64
	advance := func(state int, gap int64, chain *int, duration *int64, longest []int64) {
		if state < 0 {
			*chain, *duration = -1, 0
			return
		}
		if *chain != state {
			*duration = 0
		}
		*chain, *duration = state, *duration+gap
		longest[state] = max(longest[state], *duration)
	}
	j, lastTarget := 0, -1
	for i := start; i < end; i++ {
		if err := budget.spend(); err != nil {
			return value, err
		}
		target := a.Visual[i].Ticks + offset
		for j+1 < len(b.Visual) && absolute(b.Visual[j+1].Ticks-target) <= absolute(b.Visual[j].Ticks-target) {
			if err := budget.spend(); err != nil {
				return value, err
			}
			j++
		}
		if j >= len(b.Visual) || j <= lastTarget || b.Visual[j].Ticks < audio.b.StartTicks || b.Visual[j].Ticks > audio.b.EndTicks || absolute(b.Visual[j].Ticks-target) > o.VisualAlignmentTicks {
			previousI, previousJ, previousKind = -1, -1, 0
			chainA, chainB, chainTicksA, chainTicksB = -1, -1, 0, 0
			continue
		}
		lastTarget = j
		kind := 0
		if a.Visual[i].Contrast >= o.MinVisualContrast && b.Visual[j].Contrast >= o.MinVisualContrast {
			kind = 2
			if bits.OnesCount64(a.Visual[i].Hash^b.Visual[j].Hash) <= o.MaxVisualHamming {
				kind = 1
				if value.firstA < 0 {
					value.firstA, value.firstB = a.Visual[i].Ticks, b.Visual[j].Ticks
				}
				value.lastA, value.lastB = a.Visual[i].Ticks, b.Visual[j].Ticks
			}
		}
		stateA, stateB := -1, -1
		var stateGapA, stateGapB int64
		if previousI >= 0 && i == previousI+1 && j == previousJ+1 {
			gapA, gapB := a.Visual[i].Ticks-a.Visual[previousI].Ticks, b.Visual[j].Ticks-b.Visual[previousJ].Ticks
			if gapA <= maxVisualEvidenceEdgeTicks && gapB <= maxVisualEvidenceEdgeTicks {
				spanKind := 0
				if previousKind == 1 && kind == 1 {
					spanKind = 1
				} else if previousKind != 0 && kind != 0 {
					spanKind = 2
				}
				if spanKind != 0 {
					spansA = appendVisualSpan(spansA, Interval{a.Visual[previousI].Ticks, a.Visual[i].Ticks}, spanKind)
					spansB = appendVisualSpan(spansB, Interval{b.Visual[previousJ].Ticks, b.Visual[j].Ticks}, spanKind)
				}
				if spanKind == 1 {
					if state := statesA.ids[i]; state >= 0 && statesA.ids[previousI] == state {
						stateA, stateGapA = state, gapA
					}
					if state := statesB.ids[j]; state >= 0 && statesB.ids[previousJ] == state {
						stateB, stateGapB = state, gapB
					}
				}
			}
		}
		advance(stateA, stateGapA, &chainA, &chainTicksA, matchedStatesA)
		advance(stateB, stateGapB, &chainB, &chainTicksB, matchedStatesB)
		previousI, previousJ, previousKind = i, j, kind
	}
	ta, err := measureVisualTimeline(audio.a, spansA, o, budget)
	if err != nil {
		return value, err
	}
	tb, err := measureVisualTimeline(audio.b, spansB, o, budget)
	if err != nil {
		return value, err
	}
	// Keep one complete partition from the side with less confirmed time.
	// Other metrics independently retain the worse side; no side borrows time.
	partition := ta
	if tb.matched < ta.matched || tb.matched == ta.matched && tb.unobservable > ta.unobservable {
		partition = tb
	}
	m := &value.metrics
	m.VisualAnchorCount = min(len(ta.anchors), len(tb.anchors))
	m.VisualMinBandMatchedPermille = min(ta.minimumBand, tb.minimumBand)
	m.VisualMatchedTimePermille, m.VisualContradictedTimePermille, m.VisualUnobservableTimePermille = partition.matched, partition.contradicted, partition.unobservable
	m.VisualMaxUnconfirmedGapTicks = max(ta.maximumGap, tb.maximumGap)
	m.VisualStartAnchorGapTicks, m.VisualEndAnchorGapTicks = max(ta.startGap, tb.startGap), max(ta.endGap, tb.endGap)
	countStates := func(values []int64) int {
		count := 0
		for _, span := range values {
			if span >= o.MinVisualStateSupportTicks {
				count++
			}
		}
		return count
	}
	m.VisualDistinctStates = min(countStates(matchedStatesA), countStates(matchedStatesB))
	for _, states := range []visualStates{statesA, statesB} {
		for _, count := range states.counts {
			m.VisualDominantStatePermille = max(m.VisualDominantStatePermille, count*1000/max(1, states.total))
		}
	}
	value.allBandsAnchored, value.periodic = ta.allBandsAnchored && tb.allBandsAnchored, statesA.periodic || statesB.periodic
	return value, nil
}

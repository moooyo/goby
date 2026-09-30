package introdetect

import (
	"context"
	"fmt"
	"math/bits"
	"sort"
)

// VisualSequenceVersion identifies an experimental, independently measured
// visual discovery policy. Its observations are not application skip markers.
const VisualSequenceVersion = "visual-sequence-v1"

// VisualSequenceMetrics retain visual evidence without inventing audio support.
type VisualSequenceMetrics struct {
	Samples               int
	CoveragePermille      int
	Transitions           int
	DistinctStates        int
	DominantStatePermille int
	MaxLumaRMSPermille    int
	MaxCenterRMSPermille  int
	MaxGapTicks           int64
	PairCount             int
}

type VisualSequenceGroup struct {
	Members []Support
	Metrics VisualSequenceMetrics
}

type VisualSequenceResult struct {
	Version       string
	Experimental  bool
	Comparisons   int64
	Groups        []VisualSequenceGroup
	SearchLimited bool
}

const (
	visualSequencePrefix   = 120 * TicksPerSecond
	visualSequenceMinimum  = 8 * TicksPerSecond
	visualSequenceMaximum  = 90 * TicksPerSecond
	visualSequenceGap      = 21 * TicksPerSecond / 10
	visualSequenceResidual = 3 * TicksPerSecond / 10
	visualSequenceOffsets  = 12
)

type sequencePair struct {
	left, right int
	a, b        Interval
	offset      int64
	metrics     VisualSequenceMetrics
}

func sequenceLimit(e Episode) int64 {
	return min(visualSequencePrefix, e.DurationTicks/2)
}

func sequenceUsable(s VisualSample) bool { return s.LumaKnown && s.Contrast >= 40 }

// Descriptor distances use integer squared RMS. The center gate prevents a
// common studio background from hiding different foreground content.
func sequenceDistance(a, b VisualSample) (int, int) {
	var total, center int
	for i, x := range a.Luma {
		d := int(x) - int(b.Luma[i])
		total += d * d
		row, column := i/8, i%8
		if row >= 2 && row < 6 && column >= 2 && column < 6 {
			center += d * d
		}
	}
	return total, center
}

func sequenceClose(a, b VisualSample) bool {
	if !sequenceUsable(a) || !sequenceUsable(b) || bits.OnesCount64(a.Hash^b.Hash) > 24 {
		return false
	}
	total, center := sequenceDistance(a, b)
	return total*1_000_000 <= 550*550*32*32*64 && center*1_000_000 <= 650*650*32*32*16
}

func sequenceMoving(samples []VisualSample, i int) bool {
	return i > 0 && sequenceUsable(samples[i-1]) && sequenceUsable(samples[i]) &&
		samples[i].Ticks-samples[i-1].Ticks <= visualSequenceGap && bits.OnesCount64(samples[i].Hash^samples[i-1].Hash) >= 8
}

func sequenceOffsets(a, b Episode, budget *workBudget) ([]int64, bool, error) {
	type vote struct {
		count int
		total int64
	}
	votes := map[int64]vote{}
	for i, x := range a.Visual {
		if x.Ticks >= sequenceLimit(a) {
			break
		}
		if !sequenceMoving(a.Visual, i) {
			continue
		}
		seen := map[int64]bool{}
		for j, y := range b.Visual {
			if y.Ticks >= sequenceLimit(b) {
				break
			}
			if err := budget.spend(); err != nil {
				return nil, false, err
			}
			if !sequenceMoving(b.Visual, j) || !sequenceClose(x, y) {
				continue
			}
			delta := y.Ticks - x.Ticks
			bin := delta / (TicksPerSecond / 10)
			if delta < 0 && delta%(TicksPerSecond/10) != 0 {
				bin--
			}
			if seen[bin] {
				continue
			}
			seen[bin] = true
			v := votes[bin]
			v.count++
			v.total += delta
			votes[bin] = v
		}
	}
	type nomination struct {
		offset int64
		count  int
	}
	ordered := []nomination{}
	for _, v := range votes {
		if v.count >= 3 {
			ordered = append(ordered, nomination{v.total / int64(v.count), v.count})
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].count != ordered[j].count {
			return ordered[i].count > ordered[j].count
		}
		return ordered[i].offset < ordered[j].offset
	})
	result := []int64{}
	for _, v := range ordered {
		near := false
		for _, old := range result {
			near = near || absolute(old-v.offset) <= visualSequenceResidual
		}
		if near {
			continue
		}
		if len(result) == visualSequenceOffsets {
			return result, true, nil
		}
		result = append(result, v.offset)
	}
	return result, false, nil
}

func sequenceTargets(a, b Episode, offset int64, ar, br Interval, budget *workBudget) ([][2]int, error) {
	matched := [][2]int{}
	j, last := 0, -1
	for i, x := range a.Visual {
		if x.Ticks < ar.StartTicks-visualSequenceResidual {
			continue
		}
		if x.Ticks > ar.EndTicks+visualSequenceResidual {
			break
		}
		if err := budget.spend(); err != nil {
			return nil, err
		}
		target := x.Ticks + offset
		for j+1 < len(b.Visual) && absolute(b.Visual[j+1].Ticks-target) <= absolute(b.Visual[j].Ticks-target) {
			j++
		}
		if j >= len(b.Visual) || j <= last || b.Visual[j].Ticks < br.StartTicks-visualSequenceResidual || b.Visual[j].Ticks > br.EndTicks+visualSequenceResidual || absolute(b.Visual[j].Ticks-target) > visualSequenceResidual {
			continue
		}
		if sequenceClose(x, b.Visual[j]) {
			matched = append(matched, [2]int{i, j})
			last = j
		}
	}
	return matched, nil
}

// sequenceMeasure always measures a fixed witness, including every unmatched
// slot. A narrow good interior cannot erase an unsupported gap in that witness.
func sequenceMeasure(a, b Episode, offset int64, ar, br Interval, budget *workBudget) (VisualSequenceMetrics, bool, error) {
	m := VisualSequenceMetrics{PairCount: 1}
	if min(ar.EndTicks-ar.StartTicks, br.EndTicks-br.StartTicks) < visualSequenceMinimum || max(ar.EndTicks-ar.StartTicks, br.EndTicks-br.StartTicks) > visualSequenceMaximum {
		return m, false, nil
	}
	matches, err := sequenceTargets(a, b, offset, ar, br, budget)
	if err != nil {
		return m, false, err
	}
	if len(matches) < 16 {
		return m, false, nil
	}
	counts := [2]int{}
	for k, e := range []Episode{a, b} {
		r := ar
		if k == 1 {
			r = br
		}
		for _, s := range e.Visual {
			if s.Ticks >= r.StartTicks-visualSequenceResidual && s.Ticks <= r.EndTicks+visualSequenceResidual {
				counts[k]++
			}
		}
	}
	m.Samples = len(matches)
	m.CoveragePermille = min(1000*len(matches)/max(1, counts[0]), 1000*len(matches)/max(1, counts[1]))
	if m.CoveragePermille < 850 {
		return m, false, nil
	}
	states := [][2]int{}
	stateCounts := []int{}
	for n, p := range matches {
		x, y := a.Visual[p[0]], b.Visual[p[1]]
		total, center := sequenceDistance(x, y)
		// Integer ceiling roots are used only for truthful summary metrics.
		m.MaxLumaRMSPermille = max(m.MaxLumaRMSPermille, sequenceRMS(total, 64))
		m.MaxCenterRMSPermille = max(m.MaxCenterRMSPermille, sequenceRMS(center, 16))
		if n > 0 {
			old := matches[n-1]
			ax, by := a.Visual[old[0]], b.Visual[old[1]]
			m.MaxGapTicks = max(m.MaxGapTicks, x.Ticks-ax.Ticks, y.Ticks-by.Ticks)
			if bits.OnesCount64(x.Hash^ax.Hash) >= 8 && bits.OnesCount64(y.Hash^by.Hash) >= 8 {
				m.Transitions++
			}
		}
		state := -1
		for k, seed := range states {
			if err := budget.spend(); err != nil {
				return m, false, err
			}
			if bits.OnesCount64(x.Hash^a.Visual[seed[0]].Hash) <= 8 && bits.OnesCount64(y.Hash^b.Visual[seed[1]].Hash) <= 8 {
				state = k
				break
			}
		}
		if state < 0 {
			states = append(states, p)
			stateCounts = append(stateCounts, 1)
		} else {
			stateCounts[state]++
		}
	}
	m.DistinctStates = len(states)
	for _, count := range stateCounts {
		m.DominantStatePermille = max(m.DominantStatePermille, count*1000/len(matches))
	}
	first, last := matches[0], matches[len(matches)-1]
	anchored := absolute(a.Visual[first[0]].Ticks-ar.StartTicks) <= visualSequenceResidual && absolute(b.Visual[first[1]].Ticks-br.StartTicks) <= visualSequenceResidual && absolute(ar.EndTicks-a.Visual[last[0]].Ticks) <= visualSequenceResidual && absolute(br.EndTicks-b.Visual[last[1]].Ticks) <= visualSequenceResidual
	periodic, err := sequencePeriodic(a, b, matches, budget)
	if err != nil {
		return m, false, err
	}
	return m, anchored && !periodic && m.MaxGapTicks <= visualSequenceGap && m.Transitions >= 3 && m.DistinctStates >= 4 && m.DominantStatePermille <= 650, nil
}

func sequenceRMS(sum, count int) int {
	lo, hi := 0, 8000
	for lo < hi {
		mid := (lo + hi) / 2
		if int64(mid)*int64(mid)*int64(count*32*32) >= int64(sum)*1_000_000 {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

func discoverSequencePair(a, b Episode, offset int64, budget *workBudget) ([]sequencePair, error) {
	ar, br := Interval{0, sequenceLimit(a) - 1}, Interval{0, sequenceLimit(b) - 1}
	matches, err := sequenceTargets(a, b, offset, ar, br, budget)
	if err != nil {
		return nil, err
	}
	result := []sequencePair{}
	for start := 0; start < len(matches); {
		end := start + 1
		for end < len(matches) {
			p, q := matches[end-1], matches[end]
			if a.Visual[q[0]].Ticks-a.Visual[p[0]].Ticks > visualSequenceGap || b.Visual[q[1]].Ticks-b.Visual[p[1]].Ticks > visualSequenceGap {
				break
			}
			end++
		}
		first, last := matches[start], matches[end-1]
		x := Interval{a.Visual[first[0]].Ticks, a.Visual[last[0]].Ticks}
		y := Interval{b.Visual[first[1]].Ticks, b.Visual[last[1]].Ticks}
		// Never extrapolate to the next frame or across a prefix boundary.
		if x.EndTicks < ar.EndTicks-TicksPerSecond && y.EndTicks < br.EndTicks-TicksPerSecond {
			metrics, ok, err := sequenceMeasure(a, b, offset, x, y, budget)
			if err != nil {
				return nil, err
			}
			if ok {
				result = append(result, sequencePair{a: x, b: y, offset: offset, metrics: metrics})
			}
		}
		start = end
	}
	return result, nil
}

// DiscoverVisualSequences is a bounded research surface. It requires complete
// pairwise support from at least three independent episodes and remeasures all
// edges on the same final source-clock intersection. It does not call Analyze,
// change its thresholds, or authorize application publication.
func DiscoverVisualSequences(ctx context.Context, cohort Cohort, options Options) (VisualSequenceResult, error) {
	if ctx == nil {
		return VisualSequenceResult{}, fmt.Errorf("%w: context required", ErrInvalidInput)
	}
	o, err := normalizeOptions(options)
	if err != nil {
		return VisualSequenceResult{}, err
	}
	episodes, err := validateInput(ctx, cohort, o)
	if err != nil {
		return VisualSequenceResult{}, err
	}
	independent := sequenceIndependent(episodes)
	budget := &workBudget{ctx: ctx, limit: o.MaxComparisons}
	result := VisualSequenceResult{Version: VisualSequenceVersion, Experimental: true, Groups: []VisualSequenceGroup{}}
	pairs := map[[2]int][]sequencePair{}
	for i, a := range episodes {
		if !independent[i] {
			continue
		}
		for j := i + 1; j < len(episodes); j++ {
			b := episodes[j]
			if !independent[j] || a.AlgorithmProfile != b.AlgorithmProfile {
				continue
			}
			offsets, limited, err := sequenceOffsets(a, b, budget)
			if err != nil {
				return VisualSequenceResult{}, err
			}
			result.SearchLimited = result.SearchLimited || limited
			for _, offset := range offsets {
				found, err := discoverSequencePair(a, b, offset, budget)
				if err != nil {
					return VisualSequenceResult{}, err
				}
				for _, pair := range found {
					pair.left, pair.right = i, j
					pairs[[2]int{i, j}] = append(pairs[[2]int{i, j}], pair)
				}
			}
		}
	}
	groups, err := sequenceGroups(episodes, independent, pairs, o, budget)
	if err != nil {
		return VisualSequenceResult{}, err
	}
	result.Groups = groups
	result.Comparisons = budget.used
	if err := ctx.Err(); err != nil {
		return VisualSequenceResult{}, err
	}
	return result, nil
}

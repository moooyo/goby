package introdetect

import (
	"fmt"
	"math/bits"
	"sort"
)

type visualAlignment struct {
	start           int
	targets         []int
	phase           int64
	maximumResidual int64
}

type visualPhaseEvent struct {
	phase          int64
	source, target int
}

// Every source contributes at most one pending nearest-neighbor change. The
// heap therefore stays linear in the number of actual source observations.
type visualPhaseHeap []visualPhaseEvent

func visualEventBefore(a, b visualPhaseEvent, budget *workBudget) (bool, error) {
	if err := budget.spend(); err != nil {
		return false, err
	}
	return a.phase < b.phase || a.phase == b.phase && (a.source < b.source || a.source == b.source && a.target < b.target), nil
}

func (h *visualPhaseHeap) push(value visualPhaseEvent, budget *workBudget) error {
	if err := budget.spend(); err != nil {
		return err
	}
	*h = append(*h, value)
	for child := len(*h) - 1; child > 0; {
		parent := (child - 1) / 2
		before, err := visualEventBefore((*h)[child], (*h)[parent], budget)
		if err != nil {
			return err
		}
		if !before {
			break
		}
		(*h)[child], (*h)[parent] = (*h)[parent], (*h)[child]
		child = parent
	}
	return nil
}

func (h *visualPhaseHeap) pop(budget *workBudget) (visualPhaseEvent, error) {
	if err := budget.spend(); err != nil {
		return visualPhaseEvent{}, err
	}
	value := (*h)[0]
	last := len(*h) - 1
	(*h)[0] = (*h)[last]
	*h = (*h)[:last]
	for parent := 0; parent*2+1 < len(*h); {
		child := parent*2 + 1
		if child+1 < len(*h) {
			before, err := visualEventBefore((*h)[child+1], (*h)[child], budget)
			if err != nil {
				return visualPhaseEvent{}, err
			}
			if before {
				child++
			}
		}
		before, err := visualEventBefore((*h)[child], (*h)[parent], budget)
		if err != nil {
			return visualPhaseEvent{}, err
		}
		if !before {
			break
		}
		(*h)[parent], (*h)[child] = (*h)[child], (*h)[parent]
		parent = child
	}
	return value, nil
}

func nextVisualPhase(a VisualSample, b []VisualSample, target int, offset int64) visualPhaseEvent {
	// Integer PTS ties choose the later target, as in the original matcher.
	midpoint := b[target-1].Ticks + (b[target].Ticks-b[target-1].Ticks+1)/2
	return visualPhaseEvent{phase: midpoint - a.Ticks - offset, target: target}
}

// alignVisual selects one phase for the entire acoustic window. It never
// adjusts the acoustic map or the absolute clocks used by visual time bands.
func alignVisual(a, b Episode, audio audioMatch, offset int64, o Options, budget *workBudget) (visualAlignment, error) {
	if budget == nil || budget.ctx == nil || o.VisualAlignmentTicks < 0 || o.MaxVisualSamples < 1 {
		return visualAlignment{}, fmt.Errorf("%w: visual alignment", ErrInvalidInput)
	}
	if err := budget.ctx.Err(); err != nil {
		return visualAlignment{}, err
	}
	if len(a.Visual) > o.MaxVisualSamples || len(b.Visual) > o.MaxVisualSamples {
		return visualAlignment{}, fmt.Errorf("%w: visual alignment samples", ErrLimit)
	}
	start := sort.Search(len(a.Visual), func(i int) bool { return a.Visual[i].Ticks >= audio.a.StartTicks })
	end := sort.Search(len(a.Visual), func(i int) bool { return a.Visual[i].Ticks > audio.a.EndTicks })
	if start > end {
		return visualAlignment{}, fmt.Errorf("%w: visual alignment window", ErrInvalidInput)
	}
	if start == end || len(b.Visual) == 0 {
		value := visualAlignment{start: start, targets: make([]int, end-start)}
		for i := range value.targets {
			if err := budget.spend(); err != nil {
				return visualAlignment{}, err
			}
			value.targets[i] = -1
		}
		if err := budget.ctx.Err(); err != nil {
			return visualAlignment{}, err
		}
		return value, nil
	}
	// A target's nearest-source bucket is contiguous. Its intersection with
	// the original acoustic corridor determines its first one-to-one source.
	first, last := make([]int, len(b.Visual)), make([]int, len(b.Visual))
	lower, upper := make([]int, len(b.Visual)), make([]int, len(b.Visual))
	lo, hi := start, start
	for j, sample := range b.Visual {
		if err := budget.spend(); err != nil {
			return visualAlignment{}, err
		}
		first[j], last[j] = -1, -1
		for lo < end && a.Visual[lo].Ticks < sample.Ticks-offset-o.VisualAlignmentTicks {
			if err := budget.spend(); err != nil {
				return visualAlignment{}, err
			}
			lo++
		}
		for hi < end && a.Visual[hi].Ticks <= sample.Ticks-offset+o.VisualAlignmentTicks {
			if err := budget.spend(); err != nil {
				return visualAlignment{}, err
			}
			hi++
		}
		lower[j], upper[j] = lo, hi
	}
	contribution := func(j int) (int, error) {
		if err := budget.spend(); err != nil {
			return 0, err
		}
		i := max(first[j], lower[j])
		if first[j] < 0 || i > last[j] || i >= upper[j] || b.Visual[j].Ticks < audio.b.StartTicks || b.Visual[j].Ticks > audio.b.EndTicks ||
			a.Visual[i].Contrast < o.MinVisualContrast || b.Visual[j].Contrast < o.MinVisualContrast {
			return 0, nil
		}
		return 64 - bits.OnesCount64(a.Visual[i].Hash^b.Visual[j].Hash), nil
	}
	phase := -o.VisualAlignmentTicks
	events := make(visualPhaseHeap, 0, end-start)
	j := 0
	for i := start; i < end; i++ {
		if err := budget.spend(); err != nil {
			return visualAlignment{}, err
		}
		target := a.Visual[i].Ticks + offset + phase
		for j+1 < len(b.Visual) && absolute(b.Visual[j+1].Ticks-target) <= absolute(b.Visual[j].Ticks-target) {
			if err := budget.spend(); err != nil {
				return visualAlignment{}, err
			}
			j++
		}
		if first[j] < 0 {
			first[j] = i
		}
		last[j] = i
		if j+1 < len(b.Visual) {
			event := nextVisualPhase(a.Visual[i], b.Visual, j+1, offset)
			event.source = i
			if event.phase <= o.VisualAlignmentTicks {
				if err := events.push(event, budget); err != nil {
					return visualAlignment{}, err
				}
			}
		}
	}
	// With a fixed full-window denominator, an informative pair saves
	// 2*(64-HD) from the two unmatched observations' cost. Dark or unmapped
	// observations save nothing; discarding them cannot improve the score.
	score := 0
	for j := range b.Visual {
		value, err := contribution(j)
		if err != nil {
			return visualAlignment{}, err
		}
		score += value
	}
	bestScore, bestPhase := -1, int64(0)
	consider := func(firstPhase, lastPhase int64) {
		candidate := max(firstPhase, min(int64(0), lastPhase))
		if score > bestScore || score == bestScore && (absolute(candidate) < absolute(bestPhase) || absolute(candidate) == absolute(bestPhase) && candidate < bestPhase) {
			bestScore, bestPhase = score, candidate
		}
	}
	for len(events) > 0 {
		at := events[0].phase
		consider(phase, at-1)
		// No intermediate state within a group of simultaneous events is a
		// real phase. Rank the new assignment only after all events apply.
		for len(events) > 0 && events[0].phase == at {
			event, err := events.pop(budget)
			if err != nil {
				return visualAlignment{}, err
			}
			old, next := event.target-1, event.target
			for _, target := range []int{old, next} {
				value, err := contribution(target)
				if err != nil {
					return visualAlignment{}, err
				}
				score -= value
			}
			if last[old] != event.source || first[next] >= 0 && first[next] != event.source+1 {
				return visualAlignment{}, fmt.Errorf("%w: visual phase ordering", ErrInvalidInput)
			}
			last[old]--
			if last[old] < first[old] {
				first[old], last[old] = -1, -1
			}
			first[next] = event.source
			if last[next] < 0 {
				last[next] = event.source
			}
			for _, target := range []int{old, next} {
				value, err := contribution(target)
				if err != nil {
					return visualAlignment{}, err
				}
				score += value
			}
			if next+1 < len(b.Visual) {
				following := nextVisualPhase(a.Visual[event.source], b.Visual, next+1, offset)
				following.source = event.source
				if following.phase <= o.VisualAlignmentTicks {
					if err := events.push(following, budget); err != nil {
						return visualAlignment{}, err
					}
				}
			}
		}
		phase = at
	}
	consider(phase, o.VisualAlignmentTicks)
	// Materialize only the complete winning map. Budget exhaustion anywhere
	// above returns an error, never an incompletely searched best-so-far map.
	value := visualAlignment{start: start, targets: make([]int, end-start), phase: bestPhase}
	j, lastTarget := 0, -1
	for i := start; i < end; i++ {
		if err := budget.spend(); err != nil {
			return visualAlignment{}, err
		}
		value.targets[i-start] = -1
		originalTarget := a.Visual[i].Ticks + offset
		target := originalTarget + bestPhase
		for j+1 < len(b.Visual) && absolute(b.Visual[j+1].Ticks-target) <= absolute(b.Visual[j].Ticks-target) {
			if err := budget.spend(); err != nil {
				return visualAlignment{}, err
			}
			j++
		}
		if j <= lastTarget || b.Visual[j].Ticks < audio.b.StartTicks || b.Visual[j].Ticks > audio.b.EndTicks ||
			absolute(b.Visual[j].Ticks-originalTarget) > o.VisualAlignmentTicks {
			continue
		}
		lastTarget, value.targets[i-start] = j, j
		value.maximumResidual = max(value.maximumResidual, absolute(b.Visual[j].Ticks-originalTarget))
	}
	if err := budget.ctx.Err(); err != nil {
		return visualAlignment{}, err
	}
	return value, nil
}

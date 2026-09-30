package introdetect

import (
	"math/bits"
	"sort"
)

const calibratedNominationStep = TicksPerSecond / 2
const calibratedPhaseStep = TicksPerSecond / 20

func calibratedAnchors(episode Episode) Episode {
	view := episode
	view.Visual = nil
	last := -1
	for tick := int64(0); tick < sequenceLimit(episode); tick += calibratedNominationStep {
		i := sort.Search(len(episode.Visual), func(i int) bool { return episode.Visual[i].Ticks >= tick })
		if i == len(episode.Visual) {
			break
		}
		if i > 0 && absolute(episode.Visual[i-1].Ticks-tick) < absolute(episode.Visual[i].Ticks-tick) {
			i--
		}
		if i > last {
			view.Visual = append(view.Visual, episode.Visual[i])
			last = i
		}
	}
	return view
}

// Nomination uses a fixed one-second change, independently of dense cadence.
func calibratedMoving(samples []VisualSample, index int) bool {
	if index < 2 {
		return false
	}
	a, b := samples[index-2], samples[index]
	return sequenceUsable(a) && sequenceUsable(b) && absolute(b.Ticks-a.Ticks-TicksPerSecond) <= visualSequenceResidual && bits.OnesCount64(a.Hash^b.Hash) >= 8
}

// Hash-only nominations are deliberately not capped. Every finite prefix bin
// with enough independent anchor votes is examined or the whole call fails its
// shared work budget. Weak body votes cannot conceal a competing interval.
func calibratedOffsets(a, b Episode, budget *workBudget) ([]int64, error) {
	type vote struct {
		count int
		total int64
	}
	votes := map[int64]vote{}
	for i, x := range a.Visual {
		if !calibratedMoving(a.Visual, i) {
			continue
		}
		seen := map[int64]bool{}
		for j, y := range b.Visual {
			if err := budget.spend(); err != nil {
				return nil, err
			}
			if !calibratedMoving(b.Visual, j) || bits.OnesCount64(x.Hash^y.Hash) > 24 {
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
			if err := budget.spend(); err != nil {
				return nil, err
			}
			near = near || absolute(old-v.offset) <= visualSequenceResidual
		}
		if !near {
			result = append(result, v.offset)
		}
	}
	return result, nil
}

func calibratedPhases(offsets []int64) []int64 {
	seen := map[int64]bool{}
	for _, offset := range offsets {
		for delta := -visualSequenceResidual; delta <= visualSequenceResidual; delta += calibratedPhaseStep {
			seen[offset+delta] = true
		}
	}
	result := make([]int64, 0, len(seen))
	for offset := range seen {
		result = append(result, offset)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

type calibratedEstimate struct {
	geometry calibratedGeometry
	view     Episode
	votes    int
	distance int64
	offset   int64
}

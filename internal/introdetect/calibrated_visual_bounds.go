package introdetect

import "math/bits"

const calibratedViewCacheBytes int64 = 128 << 20

type calibratedFrameBounds struct {
	minimum, maximum [64]int8
	hashAnd, hashOr  uint64
	usable           bool
}

type calibratedViewSet struct {
	views  []Episode
	bounds []calibratedFrameBounds
	bytes  int64
}

// The cache is bounded independently of cohort size. It stores rendered source
// views, which do not depend on the current anchor, and never stores pairwise
// fitted transforms in place of a group's one consistent source geometry.
type calibratedViewCache struct {
	values map[string]*calibratedViewSet
	order  []string
	bytes  int64
}

func newCalibratedViewCache() *calibratedViewCache {
	return &calibratedViewCache{values: map[string]*calibratedViewSet{}}
}

func (cache *calibratedViewCache) get(source Episode, budget *workBudget) (*calibratedViewSet, error) {
	if err := budget.spend(); err != nil {
		return nil, err
	}
	if value, ok := cache.values[source.SourceKey]; ok {
		for i, key := range cache.order {
			if key == source.SourceKey {
				cache.order = append(append(cache.order[:i:i], cache.order[i+1:]...), key)
				break
			}
		}
		return value, nil
	}
	grid := calibratedGeometryGrid()
	// VisualSample occupies 88 bytes. Each bound includes two descriptors,
	// two hash bit masks and alignment; reserve 160 bytes per source frame.
	reservation := int64(len(source.Refinement))*(int64(len(grid))*88+160) + int64(len(grid))*256
	for cache.bytes+reservation > calibratedViewCacheBytes && len(cache.order) > 0 {
		key := cache.order[0]
		cache.order = cache.order[1:]
		cache.bytes -= cache.values[key].bytes
		delete(cache.values, key)
	}
	set := &calibratedViewSet{views: make([]Episode, 0, len(grid)), bytes: reservation}
	for _, geometry := range grid {
		view, err := calibratedView(source, geometry, budget)
		if err != nil {
			return nil, err
		}
		if set.bounds == nil {
			set.bounds = make([]calibratedFrameBounds, len(view.Visual))
		}
		for i, sample := range view.Visual {
			// One additional unit covers the 64-cell bound update.
			if err := budget.spend(); err != nil {
				return nil, err
			}
			if !sequenceUsable(sample) {
				continue
			}
			bound := &set.bounds[i]
			if !bound.usable {
				bound.minimum = sample.Luma
				bound.maximum = sample.Luma
				bound.hashAnd = sample.Hash
				bound.hashOr = sample.Hash
				bound.usable = true
				continue
			}
			bound.hashAnd &= sample.Hash
			bound.hashOr |= sample.Hash
			for cell, value := range sample.Luma {
				bound.minimum[cell] = min(bound.minimum[cell], value)
				bound.maximum[cell] = max(bound.maximum[cell], value)
			}
		}
		set.views = append(set.views, view)
	}
	if reservation <= calibratedViewCacheBytes {
		cache.values[source.SourceKey] = set
		cache.order = append(cache.order, source.SourceKey)
		cache.bytes += reservation
	}
	return set, nil
}

// The distance to the box is a lower bound for every view in the finite grid.
// Forced hash differences count only bits shared by all usable target views.
func calibratedPotentialMatch(sample VisualSample, bound calibratedFrameBounds) (bool, int64) {
	if !sequenceUsable(sample) || !bound.usable {
		return false, 0
	}
	forced := (sample.Hash &^ bound.hashOr) | (^sample.Hash & bound.hashAnd)
	if bits.OnesCount64(forced) > 24 {
		return false, 0
	}
	var total, center int64
	for i, value := range sample.Luma {
		delta := 0
		if value < bound.minimum[i] {
			delta = int(bound.minimum[i]) - int(value)
		} else if value > bound.maximum[i] {
			delta = int(value) - int(bound.maximum[i])
		}
		squared := int64(delta * delta)
		total += squared
		if row, column := i/8, i%8; row >= 2 && row < 6 && column >= 2 && column < 6 {
			center += squared
		}
	}
	return total*1_000_000 <= 550*550*32*32*64 && center*1_000_000 <= 650*650*32*32*16, total
}

type calibratedPotentialPair struct{ anchor, target int }
type calibratedPotentialPhase struct {
	offset             int64
	pairs              []calibratedPotentialPair
	distanceLowerBound int64
}

func calibratedPhaseBounds(anchors Episode, set *calibratedViewSet, offsets []int64, budget *workBudget) ([]calibratedPotentialPhase, error) {
	result := make([]calibratedPotentialPhase, 0, len(offsets))
	samples := set.views[0].Visual
	for _, offset := range offsets {
		phase := calibratedPotentialPhase{offset: offset}
		j := 0
		for i, sample := range anchors.Visual {
			if i%2 != 0 || !calibratedMoving(anchors.Visual, i) {
				continue
			}
			if err := budget.spend(); err != nil {
				return nil, err
			}
			target := sample.Ticks + offset
			for j+1 < len(samples) && absolute(samples[j+1].Ticks-target) <= absolute(samples[j].Ticks-target) {
				j++
			}
			if j >= len(samples) || absolute(samples[j].Ticks-target) > visualSequenceResidual {
				continue
			}
			possible, distance := calibratedPotentialMatch(sample, set.bounds[j])
			if possible {
				phase.pairs = append(phase.pairs, calibratedPotentialPair{i, j})
				phase.distanceLowerBound += distance
			}
		}
		if len(phase.pairs) >= 3 {
			result = append(result, phase)
		}
	}
	return result, nil
}

func calibratedBoundedEstimate(anchor, source, neutral Episode, budget *workBudget, cache *calibratedViewCache) (calibratedEstimate, bool, error) {
	anchors := calibratedAnchors(anchor)
	offsets, err := calibratedOffsets(anchors, calibratedAnchors(neutral), budget)
	if err != nil {
		return calibratedEstimate{}, false, err
	}
	if len(offsets) == 0 {
		return calibratedEstimate{}, false, nil
	}
	set, err := cache.get(source, budget)
	if err != nil {
		return calibratedEstimate{}, false, err
	}
	phases, err := calibratedPhaseBounds(anchors, set, calibratedPhases(offsets), budget)
	if err != nil {
		return calibratedEstimate{}, false, err
	}
	grid := calibratedGeometryGrid()
	best := calibratedEstimate{distance: 1 << 62}
	for index, view := range set.views {
		for _, phase := range phases {
			if err := budget.spend(); err != nil {
				return calibratedEstimate{}, false, err
			}
			if len(phase.pairs) < best.votes || len(phase.pairs) == best.votes && phase.distanceLowerBound >= best.distance {
				continue
			}
			votes, distance := 0, int64(0)
			complete := true
			for i, pair := range phase.pairs {
				if votes+len(phase.pairs)-i < max(3, best.votes) {
					complete = false
					break
				}
				if err := budget.spend(); err != nil {
					return calibratedEstimate{}, false, err
				}
				a, b := anchors.Visual[pair.anchor], view.Visual[pair.target]
				if sequenceClose(a, b) {
					votes++
					sum, _ := sequenceDistance(a, b)
					distance += int64(sum)
				}
			}
			if complete && (votes > best.votes || votes == best.votes && distance < best.distance) {
				best = calibratedEstimate{grid[index], view, votes, distance, phase.offset}
			}
		}
	}
	if best.votes < 3 {
		return calibratedEstimate{}, false, nil
	}
	return best, true, nil
}

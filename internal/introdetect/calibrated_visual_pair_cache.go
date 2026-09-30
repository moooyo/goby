package introdetect

const calibratedPairCacheEntries = 1024

type calibratedPairSource struct {
	SourceKey string
	Geometry  calibratedGeometry
}

type calibratedPairKey struct {
	Left, Right    calibratedPairSource
	CandidateLimit int
}

// Ordered keys retain the exact nomination, nearest-slot and tie-breaking
// orientation. Only successful complete scans are reusable across anchors.
type calibratedPairCache struct {
	values map[calibratedPairKey][]sequencePair
	order  []calibratedPairKey
}

func newCalibratedPairCache() *calibratedPairCache {
	return &calibratedPairCache{values: map[calibratedPairKey][]sequencePair{}}
}

func (cache *calibratedPairCache) get(a, b Episode, geometryA, geometryB calibratedGeometry, o Options, budget *workBudget) ([]sequencePair, error) {
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	if err := budget.spend(); err != nil {
		return nil, err
	}
	key := calibratedPairKey{calibratedPairSource{a.SourceKey, geometryA}, calibratedPairSource{b.SourceKey, geometryB}, o.MaxCandidatesPerPair}
	value, exists := cache.values[key]
	if !exists {
		var err error
		value, err = calibratedPairHypotheses(a, b, o, budget)
		if err != nil {
			return nil, err
		}
		if len(cache.order) >= calibratedPairCacheEntries {
			old := cache.order[0]
			cache.order = cache.order[1:]
			delete(cache.values, old)
		}
		cache.values[key] = value
		cache.order = append(cache.order, key)
	} else {
		for i, prior := range cache.order {
			if prior == key {
				cache.order = append(append(cache.order[:i:i], cache.order[i+1:]...), key)
				break
			}
		}
	}
	result := make([]sequencePair, len(value))
	for i, pair := range value {
		if err := budget.spend(); err != nil {
			return nil, err
		}
		result[i] = pair
	}
	return result, nil
}

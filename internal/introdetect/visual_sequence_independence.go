package introdetect

import "fmt"

// Visual discovery ranks aliases by measured visual evidence, not audio health.
// Identity aliases still form transitive components across all three key kinds.
func sequenceIndependent(episodes []Episode) []bool {
	parent := make([]int, len(episodes))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	seen := map[string]int{}
	for i, e := range episodes {
		for kind, key := range []string{e.EpisodeKey, e.SourceKey, e.ContentIdentity} {
			key = fmt.Sprintf("%d:%s", kind, key)
			if prior, exists := seen[key]; exists {
				a, b := find(i), find(prior)
				parent[max(a, b)] = min(a, b)
			} else {
				seen[key] = i
			}
		}
	}
	type quality struct {
		ready   bool
		samples int
		span    int64
	}
	qualities := make([]quality, len(episodes))
	profiles := map[string]map[int]bool{}
	for i, e := range episodes {
		first, last := int64(-1), int64(-1)
		for _, sample := range e.Visual {
			if sample.Ticks >= sequenceLimit(e) {
				break
			}
			if !sequenceUsable(sample) {
				continue
			}
			qualities[i].samples++
			if first < 0 {
				first = sample.Ticks
			}
			last = sample.Ticks
		}
		q := &qualities[i]
		if first >= 0 {
			q.span = last - first
		}
		q.ready = q.samples >= 16 && q.span >= visualSequenceMinimum
		if q.ready {
			if profiles[e.AlgorithmProfile] == nil {
				profiles[e.AlgorithmProfile] = map[int]bool{}
			}
			profiles[e.AlgorithmProfile][find(i)] = true
		}
	}
	best := map[int]int{}
	for i, e := range episodes {
		component := find(i)
		prior, exists := best[component]
		if !exists {
			best[component] = i
			continue
		}
		a, b := qualities[i], qualities[prior]
		profileA, profileB := len(profiles[e.AlgorithmProfile]), len(profiles[episodes[prior].AlgorithmProfile])
		better := false
		switch {
		case a.ready != b.ready:
			better = a.ready
		case profileA != profileB:
			better = profileA > profileB
		case a.samples != b.samples:
			better = a.samples > b.samples
		case a.span != b.span:
			better = a.span > b.span
		}
		if better {
			best[component] = i
		}
	}
	result := make([]bool, len(episodes))
	for _, representative := range best {
		result[representative] = true
	}
	return result
}

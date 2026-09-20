package introdetect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

func edgeIntervals(edge pairMatch, left int) (Interval, Interval) {
	if edge.left == left {
		return edge.a, edge.b
	}
	return edge.b, edge.a
}

func edgesBetween(pairs map[[2]int][]pairMatch, a, b int) []pairMatch {
	return pairs[[2]int{min(a, b), max(a, b)}]
}

func joinGroup(selected []int, current map[int]Interval, node int, pairs map[[2]int][]pairMatch, o Options, budget *workBudget) (map[int]Interval, []pairMatch, error) {
	anchor := selected[0]
	for _, first := range edgesBetween(pairs, anchor, node) {
		if err := budget.spend(); err != nil {
			return nil, nil, err
		}
		anchorRange, nodeRange := edgeIntervals(first, anchor)
		if !compatibleInterval(current[anchor], anchorRange, o) {
			continue
		}
		proposed := make(map[int]Interval, len(current)+1)
		for k, interval := range current {
			proposed[k] = interval
		}
		proposed[anchor], proposed[node] = intersect(current[anchor], anchorRange), nodeRange
		used, valid := []pairMatch{first}, true
		for _, member := range selected[1:] {
			found := false
			for _, edge := range edgesBetween(pairs, member, node) {
				if err := budget.spend(); err != nil {
					return nil, nil, err
				}
				a, b := edgeIntervals(edge, member)
				if compatibleInterval(current[member], a, o) && compatibleInterval(proposed[node], b, o) {
					proposed[member], proposed[node] = intersect(current[member], a), intersect(proposed[node], b)
					used, found = append(used, edge), true
					break
				}
			}
			if !found {
				valid = false
				break
			}
		}
		if valid {
			for _, interval := range proposed {
				valid = valid && interval.EndTicks-interval.StartTicks >= o.MinDurationTicks
			}
			if valid {
				return proposed, used, nil
			}
		}
	}
	return nil, nil, nil
}

func collectGroups(cohortKey string, episodes []Episode, pairs map[[2]int][]pairMatch, o Options, budget *workBudget) ([]Group, error) {
	groups := []Group{}
	// Enumerate seeds in source order. Greedy clique growth is bounded and
	// conservative: every added episode must match every existing member.
	// Enumerating all seed edges avoids treating a merely connected component
	// as consensus, without an exponential maximum-clique search.
	for left := range episodes {
		for right := left + 1; right < len(episodes); right++ {
			for _, seed := range pairs[[2]int{left, right}] {
				if err := budget.ctx.Err(); err != nil {
					return nil, err
				}
				selected := []int{left, right}
				ranges := map[int]Interval{left: seed.a, right: seed.b}
				metrics, reasons := seed.metrics, append([]Reason{}, seed.reasons...)
				for node := range episodes {
					if _, present := ranges[node]; present {
						continue
					}
					proposed, used, err := joinGroup(selected, ranges, node, pairs, o, budget)
					if err != nil {
						return nil, err
					}
					if proposed == nil {
						continue
					}
					selected, ranges = append(selected, node), proposed
					for _, edge := range used {
						metrics = conservativeMetrics(metrics, edge.metrics)
						for _, reason := range edge.reasons {
							reasons = addReason(reasons, reason)
						}
					}
				}
				if len(selected) < o.MinSupport {
					continue
				}
				sort.Ints(selected)
				group := Group{AlgorithmProfile: episodes[left].AlgorithmProfile, Status: Qualified,
					Reasons: reasons, Metrics: metrics, Members: make([]Support, 0, len(selected))}
				valid := true
				for _, node := range selected {
					episode, interval := episodes[node], ranges[node]
					if interval.EndTicks-interval.StartTicks < o.MinDurationTicks {
						valid = false
					}
					if interval.EndTicks-interval.StartTicks < o.AutoMinDurationTicks {
						group.Reasons = addReason(group.Reasons, ShortInterval)
					}
					group.Members = append(group.Members, Support{episode.EpisodeKey, episode.SourceKey, episode.ContentIdentity, interval})
				}
				if !valid {
					continue
				}
				groups = mergeGroup(groups, group, o)
				if len(groups) > o.MaxGroups {
					return nil, fmt.Errorf("%w: candidate groups", ErrLimit)
				}
			}
		}
	}
	// Repeated intersections during merging can shorten a previously admitted
	// interval. Recheck both duration gates on the final boundaries.
	bounded := groups[:0]
	for _, group := range groups {
		valid := true
		for _, member := range group.Members {
			duration := member.Interval.EndTicks - member.Interval.StartTicks
			valid = valid && duration >= o.MinDurationTicks
			if duration < o.AutoMinDurationTicks {
				group.Reasons = addReason(group.Reasons, ShortInterval)
			}
		}
		if valid {
			bounded = append(bounded, group)
		}
	}
	groups = bounded
	// Prefer the larger complete witness group when a smaller group describes
	// the same source intervals. Never join two cliques through one bridge.
	keep := make([]bool, len(groups))
	for i := range groups {
		if err := budget.ctx.Err(); err != nil {
			return nil, err
		}
		keep[i] = true
		for j := range groups {
			if i != j && len(groups[i].Members) < len(groups[j].Members) && groupContained(groups[i], groups[j], o) {
				keep[i] = false
				break
			}
		}
	}
	filtered := make([]Group, 0, len(groups))
	for i, group := range groups {
		if keep[i] {
			filtered = append(filtered, group)
		}
	}
	for i := range filtered {
		if err := budget.ctx.Err(); err != nil {
			return nil, err
		}
		for j := i + 1; j < len(filtered); j++ {
			conflict := false
			for _, a := range filtered[i].Members {
				for _, b := range filtered[j].Members {
					if a.SourceKey == b.SourceKey && !compatibleInterval(a.Interval, b.Interval, o) {
						conflict = true
					}
				}
			}
			if conflict {
				filtered[i].Reasons = addReason(filtered[i].Reasons, CompetingIntervals)
				filtered[j].Reasons = addReason(filtered[j].Reasons, CompetingIntervals)
			}
		}
	}
	for i := range filtered {
		if len(filtered[i].Reasons) != 0 {
			filtered[i].Status = Review
		}
		filtered[i].ID = groupID(cohortKey, filtered[i], o)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })
	return filtered, nil
}

func groupContained(a, b Group, o Options) bool {
	for _, member := range a.Members {
		found := false
		for _, other := range b.Members {
			if member.SourceKey == other.SourceKey && compatibleInterval(member.Interval, other.Interval, o) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func mergeGroup(groups []Group, value Group, o Options) []Group {
	for i, prior := range groups {
		if len(prior.Members) != len(value.Members) || !groupContained(value, prior, o) {
			continue
		}
		for j, member := range value.Members {
			groups[i].Members[j].Interval = intersect(groups[i].Members[j].Interval, member.Interval)
		}
		groups[i].Metrics = conservativeMetrics(prior.Metrics, value.Metrics)
		groups[i].Metrics.PairCount = len(value.Members) * (len(value.Members) - 1) / 2
		for _, reason := range value.Reasons {
			groups[i].Reasons = addReason(groups[i].Reasons, reason)
		}
		return groups
	}
	return append(groups, value)
}

func groupID(cohortKey string, group Group, options Options) string {
	hash := sha256.New()
	encoded, _ := json.Marshal(options) // Options contains only finite integer fields.
	hash.Write(encoded)
	for _, value := range []string{Version, cohortKey, group.AlgorithmProfile} {
		fmt.Fprintf(hash, "%d:%s;", len(value), value)
	}
	for _, member := range group.Members {
		for _, value := range []string{member.EpisodeKey, member.SourceKey, member.ContentIdentity} {
			fmt.Fprintf(hash, "%d:%s;", len(value), value)
		}
		fmt.Fprintf(hash, "%d:%d;", member.Interval.StartTicks, member.Interval.EndTicks)
	}
	return "intro-group-v1-" + hex.EncodeToString(hash.Sum(nil))
}

func distinctCandidates(candidates []Candidate, o Options) []Candidate {
	result := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		merged := false
		for i, prior := range result {
			if compatibleInterval(prior.Interval, candidate.Interval, o) {
				// Keep one actual clique's witnesses, not the union of witnesses
				// that have never all matched one another.
				if prior.Status == Review && candidate.Status == Qualified || prior.Status == candidate.Status && len(candidate.Support) > len(prior.Support) {
					result[i] = candidate
				}
				merged = true
				break
			}
		}
		if !merged {
			result = append(result, candidate)
		}
	}
	return result
}

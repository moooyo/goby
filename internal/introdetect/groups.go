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

func joinGroup(selected []int, current map[int]Interval, currentEdges map[[2]int]pairMatch, node int, pairs map[[2]int][]pairMatch, o Options, budget *workBudget) (map[int]Interval, map[[2]int]pairMatch, error) {
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
		used := make(map[[2]int]pairMatch, len(currentEdges)+len(selected))
		for key, edge := range currentEdges {
			used[key] = edge
		}
		used[[2]int{min(anchor, node), max(anchor, node)}] = first
		valid := true
		for _, member := range selected[1:] {
			found := false
			for _, edge := range edgesBetween(pairs, member, node) {
				if err := budget.spend(); err != nil {
					return nil, nil, err
				}
				a, b := edgeIntervals(edge, member)
				if compatibleInterval(current[member], a, o) && compatibleInterval(proposed[node], b, o) {
					proposed[member], proposed[node] = intersect(current[member], a), intersect(proposed[node], b)
					used[[2]int{min(member, node), max(member, node)}], found = edge, true
					break
				}
			}
			if !found {
				valid = false
				break
			}
		}
		if valid {
			_, coherent, err := checkOffsetBudget(append(append([]int(nil), selected...), node), used, o, budget)
			if err != nil {
				return nil, nil, err
			}
			valid = coherent
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
	projections := make(map[string]*Group)
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
				edges := map[[2]int]pairMatch{{left, right}: seed}
				for node := range episodes {
					if _, present := ranges[node]; present {
						continue
					}
					proposed, used, err := joinGroup(selected, ranges, edges, node, pairs, o, budget)
					if err != nil {
						return nil, err
					}
					if proposed == nil {
						continue
					}
					selected, ranges, edges = append(selected, node), proposed, used
				}
				if len(selected) < o.MinSupport {
					continue
				}
				key, err := groupProjectionKey(selected, ranges, edges, budget)
				if err != nil {
					return nil, err
				}
				group, cached := projections[key]
				if !cached {
					if len(projections) == o.MaxGroups*o.MaxCandidatesPerPair {
						return nil, fmt.Errorf("%w: projected witness cache", ErrLimit)
					}
					group, err = projectGroup(episodes, selected, ranges, edges, o, budget)
					if err != nil {
						return nil, err
					}
					projections[key] = detachedGroup(group)
				} else {
					group = detachedGroup(group)
				}
				if group == nil {
					continue
				}
				groups = mergeGroup(groups, *group, o)
				if len(groups) > o.MaxGroups {
					return nil, fmt.Errorf("%w: candidate groups", ErrLimit)
				}
			}
		}
	}
	// Every group already has a revalidated fixed final intersection. Retain
	// the bounds here too; duplicate-hypothesis selection never joins witnesses.
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
			if i != j && len(groups[i].Members) < len(groups[j].Members) && groupContained(groups[i], groups[j], o) && compatibleGroupAlignment(groups[i], groups[j], o) &&
				!(groups[i].Status == Qualified && groups[j].Status == Review) {
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
	if err := markGroupConflicts(filtered, o, budget); err != nil {
		return nil, err
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
		if len(prior.Members) != len(value.Members) || !groupContained(value, prior, o) || !compatibleGroupAlignment(value, prior, o) {
			continue
		}
		if preferGroup(value, prior, o) {
			groups[i] = value
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
	for index, member := range group.Members {
		for _, value := range []string{member.EpisodeKey, member.SourceKey, member.ContentIdentity} {
			fmt.Fprintf(hash, "%d:%s;", len(value), value)
		}
		fmt.Fprintf(hash, "%d:%d;", member.Interval.StartTicks, member.Interval.EndTicks)
		if index < len(group.alignmentOffsets) {
			fmt.Fprintf(hash, "clock:%d;", group.alignmentOffsets[index])
		}
	}
	phaseKeys := make([][2]string, 0, len(group.phaseAnchors))
	for key := range group.phaseAnchors {
		phaseKeys = append(phaseKeys, key)
	}
	sort.Slice(phaseKeys, func(i, j int) bool {
		if phaseKeys[i][0] != phaseKeys[j][0] {
			return phaseKeys[i][0] < phaseKeys[j][0]
		}
		return phaseKeys[i][1] < phaseKeys[j][1]
	})
	for _, key := range phaseKeys {
		fmt.Fprintf(hash, "phase:%d:%s;%d:%s;%d;%s;", len(key[0]), key[0], len(key[1]), key[1], group.phaseAnchors[key], group.phaseClasses[key])
	}
	return "intro-group-v3-" + hex.EncodeToString(hash.Sum(nil))
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

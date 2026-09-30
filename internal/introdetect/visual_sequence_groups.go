package introdetect

import "sort"

func sequenceEdges(pairs map[[2]int][]sequencePair, left, right int) []sequencePair {
	values := pairs[[2]int{min(left, right), max(left, right)}]
	if left < right {
		return values
	}
	result := make([]sequencePair, len(values))
	for i, p := range values {
		p.left, p.right = p.right, p.left
		p.a, p.b = p.b, p.a
		p.offset = -p.offset
		result[i] = p
	}
	return result
}

func sequenceWorst(a, b VisualSequenceMetrics) VisualSequenceMetrics {
	a.Samples = min(a.Samples, b.Samples)
	a.CoveragePermille = min(a.CoveragePermille, b.CoveragePermille)
	a.Transitions = min(a.Transitions, b.Transitions)
	a.DistinctStates = min(a.DistinctStates, b.DistinctStates)
	a.DominantStatePermille = max(a.DominantStatePermille, b.DominantStatePermille)
	a.MaxLumaRMSPermille = max(a.MaxLumaRMSPermille, b.MaxLumaRMSPermille)
	a.MaxCenterRMSPermille = max(a.MaxCenterRMSPermille, b.MaxCenterRMSPermille)
	a.MaxGapTicks = max(a.MaxGapTicks, b.MaxGapTicks)
	a.PairCount += b.PairCount
	return a
}

// Every proposed extension is measured before it can replace a valid witness.
func sequenceGroupWitness(episodes []Episode, selected []int, clocks map[int]int64, window Interval, budget *workBudget) (VisualSequenceGroup, bool, error) {
	ordered := append([]int(nil), selected...)
	sort.Ints(ordered)
	group := VisualSequenceGroup{Members: []Support{}}
	for index, a := range ordered {
		ar := Interval{window.StartTicks + clocks[a], window.EndTicks + clocks[a]}
		e := episodes[a]
		group.Members = append(group.Members, Support{EpisodeKey: e.EpisodeKey, SourceKey: e.SourceKey, ContentIdentity: e.ContentIdentity, Interval: ar})
		for _, b := range ordered[index+1:] {
			br := Interval{window.StartTicks + clocks[b], window.EndTicks + clocks[b]}
			m, ok, err := sequenceMeasure(e, episodes[b], clocks[b]-clocks[a], ar, br, budget)
			if err != nil || !ok {
				return VisualSequenceGroup{}, false, err
			}
			if group.Metrics.PairCount == 0 {
				group.Metrics = m
			} else {
				group.Metrics = sequenceWorst(group.Metrics, m)
			}
		}
	}
	return group, true, nil
}

// A shorter interval with more members cannot supersede a longer witness.
func sequenceContainsWitness(outer, inner VisualSequenceGroup) bool {
	if len(outer.Members) < len(inner.Members) {
		return false
	}
	position := 0
	for _, member := range inner.Members {
		for position < len(outer.Members) && outer.Members[position].EpisodeKey < member.EpisodeKey {
			position++
		}
		if position == len(outer.Members) {
			return false
		}
		candidate := outer.Members[position]
		if candidate.EpisodeKey != member.EpisodeKey || candidate.SourceKey != member.SourceKey || candidate.Interval.StartTicks > member.Interval.StartTicks || candidate.Interval.EndTicks < member.Interval.EndTicks {
			return false
		}
	}
	return true
}

func sequenceRetainWitness(groups []VisualSequenceGroup, group VisualSequenceGroup, o Options, budget *workBudget) ([]VisualSequenceGroup, error) {
	retained := make([]VisualSequenceGroup, 0, len(groups)+1)
	for _, prior := range groups {
		if err := budget.spend(); err != nil {
			return nil, err
		}
		if sequenceSameWitness(prior, group) {
			if !sequencePreferWitness(group, prior) {
				return groups, nil
			}
			continue
		}
		if sequenceContainsWitness(prior, group) {
			return groups, nil
		}
		if !sequenceContainsWitness(group, prior) {
			retained = append(retained, prior)
		}
	}
	if len(retained) >= o.MaxGroups {
		return nil, ErrLimit
	}
	return append(retained, group), nil
}

// Sampling phase can move projected endpoints slightly between seed pairs.
// Keep one measured witness, never a union of those independently tested clocks.
func sequenceSameWitness(a, b VisualSequenceGroup) bool {
	if len(a.Members) != len(b.Members) {
		return false
	}
	for i, x := range a.Members {
		y := b.Members[i]
		if x.SourceKey != y.SourceKey || absolute(x.Interval.StartTicks-y.Interval.StartTicks) > visualSequenceResidual || absolute(x.Interval.EndTicks-y.Interval.EndTicks) > visualSequenceResidual {
			return false
		}
	}
	return true
}

func sequencePreferWitness(a, b VisualSequenceGroup) bool {
	if a.Metrics.CoveragePermille != b.Metrics.CoveragePermille {
		return a.Metrics.CoveragePermille > b.Metrics.CoveragePermille
	}
	if a.Metrics.MaxLumaRMSPermille != b.Metrics.MaxLumaRMSPermille {
		return a.Metrics.MaxLumaRMSPermille < b.Metrics.MaxLumaRMSPermille
	}
	for i, x := range a.Members {
		y := b.Members[i]
		if x.Interval.StartTicks != y.Interval.StartTicks {
			return x.Interval.StartTicks < y.Interval.StartTicks
		}
		if x.Interval.EndTicks != y.Interval.EndTicks {
			return x.Interval.EndTicks < y.Interval.EndTicks
		}
	}
	return false
}

func sequenceGroups(episodes []Episode, independent []bool, pairs map[[2]int][]sequencePair, o Options, budget *workBudget) ([]VisualSequenceGroup, error) {
	groups := []VisualSequenceGroup{}
	for left := range episodes {
		for right := left + 1; right < len(episodes); right++ {
			for _, seed := range pairs[[2]int{left, right}] {
				if err := budget.spend(); err != nil {
					return nil, err
				}
				selected := []int{left, right}
				clocks := map[int]int64{left: 0, right: seed.offset}
				window := intersect(seed.a, Interval{seed.b.StartTicks - seed.offset, seed.b.EndTicks - seed.offset})
				for len(selected) < len(episodes) {
					var chosen []int
					var chosenClocks map[int]int64
					var chosenWindow Interval
					var chosenGroup VisualSequenceGroup
					for node := range episodes {
						if !independent[node] {
							continue
						}
						if _, exists := clocks[node]; exists {
							continue
						}
						for _, first := range sequenceEdges(pairs, left, node) {
							if err := budget.spend(); err != nil {
								return nil, err
							}
							clock := first.offset
							proposed := intersect(window, intersect(first.a, Interval{first.b.StartTicks - clock, first.b.EndTicks - clock}))
							valid := proposed.EndTicks-proposed.StartTicks >= visualSequenceMinimum
							for _, member := range selected[1:] {
								if !valid {
									break
								}
								found := false
								for _, edge := range sequenceEdges(pairs, member, node) {
									if err := budget.spend(); err != nil {
										return nil, err
									}
									if absolute(edge.offset-(clock-clocks[member])) > visualSequenceResidual {
										continue
									}
									common := intersect(proposed, intersect(Interval{edge.a.StartTicks - clocks[member], edge.a.EndTicks - clocks[member]}, Interval{edge.b.StartTicks - clock, edge.b.EndTicks - clock}))
									if common.EndTicks-common.StartTicks >= visualSequenceMinimum {
										proposed = common
										found = true
										break
									}
								}
								valid = found
							}
							if !valid {
								continue
							}
							next := append(append([]int(nil), selected...), node)
							nextClocks := make(map[int]int64, len(clocks)+1)
							for member, value := range clocks {
								nextClocks[member] = value
							}
							nextClocks[node] = clock
							group, ok, err := sequenceGroupWitness(episodes, next, nextClocks, proposed, budget)
							if err != nil {
								return nil, err
							}
							if !ok {
								continue
							}
							if chosen == nil || proposed.EndTicks-proposed.StartTicks > chosenWindow.EndTicks-chosenWindow.StartTicks {
								chosen, chosenClocks, chosenWindow, chosenGroup = next, nextClocks, proposed, group
							}
						}
					}
					if chosen == nil {
						break
					}
					if len(chosen) >= o.MinSupport {
						var err error
						groups, err = sequenceRetainWitness(groups, chosenGroup, o, budget)
						if err != nil {
							return nil, err
						}
					}
					selected, clocks, window = chosen, chosenClocks, chosenWindow
				}
			}
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i].Members[0], groups[j].Members[0]
		if a.EpisodeKey != b.EpisodeKey {
			return a.EpisodeKey < b.EpisodeKey
		}
		if a.Interval.StartTicks != b.Interval.StartTicks {
			return a.Interval.StartTicks < b.Interval.StartTicks
		}
		return a.Interval.EndTicks < b.Interval.EndTicks
	})
	return groups, nil
}

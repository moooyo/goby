package introdetect

// Temporal evidence may use samples up to one residual outside a fixed source
// clock interval. Remove that uncertainty band once from the final common
// window before treating its interior as a skip candidate. Every edge is then
// measured again using the same source views and clock offsets.
func calibratedGuardedWitness(group VisualSequenceGroup, episodes []Episode, budget *workBudget) (VisualSequenceGroup, bool, error) {
	if group.boundaryGuarded {
		return group, true, nil
	}
	if len(group.Members) < 3 {
		return VisualSequenceGroup{}, false, nil
	}
	anchor := group.Members[0].Interval
	window := Interval{anchor.StartTicks + visualSequenceResidual, anchor.EndTicks - visualSequenceResidual}
	if window.EndTicks-window.StartTicks < visualSequenceMinimum {
		return VisualSequenceGroup{}, false, nil
	}
	selected := make([]int, 0, len(group.Members))
	clocks := make(map[int]int64, len(group.Members))
	for _, member := range group.Members {
		if err := budget.spend(); err != nil {
			return VisualSequenceGroup{}, false, err
		}
		clock := member.Interval.StartTicks - anchor.StartTicks
		if member.Interval.EndTicks-clock != anchor.EndTicks {
			return VisualSequenceGroup{}, false, nil
		}
		found := false
		for index, episode := range episodes {
			if episode.SourceKey == member.SourceKey {
				selected = append(selected, index)
				clocks[index] = clock
				found = true
				break
			}
		}
		if !found {
			return VisualSequenceGroup{}, false, nil
		}
	}
	guarded, ok, err := sequenceGroupWitness(episodes, selected, clocks, window, budget)
	if err != nil || !ok {
		return VisualSequenceGroup{}, false, err
	}
	guarded.boundaryGuarded = true
	return guarded, true, nil
}

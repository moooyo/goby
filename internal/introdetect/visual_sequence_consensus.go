package introdetect

// Consensus only repairs sources that already have competing visual windows.
// Every original candidate constrains its source's exact intersection before
// any witness is projected or remeasured, so failed witnesses cannot erase
// counterevidence. Existing unambiguous publications keep their original IDs.
func recoverVisualConsensus(groups []VisualSequenceGroup, episodes []Episode, selection map[string]string, budget *workBudget) ([]VisualSequenceGroup, error) {
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	ambiguous := false
	for _, groupID := range selection {
		ambiguous = ambiguous || groupID == ""
	}
	if !ambiguous {
		return nil, nil
	}
	common := map[string]Interval{}
	for _, group := range groups {
		for _, member := range group.Members {
			if err := budget.spend(); err != nil {
				return nil, err
			}
			if prior, exists := common[member.SourceKey]; exists {
				common[member.SourceKey] = intersect(prior, member.Interval)
			} else {
				common[member.SourceKey] = member.Interval
			}
		}
	}
	result := []VisualSequenceGroup{}
	for _, group := range groups {
		needed := false
		for _, member := range group.Members {
			if err := budget.spend(); err != nil {
				return nil, err
			}
			if groupID, exists := selection[member.SourceKey]; exists && groupID == "" {
				window := common[member.SourceKey]
				needed = needed || window.EndTicks-window.StartTicks >= visualSequenceMinimum
			}
		}
		if !needed {
			continue
		}
		measured, ok, err := remeasureVisualConsensus(group, episodes, common, budget)
		if err != nil {
			return nil, err
		}
		if ok {
			result = append(result, measured)
		}
	}
	if err := budget.ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// The selected group's support, geometry and clocks remain fixed. Project all
// member intersections into that one clock before measuring every edge again.
// These intervals only shrink an already guarded witness; no second boundary
// guard is applied and no evidence from different calibrations is combined.
func remeasureVisualConsensus(group VisualSequenceGroup, episodes []Episode, common map[string]Interval, budget *workBudget) (VisualSequenceGroup, bool, error) {
	if len(group.Members) < 3 || !group.boundaryGuarded || group.Calibration == nil ||
		group.Metrics.MeasurementPolicy != VisualMeasurementCalibrated ||
		group.Metrics.CalibrationDigest != visualCalibrationDigest(group) ||
		group.Calibration.BoundaryGuardTicks != visualSequenceResidual ||
		len(group.Calibration.Members) != len(group.Members) ||
		group.Calibration.AnchorSourceKey != group.Members[0].SourceKey {
		return VisualSequenceGroup{}, false, nil
	}
	anchor := group.Members[0].Interval
	window := anchor
	selected := make([]int, 0, len(group.Members))
	clocks := make(map[int]int64, len(group.Members))
	geometries := make(map[int]calibratedGeometry, len(group.Members))
	seenEpisodes := map[string]bool{}
	seenContents := map[string]bool{}
	profile := ""
	for position, member := range group.Members {
		if err := budget.spend(); err != nil {
			return VisualSequenceGroup{}, false, err
		}
		audit := group.Calibration.Members[position]
		clock := audit.ClockOffsetTicks
		if audit.SourceKey != member.SourceKey || member.Interval.StartTicks-clock != anchor.StartTicks ||
			member.Interval.EndTicks-clock != anchor.EndTicks {
			return VisualSequenceGroup{}, false, nil
		}
		candidate, exists := common[member.SourceKey]
		if !exists || candidate.EndTicks-candidate.StartTicks < visualSequenceMinimum {
			return VisualSequenceGroup{}, false, nil
		}
		window = intersect(window, Interval{candidate.StartTicks - clock, candidate.EndTicks - clock})
		found := false
		for index, episode := range episodes {
			if err := budget.spend(); err != nil {
				return VisualSequenceGroup{}, false, err
			}
			if episode.SourceKey != member.SourceKey {
				continue
			}
			if episode.EpisodeKey != member.EpisodeKey || episode.ContentIdentity != member.ContentIdentity || len(episode.Refinement) < 16 {
				return VisualSequenceGroup{}, false, nil
			}
			if position == 0 {
				profile = episode.AlgorithmProfile
			}
			if episode.AlgorithmProfile != profile {
				return VisualSequenceGroup{}, false, nil
			}
			if seenEpisodes[episode.EpisodeKey] || seenContents[episode.ContentIdentity] {
				return VisualSequenceGroup{}, false, nil
			}
			seenEpisodes[episode.EpisodeKey], seenContents[episode.ContentIdentity] = true, true
			if _, duplicate := clocks[index]; duplicate {
				return VisualSequenceGroup{}, false, nil
			}
			selected = append(selected, index)
			clocks[index] = clock
			geometries[index] = audit.Geometry
			found = true
			break
		}
		if !found {
			return VisualSequenceGroup{}, false, nil
		}
	}
	if window.EndTicks-window.StartTicks < visualSequenceMinimum {
		return VisualSequenceGroup{}, false, nil
	}
	views := append([]Episode(nil), episodes...)
	for _, index := range selected {
		view, err := calibratedView(episodes[index], geometries[index], budget)
		if err != nil {
			return VisualSequenceGroup{}, false, err
		}
		views[index] = view
	}
	measured, ok, err := sequenceGroupWitness(views, selected, clocks, window, budget)
	if err != nil || !ok {
		return VisualSequenceGroup{}, false, err
	}
	measured.boundaryGuarded = true
	measured.Calibration = group.Calibration
	measured.Metrics.MeasurementPolicy = VisualMeasurementCalibrated
	measured.Metrics.CalibrationDigest = visualCalibrationDigest(measured)
	return measured, true, nil
}

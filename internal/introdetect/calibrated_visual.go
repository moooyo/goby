package introdetect

import "context"

func calibratedPairHypotheses(a, b Episode, o Options, budget *workBudget) ([]sequencePair, error) {
	offsets, err := calibratedOffsets(calibratedAnchors(a), calibratedAnchors(b), budget)
	if err != nil {
		return nil, err
	}
	result := []sequencePair{}
	for _, offset := range calibratedPhases(offsets) {
		found, err := discoverSequencePair(a, b, offset, budget)
		if err != nil {
			return nil, err
		}
		for _, candidate := range found {
			duplicate := -1
			for index, prior := range result {
				if err := budget.spend(); err != nil {
					return nil, err
				}
				if absolute(prior.offset-candidate.offset) <= visualSequenceResidual && absolute(prior.a.StartTicks-candidate.a.StartTicks) <= visualSequenceResidual && absolute(prior.a.EndTicks-candidate.a.EndTicks) <= visualSequenceResidual && absolute(prior.b.StartTicks-candidate.b.StartTicks) <= visualSequenceResidual && absolute(prior.b.EndTicks-candidate.b.EndTicks) <= visualSequenceResidual {
					duplicate = index
					break
				}
			}
			if duplicate < 0 {
				if len(result) >= o.MaxCandidatesPerPair {
					return nil, ErrLimit
				}
				result = append(result, candidate)
			} else {
				prior := result[duplicate]
				if candidate.metrics.CoveragePermille > prior.metrics.CoveragePermille || candidate.metrics.CoveragePermille == prior.metrics.CoveragePermille && (candidate.metrics.MaxLumaRMSPermille < prior.metrics.MaxLumaRMSPermille || candidate.metrics.MaxLumaRMSPermille == prior.metrics.MaxLumaRMSPermille && candidate.offset < prior.offset) {
					result[duplicate] = candidate
				}
			}
		}
	}
	return result, nil
}

func discoverCalibratedSequences(ctx context.Context, episodes []Episode, o Options, diagnostics *diagnosticsCollector) (VisualSequenceResult, error) {
	diagnostics.visualBranchStarted(VisualMeasurementCalibrated)
	budget := &workBudget{ctx: ctx, limit: o.MaxComparisons}
	viewCache := newCalibratedViewCache()
	pairCache := newCalibratedPairCache()
	result := VisualSequenceResult{Version: VisualSequenceVersion, Experimental: true, Groups: []VisualSequenceGroup{}}
	neutral := make([]Episode, len(episodes))
	identity := calibratedGeometry{ScaleYPermille: 1000}
	for i, episode := range episodes {
		view, err := calibratedView(episode, identity, budget)
		if err != nil {
			return VisualSequenceResult{}, err
		}
		neutral[i] = view
	}
	representativeViews := append([]Episode(nil), episodes...)
	for i, episode := range episodes {
		if len(episode.Refinement) > 0 {
			representativeViews[i].Visual = neutral[i].Visual
		}
	}
	independent := sequenceIndependent(representativeViews)
	result.representatives = independent
	ready := make([]bool, len(episodes))
	profiles := map[string]int{}
	for i, view := range neutral {
		count, first, last := 0, int64(-1), int64(-1)
		for _, sample := range view.Visual {
			if sequenceUsable(sample) {
				count++
				if first < 0 {
					first = sample.Ticks
				}
				last = sample.Ticks
			}
		}
		ready[i] = independent[i] && count >= 16 && last-first >= visualSequenceMinimum
		if ready[i] {
			profiles[view.AlgorithmProfile]++
		}
	}
	for _, count := range profiles {
		result.hasRefinementQuorum = result.hasRefinementQuorum || count >= o.MinSupport
	}
	diagnostics.visualQuorum(VisualMeasurementCalibrated, independent, ready, result.hasRefinementQuorum)
	if !result.hasRefinementQuorum {
		result.Comparisons = budget.used
		diagnostics.visualBranchCompleted(VisualMeasurementCalibrated, result)
		return result, nil
	}
	for anchor, reference := range neutral {
		if !ready[anchor] {
			continue
		}
		eligible := 1
		for node := anchor + 1; node < len(episodes); node++ {
			if ready[node] && episodes[node].AlgorithmProfile == reference.AlgorithmProfile {
				eligible++
			}
		}
		if eligible < o.MinSupport {
			continue
		}
		views := append([]Episode(nil), neutral...)
		geometries := map[int]calibratedGeometry{anchor: identity}
		members := []int{anchor}
		pairs := map[[2]int][]sequencePair{}
		for node := anchor + 1; node < len(episodes); node++ {
			if !ready[node] || episodes[node].AlgorithmProfile != reference.AlgorithmProfile {
				continue
			}
			estimate, ok, err := calibratedBoundedEstimate(reference, episodes[node], neutral[node], budget, viewCache)
			if err != nil {
				return VisualSequenceResult{}, err
			}
			diagnostics.visualCalibration(reference.SourceKey, episodes[node].SourceKey, ok)
			if !ok {
				continue
			}
			found, err := pairCache.getObserved(reference, estimate.view, identity, estimate.geometry, o, budget, reference.SourceKey, diagnostics)
			if err != nil {
				return VisualSequenceResult{}, err
			}
			if len(found) == 0 {
				continue
			}
			views[node] = estimate.view
			geometries[node] = estimate.geometry
			members = append(members, node)
			for index := range found {
				found[index].left, found[index].right = anchor, node
			}
			pairs[[2]int{anchor, node}] = found
		}
		if len(members) < o.MinSupport {
			diagnostics.visualAnchorWithoutQuorum(reference.SourceKey, len(members))
			continue
		}
		for i, left := range members[1:] {
			for _, right := range members[i+2:] {
				found, err := pairCache.getObserved(views[left], views[right], geometries[left], geometries[right], o, budget, reference.SourceKey, diagnostics)
				if err != nil {
					return VisualSequenceResult{}, err
				}
				for index := range found {
					found[index].left, found[index].right = left, right
				}
				if len(found) > 0 {
					pairs[[2]int{left, right}] = found
				}
			}
		}
		groups, err := sequenceGroupsForAnchor(views, independent, pairs, o, budget, anchor)
		if err != nil {
			return VisualSequenceResult{}, err
		}
		diagnostics.visualGroupSearch(VisualMeasurementCalibrated, reference.SourceKey, len(groups))
		for _, group := range groups {
			guarded, ok, err := calibratedGuardedWitness(group, views, budget)
			if err != nil {
				return VisualSequenceResult{}, err
			}
			diagnostics.visualGuard(reference.SourceKey, ok)
			if !ok {
				continue
			}
			group = guarded
			group.Calibration = calibratedAuditForGroup(group, anchor, views, geometries)
			group.Metrics.MeasurementPolicy = VisualMeasurementCalibrated
			group.Metrics.CalibrationDigest = visualCalibrationDigest(group)
			result.Groups, err = sequenceRetainWitness(result.Groups, group, o, budget)
			if err != nil {
				return VisualSequenceResult{}, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return VisualSequenceResult{}, err
	}
	result.Comparisons = budget.used
	diagnostics.visualBranchCompleted(VisualMeasurementCalibrated, result)
	return result, nil
}

package introdetect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// visualFallbackGroups uses its own complete visual witness only when the
// acoustic pipeline found no group. Ambiguous visual windows are not published;
// the diagnostic discovery result retains them for inspection.
func visualFallbackGroups(cohort Cohort, episodes []Episode, o Options, limited map[string]bool, budget *workBudget) ([]Group, map[string]string, error) {
	if o != DefaultOptions() {
		return nil, nil, nil
	}
	ready := 0
	for _, e := range episodes {
		for _, s := range e.Visual {
			if sequenceUsable(s) {
				ready++
				break
			}
		}
	}
	if ready < o.MinSupport {
		return nil, nil, nil
	}
	if budget.used >= budget.limit {
		return nil, nil, ErrLimit
	}
	remaining := o
	remaining.MaxComparisons = budget.limit - budget.used
	visual, err := DiscoverVisualSequences(budget.ctx, cohort, remaining)
	if err != nil {
		return nil, nil, err
	}
	budget.used += visual.Comparisons
	if visual.SearchLimited {
		return nil, nil, nil
	}
	groups := []Group{}
	for _, value := range visual.Groups {
		allowed := true
		for _, member := range value.Members {
			allowed = allowed && !limited[member.SourceKey]
		}
		if !allowed {
			continue
		}
		metrics := value.Metrics
		group := Group{Status: Qualified, Reasons: []Reason{}, Members: append([]Support(nil), value.Members...), VisualEvidence: &metrics}
		for _, e := range episodes {
			if e.SourceKey == group.Members[0].SourceKey {
				group.AlgorithmProfile = e.AlgorithmProfile
				break
			}
		}
		identity, _ := json.Marshal(struct {
			Version, Policy, Cohort, Profile string
			Members                          []Support
			Evidence                         VisualSequenceMetrics
		}{Version, VisualSequenceVersion, cohort.Key, group.AlgorithmProfile, group.Members, metrics})
		digest := sha256.Sum256(identity)
		group.ID = "intro-visual-v4-" + hex.EncodeToString(digest[:])
		for _, member := range group.Members {
			candidate := Candidate{Interval: member.Interval, Status: Qualified, Reasons: []Reason{}, Support: group.Members, VisualEvidence: &metrics}
			if !ValidateCandidateEvidence(candidate, o) {
				allowed = false
				break
			}
		}
		if allowed {
			groups = append(groups, group)
		}
	}
	selection, err := selectVisualWitnesses(groups, budget)
	if err != nil {
		return nil, nil, err
	}
	selectedGroups := make(map[string]bool, len(selection))
	for _, groupID := range selection {
		if groupID != "" {
			selectedGroups[groupID] = true
		}
	}
	retained := make([]Group, 0, len(selectedGroups))
	for _, group := range groups {
		if selectedGroups[group.ID] {
			retained = append(retained, group)
		}
	}
	return retained, selection, nil
}

// Each source selects an existing complete witness. Different support cliques
// for one nested interval do not imply competing openings. Crossing or disjoint
// windows remain ambiguous, and no interval or support membership is combined.
func selectVisualWitnesses(groups []Group, budget *workBudget) (map[string]string, error) {
	type reference struct {
		group    int
		interval Interval
	}
	bySource := map[string][]reference{}
	for index, group := range groups {
		for _, member := range group.Members {
			if err := budget.spend(); err != nil {
				return nil, err
			}
			bySource[member.SourceKey] = append(bySource[member.SourceKey], reference{index, member.Interval})
		}
	}
	selection := map[string]string{}
	for source, references := range bySource {
		compatible := true
		for i, a := range references {
			for _, b := range references[i+1:] {
				if err := budget.spend(); err != nil {
					return nil, err
				}
				if !nestedVisualIntervals(a.interval, b.interval) {
					compatible = false
					break
				}
			}
			if !compatible {
				break
			}
		}
		if !compatible {
			// Presence with an empty ID records actual visual ambiguity even
			// when no other source retains either supporting group.
			selection[source] = ""
			continue
		}
		best := references[0]
		for _, candidate := range references[1:] {
			if preferVisualPublication(groups[candidate.group], candidate.interval, groups[best.group], best.interval) {
				best = candidate
			}
		}
		selection[source] = groups[best.group].ID
	}
	return selection, nil
}

func nestedVisualIntervals(a, b Interval) bool {
	contains := func(outer, inner Interval) bool {
		return outer.StartTicks <= inner.StartTicks+visualSequenceResidual && outer.EndTicks >= inner.EndTicks-visualSequenceResidual
	}
	return contains(a, b) || contains(b, a)
}

func preferVisualPublication(a Group, ar Interval, b Group, br Interval) bool {
	if a.VisualEvidence.CoveragePermille != b.VisualEvidence.CoveragePermille {
		return a.VisualEvidence.CoveragePermille > b.VisualEvidence.CoveragePermille
	}
	if a.VisualEvidence.MaxLumaRMSPermille != b.VisualEvidence.MaxLumaRMSPermille {
		return a.VisualEvidence.MaxLumaRMSPermille < b.VisualEvidence.MaxLumaRMSPermille
	}
	if ar.StartTicks != br.StartTicks {
		return ar.StartTicks < br.StartTicks
	}
	if ar.EndTicks != br.EndTicks {
		return ar.EndTicks < br.EndTicks
	}
	return a.ID < b.ID
}

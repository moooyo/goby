package introdetect

// markGroupConflicts preserves the eligibility snapshot from before conflict
// detection. Adding one reason cannot suppress another pair's conflict proof.
// Status and group identity are finalized separately by the caller.
func markGroupConflicts(groups []Group, o Options, budget *workBudget) error {
	if err := budget.ctx.Err(); err != nil {
		return err
	}
	eligible := make([]bool, len(groups))
	conflicts := make([]bool, len(groups))
	for index := range groups {
		if err := budget.spend(); err != nil {
			return err
		}
		for range groups[index].Members {
			if err := budget.spend(); err != nil {
				return err
			}
		}
		eligible[index] = hypothesisQualifiedGroup(groups[index], o)
	}
	for i := range groups {
		if err := budget.spend(); err != nil {
			return err
		}
		for j := i + 1; j < len(groups); j++ {
			if err := budget.spend(); err != nil {
				return err
			}
			shared, conflict := false, false
			for _, a := range groups[i].Members {
				if err := budget.spend(); err != nil {
					return err
				}
				for _, b := range groups[j].Members {
					if err := budget.spend(); err != nil {
						return err
					}
					if a.SourceKey == b.SourceKey {
						shared = true
						conflict = conflict || !compatibleInterval(a.Interval, b.Interval, o)
					}
				}
			}
			if shared && eligible[i] && eligible[j] {
				// Charge the bounded internal map and member comparisons made by
				// compatibleGroupAlignment as well as the interval comparisons.
				for range groups[i].phaseAnchors {
					if err := budget.spend(); err != nil {
						return err
					}
				}
				for range groups[i].phaseClasses {
					if err := budget.spend(); err != nil {
						return err
					}
				}
				for range groups[i].Members {
					if err := budget.spend(); err != nil {
						return err
					}
					for range groups[j].Members {
						if err := budget.spend(); err != nil {
							return err
						}
					}
				}
				conflict = conflict || !compatibleGroupAlignment(groups[i], groups[j], o)
			}
			if conflict {
				conflicts[i], conflicts[j] = true, true
			}
		}
	}
	if err := budget.ctx.Err(); err != nil {
		return err
	}
	for index, conflict := range conflicts {
		if err := budget.spend(); err != nil {
			return err
		}
		if conflict {
			groups[index].Reasons = addReason(groups[index].Reasons, CompetingIntervals)
		}
	}
	return nil
}

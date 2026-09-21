package introdetect

import "fmt"

// periodicVisualEvidence detects ambiguity, not eligibility. Missing evidence
// remains the main time ledger's responsibility and cannot erase an observed
// repetition. Each proposed period is checked against the entire source window.
func periodicVisualEvidence(samples []VisualSample, ids []int, start, end int, o Options, budget *workBudget) (bool, error) {
	if budget == nil || len(ids) != len(samples) || start < 0 || end < start || end > len(samples) ||
		o.MinVisualStates < 1 || o.MaxVisualSamples < 1 || o.VisualAlignmentTicks < 0 {
		return false, fmt.Errorf("%w: periodic visual window", ErrInvalidInput)
	}
	if end-start < 2 {
		return false, nil
	}
	if end-start > o.MaxVisualSamples {
		return false, fmt.Errorf("%w: periodic visual samples", ErrLimit)
	}
	observedStates := make(map[int]struct{})
	for i := start; i < end; i++ {
		if err := budget.spend(); err != nil {
			return false, err
		}
		if samples[i].Ticks < 0 || ids[i] < -1 || i > start && samples[i].Ticks <= samples[i-1].Ticks {
			return false, fmt.Errorf("%w: periodic visual observations", ErrInvalidInput)
		}
		if periodicVisualObservable(samples, ids, i, o) {
			observedStates[ids[i]] = struct{}{}
		}
	}
	if len(observedStates) < o.MinVisualStates {
		return false, nil
	}
	windowSpan := samples[end-1].Ticks - samples[start].Ticks
	entries := make(map[int][]int64, len(observedStates))
	periods := make(map[int64]struct{})
	maximumPeriods := min(end-start, o.MaxVisualSamples)
	previous := -1
	for i := start; i < end; i++ {
		if err := budget.spend(); err != nil {
			return false, err
		}
		if !periodicVisualObservable(samples, ids, i, o) {
			previous = -1
			continue
		}
		entry := previous < 0 || ids[previous] != ids[i] || samples[i].Ticks-samples[previous].Ticks > maxVisualEvidenceEdgeTicks
		previous = i
		if !entry {
			continue
		}
		for _, prior := range entries[ids[i]] {
			if err := budget.spend(); err != nil {
				return false, err
			}
			period := samples[i].Ticks - prior
			if period <= 0 || period > windowSpan/2 {
				continue
			}
			if _, exists := periods[period]; exists {
				continue
			}
			if len(periods) == maximumPeriods {
				return false, fmt.Errorf("%w: periodic visual candidates", ErrLimit)
			}
			periods[period] = struct{}{}
			repeated, err := matchesVisualPeriod(samples, ids, start, end, period, len(observedStates), o, budget)
			if err != nil || repeated {
				return repeated, err
			}
		}
		entries[ids[i]] = append(entries[ids[i]], samples[i].Ticks)
	}
	return false, nil
}

func periodicVisualObservable(samples []VisualSample, ids []int, index int, o Options) bool {
	return ids[index] >= 0 && samples[index].Contrast >= o.MinVisualContrast
}

func matchesVisualPeriod(samples []VisualSample, ids []int, start, end int, period int64, requiredStates int, o Options, budget *workBudget) (bool, error) {
	matchedStates := make(map[int]struct{}, requiredStates)
	matched := 0
	firstSupport, lastSupport := int64(-1), int64(-1)
	right, lastTarget := start, -1
	lastTick := samples[end-1].Ticks
	for i := start; i < end; i++ {
		if err := budget.spend(); err != nil {
			return false, err
		}
		// This subtraction also avoids overflowing the forward target clock.
		if period > lastTick-samples[i].Ticks {
			break
		}
		target := samples[i].Ticks + period
		for right < end {
			if err := budget.spend(); err != nil {
				return false, err
			}
			if samples[right].Ticks >= target {
				break
			}
			right++
		}
		if right == end {
			break
		}
		j := right
		if samples[right].Ticks != target && right > start {
			left := right - 1
			// A target inside a missing-observation hole has no nearest usable
			// observation. Its two neighboring scenes cannot fill that hole.
			if samples[right].Ticks-samples[left].Ticks > maxVisualEvidenceEdgeTicks {
				continue
			}
			if target-samples[left].Ticks < samples[right].Ticks-target {
				j = left
			}
		}
		if j <= i || j <= lastTarget || absolute(samples[j].Ticks-target) > o.VisualAlignmentTicks {
			continue
		}
		lastTarget = j
		if !periodicVisualObservable(samples, ids, i, o) || !periodicVisualObservable(samples, ids, j, o) {
			continue
		}
		if ids[i] != ids[j] {
			return false, nil
		}
		matched++
		matchedStates[ids[i]] = struct{}{}
		if firstSupport < 0 {
			firstSupport = samples[i].Ticks
		}
		lastSupport = max(lastSupport, samples[j].Ticks)
	}
	return matched >= 2*o.MinVisualStates && len(matchedStates) == requiredStates &&
		firstSupport >= 0 && lastSupport-firstSupport >= 2*period, nil
}

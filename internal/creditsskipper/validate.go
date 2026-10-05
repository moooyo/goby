// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

const MaxEvidenceRanges = 4096
const MaxSummaryFrames = 250000

func validSource(source Source) bool {
	return source == ChapterSource || source == BlackFrameSource || source == ChromaprintSource || source == CombinedSource
}
func sameSegments(a, b []Segment) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func sameNumbers(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func boundedSegment(segment Segment, duration float64) bool {
	return segment.Valid() && validSource(segment.Source) && segment.Start >= 0 && segment.End <= duration
}

// ValidateStoredResult validates provenance and deterministic relationships in a
// stored result. It does not claim to re-prove decoded pixels, chapter metadata,
// or independently matched audio. The caller must bind those source facts and
// the exact Chromaprint candidate separately, and bound serialized payload size.
func ValidateStoredResult(result Result, duration float64, isMovie bool) error {
	if !finite(duration) || duration <= 0 || result.Version != Version || result.UpstreamCommit != UpstreamCommit || result.Options != DefaultOptions() || result.Window != Window(duration, isMovie) {
		return ErrInvalidInput
	}
	e := result.Evidence
	if len(e.RawCandidates) > 3 || len(e.CombinedCandidates) > 3 || len(result.Segments) > 3 || len(e.Adjustments) > 3 || len(e.HardBoundaries) > 1 || len(e.IntervalProbeRanges) > 128 || len(e.BoundaryProbes) > 128 || len(e.BlackIntervals) > MaxEvidenceRanges {
		return ErrInvalidInput
	}
	if e.BlackFrameCount < 0 || e.BlackFrameCount > MaxSummaryFrames || e.VisualCount < 0 || e.VisualCount > MaxSummaryFrames {
		return ErrInvalidInput
	}
	seen := map[Source]bool{}
	previousSourceRank := 0
	for _, candidate := range e.RawCandidates {
		if !boundedSegment(candidate, duration) || candidate.Source == CombinedSource || seen[candidate.Source] {
			return ErrInvalidInput
		}
		rank := map[Source]int{ChapterSource: 1, ChromaprintSource: 2, BlackFrameSource: 3}[candidate.Source]
		if rank <= previousSourceRank {
			return ErrInvalidInput
		}
		previousSourceRank = rank
		seen[candidate.Source] = true
		if candidate.Source == ChromaprintSource && isMovie {
			return ErrInvalidInput
		}
		if candidate.Source != ChapterSource && candidate.Start < result.Window.Start {
			return ErrInvalidInput
		}
		if candidate.Source == ChapterSource {
			maximum := float64(result.Options.MaximumCreditsDuration)
			if isMovie {
				maximum = float64(result.Options.MaximumMovieCreditsDuration)
			}
			if candidate.End-candidate.Start < float64(result.Options.MinimumCreditsDuration) || candidate.End-candidate.Start > maximum {
				return ErrInvalidInput
			}
		}
		if candidate.Source == BlackFrameSource && candidate.End-candidate.Start < float64(result.Options.MinimumCreditsDuration) {
			return ErrInvalidInput
		}
	}
	if !seen[BlackFrameSource] {
		if e.VisualMethod != "" {
			return ErrInvalidInput
		}
	} else if (e.VisualMethod != "BlackFrame" && e.VisualMethod != "Entropy") || (e.VisualMethod == "BlackFrame" && e.BlackFrameCount == 0) || (e.VisualMethod == "Entropy" && e.VisualCount == 0) {
		return ErrInvalidInput
	}
	if e.BlackFrameCount == 0 {
		if e.NormalizedMinimum != 0 || e.SceneChange != 0 || len(e.IntervalProbeRanges) != 0 || len(e.BlackIntervals) != 0 || len(e.BoundaryProbes) != 0 {
			return ErrInvalidInput
		}
	} else {
		valid := false
		for floor := 0; floor <= 30; floor++ {
			if e.NormalizedMinimum == 85*(100-floor)/100+floor && e.SceneChange == 95*(100-floor)/100+floor {
				valid = true
				break
			}
		}
		if !valid {
			return ErrInvalidInput
		}
	}
	previousEnd := -1.0
	for _, r := range e.IntervalProbeRanges {
		if !r.Valid() || r.Start < result.Window.Start || r.End > duration || r.Start <= previousEnd {
			return ErrInvalidInput
		}
		previousEnd = r.End
	}
	if err := validateRanges(e.BlackIntervals, 0, result.Window.Duration()); err != nil {
		return err
	}
	previousStart := -1.0
	for _, r := range e.BlackIntervals {
		if r.Start < previousStart {
			return ErrInvalidInput
		}
		previousStart = r.Start
		contained := false
		for _, probe := range e.IntervalProbeRanges {
			offset := probe.Start - result.Window.Start
			if r.Start >= offset && r.End <= probe.Duration()+offset {
				contained = true
				break
			}
		}
		if !contained {
			return ErrInvalidInput
		}
	}
	for _, b := range e.BoundaryProbes {
		if !b.Range.Valid() || b.Range.Start < result.Window.Start || b.Range.End > duration || b.Minimum < 0 || b.Minimum > 100 || !finite(b.OriginalStart) || !finite(b.RefinedStart) || b.OriginalStart < 0 || b.OriginalStart > result.Window.Duration() || b.RefinedStart < 0 || b.RefinedStart > b.OriginalStart {
			return ErrInvalidInput
		}
		if b.RefinedStart != b.OriginalStart && (b.RefinedStart+result.Window.Start <= b.Range.Start || b.RefinedStart+result.Window.Start > b.Range.End) {
			return ErrInvalidInput
		}
	}
	combined := Combine(e.RawCandidates, duration, result.Options.MinimumCreditsDuration)
	if !sameSegments(combined, e.CombinedCandidates) || !sameNumbers(HardBoundaries(e.RawCandidates, duration), e.HardBoundaries) || len(e.Adjustments) != len(combined) {
		return ErrInvalidInput
	}
	var final []Segment
	for i, a := range e.Adjustments {
		if a.Original != combined[i] || !validAdjustmentPlan(a.Plan, a.Original, duration, result.Options) || a.SearchRange != a.Plan.SearchRange || len(a.Silence) > MaxEvidenceRanges || len(a.Keyframes) > MaxEvidenceRanges {
			return ErrInvalidInput
		}
		if !a.Plan.ProbeEnd && (len(a.Silence) > 0 || len(a.Keyframes) > 0) {
			return ErrInvalidInput
		}
		if err := validateRanges(a.Silence, 0, duration); err != nil {
			return err
		}
		for _, value := range a.Keyframes {
			if !finite(value) || value < 0 || value > duration {
				return ErrInvalidInput
			}
		}
		if FinishTimeAdjustment(a.Plan, a.Silence, a.Keyframes) != a.Adjusted {
			return ErrInvalidInput
		}
		if a.Adjusted.Valid() {
			if !boundedSegment(a.Adjusted, duration) {
				return ErrInvalidInput
			}
			final = append(final, a.Adjusted)
		}
	}
	if !sameSegments(final, result.Segments) {
		return ErrInvalidInput
	}
	for i := 1; i < len(final); i++ {
		if final[i].Start < final[i-1].Start {
			return ErrInvalidInput
		}
	}
	return nil
}

func validAdjustmentPlan(plan AdjustmentPlan, original Segment, duration float64, options Options) bool {
	if !finite(plan.Segment.Start) || !finite(plan.Segment.End) || plan.Segment.Start < 0 || plan.Segment.End < 0 || plan.Segment.Start > duration || plan.Segment.End > duration || plan.Segment.Source != original.Source || !plan.UseSilence || !plan.UseKeyframes || plan.MinimumSilenceDuration != options.SilenceDetectionMinimumDuration {
		return false
	}
	// Chapter-only ranges never snap to chapters again; this is fully replayable
	// without source metadata. Other sources may snap within native windows.
	if original.Source == ChapterSource {
		return plan == PlanTimeAdjustment(original, duration, nil, options)
	}
	if original.Start <= options.EndSnapThreshold+adjustmentEpsilon {
		if plan.Segment.Start != 0 {
			return false
		}
	} else if plan.Segment.Start < max(0, original.Start-options.AdjustWindowOutward)-adjustmentEpsilon || plan.Segment.Start > min(duration, original.Start+options.AdjustWindowInward)+adjustmentEpsilon {
		return false
	}
	if original.End >= duration-options.EndSnapThreshold-adjustmentEpsilon {
		return !plan.ProbeEnd && plan.Segment.End == duration && plan.SearchRange == (Range{})
	}
	if !plan.ProbeEnd || plan.Segment.End < max(0, original.End-options.AdjustWindowInward)-adjustmentEpsilon || plan.Segment.End > min(duration, original.End+options.AdjustWindowOutward)+adjustmentEpsilon {
		return false
	}
	return plan.SearchRange == SearchRange(plan.Segment.End, duration, options.AdjustWindowInward, options.AdjustWindowOutward)
}

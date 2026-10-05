// SPDX-FileCopyrightText: 2026 rlauuzo
// SPDX-FileCopyrightText: 2026 AbandonedCart
// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

import (
	"context"
	"fmt"
)

// Detect orchestrates the pinned default CreditsPass on one authorized source.
// Audio comparison is performed by introskipper on its independently admitted
// season cohort beforehand. Every applicable analyzer contributes; there is no
// chapter-first or black-frame-first early exit from the complete pass.
//
// Goby's failure boundary is deliberately strict: any probe error fails this
// request rather than converting a failed source read into a completed no-match.
// No partial Result is returned. The upstream successful decisions are retained.
func Detect(ctx context.Context, request Request, probe Probe) (Result, error) {
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}
	if ctx == nil || probe == nil {
		return Result{}, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	options := DefaultOptions()
	window := Window(request.DurationSeconds, request.IsMovie)
	result := Result{Version: Version, UpstreamCommit: UpstreamCommit, Options: options, Window: window, Segments: []Segment{}}
	candidates := FindChapterCandidates(request.Chapters, request.DurationSeconds, request.IsMovie, options)
	candidates = append(candidates, request.AudioSegments...)
	frames, err := probe.ScanKeyframes(ctx, window, options.BlackFrameThreshold)
	if err != nil {
		return Result{}, fmt.Errorf("credits keyframe scan: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := validateKeyframeEvidence(frames, window.Duration()); err != nil {
		return Result{}, err
	}
	result.Evidence.BlackFrameCount = len(frames.BlackFrames)
	result.Evidence.VisualCount = len(frames.Visuals)
	var visual *Segment
	if len(frames.BlackFrames) > 0 {
		plan := PlanBlackFrames(frames.BlackFrames, window, options)
		result.Evidence.NormalizedMinimum, result.Evidence.SceneChange = plan.Minimum, plan.SceneChange
		result.Evidence.IntervalProbeRanges = append([]Range(nil), plan.IntervalProbeRanges...)
		var intervals []Range
		for _, r := range plan.IntervalProbeRanges {
			values, err := probe.ScanBlackIntervals(ctx, r, options.BlackFrameThreshold, plan.Minimum)
			if err != nil {
				return Result{}, fmt.Errorf("credits black interval scan: %w", err)
			}
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			if err := validateRanges(values, 0, r.Duration()); err != nil {
				return Result{}, err
			}
			offset := r.Start - window.Start
			if len(intervals)+len(values) > MaxEvidenceRanges {
				return Result{}, ErrInvalidInput
			}
			for _, value := range values {
				intervals = append(intervals, Range{value.Start + offset, value.End + offset})
			}
		}
		result.Evidence.BlackIntervals = append([]Range(nil), intervals...)
		for _, scene := range FinalizeBlackScenes(frames.BlackFrames, intervals, plan, options) {
			start := scene.StartTime
			last, first, exists := FindBoundaryKeyframeTimes(frames.BlackFrames, scene)
			if options.RefineCreditsBoundary && exists && ShouldRefineBoundary(scene, last, options.MinimumCreditsDuration) {
				minimum := SelectProbeMinimum(frames.BlackFrames, scene, plan.SceneChange)
				r := Range{last + window.Start, first + window.Start}
				values, err := probe.ScanBoundary(ctx, r, options.BlackFrameThreshold, minimum)
				if err != nil {
					return Result{}, fmt.Errorf("credits boundary scan: %w", err)
				}
				if err := ctx.Err(); err != nil {
					return Result{}, err
				}
				if err := validateBlackFrames(values, r.Duration()); err != nil {
					return Result{}, err
				}
				if len(values) > 0 {
					if refined, ok := TryRefineBoundaryTime(values[0].Time, last, scene.StartTime); ok {
						start = refined
					}
				}
				result.Evidence.BoundaryProbes = append(result.Evidence.BoundaryProbes, BoundaryEvidence{r, minimum, scene.StartTime, start})
			}
			if scene.EndTime-start >= float64(options.MinimumCreditsDuration) {
				visual = &Segment{start + window.Start, scene.EndTime + window.Start, BlackFrameSource}
				result.Evidence.VisualMethod = "BlackFrame"
				break
			}
		}
	}
	if visual == nil && options.DetectNonBlackCredits {
		if r := FindEntropyRange(frames.Visuals, options.MinimumCreditsDuration); r != nil {
			visual = &Segment{r.Start + window.Start, r.End + window.Start, BlackFrameSource}
			result.Evidence.VisualMethod = "Entropy"
		}
	}
	if visual != nil {
		candidates = append(candidates, *visual)
	}
	result.Evidence.RawCandidates = append([]Segment(nil), candidates...)
	combined := Combine(candidates, window.End, options.MinimumCreditsDuration)
	result.Evidence.CombinedCandidates = append([]Segment(nil), combined...)
	result.Evidence.HardBoundaries = HardBoundaries(candidates, window.End)
	for _, candidate := range combined {
		plan := PlanTimeAdjustment(candidate, request.DurationSeconds, request.Chapters, options)
		var silence []Range
		var keyframes []float64
		if plan.ProbeEnd && plan.SearchRange.Duration() > 0 {
			if plan.UseSilence {
				silence, err = probe.ScanSilence(ctx, plan.SearchRange)
				if err != nil {
					return Result{}, fmt.Errorf("credits silence adjustment: %w", err)
				}
				if err := ctx.Err(); err != nil {
					return Result{}, err
				}
				if err := validateRanges(silence, 0, request.DurationSeconds); err != nil {
					return Result{}, err
				}
			}
			if plan.UseKeyframes {
				keyframes, err = probe.ScanKeyframesAtBoundary(ctx, plan.SearchRange)
				if err != nil {
					return Result{}, fmt.Errorf("credits keyframe adjustment: %w", err)
				}
				if err := ctx.Err(); err != nil {
					return Result{}, err
				}
				// Native -skip_frame nokey may emit a keyframe past the requested
				// -to boundary. SelectNearest retains it; enforce source bounds,
				// not a new search-window clipping rule.
				for _, value := range keyframes {
					if !finite(value) || value < 0 || value > request.DurationSeconds {
						return Result{}, ErrInvalidInput
					}
				}
			}
		}
		adjusted := FinishTimeAdjustment(plan, silence, keyframes)
		result.Evidence.Adjustments = append(result.Evidence.Adjustments, AdjustmentEvidence{candidate, adjusted, plan.SearchRange, append([]Range(nil), silence...), append([]float64(nil), keyframes...), plan})
		if adjusted.Valid() {
			result.Segments = append(result.Segments, adjusted)
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := ValidateStoredResult(result, request.DurationSeconds, request.IsMovie); err != nil {
		return Result{}, err
	}
	return result, nil
}

func validateRequest(request Request) error {
	if !finite(request.DurationSeconds) || request.DurationSeconds <= 0 || len(request.Chapters) > MaxEvidenceRanges || len(request.AudioSegments) > 1 || (request.IsMovie && len(request.AudioSegments) > 0) {
		return ErrInvalidInput
	}
	previous := -1.0
	for _, chapter := range request.Chapters {
		if !finite(chapter.StartSeconds) || chapter.StartSeconds < 0 || chapter.StartSeconds > request.DurationSeconds || chapter.StartSeconds < previous {
			return ErrInvalidInput
		}
		previous = chapter.StartSeconds
	}
	window := Window(request.DurationSeconds, request.IsMovie)
	for _, segment := range request.AudioSegments {
		if !segment.Valid() || segment.Source != ChromaprintSource || segment.Start < window.Start || segment.End > window.End {
			return ErrInvalidInput
		}
	}
	return nil
}
func validateRanges(values []Range, start, end float64) error {
	if len(values) > MaxEvidenceRanges {
		return ErrInvalidInput
	}
	for _, value := range values {
		if !value.Valid() || value.Start < start || value.End > end {
			return ErrInvalidInput
		}
	}
	return nil
}
func validateBlackFrames(frames []BlackFrame, duration float64) error {
	if len(frames) > MaxSummaryFrames {
		return ErrInvalidInput
	}
	lastTime, lastFrame := -1.0, -1
	for _, frame := range frames {
		if !finite(frame.Time) || frame.Time < 0 || frame.Time > duration || frame.Time < lastTime || frame.Frame < 0 || frame.Frame < lastFrame || frame.Percentage < 0 || frame.Percentage > 100 {
			return ErrInvalidInput
		}
		lastTime, lastFrame = frame.Time, frame.Frame
	}
	return nil
}
func validateKeyframeEvidence(evidence KeyframeEvidence, duration float64) error {
	if len(evidence.Visuals) > MaxSummaryFrames {
		return ErrInvalidInput
	}
	if err := validateBlackFrames(evidence.BlackFrames, duration); err != nil {
		return err
	}
	previous := -1.0
	for _, v := range evidence.Visuals {
		if !finite(v.Time) || v.Time < 0 || v.Time > duration || v.Time < previous || !finite(v.Entropy) || v.Entropy < 0 || v.Entropy > 1 || !finite(v.Saturation) || v.Saturation < 0 || v.Saturation > 255 {
			return ErrInvalidInput
		}
		previous = v.Time
	}
	return nil
}

// SPDX-FileCopyrightText: 2022 ConfusedPolarBear
// SPDX-FileCopyrightText: 2024-2026 rlauuzo
// SPDX-FileCopyrightText: 2024-2026 AbandonedCart
// SPDX-FileCopyrightText: 2024-2026 Kilian von Pflugk
// SPDX-License-Identifier: GPL-3.0-only

// Package creditsskipper ports the pinned default chapter, visual, combination,
// and boundary-adjustment stages of Intro Skipper's CreditsPass. It performs no
// filesystem or process I/O; the caller supplies authorized, bounded probes.
package creditsskipper

import (
	"context"
	"errors"
	"math"
)

const (
	Version                       = "intro-skipper-credits-pass-v1"
	UpstreamCommit                = "6e0cb179007ac4c16cd9f358e9a617e791e9bf06"
	MaximumSceneMergeGapSeconds   = 20.0
	MinimumIntervalOverlapSeconds = 0.25
	MinimumBoundaryProbeWindow    = 0.50
	MinimumBlackIntervalDuration  = 0.1
	DefaultChapterPattern         = `(^|\s)(Credits?|ED|Ending|Outro)(?![\s:]+End)(\s|:|$)`
)

var ErrInvalidInput = errors.New("invalid credits skipper input")

type Source string

const (
	ChapterSource     Source = "Chapter"
	BlackFrameSource  Source = "BlackFrame"
	ChromaprintSource Source = "Chromaprint"
	CombinedSource    Source = "Combined"
)

// Range uses binary64 seconds. Probe methods state whether their coordinates
// are relative to the requested range or absolute within the source.
type Range struct{ Start, End float64 }

func (r Range) Duration() float64 { return r.End - r.Start }
func (r Range) Valid() bool       { return finite(r.Start) && finite(r.End) && r.End > 0 && r.End > r.Start }

type Segment struct {
	Start, End float64
	Source     Source
}

func (s Segment) Valid() bool { return Range{s.Start, s.End}.Valid() }

type Chapter struct {
	Name         string
	StartSeconds float64
}
type BlackFrame struct {
	Frame, Percentage int
	Time              float64
}
type KeyframeVisual struct{ Time, Entropy, Saturation float64 }
type Scene struct {
	StartFrame, EndFrame int
	StartTime, EndTime   float64
}
type KeyframeEvidence struct {
	BlackFrames []BlackFrame
	Visuals     []KeyframeVisual
}

// Options records the pinned upstream defaults, not a new configuration API.
type Options struct {
	MinimumCreditsDuration, MaximumCreditsDuration, MaximumMovieCreditsDuration int
	BlackFrameMinimumPercentage, BlackFrameThreshold                            int
	RefineCreditsBoundary, DetectNonBlackCredits, FullLengthChapters            bool
	EnableSponsorBlockChapterDetection                                          bool
	AdjustIntroBasedOnChapters, AdjustIntroBasedOnSilence, SnapToKeyframe       bool
	EndSnapThreshold, AdjustWindowInward, AdjustWindowOutward                   float64
	IntroStartOffset, IntroEndOffset                                            float64
	IncludeIntroStartOffsetWhenSnapping                                         bool
	SilenceDetectionMaximumNoise                                                int
	SilenceDetectionMinimumDuration                                             float64
}

func DefaultOptions() Options {
	return Options{MinimumCreditsDuration: 15, MaximumCreditsDuration: 450, MaximumMovieCreditsDuration: 900,
		BlackFrameMinimumPercentage: 85, BlackFrameThreshold: 28, RefineCreditsBoundary: true, DetectNonBlackCredits: true,
		EnableSponsorBlockChapterDetection: true, AdjustIntroBasedOnChapters: true, AdjustIntroBasedOnSilence: true, SnapToKeyframe: true,
		EndSnapThreshold: 2, AdjustWindowInward: 5, AdjustWindowOutward: 2,
		SilenceDetectionMaximumNoise: -50, SilenceDetectionMinimumDuration: 0.33}
}

func Window(durationSeconds float64, isMovie bool) Range {
	length := 450.0
	if isMovie {
		length = 900
	}
	return Range{max(0, durationSeconds-length), durationSeconds}
}

// Probe owns authorization, source identity, resource bounds, process joining,
// and cancellation. All calls must finish before returning. No probe is a
// playback action. Missing optional FFmpeg visual filters may return no Visuals;
// failed reads, decoding, or processes must return an error.
type Probe interface {
	// Both sequences use seconds relative to window.Start and retain decode order.
	ScanKeyframes(context.Context, Range, int) (KeyframeEvidence, error)
	// Intervals are relative to range.Start. Integers are pixel threshold, then
	// the normalized minimum black percentage, in that order.
	ScanBlackIntervals(context.Context, Range, int, int) ([]Range, error)
	// Frames are relative to range.Start, already filtered by the minimum
	// black percentage. Integers are pixel threshold, then minimum percentage.
	ScanBoundary(context.Context, Range, int, int) ([]BlackFrame, error)
	// Silence and boundary-keyframe timestamps are absolute source seconds.
	ScanSilence(context.Context, Range) ([]Range, error)
	ScanKeyframesAtBoundary(context.Context, Range) ([]float64, error)
}

type Request struct {
	DurationSeconds float64
	IsMovie         bool
	Chapters        []Chapter
	// Only same-season independently admitted episode audio matches belong here.
	// Movie requests must leave this empty. Identity/support are caller-owned.
	AudioSegments []Segment
}

type BoundaryEvidence struct {
	Range                       Range
	Minimum                     int
	OriginalStart, RefinedStart float64
}
type AdjustmentEvidence struct {
	Original, Adjusted Segment
	SearchRange        Range
	Silence            []Range
	Keyframes          []float64
	Plan               AdjustmentPlan
}
type Evidence struct {
	RawCandidates      []Segment
	CombinedCandidates []Segment
	// HardBoundaries constrain combination only. Native boundary adjustment
	// runs afterwards and may move a final end across one of these timestamps.
	HardBoundaries                                               []float64
	VisualMethod                                                 string
	BlackFrameCount, VisualCount, NormalizedMinimum, SceneChange int
	IntervalProbeRanges                                          []Range
	BlackIntervals                                               []Range
	BoundaryProbes                                               []BoundaryEvidence
	Adjustments                                                  []AdjustmentEvidence
}
type Result struct {
	Version, UpstreamCommit string
	Options                 Options
	Window                  Range
	Segments                []Segment
	Evidence                Evidence
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

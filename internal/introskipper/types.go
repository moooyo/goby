// SPDX-License-Identifier: GPL-3.0-only

// Package introskipper ports the pinned Intro Skipper Introduction raw-candidate
// matcher. It accepts complete caller-admitted Chromaprint sequences and does
// not perform I/O, optional boundary adjustment, or visual verification.
package introskipper

import (
	"errors"
	"fmt"
	"math"
)

const (
	Version                    = "intro-skipper-v1"
	UpstreamCommit             = "6e0cb179007ac4c16cd9f358e9a617e791e9bf06"
	UpstreamRelease            = "12.0.4.0"
	TicksPerSecond       int64 = 10_000_000
	MaxEpisodes                = 32
	MaxFingerprintPoints       = 5000
	MaxComparisons       int64 = 200_000_000
)

var (
	ErrInvalidInput = errors.New("invalid intro skipper input")
	ErrLimit        = errors.New("intro skipper resource limit exceeded")
)

// Options retains the upstream configuration names and units. AnalysisLengthLimit
// is in minutes; the duration and time-skip fields are in seconds. The upper
// bounds enforced here are Goby's execution limits, not upstream defaults.
type Options struct {
	AnalysisPercent                    int
	AnalysisLengthLimit                int
	MinimumIntroDuration               int
	MaximumIntroDuration               int
	MaximumFingerprintPointDifferences int
	MaximumTimeSkip                    float64
	InvertedIndexShift                 int
}

func DefaultOptions() Options {
	return Options{
		AnalysisPercent: 25, AnalysisLengthLimit: 10,
		MinimumIntroDuration: 15, MaximumIntroDuration: 120,
		MaximumFingerprintPointDifferences: 6, MaximumTimeSkip: 3.5,
		InvertedIndexShift: 2,
	}
}

// ValidateOptions requires a complete configuration; zero values do not select
// defaults because zero is a meaningful value for several upstream settings.
func ValidateOptions(options Options) error {
	if options.AnalysisPercent < 1 || options.AnalysisPercent > 50 {
		return fmt.Errorf("%w: AnalysisPercent must be between 1 and 50", ErrInvalidInput)
	}
	if options.AnalysisLengthLimit < 1 || options.AnalysisLengthLimit > 10 {
		return fmt.Errorf("%w: AnalysisLengthLimit must be between 1 and 10 minutes", ErrInvalidInput)
	}
	if options.MinimumIntroDuration < 1 || options.MinimumIntroDuration > 600 {
		return fmt.Errorf("%w: MinimumIntroDuration must be between 1 and 600 seconds", ErrInvalidInput)
	}
	if options.MaximumIntroDuration < options.MinimumIntroDuration || options.MaximumIntroDuration > 600 {
		return fmt.Errorf("%w: MaximumIntroDuration must be between MinimumIntroDuration and 600 seconds", ErrInvalidInput)
	}
	if options.MaximumFingerprintPointDifferences < 0 || options.MaximumFingerprintPointDifferences > 32 {
		return fmt.Errorf("%w: MaximumFingerprintPointDifferences must be between 0 and 32", ErrInvalidInput)
	}
	if math.IsNaN(options.MaximumTimeSkip) || math.IsInf(options.MaximumTimeSkip, 0) || options.MaximumTimeSkip < 0 || options.MaximumTimeSkip > 30 {
		return fmt.Errorf("%w: MaximumTimeSkip must be finite and between 0 and 30 seconds", ErrInvalidInput)
	}
	if options.InvertedIndexShift < 0 || options.InvertedIndexShift > 32 {
		return fmt.Errorf("%w: InvertedIndexShift must be between 0 and 32", ErrInvalidInput)
	}
	return nil
}

// FingerprintEndSeconds preserves the pinned upstream binary64 calculation.
// Callers must first validate a positive duration and ValidateOptions.
func FingerprintEndSeconds(durationTicks int64, options Options) float64 {
	duration := float64(durationTicks) / float64(TicksPerSecond)
	end := duration
	if duration >= 300 {
		// The upstream queue caches the divided binary64 ratio before
		// multiplying it by the episode duration.
		ratio := float64(options.AnalysisPercent) / 100
		end = duration * ratio
	}
	return min(end, float64(60*options.AnalysisLengthLimit))
}

// Episode identities are caller assertions. EpisodeKey identifies an episode
// across encodes; ContentIdentity identifies the entire source, not its prefix.
// Fingerprint must preserve the complete, original extraction sequence.
type Episode struct {
	EpisodeKey       string
	SourceKey        string
	ContentIdentity  string
	AlgorithmProfile string
	DurationTicks    int64
	Fingerprint      []uint32
}

// Cohort retains caller order; the upstream first-valid-pair rule is ordered.
type Cohort struct {
	Key      string
	Episodes []Episode
}

type Interval struct {
	StartTicks int64
	EndTicks   int64
}

// Support records the actual accepted pair at the moment a candidate won. It
// need not equal that peer episode's final candidate from a later comparison.
type Support struct {
	EpisodeKey       string
	SourceKey        string
	ContentIdentity  string
	AlgorithmProfile string
	Interval         Interval
}

type Candidate struct {
	Interval       Interval
	UpstreamCommit string
	Support        []Support
}

const (
	Qualified          = "qualified"
	NoResult           = "no_result"
	NoRepeatedInterval = "no_repeated_interval"
)

type EpisodeResult struct {
	EpisodeKey       string
	SourceKey        string
	ContentIdentity  string
	AlgorithmProfile string
	DurationTicks    int64
	Status           string
	Reasons          []string
	Candidate        *Candidate
}

type Result struct {
	Version   string
	CohortKey string
	Options   Options
	Episodes  []EpisodeResult
}

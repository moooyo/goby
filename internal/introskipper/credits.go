// SPDX-FileCopyrightText: 2022 ConfusedPolarBear
// SPDX-FileCopyrightText: 2024-2026 rlauuzo
// SPDX-FileCopyrightText: 2024-2026 AbandonedCart
// SPDX-FileCopyrightText: 2024-2026 Kilian von Pflugk
// SPDX-License-Identifier: GPL-3.0-only

package introskipper

import (
	"context"
	"fmt"
)

const (
	CreditsVersion = "intro-skipper-credits-v1"
	// CreditsFingerprintDurationSeconds is the pinned MaximumCreditsDuration
	// default for episodes. Movie, chapter, and black-frame analysis are separate.
	CreditsFingerprintDurationSeconds = 450
)

// CreditsFingerprintStartSeconds preserves the pinned queue's binary64 tail
// window calculation. Unlike introductions, credits do not use AnalysisPercent
// or AnalysisLengthLimit. Callers must first validate a positive source duration.
func CreditsFingerprintStartSeconds(durationTicks int64) float64 {
	duration := float64(durationTicks) / float64(TicksPerSecond)
	return max(0, duration-CreditsFingerprintDurationSeconds)
}

// AnalyzeCredits applies the pinned Credits raw-candidate matcher to complete
// fingerprints from the final 450 seconds of independent episodes. The caller
// owns proof of this extraction window and episode admission. Results and their
// actual winning-pair support use each source's absolute file clock.
//
// The shared matcher retains MinimumIntroDuration, point differences, time skip,
// and index shift. MaximumIntroDuration is not the credits maximum: the native
// credits check rejects an RHS longer than its tail window minus one second.
// Chapter/black-frame candidates, combination, boundary adjustment, and automatic
// movie or isolated-episode detection are outside this raw-audio entry point.
func AnalyzeCredits(ctx context.Context, cohort Cohort, options Options) (Result, error) {
	return analyze(ctx, cohort, options, true)
}

// ValidateCreditsCandidate checks the original independent winning pair and
// requires each absolute interval to stay inside its own admitted tail window.
// It does not rerun matching or fabricate evidence for an optional adjustment.
func ValidateCreditsCandidate(candidate Candidate, episode Episode, cohort Cohort, options Options) error {
	if err := ValidateCandidate(candidate, episode, cohort, options); err != nil {
		return err
	}
	if err := validateCreditsInterval(candidate.Interval, episode); err != nil {
		return err
	}
	for _, support := range candidate.Support {
		for _, e := range cohort.Episodes {
			if e.EpisodeKey == support.EpisodeKey {
				if err := validateCreditsInterval(support.Interval, e); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

func validateCreditsInterval(interval Interval, e Episode) error {
	start, err := secondsToTicks(CreditsFingerprintStartSeconds(e.DurationTicks))
	if err != nil {
		return err
	}
	if !validInterval(interval, e.DurationTicks) || interval.StartTicks < start {
		return fmt.Errorf("%w: credits interval is outside its source tail window", ErrInvalidInput)
	}
	return nil
}

// ValidateCreditsEpisodeResult validates stored source identities, terminal
// outcome, and the absolute tail-window provenance of a qualified candidate.
func ValidateCreditsEpisodeResult(result EpisodeResult, episode Episode, cohort Cohort, options Options) error {
	if err := ValidateEpisodeResult(result, episode, cohort, options); err != nil {
		return err
	}
	if result.Candidate != nil {
		return ValidateCreditsCandidate(*result.Candidate, episode, cohort, options)
	}
	return nil
}

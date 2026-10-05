// SPDX-License-Identifier: GPL-3.0-only

package introskipper

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Analyze applies the pinned Introduction raw-candidate matcher without sorting
// the cohort or adding independent detection thresholds. It returns no partial
// result on invalid input, cancellation, or resource exhaustion.
func Analyze(ctx context.Context, cohort Cohort, options Options) (Result, error) {
	return analyze(ctx, cohort, options, false)
}

func analyze(ctx context.Context, cohort Cohort, options Options, credits bool) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("%w: context is required", ErrInvalidInput)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := ValidateOptions(options); err != nil {
		return Result{}, err
	}
	if err := validateCohort(cohort, true); err != nil {
		return Result{}, err
	}
	input := make([]episode, len(cohort.Episodes))
	byID := make(map[string]Episode, len(cohort.Episodes))
	for i, e := range cohort.Episodes {
		input[i] = episode{ID: e.EpisodeKey, Fingerprint: e.Fingerprint}
		if credits {
			duration := float64(e.DurationTicks) / float64(TicksPerSecond)
			input[i].DurationSeconds = &duration
			input[i].CreditsFingerprintStart = CreditsFingerprintStartSeconds(e.DurationTicks)
		}
		byID[e.EpisodeKey] = e
	}
	a := newAnalyzer(options)
	a.credits = credits
	candidates, _, err := a.findCandidates(ctx, input)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	selected := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		interval, err := segmentInterval(candidate)
		if err != nil {
			return Result{}, err
		}
		pair := a.pairs[candidate.ID]
		left, err := pairSupport(pair.lhs, byID[pair.lhs.ID])
		if err != nil {
			return Result{}, err
		}
		right, err := pairSupport(pair.rhs, byID[pair.rhs.ID])
		if err != nil {
			return Result{}, err
		}
		selected[candidate.ID] = Candidate{Interval: interval, UpstreamCommit: UpstreamCommit, Support: []Support{left, right}}
	}
	result := Result{Version: Version, CohortKey: cohort.Key, Options: options, Episodes: make([]EpisodeResult, len(cohort.Episodes))}
	if credits {
		result.Version = CreditsVersion
	}
	for i, e := range cohort.Episodes {
		out := EpisodeResult{
			EpisodeKey: e.EpisodeKey, SourceKey: e.SourceKey, ContentIdentity: e.ContentIdentity,
			AlgorithmProfile: e.AlgorithmProfile, DurationTicks: e.DurationTicks,
			Status: NoResult, Reasons: []string{NoRepeatedInterval},
		}
		if candidate, found := selected[e.EpisodeKey]; found {
			out.Status, out.Reasons, out.Candidate = Qualified, nil, &candidate
		}
		validate := ValidateEpisodeResult
		if credits {
			validate = ValidateCreditsEpisodeResult
		}
		if err := validate(out, e, cohort, options); err != nil {
			return Result{}, err
		}
		result.Episodes[i] = out
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return result, nil
}

func pairSupport(s segment, e Episode) (Support, error) {
	interval, err := segmentInterval(s)
	if err != nil {
		return Support{}, err
	}
	return Support{EpisodeKey: e.EpisodeKey, SourceKey: e.SourceKey, ContentIdentity: e.ContentIdentity, AlgorithmProfile: e.AlgorithmProfile, Interval: interval}, nil
}

func segmentInterval(s segment) (Interval, error) {
	start, err := secondsToTicks(s.Start)
	if err != nil {
		return Interval{}, err
	}
	end, err := secondsToTicks(s.End)
	if err != nil {
		return Interval{}, err
	}
	if end <= start {
		return Interval{}, fmt.Errorf("%w: invalid candidate interval", ErrInvalidInput)
	}
	return Interval{StartTicks: start, EndTicks: end}, nil
}

// secondsToTicks mirrors upstream TickConversions.TryFromSeconds, including
// midpoint-to-even rounding and the unrepresentable binary64 2^63 boundary.
func secondsToTicks(seconds float64) (int64, error) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return 0, fmt.Errorf("%w: invalid seconds", ErrInvalidInput)
	}
	scaled := math.RoundToEven(seconds * float64(TicksPerSecond))
	if scaled >= float64(math.MaxInt64) {
		return 0, fmt.Errorf("%w: seconds overflow ticks", ErrInvalidInput)
	}
	return int64(scaled), nil
}

func validIdentity(value string) bool {
	return len(value) > 0 && len(value) <= 4096 && utf8.ValidString(value) && strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validateCohort(cohort Cohort, requireFingerprint bool) error {
	if !validIdentity(cohort.Key) || len(cohort.Episodes) < 2 || len(cohort.Episodes) > MaxEpisodes {
		return fmt.Errorf("%w: cohort requires a key and 2 to %d episodes", ErrInvalidInput, MaxEpisodes)
	}
	episodeIDs, sourceIDs, contents := map[string]bool{}, map[string]bool{}, map[string]bool{}
	profile := cohort.Episodes[0].AlgorithmProfile
	for _, e := range cohort.Episodes {
		if !validIdentity(e.EpisodeKey) || !validIdentity(e.SourceKey) || !validIdentity(e.ContentIdentity) || !validIdentity(e.AlgorithmProfile) || e.DurationTicks <= 0 {
			return fmt.Errorf("%w: complete episode identities and positive duration are required", ErrInvalidInput)
		}
		if episodeIDs[e.EpisodeKey] || sourceIDs[e.SourceKey] || contents[e.ContentIdentity] {
			return fmt.Errorf("%w: duplicate episode, source, or content identity", ErrInvalidInput)
		}
		if e.AlgorithmProfile != profile {
			return fmt.Errorf("%w: incompatible extraction profiles", ErrInvalidInput)
		}
		if requireFingerprint && (e.Fingerprint == nil || len(e.Fingerprint) > MaxFingerprintPoints) {
			return fmt.Errorf("%w: a complete bounded fingerprint array is required", ErrInvalidInput)
		}
		episodeIDs[e.EpisodeKey], sourceIDs[e.SourceKey], contents[e.ContentIdentity] = true, true, true
	}
	return nil
}

func episodeInCohort(episode Episode, cohort Cohort) bool {
	for _, e := range cohort.Episodes {
		if e.EpisodeKey == episode.EpisodeKey {
			return e.SourceKey == episode.SourceKey && e.ContentIdentity == episode.ContentIdentity &&
				e.AlgorithmProfile == episode.AlgorithmProfile && e.DurationTicks == episode.DurationTicks
		}
	}
	return false
}

func validInterval(interval Interval, duration int64) bool {
	return interval.StartTicks >= 0 && interval.EndTicks > interval.StartTicks && interval.EndTicks <= duration
}

// ValidateCandidate checks provenance and interval integrity against the admitted
// cohort. Fingerprints are not required, so stored readers can validate original
// source identities without rerunning analysis. This does not prove that audio
// was matched, nor add new confidence, visual, or multi-episode requirements.
func ValidateCandidate(candidate Candidate, episode Episode, cohort Cohort, options Options) error {
	if err := ValidateOptions(options); err != nil {
		return err
	}
	if err := validateCohort(cohort, false); err != nil {
		return err
	}
	if !episodeInCohort(episode, cohort) || candidate.UpstreamCommit != UpstreamCommit || len(candidate.Support) != 2 || !validInterval(candidate.Interval, episode.DurationTicks) {
		return fmt.Errorf("%w: candidate identity, upstream pin, pair, or interval is invalid", ErrInvalidInput)
	}
	previousIndex, ownCount := -1, 0
	for _, support := range candidate.Support {
		index := -1
		for i, e := range cohort.Episodes {
			if e.EpisodeKey == support.EpisodeKey && e.SourceKey == support.SourceKey &&
				e.ContentIdentity == support.ContentIdentity && e.AlgorithmProfile == support.AlgorithmProfile &&
				validInterval(support.Interval, e.DurationTicks) {
				index = i
				break
			}
		}
		if index <= previousIndex {
			return fmt.Errorf("%w: support must be an ordered independent pair in the admitted cohort", ErrInvalidInput)
		}
		previousIndex = index
		if support.EpisodeKey == episode.EpisodeKey {
			if support.Interval != candidate.Interval {
				return fmt.Errorf("%w: candidate and own support differ", ErrInvalidInput)
			}
			ownCount++
		}
	}
	if ownCount != 1 {
		return fmt.Errorf("%w: candidate must include its own source exactly once", ErrInvalidInput)
	}
	// Upstream only checks the RHS duration before saving a pair. A snapped LHS
	// can exceed MaximumIntroDuration; do not invalidate that result here.
	return nil
}

// ValidateEpisodeResult validates the result contract and the candidate's actual
// pair, rather than substituting a peer episode's later final candidate.
func ValidateEpisodeResult(result EpisodeResult, episode Episode, cohort Cohort, options Options) error {
	if err := ValidateOptions(options); err != nil {
		return err
	}
	if err := validateCohort(cohort, false); err != nil {
		return err
	}
	if !episodeInCohort(episode, cohort) || result.EpisodeKey != episode.EpisodeKey || result.SourceKey != episode.SourceKey ||
		result.ContentIdentity != episode.ContentIdentity || result.AlgorithmProfile != episode.AlgorithmProfile || result.DurationTicks != episode.DurationTicks {
		return fmt.Errorf("%w: result does not match its admitted source", ErrInvalidInput)
	}
	switch result.Status {
	case Qualified:
		if result.Candidate == nil || len(result.Reasons) != 0 {
			return fmt.Errorf("%w: qualified result requires only a candidate", ErrInvalidInput)
		}
		return ValidateCandidate(*result.Candidate, episode, cohort, options)
	case NoResult:
		if result.Candidate != nil || len(result.Reasons) != 1 || result.Reasons[0] != NoRepeatedInterval {
			return fmt.Errorf("%w: no-result outcome requires the no-repeated-interval reason", ErrInvalidInput)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown episode result status", ErrInvalidInput)
	}
}

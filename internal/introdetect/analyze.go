package introdetect

import (
	"context"
	"fmt"
	"sort"
)

// Analyze returns deterministic observations for the entire admitted window.
// Resource exhaustion and cancellation return no partially publishable result.
// The caller must separately revalidate source identities and publication rights.
func Analyze(ctx context.Context, cohort Cohort, options Options) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("%w: context is required", ErrInvalidInput)
	}
	o, err := normalizeOptions(options)
	if err != nil {
		return Result{}, err
	}
	episodes, err := validateInput(ctx, cohort, o)
	if err != nil {
		return Result{}, err
	}
	budget := &workBudget{ctx: ctx, limit: o.MaxComparisons}
	independent := independentEpisodes(episodes, o)
	indexes := make([]audioIndex, len(episodes))
	result := Result{Version: Version, CohortKey: cohort.Key, Options: o, Groups: []Group{}, Episodes: make([]EpisodeResult, len(episodes))}
	profiles, total := make(map[string]int), 0
	for i, episode := range episodes {
		result.Episodes[i] = EpisodeResult{EpisodeKey: episode.EpisodeKey, SourceKey: episode.SourceKey,
			ContentIdentity: episode.ContentIdentity, Status: NoResult, Reasons: []Reason{}, Candidates: []Candidate{}}
		if independent[i] {
			profiles[episode.AlgorithmProfile]++
			total++
		}
	}
	for i, episode := range episodes {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		entry := &result.Episodes[i]
		if !independent[i] {
			entry.Reasons = addReason(entry.Reasons, DuplicateIdentity)
			continue
		}
		if total < o.MinSupport {
			entry.Reasons = addReason(entry.Reasons, InsufficientEpisodes)
		} else if profiles[episode.AlgorithmProfile] < o.MinSupport {
			entry.Reasons = addReason(entry.Reasons, IncompatibleProfile)
		}
		if len(episode.Audio) == 0 {
			entry.Reasons = addReason(entry.Reasons, MissingAudio)
		}
		if len(episode.Visual) == 0 {
			entry.Reasons = addReason(entry.Reasons, MissingVisual)
		}
		informative := 0
		for j := range episode.Audio {
			if informativeAudio(episode.Audio, j) {
				informative++
			}
		}
		if len(episode.Audio) != 0 && informative < 12 {
			entry.Reasons = addReason(entry.Reasons, LowAudioEntropy)
		}
		indexes[i] = indexAudio(episode.Audio)
	}
	pairs := make(map[[2]int][]pairMatch)
	limitedSources := make(map[string]bool)
	for i, a := range episodes {
		if !independent[i] || len(a.Audio) == 0 || len(a.Visual) == 0 || profiles[a.AlgorithmProfile] < o.MinSupport {
			continue
		}
		for j := i + 1; j < len(episodes); j++ {
			b := episodes[j]
			if !independent[j] || len(b.Audio) == 0 || len(b.Visual) == 0 || a.AlgorithmProfile != b.AlgorithmProfile {
				continue
			}
			offsets, limited, err := voteOffsets(a, b, indexes[j], o, budget)
			if err != nil {
				return Result{}, err
			}
			var hypotheses []pairMatch
			for _, offset := range offsets {
				runs, reasons, err := alignedAudio(a, b, offset, o, budget)
				if err != nil {
					return Result{}, err
				}
				for _, reason := range reasons {
					result.Episodes[i].Reasons = addReason(result.Episodes[i].Reasons, reason)
					result.Episodes[j].Reasons = addReason(result.Episodes[j].Reasons, reason)
				}
				for _, run := range runs {
					match, reason, err := visualConfirm(a, b, run, offset, o, budget)
					if err != nil {
						return Result{}, err
					}
					if reason != "" {
						result.Episodes[i].Reasons = addReason(result.Episodes[i].Reasons, reason)
						result.Episodes[j].Reasons = addReason(result.Episodes[j].Reasons, reason)
					}
					if match == nil {
						continue
					}
					match.left, match.right = i, j
					if limited {
						match.reasons = addReason(match.reasons, CandidateSearchLimited)
					}
					hypotheses = append(hypotheses, *match)
					if len(hypotheses) > o.MaxOffsetCandidates*(int(o.WindowTicks/o.MinDurationTicks)+1) {
						return Result{}, fmt.Errorf("%w: raw hypotheses for one pair", ErrLimit)
					}
				}
			}
			found, err := distinctPairHypotheses(hypotheses, o, budget)
			if err != nil {
				return Result{}, err
			}
			if limited {
				limitedSources[a.SourceKey], limitedSources[b.SourceKey] = true, true
				result.Episodes[i].Reasons = addReason(result.Episodes[i].Reasons, CandidateSearchLimited)
				result.Episodes[j].Reasons = addReason(result.Episodes[j].Reasons, CandidateSearchLimited)
			}
			if len(found) != 0 {
				sort.Slice(found, func(a, b int) bool {
					return preferPair(found[a], found[b], o)
				})
				pairs[[2]int{i, j}] = found
			}
		}
	}
	groups, err := collectGroups(cohort.Key, episodes, pairs, o, budget)
	if err != nil {
		return Result{}, err
	}
	// Limited searches in a different pair can hide a competing hypothesis.
	// Keep this source-wide uncertainty even when a clean clique was found.
	for i := range groups {
		for _, member := range groups[i].Members {
			if limitedSources[member.SourceKey] {
				groups[i].Reasons = addReason(groups[i].Reasons, CandidateSearchLimited)
				groups[i].Status = Review
			}
		}
	}
	result.Groups = groups
	for i, episode := range episodes {
		entry := &result.Episodes[i]
		if !independent[i] {
			continue
		}
		for _, group := range groups {
			for _, member := range group.Members {
				if member.SourceKey == episode.SourceKey {
					entry.Candidates = append(entry.Candidates, Candidate{Interval: member.Interval, GroupID: group.ID,
						Status: group.Status, Reasons: append([]Reason{}, group.Reasons...), Metrics: group.Metrics,
						Support: append([]Support{}, group.Members...)})
				}
			}
		}
		sort.Slice(entry.Candidates, func(a, b int) bool {
			if entry.Candidates[a].Interval.StartTicks != entry.Candidates[b].Interval.StartTicks {
				return entry.Candidates[a].Interval.StartTicks < entry.Candidates[b].Interval.StartTicks
			}
			return entry.Candidates[a].GroupID < entry.Candidates[b].GroupID
		})
		entry.Candidates = distinctCandidates(entry.Candidates, o)
		if len(entry.Candidates) != 0 {
			entry.Status, entry.Reasons = Qualified, []Reason{}
			for _, candidate := range entry.Candidates {
				if candidate.Status == Review {
					entry.Status = Review
				}
				for _, reason := range candidate.Reasons {
					entry.Reasons = addReason(entry.Reasons, reason)
				}
			}
		} else if len(entry.Reasons) == 0 {
			paired := false
			for key := range pairs {
				paired = paired || key[0] == i || key[1] == i
			}
			if paired {
				entry.Reasons = addReason(entry.Reasons, InsufficientConsensus)
			} else {
				entry.Reasons = addReason(entry.Reasons, NoRepeatedInterval)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result.Comparisons = budget.used
	return result, nil
}

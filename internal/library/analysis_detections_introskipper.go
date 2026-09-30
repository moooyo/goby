package library

import (
	"context"
	"encoding/json"

	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/media"
)

// AnalysisStoredIntroSkipperResult records the pinned audio matcher's actual
// pair. It deliberately contains no legacy visual or complete-clique metrics.
type AnalysisStoredIntroSkipperResult struct {
	Version string
	Options introskipper.Options
	Episode introskipper.EpisodeResult
	Reason  string
}

func analysisIntroSkipperInterval(interval introskipper.Interval, duration int64) bool {
	return interval.StartTicks >= 0 && interval.EndTicks > interval.StartTicks &&
		interval.EndTicks <= min(duration, 600*media.TicksPerSecond)
}

func validateAnalysisStoredIntroSkipperResult(value AnalysisStoredIntroSkipperResult) error {
	episode := value.Episode
	if value.Version != introskipper.Version || introskipper.ValidateOptions(value.Options) != nil ||
		!analysisOpaque(episode.SourceKey, 256) || episode.EpisodeKey != "" && !analysisOpaque(episode.EpisodeKey, 512) ||
		episode.DurationTicks <= 0 || episode.DurationTicks > media.MaxAnalysisDurationTicks || episode.Reasons == nil {
		return ErrInvalidInput
	}
	if value.Reason != "" {
		if !analysisAbstentionReason(value.Reason) || episode.Status != introskipper.NoResult || episode.Candidate != nil ||
			len(episode.Reasons) != 0 || episode.ContentIdentity != "" || episode.AlgorithmProfile != "" {
			return ErrInvalidInput
		}
		return nil
	}
	if !analysisOpaque(episode.EpisodeKey, 512) || !analysisSHA(episode.ContentIdentity) || !analysisOpaque(episode.AlgorithmProfile, 512) {
		return ErrInvalidInput
	}
	if episode.Status == introskipper.NoResult {
		if episode.Candidate != nil || len(episode.Reasons) != 1 || episode.Reasons[0] != introskipper.NoRepeatedInterval {
			return ErrInvalidInput
		}
		return nil
	}
	if episode.Status != introskipper.Qualified || len(episode.Reasons) != 0 || episode.Candidate == nil {
		return ErrInvalidInput
	}
	candidate := episode.Candidate
	if candidate.UpstreamCommit != introskipper.UpstreamCommit || len(candidate.Support) != 2 ||
		!analysisIntroSkipperInterval(candidate.Interval, episode.DurationTicks) {
		return ErrInvalidInput
	}
	episodes, sources, contents := map[string]bool{}, map[string]bool{}, map[string]bool{}
	own := 0
	for _, support := range candidate.Support {
		if !analysisOpaque(support.EpisodeKey, 512) || !analysisOpaque(support.SourceKey, 256) || !analysisSHA(support.ContentIdentity) ||
			support.AlgorithmProfile != episode.AlgorithmProfile || !analysisIntroSkipperInterval(support.Interval, 600*media.TicksPerSecond) ||
			episodes[support.EpisodeKey] || sources[support.SourceKey] || contents[support.ContentIdentity] {
			return ErrInvalidInput
		}
		episodes[support.EpisodeKey], sources[support.SourceKey], contents[support.ContentIdentity] = true, true, true
		if support.SourceKey == episode.SourceKey {
			if support.EpisodeKey != episode.EpisodeKey || support.ContentIdentity != episode.ContentIdentity || support.Interval != candidate.Interval {
				return ErrInvalidInput
			}
			own++
		}
	}
	if own != 1 {
		return ErrInvalidInput
	}
	return nil
}

func normalizeAnalysisIntroSkipperEpisode(episode introskipper.EpisodeResult) introskipper.EpisodeResult {
	episode.Reasons = append([]string{}, episode.Reasons...)
	if episode.Candidate != nil {
		candidate := *episode.Candidate
		candidate.Support = append([]introskipper.Support{}, candidate.Support...)
		episode.Candidate = &candidate
	}
	return episode
}

func analysisIntroSkipperResultFacts(value AnalysisStoredIntroSkipperResult) AnalysisStoredResultFacts {
	episode := value.Episode
	facts := AnalysisStoredResultFacts{Version: value.Version, Reason: value.Reason,
		Episode: AnalysisStoredEpisodeFacts{EpisodeKey: episode.EpisodeKey, SourceKey: episode.SourceKey,
			ContentIdentity: episode.ContentIdentity, Status: episode.Status, Reasons: episode.Reasons,
			Candidates: []AnalysisStoredCandidateFacts{}}}
	if episode.Candidate != nil {
		candidate := episode.Candidate
		fact := AnalysisStoredCandidateFacts{Interval: AnalysisStoredIntervalFacts(candidate.Interval),
			Status: episode.Status, Reasons: []string{}, Support: []AnalysisStoredSupportFacts{}}
		for _, support := range candidate.Support {
			fact.Support = append(fact.Support, AnalysisStoredSupportFacts{EpisodeKey: support.EpisodeKey,
				SourceKey: support.SourceKey, ContentIdentity: support.ContentIdentity, Interval: AnalysisStoredIntervalFacts(support.Interval)})
		}
		facts.Episode.Candidates = append(facts.Episode.Candidates, fact)
	}
	return facts
}

func validateAnalysisIntroSkipperResultForWork(work AnalysisWork, result introskipper.Result) (map[string]AnalysisStoredIntroSkipperResult, error) {
	if work.TaskKey != TaskIntroAnalysisKey || !work.Execution.Available || ValidateAnalysisExecutionProfile(work.Execution) != nil ||
		work.Execution.DetectorVersion != introskipper.Version || result.Version != introskipper.Version || result.CohortKey != work.ScopeKey ||
		result.Options != work.Execution.IntroSkipperOptions || result.Options != work.Profile.IntroSkipper ||
		len(result.Episodes) != len(work.Sources) || len(work.Sources) < 2 || len(work.Sources) > introskipper.MaxEpisodes {
		return nil, ErrInvalidInput
	}
	bySource := make(map[string]introskipper.EpisodeResult, len(result.Episodes))
	for _, episode := range result.Episodes {
		if _, duplicate := bySource[episode.SourceKey]; duplicate {
			return nil, ErrInvalidInput
		}
		bySource[episode.SourceKey] = episode
	}
	cohort := introskipper.Cohort{Key: work.ScopeKey, Episodes: make([]introskipper.Episode, len(work.Sources))}
	for index, source := range work.Sources {
		episode, exists := bySource[source.SourceRevision]
		if !exists || episode.EpisodeKey != source.EpisodeKey || episode.DurationTicks != source.DurationTicks ||
			episode.AlgorithmProfile != work.Execution.IntroProfile {
			return nil, ErrInvalidInput
		}
		cohort.Episodes[index] = introskipper.Episode{EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision,
			ContentIdentity: episode.ContentIdentity, AlgorithmProfile: episode.AlgorithmProfile, DurationTicks: source.DurationTicks}
	}
	values := make(map[string]AnalysisStoredIntroSkipperResult, len(work.Sources))
	for index, source := range work.Sources {
		episode := normalizeAnalysisIntroSkipperEpisode(bySource[source.SourceRevision])
		value := AnalysisStoredIntroSkipperResult{Version: result.Version, Options: result.Options, Episode: episode}
		if validateAnalysisStoredIntroSkipperResult(value) != nil || introskipper.ValidateEpisodeResult(episode, cohort.Episodes[index], cohort, result.Options) != nil {
			return nil, ErrInvalidInput
		}
		values[source.ItemID] = value
	}
	return values, nil
}

// PublishIntroSkipperAnalysis uses the same source and publication fences as
// legacy detections, while validating the native two-source result contract.
func (s *Store) PublishIntroSkipperAnalysis(ctx context.Context, childID string, fence AnalysisFence, result introskipper.Result) error {
	work, err := s.RevalidateAnalysisWork(ctx, childID, fence)
	if err != nil {
		return err
	}
	values, err := validateAnalysisIntroSkipperResultForWork(work, result)
	if err != nil {
		return err
	}
	return s.withAnalysisWork(ctx, childID, fence, func(tx OwnedTx, current AnalysisWork) error {
		return publishAnalysisIntroSkipperResults(tx, current, values)
	})
}

func publishAnalysisIntroSkipperResults(tx OwnedTx, work AnalysisWork, values map[string]AnalysisStoredIntroSkipperResult) error {
	if work.TaskKey != TaskIntroAnalysisKey || ValidateAnalysisExecutionProfile(work.Execution) != nil ||
		work.Execution.Available && work.Execution.DetectorVersion != introskipper.Version {
		return ErrInvalidInput
	}
	records := make(map[string]analysisPublicationResult, len(values))
	for itemID, value := range values {
		if validateAnalysisStoredIntroSkipperResult(value) != nil || value.Options != work.Profile.IntroSkipper ||
			work.Execution.Available && value.Options != work.Execution.IntroSkipperOptions || !work.Execution.Available && value.Reason == "" {
			return ErrInvalidInput
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return ErrInvalidInput
		}
		records[itemID] = analysisPublicationResult{Raw: raw, Facts: analysisIntroSkipperResultFacts(value)}
	}
	return publishAnalysisResultRecords(tx, work, records)
}

// AnalysisDetectionCandidateInterval exposes a candidate's bounds without
// projecting one algorithm's evidence into the other algorithm's metric type.
func AnalysisDetectionCandidateInterval(value AnalysisDetection) (IntroInterval, bool) {
	if value.Candidate != nil && value.IntroSkipperCandidate == nil {
		return IntroInterval{StartTicks: value.Candidate.Interval.StartTicks, EndTicks: value.Candidate.Interval.EndTicks}, true
	}
	if value.IntroSkipperCandidate != nil && value.Candidate == nil {
		return IntroInterval{StartTicks: value.IntroSkipperCandidate.Interval.StartTicks, EndTicks: value.IntroSkipperCandidate.Interval.EndTicks}, true
	}
	return IntroInterval{}, false
}

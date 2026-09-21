package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
)

// These wire types and validation values are frozen from commit 92577f2.
// Field order is part of the v1 admission fingerprint. Never alias current
// matcher structs here or add new fields to historical canonical JSON.
const (
	analysisStoredExecutionVersionV1       = 1
	analysisStoredDetectorVersionV1        = "introdetect-v1"
	analysisStoredTicksPerSecondV1   int64 = 10_000_000
	analysisStoredPreviewProfileV1         = "source-pts-display-preceding-hold-jpeg-v3;geometry=orthogonal-display-sar-v2"
	// Both profiles were persisted by schema-50 v1 binaries. Their original
	// strings remain distinct inputs to the historical canonical fingerprint.
	analysisStoredPreviewOriginalV1 = "source-pts-display-preceding-hold-jpeg-v3"
)

type analysisStoredProfileV1 struct {
	AutoPublishIntros      bool
	PreviewIntervalSeconds int
	PreviewQuality         int
	MaxSourceBytes         int64
	MaxItemRuntimeSeconds  int
	FeatureCacheMaxBytes   int64
}

type analysisStoredOptionsV1 struct {
	MaxEpisodes             int
	MaxAudioSamples         int
	MaxVisualSamples        int
	MaxFeatureBytes         int
	MaxComparisons          int64
	MaxOffsetCandidates     int
	MaxCandidatesPerPair    int
	MaxGroups               int
	WindowTicks             int64
	MinDurationTicks        int64
	MaxDurationTicks        int64
	AutoMinDurationTicks    int64
	OffsetBinTicks          int64
	AudioAlignmentTicks     int64
	MaxAudioGapTicks        int64
	VisualAlignmentTicks    int64
	MaxVisualGapTicks       int64
	BoundaryToleranceTicks  int64
	MinSupport              int
	MaxAudioHamming         int
	MaxVisualHamming        int
	MinAudioAgreement       int
	MinAudioInformation     int
	MinVisualAgreement      int
	MinAudioSimilarity      int
	MinVisualSimilarity     int
	MinVisualContrast       uint16
	MinVisualSamples        int
	MinVisualTransitions    int
	MinVisualChangeCoverage int
	MaxVisualDominance      int
}

// defaultAnalysisStoredOptionsV1 is a starting profile, not a calibrated accuracy claim.
func defaultAnalysisStoredOptionsV1() analysisStoredOptionsV1 {
	return analysisStoredOptionsV1{
		MaxEpisodes: 32, MaxAudioSamples: 6000, MaxVisualSamples: 2400,
		MaxFeatureBytes: 256 << 10, MaxComparisons: 100_000_000,
		MaxOffsetCandidates: 6, MaxCandidatesPerPair: 12, MaxGroups: 128,
		WindowTicks: 600 * analysisStoredTicksPerSecondV1, MinDurationTicks: 15 * analysisStoredTicksPerSecondV1,
		MaxDurationTicks: 180 * analysisStoredTicksPerSecondV1, AutoMinDurationTicks: 30 * analysisStoredTicksPerSecondV1,
		OffsetBinTicks: analysisStoredTicksPerSecondV1 / 2, AudioAlignmentTicks: analysisStoredTicksPerSecondV1 / 3,
		MaxAudioGapTicks: analysisStoredTicksPerSecondV1, VisualAlignmentTicks: analysisStoredTicksPerSecondV1,
		MaxVisualGapTicks: 3 * analysisStoredTicksPerSecondV1, BoundaryToleranceTicks: 3 * analysisStoredTicksPerSecondV1,
		MinSupport: 3, MaxAudioHamming: 6, MaxVisualHamming: 10,
		MinAudioAgreement: 900, MinAudioInformation: 600, MinVisualAgreement: 850,
		MinAudioSimilarity: 850, MinVisualSimilarity: 850,
		MinVisualContrast: 40, MinVisualSamples: 8, MinVisualTransitions: 3,
		MinVisualChangeCoverage: 300, MaxVisualDominance: 600,
	}
}

type analysisStoredIntervalV1 struct {
	StartTicks int64
	EndTicks   int64
}

// analysisStoredMetricsV1 are integer similarities and observed counts, never probabilities.
// Group metrics report the worst supporting pair, except PairCount.
type analysisStoredMetricsV1 struct {
	AudioAgreementPermille       int
	AudioInformativePermille     int
	AudioSimilarityPermille      int
	AudioSamples                 int
	AudioDistinct                int
	VisualAgreementPermille      int
	VisualSimilarityPermille     int
	VisualCoveragePermille       int
	VisualSamples                int
	VisualTransitions            int
	VisualChangeCoveragePermille int
	VisualDominancePermille      int
	BoundaryUncertaintyTicks     int64
	PairCount                    int
}

type analysisStoredSupportV1 struct {
	EpisodeKey      string
	SourceKey       string
	ContentIdentity string
	Interval        analysisStoredIntervalV1
}

type analysisStoredCandidateV1 struct {
	Interval analysisStoredIntervalV1
	GroupID  string
	Status   string
	Reasons  []string
	Metrics  analysisStoredMetricsV1
	Support  []analysisStoredSupportV1
}

type analysisStoredEpisodeV1 struct {
	EpisodeKey      string
	SourceKey       string
	ContentIdentity string
	Status          string
	Reasons         []string
	Candidates      []analysisStoredCandidateV1
}

type analysisStoredExecutionV1 struct {
	Version             int
	Available           bool
	UnavailableReason   string
	FFmpegSHA256        string
	FFprobeSHA256       string
	FingerprintSHA256   string
	DetectorVersion     string
	DetectorOptions     analysisStoredOptionsV1
	VisualIntervalTicks int64
	PreviewProfile      string
	PreviewWidths       []int
	IntroProfile        string
}

type analysisStoredResultV1 struct {
	Version string
	Episode analysisStoredEpisodeV1
	Reason  string
}

func validateAnalysisProfileV1(profile analysisStoredProfileV1) error {
	if profile.PreviewIntervalSeconds < 2 || profile.PreviewIntervalSeconds > 120 ||
		profile.PreviewQuality < 40 || profile.PreviewQuality > 95 ||
		profile.MaxSourceBytes < 1 || profile.MaxSourceBytes > 1<<40 ||
		profile.MaxItemRuntimeSeconds < 1 || profile.MaxItemRuntimeSeconds > 7200 ||
		profile.FeatureCacheMaxBytes < 1<<20 || profile.FeatureCacheMaxBytes > 512<<20 {
		return fmt.Errorf("%w: invalid media analysis profile", ErrInvalidInput)
	}
	return nil
}

func validateAnalysisExecutionProfileV1(value analysisStoredExecutionV1) error {
	if value.Version != analysisStoredExecutionVersionV1 {
		return ErrInvalidInput
	}
	if !value.Available {
		switch value.UnavailableReason {
		case "disabled", "not_configured", "dependencies_unavailable", "cache_unavailable", "unsupported_platform":
		default:
			return ErrInvalidInput
		}
		expected := analysisStoredExecutionV1{Version: analysisStoredExecutionVersionV1, UnavailableReason: value.UnavailableReason}
		if !reflect.DeepEqual(value, expected) {
			return ErrInvalidInput
		}
		return nil
	}
	if value.UnavailableReason != "" || !analysisSHA(value.FFmpegSHA256) || !analysisSHA(value.FFprobeSHA256) {
		return ErrInvalidInput
	}
	if value.IntroProfile != "" {
		if !analysisSHA(value.FingerprintSHA256) || !analysisOpaque(value.IntroProfile, 512) || value.DetectorVersion != analysisStoredDetectorVersionV1 || value.DetectorOptions != defaultAnalysisStoredOptionsV1() || value.VisualIntervalTicks != analysisStoredTicksPerSecondV1/2 || value.PreviewProfile != "" || len(value.PreviewWidths) != 0 {
			return ErrInvalidInput
		}
	} else if value.FingerprintSHA256 != "" || value.DetectorVersion != "" || value.DetectorOptions != (analysisStoredOptionsV1{}) || value.VisualIntervalTicks != 0 ||
		(value.PreviewProfile != analysisStoredPreviewProfileV1 && value.PreviewProfile != analysisStoredPreviewOriginalV1) || !reflect.DeepEqual(value.PreviewWidths, []int{240, 320, 400}) {
		return ErrInvalidInput
	}
	return nil
}

func analysisAbstentionReasonV1(reason string) bool {
	switch reason {
	case "insufficient_cohort", "unsupported_item_type", "unsupported_hierarchy", "source_unavailable", "analysis_limit", "comparison_budget_exceeded", "cancelled":
		return true
	}
	return false
}
func analysisMatcherReasonV1(reason string) bool {
	switch reason {
	case "duplicate_identity", "insufficient_independent_episodes", "missing_audio", "missing_visual", "incompatible_profile", "no_repeated_interval", "low_audio_entropy", "insufficient_audio_coverage", "insufficient_visual_coverage", "low_visual_diversity", "audio_without_visual_confirmation", "insufficient_pairwise_consensus", "weak_audio_evidence", "weak_visual_evidence", "short_interval_requires_review", "competing_intervals", "analysis_boundary", "overlong_repeated_sequence", "candidate_search_limited", "source_timeline_unproven":
		return true
	}
	return false
}
func validateAnalysisReasonsV1(reasons []string) bool {
	if reasons == nil {
		return false
	}
	seen := map[string]bool{}
	for _, reason := range reasons {
		if !analysisMatcherReasonV1(reason) || seen[reason] {
			return false
		}
		seen[reason] = true
	}
	return true
}
func analysisIntervalV1(interval analysisStoredIntervalV1, duration int64) bool {
	return interval.StartTicks >= 0 && interval.EndTicks > interval.StartTicks && interval.EndTicks <= min(duration, 600*analysisStoredTicksPerSecondV1)
}
func validateAnalysisMetricsV1(metrics analysisStoredMetricsV1) bool {
	for _, value := range []int{metrics.AudioAgreementPermille, metrics.AudioInformativePermille, metrics.AudioSimilarityPermille, metrics.VisualAgreementPermille, metrics.VisualSimilarityPermille, metrics.VisualCoveragePermille, metrics.VisualChangeCoveragePermille, metrics.VisualDominancePermille} {
		if value < 0 || value > 1000 {
			return false
		}
	}
	return metrics.AudioSamples >= 0 && metrics.AudioSamples <= 8192 && metrics.AudioDistinct >= 0 && metrics.AudioDistinct <= 8192 && metrics.VisualSamples >= 0 && metrics.VisualSamples <= 4096 && metrics.VisualTransitions >= 0 && metrics.VisualTransitions <= 4096 && metrics.PairCount >= 0 && metrics.PairCount <= 496 && metrics.BoundaryUncertaintyTicks >= 0 && metrics.BoundaryUncertaintyTicks <= 30*analysisStoredTicksPerSecondV1
}
func validateAnalysisCandidateV1(candidate analysisStoredCandidateV1) bool {
	options := defaultAnalysisStoredOptionsV1()
	duration := candidate.Interval.EndTicks - candidate.Interval.StartTicks
	if !analysisIntervalV1(candidate.Interval, 600*analysisStoredTicksPerSecondV1) || duration < options.MinDurationTicks || duration > options.MaxDurationTicks || !analysisOpaque(candidate.GroupID, 256) || candidate.Status != "qualified" && candidate.Status != "review" || !validateAnalysisReasonsV1(candidate.Reasons) || !validateAnalysisMetricsV1(candidate.Metrics) || len(candidate.Support) < options.MinSupport || len(candidate.Support) > 32 || candidate.Metrics.AudioDistinct < 12 || candidate.Metrics.AudioSamples < candidate.Metrics.AudioDistinct || candidate.Metrics.PairCount != len(candidate.Support)*(len(candidate.Support)-1)/2 {
		return false
	}
	episodes, sources, contents := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, reason := range candidate.Reasons {
		if reason == "source_timeline_unproven" {
			return false
		}
	}
	for _, support := range candidate.Support {
		span := support.Interval.EndTicks - support.Interval.StartTicks
		if !analysisOpaque(support.EpisodeKey, 512) || !analysisOpaque(support.SourceKey, 256) || !analysisSHA(support.ContentIdentity) || !analysisIntervalV1(support.Interval, 600*analysisStoredTicksPerSecondV1) || span < options.MinDurationTicks || span > options.MaxDurationTicks || (candidate.Status == "qualified" && span < options.AutoMinDurationTicks) || episodes[support.EpisodeKey] || sources[support.SourceKey] || contents[support.ContentIdentity] {
			return false
		}
		episodes[support.EpisodeKey] = true
		sources[support.SourceKey] = true
		contents[support.ContentIdentity] = true
	}
	if candidate.Status == "qualified" {
		m := candidate.Metrics
		if len(candidate.Reasons) != 0 || duration < options.AutoMinDurationTicks || m.AudioAgreementPermille < options.MinAudioAgreement || m.AudioInformativePermille < options.MinAudioInformation || m.AudioSimilarityPermille < options.MinAudioSimilarity || m.VisualAgreementPermille < options.MinVisualAgreement || m.VisualSimilarityPermille < options.MinVisualSimilarity || m.VisualCoveragePermille < 700 || m.VisualChangeCoveragePermille < options.MinVisualChangeCoverage || m.VisualDominancePermille > options.MaxVisualDominance || m.VisualSamples < options.MinVisualSamples || m.VisualTransitions < options.MinVisualTransitions {
			return false
		}
	}
	return true
}

func validateAnalysisStoredResultV1(value analysisStoredResultV1) error {
	episode := value.Episode
	if value.Version != analysisStoredDetectorVersionV1 || !analysisOpaque(episode.SourceKey, 256) || (episode.EpisodeKey != "" && !analysisOpaque(episode.EpisodeKey, 512)) || !validateAnalysisReasonsV1(episode.Reasons) || episode.Candidates == nil || len(episode.Candidates) > 128 {
		return ErrInvalidInput
	}
	if value.Reason != "" {
		if !analysisAbstentionReasonV1(value.Reason) || episode.Status != "no_result" || len(episode.Candidates) != 0 || len(episode.Reasons) != 0 || episode.ContentIdentity != "" {
			return ErrInvalidInput
		}
		return nil
	}
	if !analysisOpaque(episode.EpisodeKey, 512) || !analysisSHA(episode.ContentIdentity) {
		return ErrInvalidInput
	}
	switch episode.Status {
	case "qualified":
		if len(episode.Candidates) != 1 || len(episode.Reasons) != 0 || episode.Candidates[0].Status != "qualified" {
			return ErrInvalidInput
		}
	case "review":
		if len(episode.Candidates) == 0 {
			return ErrInvalidInput
		}
	case "no_result":
		if len(episode.Candidates) != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	for _, reason := range episode.Reasons {
		if reason == "source_timeline_unproven" && (episode.Status != "no_result" || len(episode.Candidates) != 0) {
			return ErrInvalidInput
		}
	}
	groups := map[string]bool{}
	for _, candidate := range episode.Candidates {
		if !validateAnalysisCandidateV1(candidate) || groups[candidate.GroupID] {
			return ErrInvalidInput
		}
		groups[candidate.GroupID] = true
		found := false
		for _, support := range candidate.Support {
			if support.SourceKey == episode.SourceKey {
				found = support.EpisodeKey == episode.EpisodeKey && support.ContentIdentity == episode.ContentIdentity && support.Interval == candidate.Interval
			}
		}
		if !found {
			return ErrInvalidInput
		}
	}
	return nil
}

func analysisAdmissionFingerprintV1(profile analysisStoredProfileV1, execution analysisStoredExecutionV1, revision, epoch int64) string {
	raw, _ := json.Marshal(struct {
		Version         int
		Revision, Epoch int64
		Profile         analysisStoredProfileV1
		Execution       analysisStoredExecutionV1
	}{analysisStoredExecutionVersionV1, revision, epoch, profile, execution})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

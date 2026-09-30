package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
)

// These wire types and validation values are frozen from commit cfa12ff1d02dfe4ae3d3bd910aaadedeff16cbf7.
// Field order is part of the v3 admission fingerprint. Never alias current
// matcher structs here or add new fields to historical canonical JSON.
const (
	analysisStoredExecutionVersionV3       = 3
	analysisStoredDetectorVersionV3        = "introdetect-v3"
	analysisStoredTicksPerSecondV3   int64 = 10_000_000
	analysisStoredPreviewProfileV3         = "source-pts-display-preceding-hold-jpeg-v3;geometry=orthogonal-display-sar-v2"
)

type analysisStoredProfileV3 struct {
	AutoPublishIntros      bool
	PreviewIntervalSeconds int
	PreviewQuality         int
	MaxSourceBytes         int64
	MaxItemRuntimeSeconds  int
	FeatureCacheMaxBytes   int64
}

type analysisStoredOptionsV3 struct {
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
	// The three legacy fields above describe the retained v1 diagnostics.
	// They do not determine v3 eligibility.
	VisualBandTicks                 int64
	MinVisualBandMatchedPermille    int
	MinVisualAnchorTicks            int64
	MaxVisualUnconfirmedGapTicks    int64
	MaxVisualAnchorEdgeGapTicks     int64
	VisualStateRadius               int
	MinVisualStates                 int
	MinVisualStateAnchorTicks       int64
	MaxVisualStateDominancePermille int
}

// defaultAnalysisStoredOptionsV3 is a starting profile, not a calibrated accuracy claim.
func defaultAnalysisStoredOptionsV3() analysisStoredOptionsV3 {
	return analysisStoredOptionsV3{
		MaxEpisodes: 32, MaxAudioSamples: 6000, MaxVisualSamples: 2400,
		MaxFeatureBytes: 256 << 10, MaxComparisons: 100_000_000,
		MaxOffsetCandidates: 6, MaxCandidatesPerPair: 12, MaxGroups: 128,
		WindowTicks: 600 * analysisStoredTicksPerSecondV3, MinDurationTicks: 15 * analysisStoredTicksPerSecondV3,
		MaxDurationTicks: 180 * analysisStoredTicksPerSecondV3, AutoMinDurationTicks: 30 * analysisStoredTicksPerSecondV3,
		OffsetBinTicks: analysisStoredTicksPerSecondV3 / 2, AudioAlignmentTicks: analysisStoredTicksPerSecondV3 / 3,
		MaxAudioGapTicks: analysisStoredTicksPerSecondV3, VisualAlignmentTicks: analysisStoredTicksPerSecondV3,
		MaxVisualGapTicks: 3 * analysisStoredTicksPerSecondV3, BoundaryToleranceTicks: 3 * analysisStoredTicksPerSecondV3,
		MinSupport: 3, MaxAudioHamming: 6, MaxVisualHamming: 24,
		MinAudioAgreement: 900, MinAudioInformation: 600, MinVisualAgreement: 850,
		MinAudioSimilarity: 850, MinVisualSimilarity: 750,
		MinVisualContrast: 40, MinVisualSamples: 8, MinVisualTransitions: 3,
		MinVisualChangeCoverage: 300, MaxVisualDominance: 600,
		VisualBandTicks: 5 * analysisStoredTicksPerSecondV3, MinVisualBandMatchedPermille: 400,
		MinVisualAnchorTicks: analysisStoredTicksPerSecondV3, MaxVisualUnconfirmedGapTicks: 5 * analysisStoredTicksPerSecondV3,
		MaxVisualAnchorEdgeGapTicks: 3 * analysisStoredTicksPerSecondV3, VisualStateRadius: 8,
		MinVisualStates: 4, MinVisualStateAnchorTicks: analysisStoredTicksPerSecondV3, MaxVisualStateDominancePermille: 600,
	}
}

type analysisStoredIntervalV3 struct {
	StartTicks int64
	EndTicks   int64
}

// analysisStoredMetricsV3 are integer similarities and observed counts, never probabilities.
// Group metrics report the worst supporting pair, except PairCount.
type analysisStoredMetricsV3 struct {
	AudioAgreementPermille         int
	AudioInformativePermille       int
	AudioSimilarityPermille        int
	AudioSamples                   int
	AudioDistinct                  int
	VisualAgreementPermille        int
	VisualSimilarityPermille       int
	VisualCoveragePermille         int
	VisualSamples                  int
	VisualTransitions              int
	VisualChangeCoveragePermille   int
	VisualDominancePermille        int
	BoundaryUncertaintyTicks       int64
	PairCount                      int
	VisualAnchorCount              int
	VisualMinBandMatchedPermille   int
	VisualMatchedTimePermille      int
	VisualContradictedTimePermille int
	VisualUnobservableTimePermille int
	VisualMaxUnconfirmedGapTicks   int64
	VisualStartAnchorGapTicks      int64
	VisualEndAnchorGapTicks        int64
	VisualDistinctStates           int
	VisualDominantStatePermille    int
}

type analysisStoredSupportV3 struct {
	EpisodeKey      string
	SourceKey       string
	ContentIdentity string
	Interval        analysisStoredIntervalV3
}

type analysisStoredCandidateV3 struct {
	Interval analysisStoredIntervalV3
	GroupID  string
	Status   string
	Reasons  []string
	Metrics  analysisStoredMetricsV3
	Support  []analysisStoredSupportV3
}

type analysisStoredEpisodeV3 struct {
	EpisodeKey      string
	SourceKey       string
	ContentIdentity string
	Status          string
	Reasons         []string
	Candidates      []analysisStoredCandidateV3
}

type analysisStoredExecutionV3 struct {
	Version             int
	Available           bool
	UnavailableReason   string
	FFmpegSHA256        string
	FFprobeSHA256       string
	FingerprintSHA256   string
	DetectorVersion     string
	DetectorOptions     analysisStoredOptionsV3
	VisualIntervalTicks int64
	PreviewProfile      string
	PreviewWidths       []int
	IntroProfile        string
}

type analysisStoredResultV3 struct {
	Version string
	Episode analysisStoredEpisodeV3
	Reason  string
}

func validateAnalysisProfileV3(profile analysisStoredProfileV3) error {
	if profile.PreviewIntervalSeconds < 2 || profile.PreviewIntervalSeconds > 120 ||
		profile.PreviewQuality < 40 || profile.PreviewQuality > 95 ||
		profile.MaxSourceBytes < 1 || profile.MaxSourceBytes > 1<<40 ||
		profile.MaxItemRuntimeSeconds < 1 || profile.MaxItemRuntimeSeconds > 7200 ||
		profile.FeatureCacheMaxBytes < 1<<20 || profile.FeatureCacheMaxBytes > 512<<20 {
		return fmt.Errorf("%w: invalid media analysis profile", ErrInvalidInput)
	}
	return nil
}

func validateAnalysisExecutionProfileV3(value analysisStoredExecutionV3) error {
	if value.Version != analysisStoredExecutionVersionV3 {
		return ErrInvalidInput
	}
	if !value.Available {
		switch value.UnavailableReason {
		case "disabled", "not_configured", "dependencies_unavailable", "cache_unavailable", "unsupported_platform":
		default:
			return ErrInvalidInput
		}
		expected := analysisStoredExecutionV3{Version: analysisStoredExecutionVersionV3, UnavailableReason: value.UnavailableReason}
		if !reflect.DeepEqual(value, expected) {
			return ErrInvalidInput
		}
		return nil
	}
	if value.UnavailableReason != "" || !analysisSHA(value.FFmpegSHA256) || !analysisSHA(value.FFprobeSHA256) {
		return ErrInvalidInput
	}
	if value.IntroProfile != "" {
		if !analysisSHA(value.FingerprintSHA256) || !analysisOpaque(value.IntroProfile, 512) || value.DetectorVersion != analysisStoredDetectorVersionV3 || value.DetectorOptions != defaultAnalysisStoredOptionsV3() || value.VisualIntervalTicks != analysisStoredTicksPerSecondV3/2 || value.PreviewProfile != "" || len(value.PreviewWidths) != 0 {
			return ErrInvalidInput
		}
	} else if value.FingerprintSHA256 != "" || value.DetectorVersion != "" || value.DetectorOptions != (analysisStoredOptionsV3{}) || value.VisualIntervalTicks != 0 ||
		value.PreviewProfile != analysisStoredPreviewProfileV3 || !reflect.DeepEqual(value.PreviewWidths, []int{240, 320, 400}) {
		return ErrInvalidInput
	}
	return nil
}

func analysisAbstentionReasonV3(reason string) bool {
	switch reason {
	case "insufficient_cohort", "unsupported_item_type", "unsupported_hierarchy", "source_unavailable", "analysis_limit", "comparison_budget_exceeded", "cancelled":
		return true
	}
	return false
}
func analysisMatcherReasonV3(reason string) bool {
	switch reason {
	case "duplicate_identity", "insufficient_independent_episodes", "missing_audio", "missing_visual", "incompatible_profile", "no_repeated_interval", "low_audio_entropy", "insufficient_audio_coverage", "insufficient_visual_coverage", "low_visual_diversity", "audio_without_visual_confirmation", "insufficient_pairwise_consensus", "weak_audio_evidence", "weak_visual_evidence", "short_interval_requires_review", "competing_intervals", "analysis_boundary", "overlong_repeated_sequence", "candidate_search_limited", "source_timeline_unproven", "insufficient_visual_anchors", "periodic_visual_sequence", "inconsistent_time_alignment":
		return true
	}
	return false
}
func validateAnalysisReasonsV3(reasons []string) bool {
	if reasons == nil {
		return false
	}
	seen := map[string]bool{}
	for _, reason := range reasons {
		if !analysisMatcherReasonV3(reason) || seen[reason] {
			return false
		}
		seen[reason] = true
	}
	return true
}
func analysisIntervalV3(interval analysisStoredIntervalV3, duration int64) bool {
	return interval.StartTicks >= 0 && interval.EndTicks > interval.StartTicks && interval.EndTicks <= min(duration, 600*analysisStoredTicksPerSecondV3)
}
func validateAnalysisMetricsV3(m analysisStoredMetricsV3) bool {
	for _, value := range []int{m.AudioAgreementPermille, m.AudioInformativePermille, m.AudioSimilarityPermille,
		m.VisualAgreementPermille, m.VisualSimilarityPermille, m.VisualCoveragePermille,
		m.VisualChangeCoveragePermille, m.VisualDominancePermille, m.VisualMinBandMatchedPermille,
		m.VisualMatchedTimePermille, m.VisualContradictedTimePermille, m.VisualUnobservableTimePermille,
		m.VisualDominantStatePermille} {
		if value < 0 || value > 1000 {
			return false
		}
	}
	for _, value := range []int64{m.VisualMaxUnconfirmedGapTicks, m.VisualStartAnchorGapTicks, m.VisualEndAnchorGapTicks} {
		if value < 0 || value > 600*analysisStoredTicksPerSecondV3 {
			return false
		}
	}
	return m.AudioSamples >= 0 && m.AudioSamples <= 8192 && m.AudioDistinct >= 0 && m.AudioDistinct <= m.AudioSamples &&
		m.VisualSamples >= 0 && m.VisualSamples <= 4096 && m.VisualTransitions >= 0 && m.VisualTransitions <= 4096 &&
		m.VisualAnchorCount >= 0 && m.VisualAnchorCount <= 4096 && m.VisualDistinctStates >= 0 && m.VisualDistinctStates <= 4096 &&
		m.PairCount >= 0 && m.PairCount <= 496 && m.BoundaryUncertaintyTicks >= 0 && m.BoundaryUncertaintyTicks <= 30*analysisStoredTicksPerSecondV3
}

// This is the cfa12ff qualification gate. Historical candidates always use the
// frozen default Options; they never inherit current matching or metric rules.
func qualifiedAnalysisEvidenceV3(m analysisStoredMetricsV3, o analysisStoredOptionsV3) bool {
	return validateAnalysisMetricsV3(m) && m.AudioDistinct >= 12 && m.AudioAgreementPermille >= o.MinAudioAgreement &&
		m.AudioInformativePermille >= o.MinAudioInformation && m.AudioSimilarityPermille >= o.MinAudioSimilarity &&
		m.VisualSamples >= o.MinVisualSamples && m.VisualCoveragePermille >= 700 &&
		m.VisualAgreementPermille >= o.MinVisualAgreement && m.VisualSimilarityPermille >= o.MinVisualSimilarity &&
		m.VisualMatchedTimePermille >= o.MinVisualAgreement && m.VisualMinBandMatchedPermille >= o.MinVisualBandMatchedPermille &&
		m.VisualAnchorCount > 0 && m.VisualMaxUnconfirmedGapTicks <= o.MaxVisualUnconfirmedGapTicks &&
		m.VisualStartAnchorGapTicks <= o.MaxVisualAnchorEdgeGapTicks && m.VisualEndAnchorGapTicks <= o.MaxVisualAnchorEdgeGapTicks &&
		m.VisualDistinctStates >= o.MinVisualStates && m.VisualDominantStatePermille <= o.MaxVisualStateDominancePermille
}

func validateAnalysisCandidateV3(candidate analysisStoredCandidateV3) bool {
	options := defaultAnalysisStoredOptionsV3()
	duration := candidate.Interval.EndTicks - candidate.Interval.StartTicks
	if !analysisIntervalV3(candidate.Interval, 600*analysisStoredTicksPerSecondV3) || duration < options.MinDurationTicks || duration > options.MaxDurationTicks || !analysisOpaque(candidate.GroupID, 256) || candidate.Status != "qualified" && candidate.Status != "review" || !validateAnalysisReasonsV3(candidate.Reasons) || !validateAnalysisMetricsV3(candidate.Metrics) || len(candidate.Support) < options.MinSupport || len(candidate.Support) > 32 || candidate.Metrics.AudioDistinct < 12 || candidate.Metrics.AudioSamples < candidate.Metrics.AudioDistinct || candidate.Metrics.PairCount != len(candidate.Support)*(len(candidate.Support)-1)/2 {
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
		if !analysisOpaque(support.EpisodeKey, 512) || !analysisOpaque(support.SourceKey, 256) || !analysisSHA(support.ContentIdentity) || !analysisIntervalV3(support.Interval, 600*analysisStoredTicksPerSecondV3) || span < options.MinDurationTicks || span > options.MaxDurationTicks || (candidate.Status == "qualified" && span < options.AutoMinDurationTicks) || episodes[support.EpisodeKey] || sources[support.SourceKey] || contents[support.ContentIdentity] {
			return false
		}
		episodes[support.EpisodeKey] = true
		sources[support.SourceKey] = true
		contents[support.ContentIdentity] = true
	}
	if candidate.Status == "qualified" {
		return len(candidate.Reasons) == 0 && duration >= options.AutoMinDurationTicks && qualifiedAnalysisEvidenceV3(candidate.Metrics, options)
	}
	return true
}

func validateAnalysisStoredResultV3(value analysisStoredResultV3) error {
	episode := value.Episode
	if value.Version != analysisStoredDetectorVersionV3 || !analysisOpaque(episode.SourceKey, 256) || (episode.EpisodeKey != "" && !analysisOpaque(episode.EpisodeKey, 512)) || !validateAnalysisReasonsV3(episode.Reasons) || episode.Candidates == nil || len(episode.Candidates) > 128 {
		return ErrInvalidInput
	}
	if value.Reason != "" {
		if !analysisAbstentionReasonV3(value.Reason) || episode.Status != "no_result" || len(episode.Candidates) != 0 || len(episode.Reasons) != 0 || episode.ContentIdentity != "" {
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
		if !validateAnalysisCandidateV3(candidate) || groups[candidate.GroupID] {
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

func analysisAdmissionFingerprintV3(profile analysisStoredProfileV3, execution analysisStoredExecutionV3, revision, epoch int64) string {
	raw, _ := json.Marshal(struct {
		Version         int
		Revision, Epoch int64
		Profile         analysisStoredProfileV3
		Execution       analysisStoredExecutionV3
	}{analysisStoredExecutionVersionV3, revision, epoch, profile, execution})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

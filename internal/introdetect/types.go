// Package introdetect matches repeated audiovisual intervals in bounded,
// caller-authorized episode cohorts. It performs no I/O or media extraction.
package introdetect

import "errors"

const (
	TicksPerSecond int64 = 10_000_000
	Version              = "introdetect-v2"
)

var (
	ErrInvalidInput = errors.New("invalid intro detection input")
	ErrLimit        = errors.New("intro detection resource limit exceeded")
)

// AudioSample describes an extraction-time bin on the original media timeline.
// Its fingerprint can have a wider acoustic support than the bin itself.
type AudioSample struct {
	StartTicks  int64
	EndTicks    int64
	Fingerprint uint32
}

// VisualSample must use actual presentation time, including for VFR sources.
// Contrast is grayscale luminance standard deviation divided by 255, times
// 1000. Hash is a consistently oriented 64-bit perceptual image hash.
type VisualSample struct {
	Ticks    int64
	Hash     uint64
	Contrast uint16
}

// Identities are trusted caller facts, not inferred from names or fingerprints.
// EpisodeKey identifies the episode across encodes. SourceKey binds its indexed
// source revision. ContentIdentity identifies the complete media content, not
// just the analyzed prefix. Any shared identity prevents independent support.
// AlgorithmProfile includes extraction versions, audio normalization/time mapping,
// visual hash construction and sample policy. Unlike identities, its value must
// be equal across members of a match.
type Episode struct {
	EpisodeKey                    string
	SourceKey                     string
	ContentIdentity               string
	AlgorithmProfile              string
	DurationTicks                 int64
	AudioBoundaryUncertaintyTicks int64
	Audio                         []AudioSample
	Visual                        []VisualSample
}

// Cohort is an explicit complete window selected by the caller from one
// series/season. Analyze rejects oversized windows instead of truncating them.
type Cohort struct {
	Key      string
	Episodes []Episode
}

// Options contains bounded search budgets and provisional thresholds. Zero
// Options selects DefaultOptions. Nonzero Options must be a complete valid
// configuration, normally obtained by modifying DefaultOptions.
type Options struct {
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
	// They do not determine v2 eligibility.
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

// DefaultOptions is a starting profile, not a calibrated accuracy claim.
func DefaultOptions() Options {
	return Options{
		MaxEpisodes: 32, MaxAudioSamples: 6000, MaxVisualSamples: 2400,
		MaxFeatureBytes: 256 << 10, MaxComparisons: 100_000_000,
		MaxOffsetCandidates: 6, MaxCandidatesPerPair: 12, MaxGroups: 128,
		WindowTicks: 600 * TicksPerSecond, MinDurationTicks: 15 * TicksPerSecond,
		MaxDurationTicks: 180 * TicksPerSecond, AutoMinDurationTicks: 30 * TicksPerSecond,
		OffsetBinTicks: TicksPerSecond / 2, AudioAlignmentTicks: TicksPerSecond / 3,
		MaxAudioGapTicks: TicksPerSecond, VisualAlignmentTicks: TicksPerSecond,
		MaxVisualGapTicks: 3 * TicksPerSecond, BoundaryToleranceTicks: 3 * TicksPerSecond,
		MinSupport: 3, MaxAudioHamming: 6, MaxVisualHamming: 24,
		MinAudioAgreement: 900, MinAudioInformation: 600, MinVisualAgreement: 850,
		MinAudioSimilarity: 850, MinVisualSimilarity: 750,
		MinVisualContrast: 40, MinVisualSamples: 8, MinVisualTransitions: 3,
		MinVisualChangeCoverage: 300, MaxVisualDominance: 600,
		VisualBandTicks: 5 * TicksPerSecond, MinVisualBandMatchedPermille: 400,
		MinVisualAnchorTicks: TicksPerSecond, MaxVisualUnconfirmedGapTicks: 5 * TicksPerSecond,
		MaxVisualAnchorEdgeGapTicks: 3 * TicksPerSecond, VisualStateRadius: 8,
		MinVisualStates: 4, MinVisualStateAnchorTicks: TicksPerSecond, MaxVisualStateDominancePermille: 600,
	}
}

type Status string

const (
	Qualified Status = "qualified"
	Review    Status = "review"
	NoResult  Status = "no_result"
)

type Reason string

const (
	DuplicateIdentity         Reason = "duplicate_identity"
	InsufficientEpisodes      Reason = "insufficient_independent_episodes"
	MissingAudio              Reason = "missing_audio"
	MissingVisual             Reason = "missing_visual"
	IncompatibleProfile       Reason = "incompatible_profile"
	NoRepeatedInterval        Reason = "no_repeated_interval"
	LowAudioEntropy           Reason = "low_audio_entropy"
	InsufficientAudio         Reason = "insufficient_audio_coverage"
	InsufficientVisual        Reason = "insufficient_visual_coverage"
	LowVisualDiversity        Reason = "low_visual_diversity"
	AudioWithoutVisual        Reason = "audio_without_visual_confirmation"
	InsufficientConsensus     Reason = "insufficient_pairwise_consensus"
	WeakAudioEvidence         Reason = "weak_audio_evidence"
	WeakVisualEvidence        Reason = "weak_visual_evidence"
	ShortInterval             Reason = "short_interval_requires_review"
	CompetingIntervals        Reason = "competing_intervals"
	AnalysisBoundary          Reason = "analysis_boundary"
	OverlongRepeat            Reason = "overlong_repeated_sequence"
	CandidateSearchLimited    Reason = "candidate_search_limited"
	InsufficientVisualAnchors Reason = "insufficient_visual_anchors"
	PeriodicVisualEvidence    Reason = "periodic_visual_sequence"
	InconsistentTimeAlignment Reason = "inconsistent_time_alignment"
)

type Interval struct {
	StartTicks int64
	EndTicks   int64
}

// Metrics are integer similarities and observed counts, never probabilities.
// Group metrics report the worst supporting pair, except PairCount.
type Metrics struct {
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

type Support struct {
	EpisodeKey      string
	SourceKey       string
	ContentIdentity string
	Interval        Interval
}

type Candidate struct {
	Interval Interval
	GroupID  string
	Status   Status
	Reasons  []Reason
	Metrics  Metrics
	Support  []Support
}

type EpisodeResult struct {
	EpisodeKey      string
	SourceKey       string
	ContentIdentity string
	Status          Status
	Reasons         []Reason
	Candidates      []Candidate
}

type Group struct {
	ID               string
	AlgorithmProfile string
	Status           Status
	Reasons          []Reason
	Members          []Support
	Metrics          Metrics
	// Only the in-process matcher needs the chosen global source-clock map.
	// Group IDs bind it; it is not reconstructed from untrusted JSON.
	alignmentOffsets []int64
	phaseAnchors     map[[2]string]int64
	phaseClasses     map[[2]string]string
}

type Result struct {
	Version     string
	CohortKey   string
	Options     Options
	Comparisons int64
	Groups      []Group
	Episodes    []EpisodeResult
}

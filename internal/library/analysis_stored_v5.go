package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
)

// These wire types and validation values are frozen from commit a1e8123.
// Field order is part of the v5 admission fingerprint. Never alias current
// matcher structs here or add new fields to historical canonical JSON.
const (
	analysisStoredExecutionVersionV5       = 5
	analysisStoredDetectorVersionV5        = "introdetect-v5"
	analysisStoredTicksPerSecondV5   int64 = 10_000_000
	analysisStoredPreviewProfileV5         = "source-pts-display-preceding-hold-jpeg-v3;geometry=orthogonal-display-sar-v2"
)

type analysisStoredProfileV5 struct {
	AutoPublishIntros      bool
	PreviewIntervalSeconds int
	PreviewQuality         int
	MaxSourceBytes         int64
	MaxItemRuntimeSeconds  int
	FeatureCacheMaxBytes   int64
}

type analysisStoredOptionsV5 struct {
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
	// They do not determine v4 eligibility.
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

// defaultAnalysisStoredOptionsV5 is a starting profile, not a calibrated accuracy claim.
func defaultAnalysisStoredOptionsV5() analysisStoredOptionsV5 {
	return analysisStoredOptionsV5{
		MaxEpisodes: 32, MaxAudioSamples: 6000, MaxVisualSamples: 2400,
		MaxFeatureBytes: 768 << 10, MaxComparisons: 200_000_000,
		MaxOffsetCandidates: 6, MaxCandidatesPerPair: 12, MaxGroups: 128,
		WindowTicks: 600 * analysisStoredTicksPerSecondV5, MinDurationTicks: 15 * analysisStoredTicksPerSecondV5,
		MaxDurationTicks: 180 * analysisStoredTicksPerSecondV5, AutoMinDurationTicks: 30 * analysisStoredTicksPerSecondV5,
		OffsetBinTicks: analysisStoredTicksPerSecondV5 / 2, AudioAlignmentTicks: analysisStoredTicksPerSecondV5 / 3,
		MaxAudioGapTicks: analysisStoredTicksPerSecondV5, VisualAlignmentTicks: analysisStoredTicksPerSecondV5,
		MaxVisualGapTicks: 3 * analysisStoredTicksPerSecondV5, BoundaryToleranceTicks: 3 * analysisStoredTicksPerSecondV5,
		MinSupport: 3, MaxAudioHamming: 6, MaxVisualHamming: 24,
		MinAudioAgreement: 900, MinAudioInformation: 600, MinVisualAgreement: 850,
		MinAudioSimilarity: 850, MinVisualSimilarity: 750,
		MinVisualContrast: 40, MinVisualSamples: 8, MinVisualTransitions: 3,
		MinVisualChangeCoverage: 300, MaxVisualDominance: 600,
		VisualBandTicks: 5 * analysisStoredTicksPerSecondV5, MinVisualBandMatchedPermille: 400,
		MinVisualAnchorTicks: analysisStoredTicksPerSecondV5, MaxVisualUnconfirmedGapTicks: 5 * analysisStoredTicksPerSecondV5,
		MaxVisualAnchorEdgeGapTicks: 3 * analysisStoredTicksPerSecondV5, VisualStateRadius: 8,
		MinVisualStates: 4, MinVisualStateAnchorTicks: analysisStoredTicksPerSecondV5, MaxVisualStateDominancePermille: 600,
	}
}

type analysisStoredExecutionV5 struct {
	Version             int
	Available           bool
	UnavailableReason   string
	FFmpegSHA256        string
	FFprobeSHA256       string
	FingerprintSHA256   string
	DetectorVersion     string
	DetectorOptions     analysisStoredOptionsV5
	VisualIntervalTicks int64
	PreviewProfile      string
	PreviewWidths       []int
	IntroProfile        string
}

func validateAnalysisProfileV5(profile analysisStoredProfileV5) error {
	if profile.PreviewIntervalSeconds < 2 || profile.PreviewIntervalSeconds > 120 ||
		profile.PreviewQuality < 40 || profile.PreviewQuality > 95 ||
		profile.MaxSourceBytes < 1 || profile.MaxSourceBytes > 1<<40 ||
		profile.MaxItemRuntimeSeconds < 1 || profile.MaxItemRuntimeSeconds > 7200 ||
		profile.FeatureCacheMaxBytes < 1<<20 || profile.FeatureCacheMaxBytes > 512<<20 {
		return fmt.Errorf("%w: invalid media analysis profile", ErrInvalidInput)
	}
	return nil
}

func validateAnalysisExecutionProfileV5(value analysisStoredExecutionV5) error {
	if value.Version != analysisStoredExecutionVersionV5 {
		return ErrInvalidInput
	}
	if !value.Available {
		switch value.UnavailableReason {
		case "disabled", "not_configured", "dependencies_unavailable", "cache_unavailable", "unsupported_platform":
		default:
			return ErrInvalidInput
		}
		expected := analysisStoredExecutionV5{Version: analysisStoredExecutionVersionV5, UnavailableReason: value.UnavailableReason}
		if !reflect.DeepEqual(value, expected) {
			return ErrInvalidInput
		}
		return nil
	}
	if value.UnavailableReason != "" || !analysisSHA(value.FFmpegSHA256) || !analysisSHA(value.FFprobeSHA256) {
		return ErrInvalidInput
	}
	if value.IntroProfile != "" {
		if !analysisSHA(value.FingerprintSHA256) || !analysisOpaque(value.IntroProfile, 512) || value.DetectorVersion != analysisStoredDetectorVersionV5 || value.DetectorOptions != defaultAnalysisStoredOptionsV5() || value.VisualIntervalTicks != analysisStoredTicksPerSecondV5/2 || value.PreviewProfile != "" || len(value.PreviewWidths) != 0 {
			return ErrInvalidInput
		}
	} else if value.FingerprintSHA256 != "" || value.DetectorVersion != "" || value.DetectorOptions != (analysisStoredOptionsV5{}) || value.VisualIntervalTicks != 0 ||
		value.PreviewProfile != analysisStoredPreviewProfileV5 || !reflect.DeepEqual(value.PreviewWidths, []int{240, 320, 400}) {
		return ErrInvalidInput
	}
	return nil
}

func analysisAdmissionFingerprintV5(profile analysisStoredProfileV5, execution analysisStoredExecutionV5, revision, epoch int64) string {
	raw, _ := json.Marshal(struct {
		Version         int
		Revision, Epoch int64
		Profile         analysisStoredProfileV5
		Execution       analysisStoredExecutionV5
	}{analysisStoredExecutionVersionV5, revision, epoch, profile, execution})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// validateAnalysisLegacyResultExecution serves only the retained v5 result
// writer and fixtures. Current task admission and work readers require v6.
func validateAnalysisLegacyResultExecution(value AnalysisExecutionProfile) error {
	if value.IntroSkipperOptions != (AnalysisExecutionProfile{}).IntroSkipperOptions {
		return ErrInvalidInput
	}
	return validateAnalysisExecutionProfileV5(analysisStoredExecutionV5{
		Version: value.Version, Available: value.Available, UnavailableReason: value.UnavailableReason,
		FFmpegSHA256: value.FFmpegSHA256, FFprobeSHA256: value.FFprobeSHA256, FingerprintSHA256: value.FingerprintSHA256,
		DetectorVersion: value.DetectorVersion, DetectorOptions: analysisStoredOptionsV5(value.DetectorOptions),
		VisualIntervalTicks: value.VisualIntervalTicks, PreviewProfile: value.PreviewProfile,
		PreviewWidths: value.PreviewWidths, IntroProfile: value.IntroProfile,
	})
}

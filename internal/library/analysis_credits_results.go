package library

import (
	"context"
	"encoding/json"
	"math"
	"reflect"

	"github.com/moooyo/goby/internal/creditsskipper"
	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/media"
)

const AnalysisCreditsResultVersion = "goby-credits-result-v1"

// CreditsInterval retains every final credits interval and its native analyzer
// provenance. Intervening content is never converted into a credits interval.
type CreditsInterval struct {
	StartTicks int64
	EndTicks   int64
	Source     string
}

// AnalysisCreditsAudioSource retains the original winning pair's independent
// full-source identities and clocks without persisting its fingerprints.
type AnalysisCreditsAudioSource struct {
	EpisodeKey       string
	SourceKey        string
	ContentIdentity  string
	AlgorithmProfile string
	DurationTicks    int64
}

// AnalysisStoredCreditsResult is bounded audit evidence, not worker authority.
// AudioOptions and AudioSources are populated from immutable admitted work by
// the publisher; their presence does not grant a caller access to those sources.
type AnalysisStoredCreditsResult struct {
	Version        string
	ItemID         string
	ItemType       string
	SourceRevision string
	DurationTicks  int64
	Segments       []CreditsInterval
	Audio          *introskipper.EpisodeResult
	AudioOptions   *introskipper.Options
	AudioSources   []AnalysisCreditsAudioSource
	Visual         *creditsskipper.Result
	Reason         string
}

const analysisCreditsLibraryPolicySQL = `SELECT collection_type IN ('movies','tvshows','mixed')
 AND COALESCE((options->>'EnableCreditsDetection')::boolean,false) FROM libraries WHERE id=$1`

func creditsIntervalSource(source string) bool {
	switch creditsskipper.Source(source) {
	case creditsskipper.ChapterSource, creditsskipper.BlackFrameSource, creditsskipper.ChromaprintSource, creditsskipper.CombinedSource:
		return true
	}
	return false
}

func creditsTicks(seconds float64) (int64, bool) {
	value := math.RoundToEven(seconds * float64(media.TicksPerSecond))
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > float64(media.MaxAnalysisDurationTicks) {
		return 0, false
	}
	return int64(value), true
}

func validateStoredCreditsResult(value AnalysisStoredCreditsResult) error {
	if value.Version != AnalysisCreditsResultVersion || !analysisOpaque(value.ItemID, 128) ||
		(value.ItemType != "Movie" && value.ItemType != "Episode") || !analysisOpaque(value.SourceRevision, 256) || value.DurationTicks <= 0 || value.DurationTicks > media.MaxAnalysisDurationTicks ||
		value.Segments == nil || len(value.Segments) > 256 {
		return ErrInvalidInput
	}
	if value.Reason != "" && value.Reason != "no_credits_detected" && !analysisAbstentionReason(value.Reason) {
		return ErrInvalidInput
	}
	if len(value.Segments) != 0 && value.Reason != "" {
		return ErrInvalidInput
	}
	for index, segment := range value.Segments {
		if !creditsIntervalSource(segment.Source) || segment.StartTicks < 0 || segment.EndTicks <= segment.StartTicks || segment.EndTicks > value.DurationTicks ||
			index > 0 && segment.StartTicks < value.Segments[index-1].StartTicks {
			return ErrInvalidInput
		}
	}
	if value.Audio == nil {
		if value.AudioOptions != nil || len(value.AudioSources) != 0 {
			return ErrInvalidInput
		}
	} else {
		if value.AudioOptions == nil || len(value.AudioSources) != 2 || value.Audio.Status != introskipper.Qualified || value.Audio.Reasons == nil ||
			value.Audio.SourceKey != value.SourceRevision || value.Audio.DurationTicks != value.DurationTicks {
			return ErrInvalidInput
		}
		cohort := introskipper.Cohort{Key: "stored-credits-pair", Episodes: make([]introskipper.Episode, len(value.AudioSources))}
		var own introskipper.Episode
		for index, source := range value.AudioSources {
			if !analysisOpaque(source.EpisodeKey, 512) || !analysisOpaque(source.SourceKey, 256) || !analysisSHA(source.ContentIdentity) ||
				!analysisOpaque(source.AlgorithmProfile, 512) || source.DurationTicks <= 0 || source.DurationTicks > media.MaxAnalysisDurationTicks {
				return ErrInvalidInput
			}
			cohort.Episodes[index] = introskipper.Episode{EpisodeKey: source.EpisodeKey, SourceKey: source.SourceKey, ContentIdentity: source.ContentIdentity,
				AlgorithmProfile: source.AlgorithmProfile, DurationTicks: source.DurationTicks}
			if source.SourceKey == value.SourceRevision {
				own = cohort.Episodes[index]
			}
		}
		if introskipper.ValidateCreditsEpisodeResult(*value.Audio, own, cohort, *value.AudioOptions) != nil {
			return ErrInvalidInput
		}
	}
	if value.Visual == nil {
		if value.Audio != nil || len(value.Segments) != 0 || !analysisAbstentionReason(value.Reason) {
			return ErrInvalidInput
		}
		return nil
	}
	if value.Reason != "" && value.Reason != "no_credits_detected" {
		return ErrInvalidInput
	}
	// The complete pass always uses one of its native movie or episode windows.
	duration := float64(value.DurationTicks) / float64(media.TicksPerSecond)
	isMovie := value.ItemType == "Movie"
	if creditsskipper.ValidateStoredResult(*value.Visual, duration, isMovie) != nil || len(value.Visual.Segments) != len(value.Segments) {
		return ErrInvalidInput
	}
	for index, segment := range value.Visual.Segments {
		start, validStart := creditsTicks(segment.Start)
		end, validEnd := creditsTicks(segment.End)
		if !validStart || !validEnd || value.Segments[index] != (CreditsInterval{start, end, string(segment.Source)}) {
			return ErrInvalidInput
		}
	}
	matchedAudio := 0
	for _, raw := range value.Visual.Evidence.RawCandidates {
		if raw.Source != creditsskipper.ChromaprintSource {
			continue
		}
		if value.Audio == nil || value.Audio.Candidate == nil {
			return ErrInvalidInput
		}
		start, validStart := creditsTicks(raw.Start)
		end, validEnd := creditsTicks(raw.End)
		if !validStart || !validEnd || value.Audio.Candidate.Interval != (introskipper.Interval{StartTicks: start, EndTicks: end}) {
			return ErrInvalidInput
		}
		matchedAudio++
	}
	if value.Audio != nil && matchedAudio != 1 || value.Audio == nil && matchedAudio != 0 {
		return ErrInvalidInput
	}
	return nil
}

// ReadStoredCreditsAnalysisResult validates the complete current record and its
// indexed first interval. It is shared with offline database restore checks.
func ReadStoredCreditsAnalysisResult(raw []byte, status string, start, end *int64) (AnalysisStoredCreditsResult, error) {
	var value AnalysisStoredCreditsResult
	if len(raw) == 0 || len(raw) > 131072 || analysisStrictJSON(raw, &value) != nil || validateStoredCreditsResult(value) != nil {
		return value, ErrInvalidInput
	}
	if len(value.Segments) == 0 {
		if status != "no_result" || start != nil || end != nil {
			return value, ErrInvalidInput
		}
	} else if status != "qualified" || start == nil || end == nil || *start != value.Segments[0].StartTicks || *end != value.Segments[0].EndTicks {
		return value, ErrInvalidInput
	}
	return value, nil
}

func ValidateStoredCreditsAnalysisResult(raw []byte, status string, start, end *int64) error {
	_, err := ReadStoredCreditsAnalysisResult(raw, status, start, end)
	return err
}

func prepareCreditsAnalysisResults(work AnalysisWork, values map[string]AnalysisStoredCreditsResult, hashes map[string]string) (map[string]AnalysisStoredCreditsResult, error) {
	if work.TaskKey != TaskCreditsAnalysisKey || ValidateAnalysisExecutionForTask(work.TaskKey, work.Execution) != nil || len(values) > len(work.Sources) {
		return nil, ErrInvalidInput
	}
	if work.Execution.Available && work.Execution.IntroSkipperOptions != work.Profile.IntroSkipper {
		return nil, ErrInvalidInput
	}
	for itemID, hash := range hashes {
		if _, exists := FindAnalysisSource(work, itemID); !exists || !analysisSHA(hash) {
			return nil, ErrInvalidInput
		}
	}
	result := make(map[string]AnalysisStoredCreditsResult, len(values))
	for itemID, original := range values {
		source, exists := FindAnalysisSource(work, itemID)
		if !exists || original.ItemID != itemID || original.SourceRevision != source.SourceRevision || original.DurationTicks != source.DurationTicks ||
			!work.Execution.Available && (!analysisAbstentionReason(original.Reason) || original.Visual != nil || original.Audio != nil) {
			return nil, ErrInvalidInput
		}
		value := original
		if value.ItemType != "" && value.ItemType != source.ItemType {
			return nil, ErrInvalidInput
		}
		value.ItemType = source.ItemType
		if value.Audio != nil {
			// The native matcher represents a qualified result's empty reasons as
			// nil. Canonicalize the publication snapshot without mutating caller
			// evidence; the stored codec still requires an explicit empty array.
			audio := normalizeAnalysisIntroSkipperEpisode(*value.Audio)
			value.Audio = &audio
			if source.EpisodeKey == "" || value.Audio.EpisodeKey != source.EpisodeKey || value.Audio.Candidate == nil ||
				value.Audio.ContentIdentity != hashes[itemID] || value.Audio.AlgorithmProfile != work.Execution.IntroProfile {
				return nil, ErrInvalidInput
			}
			options := work.Profile.IntroSkipper
			value.AudioOptions = &options
			value.AudioSources = []AnalysisCreditsAudioSource{}
			for _, member := range work.Sources {
				for _, support := range value.Audio.Candidate.Support {
					if support.SourceKey != member.SourceRevision {
						continue
					}
					if support.EpisodeKey != member.EpisodeKey || support.ContentIdentity != hashes[member.ItemID] || support.AlgorithmProfile != work.Execution.IntroProfile {
						return nil, ErrInvalidInput
					}
					value.AudioSources = append(value.AudioSources, AnalysisCreditsAudioSource{member.EpisodeKey, member.SourceRevision, support.ContentIdentity, support.AlgorithmProfile, member.DurationTicks})
				}
			}
			if original.AudioOptions != nil && *original.AudioOptions != options || len(original.AudioSources) != 0 && !reflect.DeepEqual(original.AudioSources, value.AudioSources) {
				return nil, ErrInvalidInput
			}
		}
		if value.Visual != nil && value.Visual.Window != creditsskipper.Window(float64(source.DurationTicks)/float64(media.TicksPerSecond), source.ItemType == "Movie") {
			return nil, ErrInvalidInput
		}
		if validateStoredCreditsResult(value) != nil {
			return nil, ErrInvalidInput
		}
		encoded, err := json.Marshal(value)
		if err != nil || len(encoded) > 131072 {
			return nil, ErrInvalidInput
		}
		var snapshot AnalysisStoredCreditsResult
		if analysisStrictJSON(encoded, &snapshot) != nil {
			return nil, ErrInvalidInput
		}
		result[itemID] = snapshot
	}
	for _, source := range work.Sources {
		if _, exists := result[source.ItemID]; source.Target && !exists {
			return nil, ErrInvalidInput
		}
	}
	return result, nil
}

// PublishCreditsAnalysis publishes every selected outcome atomically, after the
// task capability, physical cohort, source revisions, and manual markers pass
// their final fence. Playback reads never call this method.
func (s *Store) PublishCreditsAnalysis(ctx context.Context, childID string, fence AnalysisFence, values map[string]AnalysisStoredCreditsResult, contentHashes map[string]string) error {
	return s.WithCreditsAnalysisPublication(ctx, childID, fence, func(tx OwnedTx, work AnalysisWork) error {
		prepared, err := prepareCreditsAnalysisResults(work, values, contentHashes)
		if err != nil {
			return err
		}
		return publishCreditsAnalysisResults(tx, work, prepared)
	})
}

func publishCreditsAnalysisResults(tx OwnedTx, work AnalysisWork, values map[string]AnalysisStoredCreditsResult) error {
	profileRevision, err := analysisRevision(work.ConfigurationRevision)
	if err != nil || profileRevision < 1 {
		return ErrInvalidInput
	}
	var libraryEnabled bool
	if err := tx.QueryRow(analysisCreditsLibraryPolicySQL+` FOR SHARE`, work.LibraryID).Scan(&libraryEnabled); err != nil {
		return err
	}
	changes := []CatalogChange{}
	for _, source := range work.Sources {
		if !source.Target {
			continue
		}
		value := values[source.ItemID]
		raw, err := json.Marshal(value)
		if err != nil || len(raw) > 131072 {
			return ErrInvalidInput
		}
		status := "no_result"
		var start, end *int64
		if len(value.Segments) != 0 {
			status = "qualified"
			start, end = &value.Segments[0].StartTicks, &value.Segments[0].EndTicks
		}
		if ValidateStoredCreditsAnalysisResult(raw, status, start, end) != nil {
			return ErrInvalidInput
		}
		var previous int64
		if err := tx.QueryRow(`SELECT COALESCE((SELECT revision FROM analysis_credits_detections WHERE item_id=$1),0)`, source.ItemID).Scan(&previous); err != nil {
			return err
		}
		if previous == math.MaxInt64 {
			return ErrAnalysisConflict
		}
		if _, err := tx.Exec(`INSERT INTO analysis_credits_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,child_id,cohort_revision,status,result,start_ticks,end_ticks,auto_published)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(item_id) DO UPDATE SET revision=EXCLUDED.revision,source_revision=EXCLUDED.source_revision,
 profile_fingerprint=EXCLUDED.profile_fingerprint,profile_revision=EXCLUDED.profile_revision,publication_epoch=EXCLUDED.publication_epoch,child_id=EXCLUDED.child_id,
 cohort_revision=EXCLUDED.cohort_revision,status=EXCLUDED.status,result=EXCLUDED.result,start_ticks=EXCLUDED.start_ticks,end_ticks=EXCLUDED.end_ticks,auto_published=EXCLUDED.auto_published,updated_at=clock_timestamp()`,
			source.ItemID, previous+1, source.SourceRevision, work.ConfigurationFingerprint, profileRevision, work.PublicationEpoch, work.ChildID, work.CohortRevision, status, raw, start, end, libraryEnabled && status == "qualified"); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM analysis_credits_detection_sources WHERE item_id=$1`, source.ItemID); err != nil {
			return err
		}
		facts := map[string]string{source.SourceRevision: ""}
		for _, support := range value.AudioSources {
			facts[support.SourceKey] = support.ContentIdentity
		}
		for _, member := range work.Sources {
			content, exists := facts[member.SourceRevision]
			if !exists {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO analysis_credits_detection_sources(item_id,source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
				source.ItemID, member.ItemID, member.LibraryID, member.RootID, member.SourceRevision, member.HierarchyRevision, member.EpisodeKey, content); err != nil {
				return err
			}
		}
		changes = append(changes, CatalogChange{Kind: CatalogUpdated, ItemID: source.ItemID, LibraryID: source.LibraryID})
	}
	return analysisRecordChanges(tx, changes...)
}

package backuppg

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/creditsskipper"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// Credits results preserve admitted source facts. A moved source, revoked
// credential, or disabled library can make publication stale without making its
// historical evidence invalid. This inspection never opens media or sidecars.
func validateAnalysisCreditsState(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if version < 59 {
		return nil
	}
	if tx == nil {
		return ErrDatabase
	}
	var valid bool
	err := tx.QueryRow(ctx, analysisCreditsStateRelationsSQL).Scan(&valid)
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	if !valid {
		return ErrSchema
	}
	return analysisStateRows(ctx, tx, `SELECT detection.status,detection.result,detection.start_ticks,detection.end_ticks,
		to_jsonb(source),profile.profile,profile.execution,COALESCE((SELECT jsonb_agg(to_jsonb(admitted)||
		jsonb_build_object('content_sha256',refs.content_sha256) ORDER BY refs.source_item_id)
		FROM (SELECT * FROM analysis_credits_detection_sources WHERE item_id=detection.item_id ORDER BY source_item_id LIMIT 3) refs
		JOIN analysis_work_sources admitted ON admitted.child_id=detection.child_id AND admitted.item_id=refs.source_item_id),'[]'::jsonb)
		FROM analysis_credits_detections detection JOIN analysis_work work ON work.child_id=detection.child_id
		JOIN analysis_run_profiles profile ON profile.run_id=work.run_id
		JOIN analysis_work_sources source ON source.child_id=detection.child_id AND source.item_id=detection.item_id
		ORDER BY detection.item_id`, func(rows pgx.Rows) error {
		var status string
		var raw, sourceRaw, profileRaw, executionRaw, refs []byte
		var start, end *int64
		if err := rows.Scan(&status, &raw, &start, &end, &sourceRaw, &profileRaw, &executionRaw, &refs); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		var source creditsStateSource
		if json.Unmarshal(sourceRaw, &source) != nil || !validCreditsStateDetection(source, status, raw, start, end, profileRaw, executionRaw, refs) {
			return ErrSchema
		}
		return nil
	})
}

const analysisCreditsStateRelationsSQL = `SELECT
	NOT EXISTS(SELECT 1 FROM analysis_work work JOIN analysis_work_sources source ON source.child_id=work.child_id
		WHERE work.task_key='media.credits_analysis' AND (work.reason<>'' OR source.item_type NOT IN ('Movie','Episode')))
	AND NOT EXISTS(SELECT 1 FROM analysis_credits_detections detection
		LEFT JOIN analysis_work work ON work.child_id=detection.child_id
		LEFT JOIN analysis_run_profiles profile ON profile.run_id=work.run_id
		LEFT JOIN analysis_work_sources source ON source.child_id=work.child_id AND source.item_id=detection.item_id
		CROSS JOIN analysis_settings settings
		WHERE work.child_id IS NULL OR work.task_key<>'media.credits_analysis' OR source.item_id IS NULL OR NOT source.target
		OR detection.source_revision<>source.source_revision OR detection.cohort_revision<>work.cohort_revision
		OR detection.profile_fingerprint<>profile.fingerprint OR detection.profile_revision<>profile.configuration_revision
		OR detection.publication_epoch<>profile.publication_epoch
		OR detection.result->>'Version' IS DISTINCT FROM 'goby-credits-result-v1'
		OR detection.result->>'ItemID' IS DISTINCT FROM detection.item_id
		OR detection.result->>'ItemType' IS DISTINCT FROM source.item_type
		OR detection.result->>'SourceRevision' IS DISTINCT FROM source.source_revision
		OR detection.result->'DurationTicks' IS DISTINCT FROM to_jsonb(source.duration_ticks)
		OR (detection.auto_published AND (profile.execution->'Available' IS DISTINCT FROM 'true'::jsonb
		OR detection.publication_epoch<>settings.publication_epoch OR detection.profile_revision<>settings.revision)))
	AND NOT EXISTS(SELECT 1 FROM analysis_credits_detection_sources evidence
		JOIN analysis_credits_detections detection ON detection.item_id=evidence.item_id
		LEFT JOIN analysis_work_sources source ON source.child_id=detection.child_id AND source.item_id=evidence.source_item_id
		WHERE source.item_id IS NULL OR (evidence.library_id,evidence.root_id,evidence.source_revision,evidence.hierarchy_revision,evidence.episode_key)
		IS DISTINCT FROM (source.library_id,source.root_id,source.source_revision,source.hierarchy_revision,source.episode_key))
	AND NOT EXISTS(SELECT 1 FROM analysis_credits_detection_sources GROUP BY item_id HAVING count(*)>2)`

type creditsStateSource struct {
	ItemID         string `json:"item_id"`
	LibraryID      string `json:"library_id"`
	RootID         string `json:"root_id"`
	SeriesID       string `json:"series_id"`
	SeasonID       string `json:"season_id"`
	EpisodeKey     string `json:"episode_key"`
	ItemType       string `json:"item_type"`
	SourceRevision string `json:"source_revision"`
	ContentSHA256  string `json:"content_sha256"`
	DurationTicks  int64  `json:"duration_ticks"`
}

func validCreditsStateDetection(source creditsStateSource, status string, raw []byte, start, end *int64, profileRaw, executionRaw, refsRaw []byte) bool {
	value, err := library.ReadStoredCreditsAnalysisResult(raw, status, start, end)
	if err != nil || source.ItemID != value.ItemID || source.ItemType != value.ItemType || source.SourceRevision != value.SourceRevision || source.DurationTicks != value.DurationTicks ||
		source.ItemType != "Movie" && source.ItemType != "Episode" {
		return false
	}
	var profile library.AnalysisProfile
	var execution library.AnalysisExecutionProfile
	if json.Unmarshal(profileRaw, &profile) != nil || library.ValidateAnalysisProfile(profile) != nil ||
		json.Unmarshal(executionRaw, &execution) != nil || library.ValidateAnalysisExecutionForTask(library.TaskCreditsAnalysisKey, execution) != nil {
		return false
	}
	if !execution.Available && (value.Visual != nil || value.Audio != nil) {
		return false
	}
	if value.Visual != nil && value.Visual.Window != creditsskipper.Window(float64(source.DurationTicks)/float64(media.TicksPerSecond), source.ItemType == "Movie") {
		return false
	}
	var refs []creditsStateSource
	if len(refsRaw) > 128<<10 || json.Unmarshal(refsRaw, &refs) != nil || len(refs) < 1 || len(refs) > 2 {
		return false
	}
	bySource := make(map[string]creditsStateSource, len(refs))
	for _, reference := range refs {
		if !analysisStateIdentifier(reference.ItemID, 256, false) || !analysisStateIdentifier(reference.SourceRevision, 256, false) || reference.DurationTicks <= 0 || reference.LibraryID != source.LibraryID {
			return false
		}
		if _, duplicate := bySource[reference.SourceRevision]; duplicate {
			return false
		}
		bySource[reference.SourceRevision] = reference
	}
	own, exists := bySource[source.SourceRevision]
	if !exists || own.ItemID != source.ItemID || own.EpisodeKey != source.EpisodeKey || own.DurationTicks != source.DurationTicks {
		return false
	}
	if value.Audio == nil {
		return len(refs) == 1 && own.ContentSHA256 == ""
	}
	if source.ItemType != "Episode" || source.SeriesID == "" || source.SeasonID == "" || source.EpisodeKey != value.Audio.EpisodeKey ||
		value.AudioOptions == nil || *value.AudioOptions != profile.IntroSkipper || *value.AudioOptions != execution.IntroSkipperOptions || len(refs) != len(value.AudioSources) {
		return false
	}
	for _, audio := range value.AudioSources {
		reference, exists := bySource[audio.SourceKey]
		if !exists || reference.EpisodeKey != audio.EpisodeKey || reference.DurationTicks != audio.DurationTicks || reference.ContentSHA256 != audio.ContentIdentity ||
			audio.AlgorithmProfile != execution.IntroProfile || reference.ItemType != "Episode" || reference.SeriesID != source.SeriesID || reference.SeasonID != source.SeasonID {
			return false
		}
	}
	return true
}

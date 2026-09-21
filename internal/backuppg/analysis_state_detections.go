package backuppg

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/library"
)

type analysisStateEvidence struct {
	ItemID         string
	SourceRevision string
	EpisodeKey     string
	ContentSHA256  string
	DurationTicks  int64
}

func validateAnalysisDetectionState(ctx context.Context, tx pgx.Tx) error {
	if err := analysisStateRows(ctx, tx, `SELECT detection.item_id,detection.source_revision,detection.status,detection.result,detection.start_ticks,detection.end_ticks,
		source.episode_key,source.duration_ticks,COALESCE((SELECT jsonb_agg(jsonb_build_object(
		'ItemID',refs.source_item_id,'SourceRevision',refs.source_revision,'EpisodeKey',refs.episode_key,
		'ContentSHA256',refs.content_sha256,'DurationTicks',admitted.duration_ticks) ORDER BY refs.source_item_id)
		FROM (SELECT * FROM analysis_detection_sources WHERE item_id=detection.item_id ORDER BY source_item_id LIMIT 33) refs
		JOIN analysis_work_sources admitted ON admitted.child_id=detection.child_id AND admitted.item_id=refs.source_item_id),'[]'::jsonb)
		FROM analysis_detections detection JOIN analysis_work_sources source
		ON source.child_id=detection.child_id AND source.item_id=detection.item_id ORDER BY detection.item_id`, func(rows pgx.Rows) error {
		var item, source, status, episode string
		var raw, refs []byte
		var start, end *int64
		var duration int64
		if err := rows.Scan(&item, &source, &status, &raw, &start, &end, &episode, &duration, &refs); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		if !validAnalysisStateDetection(item, source, episode, duration, status, raw, start, end, refs) {
			return ErrSchema
		}
		return nil
	}); err != nil {
		return err
	}
	if err := analysisStateRows(ctx, tx, `SELECT source_revision FROM analysis_intro_decisions ORDER BY item_id`, func(rows pgx.Rows) error {
		var source string
		if err := rows.Scan(&source); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		if !analysisStateIdentifier(source, 256, false) {
			return ErrSchema
		}
		return nil
	}); err != nil {
		return err
	}
	return analysisStateRows(ctx, tx, `SELECT item_id,source_revision,profile_fingerprint,action,evidence,actor_id FROM analysis_intro_audit ORDER BY id`, func(rows pgx.Rows) error {
		var item, source, profile, action, actor string
		var raw []byte
		if err := rows.Scan(&item, &source, &profile, &action, &raw, &actor); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		evidence, err := library.ReadStoredAnalysisAudit(raw, action)
		if !analysisStateIdentifier(item, 256, false) || !analysisStateIdentifier(source, 256, false) || !analysisStateIdentifier(actor, 256, true) || profile != "" && !analysisStateDigest(profile) || err != nil {
			return ErrSchema
		}
		if evidence.Result != nil && evidence.Result.Episode.SourceKey != source {
			return ErrSchema
		}
		if evidence.Decision != nil && evidence.Decision.SourceRevision != source {
			return ErrSchema
		}
		return nil
	})
}

func validAnalysisStateDetection(item, source, episode string, duration int64, status string, raw []byte, start, end *int64, refs []byte) bool {
	value, err := library.ReadStoredAnalysisResult(raw, status, start, end)
	if !analysisStateIdentifier(item, 256, false) || !analysisStateIdentifier(source, 256, false) || err != nil {
		return false
	}
	if value.Episode.SourceKey != source || value.Episode.EpisodeKey != episode {
		return false
	}
	var references []analysisStateEvidence
	if len(refs) > 128<<10 || json.Unmarshal(refs, &references) != nil || references == nil || len(references) > 32 {
		return false
	}
	bySource := make(map[string]analysisStateEvidence, len(references))
	for _, reference := range references {
		if !analysisStateIdentifier(reference.ItemID, 256, false) || !analysisStateIdentifier(reference.SourceRevision, 256, false) || !analysisStateDigest(reference.ContentSHA256) || reference.DurationTicks <= 0 {
			return false
		}
		if _, duplicate := bySource[reference.SourceRevision]; duplicate {
			return false
		}
		bySource[reference.SourceRevision] = reference
	}
	expected := map[string]bool{}
	if value.Episode.ContentIdentity != "" {
		reference, exists := bySource[source]
		if !exists || reference.ItemID != item || reference.EpisodeKey != episode || reference.ContentSHA256 != value.Episode.ContentIdentity || reference.DurationTicks != duration {
			return false
		}
		expected[source] = true
	}
	for _, candidate := range value.Episode.Candidates {
		if candidate.Interval.EndTicks > duration {
			return false
		}
		for _, support := range candidate.Support {
			reference, exists := bySource[support.SourceKey]
			if !exists || reference.EpisodeKey != support.EpisodeKey || reference.ContentSHA256 != support.ContentIdentity || support.Interval.EndTicks > reference.DurationTicks {
				return false
			}
			expected[support.SourceKey] = true
		}
	}
	return len(expected) == len(references)
}

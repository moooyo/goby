package backuppg

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/library"
)

// Analysis history retains admission-time identities even after a source,
// hierarchy, credential or device has changed. Raw validation proves the stored
// relationships and bounded payloads; it never opens cached paths, runs media
// tools or grants execution/publication authority to an archived task.
func validateAnalysisState(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if version < 50 {
		return nil
	}
	if tx == nil {
		return ErrDatabase
	}
	var valid bool
	if err := tx.QueryRow(ctx, analysisStateRelationsSQL).Scan(&valid); err != nil {
		return classifyResourceStateError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !valid {
		return ErrSchema
	}
	var profile library.AnalysisProfile
	if err := tx.QueryRow(ctx, `SELECT auto_publish_intros,preview_interval_seconds,preview_quality,max_source_bytes,
		max_item_runtime_seconds,feature_cache_max_bytes FROM analysis_settings WHERE id=1`).Scan(
		&profile.AutoPublishIntros, &profile.PreviewIntervalSeconds, &profile.PreviewQuality, &profile.MaxSourceBytes,
		&profile.MaxItemRuntimeSeconds, &profile.FeatureCacheMaxBytes); err != nil {
		return classifyResourceStateError(ctx, err)
	}
	profileErr := library.ValidateAnalysisProfile(profile)
	if err := ctx.Err(); err != nil {
		return err
	}
	if profileErr != nil {
		return ErrSchema
	}
	for _, validate := range []func(context.Context, pgx.Tx) error{
		validateAnalysisTaskState, validateAnalysisAdmissionState, validateAnalysisFeatureState, validateAnalysisPreviewState, validateAnalysisDetectionState,
	} {
		if err := validate(ctx, tx); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Available intro results bind the exact admitted detector. An unavailable
// envelope identifies only its known result wire version: the nonempty library
// Reason and strict result validator require a no-result with no matcher facts.
const analysisStateRelationsSQL = `SELECT
	(SELECT count(*) FROM analysis_settings)=1
	AND NOT EXISTS(SELECT 1 FROM analysis_run_profiles profile LEFT JOIN task_runs run ON run.id=profile.run_id
		CROSS JOIN analysis_settings settings WHERE run.id IS NULL OR run.task_key NOT IN ('media.intro_analysis','media.preview_generation')
		OR profile.fingerprint<>run.analysis_config_fingerprint OR profile.configuration_revision>settings.revision OR profile.publication_epoch>settings.publication_epoch
		OR (profile.execution->'Available'='true'::jsonb AND (run.task_key='media.intro_analysis') IS DISTINCT FROM (profile.execution->>'IntroProfile'<>'')))
	AND NOT EXISTS(SELECT 1 FROM task_runs run LEFT JOIN analysis_run_profiles profile ON profile.run_id=run.id
		WHERE run.task_key IN ('media.intro_analysis','media.preview_generation') AND profile.run_id IS NULL)
	AND NOT EXISTS(SELECT 1 FROM analysis_work work LEFT JOIN task_run_children child ON child.id=work.child_id
		LEFT JOIN task_runs run ON run.id=work.run_id
		WHERE child.id IS NULL OR run.id IS NULL OR child.run_id<>work.run_id OR child.library_id<>work.library_id
		OR child.analysis_scope_key<>work.scope_key OR run.task_key<>work.task_key
		OR work.scope_key !~ '^analysis:[0-9a-f]{64}$'
		OR child.id<>substring(encode(sha256(convert_to(work.run_id||':'||work.scope_key,'UTF8')),'hex'),1,32)
		OR work.force IS DISTINCT FROM COALESCE((run.analysis_input->>'Force')::boolean,false)
		OR (COALESCE(jsonb_array_length(run.analysis_input->'LibraryIds'),0)>0
		AND NOT (run.analysis_input->'LibraryIds' ? work.library_id)))
	AND NOT EXISTS(SELECT 1 FROM task_run_children child JOIN task_runs run ON run.id=child.run_id
		LEFT JOIN analysis_work work ON work.child_id=child.id
		WHERE (run.task_key IN ('media.intro_analysis','media.preview_generation')) IS DISTINCT FROM (child.analysis_scope_key<>'')
		OR (run.task_key IN ('media.intro_analysis','media.preview_generation') AND work.child_id IS NULL
		AND NOT (child.analysis_scope_key='availability' AND child.library_id='' AND child.state='unavailable')))
	AND NOT EXISTS(SELECT 1 FROM analysis_work_sources source JOIN analysis_work work ON work.child_id=source.child_id
		JOIN task_runs run ON run.id=work.run_id JOIN analysis_run_profiles profile ON profile.run_id=work.run_id
		WHERE source.library_id<>work.library_id OR source.size>(profile.profile->>'MaxSourceBytes')::bigint
		OR (source.target AND COALESCE(jsonb_array_length(run.analysis_input->'ItemIds'),0)>0
		AND NOT (run.analysis_input->'ItemIds' ? source.item_id)))
	AND NOT EXISTS(SELECT 1 FROM analysis_work_sources GROUP BY child_id
		HAVING count(*)>32 OR min(position)<>0 OR max(position)<>count(*)-1)
	AND NOT EXISTS(SELECT 1 FROM analysis_work work LEFT JOIN analysis_work_sources source ON source.child_id=work.child_id
		GROUP BY work.child_id,work.task_key HAVING count(source.item_id)=0 OR count(source.item_id) FILTER(WHERE source.target)=0
		OR (work.task_key='media.preview_generation' AND (count(source.item_id)<>1 OR count(source.item_id) FILTER(WHERE source.target)<>1)))
	AND (SELECT count(*) FROM analysis_feature_cache)<=8192
	AND (SELECT COALESCE(sum(bytes),0) FROM analysis_feature_cache)<=(SELECT feature_cache_max_bytes FROM analysis_settings WHERE id=1)
	AND NOT EXISTS(SELECT 1 FROM analysis_detections detection
		LEFT JOIN analysis_work work ON work.child_id=detection.child_id
		LEFT JOIN analysis_run_profiles profile ON profile.run_id=work.run_id
		LEFT JOIN analysis_work_sources source ON source.child_id=work.child_id AND source.item_id=detection.item_id
		CROSS JOIN analysis_settings settings
		WHERE work.child_id IS NULL OR work.task_key<>'media.intro_analysis' OR source.item_id IS NULL OR NOT source.target
		OR detection.source_revision<>source.source_revision OR detection.cohort_revision<>work.cohort_revision
		OR detection.profile_fingerprint<>profile.fingerprint OR detection.profile_revision<>profile.configuration_revision
		OR detection.publication_epoch<>profile.publication_epoch
		OR detection.result->>'Version' IS DISTINCT FROM CASE
			WHEN profile.execution->'Available'='true'::jsonb THEN profile.execution->>'DetectorVersion'
			WHEN profile.execution->>'Version'='1' THEN 'introdetect-v1'
			WHEN profile.execution->>'Version'='2' THEN 'introdetect-v2'
			WHEN profile.execution->>'Version'='3' THEN 'introdetect-v3' ELSE '' END
		OR (detection.result->>'Reason'='' AND profile.execution->'Available' IS DISTINCT FROM 'true'::jsonb)
		OR (detection.auto_published AND (detection.publication_epoch<>settings.publication_epoch
		OR detection.profile_revision<>settings.revision OR NOT settings.auto_publish_intros)))
	AND NOT EXISTS(SELECT 1 FROM analysis_detection_sources evidence JOIN analysis_detections detection ON detection.item_id=evidence.item_id
		LEFT JOIN analysis_work_sources source ON source.child_id=detection.child_id AND source.item_id=evidence.source_item_id
		WHERE source.item_id IS NULL OR (evidence.library_id,evidence.root_id,evidence.source_revision,evidence.hierarchy_revision,evidence.episode_key)
		IS DISTINCT FROM (source.library_id,source.root_id,source.source_revision,source.hierarchy_revision,source.episode_key))
	AND NOT EXISTS(SELECT 1 FROM analysis_detection_sources GROUP BY item_id HAVING count(*)>32)
	AND NOT EXISTS(SELECT 1 FROM analysis_intro_decisions decision WHERE decision.revision<1
		OR octet_length(decision.source_revision) NOT BETWEEN 1 AND 256)
	AND NOT EXISTS(SELECT 1 FROM analysis_preview_state state LEFT JOIN items item ON item.id=state.item_id
		WHERE item.id IS NULL OR state.revision<1 OR octet_length(state.item_id) NOT BETWEEN 1 AND 256)
	AND NOT EXISTS(SELECT 1 FROM analysis_previews preview
		LEFT JOIN analysis_work work ON work.child_id=preview.child_id
		LEFT JOIN analysis_run_profiles profile ON profile.run_id=work.run_id
		LEFT JOIN analysis_work_sources source ON source.child_id=work.child_id AND source.item_id=preview.item_id
		WHERE work.child_id IS NULL OR work.task_key<>'media.preview_generation' OR source.item_id IS NULL OR NOT source.target
		OR profile.execution->'Available' IS DISTINCT FROM 'true'::jsonb
		OR preview.source_revision<>source.source_revision OR preview.profile_fingerprint<>profile.fingerprint
		OR preview.profile_revision<>profile.configuration_revision OR preview.publication_epoch<>profile.publication_epoch)
	AND NOT EXISTS(SELECT 1 FROM analysis_previews GROUP BY item_id HAVING count(*)<>3
		OR count(DISTINCT (revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,child_id,frame_count,interval_ticks))<>1)`

func analysisStateRows(ctx context.Context, tx pgx.Tx, query string, visit func(pgx.Rows) error) error {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return classifyResourceStateError(ctx, err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		visitErr := visit(rows)
		if err := ctx.Err(); err != nil {
			return err
		}
		if visitErr != nil {
			return visitErr
		}
	}
	return classifyResourceStateError(ctx, rows.Err())
}

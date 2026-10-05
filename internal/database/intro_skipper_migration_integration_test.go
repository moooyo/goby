package database_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/database"
)

func TestIntroSkipperMigrationPreservesHistoricalPublicationAuthority(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			themeOwnersMigrateTo(t, ctx, pool, 53)
			// This migration preserves stored evidence byte-for-byte; full result
			// semantics remain the responsibility of the versioned library reader.
			if _, err := pool.Exec(ctx, `UPDATE analysis_settings SET revision=7,publication_epoch=4,
				preview_quality=81,updated_at='2026-09-01T02:03:04.123456Z';
				INSERT INTO libraries(id,name,collection_type) VALUES('skipper-library','Retained library','tvshows');
				INSERT INTO items(id,library_id,name,sort_name,type)
				VALUES('skipper-item','skipper-library','Retained episode','retained episode','Episode');
				INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,
				publication_epoch,child_id,cohort_revision,status,result,start_ticks,end_ticks,auto_published,updated_at)
				VALUES('skipper-item',3,'retained-source',repeat('b',64),7,4,'retained-child',repeat('c',64),
				'qualified','{"Version":"introdetect-v5","RetainedEvidence":true}',100000000,600000000,true,
				'2026-09-01T02:03:04.123456Z');
				INSERT INTO analysis_intro_decisions(item_id,revision,source_revision,rejected,updated_at)
				VALUES('skipper-item',2,'retained-source',false,'2026-09-01T02:03:04.123456Z');
				INSERT INTO analysis_feature_cache(cache_key,item_id,source_revision,profile_fingerprint,
				content_sha256,algorithm_profile,duration_ticks,payload,bytes,last_used_at)
				VALUES(repeat('a',64),'skipper-item','retained-source',repeat('b',64),repeat('c',64),
				'retained-feature-profile',6000000000,decode('474146420300','hex'),6,
				'2026-09-01T02:03:04.123456Z')`); err != nil {
				t.Fatal("seed schema53 publication and cache witnesses", err)
			}
			history := migrationHistory(t, ctx, pool)
			const retainedQuery = `SELECT jsonb_build_object(
				'settings',(SELECT jsonb_build_object('row',to_jsonb(s)-'intro_skipper_options','xmin',s.xmin::text) FROM analysis_settings s),
				'detections',(SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(d),'xmin',d.xmin::text)) FROM analysis_detections d),
				'decisions',(SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(d),'xmin',d.xmin::text)) FROM analysis_intro_decisions d),
				'cache',(SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(c),'xmin',c.xmin::text)) FROM analysis_feature_cache c),
				'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY name) FROM task_system_events e
					WHERE name NOT IN ('BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested','CreditsAnalysisRequested','SubtitleTimelineGenerationRequested')))::text`
			var before, after string
			if err := pool.QueryRow(ctx, retainedQuery).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if runner == "normal" {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
			} else {
				themeOwnersMigrateTo(t, ctx, pool, 54)
			}
			if err := pool.QueryRow(ctx, retainedQuery).Scan(&after); err != nil || after != before {
				t.Fatalf("migration changed existing settings, publication, evidence, cache, or scheduled rebuilds: %v", err)
			}
			// Schema57 appends one neutral event without scheduling a rebuild or
			// mutating the five event rows included in the historical snapshot.
			var backgroundEvent bool
			if err := pool.QueryRow(ctx, `SELECT count(*)=CASE WHEN EXISTS(SELECT 1 FROM schema_migrations WHERE version=57)
				THEN 1 ELSE 0 END FROM task_system_events WHERE name='BackgroundPreviewGenerationRequested'
				AND sequence=0 AND lifecycle_key='' AND occurred_at IS NOT NULL`).Scan(&backgroundEvent); err != nil || !backgroundEvent {
				t.Fatalf("migration did not append the exact neutral background event: %v", err)
			}
			var waveformEvent bool
			if err := pool.QueryRow(ctx, `SELECT count(*)=CASE WHEN EXISTS(SELECT 1 FROM schema_migrations WHERE version=58)
				THEN 1 ELSE 0 END FROM task_system_events WHERE name='AudioWaveformGenerationRequested'
				AND sequence=0 AND lifecycle_key='' AND occurred_at IS NOT NULL`).Scan(&waveformEvent); err != nil || !waveformEvent {
				t.Fatalf("migration did not append the exact neutral waveform event: %v", err)
			}
			var creditsEvent bool
			if err := pool.QueryRow(ctx, `SELECT count(*)=CASE WHEN EXISTS(SELECT 1 FROM schema_migrations WHERE version=59)
				THEN 1 ELSE 0 END FROM task_system_events WHERE name='CreditsAnalysisRequested'
				AND sequence=0 AND lifecycle_key='' AND occurred_at IS NOT NULL`).Scan(&creditsEvent); err != nil || !creditsEvent {
				t.Fatalf("migration did not append the exact neutral credits event: %v", err)
			}
			var subtitleTimelineEvent bool
			if err := pool.QueryRow(ctx, `SELECT count(*)=CASE WHEN EXISTS(SELECT 1 FROM schema_migrations WHERE version=60)
				THEN 1 ELSE 0 END FROM task_system_events WHERE name='SubtitleTimelineGenerationRequested'
				AND sequence=0 AND lifecycle_key='' AND occurred_at IS NOT NULL`).Scan(&subtitleTimelineEvent); err != nil || !subtitleTimelineEvent {
				t.Fatalf("migration did not append the exact neutral subtitle timeline event: %v", err)
			}
			var retained string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m WHERE version<=53`).Scan(&retained); err != nil || retained != history {
				t.Fatalf("migration rewrote the published schema53 history: %v", err)
			}
			var defaults bool
			if err := pool.QueryRow(ctx, `SELECT intro_skipper_options=
				'{"AnalysisPercent":25,"AnalysisLengthLimit":10,"MinimumIntroDuration":15,"MaximumIntroDuration":120,"MaximumFingerprintPointDifferences":6,"MaximumTimeSkip":3.5,"InvertedIndexShift":2}'::jsonb
				FROM analysis_settings WHERE id=1`).Scan(&defaults); err != nil || !defaults {
				t.Fatalf("migration did not initialize the exact upstream options: %v", err)
			}
			for _, patch := range []string{
				`'{"AnalysisPercent":0}'::jsonb`, `'{"AnalysisLengthLimit":11}'::jsonb`,
				`'{"MinimumIntroDuration":121}'::jsonb`, `'{"MaximumIntroDuration":601}'::jsonb`,
				`'{"MaximumFingerprintPointDifferences":33}'::jsonb`, `'{"MaximumTimeSkip":31}'::jsonb`,
				`'{"InvertedIndexShift":33}'::jsonb`, `'{"Offset":0}'::jsonb`,
				`'{"AnalysisPercent":25.5}'::jsonb`, `'{"MaximumTimeSkip":null}'::jsonb`,
			} {
				_, err := pool.Exec(ctx, `UPDATE analysis_settings SET intro_skipper_options=intro_skipper_options||`+patch)
				var pgError *pgconn.PgError
				if !errors.As(err, &pgError) || pgError.Code != "23514" || pgError.ConstraintName != "analysis_settings_intro_skipper_options_check" {
					t.Fatalf("invalid upstream options escaped their durable constraint: %s: %v", patch, err)
				}
			}
			if _, err := pool.Exec(ctx, `UPDATE analysis_settings SET intro_skipper_options=intro_skipper_options-'MaximumTimeSkip'`); err == nil {
				t.Fatal("missing zero-capable option was accepted as an implicit default")
			}
			if _, err := pool.Exec(ctx, `UPDATE analysis_settings SET intro_skipper_options=intro_skipper_options||
				'{"AnalysisPercent":50,"AnalysisLengthLimit":1,"MinimumIntroDuration":1,"MaximumIntroDuration":600,"MaximumFingerprintPointDifferences":0,"MaximumTimeSkip":0,"InvertedIndexShift":32}'::jsonb`); err != nil {
				t.Fatalf("inclusive upstream option bounds were rejected: %v", err)
			}
		})
	}
}

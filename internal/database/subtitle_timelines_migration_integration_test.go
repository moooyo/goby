package database_test

import (
	"testing"

	"github.com/moooyo/goby/internal/database"
)

func TestSubtitleTimelineMigrationPreservesSchema59AndAddsDisabledPolicy(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			themeOwnersMigrateTo(t, ctx, pool, 59)
			if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type,options) VALUES
				('timeline-old-movie','Retained movie','movies','{"EnableLocalMetadata":false,"EnableLocalImages":true,"EnableEmbeddedArtwork":false,"EnableAudioWaveformGeneration":true}'),
				('timeline-old-tv','Retained TV','tvshows','{"EnableLocalMetadata":true,"EnableLocalImages":false,"EnableEmbeddedArtwork":true,"EnableCreditsDetection":true}');
				INSERT INTO items(id,library_id,name,sort_name,type) VALUES
				('timeline-retained-item','timeline-old-movie','Retained movie','retained movie','Movie');
				INSERT INTO audio_waveform_queue(item_id) VALUES('timeline-retained-item');
				INSERT INTO audio_waveform_requests(request_id,fingerprint,queued) VALUES('retained-receipt',repeat('a',64),1)`); err != nil {
				t.Fatal(err)
			}
			before := captureDeviceLegacyTables(t, ctx, pool)
			history := migrationHistory(t, ctx, pool)
			var versions string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(id,xmin::text,options) ORDER BY id)::text FROM libraries`).Scan(&versions); err != nil {
				t.Fatal(err)
			}
			if runner == "normal" {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
			} else {
				themeOwnersMigrateTo(t, ctx, pool, 60)
			}
			assertDeviceLegacyTablesPreserved(t, ctx, pool, before)
			var retained, afterVersions string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m WHERE version<=59`).Scan(&retained); err != nil || retained != history {
				t.Fatalf("subtitle timeline migration rewrote published migration history: %v", err)
			}
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(id,xmin::text,options) ORDER BY id)::text FROM libraries`).Scan(&afterVersions); err != nil || versions != afterVersions {
				t.Fatalf("subtitle timeline migration rewrote existing library rows or policy: %v", err)
			}
			var empty bool
			if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM subtitle_timeline_queue)
				AND NOT EXISTS(SELECT 1 FROM subtitle_timeline_requests)
				AND EXISTS(SELECT 1 FROM task_system_events WHERE name='SubtitleTimelineGenerationRequested'
				AND sequence=0 AND lifecycle_key='' AND occurred_at IS NOT NULL)`).Scan(&empty); err != nil || !empty {
				t.Fatalf("migration inferred timeline artifacts or scheduled a rebuild: %v", err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
				('timeline-new-movie','New movie','movies'),('timeline-new-tv','New TV','tvshows'),
				('timeline-new-mixed','New mixed','mixed'),('timeline-new-music','New music','music')`); err != nil {
				t.Fatal(err)
			}
			var defaults bool
			if err := pool.QueryRow(ctx, `SELECT count(*)=4 FROM libraries WHERE id LIKE 'timeline-new-%'
				AND options->'EnableSubtitleTimelineGeneration'='false'::jsonb
				AND options->'EnableIntroDetection'='false'::jsonb AND options->'EnableCreditsDetection'='false'::jsonb
				AND options->'EnablePreviewGeneration'='false'::jsonb AND options->'EnableBackgroundPreviewGeneration'='false'::jsonb
				AND options->'EnableAudioWaveformGeneration'='false'::jsonb`).Scan(&defaults); err != nil || !defaults {
				t.Fatalf("new library automatic generation is not disabled: %v", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE libraries SET options=options||'{"EnableSubtitleTimelineGeneration":true}'::jsonb
				WHERE id IN ('timeline-new-movie','timeline-new-tv','timeline-new-mixed')`); err != nil {
				t.Fatal("supported subtitle timeline library policy was rejected", err)
			}
			for _, statement := range []string{
				`UPDATE libraries SET options=options||'{"EnableSubtitleTimelineGeneration":true}'::jsonb WHERE id='timeline-new-music'`,
				`UPDATE libraries SET options=options||'{"EnableSubtitleTimelineGeneration":null}'::jsonb WHERE id='timeline-new-movie'`,
				`UPDATE libraries SET options=options||'{"EnableSubtitleTimelineGeneration":"true"}'::jsonb WHERE id='timeline-new-tv'`,
				`UPDATE libraries SET options=options||'{"EnableSubtitleTimelineGeneration":1}'::jsonb WHERE id='timeline-new-mixed'`,
				`INSERT INTO subtitle_timeline_queue(item_id,force) VALUES('timeline-retained-item',true)`,
				`INSERT INTO subtitle_timeline_queue(item_id,manual) VALUES('timeline-retained-item',true)`,
				`INSERT INTO subtitle_timeline_queue(item_id,actor_user_id,actor_session_id) VALUES('timeline-retained-item','actor','session')`,
				`INSERT INTO subtitle_timeline_queue(item_id,state) VALUES('timeline-retained-item','running')`,
				`INSERT INTO subtitle_timeline_queue(item_id,completed_revision) VALUES('timeline-retained-item',2)`,
				`INSERT INTO subtitle_timeline_requests(request_id,fingerprint,queued) VALUES('request','invalid',1)`,
			} {
				if _, err := pool.Exec(ctx, statement); err == nil {
					t.Fatalf("invalid subtitle timeline policy or queue authority was accepted: %s", statement)
				}
			}
			if _, err := pool.Exec(ctx, `INSERT INTO subtitle_timeline_queue(item_id,manual,actor_user_id,actor_session_id,force)
				VALUES('timeline-retained-item',true,'actor','session',true)`); err != nil {
				t.Fatal("valid explicit subtitle timeline request was rejected", err)
			}
			complete := migrationHistory(t, ctx, pool)
			// Replay the same target so a historical recovery still compares its
			// complete original prefix, independently of later migrations.
			if runner == "normal" {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
			} else {
				themeOwnersMigrateTo(t, ctx, pool, 60)
			}
			if complete != migrationHistory(t, ctx, pool) {
				t.Fatal("repeat subtitle timeline migration changed history")
			}
		})
	}
}

package database_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

var selectedPhase3MigrationTables = []string{
	"series_episode_rosters", "episode_roster_imports", "expected_episodes",
}

func assertSelectedPhase3MigrationDefaults(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, table := range selectedPhase3MigrationTables {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil || count != 0 {
			t.Errorf("migration populated expected episode table %s: count=%d error=%v", table, count, err)
		}
	}
}

func selectedPhase3Schema45Fixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	selectedPhase2Schema43Fixture(t, ctx, pool)
	themeOwnersMigrateTo(t, ctx, pool, 45)
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 45 {
		t.Fatalf("expected episode baseline version = %d, want 45, error = %v", version, err)
	}
	var laterTables int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_tables
		WHERE schemaname=current_schema() AND tablename=ANY($1::text[])`, selectedPhase3MigrationTables).Scan(&laterTables); err != nil || laterTables != 0 {
		t.Fatalf("schema45 already contains expected episode tables: count=%d error=%v", laterTables, err)
	}
	// A gap in the physical catalog does not establish an imported episode fact.
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path,is_folder,index_number,parent_index_number)
		VALUES('phase3-series','wave-library','wave-root','wave-library','Retained Series','retained series','Series',
		'/not-opened/wave/Retained Series','Retained Series',true,0,0),
		('phase3-season','wave-library','wave-root','phase3-series','Season 1','season 1','Season',
		'/not-opened/wave/Retained Series/Season 1','Retained Series/Season 1',true,1,0),
		('phase3-episode-1','wave-library','wave-root','phase3-season','Episode 1','episode 1','Episode',
		'/not-opened/wave/Retained Series/Season 1/Episode 1.mkv','Retained Series/Season 1/Episode 1.mkv',false,1,1),
		('phase3-episode-3','wave-library','wave-root','phase3-season','Episode 3','episode 3','Episode',
		'/not-opened/wave/Retained Series/Season 1/Episode 3.mkv','Retained Series/Season 1/Episode 3.mkv',false,3,1);
		INSERT INTO media_operations(id,kind,revision,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,
		request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,stream_index,
		parameters,source_snapshot,execution_snapshot,state,progress_stage,processed,total,result_summary,result_hash,created_at,updated_at,started_at,finished_at)
		SELECT repeat(letter,32),'subtitle_ocr',9007199254740993,'wave-movie','wave-library','wave-root',
		'wave-movie','wave-library','wave-root','wave-owner','wave-session','phase3-'||state,decode(repeat(letter,64),'hex'),
		'retained-media-source','retained-source-revision',2,'{"ModelIds":["eng"],"OutputFormat":"srt"}'::jsonb,
		'{"ItemID":"wave-movie","ExactInteger":9007199254740993}'::jsonb,'{"Tool":"retained-ocr","Version":1}'::jsonb,
		state,'retained-stage',2,2,'{"Retained":[null,true,"exact"]}'::jsonb,repeat(letter,64),
		'2025-01-07T00:00:00Z','2025-01-08T00:00:00.123456Z','2025-01-07T00:00:01Z',
		CASE WHEN state='completed' THEN '2025-01-08T00:00:00.123456Z'::timestamptz ELSE NULL END
		FROM (VALUES('d','ready'),('e','completed')) AS fixture(letter,state);
		INSERT INTO media_operation_cues(operation_id,ordinal,original_start_ticks,original_end_ticks,original_text,
		start_ticks,end_ticks,text,included,confidence,warnings,is_forced,is_hearing_impaired)
		VALUES(repeat('d',32),0,10000000,20000000,'Original OCR line',11000000,22000000,'Reviewed OCR line',true,91.25,
		'["low_contrast"]',true,false),
		(repeat('d',32),1,30000000,40000000,'Original excluded line',31000000,42000000,'Reviewed excluded line',false,NULL,
		'[]',false,true)`); err != nil {
		t.Fatalf("seed retained schema45 physical episodes and media operation review: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO item_owned_subtitles(item_id,root_id,stream_index,operation_id,source_revision,active,codec,language,title,
		is_default,is_forced,is_hearing_impaired,content,content_sha256,created_at,updated_at,retired_at)
		VALUES('wave-movie','wave-root',7,repeat('e',32),'retained-owned-source',true,'srt','en','Reviewed caption',
		true,true,false,$1,encode(sha256($1::bytea),'hex'),'2025-01-09T00:00:00Z','2025-01-10T00:00:00.234567Z',NULL),
		('wave-movie','wave-root',8,NULL,'outdated-owned-source',false,'srt','en','Retired caption',
		false,false,true,$1,encode(sha256($1::bytea),'hex'),'2025-01-09T00:00:00Z','2025-01-10T00:00:00.234567Z','2025-01-10T00:00:00.234567Z')`,
		[]byte("1\n00:00:01,100 --> 00:00:02,200\nRetained caption with \\N bytes.\n\n")); err != nil {
		t.Fatalf("seed retained schema45 owned subtitle bytes: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO item_embedded_artwork(item_id,source_revision,probe_version,extraction_version,status,
		stream_index,picture_type,source_hash,mime_type,width,height,content,inspected_at)
		VALUES('wave-movie','embedded-source-v1-'||repeat('1',32),6,1,'ready',3,'Front',
		encode(sha256(decode('R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==','base64')),'hex'),'image/gif',1,1,
		decode('R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==','base64'),'2025-01-11T00:00:00.345678Z');
		INSERT INTO item_embedded_artwork(item_id,source_revision,probe_version,extraction_version,status,inspected_at)
		VALUES('phase2-reset-movie','embedded-source-v1-'||repeat('2',32),6,1,'none','2025-01-11T00:00:01.456789Z')`); err != nil {
		t.Fatalf("seed retained schema45 embedded artwork bytes and absence: %v", err)
	}
}

func TestSelectedPhase3MigrationPreservesSchema45AndLeavesNewStateEmpty(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			selectedPhase3Schema45Fixture(t, ctx, pool)
			legacy := captureDeviceLegacyTables(t, ctx, pool)
			if len(legacy) != 53 {
				t.Fatalf("historical schema45 inventory = %d, want 53", len(legacy))
			}
			for _, table := range legacy {
				switch table.name {
				case "users", "sessions", "items", "item_metadata_state", "item_subtitles", "play_sessions",
					"item_intro_state", "display_preferences", "user_item_data", "artwork_state", "artwork_images",
					"media_operations", "media_operation_cues", "item_owned_subtitles", "item_embedded_artwork":
					if table.snapshot == "[]" {
						t.Fatalf("schema45 preservation fixture left %s empty", table.name)
					}
				}
			}
			// The shared original-column helper pins device history to schema16.
			// Preserve the complete published schema45 history separately as well.
			originalHistory := migrationHistory(t, ctx, pool)
			var completedHistory string
			for attempt := 1; attempt <= 2; attempt++ {
				if runner == "normal" {
					if err := database.Migrate(ctx, pool); err != nil {
						t.Fatalf("expected episode migration attempt %d: %v", attempt, err)
					}
				} else {
					themeOwnersMigrateTo(t, ctx, pool, currentMigrationVersion(t))
				}
				assertDeviceLegacyTablesPreserved(t, ctx, pool, legacy)
				assertSelectedPhase3MigrationDefaults(t, ctx, pool)
				var preservedHistory string
				if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]'::jsonb)::text
					FROM schema_migrations m WHERE version<=45`).Scan(&preservedHistory); err != nil || preservedHistory != originalHistory {
					t.Fatalf("expected episode migration changed published schema45 history: %v", err)
				}
				var version int64
				var tables int
				if err := pool.QueryRow(ctx, `SELECT (SELECT max(version) FROM schema_migrations),
					(SELECT count(*) FROM pg_tables WHERE schemaname=current_schema())`).Scan(&version, &tables); err != nil || version != currentMigrationVersion(t) || tables != currentMigrationTableCount {
					t.Fatalf("expected episode migration inventory: version=%d tables=%d error=%v", version, tables, err)
				}
				history := migrationHistory(t, ctx, pool)
				if attempt == 1 {
					completedHistory = history
				} else if history != completedHistory {
					t.Fatal("repeated expected episode migration changed its complete history")
				}
			}
		})
	}
}

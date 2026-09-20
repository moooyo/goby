package database_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"golang.org/x/crypto/bcrypt"
)

var selectedPhase2MigrationTables = []string{
	"media_operations", "media_operation_cues", "item_owned_subtitles", "item_embedded_artwork",
}

func assertSelectedPhase2MigrationDefaults(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, table := range selectedPhase2MigrationTables {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil || count != 0 {
			t.Errorf("migration populated media processing table %s: count=%d error=%v", table, count, err)
		}
	}
}

func selectedPhase2Schema43Fixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	themeOwnersMigrateTo(t, ctx, pool, 43)
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 43 {
		t.Fatalf("media processing baseline version = %d, want 43, error = %v", version, err)
	}
	var laterTables int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_tables
		WHERE schemaname=current_schema() AND tablename=ANY($1::text[])`, selectedPhase2MigrationTables).Scan(&laterTables); err != nil || laterTables != 0 {
		t.Fatalf("schema43 already contains media processing tables: count=%d error=%v", laterTables, err)
	}
	featureWaveSeed(t, ctx, pool)
	hash, err := bcrypt.GenerateFromPassword([]byte("synthetic-phase2-local-password"), 10)
	if err != nil {
		t.Fatal("prepare the non-production local password witness")
	}
	// The PIN bytes are an opaque storage witness. Identity tests own encryption
	// and decryption; this migration must preserve every stored byte unchanged.
	if result, err := pool.Exec(ctx, `UPDATE users SET local_password_hash=$1,
		profile_pin_ciphertext=decode('47505001'||repeat('91',32),'hex'),
		local_credentials_revision=9007199254740993,local_password_failures=4,
		local_password_blocked_until='2030-01-01T00:00:00Z',configuration_revision=23,
		configuration='{"EnableLocalPassword":true,"IntroSkipMode":"ShowButton","EnableNextEpisodeAutoPlay":false,"AudioLanguagePreference":"eng"}'::jsonb
		WHERE id='wave-owner'`, string(hash)); err != nil || result.RowsAffected() != 1 {
		t.Fatalf("seed established schema43 local credentials: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE item_metadata_state SET
		effective=automatic||'{"Name":"Retained name","Genres":["Retained genre"]}'::jsonb
		WHERE item_id='wave-movie';
		UPDATE sessions SET local_auth=true WHERE id='wave-session';
		INSERT INTO sessions(id,user_id,token_hash,kind,created_at,expires_at,local_auth)
		VALUES('phase2-ordinary-session','wave-peer',decode(repeat('b6',32),'hex'),'emby',
		'2025-01-01T00:00:00Z','2030-01-01T00:00:00Z',false);
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path)
		VALUES('phase2-reset-movie','wave-library','wave-root','wave-library','Reset Film','reset film',
		'Movie','/not-opened/wave/Reset.mkv','Reset.mkv');
		INSERT INTO item_intro_state(item_id,revision,source_revision,start_ticks,end_ticks,provenance,last_edited_by,last_edited_at)
		VALUES('wave-movie',9007199254740995,'retained-media-source',30000000,80000000,'Import','wave-owner','2025-01-03T00:00:00.123456Z'),
		('phase2-reset-movie',9007199254740997,'',NULL,NULL,'',NULL,'2025-01-04T00:00:00.234567Z');
		INSERT INTO display_preferences(user_id,client,preferences_id,preferences,revision,updated_at)
		VALUES('wave-owner','phase2-client','home','{"SortBy":"Name","CustomPrefs":{"Preserve":true}}',29,'2025-01-05T00:00:00Z');
		INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,hide_from_resume,rating,likes,
		remembered_media_source_id,remembered_media_stamp,remembered_audio_stream_index,remembered_subtitle_stream_index)
		VALUES('wave-owner','wave-movie',40000000,3,true,true,8.5,true,'retained-media-source',repeat('c',32),2,1);
		INSERT INTO artwork_state(item_id,revision,managed_types,updated_at)
		VALUES('wave-movie',31,ARRAY['Primary'],'2025-01-06T00:00:00Z');
		INSERT INTO artwork_images(state_id,image_type,image_index,content,mime_type,width,height,source_hash,modified_at)
		SELECT id,'Primary',0,decode('R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==','base64'),
		'image/gif',1,1,encode(sha256(decode('R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==','base64')),'hex'),
		'2025-01-06T00:00:00Z' FROM artwork_state WHERE item_id='wave-movie'`); err != nil {
		t.Fatalf("seed retained schema43 playback, intro, preference, and artwork state: %v", err)
	}
}

func TestSelectedPhase2MigrationPreservesSchema43AndLeavesNewStateEmpty(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			selectedPhase2Schema43Fixture(t, ctx, pool)
			legacy := captureDeviceLegacyTables(t, ctx, pool)
			if len(legacy) != 49 {
				t.Fatalf("historical schema43 inventory = %d, want 49", len(legacy))
			}
			for _, table := range legacy {
				switch table.name {
				case "users", "sessions", "items", "item_metadata_state", "item_subtitles", "play_sessions",
					"item_intro_state", "display_preferences", "user_item_data", "artwork_state", "artwork_images":
					if table.snapshot == "[]" {
						t.Fatalf("schema43 preservation fixture left %s empty", table.name)
					}
				}
			}
			// The shared original-column helper pins device history to schema16.
			// Preserve the complete published schema43 history separately as well.
			originalHistory := migrationHistory(t, ctx, pool)
			var completedHistory string
			for attempt := 1; attempt <= 2; attempt++ {
				if runner == "normal" {
					if err := database.Migrate(ctx, pool); err != nil {
						t.Fatalf("media processing migration attempt %d: %v", attempt, err)
					}
				} else {
					themeOwnersMigrateTo(t, ctx, pool, currentMigrationVersion(t))
				}
				assertDeviceLegacyTablesPreserved(t, ctx, pool, legacy)
				assertSelectedPhase2MigrationDefaults(t, ctx, pool)
				var preservedHistory string
				if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]'::jsonb)::text
					FROM schema_migrations m WHERE version<=43`).Scan(&preservedHistory); err != nil || preservedHistory != originalHistory {
					t.Fatalf("media processing migration changed published schema43 history: %v", err)
				}
				var version int64
				var tables int
				if err := pool.QueryRow(ctx, `SELECT (SELECT max(version) FROM schema_migrations),
					(SELECT count(*) FROM pg_tables WHERE schemaname=current_schema())`).Scan(&version, &tables); err != nil || version != currentMigrationVersion(t) || tables != currentMigrationTableCount {
					t.Fatalf("media processing migration inventory: version=%d tables=%d error=%v", version, tables, err)
				}
				history := migrationHistory(t, ctx, pool)
				if attempt == 1 {
					completedHistory = history
				} else if history != completedHistory {
					t.Fatal("repeated media processing migration changed its complete history")
				}
			}
		})
	}
}

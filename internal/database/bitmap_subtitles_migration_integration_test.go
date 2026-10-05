package database_test

import (
	"testing"

	"github.com/moooyo/goby/internal/database"
)

func TestBitmapSubtitleMigrationPreservesSchema60RowsAndRetiredTextIndexes(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			themeOwnersMigrateTo(t, ctx, pool, 60)
			if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type,options)
				VALUES('bitmap-migration-library','Retained movie','movies','{"EnableLocalMetadata":true,"EnableLocalImages":false,"EnableEmbeddedArtwork":false,"EnableSubtitleTimelineGeneration":true,"EnableAudioWaveformGeneration":true}');
				INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
				VALUES('bitmap-migration-root','bitmap-migration-library','/not-opened/bitmap','/not-opened','bitmap');
				INSERT INTO items(id,library_id,root_id,relative_path,name,sort_name,type)
				VALUES('bitmap-migration-item','bitmap-migration-library','bitmap-migration-root','Film.mkv','Film','film','Movie');
				INSERT INTO item_subtitles(item_id,root_id,stream_index,active,relative_path,file_identity,source_hash,
				file_size,modified_at,change_time_ns,codec,mime_type)
				VALUES('bitmap-migration-item','bitmap-migration-root',3,false,'Film.retired.srt','retired-source',repeat('a',64),100,'2026-01-01T00:00:00Z',9007199254740993,'srt','application/x-subrip'),
				('bitmap-migration-item','bitmap-migration-root',7,true,'Film.en.srt','current-source',repeat('b',64),110,'2026-01-02T00:00:00Z',9007199254740995,'srt','application/x-subrip');
				INSERT INTO subtitle_timeline_queue(item_id,state,completed_revision,error_code)
				VALUES('bitmap-migration-item','cancelled',1,'retained-cancellation');
				INSERT INTO subtitle_timeline_requests(request_id,fingerprint,queued)
				VALUES('retained-bitmap-request',repeat('c',64),1)`); err != nil {
				t.Fatal("seed schema 60 subtitle identity and timeline history", err)
			}
			before := captureDeviceLegacyTables(t, ctx, pool)
			history := migrationHistory(t, ctx, pool)
			if runner == "normal" {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
			} else {
				themeOwnersMigrateTo(t, ctx, pool, 61)
			}
			for _, table := range before {
				if actual := deviceLegacyTableSnapshot(t, ctx, pool, table); actual != table.snapshot {
					t.Errorf("bitmap subtitle migration changed original rows in %s", table.name)
				}
			}
			var retained string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m WHERE version<=60`).Scan(&retained); err != nil || retained != history {
				t.Fatalf("bitmap subtitle migration changed published history: %v", err)
			}
			var empty bool
			if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM item_bitmap_subtitles)`).Scan(&empty); err != nil || !empty {
				t.Fatalf("migration inferred external files or generated new subtitle work: %v", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE item_subtitles SET codec='hdmv_pgs_subtitle' WHERE item_id='bitmap-migration-item' AND stream_index=7`); err == nil {
				t.Fatal("bitmap migration weakened the historical text subtitle codec constraint")
			}
			complete := migrationHistory(t, ctx, pool)
			if runner == "normal" {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
			} else {
				themeOwnersMigrateTo(t, ctx, pool, 61)
			}
			if complete != migrationHistory(t, ctx, pool) {
				t.Fatal("repeat bitmap migration changed history")
			}
		})
	}
}

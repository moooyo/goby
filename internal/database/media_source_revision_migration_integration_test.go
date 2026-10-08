package database_test

import (
	"testing"

	"github.com/moooyo/goby/internal/database"
)

func TestMediaOperationSourceRevisionMigrationPreservesSchema62(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			themeOwnersMigrateTo(t, ctx, pool, 62)
			if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('stamp-old-library','Retained','movies');
				INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path,binding_revision)
				VALUES('stamp-old-root','stamp-old-library','/stamp/old','/stamp','old',7);
				INSERT INTO items(id,library_id,root_id,name,sort_name,type,relative_path,file_identity,file_size,modified_at,media)
				VALUES('stamp-old-item','stamp-old-library','stamp-old-root','Retained','retained','Movie','Film.mkv','source-identity',123,
				'2026-01-02T03:04:05.123456Z','{"DurationTicks":600000000,"Container":"mkv"}');
				INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
				VALUES('stamp-old-folder','stamp-old-library','Retained folder','retained folder','Folder',true)`); err != nil {
				t.Fatal(err)
			}
			before := captureDeviceLegacyTables(t, ctx, pool)
			var legacy string
			if err := pool.QueryRow(ctx, `SELECT 'media-operation-source-v1-' || md5(jsonb_build_array(i.root_id,
				i.relative_path,i.file_identity,i.file_size,extract(epoch FROM i.modified_at),i.media,
				(SELECT r.binding_revision FROM library_roots r WHERE r.id=i.root_id))::text)
				FROM items i WHERE i.id='stamp-old-item'`).Scan(&legacy); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if runner == "normal" {
					if err := database.Migrate(ctx, pool); err != nil {
						t.Fatal(err)
					}
				} else {
					themeOwnersMigrateTo(t, ctx, pool, 63)
				}
				assertDeviceLegacyTablesPreserved(t, ctx, pool, before)
				var cached string
				var binding int64
				if err := pool.QueryRow(ctx, `SELECT media_operation_source_revision,media_operation_source_binding_revision
					FROM items WHERE id='stamp-old-item'`).Scan(&cached, &binding); err != nil || cached != legacy || binding != 7 {
					t.Fatalf("migration changed the v1 stamp or missed existing media: %v", err)
				}
				var empty bool
				if err := pool.QueryRow(ctx, `SELECT media_operation_source_revision IS NULL AND media_operation_source_binding_revision IS NULL
					FROM items WHERE id='stamp-old-folder'`).Scan(&empty); err != nil || !empty {
					t.Fatalf("migration created a media cache for an organizational folder: %v", err)
				}
			}
		})
	}
}

func TestMediaOperationSourceRevisionMigrationRollsBackWithCaller(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersMigrateTo(t, ctx, pool, 62)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := database.RecoveryMigrateTo(ctx, tx, 63); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var absent bool
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM information_schema.columns
		WHERE table_schema=current_schema() AND table_name='items' AND column_name='media_operation_source_revision')
		AND NOT EXISTS(SELECT 1 FROM schema_migrations WHERE version=63)`).Scan(&absent); err != nil || !absent {
		t.Fatalf("rolled-back migration retained source cache state: %v", err)
	}
}

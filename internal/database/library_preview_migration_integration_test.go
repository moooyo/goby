package database_test

import (
	"testing"

	"github.com/moooyo/goby/internal/database"
)

func TestLibraryPreviewMigrationPreservesSchema51AndAddsDisabledPolicy(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			selectedPhase3Schema45Fixture(t, ctx, pool)
			themeOwnersMigrateTo(t, ctx, pool, 51)
			if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type,options) VALUES
				('preview-old-three','Legacy options','movies','{"EnableLocalMetadata":false,"EnableLocalImages":true,"EnableEmbeddedArtwork":false}'),
				('preview-old-four','Retained intro policy','tvshows','{"EnableLocalMetadata":true,"EnableLocalImages":false,"EnableEmbeddedArtwork":true,"EnableIntroDetection":true}')`); err != nil {
				t.Fatal(err)
			}
			before := captureDeviceLegacyTables(t, ctx, pool)
			history := migrationHistory(t, ctx, pool)
			var libraryVersions string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(id,xmin::text,options) ORDER BY id)::text FROM libraries`).Scan(&libraryVersions); err != nil {
				t.Fatal(err)
			}
			if runner == "normal" {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
			} else {
				themeOwnersMigrateTo(t, ctx, pool, 52)
			}
			assertDeviceLegacyTablesPreserved(t, ctx, pool, before)
			var retained, versions string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m WHERE version<=51`).Scan(&retained); err != nil || retained != history {
				t.Fatalf("schema52 changed published migration history: %v", err)
			}
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(id,xmin::text,options) ORDER BY id)::text FROM libraries`).Scan(&versions); err != nil || versions != libraryVersions {
				t.Fatalf("schema52 rewrote prior library rows or policies: %v", err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
				('preview-new-tv','New TV','tvshows'),('preview-new-movie','New movie','movies'),
				('preview-new-mixed','New mixed','mixed'),('preview-new-music','New music','music')`); err != nil {
				t.Fatal(err)
			}
			var defaults bool
			if err := pool.QueryRow(ctx, `SELECT count(*)=4 FROM libraries WHERE id IN
				('preview-new-tv','preview-new-movie','preview-new-mixed','preview-new-music')
				AND options='{"EnableLocalMetadata":true,"EnableLocalImages":true,"EnableEmbeddedArtwork":true,"EnableIntroDetection":false,"EnablePreviewGeneration":false}'::jsonb`).Scan(&defaults); err != nil || !defaults {
				t.Fatalf("new library defaults did not keep both automation switches disabled: %v", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE libraries SET options=options||'{"EnablePreviewGeneration":true}'::jsonb
				WHERE id IN ('preview-new-tv','preview-new-movie','preview-new-mixed')`); err != nil {
				t.Fatal("schema52 rejected supported preview generation policies")
			}
			for _, statement := range []string{
				`UPDATE libraries SET options=options||'{"EnablePreviewGeneration":true}'::jsonb WHERE id='preview-new-music'`,
				`UPDATE libraries SET options=options||'{"EnablePreviewGeneration":null}'::jsonb WHERE id='preview-new-tv'`,
				`UPDATE libraries SET options=options||'{"EnablePreviewGeneration":"true"}'::jsonb WHERE id='preview-new-tv'`,
				`UPDATE libraries SET options=options||'{"EnablePreviewGeneration":1}'::jsonb WHERE id='preview-new-tv'`,
				`UPDATE libraries SET options=options||'{"PreviewInterval":10}'::jsonb WHERE id='preview-new-tv'`,
				`UPDATE libraries SET options=options||'{"EnableIntroDetection":true}'::jsonb WHERE id='preview-new-movie'`,
			} {
				if _, err := pool.Exec(ctx, statement); err == nil {
					t.Fatal("schema52 accepted an unsupported policy or broadened intro detection")
				}
			}
			complete := migrationHistory(t, ctx, pool)
			if err := database.Migrate(ctx, pool); err != nil || complete != migrationHistory(t, ctx, pool) {
				t.Fatalf("repeat migration changed the complete history: %v", err)
			}
		})
	}
}

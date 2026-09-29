package database_test

import (
	"testing"

	"github.com/moooyo/goby/internal/database"
)

func TestLibraryIntroMigrationPreservesSchema50AndAddsDisabledPolicy(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			selectedPhase3Schema45Fixture(t, ctx, pool)
			themeOwnersMigrateTo(t, ctx, pool, 50)
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
				themeOwnersMigrateTo(t, ctx, pool, 51)
			}
			assertDeviceLegacyTablesPreserved(t, ctx, pool, before)
			var retained, versions string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m WHERE version<=50`).Scan(&retained); err != nil || retained != history {
				t.Fatalf("schema51 changed published migration history: %v", err)
			}
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(id,xmin::text,options) ORDER BY id)::text FROM libraries`).Scan(&versions); err != nil || versions != libraryVersions {
				t.Fatalf("schema51 rewrote existing library rows: %v", err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
				('intro-new-tv','New TV','tvshows'),('intro-new-movie','New movie','movies')`); err != nil {
				t.Fatal(err)
			}
			var defaults bool
			if err := pool.QueryRow(ctx, `SELECT count(*)=2 FROM libraries WHERE id IN ('intro-new-tv','intro-new-movie')
				AND options->'EnableIntroDetection'='false'::jsonb`).Scan(&defaults); err != nil || !defaults {
				t.Fatalf("new libraries must have explicit disabled intro policy: %v", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE libraries SET options=options||'{"EnableIntroDetection":true}'::jsonb WHERE id='intro-new-tv'`); err != nil {
				t.Fatal("schema51 rejected a supported TV opt-in")
			}
			for _, statement := range []string{
				`UPDATE libraries SET options=options||'{"EnableIntroDetection":true}'::jsonb WHERE id='intro-new-movie'`,
				`UPDATE libraries SET options=options||'{"EnableIntroDetection":null}'::jsonb WHERE id='intro-new-tv'`,
				`UPDATE libraries SET options=options||'{"EnableIntroDetection":"true"}'::jsonb WHERE id='intro-new-tv'`,
				`UPDATE libraries SET options=options||'{"EnableIntroDetection":1}'::jsonb WHERE id='intro-new-tv'`,
				`UPDATE libraries SET options=options||'{"UnknownIntroOption":true}'::jsonb WHERE id='intro-new-tv'`,
			} {
				if _, err := pool.Exec(ctx, statement); err == nil {
					t.Fatal("schema51 accepted an unsupported intro policy")
				}
			}
			complete := migrationHistory(t, ctx, pool)
			if err := database.Migrate(ctx, pool); err != nil || complete != migrationHistory(t, ctx, pool) {
				t.Fatalf("repeat migration changed the complete history: %v", err)
			}
		})
	}
}

func TestLibraryIntroMigrationNormalizesLegacyGlobalOptOutOnce(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	selectedPhase3Schema45Fixture(t, ctx, pool)
	themeOwnersMigrateTo(t, ctx, pool, 50)
	if _, err := pool.Exec(ctx, `UPDATE analysis_settings SET auto_publish_intros=false,revision=17 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var normalized bool
	if err := pool.QueryRow(ctx, `SELECT auto_publish_intros AND revision=18 FROM analysis_settings WHERE id=1`).Scan(&normalized); err != nil || !normalized {
		t.Fatalf("legacy global opt-out was not retired at a new profile revision: %v", err)
	}
	var first, second string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(settings)::text FROM analysis_settings settings WHERE id=1`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(settings)::text FROM analysis_settings settings WHERE id=1`).Scan(&second); err != nil || first != second {
		t.Fatalf("repeat migration changed the canonical intro profile: %v", err)
	}
}

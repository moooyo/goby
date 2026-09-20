package database_test

import (
	"testing"

	"github.com/moooyo/goby/internal/database"
)

func TestMediaAnalysisMigrationPreservesSchema49AndInitializesOnlyDefaults(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			selectedPhase3Schema45Fixture(t, ctx, pool)
			themeOwnersMigrateTo(t, ctx, pool, 49)
			before := captureDeviceLegacyTables(t, ctx, pool)
			history := migrationHistory(t, ctx, pool)
			if runner == "normal" {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
			} else {
				themeOwnersMigrateTo(t, ctx, pool, 50)
			}
			assertDeviceLegacyTablesPreserved(t, ctx, pool, before)
			var retained string
			if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]'::jsonb)::text FROM schema_migrations m WHERE version<=49`).Scan(&retained); err != nil || retained != history {
				t.Fatalf("schema50 rewrote published history: %v", err)
			}
			var defaults bool
			if err := pool.QueryRow(ctx, `SELECT
   (SELECT count(*)=1 FROM analysis_settings)
   AND EXISTS(SELECT 1 FROM analysis_settings WHERE id=1 AND revision=1 AND publication_epoch=1 AND auto_publish_intros
    AND preview_interval_seconds=10 AND preview_quality=80 AND max_source_bytes=137438953472
    AND max_item_runtime_seconds=1200 AND feature_cache_max_bytes=134217728)
   AND NOT EXISTS(SELECT 1 FROM analysis_run_profiles) AND NOT EXISTS(SELECT 1 FROM analysis_work)
   AND NOT EXISTS(SELECT 1 FROM analysis_work_sources) AND NOT EXISTS(SELECT 1 FROM analysis_feature_cache)
   AND NOT EXISTS(SELECT 1 FROM analysis_detections) AND NOT EXISTS(SELECT 1 FROM analysis_detection_sources)
   AND NOT EXISTS(SELECT 1 FROM analysis_intro_decisions) AND NOT EXISTS(SELECT 1 FROM analysis_intro_audit)
   AND NOT EXISTS(SELECT 1 FROM analysis_preview_state) AND NOT EXISTS(SELECT 1 FROM analysis_previews)
   AND NOT EXISTS(SELECT 1 FROM task_runs WHERE analysis_input IS NOT NULL OR analysis_config_fingerprint<>'' OR actor_application_key_id<>0 OR actor_client_session_id<>'' OR actor_peer_ip<>'')
   AND NOT EXISTS(SELECT 1 FROM task_run_children WHERE analysis_scope_key<>'')`).Scan(&defaults); err != nil || !defaults {
				t.Fatalf("migration invented analysis work or changed defaults: %v", err)
			}
			for _, statement := range []string{
				`UPDATE analysis_settings SET preview_interval_seconds=1`,
				`UPDATE analysis_settings SET preview_quality=96`,
				`UPDATE analysis_settings SET max_source_bytes=1099511627777`,
				`UPDATE analysis_settings SET max_item_runtime_seconds=7201`,
				`UPDATE analysis_settings SET feature_cache_max_bytes=536870913`,
				`UPDATE analysis_settings SET publication_epoch=0`,
			} {
				if _, err := pool.Exec(ctx, statement); err == nil {
					t.Fatal("schema50 accepted an out-of-contract durable profile")
				}
			}
			complete := migrationHistory(t, ctx, pool)
			if err := database.Migrate(ctx, pool); err != nil {
				t.Fatal(err)
			}
			if complete != migrationHistory(t, ctx, pool) {
				t.Fatal("repeat migration altered complete history")
			}
		})
	}
}

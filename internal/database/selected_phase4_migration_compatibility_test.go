package database_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/library"
)

func assertSelectedPhase4MigrationDefaults(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM managed_settings WHERE runtime_overrides IS DISTINCT FROM
		'{"Network":null,"Hardware":null,"Threads":null,"H264":null,"HEVC":null,"SoftwareToneMapping":null,"VulkanToneMapping":null}'::jsonb)
		AND (SELECT count(*) FROM notification_transport)=1
		AND EXISTS(SELECT 1 FROM notification_transport WHERE id=1 AND revision=1 AND NOT enabled AND endpoint=''
			AND allowed_networks='{}'::text[] AND credential_ciphertext IS NULL AND credential_generation=1)
		AND (SELECT count(*) FROM notification_journal_state)=1
		AND EXISTS(SELECT 1 FROM notification_journal_state WHERE id=1 AND sequence=0)
		AND NOT EXISTS(SELECT 1 FROM notification_registrations)
		AND NOT EXISTS(SELECT 1 FROM notification_source_events)
		AND NOT EXISTS(SELECT 1 FROM notification_deliveries)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("migration inferred target runtime choices, notification credentials or work: %v", err)
	}
}

func TestSelectedPhase4MigrationPreservesSchema46AndLeavesNewStateNeutral(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			selectedPhase3Schema45Fixture(t, ctx, pool)
			themeOwnersMigrateTo(t, ctx, pool, 46)
			payload := []byte(`{"ParserVersion":1,"Source":{"Key":"retained-local","Label":"Retained roster","Revision":"v1"},"Entries":[{"Key":"unknown","SeasonNumber":1,"EpisodeNumber":2,"Name":""}]}`)
			_, digest, err := library.ParseEpisodeRosterPayload(payload)
			if err != nil {
				t.Fatal("prepare canonical schema46 provenance")
			}
			if _, err := pool.Exec(ctx, `INSERT INTO series_episode_rosters(series_id,revision,state,source_key,source_label,source_revision,parser_version,payload_sha256,last_edited_by)
			VALUES('phase3-series',9007199254740993,'active','retained-local','Retained roster','v1',1,$1,'wave-owner')`, digest); err != nil {
				t.Fatalf("seed schema46 roster authority: %v", err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO episode_roster_imports(series_id,revision,action,source_key,source_label,source_revision,parser_version,payload,payload_sha256,actor_id)
			VALUES('phase3-series',9007199254740993,'replace','retained-local','Retained roster','v1',1,$1,$2,'wave-owner')`, payload, digest); err != nil {
				t.Fatalf("seed schema46 source history: %v", err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO expected_episodes(id,series_id,source_key,entry_key,season_number,episode_number,name,active,import_revision)
			VALUES($1,'phase3-series','retained-local','unknown',1,2,'',true,9007199254740993)`,
				library.ExpectedEpisodeID("phase3-series", "retained-local", "unknown")); err != nil {
				t.Fatalf("seed schema46 expected episode state: %v", err)
			}
			legacy := captureDeviceLegacyTables(t, ctx, pool)
			if len(legacy) != 56 {
				t.Fatalf("schema46 table inventory=%d, want56", len(legacy))
			}
			history := migrationHistory(t, ctx, pool)
			var completed string
			for attempt := 1; attempt <= 2; attempt++ {
				if runner == "normal" {
					if err := database.Migrate(ctx, pool); err != nil {
						t.Fatal(err)
					}
				} else {
					themeOwnersMigrateTo(t, ctx, pool, currentMigrationVersion(t))
				}
				assertDeviceLegacyTablesPreserved(t, ctx, pool, legacy)
				assertSelectedPhase4MigrationDefaults(t, ctx, pool)
				var retained string
				var tables int
				if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]'::jsonb)::text FROM schema_migrations m WHERE version<=46`).Scan(&retained); err != nil || retained != history {
					t.Fatalf("schema47/48 changed published schema46 history: %v", err)
				}
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname=current_schema()`).Scan(&tables); err != nil || tables != currentMigrationTableCount {
					t.Fatalf("schema48 inventory=%d want%d: %v", tables, currentMigrationTableCount, err)
				}
				now := migrationHistory(t, ctx, pool)
				if attempt == 1 {
					completed = now
				} else if now != completed {
					t.Fatal("repeated phase4 migration changed complete history")
				}
			}
		})
	}
}

package database_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/database"
)

func TestAnalysisRefinementMigrationPreservesSchema52AndExpandsOnlyPayloadCapacity(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			themeOwnersMigrateTo(t, ctx, pool, 52)
			if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type)
				VALUES('refinement-library','Retained library','tvshows');
				INSERT INTO items(id,library_id,name,sort_name,type)
				VALUES('refinement-item','refinement-library','Retained episode','retained episode','Episode');
				INSERT INTO analysis_feature_cache(cache_key,item_id,source_revision,profile_fingerprint,
				content_sha256,algorithm_profile,duration_ticks,payload,bytes,last_used_at)
				VALUES(repeat('a',64),'refinement-item','retained-source',repeat('b',64),repeat('c',64),
				'retained-feature-profile',6000000000,decode(repeat('e7',262144),'hex'),262144,
				'2026-09-01T02:03:04.123456Z')`); err != nil {
				t.Fatal("seed the published schema52 feature-cache witness", err)
			}
			checkPayloadRejected := func(size int, wantConstraint string) {
				t.Helper()
				_, err := pool.Exec(ctx, `UPDATE analysis_feature_cache
					SET payload=decode(repeat('e7',$1::integer),'hex'),bytes=$1::integer`, size)
				var pgError *pgconn.PgError
				if !errors.As(err, &pgError) || pgError.Code != "23514" || pgError.ConstraintName != wantConstraint {
					t.Fatalf("payload size %d did not fail the expected storage constraint: %v", size, err)
				}
			}
			checkPayloadRejected(262145, "analysis_feature_cache_payload_check")
			before := captureDeviceLegacyTables(t, ctx, pool)
			history := migrationHistory(t, ctx, pool)
			var cacheBefore, settingsBefore string
			if err := pool.QueryRow(ctx, `SELECT to_jsonb(c)::text||c.xmin::text FROM analysis_feature_cache c`).Scan(&cacheBefore); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `SELECT to_jsonb(s)::text||s.xmin::text FROM analysis_settings s`).Scan(&settingsBefore); err != nil {
				t.Fatal(err)
			}
			if runner == "normal" {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
			} else {
				themeOwnersMigrateTo(t, ctx, pool, 53)
			}
			assertDeviceLegacyTablesPreserved(t, ctx, pool, before)
			var retained, cacheAfter, settingsAfter string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text
				FROM schema_migrations m WHERE version<=52`).Scan(&retained); err != nil || retained != history {
				t.Fatalf("schema53 changed published migration history: %v", err)
			}
			if err := pool.QueryRow(ctx, `SELECT to_jsonb(c)::text||c.xmin::text FROM analysis_feature_cache c`).Scan(&cacheAfter); err != nil || cacheAfter != cacheBefore {
				t.Fatalf("schema53 rewrote existing cache bytes or metadata: %v", err)
			}
			if err := pool.QueryRow(ctx, `SELECT to_jsonb(s)::text||s.xmin::text FROM analysis_settings s`).Scan(&settingsAfter); err != nil || settingsAfter != settingsBefore {
				t.Fatalf("schema53 changed the aggregate cache budget or configuration: %v", err)
			}
			for _, size := range []int{1, 262145, 513892, 524288} {
				if _, err := pool.Exec(ctx, `UPDATE analysis_feature_cache
					SET payload=decode(repeat('e7',$1::integer),'hex'),bytes=$1::integer`, size); err != nil {
					t.Fatalf("schema53 rejected a bounded %d-byte feature payload: %v", size, err)
				}
			}
			checkPayloadRejected(0, "analysis_feature_cache_payload_check")
			checkPayloadRejected(524289, "analysis_feature_cache_payload_check")
			if _, err := pool.Exec(ctx, `UPDATE analysis_feature_cache SET bytes=bytes-1`); err == nil {
				t.Fatal("schema53 relaxed the exact payload length contract")
			}
			complete := migrationHistory(t, ctx, pool)
			if runner == "normal" {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
			} else {
				themeOwnersMigrateTo(t, ctx, pool, 53)
			}
			if complete != migrationHistory(t, ctx, pool) {
				t.Fatal("repeat migration changed the complete history")
			}
		})
	}
}

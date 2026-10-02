package database_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/database"
)

func TestPlaybackDemandMigrationPreservesHistoricalRowsAndInitializesRevision(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			deviceVersion16Baseline(t, ctx, pool)
			seedDeviceLegacyLogins(t, ctx, pool)
			seedDeviceMigrationHistory(t, ctx, pool)
			themeOwnersMigrateTo(t, ctx, pool, 54)
			const retainedQuery = `SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(p)-'playback_revision','xmin',p.xmin::text) ORDER BY id)::text FROM play_sessions p`
			var before, after string
			if err := pool.QueryRow(ctx, retainedQuery).Scan(&before); err != nil {
				t.Fatal(err)
			}
			history := migrationHistory(t, ctx, pool)
			if runner == "normal" {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
			} else {
				themeOwnersMigrateTo(t, ctx, pool, 55)
			}
			if err := pool.QueryRow(ctx, retainedQuery).Scan(&after); err != nil || after != before {
				t.Fatalf("demand migration rewrote a historical playback row, identity, timestamp or tuple: %v", err)
			}
			var defaults bool
			if err := pool.QueryRow(ctx, `SELECT count(*)=3 AND bool_and(playback_revision=0) FROM play_sessions`).Scan(&defaults); err != nil || !defaults {
				t.Fatalf("historical ordinary/key and terminal sessions did not receive revision zero: %v", err)
			}
			var retainedHistory string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m WHERE version<=54`).Scan(&retainedHistory); err != nil || retainedHistory != history {
				t.Fatalf("demand migration rewrote published migration history: %v", err)
			}
			for _, invalid := range []struct {
				value, code string
			}{{"-1", "23514"}, {"NULL", "23502"}} {
				_, err := pool.Exec(ctx, "UPDATE play_sessions SET playback_revision="+invalid.value+" WHERE id='device-migration-play-key'")
				var constraint *pgconn.PgError
				if !errors.As(err, &constraint) || constraint.Code != invalid.code {
					t.Fatalf("invalid playback revision was not constrained: %v", err)
				}
			}
			var initialized string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(p) ORDER BY id)::text FROM play_sessions p`).Scan(&initialized); err != nil {
				t.Fatal(err)
			}
			if err := database.Migrate(ctx, pool); err != nil {
				t.Fatal(err)
			}
			var repeated string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(p) ORDER BY id)::text FROM play_sessions p`).Scan(&repeated); err != nil || repeated != initialized {
				t.Fatalf("repeated demand migration changed the initialized revisions or rows: %v", err)
			}
		})
	}
}

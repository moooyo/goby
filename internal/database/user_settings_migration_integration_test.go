package database_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// The historical source contains the published migrations through schema 23,
// never a current schema with its newest migration-history row removed.
func userSettingsVersion23Baseline(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	scheduledTasksVersion18Baseline(t, ctx, pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin schema 23 user settings baseline: %v", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanupCtx)
	}()
	if err := database.RecoveryMigrateTo(ctx, tx, 23); err != nil {
		t.Fatalf("apply published migrations through schema 23: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit schema 23 user settings baseline: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 23 {
		t.Fatalf("user settings baseline schema = %d, want 23: %v", version, err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('user_settings') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("schema 23 already contains user settings: exists=%v error=%v", exists, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET
		configuration='{"AudioLanguagePreference":"eng","SubtitleMode":"Default","LegacyExtension":{"ExactInteger":9007199254740993,"Keep":[1,true,"same"]}}'::jsonb,
		management_revision=9007199254740993, updated_at='2025-09-01T00:00:00Z'
		WHERE id='device-user-a';
		UPDATE users SET configuration='{"SubtitleLanguagePreference":"fra","EnableNextEpisodeAutoPlay":false,"LegacyNull":null}'::jsonb,
		updated_at='2025-09-02T00:00:00Z' WHERE id='device-user-b';
		UPDATE managed_settings SET revision=9007199254740993, server_name='Preserved schema 23 server',
		server_name_mode='custom', updated_at='2025-09-03T00:00:00Z';
		INSERT INTO activity_entries(created_at,action,severity,source,actor_kind,resource_kind,resource_id,state)
		VALUES('2025-09-04T00:00:00Z','backup.finished','Info','system','system','backup','preserved-schema-23-backup','completed')`); err != nil {
		t.Fatalf("seed schema 23 configuration and activity witnesses: %v", err)
	}
}

// Compare complete historical rows without parsing JSON numbers or projecting
// out user configuration, credentials, timestamps, or management revisions.
// Later music source and credit group columns are checked separately from
// schema 23 fields.
func userSettingsLegacySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) string {
	t.Helper()
	statement := `SELECT COALESCE(jsonb_agg(to_jsonb(original) ORDER BY to_jsonb(original)::text), '[]'::jsonb)::text FROM ` + pgx.Identifier{table}.Sanitize() + ` original`
	if table == "item_metadata_state" {
		statement = `SELECT COALESCE(jsonb_agg(to_jsonb(original) - 'music_source'
			ORDER BY (to_jsonb(original) - 'music_source')::text), '[]'::jsonb)::text FROM item_metadata_state original`
	}
	if table == "item_entities" {
		statement = `SELECT COALESCE(jsonb_agg(to_jsonb(original) - 'credit_group'
			ORDER BY (to_jsonb(original) - 'credit_group')::text), '[]'::jsonb)::text FROM item_entities original`
	}
	if table == "schema_migrations" {
		statement += " WHERE version <= 23"
	}
	var snapshot string
	if err := pool.QueryRow(ctx, statement).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot original schema 23 table %s: %v", table, err)
	}
	return snapshot
}

func TestMigrateUserSettingsPreservesSchema23DataAndLeavesPreferencesEmpty(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	userSettingsVersion23Baseline(t, ctx, pool)
	legacy := captureDeviceLegacyTables(t, ctx, pool)
	if len(legacy) != 29 {
		t.Fatalf("schema 23 preservation inventory = %d tables, want 29", len(legacy))
	}
	for index := range legacy {
		legacy[index].snapshot = userSettingsLegacySnapshot(t, ctx, pool, legacy[index].name)
	}
	var firstHistory string
	for attempt := 1; attempt <= 2; attempt++ {
		if err := database.Migrate(ctx, pool); err != nil {
			t.Fatalf("user settings migration attempt %d: %v", attempt, err)
		}
		if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 25 {
			t.Fatalf("user settings migration schema = %d, want 25: %v", version, err)
		}
		var name string
		if err := pool.QueryRow(ctx, "SELECT name FROM schema_migrations WHERE version=24").Scan(&name); err != nil || name != "0024_user_settings.sql" {
			t.Fatalf("user settings migration name = %q: %v", name, err)
		}
		var tableCount, preferenceCount int
		if err := pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM pg_tables WHERE schemaname=current_schema()),
			(SELECT count(*) FROM user_settings)`).Scan(&tableCount, &preferenceCount); err != nil || tableCount != 30 || preferenceCount != 0 {
			t.Fatalf("user settings migration table count=%d preferences=%d, want 30 and 0: %v", tableCount, preferenceCount, err)
		}
		var inferredMusicSources, assignedCreditGroups int
		if err := pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM item_metadata_state WHERE music_source IS DISTINCT FROM '{}'::jsonb),
			(SELECT count(*) FROM item_entities WHERE credit_group IS DISTINCT FROM 0)`).Scan(&inferredMusicSources, &assignedCreditGroups); err != nil || inferredMusicSources != 0 || assignedCreditGroups != 0 {
			t.Fatalf("migration inferred historical music sources or credit groups: sources=%d groups=%d error=%v", inferredMusicSources, assignedCreditGroups, err)
		}
		for _, table := range legacy {
			if after := userSettingsLegacySnapshot(t, ctx, pool, table.name); after != table.snapshot {
				t.Errorf("migration attempt %d changed historical rows or columns in %s", attempt, table.name)
			}
		}
		history := migrationHistory(t, ctx, pool)
		if attempt == 1 {
			firstHistory = history
		} else if history != firstHistory {
			t.Error("repeated user settings migration changed complete migration history")
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_settings(user_id,settings,updated_at)
		VALUES('device-user-a','{"MaxStreamingBitrate":"4000000","SubtitleMode":"Smart"}'::jsonb,'2025-09-05T00:00:00Z')`); err != nil {
		t.Fatalf("persist independent user preferences after migration: %v", err)
	}
	preferencesBefore := userSettingsLegacySnapshot(t, ctx, pool, "user_settings")
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat migration after preferences were written: %v", err)
	}
	if after := userSettingsLegacySnapshot(t, ctx, pool, "user_settings"); after != preferencesBefore {
		t.Error("repeated migration reset persisted preferences or their update time")
	}
	if history := migrationHistory(t, ctx, pool); history != firstHistory {
		t.Error("repeated migration after preference writes changed complete migration history")
	}
	for _, table := range legacy {
		if after := userSettingsLegacySnapshot(t, ctx, pool, table.name); after != table.snapshot {
			t.Errorf("writing independent preferences or repeating migration changed historical table %s", table.name)
		}
	}
}

func TestUserSettingsMigrationEnforcesStorageBoundsAndUserOwnership(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("initialize user settings constraint schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,name,normalized_name,password_hash)
		VALUES('settings-owner','Settings Owner','settings owner','fixture-owner-digest'),
		('settings-other','Settings Other','settings other','fixture-other-digest')`); err != nil {
		t.Fatalf("seed independent preference owners: %v", err)
	}
	for _, test := range []struct {
		name, statement, code string
	}{
		{"null owner", `INSERT INTO user_settings(user_id) VALUES(NULL)`, "23502"},
		{"unknown owner", `INSERT INTO user_settings(user_id) VALUES('missing-user')`, "23503"},
		{"null settings", `INSERT INTO user_settings(user_id,settings) VALUES('settings-owner',NULL)`, "23502"},
		{"json null", `INSERT INTO user_settings(user_id,settings) VALUES('settings-owner','null'::jsonb)`, "23514"},
		{"json array", `INSERT INTO user_settings(user_id,settings) VALUES('settings-owner','[]'::jsonb)`, "23514"},
		{"json string", `INSERT INTO user_settings(user_id,settings) VALUES('settings-owner','"text"'::jsonb)`, "23514"},
		{"json boolean", `INSERT INTO user_settings(user_id,settings) VALUES('settings-owner','true'::jsonb)`, "23514"},
		{"json number", `INSERT INTO user_settings(user_id,settings) VALUES('settings-owner','42'::jsonb)`, "23514"},
		{"object above byte limit", `INSERT INTO user_settings(user_id,settings) VALUES('settings-owner',jsonb_build_object('Value',repeat('a',262145)))`, "23514"},
		{"multibyte object above byte limit", `INSERT INTO user_settings(user_id,settings) VALUES('settings-owner',jsonb_build_object('Value',repeat(chr(233),131073)))`, "23514"},
		{"null update time", `INSERT INTO user_settings(user_id,updated_at) VALUES('settings-owner',NULL)`, "23502"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, test.statement)
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != test.code {
				t.Fatalf("user settings constraint error = %v, want SQLSTATE %s", err, test.code)
			}
		})
	}
	var defaultsValid bool
	if err := pool.QueryRow(ctx, `INSERT INTO user_settings(user_id) VALUES('settings-owner')
		RETURNING settings='{}'::jsonb AND updated_at IS NOT NULL`).Scan(&defaultsValid); err != nil || !defaultsValid {
		t.Fatalf("omitted user settings columns did not retain their defaults: %v", err)
	}
	var exactBytes int
	if err := pool.QueryRow(ctx, `INSERT INTO user_settings(user_id,settings)
		VALUES('settings-other',jsonb_build_object('Value',repeat('a',262144-octet_length(jsonb_build_object('Value','')::text))))
		RETURNING octet_length(settings::text)`).Scan(&exactBytes); err != nil || exactBytes != 262144 {
		t.Fatalf("object at exact user settings byte limit = %d, want 262144: %v", exactBytes, err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO user_settings(user_id) VALUES('settings-owner')`)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23505" {
		t.Fatalf("duplicate preference owner error = %v, want SQLSTATE 23505", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id='settings-owner'`); err != nil {
		t.Fatalf("delete one preference owner: %v", err)
	}
	var remaining []string
	if err := pool.QueryRow(ctx, "SELECT array_agg(user_id ORDER BY user_id) FROM user_settings").Scan(&remaining); err != nil || len(remaining) != 1 || remaining[0] != "settings-other" {
		t.Fatalf("deleting one owner changed another user's preferences or retained an orphan: %v", err)
	}
}

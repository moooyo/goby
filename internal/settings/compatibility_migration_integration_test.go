package settings

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// Only columns added after schema 20 are excluded. Every schema-20 field,
// including the raw name, revision, timestamps, and migration history, remains
// part of the comparison. Snapshots never leave PostgreSQL as parsed JSON.
func compatibilityMigrationSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) string {
	t.Helper()
	projection := "to_jsonb(original)"
	if table == "managed_settings" {
		projection += " - 'server_name_mode' - 'compatibility_max_width'"
	}
	if table == "item_metadata_state" {
		projection += " - 'music_source'"
	}
	if table == "item_entities" {
		projection += " - 'credit_group'"
	}
	statement := "SELECT COALESCE(jsonb_agg(" + projection + " ORDER BY (" + projection + ")::text), '[]'::jsonb)::text FROM " + pgx.Identifier{table}.Sanitize() + " original"
	if table == "schema_migrations" {
		statement += " WHERE version <= 20"
	}
	var snapshot string
	if err := pool.QueryRow(ctx, statement).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot compatibility migration table %s: %v", table, err)
	}
	return snapshot
}

func compatibilityMigrationManagedColumns(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []string {
	t.Helper()
	var columns []string
	if err := pool.QueryRow(ctx, `SELECT array_agg(attname::text ORDER BY attname)
		FROM pg_attribute WHERE attrelid = 'managed_settings'::regclass
		AND attnum > 0 AND NOT attisdropped`).Scan(&columns); err != nil {
		t.Fatalf("read managed settings compatibility column inventory: %v", err)
	}
	return columns
}

func compatibilityMigrationHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var count int
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT count(*), jsonb_agg(to_jsonb(m) ORDER BY version)::text
		FROM schema_migrations m`).Scan(&count, &snapshot); err != nil || count != 26 {
		t.Fatalf("compatibility full migration history count = %d, want 26: %v", count, err)
	}
	return snapshot
}

func TestConfigurationCompatibilityMigrationPreservesEverySchema20Field(t *testing.T) {
	for _, test := range []struct {
		name, rawName, mode string
		explicitLimits      bool
	}{
		{"deployment name", "", "deployment", false},
		{"custom name", "  Preserved Server  ", "custom", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, pool := settingsMigrationPool(t)
			settingsMigrationVersion19(t, ctx, pool)
			settingsMigrationSeedLegacy(t, ctx, pool)
			settingsMigrationVersion20(t, ctx, pool)
			if _, err := pool.Exec(ctx, `UPDATE managed_settings SET revision = 9007199254740993,
				server_name = NULLIF($1, ''), max_width = 7680,
				max_bitrate = CASE WHEN $2 THEN 987654321 ELSE NULL END,
				max_height = CASE WHEN $2 THEN 4320 ELSE NULL END,
				max_audio_channels = CASE WHEN $2 THEN 7 ELSE NULL END,
				created_at = '2024-01-02T03:04:05Z', updated_at = '2025-02-03T04:05:06Z'
				WHERE id = 1`, test.rawName, test.explicitLimits); err != nil {
				t.Fatalf("seed original schema-20 settings: %v", err)
			}
			tables := settingsMigrationTables(t, ctx, pool)
			wantTables := append(strings.Fields(settingsMigrationLegacyTables), "managed_settings")
			sort.Strings(wantTables)
			if len(tables) != 28 || strings.Join(tables, " ") != strings.Join(wantTables, " ") {
				t.Fatalf("schema 20 compatibility fixture table inventory = %v, want all 28 historical tables", tables)
			}
			before := make(map[string]string, len(tables))
			for _, table := range tables {
				before[table] = compatibilityMigrationSnapshot(t, ctx, pool, table)
				if before[table] == "[]" {
					t.Fatalf("compatibility migration fixture left historical table %s empty", table)
				}
			}
			columns := compatibilityMigrationManagedColumns(t, ctx, pool)
			if len(columns) != 9 {
				t.Fatalf("schema 20 managed settings has %d columns, want 9", len(columns))
			}
			wantColumns := append(append([]string(nil), columns...), "server_name_mode", "compatibility_max_width")
			sort.Strings(wantColumns)
			currentTables := append(append([]string(nil), tables...), "activity_entries", "user_settings", "theme_owner_ids", "theme_reserved_paths", "item_theme_resources")
			sort.Strings(currentTables)
			var migratedSettings, migratedHistory string
			for attempt := 1; attempt <= 2; attempt++ {
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatalf("compatibility migration attempt %d: %v", attempt, err)
				}
				if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 26 {
					t.Fatalf("compatibility full migration schema version = %d, want 26: %v", version, err)
				}
				var name string
				if err := pool.QueryRow(ctx, "SELECT name FROM schema_migrations WHERE version = 21").Scan(&name); err != nil || name != "0021_configuration_compatibility.sql" {
					t.Fatalf("compatibility migration history name = %q: %v", name, err)
				}
				if after := settingsMigrationTables(t, ctx, pool); len(after) != 33 || strings.Join(after, " ") != strings.Join(currentTables, " ") {
					t.Errorf("current migration did not retain every historical table and add activity entries, user settings, and three theme tables: %v", after)
				}
				var activityCount int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM activity_entries").Scan(&activityCount); err != nil || activityCount != 0 {
					t.Errorf("current migration backfilled historical state into activity entries: count=%d error=%v", activityCount, err)
				}
				var userSettingsCount int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_settings").Scan(&userSettingsCount); err != nil || userSettingsCount != 0 {
					t.Errorf("current migration populated user settings: count=%d error=%v", userSettingsCount, err)
				}
				var musicSourceCount, groupedCreditCount int
				if err := pool.QueryRow(ctx, `SELECT
					(SELECT count(*) FROM item_metadata_state WHERE music_source IS DISTINCT FROM '{}'::jsonb),
					(SELECT count(*) FROM item_entities WHERE credit_group IS DISTINCT FROM 0)`).Scan(&musicSourceCount, &groupedCreditCount); err != nil || musicSourceCount != 0 || groupedCreditCount != 0 {
					t.Errorf("current migration populated historical music sources or grouped credits: sources=%d credits=%d error=%v", musicSourceCount, groupedCreditCount, err)
				}
				if after := compatibilityMigrationManagedColumns(t, ctx, pool); strings.Join(after, " ") != strings.Join(wantColumns, " ") {
					t.Errorf("compatibility migration did not add exactly its two columns: %v", after)
				}
				for _, table := range tables {
					if after := compatibilityMigrationSnapshot(t, ctx, pool, table); after != before[table] {
						t.Errorf("compatibility migration attempt %d changed original rows or fields in %s", attempt, table)
					}
				}
				var total, expected int
				if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE id = 1
					AND server_name_mode = $1 AND server_name IS NOT DISTINCT FROM NULLIF($2, '')
					AND compatibility_max_width = 0) FROM managed_settings`, test.mode, test.rawName).Scan(&total, &expected); err != nil || total != 1 || expected != 1 {
					t.Fatalf("compatibility initial rows = %d, correctly mapped rows = %d, want one preserved %s row with width zero: %v", total, expected, test.mode, err)
				}
				currentSettings := settingsMigrationSnapshot(t, ctx, pool, "managed_settings")
				currentHistory := compatibilityMigrationHistory(t, ctx, pool)
				if attempt == 1 {
					migratedSettings, migratedHistory = currentSettings, currentHistory
				} else if currentSettings != migratedSettings || currentHistory != migratedHistory {
					t.Error("reapplying compatibility migration changed settings, mode, width, or complete migration history")
				}
			}
			// Reapplication must not reconstruct the mode from the raw name after
			// compatibility writes have deliberately selected any of the four states.
			for _, state := range []struct {
				mode string
				raw  any
			}{
				{"empty", ""}, {"unset", nil}, {"custom", "  Persisted Compatibility Name  "}, {"deployment", nil},
			} {
				if _, err := pool.Exec(ctx, `UPDATE managed_settings SET server_name_mode = $1, server_name = $2,
					compatibility_max_width = 4096, revision = 9007199254740995,
					updated_at = '2025-03-04T05:06:07Z' WHERE id = 1`, state.mode, state.raw); err != nil {
					t.Fatalf("persist compatibility name mode %s: %v", state.mode, err)
				}
				persisted := settingsMigrationSnapshot(t, ctx, pool, "managed_settings")
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatalf("repeat compatibility migration with mode %s: %v", state.mode, err)
				}
				if after := settingsMigrationSnapshot(t, ctx, pool, "managed_settings"); after != persisted {
					t.Errorf("repeated compatibility migration changed persisted mode %s, width, or original settings", state.mode)
				}
				if after := compatibilityMigrationHistory(t, ctx, pool); after != migratedHistory {
					t.Errorf("repeated compatibility migration with mode %s changed complete migration history", state.mode)
				}
				for _, table := range tables {
					if table != "managed_settings" && compatibilityMigrationSnapshot(t, ctx, pool, table) != before[table] {
						t.Errorf("repeated compatibility migration with mode %s changed historical table %s", state.mode, table)
					}
				}
			}
		})
	}
}

func TestConfigurationCompatibilityMigrationEnforcesNameStatesAndWidthBounds(t *testing.T) {
	ctx, pool := settingsMigrationPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("initialize compatibility constraint schema: %v", err)
	}
	for _, test := range []struct {
		name, statement, code string
	}{
		{"null mode", "UPDATE managed_settings SET server_name_mode = NULL WHERE id = 1", "23502"},
		{"empty mode", "UPDATE managed_settings SET server_name_mode = '' WHERE id = 1", "23514"},
		{"unknown mode", "UPDATE managed_settings SET server_name_mode = 'automatic' WHERE id = 1", "23514"},
		{"mode case mismatch", "UPDATE managed_settings SET server_name_mode = 'Deployment' WHERE id = 1", "23514"},
		{"deployment with empty name", "UPDATE managed_settings SET server_name_mode = 'deployment', server_name = '' WHERE id = 1", "23514"},
		{"deployment with custom name", "UPDATE managed_settings SET server_name_mode = 'deployment', server_name = 'A' WHERE id = 1", "23514"},
		{"unset with empty name", "UPDATE managed_settings SET server_name_mode = 'unset', server_name = '' WHERE id = 1", "23514"},
		{"unset with custom name", "UPDATE managed_settings SET server_name_mode = 'unset', server_name = 'A' WHERE id = 1", "23514"},
		{"empty with null name", "UPDATE managed_settings SET server_name_mode = 'empty', server_name = NULL WHERE id = 1", "23514"},
		{"empty with whitespace name", "UPDATE managed_settings SET server_name_mode = 'empty', server_name = ' ' WHERE id = 1", "23514"},
		{"empty with custom name", "UPDATE managed_settings SET server_name_mode = 'empty', server_name = 'A' WHERE id = 1", "23514"},
		{"custom with null name", "UPDATE managed_settings SET server_name_mode = 'custom', server_name = NULL WHERE id = 1", "23514"},
		{"custom with empty name", "UPDATE managed_settings SET server_name_mode = 'custom', server_name = '' WHERE id = 1", "23514"},
		{"custom name above byte limit", "UPDATE managed_settings SET server_name_mode = 'custom', server_name = repeat('a', 129) WHERE id = 1", "23514"},
		{"custom multibyte name above byte limit", "UPDATE managed_settings SET server_name_mode = 'custom', server_name = repeat(chr(233), 65) WHERE id = 1", "23514"},
		{"negative compatibility width", "UPDATE managed_settings SET compatibility_max_width = -1 WHERE id = 1", "23514"},
		{"compatibility width above maximum", "UPDATE managed_settings SET compatibility_max_width = 8193 WHERE id = 1", "23514"},
		{"null compatibility width", "UPDATE managed_settings SET compatibility_max_width = NULL WHERE id = 1", "23502"},
		{"native width still rejects zero", "UPDATE managed_settings SET max_width = 0 WHERE id = 1", "23514"},
		{"native width still rejects above maximum", "UPDATE managed_settings SET max_width = 8193 WHERE id = 1", "23514"},
		{"native bitrate still rejects zero", "UPDATE managed_settings SET max_bitrate = 0 WHERE id = 1", "23514"},
		{"native height still rejects zero", "UPDATE managed_settings SET max_height = 0 WHERE id = 1", "23514"},
		{"native audio channels still reject zero", "UPDATE managed_settings SET max_audio_channels = 0 WHERE id = 1", "23514"},
		{"revision still rejects zero", "UPDATE managed_settings SET revision = 0 WHERE id = 1", "23514"},
		{"singleton still rejects second identity", "INSERT INTO managed_settings (id) VALUES (2)", "23514"},
		{"singleton still rejects duplicates", "INSERT INTO managed_settings (id) VALUES (1)", "23505"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, test.statement)
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) {
				t.Fatalf("expected compatibility PostgreSQL constraint %s, got error type %T", test.code, err)
			}
			if databaseError.Code != test.code {
				t.Fatalf("compatibility constraint code = %s (%s), want %s", databaseError.Code, databaseError.ConstraintName, test.code)
			}
		})
	}
	for _, test := range []struct {
		name, mode string
		raw        any
		width      int
	}{
		{"deployment with null name", "deployment", nil, 0},
		{"unset with null name", "unset", nil, 1},
		{"empty with exact empty name", "empty", "", 8192},
		{"custom minimum name", "custom", "A", 0},
		{"custom maximum name", "custom", strings.Repeat("a", 128), 8192},
		{"custom multibyte maximum name", "custom", strings.Repeat("\u00e9", 64), 4096},
		{"custom whitespace name", "custom", " ", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			result, err := tx.Exec(ctx, `UPDATE managed_settings SET server_name_mode = $1, server_name = $2,
				compatibility_max_width = $3, max_width = 7680 WHERE id = 1`, test.mode, test.raw, test.width)
			if err != nil || result.RowsAffected() != 1 {
				t.Fatalf("valid compatibility state was rejected: rows=%d error=%v", result.RowsAffected(), err)
			}
			var mode string
			var raw *string
			var compatibilityWidth, nativeWidth int
			if err := tx.QueryRow(ctx, `SELECT server_name_mode, server_name, compatibility_max_width, max_width
				FROM managed_settings WHERE id = 1`).Scan(&mode, &raw, &compatibilityWidth, &nativeWidth); err != nil {
				t.Fatalf("read accepted compatibility state: %v", err)
			}
			if mode != test.mode || compatibilityWidth != test.width || nativeWidth != 7680 {
				t.Error("accepted compatibility state changed its mode or mixed compatibility and native widths")
			}
			if test.raw == nil {
				if raw != nil {
					t.Error("accepted null name was materialized as text")
				}
			} else if raw == nil || *raw != test.raw.(string) {
				t.Error("accepted compatibility name did not preserve its exact original text")
			}
		})
	}
	t.Run("omitted compatibility columns keep defaults", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, "DELETE FROM managed_settings WHERE id = 1"); err != nil {
			t.Fatalf("prepare owned compatibility default fixture: %v", err)
		}
		var mode string
		var width int
		var raw *string
		if err := tx.QueryRow(ctx, `INSERT INTO managed_settings (id) VALUES (1)
			RETURNING server_name_mode, compatibility_max_width, server_name`).Scan(&mode, &width, &raw); err != nil {
			t.Fatalf("insert settings with omitted compatibility columns: %v", err)
		}
		if mode != "deployment" || width != 0 || raw != nil {
			t.Errorf("omitted compatibility defaults = mode %q, width %d, null name %v", mode, width, raw == nil)
		}
	})
}

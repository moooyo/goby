package database_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/database"
)

func TestMigrateRejectsSparseOrRenamedHistoryBeforeSQL(t *testing.T) {
	for _, test := range []struct {
		name   string
		change string
		want   string
	}{
		{"missing first", "DELETE FROM schema_migrations WHERE version = 1", "contiguous published prefix"},
		{"missing middle", "DELETE FROM schema_migrations WHERE version = 14", "contiguous published prefix"},
		{"renamed history", "UPDATE schema_migrations SET name = '0014_renamed.sql' WHERE version = 14", "unexpected filename"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			if err := database.Migrate(ctx, pool); err != nil {
				t.Fatalf("create published schema fixture: %v", err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO server_settings(key,value)
				VALUES ('migration-integrity-sentinel','preserve this value')`); err != nil {
				t.Fatalf("seed retained application row: %v", err)
			}
			if _, err := pool.Exec(ctx, test.change); err != nil {
				t.Fatalf("create invalid historical prefix fixture: %v", err)
			}
			historyBefore := migrationHistory(t, ctx, pool)
			var settingsBefore string
			if err := pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(s) ORDER BY key)::text FROM server_settings s").Scan(&settingsBefore); err != nil {
				t.Fatal(err)
			}
			if err := database.Migrate(ctx, pool); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("history must be rejected before attempting already-applied DDL: %v", err)
			}
			if after := migrationHistory(t, ctx, pool); after != historyBefore {
				t.Fatal("rejected migration filled or rewrote historical rows")
			}
			var settingsAfter string
			if err := pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(s) ORDER BY key)::text FROM server_settings s").Scan(&settingsAfter); err != nil || settingsAfter != settingsBefore {
				t.Fatalf("rejected migration changed application state: %v", err)
			}
		})
	}
}

func TestMigratePublishedPrefixPreservesHistoricalRowsWithoutInventingChecksums(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if err := database.RecoveryMigrateTo(ctx, tx, 23); err != nil {
		t.Fatalf("create exact published schema-23 prefix: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	historyBefore := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade a valid published history prefix: %v", err)
	}
	var historicalRows string
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text
		FROM schema_migrations m WHERE version <= 23`).Scan(&historicalRows); err != nil || historicalRows != historyBefore {
		t.Fatalf("ordinary migration rewrote published historical rows: %v", err)
	}
	var columns string
	if err := pool.QueryRow(ctx, `SELECT string_agg(column_name, ',' ORDER BY ordinal_position)
		FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'schema_migrations'`).Scan(&columns); err != nil || columns != "version,name,applied_at" {
		t.Fatalf("published history shape changed or invented execution checksums: columns=%s error=%v", columns, err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != currentMigrationVersion(t) {
		t.Fatalf("integrity enforcement must reach the current published endpoint: version=%d error=%v", version, err)
	}
	completeHistory := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil || migrationHistory(t, ctx, pool) != completeHistory {
		t.Fatalf("repeat migration changed the exact published history: %v", err)
	}
}

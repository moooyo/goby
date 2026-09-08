package database_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// migrationTestPool owns a fresh schema and never modifies existing schemas.
func migrationTestPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("create integration database connection")
	}
	t.Cleanup(adminPool.Close)
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schema := "goby_migration_test_" + hex.EncodeToString(suffix[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create isolated test schema: %v", err)
	}
	// Register removal only after this test creates the exact random schema.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("remove owned test schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration database configuration")
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create isolated integration database pool")
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}

func migrationHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version), '[]'::jsonb)::text
		FROM schema_migrations m`).Scan(&snapshot); err != nil {
		t.Fatalf("read migration history snapshot: %v", err)
	}
	return snapshot
}

func TestMigrateConcurrentAndIdempotent(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	const attempts = 4
	start := make(chan struct{})
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			<-start
			results <- database.Migrate(ctx, pool)
		}()
	}
	close(start)
	for i := 0; i < attempts; i++ {
		if err := <-results; err != nil {
			t.Errorf("concurrent migration attempt %d: %v", i+1, err)
		}
	}
	version, err := database.SchemaVersion(ctx, pool)
	if err != nil || version != 1 {
		t.Fatalf("schema version after concurrent migration = %d, want 1, error = %v", version, err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("migration history count = %d, want 1", count)
	}
	before := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat completed migration: %v", err)
	}
	if after := migrationHistory(t, ctx, pool); after != before {
		t.Errorf("repeated migration changed history: before = %s, after = %s", before, after)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 1 {
		t.Errorf("schema version after repeated migration = %d, want 1, error = %v", version, err)
	}
	// Successful history entries must correspond to the actual application tables.
	for _, table := range []string{"users", "sessions", "server_settings"} {
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil || !exists {
			t.Errorf("migrated table %s exists = %v, error = %v", table, exists, err)
		}
	}
}

func TestMigrateRejectsUnknownVersionWithoutChangingUsers(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("create known schema: %v", err)
	}
	// Authentication is deliberately outside this package's migration tests.
	if _, err := pool.Exec(ctx, `INSERT INTO users
		(id, name, normalized_name, password_hash, is_administrator, policy, configuration)
		VALUES ('existing-user', 'Existing User', 'existing user', 'fixture-only-no-authentication', true,
		'{"EnableRemoteAccess": false}'::jsonb, '{"AudioLanguagePreference": "eng"}'::jsonb)`); err != nil {
		t.Fatalf("insert existing user fixture: %v", err)
	}
	var usersBefore string
	if err := pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(u) ORDER BY id)::text FROM users u").Scan(&usersBefore); err != nil {
		t.Fatalf("read existing user snapshot: %v", err)
	}
	const unsupportedVersion int64 = 999999
	if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", unsupportedVersion, "999999_future.sql"); err != nil {
		t.Fatalf("record unknown migration fixture: %v", err)
	}
	historyBefore := migrationHistory(t, ctx, pool)
	err := database.Migrate(ctx, pool)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("migrate unknown database version: got %v, want unsupported-version error", err)
	}
	var usersAfter string
	if err := pool.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(u) ORDER BY id), '[]'::jsonb)::text FROM users u").Scan(&usersAfter); err != nil {
		t.Fatalf("read users after rejected migration: %v", err)
	}
	if usersAfter != usersBefore {
		t.Errorf("rejected migration changed existing users: before = %s, after = %s", usersBefore, usersAfter)
	}
	if historyAfter := migrationHistory(t, ctx, pool); historyAfter != historyBefore {
		t.Errorf("rejected migration changed history: before = %s, after = %s", historyBefore, historyAfter)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != unsupportedVersion {
		t.Errorf("schema version after rejected migration = %d, want %d, error = %v", version, unsupportedVersion, err)
	}
}

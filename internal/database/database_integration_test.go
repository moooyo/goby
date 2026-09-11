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

// Historical settings and music migrations keep their published schema25
// endpoint even after the current runner gains later schema additions.
func migratePublishedSchema25TestPrefix(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanupCtx)
	}()
	if err := database.RecoveryMigrateTo(ctx, tx, 25); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	if err != nil || version != 27 {
		t.Fatalf("schema version after concurrent migration = %d, want 27, error = %v", version, err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}
	if count != 27 {
		t.Fatalf("migration history count = %d, want 27", count)
	}
	before := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat completed migration: %v", err)
	}
	if after := migrationHistory(t, ctx, pool); after != before {
		t.Errorf("repeated migration changed history: before = %s, after = %s", before, after)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 27 {
		t.Errorf("schema version after repeated migration = %d, want 27, error = %v", version, err)
	}
	// Successful history entries must correspond to the actual application tables.
	for _, table := range []string{"users", "sessions", "server_settings", "libraries", "library_roots", "items", "scan_jobs", "catalog_entities", "item_entities", "item_images", "user_item_data", "play_sessions", "item_subtitles", "encoding_jobs", "client_playback_references", "item_metadata_state", "application_keys", "application_key_clients", "devices", "application_key_devices", "managed_settings", "activity_entries", "user_settings", "theme_owner_ids", "theme_reserved_paths", "item_theme_resources", "extra_reserved_paths", "item_extra_resources"} {
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil || !exists {
			t.Errorf("migrated table %s exists = %v, error = %v", table, exists, err)
		}
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_tables WHERE schemaname = current_schema()").Scan(&count); err != nil || count != 35 {
		t.Errorf("current schema table count = %d, want 35, error = %v", count, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM activity_entries").Scan(&count); err != nil || count != 0 {
		t.Errorf("fresh migration populated activity entries: count=%d error=%v", count, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_settings").Scan(&count); err != nil || count != 0 {
		t.Errorf("fresh migration populated user settings: count=%d error=%v", count, err)
	}
	var owners, virtualRoots, reservedPaths, themeResources, extraPaths, extraResources int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM theme_owner_ids),
		(SELECT count(*) FROM theme_owner_ids WHERE virtual_root AND item_id IS NULL),
		(SELECT count(*) FROM theme_reserved_paths),
		(SELECT count(*) FROM item_theme_resources),
		(SELECT count(*) FROM extra_reserved_paths),
		(SELECT count(*) FROM item_extra_resources)`).Scan(&owners, &virtualRoots, &reservedPaths, &themeResources, &extraPaths, &extraResources); err != nil || owners != 1 || virtualRoots != 1 || reservedPaths != 0 || themeResources != 0 || extraPaths != 0 || extraResources != 0 {
		t.Errorf("fresh auxiliary state must contain one virtual owner and no discovered resources: owners=%d roots=%d theme_paths=%d themes=%d extra_paths=%d extras=%d error=%v", owners, virtualRoots, reservedPaths, themeResources, extraPaths, extraResources, err)
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

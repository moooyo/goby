package settings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

const settingsMigrationLegacyTables = "application_key_clients application_key_devices application_keys catalog_entities client_playback_references devices encoding_jobs item_entities item_images item_metadata_state item_subtitles items libraries library_roots play_sessions scan_jobs schema_migrations server_settings sessions task_definitions task_occurrences task_run_children task_run_requests task_runs task_triggers user_item_data users"

// Each test owns one random schema; the connection URL's existing data is never
// migrated, seeded, snapshotted, or removed.
func settingsMigrationPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("create settings migration administration connection")
	}
	t.Cleanup(adminPool.Close)
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate settings migration schema name: %v", err)
	}
	schema := "goby_settings_migration_test_" + hex.EncodeToString(suffix[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create owned settings migration schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("remove owned settings migration schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse settings migration database configuration")
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create isolated settings migration pool")
	}
	t.Cleanup(pool.Close)
	var actualSchema string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&actualSchema); err != nil || actualSchema != schema {
		t.Fatalf("settings migration pool is not scoped to its owned schema: %v", err)
	}
	return ctx, pool
}

// Apply the published migrations directly to construct the real upgrade
// boundary, without creating schema 20 first and then removing its state.
func settingsMigrationVersion19(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	directory := filepath.Join("..", "database", "migrations")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read historical settings migration inventory: %v", err)
	}
	names := make(map[int]string)
	for _, entry := range entries {
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		version, err := strconv.Atoi(prefix)
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") || !ok || err != nil || version < 1 || version > 19 {
			continue
		}
		if names[version] != "" {
			t.Fatalf("duplicate historical migration version %d", version)
		}
		names[version] = entry.Name()
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin settings schema 19 fixture: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `CREATE TABLE schema_migrations (
		version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatalf("create settings migration history fixture: %v", err)
	}
	for version := 1; version <= 19; version++ {
		name := names[version]
		if name == "" {
			t.Fatalf("historical settings migration %d is missing", version)
		}
		content, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatalf("read historical migration %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply historical migration %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name, applied_at)
			VALUES ($1, $2, '2025-01-02T03:04:05Z')`, version, name); err != nil {
			t.Fatalf("record historical migration %s: %v", name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit settings schema 19 fixture: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 19 {
		t.Fatalf("settings upgrade baseline version = %d, want 19: %v", version, err)
	}
	var managedSettingsExist bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('managed_settings') IS NOT NULL").Scan(&managedSettingsExist); err != nil || managedSettingsExist {
		t.Fatalf("schema 19 unexpectedly contains managed settings: exists=%v error=%v", managedSettingsExist, err)
	}
}

func settingsMigrationSeedLegacy(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO server_settings (key, value, created_at, updated_at) VALUES
			('server_id', '0123456789abcdef0123456789abcdef', '2020-01-01T00:00:00Z', '2020-01-02T00:00:00Z'),
			('setup_completed', 'true', '2020-01-01T00:00:00Z', '2020-01-02T00:00:00Z'),
			('legacy_extension', '{"Preserved":true}', '2021-01-01T00:00:00Z', '2021-02-01T00:00:00Z');
		INSERT INTO users (id, name, normalized_name, password_hash, is_administrator, is_disabled,
			policy, configuration, management_revision) VALUES
			('settings-migration-user', 'Historical Administrator', 'historical administrator', 'synthetic-admin-digest',
			 true, false, '{"EnableAllFolders":false,"EnabledFolders":["settings-migration-library"]}',
			 '{"PreferredAudioLanguage":"eng"}', 9007199254740993),
			('settings-migration-disabled-user', 'Disabled Viewer', 'disabled viewer', 'synthetic-viewer-digest',
			 false, true, '{"EnablePlaybackRemuxing":false}', '{"PreferredSubtitleLanguage":"fra"}', 5);
		INSERT INTO devices (reported_device_id, reported_name, custom_name, app_name, app_version,
			last_user_id, revision, ip_address, created_at, last_seen_at, deleted_at) VALUES
			('settings-migration-device', 'Current Device', 'Preserved Device Override', 'Historical App', '1.2',
			 'settings-migration-user', 9, '192.0.2.90', '2024-01-01T00:00:00Z', '2025-01-01T00:00:00Z', NULL),
			('settings-migration-device', 'Retired Device', NULL, 'Older App', '1.0',
			 'settings-migration-disabled-user', 4, '192.0.2.91', '2020-01-01T00:00:00Z',
			 '2021-01-01T00:00:00Z', '2021-02-01T00:00:00Z');
		INSERT INTO sessions (id, user_id, token_hash, kind, device_id, device_name, client_name, client_version,
			created_at, last_seen_at, expires_at, revoked_at, client_capabilities, device_registry_id) VALUES
			('settings-migration-auth', 'settings-migration-user', decode(repeat('a1', 32), 'hex'), 'emby',
			 'settings-migration-device', 'Reported Device', 'Historical App', '1.2', '2024-01-01T00:00:00Z',
			 '2025-01-01T00:00:00Z', '2035-01-01T00:00:00Z', NULL, '{"SupportsMediaControl":true}',
			 (SELECT id FROM devices WHERE reported_device_id = 'settings-migration-device' AND deleted_at IS NULL)),
			('settings-migration-revoked-auth', 'settings-migration-disabled-user', decode(repeat('b2', 32), 'hex'), 'admin',
			 'historical-browser', 'Retired Browser', 'Historical Web', '0.1', '2020-01-01T00:00:00Z',
			 '2021-01-01T00:00:00Z', '2022-01-01T00:00:00Z', '2021-02-01T00:00:00Z', '{}', NULL),
			('settings-migration-key', NULL, decode(repeat('c3', 32), 'hex'), 'application_key',
			 'settings-server', 'Historical Server', 'Historical Integration', '3.0', '2024-01-01T00:00:00Z',
			 '2025-01-01T00:00:00Z', NULL, NULL, '{"SupportsMediaControl":true}', NULL);
		INSERT INTO application_key_devices (id, reported_device_id, reported_name, custom_name, app_name,
			app_version, revision, created_at, last_seen_at, ip_address)
		VALUES (1, 'settings-server', 'Historical Server', 'Preserved Server Override', 'Historical Integration',
			'3.0', 3, '2024-01-01T00:00:00Z', '2025-01-01T00:00:00Z', '192.0.2.80');
		INSERT INTO application_keys (credential_id, secret_ciphertext, created_by, last_used_at, ip_address)
		VALUES ('settings-migration-key', decode('010203040506', 'hex'), 'settings-migration-user',
			'2025-01-01T00:00:00Z', '192.0.2.80');
		INSERT INTO application_key_clients (id, credential_id, client_name, device_id, device_name,
			client_version, client_capabilities, created_at, last_seen_at)
		VALUES ('settings-migration-key-client', 'settings-migration-key', 'Historical Integration', 'settings-server',
			'Historical Server', '3.0', '{"SupportsMediaControl":true}', '2024-01-01T00:00:00Z', '2025-01-01T00:00:00Z');
		INSERT INTO libraries (id, name, collection_type, last_scan_at)
		VALUES ('settings-migration-library', 'Settings Migration Library', 'movies', '2025-01-01T00:00:00Z');
		INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
		VALUES ('settings-migration-root', 'settings-migration-library', '/synthetic/settings-library', '/synthetic', 'settings-library');
		INSERT INTO items (id, library_id, root_id, name, sort_name, type, path, relative_path,
			media, file_identity, file_size, modified_at, local_metadata, local_metadata_hash, local_metadata_path)
		VALUES ('settings-migration-item', 'settings-migration-library', 'settings-migration-root', 'Preserved Movie',
			'preserved movie', 'Movie', '/synthetic/settings-library/movie.mp4', 'movie.mp4',
			'{"Container":"mp4","DurationTicks":90000000}', 'preserved-file', 8192, '2025-01-01T00:00:00Z',
			'{"Overview":"Preserved overview","Genres":["Preserved Genre"]}', repeat('a', 64), 'movie.nfo');
		SELECT sync_catalog_item_entities('settings-migration-item',
			'{"Genres":["Preserved Genre"],"Tags":["Preserved Tag"],"Studios":["Preserved Studio"],"People":[{"Name":"Preserved Person","Role":"Original Role","Type":"Actor","SortOrder":2}]}');
		UPDATE item_metadata_state SET overrides = '{"Name":"Preserved Override"}',
			locked_values = '{"Genres":["Preserved Genre"]}', revision = 7,
			last_edited_by = 'settings-migration-user', last_edited_at = '2025-02-01T00:00:00Z'
			WHERE item_id = 'settings-migration-item';
		INSERT INTO item_images (item_id, root_id, image_type, image_index, relative_path, file_identity,
			source_hash, file_size, modified_at, width, height, mime_type)
		VALUES ('settings-migration-item', 'settings-migration-root', 'Primary', 0, 'poster.jpg', 'preserved-image',
			repeat('b', 64), 1024, '2025-01-01T00:00:00Z', 32, 32, 'image/jpeg');
		INSERT INTO item_subtitles (item_id, root_id, stream_index, relative_path, file_identity, source_hash,
			file_size, modified_at, change_time_ns, codec, language, title, is_default, mime_type)
		VALUES ('settings-migration-item', 'settings-migration-root', 5, 'movie.en.srt', 'preserved-subtitle',
			repeat('c', 64), 64, '2025-01-01T00:00:00Z', 123456789, 'srt', 'eng', 'Original Subtitle', true, 'application/x-subrip');
		INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, play_count, is_favorite, last_played_at)
		VALUES ('settings-migration-user', 'settings-migration-item', 12345, 7, true, '2025-01-01T00:00:00Z');
		INSERT INTO play_sessions (id, user_id, auth_session_id, application_client_id, device_id, item_id,
			media_source_id, state, position_ticks, duration_ticks, counted, expires_at, client_correlated, player_state) VALUES
			('settings-migration-play', 'settings-migration-user', 'settings-migration-auth', NULL, 'settings-migration-device',
			 'settings-migration-item', 'settings-migration-source', 'Stopped', 12345, 90000000, true,
			 '2025-01-01T00:00:00Z', true, '{"CanSeek":true,"IsMuted":false}'),
			('settings-migration-key-play', NULL, 'settings-migration-key', 'settings-migration-key-client', 'settings-server',
			 'settings-migration-item', 'settings-migration-source', 'Prepared', 0, 90000000, false,
			 '2035-01-01T00:00:00Z', true, '{}');
		INSERT INTO client_playback_references (user_id, auth_session_id, application_client_id, device_id, client_nonce, play_session_id) VALUES
			('settings-migration-user', 'settings-migration-auth', NULL, 'settings-migration-device', 'preserved-reference', 'settings-migration-play'),
			('settings-migration-user', 'settings-migration-auth', NULL, 'settings-migration-device', 'preserved-tombstone', NULL),
			(NULL, 'settings-migration-key', 'settings-migration-key-client', 'settings-server', 'preserved-key-reference', 'settings-migration-key-play');
		INSERT INTO encoding_jobs (id, user_id, auth_session_id, application_client_id, device_id, play_session_id,
			item_id, media_source_id, source_stamp, plan, state, output_bytes, created_at, updated_at, last_access_at) VALUES
			(repeat('d', 32), 'settings-migration-user', 'settings-migration-auth', NULL, 'settings-migration-device',
			 'settings-migration-play', 'settings-migration-item', 'settings-migration-source', 'preserved-source',
			 '{"Container":"mp4"}', 'completed', 4096, '2024-01-01T00:00:00Z', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z'),
			(repeat('e', 32), NULL, 'settings-migration-key', 'settings-migration-key-client', 'settings-server',
			 'settings-migration-key-play', 'settings-migration-item', 'settings-migration-source', 'preserved-key-source',
			 '{}', 'queued', 0, '2024-01-01T00:00:00Z', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z');
		INSERT INTO scan_jobs (id, library_id, status, error, scanned, added, updated, force_probe, started_at, finished_at)
		VALUES ('settings-migration-legacy-scan', 'settings-migration-library', 'Completed', 'Preserved diagnostic',
			11, 7, 4, true, '2025-01-01T00:00:00Z', '2025-01-01T01:00:00Z');
		INSERT INTO task_definitions (id, key, name, revision, schedule_timezone)
		VALUES (repeat('1', 32), 'settings-migration-task', 'Settings Migration Task', 5, 'UTC');
		INSERT INTO task_triggers (id, task_id, schedule_revision, position, kind, time_of_day_ticks, timezone, next_fire_at)
		VALUES (repeat('2', 32), repeat('1', 32), 5, 0, 'daily', 0, 'UTC', '2026-01-03T00:00:00Z');
		INSERT INTO task_runs (id, task_id, state, source, request_id, request_fingerprint, actor_user_id, actor_session_id,
			actor_kind, task_key, task_name, trigger_id, trigger_revision, scheduled_for, started_at, finished_at,
			total_children, terminal_children, completed_children, scanned, added, updated)
		VALUES (repeat('3', 32), repeat('1', 32), 'completed', 'schedule', 'settings-migration-request',
			decode(repeat('ab', 32), 'hex'), 'settings-migration-user', 'settings-migration-auth', 'emby',
			'settings-migration-task', 'Settings Migration Task', repeat('2', 32), 5, '2026-01-02T00:00:00Z',
			'2026-01-02T00:00:01Z', '2026-01-02T00:00:02Z', 1, 1, 1, 9, 6, 3);
		INSERT INTO task_run_requests (task_id, request_id, run_id, fingerprint)
		VALUES (repeat('1', 32), 'settings-migration-request', repeat('3', 32), decode(repeat('ab', 32), 'hex'));
		INSERT INTO task_run_children (id, run_id, library_id, library_name, ordinal, state, scan_job_id,
			started_at, finished_at, scanned, added, updated)
		VALUES (repeat('4', 32), repeat('3', 32), 'settings-migration-library', 'Settings Migration Library',
			0, 'completed', 'settings-migration-task-scan', '2026-01-02T00:00:01Z', '2026-01-02T00:00:02Z', 9, 6, 3);
		INSERT INTO task_occurrences (id, task_id, trigger_id, schedule_revision, due_at, disposition, run_id)
		VALUES (repeat('5', 32), repeat('1', 32), repeat('2', 32), 5, '2026-01-02T00:00:00Z', 'admitted', repeat('3', 32));
		INSERT INTO scan_jobs (id, library_id, status, started_at, finished_at, task_child_id, scanned, added, updated)
		VALUES ('settings-migration-task-scan', 'settings-migration-library', 'Completed',
			'2026-01-02T00:00:01Z', '2026-01-02T00:00:02Z', repeat('4', 32), 9, 6, 3);
	`); err != nil {
		t.Fatalf("seed schema 19 settings migration preservation fixture: %v", err)
	}
}

func settingsMigrationTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []string {
	t.Helper()
	var tables []string
	if err := pool.QueryRow(ctx, `SELECT array_agg(tablename ORDER BY tablename)
		FROM pg_tables WHERE schemaname = current_schema()`).Scan(&tables); err != nil {
		t.Fatalf("read settings migration table inventory: %v", err)
	}
	return tables
}

// Compare all original rows and fields, including credentials, nullable owner
// references, exact bigint revisions, JSON, and timestamps. Never log snapshots.
func settingsMigrationSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) string {
	t.Helper()
	statement := `SELECT COALESCE(jsonb_agg(to_jsonb(original) ORDER BY to_jsonb(original)::text), '[]'::jsonb)::text FROM ` + pgx.Identifier{table}.Sanitize() + " original"
	if table == "schema_migrations" {
		statement += " WHERE version <= 19"
	}
	var snapshot string
	if err := pool.QueryRow(ctx, statement).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot settings migration table %s: %v", table, err)
	}
	return snapshot
}

func TestManagedSettingsMigrationPreservesEverySchema19Table(t *testing.T) {
	ctx, pool := settingsMigrationPool(t)
	settingsMigrationVersion19(t, ctx, pool)
	settingsMigrationSeedLegacy(t, ctx, pool)
	tables := settingsMigrationTables(t, ctx, pool)
	if len(tables) != 27 || strings.Join(tables, " ") != settingsMigrationLegacyTables {
		t.Fatalf("schema 19 settings migration table inventory = %v, want all 27 historical tables", tables)
	}
	before := make(map[string]string, len(tables))
	for _, table := range tables {
		before[table] = settingsMigrationSnapshot(t, ctx, pool, table)
		if before[table] == "[]" {
			t.Fatalf("settings migration fixture left historical table %s empty", table)
		}
	}
	var initialSettings, initialHistory string
	for attempt := 1; attempt <= 2; attempt++ {
		if err := database.Migrate(ctx, pool); err != nil {
			t.Fatalf("settings migration attempt %d: %v", attempt, err)
		}
		if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 20 {
			t.Fatalf("settings schema version = %d, want 20: %v", version, err)
		}
		var name string
		if err := pool.QueryRow(ctx, "SELECT name FROM schema_migrations WHERE version = 20").Scan(&name); err != nil || name != "0020_managed_settings.sql" {
			t.Fatalf("managed settings migration history name = %q: %v", name, err)
		}
		var history string
		var historyCount int
		if err := pool.QueryRow(ctx, `SELECT count(*), jsonb_agg(to_jsonb(m) ORDER BY version)::text
			FROM schema_migrations m`).Scan(&historyCount, &history); err != nil || historyCount != 20 {
			t.Fatalf("settings migration history count = %d, want 20: %v", historyCount, err)
		}
		var total, defaults int
		if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE id = 1 AND revision = 1
			AND server_name IS NULL AND max_bitrate IS NULL AND max_width IS NULL
			AND max_height IS NULL AND max_audio_channels IS NULL
			AND created_at IS NOT NULL AND updated_at IS NOT NULL) FROM managed_settings`).Scan(&total, &defaults); err != nil || total != 1 || defaults != 1 {
			t.Fatalf("managed settings initial rows = %d, default rows = %d, want one revision-1 row with five NULL overrides: %v", total, defaults, err)
		}
		currentSettings := settingsMigrationSnapshot(t, ctx, pool, "managed_settings")
		if attempt == 1 {
			initialSettings, initialHistory = currentSettings, history
		} else if currentSettings != initialSettings || history != initialHistory {
			t.Error("reapplying managed settings migration changed its initial row or migration history")
		}
		upgradedTables := settingsMigrationTables(t, ctx, pool)
		var retainedTables []string
		for _, table := range upgradedTables {
			if table != "managed_settings" {
				retainedTables = append(retainedTables, table)
			}
		}
		if len(upgradedTables) != 28 || strings.Join(retainedTables, " ") != settingsMigrationLegacyTables {
			t.Errorf("settings migration did not add exactly its one table: %v", upgradedTables)
		}
		for _, table := range tables {
			if after := settingsMigrationSnapshot(t, ctx, pool, table); after != before[table] {
				t.Errorf("settings migration attempt %d changed original rows or fields in %s", attempt, table)
			}
		}
		var serverID, setupCompleted string
		if err := pool.QueryRow(ctx, `SELECT
			(SELECT value FROM server_settings WHERE key = 'server_id'),
			(SELECT value FROM server_settings WHERE key = 'setup_completed')`).Scan(&serverID, &setupCompleted); err != nil || serverID != "0123456789abcdef0123456789abcdef" || setupCompleted != "true" {
			t.Fatalf("settings migration changed the legacy server identity or setup-completion value: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE managed_settings SET revision = 7, server_name = 'Persisted Server',
		max_bitrate = 64000000, max_width = 3840, max_height = 2160, max_audio_channels = 6,
		updated_at = '2026-08-01T00:00:00Z' WHERE id = 1`); err != nil {
		t.Fatalf("persist explicit settings after upgrade: %v", err)
	}
	persisted := settingsMigrationSnapshot(t, ctx, pool, "managed_settings")
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat migration with explicit settings: %v", err)
	}
	if after := settingsMigrationSnapshot(t, ctx, pool, "managed_settings"); after != persisted {
		t.Error("repeated migration reset persisted explicit settings or their revision")
	}
}

func TestManagedSettingsMigrationEnforcesSingletonAndStorageBounds(t *testing.T) {
	ctx, pool := settingsMigrationPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("initialize managed settings constraint schema: %v", err)
	}
	for _, test := range []struct {
		name, statement, code string
	}{
		{"duplicate singleton", "INSERT INTO managed_settings (id) VALUES (1)", "23505"},
		{"second identity", "INSERT INTO managed_settings (id) VALUES (2)", "23514"},
		{"zero identity", "UPDATE managed_settings SET id = 0 WHERE id = 1", "23514"},
		{"null identity", "UPDATE managed_settings SET id = NULL WHERE id = 1", "23502"},
		{"zero revision", "UPDATE managed_settings SET revision = 0 WHERE id = 1", "23514"},
		{"negative revision", "UPDATE managed_settings SET revision = -1 WHERE id = 1", "23514"},
		{"null revision", "UPDATE managed_settings SET revision = NULL WHERE id = 1", "23502"},
		{"empty server name", "UPDATE managed_settings SET server_name = '' WHERE id = 1", "23514"},
		{"server name above byte limit", "UPDATE managed_settings SET server_name = repeat('a', 129) WHERE id = 1", "23514"},
		{"multibyte server name above byte limit", "UPDATE managed_settings SET server_name = repeat(chr(233), 65) WHERE id = 1", "23514"},
		{"bitrate below minimum", "UPDATE managed_settings SET max_bitrate = 0 WHERE id = 1", "23514"},
		{"bitrate above maximum", "UPDATE managed_settings SET max_bitrate = 1000000001 WHERE id = 1", "23514"},
		{"width below minimum", "UPDATE managed_settings SET max_width = 0 WHERE id = 1", "23514"},
		{"width above maximum", "UPDATE managed_settings SET max_width = 8193 WHERE id = 1", "23514"},
		{"height below minimum", "UPDATE managed_settings SET max_height = 0 WHERE id = 1", "23514"},
		{"height above maximum", "UPDATE managed_settings SET max_height = 8193 WHERE id = 1", "23514"},
		{"channels below minimum", "UPDATE managed_settings SET max_audio_channels = 0 WHERE id = 1", "23514"},
		{"channels above maximum", "UPDATE managed_settings SET max_audio_channels = 9 WHERE id = 1", "23514"},
		{"null created timestamp", "UPDATE managed_settings SET created_at = NULL WHERE id = 1", "23502"},
		{"null updated timestamp", "UPDATE managed_settings SET updated_at = NULL WHERE id = 1", "23502"},
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
				t.Fatalf("expected managed settings PostgreSQL constraint %s, got error type %T", test.code, err)
			}
			if databaseError.Code != test.code {
				t.Fatalf("managed settings constraint code = %s (%s), want %s", databaseError.Code, databaseError.ConstraintName, test.code)
			}
		})
	}
	for _, test := range []struct{ name, statement string }{
		{"inclusive lower bounds", `UPDATE managed_settings SET server_name = 'A', max_bitrate = 1,
			max_width = 1, max_height = 1, max_audio_channels = 1, revision = 1 WHERE id = 1`},
		{"inclusive upper bounds", `UPDATE managed_settings SET server_name = repeat('a', 128), max_bitrate = 1000000000,
			max_width = 8192, max_height = 8192, max_audio_channels = 8, revision = 9223372036854775807 WHERE id = 1`},
		{"multibyte name at byte limit", "UPDATE managed_settings SET server_name = repeat(chr(233), 64) WHERE id = 1"},
		{"nullable overrides", `UPDATE managed_settings SET server_name = NULL, max_bitrate = NULL,
			max_width = NULL, max_height = NULL, max_audio_channels = NULL, revision = 2 WHERE id = 1`},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			result, err := tx.Exec(ctx, test.statement)
			if err != nil || result.RowsAffected() != 1 {
				t.Fatalf("valid managed settings boundary was rejected: rows=%d error=%v", result.RowsAffected(), err)
			}
		})
	}
}

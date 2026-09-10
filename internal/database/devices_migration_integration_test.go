package database_test

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// The baseline applies the published schema instead of removing columns from a
// current database. The same file inventory identifies the current target.
//
//go:embed migrations/*.sql
var deviceMigrationFiles embed.FS

func deviceVersion16Baseline(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	applicationKeyVersion15Baseline(t, ctx, pool)
	const name = "0016_application_keys.sql"
	content, err := deviceMigrationFiles.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatalf("read historical device baseline migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(content)); err != nil {
		t.Fatalf("apply historical device baseline migration: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES (16, $1)", name); err != nil {
		t.Fatal(err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 16 {
		t.Fatalf("device migration baseline = %d, want 16, error=%v", version, err)
	}
	var devicesExist, generationColumnExists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('devices') IS NOT NULL,
		EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'sessions'::regclass
		AND attname = 'device_registry_id' AND NOT attisdropped)`).Scan(&devicesExist, &generationColumnExists); err != nil {
		t.Fatal(err)
	}
	if devicesExist || generationColumnExists {
		t.Fatal("historical device baseline already contained device registration state")
	}
}

func currentDeviceMigrationVersion(t *testing.T) int64 {
	t.Helper()
	entries, err := deviceMigrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var latest int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		version, err := strconv.ParseInt(prefix, 10, 64)
		if !ok || err != nil {
			t.Fatalf("invalid device migration test inventory entry %q", entry.Name())
		}
		if version > latest {
			latest = version
		}
	}
	if latest < 17 {
		t.Fatal("device migration is missing from the embedded current schema")
	}
	return latest
}

type deviceLegacyTable struct {
	name     string
	columns  []string
	snapshot string
}

// Capture every existing column of every baseline table. Later schema additions
// may create tables or append columns, but every original value and row remains
// compared, including bytea credentials, JSON, nullable fields, and timestamps.
func captureDeviceLegacyTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []deviceLegacyTable {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT relation.relname, attribute.attname
		FROM pg_class relation JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
		JOIN pg_attribute attribute ON attribute.attrelid = relation.oid
		WHERE namespace.nspname = current_schema() AND relation.relkind = 'r'
		AND attribute.attnum > 0 AND NOT attribute.attisdropped
		ORDER BY relation.relname, attribute.attnum`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []deviceLegacyTable
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if len(tables) == 0 || tables[len(tables)-1].name != table {
			tables = append(tables, deviceLegacyTable{name: table})
		}
		tables[len(tables)-1].columns = append(tables[len(tables)-1].columns, column)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(tables) == 0 {
		t.Fatal("device migration baseline has no tables to preserve")
	}
	for index := range tables {
		tables[index].snapshot = deviceLegacyTableSnapshot(t, ctx, pool, tables[index])
	}
	return tables
}

func deviceLegacyTableSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table deviceLegacyTable) string {
	t.Helper()
	quoted := make([]string, len(table.columns))
	for index, column := range table.columns {
		quoted[index] = pgx.Identifier{column}.Sanitize()
	}
	statement := "SELECT " + strings.Join(quoted, ", ") + " FROM " + pgx.Identifier{table.name}.Sanitize()
	if table.name == "schema_migrations" {
		statement += " WHERE version <= 16"
	}
	var snapshot string
	err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(legacy_row)
		ORDER BY to_jsonb(legacy_row)::text), '[]'::jsonb)::text FROM (`+statement+`) legacy_row`).Scan(&snapshot)
	if err != nil {
		t.Fatalf("snapshot original columns of device migration table %s: %v", table.name, err)
	}
	return snapshot
}

func assertDeviceLegacyTablesPreserved(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tables []deviceLegacyTable) {
	t.Helper()
	for _, table := range tables {
		if after := deviceLegacyTableSnapshot(t, ctx, pool, table); after != table.snapshot {
			// Do not print snapshots: they intentionally include secret storage.
			t.Errorf("device migration changed original rows or fields in %s", table.name)
		}
	}
}

type deviceLegacyLogin struct {
	id, userID, kind, reportedID, name, app, version string
	createdAt, lastSeenAt, expiresAt, revokedAt      string
}

func seedDeviceLegacyLogins(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []deviceLegacyLogin {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO server_settings (key, value, created_at, updated_at)
		VALUES ('server_id', '0123456789abcdef0123456789abcdef', '2020-01-01T00:00:00Z', '2020-01-02T00:00:00Z'),
		('setup_completed', 'true', '2020-01-01T00:00:00Z', '2020-01-02T00:00:00Z');
		INSERT INTO users (id, name, normalized_name, password_hash, is_administrator, is_disabled, policy, configuration)
		VALUES ('device-user-a', 'Historical Administrator', 'historical administrator', 'synthetic-admin-digest', true, false,
		'{"EnableAllFolders":false,"EnabledFolders":["device-migration-library"]}', '{"PreferredAudioLanguage":"eng"}'),
		('device-user-b', 'Historical Disabled Viewer', 'historical disabled viewer', 'synthetic-viewer-digest', false, true,
		'{"EnablePlaybackRemuxing":false}', '{"PreferredSubtitleLanguage":"fra"}')`); err != nil {
		t.Fatal(err)
	}
	// Different activity, creation, and ID winners isolate each tie-break rule.
	// In particular, the newest Shared-ID record is revoked and belongs to a
	// disabled account, while the oldest record has already expired.
	logins := []deviceLegacyLogin{
		{"device-shared-first", "device-user-a", "emby", "Shared-ID", "Earliest Name", "Earliest App", "0.1", "2020-01-01T00:00:00Z", "2021-01-01T00:00:00Z", "2022-01-01T00:00:00Z", ""},
		{"device-shared-z", "device-user-a", "emby", "Shared-ID", "Later Creation", "Creation Decoy", "0.2", "2024-01-01T00:00:00Z", "2025-05-01T00:00:00Z", "2035-01-01T00:00:00Z", ""},
		{"device-shared-winner", "device-user-b", "emby", "Shared-ID", "Latest Activity", "Activity Winner", "3.0", "2021-01-01T00:00:00Z", "2025-06-01T00:00:00Z", "2035-01-01T00:00:00Z", "2025-07-01T00:00:00Z"},
		{"device-created-z", "device-user-a", "emby", "TieCreated", "Older Creation", "Creation Loser", "1.0", "2021-01-01T00:00:00Z", "2025-06-01T00:00:00Z", "2035-01-01T00:00:00Z", ""},
		{"device-created-a", "device-user-b", "emby", "TieCreated", "Latest Creation", "Creation Winner", "2.0", "2024-01-01T00:00:00Z", "2025-06-01T00:00:00Z", "2035-01-01T00:00:00Z", ""},
		{"device-tie-a", "device-user-a", "emby", "TieID", "Lower ID", "ID Loser", "1.0", "2022-01-01T00:00:00Z", "2025-06-01T00:00:00Z", "2035-01-01T00:00:00Z", ""},
		{"device-tie-z", "device-user-b", "emby", "TieID", "Higher ID", "ID Winner", "2.0", "2022-01-01T00:00:00Z", "2025-06-01T00:00:00Z", "2035-01-01T00:00:00Z", ""},
		{"device-lower-case", "device-user-a", "emby", "shared-id", "Lowercase Device", "Case App", "1.0", "2023-01-01T00:00:00Z", "2024-01-01T00:00:00Z", "2035-01-01T00:00:00Z", ""},
		{"device-spaced", "device-user-a", "emby", " Shared-ID ", "Spaced Device", "Space App", "1.0", "2023-01-01T00:00:00Z", "2024-01-01T00:00:00Z", "2035-01-01T00:00:00Z", ""},
		{"device-trailing-space", "device-user-a", "emby", "Shared-ID ", "Trailing Space Device", "Trailing App", "1.0", "2023-01-01T00:00:00Z", "2024-01-01T00:00:00Z", "2035-01-01T00:00:00Z", ""},
		{"device-numeric", "device-user-a", "emby", "2", "Numeric Report", "Numeric App", "1.0", "2023-01-01T00:00:00Z", "2024-01-01T00:00:00Z", "2035-01-01T00:00:00Z", ""},
		{"device-empty", "device-user-a", "emby", "", "Unidentified Device", "No ID App", "1.0", "2023-01-01T00:00:00Z", "2024-01-01T00:00:00Z", "2035-01-01T00:00:00Z", ""},
		{"device-admin-shared", "device-user-a", "admin", "Shared-ID", "Excluded Native Browser", "Excluded Native App", "9.0", "2024-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "2035-01-01T00:00:00Z", ""},
		{"device-admin-only", "device-user-a", "admin", "native-only", "Native Only Browser", "Native Only App", "9.1", "2024-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "2035-01-01T00:00:00Z", ""},
		{"device-application-key", "", "application_key", "Shared-ID", "Excluded Application Server", "Excluded Key App", "10.0", "2024-01-01T00:00:00Z", "2027-01-01T00:00:00Z", "", ""},
	}
	for index, login := range logins {
		if _, err := pool.Exec(ctx, `INSERT INTO sessions
			(id, user_id, token_hash, kind, device_id, device_name, client_name, client_version,
			created_at, last_seen_at, expires_at, revoked_at, client_capabilities)
			VALUES ($1, NULLIF($2, ''), decode(repeat($3, 32), 'hex'), $4, $5, $6, $7, $8,
			$9::timestamptz, $10::timestamptz, NULLIF($11, '')::timestamptz, NULLIF($12, '')::timestamptz,
			'{"PlayableMediaTypes":["Video"],"SupportsMediaControl":true}')`, login.id, login.userID,
			fmt.Sprintf("%02x", index+1), login.kind, login.reportedID, login.name, login.app, login.version,
			login.createdAt, login.lastSeenAt, login.expiresAt, login.revokedAt); err != nil {
			t.Fatalf("seed historical device login %s: %v", login.id, err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO application_keys
		(credential_id, secret_ciphertext, created_by, last_used_at, ip_address)
		VALUES ('device-application-key', decode('010203040506', 'hex'), 'device-user-a', '2027-01-01T00:00:00Z', '192.0.2.80');
		INSERT INTO application_key_clients (id, credential_id, client_name, device_id, device_name,
		client_version, created_at, last_seen_at, client_capabilities)
		VALUES ('device-key-default-context', 'device-application-key', 'Excluded Key App', 'Shared-ID', 'Excluded Application Server',
		'10.0', '2024-01-01T00:00:00Z', '2027-01-01T00:00:00Z', '{"SupportsMediaControl":true}'),
		('device-key-other-context', 'device-application-key', 'Other Context App', 'context-only', 'Context Only Device',
		'1.0', '2024-01-01T00:00:00Z', '2027-01-01T00:00:00Z', '{}')`); err != nil {
		t.Fatalf("seed historical application-key device isolation: %v", err)
	}
	return logins
}

func seedDeviceMigrationHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id, name, collection_type)
		VALUES ('device-migration-library', 'Preserved Device Library', 'movies');
		INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
		VALUES ('device-migration-root', 'device-migration-library', '/synthetic/device-library', '/synthetic', 'device-library');
		INSERT INTO items (id, library_id, root_id, name, sort_name, type, path, relative_path, media)
		VALUES ('device-migration-item', 'device-migration-library', 'device-migration-root', 'Preserved Movie', 'preserved movie',
		'Movie', '/synthetic/device-library/movie.mp4', 'movie.mp4', '{"Container":"mp4","DurationTicks":90000000}');
		INSERT INTO scan_jobs (id, library_id, status, error, scanned, added, updated, force_probe)
		VALUES ('device-migration-scan', 'device-migration-library', 'Completed', 'Preserved diagnostic', 11, 7, 4, true);
		INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, play_count, is_favorite, last_played_at)
		VALUES ('device-user-a', 'device-migration-item', 12345, 7, true, '2025-01-01T00:00:00Z');
		INSERT INTO play_sessions (id, user_id, auth_session_id, application_client_id, device_id, item_id, media_source_id,
		state, position_ticks, duration_ticks, counted, expires_at, client_correlated, player_state)
		VALUES ('device-migration-play-expired', 'device-user-a', 'device-shared-first', NULL, 'Shared-ID', 'device-migration-item',
		'device-migration-source', 'Expired', 12345, 90000000, true, '2022-01-01T00:00:00Z', true, '{"CanSeek":true,"IsMuted":false}'),
		('device-migration-play-revoked', 'device-user-b', 'device-shared-winner', NULL, 'Shared-ID', 'device-migration-item',
		'device-migration-source', 'Stopped', 23456, 90000000, true, '2025-07-01T00:00:00Z', true, '{}'),
		('device-migration-play-key', NULL, 'device-application-key', 'device-key-default-context', 'Shared-ID', 'device-migration-item',
		'device-migration-source', 'Prepared', 0, 90000000, false, '2035-01-01T00:00:00Z', true, '{}');
		INSERT INTO client_playback_references (user_id, auth_session_id, application_client_id, device_id, client_nonce, play_session_id)
		VALUES ('device-user-a', 'device-shared-first', NULL, 'Shared-ID', 'expired-login-reference', 'device-migration-play-expired'),
		('device-user-b', 'device-shared-winner', NULL, 'Shared-ID', 'revoked-login-reference', 'device-migration-play-revoked'),
		('device-user-a', 'device-shared-first', NULL, 'Shared-ID', 'historical-nonce-tombstone', NULL),
		(NULL, 'device-application-key', 'device-key-default-context', 'Shared-ID', 'userless-reference', 'device-migration-play-key');
		INSERT INTO encoding_jobs (id, user_id, auth_session_id, application_client_id, device_id, play_session_id, item_id,
		media_source_id, source_stamp, plan, state, output_bytes, created_at, updated_at, last_access_at)
		VALUES (repeat('d', 32), 'device-user-a', 'device-shared-first', NULL, 'Shared-ID', 'device-migration-play-expired',
		'device-migration-item', 'device-migration-source', 'preserved-ordinary-source', '{}', 'completed', 4096,
		'2021-01-01T00:00:00Z', '2021-01-02T00:00:00Z', '2021-01-02T00:00:00Z'),
		(repeat('e', 32), NULL, 'device-application-key', 'device-key-default-context', 'Shared-ID', 'device-migration-play-key',
		'device-migration-item', 'device-migration-source', 'preserved-userless-source', '{}', 'queued', 0,
		'2025-01-01T00:00:00Z', '2025-01-02T00:00:00Z', '2025-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("seed retained device migration playback history: %v", err)
	}
}

type migratedOrdinaryDevice struct {
	id                             int64
	reportedID, name, app, version string
	lastUserID, ipAddress          string
	createdAt, lastSeenAt          time.Time
	customName                     *string
	revision                       int64
	deletedAt                      *time.Time
}

func readMigratedOrdinaryDevices(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]migratedOrdinaryDevice {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT id, reported_device_id, reported_name, app_name, app_version, last_user_id,
		created_at, last_seen_at, ip_address, custom_name, revision, deleted_at FROM devices WHERE id > 1 ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]migratedOrdinaryDevice)
	for rows.Next() {
		var device migratedOrdinaryDevice
		if err := rows.Scan(&device.id, &device.reportedID, &device.name, &device.app, &device.version, &device.lastUserID,
			&device.createdAt, &device.lastSeenAt, &device.ipAddress, &device.customName, &device.revision, &device.deletedAt); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if _, exists := result[device.reportedID]; exists {
			t.Errorf("device migration created duplicate current generations for reported identifier %q", device.reportedID)
		}
		result[device.reportedID] = device
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func deviceMigrationState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'devices', (SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM devices d),
		'assignments', (SELECT jsonb_agg(jsonb_build_array(id, device_registry_id) ORDER BY id) FROM sessions))::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestMigrateDevicesPreservesVersion16HistoryAndGroupsOrdinaryLogins(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	deviceVersion16Baseline(t, ctx, pool)
	logins := seedDeviceLegacyLogins(t, ctx, pool)
	seedDeviceMigrationHistory(t, ctx, pool)
	legacy := captureDeviceLegacyTables(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade version 16 device registry: %v", err)
	}
	wantVersion := currentDeviceMigrationVersion(t)
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != wantVersion {
		t.Fatalf("device upgrade version = %d, want current %d, error=%v", version, wantVersion, err)
	}
	var migrationName string
	if err := pool.QueryRow(ctx, "SELECT name FROM schema_migrations WHERE version = 17").Scan(&migrationName); err != nil || migrationName != "0017_devices.sql" {
		t.Fatalf("device migration did not record its actual version 17 execution: name=%q error=%v", migrationName, err)
	}
	assertDeviceLegacyTablesPreserved(t, ctx, pool, legacy)
	devices := readMigratedOrdinaryDevices(t, ctx, pool)
	expectedWinners := map[string]string{
		"Shared-ID": "device-shared-winner", "TieCreated": "device-created-a", "TieID": "device-tie-z",
		"shared-id": "device-lower-case", " Shared-ID ": "device-spaced", "Shared-ID ": "device-trailing-space", "2": "device-numeric",
	}
	if len(devices) != len(expectedWinners) {
		t.Fatalf("ordinary device generations = %d, want %d exact reported identifiers", len(devices), len(expectedWinners))
	}
	byLoginID := make(map[string]deviceLegacyLogin)
	for _, login := range logins {
		byLoginID[login.id] = login
	}
	for reportedID, winnerID := range expectedWinners {
		device, exists := devices[reportedID]
		if !exists {
			t.Errorf("device migration changed or omitted exact reported identifier %q", reportedID)
			continue
		}
		winner := byLoginID[winnerID]
		firstCreated := winner.createdAt
		for _, login := range logins {
			if login.kind == "emby" && login.reportedID == reportedID && login.createdAt < firstCreated {
				firstCreated = login.createdAt
			}
		}
		if device.id < 2 || device.name != winner.name || device.app != winner.app || device.version != winner.version || device.lastUserID != winner.userID ||
			device.createdAt.UTC().Format(time.RFC3339) != firstCreated || device.lastSeenAt.UTC().Format(time.RFC3339) != winner.lastSeenAt {
			t.Errorf("device %q lost its earliest creation, latest activity, or stable metadata winner", reportedID)
		}
		if device.customName != nil || device.deletedAt != nil || device.revision != 1 || device.ipAddress != "" {
			t.Errorf("device %q invented historical administrator options, peer address, or deletion state", reportedID)
		}
	}
	for _, login := range logins {
		var generation *int64
		if err := pool.QueryRow(ctx, "SELECT device_registry_id FROM sessions WHERE id = $1", login.id).Scan(&generation); err != nil {
			t.Fatalf("read historical login device assignment: %v", err)
		}
		if login.kind != "emby" || login.reportedID == "" {
			if generation != nil {
				t.Errorf("native, userless, or unidentified credential %s acquired an ordinary generation", login.id)
			}
			continue
		}
		if generation == nil || *generation != devices[login.reportedID].id {
			t.Errorf("ordinary credential %s did not retain its exact reported-ID generation, including historical revocation and expiry", login.id)
		}
	}
	var credentialCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions").Scan(&credentialCount); err != nil || credentialCount != len(logins) {
		t.Fatalf("device migration added or removed authentication credentials: count=%d error=%v", credentialCount, err)
	}
	// A device row cannot become a cascade root for authentication and playback.
	// These failed writes must leave every historical row unchanged.
	for _, constraintCase := range []struct {
		statement string
		code      string
	}{
		{"UPDATE sessions SET device_registry_id = $1 WHERE id = 'device-admin-shared'", "23514"},
		{"UPDATE sessions SET device_registry_id = $1 WHERE id = 'device-application-key'", "23514"},
		{"UPDATE sessions SET device_registry_id = $1 + 10000 WHERE id = 'device-shared-first'", "23503"},
		{"DELETE FROM devices WHERE id = $1", "23503"},
	} {
		_, err := pool.Exec(ctx, constraintCase.statement, devices["Shared-ID"].id)
		var constraint *pgconn.PgError
		if !errors.As(err, &constraint) || constraint.Code != constraintCase.code {
			t.Errorf("device registry ownership constraint failed to reject an unsafe relation change: %v", err)
		}
	}
	assertDeviceLegacyTablesPreserved(t, ctx, pool, legacy)
	stateBefore := deviceMigrationState(t, ctx, pool)
	historyBefore := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat device migration: %v", err)
	}
	if deviceMigrationState(t, ctx, pool) != stateBefore || migrationHistory(t, ctx, pool) != historyBefore {
		t.Error("repeated migration rewrote device generations, login assignments, or migration history")
	}
	assertDeviceLegacyTablesPreserved(t, ctx, pool, legacy)
}

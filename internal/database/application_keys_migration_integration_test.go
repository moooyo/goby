package database_test

import (
	"context"
	"embed"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

//go:embed migrations/0013_managed_users.sql migrations/0014_item_metadata.sql migrations/0015_scan_force_probe.sql
var applicationKeyBaselineFiles embed.FS

func applicationKeyVersion15Baseline(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	managedUsersVersion12Baseline(t, ctx, pool)
	for index, name := range []string{"0013_managed_users.sql", "0014_item_metadata.sql", "0015_scan_force_probe.sql"} {
		content, err := applicationKeyBaselineFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatalf("read historical application-key baseline migration: %v", err)
		}
		if _, err := pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply historical application-key baseline migration: %v", err)
		}
		if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", index+13, name); err != nil {
			t.Fatal(err)
		}
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 15 {
		t.Fatalf("application key historical baseline = %d, error=%v", version, err)
	}
}

func requireApplicationKeyConstraint(t *testing.T, err error, code string) {
	t.Helper()
	var constraint *pgconn.PgError
	if !errors.As(err, &constraint) || constraint.Code != code {
		t.Fatalf("application key schema accepted invalid ownership, expected %s: %v", code, err)
	}
}

// Version 15 already contains management revisions and all login metadata.
// Exclude the new device registry and nullable client-context columns; every
// old field, including secrets and timestamps, remains compared.
func applicationKeyLegacySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'settings', (SELECT jsonb_agg(to_jsonb(t) ORDER BY key) FROM server_settings t),
		'users', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM users t),
		'credentials', (SELECT jsonb_agg(to_jsonb(t) - 'device_registry_id' ORDER BY id) FROM sessions t),
		'libraries', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM libraries t),
		'roots', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM library_roots t),
		'items', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM items t),
		'playback', (SELECT jsonb_agg(to_jsonb(t) - 'application_client_id' ORDER BY id) FROM play_sessions t),
		'userdata', (SELECT jsonb_agg(to_jsonb(t) ORDER BY user_id, item_id) FROM user_item_data t),
		'encodings', (SELECT jsonb_agg(to_jsonb(t) - 'application_client_id' ORDER BY id) FROM encoding_jobs t),
		'references', (SELECT jsonb_agg(to_jsonb(t) - 'application_client_id' ORDER BY user_id, auth_session_id, device_id, client_nonce)
			FROM client_playback_references t))::text`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot historical application key upgrade data: %v", err)
	}
	return snapshot
}

func TestMigrateApplicationKeysPreservesLoginsAndUserlessPlaybackConstraints(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	applicationKeyVersion15Baseline(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, name, normalized_name, password_hash, is_administrator)
		VALUES ('key-legacy-user', 'Legacy User', 'legacy user', 'synthetic-password-digest', true),
		('key-audit-creator', 'Audit Creator', 'audit creator', 'synthetic-creator-digest', false);
		INSERT INTO sessions (id, user_id, token_hash, kind, client_name, device_id, device_name,
			client_version, created_at, expires_at, last_seen_at, revoked_at, client_capabilities) VALUES
		('key-legacy-admin', 'key-legacy-user', decode(repeat('21', 32), 'hex'), 'admin', 'Dashboard', 'admin-device', 'Browser',
			'1.0', '2025-01-01T00:00:00Z', '2030-01-01T00:00:00Z', '2025-02-01T00:00:00Z', NULL, '{}'),
		('key-legacy-emby', 'key-legacy-user', decode(repeat('22', 32), 'hex'), 'emby', 'Media app', 'media-device', 'Player',
			'2.0', '2025-01-01T00:00:00Z', '2030-01-01T00:00:00Z', '2025-03-01T00:00:00Z', NULL, '{"SupportsMediaControl":true}'),
		('key-legacy-revoked', 'key-legacy-user', decode(repeat('23', 32), 'hex'), 'emby', 'Old app', 'old-device', 'Player',
			'0.5', '2025-01-01T00:00:00Z', '2025-06-01T00:00:00Z', '2025-02-01T00:00:00Z', '2025-03-01T00:00:00Z', '{}');
		INSERT INTO libraries (id, name, collection_type) VALUES ('key-library', 'Legacy Library', 'movies');
		INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
		VALUES ('key-root', 'key-library', '/synthetic/key-library', '/synthetic', 'key-library');
		INSERT INTO items (id, library_id, root_id, name, sort_name, type, path, relative_path, media)
		VALUES ('key-item', 'key-library', 'key-root', 'Legacy Movie', 'legacy movie', 'Movie',
			'/synthetic/key-library/movie.mp4', 'movie.mp4', '{"Container":"mp4","DurationTicks":90000000}');
		INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, play_count, is_favorite)
		VALUES ('key-legacy-user', 'key-item', 1000, 4, true);
		INSERT INTO play_sessions (id, user_id, auth_session_id, device_id, item_id, media_source_id,
			state, duration_ticks, expires_at, client_correlated)
		VALUES ('play_key_legacy', 'key-legacy-user', 'key-legacy-emby', 'media-device', 'key-item', 'source_key-item',
			'Playing', 90000000, '2030-01-01T00:00:00Z', true);
		INSERT INTO client_playback_references (user_id, auth_session_id, device_id, client_nonce, play_session_id)
		VALUES ('key-legacy-user', 'key-legacy-emby', 'media-device', 'legacy-reference', 'play_key_legacy');
		INSERT INTO encoding_jobs (id, user_id, auth_session_id, device_id, play_session_id, item_id,
			media_source_id, source_stamp, plan, state, created_at, updated_at, last_access_at)
		VALUES (repeat('a', 32), 'key-legacy-user', 'key-legacy-emby', 'media-device', 'play_key_legacy',
			'key-item', 'source_key-item', 'legacy-source-stamp', '{}', 'completed',
			'2025-01-01T00:00:00Z', '2025-01-02T00:00:00Z', '2025-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("seed application key upgrade fixture: %v", err)
	}
	before := applicationKeyLegacySnapshot(t, ctx, pool)
	historyBefore := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate application credentials: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 26 {
		t.Fatalf("application key schema = %d, want 26, error=%v", version, err)
	}
	if after := applicationKeyLegacySnapshot(t, ctx, pool); after != before {
		t.Fatal("application key migration changed historical rows or credential material")
	}
	var registeredLogins int
	var registryMatches bool
	if err := pool.QueryRow(ctx, `SELECT count(*), bool_and(authentication.device_registry_id IS NOT NULL
		AND device.id >= 2 AND device.reported_device_id = authentication.device_id
		AND device.reported_name = authentication.device_name AND device.app_name = authentication.client_name
		AND device.app_version = authentication.client_version AND device.last_user_id = authentication.user_id)
		FROM sessions authentication LEFT JOIN devices device ON device.id = authentication.device_registry_id
		WHERE authentication.kind = 'emby'`).Scan(&registeredLogins, &registryMatches); err != nil || registeredLogins != 2 || !registryMatches {
		t.Fatalf("ordinary login device backfill lost its historical metadata: count=%d matched=%v error=%v", registeredLogins, registryMatches, err)
	}
	var administratorUnregistered bool
	if err := pool.QueryRow(ctx, "SELECT device_registry_id IS NULL FROM sessions WHERE id = 'key-legacy-admin'").Scan(&administratorUnregistered); err != nil || !administratorUnregistered {
		t.Fatalf("device migration registered a dashboard credential: %v", err)
	}
	var historyAfter string
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m WHERE version <= 15`).Scan(&historyAfter); err != nil || historyAfter != historyBefore {
		t.Fatalf("application key migration rewrote historical migration rows: %v", err)
	}
	for _, statement := range []string{
		"UPDATE sessions SET user_id = NULL WHERE id = 'key-legacy-emby'",
		"UPDATE sessions SET expires_at = NULL WHERE id = 'key-legacy-emby'",
		"UPDATE sessions SET user_id = NULL WHERE id = 'key-legacy-admin'",
		"UPDATE sessions SET kind = 'application_key' WHERE id = 'key-legacy-admin'",
	} {
		_, err := pool.Exec(ctx, statement)
		requireApplicationKeyConstraint(t, err, "23514")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id, token_hash, kind, client_name, device_id, device_name, client_version)
		VALUES ('key-userless', decode(repeat('31', 32), 'hex'), 'application_key', 'App', 'server-device', 'Server', '1.0');
		INSERT INTO application_key_devices (id, reported_device_id, reported_name, app_name, app_version)
		VALUES (1, 'server-device', 'Server', 'App', '1.0');
		INSERT INTO application_keys (credential_id, secret_ciphertext, created_by)
		VALUES ('key-userless', decode('123456', 'hex'), 'key-audit-creator');
		INSERT INTO application_key_clients (id, credential_id, client_name, device_id, device_name, client_version)
		VALUES ('key-default-client', 'key-userless', 'App', 'server-device', 'Server', '1.0'),
		('key-second-client', 'key-userless', 'Other App', 'server-device', 'Other Device', '2.0')`); err != nil {
		t.Fatalf("insert typed userless credential: %v", err)
	}
	var defaultDeviceMatches bool
	if err := pool.QueryRow(ctx, `SELECT application.reported_device_numeric_id = 1
		AND device.reported_device_id = authentication.device_id AND device.reported_name = authentication.device_name
		AND device.app_name = authentication.client_name AND device.app_version = authentication.client_version
		AND authentication.device_registry_id IS NULL
		FROM application_keys application JOIN sessions authentication ON authentication.id = application.credential_id
		JOIN application_key_devices device ON device.id = application.reported_device_numeric_id
		WHERE authentication.id = 'key-userless'`).Scan(&defaultDeviceMatches); err != nil || !defaultDeviceMatches {
		t.Fatalf("default application device lost the credential's server identity: %v", err)
	}
	for _, statement := range []string{
		"UPDATE sessions SET user_id = 'key-legacy-user' WHERE id = 'key-userless'",
		"UPDATE sessions SET expires_at = now() + interval '1 day' WHERE id = 'key-userless'",
		"UPDATE sessions SET kind = 'emby' WHERE id = 'key-userless'",
	} {
		_, err := pool.Exec(ctx, statement)
		requireApplicationKeyConstraint(t, err, "23514")
	}
	_, err := pool.Exec(ctx, `INSERT INTO sessions (id, token_hash, kind) VALUES ('key-duplicate-hash', decode(repeat('22', 32), 'hex'), 'application_key')`)
	requireApplicationKeyConstraint(t, err, "23505")
	_, err = pool.Exec(ctx, `INSERT INTO application_keys (credential_id, secret_ciphertext) VALUES ('key-userless', decode('77', 'hex'))`)
	requireApplicationKeyConstraint(t, err, "23505")
	_, err = pool.Exec(ctx, "UPDATE application_keys SET secret_ciphertext = NULL")
	requireApplicationKeyConstraint(t, err, "23502")
	_, err = pool.Exec(ctx, "UPDATE application_keys SET reported_device_numeric_id = NULL")
	requireApplicationKeyConstraint(t, err, "23502")
	_, err = pool.Exec(ctx, "UPDATE application_keys SET reported_device_numeric_id = 9")
	requireApplicationKeyConstraint(t, err, "23503")
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = 'key-audit-creator'"); err != nil {
		t.Fatal(err)
	}
	var creatorRemoved bool
	if err := pool.QueryRow(ctx, "SELECT created_by IS NULL FROM application_keys WHERE credential_id = 'key-userless'").Scan(&creatorRemoved); err != nil || !creatorRemoved {
		t.Fatalf("creator audit did not survive account deletion: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO play_sessions (id, auth_session_id, application_client_id, device_id, item_id, media_source_id, state, duration_ticks, expires_at)
		VALUES ('play_key_userless', 'key-userless', 'key-default-client', 'server-device', 'key-item', 'source_key-item', 'Prepared', 90000000, '2030-01-01T00:00:00Z'),
		('play_key_other_client', 'key-userless', 'key-second-client', 'server-device', 'key-item', 'source_key-item', 'Prepared', 90000000, '2030-01-01T00:00:00Z');
		INSERT INTO client_playback_references (auth_session_id, application_client_id, device_id, client_nonce, play_session_id)
		VALUES ('key-userless', 'key-default-client', 'server-device', 'key-reference', 'play_key_userless'),
		('key-userless', 'key-second-client', 'server-device', 'key-reference', 'play_key_other_client');
		INSERT INTO encoding_jobs (id, auth_session_id, application_client_id, device_id, play_session_id, item_id,
			media_source_id, source_stamp, plan, state, created_at, updated_at, last_access_at)
		VALUES (repeat('b', 32), 'key-userless', 'key-default-client', 'server-device', 'play_key_userless',
			'key-item', 'source_key-item', 'key-source-stamp', '{}', 'queued',
			'2025-01-01T00:00:00Z', '2025-01-02T00:00:00Z', '2025-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("insert userless playback ownership: %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO client_playback_references (auth_session_id, application_client_id, device_id, client_nonce)
		VALUES ('key-userless', 'key-default-client', 'server-device', 'key-reference')`)
	requireApplicationKeyConstraint(t, err, "23505")
	_, err = pool.Exec(ctx, `INSERT INTO play_sessions (id, auth_session_id, application_client_id, device_id, item_id, media_source_id, state, duration_ticks, expires_at)
		VALUES ('play_key_duplicate', 'key-userless', 'key-default-client', 'server-device', 'key-item', 'source_key-item', 'Prepared', 90000000, '2030-01-01T00:00:00Z')`)
	requireApplicationKeyConstraint(t, err, "23505")
	_, err = pool.Exec(ctx, `INSERT INTO application_key_clients (id, credential_id, client_name, device_id, device_name, client_version)
		VALUES ('key-duplicate-client', 'key-userless', 'App', 'server-device', 'Renamed', '3.0')`)
	requireApplicationKeyConstraint(t, err, "23505")
	_, err = pool.Exec(ctx, `UPDATE play_sessions SET application_client_id = 'missing-client' WHERE id = 'play_key_userless'`)
	requireApplicationKeyConstraint(t, err, "23503")
	if _, err := pool.Exec(ctx, "DELETE FROM application_key_clients WHERE id = 'key-second-client'"); err != nil {
		t.Fatal(err)
	}
	var retired int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM play_sessions WHERE id = 'play_key_other_client')
		+ (SELECT count(*) FROM client_playback_references WHERE application_client_id = 'key-second-client')`).Scan(&retired); err != nil || retired != 0 {
		t.Fatalf("client context deletion did not retire its playback ownership: remaining=%d error=%v", retired, err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM sessions WHERE id = 'key-userless'"); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM application_keys)
		+ (SELECT count(*) FROM application_key_clients)
		+ (SELECT count(*) FROM play_sessions WHERE auth_session_id = 'key-userless')
		+ (SELECT count(*) FROM client_playback_references WHERE auth_session_id = 'key-userless')
		+ (SELECT count(*) FROM encoding_jobs WHERE auth_session_id = 'key-userless')`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("shared credential deletion lost existing playback cascades: remaining=%d error=%v", remaining, err)
	}
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM play_sessions WHERE id = 'play_key_legacy')
		+ (SELECT count(*) FROM encoding_jobs WHERE auth_session_id = 'key-legacy-emby')
		+ (SELECT count(*) FROM client_playback_references WHERE auth_session_id = 'key-legacy-emby')`).Scan(&remaining); err != nil || remaining != 3 {
		t.Fatalf("key deletion damaged ordinary playback ownership: remaining=%d error=%v", remaining, err)
	}
	history := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if migrationHistory(t, ctx, pool) != history {
		t.Fatal("repeated application key migration changed history")
	}
}

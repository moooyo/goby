package database_test

import (
	"context"
	"embed"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// Start from the published version 12 schema, never from a downgraded current
// schema whose new columns or constraints would hide an upgrade regression.
//
//go:embed migrations/0011_encoding_vod_plans.sql migrations/0012_client_playback_references.sql
var managedUsersBaselineFiles embed.FS

func managedUsersVersion12Baseline(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	encodingVODVersion10Baseline(t, ctx, pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin version 12 fixture: %v", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	for index, name := range []string{"0011_encoding_vod_plans.sql", "0012_client_playback_references.sql"} {
		content, err := managedUsersBaselineFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatalf("read historical migration %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply historical migration %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", int64(index+11), name); err != nil {
			t.Fatalf("record historical migration %s: %v", name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit version 12 fixture: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 12 {
		t.Fatalf("historical schema version = %d, want 12, error = %v", version, err)
	}
	var revisionExists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_attribute
		WHERE attrelid = 'users'::regclass AND attname = 'management_revision' AND NOT attisdropped)`).Scan(&revisionExists); err != nil || revisionExists {
		t.Fatalf("version 12 fixture already has the managed revision column: exists = %v, error = %v", revisionExists, err)
	}
}

// Exclude new management, device registry, and client-context columns; old fields, including
// password digests, token digests, timestamps, policy, and configuration, stays
// in the exact comparison. Snapshot contents are never printed on failure.
func managedUsersLegacySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'settings', (SELECT jsonb_agg(to_jsonb(t) ORDER BY key) FROM server_settings t),
		'users', (SELECT jsonb_agg(to_jsonb(t) - 'management_revision' ORDER BY id) FROM users t),
		'auth', (SELECT jsonb_agg(to_jsonb(t) - 'device_registry_id' ORDER BY id) FROM sessions t),
		'libraries', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM libraries t),
		'roots', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM library_roots t),
		'items', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM items t),
		'play', (SELECT jsonb_agg(to_jsonb(t) - 'application_client_id' ORDER BY id) FROM play_sessions t),
		'userdata', (SELECT jsonb_agg(to_jsonb(t) ORDER BY user_id, item_id) FROM user_item_data t),
		'encodings', (SELECT jsonb_agg(to_jsonb(t) - 'application_client_id' ORDER BY id) FROM encoding_jobs t),
		'references', (SELECT jsonb_agg(to_jsonb(t) - 'application_client_id' ORDER BY user_id, auth_session_id, device_id, client_nonce)
			FROM client_playback_references t)
	)::text`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot managed user migration fixture: %v", err)
	}
	return snapshot
}

func TestMigrateManagedUsersPreservesVersion12DataAndInitializesRevisions(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	managedUsersVersion12Baseline(t, ctx, pool)
	// Synthetic credentials exercise storage preservation without authenticating
	// or invoking an application path that could update activity or user state.
	if _, err := pool.Exec(ctx, `
		INSERT INTO server_settings (key, value, created_at, updated_at) VALUES
			('server_id', '0123456789abcdef0123456789abcdef', '2025-12-01T00:00:00Z', '2025-12-02T00:00:00Z'),
			('setup_completed', 'true', '2025-12-01T00:00:00Z', '2025-12-02T00:00:00Z'),
			('managed-migration-setting', 'preserve-exactly', '2025-12-01T00:00:00Z', '2025-12-02T00:00:00Z');
		INSERT INTO users (id, name, normalized_name, password_hash, has_password,
			is_administrator, is_disabled, policy, configuration, created_at, updated_at) VALUES
			('managed-legacy-admin', 'Legacy Administrator', 'legacy administrator', 'fixture-admin-password-digest', true,
			true, false, '{"EnableAllFolders":false,"EnabledFolders":["managed-library"],"EnableMediaPlayback":true,
			"IsAdministrator":false,"CustomExtension":{"Keep":[1,true,"same"]}}'::jsonb,
			'{"AudioLanguagePreference":"eng","CustomConfiguration":[1,2]}'::jsonb,
			'2025-12-01T00:00:00Z', '2025-12-02T00:00:00Z'),
			('managed-legacy-guest', 'Legacy Guest', 'legacy guest', 'fixture-empty-password-digest', false,
			false, true, '{"EnableAllFolders":"invalid","EnabledFolders":[1],"EnableMediaPlayback":null}'::jsonb,
			'{"SubtitleLanguagePreference":"fra"}'::jsonb, '2025-12-03T00:00:00Z', '2025-12-04T00:00:00Z');
		INSERT INTO sessions (id, user_id, token_hash, kind, client_name, device_id, device_name,
			client_version, client_capabilities, created_at, expires_at, last_seen_at, revoked_at) VALUES
			('managed-auth-admin', 'managed-legacy-admin', decode(repeat('a1', 32), 'hex'), 'admin', 'Dashboard', 'admin-device', 'Browser',
			'1.0', '{}'::jsonb, '2025-12-01T00:00:00Z', '2030-01-01T00:00:00Z', '2025-12-02T00:00:00Z', NULL),
			('managed-auth-emby', 'managed-legacy-admin', decode(repeat('b2', 32), 'hex'), 'emby', 'Media Client', 'media-device', 'Player',
			'2.0', '{"PlayableMediaTypes":["Video"],"SupportedCommands":["PlayState"]}'::jsonb,
			'2025-12-01T00:00:00Z', '2030-01-01T00:00:00Z', '2025-12-03T00:00:00Z', NULL),
			('managed-auth-revoked', 'managed-legacy-guest', decode(repeat('c3', 32), 'hex'), 'emby', 'Old Client', 'old-device', 'Player',
			'0.9', '{}'::jsonb, '2025-12-01T00:00:00Z', '2026-01-01T00:00:00Z', '2025-12-04T00:00:00Z', '2025-12-05T00:00:00Z');
		INSERT INTO libraries (id, name, collection_type, created_at, last_scan_at)
			VALUES ('managed-library', 'Legacy Library', 'movies', '2025-12-01T00:00:00Z', '2025-12-02T00:00:00Z');
		INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
			VALUES ('managed-root', 'managed-library', '/synthetic/library', '/synthetic', 'library');
		INSERT INTO items (id, library_id, root_id, name, sort_name, type, path, relative_path, media,
			created_at, updated_at) VALUES ('managed-item', 'managed-library', 'managed-root', 'Legacy Movie', 'legacy movie',
			'Movie', '/synthetic/library/movie.mp4', 'movie.mp4', '{"Container":"mp4","DurationTicks":6000000000}'::jsonb,
			'2025-12-01T00:00:00Z', '2025-12-02T00:00:00Z');
		INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_at, updated_at)
			VALUES ('managed-legacy-admin', 'managed-item', 300000000, 7, true, false, '2026-01-01T00:00:00Z', '2026-01-01T00:00:01Z');
		INSERT INTO play_sessions (id, user_id, auth_session_id, device_id, item_id, media_source_id,
			state, position_ticks, duration_ticks, counted, created_at, updated_at, expires_at, started_at, player_state, client_correlated)
			VALUES ('play_managed_legacy', 'managed-legacy-admin', 'managed-auth-emby', 'media-device', 'managed-item', 'mediasource_managed-item',
			'Paused', 300000000, 6000000000, true, '2026-01-01T00:00:00Z', '2026-01-01T00:00:01Z', '2030-01-01T00:00:00Z',
			'2026-01-01T00:00:00Z', '{"PlayMethod":"Transcode","VolumeLevel":25,"CanSeek":true}'::jsonb, true);
		INSERT INTO encoding_jobs (id, user_id, auth_session_id, device_id, play_session_id, item_id,
			media_source_id, source_stamp, plan, state, output_bytes, created_at, updated_at, last_access_at)
			VALUES (repeat('d', 32), 'managed-legacy-admin', 'managed-auth-emby', 'media-device', 'play_managed_legacy', 'managed-item',
			'mediasource_managed-item', 'legacy-source-stamp', '{"Container":"ts","SegmentTimes":[0,3,6]}'::jsonb, 'completed', 8192,
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:01Z', '2026-01-01T00:00:02Z');
		INSERT INTO client_playback_references (user_id, auth_session_id, device_id, client_nonce, play_session_id, created_at) VALUES
			('managed-legacy-admin', 'managed-auth-emby', 'media-device', 'legacy-current-reference', 'play_managed_legacy', '2026-01-01T00:00:00Z'),
			('managed-legacy-admin', 'managed-auth-emby', 'media-device', 'legacy-reference-tombstone', NULL, '2026-01-01T00:00:01Z');
	`); err != nil {
		t.Fatalf("seed version 12 identity and playback state: %v", err)
	}
	before := managedUsersLegacySnapshot(t, ctx, pool)
	historyBefore := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade managed users from version 12: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 19 {
		t.Fatalf("managed user schema version = %d, want 19, error = %v", version, err)
	}
	if after := managedUsersLegacySnapshot(t, ctx, pool); after != before {
		t.Error("managed user migration changed historical identity, settings, catalog, or playback state")
	}
	var preservedHistory, migrationName string
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(t) ORDER BY version)::text
		FROM schema_migrations t WHERE version <= 12`).Scan(&preservedHistory); err != nil || preservedHistory != historyBefore {
		t.Errorf("managed user migration changed published migration history: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT name FROM schema_migrations WHERE version = 13").Scan(&migrationName); err != nil || migrationName != "0013_managed_users.sql" {
		t.Fatalf("managed user migration name = %q, error = %v", migrationName, err)
	}
	var users int
	var initialized bool
	if err := pool.QueryRow(ctx, "SELECT count(*), bool_and(management_revision = 1) FROM users").Scan(&users, &initialized); err != nil || users != 2 || !initialized {
		t.Fatalf("existing revisions were not initialized to one: users = %d, initialized = %v, error = %v", users, initialized, err)
	}
	for _, revision := range []int64{0, -1} {
		_, err := pool.Exec(ctx, "UPDATE users SET management_revision = $1 WHERE id = 'managed-legacy-admin'", revision)
		var pgError *pgconn.PgError
		if !errors.As(err, &pgError) || pgError.Code != "23514" {
			t.Fatalf("revision %d was not rejected by its check constraint: %v", revision, err)
		}
	}
	_, err := pool.Exec(ctx, "UPDATE users SET management_revision = NULL WHERE id = 'managed-legacy-admin'")
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != "23502" {
		t.Fatalf("NULL revision was not rejected by its not-null constraint: %v", err)
	}
	var revision int64
	if err := pool.QueryRow(ctx, "SELECT management_revision FROM users WHERE id = 'managed-legacy-admin'").Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("rejected revision writes changed the existing revision: revision = %d, error = %v", revision, err)
	}
	if after := managedUsersLegacySnapshot(t, ctx, pool); after != before {
		t.Error("rejected revision writes changed historical account or playback data")
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (id, name, normalized_name, password_hash)
		VALUES ('managed-new-user', 'New User', 'new user', 'fixture-new-password-digest')
		RETURNING management_revision`).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("new account revision default = %d, want 1, error = %v", revision, err)
	}
	// A counter beyond JavaScript's exact-integer range also verifies bigint
	// storage and ensures a repeated migration does not reset a mature revision.
	const advancedRevision int64 = 9007199254740993
	if _, err := pool.Exec(ctx, "UPDATE users SET management_revision = $1 WHERE id = 'managed-legacy-admin'", advancedRevision); err != nil {
		t.Fatalf("advance an existing account revision before repeated migration: %v", err)
	}
	beforeRepeat := managedUsersLegacySnapshot(t, ctx, pool)
	historyBeforeRepeat := migrationHistory(t, ctx, pool)
	var usersBeforeRepeat string
	if err := pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM users t").Scan(&usersBeforeRepeat); err != nil {
		t.Fatalf("snapshot revisions before repeated migration: %v", err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat managed user migration: %v", err)
	}
	var usersAfterRepeat string
	if err := pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM users t").Scan(&usersAfterRepeat); err != nil || usersAfterRepeat != usersBeforeRepeat {
		t.Errorf("repeated migration changed account data or existing revision values: %v", err)
	}
	if after := managedUsersLegacySnapshot(t, ctx, pool); after != beforeRepeat {
		t.Error("repeated managed user migration changed historical data")
	}
	if after := migrationHistory(t, ctx, pool); after != historyBeforeRepeat {
		t.Error("repeated managed user migration changed migration history")
	}
}

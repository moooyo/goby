package database_test

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

//go:embed migrations/0011_encoding_vod_plans.sql
var clientPlaybackVersion11Migration string

func clientPlaybackMigrationConstraint(t *testing.T, err error, code string) {
	t.Helper()
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != code {
		t.Fatalf("client playback constraint error = %v, want PostgreSQL %s", err, code)
	}
}

func clientPlaybackLegacySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'users', (SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
		'auth', (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM sessions a),
		'play', (SELECT jsonb_agg(to_jsonb(p) - 'client_correlated' ORDER BY id) FROM play_sessions p),
		'userdata', (SELECT jsonb_agg(to_jsonb(d) ORDER BY user_id, item_id) FROM user_item_data d)
	)::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestMigrateClientPlaybackReferencesPreservesDefaultsAndRetainsScopedTombstones(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	encodingVODVersion10Baseline(t, ctx, pool)
	if _, err := pool.Exec(ctx, clientPlaybackVersion11Migration); err != nil {
		t.Fatalf("apply historical version 11 migration: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES (11, '0011_encoding_vod_plans.sql')"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, normalized_name, password_hash) VALUES ('reference-user', 'Reference User', 'reference user', 'fixture-only');
		INSERT INTO sessions (id, user_id, token_hash, kind, device_id, expires_at) VALUES
			('reference-auth-a', 'reference-user', decode(repeat('a1', 32), 'hex'), 'emby', 'reference-device', now() + interval '1 day'),
			('reference-auth-b', 'reference-user', decode(repeat('b2', 32), 'hex'), 'emby', 'reference-device', now() + interval '1 day');
		INSERT INTO libraries (id, name, collection_type) VALUES ('reference-library', 'Reference Library', 'movies');
		INSERT INTO items (id, library_id, name, sort_name, type) VALUES ('reference-item', 'reference-library', 'Reference Item', 'reference item', 'Movie');
		INSERT INTO user_item_data (user_id, item_id, play_count) VALUES ('reference-user', 'reference-item', 3);
		INSERT INTO play_sessions (id, user_id, auth_session_id, device_id, item_id, media_source_id, state, duration_ticks, expires_at) VALUES
			('play_existing', 'reference-user', 'reference-auth-a', 'reference-device', 'reference-item', 'mediasource_reference-item', 'Playing', 6000000000, now() + interval '30 minutes'),
			('play_terminal', 'reference-user', 'reference-auth-a', 'reference-device', 'reference-item', 'mediasource_reference-item', 'Stopped', 6000000000, now());
	`); err != nil {
		t.Fatalf("seed version 11 playback records: %v", err)
	}
	before := clientPlaybackLegacySnapshot(t, ctx, pool)
	history := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade client playback references: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 12 {
		t.Fatalf("client playback schema version = %d, error = %v", version, err)
	}
	if after := clientPlaybackLegacySnapshot(t, ctx, pool); before != after {
		t.Error("client playback migration changed existing identity, playback, or user-data values")
	}
	var oldHistory string
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m WHERE version <= 11`).Scan(&oldHistory); err != nil || oldHistory != history {
		t.Error("client playback migration changed published migration history")
	}
	var correlated, references int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM play_sessions WHERE client_correlated").Scan(&correlated); err != nil || correlated != 0 {
		t.Errorf("existing sessions did not retain default matching: count = %d, error = %v", correlated, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM client_playback_references").Scan(&references); err != nil || references != 0 {
		t.Errorf("migration invented client references: count = %d, error = %v", references, err)
	}
	insertPlay := func(id, auth string, clientCorrelated bool) error {
		_, err := pool.Exec(ctx, `INSERT INTO play_sessions
			(id, user_id, auth_session_id, device_id, item_id, media_source_id, state, duration_ticks, expires_at, client_correlated)
			VALUES ($1, 'reference-user', $2, 'reference-device', 'reference-item', 'mediasource_reference-item', 'Prepared', 6000000000, now() + interval '30 minutes', $3)`,
			id, auth, clientCorrelated)
		return err
	}
	clientPlaybackMigrationConstraint(t, insertPlay("play_duplicate_default", "reference-auth-a", false), "23505")
	for _, entry := range []struct{ id, auth string }{
		{"play_correlated_a", "reference-auth-a"}, {"play_correlated_b", "reference-auth-a"}, {"play_correlated_c", "reference-auth-b"},
	} {
		if err := insertPlay(entry.id, entry.auth, true); err != nil {
			t.Fatalf("independent correlated sessions must share a media source: %v", err)
		}
	}
	bind := func(auth, nonce, playID string) error {
		_, err := pool.Exec(ctx, `INSERT INTO client_playback_references
			(user_id, auth_session_id, device_id, client_nonce, play_session_id)
			VALUES ('reference-user', $1, 'reference-device', $2, $3)`, auth, nonce, playID)
		return err
	}
	for _, entry := range []struct{ auth, nonce, play string }{
		{"reference-auth-a", "shared-client-nonce", "play_correlated_a"},
		{"reference-auth-a", "second-client-nonce", "play_correlated_b"},
		{"reference-auth-b", "shared-client-nonce", "play_correlated_c"},
	} {
		if err := bind(entry.auth, entry.nonce, entry.play); err != nil {
			t.Fatalf("scope-local reference binding failed: %v", err)
		}
	}
	clientPlaybackMigrationConstraint(t, bind("reference-auth-a", "shared-client-nonce", "play_terminal"), "23505")
	clientPlaybackMigrationConstraint(t, bind("reference-auth-a", "third-client-nonce", "play_correlated_b"), "23505")
	for _, nonce := range []string{"", "   ", "play_reserved", "line\nbreak", strings.Repeat("x", 257)} {
		clientPlaybackMigrationConstraint(t, bind("reference-auth-a", nonce, "play_terminal"), "23514")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM play_sessions WHERE id = 'play_correlated_a'"); err != nil {
		t.Fatal(err)
	}
	var target *string
	if err := pool.QueryRow(ctx, `SELECT play_session_id FROM client_playback_references
		WHERE user_id = 'reference-user' AND auth_session_id = 'reference-auth-a'
		AND device_id = 'reference-device' AND client_nonce = 'shared-client-nonce'`).Scan(&target); err != nil || target != nil {
		t.Errorf("playback deletion did not retain a NULL-target tombstone: error = %v", err)
	}
	clientPlaybackMigrationConstraint(t, bind("reference-auth-a", "shared-client-nonce", "play_terminal"), "23505")
	if _, err := pool.Exec(ctx, "DELETE FROM sessions WHERE id = 'reference-auth-a'"); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM client_playback_references").Scan(&references); err != nil || references != 1 {
		t.Errorf("authentication deletion did not preserve the sibling scope: count = %d, error = %v", references, err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = 'reference-user'"); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM client_playback_references").Scan(&references); err != nil || references != 0 {
		t.Errorf("account deletion retained playback references: count = %d, error = %v", references, err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat client playback migration: %v", err)
	}
}

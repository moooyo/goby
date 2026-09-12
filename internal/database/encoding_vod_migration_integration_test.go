package database_test

import (
	"context"
	"embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// Only the historical migrations are embedded so the fixture always starts at
// the actual version 10 schema, without downgrading an already migrated schema.
//
//go:embed migrations/000[1-9]_*.sql migrations/0010_*.sql
var encodingVODBaselineFiles embed.FS

func encodingVODVersion10Baseline(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	names := []string{
		"0001_identity.sql",
		"0002_library.sql",
		"0003_local_metadata.sql",
		"0004_catalog_entities.sql",
		"0005_item_images.sql",
		"0006_playback_state.sql",
		"0007_client_capabilities.sql",
		"0008_player_state.sql",
		"0009_subtitles.sql",
		"0010_encoding_jobs.sql",
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin version 10 fixture: %v", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if _, err := tx.Exec(ctx, `CREATE TABLE schema_migrations (
		version bigint PRIMARY KEY,
		name text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatalf("create version 10 migration history: %v", err)
	}
	for index, name := range names {
		content, err := encodingVODBaselineFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatalf("read historical migration %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply historical migration %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", int64(index+1), name); err != nil {
			t.Fatalf("record historical migration %s: %v", name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit version 10 fixture: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 10 {
		t.Fatalf("historical schema version = %d, want 10, error = %v", version, err)
	}
}

func encodingVODDataSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'settings', (SELECT jsonb_agg(to_jsonb(t) ORDER BY key) FROM server_settings t),
		'users', (SELECT jsonb_agg(to_jsonb(t) - 'management_revision' ORDER BY id) FROM users t),
		'auth', (SELECT jsonb_agg(to_jsonb(t) - 'device_registry_id' ORDER BY id) FROM sessions t),
		'libraries', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM libraries t),
		'items', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM items t),
		'play', (SELECT jsonb_agg(to_jsonb(t) - 'client_correlated' - 'application_client_id' ORDER BY id) FROM play_sessions t),
		'userdata', (SELECT jsonb_agg(to_jsonb(t) ORDER BY user_id, item_id) FROM user_item_data t),
		'encodings', (SELECT jsonb_agg(to_jsonb(t) - 'application_client_id' ORDER BY id) FROM encoding_jobs t)
	)::text`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot encoding migration fixture: %v", err)
	}
	return snapshot
}

func encodingVODPlanAtSize(t *testing.T, ctx context.Context, pool *pgxpool.Pool, size int) string {
	t.Helper()
	// PostgreSQL adds one space after the colon in the JSONB text form. Measure
	// that form explicitly; the input JSON byte count is not the constraint.
	const emptyObject = `{"SegmentTimes": ""}`
	plan := `{"SegmentTimes":"` + strings.Repeat("0", size-len(emptyObject)) + `"}`
	var storedSize int
	if err := pool.QueryRow(ctx, "SELECT octet_length($1::jsonb::text)", plan).Scan(&storedSize); err != nil {
		t.Fatalf("measure JSONB plan fixture: %v", err)
	}
	if storedSize != size {
		t.Fatalf("JSONB plan fixture has %d bytes, want %d", storedSize, size)
	}
	return plan
}

func encodingVODInsertPlan(ctx context.Context, pool *pgxpool.Pool, id, plan string) error {
	_, err := pool.Exec(ctx, `INSERT INTO encoding_jobs (
		id, user_id, auth_session_id, device_id, play_session_id, item_id, media_source_id,
		source_stamp, plan, state, created_at, updated_at, last_access_at
	) VALUES ($1, 'encoding-viewer', 'encoding-auth', 'encoding-device', 'encoding-play',
		'encoding-item', 'mediasource_encoding-item', 'fixture-source-stamp', $2::jsonb,
		'queued', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, id, plan)
	return err
}

func assertEncodingVODPlanConstraint(t *testing.T, err error) {
	t.Helper()
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != "23514" || pgError.ConstraintName != "encoding_jobs_plan_check" {
		t.Fatalf("plan must violate encoding_jobs_plan_check: %v", err)
	}
}

func TestMigrateEncodingVODPlansPreservesVersion10DataAndJSONBounds(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	encodingVODVersion10Baseline(t, ctx, pool)
	// All rows and synthetic authentication data belong to this test's fresh
	// schema. Terminal outputs and their timestamps must survive the upgrade.
	if _, err := pool.Exec(ctx, `
		INSERT INTO server_settings (key, value) VALUES ('encoding-migration-sentinel', 'preserve');
		INSERT INTO users (id, name, normalized_name, password_hash, has_password, policy)
		VALUES ('encoding-viewer', 'Encoding Viewer', 'encoding viewer', 'fixture-only-no-authentication',
			false, '{"EnableAllFolders":false,"EnabledFolders":["encoding-library"]}'::jsonb);
		INSERT INTO sessions (id, user_id, token_hash, kind, device_id, created_at, expires_at, revoked_at)
		VALUES ('encoding-auth', 'encoding-viewer', decode(repeat('ab', 32), 'hex'), 'emby', 'encoding-device',
			'2025-12-01T00:00:00Z', '2026-01-02T00:00:00Z', '2026-01-01T00:00:02Z');
		INSERT INTO libraries (id, name, collection_type)
		VALUES ('encoding-library', 'Encoding Library', 'movies');
		INSERT INTO items (id, library_id, name, sort_name, type, path)
		VALUES ('encoding-item', 'encoding-library', 'Encoding Item', 'encoding item', 'Movie', '/synthetic/source');
		INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, play_count, is_favorite, played)
		VALUES ('encoding-viewer', 'encoding-item', 0, 7, true, true);
		INSERT INTO play_sessions (id, user_id, auth_session_id, device_id, item_id, media_source_id,
			state, duration_ticks, counted, created_at, updated_at, expires_at, started_at, stopped_at, player_state)
		VALUES ('encoding-play', 'encoding-viewer', 'encoding-auth', 'encoding-device', 'encoding-item',
			'mediasource_encoding-item', 'Stopped', 6000000000, true, '2026-01-01T00:00:00Z',
			'2026-01-01T00:00:02Z', '2026-01-01T00:30:00Z', '2026-01-01T00:00:00Z',
			'2026-01-01T00:00:02Z', '{"PlayMethod":"Transcode","VolumeLevel":25}'::jsonb);
		INSERT INTO encoding_jobs (id, user_id, auth_session_id, device_id, play_session_id, item_id,
			media_source_id, source_stamp, plan, state, output_bytes, error_code, created_at, updated_at, last_access_at)
		SELECT repeat(job.id, 32), 'encoding-viewer', 'encoding-auth', 'encoding-device', 'encoding-play',
			'encoding-item', 'mediasource_encoding-item', 'fixture-source-stamp',
			'{"Container":"ts","VideoCodec":"copy","SegmentSeconds":3}'::jsonb,
			job.state, job.bytes, job.error_code, '2026-01-01T00:00:00Z'::timestamptz,
			'2026-01-01T00:00:01Z'::timestamptz, '2026-01-01T00:00:02Z'::timestamptz
		FROM (VALUES ('a', 'completed', 8192, ''), ('b', 'failed', 4096, 'process_failed'))
			AS job(id, state, bytes, error_code);
	`); err != nil {
		t.Fatalf("insert version 10 encoding history and related data: %v", err)
	}
	oldLimitPlan := encodingVODPlanAtSize(t, ctx, pool, 8192)
	if err := encodingVODInsertPlan(ctx, pool, strings.Repeat("c", 32), oldLimitPlan); err != nil {
		t.Fatalf("version 10 must accept its 8192-byte boundary: %v", err)
	}
	const planLimit = 128 * 1024
	largePlan := encodingVODPlanAtSize(t, ctx, pool, planLimit)
	largeID := strings.Repeat("d", 32)
	assertEncodingVODPlanConstraint(t, encodingVODInsertPlan(ctx, pool, largeID, encodingVODPlanAtSize(t, ctx, pool, 8193)))
	assertEncodingVODPlanConstraint(t, encodingVODInsertPlan(ctx, pool, largeID, largePlan))
	before := encodingVODDataSnapshot(t, ctx, pool)
	historyBefore := migrationHistory(t, ctx, pool)

	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade encoding plans from version 10: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 28 {
		t.Fatalf("upgraded schema version = %d, want 28, error = %v", version, err)
	}
	if after := encodingVODDataSnapshot(t, ctx, pool); after != before {
		t.Error("encoding plan migration changed existing jobs or related data")
	}
	var oldHistory, latestName string
	var count int
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text
		FROM schema_migrations m WHERE version <= 10`).Scan(&oldHistory); err != nil {
		t.Fatalf("read preserved version 10 history: %v", err)
	}
	if oldHistory != historyBefore {
		t.Error("encoding plan migration changed historical migration rows or applied timestamps")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 28 {
		t.Fatalf("upgraded migration count = %d, want 28, error = %v", count, err)
	}
	if err := pool.QueryRow(ctx, "SELECT name FROM schema_migrations WHERE version = 11").Scan(&latestName); err != nil || latestName != "0011_encoding_vod_plans.sql" {
		t.Fatalf("version 11 migration name = %q, error = %v", latestName, err)
	}
	if err := encodingVODInsertPlan(ctx, pool, largeID, largePlan); err != nil {
		t.Fatalf("upgraded schema must accept a 128 KiB JSONB object: %v", err)
	}
	var storedSize int
	if err := pool.QueryRow(ctx, "SELECT octet_length(plan::text) FROM encoding_jobs WHERE id = $1", largeID).Scan(&storedSize); err != nil || storedSize != planLimit {
		t.Fatalf("stored expanded plan size = %d, want %d, error = %v", storedSize, planLimit, err)
	}
	for _, test := range []struct {
		name string
		plan string
	}{
		{"one byte above the limit", encodingVODPlanAtSize(t, ctx, pool, planLimit+1)},
		{"array", `[]`},
		{"string", `"invalid"`},
		{"number", `1`},
		{"boolean", `true`},
		{"null", `null`},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertEncodingVODPlanConstraint(t, encodingVODInsertPlan(ctx, pool, strings.Repeat("e", 32), test.plan))
		})
	}
	beforeRepeat := encodingVODDataSnapshot(t, ctx, pool)
	historyBeforeRepeat := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat encoding plan migration: %v", err)
	}
	if after := encodingVODDataSnapshot(t, ctx, pool); after != beforeRepeat {
		t.Error("repeated encoding plan migration changed jobs or related data")
	}
	if after := migrationHistory(t, ctx, pool); after != historyBeforeRepeat {
		t.Error("repeated encoding plan migration changed migration history")
	}
}

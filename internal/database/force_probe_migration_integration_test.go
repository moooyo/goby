package database_test

import (
	"embed"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/database"
)

// Extend the published version 12 fixture to the actual version 14 schema.
//
//go:embed migrations/0013_managed_users.sql migrations/0014_item_metadata.sql
var forceProbeBaselineFiles embed.FS

func TestForceProbeMigrationPreservesHistoryAndDefaultsToOrdinaryScanning(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	managedUsersVersion12Baseline(t, ctx, pool)
	for index, name := range []string{"0013_managed_users.sql", "0014_item_metadata.sql"} {
		content, err := forceProbeBaselineFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatalf("read historical migration %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply historical migration %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", int64(index+13), name); err != nil {
			t.Fatalf("record historical migration %s: %v", name, err)
		}
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 14 {
		t.Fatalf("force probe baseline schema = %d, want 14, error=%v", version, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id, name, collection_type)
		VALUES ('force-migration-a', 'First historical library', 'movies'),
		('force-migration-b', 'Second historical library', 'movies')`); err != nil {
		t.Fatalf("seed historical scan libraries: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scan_jobs
		(id, library_id, status, error, scanned, added, updated, started_at, finished_at, cancel_requested)
		SELECT 'legacy-' || lower(status),
		CASE WHEN status = 'Running' THEN 'force-migration-b' ELSE 'force-migration-a' END,
		status, 'Historical scan result', 8, 3, 2,
		CASE WHEN status <> 'Queued' THEN '2026-01-01T00:00:00Z'::timestamptz ELSE NULL END,
		CASE WHEN status NOT IN ('Queued', 'Running') THEN '2026-01-01T00:01:00Z'::timestamptz ELSE NULL END,
		status = 'Cancelled'
		FROM unnest($1::text[]) AS status`, []string{"Queued", "Running", "Completed", "Failed", "Cancelled", "Interrupted"}); err != nil {
		t.Fatalf("seed every historical scan status: %v", err)
	}
	// Later nullable task ownership is checked independently after migration.
	const legacyJobs = `SELECT jsonb_agg(to_jsonb(j) - 'force_probe' - 'task_child_id' ORDER BY id)::text FROM scan_jobs j`
	var before string
	if err := pool.QueryRow(ctx, legacyJobs).Scan(&before); err != nil {
		t.Fatalf("snapshot historical scan jobs: %v", err)
	}
	beforeHistory := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade force probe job policy: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 19 {
		t.Fatalf("force probe migrated schema = %d, want 19, error=%v", version, err)
	}
	var after, oldHistory, name string
	if err := pool.QueryRow(ctx, legacyJobs).Scan(&after); err != nil || after != before {
		t.Fatalf("force probe migration changed historical scan fields: error=%v", err)
	}
	var taskLinkedScans int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM scan_jobs WHERE task_child_id IS NOT NULL").Scan(&taskLinkedScans); err != nil || taskLinkedScans != 0 {
		t.Fatalf("migration attached historical scans to task children: count=%d error=%v", taskLinkedScans, err)
	}
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text
		FROM schema_migrations m WHERE version <= 14`).Scan(&oldHistory); err != nil || oldHistory != beforeHistory {
		t.Fatalf("force probe migration rewrote previous migration history: error=%v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT name FROM schema_migrations WHERE version = 15").Scan(&name); err != nil || name != "0015_scan_force_probe.sql" {
		t.Fatalf("force probe migration history entry = %q, error=%v", name, err)
	}
	var defaults, requiredBoolean bool
	if err := pool.QueryRow(ctx, "SELECT bool_and(NOT force_probe) FROM scan_jobs").Scan(&defaults); err != nil || !defaults {
		t.Fatalf("historical scans were not backfilled to ordinary scanning: defaults=%v error=%v", defaults, err)
	}
	if err := pool.QueryRow(ctx, `SELECT attnotnull AND atttypid = 'boolean'::regtype FROM pg_attribute
		WHERE attrelid = 'scan_jobs'::regclass AND attname = 'force_probe' AND NOT attisdropped`).Scan(&requiredBoolean); err != nil || !requiredBoolean {
		t.Fatalf("force probe policy is not a required boolean: required=%v error=%v", requiredBoolean, err)
	}
	var defaultForce bool
	if err := pool.QueryRow(ctx, `INSERT INTO scan_jobs (id, library_id, status)
		VALUES ('new-default', 'force-migration-a', 'Completed') RETURNING force_probe`).Scan(&defaultForce); err != nil || defaultForce {
		t.Fatalf("omitted policy did not default to ordinary scanning: force=%v error=%v", defaultForce, err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO scan_jobs (id, library_id, status, force_probe)
		VALUES ('invalid-null', 'force-migration-a', 'Completed', NULL)`)
	var constraint *pgconn.PgError
	if !errors.As(err, &constraint) || constraint.Code != "23502" {
		t.Fatalf("nullable scan policy was accepted: %v", err)
	}
	var explicitForce bool
	if err := pool.QueryRow(ctx, `INSERT INTO scan_jobs (id, library_id, status, force_probe)
		VALUES ('new-forced', 'force-migration-a', 'Completed', true) RETURNING force_probe`).Scan(&explicitForce); err != nil || !explicitForce {
		t.Fatalf("explicit force policy did not persist: force=%v error=%v", explicitForce, err)
	}
	completedHistory := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat force probe migration: %v", err)
	}
	if after := migrationHistory(t, ctx, pool); after != completedHistory {
		t.Fatal("repeated force probe migration changed migration history")
	}
}

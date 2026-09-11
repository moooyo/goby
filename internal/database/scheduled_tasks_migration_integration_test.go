package database_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

const scheduledTaskLegacyTables = "application_key_clients application_key_devices application_keys catalog_entities client_playback_references devices encoding_jobs item_entities item_images item_metadata_state item_subtitles items libraries library_roots play_sessions scan_jobs schema_migrations server_settings sessions user_item_data users"

var scheduledTaskNewTables = []string{
	"task_definitions", "task_occurrences", "task_run_children", "task_run_requests", "task_runs", "task_triggers",
}

// Build the published baseline from historical migrations. Seeding before the
// two device migrations also retains real ordinary and application generations.
func scheduledTasksVersion18Baseline(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	deviceVersion16Baseline(t, ctx, pool)
	seedDeviceLegacyLogins(t, ctx, pool)
	seedDeviceMigrationHistory(t, ctx, pool)
	for index, name := range []string{"0017_devices.sql", "0018_application_key_devices.sql"} {
		content, err := deviceMigrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatalf("read published scheduled-task baseline: %v", err)
		}
		if _, err := pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply published scheduled-task baseline: %v", err)
		}
		if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", index+17, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `
		SELECT sync_catalog_item_entities('device-migration-item', '{"Genres":["Preserved Genre"],"People":[{"Name":"Preserved Person","Role":"Original Role","Type":"Actor"}]}');
		INSERT INTO item_images (item_id, root_id, image_type, image_index, relative_path, file_identity,
			source_hash, file_size, modified_at, width, height, mime_type)
		VALUES ('device-migration-item', 'device-migration-root', 'Primary', 0, 'poster.jpg', 'preserved-image',
			repeat('a', 64), 1024, '2025-01-01T00:00:00Z', 32, 32, 'image/jpeg');
		INSERT INTO item_subtitles (item_id, root_id, stream_index, relative_path, file_identity, source_hash,
			file_size, modified_at, change_time_ns, codec, language, title, is_default, mime_type)
		VALUES ('device-migration-item', 'device-migration-root', 5, 'movie.en.srt', 'preserved-subtitle',
			repeat('b', 64), 64, '2025-01-01T00:00:00Z', 123456789, 'srt', 'eng', 'Original Subtitle', true, 'application/x-subrip');
		UPDATE item_metadata_state SET overrides = '{"Name":"Preserved Override"}', locked_values = '{"Genres":["Preserved Genre"]}',
			revision = 7, last_edited_by = 'device-user-a', last_edited_at = '2025-02-01T00:00:00Z'
			WHERE item_id = 'device-migration-item';
		UPDATE devices SET custom_name = 'Preserved Device Override', revision = 9, ip_address = '192.0.2.90'
			WHERE reported_device_id = 'Shared-ID';
		INSERT INTO devices (reported_device_id, reported_name, revision, created_at, last_seen_at, deleted_at)
		VALUES ('retired-task-baseline-device', 'Retained Generation', 4,
			'2024-01-01T00:00:00Z', '2025-01-01T00:00:00Z', '2025-02-01T00:00:00Z');
		UPDATE application_key_devices SET custom_name = 'Preserved Server Override', revision = 3 WHERE id = 1;
		INSERT INTO libraries (id, name, collection_type) VALUES ('task-baseline-running-library', 'Independent Running Library', 'music');
		INSERT INTO scan_jobs (id, library_id, status, scanned, added, updated, started_at, finished_at, cancel_requested, force_probe)
		VALUES ('task-baseline-queued-scan', 'device-migration-library', 'Queued', 0, 0, 0, NULL, NULL, false, false),
			('task-baseline-running-scan', 'task-baseline-running-library', 'Running', 19, 3, 8, '2025-03-01T00:00:00Z', NULL, true, true),
			('task-baseline-cancelled-scan', 'device-migration-library', 'Cancelled', 4, 1, 2,
			 '2025-03-01T00:00:00Z', '2025-03-02T00:00:00Z', true, false)`); err != nil {
		t.Fatalf("seed scheduled-task migration preservation fixtures: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 18 {
		t.Fatalf("scheduled-task baseline version = %d, want 18: %v", version, err)
	}
	var taskTables, childColumns int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM pg_tables WHERE schemaname = current_schema() AND tablename = ANY($1::text[])),
		(SELECT count(*) FROM pg_attribute WHERE attrelid = 'scan_jobs'::regclass AND attname = 'task_child_id' AND NOT attisdropped)`,
		scheduledTaskNewTables).Scan(&taskTables, &childColumns); err != nil || taskTables != 0 || childColumns != 0 {
		t.Fatalf("schema 18 already contained scheduled-task state: tables=%d columns=%d error=%v", taskTables, childColumns, err)
	}
}

// Retain every original column, including all eighteen migration-history rows.
// Never print these snapshots: credential hashes and ciphertext are deliberate
// parts of the preservation fixture.
func scheduledTasksLegacySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table deviceLegacyTable) string {
	t.Helper()
	columns := make([]string, len(table.columns))
	for index, column := range table.columns {
		columns[index] = pgx.Identifier{column}.Sanitize()
	}
	statement := "SELECT " + strings.Join(columns, ", ") + " FROM " + pgx.Identifier{table.name}.Sanitize()
	if table.name == "schema_migrations" {
		statement += " WHERE version <= 18"
	}
	var result string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(legacy_row)
		ORDER BY to_jsonb(legacy_row)::text), '[]'::jsonb)::text FROM (`+statement+`) legacy_row`).Scan(&result); err != nil {
		t.Fatalf("snapshot scheduled-task baseline table %s: %v", table.name, err)
	}
	return result
}

func scheduledTasksAssertEmptyTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var totalTables int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_tables WHERE schemaname = current_schema()").Scan(&totalTables); err != nil || totalTables != 30 {
		t.Fatalf("current schema did not retain six task tables, two settings tables, and one activity table beyond schema 18: count=%d error=%v", totalTables, err)
	}
	var actual []string
	if err := pool.QueryRow(ctx, `SELECT array_agg(tablename ORDER BY tablename)
		FROM pg_tables WHERE schemaname = current_schema() AND tablename = ANY($1::text[])`, scheduledTaskNewTables).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if strings.Join(actual, " ") != strings.Join(scheduledTaskNewTables, " ") {
		t.Fatalf("scheduled-task migration table inventory = %v", actual)
	}
	for _, name := range scheduledTaskNewTables {
		var count int64
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{name}.Sanitize()).Scan(&count); err != nil || count != 0 {
			t.Errorf("migration populated task table %s: count=%d error=%v", name, count, err)
		}
	}
	var activityCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM activity_entries").Scan(&activityCount); err != nil || activityCount != 0 {
		t.Errorf("migration backfilled historical state into activity entries: count=%d error=%v", activityCount, err)
	}
	var userSettingsCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_settings").Scan(&userSettingsCount); err != nil || userSettingsCount != 0 {
		t.Errorf("migration populated user settings: count=%d error=%v", userSettingsCount, err)
	}
	var musicSourceCount, creditGroupCount int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM item_metadata_state WHERE music_source IS DISTINCT FROM '{}'::jsonb),
		(SELECT count(*) FROM item_entities WHERE credit_group IS DISTINCT FROM 0)`).Scan(&musicSourceCount, &creditGroupCount); err != nil || musicSourceCount != 0 || creditGroupCount != 0 {
		t.Errorf("migration populated historical music sources or credit groups: sources=%d groups=%d error=%v", musicSourceCount, creditGroupCount, err)
	}
	var settingsCount int
	var defaultSettings bool
	if err := pool.QueryRow(ctx, `SELECT count(*),bool_and(id=1 AND revision=1 AND server_name IS NULL
		AND server_name_mode='deployment' AND compatibility_max_width=0
		AND max_bitrate IS NULL AND max_width IS NULL AND max_height IS NULL AND max_audio_channels IS NULL)
		FROM managed_settings`).Scan(&settingsCount, &defaultSettings); err != nil || settingsCount != 1 || !defaultSettings {
		t.Errorf("current migration did not retain one settings singleton with deployment compatibility defaults and no overrides: count=%d error=%v", settingsCount, err)
	}
	var count, linked int
	if err := pool.QueryRow(ctx, "SELECT count(*), count(task_child_id) FROM scan_jobs").Scan(&count, &linked); err != nil || count != 4 || linked != 0 {
		t.Errorf("migration rewrote or linked historical scans: count=%d linked=%d error=%v", count, linked, err)
	}
}

func TestMigrateScheduledTasksPreservesSchema18AndLeavesLegacyScansUnlinked(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	scheduledTasksVersion18Baseline(t, ctx, pool)
	legacy := captureDeviceLegacyTables(t, ctx, pool)
	names := make([]string, len(legacy))
	for index := range legacy {
		names[index] = legacy[index].name
		legacy[index].snapshot = scheduledTasksLegacySnapshot(t, ctx, pool, legacy[index])
		if legacy[index].snapshot == "[]" {
			t.Fatalf("preservation fixture left legacy table %s empty", legacy[index].name)
		}
	}
	if strings.Join(names, " ") != scheduledTaskLegacyTables {
		t.Fatalf("schema 18 table inventory differs: %v", names)
	}
	var firstMigrationHistory string
	for attempt := 1; attempt <= 2; attempt++ {
		if err := database.Migrate(ctx, pool); err != nil {
			t.Fatalf("scheduled-task migration attempt %d: %v", attempt, err)
		}
		if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 25 {
			t.Fatalf("scheduled-task full migration schema = %d, want 25: %v", version, err)
		}
		var name string
		if err := pool.QueryRow(ctx, "SELECT name FROM schema_migrations WHERE version = 19").Scan(&name); err != nil || name != "0019_scheduled_tasks.sql" {
			t.Fatalf("scheduled-task migration history differs: name=%q error=%v", name, err)
		}
		history := migrationHistory(t, ctx, pool)
		if attempt == 1 {
			firstMigrationHistory = history
		} else if history != firstMigrationHistory {
			t.Error("reapplying the migration changed its complete recorded history")
		}
		for _, table := range legacy {
			if after := scheduledTasksLegacySnapshot(t, ctx, pool, table); after != table.snapshot {
				t.Errorf("migration attempt %d changed an original field or row in %s", attempt, table.name)
			}
		}
		scheduledTasksAssertEmptyTables(t, ctx, pool)
	}
}

func scheduledTasksConstraintPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx, pool := migrationTestPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("initialize scheduled-task constraint schema: %v", err)
	}
	return ctx, pool
}

func scheduledTasksConstraintTx(t *testing.T, ctx context.Context, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	if _, err := tx.Exec(ctx, `INSERT INTO task_definitions (id, key, name) VALUES
		(repeat('a', 32), 'synthetic.first', 'First Synthetic Task'),
		(repeat('b', 32), 'synthetic.second', 'Second Synthetic Task');
		INSERT INTO task_runs (id, task_id, state, source, task_key, task_name, created_at) VALUES
		(repeat('1', 32), repeat('a', 32), 'pending', 'manual', 'synthetic.first', 'First Synthetic Task', '2025-01-01T00:00:00Z'),
		(repeat('2', 32), repeat('b', 32), 'pending', 'manual', 'synthetic.second', 'Second Synthetic Task', '2025-01-01T00:00:00Z');
		INSERT INTO libraries (id, name, collection_type) VALUES
		('task-owned-library', 'Owned Library', 'movies'), ('task-independent-library', 'Independent Library', 'music');
		INSERT INTO task_run_children (id, run_id, library_id, library_name, ordinal, created_at) VALUES
		(repeat('c', 32), repeat('1', 32), 'task-owned-library', 'Owned Library', 0, '2025-01-01T00:00:00Z'),
		(repeat('d', 32), repeat('1', 32), 'task-independent-library', 'Independent Library', 1, '2025-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed isolated scheduled-task constraints: %v", err)
	}
	return tx
}

func scheduledTasksRequireConstraint(t *testing.T, err error, code, constraint string) {
	t.Helper()
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != code || (constraint != "" && databaseError.ConstraintName != constraint) {
		// PostgreSQL details can contain complete inserted rows. Report only the
		// diagnostic identity, never credential-bearing fixture values.
		if databaseError == nil {
			t.Fatalf("expected scheduled-task constraint %s (%s), got non-constraint error %T", code, constraint, err)
		}
		t.Fatalf("expected constraint %s (%s), got %s (%s)", code, constraint, databaseError.Code, databaseError.ConstraintName)
	}
}

func TestScheduledTasksMigrationEnforcesActiveRunsAndRequestOwnership(t *testing.T) {
	ctx, pool := scheduledTasksConstraintPool(t)
	for _, state := range []string{"pending", "running", "stopping"} {
		t.Run("one active run including "+state, func(t *testing.T) {
			tx := scheduledTasksConstraintTx(t, ctx, pool)
			if _, err := tx.Exec(ctx, `UPDATE task_runs SET state = $1,
				started_at = CASE WHEN $1 = 'running' THEN clock_timestamp() END,
				stop_requested_at = CASE WHEN $1 = 'stopping' THEN clock_timestamp() END,
				stop_reason = CASE WHEN $1 = 'stopping' THEN 'administrator' ELSE '' END WHERE id = repeat('1', 32)`, state); err != nil {
				t.Fatal(err)
			}
			_, err := tx.Exec(ctx, `INSERT INTO task_runs (id, task_id, state, source, task_key, task_name)
				VALUES (repeat('3', 32), repeat('a', 32), 'pending', 'manual', 'synthetic.first', 'First Synthetic Task')`)
			scheduledTasksRequireConstraint(t, err, "23505", "task_runs_one_active_idx")
		})
	}
	t.Run("terminal run releases only its active slot", func(t *testing.T) {
		tx := scheduledTasksConstraintTx(t, ctx, pool)
		if _, err := tx.Exec(ctx, `UPDATE task_runs SET state = 'completed', finished_at = clock_timestamp() WHERE id = repeat('1', 32);
			INSERT INTO task_runs (id, task_id, state, source, task_key, task_name)
			VALUES (repeat('3', 32), repeat('a', 32), 'pending', 'manual', 'synthetic.first', 'First Synthetic Task')`); err != nil {
			t.Fatalf("terminal history blocked a later independent run: %v", err)
		}
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM task_runs").Scan(&count); err != nil || count != 3 {
			t.Fatalf("new admission replaced run history: count=%d error=%v", count, err)
		}
	})
	for _, test := range []struct{ name, statement, code, constraint string }{
		{"receipt cannot select another task's run", `INSERT INTO task_run_requests (task_id, request_id, run_id, fingerprint) VALUES (repeat('a',32), 'cross-task', repeat('2',32), decode(repeat('11',32),'hex'))`, "23503", ""},
		{"receipt requires an existing run", `INSERT INTO task_run_requests (task_id, request_id, run_id, fingerprint) VALUES (repeat('a',32), 'missing-run', repeat('9',32), decode(repeat('11',32),'hex'))`, "23503", ""},
		{"receipt fingerprint has fixed size", `INSERT INTO task_run_requests (task_id, request_id, run_id, fingerprint) VALUES (repeat('a',32), 'bad-hash', repeat('1',32), decode(repeat('11',31),'hex'))`, "23514", ""},
		{"legacy request identity needs its fingerprint", `UPDATE task_runs SET request_id = 'incomplete-request' WHERE id = repeat('1',32)`, "23514", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := scheduledTasksConstraintTx(t, ctx, pool)
			_, err := tx.Exec(ctx, test.statement)
			scheduledTasksRequireConstraint(t, err, test.code, test.constraint)
		})
	}
	t.Run("coalesced receipts retain separate retry identities", func(t *testing.T) {
		tx := scheduledTasksConstraintTx(t, ctx, pool)
		if _, err := tx.Exec(ctx, `INSERT INTO task_run_requests (task_id, request_id, run_id, fingerprint) VALUES
			(repeat('a',32), 'request-one', repeat('1',32), decode(repeat('11',32),'hex')),
			(repeat('a',32), 'request-two', repeat('1',32), decode(repeat('22',32),'hex')),
			(repeat('b',32), 'request-one', repeat('2',32), decode(repeat('33',32),'hex'));
			UPDATE task_runs SET state = 'completed', finished_at = clock_timestamp() WHERE id = repeat('1',32)`); err != nil {
			t.Fatal(err)
		}
		var receipts int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM task_run_requests").Scan(&receipts); err != nil || receipts != 3 {
			t.Fatalf("completion discarded retry receipts: count=%d error=%v", receipts, err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO task_run_requests (task_id, request_id, run_id, fingerprint)
			VALUES (repeat('a',32), 'request-one', repeat('1',32), decode(repeat('44',32),'hex'))`)
		scheduledTasksRequireConstraint(t, err, "23505", "task_run_requests_pkey")
	})
	t.Run("receipt prevents reassignment of its run to another task", func(t *testing.T) {
		tx := scheduledTasksConstraintTx(t, ctx, pool)
		if _, err := tx.Exec(ctx, `INSERT INTO task_run_requests (task_id, request_id, run_id, fingerprint)
			VALUES (repeat('a',32), 'owned-request', repeat('1',32), decode(repeat('11',32),'hex'));
			UPDATE task_runs SET state = 'completed', finished_at = clock_timestamp()`); err != nil {
			t.Fatal(err)
		}
		_, err := tx.Exec(ctx, "UPDATE task_runs SET task_id = repeat('b',32) WHERE id = repeat('1',32)")
		scheduledTasksRequireConstraint(t, err, "23503", "")
	})
	t.Run("completed admission retains its original request key", func(t *testing.T) {
		tx := scheduledTasksConstraintTx(t, ctx, pool)
		if _, err := tx.Exec(ctx, `UPDATE task_runs SET request_id='original-start',request_fingerprint=decode(repeat('11',32),'hex'),
			state='completed',finished_at=clock_timestamp() WHERE id=repeat('1',32)`); err != nil {
			t.Fatal(err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO task_runs (id,task_id,state,source,task_key,task_name,request_id,request_fingerprint)
			VALUES (repeat('3',32),repeat('a',32),'pending','manual','synthetic.first','First Synthetic Task','original-start',decode(repeat('11',32),'hex'))`)
		scheduledTasksRequireConstraint(t, err, "23505", "task_runs_request_id_idx")
	})
}

func scheduledTasksLinkScan(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	if _, err := tx.Exec(ctx, `UPDATE task_run_children SET state = 'queued', scan_job_id = 'task-linked-scan' WHERE id = repeat('c',32);
		INSERT INTO scan_jobs (id, library_id, status, task_child_id)
		VALUES ('task-linked-scan', 'task-owned-library', 'Queued', repeat('c',32));
		INSERT INTO scan_jobs (id, library_id, status) VALUES
		('task-legacy-null-one', 'task-independent-library', 'Completed'),
		('task-legacy-null-two', 'task-independent-library', 'Completed')`); err != nil {
		t.Fatalf("link one owned scan while retaining legacy null links: %v", err)
	}
}

func TestScheduledTasksMigrationEnforcesChildLinksAndRetainsLibraryHistory(t *testing.T) {
	ctx, pool := scheduledTasksConstraintPool(t)
	for _, test := range []struct{ name, statement, code, constraint string }{
		{"second scan cannot take the same child", `INSERT INTO scan_jobs (id,library_id,status,task_child_id) VALUES ('second-linked-scan','task-independent-library','Completed',repeat('c',32))`, "23505", "scan_jobs_task_child_id_idx"},
		{"second child cannot take the same scan", `UPDATE task_run_children SET state='queued',scan_job_id='task-linked-scan' WHERE id=repeat('d',32)`, "23505", "task_run_children_scan_job_idx"},
		{"scan cannot name a missing child", `INSERT INTO scan_jobs (id,library_id,status,task_child_id) VALUES ('orphan-linked-scan','task-independent-library','Completed',repeat('9',32))`, "23503", "scan_jobs_task_child_id_fkey"},
		{"linked child cannot be discarded", `DELETE FROM task_run_children WHERE id=repeat('c',32)`, "23503", "scan_jobs_task_child_id_fkey"},
		{"linked run cannot cascade through scan history", `DELETE FROM task_runs WHERE id=repeat('1',32)`, "23503", "scan_jobs_task_child_id_fkey"},
		{"one library snapshot per run", `INSERT INTO task_run_children (id,run_id,library_id,library_name,ordinal) VALUES (repeat('e',32),repeat('1',32),'task-owned-library','Duplicate Library',2)`, "23505", ""},
		{"one child per ordinal", `INSERT INTO task_run_children (id,run_id,library_id,library_name,ordinal) VALUES (repeat('e',32),repeat('1',32),'removed-library','Snapshot Only',0)`, "23505", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := scheduledTasksConstraintTx(t, ctx, pool)
			scheduledTasksLinkScan(t, ctx, tx)
			_, err := tx.Exec(ctx, test.statement)
			scheduledTasksRequireConstraint(t, err, test.code, test.constraint)
		})
	}
	t.Run("library deletion preserves terminal child snapshots", func(t *testing.T) {
		tx := scheduledTasksConstraintTx(t, ctx, pool)
		scheduledTasksLinkScan(t, ctx, tx)
		if _, err := tx.Exec(ctx, `UPDATE scan_jobs SET status='Completed',scanned=9,added=4,updated=3,finished_at=clock_timestamp() WHERE id='task-linked-scan';
			UPDATE task_run_children SET state='completed',scanned=9,added=4,updated=3,finished_at=clock_timestamp(),error_message='Preserved scan warning' WHERE id=repeat('c',32);
			DELETE FROM libraries WHERE id='task-owned-library'`); err != nil {
			t.Fatalf("remove library after its terminal snapshot: %v", err)
		}
		var library, name, scan, state, message string
		var scanned, added, updated, remainingScans int
		if err := tx.QueryRow(ctx, `SELECT library_id,library_name,scan_job_id,state,error_message,scanned,added,updated
			FROM task_run_children WHERE id=repeat('c',32)`).Scan(&library, &name, &scan, &state, &message, &scanned, &added, &updated); err != nil {
			t.Fatalf("library deletion lost generic child history: %v", err)
		}
		if library != "task-owned-library" || name != "Owned Library" || scan != "task-linked-scan" || state != "completed" || message != "Preserved scan warning" || scanned != 9 || added != 4 || updated != 3 {
			t.Fatal("library deletion changed the durable child result")
		}
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM scan_jobs WHERE task_child_id IS NULL").Scan(&remainingScans); err != nil || remainingScans != 2 {
			t.Fatalf("independent legacy scans were lost or attached: count=%d error=%v", remainingScans, err)
		}
	})
}

func TestScheduledTasksMigrationRejectsInconsistentExecutionStates(t *testing.T) {
	ctx, pool := scheduledTasksConstraintPool(t)
	for _, test := range []struct{ name, statement string }{
		{"terminal run without finish", `UPDATE task_runs SET state='completed' WHERE id=repeat('1',32)`},
		{"active run with finish", `UPDATE task_runs SET finished_at=clock_timestamp() WHERE id=repeat('1',32)`},
		{"running run without start", `UPDATE task_runs SET state='running' WHERE id=repeat('1',32)`},
		{"stopping run without stop request", `UPDATE task_runs SET state='stopping' WHERE id=repeat('1',32)`},
		{"stop reason without request time", `UPDATE task_runs SET stop_reason='administrator' WHERE id=repeat('1',32)`},
		{"stop request time without reason", `UPDATE task_runs SET stop_requested_at=clock_timestamp() WHERE id=repeat('1',32)`},
		{"deadline without start", `UPDATE task_runs SET deadline_at=clock_timestamp() WHERE id=repeat('1',32)`},
		{"negative run progress", `UPDATE task_runs SET scanned=-1 WHERE id=repeat('1',32)`},
		{"terminal total above selected total", `UPDATE task_runs SET total_children=0,terminal_children=1,completed_children=1 WHERE id=repeat('1',32)`},
		{"terminal categories disagree with total", `UPDATE task_runs SET total_children=1,terminal_children=1 WHERE id=repeat('1',32)`},
		{"terminal child without finish", `UPDATE task_run_children SET state='cancelled' WHERE id=repeat('c',32)`},
		{"active child with finish", `UPDATE task_run_children SET finished_at=clock_timestamp() WHERE id=repeat('c',32)`},
		{"queued child without scan identity", `UPDATE task_run_children SET state='queued' WHERE id=repeat('c',32)`},
		{"running child without start", `UPDATE task_run_children SET state='running',scan_job_id='historical-scan' WHERE id=repeat('c',32)`},
		{"waiting child with scan identity", `UPDATE task_run_children SET scan_job_id='historical-scan' WHERE id=repeat('c',32)`},
		{"negative child progress", `UPDATE task_run_children SET added=-1 WHERE id=repeat('c',32)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := scheduledTasksConstraintTx(t, ctx, pool)
			_, err := tx.Exec(ctx, test.statement)
			scheduledTasksRequireConstraint(t, err, "23514", "")
		})
	}
	for _, state := range []string{"completed", "failed", "cancelled", "interrupted"} {
		t.Run("valid terminal result "+state, func(t *testing.T) {
			tx := scheduledTasksConstraintTx(t, ctx, pool)
			if _, err := tx.Exec(ctx, `UPDATE task_runs SET state=$1,started_at='2025-01-02T00:00:00Z',finished_at='2025-01-03T00:00:00Z' WHERE id=repeat('1',32)`, state); err != nil {
				t.Fatalf("valid terminal run %s was rejected: %v", state, err)
			}
			if _, err := tx.Exec(ctx, `UPDATE task_run_children SET state=$1,scan_job_id='historical-result',started_at='2025-01-02T00:00:00Z',finished_at='2025-01-03T00:00:00Z' WHERE id=repeat('c',32)`, state); err != nil {
				t.Fatalf("valid terminal child %s was rejected: %v", state, err)
			}
		})
	}
}

func TestScheduledTasksMigrationEnforcesTriggerAndOccurrenceIdentity(t *testing.T) {
	ctx, pool := scheduledTasksConstraintPool(t)
	for _, test := range []struct{ name, statement, code, constraint string }{
		{"interval needs an anchor", `INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind,interval_ticks) VALUES(repeat('3',32),repeat('a',32),1,0,'interval',10000000)`, "23514", ""},
		{"daily time stays within a day", `INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind,time_of_day_ticks,timezone) VALUES(repeat('3',32),repeat('a',32),1,0,'daily',864000000000,'UTC')`, "23514", ""},
		{"weekly trigger needs weekday", `INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind,time_of_day_ticks,timezone) VALUES(repeat('3',32),repeat('a',32),1,0,'weekly',0,'UTC')`, "23514", ""},
		{"startup cannot carry calendar time", `INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind,time_of_day_ticks) VALUES(repeat('3',32),repeat('a',32),1,0,'startup',0)`, "23514", ""},
		{"runtime ticks cannot overflow duration", `INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind,max_runtime_ticks) VALUES(repeat('3',32),repeat('a',32),1,0,'startup',92233720368547759)`, "23514", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := scheduledTasksConstraintTx(t, ctx, pool)
			_, err := tx.Exec(ctx, test.statement)
			scheduledTasksRequireConstraint(t, err, test.code, test.constraint)
		})
	}
	t.Run("retired rule retains history and releases its position", func(t *testing.T) {
		tx := scheduledTasksConstraintTx(t, ctx, pool)
		if _, err := tx.Exec(ctx, `INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind) VALUES(repeat('3',32),repeat('a',32),1,0,'startup');
			INSERT INTO task_occurrences(id,task_id,trigger_id,schedule_revision,due_at,disposition,run_id)
			VALUES(repeat('5',32),repeat('a',32),repeat('3',32),1,'2025-02-01T00:00:00Z','admitted',repeat('1',32));
			UPDATE task_triggers SET retired_at=clock_timestamp() WHERE id=repeat('3',32);
			INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind) VALUES(repeat('4',32),repeat('a',32),2,0,'startup')`); err != nil {
			t.Fatalf("replacement discarded a referenced schedule: %v", err)
		}
		_, err := tx.Exec(ctx, `DELETE FROM task_triggers WHERE id=repeat('3',32)`)
		scheduledTasksRequireConstraint(t, err, "23503", "")
	})
	for _, test := range []struct{ name, values, code string }{
		{"same occurrence cannot be admitted twice", `'2025-02-01T00:00:00Z',NULL,1,'admitted',repeat('1',32)`, "23505"},
		{"admission requires a run", `'2025-02-02T00:00:00Z',NULL,1,'admitted',NULL`, "23514"},
		{"missed range cannot own a run", `'2025-02-02T00:00:00Z',NULL,1,'missed',repeat('1',32)`, "23514"},
		{"missed range cannot end before its first occurrence", `'2025-02-02T00:00:00Z','2025-02-01T00:00:00Z',2,'missed',NULL`, "23514"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := scheduledTasksConstraintTx(t, ctx, pool)
			if _, err := tx.Exec(ctx, `INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind) VALUES(repeat('3',32),repeat('a',32),1,0,'startup');
				INSERT INTO task_occurrences(id,task_id,trigger_id,schedule_revision,due_at,disposition,run_id)
				VALUES(repeat('5',32),repeat('a',32),repeat('3',32),1,'2025-02-01T00:00:00Z','admitted',repeat('1',32))`); err != nil {
				t.Fatal(err)
			}
			_, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO task_occurrences(id,task_id,trigger_id,schedule_revision,due_at,last_due_at,occurrence_count,disposition,run_id)
				VALUES(repeat('6',32),repeat('a',32),repeat('3',32),1,%s)`, test.values))
			scheduledTasksRequireConstraint(t, err, test.code, "")
		})
	}
}

func TestScheduledTasksMigrationRejectsCrossTaskTriggerAndOccurrenceLinks(t *testing.T) {
	ctx, pool := scheduledTasksConstraintPool(t)
	for _, test := range []struct{ name, statement, code, constraint string }{
		{"scheduled run cannot borrow another task's trigger", `UPDATE task_runs SET source='schedule',trigger_id=repeat('4',32),trigger_revision=7,scheduled_for=clock_timestamp() WHERE id=repeat('1',32)`, "23503", "task_runs_trigger_scope_fkey"},
		{"scheduled run cannot invent a trigger revision", `UPDATE task_runs SET source='schedule',trigger_id=repeat('3',32),trigger_revision=8,scheduled_for=clock_timestamp() WHERE id=repeat('1',32)`, "23503", "task_runs_trigger_scope_fkey"},
		{"scheduled run needs a complete trigger snapshot", `UPDATE task_runs SET source='schedule',trigger_id=repeat('3',32),scheduled_for=clock_timestamp() WHERE id=repeat('1',32)`, "23514", "task_runs_trigger_source_check"},
		{"manual run cannot carry a trigger snapshot", `UPDATE task_runs SET trigger_id=repeat('3',32),trigger_revision=7,scheduled_for=clock_timestamp() WHERE id=repeat('1',32)`, "23514", "task_runs_trigger_source_check"},
		{"occurrence cannot borrow another task's trigger", `INSERT INTO task_occurrences(id,task_id,trigger_id,schedule_revision,due_at,disposition,run_id) VALUES(repeat('5',32),repeat('a',32),repeat('4',32),7,clock_timestamp(),'overlap',repeat('1',32))`, "23503", "task_occurrences_trigger_scope_fkey"},
		{"occurrence cannot invent a trigger revision", `INSERT INTO task_occurrences(id,task_id,trigger_id,schedule_revision,due_at,disposition,run_id) VALUES(repeat('5',32),repeat('a',32),repeat('3',32),8,clock_timestamp(),'overlap',repeat('1',32))`, "23503", "task_occurrences_trigger_scope_fkey"},
		{"occurrence cannot attach another task's run", `INSERT INTO task_occurrences(id,task_id,trigger_id,schedule_revision,due_at,disposition,run_id) VALUES(repeat('5',32),repeat('a',32),repeat('3',32),7,clock_timestamp(),'overlap',repeat('2',32))`, "23503", "task_occurrences_run_scope_fkey"},
		{"active schedule positions cannot collide across revisions", `INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind) VALUES(repeat('5',32),repeat('a',32),8,0,'startup')`, "23505", "task_triggers_active_position_idx"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := scheduledTasksConstraintTx(t, ctx, pool)
			if _, err := tx.Exec(ctx, `INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind,time_of_day_ticks,timezone) VALUES
				(repeat('3',32),repeat('a',32),7,0,'daily',0,'UTC'),
				(repeat('4',32),repeat('b',32),7,0,'daily',0,'UTC')`); err != nil {
				t.Fatal(err)
			}
			_, err := tx.Exec(ctx, test.statement)
			scheduledTasksRequireConstraint(t, err, test.code, test.constraint)
		})
	}
	t.Run("scheduled run retains its exact retired trigger revision", func(t *testing.T) {
		tx := scheduledTasksConstraintTx(t, ctx, pool)
		if _, err := tx.Exec(ctx, `INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind,time_of_day_ticks,timezone)
			VALUES(repeat('3',32),repeat('a',32),7,0,'daily',0,'UTC');
			UPDATE task_runs SET source='schedule',trigger_id=repeat('3',32),trigger_revision=7,scheduled_for='2025-02-01T00:00:00Z'
			WHERE id=repeat('1',32);
			INSERT INTO task_occurrences(id,task_id,trigger_id,schedule_revision,due_at,disposition,run_id)
			VALUES(repeat('5',32),repeat('a',32),repeat('3',32),7,'2025-02-01T00:00:00Z','admitted',repeat('1',32));
			UPDATE task_triggers SET retired_at=clock_timestamp() WHERE id=repeat('3',32);
			INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind)
			VALUES(repeat('4',32),repeat('a',32),8,0,'startup')`); err != nil {
			t.Fatalf("a valid scheduled execution or its history was rejected: %v", err)
		}
		_, err := tx.Exec(ctx, "UPDATE task_triggers SET schedule_revision=9 WHERE id=repeat('3',32)")
		scheduledTasksRequireConstraint(t, err, "23503", "")
	})
}

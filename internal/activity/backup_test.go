package activity_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/database"
)

func activityMigrationVersion22(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	activityMigrationVersion21(t, ctx, pool)
	const name = "0022_activity_entries.sql"
	content, err := os.ReadFile(filepath.Join("..", "database", "migrations", name))
	if err != nil {
		t.Fatalf("read published activity migration: %v", err)
	}
	tx := beginActivityTransaction(t, ctx, pool)
	if _, err := tx.Exec(ctx, string(content)); err != nil {
		t.Fatalf("apply published activity migration: %v", err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES (22, $1)", name); err != nil {
		t.Fatalf("record published activity migration: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit schema 22 activity fixture: %v", err)
	}
}

func snapshotActivityRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(entry) ORDER BY id), '[]'::jsonb)::text
		FROM activity_entries entry`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot activity history: %v", err)
	}
	return snapshot
}

func snapshotActivityChecks(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_object('Name', conname,
		'Definition', pg_get_constraintdef(oid)) ORDER BY conname)::text
		FROM pg_catalog.pg_constraint WHERE conrelid = 'activity_entries'::regclass AND contype = 'c'`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot activity check constraints: %v", err)
	}
	return snapshot
}

func TestBackupActivityMigrationPreservesPublishedHistoryAndFindsRenamedChecks(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, false)
	activityMigrationVersion22(t, ctx, pool)
	seedActivityUser(t, ctx, pool)
	insertActivityEvent(t, ctx, pool, activityTestEvent("historical-user"))
	insertActivityEvent(t, ctx, pool, activity.Event{Action: activity.ActionTaskFinished,
		Source: activity.SourceSystem, Actor: activity.Actor{Kind: activity.ActorSystem},
		Resource: activity.Resource{Kind: activity.ResourceTaskRun, ID: "historical-task"}, State: activity.StateInterrupted})
	before := snapshotActivityRows(t, ctx, pool)

	// Renaming every CHECK proves discovery does not depend on PostgreSQL's
	// generated names, while keeping the published expressions unchanged.
	if _, err := pool.Exec(ctx, `DO $$ DECLARE entry record; counter integer := 0; BEGIN
		FOR entry IN SELECT conname FROM pg_catalog.pg_constraint
			WHERE conrelid = 'activity_entries'::regclass AND contype = 'c' ORDER BY conname
		LOOP
			counter := counter + 1;
			EXECUTE format('ALTER TABLE activity_entries RENAME CONSTRAINT %I TO %I',
				entry.conname, 'owned_historical_check_' || counter::text);
		END LOOP;
	END $$`); err != nil {
		t.Fatalf("rename owned historical activity checks: %v", err)
	}
	for range 2 {
		if err := database.Migrate(ctx, pool); err != nil {
			t.Fatalf("upgrade retained activity history: %v", err)
		}
	}
	if after := snapshotActivityRows(t, ctx, pool); after != before {
		t.Fatal("schema 23 changed or backfilled retained activity history")
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 26 {
		t.Fatalf("upgraded activity schema version = %d, want 26: %v", version, err)
	}
	insertActivityEvent(t, ctx, pool, activity.Event{Action: activity.ActionBackupFinished,
		Source: activity.SourceSystem, Actor: activity.Actor{Kind: activity.ActorSystem},
		Resource: activity.Resource{Kind: activity.ResourceBackup, ID: "new-backup"}, State: activity.StateCompleted})
	page := queryActivityPage(t, ctx, pool, activity.QueryOptions{Action: activity.ActionBackupFinished})
	if page.TotalRecordCount != 1 || len(page.Items) != 1 || page.Items[0].Name != "Backup completed" {
		t.Fatal("upgraded activity history did not accept and project a new backup event")
	}
}

func TestBackupActivityMigrationRejectsAmbiguousCatalogAtomically(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, false)
	activityMigrationVersion22(t, ctx, pool)
	insertActivityEvent(t, ctx, pool, activityTestEvent("retained-on-migration-failure"))
	before := snapshotActivityRows(t, ctx, pool)
	// Discovery reaches this second target after dropping the action check in
	// the same migration transaction. A rejection must restore that first drop.
	if _, err := pool.Exec(ctx, `ALTER TABLE activity_entries ADD CONSTRAINT owned_extra_resource_check
		CHECK (length(resource_kind) > 0)`); err != nil {
		t.Fatalf("install an ambiguous owned activity check: %v", err)
	}
	checksBefore := snapshotActivityChecks(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err == nil {
		t.Fatal("migration accepted ambiguous resource constraint discovery")
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 22 {
		t.Fatalf("failed activity migration changed its ledger: version=%d error=%v", version, err)
	}
	if after := snapshotActivityRows(t, ctx, pool); after != before {
		t.Fatal("failed migration changed historical activity rows")
	}
	if checksAfter := snapshotActivityChecks(t, ctx, pool); checksAfter != checksBefore {
		t.Fatal("rejected migration did not restore the original check constraints")
	}
	var newIndex int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_class
		WHERE relnamespace = current_schema()::regnamespace
		AND relname = 'activity_entries_backup_restore_phase_idx'`).Scan(&newIndex); err != nil {
		t.Fatalf("read catalog after rejected migration: %v", err)
	}
	if newIndex != 0 {
		t.Fatal("rejected migration created its phase index")
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE activity_entries DROP CONSTRAINT owned_extra_resource_check"); err != nil {
		t.Fatalf("remove owned ambiguous activity check: %v", err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade after removing the owned ambiguity: %v", err)
	}
}

const directBackupActivityInsert = `INSERT INTO activity_entries
	(action,severity,source,actor_kind,resource_kind,resource_id,state,changed_fields,revision,affected_count)
	VALUES ($1,'Info','system','system',$2,$3,$4,$5,$6,$7)`

func TestBackupRestoreActivityDatabaseRejectsInvalidFactsWithoutTypedWriter(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, true)
	fixtures := []struct {
		action   string
		resource string
		states   []string
	}{
		{"backup.requested", "backup", []string{""}},
		{"backup.cancel_requested", "backup", []string{""}},
		{"backup.finished", "backup", []string{"completed", "failed", "cancelled", "interrupted"}},
		{"backup.imported", "backup", []string{""}},
		{"backup.delete_requested", "backup", []string{""}},
		{"backup.deleted", "backup", []string{""}},
		{"backup.downloaded", "backup", []string{""}},
		{"restore.requested", "restore", []string{""}},
		{"restore.planned", "restore", []string{""}},
		{"restore.apply_requested", "restore", []string{""}},
		{"restore.applied", "restore", []string{"completed"}},
		{"restore.rollback_requested", "restore", []string{""}},
		{"restore.cancel_requested", "restore", []string{""}},
		{"restore.failed", "restore", []string{"failed"}},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.action, func(t *testing.T) {
			for stateIndex, state := range []string{"", "completed", "failed", "cancelled", "interrupted", "unknown"} {
				allowed := false
				for _, permitted := range fixture.states {
					allowed = allowed || state == permitted
				}
				_, err := pool.Exec(ctx, directBackupActivityInsert, fixture.action, fixture.resource,
					fixture.action+"-state-"+strconv.Itoa(stateIndex), state, []string{}, int64(0), int64(0))
				if allowed {
					if err != nil {
						t.Fatalf("database rejected allowed state %q: %v", state, err)
					}
				} else {
					assertBackupActivitySQLState(t, err, "23514")
				}
			}
			for _, invalid := range []struct {
				name, resource, id string
				fields             []string
				revision, count    int64
			}{
				{"wrong resource", "user", "invalid-resource", []string{}, 0, 0},
				{"private path", fixture.resource, "/private/backup.enc", []string{}, 0, 0},
				{"known field", fixture.resource, "invalid-known-field", []string{"Name"}, 0, 0},
				{"secret field", fixture.resource, "invalid-secret-field", []string{"Passphrase=private-value"}, 0, 0},
				{"negative revision", fixture.resource, "invalid-revision", []string{}, -1, 0},
				{"negative count", fixture.resource, "invalid-count", []string{}, 0, -1},
			} {
				t.Run(invalid.name, func(t *testing.T) {
					_, err := pool.Exec(ctx, directBackupActivityInsert, fixture.action, invalid.resource,
						invalid.id, fixture.states[0], invalid.fields, invalid.revision, invalid.count)
					assertBackupActivitySQLState(t, err, "23514")
				})
			}
		})
	}
}

func assertBackupActivitySQLState(t *testing.T, err error, code string) {
	t.Helper()
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != code {
		t.Fatalf("activity database failure = %v, want SQLSTATE %s", err, code)
	}
}

func TestBackupRestoreActivityUniquePhasesAllowRepeatedDownloadPreparation(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, true)
	for _, event := range []activity.Event{
		{Action: activity.ActionBackupRequested, Resource: activity.Resource{Kind: activity.ResourceBackup}},
		{Action: activity.ActionBackupCancelRequested, Resource: activity.Resource{Kind: activity.ResourceBackup}},
		{Action: activity.ActionBackupFinished, Resource: activity.Resource{Kind: activity.ResourceBackup}, State: activity.StateCompleted},
		{Action: activity.ActionBackupImported, Resource: activity.Resource{Kind: activity.ResourceBackup}},
		{Action: activity.ActionBackupDeleteRequested, Resource: activity.Resource{Kind: activity.ResourceBackup}},
		{Action: activity.ActionBackupDeleted, Resource: activity.Resource{Kind: activity.ResourceBackup}},
		{Action: activity.ActionRestoreRequested, Resource: activity.Resource{Kind: activity.ResourceRestore}},
		{Action: activity.ActionRestorePlanned, Resource: activity.Resource{Kind: activity.ResourceRestore}},
		{Action: activity.ActionRestoreApplyRequested, Resource: activity.Resource{Kind: activity.ResourceRestore}},
		{Action: activity.ActionRestoreApplied, Resource: activity.Resource{Kind: activity.ResourceRestore}, State: activity.StateCompleted},
		{Action: activity.ActionRestoreRollbackRequested, Resource: activity.Resource{Kind: activity.ResourceRestore}},
		{Action: activity.ActionRestoreCancelRequested, Resource: activity.Resource{Kind: activity.ResourceRestore}},
		{Action: activity.ActionRestoreFailed, Resource: activity.Resource{Kind: activity.ResourceRestore}, State: activity.StateFailed},
	} {
		t.Run(string(event.Action), func(t *testing.T) {
			event.Source, event.Actor = activity.SourceSystem, activity.Actor{Kind: activity.ActorSystem}
			event.Resource.ID = "shared-persisted-operation-identity"
			insertActivityEvent(t, ctx, pool, event)
			tx := beginActivityTransaction(t, ctx, pool)
			assertBackupActivitySQLState(t, activity.Record(ctx, tx, event), "23505")
			if err := tx.Rollback(ctx); err != nil {
				t.Fatalf("roll back duplicate activity replay: %v", err)
			}
			event.Resource.ID = "different-persisted-operation-identity"
			insertActivityEvent(t, ctx, pool, event)
		})
	}
	for range 2 {
		insertActivityEvent(t, ctx, pool, activity.Event{Action: activity.ActionBackupDownloaded,
			Source: activity.SourceNative, Actor: activity.Actor{Kind: activity.ActorUser, ID: "private-actor-marker", CredentialID: "private-session-marker"},
			Resource: activity.Resource{Kind: activity.ResourceBackup, ID: "private-resource-marker"}, Revision: 7, Count: 1})
		insertActivityEvent(t, ctx, pool, activityTestEvent("repeatable-old-action"))
	}
	page := queryActivityPage(t, ctx, pool, activity.QueryOptions{Action: activity.ActionBackupDownloaded})
	if page.TotalRecordCount != 2 || len(page.Items) != 2 {
		t.Fatal("separate download preparations were incorrectly deduplicated")
	}
	for _, entry := range page.Items {
		for _, marker := range []string{entry.Actor.ID, entry.Actor.CredentialID, entry.Resource.ID} {
			if strings.Contains(entry.Name, marker) || strings.Contains(entry.Overview, marker) {
				t.Fatal("activity descriptions interpolated a private or request-selected value")
			}
		}
		if len(entry.ChangedFields) != 0 || entry.Name != "Backup download prepared" ||
			entry.Overview != "A backup was prepared for an authorized download." {
			t.Fatal("download activity did not preserve its bounded preparation-only description")
		}
	}
	if page := queryActivityPage(t, ctx, pool, activity.QueryOptions{Action: activity.ActionUserUpdated}); page.TotalRecordCount != 2 {
		t.Fatal("backup phase uniqueness changed repeatability of pre-existing actions")
	}
}

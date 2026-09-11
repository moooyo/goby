//go:build linux

package backuppg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// Preserve complete PostgreSQL JSON text so bigint and JSON numbers never pass
// through floating-point decoding. Failure output identifies only the table.
func schema23ArchiveRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) string {
	t.Helper()
	return historicalArchiveRows(t, ctx, pool, table, 23)
}

// Only columns introduced after these historical schemas are omitted. Every
// original field, including unbounded credit types and exact JSON numbers,
// remains part of the ordered row multiset.
func historicalArchiveRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, version int64) string {
	t.Helper()
	projection := "to_jsonb(original)"
	if version < 25 && table == "item_metadata_state" {
		projection += " - 'music_source'"
	}
	if version < 25 && table == "item_entities" {
		projection += " - 'credit_group'"
	}
	statement := `SELECT COALESCE(jsonb_agg(` + projection + ` ORDER BY (` + projection + `)::text), '[]'::jsonb)::text FROM ` + pgx.Identifier{table}.Sanitize() + ` original`
	if table == "schema_migrations" {
		statement += fmt.Sprintf(" WHERE version <= %d", version)
	}
	var rows string
	if err := pool.QueryRow(ctx, statement).Scan(&rows); err != nil {
		t.Fatalf("snapshot schema %d archive table %s: %v", version, table, err)
	}
	return rows
}

func TestPostgreSQLRestoreSchema23ArchivePreservesDataAndMigratesToCurrent(t *testing.T) {
	restoreSchema23Archive(t, false)
}

func TestPostgreSQLOfflineSchema23RestoreFinalizerFailureRollsBackAndRetries(t *testing.T) {
	restoreSchema23Archive(t, true)
}

func restoreSchema23Archive(t *testing.T, offline bool) {
	t.Helper()
	ctx, source, target, options := recoveryFixtureAtVersion(t, 23)
	if version, err := database.SchemaVersion(ctx, source); err != nil || version != 23 {
		t.Fatalf("historical archive source schema = %d, want 23: %v", version, err)
	}
	var exists bool
	if err := source.QueryRow(ctx, "SELECT to_regclass('user_settings') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("schema 23 archive source already has user preferences: exists=%v error=%v", exists, err)
	}
	if _, err := source.Exec(ctx, `UPDATE users SET
		configuration='{"AudioLanguagePreference":"eng","SubtitleMode":"Default","EnableNextEpisodeAutoPlay":false,"LegacyExtension":{"ExactInteger":9007199254740993,"Keep":[1,true,"same"]}}'::jsonb,
		policy='{"EnableAllFolders":false,"EnabledFolders":["legacy-library"],"LegacyNull":null}'::jsonb,
		management_revision=9007199254740993 WHERE id='backup-admin';
		INSERT INTO sessions(id,user_id,token_hash,kind,created_at,expires_at,last_seen_at,client_capabilities)
		VALUES('schema-23-session','backup-admin',decode(repeat('a1',32),'hex'),'emby',
		'2020-01-01T00:00:00Z','2030-01-01T00:00:00Z','2020-01-02T00:00:00Z','{"PlayableMediaTypes":["Video"]}'::jsonb);
		INSERT INTO activity_entries(created_at,action,severity,source,actor_kind,resource_kind,resource_id,state)
		VALUES('2020-01-03T00:00:00Z','backup.finished','Info','system','system','backup','historical-schema-23-backup','completed')`); err != nil {
		t.Fatalf("seed schema 23 archive preservation witnesses: %v", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	if facts.SchemaVersion != 23 || len(facts.MigrationChecksums) != 23 || facts.MigrationChecksums[22].Version != 23 {
		t.Fatal("historical archive facts did not retain exactly the published schema 23 migration prefix")
	}
	if len(facts.Tables) != 29 {
		t.Fatalf("schema 23 archive table inventory = %d, want 29", len(facts.Tables))
	}
	before := make(map[string]string, len(facts.Tables))
	for _, table := range facts.Tables {
		if table.Name == "user_settings" {
			t.Fatal("schema 23 archive claims a table introduced only in schema 24")
		}
		before[table.Name] = schema23ArchiveRows(t, ctx, source, table.Name)
	}
	factsBefore, err := json.Marshal(facts)
	if err != nil {
		t.Fatal("snapshot immutable schema 23 source facts")
	}
	var result RestoreResult
	if offline {
		offlineOptions := options
		offlineOptions.SourceURL = unavailableSourceURL(t, options.SourceURL)
		beforeSource, sequences := unchangedSourceWitness(t, ctx, source, options)
		refused := errors.New("owned schema 23 finalizer refusal")
		called, written := false, false
		failed, restoreErr := RestoreOfflineFinalized(ctx, target, archive, facts, offlineOptions,
			func(callbackCtx context.Context, tx pgx.Tx, raw RestoreResult) error {
				called = true
				if raw.SourceVersion != 23 || raw.CurrentVersion < 26 || !equalJSON(raw.Tables, facts.Tables) {
					return errors.New("finalizer did not receive preserved source facts after migration")
				}
				var version int64
				var preferenceCount int
				if err := tx.QueryRow(callbackCtx, `SELECT
					(SELECT max(version) FROM schema_migrations),
					(SELECT count(*) FROM user_settings)`).Scan(&version, &preferenceCount); err != nil {
					return err
				}
				if version != raw.CurrentVersion || preferenceCount != 0 {
					return errors.New("finalizer did not observe the migrated empty preference table")
				}
				var users string
				if err := tx.QueryRow(callbackCtx, `SELECT COALESCE(jsonb_agg(to_jsonb(original)
					ORDER BY to_jsonb(original)::text), '[]'::jsonb)::text FROM users original`).Scan(&users); err != nil {
					return err
				}
				if users != before["users"] {
					return errors.New("finalizer did not receive the unchanged historical user rows")
				}
				// These successful writes happen after both data restoration and
				// migration. Refusing the finalizer must undo all three stages.
				if _, err := tx.Exec(callbackCtx, `UPDATE users SET name='Uncommitted finalizer mutation'
					WHERE id='backup-admin';
					INSERT INTO user_settings(user_id,settings,updated_at)
					VALUES('backup-admin','{"Theme":"uncommitted"}'::jsonb,'2020-01-06T00:00:00Z')`); err != nil {
					return err
				}
				written = true
				return refused
			})
		if !called || !written || !errors.Is(restoreErr, refused) || failed.SourceVersion != 0 ||
			failed.CurrentVersion != 0 || len(failed.Tables) != 0 {
			t.Fatalf("schema 23 finalizer refusal did not reject completed transactional writes: %v", restoreErr)
		}
		var objects int
		if err := target.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1) +
			(SELECT count(*) FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=$1) +
			(SELECT count(*) FROM pg_catalog.pg_type t JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname=$1)`,
			options.Schema).Scan(&objects); err != nil || objects != 0 {
			t.Fatalf("failed cross-version finalizer retained tables, sequences, functions, or types: count=%d error=%v", objects, err)
		}
		assertSourceWitness(t, ctx, source, options, beforeSource, sequences)
		if _, err := archive.Seek(0, 0); err != nil {
			t.Fatal("rewind the same schema 23 archive after finalizer rollback")
		}
		// A retry uses the same archive and formerly failed target, with the
		// trusted source still unreachable through the offline configuration.
		result, err = RestoreOffline(ctx, target, archive, facts, offlineOptions)
		assertSourceWitness(t, ctx, source, options, beforeSource, sequences)
	} else {
		result, err = Restore(ctx, source, target, archive, facts, options)
	}
	if err != nil {
		logFixtureCatalogDifference(t, ctx, target, options)
		t.Fatalf("restore real schema 23 pg_dump archive into current migrations: %v", err)
	}
	migrations, err := database.EmbeddedMigrations()
	if err != nil || len(migrations) == 0 {
		t.Fatal("read current compiled migration inventory")
	}
	current := migrations[len(migrations)-1].Version
	if current < 26 || result.SourceVersion != 23 || result.CurrentVersion != current || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("cross-version restore result source=%d current=%d, want source 23 and compiled version %d with unchanged source table facts", result.SourceVersion, result.CurrentVersion, current)
	}
	factsAfter, err := json.Marshal(facts)
	if err != nil || string(factsAfter) != string(factsBefore) {
		t.Fatal("cross-version restoration rewrote the original schema 23 source facts")
	}
	if version, err := database.SchemaVersion(ctx, target); err != nil || version != current {
		t.Fatalf("restored target schema = %d, want %d: %v", version, current, err)
	}
	if version, err := database.SchemaVersion(ctx, source); err != nil || version != 23 {
		t.Fatalf("cross-version restoration migrated its source: schema=%d error=%v", version, err)
	}
	for table, rows := range before {
		if restored := schema23ArchiveRows(t, ctx, target, table); restored != rows {
			t.Errorf("cross-version restoration changed historical fields or rows in %s", table)
		}
		if original := schema23ArchiveRows(t, ctx, source, table); original != rows {
			t.Errorf("cross-version restoration changed source table %s", table)
		}
	}
	var count int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM user_settings").Scan(&count); err != nil || count != 0 {
		t.Fatalf("restored schema 23 configuration was backfilled into separate user preferences: count=%d error=%v", count, err)
	}
	assertHistoricalArchiveMusicDefaults(t, ctx, target)
	assertHistoricalArchiveThemeDefaults(t, ctx, target)
	var migrationName string
	if err := target.QueryRow(ctx, "SELECT name FROM schema_migrations WHERE version=24").Scan(&migrationName); err != nil || migrationName != "0024_user_settings.sql" {
		t.Fatalf("restored schema 24 migration name = %q: %v", migrationName, err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != len(migrations) {
		t.Fatalf("restored current migration history count = %d, want %d: %v", count, len(migrations), err)
	}
	if _, err := target.Exec(ctx, `INSERT INTO user_settings(user_id,settings)
		VALUES('backup-admin','{"MaxStreamingBitrate":"4000000","SubtitleMode":"Smart"}'::jsonb)`); err != nil {
		t.Fatalf("write new independent preferences after historical restore: %v", err)
	}
	if restored := schema23ArchiveRows(t, ctx, target, "users"); restored != before["users"] {
		t.Error("new preferences after historical restore changed legacy user configuration or account fields")
	}
	if err := source.QueryRow(ctx, "SELECT to_regclass('user_settings') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("restoring or using target preferences added schema 24 state to the source: exists=%v error=%v", exists, err)
	}
}

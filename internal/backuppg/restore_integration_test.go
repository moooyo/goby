//go:build linux

package backuppg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/database"
)

// These tests require two explicitly disposable databases owned by distinct,
// non-superuser roles. They create and remove only their freshly named schema.
// The outside test operator owns database and role provisioning and removal.
func recoveryFixture(t *testing.T) (context.Context, *pgxpool.Pool, *pgxpool.Pool, Options) {
	t.Helper()
	return recoveryFixtureAtVersion(t, 0)
}

// A historical source starts from its exact compiled migration prefix. Never
// emulate an older archive by deleting history from a current schema.
func recoveryFixtureAtVersion(t *testing.T, version int64) (context.Context, *pgxpool.Pool, *pgxpool.Pool, Options) {
	t.Helper()
	sourceURL, targetURL := os.Getenv("GOBY_TEST_BACKUP_SOURCE_DATABASE_URL"), os.Getenv("GOBY_TEST_BACKUP_TARGET_DATABASE_URL")
	if sourceURL == "" || targetURL == "" {
		t.Skip("two disposable backup test databases are required")
	}
	if os.Getenv("GOBY_TEST_BACKUP_DISPOSABLE_DATABASES") != "1" {
		t.Fatal("explicit disposable database marker is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	sourceConfig, err := pgxpool.ParseConfig(sourceURL)
	if err != nil {
		t.Fatal("parse source database configuration")
	}
	targetConfig, err := pgxpool.ParseConfig(targetURL)
	if err != nil {
		t.Fatal("parse target database configuration")
	}
	if sourceConfig.ConnConfig.Database == targetConfig.ConnConfig.Database || sourceConfig.ConnConfig.User == targetConfig.ConnConfig.User || !strings.HasPrefix(sourceConfig.ConnConfig.Database, "goby_backup_") || !strings.HasPrefix(targetConfig.ConnConfig.Database, "goby_backup_") {
		t.Fatal("test databases must have independent goby_backup_ names and roles")
	}
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal("generate owned schema name")
	}
	schema := "goby_backup_test_" + hex.EncodeToString(suffix[:])
	for _, config := range []*pgxpool.Config{sourceConfig, targetConfig} {
		config.MaxConns = 4
		config.ConnConfig.RuntimeParams["search_path"] = schema
	}
	source, err := pgxpool.NewWithConfig(ctx, sourceConfig)
	if err != nil {
		t.Fatal("open source fixture database")
	}
	t.Cleanup(source.Close)
	target, err := pgxpool.NewWithConfig(ctx, targetConfig)
	if err != nil {
		t.Fatal("open target fixture database")
	}
	t.Cleanup(target.Close)
	for _, pool := range []*pgxpool.Pool{source, target} {
		if _, err := pool.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
			t.Fatal("create independently owned test schema")
		}
		pool := pool
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if _, err := pool.Exec(cleanupCtx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`); err != nil {
				t.Error("remove only the owned test schema")
			}
		})
	}
	if version == 0 {
		if err := database.Migrate(ctx, source); err != nil {
			t.Fatalf("apply compiled source migrations: %v", err)
		}
	} else {
		tx, err := source.Begin(ctx)
		if err != nil {
			t.Fatal("begin historical source migration")
		}
		defer rollback(tx)
		if err := database.RecoveryMigrateTo(ctx, tx, version); err != nil {
			t.Fatalf("apply historical source migrations through schema %d: %v", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal("commit historical source migrations")
		}
	}
	if _, err := source.Exec(ctx, `INSERT INTO server_settings(key,value,created_at,updated_at) VALUES('server_id','backup-test-server','2020-01-01T00:00:00Z','2020-01-02T00:00:00Z');
		INSERT INTO users(id,name,normalized_name,password_hash,is_administrator,created_at,updated_at) VALUES('backup-admin','Before snapshot','before snapshot','test-only-password-hash',true,'2020-01-01T00:00:00Z','2020-01-02T00:00:00Z');
		INSERT INTO devices(reported_device_id,reported_name,last_user_id) VALUES('snapshot-device','Snapshot device','backup-admin');
		INSERT INTO application_key_devices(reported_device_id) VALUES('snapshot-key-device');
		INSERT INTO catalog_entities(kind,name) VALUES('Genre',E'Precise\\name\n\u65e5\u672c\u8a9e');
		UPDATE managed_settings SET revision=9007199254740993,server_name='Immutable snapshot',server_name_mode='custom';`); err != nil {
		t.Fatal("seed source fixture rows")
	}
	options := Options{SourceURL: sourceURL, PGDump: os.Getenv("GOBY_TEST_PG_DUMP"), PGRestore: os.Getenv("GOBY_TEST_PG_RESTORE"), Schema: schema, Timeout: 90 * time.Second, MaxDumpBytes: 8 << 20, ProbeVersion: 6}
	if options.PGDump == "" || options.PGRestore == "" {
		t.Fatal("explicit PostgreSQL 17 tool paths are required")
	}
	return ctx, source, target, options
}

func sourceArchive(t *testing.T, ctx context.Context, source *pgxpool.Pool, options Options) (*os.File, backupformat.SourceFacts) {
	t.Helper()
	snapshot, err := OpenSnapshot(ctx, source, options)
	if err != nil {
		logFixtureCatalogDifference(t, ctx, source, options)
		t.Fatalf("open exported snapshot: %v", err)
	}
	defer snapshot.Close()
	facts, err := snapshot.Facts(ctx)
	if err != nil {
		t.Fatalf("read snapshot fingerprints: %v", err)
	}
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "database.dump"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal("create private dump fixture")
	}
	t.Cleanup(func() { _ = file.Close() })
	if err := snapshot.Dump(ctx, file); err != nil {
		t.Fatalf("create real pg_dump archive: %v", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal("rewind archive")
	}
	return file, facts
}

func TestPostgreSQLBackupConsistentSnapshotAndRestore(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	const preferences = `{"MaxStreamingBitrate":"4000000","SubtitleMode":"Smart","Theme":"dark","Custom":"{Keep:[1,true,same]}"}`
	const viewerPreferences = `{"MaxStreamingBitrate":"900000","SubtitleMode":"Always","Theme":"light","ViewerOnly":"retained"}`
	if _, err := source.Exec(ctx, `INSERT INTO users(id,name,normalized_name,password_hash,configuration,created_at,updated_at)
		VALUES('backup-viewer','Snapshot viewer','snapshot viewer','test-only-viewer-password-hash',
		'{"AudioLanguagePreference":"fra"}'::jsonb,'2020-01-01T00:00:00Z','2020-01-02T00:00:00Z')`); err != nil {
		t.Fatal("seed an independent preference owner before snapshot")
	}
	if _, err := source.Exec(ctx, `INSERT INTO user_settings(user_id,settings,updated_at)
		VALUES('backup-admin',$1::jsonb,'2020-01-03T00:00:00Z'),
		('backup-viewer',$2::jsonb,'2020-01-04T00:00:00Z')`, preferences, viewerPreferences); err != nil {
		t.Fatal("seed separate user preference maps before snapshot")
	}
	musicBefore := seedMusicSnapshotWitness(t, ctx, source)
	snapshot, err := OpenSnapshot(ctx, source, options)
	if err != nil {
		logFixtureCatalogDifference(t, ctx, source, options)
		t.Fatalf("open exported snapshot: %v", err)
	}
	defer snapshot.Close()
	facts, err := snapshot.Facts(ctx)
	if err != nil {
		t.Fatal("read snapshot fingerprints")
	}
	if facts.SchemaVersion != 27 || len(facts.MigrationChecksums) != 27 || len(facts.Tables) != 35 {
		t.Fatal("the current archive fixture did not describe all schema27 tables")
	}
	if _, err := source.Exec(ctx, `UPDATE users SET name='After snapshot' WHERE id='backup-admin';
		UPDATE managed_settings SET revision=revision+1;
		UPDATE user_settings SET settings=jsonb_set(settings,'{Theme}','"after-snapshot"'::jsonb),
		updated_at='2020-01-05T00:00:00Z';
		UPDATE item_metadata_state SET music_source=jsonb_set(music_source,'{Name}','"Later accepted title"'::jsonb),
		automatic=jsonb_set(automatic,'{Name}','"Later accepted title"'::jsonb),
		effective=jsonb_set(effective,'{Name}','"Later accepted title"'::jsonb),revision=revision+1,
		updated_at='2020-01-05T00:00:00Z' WHERE item_id='music-snapshot-track';
		UPDATE item_metadata_state SET source_key=source_key || jsonb_build_object('MusicSourceHash',encode(sha256(convert_to(music_source::text,'UTF8')),'hex'))
		WHERE item_id='music-snapshot-track';
		UPDATE items SET name='Later accepted title',sort_name='later accepted title' WHERE id='music-snapshot-track';
		INSERT INTO devices(reported_device_id) VALUES('later-device');`); err != nil {
		t.Fatal("commit independent newer state")
	}
	musicAfter := musicSnapshotState(t, ctx, source)
	if musicAfter == musicBefore {
		t.Fatal("the concurrent source commit did not change its accepted music witness")
	}
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "database.dump"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal("create private archive")
	}
	defer file.Close()
	if err := snapshot.Dump(ctx, file); err != nil {
		t.Fatalf("dump original snapshot after concurrent commit: %v", err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal("close original snapshot")
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal("rewind archive")
	}
	result, err := Restore(ctx, source, target, file, facts, options)
	if err != nil {
		logFixtureCatalogDifference(t, ctx, target, options)
		t.Fatalf("restore decoded archive: %v", err)
	}
	if result.SourceVersion != facts.SchemaVersion || result.CurrentVersion != 27 || !equalJSON(result.Tables, facts.Tables) {
		t.Fatal("restore result differs from original snapshot")
	}
	if musicSnapshotState(t, ctx, target) != musicBefore || musicSnapshotState(t, ctx, source) != musicAfter {
		t.Fatal("restore changed the exported music facts, relation IDs, role keys, or later source state")
	}
	assertMusicSnapshotIdentity(t, ctx, target)
	var sourceName, targetName string
	var revision int64
	var count int
	if source.QueryRow(ctx, `SELECT name FROM users WHERE id='backup-admin'`).Scan(&sourceName) != nil || sourceName != "After snapshot" {
		t.Fatal("restore modified newer source state")
	}
	if target.QueryRow(ctx, `SELECT name FROM users WHERE id='backup-admin'`).Scan(&targetName) != nil || targetName != "Before snapshot" {
		t.Fatal("restore did not preserve the exported snapshot")
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM user_settings").Scan(&count); err != nil || count != 2 {
		t.Fatal("restore changed the number of independent preference owners")
	}
	for _, expected := range []struct{ userID, settings, updatedAt string }{
		{"backup-admin", preferences, "2020-01-03T00:00:00Z"},
		{"backup-viewer", viewerPreferences, "2020-01-04T00:00:00Z"},
	} {
		var preferencesPreserved, sourcePreserved bool
		if err := target.QueryRow(ctx, `SELECT settings=$2::jsonb AND updated_at=$3::timestamptz
			FROM user_settings WHERE user_id=$1`, expected.userID, expected.settings, expected.updatedAt).Scan(&preferencesPreserved); err != nil || !preferencesPreserved {
			t.Fatalf("restore mixed the preference map or exact update time for %s", expected.userID)
		}
		if err := source.QueryRow(ctx, `SELECT settings=jsonb_set($2::jsonb,'{Theme}','"after-snapshot"'::jsonb)
			AND updated_at='2020-01-05T00:00:00Z'::timestamptz FROM user_settings WHERE user_id=$1`,
			expected.userID, expected.settings).Scan(&sourcePreserved); err != nil || !sourcePreserved {
			t.Fatalf("restore changed the later source preferences for %s", expected.userID)
		}
	}
	if target.QueryRow(ctx, `SELECT revision FROM managed_settings`).Scan(&revision) != nil || revision != 9007199254740993 {
		t.Fatal("restore lost exact bigint precision")
	}
	if target.QueryRow(ctx, `SELECT count(*) FROM devices`).Scan(&count) != nil || count != 1 {
		t.Fatal("snapshot included a later device")
	}
	var next int64
	if target.QueryRow(ctx, `INSERT INTO devices(reported_device_id) VALUES('post-restore-device') RETURNING id`).Scan(&next) != nil || next <= 3 {
		t.Fatal("sequence state cannot allocate a safe next ID")
	}
	if target.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_constraint k JOIN pg_catalog.pg_class c ON c.oid=k.conrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND k.contype='f' AND NOT k.convalidated`, options.Schema).Scan(&count) != nil || count != 0 {
		t.Fatal("restored foreign keys were not validated")
	}
	if target.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_trigger t JOIN pg_catalog.pg_class c ON c.oid=t.tgrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND NOT t.tgisinternal AND t.tgenabled<>'O'`, options.Schema).Scan(&count) != nil || count != 0 {
		t.Fatal("restored user triggers were not re-enabled")
	}
}

func TestPostgreSQLRestoreFingerprintFailureLeavesEmptyTarget(t *testing.T) {
	for _, failure := range []string{"fingerprint", "shared_sequence"} {
		t.Run(failure, func(t *testing.T) {
			ctx, source, target, options := recoveryFixture(t)
			if failure == "shared_sequence" {
				if _, err := source.Exec(ctx, `SELECT setval('devices_id_seq',3,false)`); err != nil {
					t.Fatal("set shared sequence collision fixture")
				}
			}
			file, facts := sourceArchive(t, ctx, source, options)
			if failure == "fingerprint" {
				facts.Tables[0].SHA256 = strings.Repeat("0", 64)
			}
			if _, err := Restore(ctx, source, target, file, facts, options); !errors.Is(err, ErrArchive) {
				logFixtureCatalogDifference(t, ctx, source, options)
				t.Fatalf("tampered table fingerprint error = %v", err)
			}
			var count int
			if target.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1`, options.Schema).Scan(&count) != nil || count != 0 {
				t.Fatal("failed restore retained partial schema or sequence objects")
			}
			var name string
			if source.QueryRow(ctx, `SELECT name FROM users WHERE id='backup-admin'`).Scan(&name) != nil || name != "Before snapshot" {
				t.Fatal("failed restore modified the source")
			}
		})
	}
}

func TestPostgreSQLRestoreRejectsPopulatedTarget(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	file, facts := sourceArchive(t, ctx, source, options)
	if _, err := target.Exec(ctx, `CREATE TABLE must_survive(id integer PRIMARY KEY); INSERT INTO must_survive VALUES(42)`); err != nil {
		t.Fatal("create target preservation witness")
	}
	if _, err := Restore(ctx, source, target, file, facts, options); !errors.Is(err, ErrTarget) {
		t.Fatalf("populated target error = %v", err)
	}
	var id int
	if target.QueryRow(ctx, `SELECT id FROM must_survive`).Scan(&id) != nil || id != 42 {
		t.Fatal("restore changed existing target data")
	}
}

func TestPostgreSQLSnapshotRejectsSchemaDrift(t *testing.T) {
	for name, statement := range map[string]string{
		"column":      `ALTER TABLE users ADD COLUMN untrusted_extra text`,
		"function":    `CREATE FUNCTION unexpected_function() RETURNS integer LANGUAGE sql AS 'SELECT 1'`,
		"trigger":     `ALTER TABLE catalog_entities DISABLE TRIGGER catalog_entities_name_hash`,
		"index":       `CREATE INDEX unexpected_users_name ON users(name)`,
		"constraint":  `ALTER TABLE devices DROP CONSTRAINT devices_revision_check`,
		"inheritance": `CREATE TABLE public.backup_inheritance_child () INHERITS(users)`,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, source, _, options := recoveryFixture(t)
			if _, err := source.Exec(ctx, statement); err != nil {
				t.Fatal("create source schema drift")
			}
			if name == "inheritance" {
				t.Cleanup(func() {
					cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					if _, err := source.Exec(cleanupCtx, `DROP TABLE public.backup_inheritance_child`); err != nil {
						t.Error("remove only the owned inheritance fixture")
					}
				})
			}
			if snapshot, err := OpenSnapshot(ctx, source, options); !errors.Is(err, ErrSchema) {
				if snapshot != nil {
					_ = snapshot.Close()
				}
				t.Fatalf("schema drift error = %v", err)
			}
		})
	}
}

func TestPostgreSQLSnapshotCancellationAndSettingsIsolation(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	var before string
	if source.QueryRow(ctx, `SELECT current_setting('statement_timeout')`).Scan(&before) != nil {
		t.Fatal("read ordinary connection setting")
	}
	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	snapshot, err := OpenSnapshot(jobCtx, source, options)
	if err != nil {
		logFixtureCatalogDifference(t, ctx, source, options)
		t.Fatalf("open bounded snapshot: %v", err)
	}
	defer snapshot.Close()
	if _, err := snapshot.Facts(ctx); err != nil {
		t.Fatal("cache valid snapshot facts before cancellation")
	}
	var timeout string
	if snapshot.Tx().QueryRow(snapshot.Context(), `SELECT current_setting('idle_in_transaction_session_timeout')`).Scan(&timeout) != nil || timeout == "30s" || timeout == "0" {
		t.Fatal("snapshot did not set its own finite idle budget")
	}
	cancel()
	<-snapshot.Context().Done()
	if _, err := snapshot.Facts(ctx); err == nil {
		t.Fatal("expired snapshot accepted work")
	}
	_ = snapshot.Close()
	if source.QueryRow(ctx, `SELECT current_setting('statement_timeout')`).Scan(&timeout) != nil || timeout != before {
		t.Fatal("snapshot settings leaked beyond its transaction")
	}
}

// Diagnostics contain only schema definitions from this test's fresh trusted
// migrations, never table data, passwords, URLs, or runtime configuration.
func logFixtureCatalogDifference(t *testing.T, ctx context.Context, pool *pgxpool.Pool, options Options) {
	t.Helper()
	version, err := database.SchemaVersion(ctx, pool)
	if err != nil || version < 1 {
		return
	}
	actualBytes, err := ExportCatalog(ctx, pool, options.Schema, version)
	if err != nil {
		t.Logf("Catalog diagnostic unavailable: %v", err)
		return
	}
	expectedBytes, err := catalogFiles.ReadFile(fmt.Sprintf("catalogs/schema-%d-postgresql-17.json", version))
	if err != nil {
		return
	}
	var actual, expected catalogBaseline
	if json.Unmarshal(actualBytes, &actual) != nil || json.Unmarshal(expectedBytes, &expected) != nil {
		return
	}
	var actualObjects, expectedObjects []json.RawMessage
	if json.Unmarshal(actual.Objects, &actualObjects) != nil || json.Unmarshal(expected.Objects, &expectedObjects) != nil {
		return
	}
	for index := 0; index < min(len(actualObjects), len(expectedObjects)); index++ {
		a, _ := normalizeCatalogObjects(actualObjects[index])
		e, _ := normalizeCatalogObjects(expectedObjects[index])
		if string(a) != string(e) {
			t.Logf("Catalog object difference %d: actual=%s expected=%s", index, a, e)
			return
		}
	}
	if !equalJSON(actual.Catalog, expected.Catalog) {
		a, _ := json.Marshal(actual.Catalog)
		e, _ := json.Marshal(expected.Catalog)
		t.Logf("Catalog inventory difference: actual=%s expected=%s", a, e)
		return
	}
	t.Log("Catalog diagnostic found matching metadata after the failed operation")
}

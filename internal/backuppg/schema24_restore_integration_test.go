//go:build linux

package backuppg

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

func assertHistoricalArchiveMusicDefaults(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var sources, groups, artists int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM item_metadata_state WHERE music_source IS DISTINCT FROM '{}'::jsonb),
		(SELECT count(*) FROM item_entities WHERE credit_group IS DISTINCT FROM 0),
		(SELECT count(*) FROM catalog_entities WHERE kind='MusicArtist')`).Scan(&sources, &groups, &artists); err != nil || sources != 0 || groups != 0 || artists != 0 {
		t.Fatalf("historical restoration inferred music provenance or identities: sources=%d groups=%d artists=%d error=%v", sources, groups, artists, err)
	}
}

// This archive is generated from the actual schema24 prefix with two separate
// preference owners and a legal old credit type larger than a B-tree key. The
// offline retry must preserve those rows while applying the real schema28 DDL.
func TestPostgreSQLOfflineSchema24RestorePreservesDataAndRetriesFinalizer(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 24)
	if _, err := source.Exec(ctx, `INSERT INTO users(id,name,normalized_name,password_hash,configuration)
		VALUES('schema24-viewer','Historical viewer','historical viewer','historical-test-digest',
		'{"AudioLanguagePreference":"fra","ExactInteger":9007199254740993}'::jsonb);
		INSERT INTO user_settings(user_id,settings,updated_at) VALUES
		('backup-admin','{"Theme":"dark","AdminOnly":"preserved"}'::jsonb,'2020-01-03T00:00:00Z'),
		('schema24-viewer','{"Theme":"light","ViewerOnly":"preserved"}'::jsonb,'2020-01-04T00:00:00Z');
		INSERT INTO libraries(id,name,collection_type) VALUES('schema24-library','Historical metadata','music');
		INSERT INTO items(id,library_id,name,sort_name,type,local_metadata)
		SELECT 'schema24-item','schema24-library','Historical title','historical title','Audio',
		jsonb_build_object('Genres',jsonb_build_array('Historical genre'),'Tags',jsonb_build_array('Historical tag'),
		'Studios',jsonb_build_array('Historical studio'),'People',jsonb_build_array(jsonb_build_object(
		'Name','Historical person','Role','Retained role','Type',(SELECT string_agg(md5(g::text),'' ORDER BY g) FROM generate_series(1,1024) g))));
		SELECT sync_catalog_item_entities(id,local_metadata) FROM items WHERE id='schema24-item';
		UPDATE item_metadata_state SET overrides='{"Name":"Historical override"}'::jsonb,
		locked_values='{"Tags":["Historical tag"]}'::jsonb,revision=9007199254740993,
		last_edited_by='backup-admin',last_edited_at='2020-01-05T00:00:00Z',updated_at='2020-01-06T00:00:00Z'
		WHERE item_id='schema24-item'`); err != nil {
		t.Fatalf("seed real schema24 archive witnesses: %v", err)
	}
	var legacyBytes int
	if err := source.QueryRow(ctx, "SELECT octet_length(credit_type) FROM item_entities WHERE role='Retained role'").Scan(&legacyBytes); err != nil || legacyBytes != 32768 {
		t.Fatalf("historical archive lacks the long unbounded credit witness: bytes=%d error=%v", legacyBytes, err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	if facts.SchemaVersion != 24 || len(facts.MigrationChecksums) != 24 || len(facts.Tables) != 30 {
		t.Fatal("the archive is not the genuine published schema24 migration prefix")
	}
	before := make(map[string]string, len(facts.Tables))
	for _, table := range facts.Tables {
		before[table.Name] = historicalArchiveRows(t, ctx, source, table.Name, 24)
	}
	factsBefore, err := json.Marshal(facts)
	if err != nil {
		t.Fatal("snapshot schema24 source facts")
	}
	beforeSource, sequences := unchangedSourceWitness(t, ctx, source, options)
	offlineOptions := options
	offlineOptions.SourceURL = unavailableSourceURL(t, options.SourceURL)
	refused := errors.New("schema24 finalizer refusal after migrated writes")
	called := false
	failed, restoreErr := RestoreOfflineFinalized(ctx, target, archive, facts, offlineOptions,
		func(callbackCtx context.Context, tx pgx.Tx, result RestoreResult) error {
			if result.SourceVersion != 24 || result.CurrentVersion != 28 || !equalJSON(result.Tables, facts.Tables) {
				return errors.New("schema24 finalizer received incorrect archive or target facts")
			}
			var preferenceRows string
			if err := tx.QueryRow(callbackCtx, `SELECT jsonb_agg(to_jsonb(p) ORDER BY to_jsonb(p)::text)::text FROM user_settings p`).Scan(&preferenceRows); err != nil {
				return err
			}
			if preferenceRows != before["user_settings"] {
				return errors.New("schema24 finalizer lost separate preference owners")
			}
			if _, err := tx.Exec(callbackCtx, `UPDATE user_settings SET settings='{"Uncommitted":"discard"}'::jsonb;
				UPDATE item_metadata_state SET music_source='{"Version":1,"Artists":["Uncommitted"],"AlbumArtists":[]}'::jsonb;
				SELECT sync_catalog_item_entities('schema24-item','{"Artists":["Uncommitted"],"AlbumArtists":["Uncommitted"]}'::jsonb)`); err != nil {
				return err
			}
			called = true
			return refused
		})
	if !called || !errors.Is(restoreErr, refused) || failed.SourceVersion != 0 || failed.CurrentVersion != 0 || len(failed.Tables) != 0 {
		t.Fatalf("schema24 finalizer did not reject its completed migrated writes: %v", restoreErr)
	}
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal("inspect the rejected schema24 target")
	}
	inspectionErr := validateEmptyTarget(ctx, tx, options.Schema)
	rollback(tx)
	if inspectionErr != nil {
		t.Fatalf("schema24 finalizer refusal left restored or migrated objects: %v", inspectionErr)
	}
	assertSourceWitness(t, ctx, source, options, beforeSource, sequences)
	if _, err := archive.Seek(0, 0); err != nil {
		t.Fatal("rewind the exact schema24 archive after finalizer rollback")
	}
	result, err := RestoreOffline(ctx, target, archive, facts, offlineOptions)
	if err != nil {
		logFixtureCatalogDifference(t, ctx, target, options)
		t.Fatalf("retry the real schema24 archive into schema28: %v", err)
	}
	if result.SourceVersion != 24 || result.CurrentVersion != 28 || !equalJSON(result.Tables, facts.Tables) {
		t.Fatal("schema24 retry changed source facts or did not reach schema28")
	}
	if version, err := database.SchemaVersion(ctx, target); err != nil || version != 28 {
		t.Fatalf("schema24 restored target version=%d, want 28: %v", version, err)
	}
	if version, err := database.SchemaVersion(ctx, source); err != nil || version != 24 {
		t.Fatalf("offline restore changed original schema24: version=%d error=%v", version, err)
	}
	for table, expected := range before {
		if historicalArchiveRows(t, ctx, target, table, 24) != expected {
			t.Errorf("schema24 restoration changed original rows or fields in %s", table)
		}
		if historicalArchiveRows(t, ctx, source, table, 24) != expected {
			t.Errorf("schema24 restoration changed its source table %s", table)
		}
	}
	assertHistoricalArchiveMusicDefaults(t, ctx, target)
	assertHistoricalArchiveThemeDefaults(t, ctx, target)
	assertHistoricalArchiveExtraDefaults(t, ctx, target)
	assertHistoricalArchiveBindingDefaults(t, ctx, target)
	assertSourceWitness(t, ctx, source, options, beforeSource, sequences)
	factsAfter, err := json.Marshal(facts)
	if err != nil || string(factsAfter) != string(factsBefore) {
		t.Fatal("schema24 restoration mutated the archived source facts")
	}
	var historyCount int
	var musicMigrationName, themeMigrationName, extraMigrationName, bindingMigrationName string
	if err := target.QueryRow(ctx, `SELECT count(*),max(name) FILTER (WHERE version=25),max(name) FILTER (WHERE version=26),max(name) FILTER (WHERE version=27),max(name) FILTER (WHERE version=28)
		FROM schema_migrations`).Scan(&historyCount, &musicMigrationName, &themeMigrationName, &extraMigrationName, &bindingMigrationName); err != nil || historyCount != 28 ||
		musicMigrationName != "0025_music_artists.sql" || themeMigrationName != "0026_theme_owners.sql" || extraMigrationName != "0027_movie_extras.sql" ||
		bindingMigrationName != "0028_storage_root_bindings.sql" {
		t.Fatal("schema24 restoration did not append exactly the music, theme, extra and root binding migrations")
	}
}

//go:build linux

package backuppg

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

func assertHistoricalArchiveThemeDefaults(t *testing.T, ctx context.Context, pool *pgxpool.Pool, expectedPaths ...string) {
	t.Helper()
	paths := "[]"
	if len(expectedPaths) > 1 {
		t.Fatal("a historical theme fixture supplied multiple path inventories")
	}
	if len(expectedPaths) == 1 {
		paths = expectedPaths[0]
	}
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM theme_owner_ids WHERE virtual_root AND item_id IS NULL)=1
		AND (SELECT count(*) FROM theme_owner_ids)=(SELECT count(*)+1 FROM items)
		AND NOT EXISTS(SELECT 1 FROM items i LEFT JOIN theme_owner_ids owner ON owner.item_id=i.id WHERE owner.id IS NULL)
		AND NOT EXISTS(SELECT 1 FROM theme_owner_ids WHERE id<=0)
		AND COALESCE((SELECT jsonb_agg(jsonb_build_array(root_id,relative_path,is_directory)
		ORDER BY root_id,relative_path) FROM theme_reserved_paths),'[]'::jsonb)=$1::jsonb
		AND NOT EXISTS(SELECT 1 FROM item_theme_resources)`, paths).Scan(&valid); err != nil || !valid {
		t.Fatalf("historical restoration omitted theme owners or inferred unattested theme resources: complete=%v error=%v", valid, err)
	}
}

func seedMusicSnapshotWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('music-snapshot-library','Music snapshot','music');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES
		('music-snapshot-album','music-snapshot-library','Accepted album','accepted album','MusicAlbum',true);
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type) VALUES
		('music-snapshot-track','music-snapshot-library','music-snapshot-album','Accepted title','accepted title','Audio');
		UPDATE item_metadata_state SET music_source=jsonb_build_object('Version',1,'Name',
		CASE item_id WHEN 'music-snapshot-track' THEN 'Accepted title' ELSE 'Accepted album' END,
		'Album','Accepted album','Artists',jsonb_build_array('Snapshot artist','Featured artist'),
		'AlbumArtists',jsonb_build_array('Snapshot artist')),
		overrides='{"Overview":"Retained music override"}'::jsonb,locked_values='{"Tags":["Retained tag"]}'::jsonb,
		revision=9007199254740993,last_edited_by='backup-admin',last_edited_at='2020-01-04T00:00:00Z',
		updated_at='2020-01-04T00:00:00Z' WHERE item_id IN ('music-snapshot-track','music-snapshot-album');
		UPDATE item_metadata_state SET automatic=(music_source - 'Version') || '{"Genres":["Music genre"],"Tags":["Retained tag"],"Studios":[],"People":[{"Name":"Snapshot artist","Type":"Composer"}]}'::jsonb,
		source_key=source_key || jsonb_build_object('MusicSourceHash',encode(sha256(convert_to(music_source::text,'UTF8')),'hex'))
		WHERE item_id IN ('music-snapshot-track','music-snapshot-album');
		UPDATE item_metadata_state SET effective=automatic || overrides || locked_values
		WHERE item_id IN ('music-snapshot-track','music-snapshot-album');
		SELECT sync_catalog_item_entities(item_id,effective) FROM item_metadata_state
		WHERE item_id IN ('music-snapshot-track','music-snapshot-album') ORDER BY item_id`); err != nil {
		t.Fatalf("seed accepted music source and durable catalog associations: %v", err)
	}
	assertMusicSnapshotIdentity(t, ctx, pool)
	return musicSnapshotState(t, ctx, pool)
}

func musicSnapshotState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i WHERE library_id='music-snapshot-library'),
		'metadata',(SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m WHERE item_id IN ('music-snapshot-track','music-snapshot-album')),
		'associations',(SELECT jsonb_agg(to_jsonb(a) ORDER BY item_id,entity_id,credit_group,position) FROM item_entities a WHERE item_id IN ('music-snapshot-track','music-snapshot-album')),
		'entities',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM catalog_entities e WHERE id IN
			(SELECT entity_id FROM item_entities WHERE item_id IN ('music-snapshot-track','music-snapshot-album'))))::text`).Scan(&state); err != nil {
		t.Fatalf("read complete music snapshot rows: %v", err)
	}
	return state
}

func assertMusicSnapshotIdentity(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM item_entities a JOIN catalog_entities e ON e.id=a.entity_id
		 WHERE a.item_id='music-snapshot-track' AND e.kind='MusicArtist' AND e.normalized_name='snapshot artist'
		 AND a.position=1 AND ((a.credit_group=1 AND a.credit_type='Artist') OR (a.credit_group=2 AND a.credit_type='AlbumArtist')))=2
		AND (SELECT count(DISTINCT a.entity_id) FROM item_entities a JOIN catalog_entities e ON e.id=a.entity_id
		 WHERE a.item_id IN ('music-snapshot-track','music-snapshot-album') AND e.kind='MusicArtist' AND e.normalized_name='snapshot artist')=1
		AND (SELECT count(*) FROM catalog_entities WHERE normalized_name='snapshot artist' AND kind IN ('Person','MusicArtist'))=2
		AND NOT EXISTS(SELECT 1 FROM item_entities a LEFT JOIN items i ON i.id=a.item_id
		 LEFT JOIN catalog_entities e ON e.id=a.entity_id WHERE i.id IS NULL OR e.id IS NULL)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("music snapshot lost shared artist identity, distinct Person identity, dual-role position, or foreign keys: %v", err)
	}
}

func TestPostgreSQLOfflineSchema25ArchivePreservesMusicAndBackfillsThemeOwners(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 25)
	var themeAbsent bool
	if err := source.QueryRow(ctx, `SELECT to_regclass('theme_owner_ids') IS NULL
		AND to_regclass('theme_reserved_paths') IS NULL AND to_regclass('item_theme_resources') IS NULL`).Scan(&themeAbsent); err != nil || !themeAbsent {
		t.Fatal("the actual schema25 archive source already contains theme storage")
	}
	musicBefore := seedMusicSnapshotWitness(t, ctx, source)
	if _, err := source.Exec(ctx, `INSERT INTO user_settings(user_id,settings,updated_at)
		VALUES('backup-admin','{"Theme":"retained-schema25","ExactInteger":9007199254740993,"Extension":[null,true,"same"]}'::jsonb,
		'2020-01-07T00:00:00Z')`); err != nil {
		t.Fatalf("seed separate schema25 preference state: %v", err)
	}
	// These are real schema25 ordinary items, with no theme tables or inferred
	// owner roles. Migration may only add a permanent reservation around them.
	if _, err := source.Exec(ctx, `INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('schema25-theme-root','music-snapshot-library','/synthetic/legacy-themes','/synthetic','legacy-themes');
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,relative_path) VALUES
		('schema25-theme-directory','music-snapshot-library','schema25-theme-root','music-snapshot-album',
		'Historical theme directory','historical theme directory','MusicAlbum',true,'Legacy/Theme-Music'),
		('schema25-theme-song','music-snapshot-library','schema25-theme-root','schema25-theme-directory',
		'Historical ordinary song','historical ordinary song','Audio',false,'Legacy/Theme-Music/song.flac')`); err != nil {
		t.Fatalf("seed known historical theme paths without changing the schema25 contract: %v", err)
	}
	musicBefore = musicSnapshotState(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	if facts.SchemaVersion != 25 || len(facts.MigrationChecksums) != 25 || len(facts.Tables) != 30 {
		t.Fatal("the historical music archive is not the exact schema25 migration prefix")
	}
	before := make(map[string]string, len(facts.Tables))
	for _, table := range facts.Tables {
		before[table.Name] = historicalArchiveRows(t, ctx, source, table.Name, 25)
	}
	factsBefore, err := json.Marshal(facts)
	if err != nil {
		t.Fatal("snapshot original schema25 archive facts")
	}
	sourceBefore, sequences := unchangedSourceWitness(t, ctx, source, options)
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil {
		logFixtureCatalogDifference(t, ctx, target, options)
		t.Fatalf("restore the actual schema25 music archive into schema28: %v", err)
	}
	if result.SourceVersion != 25 || result.CurrentVersion != 28 || !equalJSON(result.Tables, facts.Tables) {
		t.Fatal("theme migration rewrote the historical archive version or source table facts")
	}
	for table, expected := range before {
		if historicalArchiveRows(t, ctx, target, table, 25) != expected {
			t.Errorf("schema25 restoration changed an original row or field in %s", table)
		}
	}
	if musicSnapshotState(t, ctx, target) != musicBefore {
		t.Fatal("theme owner backfill changed persisted music source, identities, roles, or metadata layers")
	}
	assertMusicSnapshotIdentity(t, ctx, target)
	assertHistoricalArchiveThemeDefaults(t, ctx, target, `[["schema25-theme-root","Legacy/Theme-Music",true]]`)
	assertHistoricalArchiveExtraDefaults(t, ctx, target)
	assertHistoricalArchiveBindingDefaults(t, ctx, target)
	var exposed int
	if err := target.QueryRow(ctx, `SELECT count(*) FROM items i WHERE i.id IN ('schema25-theme-directory','schema25-theme-song')
		AND (`+database.ThemeOrdinaryItemSQL("i")+` OR `+database.ThemeDirectItemSQL("i")+`)`).Scan(&exposed); err != nil || exposed != 0 {
		t.Fatal("historical reserved items became visible or were fabricated into playable theme resources")
	}
	for name, expected := range sequences {
		var restored sequenceState
		if err := target.QueryRow(ctx, "SELECT last_value,is_called FROM "+qualified(options.Schema, name)).Scan(&restored.value, &restored.called); err != nil || restored != expected {
			t.Fatalf("theme backfill consumed or rewrote an original sequence %s: %v", name, err)
		}
	}
	ownersBefore := historicalArchiveRows(t, ctx, target, "theme_owner_ids", 26)
	if err := database.Migrate(ctx, target); err != nil {
		t.Fatalf("repeat migration after historical music restoration: %v", err)
	}
	if historicalArchiveRows(t, ctx, target, "theme_owner_ids", 26) != ownersBefore {
		t.Fatal("repeating current migrations rotated restored theme owner identities")
	}
	var targetVersion, targetTables int
	if err := target.QueryRow(ctx, `SELECT (SELECT max(version) FROM schema_migrations),
		(SELECT count(*) FROM pg_tables WHERE schemaname=current_schema())`).Scan(&targetVersion, &targetTables); err != nil || targetVersion != 28 || targetTables != 35 {
		t.Fatalf("historical music restoration did not reach the complete schema28: version=%d tables=%d error=%v", targetVersion, targetTables, err)
	}
	assertSourceWitness(t, ctx, source, options, sourceBefore, sequences)
	if err := source.QueryRow(ctx, `SELECT to_regclass('theme_owner_ids') IS NULL
		AND to_regclass('theme_reserved_paths') IS NULL AND to_regclass('item_theme_resources') IS NULL`).Scan(&themeAbsent); err != nil || !themeAbsent {
		t.Fatal("offline restoration introduced theme storage into its historical source")
	}
	factsAfter, err := json.Marshal(facts)
	if err != nil || string(factsAfter) != string(factsBefore) {
		t.Fatal("restoring schema25 mutated the original archive facts")
	}
}

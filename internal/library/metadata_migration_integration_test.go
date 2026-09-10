package library

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/media"
)

func metadataMigrationLegacyPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("create metadata migration administrator pool")
	}
	libraryIntegrationPoolCleanup(t, admin)
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate metadata migration schema: %v", err)
	}
	schema := "goby_metadata_legacy_" + hex.EncodeToString(suffix[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatalf("create owned metadata migration schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("remove owned metadata migration schema: %v", err)
		}
	})
	config := admin.Config()
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create isolated metadata migration pool")
	}
	libraryIntegrationPoolCleanup(t, pool)
	directory := filepath.Join("..", "database", "migrations")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read legacy migration fixtures: %v", err)
	}
	names := make(map[int]string)
	for _, entry := range entries {
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		version, err := strconv.Atoi(prefix)
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") || !ok || err != nil || version < 1 || version > 13 {
			continue
		}
		if names[version] != "" {
			t.Fatalf("duplicate legacy migration version %d", version)
		}
		names[version] = entry.Name()
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin schema thirteen fixture: %v", err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `CREATE TABLE schema_migrations (
		version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatalf("create legacy migration history: %v", err)
	}
	for version := 1; version <= 13; version++ {
		name := names[version]
		if name == "" {
			t.Fatalf("legacy migration %d is missing", version)
		}
		contents, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, string(contents)); err != nil {
			t.Fatalf("apply legacy migration %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", version, name); err != nil {
			t.Fatalf("record legacy migration %s: %v", name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit schema thirteen fixture: %v", err)
	}
	return ctx, pool
}

func metadataMigrationTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname = current_schema() ORDER BY tablename")
	if err != nil {
		t.Fatalf("enumerate owned legacy tables: %v", err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("read owned legacy table name: %v", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read legacy table inventory: %v", err)
	}
	return tables
}

func metadataMigrationSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tables []string) map[string]string {
	t.Helper()
	result := make(map[string]string, len(tables))
	for _, table := range tables {
		statement := `SELECT COALESCE(jsonb_agg(to_jsonb(original) ORDER BY to_jsonb(original)::text), '[]'::jsonb)::text FROM ` + pgx.Identifier{table}.Sanitize() + " original"
		if table == "scan_jobs" {
			// Nullable task ownership is checked independently after migration.
			statement = `SELECT COALESCE(jsonb_agg(to_jsonb(original) - 'force_probe' - 'task_child_id' ORDER BY id), '[]'::jsonb)::text FROM scan_jobs original`
		}
		if table == "sessions" {
			// Device registration adds a foreign key without changing login data.
			statement = `SELECT COALESCE(jsonb_agg(to_jsonb(original) - 'device_registry_id'
				ORDER BY (to_jsonb(original) - 'device_registry_id')::text), '[]'::jsonb)::text FROM sessions original`
		}
		if table == "play_sessions" || table == "encoding_jobs" || table == "client_playback_references" {
			// New application-client columns do not change any historical field.
			statement = `SELECT COALESCE(jsonb_agg(to_jsonb(original) - 'application_client_id'
				ORDER BY (to_jsonb(original) - 'application_client_id')::text), '[]'::jsonb)::text FROM ` + pgx.Identifier{table}.Sanitize() + " original"
		}
		if table == "schema_migrations" {
			statement += " WHERE version <= 13"
		}
		var snapshot string
		if err := pool.QueryRow(ctx, statement).Scan(&snapshot); err != nil {
			t.Fatalf("snapshot legacy table %s: %v", table, err)
		}
		result[table] = snapshot
	}
	return result
}

func metadataMigrationItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string, legacy bool) Item {
	t.Helper()
	columns := itemColumns
	if legacy {
		columns = strings.Replace(columns,
			"COALESCE((SELECT ms.effective FROM item_metadata_state ms WHERE ms.item_id = i.id), i.local_metadata)",
			"i.local_metadata", 1)
		if strings.Contains(columns, "item_metadata_state") {
			t.Fatal("legacy projection still depends on the new metadata table")
		}
	}
	item, err := scanItem(pool.QueryRow(ctx, "SELECT "+columns+" FROM items i WHERE id = $1", itemID))
	if err != nil {
		t.Fatalf("read unchanged legacy item projection: %v", err)
	}
	return item
}

func TestMetadataMigrationFromThirteenPreservesEveryExistingTableAndProjection(t *testing.T) {
	ctx, pool := metadataMigrationLegacyPool(t)
	actor := metadataEditTestActor(t, ctx, pool, "legacy-metadata-administrator")
	if _, err := pool.Exec(ctx, "UPDATE users SET management_revision = 9007199254740993 WHERE id = $1", actor.User.ID); err != nil {
		t.Fatalf("seed exact large management revision before migration: %v", err)
	}
	allowedRoot := t.TempDir()
	mediaPath := libraryIntegrationFile(t, allowedRoot, "Legacy.mp4", "video:legacy-metadata")
	nfo := `<movie><title>Legacy Movie</title><sorttitle>legacy sort</sorttitle><plot>Legacy overview.</plot><year>2018</year><genre>Legacy Genre</genre></movie>`
	libraryIntegrationFile(t, allowedRoot, "Legacy.nfo", nfo)
	digest := sha256.Sum256([]byte(nfo))
	const libraryID, rootID = "metadata-legacy-library", "metadata-legacy-root"
	if _, err := pool.Exec(ctx, "INSERT INTO libraries (id, name, collection_type) VALUES ($1, 'Legacy metadata library', 'mixed')", libraryID); err != nil {
		t.Fatalf("insert legacy library: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
		VALUES ($1, $2, $3, $3, '.')`, rootID, libraryID, allowedRoot); err != nil {
		t.Fatalf("insert legacy root: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO items (id, library_id, name, sort_name, type, is_folder)
		VALUES ($1, $1, 'Legacy metadata library', 'legacy metadata library', 'CollectionFolder', true)`, libraryID); err != nil {
		t.Fatalf("insert legacy collection folder: %v", err)
	}
	stamp := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	info := libraryMediaFixture([]byte("video:legacy-metadata"))
	info.DurationTicks = 600 * media.TicksPerSecond
	info.Streams = append(info.Streams, media.Stream{Index: 42, Codec: "subrip", CodecType: "subtitle", Language: "eng", Title: "Legacy external subtitle", IsExternal: true, IsTextSubtitleStream: true})
	encodedMedia, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("encode legacy technical media: %v", err)
	}
	type fixture struct {
		id, name, sortName, kind, parent string
		folder                           bool
		index, parentIndex               int
		source                           []byte
	}
	fixtures := []fixture{
		{id: "metadata-legacy-series", name: "Legacy Series", sortName: "legacy series", kind: "Series", parent: libraryID, folder: true},
		{id: "metadata-legacy-season", name: "Legacy Season", sortName: "legacy season", kind: "Season", parent: "metadata-legacy-series", folder: true, index: 1,
			source: []byte(`{"Kind":"season","Name":"Legacy Season","IndexNumber":null,"Genres":null,"People":null}`)},
		{id: "metadata-legacy-movie", name: "Legacy Movie", sortName: "legacy sort", kind: "Movie", parent: libraryID,
			source: []byte(`{"Kind":"movie","Name":"Legacy Movie","SortName":"legacy sort","Overview":"Legacy overview.","ProductionYear":2018,"ProviderIDs":{"Imdb":"tt1234"},"Genres":["Legacy Genre"],"Tags":null,"Studios":["Legacy Studio"],"People":[{"Name":"Legacy Actor","Role":"Lead","Type":"Actor","SortOrder":0,"OpaqueCredit":{"Id":9007199254740993}}],"InternalExtension":{"LargeInteger":9007199254740993,"NullValue":null}}`)},
		{id: "metadata-legacy-episode", name: "Legacy Episode", sortName: "legacy episode", kind: "Episode", parent: "metadata-legacy-season", index: 2, parentIndex: 1,
			source: []byte(`{"Kind":"episodedetails","Name":"Legacy Episode","IndexNumber":2,"ParentIndexNumber":1,"Genres":null,"Tags":[],"Studios":null,"People":null,"ProviderIDs":null,"OpaqueEpisode":[1,null,"keep"]}`)},
		{id: "metadata-legacy-sql-null", name: "Legacy SQL Null", sortName: "legacy sql null", kind: "Video", parent: libraryID},
		{id: "metadata-legacy-json-null", name: "Legacy JSON Null", sortName: "legacy json null", kind: "Video", parent: libraryID, source: []byte(`null`)},
		{id: "metadata-legacy-array", name: "Legacy Array", sortName: "legacy array", kind: "Video", parent: libraryID, source: []byte(`[{"Opaque":9007199254740993},null]`)},
		{id: "metadata-legacy-scalar", name: "Legacy Scalar", sortName: "legacy scalar", kind: "Video", parent: libraryID, source: []byte(`9007199254740993`)},
	}
	for _, item := range fixtures {
		path := filepath.Join(allowedRoot, item.id)
		if item.id == "metadata-legacy-movie" {
			path = mediaPath
		}
		var mediaValue []byte
		if !item.folder {
			mediaValue = encodedMedia
		}
		if _, err := pool.Exec(ctx, `INSERT INTO items
			(id, library_id, root_id, parent_id, name, sort_name, type, path, relative_path, overview,
			 is_folder, index_number, parent_index_number, media, file_identity, file_size, modified_at,
			 local_metadata, local_metadata_hash, local_metadata_path)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $1, 'Legacy overview.', $9, $10, $11, $12,
			 'legacy-file-identity', 21, $13, $14, $15, 'Legacy.nfo')`,
			item.id, libraryID, rootID, item.parent, item.name, item.sortName, item.kind, path,
			item.folder, item.index, item.parentIndex, mediaValue, stamp, item.source, hex.EncodeToString(digest[:])); err != nil {
			t.Fatalf("insert legacy metadata item %s: %v", item.id, err)
		}
		if _, err := pool.Exec(ctx, "SELECT sync_catalog_item_entities($1, $2::jsonb)", item.id, item.source); err != nil {
			t.Fatalf("seed legacy metadata entities: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO item_images
		(item_id, root_id, image_type, image_index, relative_path, file_identity, source_hash, file_size, modified_at, width, height, mime_type)
		VALUES ('metadata-legacy-movie', $1, 'Primary', 0, 'poster.png', 'legacy-image', repeat('b',64), 128, $2, 320, 180, 'image/png')`, rootID, stamp); err != nil {
		t.Fatalf("seed existing image association: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO item_subtitles
		(item_id, root_id, stream_index, relative_path, file_identity, source_hash, file_size, modified_at,
		 change_time_ns, codec, language, title, is_default, mime_type)
		VALUES ('metadata-legacy-movie', $1, 42, 'Legacy.en.srt', 'legacy-subtitle', repeat('c',64), 32,
		 $2, 1, 'srt', 'eng', 'Legacy external subtitle', true, 'application/x-subrip')`, rootID, stamp); err != nil {
		t.Fatalf("seed existing subtitle association: %v", err)
	}
	userDataSeed(t, ctx, pool, actor.User.ID, UserData{ItemID: "metadata-legacy-movie", PlaybackPositionTicks: 150 * media.TicksPerSecond, PlayCount: 7, IsFavorite: true, LastPlayedDate: &stamp})
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 13 {
		t.Fatalf("metadata migration did not start from schema thirteen: version = %d, error = %v", version, err)
	}
	tables := metadataMigrationTables(t, ctx, pool)
	before := metadataMigrationSnapshot(t, ctx, pool, tables)
	projectionIDs := []string{"metadata-legacy-movie", "metadata-legacy-episode", "metadata-legacy-season", "metadata-legacy-series", "metadata-legacy-sql-null", "metadata-legacy-json-null"}
	projections := make(map[string]Item, len(projectionIDs))
	for _, id := range projectionIDs {
		projections[id] = metadataMigrationItem(t, ctx, pool, id, true)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade administrator metadata state: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 22 {
		t.Fatalf("metadata migration version = %d, want 22, error = %v", version, err)
	}
	assertOldTables := func() {
		t.Helper()
		after := metadataMigrationSnapshot(t, ctx, pool, tables)
		for _, table := range tables {
			if after[table] != before[table] {
				t.Errorf("metadata migration rewrote existing table %s", table)
			}
		}
		var taskLinkedScans int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM scan_jobs WHERE task_child_id IS NOT NULL").Scan(&taskLinkedScans); err != nil || taskLinkedScans != 0 {
			t.Errorf("metadata migration attached historical scans to task children: count=%d error=%v", taskLinkedScans, err)
		}
		var activityCount int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM activity_entries").Scan(&activityCount); err != nil || activityCount != 0 {
			t.Errorf("metadata migration backfilled historical state into activity entries: count=%d error=%v", activityCount, err)
		}
	}
	assertOldTables()
	for _, id := range projectionIDs {
		if item := metadataMigrationItem(t, ctx, pool, id, false); !reflect.DeepEqual(item, projections[id]) {
			t.Errorf("metadata migration changed the legacy catalog projection for %s", id)
		}
	}
	var states, missing, changedProjection, incomplete int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM item_metadata_state),
		(SELECT count(*) FROM items i LEFT JOIN item_metadata_state s ON s.item_id = i.id WHERE s.item_id IS NULL),
		(SELECT count(*) FROM items i JOIN item_metadata_state s ON s.item_id = i.id WHERE s.effective IS DISTINCT FROM i.local_metadata),
		(SELECT count(*) FROM items i JOIN item_metadata_state s ON s.item_id = i.id WHERE
			jsonb_typeof(s.automatic) IS DISTINCT FROM 'object' OR s.automatic->>'Name' IS DISTINCT FROM i.name
			OR s.automatic->>'SortName' IS DISTINCT FROM i.sort_name OR s.automatic->>'Overview' IS DISTINCT FROM i.overview
			OR jsonb_typeof(s.automatic->'ProviderIDs') IS DISTINCT FROM 'object'
			OR jsonb_typeof(s.automatic->'Genres') IS DISTINCT FROM 'array' OR jsonb_typeof(s.automatic->'Tags') IS DISTINCT FROM 'array'
			OR jsonb_typeof(s.automatic->'Studios') IS DISTINCT FROM 'array' OR jsonb_typeof(s.automatic->'People') IS DISTINCT FROM 'array')`).Scan(&states, &missing, &changedProjection, &incomplete); err != nil {
		t.Fatalf("inspect metadata state backfill: %v", err)
	}
	if states != len(fixtures)+1 || missing != 0 || changedProjection != 0 || incomplete != 0 {
		t.Errorf("metadata backfill lost source semantics: states = %d, missing = %d, changed effective = %d, incomplete automatic = %d", states, missing, changedProjection, incomplete)
	}
	var managementRevision, opaqueNumber string
	if err := pool.QueryRow(ctx, "SELECT management_revision::text FROM users WHERE id = $1", actor.User.ID).Scan(&managementRevision); err != nil || managementRevision != "9007199254740993" {
		t.Errorf("migration changed the exact management revision: revision = %q, error = %v", managementRevision, err)
	}
	if err := pool.QueryRow(ctx, "SELECT automatic #>> '{InternalExtension,LargeInteger}' FROM item_metadata_state WHERE item_id = 'metadata-legacy-movie'").Scan(&opaqueNumber); err != nil || opaqueNumber != "9007199254740993" {
		t.Errorf("automatic backfill lost opaque integer precision: value = %q, error = %v", opaqueNumber, err)
	}
	newTables := metadataMigrationTables(t, ctx, pool)
	var additions []string
	oldNames := make(map[string]bool, len(tables))
	for _, table := range tables {
		oldNames[table] = true
	}
	for _, table := range newTables {
		if !oldNames[table] {
			additions = append(additions, table)
		}
	}
	sort.Strings(additions)
	taskTables := []string{"task_definitions", "task_occurrences", "task_run_children", "task_run_requests", "task_runs", "task_triggers"}
	expectedAdditions := append([]string{"activity_entries", "application_key_clients", "application_key_devices", "application_keys", "devices", "item_metadata_state", "managed_settings"}, taskTables...)
	if !reflect.DeepEqual(additions, expectedAdditions) {
		t.Errorf("metadata migration created unexpected tables: %+v", additions)
	}
	// Migration creates task storage; this fixture never initializes a registry.
	for _, table := range taskTables {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil || count != 0 {
			t.Errorf("metadata migration populated task table %s without registry initialization: count=%d error=%v", table, count, err)
		}
	}
	var keys, clients int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM application_keys),
		(SELECT count(*) FROM application_key_clients)`).Scan(&keys, &clients); err != nil || keys != 0 || clients != 0 {
		t.Errorf("metadata upgrade created application credentials or clients: keys=%d clients=%d error=%v", keys, clients, err)
	}
	prober := &libraryFixtureProber{}
	store, err := New(pool, prober, []string{allowedRoot})
	if err != nil {
		t.Fatalf("open migrated metadata catalog: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanupCtx); err != nil {
			t.Errorf("close migrated metadata catalog: %v", err)
		}
	})
	for _, item := range fixtures {
		detail := metadataEditTestDetail(t, ctx, store, actor, item.id)
		if detail.Automatic.Name != item.name || detail.Automatic.SortName != item.sortName || detail.Effective.Name != item.name || detail.Revision != "1" || len(detail.Overrides) != 0 || len(detail.LockedValues) != 0 {
			t.Errorf("automatic administrator view requires a rescan or changed legacy values: %+v", detail)
		}
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat metadata migration: %v", err)
	}
	assertOldTables()
	if calls := len(prober.calls()); calls != 0 {
		t.Errorf("metadata migration or startup probed media %d times", calls)
	}
}

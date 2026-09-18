package database_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

var featureWaveTables = []string{
	"media_collections", "media_collection_entries", "media_collection_shares",
	"item_provider_sources", "item_provider_images", "item_subtitle_provider_sources", "media_deletion_operations",
}

func featureWaveSeed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,name,normalized_name,password_hash) VALUES
		('wave-owner','Wave Owner','wave owner','synthetic-only'),('wave-peer','Wave Peer','wave peer','synthetic-only');
		INSERT INTO libraries(id,name,collection_type) VALUES('wave-library','Wave Library','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('wave-root','wave-library','/not-opened/wave','/not-opened','wave');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
		VALUES('wave-library','wave-library','Wave Library','wave library','CollectionFolder',true);
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path)
		VALUES('wave-movie','wave-library','wave-root','wave-library','Film','film','Movie','/not-opened/wave/Film.mkv','Film.mkv');
		UPDATE item_metadata_state SET revision=7,overrides='{"Name":"Retained name"}',locked_values='{"Genres":["Retained genre"]}' WHERE item_id='wave-movie';
		INSERT INTO item_subtitles(item_id,root_id,stream_index,relative_path,file_identity,source_hash,file_size,modified_at,change_time_ns,codec,mime_type)
		VALUES('wave-movie','wave-root',1,'Film.en.srt','synthetic-subtitle',repeat('a',64),100,'2026-01-01T00:00:00Z',1,'srt','application/x-subrip');
		INSERT INTO sessions(id,user_id,token_hash,kind,created_at,expires_at)
		VALUES('wave-session','wave-owner',decode(repeat('a1',32),'hex'),'emby','2026-01-01T00:00:00Z','2030-01-01T00:00:00Z');
		INSERT INTO play_sessions(id,user_id,auth_session_id,device_id,item_id,media_source_id,state,position_ticks,duration_ticks,expires_at)
		VALUES('wave-play','wave-owner','wave-session','wave-device','wave-movie','wave-source','Stopped',10,100,'2030-01-01T00:00:00Z');
		UPDATE managed_settings SET revision=17,server_name='Retained server',server_name_mode='custom',max_width=1920 WHERE id=1;
		INSERT INTO task_definitions(id,key,name) VALUES(repeat('a',32),'wave.generic','Wave Task');
		INSERT INTO task_runs(id,task_id,state,source,task_key,task_name)
		VALUES(repeat('b',32),repeat('a',32),'pending','manual','wave.generic','Wave Task');
		INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal)
		VALUES(repeat('c',32),repeat('b',32),'wave-library','Wave Library',0);
		INSERT INTO scan_jobs(id,library_id,status,started_at,finished_at)
		VALUES('wave-completed-scan','wave-library','Completed','2026-01-01T00:00:00Z','2026-01-01T00:01:00Z')`); err != nil {
		t.Fatalf("seed feature-wave migration fixture: %v", err)
	}
}

func featureWaveCurrent(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx, pool := migrationTestPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("create current feature-wave schema: %v", err)
	}
	featureWaveSeed(t, ctx, pool)
	return ctx, pool
}

func featureWaveHistoricalRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table deviceLegacyTable) string {
	t.Helper()
	columns := make([]string, len(table.columns))
	for index, column := range table.columns {
		columns[index] = pgx.Identifier{column}.Sanitize()
	}
	statement := "SELECT " + strings.Join(columns, ",") + " FROM " + pgx.Identifier{table.name}.Sanitize()
	if table.name == "schema_migrations" {
		statement += " WHERE version<=29"
	}
	var result string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(original) ORDER BY to_jsonb(original)::text),'[]'::jsonb)::text FROM (`+statement+`) original`).Scan(&result); err != nil {
		t.Fatalf("snapshot historical table %s: %v", table.name, err)
	}
	return result
}

func featureWaveSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != code {
		actual := "non-PostgreSQL error"
		if databaseError != nil {
			actual = databaseError.Code
		}
		t.Fatalf("constraint returned %s (%T), want SQLSTATE %s", actual, err, code)
	}
}

func featureWaveStatement(t *testing.T, ctx context.Context, pool *pgxpool.Pool, statement, code string) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer themeOwnersRollback(tx)
	_, err = tx.Exec(ctx, statement)
	if code != "" {
		featureWaveSQLState(t, err, code)
		return
	}
	if err != nil {
		t.Fatalf("valid feature-wave state was rejected: %v", err)
	}
}

func TestFeatureWaveMigrationPreservesSchema29RowsAndInitializesDefaults(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersMigrateTo(t, ctx, pool, 29)
	featureWaveSeed(t, ctx, pool)
	legacy := captureDeviceLegacyTables(t, ctx, pool)
	if len(legacy) != 35 {
		t.Fatalf("historical schema29 inventory = %d, want 35", len(legacy))
	}
	for index := range legacy {
		legacy[index].snapshot = featureWaveHistoricalRows(t, ctx, pool, legacy[index])
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade actual schema29: %v", err)
	}
	for _, table := range legacy {
		if featureWaveHistoricalRows(t, ctx, pool, table) != table.snapshot {
			t.Errorf("feature-wave migration changed original rows in %s", table.name)
		}
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != currentMigrationVersion(t) {
		t.Fatalf("feature-wave target version=%d error=%v", version, err)
	}
	const defaults = `{"Metadata":{"EnableInternetProviders":false,"PreferredMetadataLanguage":"en","MetadataCountryCode":"US"},"Subtitles":{"DownloadLanguages":["en"],"DownloadMovieSubtitles":true,"DownloadEpisodeSubtitles":true},"Tasks":{"MaxConcurrent":2,"CacheRetentionDays":30,"CacheMaxEntries":10000}}`
	var coherent bool
	if err := pool.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM item_metadata_state WHERE online_source<>'{}'::jsonb OR online_type<>'' OR online_base IS NOT NULL)
		AND NOT EXISTS(SELECT 1 FROM task_run_children WHERE executor_token IS NOT NULL)
		AND NOT EXISTS(SELECT 1 FROM play_sessions WHERE is_dynamic)
		AND EXISTS(SELECT 1 FROM managed_settings WHERE id=1 AND revision=17 AND management=$1::jsonb)`, defaults).Scan(&coherent); err != nil || !coherent {
		t.Fatalf("new columns inferred state or changed settings revision: %v", err)
	}
	for _, table := range featureWaveTables {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil || count != 0 {
			t.Errorf("new table %s was not empty: count=%d error=%v", table, count, err)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT attidentity='a' FROM pg_attribute WHERE attrelid='media_collection_entries'::regclass AND attname='id')
		AND pg_get_serial_sequence('media_collection_entries','id')::regclass<>pg_get_serial_sequence('catalog_entities','id')::regclass
		AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='users'::regclass AND tgname='users_delete_owned_media_collections' AND NOT tgisinternal AND tgenabled='O')
		AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='scan_jobs'::regclass AND tgname='scan_jobs_pending_media_deletion' AND NOT tgisinternal AND tgenabled='O')`).Scan(&coherent); err != nil || !coherent {
		t.Fatalf("feature-wave identity or trigger wiring missing: %v", err)
	}
	history := migrationHistory(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil || migrationHistory(t, ctx, pool) != history {
		t.Fatalf("repeated feature-wave migration changed history: %v", err)
	}
}

func TestFeatureWaveCollectionIdentityOrderingAndOwnerDeletion(t *testing.T) {
	ctx, pool := featureWaveCurrent(t)
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('wave-playlist','wave-library','wave-library','Playlist','playlist','Playlist',true),
		('wave-box','wave-library','wave-library','Box','box','BoxSet',true),
		('wave-peer-box','wave-library','wave-box','Peer Box','peer box','BoxSet',true);
		INSERT INTO media_collections(item_id,owner_id,kind) VALUES
		('wave-playlist','wave-owner','Playlist'),('wave-box','wave-owner','BoxSet'),('wave-peer-box','wave-peer','BoxSet');
		INSERT INTO media_collection_shares(collection_id,user_id,can_edit) VALUES('wave-playlist','wave-peer',true);
		INSERT INTO media_collection_entries(collection_id,item_id,position) VALUES
		('wave-playlist','wave-movie',0),('wave-playlist','wave-movie',1)`); err != nil {
		t.Fatal(err)
	}
	var originalIDs []int64
	if err := pool.QueryRow(ctx, `SELECT array_agg(id ORDER BY position) FROM media_collection_entries WHERE collection_id='wave-playlist'`).Scan(&originalIDs); err != nil || len(originalIDs) != 2 || originalIDs[0] == originalIDs[1] {
		t.Fatalf("duplicate members lost independent identities: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE media_collection_entries SET position=1-position WHERE collection_id='wave-playlist'`); err != nil {
		t.Fatalf("deferrable positions could not swap atomically: %v", err)
	}
	var reorderedIDs []int64
	if err := pool.QueryRow(ctx, `SELECT array_agg(id ORDER BY position) FROM media_collection_entries WHERE collection_id='wave-playlist'`).Scan(&reorderedIDs); err != nil || len(reorderedIDs) != 2 || reorderedIDs[0] != originalIDs[1] || reorderedIDs[1] != originalIDs[0] {
		t.Fatalf("reordering changed entry identities: %v", err)
	}
	featureWaveStatement(t, ctx, pool, `INSERT INTO media_collection_entries(id,collection_id,item_id,position) VALUES(9999,'wave-playlist','wave-movie',2)`, "428C9")
	featureWaveStatement(t, ctx, pool, `INSERT INTO media_collection_entries(collection_id,item_id,position) VALUES('wave-playlist','wave-playlist',2)`, "23514")
	featureWaveStatement(t, ctx, pool, `INSERT INTO media_collection_entries(collection_id,item_id,position) VALUES('wave-playlist','wave-movie',-1)`, "23514")
	featureWaveStatement(t, ctx, pool, `INSERT INTO media_collection_shares(collection_id,user_id) VALUES('wave-playlist','missing-user')`, "23503")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO media_collection_entries(collection_id,item_id,position) VALUES('wave-playlist','wave-movie',0)`); err != nil {
		themeOwnersRollback(tx)
		t.Fatalf("position constraint was not initially deferred: %v", err)
	}
	featureWaveSQLState(t, tx.Commit(ctx), "23505")
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id='wave-owner'`); err != nil {
		t.Fatalf("delete collection owner: %v", err)
	}
	var retained bool
	if err := pool.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM media_collections WHERE owner_id='wave-owner')
		AND NOT EXISTS(SELECT 1 FROM items WHERE id IN ('wave-playlist','wave-box'))
		AND NOT EXISTS(SELECT 1 FROM media_collection_entries)
		AND NOT EXISTS(SELECT 1 FROM media_collection_shares)
		AND EXISTS(SELECT 1 FROM items WHERE id='wave-movie')
		AND EXISTS(SELECT 1 FROM items WHERE id='wave-peer-box' AND parent_id IS NULL)
		AND EXISTS(SELECT 1 FROM media_collections WHERE item_id='wave-peer-box' AND owner_id='wave-peer')`).Scan(&retained); err != nil || !retained {
		t.Fatalf("owner cleanup removed source/foreign collection or retained owned state: %v", err)
	}
}

func TestFeatureWaveProviderAndStyledSubtitleConstraints(t *testing.T) {
	ctx, pool := featureWaveCurrent(t)
	for _, test := range []struct{ name, statement, code string }{
		{"online source requires object", `UPDATE item_metadata_state SET online_source='[]' WHERE item_id='wave-movie'`, "23514"},
		{"online base requires object", `UPDATE item_metadata_state SET online_base='true' WHERE item_id='wave-movie'`, "23514"},
		{"online type is bounded", `UPDATE item_metadata_state SET online_type=repeat('x',65) WHERE item_id='wave-movie'`, "23514"},
		{"provider source is closed", `INSERT INTO item_provider_sources(item_id,provider,provider_id,source_url,fields) VALUES('wave-movie','unknown','1','','{}')`, "23514"},
		{"provider fields require object", `INSERT INTO item_provider_sources(item_id,provider,provider_id,source_url,fields) VALUES('wave-movie','tmdb','1','','[]')`, "23514"},
		{"styled MIME must agree", `UPDATE item_subtitles SET codec='ass',mime_type='text/vtt' WHERE item_id='wave-movie' AND stream_index=1`, "23514"},
		{"ASS accepted", `UPDATE item_subtitles SET codec='ass',mime_type='text/x-ssa' WHERE item_id='wave-movie' AND stream_index=1`, ""},
		{"SSA accepted", `UPDATE item_subtitles SET codec='ssa',mime_type='text/x-ssa' WHERE item_id='wave-movie' AND stream_index=1`, ""},
		{"unknown subtitle codec", `UPDATE item_subtitles SET codec='unknown',mime_type='text/x-ssa' WHERE item_id='wave-movie' AND stream_index=1`, "23514"},
		{"provider track requires subtitle", `INSERT INTO item_subtitle_provider_sources(item_id,stream_index,provider,provider_id) VALUES('wave-movie',999,'opensubtitles','1')`, "23503"},
	} {
		t.Run(test.name, func(t *testing.T) { featureWaveStatement(t, ctx, pool, test.statement, test.code) })
	}
	imageInsert := `INSERT INTO item_provider_images(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash)
		VALUES('wave-movie','Primary',0,'tmdb','1','image',decode('010203','hex'),'image/png',100,100,repeat('a',64))`
	if _, err := pool.Exec(ctx, imageInsert); err != nil {
		t.Fatalf("insert bounded provider artwork: %v", err)
	}
	featureWaveStatement(t, ctx, pool, `UPDATE item_provider_images SET width=6000,height=5000 WHERE item_id='wave-movie'`, "23514")
	featureWaveStatement(t, ctx, pool, `UPDATE item_provider_images SET image_index=1 WHERE item_id='wave-movie'`, "23514")
	featureWaveStatement(t, ctx, pool, `UPDATE item_provider_images SET content=''::bytea WHERE item_id='wave-movie'`, "23514")
	if _, err := pool.Exec(ctx, `INSERT INTO item_provider_sources(item_id,provider,provider_id,source_url,fields) VALUES('wave-movie','tmdb','1','https://example.invalid/fixture','{}');
		INSERT INTO item_subtitle_provider_sources(item_id,stream_index,provider,provider_id) VALUES('wave-movie',1,'opensubtitles','subtitle-1');
		DELETE FROM item_subtitles WHERE item_id='wave-movie' AND stream_index=1`); err != nil {
		t.Fatal(err)
	}
	var noOrphans bool
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM item_subtitle_provider_sources)`).Scan(&noOrphans); err != nil || !noOrphans {
		t.Fatalf("subtitle deletion retained provider provenance: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM items WHERE id='wave-movie'`); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM item_provider_sources) AND NOT EXISTS(SELECT 1 FROM item_provider_images)`).Scan(&noOrphans); err != nil || !noOrphans {
		t.Fatalf("item deletion retained provider state: %v", err)
	}
}

func TestFeatureWaveGenericClaimsAndDynamicPlaybackRemainConstrained(t *testing.T) {
	ctx, pool := featureWaveCurrent(t)
	for _, test := range []struct{ name, statement, code string }{
		{"generic running claim", `UPDATE task_run_children SET state='running',started_at=clock_timestamp(),executor_token=repeat('d',32) WHERE id=repeat('c',32)`, ""},
		{"unclaimed running work", `UPDATE task_run_children SET state='running',started_at=clock_timestamp() WHERE id=repeat('c',32)`, "23514"},
		{"malformed claim token", `UPDATE task_run_children SET state='running',started_at=clock_timestamp(),executor_token='not-a-token' WHERE id=repeat('c',32)`, "23514"},
		{"waiting claim forbidden", `UPDATE task_run_children SET executor_token=repeat('d',32) WHERE id=repeat('c',32)`, "23514"},
		{"scan and generic claim exclusive", `UPDATE task_run_children SET state='running',started_at=clock_timestamp(),scan_job_id='wave-completed-scan',executor_token=repeat('d',32) WHERE id=repeat('c',32)`, "23514"},
		{"legacy scan reference remains valid", `UPDATE task_run_children SET state='running',started_at=clock_timestamp(),scan_job_id='wave-completed-scan' WHERE id=repeat('c',32)`, ""},
		{"dynamic elapsed clock", `UPDATE play_sessions SET is_dynamic=true,position_ticks=1000000 WHERE id='wave-play'`, ""},
		{"ordinary playback duration bound", `UPDATE play_sessions SET position_ticks=1000000 WHERE id='wave-play'`, "23514"},
		{"dynamic negative elapsed clock", `UPDATE play_sessions SET is_dynamic=true,position_ticks=-1 WHERE id='wave-play'`, "23514"},
		{"dynamic negative duration", `UPDATE play_sessions SET is_dynamic=true,duration_ticks=-1 WHERE id='wave-play'`, "23514"},
		{"management requires object", `UPDATE managed_settings SET management='[]' WHERE id=1`, "23514"},
		{"management audit field", `INSERT INTO activity_entries(action,severity,source,actor_kind,actor_id,resource_kind,resource_id,changed_fields) VALUES('settings.updated','Info','native','user','wave-owner','settings','management',ARRAY['Management'])`, ""},
		{"unregistered audit field", `INSERT INTO activity_entries(action,severity,source,actor_kind,actor_id,resource_kind,resource_id,changed_fields) VALUES('settings.updated','Info','native','user','wave-owner','settings','management',ARRAY['UnknownSetting'])`, "23514"},
	} {
		t.Run(test.name, func(t *testing.T) { featureWaveStatement(t, ctx, pool, test.statement, test.code) })
	}
}

func TestFeatureWaveDeletionJournalGuardsScanAdmissionAndSurvivesCatalogRemoval(t *testing.T) {
	ctx, pool := featureWaveCurrent(t)
	if _, err := pool.Exec(ctx, `INSERT INTO media_deletion_operations(id,item_id,library_id,root_id,actor_id,credential_id,origin_host,kind,state,source_snapshot)
		VALUES('wave-deletion','wave-movie','wave-library','wave-root','wave-owner','wave-session',repeat('a',64),'media','prepared','{}')`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, statement, code string }{
		{"queued scan blocked", `INSERT INTO scan_jobs(id,library_id,status) VALUES('wave-new-scan','wave-library','Queued')`, "55000"},
		{"running transition blocked", `UPDATE scan_jobs SET status='Running',finished_at=NULL WHERE id='wave-completed-scan'`, "55000"},
		{"terminal history allowed", `INSERT INTO scan_jobs(id,library_id,status,finished_at) VALUES('wave-terminal-scan','wave-library','Failed',clock_timestamp())`, ""},
		{"journal library retained", `DELETE FROM libraries WHERE id='wave-library'`, "23503"},
		{"journal root retained", `DELETE FROM library_roots WHERE id='wave-root'`, "23503"},
		{"media entry excludes subtitle index", `UPDATE media_deletion_operations SET subtitle_index=0 WHERE id='wave-deletion'`, "23514"},
		{"subtitle entry requires index", `UPDATE media_deletion_operations SET kind='subtitle' WHERE id='wave-deletion'`, "23514"},
		{"journal snapshot requires object", `UPDATE media_deletion_operations SET source_snapshot='[]' WHERE id='wave-deletion'`, "23514"},
		{"journal origin must be a fingerprint", `UPDATE media_deletion_operations SET origin_host='other-host' WHERE id='wave-deletion'`, "23514"},
		{"journal state is closed", `UPDATE media_deletion_operations SET state='complete' WHERE id='wave-deletion'`, "23514"},
		{"media deletion audit", `INSERT INTO activity_entries(action,severity,source,actor_kind,actor_id,resource_kind,resource_id) VALUES('item.deleted','Info','emby','user','wave-owner','item','wave-movie')`, ""},
		{"subtitle deletion audit", `INSERT INTO activity_entries(action,severity,source,actor_kind,actor_id,resource_kind,resource_id) VALUES('subtitle.deleted','Info','emby','user','wave-owner','item','wave-movie')`, ""},
		{"deletion audit requires item", `INSERT INTO activity_entries(action,severity,source,actor_kind,actor_id,resource_kind,resource_id) VALUES('item.deleted','Info','emby','user','wave-owner','library','wave-library')`, "23514"},
	} {
		t.Run(test.name, func(t *testing.T) { featureWaveStatement(t, ctx, pool, test.statement, test.code) })
	}
	if _, err := pool.Exec(ctx, `DELETE FROM items WHERE id='wave-movie'; UPDATE media_deletion_operations SET state='catalog_removed' WHERE id='wave-deletion'`); err != nil {
		t.Fatal(err)
	}
	var retained bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media_deletion_operations WHERE id='wave-deletion' AND item_id='wave-movie' AND state='catalog_removed')`).Scan(&retained); err != nil || !retained {
		t.Fatalf("catalog removal discarded deletion recovery: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM media_deletion_operations WHERE id='wave-deletion'; INSERT INTO scan_jobs(id,library_id,status) VALUES('wave-unblocked-scan','wave-library','Queued')`); err != nil {
		t.Fatalf("completed recovery did not release scan admission: %v", err)
	}
}

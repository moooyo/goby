//go:build linux

package backuppg

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgreSQLFeatureWaveArchivePreservesNonemptyStateAndRecoveryConstraints(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	// Paths, identities, and provider bytes are synthetic SQL witnesses. This
	// test never opens media roots or contacts an online provider.
	if _, err := source.Exec(ctx, `INSERT INTO users(id,name,normalized_name,password_hash)
		VALUES('feature-wave-peer','Feature Wave Peer','feature wave peer','synthetic-peer-digest');
		INSERT INTO libraries(id,name,collection_type)
		VALUES('feature-wave-library','Feature Wave Library','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('feature-wave-root','feature-wave-library','/synthetic/feature-wave','/synthetic','feature-wave');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
		VALUES('feature-wave-library','feature-wave-library','Feature Wave Library','feature wave library','CollectionFolder',true);
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path,media)
		VALUES('feature-wave-movie','feature-wave-library','feature-wave-root','feature-wave-library',
		'Feature Wave Movie','feature wave movie','Movie','/synthetic/feature-wave/Film.mkv','Film.mkv',
		'{"ProbeVersion":6,"RunTimeTicks":100}'::jsonb);
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
		VALUES('feature-wave-playlist','feature-wave-library','feature-wave-library','Retained Playlist','retained playlist','Playlist',true);
		INSERT INTO media_collections(item_id,owner_id,kind,media_type,is_public,is_locked,created_at,updated_at)
		VALUES('feature-wave-playlist','backup-admin','Playlist','Video',false,true,'2020-01-03T00:00:00Z','2020-01-04T00:00:00Z');
		INSERT INTO media_collection_shares(collection_id,user_id,can_edit)
		VALUES('feature-wave-playlist','feature-wave-peer',true);
		SELECT setval('media_collection_entries_id_seq',9007199254740993,false);
		INSERT INTO media_collection_entries(collection_id,item_id,position)
		VALUES('feature-wave-playlist','feature-wave-movie',0),('feature-wave-playlist','feature-wave-movie',1);
		UPDATE item_metadata_state SET online_base=automatic,online_type='Movie',
		online_source='{"Overview":"Retained online overview","ProviderIDs":{"Tmdb":"12345"}}'::jsonb,
		revision=9007199254740993,updated_at='2020-01-05T00:00:00Z' WHERE item_id='feature-wave-movie';
		UPDATE item_metadata_state SET automatic=online_base||online_source,effective=online_base||online_source
		WHERE item_id='feature-wave-movie';
		UPDATE items SET overview='Retained online overview' WHERE id='feature-wave-movie';
		INSERT INTO item_provider_sources(item_id,provider,provider_id,source_url,language,fields,fetched_at)
		SELECT item_id,'tmdb','12345','https://example.invalid/feature-wave','fr',online_source,'2020-01-05T00:00:00Z'
		FROM item_metadata_state WHERE item_id='feature-wave-movie';
		INSERT INTO item_provider_images(item_id,image_type,image_index,provider,provider_id,image_id,
		content,mime_type,width,height,source_hash,fetched_at)
		VALUES('feature-wave-movie','Primary',0,'tmdb','12345','retained-image',decode('89504e470d0a1a0a00ff5c','hex'),
		'image/png',1,1,encode(sha256(decode('89504e470d0a1a0a00ff5c','hex')),'hex'),'2020-01-06T00:00:00Z');
		INSERT INTO item_subtitles(item_id,root_id,stream_index,relative_path,file_identity,source_hash,
		file_size,modified_at,change_time_ns,codec,language,title,is_default,mime_type)
		VALUES('feature-wave-movie','feature-wave-root',7,'Film.fr.ass','synthetic-ass-identity',repeat('e',64),
		256,'2020-01-07T00:00:00Z',9007199254740993,'ass','fra','Retained styled subtitle',true,'text/x-ssa');
		INSERT INTO item_subtitle_provider_sources(item_id,stream_index,provider,provider_id,downloaded_at)
		VALUES('feature-wave-movie',7,'opensubtitles','retained-subtitle','2020-01-07T00:00:00Z');
		UPDATE managed_settings SET management='{"Metadata":{"EnableInternetProviders":true,"PreferredMetadataLanguage":"fr","MetadataCountryCode":"FR"},"Subtitles":{"DownloadLanguages":["fr","en"],"DownloadMovieSubtitles":true,"DownloadEpisodeSubtitles":false},"Tasks":{"MaxConcurrent":3,"CacheRetentionDays":14,"CacheMaxEntries":9000}}'::jsonb
		WHERE id=1;
		INSERT INTO task_definitions(id,key,name)
		VALUES(repeat('a',32),'feature-wave.generic','Feature Wave Generic');
		INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,total_children,started_at)
		VALUES(repeat('b',32),repeat('a',32),'running','manual','feature-wave.generic','Feature Wave Generic',1,'2020-01-08T00:00:00Z');
		INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,state,started_at,executor_token)
		VALUES(repeat('c',32),repeat('b',32),'feature-wave-library','Feature Wave Library',0,'running','2020-01-08T00:00:00Z',repeat('d',32));
		INSERT INTO sessions(id,user_id,token_hash,kind,created_at,expires_at)
		VALUES('feature-wave-session','backup-admin',decode(repeat('a5',32),'hex'),'emby','2020-01-01T00:00:00Z','2030-01-01T00:00:00Z');
		INSERT INTO play_sessions(id,user_id,auth_session_id,device_id,item_id,media_source_id,state,
		position_ticks,duration_ticks,expires_at,is_dynamic)
		VALUES('feature-wave-play','backup-admin','feature-wave-session','feature-wave-device','feature-wave-movie',
		'feature-wave-source','Stopped',9007199254740993,100,'2030-01-01T00:00:00Z',true);
		INSERT INTO media_deletion_operations(id,item_id,library_id,root_id,actor_id,credential_id,origin_host,kind,state,source_snapshot)
		VALUES('feature-wave-deletion','feature-wave-movie','feature-wave-library','feature-wave-root','backup-admin',
		'feature-wave-session',repeat('c',64),'media','prepared',
		'{"RelativePath":"Film.mkv","Identity":"synthetic-media","ExactInteger":9007199254740993,"Parts":[null,true,"retained"]}'::jsonb);
		INSERT INTO activity_entries(action,severity,source,actor_kind,actor_id,resource_kind,resource_id,changed_fields)
		VALUES('settings.updated','Info','native','user','backup-admin','settings','management',ARRAY['Management']),
		('item.deleted','Info','emby','user','backup-admin','item','feature-wave-retired-movie',ARRAY[]::text[]),
		('subtitle.deleted','Info','emby','user','backup-admin','item','feature-wave-movie',ARRAY[]::text[])`); err != nil {
		t.Fatalf("seed nonempty feature-wave archive state: %v", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	assertCurrentRecoveryFacts(t, facts)
	counts := make(map[string]int64, len(facts.Tables))
	for _, table := range facts.Tables {
		counts[table.Name] = table.Rows
	}
	for table, expected := range map[string]int64{
		"media_collections": 1, "media_collection_entries": 2, "media_collection_shares": 1,
		"item_provider_sources": 1, "item_provider_images": 1, "item_subtitle_provider_sources": 1,
		"media_deletion_operations": 1, "item_subtitles": 1, "managed_settings": 1,
		"task_run_children": 1, "play_sessions": 1, "activity_entries": 3,
	} {
		if counts[table] != expected {
			t.Fatalf("feature-wave archive table %s contains %d rows, want %d", table, counts[table], expected)
		}
	}
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if !equalJSON(before, facts) {
		t.Fatal("feature-wave source changed after its archive snapshot")
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != facts.SchemaVersion || result.CurrentVersion != currentRecoveryVersion(t) ||
		!equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore the complete feature-wave archive without its source: %v", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	assertCurrentRecoveryFacts(t, after)
	if !equalJSON(after, facts) || len(targetSequences) != len(sequences) {
		t.Fatal("feature-wave restoration changed complete table fingerprints or sequence inventory")
	}
	for name, expected := range sequences {
		if actual, exists := targetSequences[name]; !exists || actual != expected {
			t.Fatalf("feature-wave restoration changed sequence %s", name)
		}
	}
	var entryIDs []int64
	if err := target.QueryRow(ctx, `SELECT array_agg(id ORDER BY position) FROM media_collection_entries
		WHERE collection_id='feature-wave-playlist' AND item_id='feature-wave-movie'`).Scan(&entryIDs); err != nil ||
		!equalJSON(entryIDs, []int64{9007199254740993, 9007199254740994}) {
		t.Fatalf("restoration lost duplicate playlist members or exact independent entry identities: %v", err)
	}
	for _, check := range []struct{ name, statement, code string }{
		{"collection share foreign key", `INSERT INTO media_collection_shares(collection_id,user_id) VALUES('feature-wave-playlist','missing-feature-wave-user')`, "23503"},
		{"subtitle provenance foreign key", `INSERT INTO item_subtitle_provider_sources(item_id,stream_index,provider,provider_id) VALUES('feature-wave-movie',8,'opensubtitles','missing-track')`, "23503"},
		{"pending deletion scan guard", `INSERT INTO scan_jobs(id,library_id,status) VALUES('feature-wave-blocked-scan','feature-wave-library','Queued')`, "55000"},
	} {
		_, err := target.Exec(ctx, check.statement)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != check.code {
			t.Fatalf("restored %s did not enforce SQLSTATE %s: %v", check.name, check.code, err)
		}
	}
	cleanup, err := target.Begin(ctx)
	if err != nil {
		t.Fatal("begin rollback-only restored collection trigger witness")
	}
	defer rollback(cleanup)
	if _, err := cleanup.Exec(ctx, "DELETE FROM users WHERE id='backup-admin'"); err != nil {
		t.Fatalf("execute restored collection owner cleanup trigger: %v", err)
	}
	var retained bool
	if err := cleanup.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM items WHERE id='feature-wave-playlist')
		AND NOT EXISTS(SELECT 1 FROM media_collections)
		AND NOT EXISTS(SELECT 1 FROM media_collection_entries)
		AND NOT EXISTS(SELECT 1 FROM media_collection_shares)
		AND EXISTS(SELECT 1 FROM items WHERE id='feature-wave-movie')
		AND EXISTS(SELECT 1 FROM item_provider_sources WHERE item_id='feature-wave-movie')
		AND EXISTS(SELECT 1 FROM item_provider_images WHERE item_id='feature-wave-movie')
		AND EXISTS(SELECT 1 FROM media_deletion_operations WHERE id='feature-wave-deletion')`).Scan(&retained); err != nil || !retained {
		t.Fatalf("restored owner cleanup lost source media or retained owned collection state: %v", err)
	}
	if err := cleanup.Rollback(ctx); err != nil {
		t.Fatal("discard the restored collection trigger witness")
	}
	assertSourceWitness(t, ctx, target, targetOptions, after, targetSequences)
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

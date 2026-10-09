//go:build linux

package backuppg

import (
	"fmt"
	"testing"
)

func TestPostgreSQLItemReferenceIndexesRestorePreservesHistoryAndDeletion(t *testing.T) {
	for _, version := range []int64{63, 64} {
		t.Run(fmt.Sprintf("schema%d", version), func(t *testing.T) {
			ctx, source, target, options := recoveryFixtureAtVersion(t, version)
			if _, err := source.Exec(ctx, `INSERT INTO libraries(id,name,collection_type)
				VALUES('item-index-library','Item reference archive','movies');
				INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
				VALUES('item-index-root','item-index-library','/retired/item-index','/retired','item-index');
				INSERT INTO items(id,library_id,root_id,relative_path,name,sort_name,type)
				SELECT 'item-index-'||name,'item-index-library','item-index-root',name||'.mkv',name,name,'Movie'
				FROM (VALUES('delete'),('keep')) fixture(name);
				INSERT INTO users(id,name,normalized_name,password_hash)
				VALUES('item-index-viewer','Index viewer','index viewer','test-only-password');
				INSERT INTO sessions(id,user_id,token_hash,kind,device_id,expires_at,revoked_at)
				SELECT 'item-index-auth-'||owner,owner,sha256(convert_to(owner,'UTF8')),'emby','retained-device',
				'2099-01-01T00:00:00Z',CASE WHEN owner='backup-admin' THEN '2026-01-01T00:00:00Z'::timestamptz END
				FROM (VALUES('backup-admin'),('item-index-viewer')) fixture(owner);
				INSERT INTO user_item_data(user_id,item_id,play_count,played,is_favorite)
				SELECT owner,'item-index-'||item,7,owner='backup-admin',owner='item-index-viewer'
				FROM (VALUES('backup-admin'),('item-index-viewer')) owners(owner)
				CROSS JOIN (VALUES('delete'),('keep')) items(item);
				INSERT INTO play_sessions(id,user_id,auth_session_id,device_id,item_id,media_source_id,state,duration_ticks,expires_at)
				SELECT 'item-index-play-'||item||'-'||owner,owner,'item-index-auth-'||owner,'retained-device',
				'item-index-'||item,'retained-source',CASE WHEN owner='backup-admin' THEN 'Expired' ELSE 'Playing' END,
				1000,'2099-01-01T00:00:00Z'
				FROM (VALUES('backup-admin'),('item-index-viewer')) owners(owner)
				CROSS JOIN (VALUES('delete'),('keep')) items(item);
				INSERT INTO media_operations(id,kind,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,
				request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,stream_index,
				parameters,source_snapshot,execution_snapshot,state)
				SELECT md5(name),'subtitle_ocr',item,'item-index-library','item-index-root',historical,
				'item-index-library','item-index-root','backup-admin','retired-credential',name,sha256(convert_to(name,'UTF8')),
				'retained-source','retained-revision',0,'{}','{}','{}',state
				FROM (VALUES('index-completed','item-index-delete','earlier-source-item','completed'),
				('index-failed','item-index-delete','item-index-delete','failed'),
				('index-kept','item-index-keep','item-index-keep','cancelled'),
				('index-detached',NULL,'removed-source-item','completed')) fixture(name,item,historical,state)`); err != nil {
				t.Fatalf("seed retained user, playback, and media-operation history: %v", err)
			}
			before, sequences := unchangedSourceWitness(t, ctx, source, options)
			archive, facts := sourceArchive(t, ctx, source, options)
			result, err := RestoreOffline(ctx, target, archive, facts, options)
			if err != nil || result.SourceVersion != version || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
				t.Fatalf("restore original item-reference fingerprints before trusted index migration: %v", err)
			}
			targetOptions := options
			targetOptions.SourceURL = target.Config().ConnString()
			after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
			if version < currentRecoveryVersion(t) {
				assertHistoricalRecoveryFacts(t, ctx, source, target, facts, after, sequences, targetSequences)
			} else if !equalJSON(before.Tables, after.Tables) || !equalJSON(sequences, targetSequences) {
				t.Fatal("current index catalog restoration changed retained rows or sequence state")
			}
			var valid bool
			if err := target.QueryRow(ctx, `SELECT to_regclass('user_item_data_item_idx') IS NOT NULL
				AND to_regclass('play_sessions_item_idx') IS NOT NULL AND to_regclass('media_operations_item_idx') IS NOT NULL`).Scan(&valid); err != nil || !valid {
				t.Fatalf("restored current catalog omitted item-reference indexes: %v", err)
			}
			if _, err := target.Exec(ctx, `DELETE FROM items WHERE id='item-index-delete'`); err != nil {
				t.Fatal("delete restored item through its retained foreign keys", err)
			}
			if err := target.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM user_item_data)=2 AND (SELECT count(*) FROM play_sessions)=2
				AND NOT EXISTS(SELECT 1 FROM user_item_data WHERE item_id='item-index-delete')
				AND NOT EXISTS(SELECT 1 FROM play_sessions WHERE item_id='item-index-delete')
				AND (SELECT count(*) FROM media_operations)=4
				AND (SELECT count(*) FROM media_operations WHERE item_id IS NULL)=3
				AND EXISTS(SELECT 1 FROM media_operations WHERE id=md5('index-completed')
					AND item_id IS NULL AND source_item_id='earlier-source-item' AND state='completed')
				AND EXISTS(SELECT 1 FROM media_operations WHERE id=md5('index-kept') AND item_id='item-index-keep')`).Scan(&valid); err != nil || !valid {
				t.Fatalf("restored index schema changed CASCADE, SET NULL, or historical source evidence: %v", err)
			}
			assertSourceWitness(t, ctx, source, options, before, sequences)
		})
	}
}

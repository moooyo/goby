package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

func seedItemReferenceIndexHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('fk-library','Item references','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('fk-root','fk-library','/item-references/media','/item-references','media');
		INSERT INTO items(id,library_id,root_id,relative_path,name,sort_name,type)
		SELECT 'fk-'||name,'fk-library','fk-root',name||'.mkv',name,name,'Movie'
		FROM (VALUES('delete'),('keep'),('protected')) fixture(name);
		INSERT INTO users(id,name,normalized_name,password_hash)
		SELECT 'fk-user-'||n,'Reference user '||n,'reference user '||n,'test-only-password' FROM generate_series(1,2) n;
		INSERT INTO sessions(id,user_id,token_hash,kind,device_id,expires_at)
		SELECT 'fk-auth-'||n,'fk-user-'||n,sha256(convert_to('fk-auth-'||n,'UTF8')),'emby','fk-device-'||n,'2099-01-01T00:00:00Z'
		FROM generate_series(1,2) n;
		INSERT INTO sessions(id,token_hash,kind) VALUES('fk-key',sha256('fk-key'::bytea),'application_key');
		INSERT INTO application_key_clients(id,credential_id,client_name,device_id,device_name,client_version)
		VALUES('fk-client','fk-key','Client','fk-device','Device','1');
		INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,played,is_favorite)
		SELECT 'fk-user-'||n,item,CASE WHEN n=1 THEN 123 ELSE 0 END,n,n=2,n=1
		FROM generate_series(1,2) n CROSS JOIN (VALUES('fk-delete'),('fk-keep')) fixture(item);
		INSERT INTO play_sessions(id,user_id,auth_session_id,device_id,item_id,media_source_id,state,duration_ticks,expires_at)
		SELECT 'fk-play-'||state,'fk-user-1','fk-auth-1','fk-device-1','fk-delete','source-'||state,state,1000,'2099-01-01T00:00:00Z'
		FROM (VALUES('Prepared'),('Playing'),('Paused'),('Stopped'),('Expired')) fixture(state);
		INSERT INTO play_sessions(id,user_id,auth_session_id,application_client_id,device_id,item_id,media_source_id,state,duration_ticks,expires_at)
		VALUES('fk-userless',NULL,'fk-key','fk-client','fk-device','fk-delete','userless-source','Stopped',1000,'2099-01-01T00:00:00Z'),
		('fk-play-keep','fk-user-2','fk-auth-2',NULL,'fk-device-2','fk-keep','kept-source','Stopped',1000,'2099-01-01T00:00:00Z');
		INSERT INTO media_operations(id,kind,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,
		request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,stream_index,
		parameters,source_snapshot,execution_snapshot,state,publication_phase)
		SELECT md5(name),CASE WHEN phase='prepared' THEN 'remove_embedded_subtitle' ELSE 'subtitle_ocr' END,
		item,'fk-library','fk-root',source,'fk-library','fk-root','fk-user-1','fk-auth-1',name,sha256(convert_to(name,'UTF8')),
		'retained-source','retained-revision',0,'{}','{}','{}',state,phase
		FROM (VALUES('fk-completed','fk-delete','historical-different-item','completed','none'),
		('fk-failed','fk-delete','fk-delete','failed','none'),('fk-retained','fk-keep','fk-keep','cancelled','none'),
		('fk-detached',NULL,'already-deleted-item','completed','none'),
		('fk-publishing','fk-protected','fk-protected','applying','prepared')) fixture(name,item,source,state,phase)`); err != nil {
		t.Fatalf("seed mixed item references and retained terminal history: %v", err)
	}
}

func assertItemReferenceIndexes(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, index := range []struct {
		name, table, predicate string
	}{
		{"user_item_data_item_idx", "user_item_data", ""},
		{"play_sessions_item_idx", "play_sessions", ""},
		{"media_operations_item_idx", "media_operations", "item_id IS NOT NULL"},
	} {
		var valid bool
		err := pool.QueryRow(ctx, `SELECT c.relname=$2 AND a.amname='btree' AND i.indisvalid AND i.indisready AND i.indislive
			AND NOT i.indisunique AND i.indnkeyatts=1 AND i.indnatts=1
			AND pg_get_indexdef(i.indexrelid,1,true)='item_id'
			AND COALESCE(pg_get_expr(i.indpred,i.indrelid,true),'')=$3
			FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid JOIN pg_class x ON x.oid=i.indexrelid
			JOIN pg_am a ON a.oid=x.relam WHERE i.indexrelid=to_regclass($1)`, index.name, index.table, index.predicate).Scan(&valid)
		if err != nil || !valid {
			t.Fatalf("item-reference index %s has the wrong lookup semantics: valid=%v error=%v", index.name, valid, err)
		}
	}
}

func itemReferenceHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'items',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM items r),
		'user_data',(SELECT jsonb_agg(to_jsonb(r) ORDER BY user_id,item_id) FROM user_item_data r),
		'plays',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM play_sessions r),
		'operations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM media_operations r),
		'migrations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY version) FROM schema_migrations r WHERE version<=63))::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertItemReferenceDeleteSemantics(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if tag, err := pool.Exec(ctx, `DELETE FROM items WHERE id='fk-delete'`); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("delete referenced parent item: %v", err)
	}
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM user_item_data WHERE item_id='fk-delete')
		AND NOT EXISTS(SELECT 1 FROM play_sessions WHERE item_id='fk-delete')
		AND (SELECT count(*) FROM user_item_data WHERE item_id='fk-keep')=2
		AND (SELECT count(*) FROM play_sessions WHERE item_id='fk-keep')=1
		AND (SELECT count(*) FROM media_operations)=5
		AND (SELECT count(*) FROM media_operations WHERE item_id IS NULL)=3
		AND EXISTS(SELECT 1 FROM media_operations WHERE id=md5('fk-completed') AND item_id IS NULL
			AND source_item_id='historical-different-item' AND state='completed')
		AND EXISTS(SELECT 1 FROM media_operations WHERE id=md5('fk-retained') AND item_id='fk-keep')`).Scan(&valid); err != nil || !valid {
		t.Fatalf("item deletion changed CASCADE, SET NULL, or retained history: valid=%v error=%v", valid, err)
	}
	before := itemReferenceHistory(t, ctx, pool)
	_, err := pool.Exec(ctx, `DELETE FROM items WHERE id='fk-protected'`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" || itemReferenceHistory(t, ctx, pool) != before {
		t.Fatalf("item indexes must preserve the active publication deletion barrier: %v", err)
	}
}

func TestItemReferenceIndexesFreshSchemaPreservesDeletionSemantics(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	assertItemReferenceIndexes(t, ctx, pool)
	seedItemReferenceIndexHistory(t, ctx, pool)
	assertItemReferenceDeleteSemantics(t, ctx, pool)
}

func TestItemReferenceIndexesUpgradePreservesSchema63(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			themeOwnersMigrateTo(t, ctx, pool, 63)
			seedItemReferenceIndexHistory(t, ctx, pool)
			before := itemReferenceHistory(t, ctx, pool)
			for range 2 {
				if runner == "normal" {
					if err := database.Migrate(ctx, pool); err != nil {
						t.Fatal(err)
					}
				} else {
					themeOwnersMigrateTo(t, ctx, pool, 64)
				}
				assertItemReferenceIndexes(t, ctx, pool)
				if itemReferenceHistory(t, ctx, pool) != before {
					t.Fatal("index-only migration rewrote original item references or published history")
				}
			}
			assertItemReferenceDeleteSemantics(t, ctx, pool)
		})
	}
}

func TestItemReferenceIndexesRecoveryMigrationRollsBackWithCaller(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersMigrateTo(t, ctx, pool, 63)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := database.RecoveryMigrateTo(ctx, tx, 64); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var absent bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('user_item_data_item_idx') IS NULL
		AND to_regclass('play_sessions_item_idx') IS NULL AND to_regclass('media_operations_item_idx') IS NULL
		AND NOT EXISTS(SELECT 1 FROM schema_migrations WHERE version=64)`).Scan(&absent); err != nil || !absent {
		t.Fatalf("rolled-back migration retained indexes or its history row: %v", err)
	}
}

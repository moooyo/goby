package database_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This bounded observation is opt-in so ordinary migration tests do not seed
// large histories. It compares the same retained rows before and after the
// trusted migration, without disabling sequential scans or changing FK rules.
func TestItemReferenceIndexesPostgreSQL17Observation(t *testing.T) {
	if os.Getenv("GOBY_TEST_ITEM_REFERENCE_INDEX_OBSERVATION") != "1" {
		t.Skip("GOBY_TEST_ITEM_REFERENCE_INDEX_OBSERVATION=1 selects the bounded PostgreSQL observation")
	}
	ctx, pool := migrationTestPool(t)
	var major int
	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::integer/10000`).Scan(&major); err != nil || major != 17 {
		t.Fatalf("item-reference plan observations require PostgreSQL 17: major=%d error=%v", major, err)
	}
	themeOwnersMigrateTo(t, ctx, pool, 63)
	const items = 6000
	if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO libraries(id,name,collection_type)
		VALUES('fk-observation-library','Bounded item reference observation','movies');
		INSERT INTO items(id,library_id,name,sort_name,type)
		SELECT 'fk-observation-item-'||n,'fk-observation-library','Film '||n,'film '||n,'Movie' FROM generate_series(1,%[1]d) n;
		INSERT INTO users(id,name,normalized_name,password_hash)
		SELECT 'fk-observation-user-'||n,'History user '||n,'history user '||n,'test-only-password' FROM generate_series(0,128) n;
		INSERT INTO sessions(id,user_id,token_hash,kind,device_id,created_at,expires_at,revoked_at)
		SELECT 'fk-observation-auth-'||n,'fk-observation-user-'||n,sha256(convert_to('auth-'||n,'UTF8')),
		'emby','history-device-'||n,'2025-01-01T00:00:00Z','2099-01-01T00:00:00Z',
		CASE WHEN n%%2=0 THEN '2026-01-01T00:00:00Z'::timestamptz END FROM generate_series(0,128) n;
		INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,played,last_played_at)
		SELECT 'fk-observation-user-'||((n+h)%%128),'fk-observation-item-'||n,
		CASE WHEN h=1 THEN 900 ELSE 0 END,h,h=2,h=3,'2025-01-01T00:00:00Z'::timestamptz+n*interval '1 second'
		FROM generate_series(1,%[1]d) n CROSS JOIN generate_series(1,4) h;
		INSERT INTO play_sessions(id,user_id,auth_session_id,device_id,item_id,media_source_id,state,duration_ticks,expires_at)
		SELECT 'fk-observation-play-'||n||'-'||h,'fk-observation-user-'||((n+h)%%128),
		'fk-observation-auth-'||((n+h)%%128),'history-device-'||((n+h)%%128),'fk-observation-item-'||n,'source-'||n,
		(ARRAY['Prepared','Playing','Paused','Stopped','Expired'])[h],1000,'2099-01-01T00:00:00Z'
		FROM generate_series(1,%[1]d) n CROSS JOIN generate_series(1,5) h;
		INSERT INTO media_operations(id,kind,item_id,source_item_id,source_library_id,source_root_id,
		request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,stream_index,
		parameters,source_snapshot,execution_snapshot,state)
		SELECT md5('operation-'||n||'-'||h),'subtitle_ocr',CASE WHEN h<5 THEN 'fk-observation-item-'||n END,
		'historical-item-'||n,'retained-library','retained-root','retained-user','retained-auth',n||'-'||h,
		sha256(convert_to(n||'-'||h,'UTF8')),'retained-source','retained-revision',0,'{}','{}','{}',
		(ARRAY['completed','failed','cancelled','stale','completed'])[h]
		FROM generate_series(1,%[1]d) n CROSS JOIN generate_series(1,5) h`, items)); err != nil {
		t.Fatalf("seed bounded mixed user histories and retained terminal operations: %v", err)
	}
	t.Logf("item_reference_fixture items=%d users=129 user_item_data=%d play_sessions=%d media_operations=%d detached_operations=%d delete_batch=128",
		items, 4*items, 5*items, 5*items, items)
	for _, phase := range []string{"schema63", "schema64"} {
		if phase == "schema64" {
			themeOwnersMigrateTo(t, ctx, pool, 64)
			assertItemReferenceIndexes(t, ctx, pool)
		}
		// Each observation rolls back mutations. Remove their dead tuples before
		// the next phase so aborted SET NULL updates do not become the comparison.
		for _, table := range []string{"items", "user_item_data", "play_sessions", "media_operations"} {
			if _, err := pool.Exec(ctx, "VACUUM (ANALYZE) "+table); err != nil {
				t.Fatal("prepare stable fixture statistics", err)
			}
		}
		var sizes string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_object('schema_bytes',
			(SELECT sum(pg_total_relation_size(c.oid)) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
			 WHERE n.nspname=current_schema() AND c.relkind='r'),'new_index_bytes',
			(SELECT COALESCE(jsonb_object_agg(c.relname,pg_relation_size(c.oid)),'{}') FROM pg_class c
			 WHERE c.oid IN (to_regclass('user_item_data_item_idx'),to_regclass('play_sessions_item_idx'),
			 to_regclass('media_operations_item_idx'))))::text`).Scan(&sizes); err != nil {
			t.Fatal(err)
		}
		t.Logf("item_reference_sizes phase=%s %s", phase, sizes)
		for _, lookup := range []struct {
			table, index, statement string
		}{
			{"user_item_data", "user_item_data_item_idx", `DELETE FROM ONLY user_item_data WHERE $1=item_id`},
			{"play_sessions", "play_sessions_item_idx", `DELETE FROM ONLY play_sessions WHERE $1=item_id`},
			{"media_operations", "media_operations_item_idx", `UPDATE ONLY media_operations SET item_id=NULL WHERE $1=item_id`},
		} {
			plan := observeItemReferenceStatement(t, ctx, pool, phase, "fk_lookup_"+lookup.table, lookup.statement, "fk-observation-item-3000")
			if phase == "schema64" && !strings.Contains(string(plan), `"Index Name": "`+lookup.index+`"`) {
				t.Fatalf("PostgreSQL did not select the item-reference index for %s: %s", lookup.table, plan)
			}
		}
		for trial := range 3 {
			observeItemReferenceStatement(t, ctx, pool, phase, fmt.Sprintf("parent_delete_batch_%d", trial),
				`DELETE FROM items WHERE id IN (SELECT 'fk-observation-item-'||n FROM generate_series(2001,2128) n)`)
		}
		for _, write := range []struct {
			table, statement string
		}{
			{"user_item_data", `INSERT INTO user_item_data(user_id,item_id)
				SELECT 'fk-observation-user-128','fk-observation-item-'||n FROM generate_series(1,512) n`},
			{"play_sessions", `INSERT INTO play_sessions(id,user_id,auth_session_id,device_id,item_id,media_source_id,state,duration_ticks,expires_at)
				SELECT 'maintenance-play-'||n,'fk-observation-user-128','fk-observation-auth-128','history-device-128',
				'fk-observation-item-'||n,'maintenance-source','Stopped',1000,'2099-01-01T00:00:00Z' FROM generate_series(1,512) n`},
			{"media_operations", `INSERT INTO media_operations(id,kind,item_id,source_item_id,source_library_id,source_root_id,
				request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,stream_index,
				parameters,source_snapshot,execution_snapshot,state)
				SELECT md5('maintenance-operation-'||n),'subtitle_ocr','fk-observation-item-'||n,'historical-item-'||n,
				'retained-library','retained-root','maintenance-user','retained-auth',n::text,
				sha256(convert_to(n::text,'UTF8')),'retained-source','retained-revision',0,'{}','{}','{}','completed'
				FROM generate_series(1,512) n`},
		} {
			observeItemReferenceStatement(t, ctx, pool, phase, "insert_512_"+write.table, write.statement)
		}
	}
}

func observeItemReferenceStatement(t *testing.T, ctx context.Context, pool *pgxpool.Pool, phase, name, statement string, args ...any) json.RawMessage {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT id FROM libraries WHERE id='fk-observation-library' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	// This client interval includes EXPLAIN/rollback protocol round trips after
	// acquiring the row lock. It does not measure the Go catalog-owner mutex,
	// filesystem scans, or publication; the plan records server execution time.
	started := time.Now()
	var plan json.RawMessage
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, WAL, FORMAT JSON) "+statement, args...).Scan(&plan); err != nil {
		t.Fatalf("observe %s %s: %v", phase, name, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("item_reference_observation phase=%s name=%s sql_locked_ms=%.3f plan=%s", phase, name, float64(time.Since(started).Microseconds())/1000, plan)
	return plan
}

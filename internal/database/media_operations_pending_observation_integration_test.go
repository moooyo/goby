package database_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// This opt-in profile holds retained history and candidate order constant while
// adding schema 67. It does not assert a universal latency or planner choice.
func TestMediaOperationsPendingIndexPostgreSQL17Observation(t *testing.T) {
	if os.Getenv("GOBY_TEST_MEDIA_PENDING_INDEX_OBSERVATION") != "1" {
		t.Skip("GOBY_TEST_MEDIA_PENDING_INDEX_OBSERVATION=1 selects the bounded PostgreSQL observation")
	}
	ctx, pool := migrationTestPool(t)
	var major int
	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::integer/10000`).Scan(&major); err != nil || major != 17 {
		t.Fatalf("pending-index observation requires PostgreSQL 17: major=%d error=%v", major, err)
	}
	themeOwnersMigrateTo(t, ctx, pool, 66)
	seedMediaOperationsPendingHistory(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO media_operations(id,kind,source_item_id,source_library_id,source_root_id,
		request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,
		stream_index,parameters,source_snapshot,execution_snapshot,state,created_at)
		SELECT md5(label),'subtitle_ocr',label,'retained-library','retained-root','retained-actor','retained-credential',
		label,sha256(convert_to(label,'UTF8')),'retained-media','retained-revision',0,'{}','{}','{}','completed',
		'2025-01-01T00:00:00Z'::timestamptz+n*interval '1 second'
		FROM (SELECT n,'retained-pending-history-'||n AS label FROM generate_series(1,12000) n) fixture`); err != nil {
		t.Fatal("seed bounded operation history", err)
	}
	t.Log("pending_index_fixture terminal_history=12000 mixed_states=40 pending=6 cancellations=4")
	before := map[bool][]string{}
	for _, phase := range []string{"schema66", "schema67"} {
		if phase == "schema67" {
			themeOwnersMigrateTo(t, ctx, pool, 67)
			assertMediaOperationsPendingIndex(t, ctx, pool)
		}
		if _, err := pool.Exec(ctx, "VACUUM (ANALYZE) media_operations"); err != nil {
			t.Fatal(err)
		}
		var indexBytes int64
		if err := pool.QueryRow(ctx, `SELECT COALESCE(pg_relation_size(to_regclass('media_operations_pending_order_idx')),0)`).Scan(&indexBytes); err != nil {
			t.Fatal(err)
		}
		for _, cancellationOnly := range []bool{false, true} {
			ids := mediaOperationsPendingIDs(t, ctx, pool, cancellationOnly)
			if phase == "schema66" {
				before[cancellationOnly] = ids
			} else if !reflect.DeepEqual(ids, before[cancellationOnly]) {
				t.Fatal("pending-index observation changed candidates or priority order")
			}
			for trial := range 3 {
				var plan json.RawMessage
				if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+mediaOperationsPendingObservationSQL,
					16, cancellationOnly).Scan(&plan); err != nil {
					t.Fatal(err)
				}
				t.Logf("pending_index_observation phase=%s cancellation_only=%t trial=%d index_bytes=%d plan=%s",
					phase, cancellationOnly, trial, indexBytes, plan)
			}
		}
		connection, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
			if _, err := connection.Exec(ctx, "SET plan_cache_mode="+mode); err != nil {
				connection.Release()
				t.Fatal(err)
			}
			if _, err := connection.Exec(ctx, "PREPARE media_pending_observation(integer,boolean) AS "+mediaOperationsPendingObservationSQL); err != nil {
				connection.Release()
				t.Fatal(err)
			}
			for _, flag := range []string{"false", "true"} {
				var plan json.RawMessage
				if err := connection.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE media_pending_observation(16,"+flag+")").Scan(&plan); err != nil {
					connection.Release()
					t.Fatal(err)
				}
				t.Logf("pending_index_prepared phase=%s mode=%s cancellation_only=%s plan=%s", phase, mode, flag, plan)
			}
			if _, err := connection.Exec(ctx, "DEALLOCATE media_pending_observation"); err != nil {
				connection.Release()
				t.Fatal(err)
			}
		}
		if _, err := connection.Exec(ctx, "RESET plan_cache_mode"); err != nil {
			connection.Release()
			t.Fatal(err)
		}
		connection.Release()
	}
}

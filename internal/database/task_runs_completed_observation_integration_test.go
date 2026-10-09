package database_test

import (
	"encoding/json"
	"os"
	"testing"
)

// This opt-in observation retains the same rows before and after schema 65.
// Plans and buffer counts are evidence for this fixture, not a latency promise.
func TestTaskRunsCompletedIndexPostgreSQL17Observation(t *testing.T) {
	if os.Getenv("GOBY_TEST_TASK_COMPLETED_INDEX_OBSERVATION") != "1" {
		t.Skip("GOBY_TEST_TASK_COMPLETED_INDEX_OBSERVATION=1 selects the bounded PostgreSQL observation")
	}
	ctx, pool := migrationTestPool(t)
	var major int
	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::integer/10000`).Scan(&major); err != nil || major != 17 {
		t.Fatalf("completed-run observation requires PostgreSQL 17: major=%d error=%v", major, err)
	}
	themeOwnersMigrateTo(t, ctx, pool, 64)
	if _, err := pool.Exec(ctx, `INSERT INTO task_definitions(id,key,name)
		SELECT md5('completed-index-'||n),'completed-index-'||n,'Observed task '||n FROM generate_series(1,8) n;
		INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,created_at,finished_at)
		SELECT md5('history-'||task||'-'||n),md5('completed-index-'||task),'completed','manual',
		'completed-index-'||task,'Observed task '||task,
		'2026-01-01T00:00:00Z'::timestamptz+n*interval '1 second',
		'2026-01-02T00:00:00Z'::timestamptz+((n*17)%4000)*interval '1 second'
		FROM generate_series(1,8) task CROSS JOIN generate_series(1,4000) n;
		INSERT INTO task_runs(id,task_id,state,source,task_key,task_name)
		SELECT md5('active-'||n),md5('completed-index-'||n),'pending','manual','completed-index-'||n,'Observed task '||n
		FROM generate_series(1,8) n`); err != nil {
		t.Fatal("seed bounded completed task histories", err)
	}
	t.Log("completed_index_fixture tasks=8 completed_runs=32000 active_runs=8")
	const statement = `SELECT to_jsonb(r) - 'request_fingerprint' FROM task_runs r
		WHERE r.task_id=md5('completed-index-4') AND r.finished_at IS NOT NULL
		ORDER BY r.finished_at DESC,r.id DESC LIMIT 1`
	var original json.RawMessage
	for _, phase := range []string{"schema64", "schema65"} {
		if phase == "schema65" {
			themeOwnersMigrateTo(t, ctx, pool, 65)
			assertTaskRunsCompletedIndex(t, ctx, pool)
		}
		if _, err := pool.Exec(ctx, "VACUUM (ANALYZE) task_runs"); err != nil {
			t.Fatal("prepare retained task history statistics", err)
		}
		var result json.RawMessage
		if err := pool.QueryRow(ctx, statement).Scan(&result); err != nil {
			t.Fatal(err)
		}
		if phase == "schema64" {
			original = result
		} else if string(result) != string(original) {
			t.Fatal("completed-run index changed the selected result")
		}
		var indexBytes int64
		if err := pool.QueryRow(ctx, `SELECT COALESCE(pg_relation_size(to_regclass('task_runs_completed_idx')),0)`).Scan(&indexBytes); err != nil {
			t.Fatal(err)
		}
		for trial := range 3 {
			var plan json.RawMessage
			if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+statement).Scan(&plan); err != nil {
				t.Fatal("observe completed-run projection", err)
			}
			t.Logf("completed_index_observation phase=%s trial=%d index_bytes=%d plan=%s", phase, trial, indexBytes, plan)
		}
	}
}

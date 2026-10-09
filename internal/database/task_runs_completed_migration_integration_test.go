package database_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

func assertTaskRunsCompletedIndex(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	err := pool.QueryRow(ctx, `SELECT c.relname='task_runs' AND a.amname='btree'
		AND i.indisvalid AND i.indisready AND i.indislive AND NOT i.indisunique
		AND i.indnkeyatts=3 AND i.indnatts=3 AND i.indoption::text='0 3 3'
		AND pg_get_indexdef(i.indexrelid,1,true)='task_id'
		AND pg_get_indexdef(i.indexrelid,2,true)='finished_at'
		AND pg_get_indexdef(i.indexrelid,3,true)='id'
		AND pg_get_expr(i.indpred,i.indrelid,true)='finished_at IS NOT NULL'
		AND to_regclass('task_runs_history_idx') IS NOT NULL
		FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid
		JOIN pg_class x ON x.oid=i.indexrelid JOIN pg_am a ON a.oid=x.relam
		WHERE i.indexrelid=to_regclass('task_runs_completed_idx')`).Scan(&valid)
	if err != nil || !valid {
		t.Fatalf("completed-run index lost its predicate, ordering, or history companion: valid=%v error=%v", valid, err)
	}
}

func taskRunsCompletedHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'definitions',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM task_definitions r),
		'runs',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM task_runs r),
		'history_index',pg_get_indexdef('task_runs_history_idx'::regclass),
		'migrations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY version) FROM schema_migrations r WHERE version<=64))::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestTaskRunsCompletedIndexFreshSchema(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	assertTaskRunsCompletedIndex(t, ctx, pool)
}

func TestTaskRunsCompletedIndexUpgradePreservesSchema64(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			themeOwnersMigrateTo(t, ctx, pool, 64)
			if _, err := pool.Exec(ctx, `INSERT INTO task_definitions(id,key,name)
				VALUES(repeat('1',32),'completed-index-history','Retained task history');
				INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,created_at,finished_at)
				SELECT repeat(n::text,32),repeat('1',32),state,'manual','completed-index-history','Retained task history',
				'2026-01-01T00:00:00Z'::timestamptz+n*interval '1 minute',finished::timestamptz
				FROM (VALUES(2,'completed','2026-01-02T00:00:00Z'),(3,'failed','2026-01-02T00:00:00Z'),
				(4,'cancelled','2026-01-01T12:00:00Z'),(5,'pending',NULL)) fixture(n,state,finished)`); err != nil {
				t.Fatal("seed retained completed and active task history", err)
			}
			before := taskRunsCompletedHistory(t, ctx, pool)
			for range 2 {
				if runner == "normal" {
					if err := database.Migrate(ctx, pool); err != nil {
						t.Fatal(err)
					}
				} else {
					themeOwnersMigrateTo(t, ctx, pool, 65)
				}
				assertTaskRunsCompletedIndex(t, ctx, pool)
				if taskRunsCompletedHistory(t, ctx, pool) != before {
					t.Fatal("completed-run index migration changed retained rows, history ordering, or the published prefix")
				}
				wantVersion := int64(65)
				if runner == "normal" {
					migrations, err := database.EmbeddedMigrations()
					if err != nil {
						t.Fatal(err)
					}
					wantVersion = migrations[len(migrations)-1].Version
				}
				if version, err := database.SchemaVersion(ctx, pool); err != nil || version != wantVersion {
					t.Fatalf("completed-run migration version = %d, want %d: %v", version, wantVersion, err)
				}
			}
		})
	}
}

func TestTaskRunsCompletedIndexRecoveryMigrationRollsBackWithCaller(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersMigrateTo(t, ctx, pool, 64)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := database.RecoveryMigrateTo(ctx, tx, 65); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var unchanged bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('task_runs_completed_idx') IS NULL
		AND to_regclass('task_runs_history_idx') IS NOT NULL
		AND NOT EXISTS(SELECT 1 FROM schema_migrations WHERE version=65)`).Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("rolled-back migration retained the completed-run index or its history row: %v", err)
	}
}

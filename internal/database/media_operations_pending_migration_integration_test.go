package database_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

const mediaOperationsPendingIndexPredicate = `worker_token='' AND (state IN ('queued','applying')
	OR (state IN ('ready','interrupted') AND cancel_requested_at IS NOT NULL))`

const mediaOperationsPendingObservationSQL = `SELECT id,state,parameters FROM media_operations
	WHERE ` + mediaOperationsPendingIndexPredicate + `
	AND (NOT $2::boolean OR (cancel_requested_at IS NOT NULL AND publication_phase='none'))
	ORDER BY (cancel_requested_at IS NOT NULL) DESC,created_at,id LIMIT $1`

func seedMediaOperationsPendingHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO media_operations(id,kind,source_item_id,source_library_id,source_root_id,
		request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,
		stream_index,parameters,source_snapshot,execution_snapshot,state,worker_token,cancel_requested_at,created_at)
		SELECT md5(label),'subtitle_ocr',label,'retained-library','retained-root','retained-actor','retained-credential',
		label,sha256(convert_to(label,'UTF8')),'retained-media','retained-revision',0,'{}','{}','{}',state,
		CASE WHEN owned THEN md5(label||'-worker') ELSE '' END,
		CASE WHEN cancelled THEN '2026-01-02T00:00:00Z'::timestamptz END,
		'2026-01-01T00:00:00Z'::timestamptz
		FROM (SELECT state,owned,cancelled,'pending-index-'||state||'-'||owned||'-'||cancelled AS label
			FROM (VALUES('queued'),('running'),('ready'),('applying'),('completed'),('failed'),
				('cancelled'),('interrupted'),('stale'),('recovery_required')) states(state)
			CROSS JOIN (VALUES(false),(true)) owners(owned)
			CROSS JOIN (VALUES(false),(true)) cancellations(cancelled)) fixture`); err != nil {
		t.Fatal("seed retained media operation states", err)
	}
}

func mediaOperationsPendingHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var history string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'operations',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM media_operations o),
		'queued_index',pg_get_indexdef('media_operations_runnable_idx'::regclass),
		'history_index',pg_get_indexdef('media_operations_history_idx'::regclass),
		'migrations',(SELECT jsonb_agg(to_jsonb(m) ORDER BY version) FROM schema_migrations m WHERE version<=66))::text`).Scan(&history); err != nil {
		t.Fatal(err)
	}
	return history
}

func mediaOperationsPendingIDs(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cancellationOnly bool) []string {
	t.Helper()
	rows, err := pool.Query(ctx, mediaOperationsPendingObservationSQL, 128, cancellationOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id, state string
		var parameters []byte
		if err := rows.Scan(&id, &state, &parameters); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func assertMediaOperationsPendingIndex(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	var predicate string
	if err := pool.QueryRow(ctx, `SELECT c.relname='media_operations' AND a.amname='btree'
		AND i.indisvalid AND i.indisready AND i.indislive AND NOT i.indisunique
		AND i.indnkeyatts=3 AND i.indnatts=3 AND i.indoption::text='3 0 0'
		AND pg_get_indexdef(i.indexrelid,1,true)='(cancel_requested_at IS NOT NULL)'
		AND pg_get_indexdef(i.indexrelid,2,true)='created_at'
		AND pg_get_indexdef(i.indexrelid,3,true)='id'
		AND to_regclass('media_operations_runnable_idx') IS NOT NULL
		AND to_regclass('media_operations_history_idx') IS NOT NULL,
		pg_get_expr(i.indpred,i.indrelid,true)
		FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid
		JOIN pg_class x ON x.oid=i.indexrelid JOIN pg_am a ON a.oid=x.relam
		WHERE i.indexrelid=to_regclass('media_operations_pending_order_idx')`).Scan(&valid, &predicate); err != nil || !valid {
		t.Fatalf("pending index lost ordering, validity or retained indexes: valid=%v error=%v", valid, err)
	}
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM media_operations
		WHERE (`+predicate+`) IS DISTINCT FROM (`+mediaOperationsPendingIndexPredicate+`))`).Scan(&valid); err != nil || !valid {
		t.Fatalf("pending index predicate changed state, ownership or cancellation eligibility: %v", err)
	}
}

func TestMediaOperationsPendingIndexFreshSchema(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	seedMediaOperationsPendingHistory(t, ctx, pool)
	assertMediaOperationsPendingIndex(t, ctx, pool)
	if got := mediaOperationsPendingIDs(t, ctx, pool, false); len(got) != 6 {
		t.Fatalf("full pending selection = %v, want six unowned candidates", got)
	}
	if got := mediaOperationsPendingIDs(t, ctx, pool, true); len(got) != 4 {
		t.Fatalf("cancellation selection = %v, want four unowned candidates", got)
	}
}

func TestMediaOperationsPendingIndexUpgradePreservesSchema66(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			themeOwnersMigrateTo(t, ctx, pool, 66)
			seedMediaOperationsPendingHistory(t, ctx, pool)
			before := mediaOperationsPendingHistory(t, ctx, pool)
			pending := mediaOperationsPendingIDs(t, ctx, pool, false)
			cancelled := mediaOperationsPendingIDs(t, ctx, pool, true)
			for range 2 {
				if runner == "normal" {
					if err := database.Migrate(ctx, pool); err != nil {
						t.Fatal(err)
					}
				} else {
					themeOwnersMigrateTo(t, ctx, pool, 67)
				}
				assertMediaOperationsPendingIndex(t, ctx, pool)
				if mediaOperationsPendingHistory(t, ctx, pool) != before ||
					!reflect.DeepEqual(mediaOperationsPendingIDs(t, ctx, pool, false), pending) ||
					!reflect.DeepEqual(mediaOperationsPendingIDs(t, ctx, pool, true), cancelled) {
					t.Fatal("pending index migration changed retained history, candidates or ordering")
				}
			}
		})
	}
}

func TestMediaOperationsPendingIndexRecoveryMigrationRollsBackWithCaller(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersMigrateTo(t, ctx, pool, 66)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := database.RecoveryMigrateTo(ctx, tx, 67); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var unchanged bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('media_operations_pending_order_idx') IS NULL
		AND to_regclass('media_operations_runnable_idx') IS NOT NULL
		AND NOT EXISTS(SELECT 1 FROM schema_migrations WHERE version=67)`).Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("rolled-back migration retained its index or history: %v", err)
	}
}

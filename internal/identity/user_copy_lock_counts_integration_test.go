package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type userCopyLockCountQuery struct {
	kind string
	sql  string
	args []any
	rows int64
	err  error
}

type userCopyLockCountTraceKey struct{}

type userCopyInsertGate struct {
	reached chan int32
	release chan struct{}
	entered sync.Once
	opened  sync.Once
}

func newUserCopyInsertGate() *userCopyInsertGate {
	return &userCopyInsertGate{reached: make(chan int32, 1), release: make(chan struct{})}
}

func (gate *userCopyInsertGate) open() {
	gate.opened.Do(func() { close(gate.release) })
}

type userCopyLockCountTrace struct {
	mu           sync.Mutex
	queries      []*userCopyLockCountQuery
	beforeInsert *userCopyInsertGate
}

func (trace *userCopyLockCountTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.Join(strings.Fields(data.SQL), " ")
	kind := ""
	if strings.Contains(statement, "FOR KEY SHARE OF i") && strings.Contains(statement, "JOIN user_item_data d") {
		kind = "items"
	} else if strings.Contains(statement, "FOR KEY SHARE OF entity") && strings.Contains(statement, "JOIN entity_user_data d") {
		kind = "entities"
	}
	if kind != "" {
		query := &userCopyLockCountQuery{kind: kind, sql: data.SQL, args: append([]any(nil), data.Args...)}
		trace.mu.Lock()
		trace.queries = append(trace.queries, query)
		trace.mu.Unlock()
		ctx = context.WithValue(ctx, userCopyLockCountTraceKey{}, query)
	}
	if gate := trace.beforeInsert; gate != nil && strings.HasPrefix(statement, "INSERT INTO users ") {
		// Pause before the INSERT reaches PostgreSQL. Any target-row locks at
		// this point belong to the copy's locking queries, not inserted FKs.
		gate.entered.Do(func() {
			gate.reached <- int32(conn.PgConn().PID())
			select {
			case <-gate.release:
			case <-ctx.Done():
			}
		})
	}
	return ctx
}

func (trace *userCopyLockCountTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	query, ok := ctx.Value(userCopyLockCountTraceKey{}).(*userCopyLockCountQuery)
	if !ok {
		return
	}
	trace.mu.Lock()
	query.rows, query.err = data.CommandTag.RowsAffected(), data.Err
	trace.mu.Unlock()
}

func (trace *userCopyLockCountTrace) snapshot() []userCopyLockCountQuery {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	queries := make([]userCopyLockCountQuery, len(trace.queries))
	for index, query := range trace.queries {
		queries[index] = *query
	}
	return queries
}

func (trace *userCopyLockCountTrace) reset() {
	trace.mu.Lock()
	trace.queries = nil
	trace.mu.Unlock()
}

type userCopyLockCountFixture struct {
	ctx    context.Context
	pool   *pgxpool.Pool
	copier *identity.Store
	trace  *userCopyLockCountTrace
	actor  identity.Principal
	source identity.User
}

func newUserCopyLockCountFixture(t *testing.T, items, entities int) userCopyLockCountFixture {
	t.Helper()
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	source, err := store.CreateUser(ctx, "Count Copy Source", "source-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type)
		VALUES('count-copy-library','Count Copy Library','movies')`); err != nil {
		t.Fatal(err)
	}
	if items != 0 {
		if _, err := pool.Exec(ctx, `WITH created AS (
			INSERT INTO items(id,library_id,name,sort_name,type)
			SELECT 'count-copy-item-' || lpad(n::text,6,'0'),'count-copy-library','Feature ' || n::text,n::text,'Movie'
			FROM generate_series(1,$2::integer) n RETURNING id)
			INSERT INTO user_item_data(user_id,item_id,play_count) SELECT $1,id,7 FROM created`, source.ID, items); err != nil {
			t.Fatal(err)
		}
	}
	if entities != 0 {
		if _, err := pool.Exec(ctx, `WITH created AS (
			INSERT INTO catalog_entities(kind,name)
			SELECT 'Tag','Count Copy Entity ' || n::text FROM generate_series(1,$2::integer) n RETURNING id)
			INSERT INTO entity_user_data(user_id,entity_id,play_count) SELECT $1,id,13 FROM created`, source.ID, entities); err != nil {
			t.Fatal(err)
		}
	}
	trace := &userCopyLockCountTrace{}
	config := pool.Config()
	config.ConnConfig.Tracer = trace
	tracedPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tracedPool.Close)
	return userCopyLockCountFixture{ctx: ctx, pool: pool, copier: identity.New(tracedPool), trace: trace, actor: actor, source: source}
}

func assertUserCopyLockCountQueries(t *testing.T, trace *userCopyLockCountTrace, sourceID string, limits ...int) []userCopyLockCountQuery {
	t.Helper()
	queries := trace.snapshot()
	if len(queries) != len(limits) {
		t.Fatalf("copy issued %d target-lock queries, want %d", len(queries), len(limits))
	}
	for index, query := range queries {
		wantKind := "items"
		if index == 1 {
			wantKind = "entities"
		}
		if query.kind != wantKind || query.err != nil || query.rows != 1 {
			t.Fatalf("target-lock query %d: kind=%s, returned rows=%d, error=%v; want %s, one row, no error",
				index, query.kind, query.rows, query.err, wantKind)
		}
		if len(query.args) != 2 || query.args[0] != sourceID || fmt.Sprint(query.args[1]) != fmt.Sprint(limits[index]) {
			t.Fatalf("%s lock query did not use the source and remaining combined budget: %v", query.kind, query.args)
		}
		t.Logf("copy target locks: kind=%s returned_rows=%d limit=%d", query.kind, query.rows, limits[index])
	}
	return queries
}

func TestUserCopyLockCountsBoundResultCardinalityAndCombinedBudget(t *testing.T) {
	for _, test := range []struct {
		name            string
		items, entities int
	}{
		{name: "empty"},
		{name: "items", items: 100000},
		{name: "mixed", items: 50000, entities: 50000},
		{name: "entities", entities: 100000},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Run one target population at a time, then reuse it for overflow.
			// The next phase starts only after this schema has been removed.
			fixture := newUserCopyLockCountFixture(t, test.items, test.entities)
			logUserCopyLockCountFixtureSize(t, fixture, "source")
			queries := assertUserCopyLockCountBoundary(t, fixture, test.items, test.entities, false)
			logUserCopyLockCountFixtureSize(t, fixture, "copied")
			if test.name == "mixed" {
				for _, query := range queries {
					assertUserCopyLockCountPlan(t, fixture.ctx, fixture.pool, query, 50000)
				}
			}
			if test.name == "empty" {
				return
			}
			items, entities := test.items, test.entities
			if test.name == "items" {
				if _, err := fixture.pool.Exec(fixture.ctx, `WITH created AS (
					INSERT INTO items(id,library_id,name,sort_name,type)
					VALUES('count-copy-item-100001','count-copy-library','Overflow Feature','Overflow Feature','Movie') RETURNING id)
					INSERT INTO user_item_data(user_id,item_id,play_count) SELECT $1,id,7 FROM created`, fixture.source.ID); err != nil {
					t.Fatal(err)
				}
				items++
			} else {
				if _, err := fixture.pool.Exec(fixture.ctx, `WITH created AS (
					INSERT INTO catalog_entities(kind,name) VALUES('Tag','Count Copy Overflow Entity') RETURNING id)
					INSERT INTO entity_user_data(user_id,entity_id,play_count) SELECT $1,id,13 FROM created`, fixture.source.ID); err != nil {
					t.Fatal(err)
				}
				entities++
			}
			assertUserCopyLockCountBoundary(t, fixture, items, entities, true)
		})
	}
}

func assertUserCopyLockCountBoundary(t *testing.T, fixture userCopyLockCountFixture, wantItems, wantEntities int, overLimit bool) []userCopyLockCountQuery {
	t.Helper()
	fixture.trace.reset()
	name := "Count Copy Result"
	if overLimit {
		name = "Count Copy Over Limit"
	}
	copied, err := fixture.copier.CreateManagedUserCopy(fixture.ctx, fixture.actor, name, fixture.source.ID, []string{"UserData"})
	if overLimit {
		var validation *identity.ManagedUserValidationError
		if !errors.As(err, &validation) || validation.Fields["UserCopyOptions"] == "" {
			t.Fatalf("over-limit copy returned %v, want a UserCopyOptions validation error", err)
		}
	} else if err != nil {
		t.Fatalf("copy at the supported budget failed: %v", err)
	}
	limits := []int{100001}
	if wantItems <= 100000 {
		limits = append(limits, 100001-wantItems)
	}
	queries := assertUserCopyLockCountQueries(t, fixture.trace, fixture.source.ID, limits...)
	var users, items, entities, wrongValues int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT count(*) FROM users WHERE normalized_name=lower($2)),
		(SELECT count(*) FROM user_item_data WHERE user_id=$1),
		(SELECT count(*) FROM entity_user_data WHERE user_id=$1),
		(SELECT count(*) FROM user_item_data WHERE user_id=$1 AND play_count<>7)+
		(SELECT count(*) FROM entity_user_data WHERE user_id=$1 AND play_count<>13)`, copied.ID, name).
		Scan(&users, &items, &entities, &wrongValues); err != nil {
		t.Fatal(err)
	}
	if overLimit {
		if users != 0 || copied.ID != "" || items != 0 || entities != 0 {
			t.Fatal("over-limit copy left an account or partial copied state")
		}
	} else if users != 1 || items != wantItems || entities != wantEntities || wrongValues != 0 {
		t.Fatalf("copy counts: users=%d items=%d entities=%d wrong_values=%d", users, items, entities, wrongValues)
	}
	var sourceItems, sourceEntities int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT count(*) FROM user_item_data WHERE user_id=$1),
		(SELECT count(*) FROM entity_user_data WHERE user_id=$1)`, fixture.source.ID).Scan(&sourceItems, &sourceEntities); err != nil {
		t.Fatal(err)
	}
	if sourceItems != wantItems || sourceEntities != wantEntities {
		t.Fatal("copy changed the source's state cardinality")
	}
	return queries
}

func logUserCopyLockCountFixtureSize(t *testing.T, fixture userCopyLockCountFixture, phase string) {
	t.Helper()
	var items, entities, itemState, entityState, schema int64
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT pg_total_relation_size('items'),pg_total_relation_size('catalog_entities'),
		pg_total_relation_size('user_item_data'),pg_total_relation_size('entity_user_data'),
		(SELECT COALESCE(sum(pg_total_relation_size(c.oid)),0)::bigint FROM pg_class c
		JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind IN ('r','m'))`).
		Scan(&items, &entities, &itemState, &entityState, &schema); err != nil {
		t.Fatal(err)
	}
	t.Logf("copy fixture relation bytes: phase=%s items=%d entities=%d item_state=%d entity_state=%d schema=%d",
		phase, items, entities, itemState, entityState, schema)
}

type userCopyLockCountPlan struct {
	NodeType    string                  `json:"Node Type"`
	ActualRows  int64                   `json:"Actual Rows"`
	ActualLoops int64                   `json:"Actual Loops"`
	Plans       []userCopyLockCountPlan `json:"Plans"`
}

func assertUserCopyLockCountPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query userCopyLockCountQuery, lockedRows int64) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanupCtx)
	}()
	var raw []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) "+query.sql, query.args...).Scan(&raw); err != nil {
		t.Fatalf("explain executed %s lock query: %v", query.kind, err)
	}
	var explained []struct {
		Plan userCopyLockCountPlan `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &explained); err != nil || len(explained) != 1 {
		t.Fatalf("decode %s lock query plan: %v", query.kind, err)
	}
	root := explained[0].Plan
	if root.NodeType != "Aggregate" || root.ActualRows != 1 || root.ActualLoops != 1 {
		t.Fatalf("%s lock projection is not a one-row executed aggregate: %s", query.kind, raw)
	}
	var nodes []string
	var boundedLockRows bool
	var visit func(userCopyLockCountPlan, bool)
	visit = func(node userCopyLockCountPlan, underLimit bool) {
		nodes = append(nodes, node.NodeType)
		if node.NodeType == "Limit" {
			underLimit = node.ActualRows == lockedRows && node.ActualLoops == 1
		}
		if node.NodeType == "LockRows" && underLimit && node.ActualRows == lockedRows && node.ActualLoops == 1 {
			boundedLockRows = true
		}
		for _, child := range node.Plans {
			visit(child, underLimit)
		}
	}
	visit(root, false)
	if !boundedLockRows {
		t.Fatalf("%s query did not execute %d row locks below its limit: %s", query.kind, lockedRows, raw)
	}
	t.Logf("copy lock plan: kind=%s returned_rows=%d locked_rows=%d nodes=%v plan=%s", query.kind, root.ActualRows, lockedRows, nodes, raw)
}

type userCopyCountOutcome struct {
	user identity.User
	err  error
}

func startUserCopyCountOperation(ctx context.Context, fixture userCopyLockCountFixture) (<-chan userCopyCountOutcome, <-chan struct{}) {
	results := make(chan userCopyCountOutcome, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		user, err := fixture.copier.CreateManagedUserCopy(ctx, fixture.actor, "Count Concurrent Copy", fixture.source.ID, []string{"UserData"})
		results <- userCopyCountOutcome{user: user, err: err}
	}()
	return results, done
}

func waitUserCopyInsertGate(t *testing.T, ctx context.Context, gate *userCopyInsertGate, done <-chan struct{}) int32 {
	t.Helper()
	select {
	case pid := <-gate.reached:
		return pid
	case <-done:
		t.Fatal("copy finished before its target locks were acquired")
	case <-ctx.Done():
		t.Fatal("copy did not reach its pre-insert gate")
	}
	return 0
}

func awaitUserCopyCountOperation(t *testing.T, ctx context.Context, results <-chan userCopyCountOutcome) identity.User {
	t.Helper()
	select {
	case result := <-results:
		if result.err != nil {
			t.Fatalf("concurrent copy failed: %v", result.err)
		}
		return result.user
	case <-ctx.Done():
		t.Fatal("copy did not finish after releasing its pre-insert gate")
		return identity.User{}
	}
}

func cleanupUserCopyCountWorkers(t *testing.T, cancel context.CancelFunc, gate *userCopyInsertGate, workers ...<-chan struct{}) {
	t.Helper()
	t.Cleanup(func() {
		cancel()
		gate.open()
		deadline, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		for _, done := range workers {
			select {
			case <-done:
			case <-deadline.Done():
				t.Error("copy concurrency worker did not stop before cleanup")
				return
			}
		}
	})
}

func TestUserCopyLockCountsRetainForeignKeyTargetsBeforeInsert(t *testing.T) {
	for _, target := range []string{"item", "entity"} {
		t.Run(target, func(t *testing.T) {
			fixture := newUserCopyLockCountFixture(t, 1, 1)
			ctx, cancel := context.WithTimeout(fixture.ctx, 20*time.Second)
			gate := newUserCopyInsertGate()
			fixture.trace.beforeInsert = gate
			results, copyDone := startUserCopyCountOperation(ctx, fixture)
			cleanupUserCopyCountWorkers(t, cancel, gate, copyDone)
			copyPID := waitUserCopyInsertGate(t, ctx, gate, copyDone)
			assertUserCopyLockCountQueries(t, fixture.trace, fixture.source.ID, 100001, 100000)
			statement := "DELETE FROM items WHERE id='count-copy-item-000001'"
			fragment := "DELETE FROM items"
			if target == "entity" {
				statement = "DELETE FROM catalog_entities WHERE name='Count Copy Entity 1'"
				fragment = "DELETE FROM catalog_entities"
			}
			type deletionOutcome struct {
				rows int64
				err  error
			}
			deletions := make(chan deletionOutcome, 1)
			deleteDone := make(chan struct{})
			go func() {
				defer close(deleteDone)
				tx, err := fixture.pool.Begin(ctx)
				if err != nil {
					deletions <- deletionOutcome{err: err}
					return
				}
				tag, err := tx.Exec(ctx, statement)
				// The deleting worker owns its transaction until rollback. Keeping
				// the delete uncommitted preserves copied state for the assertions.
				cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				err = errors.Join(err, tx.Rollback(cleanupCtx))
				deletions <- deletionOutcome{rows: tag.RowsAffected(), err: err}
			}()
			cleanupUserCopyCountWorkers(t, cancel, gate, copyDone, deleteDone)
			waitManagedBlockedQuery(t, ctx, fixture.pool, copyPID, fragment, deleteDone)
			var inserted bool
			if err := fixture.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE normalized_name='count concurrent copy')").Scan(&inserted); err != nil || inserted {
				t.Fatalf("new user existed before the target-lock proof: inserted=%v, error=%v", inserted, err)
			}
			gate.open()
			copied := awaitUserCopyCountOperation(t, ctx, results)
			select {
			case deletion := <-deletions:
				if deletion.err != nil || deletion.rows != 1 {
					t.Fatalf("%s deletion after copy commit: rows=%d, error=%v", target, deletion.rows, deletion.err)
				}
			case <-ctx.Done():
				t.Fatal("deletion did not finish after the copy released its target locks")
			}
			var copiedItems, copiedEntities int
			if err := fixture.pool.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM user_item_data WHERE user_id=$1 AND play_count=7),
				(SELECT count(*) FROM entity_user_data WHERE user_id=$1 AND play_count=13)`, copied.ID).
				Scan(&copiedItems, &copiedEntities); err != nil || copiedItems != 1 || copiedEntities != 1 {
				t.Fatalf("concurrent deletion damaged copied state: items=%d entities=%d error=%v", copiedItems, copiedEntities, err)
			}
		})
	}
}

package tasks

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/library"
)

const executionRefreshRunRead = "SELECT to_jsonb(r) - 'request_fingerprint' FROM task_runs r WHERE id = $1"

type executionRefreshObserver struct {
	owner      library.OwnedTransactions
	statements []string
	fail       func(string) error
}

func (observer *executionRefreshObserver) WithOwnedTx(ctx context.Context, callback func(library.OwnedTx) error) error {
	return observer.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		return callback(executionRefreshTx{OwnedTx: tx, observer: observer})
	})
}

func (observer *executionRefreshObserver) observe(statement string) error {
	statement = strings.Join(strings.Fields(statement), " ")
	observer.statements = append(observer.statements, statement)
	if observer.fail != nil {
		return observer.fail(statement)
	}
	return nil
}

func (observer *executionRefreshObserver) assertQueries(t *testing.T, locked, readback, aggregates, writes int) {
	t.Helper()
	if locked != 0 && (len(observer.statements) == 0 || observer.statements[0] != executionRefreshRunRead+" FOR UPDATE") {
		t.Fatalf("execution refresh did not lock its parent before child work: %v", observer.statements)
	}
	var actual [4]int
	for _, statement := range observer.statements {
		switch {
		case statement == executionRefreshRunRead+" FOR UPDATE":
			actual[0]++
		case statement == executionRefreshRunRead:
			actual[1]++
		case strings.HasPrefix(statement, "SELECT count(*),") && strings.Contains(statement, "FROM task_run_children WHERE run_id = $1"):
			actual[2]++
		case strings.HasPrefix(statement, "UPDATE task_runs SET state = $2,"):
			actual[3]++
		}
	}
	if want := [4]int{locked, readback, aggregates, writes}; actual != want {
		t.Fatalf("execution refresh queries = %v, want %v; statements=%v", actual, want, observer.statements)
	}
}

type executionRefreshTx struct {
	library.OwnedTx
	observer *executionRefreshObserver
}

func (tx executionRefreshTx) Exec(statement string, args ...any) (pgconn.CommandTag, error) {
	if err := tx.observer.observe(statement); err != nil {
		return pgconn.CommandTag{}, err
	}
	return tx.OwnedTx.Exec(statement, args...)
}

func (tx executionRefreshTx) QueryRow(statement string, args ...any) library.OwnedRow {
	if err := tx.observer.observe(statement); err != nil {
		return executionRefreshErrorRow{err: err}
	}
	return tx.OwnedTx.QueryRow(statement, args...)
}

type executionRefreshErrorRow struct{ err error }

func (row executionRefreshErrorRow) Scan(...any) error { return row.err }

type executionRefreshFixture struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	store      *Store
	actor      Actor
	definition Definition
	observer   *executionRefreshObserver
	run        Run
	children   []Child
	tokens     []string
}

func newExecutionRefreshFixture(t *testing.T, key string, libraryCount int) *executionRefreshFixture {
	t.Helper()
	ctx, pool, owner, _, actor, _ := taskRepository(t, libraryCount)
	registry, err := NewExecutorRegistry(ExecutorRegistration{Key: key, Name: "Execution refresh", Executor: &genericTestExecutor{available: true}})
	if err != nil {
		t.Fatal(err)
	}
	observer := &executionRefreshObserver{owner: owner}
	store, err := New(pool, observer, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if key == library.TaskBackgroundPreviewGenerationKey {
		if _, err := pool.Exec(ctx, `UPDATE libraries SET options=options||'{"EnableBackgroundPreviewGeneration":true}'::jsonb`); err != nil {
			t.Fatal(err)
		}
	}
	definition, err := store.GetByKey(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &executionRefreshFixture{ctx: ctx, pool: pool, store: store, actor: actor, definition: definition, observer: observer}
	fixture.start(t)
	return fixture
}

func (fixture *executionRefreshFixture) start(t *testing.T) {
	t.Helper()
	admission, err := fixture.store.Start(fixture.ctx, fixture.actor, StartRequest{TaskID: fixture.definition.ID})
	if err != nil || !admission.Admitted {
		t.Fatalf("admit execution refresh fixture: %+v %v", admission, err)
	}
	fixture.run, err = fixture.store.BeginRun(fixture.ctx, admission.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := fixture.store.ListChildren(fixture.ctx, fixture.run.ID, Page{})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("execution refresh children: %+v %v", page, err)
	}
	fixture.children, fixture.tokens = page.Items, make([]string, len(page.Items))
	for index, child := range fixture.children {
		fixture.tokens[index], err = randomID()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.claimExecution(fixture.ctx, fixture.run.ID, child.ID, fixture.tokens[index]); err != nil {
			t.Fatal(err)
		}
	}
	fixture.observer.statements = nil
}

func (fixture *executionRefreshFixture) snapshot(t *testing.T) string {
	t.Helper()
	var result string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT jsonb_build_object('run',to_jsonb(r),
		'children',(SELECT jsonb_agg(to_jsonb(c) ORDER BY c.ordinal) FROM task_run_children c WHERE c.run_id=r.id),
		'queue',(SELECT jsonb_agg(to_jsonb(q) ORDER BY q.item_id) FROM background_preview_queue q WHERE q.run_id=r.id))::text
		FROM task_runs r WHERE r.id=$1`, fixture.run.ID).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestExecutionRefreshReusesParentAndReadsFreshDurableTotals(t *testing.T) {
	f := newExecutionRefreshFixture(t, MetadataRefreshKey, 2)
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_run_children SET scanned=5,added=2,updated=1 WHERE id=$1`, f.children[1].ID); err != nil {
		t.Fatal(err)
	}
	progress := Progress{Processed: 7, Added: 4, Updated: 2}
	if err := f.store.updateExecutionProgress(f.ctx, f.run.ID, f.children[0].ID, f.tokens[0], progress); err != nil {
		t.Fatal(err)
	}
	f.observer.assertQueries(t, 1, 1, 1, 1)
	run, err := f.store.GetRun(f.ctx, f.run.ID)
	if err != nil || run.State != RunRunning || run.Scanned != 12 || run.Added != 6 || run.Updated != 3 || !reflect.DeepEqual(run.StartedAt, f.run.StartedAt) {
		t.Fatalf("progress lost fresh sibling totals or its runtime origin: %+v %v", run, err)
	}
	var version string
	if err := f.pool.QueryRow(f.ctx, "SELECT xmin::text FROM task_runs WHERE id=$1", f.run.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	f.observer.statements = nil
	if err := f.store.updateExecutionProgress(f.ctx, f.run.ID, f.children[0].ID, f.tokens[0], progress); err != nil {
		t.Fatal(err)
	}
	f.observer.assertQueries(t, 1, 0, 1, 0)
	var unchanged string
	if err := f.pool.QueryRow(f.ctx, "SELECT xmin::text FROM task_runs WHERE id=$1", f.run.ID).Scan(&unchanged); err != nil || unchanged != version {
		t.Fatalf("unchanged execution progress rewrote its parent: before=%s after=%s error=%v", version, unchanged, err)
	}
	for _, test := range []struct {
		token string
		value Progress
	}{
		{token: "stale-token", value: progress},
		{token: f.tokens[0], value: Progress{Processed: 6, Added: 4, Updated: 2}},
	} {
		before := f.snapshot(t)
		f.observer.statements = nil
		if err := f.store.updateExecutionProgress(f.ctx, f.run.ID, f.children[0].ID, test.token, test.value); !errors.Is(err, ErrInconsistent) {
			t.Fatalf("invalid progress bypassed the current claim: %v", err)
		}
		f.observer.assertQueries(t, 1, 0, 0, 0)
		if f.snapshot(t) != before {
			t.Fatal("rejected progress changed the parent or child")
		}
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE task_runs SET total_children=3 WHERE id=$1", f.run.ID); err != nil {
		t.Fatal(err)
	}
	for _, finishing := range []bool{false, true} {
		before := f.snapshot(t)
		f.observer.statements = nil
		var err error
		if finishing {
			err = f.store.finishExecution(f.ctx, f.run.ID, f.children[0].ID, f.tokens[0], nil, false)
		} else {
			err = f.store.updateExecutionProgress(f.ctx, f.run.ID, f.children[0].ID, f.tokens[0], progress)
		}
		if !errors.Is(err, ErrInconsistent) || f.snapshot(t) != before {
			t.Fatalf("aggregate mismatch was accepted or changed durable state: finishing=%t error=%v", finishing, err)
		}
		f.observer.assertQueries(t, 1, 0, 1, 0)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE task_runs SET total_children=2 WHERE id=$1", f.run.ID); err != nil {
		t.Fatal(err)
	}
	for index, child := range f.children {
		f.observer.statements = nil
		if err := f.store.finishExecution(f.ctx, f.run.ID, child.ID, f.tokens[index], nil, false); err != nil {
			t.Fatal(err)
		}
		f.observer.assertQueries(t, 1, 1, 1, 1)
		run, err = f.store.GetRun(f.ctx, f.run.ID)
		if err != nil || run.TerminalChildren != int64(index+1) || run.CompletedChildren != int64(index+1) || run.Scanned != 12 {
			t.Fatalf("completion lost fresh child results: %+v %v", run, err)
		}
		if index == 0 && run.State != RunRunning || index == 1 && (run.State != RunCompleted || run.FinishedAt == nil) {
			t.Fatalf("completion changed the aggregate terminal boundary: %+v", run)
		}
	}
	f.observer.statements = nil
	if err := f.store.finishExecution(f.ctx, f.run.ID, f.children[1].ID, f.tokens[1], nil, false); err != nil {
		t.Fatal(err)
	}
	f.observer.assertQueries(t, 1, 0, 0, 0)
	assertTaskLifecycleAudit(t, f.ctx, f.pool, f.run.ID,
		taskLifecycleUserEntry("task.admitted", "native", f.run.ID, f.actor, f.definition.Revision, 2),
		taskLifecycleFinishedEntry(f.run.ID, RunCompleted, 2))
}

func TestExecutionRefreshFailuresRollBackChildParentAndQueue(t *testing.T) {
	f := newExecutionRefreshFixture(t, library.TaskBackgroundPreviewGenerationKey, 1)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items(id,library_id,name,sort_name,type) VALUES('refresh-queue-item','library-1','Queue item','queue item','Movie');
		INSERT INTO background_preview_queue(item_id) VALUES('refresh-queue-item')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE background_preview_queue SET state='running',claimed_revision=1,run_id=$1,child_id=$2`, f.run.ID, f.children[0].ID); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		progress bool
		prefix   string
	}{
		{name: "progress totals", progress: true, prefix: "SELECT count(*),"},
		{name: "progress readback", progress: true, prefix: executionRefreshRunRead},
		{name: "finish totals", prefix: "SELECT count(*),"},
		{name: "finish readback", prefix: executionRefreshRunRead},
		{name: "finish queue", prefix: "UPDATE background_preview_queue"},
		{name: "finish activity", prefix: "INSERT INTO activity_entries"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := f.snapshot(t)
			injected := errors.New("injected execution refresh failure")
			f.observer.fail = func(statement string) error {
				if strings.HasPrefix(statement, test.prefix) && !strings.HasSuffix(statement, " FOR UPDATE") {
					return injected
				}
				return nil
			}
			var err error
			if test.progress {
				err = f.store.updateExecutionProgress(f.ctx, f.run.ID, f.children[0].ID, f.tokens[0], Progress{Processed: 9})
			} else {
				err = f.store.finishExecution(f.ctx, f.run.ID, f.children[0].ID, f.tokens[0], nil, false)
			}
			f.observer.fail = nil
			if !errors.Is(err, injected) || f.snapshot(t) != before {
				t.Fatalf("failed execution refresh changed durable state or lost its error: %v", err)
			}
			assertTaskLifecycleAudit(t, f.ctx, f.pool, f.run.ID,
				taskLifecycleUserEntry("task.admitted", "native", f.run.ID, f.actor, f.definition.Revision, 1))
		})
	}
	f.observer.statements = nil
	if err := f.store.finishExecution(f.ctx, f.run.ID, f.children[0].ID, f.tokens[0], nil, false); err != nil {
		t.Fatal(err)
	}
	f.observer.assertQueries(t, 1, 1, 1, 1)
	var state, code string
	if err := f.pool.QueryRow(f.ctx, "SELECT state,error_code FROM background_preview_queue WHERE item_id='refresh-queue-item'").Scan(&state, &code); err != nil || state != "pending" || code != "server_interrupted" {
		t.Fatalf("terminal refresh did not settle the retained queue using its persisted Run: state=%s code=%s error=%v", state, code, err)
	}
	assertTaskLifecycleAudit(t, f.ctx, f.pool, f.run.ID,
		taskLifecycleUserEntry("task.admitted", "native", f.run.ID, f.actor, f.definition.Revision, 1),
		taskLifecycleFinishedEntry(f.run.ID, RunCompleted, 1))
}

func TestExecutionRefreshRetainsStopShutdownAndClaimPredicates(t *testing.T) {
	f := newExecutionRefreshFixture(t, MetadataRefreshKey, 1)
	for index, reason := range []string{"administrator", "max_runtime", "shutdown"} {
		if index != 0 {
			f.start(t)
		}
		var stopping Run
		var err error
		if reason == "administrator" {
			stopping, err = f.store.Stop(f.ctx, f.actor, f.run.ID)
		} else {
			stopping, err = f.store.SystemStopRun(f.ctx, f.run.ID, reason)
		}
		if err != nil || stopping.State != RunStopping || stopping.StopReason != reason {
			t.Fatalf("stop fixture did not retain the running child: %+v %v", stopping, err)
		}
		before := f.snapshot(t)
		if err := f.store.updateExecutionProgress(f.ctx, f.run.ID, f.children[0].ID, f.tokens[0], Progress{Processed: 1}); !errors.Is(err, context.Canceled) {
			t.Fatalf("stopping parent accepted progress: %v", err)
		}
		if err := f.store.finishExecution(f.ctx, f.run.ID, f.children[0].ID, "stale-token", nil, reason == "shutdown"); !errors.Is(err, ErrInconsistent) || f.snapshot(t) != before {
			t.Fatalf("stopping completion bypassed the current claim: %v", err)
		}
		f.observer.statements = nil
		if err := f.store.finishExecution(f.ctx, f.run.ID, f.children[0].ID, f.tokens[0], nil, reason == "shutdown"); err != nil {
			t.Fatal(err)
		}
		f.observer.assertQueries(t, 1, 1, 1, 1)
		run, err := f.store.GetRun(f.ctx, f.run.ID)
		wantState, wantCode := RunCancelled, "cancelled"
		if reason == "max_runtime" {
			wantCode = "max_runtime"
		} else if reason == "shutdown" {
			wantState, wantCode = RunInterrupted, "server_interrupted"
		}
		if err != nil || run.State != wantState || run.ErrorCode != wantCode || run.TerminalChildren != 1 || !reflect.DeepEqual(run.StopRequestedAt, stopping.StopRequestedAt) {
			t.Fatalf("completion replaced the locked stop decision: %+v %v", run, err)
		}
	}
}

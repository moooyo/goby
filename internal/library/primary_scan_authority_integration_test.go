//go:build linux

package library

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
)

type scanOperationAuthoritySQLTracer struct {
	queries  atomic.Int64
	startups atomic.Int64
}

func (trace *scanOperationAuthoritySQLTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.queries.Add(1)
	if strings.Contains(data.SQL, "/* scan_operation_authority */") {
		trace.startups.Add(1)
	}
	return ctx
}

func (*scanOperationAuthoritySQLTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func (trace *scanOperationAuthoritySQLTracer) reset() {
	trace.queries.Store(0)
	trace.startups.Store(0)
}

func TestScanOperationAuthorityReusesStartupSnapshotWithoutSQL(t *testing.T) {
	trace := &scanOperationAuthoritySQLTracer{}
	ctx, pool, store, state, _, _ := scanCachedVisitFixture(t, trace)
	before, err := state.readPrimaryScanAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if trace.startups.Load() == 0 {
		t.Fatal("fixture did not capture a startup authorization snapshot")
	}
	if _, err := pool.Exec(ctx, `UPDATE library_roots SET binding_revision=binding_revision+1,
		storage_binding=NULL,bound_at=NULL,bound_by=NULL WHERE id=$1`, state.root.id); err != nil {
		t.Fatal(err)
	}
	trace.reset()
	for range 8 {
		row, err := state.readPrimaryScanAuthority(ctx)
		if err != nil || !row.same(before) {
			t.Fatalf("active scan lost its startup approval snapshot: row=%+v error=%v", row, err)
		}
		row, err = store.readScanOperationRoot(ctx, state.task, state.root.id)
		if err != nil || !row.same(before) {
			t.Fatalf("root lookup lost its operation snapshot: row=%+v error=%v", row, err)
		}
	}
	if trace.queries.Load() != 0 || trace.startups.Load() != 0 {
		t.Fatalf("operation snapshot reuse issued SQL: queries=%d startup=%d", trace.queries.Load(), trace.startups.Load())
	}
	if !store.Available() {
		t.Fatal("an approval change retired the active catalog owner")
	}
}

func TestScanOperationAuthorityStartsFreshForAnotherTask(t *testing.T) {
	trace := &scanOperationAuthoritySQLTracer{}
	ctx, pool, store, state, _, _ := scanCachedVisitFixture(t, trace)
	before, err := state.readPrimaryScanAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", state.root.id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE scan_jobs SET status='Completed',finished_at=clock_timestamp() WHERE id=$1", state.task.job.ID); err != nil {
		t.Fatal(err)
	}
	state.task.cancel()
	store.mu.Lock()
	delete(store.active, state.task.job.ID)
	store.mu.Unlock()
	next := rootBindingScanOwnedTask(t, ctx, pool, store, state.library)
	trace.reset()
	if err := store.prepareScanOperationAuthority(ctx, next, []libraryRoot{state.root}); err != nil {
		t.Fatalf("fresh scan did not authorize its current root: %v", err)
	}
	if trace.startups.Load() != 1 {
		t.Fatalf("new task did not read one fresh root authorization snapshot: %d", trace.startups.Load())
	}
	trace.reset()
	after, err := store.readScanOperationAuthority(ctx, next, state.root)
	if err != nil || after.revision != before.revision+1 || after.same(before) {
		t.Fatalf("new task reused another operation's approval: before=%+v after=%+v error=%v", before, after, err)
	}
	if trace.queries.Load() != 0 {
		t.Fatalf("new operation did not retain its own startup snapshot: queries=%d", trace.queries.Load())
	}
}

func TestScanOperationAuthorityRejectsInadmissibleStartup(t *testing.T) {
	for _, mode := range []string{"manual", "task"} {
		for _, change := range []string{"persisted_cancel", "inactive_job", "changed_mapping", "lost_owner", "cancelled_context"} {
			t.Run(mode+"/"+change, func(t *testing.T) {
				prober := &primaryScanReadTestProber{joined: true}
				fixture := primaryScanReadFixtureAt(t, prober, "")
				ctx, pool, store, state := fixture.ctx, fixture.pool, fixture.store, fixture.state
				if mode == "manual" {
					if _, err := pool.Exec(ctx, "UPDATE scan_jobs SET task_child_id=NULL WHERE id=$1", state.task.job.ID); err != nil {
						t.Fatal(err)
					}
					state.task.job.TaskChildID = ""
				}
				switch change {
				case "persisted_cancel":
					if _, err := pool.Exec(ctx, "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", state.task.job.ID); err != nil {
						t.Fatal(err)
					}
				case "inactive_job":
					if _, err := pool.Exec(ctx, "UPDATE scan_jobs SET status='Completed',finished_at=clock_timestamp() WHERE id=$1", state.task.job.ID); err != nil {
						t.Fatal(err)
					}
				case "changed_mapping":
					if _, err := pool.Exec(ctx, "UPDATE library_roots SET relative_path='changed-root' WHERE id=$1", state.root.id); err != nil {
						t.Fatal(err)
					}
				case "lost_owner":
					store.mu.Lock()
					delete(store.active, state.task.job.ID)
					store.mu.Unlock()
				case "cancelled_context":
					state.task.cancel()
				}
				if err := store.prepareScanOperationAuthority(ctx, state.task, []libraryRoot{state.root}); err == nil {
					t.Fatal("inadmissible startup received an operation authorization")
				}
				if prober.calls.Load() != 0 || scanProbeOpenDescriptors(t, []string{fixture.path})[0] != 0 {
					t.Fatal("rejected startup delivered a source descriptor or probe callback")
				}
			})
		}
	}
}

func TestScanOperationAuthorityRetainsCancellationAndOperationIdentity(t *testing.T) {
	for _, change := range []string{"cancelled_context", "lost_owner", "changed_force_mode", "foreign_root"} {
		t.Run(change, func(t *testing.T) {
			trace := &scanOperationAuthoritySQLTracer{}
			ctx, _, store, state, _, _ := scanCachedVisitFixture(t, trace)
			root := state.root
			switch change {
			case "cancelled_context":
				state.task.cancel()
			case "lost_owner":
				store.mu.Lock()
				delete(store.active, state.task.job.ID)
				store.mu.Unlock()
			case "changed_force_mode":
				state.task.job.ForceProbe = !state.task.job.ForceProbe
			case "foreign_root":
				root.id = "foreign-operation-root"
			}
			trace.reset()
			_, err := store.readScanOperationAuthority(ctx, state.task, root)
			if err == nil || change == "cancelled_context" && !errors.Is(err, context.Canceled) {
				t.Fatalf("operation authorization accepted invalid execution state: %v", err)
			}
			if trace.queries.Load() != 0 {
				t.Fatalf("local operation rejection queried fresh permissions: queries=%d", trace.queries.Load())
			}
		})
	}
}

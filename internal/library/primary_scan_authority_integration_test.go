//go:build linux

package library

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/storagebinding"
)

func TestPrimaryScanAuthorityMeasurementSQLMatchesProduction(t *testing.T) {
	for _, pair := range []struct{ name, production, measurement string }{
		{"manual", primaryScanManualAuthoritySQL, scanPerformanceManualAuthoritySQL},
		{"task", primaryScanTaskAuthoritySQL, scanPerformanceTaskAuthoritySQL},
	} {
		if pair.production != pair.measurement {
			t.Fatalf("%s authority measurement SQL differs from production", pair.name)
		}
	}
}

func primaryScanAuthorityConsumeRows(ctx context.Context, query interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, statement string, arguments ...any) error {
	rows, err := query.Query(ctx, statement, arguments...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

func TestPrimaryScanAuthorityClassifiesRequestsAndReleasesFallbackConnection(t *testing.T) {
	for _, manual := range []bool{false, true} {
		mode := "task"
		if manual {
			mode = "manual"
		}
		t.Run(mode, func(t *testing.T) {
			fixture := primaryScanReadFixtureAt(t, &primaryScanReadTestProber{joined: true}, "")
			ctx, pool, task := fixture.ctx, fixture.pool, fixture.state.task
			statement := primaryScanTaskAuthoritySQL
			if manual {
				if _, err := pool.Exec(ctx, "UPDATE scan_jobs SET task_child_id=NULL WHERE id=$1", task.job.ID); err != nil {
					t.Fatal(err)
				}
				task.job.TaskChildID = ""
				statement = primaryScanManualAuthoritySQL
			}
			trace := &scanPerformanceSQLTracer{}
			config := pool.Config()
			config.MaxConns, config.MinConns = 1, 0
			config.ConnConfig.Tracer = trace
			authorityPool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			libraryIntegrationPoolCleanup(t, authorityPool)
			// This read-only authority fixture has no ownership session or workers.
			// A single connection makes an unreleased fast result block fallback.
			store := &Store{pool: authorityPool, roots: []approvedRoot{{path: fixture.state.root.allowedPath}},
				active: map[string]*scanTask{task.job.ID: task}}
			state := &scanState{store: store, task: task, root: fixture.state.root}
			arguments := []any{task.job.ID, task.job.LibraryID, task.job.TaskChildID, task.job.ForceProbe,
				state.root.id, storagebinding.MaxDocumentBytes}
			if row, err := state.readPrimaryScanAuthority(ctx); err != nil || row.root != state.root {
				t.Fatalf("single-request authority did not return the current root: row=%+v error=%v", row.root, err)
			}
			primaryScanRoutingAssertAuthorityBudget(t, trace, 1)
			if authorityPool.Stat().AcquiredConns() != 0 {
				t.Fatal("successful authority retained its only pooled connection")
			}

			trace.reset()
			tx, err := authorityPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if err := primaryScanAuthorityConsumeRows(ctx, tx, statement, arguments...); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if trace.authorityImplicitAttempts.Load() != 0 || trace.authorityImplicitCommits.Load() != 0 ||
				trace.authorityQueries.Load() != 0 || trace.begins.Load() != 1 || trace.commits.Load() != 1 {
				t.Fatal("authority SQL inside an explicit transaction was classified as a standalone request")
			}

			if _, err := pool.Exec(ctx, "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", task.job.ID); err != nil {
				t.Fatal(err)
			}
			trace.reset()
			fallbackCtx, cancelFallback := context.WithTimeout(ctx, 5*time.Second)
			_, err = state.readPrimaryScanAuthority(fallbackCtx)
			cancelFallback()
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("empty authority result did not release its connection for detailed cancellation fallback: %v", err)
			}
			if trace.authorityImplicitAttempts.Load() != 1 || trace.authorityImplicitCommits.Load() != 1 ||
				trace.authorityImplicitEmpty.Load() != 1 || trace.authorityImplicitSingleRows.Load() != 0 ||
				trace.authorityImplicitErrors.Load() != 0 || trace.authorityImplicitUnconfirmed.Load() != 0 ||
				trace.authorityImplicitUnexpectedRows.Load() != 0 || trace.begins.Load() != 1 || trace.rollbacks.Load() != 1 {
				t.Fatalf("empty authority result lost its committed request or explicit fallback: attempts=%d commits=%d empty=%d single_rows=%d errors=%d unconfirmed=%d unexpected=%d fallback_begin=%d rollback=%d",
					trace.authorityImplicitAttempts.Load(), trace.authorityImplicitCommits.Load(), trace.authorityImplicitEmpty.Load(),
					trace.authorityImplicitSingleRows.Load(), trace.authorityImplicitErrors.Load(), trace.authorityImplicitUnconfirmed.Load(),
					trace.authorityImplicitUnexpectedRows.Load(), trace.begins.Load(), trace.rollbacks.Load())
			}
			if authorityPool.Stat().AcquiredConns() != 0 {
				t.Fatal("detailed authority fallback retained its only pooled connection")
			}

			if _, err := pool.Exec(ctx, "UPDATE scan_jobs SET cancel_requested=false WHERE id=$1", task.job.ID); err != nil {
				t.Fatal(err)
			}
			var readerPID int32
			if err := authorityPool.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&readerPID); err != nil {
				t.Fatal(err)
			}
			gate, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(gate)
			if _, err := gate.Exec(ctx, "SELECT id FROM scan_jobs WHERE id=$1 FOR UPDATE", task.job.ID); err != nil {
				t.Fatal(err)
			}
			trace.reset()
			requestCtx, cancelRequest := context.WithTimeout(ctx, 10*time.Second)
			result, finished := make(chan error, 1), make(chan struct{})
			t.Cleanup(func() {
				cancelRequest()
				rollback(gate)
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				primaryScanReadSignal(t, cleanup, finished, "cancelled authority statement cleanup")
			})
			go func() {
				defer close(finished)
				_, err := state.readPrimaryScanAuthority(requestCtx)
				result <- err
			}()
			ownedTransactionsWaitForBlock(t, ctx, pool, readerPID, int32(gate.Conn().PgConn().PID()), result)
			var cancelled bool
			if err := pool.QueryRow(ctx, "SELECT pg_cancel_backend($1)", readerPID).Scan(&cancelled); err != nil || !cancelled {
				t.Fatalf("cancel the exact blocked authority statement: cancelled=%v error=%v", cancelled, err)
			}
			select {
			case err = <-result:
			case <-requestCtx.Done():
				t.Fatalf("cancelled authority statement did not return: %v", requestCtx.Err())
			}
			primaryScanReadSignal(t, ctx, finished, "cancelled authority statement completion")
			cancelRequest()
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != "57014" {
				t.Fatalf("authority did not preserve its database cancellation error: %v", err)
			}
			if trace.authorityImplicitAttempts.Load() != 1 || trace.authorityImplicitErrors.Load() != 1 ||
				trace.authorityImplicitCommits.Load() != 0 || trace.authorityImplicitSingleRows.Load() != 0 ||
				trace.authorityImplicitEmpty.Load() != 0 || trace.begins.Load() != 0 || trace.rollbacks.Load() != 0 {
				t.Fatal("failed authority statement was lost or reported as a committed authority transaction")
			}
			if err := gate.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if authorityPool.Stat().AcquiredConns() != 0 {
				t.Fatal("failed authority statement retained its only pooled connection")
			}
		})
	}
}

// The held transaction is private to this fixture. A database-observed lock
// wait proves that the reader reached the intended tuple before it changes.
func primaryScanAuthorityWaitForLock(t *testing.T, ctx context.Context, pool *pgxpool.Pool, blocker uint32, finished <-chan error) {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(waitCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock'
			AND $1::integer=ANY(pg_blocking_pids(pid)))`, blocker).Scan(&blocked); err != nil {
			t.Fatalf("observe scan authority waiting for its tuple lock: %v", err)
		}
		if blocked {
			return
		}
		select {
		case err := <-finished:
			t.Fatalf("scan authority returned before reaching the held tuple: %v", err)
		case <-ticker.C:
		case <-waitCtx.Done():
			t.Fatalf("scan authority did not reach its tuple lock: %v", waitCtx.Err())
		}
	}
}

func TestPrimaryScanAuthorityRechecksTuplesAfterLockWait(t *testing.T) {
	for _, manual := range []bool{false, true} {
		mode := "task"
		if manual {
			mode = "manual"
		}
		for _, scenario := range []struct {
			name, table, statement string
			want                   error
		}{
			{"unchanged_job", "scan_jobs", "UPDATE scan_jobs SET scanned=scanned WHERE id=$1", nil},
			{"cancel_job", "scan_jobs", "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", context.Canceled},
			{"unchanged_root", "library_roots", "UPDATE library_roots SET binding_revision=binding_revision WHERE id=$1", nil},
			{"root_revision", "library_roots", "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", ErrRootBindingConflict},
			{"root_document", "library_roots", `UPDATE library_roots SET storage_binding='{"authority_lock_witness":"after"}' WHERE id=$1`, ErrRootBindingConflict},
			{"cancel_run", "task_runs", "UPDATE task_runs SET state='stopping',stop_requested_at=clock_timestamp(),stop_reason='administrator' WHERE id=$1", context.Canceled},
			{"child_scan_link", "task_run_children", "UPDATE task_run_children SET scan_job_id=repeat('1',32) WHERE id=$1", ErrUnavailable},
			{"child_parent_link", "task_run_children", "UPDATE task_run_children SET run_id=$2 WHERE id=$1", context.Canceled},
		} {
			if manual && (scenario.table == "task_runs" || scenario.table == "task_run_children") {
				continue
			}
			t.Run(mode+"/"+scenario.name, func(t *testing.T) {
				fixture := primaryScanReadFixtureAt(t, &primaryScanReadTestProber{joined: true}, "")
				state, ctx, pool := fixture.state, fixture.ctx, fixture.pool
				var runID string
				if err := pool.QueryRow(ctx, "SELECT run_id FROM task_run_children WHERE id=$1", state.task.job.TaskChildID).Scan(&runID); err != nil {
					t.Fatal(err)
				}
				if manual {
					if _, err := pool.Exec(ctx, "UPDATE scan_jobs SET task_child_id=NULL WHERE id=$1", state.task.job.ID); err != nil {
						t.Fatal(err)
					}
					state.task.job.TaskChildID = ""
				}
				if scenario.name == "root_document" {
					if _, err := pool.Exec(ctx, `UPDATE library_roots SET storage_binding='{"authority_lock_witness":"before"}',
						bound_at=clock_timestamp(),bound_by='authority-lock-test' WHERE id=$1`, state.root.id); err != nil {
						t.Fatal(err)
					}
				}
				id := state.task.job.ID
				if scenario.table == "library_roots" {
					id = state.root.id
				} else if scenario.table == "task_runs" {
					id = runID
				} else if scenario.table == "task_run_children" {
					id = state.task.job.TaskChildID
				}
				arguments := []any{id}
				if scenario.name == "child_parent_link" {
					otherRun, _ := taskScanFixture(t, ctx, pool)
					if _, err := pool.Exec(ctx, "UPDATE task_runs SET state='stopping',stop_requested_at=clock_timestamp(),stop_reason='administrator' WHERE id=$1", otherRun); err != nil {
						t.Fatal(err)
					}
					arguments = append(arguments, otherRun)
				}
				primaryScanRoutingRetainWalk(t, state)
				assertUnread := primaryScanRoutingWatchSource(t, fixture.path)
				beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
				beforeJob, beforeWarnings := state.task.job, state.warnings
				gate, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer rollback(gate)
				var lockedID string
				if err := gate.QueryRow(ctx, "SELECT id FROM "+pgx.Identifier{scenario.table}.Sanitize()+" WHERE id=$1 FOR UPDATE", id).Scan(&lockedID); err != nil || lockedID != id {
					t.Fatalf("lock scan authority fixture tuple: id=%q error=%v", lockedID, err)
				}
				var called atomic.Bool
				result := make(chan error, 1)
				finished := make(chan struct{})
				t.Cleanup(func() {
					state.task.cancel()
					rollback(gate)
					cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					primaryScanReadSignal(t, cleanup, finished, "scan authority lock-wait cleanup")
				})
				go func() {
					defer close(finished)
					result <- state.runPrimaryScanMetadata(state.task.ctx, func(context.Context) error {
						called.Store(true)
						// The authority statement must finish its implicit transaction
						// before delivering even a successful source callback.
						var releasedID string
						if err := pool.QueryRow(ctx, "SELECT id FROM "+pgx.Identifier{scenario.table}.Sanitize()+" WHERE id=$1 FOR UPDATE NOWAIT", id).Scan(&releasedID); err != nil {
							return err
						}
						file, err := openScanFile(state.opened, "Feature.mp4")
						if err != nil {
							return err
						}
						return file.Close()
					})
				}()
				primaryScanAuthorityWaitForLock(t, ctx, pool, gate.Conn().PgConn().PID(), result)
				assertUnread()
				if called.Load() || scanProbeOpenDescriptors(t, []string{fixture.path})[0] != 0 {
					t.Fatal("scan source reached its callback while authority waited for a tuple lock")
				}
				if _, err := gate.Exec(ctx, scenario.statement, arguments...); err != nil {
					t.Fatal(err)
				}
				if err := gate.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case err = <-result:
				case <-ctx.Done():
					t.Fatalf("scan authority did not finish after its tuple lock released: %v", ctx.Err())
				}
				primaryScanReadSignal(t, ctx, finished, "scan authority completion")
				if scenario.want == nil {
					if err != nil || !called.Load() {
						t.Fatalf("unchanged post-wait authority did not grant the source callback: called=%v error=%v", called.Load(), err)
					}
				} else {
					// A changed parent may be rejected by the original locked lookup
					// or by its fresh fallback reading the replacement stopped run.
					matches := errors.Is(err, scenario.want) || scenario.name == "child_parent_link" && errors.Is(err, ErrNotFound)
					if !matches || called.Load() {
						t.Fatalf("scan authority accepted a stale tuple after waiting: called=%v error=%v want=%v", called.Load(), err, scenario.want)
					}
					assertUnread()
				}
				if scanProbeOpenDescriptors(t, []string{fixture.path})[0] != 0 || state.task.job.Scanned != beforeJob.Scanned ||
					state.task.job.Added != beforeJob.Added || state.task.job.Updated != beforeJob.Updated || state.warnings != beforeWarnings {
					t.Fatal("scan authority retained a source descriptor or changed scan progress")
				}
				if stats := originalMediaReadGovernor.Stats(); stats != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners {
					t.Fatalf("scan authority retained admission resources after its tuple wait: before=%+v after=%+v owners=%+v", beforeIO, stats, originalMediaReadOwners.Stats())
				}
			})
		}
	}
}

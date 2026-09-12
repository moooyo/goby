//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type rootBindingScanFixture struct {
	rootBindingReadFixture
	scanRoot libraryRoot
	task     *scanTask
}

func rootBindingScanOwnedTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, library Library) *scanTask {
	t.Helper()
	jobID, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	job, err := scanJob(pool.QueryRow(ctx, `INSERT INTO scan_jobs (id, library_id, status, started_at)
		VALUES ($1, $2, 'Running', clock_timestamp()) RETURNING `+jobColumns, jobID, library.ID))
	if err != nil {
		t.Fatal(err)
	}
	scanCtx, cancel := context.WithCancel(ctx)
	task := &scanTask{job: job, ctx: scanCtx, cancel: cancel}
	store.mu.Lock()
	store.active[job.ID] = task
	store.mu.Unlock()
	t.Cleanup(func() {
		cancel()
		store.mu.Lock()
		delete(store.active, job.ID)
		store.mu.Unlock()
	})
	return task
}

func newRootBindingScanFixture(t *testing.T) rootBindingScanFixture {
	t.Helper()
	fixture := newRootBindingReadFixture(t)
	fixture.bind(t, 13)
	root := libraryRoot{id: fixture.root.RootID, libraryID: fixture.library.ID, path: fixture.root.Path,
		allowedPath: fixture.root.AllowedPath, relativePath: fixture.root.RelativePath}
	return rootBindingScanFixture{fixture, root, rootBindingScanOwnedTask(t, fixture.ctx, fixture.pool, fixture.store, fixture.library)}
}

func (fixture rootBindingScanFixture) prepare(capture rootBindingWriteCapture) (*rootBindingScanCapture, error) {
	return fixture.store.prepareRootBindingScanWithCapture(fixture.task, fixture.scanRoot,
		func(context.Context, libraryRoot) (rootBindingWriteCapture, error) { return capture, nil })
}

func rootBindingScanAnchor(t *testing.T, store *Store, rootID string) *os.Root {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.rootBindingAnchors[rootID].approved
}

func TestRootBindingScanIntegrationMatchesApprovalWithoutAuthorityOrCatalogWrites(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	fixture.bind(t, 9223372036854775807)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET is_administrator = false, is_disabled = true WHERE id = $1`, fixture.actor.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1`, fixture.actor.SessionID); err != nil {
		t.Fatal(err)
	}
	before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
	previousAnchor := rootBindingScanAnchor(t, fixture.store, fixture.scanRoot.id)
	capture := &rootBindingScanTestCapture{snapshot: fixture.snapshot}
	capture.cloneHook = func() {
		if !fixture.store.mu.TryLock() {
			t.Fatal("candidate acquisition held Store.mu")
		}
		fixture.store.mu.Unlock()
		if !fixture.store.ownership.mu.TryLock() {
			t.Fatal("candidate acquisition held ownership.mu")
		}
		fixture.store.ownership.mu.Unlock()
	}
	capture.revalidate = func(ctx context.Context, count int) error {
		if count == 2 {
			if fixture.store.mu.TryLock() {
				fixture.store.mu.Unlock()
				t.Fatal("final capture validation released admission")
			}
			if fixture.store.ownership.mu.TryLock() {
				fixture.store.ownership.mu.Unlock()
				t.Fatal("final capture validation released ownership")
			}
			if capture.anchor == nil || fixture.store.rootBindingAnchors[fixture.scanRoot.id].approved != previousAnchor {
				t.Fatal("anchor was not prepared before the transaction or was installed before commit")
			}
		}
		return ctx.Err()
	}
	result, err := fixture.prepare(capture)
	if result != nil {
		defer result.Close()
	}
	if err != nil || result == nil || result.status != RootBindingVerified || result.row.revision != 9223372036854775807 ||
		result.row.root != fixture.scanRoot || result.opened == nil || capture.checks != 2 || capture.closes != 0 {
		t.Fatalf("recover original approval: result = %+v, error = %v, checks = %d", result, err, capture.checks)
	}
	if rootBindingScanAnchor(t, fixture.store, fixture.scanRoot.id) != capture.anchor {
		t.Fatal("successful recovery did not install the prepared root-specific anchor")
	}
	if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
		t.Fatal("scan recovery changed approval, audit, task or media rows")
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := capture.anchor.Stat("."); err != nil {
		t.Fatalf("closing scan evidence invalidated the installed Store anchor: %v", err)
	}
}

func TestRootBindingScanIntegrationUnverifiedStoragePreservesApprovalAndAnchor(t *testing.T) {
	for _, test := range []struct {
		name   string
		status RootBindingStatus
		change func(*testing.T, rootBindingScanFixture, *rootBindingScanTestCapture)
	}{
		{"unbound", RootBindingUnbound, func(t *testing.T, fixture rootBindingScanFixture, capture *rootBindingScanTestCapture) {
			if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE library_roots SET storage_binding = NULL, bound_at = NULL, bound_by = NULL WHERE id = $1`, fixture.scanRoot.id); err != nil {
				t.Fatal(err)
			}
			capture.snapshotHook = func() { t.Fatal("unbound scan reached filesystem identity capture") }
		}},
		{"identity mismatch", RootBindingMismatch, func(_ *testing.T, _ rootBindingScanFixture, capture *rootBindingScanTestCapture) {
			capture.snapshot.Anchor.FilesystemUUID = "ffffffffffffffffffffffffffffffff"
		}},
		{"incomplete snapshot", RootBindingUnavailable, func(_ *testing.T, _ rootBindingScanFixture, capture *rootBindingScanTestCapture) {
			capture.snapshot.RegisteredRoot.Handle = nil
		}},
		{"snapshot unavailable", RootBindingUnavailable, func(_ *testing.T, _ rootBindingScanFixture, capture *rootBindingScanTestCapture) {
			capture.snapshotErr = os.ErrPermission
		}},
		{"anchor unavailable", RootBindingUnavailable, func(_ *testing.T, _ rootBindingScanFixture, capture *rootBindingScanTestCapture) {
			capture.cloneErr = os.ErrPermission
		}},
		{"changed before admission", RootBindingUnavailable, func(_ *testing.T, _ rootBindingScanFixture, capture *rootBindingScanTestCapture) {
			capture.revalidate = func(context.Context, int) error { return ErrRootTopologyChanged }
		}},
		{"changed inside admission", RootBindingUnavailable, func(_ *testing.T, _ rootBindingScanFixture, capture *rootBindingScanTestCapture) {
			capture.revalidate = func(_ context.Context, count int) error {
				if count == 2 {
					return ErrRootTopologyChanged
				}
				return nil
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRootBindingScanFixture(t)
			capture := &rootBindingScanTestCapture{snapshot: fixture.snapshot.Clone()}
			test.change(t, fixture, capture)
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			anchor := rootBindingScanAnchor(t, fixture.store, fixture.scanRoot.id)
			result, err := fixture.prepare(capture)
			if result != nil {
				defer result.Close()
			}
			if err != nil || result == nil || result.status != test.status || result.opened != nil || result.capture != nil {
				t.Fatalf("unverified storage: result = %+v, error = %v; want %s", result, err, test.status)
			}
			if rootBindingScanAnchor(t, fixture.store, fixture.scanRoot.id) != anchor {
				t.Fatal("unverified observation changed its approved anchor")
			}
			if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
				t.Fatal("unverified observation changed catalog or approval rows")
			}
			if test.status != RootBindingUnbound && capture.closes != 1 {
				t.Fatalf("unverified observation retained its capture: %d closes", capture.closes)
			}
			if capture.anchor != nil {
				if _, err := capture.anchor.Stat("."); !errors.Is(err, os.ErrClosed) {
					t.Fatalf("rejected candidate remained open: %v", err)
				}
			}
		})
	}
}

func TestRootBindingScanIntegrationRechecksExactApprovalAndTaskAfterCapture(t *testing.T) {
	for _, test := range []struct {
		name      string
		statement string
		rootRow   bool
		change    func(rootBindingScanFixture)
		want      error
	}{
		{name: "revision", statement: `UPDATE library_roots SET binding_revision = binding_revision + 1 WHERE id = $1`, rootRow: true, want: ErrRootBindingConflict},
		{name: "approval time", statement: `UPDATE library_roots SET bound_at = bound_at + interval '1 second' WHERE id = $1`, rootRow: true, want: ErrRootBindingConflict},
		{name: "approval actor", statement: `UPDATE library_roots SET bound_by = 'changed-approver' WHERE id = $1`, rootRow: true, want: ErrRootBindingConflict},
		{name: "approval document", statement: `UPDATE library_roots SET storage_binding = jsonb_set(storage_binding, '{anchor,filesystem_uuid}', '"ffffffffffffffffffffffffffffffff"'::jsonb) WHERE id = $1`, rootRow: true, want: ErrRootBindingConflict},
		{name: "removed approval", statement: `UPDATE library_roots SET storage_binding = NULL, bound_at = NULL, bound_by = NULL WHERE id = $1`, rootRow: true, want: ErrRootBindingConflict},
		{name: "mapping", statement: `UPDATE library_roots SET relative_path = 'changed' WHERE id = $1`, rootRow: true, want: ErrUnavailable},
		{name: "persisted cancellation", statement: `UPDATE scan_jobs SET cancel_requested = true WHERE id = $1`, want: context.Canceled},
		{name: "terminal job", statement: `UPDATE scan_jobs SET status = 'Completed', finished_at = clock_timestamp() WHERE id = $1`, want: ErrTaskScanInactive},
		{name: "lost admission", change: func(fixture rootBindingScanFixture) {
			fixture.store.mu.Lock()
			delete(fixture.store.active, fixture.task.job.ID)
			fixture.store.mu.Unlock()
		}, want: ErrTaskScanInactive},
		{name: "cancelled context", change: func(fixture rootBindingScanFixture) { fixture.task.cancel() }, want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRootBindingScanFixture(t)
			anchor := rootBindingScanAnchor(t, fixture.store, fixture.scanRoot.id)
			var afterChange string
			capture := &rootBindingScanTestCapture{snapshot: fixture.snapshot, snapshotHook: func() {
				if test.statement != "" {
					id := fixture.task.job.ID
					if test.rootRow {
						id = fixture.scanRoot.id
					}
					if _, err := fixture.pool.Exec(fixture.ctx, test.statement, id); err != nil {
						t.Fatal(err)
					}
				}
				if test.change != nil {
					test.change(fixture)
				}
				afterChange = catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			}}
			result, err := fixture.prepare(capture)
			if result != nil {
				defer result.Close()
			}
			if !errors.Is(err, test.want) || result != nil {
				t.Fatalf("changed scan approval was admitted: result = %+v, error = %v; want %v", result, err, test.want)
			}
			if rootBindingScanAnchor(t, fixture.store, fixture.scanRoot.id) != anchor || capture.closes != 1 {
				t.Fatal("rejected scan changed its Store anchor or retained capture descriptors")
			}
			if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != afterChange {
				t.Fatal("rejected recovery added writes after the controlled row change")
			}
		})
	}
}

func TestRootBindingScanIntegrationRejectsCancellationInsideProtectedTransaction(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	anchor := rootBindingScanAnchor(t, fixture.store, fixture.scanRoot.id)
	before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
	capture := &rootBindingScanTestCapture{snapshot: fixture.snapshot, revalidate: func(ctx context.Context, count int) error {
		if count == 2 {
			fixture.task.cancel()
			// A successful adapter must not hide cancellation of the caller.
			return nil
		}
		return ctx.Err()
	}}
	result, err := fixture.prepare(capture)
	if result != nil {
		defer result.Close()
	}
	if !errors.Is(err, context.Canceled) || result != nil || capture.checks != 2 {
		t.Fatalf("cancelled protected recovery was admitted: result = %+v, error = %v", result, err)
	}
	if rootBindingScanAnchor(t, fixture.store, fixture.scanRoot.id) != anchor || fixture.store.ownership.lost.Load() {
		t.Fatal("caller cancellation changed the anchor or destroyed ownership")
	}
	if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
		t.Fatal("cancelled protected recovery mutated catalog rows")
	}
}

func TestRootBindingScanIntegrationRejectsUnownedJobsAndChangedInputMapping(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*rootBindingScanFixture)
		want   error
	}{
		{"different task pointer", func(fixture *rootBindingScanFixture) {
			copy := *fixture.task
			fixture.task = &copy
		}, ErrTaskScanInactive},
		{"cached queued status", func(fixture *rootBindingScanFixture) { fixture.task.job.Status = "Queued" }, ErrTaskScanInactive},
		{"cached cancellation", func(fixture *rootBindingScanFixture) { fixture.task.job.CancelRequested = true }, context.Canceled},
		{"different library", func(fixture *rootBindingScanFixture) { fixture.scanRoot.libraryID = "foreign-library" }, ErrInvalidInput},
		{"changed root path", func(fixture *rootBindingScanFixture) { fixture.scanRoot.path += "/changed" }, ErrRootBindingConflict},
		{"changed relative path", func(fixture *rootBindingScanFixture) { fixture.scanRoot.relativePath = "changed" }, ErrRootBindingConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRootBindingScanFixture(t)
			test.change(&fixture)
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			result, err := fixture.store.prepareRootBindingScanWithCapture(fixture.task, fixture.scanRoot,
				func(context.Context, libraryRoot) (rootBindingWriteCapture, error) {
					t.Fatal("invalid scan reached filesystem observation")
					return nil, nil
				})
			if result != nil {
				defer result.Close()
			}
			if result != nil || !errors.Is(err, test.want) {
				t.Fatalf("invalid scan was admitted: result = %+v, error = %v", result, err)
			}
			if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
				t.Fatal("invalid scan changed catalog rows")
			}
		})
	}
}

func TestRootBindingScanIntegrationKeepsLostOwnershipFatalAfterObservation(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	ownership := fixture.store.ownership
	defer func() { fixture.store.ownership = ownership }()
	before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
	anchor := rootBindingScanAnchor(t, fixture.store, fixture.scanRoot.id)
	result, err := fixture.store.prepareRootBindingScanWithCapture(fixture.task, fixture.scanRoot,
		func(context.Context, libraryRoot) (rootBindingWriteCapture, error) {
			fixture.store.mu.Lock()
			fixture.store.ownership = nil
			fixture.store.mu.Unlock()
			return nil, os.ErrPermission
		})
	if result != nil {
		defer result.Close()
	}
	fixture.store.ownership = ownership
	if result != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("lost ownership became a soft observation: result = %+v, error = %v", result, err)
	}
	if rootBindingScanAnchor(t, fixture.store, fixture.scanRoot.id) != anchor {
		t.Fatal("unowned recovery changed the anchor")
	}
	if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
		t.Fatal("unowned recovery changed catalog rows")
	}
}

func TestRootBindingScanIntegrationChecksTaskChildAndRunCancellation(t *testing.T) {
	for _, phase := range []string{"running", "run stopping before capture", "run stopping during capture", "child queued", "unexpected child"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newRootBindingScanFixture(t)
			runID, children := taskScanFixture(t, fixture.ctx, fixture.pool, fixture.library)
			if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE scan_jobs SET task_child_id = $2 WHERE id = $1`, fixture.task.job.ID, children[0]); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE task_run_children SET scan_job_id = $2, state = 'running', started_at = clock_timestamp() WHERE id = $1`, children[0], fixture.task.job.ID); err != nil {
				t.Fatal(err)
			}
			fixture.task.job.TaskChildID = children[0]
			stopRun := func() {
				if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE task_runs SET state = 'stopping', stop_reason = 'administrator', stop_requested_at = clock_timestamp() WHERE id = $1`, runID); err != nil {
					t.Fatal(err)
				}
			}
			capture := &rootBindingScanTestCapture{snapshot: fixture.snapshot}
			switch phase {
			case "run stopping before capture":
				stopRun()
			case "run stopping during capture":
				capture.snapshotHook = stopRun
			case "child queued":
				if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE task_run_children SET state = 'queued' WHERE id = $1`, children[0]); err != nil {
					t.Fatal(err)
				}
			case "unexpected child":
				fixture.task.job.TaskChildID = ""
			}
			anchor := rootBindingScanAnchor(t, fixture.store, fixture.scanRoot.id)
			result, err := fixture.prepare(capture)
			if result != nil {
				defer result.Close()
			}
			if phase == "running" {
				if err != nil || result == nil || result.status != RootBindingVerified {
					t.Fatalf("running task child could not recover storage: result = %+v, error = %v", result, err)
				}
				return
			}
			want := context.Canceled
			if phase == "unexpected child" {
				want = ErrTaskScanInactive
			}
			if result != nil || !errors.Is(err, want) || fixture.task.ctx.Err() != nil {
				t.Fatalf("inactive task child was admitted before context cancellation: result = %+v, error = %v", result, err)
			}
			if rootBindingScanAnchor(t, fixture.store, fixture.scanRoot.id) != anchor {
				t.Fatal("inactive task child replaced its anchor")
			}
		})
	}
}

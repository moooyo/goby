//go:build linux

package recovery

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/lifecycle"
)

// Model only the final control-state publication of an admitted restore. The
// fixture owns real authorization, audit receipts, and control-store CAS writes;
// no archive creation, restore, scanner, media probe, or encoder is executed.
func TestRecoveryManagerLatePlanCancellationClosesSuccessfulWorker(t *testing.T) {
	for _, test := range []struct {
		name                string
		publishBeforeCancel bool
	}{
		{"cancel_after_ready", true},
		{"cancel_before_ready", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newManagerIntegrationFixture(t)
			m, actor := f.manager, f.seed.actor
			ctx, cancel := context.WithTimeout(f.seed.ctx, 20*time.Second)
			t.Cleanup(cancel)
			op := admitLateCancellationRestore(t, ctx, f)
			atBoundary := make(chan struct{})
			allowPublication := make(chan struct{})
			allowReturn := make(chan struct{})
			workerResult := make(chan error, 1)
			var publicationOnce, returnOnce sync.Once
			releasePublication := func() { publicationOnce.Do(func() { close(allowPublication) }) }
			releaseReturn := func() { returnOnce.Do(func() { close(allowReturn) }) }
			var job *activeJob
			var done <-chan struct{}
			// Register after the fixture so both barriers open and this worker
			// finishes before fixture cleanup closes its stores and database pair.
			t.Cleanup(func() {
				if job != nil {
					job.cancel()
				}
				releasePublication()
				releaseReturn()
				if done != nil {
					select {
					case <-done:
					case <-time.After(10 * time.Second):
						t.Error("late-cancellation worker did not drain after releasing its barriers")
					}
				}
			})
			m.mu.Lock()
			m.startLocked(op.ID, func(work context.Context) {
				// planJob records this receipt before its last ready publication.
				if err := m.recordSystem(work, op, activity.ActionRestorePlanned, ""); err != nil {
					m.failJob(op.ID, err)
					workerResult <- err
					return
				}
				if !test.publishBeforeCancel {
					close(atBoundary)
					<-allowPublication
				}
				if err := m.updateOperation(op.ID, "ready", "ready", "", nil); err != nil {
					// Preserve Plan's existing error path if cancellation makes
					// publication refuse after the product fix.
					m.failJob(op.ID, err)
					workerResult <- err
					return
				}
				if test.publishBeforeCancel {
					close(atBoundary)
				}
				// Successful resource release can outlive the work context.
				// Cancellation must therefore settle even when work returns nil.
				<-allowReturn
				workerResult <- nil
			})
			job = m.jobs[op.ID]
			done = job.done
			m.mu.Unlock()
			select {
			case <-atBoundary:
			case err := <-workerResult:
				t.Fatalf("worker exited before the cancellation boundary: %v", err)
			case <-ctx.Done():
				t.Fatal("worker did not reach the bounded cancellation boundary")
			}
			view, err := m.Operation(ctx, actor, op.ID)
			if err != nil {
				t.Fatal("read the exact cancellable operation revision")
			}
			expectedState := "running"
			if test.publishBeforeCancel {
				expectedState = "ready"
			}
			m.mu.Lock()
			workerStillOwned := m.jobs[op.ID] == job
			m.mu.Unlock()
			if view.State != expectedState || !view.CanCancel || !workerStillOwned {
				t.Fatal("the operation did not retain its admitted worker at the selected boundary")
			}
			cancelled, err := m.Cancel(ctx, actor, op.ID, view.Revision)
			if err != nil || cancelled.CanApply || cancelled.CanCancel {
				t.Fatal("real cancellation did not commit and remove apply/cancel authority")
			}
			releasePublication()
			releaseReturn()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("cancelled worker did not finish its bounded normal cleanup")
			}
			if err := <-workerResult; err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("final publication returned an unexpected error: %v", err)
			}
			view, err = m.Operation(ctx, actor, op.ID)
			if err != nil {
				t.Fatal("read the operation after its actual job.done signal")
			}
			durable, _, err := readControl(ctx, f.runtime)
			if err != nil {
				t.Fatal("read the real persisted cancellation journal")
			}
			var retained *operationRecord
			for index := range durable.Operations {
				if durable.Operations[index].ID == op.ID {
					retained = &durable.Operations[index]
				}
			}
			if retained == nil || !retained.CancelAuthorized {
				t.Fatal("the committed cancellation authority was not durably retained")
			}
			status, err := m.Status(ctx, actor)
			if err != nil {
				t.Fatal("read recovery availability after worker cleanup")
			}
			m.mu.Lock()
			busy, activeWorker := m.busyLocked(""), m.jobs[op.ID] != nil
			m.mu.Unlock()
			t.Logf("settled state=%s phase=%s code=%q cancelAuthorized=%t busy=%t activeWorker=%t",
				view.State, view.Phase, view.ErrorCode, retained.CancelAuthorized, status.Busy, activeWorker)
			if view.State != "cancelled" || view.Phase != "finished" || view.ErrorCode != "operation_cancelled" ||
				retained.State != "cancelled" || retained.Phase != "finished" || retained.ErrorCode != "operation_cancelled" {
				t.Error("successful worker exit left an accepted cancellation without its cancelled terminal state")
			}
			if status.Busy || status.ActiveOperationId != "" || busy || activeWorker {
				t.Error("accepted cancellation left recovery admission busy after its worker exited")
			}
			if view.CanApply || view.CanCancel {
				t.Error("settled cancellation retained apply or cancel authority")
			}
			if _, err := m.Apply(ctx, actor, op.ID, ApplyRequest{Revision: view.Revision, GenerationRevision: view.GenerationRevision}); !errors.Is(err, ErrConflict) {
				t.Error("accepted cancellation allowed restore application")
			}
			// Cancel is idempotent once authorized: it must not grant another
			// cancellation or revive either capability, even on the broken state.
			replayed, err := m.Cancel(ctx, actor, op.ID, view.Revision)
			if err != nil || replayed.State != view.State || replayed.Revision != view.Revision || replayed.CanApply || replayed.CanCancel {
				t.Error("cancellation replay changed the settled operation or its capabilities")
			}
			var receipts int
			if err := f.seed.source.QueryRow(ctx, `SELECT count(*) FROM public.activity_entries
				WHERE action=$1 AND resource_id=$2`, string(activity.ActionRestoreCancelRequested), op.ID).Scan(&receipts); err != nil || receipts != 1 {
				t.Error("real cancellation did not retain exactly one authorization receipt")
			}
		})
	}
}

func TestRecoveryManagerLatePlanCancellationFailsClosedOnJournalConflict(t *testing.T) {
	f := newManagerIntegrationFixture(t)
	m, actor := f.manager, f.seed.actor
	ctx, cancel := context.WithTimeout(f.seed.ctx, 20*time.Second)
	t.Cleanup(cancel)
	op := admitLateCancellationRestore(t, ctx, f)
	atBoundary := make(chan struct{})
	allowReturn := make(chan struct{})
	workerResult := make(chan error, 1)
	var returnOnce sync.Once
	releaseReturn := func() { returnOnce.Do(func() { close(allowReturn) }) }
	var job *activeJob
	t.Cleanup(func() {
		if job != nil {
			job.cancel()
		}
		releaseReturn()
		if job != nil {
			select {
			case <-job.done:
			case <-time.After(10 * time.Second):
				t.Error("journal-conflict worker did not drain after releasing its barrier")
			}
		}
	})
	m.mu.Lock()
	m.startLocked(op.ID, func(work context.Context) {
		if err := m.recordSystem(work, op, activity.ActionRestorePlanned, ""); err != nil {
			m.failJob(op.ID, err)
			workerResult <- err
			return
		}
		if err := m.updateOperation(op.ID, "ready", "ready", "", nil); err != nil {
			m.failJob(op.ID, err)
			workerResult <- err
			return
		}
		close(atBoundary)
		<-allowReturn
		workerResult <- nil
	})
	job = m.jobs[op.ID]
	m.mu.Unlock()
	select {
	case <-atBoundary:
	case err := <-workerResult:
		t.Fatalf("worker exited before the journal-conflict boundary: %v", err)
	case <-ctx.Done():
		t.Fatal("worker did not reach the bounded journal-conflict boundary")
	}
	ready, err := m.Operation(ctx, actor, op.ID)
	if err != nil || ready.State != "ready" || !ready.CanCancel {
		t.Fatal("read the cancellable ready operation while its worker remains owned")
	}
	accepted, err := m.Cancel(ctx, actor, op.ID, ready.Revision)
	if err != nil || accepted.State != "ready" || accepted.CanApply || accepted.CanCancel {
		t.Fatal("commit real cancellation before the worker releases its resources")
	}
	_, before, err := readControl(ctx, f.runtime)
	if err != nil {
		t.Fatal("read the committed cancellation before advancing its store revision")
	}
	m.mu.Lock()
	owned := m.jobs[op.ID] == job
	managerDigest, managerRevision := m.control.Digest, m.control.Revision
	m.mu.Unlock()
	if !owned || managerDigest != before.Digest || managerRevision != before.Revision {
		t.Fatal("manager did not retain the exact expected journal and gated worker")
	}
	// A real CAS advances the record revision and digest even with an identical
	// payload. Keep the manager's expected snapshot unchanged so its terminal
	// publication encounters a genuine stale-write refusal without file damage.
	advanced, err := f.runtime.control.CompareAndSwap(ctx, before.Digest, before.Payload)
	if err != nil || advanced.Revision != before.Revision+1 || advanced.Digest == before.Digest ||
		!bytes.Equal(advanced.Payload, before.Payload) {
		t.Fatal("same-payload CAS did not create the exact journal-conflict boundary")
	}
	releaseReturn()
	select {
	case <-job.done:
	case <-ctx.Done():
		t.Fatal("journal-conflict worker did not finish its bounded normal cleanup")
	}
	if err := <-workerResult; err != nil {
		t.Fatalf("worker failed before terminal publication encountered the journal conflict: %v", err)
	}
	m.mu.Lock()
	fault, activeWorker := m.fault, m.jobs[op.ID] != nil
	managerDigest, managerRevision = m.control.Digest, m.control.Revision
	m.mu.Unlock()
	if !fault || activeWorker {
		t.Error("failed terminal publication did not fault admission and release its worker")
	}
	if managerDigest != before.Digest || managerRevision != before.Revision {
		t.Error("failed terminal publication adopted an unconfirmed journal snapshot")
	}
	durable, after, err := readControl(ctx, f.runtime)
	if err != nil || after.Digest != advanced.Digest || after.Revision != advanced.Revision ||
		!bytes.Equal(after.Payload, before.Payload) {
		t.Fatal("failed terminal publication changed the actual committed journal")
	}
	if len(durable.Operations) != 1 || durable.Operations[0].ID != op.ID ||
		durable.Operations[0].State != "ready" || !durable.Operations[0].CancelAuthorized {
		t.Fatal("journal conflict did not preserve the accepted cancellation for recovery")
	}
	view, err := m.Operation(ctx, actor, op.ID)
	if err != nil || view.State != accepted.State || view.Phase != accepted.Phase ||
		view.ErrorCode != accepted.ErrorCode || view.Revision != accepted.Revision || view.CanApply || view.CanCancel {
		t.Error("operation query exposed an uncommitted cancellation terminal state or revision")
	}
	page, err := m.ListOperations(ctx, actor, 0, 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].Id != op.ID ||
		page.Items[0].State != accepted.State || page.Items[0].Phase != accepted.Phase ||
		page.Items[0].ErrorCode != accepted.ErrorCode || page.Items[0].Revision != accepted.Revision ||
		page.Items[0].CanApply || page.Items[0].CanCancel {
		t.Error("operation listing exposed an uncommitted cancellation terminal state or revision")
	}
	status, err := m.Status(ctx, actor)
	if err != nil || status.Available || status.RestoreAvailable ||
		status.UnavailableReason != "recovery_required" || status.RestoreUnavailableReason != "recovery_required" ||
		!status.Busy || status.ActiveOperationId != op.ID {
		t.Error("journal-conflict status advertised recovery availability or lost the unresolved operation")
	}
	if _, err := m.Apply(ctx, actor, op.ID, ApplyRequest{Revision: accepted.Revision, GenerationRevision: accepted.GenerationRevision}); !errors.Is(err, ErrUnavailable) {
		t.Error("faulted terminal publication did not reject application as unavailable")
	}
	if _, err := m.Cancel(ctx, actor, op.ID, accepted.Revision); !errors.Is(err, ErrUnavailable) {
		t.Error("faulted terminal publication did not reject cancellation replay as unavailable")
	}
	_, final, err := readControl(ctx, f.runtime)
	if err != nil || final.Digest != advanced.Digest || final.Revision != advanced.Revision ||
		!bytes.Equal(final.Payload, before.Payload) {
		t.Error("faulted API requests retried or advanced the refused terminal publication")
	}
}

func admitLateCancellationRestore(t *testing.T, ctx context.Context, f *managerIntegrationFixture) operationRecord {
	t.Helper()
	m := f.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	op, err := m.newOperationLocked(f.seed.actor, recoveryEngineTestID(t), "restore",
		requestFingerprint(struct{ Kind, Generation string }{"restore", m.current.Digest}))
	if err != nil {
		t.Fatal("register the owned restore operation fixture")
	}
	// These are valid opaque control identities; no backup object or restored
	// target is needed to exercise cancellation at the worker publication edge.
	op.BackupID, op.GenerationID = recoveryEngineTestID(t), recoveryEngineTestID(t)
	op.TargetSlot = lifecycle.DatabaseRecovery
	if err := m.persistLocked(ctx); err != nil {
		t.Fatal("persist the restore identity before admission")
	}
	if err := m.grantLocked(ctx, f.seed.actor, op, activity.ActionRestoreRequested); err != nil {
		t.Fatal("commit the real administrator restore admission receipt")
	}
	op.Authorized = true
	m.changeLocked(op, "running", "staging", "")
	if err := m.persistLocked(ctx); err != nil {
		t.Fatal("persist the admitted pre-publication restore state")
	}
	return *op
}

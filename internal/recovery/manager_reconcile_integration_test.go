//go:build linux

package recovery

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/backupstore"
)

// These fixtures stop at durable boundaries in the real production protocol.
// PostgreSQL receipts, private journal CAS writes, complete age archives, and
// prepared files are real; only the process termination itself is simulated.
func TestRecoveryManagerPublicationCrashRecovery(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newManagerIntegrationFixture(t)
	ctx := f.seed.ctx
	passphrase := []byte("publication-crash-recovery-passphrase")
	defer clear(passphrase)
	retainedSource := recoveryEngineRetainedState(t, ctx, f.seed.source)
	var encrypted []byte
	run := func(name string, check func(*testing.T)) {
		if !t.Run(name, check) {
			t.Fatal("stop the owned publication fixture after a failed recovery boundary")
		}
	}

	run("create_completion_committed_before_publication", func(t *testing.T) {
		op, writer := beginManagerReceiptFixture(t, f, "create", true)
		manifest, err := f.manager.engine.Create(ctx, writer, passphrase)
		if err != nil {
			t.Fatalf("create genuine encrypted archive for publication boundary: %v", err)
		}
		releaseRecoveryEngineTestMemory()
		proof := prepareManagerReceiptFixture(t, f, op.ID, writer, &manifest)
		op, err = f.manager.operationCopy(op.ID)
		if err != nil || f.manager.recordSystem(ctx, op, activity.ActionBackupFinished, activity.StateCompleted) != nil {
			t.Fatal("commit exact backup completion receipt before publication")
		}
		if err := writer.Abort(ctx, backupstore.CodeInterrupted); err != nil {
			t.Fatal("preserve prepared bytes at simulated process termination")
		}
		f.reopen(t)
		if err := f.manager.Reconcile(ctx); err != nil {
			t.Fatalf("recover receipt-authorized prepared generation: %v", err)
		}
		completed := f.waitOperation(t, op.ID, "completed")
		object, err := f.manager.Backup(ctx, f.seed.actor, completed.BackupId)
		if err != nil || !object.Verified || object.SHA256 != proof.Digest || object.SizeBytes != decimal(uint64(proof.Size)) {
			t.Fatal("restart regenerated or replaced the exact prepared ciphertext")
		}
		f.auditID(t, activity.ActionBackupRequested, op.ID, "")
		f.auditID(t, activity.ActionBackupFinished, op.ID, activity.StateCompleted)
		reader, err := f.runtime.backups.Snapshot(ctx, op.BackupID)
		if err != nil {
			t.Fatal("open recovered generated ciphertext")
		}
		encrypted, err = io.ReadAll(reader)
		if closeErr := reader.Close(); err != nil || closeErr != nil {
			t.Fatal("read complete recovered generated ciphertext")
		}
		before, err := f.manager.Operation(ctx, f.seed.actor, op.ID)
		if err != nil || f.manager.Reconcile(ctx) != nil {
			t.Fatal("repeat reconciliation of completed generated archive")
		}
		after, err := f.manager.Operation(ctx, f.seed.actor, op.ID)
		if err != nil || before.Revision != after.Revision || before.State != after.State {
			t.Fatal("repeat reconciliation rewrote terminal publication history")
		}
	})

	run("import_admission_committed_before_control_cas", func(t *testing.T) {
		op, writer := beginManagerReceiptFixture(t, f, "import", false)
		if _, err := writer.Write(encrypted); err != nil {
			t.Fatal("stage exact imported age bytes")
		}
		proof := prepareManagerReceiptFixture(t, f, op.ID, writer, nil)
		commitManagerReceiptOnly(t, f, op.ID, activity.ActionBackupImported)
		if err := writer.Abort(ctx, backupstore.CodeInterrupted); err != nil {
			t.Fatal("preserve import at admission commit-before-CAS boundary")
		}
		f.reopen(t)
		if err := f.manager.Reconcile(ctx); err != nil {
			t.Fatalf("recover committed import admission: %v", err)
		}
		completed := f.waitOperation(t, op.ID, "completed")
		object, err := f.manager.Backup(ctx, f.seed.actor, completed.BackupId)
		if err != nil || object.Verified || object.Source != nil || object.SHA256 != proof.Digest {
			t.Fatal("recovered import changed ciphertext or claimed full archive verification")
		}
		f.auditID(t, activity.ActionBackupImported, op.ID, "")
		f.auditID(t, activity.ActionBackupFinished, op.ID, activity.StateCompleted)
	})

	run("import_published_before_completion_cas", func(t *testing.T) {
		op, writer := beginManagerReceiptFixture(t, f, "import", false)
		if _, err := writer.Write(encrypted); err != nil {
			t.Fatal("stage imported ciphertext for completed-publication boundary")
		}
		proof := prepareManagerReceiptFixture(t, f, op.ID, writer, nil)
		commitManagerReceiptOnly(t, f, op.ID, activity.ActionBackupImported)
		if err := f.manager.updateOperation(op.ID, "running", "publication", "", func(current *operationRecord) {
			current.Authorized = true
		}); err != nil {
			t.Fatal("persist admitted import before file publication")
		}
		op, err := f.manager.operationCopy(op.ID)
		if err != nil || f.manager.recordSystem(ctx, op, activity.ActionBackupFinished, activity.StateCompleted) != nil {
			t.Fatal("commit exact import completion receipt")
		}
		if _, err := writer.Publish(ctx, proof, nil); err != nil {
			t.Fatal("publish the already receipted imported object")
		}
		f.reopen(t)
		if err := f.manager.Reconcile(ctx); err != nil {
			t.Fatalf("recover publication that preceded journal completion: %v", err)
		}
		f.waitOperation(t, op.ID, "completed")
		object, err := f.runtime.backups.Get(ctx, op.BackupID)
		if err != nil || object.State != backupstore.StateReady || object.Digest != proof.Digest || object.Verified {
			t.Fatal("reconciliation failed to retain the exact published import")
		}
		f.auditID(t, activity.ActionBackupFinished, op.ID, activity.StateCompleted)
	})

	run("prepared_import_without_admission_is_not_published", func(t *testing.T) {
		op, writer := beginManagerReceiptFixture(t, f, "import", false)
		if _, err := writer.Write(encrypted); err != nil {
			t.Fatal("stage complete but unadmitted import fixture")
		}
		prepareManagerReceiptFixture(t, f, op.ID, writer, nil)
		if err := writer.Abort(ctx, backupstore.CodeInterrupted); err != nil {
			t.Fatal("retain unadmitted prepared fixture bytes")
		}
		f.reopen(t)
		if err := f.manager.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile an unadmitted prepared import: %v", err)
		}
		view := f.waitOperation(t, op.ID, "interrupted")
		if view.CanApply || view.CanCancel {
			t.Fatal("unadmitted import retained operational authority")
		}
		if _, err := f.runtime.backups.Snapshot(ctx, op.BackupID); !errors.Is(err, backupstore.ErrNotReady) {
			t.Fatal("unadmitted prepared bytes became downloadable after restart")
		}
		var count int
		if err := f.seed.source.QueryRow(ctx, `SELECT count(*) FROM public.activity_entries WHERE resource_id=$1
			AND action IN ($2,$3)`, op.ID, string(activity.ActionBackupImported), string(activity.ActionBackupFinished)).Scan(&count); err != nil || count != 0 {
			t.Fatal("reconciliation invented admission or completion for an unadmitted import")
		}
	})
	if after := recoveryEngineRetainedState(t, ctx, f.seed.source); after != retainedSource {
		t.Fatal("publication recovery changed retained source identity or catalog state")
	}
}

func TestRecoveryManagerCancellationReceiptSurvivesRestart(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newManagerIntegrationFixture(t)
	ctx, actor := f.seed.ctx, f.seed.actor
	passphrase := []byte("cancellation-receipt-passphrase")
	defer clear(passphrase)
	created, err := f.manager.Create(ctx, actor, CreateRequest{RequestId: recoveryEngineTestID(t), Passphrase: passphrase})
	if err != nil {
		t.Fatal("admit real archive for cancellation receipt recovery")
	}
	created = f.waitOperation(t, created.Id, "completed")
	releaseRecoveryEngineTestMemory()
	object, err := f.manager.Backup(ctx, actor, created.BackupId)
	if err != nil {
		t.Fatal("read generated archive digest")
	}
	plan, err := f.manager.Plan(ctx, actor, PlanRequest{RequestId: recoveryEngineTestID(t), BackupId: object.Id,
		SHA256: object.SHA256, Passphrase: passphrase, RestoreDefaults: true, GenerationRevision: "0"})
	if err != nil {
		t.Fatal("admit genuine restoration plan")
	}
	plan = f.waitOperation(t, plan.Id, "ready")
	releaseRecoveryEngineTestMemory()
	if !plan.CanCancel || !plan.CanApply {
		t.Fatal("fixture plan did not reach its cancellable ready boundary")
	}
	// Cancel commits its audit receipt before setting CancelAuthorized in the
	// protected journal. Stop at exactly that committed-but-unrecorded boundary.
	commitManagerReceiptOnly(t, f, plan.Id, activity.ActionRestoreCancelRequested)
	f.reopen(t)
	if err := f.manager.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile committed restore cancellation: %v", err)
	}
	cancelled, err := f.manager.Operation(ctx, actor, plan.Id)
	if err != nil || cancelled.State != "cancelled" || cancelled.CanApply || cancelled.CanCancel || cancelled.ErrorCode != "operation_cancelled" {
		t.Fatal("restart revived a plan whose cancellation receipt was committed")
	}
	if _, err := f.manager.Apply(ctx, actor, plan.Id, ApplyRequest{Revision: cancelled.Revision, GenerationRevision: "0"}); !errors.Is(err, ErrConflict) {
		t.Fatal("a durably cancelled plan could be applied after journal recovery")
	}
	f.auditID(t, activity.ActionRestoreCancelRequested, plan.Id, "")

	op, writer := beginManagerReceiptFixture(t, f, "create", true)
	commitManagerReceiptOnly(t, f, op.ID, activity.ActionBackupCancelRequested)
	if err := writer.Abort(ctx, backupstore.CodeInterrupted); err != nil {
		t.Fatal("close interrupted pre-encryption fixture")
	}
	f.reopen(t)
	if err := f.manager.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile committed creation cancellation: %v", err)
	}
	if cancelled, err := f.manager.Operation(ctx, actor, op.ID); err != nil || cancelled.State != "cancelled" || cancelled.CanCancel {
		t.Fatal("creation cancellation receipt was reclassified as an interrupted computation")
	}
	f.auditID(t, activity.ActionBackupCancelRequested, op.ID, "")
	f.auditID(t, activity.ActionBackupFinished, op.ID, activity.StateCancelled)
}

func beginManagerReceiptFixture(t *testing.T, f *managerIntegrationFixture, kind string, admitted bool) (operationRecord, *backupstore.Writer) {
	t.Helper()
	m := f.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthyLocked(); err != nil {
		t.Fatal("receipt fixture manager is unavailable")
	}
	fingerprint := requestFingerprint(struct{ Kind, Generation string }{kind, m.current.Digest})
	op, err := m.newOperationLocked(f.seed.actor, recoveryEngineTestID(t), kind, fingerprint)
	if err != nil {
		t.Fatal("create owned durable operation fixture")
	}
	storageKind := backupstore.KindGenerated
	if kind == "import" {
		storageKind = backupstore.KindImported
	}
	writer, err := m.runtime.backups.Begin(m.ctx, backupstore.BeginOptions{
		Kind: storageKind, CreatorID: f.seed.actor.User.ID, SessionID: f.seed.actor.SessionID,
	})
	if err != nil {
		t.Fatal("begin owned unpublished receipt fixture")
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = writer.Abort(closeCtx, backupstore.CodeInterrupted)
	})
	op.BackupID = writer.Metadata().ID
	if err := m.persistLocked(f.seed.ctx); err != nil {
		t.Fatal("persist operation identity before its receipt")
	}
	if admitted {
		if err := m.grantLocked(f.seed.ctx, f.seed.actor, op, admissionAction(kind)); err != nil {
			t.Fatal("commit production operation admission receipt")
		}
		op.Authorized = true
	}
	phase := "snapshot"
	if kind == "import" {
		phase = "upload"
	}
	m.changeLocked(op, "running", phase, "")
	if err := m.persistLocked(f.seed.ctx); err != nil {
		t.Fatal("persist pre-computation operation state")
	}
	return *op, writer
}

func prepareManagerReceiptFixture(t *testing.T, f *managerIntegrationFixture, id string, writer *backupstore.Writer, manifest *backupformat.Manifest) backupstore.Prepared {
	t.Helper()
	proof, err := writer.Prepare(f.seed.ctx)
	if err != nil {
		t.Fatal("durably prepare exact encrypted fixture bytes")
	}
	if err := f.manager.updateOperation(id, "running", "publication", "", func(op *operationRecord) {
		op.Digest, op.Size, op.Manifest = proof.Digest, proof.Size, manifest
		if manifest != nil {
			summary := Summary(*manifest)
			op.Source = summaryView(&summary)
			op.Source.ServerName = f.seed.configuration.ServerName
		}
	}); err != nil {
		t.Fatal("persist exact prepared byte proof")
	}
	return proof
}

func commitManagerReceiptOnly(t *testing.T, f *managerIntegrationFixture, id string, action activity.Action) {
	t.Helper()
	m := f.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	op := m.operationLocked(id)
	if op == nil {
		t.Fatal("receipt boundary has no durable operation")
	}
	before := bytes.Clone(m.control.Payload)
	if err := m.grantLocked(f.seed.ctx, f.seed.actor, op, action); err != nil {
		t.Fatal("commit receipt before the deliberately omitted journal CAS")
	}
	stored, err := m.runtime.control.Read(f.seed.ctx)
	if err != nil || !bytes.Equal(before, stored.Payload) {
		t.Fatal("receipt fixture advanced the journal past the intended crash boundary")
	}
}

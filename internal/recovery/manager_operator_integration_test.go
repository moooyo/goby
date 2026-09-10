//go:build linux

package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
)

func TestRecoveryOperatorUsesOnlyTrustedOfflineAuthority(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	var verificationPool *pgxpool.Pool
	t.Cleanup(func() {
		if verificationPool != nil {
			verificationPool.Close()
		}
	})
	f := newManagerIntegrationFixture(t)
	ctx, actor := f.seed.ctx, f.seed.actor
	passphrase := []byte("offline-operator-recovery-passphrase")
	defer clear(passphrase)
	passphraseBefore := bytes.Clone(passphrase)
	defer clear(passphraseBefore)
	if err := f.runtime.BindDatabase(ctx, f.seed.configuration, f.seed.source, f.lease); err != nil {
		t.Fatal("bind the original deployed database before taking its backup")
	}
	created, err := f.manager.Create(ctx, actor, CreateRequest{RequestId: recoveryEngineTestID(t), Passphrase: passphrase})
	if err != nil {
		t.Fatal("admit genuine archive generation before the original server becomes unavailable")
	}
	created = f.waitOperation(t, created.Id, "completed")
	releaseRecoveryEngineTestMemory()
	object, err := f.manager.Backup(ctx, actor, created.BackupId)
	if err != nil {
		t.Fatal("read generated object identity")
	}
	reader, err := f.runtime.backups.Snapshot(ctx, created.BackupId)
	if err != nil {
		t.Fatal("open actual generated encrypted archive")
	}
	encrypted, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err != nil || closeErr != nil {
		t.Fatal("retain actual encrypted bytes for offline import")
	}
	planInput := PlanRequest{RequestId: recoveryEngineTestID(t), BackupId: object.Id, SHA256: object.SHA256,
		Passphrase: passphrase, RestoreDefaults: true, GenerationRevision: "0"}
	for name, attempt := range map[string]func() error{
		"status": func() error { _, err := f.manager.OperatorStatus(ctx); return err },
		"import": func() error {
			_, err := f.manager.OperatorImport(ctx, recoveryEngineTestID(t), io.NopCloser(bytes.NewReader(encrypted)))
			return err
		},
		"plan": func() error { _, err := f.manager.OperatorPlan(ctx, planInput); return err },
		"apply": func() error {
			_, err := f.manager.OperatorApply(ctx, created.Id, ApplyRequest{Revision: created.Revision, GenerationRevision: "0"})
			return err
		},
		"rollback": func() error {
			_, err := f.manager.OperatorRollback(ctx, RollbackRequest{RequestId: recoveryEngineTestID(t), GenerationRevision: "0"})
			return err
		},
	} {
		if !t.Run("online_rejects_operator_"+name, func(t *testing.T) {
			if err := attempt(); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal("online manager accepted process-local offline authority")
			}
		}) {
			t.Fatal("stop after the online authority boundary failed")
		}
	}
	beforeFacts := recoveryEngineTestFacts(t, ctx, f.seed.source, f.seed.engine.options)
	beforeHistory := recoveryEngineRetainedState(t, ctx, f.seed.source)
	closeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	if err := f.manager.Close(closeCtx); err != nil {
		cancel()
		t.Fatal("stop online manager before taking offline ownership")
	}
	cancel()
	if err := f.lease.Close(); err != nil {
		t.Fatal("retire the original server deployment lease")
	}
	verificationPool = retireTransitionSourcePool(t, f)
	if err := f.runtime.Close(); err != nil {
		t.Fatal("release private stores before the explicit offline command")
	}
	offlineConfig := f.seed.configuration
	original, err := url.Parse(offlineConfig.DatabaseURL)
	if err != nil || original.Host == "" {
		t.Fatal("parse trusted deployment source identity")
	}
	original.Host = "127.0.0.1:1"
	offlineConfig.DatabaseURL = original.String()
	offlineConfig.APIKeyMasterKeyFile = filepath.Join(filepath.Dir(offlineConfig.APIKeyMasterKeyFile), "missing-offline-source-master.key")
	f.runtime, err = Open(ctx, offlineConfig)
	if err != nil {
		t.Fatal("acquire exclusive local runtime for offline recovery")
	}
	f.manager, err = NewOfflineManager(ctx, f.runtime, offlineConfig, "operator-integration")
	if err != nil || f.manager.pool != nil || f.manager.lease != nil || f.manager.vault != nil || f.manager.engine.pool != nil || f.manager.engine.vault != nil {
		t.Fatal("offline manager required original database or vault resources")
	}
	if _, err := os.Lstat(offlineConfig.APIKeyMasterKeyFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("offline construction opened or created the unavailable original master")
	}
	if err := f.manager.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile terminal native history without contacting its original database: %v", err)
	}
	for name, attempt := range map[string]func() error{
		"status": func() error { _, err := f.manager.Status(ctx, actor); return err },
		"create": func() error {
			_, err := f.manager.Create(ctx, actor, CreateRequest{RequestId: recoveryEngineTestID(t), Passphrase: passphrase})
			return err
		},
		"import": func() error {
			_, err := f.manager.Import(ctx, actor, recoveryEngineTestID(t), io.NopCloser(bytes.NewReader(encrypted)))
			return err
		},
		"plan":     func() error { _, err := f.manager.Plan(ctx, actor, planInput); return err },
		"download": func() error { _, err := f.manager.Download(ctx, actor, created.BackupId); return err },
		"delete": func() error {
			_, err := f.manager.Delete(ctx, actor, created.BackupId, DeleteRequest{RequestId: recoveryEngineTestID(t), SHA256: object.SHA256})
			return err
		},
	} {
		if !t.Run("offline_rejects_native_"+name, func(t *testing.T) {
			if err := attempt(); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal("a native principal crossed the offline capability boundary")
			}
		}) {
			t.Fatal("stop after the offline authority boundary failed")
		}
	}
	status, err := f.manager.OperatorStatus(ctx)
	if err != nil || !status.Available || !status.RestoreAvailable || status.GenerationRevision != "0" {
		t.Fatal("explicit offline authority cannot inspect its deployment")
	}
	importID := recoveryEngineTestID(t)
	imported, err := f.manager.OperatorImport(ctx, importID, io.NopCloser(bytes.NewReader(encrypted)))
	if err != nil {
		t.Fatalf("import actual archive through offline authority: %v", err)
	}
	imported = waitOperatorOperation(t, ctx, f.manager, imported.Id, "completed")
	op, err := f.manager.operationCopy(imported.Id)
	if err != nil || !op.Operator || !op.Authorized || op.ActorID != "" || op.CredentialID != "" {
		t.Fatal("offline import fabricated a native user credential or lost its local grant")
	}
	metadata, err := f.runtime.backups.Get(ctx, imported.BackupId)
	if err != nil || metadata.Verified || metadata.Digest != object.SHA256 {
		t.Fatal("offline import claimed verification before a real target rehearsal")
	}
	request := PlanRequest{RequestId: recoveryEngineTestID(t), BackupId: imported.BackupId, SHA256: metadata.Digest,
		Passphrase: passphrase, RestoreDefaults: true, GenerationRevision: "0"}
	plan, err := f.manager.OperatorPlan(ctx, request)
	if err != nil {
		t.Fatalf("admit offline restoration with an unreachable original endpoint: %v", err)
	}
	plan = waitOperatorOperation(t, ctx, f.manager, plan.Id, "ready")
	releaseRecoveryEngineTestMemory()
	if !plan.CanApply || !plan.CanCancel || !bytes.Equal(passphrase, passphraseBefore) {
		t.Fatal("offline ready plan lost authority or changed caller-owned passphrase bytes")
	}
	f.assertReadyTarget(t, plan, beforeHistory)
	if after := recoveryEngineTestFacts(t, ctx, f.seed.source, f.seed.engine.options); !reflect.DeepEqual(after, beforeFacts) {
		t.Fatal("offline import or planning wrote to the original database")
	}
	if metadata, err := f.runtime.backups.Get(ctx, imported.BackupId); err != nil || !metadata.Verified {
		t.Fatal("offline full archive restoration did not verify the imported object")
	}
	reopenOfflineManagerFixture(t, f, offlineConfig)
	if err := f.manager.Reconcile(ctx); err != nil {
		t.Fatalf("recover ready operator plan without its original database: %v", err)
	}
	replayed, err := f.manager.OperatorPlan(ctx, request)
	if err != nil || replayed.Id != plan.Id || replayed.State != "ready" || !replayed.CanApply {
		t.Fatal("offline restart forgot the exact ready plan or its request identity")
	}
	operations, err := f.manager.OperatorOperations(ctx, 0, 100)
	if err != nil {
		t.Fatal("inspect persisted operator history")
	}
	encoded, err := json.Marshal(operations)
	if err != nil || bytes.Contains(encoded, passphrase) || bytes.Contains(encoded, []byte(offlineConfig.DatabaseURL)) ||
		bytes.Contains(encoded, []byte(f.seed.activeKey.Token)) || bytes.Contains(encoded, []byte(offlineConfig.APIKeyMasterKeyFile)) {
		t.Fatal("offline operation projection exposed secrets or deployment paths")
	}
	applied, err := f.manager.OperatorApply(ctx, plan.Id, ApplyRequest{Revision: replayed.Revision, GenerationRevision: "0"})
	if err != nil || applied.State != "applying" || applied.CanCancel || applied.CanApply {
		t.Fatal("explicit operator apply did not persist its separate activation request")
	}
	select {
	case id := <-f.manager.SwitchRequests():
		if id != plan.Id {
			t.Fatal("operator apply queued a different generation operation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("operator apply did not signal its trusted startup owner")
	}
	current, err := f.runtime.lifecycle.Current()
	if err != nil || current.Revision != 0 {
		t.Fatal("operator admission activated a generation without the transition owner")
	}
	candidate, err := f.manager.PrepareSwitch(ctx, plan.Id)
	if err != nil || candidate.State.Revision != 1 {
		t.Fatalf("activate offline target without opening the unavailable original endpoint: %v", err)
	}
	registerTransitionCandidateCleanup(t, candidate)
	f.manager.mu.Lock()
	transition := f.manager.data.Transition
	canReturn := transition != nil && transition.CanReturn
	f.manager.mu.Unlock()
	if transition == nil || canReturn {
		t.Fatal("offline activation fabricated a retained original-source proof")
	}
	if err := f.runtime.ReturnUnaccepted(ctx); !errors.Is(err, ErrConflict) {
		t.Fatal("offline activation allowed an automatic return without source evidence")
	}
	closeTransitionManager(t, f.manager)
	f.manager = managerForTransitionCandidate(t, f, candidate)
	if err := f.manager.Reconcile(ctx); err != nil || f.manager.ValidateSwitch(ctx) != nil {
		t.Fatal("validate offline-prepared generation through the normal online startup owner")
	}
	stopStartup := initializeTransitionApplication(t, ctx, candidate.Pool, f.manager.cfg)
	defer stopStartup()
	if err := f.manager.AcceptSwitch(ctx); err != nil {
		t.Fatalf("accept offline-restored generation with its first target audit receipt: %v", err)
	}
	assertTransitionAppliedReceipt(t, ctx, candidate.Pool, plan.Id)
	assertTransitionCredentialsRevoked(t, ctx, candidate.Pool, f)
	if _, err := f.manager.OperatorStatus(ctx); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("accepted online generation retained process-local offline authority")
	}
	if after := recoveryEngineTestFacts(t, ctx, f.seed.source, f.seed.engine.options); !reflect.DeepEqual(after, beforeFacts) {
		t.Fatal("offline activation or acceptance wrote to the original database")
	}
}

func reopenOfflineManagerFixture(t *testing.T, f *managerIntegrationFixture, cfg config.Config) {
	t.Helper()
	closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := f.manager.Close(closeCtx); err != nil || f.runtime.Close() != nil {
		t.Fatal("close offline manager and its private stores")
	}
	runtime, err := Open(f.seed.ctx, cfg)
	if err != nil {
		t.Fatal("reopen private offline control state")
	}
	f.runtime = runtime
	f.manager, err = NewOfflineManager(f.seed.ctx, runtime, cfg, "operator-integration")
	if err != nil {
		t.Fatal("reopen explicit offline authority")
	}
}

func waitOperatorOperation(t *testing.T, parent context.Context, manager *Manager, id, expected string) OperationView {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		view, err := manager.OperatorOperation(ctx, id)
		if err != nil {
			t.Fatalf("observe offline recovery operation: %v", err)
		}
		if view.State == expected {
			manager.mu.Lock()
			job := manager.jobs[id]
			manager.mu.Unlock()
			if job != nil {
				select {
				case <-job.done:
				case <-ctx.Done():
					t.Fatal("operator worker did not release its target resources")
				}
			}
			return view
		}
		if terminalOperation(view.State) {
			t.Fatalf("operator job ended as %s with safe code %s; expected %s", view.State, view.ErrorCode, expected)
		}
		select {
		case <-ctx.Done():
			t.Fatal("operator job did not reach bounded completion")
		case <-ticker.C:
		}
	}
}

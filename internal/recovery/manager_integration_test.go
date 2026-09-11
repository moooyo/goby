//go:build linux

package recovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/recoverydb"
)

type managerIntegrationFixture struct {
	seed    *engineRecoveryFixture
	runtime *Runtime
	manager *Manager
	lease   *database.Lease
}

// This scenario exclusively owns a dedicated, initially empty database pair.
// Only named objects created by the embedded migrations are removed. The
// external operator remains responsible for creating and dropping the pair.
func TestRecoveryManagerNativeWorkflow(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newManagerIntegrationFixture(t)
	ctx, actor := f.seed.ctx, f.seed.actor
	passphrase := []byte("manager-integration-secret-passphrase")
	defer clear(passphrase)
	beforePassphrase := bytes.Clone(passphrase)
	defer clear(beforePassphrase)
	var created, imported OperationView
	var createdBackup, importedBackup BackupView
	var encrypted []byte
	var planRequest PlanRequest
	var readyPlan OperationView
	var sourceHistory string
	run := func(name string, check func(*testing.T)) {
		if !t.Run(name, check) {
			t.Fatal("stop using the dedicated database pair after a failed workflow step")
		}
	}

	run("authorization_create_and_request_replay", func(t *testing.T) {
		viewer, err := f.seed.identities.CreateUser(ctx, "Recovery viewer", "recovery-viewer-password", false)
		if err != nil {
			t.Fatal("create non-administrator fixture")
		}
		viewerLogin, err := f.seed.identities.Authenticate(ctx, viewer.Name, "recovery-viewer-password", identity.Client{}, "emby")
		if err != nil {
			t.Fatal("issue non-administrator fixture credential")
		}
		viewerActor, err := f.seed.identities.Resolve(ctx, viewerLogin.Token, "emby")
		if err != nil {
			t.Fatal("resolve non-administrator fixture credential")
		}
		revokedLogin, err := f.seed.identities.Authenticate(ctx, actor.User.Name, "recovery-administrator-password", identity.Client{}, "admin")
		if err != nil {
			t.Fatal("issue administrator revocation fixture")
		}
		revokedActor, err := f.seed.identities.Resolve(ctx, revokedLogin.Token, "admin")
		if err != nil || f.seed.identities.Revoke(ctx, revokedLogin.Token) != nil {
			t.Fatal("revoke administrator fixture credential")
		}
		keyActor, err := f.seed.identities.ResolveEmby(ctx, f.seed.activeKey.Token)
		if err != nil {
			t.Fatal("resolve userless application credential fixture")
		}
		for _, denied := range []identity.Principal{viewerActor, revokedActor, keyActor} {
			if _, err := f.manager.Create(ctx, denied, CreateRequest{RequestId: recoveryEngineTestID(t), Passphrase: passphrase}); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal("native recovery accepted an unauthorized or revoked credential")
			}
		}
		if status := f.runtime.backups.Status(); status.Objects != 0 || status.Writers != 0 {
			t.Fatal("denied admissions created storage artifacts")
		}
		sourceHistory = recoveryEngineRetainedState(t, ctx, f.seed.source)
		request := CreateRequest{RequestId: recoveryEngineTestID(t), Passphrase: passphrase}
		created, err = f.manager.Create(ctx, actor, request)
		if err != nil || created.Kind != "create" || created.BackupId == "" {
			t.Fatalf("admit native backup creation: %v", err)
		}
		replayed, err := f.manager.Create(ctx, actor, request)
		if err != nil || replayed.Id != created.Id || replayed.BackupId != created.BackupId {
			t.Fatal("same request ID created a second operation or artifact")
		}
		conflictingBody := io.NopCloser(strings.NewReader("age-encryption.org/v1\n"))
		defer conflictingBody.Close()
		if _, err := f.manager.Import(ctx, actor, request.RequestId, conflictingBody); !errors.Is(err, ErrConflict) {
			t.Fatal("a request ID was reused for a conflicting operation kind")
		}
		created = f.waitOperation(t, created.Id, "completed")
		releaseRecoveryEngineTestMemory()
		createdBackup, err = f.manager.Backup(ctx, actor, created.BackupId)
		if err != nil || createdBackup.State != "ready" || !createdBackup.Verified || createdBackup.Source == nil {
			t.Fatal("completed native backup was not durably published")
		}
		if replayed, err := f.manager.Create(ctx, actor, request); err != nil || replayed.Id != created.Id || replayed.BackupId != created.BackupId {
			t.Fatal("completed request replay created new ciphertext")
		}
		if status := f.runtime.backups.Status(); status.Objects != 1 || status.Writers != 0 || status.ScratchFiles != 0 {
			t.Fatal("creation replay leaked or duplicated storage resources")
		}
		if !bytes.Equal(passphrase, beforePassphrase) {
			t.Fatal("asynchronous creation changed caller-owned passphrase bytes")
		}
		requested := f.auditID(t, activity.ActionBackupRequested, created.Id, "")
		finished := f.auditID(t, activity.ActionBackupFinished, created.Id, activity.StateCompleted)
		if requested >= finished {
			t.Fatal("backup completion was recorded before durable admission")
		}
		f.assertNoSecrets(t, passphrase)
	})

	run("active_create_cancellation", func(t *testing.T) {
		gate, err := f.seed.source.Begin(ctx)
		if err != nil {
			t.Fatal("begin owned snapshot cancellation gate")
		}
		defer gate.Rollback(ctx)
		if _, err := gate.Exec(ctx, `LOCK TABLE public.items IN ACCESS EXCLUSIVE MODE`); err != nil {
			t.Fatal("hold owned snapshot cancellation gate")
		}
		operation, err := f.manager.Create(ctx, actor, CreateRequest{RequestId: recoveryEngineTestID(t), Passphrase: passphrase})
		if err != nil {
			t.Fatal("admit cancellable backup")
		}
		operation, err = f.manager.Operation(ctx, actor, operation.Id)
		if err != nil || !operation.CanCancel {
			t.Fatal("active backup did not offer cancellation")
		}
		if _, err := f.manager.Cancel(ctx, actor, operation.Id, operation.Revision); err != nil {
			t.Fatal("authorize active backup cancellation")
		}
		if err := gate.Rollback(ctx); err != nil {
			t.Fatal("release owned snapshot cancellation gate")
		}
		cancelled := f.waitOperation(t, operation.Id, "cancelled")
		if cancelled.CanApply || cancelled.CanCancel || cancelled.ErrorCode != "operation_cancelled" {
			t.Fatal("cancelled backup retained active capabilities")
		}
		f.auditID(t, activity.ActionBackupCancelRequested, operation.Id, "")
		f.auditID(t, activity.ActionBackupFinished, operation.Id, activity.StateCancelled)
	})

	run("invalid_oversized_and_disconnected_imports", func(t *testing.T) {
		for _, test := range []struct {
			name string
			body io.ReadCloser
		}{
			{name: "invalid magic", body: io.NopCloser(strings.NewReader("invalid archive"))},
			{name: "over byte limit", body: io.NopCloser(io.MultiReader(strings.NewReader("age-encryption.org/v1\n"), io.LimitReader(managerZeroReader{}, f.seed.configuration.Recovery.Backups.MaxObjectBytes)))},
			{name: "disconnected body", body: &managerDisconnectedReader{}},
		} {
			t.Run(test.name, func(t *testing.T) {
				defer test.body.Close()
				requestID := recoveryEngineTestID(t)
				if _, err := f.manager.Import(ctx, actor, requestID, test.body); err == nil {
					t.Fatal("incomplete or oversized import reported success")
				}
				operation := f.operationByRequest(t, requestID)
				if operation.State == "completed" || !terminalOperation(operation.State) {
					t.Fatal("failed import was left completed or indefinitely running")
				}
				if object, err := f.runtime.backups.Get(ctx, operation.BackupId); err != nil || object.State == backupstore.StateReady || object.Verified {
					t.Fatal("failed import published a downloadable or verified object")
				}
			})
		}
		requestCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		body := &managerBlockingReader{reading: make(chan struct{}), closed: make(chan struct{})}
		requestID := recoveryEngineTestID(t)
		result := make(chan error, 1)
		go func() {
			_, err := f.manager.Import(requestCtx, actor, requestID, body)
			result <- err
		}()
		select {
		case <-body.reading:
		case <-time.After(10 * time.Second):
			t.Fatal("import did not begin reading the owned blocking body")
		}
		cancel()
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("cancelled upload reported successful publication")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("request cancellation did not close and unblock its upload body")
		}
		operation := f.operationByRequest(t, requestID)
		if operation.State != "interrupted" || operation.CanApply {
			t.Fatal("lost upload was not retained as interrupted history")
		}
	})

	run("real_import_is_unverified_until_plan", func(t *testing.T) {
		download, err := f.manager.Download(ctx, actor, created.BackupId)
		if err != nil {
			t.Fatal("open authenticated native download")
		}
		encrypted, err = io.ReadAll(download)
		if closeErr := download.Close(); err != nil || closeErr != nil || !bytes.HasPrefix(encrypted, []byte("age-encryption.org/v1\n")) {
			t.Fatal("download did not retain a complete real age archive")
		}
		digest := sha256.Sum256(encrypted)
		if hex.EncodeToString(digest[:]) != createdBackup.SHA256 {
			t.Fatal("download bytes do not match the published digest")
		}
		requestID := recoveryEngineTestID(t)
		body := io.NopCloser(bytes.NewReader(encrypted))
		defer body.Close()
		imported, err = f.manager.Import(ctx, actor, requestID, body)
		if err != nil {
			t.Fatalf("import actual generated archive bytes: %v", err)
		}
		imported = f.waitOperation(t, imported.Id, "completed")
		importedBackup, err = f.manager.Backup(ctx, actor, imported.BackupId)
		if err != nil || importedBackup.SHA256 != createdBackup.SHA256 || importedBackup.State != "ready" || importedBackup.Verified || importedBackup.Source != nil {
			t.Fatal("magic and digest checks improperly marked an import as verified")
		}
		replayed, err := f.manager.Import(ctx, actor, requestID, io.NopCloser(bytes.NewReader(encrypted)))
		if err != nil || replayed.Id != imported.Id || replayed.BackupId != imported.BackupId {
			t.Fatal("import request replay created another object")
		}
		f.auditID(t, activity.ActionBackupImported, imported.Id, "")
		f.auditID(t, activity.ActionBackupFinished, imported.Id, activity.StateCompleted)
	})

	run("delete_digest_audit_and_pinned_download", func(t *testing.T) {
		if _, err := f.manager.Delete(ctx, actor, created.BackupId, DeleteRequest{RequestId: recoveryEngineTestID(t), SHA256: strings.Repeat("0", 64)}); !errors.Is(err, ErrConflict) {
			t.Fatal("delete accepted a stale object digest")
		}
		download, err := f.manager.Download(ctx, actor, created.BackupId)
		if err != nil {
			t.Fatal("pin generated backup with an active download")
		}
		defer download.Close()
		deleted, err := f.manager.Delete(ctx, actor, created.BackupId, DeleteRequest{RequestId: recoveryEngineTestID(t), SHA256: createdBackup.SHA256})
		if err != nil {
			t.Fatal("admit digest-bound backup deletion")
		}
		requested := f.auditID(t, activity.ActionBackupDeleteRequested, deleted.Id, "")
		if object, err := f.runtime.backups.Get(ctx, created.BackupId); err != nil || object.State != backupstore.StateReady {
			t.Fatal("delete removed bytes while a download was active")
		}
		if view, err := f.manager.Operation(ctx, actor, deleted.Id); err != nil || terminalOperation(view.State) {
			t.Fatal("delete completed before its pinned download closed")
		}
		var completed int
		if err := f.seed.source.QueryRow(ctx, `SELECT count(*) FROM public.activity_entries WHERE action=$1 AND resource_id=$2`,
			string(activity.ActionBackupDeleted), deleted.Id).Scan(&completed); err != nil || completed != 0 {
			t.Fatal("delete completion was audited before bytes could be removed")
		}
		var prefix [21]byte
		if _, err := io.ReadFull(download, prefix[:]); err != nil {
			t.Fatal("delete invalidated a still-authorized download")
		}
		if err := download.Close(); err != nil {
			t.Fatal("release active download")
		}
		f.waitOperation(t, deleted.Id, "completed")
		if _, err := f.runtime.backups.Get(ctx, created.BackupId); !errors.Is(err, backupstore.ErrNotFound) {
			t.Fatal("completed delete retained the encrypted object")
		}
		if completed := f.auditID(t, activity.ActionBackupDeleted, deleted.Id, ""); completed <= requested {
			t.Fatal("delete audit receipts are not in admission and completion order")
		}
	})

	run("close_and_reopen_preserves_history", func(t *testing.T) {
		before, err := f.manager.ListOperations(ctx, actor, 0, 100)
		if err != nil {
			t.Fatal("read durable operation history before restart")
		}
		f.reopen(t)
		after, err := f.manager.ListOperations(ctx, actor, 0, 100)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("restart lost or rewrote completed operation history")
		}
		if object, err := f.manager.Backup(ctx, actor, imported.BackupId); err != nil || object.Verified || object.SHA256 != importedBackup.SHA256 {
			t.Fatal("restart lost the unverified imported archive")
		}
		replayed, err := f.manager.Create(ctx, actor, CreateRequest{RequestId: created.RequestId, Passphrase: passphrase})
		if err != nil || replayed.Id != created.Id || replayed.BackupId != created.BackupId {
			t.Fatal("restart forgot idempotency history for an already deleted artifact")
		}
	})

	run("real_plan_stamps_local_target_and_preserves_source", func(t *testing.T) {
		status, err := f.manager.Status(ctx, actor)
		if err != nil || !status.RestoreAvailable || status.GenerationRevision != "0" || status.Rollback.MustReplace {
			t.Fatal("initial recovery slot was not available for planning")
		}
		planRequest = PlanRequest{RequestId: recoveryEngineTestID(t), BackupId: imported.BackupId,
			SHA256: importedBackup.SHA256, Passphrase: passphrase, RestoreDefaults: true, GenerationRevision: status.GenerationRevision}
		wrong := planRequest
		wrong.GenerationRevision = "1"
		if _, err := f.manager.Plan(ctx, actor, wrong); !errors.Is(err, ErrConflict) {
			t.Fatal("plan ignored a stale deployment generation")
		}
		readyPlan, err = f.manager.Plan(ctx, actor, planRequest)
		if err != nil {
			t.Fatalf("admit real target restoration plan: %v", err)
		}
		replayed, err := f.manager.Plan(ctx, actor, planRequest)
		if err != nil || replayed.Id != readyPlan.Id {
			t.Fatal("plan replay admitted a second target operation")
		}
		wrong = planRequest
		wrong.RestoreDefaults = false
		if _, err := f.manager.Plan(ctx, actor, wrong); !errors.Is(err, ErrConflict) {
			t.Fatal("plan reused a request ID with conflicting restore policy")
		}
		readyPlan = f.waitOperation(t, readyPlan.Id, "ready")
		releaseRecoveryEngineTestMemory()
		if !readyPlan.CanApply || !readyPlan.CanCancel || !bytes.Equal(passphrase, beforePassphrase) {
			t.Fatal("ready plan lost capabilities or changed its caller-owned secret")
		}
		f.assertReadyTarget(t, readyPlan, sourceHistory)
		if object, err := f.manager.Backup(ctx, actor, imported.BackupId); err != nil || !object.Verified || object.Source == nil {
			t.Fatal("successful raw restore and vault witness did not verify the imported archive")
		}
		f.auditID(t, activity.ActionRestoreRequested, readyPlan.Id, "")
		f.auditID(t, activity.ActionRestorePlanned, readyPlan.Id, "")
		if _, err := f.manager.Apply(ctx, actor, readyPlan.Id, ApplyRequest{Revision: "0", GenerationRevision: "0"}); !errors.Is(err, ErrConflict) {
			t.Fatal("apply ignored a stale plan revision")
		}
		if _, err := f.manager.Apply(ctx, actor, readyPlan.Id, ApplyRequest{Revision: readyPlan.Revision, GenerationRevision: "1"}); !errors.Is(err, ErrConflict) {
			t.Fatal("apply ignored a stale deployment revision")
		}
		f.assertNoSecrets(t, passphrase)
	})

	run("ready_plan_cancellation_and_replace_guard", func(t *testing.T) {
		cancelled, err := f.manager.Cancel(ctx, actor, readyPlan.Id, readyPlan.Revision)
		if err != nil || cancelled.State != "cancelled" || cancelled.CanApply || cancelled.CanCancel {
			t.Fatal("ready plan cancellation did not retire application authority")
		}
		if _, err := f.manager.Apply(ctx, actor, readyPlan.Id, ApplyRequest{Revision: cancelled.Revision, GenerationRevision: "0"}); !errors.Is(err, ErrConflict) {
			t.Fatal("cancelled restore plan could still be applied")
		}
		if _, err := f.manager.Rollback(ctx, actor, RollbackRequest{RequestId: recoveryEngineTestID(t), GenerationRevision: "0"}); !errors.Is(err, ErrConflict) {
			t.Fatal("an unactivated staged candidate was accepted as a retained rollback")
		}
		status, err := f.manager.Status(ctx, actor)
		if err != nil || !status.Rollback.MustReplace || status.Rollback.Available {
			t.Fatal("staged target replacement guard was not exposed")
		}
		replacement := planRequest
		replacement.RequestId = recoveryEngineTestID(t)
		if _, err := f.manager.Plan(ctx, actor, replacement); !errors.Is(err, ErrConflict) {
			t.Fatal("a populated staged target was replaced without explicit policy")
		}
		replacement.ReplaceRollback = true
		operation, err := f.manager.Plan(ctx, actor, replacement)
		if err != nil {
			t.Fatal("admit explicit replacement of the locally retained candidate")
		}
		operation = f.waitOperation(t, operation.Id, "ready")
		releaseRecoveryEngineTestMemory()
		f.assertReadyTarget(t, operation, sourceHistory)
		if _, err := f.manager.Cancel(ctx, actor, operation.Id, operation.Revision); err != nil {
			t.Fatal("retire final ready fixture plan")
		}
	})
}

func newManagerIntegrationFixture(t *testing.T) *managerIntegrationFixture {
	t.Helper()
	sourceURL := os.Getenv("GOBY_TEST_BACKUP_SOURCE_DATABASE_URL")
	targetURL := os.Getenv("GOBY_TEST_BACKUP_TARGET_DATABASE_URL")
	if sourceURL == "" || targetURL == "" {
		t.Skip("two disposable backup test databases are required")
	}
	if os.Getenv("GOBY_TEST_BACKUP_DISPOSABLE_DATABASES") != "1" {
		t.Fatal("explicit disposable database marker is required")
	}
	sourceConfig, err := pgxpool.ParseConfig(sourceURL)
	if err != nil {
		t.Fatal("parse explicit source database configuration")
	}
	targetConfig, err := pgxpool.ParseConfig(targetURL)
	if err != nil {
		t.Fatal("parse explicit target database configuration")
	}
	if sourceConfig.ConnConfig.Database == targetConfig.ConnConfig.Database || sourceConfig.ConnConfig.User == targetConfig.ConnConfig.User ||
		!strings.HasPrefix(sourceConfig.ConnConfig.Database, "goby_backup_") || !strings.HasPrefix(targetConfig.ConnConfig.Database, "goby_backup_") {
		t.Fatal("manager tests require distinct goby_backup_ databases and roles")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	t.Cleanup(cancel)
	root := t.TempDir()
	mediaRoot := filepath.Join(root, "media")
	if err := os.Mkdir(mediaRoot, 0700); err != nil {
		t.Fatal("create approved manager fixture media root")
	}
	cfg := config.Config{
		DatabaseURL: sourceURL, ServerName: "Manager source deployment",
		APIKeyMasterKeyFile: filepath.Join(root, "application-master.key"),
		MediaRoots:          []string{mediaRoot}, FFprobePath: "/unused-manager-test-ffprobe", FFmpegPath: "/unused-manager-test-ffmpeg",
		Transcoding: config.TranscodingConfig{MaxBitrate: 8_000_000, MaxWidth: 1280, MaxHeight: 720, MaxAudioChannels: 2},
		Recovery: config.RecoveryConfig{
			Directory: filepath.Join(root, "lifecycle"), OperationsDirectory: filepath.Join(root, "operations"), DatabaseURL: targetURL,
			Backups:    backupstore.Config{Directory: filepath.Join(root, "backups"), MaxObjectBytes: 8 << 20, MaxTotalBytes: 64 << 20, MaxObjects: 32, MinFreeBytes: 1 << 20},
			PGDumpPath: os.Getenv("GOBY_TEST_PG_DUMP"), PGRestorePath: os.Getenv("GOBY_TEST_PG_RESTORE"), OperationTimeout: 3 * time.Minute,
		},
	}
	if cfg.Recovery.PGDumpPath == "" || cfg.Recovery.PGRestorePath == "" {
		t.Fatal("explicit PostgreSQL 17 executable paths are required")
	}
	fixture := &managerIntegrationFixture{seed: &engineRecoveryFixture{ctx: ctx, schema: "public", configuration: cfg}}
	fixture.runtime, err = Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open private lifecycle and recovery stores: %v", err)
	}
	t.Cleanup(func() {
		if fixture.manager != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := fixture.manager.Close(closeCtx); err != nil {
				t.Error("drain manager fixture jobs")
			}
		}
		if fixture.runtime != nil {
			if err := fixture.runtime.Close(); err != nil {
				t.Error("close private manager fixture stores")
			}
		}
	})
	for index, poolConfig := range []*pgxpool.Config{sourceConfig, targetConfig} {
		poolConfig.MaxConns = 4
		if poolConfig.ConnConfig.RuntimeParams == nil {
			poolConfig.ConnConfig.RuntimeParams = make(map[string]string)
		}
		poolConfig.ConnConfig.RuntimeParams["search_path"] = "public"
		poolConfig.ConnConfig.RuntimeParams["timezone"] = "UTC"
		pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
		if err != nil {
			t.Fatal("open dedicated manager fixture database")
		}
		t.Cleanup(pool.Close)
		var unsafe, empty bool
		if err := pool.QueryRow(ctx, `SELECT r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls,
			NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
				WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema'
				UNION ALL SELECT 1 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace
				WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema'
				UNION ALL SELECT 1 FROM pg_catalog.pg_type y JOIN pg_catalog.pg_namespace n ON n.oid=y.typnamespace
				WHERE n.nspname !~ '^pg_' AND n.nspname<>'information_schema')
			FROM pg_catalog.pg_roles r WHERE r.rolname=current_user`).Scan(&unsafe, &empty); err != nil || unsafe || !empty {
			t.Fatal("manager fixture requires an empty database owned by a low-privilege role")
		}
		if index == 0 {
			fixture.seed.source = pool
		} else {
			fixture.seed.target = pool
		}
	}
	// This registration follows both empty-database checks. Cleanup cannot run
	// against a pre-existing application schema or an unvalidated database.
	t.Cleanup(func() {
		if fixture.manager != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_ = fixture.manager.Close(closeCtx)
			cancel()
		}
		if fixture.lease != nil {
			_ = fixture.lease.Close()
		}
		cleanupManagerPublicObjects(t, fixture.seed.target)
		cleanupManagerPublicObjects(t, fixture.seed.source)
	})
	fixture.lease, err = database.AcquireLease(ctx, fixture.seed.source)
	if err != nil {
		t.Fatal("acquire source deployment lifetime lease")
	}
	if err := database.Migrate(ctx, fixture.seed.source); err != nil {
		t.Fatal("create only the compiled public fixture objects")
	}
	fixture.seed.vault = identity.NewApplicationKeyVault(cfg.APIKeyMasterKeyFile)
	fixture.seed.identities = identity.NewWithApplicationKeyVault(fixture.seed.source, fixture.seed.vault)
	fixture.seed.seed(t)
	fixture.manager, err = NewManager(ctx, fixture.runtime, cfg, fixture.seed.source, fixture.lease, fixture.seed.vault, "manager-integration")
	if err != nil {
		t.Fatalf("construct native recovery manager: %v", err)
	}
	fixture.seed.engine = fixture.manager.engine
	return fixture
}

func (f *managerIntegrationFixture) reopen(t *testing.T) {
	t.Helper()
	closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := f.manager.Close(closeCtx); err != nil {
		t.Fatal("drain manager before reopening control history")
	}
	if err := f.runtime.Close(); err != nil {
		t.Fatal("close private stores before reopening them")
	}
	runtime, err := Open(f.seed.ctx, f.seed.configuration)
	if err != nil {
		t.Fatal("reopen private lifecycle and operation journals")
	}
	f.runtime = runtime
	f.manager, err = NewManager(f.seed.ctx, runtime, f.seed.configuration, f.seed.source, f.lease, f.seed.vault, "manager-integration")
	if err != nil {
		t.Fatalf("reopen manager against the same active generation: %v", err)
	}
	f.seed.engine = f.manager.engine
}

func (f *managerIntegrationFixture) waitOperation(t *testing.T, id, expected string) OperationView {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.seed.ctx, 2*time.Minute)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		view, err := f.manager.Operation(ctx, f.seed.actor, id)
		if err != nil {
			t.Fatalf("observe native recovery operation: %v", err)
		}
		if view.State == expected {
			// Completion state is persisted before the worker's final resource
			// release. Join its bounded done signal before starting another job.
			f.manager.mu.Lock()
			job := f.manager.jobs[id]
			f.manager.mu.Unlock()
			if job != nil {
				select {
				case <-job.done:
				case <-ctx.Done():
					t.Fatal("completed operation did not release its worker resources")
				}
			}
			return view
		}
		if terminalOperation(view.State) {
			t.Fatalf("operation ended as %s with safe code %s; expected %s", view.State, view.ErrorCode, expected)
		}
		select {
		case <-ctx.Done():
			t.Fatal("native operation did not reach its expected bounded completion")
		case <-ticker.C:
		}
	}
}

func (f *managerIntegrationFixture) operationByRequest(t *testing.T, requestID string) OperationView {
	t.Helper()
	page, err := f.manager.ListOperations(f.seed.ctx, f.seed.actor, 0, 100)
	if err != nil {
		t.Fatal("read bounded operation history")
	}
	for _, operation := range page.Items {
		if operation.RequestId == requestID {
			return operation
		}
	}
	t.Fatal("admitted request is missing from durable operation history")
	return OperationView{}
}

func (f *managerIntegrationFixture) auditID(t *testing.T, action activity.Action, resource string, state activity.State) int64 {
	t.Helper()
	var count int
	var id int64
	if err := f.seed.source.QueryRow(f.seed.ctx, `SELECT count(*),COALESCE(min(id),0) FROM public.activity_entries
		WHERE action=$1 AND resource_id=$2 AND state=$3`, string(action), resource, string(state)).Scan(&count, &id); err != nil || count != 1 || id < 1 {
		t.Fatalf("expected one persisted %s receipt", action)
	}
	return id
}

func (f *managerIntegrationFixture) assertReadyTarget(t *testing.T, plan OperationView, sourceHistory string) {
	t.Helper()
	op, err := f.manager.operationCopy(plan.Id)
	if err != nil || op.GenerationID == "" || op.Target == nil {
		t.Fatal("ready plan lacks an exact locally retained target")
	}
	current, err := f.runtime.lifecycle.Current()
	if err != nil || current.Revision != 0 || current.DatabaseSlot != lifecycle.DatabasePrimary || current.GenerationID != "" {
		t.Fatal("planning activated the target before a separate apply request")
	}
	var raw string
	if err := f.seed.target.QueryRow(f.seed.ctx, `SELECT value FROM public.server_settings WHERE key=$1`, recoverydb.MarkerKey).Scan(&raw); err != nil {
		t.Fatal("restored target lacks its local ownership marker")
	}
	marker, err := recoverydb.DecodeMarker(raw)
	if err != nil || marker.DeploymentID != current.DeploymentID || marker.GenerationID != op.GenerationID || marker.Slot != lifecycle.DatabaseRecovery || marker != op.Target.Marker {
		t.Fatal("archive contents substituted for the local target generation marker")
	}
	files, err := f.runtime.lifecycle.ReadGeneration(f.seed.ctx, op.GenerationID)
	if err != nil {
		t.Fatal("read protected staged generation files")
	}
	defer clear(files.Master)
	defaults, err := config.DecodeBackupDefaults(files.Config)
	if err != nil || defaults.ServerName != f.seed.configuration.ServerName {
		t.Fatal("staged generation lost the requested logical defaults")
	}
	tx, err := f.seed.target.BeginTx(f.seed.ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal("begin target vault witness")
	}
	witness, witnessErr := identity.ValidateApplicationKeyRecovery(f.seed.ctx, tx, files.Master)
	_ = tx.Rollback(f.seed.ctx)
	if witnessErr != nil || witness.SealedKeyCount != 2 || !witness.HasMasterKey {
		t.Fatal("ready target does not authenticate all retained application key history")
	}
	var liveCredentials, activeWork int
	if err := f.seed.target.QueryRow(f.seed.ctx, `SELECT
		(SELECT count(*) FROM public.sessions WHERE revoked_at IS NULL),
		(SELECT count(*) FROM public.play_sessions WHERE state IN ('Prepared','Playing','Paused'))+
		(SELECT count(*) FROM public.encoding_jobs WHERE state IN ('queued','running'))+
		(SELECT count(*) FROM public.scan_jobs WHERE status IN ('Queued','Running'))+
		(SELECT count(*) FROM public.task_runs WHERE state IN ('pending','running','stopping'))`).Scan(&liveCredentials, &activeWork); err != nil || liveCredentials != 0 || activeWork != 0 {
		t.Fatal("ready target retained imported credentials or active work")
	}
	if state := recoveryEngineRetainedState(t, f.seed.ctx, f.seed.target); state != sourceHistory {
		t.Fatal("planning changed retained account, key, device, catalog, or schedule history")
	}
	if state := recoveryEngineRetainedState(t, f.seed.ctx, f.seed.source); state != sourceHistory {
		t.Fatal("native operations changed original source state beyond their audit receipts")
	}
}

func (f *managerIntegrationFixture) assertNoSecrets(t *testing.T, passphrase []byte) {
	t.Helper()
	operations, err := f.manager.ListOperations(f.seed.ctx, f.seed.actor, 0, 100)
	if err != nil {
		t.Fatal("read operation views for secret projection checks")
	}
	backups, err := f.manager.ListBackups(f.seed.ctx, f.seed.actor, 0, 100)
	if err != nil {
		t.Fatal("read backup views for secret projection checks")
	}
	status, err := f.manager.Status(f.seed.ctx, f.seed.actor)
	if err != nil {
		t.Fatal("read native status projection")
	}
	encoded, err := json.Marshal(struct {
		Operations OperationPage
		Backups    BackupPage
		Status     StatusView
	}{operations, backups, status})
	if err != nil {
		t.Fatal("serialize explicit native recovery projections")
	}
	journal, err := f.runtime.control.Read(f.seed.ctx)
	if err != nil {
		t.Fatal("read protected operation journal")
	}
	master, err := os.ReadFile(f.seed.configuration.APIKeyMasterKeyFile)
	if err != nil {
		t.Fatal("read owned master fixture for negative projection checks")
	}
	defer clear(master)
	for _, secret := range [][]byte{passphrase, []byte(f.seed.activeKey.Token), []byte(f.seed.revokedKey.Token), []byte(f.seed.adminLogin.Token),
		master, []byte(hex.EncodeToString(master)), []byte(base64.StdEncoding.EncodeToString(master))} {
		if len(secret) == 0 || bytes.Contains(encoded, secret) || bytes.Contains(journal.Payload, secret) {
			t.Fatal("a native view or operation journal retained secret contents")
		}
	}
	for _, private := range []string{f.seed.actor.SessionID, f.seed.configuration.DatabaseURL, f.seed.configuration.Recovery.DatabaseURL,
		f.seed.configuration.APIKeyMasterKeyFile, f.seed.configuration.MediaRoots[0]} {
		if bytes.Contains(encoded, []byte(private)) {
			t.Fatal("native projection exposed private credentials or deployment paths")
		}
	}
}

func cleanupManagerPublicObjects(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if pool == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Error("begin cleanup of explicitly owned manager objects")
		return
	}
	defer tx.Rollback(ctx)
	const ownedTables = "activity_entries application_key_clients application_key_devices application_keys catalog_entities client_playback_references devices encoding_jobs item_entities item_images item_metadata_state item_subtitles item_theme_resources items libraries library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings sessions task_definitions task_occurrences task_run_children task_run_requests task_runs task_triggers theme_owner_ids theme_reserved_paths user_item_data user_settings users"
	qualified := make([]string, 0, 33)
	for _, table := range strings.Fields(ownedTables) {
		qualified = append(qualified, pgx.Identifier{"public", table}.Sanitize())
	}
	if _, err := tx.Exec(ctx, "DROP TABLE IF EXISTS "+strings.Join(qualified, ",")+" CASCADE"); err != nil {
		t.Error("remove only explicitly owned compiled fixture tables")
		return
	}
	// Dropping items also removes its composite-argument source-key function.
	if _, err := tx.Exec(ctx, `DROP FUNCTION IF EXISTS
		public.catalog_metadata_automatic_values(text,text,text,text,integer,integer,jsonb),
		public.initialize_catalog_metadata_state(),public.set_catalog_entity_name_hash(),
		public.sync_catalog_item_entities(text,jsonb),public.assign_theme_owner_id()`); err != nil {
		t.Error("remove only explicitly owned compiled fixture functions")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		t.Error("commit cleanup of explicitly owned fixture objects")
	}
}

type managerZeroReader struct{}

func (managerZeroReader) Read(data []byte) (int, error) {
	clear(data)
	return len(data), nil
}

type managerDisconnectedReader struct{ sent bool }

func (r *managerDisconnectedReader) Read(data []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(data, "age-encryption.org/v1\n"), nil
	}
	return 0, errors.New("fixture upload disconnected")
}

func (*managerDisconnectedReader) Close() error { return nil }

type managerBlockingReader struct {
	reading chan struct{}
	closed  chan struct{}
	read    sync.Once
	close   sync.Once
}

func (r *managerBlockingReader) Read([]byte) (int, error) {
	r.read.Do(func() { close(r.reading) })
	<-r.closed
	return 0, io.ErrClosedPipe
}

func (r *managerBlockingReader) Close() error {
	r.close.Do(func() { close(r.closed) })
	return nil
}

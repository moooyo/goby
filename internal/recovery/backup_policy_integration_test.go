//go:build linux

package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type backupPolicyNoProbe struct{}

func (backupPolicyNoProbe) ProbeFile(context.Context, *os.File) (media.Info, error) {
	return media.Info{}, errors.New("automatic backup tests do not probe media")
}

func backupPolicyRepository(t *testing.T, f *managerIntegrationFixture) (*library.Store, *tasks.Store, *tasks.Manager) {
	t.Helper()
	owner, err := library.New(f.seed.source, backupPolicyNoProbe{}, f.seed.configuration.MediaRoots)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := owner.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	registry, err := tasks.NewExecutorRegistry(f.manager.BackupPolicyTaskRegistration(owner))
	if err != nil {
		t.Fatal(err)
	}
	store, err := tasks.New(f.seed.source, owner, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Reconcile(f.seed.ctx); err != nil {
		t.Fatal(err)
	}
	if err = store.RecoverRuns(f.seed.ctx); err != nil {
		t.Fatal(err)
	}
	// The seed preserves an old startup scan. This fixture exercises backups
	// only and does not ask a scanner to touch the registered media directory.
	scan, err := store.GetByKey(f.seed.ctx, tasks.LibraryScanKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReplaceTriggers(f.seed.ctx, tasks.Actor{Principal: f.seed.actor, Audience: identity.AdministratorNative}, tasks.ReplaceTriggersRequest{TaskID: scan.ID, Revision: scan.Revision, ScheduleTimezone: "UTC", Triggers: []tasks.ScheduleRule{}}); err != nil {
		t.Fatal(err)
	}
	manager, err := tasks.NewManager(store, owner, tasks.ManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return owner, store, manager
}

func waitAutomaticBackupRun(t *testing.T, f *managerIntegrationFixture, store *tasks.Store, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.seed.ctx, 2*time.Minute)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		run, err := store.GetRun(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !run.State.Active() {
			if run.State != tasks.RunCompleted {
				t.Fatalf("automatic backup task ended as %s with %s", run.State, run.ErrorCode)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("automatic backup did not finish")
		case <-tick.C:
		}
	}
}

func TestAutomaticBackupPolicyPersistsSecretsSchedulesAndRetainsOnlyOwnedArchives(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newManagerIntegrationFixture(t)
	_, store, taskManager := backupPolicyRepository(t, f)
	ctx, actor := f.seed.ctx, f.seed.actor
	policy, err := f.manager.BackupPolicy(ctx, actor)
	if err != nil || policy.Enabled || policy.HasPassphrase || !policy.Available {
		t.Fatalf("initial backup policy: %#v %v", policy, err)
	}
	passphrase := []byte("automatic-backup-fixture-passphrase")
	defer clear(passphrase)
	request := BackupPolicyRequest{Revision: policy.Revision, Enabled: true, Schedule: BackupSchedule{Kind: "daily", TimeOfDay: time.Now().UTC().Add(time.Hour).Format("15:04"), Timezone: "UTC"}, KeepCount: 1, IncludeImages: false, Passphrase: &passphrase}
	saved, err := f.manager.UpdateBackupPolicy(ctx, actor, request)
	if err != nil || !saved.Enabled || !saved.HasPassphrase || saved.IncludeImages || saved.NextRunAt == nil {
		t.Fatalf("save backup policy: %#v %v", saved, err)
	}
	if _, err = f.manager.UpdateBackupPolicy(ctx, actor, request); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale backup policy was not rejected: %v", err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil || bytes.Contains(encoded, passphrase) {
		t.Fatal("policy view exposed its passphrase")
	}
	var ciphertext []byte
	var generation int64
	if err = f.seed.source.QueryRow(ctx, `SELECT secret_generation,passphrase_ciphertext FROM backup_policy WHERE id=1`).Scan(&generation, &ciphertext); err != nil || bytes.Contains(ciphertext, passphrase) {
		t.Fatal("automatic passphrase was stored in plaintext")
	}
	opened, err := f.seed.vault.OpenBackupPolicyPassphrase(ctx, generation, ciphertext)
	if err != nil || !bytes.Equal(opened, passphrase) {
		clear(opened)
		t.Fatal("saved passphrase could not be authenticated")
	}
	clear(opened)
	manual, err := f.manager.Create(ctx, actor, CreateRequest{RequestId: recoveryEngineTestID(t), Passphrase: passphrase})
	if err != nil {
		t.Fatal(err)
	}
	manual = f.waitOperation(t, manual.Id, "completed")
	manualMetadata, err := f.runtime.backups.Get(ctx, manual.BackupId)
	if err != nil {
		t.Fatal(err)
	}
	var publicationCount int
	if err = f.seed.source.QueryRow(ctx, `SELECT count(*) FROM activity_entries WHERE action='backup.published' AND resource_id=$1`, manual.BackupId).Scan(&publicationCount); err != nil || publicationCount != 1 {
		t.Fatal("manual publication did not record one successful completion fact")
	}
	if err = f.manager.recordBackupPublished(ctx, manual.BackupId, manualMetadata.Digest, manualMetadata.Size); err != nil {
		t.Fatal(err)
	}
	// Log retention must not erase the permanent publication identity and
	// cause reconciliation to create a second notification source activity.
	if _, err = f.seed.source.Exec(ctx, `DELETE FROM activity_entries WHERE action='backup.published' AND resource_id=$1`, manual.BackupId); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = f.manager.recordBackupPublished(ctx, manual.BackupId, manualMetadata.Digest, manualMetadata.Size); err != nil {
			t.Fatal(err)
		}
	}
	if err = f.seed.source.QueryRow(ctx, `SELECT count(*) FROM activity_entries WHERE action='backup.published' AND resource_id=$1`, manual.BackupId).Scan(&publicationCount); err != nil || publicationCount != 0 {
		t.Fatal("publication reconciliation recreated a retained completion notification")
	}
	if err = f.seed.source.QueryRow(ctx, `SELECT count(*) FROM backup_publication_receipts WHERE backup_id=$1`, manual.BackupId).Scan(&publicationCount); err != nil || publicationCount != 1 {
		t.Fatal("publication identity was not retained independently from display history")
	}
	var automaticIDs []string
	for _, requestID := range []string{"automatic-policy-first", "automatic-policy-second"} {
		admission, err := taskManager.Start(ctx, tasks.Actor{Principal: actor, Audience: identity.AdministratorNative}, tasks.StartRequest{TaskID: saved.TaskId, RequestID: requestID})
		if err != nil || !admission.Admitted {
			t.Fatalf("admit automatic backup: %v", err)
		}
		waitAutomaticBackupRun(t, f, store, admission.Run.ID)
		var id string
		if err = f.seed.source.QueryRow(ctx, `SELECT backup_id FROM backup_policy_runs WHERE run_id=$1`, admission.Run.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		automaticIDs = append(automaticIDs, id)
		if err = f.seed.source.QueryRow(ctx, `SELECT count(*) FROM activity_entries WHERE action='backup.published' AND resource_id=$1`, id).Scan(&publicationCount); err != nil || publicationCount != 1 {
			t.Fatal("automatic publication did not record exactly one successful completion")
		}
		if len(automaticIDs) == 1 {
			metadata, err := f.runtime.backups.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.seed.source.Exec(ctx, `UPDATE backup_policy_runs SET state='deleting' WHERE run_id=$1`, admission.Run.ID); err != nil {
				t.Fatal(err)
			}
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			if err := f.runtime.backups.Delete(cancelled, id, metadata.Digest); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled retention changed the store: %v", err)
			}
			if err := f.manager.backupPolicy.releaseRetention(admission.Run.ID, id, metadata.Digest, "another-deployment"); err != nil {
				t.Fatal(err)
			}
			var state string
			if err = f.seed.source.QueryRow(ctx, `SELECT state FROM backup_policy_runs WHERE run_id=$1`, admission.Run.ID).Scan(&state); err != nil || state != "deleting" {
				t.Fatal("cleanup crossed deployment ownership")
			}
			if err := f.manager.backupPolicy.releaseRetention(admission.Run.ID, id, metadata.Digest, f.manager.data.DeploymentID); err != nil {
				t.Fatal(err)
			}
			if err = f.seed.source.QueryRow(ctx, `SELECT state FROM backup_policy_runs WHERE run_id=$1`, admission.Run.ID).Scan(&state); err != nil || state != "completed" {
				t.Fatal("cancelled retention did not become eligible for the next run")
			}
			if _, err := f.runtime.backups.Get(ctx, id); err != nil {
				t.Fatal("retention cleanup deleted the ready archive")
			}
		}
		replay, err := taskManager.Start(ctx, tasks.Actor{Principal: actor, Audience: identity.AdministratorNative}, tasks.StartRequest{TaskID: saved.TaskId, RequestID: requestID})
		if err != nil || replay.Admitted || replay.Run.ID != admission.Run.ID {
			t.Fatal("completed task request started a second backup")
		}
	}
	if _, err = f.runtime.backups.Get(ctx, automaticIDs[0]); !errors.Is(err, backupstore.ErrNotFound) {
		t.Fatalf("retention kept an old owned automatic archive: %v", err)
	}
	if _, err = f.runtime.backups.Get(ctx, automaticIDs[1]); err != nil {
		t.Fatal("retention removed the newest automatic archive")
	}
	if _, err = f.runtime.backups.Get(ctx, manual.BackupId); err != nil {
		t.Fatal("retention removed a manual backup")
	}
	current, err := f.manager.BackupPolicy(ctx, actor)
	if err != nil || current.LastRunState != "completed" || current.LastBackupId != automaticIDs[1] {
		t.Fatalf("automatic backup completion view: %#v %v", current, err)
	}
	clearRequest := BackupPolicyRequest{Revision: current.Revision, Enabled: false, Schedule: current.Schedule, KeepCount: current.KeepCount, IncludeImages: current.IncludeImages, Passphrase: &[]byte{}}
	cleared, err := f.manager.UpdateBackupPolicy(ctx, actor, clearRequest)
	if err != nil || cleared.Enabled || cleared.HasPassphrase || cleared.NextRunAt != nil {
		t.Fatalf("disable and clear saved backup secret: %#v %v", cleared, err)
	}
}

func TestAutomaticBackupPolicyRejectsUnsealedWorkAndClearingAnEnabledSecret(t *testing.T) {
	f := newManagerIntegrationFixture(t)
	_, _, _ = backupPolicyRepository(t, f)
	ctx, actor := f.seed.ctx, f.seed.actor
	policy, err := f.manager.BackupPolicy(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	empty := []byte{}
	_, err = f.manager.UpdateBackupPolicy(ctx, actor, BackupPolicyRequest{Revision: policy.Revision, Enabled: true, Schedule: policy.Schedule, KeepCount: 1, IncludeImages: true, Passphrase: &empty})
	var validation *BackupPolicyValidationError
	if !errors.As(err, &validation) || validation.Fields["Passphrase"] == "" {
		t.Fatalf("enabled automatic backup accepted no passphrase: %v", err)
	}
	work := tasks.Work{TaskKey: BackupTaskKey, RunID: recoveryEngineTestID(t), ChildID: recoveryEngineTestID(t)}
	if err = f.manager.backupPolicy.Execute(ctx, work, func(tasks.Progress) error { return nil }); err == nil {
		t.Fatal("unsealed work acquired automatic backup authority")
	}
	page, err := f.runtime.backups.List(ctx, 0, 100)
	if err != nil || page.TotalRecordCount != 0 {
		t.Fatal("unsealed work allocated a backup object")
	}
	writer, err := f.runtime.backups.Begin(ctx, backupstore.BeginOptions{Kind: backupstore.KindGenerated})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	beforePublish := writer.Metadata().ID
	if err := f.manager.recordBackupPublished(ctx, beforePublish, strings.Repeat("a", 64), 100); !errors.Is(err, ErrConflict) {
		t.Fatalf("an unpublished backup acquired a completion fact: %v", err)
	}
	var facts int
	if err := f.seed.source.QueryRow(ctx, `SELECT count(*) FROM backup_publication_receipts WHERE backup_id=$1`, beforePublish).Scan(&facts); err != nil || facts != 0 {
		t.Fatal("an unpublished object retained a successful receipt")
	}
}

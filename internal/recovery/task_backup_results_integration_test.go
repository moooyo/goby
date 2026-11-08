//go:build linux

package recovery

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

func taskBackupResultsSeedRun(t *testing.T, f *managerIntegrationFixture, taskID, taskKey string) string {
	t.Helper()
	id := recoveryEngineTestID(t)
	if _, err := f.seed.source.Exec(f.seed.ctx, `INSERT INTO task_runs(id,task_id,task_key,task_name,state,source,started_at,finished_at)
		VALUES($1,$2,$3,'Recorded backup result fixture','completed','manual',clock_timestamp(),clock_timestamp())`, id, taskID, taskKey); err != nil {
		t.Fatal("seed an exact completed task run")
	}
	return id
}

func taskBackupResultsSeedAssociation(t *testing.T, f *managerIntegrationFixture, runID string, metadata backupstore.Metadata, state string) {
	t.Helper()
	if _, err := f.seed.source.Exec(f.seed.ctx, `INSERT INTO backup_policy_runs(run_id,backup_id,deployment_id,policy_revision,include_images,state,sha256)
		VALUES($1,$2,$3,1,false,$4,$5)`, runID, metadata.ID, f.manager.data.DeploymentID, state, metadata.Digest); err != nil {
		t.Fatal("seed an exact persisted backup association")
	}
}

func taskBackupResultsOpaqueObject(t *testing.T, f *managerIntegrationFixture, options backupstore.BeginOptions) backupstore.Metadata {
	t.Helper()
	writer, err := f.runtime.backups.Begin(f.seed.ctx, options)
	if err != nil {
		t.Fatal("begin the unrelated backup object")
	}
	defer writer.Close()
	data := []byte("Opaque backup result fixture bytes")
	if count, err := writer.Write(data); err != nil || count != len(data) {
		t.Fatal("write the unrelated backup object")
	}
	proof, err := writer.Prepare(f.seed.ctx)
	if err != nil {
		t.Fatal("prepare the unrelated backup object")
	}
	metadata, err := writer.Publish(f.seed.ctx, proof, nil)
	if err != nil {
		t.Fatal("publish the unverified unrelated backup object")
	}
	return metadata
}

func taskBackupResultsExpectNoMetadata(t *testing.T, result TaskBackupResultView, status, id string) {
	t.Helper()
	state := ""
	if status == "deleted" || status == "deleting" {
		state = status
	}
	if result.Status != status || result.BackupId != id || result.State != state || result.SizeBytes != "" || result.SHA256 != "" || result.Verified != nil {
		t.Fatalf("%s result disclosed or invented object metadata: %#v", status, result)
	}
}

func TestTaskBackupResultsMatchAutomaticRunsAndRetainedEvidence(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newManagerIntegrationFixture(t)
	_, store, taskManager := backupPolicyRepository(t, f)
	ctx, actor := f.seed.ctx, f.seed.actor
	policy, err := f.manager.BackupPolicy(ctx, actor)
	if err != nil {
		t.Fatal("read the automatic backup policy")
	}
	passphrase := []byte("task-backup-results-integration-passphrase")
	defer clear(passphrase)
	policy, err = f.manager.UpdateBackupPolicySettings(ctx, actor, BackupPolicySettingsRequest{
		Revision: policy.Revision, KeepCount: 1, IncludeImages: false, Passphrase: &passphrase,
	})
	if err != nil {
		t.Fatal("save automatic backup fixture settings")
	}
	start := func(requestID string) (string, backupstore.Metadata) {
		t.Helper()
		admission, err := taskManager.Start(ctx, tasks.Actor{Principal: actor, Audience: identity.AdministratorNative}, tasks.StartRequest{TaskID: policy.TaskId, RequestID: requestID})
		if err != nil || !admission.Admitted {
			t.Fatalf("admit the actual automatic backup: %v", err)
		}
		waitAutomaticBackupRun(t, f, store, admission.Run.ID)
		var backupID string
		if err := f.seed.source.QueryRow(ctx, `SELECT backup_id FROM backup_policy_runs WHERE run_id=$1`, admission.Run.ID).Scan(&backupID); err != nil {
			t.Fatal("read the admitted run's exact backup identity")
		}
		metadata, err := f.runtime.backups.Get(ctx, backupID)
		if err != nil || metadata.State != backupstore.StateReady || !metadata.Verified || metadata.CreatorID != "" || metadata.SessionID != "" {
			t.Fatalf("actual automatic metadata lost its verified system-owned identity: %v", err)
		}
		return admission.Run.ID, metadata
	}
	firstRun, firstMetadata := start("task-backup-results-first")
	// A subsequent settings revision cannot erase the first run's association.
	policy, err = f.manager.UpdateBackupPolicySettings(ctx, actor, BackupPolicySettingsRequest{Revision: policy.Revision, KeepCount: 2, IncludeImages: false})
	if err != nil {
		t.Fatal("advance the saved policy revision")
	}
	results, err := f.manager.TaskBackupResults(ctx, actor, []string{firstRun})
	first := results[firstRun]
	if err != nil || len(results) != 1 || first.Status != "available" || first.BackupId != firstMetadata.ID || first.State != string(firstMetadata.State) ||
		first.SizeBytes != strconv.FormatInt(firstMetadata.Size, 10) || first.SHA256 != firstMetadata.Digest || first.Verified == nil || *first.Verified != firstMetadata.Verified {
		t.Fatalf("exact automatic result did not match actual metadata: %#v %v", first, err)
	}
	policy, err = f.manager.UpdateBackupPolicySettings(ctx, actor, BackupPolicySettingsRequest{Revision: policy.Revision, KeepCount: 1, IncludeImages: false})
	if err != nil {
		t.Fatal("restore one-object automatic retention")
	}
	lastRun, lastMetadata := start("task-backup-results-second")
	if err := taskManager.Close(ctx); err != nil {
		t.Fatal("drain automatic workers before inspecting retained evidence")
	}
	results, err = f.manager.TaskBackupResults(ctx, actor, []string{firstRun, lastRun})
	if err != nil || len(results) != 2 {
		t.Fatalf("read retained automatic results: %v", err)
	}
	taskBackupResultsExpectNoMetadata(t, results[firstRun], "deleted", firstMetadata.ID)
	if results[firstRun].State != "deleted" || results[lastRun].Status != "available" || results[lastRun].BackupId != lastMetadata.ID {
		t.Fatal("retention changed the newest result or lost durable deletion evidence")
	}
	if _, err := f.runtime.backups.Get(ctx, firstMetadata.ID); !errors.Is(err, backupstore.ErrNotFound) {
		t.Fatal("the first automatic object was not actually removed")
	}

	unlinkedRun := taskBackupResultsSeedRun(t, f, policy.TaskId, BackupTaskKey)
	manual := taskBackupResultsOpaqueObject(t, f, backupstore.BeginOptions{Kind: backupstore.KindGenerated, CreatorID: actor.User.ID, SessionID: actor.SessionID})
	imported := taskBackupResultsOpaqueObject(t, f, backupstore.BeginOptions{Kind: backupstore.KindImported})
	scan, err := store.GetByKey(ctx, tasks.LibraryScanKey)
	if err != nil {
		t.Fatal("read the independent scan definition")
	}
	scanRun := taskBackupResultsSeedRun(t, f, scan.ID, tasks.LibraryScanKey)
	// The policy table has no task-run foreign key. A valid-looking policy row
	// attached to another task type must not create a backup result for it.
	phantom := backupstore.Metadata{ID: recoveryEngineTestID(t), Digest: strings.Repeat("a", 64)}
	taskBackupResultsSeedAssociation(t, f, scanRun, phantom, "completed")
	absentRun := recoveryEngineTestID(t)
	results, err = f.manager.TaskBackupResults(ctx, actor, []string{unlinkedRun, scanRun, absentRun})
	if err != nil || len(results) != 1 {
		t.Fatalf("bounded exact lookup associated a foreign or nonexistent run: %#v %v", results, err)
	}
	taskBackupResultsExpectNoMetadata(t, results[unlinkedRun], "not_recorded", "")
	for _, metadata := range []backupstore.Metadata{manual, imported} {
		forgedRun := taskBackupResultsSeedRun(t, f, policy.TaskId, BackupTaskKey)
		taskBackupResultsSeedAssociation(t, f, forgedRun, metadata, "completed")
		results, err := f.manager.TaskBackupResults(ctx, actor, []string{forgedRun})
		if err != nil {
			t.Fatal("read the mismatched object association")
		}
		taskBackupResultsExpectNoMetadata(t, results[forgedRun], "mismatch", "")
	}

	runCase := func(name string, check func(*testing.T)) {
		if !t.Run(name, check) {
			t.Fatal("stop inspecting shared fixture evidence after a failed result check")
		}
	}
	runCase("deployment_restoration_and_digest_mismatches", func(t *testing.T) {
		deploymentID := f.manager.data.DeploymentID
		for _, mismatch := range []struct{ deployment, state, digest string }{
			{"another-deployment", "completed", lastMetadata.Digest},
			{deploymentID, "restored", lastMetadata.Digest},
			{deploymentID, "completed", strings.Repeat("0", 64)},
		} {
			if _, err := f.seed.source.Exec(ctx, `UPDATE backup_policy_runs SET deployment_id=$2,state=$3,sha256=$4 WHERE run_id=$1`, lastRun, mismatch.deployment, mismatch.state, mismatch.digest); err != nil {
				t.Fatal("prepare the exact mismatch evidence")
			}
			results, err := f.manager.TaskBackupResults(ctx, actor, []string{lastRun})
			if err != nil {
				t.Fatal("read the exact mismatched backup result")
			}
			taskBackupResultsExpectNoMetadata(t, results[lastRun], "mismatch", "")
		}
		if _, err := f.seed.source.Exec(ctx, `UPDATE backup_policy_runs SET deployment_id=$2,state='completed',sha256=$3 WHERE run_id=$1`, lastRun, deploymentID, lastMetadata.Digest); err != nil {
			t.Fatal("restore the real completed association")
		}
	})
	runCase("missing_is_distinct_from_durable_deletion", func(t *testing.T) {
		if err := f.runtime.backups.Delete(ctx, lastMetadata.ID, lastMetadata.Digest); err != nil {
			t.Fatal("remove the object without altering its run evidence")
		}
		results, err := f.manager.TaskBackupResults(ctx, actor, []string{lastRun})
		if err != nil {
			t.Fatal("read the missing object result")
		}
		taskBackupResultsExpectNoMetadata(t, results[lastRun], "missing", lastMetadata.ID)
		for _, state := range []string{"deleting", "deleted"} {
			if _, err := f.seed.source.Exec(ctx, `UPDATE backup_policy_runs SET state=$2 WHERE run_id=$1`, lastRun, state); err != nil {
				t.Fatal("persist the exact deletion phase")
			}
			results, err := f.manager.TaskBackupResults(ctx, actor, []string{lastRun})
			if err != nil {
				t.Fatal("read the durable deletion phase")
			}
			taskBackupResultsExpectNoMetadata(t, results[lastRun], state, lastMetadata.ID)
			if results[lastRun].State != state {
				t.Fatal("the missing file guessed a different durable deletion phase")
			}
		}
	})
}

type taskBackupResultsHookOwner struct {
	owner  library.OwnedTransactions
	before func(int) error
	after  func(int) error
	wrap   func(library.OwnedTx) library.OwnedTx
	calls  int
}

func (owner *taskBackupResultsHookOwner) WithOwnedTx(ctx context.Context, callback func(library.OwnedTx) error) error {
	owner.calls++
	call := owner.calls
	if owner.before != nil {
		if err := owner.before(call); err != nil {
			return err
		}
	}
	err := owner.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if owner.wrap != nil {
			tx = owner.wrap(tx)
		}
		return callback(tx)
	})
	if err == nil && owner.after != nil {
		err = owner.after(call)
	}
	return err
}

type taskBackupResultsQueryFailureTx struct{ library.OwnedTx }

func (taskBackupResultsQueryFailureTx) Query(string, ...any) (library.OwnedRows, error) {
	return nil, errors.New("backup result query fixture failure")
}

func taskBackupResultsFreshActor(t *testing.T, f *managerIntegrationFixture) (identity.Principal, string) {
	t.Helper()
	login, err := f.seed.identities.Authenticate(f.seed.ctx, f.seed.actor.User.Name, "recovery-administrator-password", identity.Client{}, "admin")
	if err != nil {
		t.Fatal("issue a separate result-read credential")
	}
	actor, err := f.seed.identities.Resolve(f.seed.ctx, login.Token, "admin")
	if err != nil {
		t.Fatal("resolve the separate result-read credential")
	}
	return actor, login.Token
}

func TestTaskBackupResultsFenceAuthorityOwnershipAndConcurrentChanges(t *testing.T) {
	f := newManagerIntegrationFixture(t)
	owner, store, taskManager := backupPolicyRepository(t, f)
	ctx, actor := f.seed.ctx, f.seed.actor
	if err := taskManager.Close(ctx); err != nil {
		t.Fatal("drain unrelated scheduler work before the read-fence checks")
	}
	definition, err := store.GetByKey(ctx, BackupTaskKey)
	if err != nil {
		t.Fatal("read the automatic backup definition")
	}
	runID := taskBackupResultsSeedRun(t, f, definition.ID, BackupTaskKey)
	install := func(t *testing.T, hook *taskBackupResultsHookOwner) {
		t.Helper()
		hook.owner = owner
		f.manager.backupPolicy.owner = hook
		t.Cleanup(func() { f.manager.backupPolicy.owner = owner })
	}
	runCase := func(name string, check func(*testing.T)) {
		if !t.Run(name, check) {
			t.Fatal("stop using the read-fence fixture after a failed boundary check")
		}
	}
	runCase("actor_revoked_during_owner_wait", func(t *testing.T) {
		reader, token := taskBackupResultsFreshActor(t, f)
		entered, release := make(chan struct{}), make(chan struct{})
		hook := &taskBackupResultsHookOwner{before: func(call int) error {
			if call == 1 {
				close(entered)
				<-release
			}
			return nil
		}}
		install(t, hook)
		done := make(chan error, 1)
		go func() {
			results, err := f.manager.TaskBackupResults(ctx, reader, []string{runID})
			if results != nil {
				done <- errors.New("revoked read exposed a result map")
				return
			}
			done <- err
		}()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("the result read did not reach its owned transaction")
		}
		if err := f.seed.identities.Revoke(ctx, token); err != nil {
			close(release)
			t.Fatal("revoke the delayed reader")
		}
		close(release)
		select {
		case err := <-done:
			if !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("the delayed actor bypassed fresh owner authority: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the revoked owner read did not settle")
		}
	})
	runCase("actor_revoked_after_final_owned_sql", func(t *testing.T) {
		reader, token := taskBackupResultsFreshActor(t, f)
		hook := &taskBackupResultsHookOwner{after: func(call int) error {
			if call == 2 {
				return f.seed.identities.Revoke(ctx, token)
			}
			return nil
		}}
		install(t, hook)
		results, err := f.manager.TaskBackupResults(ctx, reader, []string{runID})
		if results != nil || !errors.Is(err, identity.ErrUnauthorized) || hook.calls != 2 {
			t.Fatalf("the final independent authorization was omitted: %#v %v", results, err)
		}
	})
	runCase("cancelled_after_admission_preserves_owner", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		defer cancel()
		install(t, &taskBackupResultsHookOwner{before: func(call int) error {
			if call == 1 {
				cancel()
			}
			return nil
		}})
		results, err := f.manager.TaskBackupResults(cancelled, actor, []string{runID})
		if results != nil || !errors.Is(err, context.Canceled) || !owner.Available() {
			t.Fatalf("caller cancellation exposed data or lost the catalog owner: %#v %v", results, err)
		}
	})
	runCase("sql_failure_does_not_expose_partial_results", func(t *testing.T) {
		install(t, &taskBackupResultsHookOwner{wrap: func(tx library.OwnedTx) library.OwnedTx {
			return taskBackupResultsQueryFailureTx{OwnedTx: tx}
		}})
		results, err := f.manager.TaskBackupResults(ctx, actor, []string{runID})
		if results != nil || !errors.Is(err, ErrUnavailable) || !owner.Available() {
			t.Fatalf("SQL failure was exposed as a successful partial association: %#v %v", results, err)
		}
	})
	runCase("retention_between_rounds_discards_prior_metadata", func(t *testing.T) {
		metadata := taskBackupResultsOpaqueObject(t, f, backupstore.BeginOptions{Kind: backupstore.KindGenerated})
		taskBackupResultsSeedAssociation(t, f, runID, metadata, "completed")
		install(t, &taskBackupResultsHookOwner{before: func(call int) error {
			if call != 2 {
				return nil
			}
			if err := f.runtime.backups.Delete(ctx, metadata.ID, metadata.Digest); err != nil {
				return err
			}
			_, err := f.seed.source.Exec(ctx, `UPDATE backup_policy_runs SET state='deleted' WHERE run_id=$1`, runID)
			return err
		}})
		results, err := f.manager.TaskBackupResults(ctx, actor, []string{runID})
		if err != nil {
			t.Fatal("read the concurrently deleted backup")
		}
		taskBackupResultsExpectNoMetadata(t, results[runID], "deleted", metadata.ID)
	})
	runCase("generation_changes_during_the_observation", func(t *testing.T) {
		current := f.manager.current
		t.Cleanup(func() {
			f.manager.mu.Lock()
			f.manager.current = current
			f.manager.mu.Unlock()
		})
		install(t, &taskBackupResultsHookOwner{after: func(call int) error {
			if call == 1 {
				f.manager.mu.Lock()
				f.manager.current.Revision++
				f.manager.mu.Unlock()
			}
			return nil
		}})
		results, err := f.manager.TaskBackupResults(ctx, actor, []string{runID})
		if results != nil || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("an outdated application generation exposed results: %#v %v", results, err)
		}
	})
	runCase("unavailable_storage_preserves_only_existing_deletion_evidence", func(t *testing.T) {
		liveRun := taskBackupResultsSeedRun(t, f, definition.ID, BackupTaskKey)
		metadata := taskBackupResultsOpaqueObject(t, f, backupstore.BeginOptions{Kind: backupstore.KindGenerated})
		taskBackupResultsSeedAssociation(t, f, liveRun, metadata, "completed")
		if err := f.runtime.backups.Close(); err != nil {
			t.Fatal("close the backup store before the metadata observation")
		}
		results, err := f.manager.TaskBackupResults(ctx, actor, []string{runID, liveRun})
		if err != nil || len(results) != 2 {
			t.Fatalf("storage failure erased independently retained run evidence: %#v %v", results, err)
		}
		taskBackupResultsExpectNoMetadata(t, results[liveRun], "unavailable", metadata.ID)
		if results[runID].Status != "deleted" || results[runID].SizeBytes != "" || results[runID].Verified != nil {
			t.Fatal("storage failure changed a durable deletion into guessed metadata")
		}
	})
	runCase("lost_catalog_owner_cannot_be_replaced", func(t *testing.T) {
		if err := owner.Close(ctx); err != nil {
			t.Fatal("close the original catalog owner")
		}
		results, err := f.manager.TaskBackupResults(ctx, actor, []string{runID})
		if results != nil || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("a closed owner returned a result projection: %#v %v", results, err)
		}
	})
	runCase("lost_deployment_lease_rejects_the_projection", func(t *testing.T) {
		if err := f.lease.Close(); err != nil {
			t.Fatal("close the original deployment lease")
		}
		results, err := f.manager.TaskBackupResults(ctx, actor, []string{runID})
		if results != nil || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("a lost deployment lease exposed backup results: %#v %v", results, err)
		}
	})
}

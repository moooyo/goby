package recovery

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

func automaticBackupEvent(action activity.Action, id string, state activity.State) activity.Event {
	return activity.Event{Action: action, Source: activity.SourceSystem, Actor: activity.Actor{Kind: activity.ActorSystem}, Resource: activity.Resource{Kind: activity.ResourceBackup, ID: id}, State: state}
}

func (executor *backupPolicyExecutor) Execute(ctx context.Context, work tasks.Work, progress func(tasks.Progress) error) (resultErr error) {
	if !executor.Available() || work.TaskKey != BackupTaskKey || work.LibraryID != "" {
		return tasks.ErrUnavailable
	}
	m := executor.manager
	m.mu.Lock()
	if err := m.healthyLocked(ctx); err != nil {
		m.mu.Unlock()
		return err
	}
	if m.busyLocked("") {
		m.mu.Unlock()
		return ErrBusy
	}
	workCtx, cancel := context.WithTimeout(ctx, m.cfg.Recovery.WithDefaults().OperationTimeout)
	stop := context.AfterFunc(m.ctx, cancel)
	job := &activeJob{cancel: cancel, done: make(chan struct{})}
	m.jobs[work.RunID] = job
	m.automaticBackupID = work.RunID
	deploymentID := m.data.DeploymentID
	m.mu.Unlock()
	defer func() {
		stop()
		cancel()
		m.mu.Lock()
		delete(m.jobs, work.RunID)
		m.automaticBackupID = ""
		close(job.done)
		m.mu.Unlock()
	}()
	work = work.WithContext(workCtx)
	var generation, revision int64
	var sealed []byte
	var includeImages bool
	var keep int
	var completed bool
	err := executor.owner.WithOwnedTx(workCtx, func(tx library.OwnedTx) error {
		if err := work.Fence(tx); err != nil {
			return err
		}
		var prior string
		err := tx.QueryRow(`SELECT state FROM backup_policy_runs WHERE run_id=$1`, work.RunID).Scan(&prior)
		if err == nil {
			if prior == "completed" || prior == "deleted" {
				completed = true
				return work.Fence(tx)
			}
			return ErrConflict
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := tx.QueryRow(`SELECT d.revision,p.secret_generation,p.passphrase_ciphertext,p.include_images,p.keep_count FROM task_definitions d CROSS JOIN backup_policy p WHERE d.key=$1 AND p.id=1`, BackupTaskKey).Scan(&revision, &generation, &sealed, &includeImages, &keep); err != nil {
			return err
		}
		if len(sealed) == 0 {
			return ErrUnavailable
		}
		return work.Fence(tx)
	})
	if err != nil {
		return err
	}
	if completed {
		return progress(tasks.Progress{Processed: 1, Added: 1})
	}
	passphrase, err := m.vault.OpenBackupPolicyPassphrase(workCtx, generation, sealed)
	if err != nil {
		return ErrUnavailable
	}
	defer clear(passphrase)
	writer, err := m.runtime.backups.Begin(workCtx, backupstore.BeginOptions{Kind: backupstore.KindGenerated})
	if err != nil {
		return err
	}
	defer writer.Close()
	backupID := writer.Metadata().ID
	admitted := false
	defer func() {
		if resultErr == nil {
			return
		}
		abortWriter(writer, writerErrorCode(resultErr))
		if admitted {
			executor.finishFailure(work.RunID, backupID, resultErr)
		}
	}()
	err = executor.owner.WithOwnedTx(workCtx, func(tx library.OwnedTx) error {
		if err := work.Fence(tx); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO backup_policy_runs(run_id,backup_id,deployment_id,policy_revision,include_images,state) VALUES($1,$2,$3,$4,$5,'running')`, work.RunID, backupID, deploymentID, revision, includeImages); err != nil {
			return err
		}
		if err := activity.RecordOwned(tx, automaticBackupEvent(activity.ActionBackupRequested, backupID, "")); err != nil {
			return err
		}
		return work.Fence(tx)
	})
	if err != nil {
		return err
	}
	admitted = true
	manifest, err := m.engine.CreateWithOptions(workCtx, writer, passphrase, CreateOptions{IncludeImages: includeImages})
	if err != nil {
		return err
	}
	clear(passphrase)
	proof, err := writer.Prepare(workCtx)
	if err != nil {
		return err
	}
	err = executor.owner.WithOwnedTx(workCtx, func(tx library.OwnedTx) error {
		if err := work.Fence(tx); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE backup_policy_runs SET state='publishing',sha256=$2,updated_at=clock_timestamp() WHERE run_id=$1 AND state='running'`, work.RunID, proof.Digest); err != nil {
			return err
		}
		if err := activity.RecordOwned(tx, automaticBackupEvent(activity.ActionBackupFinished, backupID, activity.StateCompleted)); err != nil {
			return err
		}
		return work.Fence(tx)
	})
	if err != nil {
		return err
	}
	summary := Summary(manifest)
	if _, err = writer.Publish(workCtx, proof, &summary); err != nil {
		return err
	}
	if err = m.recordBackupPublished(workCtx, backupID, proof.Digest, proof.Size); err != nil {
		return err
	}
	err = executor.owner.WithOwnedTx(workCtx, func(tx library.OwnedTx) error {
		if err := work.Fence(tx); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE backup_policy_runs SET state='completed',error_code='',updated_at=clock_timestamp() WHERE run_id=$1 AND state='publishing' AND sha256=$2`, work.RunID, proof.Digest); err != nil {
			return err
		}
		return work.Fence(tx)
	})
	if err != nil {
		return err
	}
	if err = progress(tasks.Progress{Processed: 1, Added: 1}); err != nil {
		return err
	}
	return executor.retain(workCtx, work, deploymentID, keep)
}

func (executor *backupPolicyExecutor) finishFailure(runID, backupID string, cause error) {
	m := executor.manager
	if m == nil || !m.lease.Protects(m.pool) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	state := "failed"
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		state = "interrupted"
	}
	code := errorCode(cause)
	var phase, digest string
	if m.pool.QueryRow(ctx, `SELECT state,sha256 FROM backup_policy_runs WHERE run_id=$1 AND backup_id=$2`, runID, backupID).Scan(&phase, &digest) != nil {
		return
	}
	if phase == "publishing" {
		metadata, err := m.runtime.backups.Get(ctx, backupID)
		if err == nil && metadata.Kind == backupstore.KindGenerated && metadata.State == backupstore.StateReady && metadata.Verified && metadata.Digest == digest {
			if err := m.recordBackupPublished(ctx, backupID, metadata.Digest, metadata.Size); err == nil {
				state, code = "completed", ""
			} else {
				state, code = "publishing", "audit_unavailable"
			}
		} else {
			state = "interrupted"
		}
	}
	_, _ = m.pool.Exec(ctx, `UPDATE backup_policy_runs SET state=$3,error_code=$4,updated_at=clock_timestamp() WHERE run_id=$1 AND backup_id=$2 AND state IN ('running','publishing')`, runID, backupID, state, code)
}

func (executor *backupPolicyExecutor) retain(ctx context.Context, work tasks.Work, deploymentID string, keep int) error {
	m := executor.manager
	// The bounded backup store limits live objects, but retained run evidence
	// can outlive deleted objects. Select only exact, completed owned archives.
	rows, err := m.pool.Query(ctx, `SELECT run_id,backup_id,sha256 FROM backup_policy_runs WHERE deployment_id=$1 AND state='completed' ORDER BY created_at DESC,run_id DESC OFFSET $2 LIMIT 128`, deploymentID, keep)
	if err != nil {
		return ErrUnavailable
	}
	type candidate struct{ run, id, digest string }
	var items []candidate
	for rows.Next() {
		var c candidate
		if rows.Scan(&c.run, &c.id, &c.digest) != nil {
			rows.Close()
			return ErrUnavailable
		}
		items = append(items, c)
	}
	if rows.Err() != nil {
		rows.Close()
		return ErrUnavailable
	}
	rows.Close()
	for _, item := range items {
		metadata, getErr := m.runtime.backups.Get(ctx, item.id)
		if errors.Is(getErr, backupstore.ErrNotFound) {
			if err = executor.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
				if err := work.Fence(tx); err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE backup_policy_runs SET state='deleted',updated_at=clock_timestamp() WHERE run_id=$1 AND backup_id=$2 AND sha256=$3 AND state='completed'`, item.run, item.id, item.digest); err != nil {
					return err
				}
				return work.Fence(tx)
			}); err != nil {
				return err
			}
			continue
		}
		if getErr != nil {
			return getErr
		}
		if metadata.Kind != backupstore.KindGenerated || metadata.State != backupstore.StateReady || !metadata.Verified || metadata.Digest != item.digest {
			continue
		}
		err = executor.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
			if err := work.Fence(tx); err != nil {
				return err
			}
			tag, err := tx.Exec(`UPDATE backup_policy_runs SET state='deleting',updated_at=clock_timestamp() WHERE run_id=$1 AND backup_id=$2 AND deployment_id=$3 AND sha256=$4 AND state='completed'`, item.run, item.id, deploymentID, item.digest)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrConflict
			}
			if err = activity.RecordOwned(tx, automaticBackupEvent(activity.ActionBackupDeleteRequested, item.id, "")); err != nil {
				return err
			}
			return work.Fence(tx)
		})
		if err != nil {
			// A lost commit reply can still have persisted the deletion grant.
			// Revoke that grant after observing the unchanged ready object.
			_ = executor.releaseRetention(item.run, item.id, item.digest, deploymentID)
			return err
		}
		err = m.runtime.backups.Delete(ctx, item.id, item.digest)
		if errors.Is(err, backupstore.ErrBusy) {
			// An active reader is never interrupted for retention. The next
			// successful scheduled backup retries the same completed object.
			if cleanupErr := executor.releaseRetention(item.run, item.id, item.digest, deploymentID); cleanupErr != nil {
				return cleanupErr
			}
			continue
		}
		if err != nil && !errors.Is(err, backupstore.ErrNotFound) {
			_ = executor.releaseRetention(item.run, item.id, item.digest, deploymentID)
			return err
		}
		err = executor.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
			if err := work.Fence(tx); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE backup_policy_runs SET state='deleted',error_code='',updated_at=clock_timestamp() WHERE run_id=$1 AND state='deleting'`, item.run); err != nil {
				return err
			}
			if err := activity.RecordOwned(tx, automaticBackupEvent(activity.ActionBackupDeleted, item.id, "")); err != nil {
				return err
			}
			return work.Fence(tx)
		})
		if err != nil {
			_ = executor.releaseRetention(item.run, item.id, item.digest, deploymentID)
			return err
		}
	}
	return nil
}

// releaseRetention settles an already-authorized deletion after its caller
// loses cancellation/timeout authority. It never deletes a file. An exact
// ready object loses its deletion grant and becomes eligible for a later
// successful task; an already-absent object gets its completion receipt. A
// failed observation keeps the uncertain deleting receipt for startup review.
func (executor *backupPolicyExecutor) releaseRetention(runID, backupID, digest, deploymentID string) error {
	m := executor.manager
	if m == nil || !m.lease.Protects(m.pool) {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	state, code := "completed", "retention_deferred"
	metadata, err := m.runtime.backups.Get(ctx, backupID)
	if errors.Is(err, backupstore.ErrNotFound) {
		state, code = "deleted", ""
	} else if err != nil {
		return err
	} else if metadata.Kind != backupstore.KindGenerated || metadata.State != backupstore.StateReady || !metadata.Verified || metadata.Digest != digest {
		return ErrConflict
	}
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ErrUnavailable
	}
	defer rollbackRestore(tx)
	tag, err := tx.Exec(ctx, `UPDATE backup_policy_runs SET state=$5,error_code=$6,updated_at=clock_timestamp() WHERE run_id=$1 AND backup_id=$2 AND sha256=$3 AND deployment_id=$4 AND state='deleting'`, runID, backupID, digest, deploymentID, state, code)
	if err != nil {
		return ErrUnavailable
	}
	if tag.RowsAffected() == 1 && state == "deleted" {
		if err := activity.Record(ctx, tx, automaticBackupEvent(activity.ActionBackupDeleted, backupID, "")); err != nil {
			return ErrUnavailable
		}
	}
	if !m.lease.Protects(m.pool) {
		return ErrUnavailable
	}
	return tx.Commit(ctx)
}

// ReconcileAutomaticBackups runs before scheduler startup. It never regenerates
// an uncertain archive. Only matching already-published bytes are completed;
// incomplete calculations are interrupted, and deletion receipts bind exact
// deployment, object id and digest before any filesystem mutation resumes.
func (m *Manager) ReconcileAutomaticBackups(ctx context.Context) error {
	if m == nil || m.operator || m.pool == nil {
		return nil
	}
	if !m.lease.Protects(m.pool) {
		return ErrUnavailable
	}
	var exists bool
	if err := m.pool.QueryRow(ctx, `SELECT to_regclass('backup_policy_runs') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		return err
	}
	m.mu.Lock()
	deploymentID := m.data.DeploymentID
	m.mu.Unlock()
	rows, err := m.pool.Query(ctx, `SELECT run_id,backup_id,sha256,state FROM backup_policy_runs WHERE deployment_id=$1 AND state IN ('running','publishing','deleting') ORDER BY created_at,run_id`, deploymentID)
	if err != nil {
		return ErrUnavailable
	}
	type retained struct{ run, id, digest, state string }
	var items []retained
	for rows.Next() {
		var item retained
		if rows.Scan(&item.run, &item.id, &item.digest, &item.state) != nil {
			rows.Close()
			return ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		rows.Close()
		return ErrUnavailable
	}
	rows.Close()
	for _, item := range items {
		state, code := "interrupted", "operation_interrupted"
		metadata, getErr := m.runtime.backups.Get(ctx, item.id)
		if item.state == "deleting" {
			if getErr == nil && metadata.Kind == backupstore.KindGenerated && metadata.State == backupstore.StateReady && metadata.Verified && metadata.Digest == item.digest {
				getErr = m.runtime.backups.Delete(ctx, item.id, item.digest)
			} else if getErr == nil {
				return ErrConflict
			}
			if errors.Is(getErr, backupstore.ErrNotFound) || getErr == nil {
				state, code = "deleted", ""
			} else if errors.Is(getErr, backupstore.ErrBusy) {
				state, code = "completed", "retention_deferred"
			} else {
				return ErrUnavailable
			}
		} else if item.state == "publishing" && getErr == nil && metadata.Kind == backupstore.KindGenerated && metadata.State == backupstore.StateReady && metadata.Verified && metadata.Digest == item.digest {
			if err := m.recordBackupPublished(ctx, item.id, metadata.Digest, metadata.Size); err != nil {
				return err
			}
			state, code = "completed", ""
		}
		if _, err = m.pool.Exec(ctx, `UPDATE backup_policy_runs SET state=$2,error_code=$3,updated_at=clock_timestamp() WHERE run_id=$1 AND deployment_id=$4 AND state=$5`, item.run, state, code, deploymentID, item.state); err != nil {
			return ErrUnavailable
		}
	}
	return nil
}

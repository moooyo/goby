package recovery

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/identity"
)

func (m *Manager) Create(ctx context.Context, actor identity.Principal, request CreateRequest) (OperationView, error) {
	if ValidatePassphrase(request.Passphrase) != nil || !hexID(request.RequestId, 32) {
		return OperationView{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor); err != nil {
		return OperationView{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthyLocked(); err != nil {
		return OperationView{}, err
	}
	if m.engine == nil {
		return OperationView{}, ErrUnavailable
	}
	fingerprint := requestFingerprint(struct{ Kind, Generation string }{"create", m.current.Digest})
	if existing, err := m.findRequestLocked(request.RequestId, "create", fingerprint, actor.User.ID); err != nil || existing != nil {
		if err != nil {
			return OperationView{}, err
		}
		return operationView(*existing), nil
	}
	op, err := m.newOperationLocked(actor, request.RequestId, "create", fingerprint)
	if err != nil {
		return OperationView{}, err
	}
	writer, err := m.runtime.backups.Begin(m.ctx, backupstore.BeginOptions{Kind: backupstore.KindGenerated, CreatorID: actor.User.ID, SessionID: actor.SessionID})
	if err != nil {
		m.data.Operations = m.data.Operations[:len(m.data.Operations)-1]
		return OperationView{}, err
	}
	op.BackupID = writer.Metadata().ID
	if err := m.persistLocked(ctx); err != nil {
		abortWriter(writer, backupstore.CodeInterrupted)
		return OperationView{}, err
	}
	if err := m.grantLocked(ctx, actor, op, activity.ActionBackupRequested); err != nil {
		abortWriter(writer, backupstore.CodeInterrupted)
		m.admissionFailedLocked(op, err)
		return OperationView{}, err
	}
	op.Authorized = true
	m.changeLocked(op, "running", "snapshot", "")
	if err := m.persistLocked(ctx); err != nil {
		abortWriter(writer, backupstore.CodeInterrupted)
		return OperationView{}, err
	}
	id := op.ID
	passphrase := bytes.Clone(request.Passphrase)
	m.startLocked(id, func(work context.Context) {
		defer clear(passphrase)
		defer writer.Close()
		manifest, err := m.engine.Create(work, writer, passphrase)
		if err != nil {
			abortWriter(writer, writerErrorCode(err))
			m.failJob(id, err)
			return
		}
		proof, err := writer.Prepare(work)
		if err != nil {
			abortWriter(writer, writerErrorCode(err))
			m.failJob(id, err)
			return
		}
		summary := Summary(manifest)
		source := summaryView(&summary)
		source.ServerName = m.cfg.ServerName
		if err := m.updateOperation(id, "running", "publication", "", func(op *operationRecord) {
			op.Digest, op.Size, op.Manifest, op.Source = proof.Digest, proof.Size, &manifest, source
		}); err != nil {
			return
		}
		m.publishPrepared(work, id, writer, proof, &summary)
	})
	return operationView(*op), nil
}

func (m *Manager) admissionFailedLocked(op *operationRecord, cause error) {
	m.changeLocked(op, "failed", "admission", errorCode(cause))
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.persistLocked(cleanup)
}

func writerErrorCode(err error) backupstore.ErrorCode {
	switch {
	case errors.Is(err, context.Canceled):
		return backupstore.CodeInterrupted
	case errors.Is(err, backupstore.ErrQuota):
		return backupstore.CodeQuota
	default:
		return backupstore.CodeGeneration
	}
}

func abortWriter(writer *backupstore.Writer, code backupstore.ErrorCode) {
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = writer.Abort(cleanup, code)
}

func (m *Manager) publishPrepared(ctx context.Context, id string, writer *backupstore.Writer, proof backupstore.Prepared, summary *backupstore.SourceSummary) {
	op, err := m.operationCopy(id)
	if err != nil || !op.Authorized {
		return
	}
	// Prepared bytes are already durable and bound to the operation. This
	// completion fact authorizes publication; a failed publication is replayed
	// from the same digest, never replaced with newly generated ciphertext.
	if err := m.recordSystem(ctx, op, activity.ActionBackupFinished, activity.StateCompleted); err != nil {
		// Preserve the prepared phase for exact receipt inspection on restart.
		_ = m.updateOperation(id, "interrupted", "publication", "audit_unavailable", nil)
		return
	}
	if _, err := writer.Publish(ctx, proof, summary); err != nil {
		_ = m.updateOperation(id, "interrupted", "publication", "storage_unavailable", nil)
		return
	}
	_ = m.updateOperation(id, "completed", "finished", "", func(op *operationRecord) { op.Manifest = nil })
}

// Import consumes the complete caller-owned body before returning. Its
// publication can continue after the HTTP response, but no worker retains the
// request body. Cancellation closes the body to unblock an outstanding Read.
func (m *Manager) Import(ctx context.Context, actor identity.Principal, requestID string, input io.ReadCloser) (OperationView, error) {
	if input == nil || !hexID(requestID, 32) {
		return OperationView{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor); err != nil {
		return OperationView{}, err
	}
	m.mu.Lock()
	if err := m.healthyLocked(); err != nil {
		m.mu.Unlock()
		return OperationView{}, err
	}
	fingerprint := requestFingerprint(struct{ Kind, Generation string }{"import", m.current.Digest})
	if existing, err := m.findRequestLocked(requestID, "import", fingerprint, actor.User.ID); err != nil || existing != nil {
		if err != nil {
			m.mu.Unlock()
			return OperationView{}, err
		}
		view := operationView(*existing)
		m.mu.Unlock()
		return view, nil
	}
	op, err := m.newOperationLocked(actor, requestID, "import", fingerprint)
	if err != nil {
		m.mu.Unlock()
		return OperationView{}, err
	}
	writer, err := m.runtime.backups.Begin(m.ctx, backupstore.BeginOptions{Kind: backupstore.KindImported, CreatorID: actor.User.ID, SessionID: actor.SessionID})
	if err != nil {
		m.data.Operations = m.data.Operations[:len(m.data.Operations)-1]
		m.mu.Unlock()
		return OperationView{}, err
	}
	op.BackupID = writer.Metadata().ID
	m.changeLocked(op, "running", "upload", "")
	if err := m.persistLocked(ctx); err != nil {
		m.mu.Unlock()
		abortWriter(writer, backupstore.CodeImport)
		return OperationView{}, err
	}
	id := op.ID
	work, cancel := context.WithTimeout(m.ctx, m.cfg.Recovery.WithDefaults().OperationTimeout)
	job := &activeJob{cancel: cancel, done: make(chan struct{})}
	m.jobs[id] = job
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.jobs, id)
		close(job.done)
		m.mu.Unlock()
	}()
	defer writer.Close()
	stopRequest := context.AfterFunc(ctx, cancel)
	stopRead := context.AfterFunc(work, func() { _ = input.Close() })
	defer stopRequest()
	defer stopRead()
	reader := bufio.NewReaderSize(&contextReader{ctx: work, reader: input}, 64<<10)
	const header = "age-encryption.org/v1\n"
	prefix, err := reader.Peek(len(header))
	if err != nil || string(prefix) != header {
		cause := err
		if cause == nil || errors.Is(cause, io.EOF) || errors.Is(cause, io.ErrUnexpectedEOF) {
			cause = ErrArchive
		}
		if work.Err() != nil {
			cause = work.Err()
		}
		abortWriter(writer, backupstore.CodeImport)
		m.failJob(id, cause)
		return OperationView{}, cause
	}
	maximum := m.cfg.Recovery.WithDefaults().Backups.MaxObjectBytes
	written, err := io.CopyBuffer(writer, io.LimitReader(reader, maximum+1), make([]byte, 64<<10))
	if err == nil && written > maximum {
		err = ErrCapacity
	}
	if err != nil || ctx.Err() != nil || work.Err() != nil {
		if err == nil {
			err = work.Err()
			if err == nil {
				err = ctx.Err()
			}
		}
		abortWriter(writer, backupstore.CodeImport)
		m.failJob(id, err)
		return OperationView{}, err
	}
	proof, err := writer.Prepare(work)
	if err != nil {
		m.failJob(id, err)
		return OperationView{}, err
	}
	m.mu.Lock()
	op = m.operationLocked(id)
	op.Digest, op.Size = proof.Digest, proof.Size
	op.Phase = "publication"
	if err := m.persistLocked(ctx); err != nil {
		m.mu.Unlock()
		return OperationView{}, err
	}
	if err := m.grantLocked(ctx, actor, op, activity.ActionBackupImported); err != nil {
		m.admissionFailedLocked(op, err)
		m.mu.Unlock()
		return OperationView{}, err
	}
	op.Authorized = true
	if err := m.persistLocked(work); err != nil {
		m.mu.Unlock()
		return OperationView{}, err
	}
	m.mu.Unlock()
	// Stop watching the request only after its admission has committed. From
	// this point publication follows the durable grant, even after disconnect.
	stopRequest()
	m.publishPrepared(work, id, writer, proof, nil)
	copy, err := m.operationCopy(id)
	if err != nil {
		return OperationView{}, err
	}
	return operationView(copy), nil
}

func (m *Manager) Delete(ctx context.Context, actor identity.Principal, id string, request DeleteRequest) (OperationView, error) {
	if !hexID(id, 32) || !hexID(request.RequestId, 32) || request.SHA256 != "" && !hexID(request.SHA256, 64) {
		return OperationView{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor); err != nil {
		return OperationView{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthyLocked(); err != nil {
		return OperationView{}, err
	}
	fingerprint := requestFingerprint(struct{ Kind, Id, Digest, Generation string }{"delete", id, request.SHA256, m.current.Digest})
	if existing, err := m.findRequestLocked(request.RequestId, "delete", fingerprint, actor.User.ID); err != nil || existing != nil {
		if err != nil {
			return OperationView{}, err
		}
		return operationView(*existing), nil
	}
	metadata, err := m.runtime.backups.Get(ctx, id)
	if err != nil {
		return OperationView{}, err
	}
	if metadata.Digest != request.SHA256 {
		return OperationView{}, ErrConflict
	}
	op, err := m.newOperationLocked(actor, request.RequestId, "delete", fingerprint)
	if err != nil {
		return OperationView{}, err
	}
	op.BackupID, op.Digest, op.Size = id, request.SHA256, metadata.Size
	if err := m.persistLocked(ctx); err != nil {
		return OperationView{}, err
	}
	if err := m.grantLocked(ctx, actor, op, activity.ActionBackupDeleteRequested); err != nil {
		m.admissionFailedLocked(op, err)
		return OperationView{}, err
	}
	op.Authorized = true
	m.changeLocked(op, "running", "cleanup", "")
	if err := m.persistLocked(ctx); err != nil {
		return OperationView{}, err
	}
	operationID := op.ID
	m.startLocked(operationID, func(work context.Context) { m.deleteJob(work, operationID) })
	return operationView(*op), nil
}

func (m *Manager) deleteJob(ctx context.Context, id string) {
	op, err := m.operationCopy(id)
	if err != nil || !op.Authorized {
		return
	}
	for {
		err = m.runtime.backups.Delete(ctx, op.BackupID, op.Digest)
		if !errors.Is(err, backupstore.ErrBusy) {
			break
		}
		select {
		case <-ctx.Done():
			m.failJob(id, ctx.Err())
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
	if err != nil && !errors.Is(err, backupstore.ErrNotFound) {
		m.failJob(id, err)
		return
	}
	if err := m.recordSystem(ctx, op, activity.ActionBackupDeleted, ""); err != nil {
		_ = m.updateOperation(id, "interrupted", "cleanup", "audit_unavailable", nil)
		return
	}
	_ = m.updateOperation(id, "completed", "finished", "", nil)
}

func (m *Manager) Cancel(ctx context.Context, actor identity.Principal, id, revision string) (OperationView, error) {
	expected, err := parseRevision(revision)
	if err != nil || !hexID(id, 32) {
		return OperationView{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor); err != nil {
		return OperationView{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthyLocked(); err != nil {
		return OperationView{}, err
	}
	op := m.operationLocked(id)
	if op == nil {
		return OperationView{}, ErrNotFound
	}
	if op.CancelAuthorized || terminalOperation(op.State) {
		return operationView(*op), nil
	}
	if op.Revision != expected || !operationView(*op).CanCancel {
		return OperationView{}, ErrConflict
	}
	action := activity.ActionBackupCancelRequested
	if op.Kind == "restore" {
		action = activity.ActionRestoreCancelRequested
	}
	if err := m.grantLocked(ctx, actor, op, action); err != nil {
		return OperationView{}, err
	}
	op.CancelAuthorized = true
	op.Revision++
	op.UpdatedAt = time.Now().UTC()
	if job := m.jobs[id]; job != nil {
		if err := m.persistLocked(ctx); err != nil {
			return OperationView{}, err
		}
		job.cancel()
	} else {
		m.changeLocked(op, "cancelled", "finished", "operation_cancelled")
		if err := m.persistLocked(ctx); err != nil {
			return OperationView{}, err
		}
	}
	return operationView(*op), nil
}

func (m *Manager) Revalidate(ctx context.Context, actor identity.Principal) error {
	return m.authorize(ctx, actor)
}

func (m *Manager) Download(ctx context.Context, actor identity.Principal, id string) (*backupstore.Snapshot, error) {
	if err := m.authorize(ctx, actor); err != nil {
		return nil, err
	}
	snapshot, err := m.runtime.backups.Snapshot(ctx, id)
	if err != nil {
		return nil, err
	}
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		snapshot.Close()
		return nil, ErrUnavailable
	}
	defer rollbackRestore(tx)
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, true); err != nil {
		snapshot.Close()
		return nil, err
	}
	if err := activity.Record(ctx, tx, activity.Event{Action: activity.ActionBackupDownloaded,
		Source: activity.SourceNative, Actor: activity.Actor{Kind: activity.ActorUser, ID: actor.User.ID, CredentialID: actor.SessionID},
		Resource: activity.Resource{Kind: activity.ResourceBackup, ID: id}}); err != nil {
		snapshot.Close()
		return nil, ErrUnavailable
	}
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		snapshot.Close()
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		snapshot.Close()
		return nil, ErrUnavailable
	}
	return snapshot, nil
}

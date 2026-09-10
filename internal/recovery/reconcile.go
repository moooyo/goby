package recovery

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/backupstore"
)

// Reconcile runs before exposing the generation's HTTP listener. It resumes
// only work with a committed admission and sufficient durable byte/data proof.
// A passphrase-dependent interrupted computation is never recreated by guess.
func (m *Manager) Reconcile(ctx context.Context) error {
	m.mu.Lock()
	ids := make([]string, len(m.data.Operations))
	for index, op := range m.data.Operations {
		ids[index] = op.ID
	}
	m.mu.Unlock()
	for _, id := range ids {
		op, err := m.operationCopy(id)
		if err != nil {
			return err
		}
		if op.Authorized && op.State == "failed" && (op.Kind == "restore" || op.Kind == "rollback") &&
			(op.SourceState == m.current || op.FailureGeneration == m.current.Digest) {
			// A failed switch records completion only after the original slot is
			// active again. This must not change a quiescent retained image.
			if err := m.recordSystem(ctx, op, activity.ActionRestoreFailed, activity.StateFailed); err != nil {
				return err
			}
		}
		if op.SourceState != m.current {
			// The accepted transition itself belongs to the previous generation.
			// Its new-generation receipt is handled by AcceptSwitch, not by the
			// old database's admission lookup.
			if terminalOperation(op.State) {
				continue
			}
			m.mu.Lock()
			transition := m.data.Transition
			owned := transition != nil && transition.OperationID == id && transition.After == m.current
			m.mu.Unlock()
			if owned {
				continue
			}
			if err := m.updateOperation(id, "interrupted", "finished", "source_changed", nil); err != nil {
				return err
			}
			continue
		}
		if !op.Authorized {
			granted, err := m.hasGrant(ctx, op, admissionAction(op.Kind), true)
			if err != nil {
				return err
			}
			if !granted {
				if !terminalOperation(op.State) {
					if err := m.updateOperation(id, "interrupted", "finished", "operation_interrupted", nil); err != nil {
						return err
					}
				}
				continue
			}
			if err := m.updateOperation(id, op.State, op.Phase, op.ErrorCode, func(current *operationRecord) { current.Authorized = true }); err != nil {
				return err
			}
			op, _ = m.operationCopy(id)
		}
		if !op.ApplyAuthorized && !op.CancelAuthorized && !terminalOperation(op.State) &&
			(op.Kind == "create" || op.Kind == "import" || op.Kind == "restore") {
			action := activity.ActionBackupCancelRequested
			if op.Kind == "restore" {
				action = activity.ActionRestoreCancelRequested
			}
			granted, err := m.hasGrant(ctx, op, action, false)
			if err != nil {
				return err
			}
			if granted {
				op.CancelAuthorized = true
				if err := m.updateOperation(id, op.State, op.Phase, op.ErrorCode, func(current *operationRecord) {
					current.CancelAuthorized = true
				}); err != nil {
					return err
				}
			}
		}
		if op.CancelAuthorized && !op.ApplyAuthorized && !terminalOperation(op.State) {
			if op.Kind == "create" || op.Kind == "import" {
				if err := m.recordSystem(ctx, op, activity.ActionBackupFinished, activity.StateCancelled); err != nil {
					return err
				}
			}
			if err := m.updateOperation(id, "cancelled", "finished", "operation_cancelled", nil); err != nil {
				return err
			}
			continue
		}
		if op.Kind == "restore" && op.State == "ready" && !op.ApplyAuthorized {
			granted, err := m.hasGrant(ctx, op, activity.ActionRestoreApplyRequested, false)
			if err != nil {
				return err
			}
			if granted {
				m.mu.Lock()
				current := m.operationLocked(id)
				current.ApplyAuthorized = true
				m.changeLocked(current, "applying", "activation", "")
				m.data.Transition = &transitionRecord{OperationID: id, Phase: "requested", Before: m.current}
				err = m.persistLocked(ctx)
				m.mu.Unlock()
				if err != nil {
					return err
				}
				op, _ = m.operationCopy(id)
			}
		}
		if op.ApplyAuthorized && op.State == "applying" {
			select {
			case m.switches <- id:
			default:
			}
			continue
		}
		if op.State == "ready" || terminalOperation(op.State) && op.Phase != "publication" && op.Phase != "cleanup" {
			continue
		}
		if op.Kind == "create" || op.Kind == "import" {
			if err := m.reconcilePublication(ctx, op); err != nil {
				return err
			}
			continue
		}
		if op.Kind == "delete" {
			m.mu.Lock()
			m.startLocked(id, func(work context.Context) { m.deleteJob(work, id) })
			m.mu.Unlock()
			continue
		}
		if op.Kind == "restore" && op.Target != nil {
			planned, err := m.hasSystem(ctx, op, activity.ActionRestorePlanned, "")
			if err != nil {
				return err
			}
			if planned {
				if err := m.updateOperation(id, "ready", "ready", "", nil); err != nil {
					return err
				}
				continue
			}
		}
		if err := m.updateOperation(id, "interrupted", "finished", "operation_interrupted", nil); err != nil {
			return err
		}
	}
	return nil
}

func admissionAction(kind string) activity.Action {
	switch kind {
	case "create":
		return activity.ActionBackupRequested
	case "import":
		return activity.ActionBackupImported
	case "delete":
		return activity.ActionBackupDeleteRequested
	case "restore":
		return activity.ActionRestoreRequested
	case "rollback":
		return activity.ActionRestoreRollbackRequested
	default:
		return ""
	}
}

func (m *Manager) hasGrant(ctx context.Context, op operationRecord, action activity.Action, initialActor bool) (bool, error) {
	if op.Operator {
		switch action {
		case activity.ActionBackupCancelRequested, activity.ActionRestoreCancelRequested:
			return op.CancelAuthorized, nil
		case activity.ActionRestoreApplyRequested:
			return op.ApplyAuthorized, nil
		default:
			return op.Authorized, nil
		}
	}
	if m.operator {
		// A disconnected original database cannot settle an uncertain native
		// transaction. Only the already persisted authority may be used.
		return false, nil
	}
	var found bool
	err := m.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM activity_entries
		WHERE action=$1 AND resource_id=$2 AND resource_kind=$3 AND source='native' AND actor_kind='user'
		AND actor_id<>'' AND actor_credential_id<>'' AND state='' AND revision=0 AND affected_count=0
		AND cardinality(changed_fields)=0 AND (NOT $4 OR (actor_id=$5 AND actor_credential_id=$6)))`,
		string(action), op.ID, string(operationResource(op.Kind)), initialActor, op.ActorID, op.CredentialID).Scan(&found)
	if err != nil {
		return false, ErrUnavailable
	}
	return found, nil
}

func (m *Manager) hasSystem(ctx context.Context, op operationRecord, action activity.Action, state activity.State) (bool, error) {
	if m.operator || op.Operator {
		if action == activity.ActionRestorePlanned {
			return op.Target != nil && op.State == "ready", nil
		}
		if action == activity.ActionBackupFinished {
			return op.Authorized && op.Phase == "publication" && hexID(op.Digest, 64) && op.Size > 0 ||
				op.State == "completed", nil
		}
		return false, nil
	}
	var found bool
	err := m.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM activity_entries
		WHERE action=$1 AND resource_id=$2 AND resource_kind=$3 AND source='system' AND actor_kind='system'
		AND actor_id='' AND actor_credential_id='' AND state=$4 AND revision=0 AND affected_count=0
		AND cardinality(changed_fields)=0)`, string(action), op.ID, string(operationResource(op.Kind)), string(state)).Scan(&found)
	if err != nil {
		return false, ErrUnavailable
	}
	return found, nil
}

func (m *Manager) reconcilePublication(ctx context.Context, op operationRecord) error {
	var state string
	var err error
	if m.operator && !op.Operator {
		return m.updateOperation(op.ID, "interrupted", "finished", "operation_interrupted", nil)
	}
	if op.Operator {
		err = pgx.ErrNoRows
	} else {
		err = m.pool.QueryRow(ctx, `SELECT state FROM activity_entries WHERE action=$1 AND resource_id=$2`,
			string(activity.ActionBackupFinished), op.ID).Scan(&state)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ErrUnavailable
	}
	if err == nil && state != string(activity.StateCompleted) {
		return m.updateOperation(op.ID, state, "finished", "operation_interrupted", nil)
	}
	metadata, getErr := m.runtime.backups.Get(ctx, op.BackupID)
	if getErr == nil && metadata.State == backupstore.StateReady {
		complete, err := m.hasSystem(ctx, op, activity.ActionBackupFinished, activity.StateCompleted)
		if err != nil || !complete || metadata.Digest != op.Digest || metadata.Size != op.Size {
			return ErrConflict
		}
		return m.updateOperation(op.ID, "completed", "finished", "", func(op *operationRecord) { op.Manifest = nil })
	}
	if op.Phase != "publication" || !hexID(op.Digest, 64) || op.Size < 1 || op.Kind == "create" && op.Manifest == nil {
		if err := m.recordSystem(ctx, op, activity.ActionBackupFinished, activity.StateInterrupted); err != nil {
			return err
		}
		return m.updateOperation(op.ID, "interrupted", "finished", "operation_interrupted", nil)
	}
	writer, err := m.runtime.backups.ReopenPrepared(ctx, op.BackupID, op.Digest)
	if err != nil {
		return err
	}
	defer writer.Close()
	proof, err := writer.Prepare(ctx)
	if err != nil || proof.Size != op.Size {
		return ErrConflict
	}
	var summary *backupstore.SourceSummary
	if op.Manifest != nil {
		value := Summary(*op.Manifest)
		summary = &value
	}
	m.publishPrepared(ctx, op.ID, writer, proof, summary)
	return nil
}

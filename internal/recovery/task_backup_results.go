package recovery

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

// TaskBackupResultView contains only the result associated with one exact
// automatic task run. Missing metadata is omitted, including after deletion.
// No source, actor, deployment, credential, or filesystem details are exposed.
type TaskBackupResultView struct {
	Status    string
	BackupId  string `json:"BackupId,omitempty"`
	State     string `json:"State,omitempty"`
	SizeBytes string `json:"SizeBytes,omitempty"`
	SHA256    string `json:"SHA256,omitempty"`
	Verified  *bool  `json:"Verified,omitempty"`
}

type taskBackupAssociation struct {
	runID, backupID, deploymentID, state, digest string
}

// TaskBackupResults projects existing run evidence without creating or
// reconciling backups. Both SQL rounds are bounded by the supplied exact IDs.
func (m *Manager) TaskBackupResults(ctx context.Context, actor identity.Principal, runIDs []string) (map[string]TaskBackupResultView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(runIDs) > tasks.MaxPageLimit {
		return nil, ErrInvalid
	}
	for _, id := range runIDs {
		if !hexID(id, 32) {
			return nil, ErrInvalid
		}
	}
	if err := m.authorize(ctx, actor); err != nil {
		return nil, taskBackupReadError(err)
	}
	m.mu.Lock()
	if m.operator || m.runtime == nil || m.runtime.backups == nil || m.backupPolicy == nil || m.backupPolicy.owner == nil {
		m.mu.Unlock()
		return nil, ErrUnavailable
	}
	if err := m.healthyLocked(ctx); err != nil {
		m.mu.Unlock()
		return nil, taskBackupReadError(err)
	}
	current, deploymentID, executor := m.current, m.data.DeploymentID, m.backupPolicy
	owner, storage := executor.owner, m.runtime.backups
	m.mu.Unlock()
	if len(runIDs) == 0 {
		return map[string]TaskBackupResultView{}, nil
	}

	initial, err := readTaskBackupAssociations(ctx, owner, actor, runIDs)
	if err != nil {
		return nil, taskBackupReadError(err)
	}
	results := make(map[string]TaskBackupResultView, len(initial))
	for id, association := range initial {
		view, needsMetadata := taskBackupAssociationStatus(association, deploymentID)
		if needsMetadata {
			metadata, readErr := storage.Get(ctx, association.backupID)
			view = taskBackupMetadataResult(association, deploymentID, metadata, readErr)
		}
		results[id] = view
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Retention may change a record while storage is read. Confirm the exact
	// association again instead of attributing an earlier object's metadata to
	// a new or changed association. The owner callback never takes m.mu.
	confirmed, err := readTaskBackupAssociations(ctx, owner, actor, runIDs)
	if err != nil {
		return nil, taskBackupReadError(err)
	}
	for id, first := range initial {
		last, exists := confirmed[id]
		if exists && first == last {
			continue
		}
		view := TaskBackupResultView{Status: "unavailable"}
		if exists {
			if latest, needsMetadata := taskBackupAssociationStatus(last, deploymentID); !needsMetadata {
				view = latest
			}
		}
		results[id] = view
	}

	// The metadata observation is independent of the owner transaction. A
	// revoked or expired administrator cannot receive its previously read data.
	if err := m.authorize(ctx, actor); err != nil {
		return nil, taskBackupReadError(err)
	}
	m.mu.Lock()
	err = m.healthyLocked(ctx)
	if err == nil && (m.current != current || m.data.DeploymentID != deploymentID || m.backupPolicy != executor) {
		err = ErrConflict
	}
	m.mu.Unlock()
	if err != nil {
		return nil, taskBackupReadError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

type taskBackupAuthorizationTx struct{ tx library.OwnedTx }

func (adapter taskBackupAuthorizationTx) QueryRow(_ context.Context, statement string, args ...any) pgx.Row {
	return adapter.tx.QueryRow(statement, args...)
}

func readTaskBackupAssociations(ctx context.Context, owner library.OwnedTransactions, actor identity.Principal, runIDs []string) (map[string]taskBackupAssociation, error) {
	requested := make(map[string]bool, len(runIDs))
	for _, id := range runIDs {
		requested[id] = true
	}
	result := make(map[string]taskBackupAssociation, len(runIDs))
	err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		authority := taskBackupAuthorizationTx{tx: tx}
		if err := identity.CheckAdministrator(ctx, authority, actor, identity.AdministratorNative, false); err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT r.id,COALESCE(b.backup_id,''),COALESCE(b.deployment_id,''),COALESCE(b.state,''),COALESCE(b.sha256,'')
			FROM task_runs r JOIN task_definitions d ON d.id=r.task_id
			LEFT JOIN backup_policy_runs b ON b.run_id=r.id
			WHERE r.id=ANY($1::text[]) AND r.task_key=$2 AND d.key=$2 LIMIT $3`, runIDs, BackupTaskKey, tasks.MaxPageLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var association taskBackupAssociation
			if err := rows.Scan(&association.runID, &association.backupID, &association.deploymentID, &association.state, &association.digest); err != nil {
				return err
			}
			if !requested[association.runID] || len(result) >= len(requested) {
				return ErrConflict
			}
			if _, exists := result[association.runID]; exists {
				return ErrConflict
			}
			result[association.runID] = association
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		if err := ctx.Err(); err != nil {
			return err
		}
		return identity.CheckAdministrator(ctx, authority, actor, identity.AdministratorNative, false)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func taskBackupReadError(err error) error {
	if errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, identity.ErrInvalidCredentials) || errors.Is(err, identity.ErrClientSessionForbidden) {
		return err
	}
	return preserveContextError(err, ErrUnavailable)
}

func taskBackupAssociationStatus(association taskBackupAssociation, deploymentID string) (TaskBackupResultView, bool) {
	if association.backupID == "" {
		return TaskBackupResultView{Status: "not_recorded"}, false
	}
	if deploymentID == "" || association.deploymentID != deploymentID || !hexID(association.backupID, 32) || association.digest != "" && !hexID(association.digest, 64) {
		return TaskBackupResultView{Status: "mismatch"}, false
	}
	if association.digest == "" && (association.state == "publishing" || association.state == "completed" || association.state == "deleting" || association.state == "deleted") {
		return TaskBackupResultView{Status: "mismatch"}, false
	}
	view := TaskBackupResultView{BackupId: association.backupID}
	switch association.state {
	case "deleted", "deleting":
		view.Status, view.State = association.state, association.state
		return view, false
	case "running", "publishing", "completed", "failed", "interrupted":
		return view, true
	default:
		return TaskBackupResultView{Status: "mismatch"}, false
	}
}

func taskBackupMetadataResult(association taskBackupAssociation, deploymentID string, metadata backupstore.Metadata, err error) TaskBackupResultView {
	view, needsMetadata := taskBackupAssociationStatus(association, deploymentID)
	if !needsMetadata {
		return view
	}
	if err != nil {
		view.Status = "unavailable"
		if errors.Is(err, backupstore.ErrNotFound) {
			view.Status = "missing"
		}
		return view
	}
	if metadata.ID != association.backupID || metadata.Kind != backupstore.KindGenerated || metadata.CreatorID != "" || metadata.SessionID != "" || metadata.Size < 0 ||
		metadata.Digest != "" && !hexID(metadata.Digest, 64) ||
		metadata.Digest != "" && metadata.Digest != association.digest ||
		metadata.Verified && (metadata.State != backupstore.StateReady || metadata.Digest == "" || association.digest == "") {
		return TaskBackupResultView{Status: "mismatch"}
	}
	switch metadata.State {
	case backupstore.StateWriting, backupstore.StateReady, backupstore.StateFailed, backupstore.StateCancelled, backupstore.StateInterrupted:
	default:
		return TaskBackupResultView{Status: "mismatch"}
	}
	if association.digest != "" && metadata.Digest == "" && metadata.State != backupstore.StateWriting {
		return TaskBackupResultView{Status: "mismatch"}
	}
	verified := metadata.Verified
	view.Status, view.State, view.SizeBytes, view.SHA256, view.Verified = "available", string(metadata.State), strconv.FormatInt(metadata.Size, 10), metadata.Digest, &verified
	return view
}

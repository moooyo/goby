package recovery

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/recoverydb"
)

func (m *Manager) Plan(ctx context.Context, actor identity.Principal, request PlanRequest) (OperationView, error) {
	expected, err := parseRevision(request.GenerationRevision)
	if err != nil || !hexID(request.RequestId, 32) || !hexID(request.BackupId, 32) || !hexID(request.SHA256, 64) || ValidatePassphrase(request.Passphrase) != nil {
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
	if m.engine == nil || m.cfg.Recovery.DatabaseURL == "" {
		return OperationView{}, ErrUnavailable
	}
	if expected != m.current.Revision {
		return OperationView{}, ErrConflict
	}
	fingerprint := requestFingerprint(struct {
		Kind, Backup, Digest, Generation string
		Defaults, Replace                bool
	}{"restore", request.BackupId, request.SHA256, m.current.Digest, request.RestoreDefaults, request.ReplaceRollback})
	if existing, err := m.findRequestLocked(request.RequestId, "restore", fingerprint, actor.User.ID); err != nil || existing != nil {
		if err != nil {
			return OperationView{}, err
		}
		return operationView(*existing), nil
	}
	metadata, err := m.runtime.backups.Get(ctx, request.BackupId)
	if err != nil {
		return OperationView{}, err
	}
	if string(metadata.State) != "ready" || metadata.Digest != request.SHA256 {
		return OperationView{}, ErrConflict
	}
	targetSlot := otherSlot(m.current.DatabaseSlot)
	previous := m.slotLocked(targetSlot)
	if previous.State != "empty" && previous.State != "unclaimed" && !request.ReplaceRollback {
		return OperationView{}, ErrConflict
	}
	op, err := m.newOperationLocked(actor, request.RequestId, "restore", fingerprint)
	if err != nil {
		return OperationView{}, err
	}
	op.BackupID, op.Digest, op.Size = request.BackupId, request.SHA256, metadata.Size
	op.RestoreDefaults, op.ReplaceRollback, op.TargetSlot = request.RestoreDefaults, request.ReplaceRollback, targetSlot
	op.GenerationID, err = randomOperationID()
	if err != nil {
		m.data.Operations = m.data.Operations[:len(m.data.Operations)-1]
		return OperationView{}, err
	}
	if err := m.persistLocked(ctx); err != nil {
		return OperationView{}, err
	}
	if err := m.grantLocked(ctx, actor, op, activity.ActionRestoreRequested); err != nil {
		m.admissionFailedLocked(op, err)
		return OperationView{}, err
	}
	op.Authorized = true
	m.changeLocked(op, "running", "validation", "")
	if err := m.persistLocked(ctx); err != nil {
		return OperationView{}, err
	}
	id, passphrase := op.ID, bytes.Clone(request.Passphrase)
	m.startLocked(id, func(work context.Context) {
		defer clear(passphrase)
		if err := m.planJob(work, id, passphrase); err != nil {
			m.failJob(id, err)
		}
	})
	return operationView(*op), nil
}

func otherSlot(slot lifecycle.DatabaseSlot) lifecycle.DatabaseSlot {
	if slot == lifecycle.DatabasePrimary {
		return lifecycle.DatabaseRecovery
	}
	return lifecycle.DatabasePrimary
}

func (m *Manager) slotLocked(slot lifecycle.DatabaseSlot) *slotRecord {
	for index := range m.data.Slots {
		if m.data.Slots[index].Slot == slot {
			return &m.data.Slots[index]
		}
	}
	return nil
}

func (m *Manager) targetConfig() config.Config {
	result := m.cfg
	result.DatabaseURL, result.Recovery.DatabaseURL = m.cfg.Recovery.DatabaseURL, m.cfg.DatabaseURL
	return result
}

func (m *Manager) openSlot(ctx context.Context, cfg config.Config, slot lifecycle.DatabaseSlot) (*pgxpool.Pool, *database.Lease, *recoverydb.Store, error) {
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, nil, ErrUnavailable
	}
	lease, err := database.AcquireLease(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, nil, nil, err
	}
	options := m.engine.options
	options.SourceURL = cfg.DatabaseURL
	store, err := recoverydb.New(pool, lease, recoverydb.Config{Postgres: options, DeploymentID: m.current.DeploymentID, Slot: slot})
	if err != nil {
		lease.Close()
		pool.Close()
		return nil, nil, nil, err
	}
	return pool, lease, store, nil
}

func (m *Manager) planJob(ctx context.Context, id string, passphrase []byte) error {
	op, err := m.operationCopy(id)
	if err != nil || !op.Authorized {
		return ErrUnavailable
	}
	snapshot, err := m.runtime.backups.Snapshot(ctx, op.BackupID)
	if err != nil {
		return err
	}
	defer snapshot.Close()
	archive, err := m.engine.OpenArchive(ctx, snapshot, passphrase)
	if err != nil {
		return err
	}
	defer archive.Close()
	targetCfg := m.targetConfig()
	if op.RestoreDefaults {
		targetCfg, err = archive.Defaults.Apply(targetCfg)
		if err != nil {
			return ErrArchive
		}
	}
	encoded, err := config.EncodeBackupDefaults(targetCfg)
	if err != nil {
		return err
	}
	master := bytes.Clone(archive.Master)
	if len(master) == 0 {
		master = make([]byte, 32)
		if _, err := rand.Read(master); err != nil {
			return ErrUnavailable
		}
	}
	defer clear(master)
	if _, err := m.runtime.lifecycle.StageGeneration(ctx, op.GenerationID, encoded, master); err != nil {
		return err
	}
	summary := Summary(archive.Manifest)
	source := summaryView(&summary)
	source.ServerName = archive.Defaults.ServerName
	if err := m.updateOperation(id, "running", "staging", "", func(op *operationRecord) {
		op.Manifest, op.Source = &archive.Manifest, source
	}); err != nil {
		return err
	}
	pool, lease, binding, err := m.openSlot(ctx, targetCfg, op.TargetSlot)
	if err != nil {
		return err
	}
	defer pool.Close()
	defer lease.Close()
	if err := m.prepareTarget(ctx, binding, op); err != nil {
		return err
	}
	m.mu.Lock()
	slot := m.slotLocked(op.TargetSlot)
	slot.State, slot.ImageID, slot.Operation = "staging", op.GenerationID, id
	slot.Retained, slot.Name, slot.Captured = nil, "", time.Time{}
	if err := m.persistLocked(ctx); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	desired := recoverydb.Marker{Version: 1, DeploymentID: m.current.DeploymentID, GenerationID: op.GenerationID, Slot: op.TargetSlot}
	stamp := func(ctx context.Context, tx pgx.Tx) error {
		var value string
		err := tx.QueryRow(ctx, `SELECT value FROM public.server_settings WHERE key=$1`, recoverydb.MarkerKey).Scan(&value)
		expected := recoverydb.RawMarker{Present: err == nil, Value: value}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ErrUnavailable
		}
		_, err = binding.StampTx(ctx, tx, desired, expected)
		return err
	}
	if _, err := archive.RestoreInto(ctx, pool, lease, targetCfg, stamp); err != nil {
		m.captureFailedStage(binding, op, targetCfg.ServerName)
		return err
	}
	retained, err := binding.Capture(ctx)
	if err != nil {
		return err
	}
	if retained.Marker != desired {
		return ErrConflict
	}
	m.mu.Lock()
	slot = m.slotLocked(op.TargetSlot)
	slot.State, slot.ImageID, slot.Operation = "staged", op.GenerationID, id
	slot.Name, slot.Captured, slot.Retained = targetCfg.ServerName, time.Now().UTC(), &retained
	current := m.operationLocked(id)
	current.Target = &retained
	if err := m.persistLocked(ctx); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	if _, err := m.runtime.backups.Verify(ctx, op.BackupID, op.Digest, summary); err != nil {
		return err
	}
	if err := m.recordSystem(ctx, op, activity.ActionRestorePlanned, ""); err != nil {
		return err
	}
	return m.updateOperation(id, "ready", "ready", "", nil)
}

func (m *Manager) prepareTarget(ctx context.Context, binding *recoverydb.Store, op operationRecord) error {
	if _, err := binding.InspectEmpty(ctx); err == nil {
		return nil
	}
	if !op.ReplaceRollback {
		return ErrConflict
	}
	m.mu.Lock()
	slot := *m.slotLocked(op.TargetSlot)
	previous := m.operationLocked(slot.Operation)
	previousGeneration := ""
	if previous != nil {
		previousGeneration = previous.GenerationID
	}
	m.mu.Unlock()
	if slot.Retained == nil {
		// Only a registered, previously interrupted staging operation can
		// establish this recovery path. A populated unclaimed target cannot.
		if previousGeneration == "" || (slot.State != "staging" && slot.State != "failed") {
			return ErrConflict
		}
		observed, err := binding.Capture(ctx)
		if err != nil || observed.Marker.GenerationID != previousGeneration {
			return ErrConflict
		}
		slot.Retained = &observed
		m.mu.Lock()
		m.slotLocked(op.TargetSlot).Retained = &observed
		err = m.persistLocked(ctx)
		m.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return binding.ResetOwnedTarget(ctx, m.current, *slot.Retained)
}

func (m *Manager) captureFailedStage(binding *recoverydb.Store, op operationRecord, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	retained, captureErr := binding.Capture(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	slot := m.slotLocked(op.TargetSlot)
	slot.State, slot.ImageID, slot.Operation = "failed", op.GenerationID, op.ID
	if captureErr == nil && retained.Marker.GenerationID == op.GenerationID {
		slot.Retained, slot.Captured, slot.Name = &retained, time.Now().UTC(), name
	}
	_ = m.persistLocked(ctx)
}

func (m *Manager) Apply(ctx context.Context, actor identity.Principal, id string, request ApplyRequest) (OperationView, error) {
	revision, err := parseRevision(request.Revision)
	if err != nil || !hexID(id, 32) {
		return OperationView{}, ErrInvalid
	}
	generation, err := parseRevision(request.GenerationRevision)
	if err != nil {
		return OperationView{}, err
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
	if op.ApplyAuthorized {
		return operationView(*op), nil
	}
	if generation != m.current.Revision || op.SourceState != m.current || op.Revision != revision || !operationView(*op).CanApply || m.busyLocked(id) {
		return OperationView{}, ErrConflict
	}
	if err := m.grantLocked(ctx, actor, op, activity.ActionRestoreApplyRequested); err != nil {
		return OperationView{}, err
	}
	op.ApplyAuthorized = true
	m.changeLocked(op, "applying", "activation", "")
	m.data.Transition = &transitionRecord{OperationID: id, Phase: "requested", Before: m.current}
	if err := m.persistLocked(ctx); err != nil {
		return OperationView{}, err
	}
	select {
	case m.switches <- id:
	default:
		return OperationView{}, ErrBusy
	}
	return operationView(*op), nil
}

func (m *Manager) Rollback(ctx context.Context, actor identity.Principal, request RollbackRequest) (OperationView, error) {
	generation, err := parseRevision(request.GenerationRevision)
	if err != nil || !hexID(request.RequestId, 32) {
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
	if generation != m.current.Revision || m.engine == nil {
		return OperationView{}, ErrConflict
	}
	fingerprint := requestFingerprint(struct{ Kind, Generation string }{"rollback", m.current.Digest})
	if existing, err := m.findRequestLocked(request.RequestId, "rollback", fingerprint, actor.User.ID); err != nil || existing != nil {
		if err != nil {
			return OperationView{}, err
		}
		return operationView(*existing), nil
	}
	slot := m.slotLocked(otherSlot(m.current.DatabaseSlot))
	if slot.State != "retained" || slot.Retained == nil || slot.ImageID == "" {
		return OperationView{}, ErrConflict
	}
	op, err := m.newOperationLocked(actor, request.RequestId, "rollback", fingerprint)
	if err != nil {
		return OperationView{}, err
	}
	op.TargetSlot, op.GenerationID, op.Target = slot.Slot, slot.ImageID, slot.Retained
	if err := m.persistLocked(ctx); err != nil {
		return OperationView{}, err
	}
	if err := m.grantLocked(ctx, actor, op, activity.ActionRestoreRollbackRequested); err != nil {
		m.admissionFailedLocked(op, err)
		return OperationView{}, err
	}
	op.Authorized, op.ApplyAuthorized = true, true
	m.changeLocked(op, "applying", "rollback", "")
	m.data.Transition = &transitionRecord{OperationID: op.ID, Phase: "requested", Before: m.current}
	if err := m.persistLocked(ctx); err != nil {
		return OperationView{}, err
	}
	select {
	case m.switches <- op.ID:
	default:
		return OperationView{}, ErrBusy
	}
	return operationView(*op), nil
}

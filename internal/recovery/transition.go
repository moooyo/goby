package recovery

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/recoverycontrol"
	"github.com/moooyo/goby/internal/recoverydb"
)

// SwitchCandidate owns the newly activated database lease and pool. On success
// the caller must transfer both to its generation constructor, or close both.
// PrepareSwitch never closes the retiring application's resources. The caller
// must stop ingress and join application writers before calling it, then close
// the old generation before initializing the candidate. No listener may serve
// the candidate until AcceptSwitch succeeds.
type SwitchCandidate struct {
	Pool        *pgxpool.Pool
	Lease       *database.Lease
	State       lifecycle.State
	OperationID string
}

type switchJournal struct {
	runtime  *Runtime
	data     controlData
	snapshot recoverycontrol.Snapshot
	fault    bool
}

func loadSwitchJournal(ctx context.Context, runtime *Runtime) (*switchJournal, error) {
	if runtime == nil {
		return nil, ErrUnavailable
	}
	data, snapshot, err := readControl(ctx, runtime)
	if err != nil {
		return nil, err
	}
	return &switchJournal{runtime: runtime, data: data, snapshot: snapshot}, nil
}

func (j *switchJournal) save(ctx context.Context) error {
	snapshot, err := writeControl(ctx, j.runtime, j.snapshot, j.data)
	if err != nil {
		j.fault = true
		return ErrUnavailable
	}
	j.snapshot = snapshot
	return nil
}

func (j *switchJournal) operation() *operationRecord {
	if j.data.Transition != nil {
		for i := range j.data.Operations {
			if j.data.Operations[i].ID == j.data.Transition.OperationID {
				return &j.data.Operations[i]
			}
		}
	}
	return nil
}

func (j *switchJournal) slot(slot lifecycle.DatabaseSlot) *slotRecord {
	for i := range j.data.Slots {
		if j.data.Slots[i].Slot == slot {
			return &j.data.Slots[i]
		}
	}
	return nil
}

func (m *Manager) adoptSwitchJournal(j *switchJournal) {
	m.data, m.control = j.data, j.snapshot
	m.fault = m.fault || j.fault
}

// PendingSwitch is called before constructing application components. A false
// activated result requires the source to remain quiescent and PrepareSwitch
// to run. A true result requires ValidateSwitch before startup writers and
// AcceptSwitch before serving. An empty ID means there is no pending switch.
func (m *Manager) PendingSwitch(ctx context.Context) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthyLocked(); err != nil {
		return "", false, err
	}
	j, err := loadSwitchJournal(ctx, m.runtime)
	if err != nil {
		return "", false, err
	}
	defer m.adoptSwitchJournal(j)
	return j.inspect(ctx)
}

// RecoverStartupTransition inspects local publication before any database
// migration or application startup. It resumes only a previously durable
// return intention; it never invents a return because target startup failed.
// An uncertain acceptance always resumes the activated target.
func (r *Runtime) RecoverStartupTransition(ctx context.Context) (string, bool, error) {
	j, err := loadSwitchJournal(ctx, r)
	if err != nil {
		return "", false, err
	}
	if t := j.data.Transition; t != nil && returningPhase(t.Phase) {
		if err := r.ReturnUnaccepted(ctx); err != nil {
			return "", false, err
		}
		return "", false, nil
	}
	return j.inspect(ctx)
}

func returningPhase(phase string) bool {
	return phase == "returning" || strings.HasPrefix(phase, "return_") || phase == "returned"
}

// inspect accepts a lifecycle journal only when all of its candidate fields
// match a previously persisted coordinator intention. A missing journal is
// expected only before Plan or after the acceptance/return Finish boundary.
func (j *switchJournal) inspect(ctx context.Context) (string, bool, error) {
	if t := j.data.Transition; t != nil && t.Phase == "aborting" {
		if err := j.abortBeforePublication(ctx); err != nil {
			return "", false, err
		}
	}
	current, err := j.runtime.lifecycle.Current()
	if err != nil {
		return "", false, err
	}
	pending, err := j.runtime.lifecycle.Pending(ctx)
	if err != nil {
		return "", false, err
	}
	t := j.data.Transition
	if t == nil {
		if pending != nil {
			return "", false, ErrConflict
		}
		return "", false, nil
	}
	op := j.operation()
	if op == nil || !op.Authorized || !op.ApplyAuthorized || op.State != "applying" || op.SourceState != t.Before ||
		op.Target == nil || op.TargetSlot == t.Before.DatabaseSlot || !hexID(op.GenerationID, 32) || returningPhase(t.Phase) {
		return "", false, ErrConflict
	}
	if pending != nil {
		if pending.Before != t.Before || pending.After.GenerationID != op.GenerationID || pending.After.DatabaseSlot != op.TargetSlot ||
			pending.After.Master != t.TargetMaster || t.PlanID != "" && pending.ID != t.PlanID ||
			t.After.Digest != "" && pending.After != t.After || t.TargetAfter == nil {
			return "", false, ErrConflict
		}
		changed := t.PlanID != pending.ID || t.After != pending.After
		t.PlanID, t.After = pending.ID, pending.After
		if pending.Status == lifecycle.PlanActivated {
			if current != t.After {
				return "", false, ErrConflict
			}
			if t.Phase == "planning" || t.Phase == "prepared" {
				t.Phase, changed = "activated", true
			}
		} else {
			if current != t.Before || t.Phase != "planning" && t.Phase != "prepared" {
				return "", false, ErrConflict
			}
			if t.Phase != "prepared" {
				t.Phase, changed = "prepared", true
			}
		}
		if changed {
			if err := j.save(ctx); err != nil {
				return "", false, err
			}
		}
	} else if current == t.Before {
		if t.PlanID != "" || t.After.Digest != "" || t.Phase != "requested" && t.Phase != "retiring" && t.Phase != "rebinding" && t.Phase != "planning" {
			return "", false, ErrConflict
		}
	} else if current != t.After || t.Phase != "accepted" {
		return "", false, ErrConflict
	}
	return t.OperationID, current == t.After, nil
}

// PrepareSwitch captures the fully drained source and acquires the target
// lease before publication. Offline operators have no source proof and no
// automatic return capability. A failed pre-publication preparation is aborted
// only when both durable stores still prove the exact source is current.
// An offline manager cannot resume an already promised online retirement while
// its source is unavailable; that promise is never silently waived.
func (m *Manager) PrepareSwitch(ctx context.Context, id string) (_ *SwitchCandidate, resultErr error) {
	work, cancel := context.WithTimeout(ctx, m.cfg.Recovery.WithDefaults().OperationTimeout)
	stop := context.AfterFunc(m.ctx, cancel)
	defer func() { stop(); cancel() }()
	ctx = work
	// The ready receipt precedes the staging worker's deferred resource
	// cleanup. Join that exact authorized worker without holding the mutex
	// its completion needs; never cancel or race its target lease cleanup.
	m.mu.Lock()
	var done <-chan struct{}
	if job := m.jobs[id]; job != nil {
		op := m.operationLocked(id)
		if len(m.jobs) != 1 || op == nil || !op.ApplyAuthorized {
			m.mu.Unlock()
			return nil, ErrBusy
		}
		done = job.done
	} else if len(m.jobs) != 0 {
		m.mu.Unlock()
		return nil, ErrBusy
	}
	m.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthyLocked(); err != nil {
		return nil, err
	}
	if len(m.jobs) != 0 || m.engine == nil {
		return nil, ErrBusy
	}
	j, err := loadSwitchJournal(ctx, m.runtime)
	if err != nil {
		return nil, err
	}
	defer m.adoptSwitchJournal(j)
	pendingID, activated, err := j.inspect(ctx)
	if err != nil || pendingID != id || activated {
		return nil, ErrConflict
	}
	t, op := j.data.Transition, j.operation()
	if op == nil || t.Before != m.current {
		return nil, ErrConflict
	}
	defer func() {
		if resultErr != nil && !j.fault {
			// Cancellation also has a bounded cleanup opportunity. A failed or
			// uncertain cleanup leaves the intention for restart inspection.
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = j.abortBeforePublication(cleanup)
		}
	}()
	if t.Phase == "requested" {
		t.CanReturn = !m.operator
		if t.CanReturn {
			t.BeforeImage, err = randomOperationID()
			if err != nil {
				return nil, err
			}
		}
		t.Phase = "retiring"
		if err := j.save(ctx); err != nil {
			return nil, err
		}
	}
	if t.CanReturn && t.BeforeRetained == nil {
		retained, master, source, err := m.captureRetiring(ctx)
		if err != nil {
			return nil, err
		}
		defer clear(master)
		encoded, err := config.EncodeBackupDefaults(m.cfg)
		if err != nil {
			return nil, err
		}
		if _, err := m.runtime.lifecycle.StageGeneration(ctx, t.BeforeImage, encoded, master); err != nil {
			return nil, err
		}
		t.BeforeRetained, t.BeforeMaster = &retained, source
		slot := j.slot(t.Before.DatabaseSlot)
		slot.State, slot.ImageID, slot.Retained, slot.Operation = "retained", t.BeforeImage, &retained, op.ID
		slot.Name, slot.Captured = "Retired generation", time.Now().UTC()
		if err := j.save(ctx); err != nil {
			return nil, err
		}
	}
	targetCfg, master, targetMaster, err := m.runtime.imageConfig(ctx, op.TargetSlot, op.GenerationID)
	if err != nil {
		return nil, err
	}
	defer clear(master)
	pool, lease, binding, err := m.runtime.openTransitionSlot(ctx, targetCfg, op.TargetSlot)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			pool.Close()
			_ = lease.Close()
		}
	}()
	bound, finish, err := binding.BoundContext(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	if t.TargetMaster != "" && t.TargetMaster != targetMaster {
		return nil, ErrConflict
	}
	t.TargetMaster = targetMaster
	if t.TargetBefore == nil {
		copy := *op.Target
		t.TargetBefore, t.Phase = &copy, "rebinding"
		if err := j.save(bound); err != nil {
			return nil, err
		}
	}
	if t.Phase == "rebinding" {
		desired := recoverydb.Marker{Version: 1, DeploymentID: t.Before.DeploymentID, GenerationID: op.GenerationID, Slot: op.TargetSlot}
		if err := j.mutateCandidate(bound, pool, lease, binding, t.TargetBefore, &t.TargetAfter, desired, master, targetCfg, op.Kind == "rollback"); err != nil {
			return nil, err
		}
		op.Target = t.TargetAfter
		t.Phase = "planning"
		slot := j.slot(op.TargetSlot)
		slot.State, slot.Retained, slot.ImageID, slot.Operation = "staged", t.TargetAfter, op.GenerationID, op.ID
		if err := j.save(bound); err != nil {
			return nil, err
		}
	}
	actual, err := binding.Capture(bound)
	if err != nil || !sameRetained(&actual, t.TargetAfter) {
		return nil, ErrConflict
	}
	if t.CanReturn {
		binding, _, err := m.runtime.databaseBinding(m.cfg, m.pool, m.lease)
		if err != nil {
			return nil, err
		}
		actual, err := binding.Capture(bound)
		if err != nil || !sameRetained(&actual, t.BeforeRetained) {
			return nil, ErrConflict
		}
	}
	if t.PlanID == "" {
		plan, err := m.runtime.lifecycle.Plan(bound, t.Before, lifecycle.Candidate{GenerationID: op.GenerationID, DatabaseSlot: op.TargetSlot, Master: t.TargetMaster})
		if err != nil {
			return nil, err
		}
		t.PlanID, t.After, t.Phase = plan.ID, plan.After, "prepared"
		if err := j.save(bound); err != nil {
			return nil, err
		}
	}
	if !lease.Protects(pool) || !m.operator && !m.lease.Protects(m.pool) {
		return nil, ErrUnavailable
	}
	after, err := m.runtime.lifecycle.Activate(bound, t.PlanID)
	if err != nil {
		return nil, err
	}
	if after != t.After {
		return nil, ErrConflict
	}
	t.Phase = "activated"
	if err := j.save(bound); err != nil {
		return nil, err
	}
	keep = true
	return &SwitchCandidate{Pool: pool, Lease: lease, State: after, OperationID: op.ID}, nil
}

func (m *Manager) captureRetiring(ctx context.Context) (recoverydb.Retained, []byte, lifecycle.MasterSource, error) {
	if m.operator || m.engine == nil || m.vault == nil || !m.lease.Protects(m.pool) {
		return recoverydb.Retained{}, nil, "", ErrUnavailable
	}
	binding, _, err := m.runtime.databaseBinding(m.cfg, m.pool, m.lease)
	if err != nil {
		return recoverydb.Retained{}, nil, "", err
	}
	bound, finish, err := binding.BoundContext(ctx)
	if err != nil {
		return recoverydb.Retained{}, nil, "", err
	}
	defer finish()
	snapshot, err := backuppg.OpenSnapshot(bound, m.pool, m.engine.options)
	if err != nil {
		return recoverydb.Retained{}, nil, "", err
	}
	defer snapshot.Close()
	if !m.lease.ProtectsTransaction(m.pool, snapshot.Tx()) {
		return recoverydb.Retained{}, nil, "", ErrUnavailable
	}
	inspection, err := backuppg.InspectRecoveryTransaction(snapshot.Context(), snapshot.Tx(), "public")
	if err != nil {
		return recoverydb.Retained{}, nil, "", err
	}
	raw, marker, err := readSwitchMarker(snapshot.Context(), snapshot.Tx())
	if err != nil || marker.DeploymentID != m.current.DeploymentID || marker.Slot != m.current.DatabaseSlot || marker.GenerationID != m.current.GenerationID {
		return recoverydb.Retained{}, nil, "", ErrConflict
	}
	master := make([]byte, 32)
	witness, err := m.vault.WitnessBackup(snapshot.Context(), snapshot.Tx(), master)
	if err != nil {
		clear(master)
		return recoverydb.Retained{}, nil, "", err
	}
	facts, err := snapshot.Facts(bound)
	if err != nil {
		clear(master)
		return recoverydb.Retained{}, nil, "", err
	}
	source := lifecycle.MasterGeneration
	if !witness.HasMasterKey {
		clear(master)
		master, source = nil, lifecycle.MasterDefault
	}
	id := inspection.Identity()
	return recoverydb.Retained{Marker: marker, RawMarker: raw, Database: id.Database, Role: id.Role, Facts: facts}, master, source, nil
}

// mutateCandidate first locks every trusted table and validates the exact
// durable input. Its predicted post-transaction proof is persisted BEFORE SQL
// COMMIT. After a crash only the exact before or after image is admissible.
// Unknown writes cannot be adopted by merely observing a matching marker.
func (j *switchJournal) mutateCandidate(ctx context.Context, pool *pgxpool.Pool, lease *database.Lease, binding *recoverydb.Store,
	before *recoverydb.Retained, after **recoverydb.Retained, desired recoverydb.Marker, master []byte, cfg config.Config, normalize bool) error {
	if before == nil {
		return ErrConflict
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ErrUnavailable
	}
	defer rollbackRestore(tx)
	if !lease.ProtectsTransaction(pool, tx) {
		return ErrUnavailable
	}
	inspection, err := backuppg.InspectRecoveryTransaction(ctx, tx, "public")
	if err != nil {
		return err
	}
	if err := inspection.LockTables(ctx); err != nil {
		return err
	}
	actual, err := captureLocked(ctx, tx, inspection)
	if err != nil {
		return err
	}
	if *after != nil && sameRetained(&actual, *after) {
		return validateSwitchData(ctx, tx, master, cfg)
	}
	if !sameRetained(&actual, before) {
		return ErrConflict
	}
	if normalize {
		if _, err := normalizeRestoredIdentity(ctx, tx, master, cfg); err != nil {
			return err
		}
	} else if err := validateSwitchData(ctx, tx, master, cfg); err != nil {
		return err
	}
	if _, err := binding.StampTx(ctx, tx, desired, actual.RawMarker); err != nil {
		return err
	}
	result, err := captureLocked(ctx, tx, inspection)
	if err != nil {
		return err
	}
	*after = &result
	if err := j.save(ctx); err != nil {
		return err
	}
	if !lease.ProtectsTransaction(pool, tx) {
		return ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrUnavailable
	}
	return nil
}

func captureLocked(ctx context.Context, tx pgx.Tx, inspection *backuppg.RecoveryInspection) (recoverydb.Retained, error) {
	raw, marker, err := readSwitchMarker(ctx, tx)
	if err != nil {
		return recoverydb.Retained{}, err
	}
	facts, err := inspection.Facts(ctx, media.CurrentProbeVersion)
	if err != nil {
		return recoverydb.Retained{}, err
	}
	id := inspection.Identity()
	return recoverydb.Retained{Marker: marker, RawMarker: raw, Database: id.Database, Role: id.Role, Facts: facts}, nil
}

func readSwitchMarker(ctx context.Context, tx pgx.Tx) (recoverydb.RawMarker, recoverydb.Marker, error) {
	var value *string
	if err := tx.QueryRow(ctx, `SELECT CASE WHEN octet_length(value)<=$2 THEN value END FROM public.server_settings WHERE key=$1`,
		recoverydb.MarkerKey, recoverydb.MaxMarkerBytes).Scan(&value); err != nil || value == nil {
		return recoverydb.RawMarker{}, recoverydb.Marker{}, ErrConflict
	}
	marker, err := recoverydb.DecodeMarker(*value)
	if err != nil {
		return recoverydb.RawMarker{}, recoverydb.Marker{}, ErrConflict
	}
	return recoverydb.RawMarker{Present: true, Value: *value}, marker, nil
}

func sameRetained(first, second *recoverydb.Retained) bool {
	if first == nil || second == nil {
		return false
	}
	// Minor PostgreSQL upgrades do not change the PG17 canonical serializer.
	a, b := *first, *second
	a.Facts.PostgreSQLVersion, b.Facts.PostgreSQLVersion = "", ""
	a.Facts.PostgreSQLVersionNum /= 10000
	b.Facts.PostgreSQLVersionNum /= 10000
	x, err := json.Marshal(a)
	y, other := json.Marshal(b)
	return err == nil && other == nil && string(x) == string(y)
}

func validateSwitchData(ctx context.Context, tx pgx.Tx, master []byte, cfg config.Config) error {
	if _, err := identity.ValidateApplicationKeyRecovery(ctx, tx, master); err != nil {
		return err
	}
	if _, err := validateRestoredRoots(ctx, tx, cfg.MediaRoots); err != nil {
		return err
	}
	var administrators int64
	if tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE is_administrator AND NOT is_disabled`).Scan(&administrators) != nil || administrators < 1 {
		return ErrArchive
	}
	return nil
}

// ValidateSwitch performs the one-time exact staged fingerprint check before
// startup writers and persists initializing while the target lease is held.
// A restart in initializing/accepting validates ownership, compiled schema,
// key history, administrators, and approved roots. It deliberately does not
// compare an obsolete pre-startup fingerprint after task recovery has written.
func (m *Manager) ValidateSwitch(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthyLocked(); err != nil || m.operator {
		return ErrUnavailable
	}
	j, err := loadSwitchJournal(ctx, m.runtime)
	if err != nil {
		return err
	}
	defer m.adoptSwitchJournal(j)
	id, activated, err := j.inspect(ctx)
	if err != nil || id == "" || !activated {
		return ErrConflict
	}
	t := j.data.Transition
	if t.After != m.current || t.Phase != "activated" && t.Phase != "initializing" && t.Phase != "accepting" && t.Phase != "accepted" {
		return ErrConflict
	}
	cfg, master, _, err := m.runtime.imageConfig(ctx, m.current.DatabaseSlot, m.current.GenerationID)
	if err != nil {
		return err
	}
	defer clear(master)
	binding, _, err := m.runtime.databaseBinding(m.cfg, m.pool, m.lease)
	if err != nil {
		return err
	}
	bound, finish, err := binding.BoundContext(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if t.Phase == "activated" {
		actual, err := binding.Capture(bound)
		if err != nil || !sameRetained(&actual, t.TargetAfter) {
			return ErrConflict
		}
	}
	tx, err := m.pool.BeginTx(bound, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ErrUnavailable
	}
	defer rollbackRestore(tx)
	if !m.lease.ProtectsTransaction(m.pool, tx) {
		return ErrUnavailable
	}
	if _, err := backuppg.InspectRecoveryTransaction(bound, tx, "public"); err != nil {
		return err
	}
	_, marker, err := readSwitchMarker(bound, tx)
	if err != nil || marker.DeploymentID != m.current.DeploymentID || marker.Slot != m.current.DatabaseSlot || marker.GenerationID != m.current.GenerationID {
		return ErrConflict
	}
	if err := validateSwitchData(bound, tx, master, cfg); err != nil {
		return err
	}
	if t.Phase == "activated" {
		t.Phase = "initializing"
		return j.save(bound)
	}
	return bound.Err()
}

// AcceptSwitch is the irreversible coordinator acceptance boundary. The
// durable accepting phase precedes the target audit commit. Any error from
// this method requires resuming acceptance on the target, never automatic
// return. It is idempotent across a lost database or filesystem commit reply.
func (m *Manager) AcceptSwitch(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthyLocked(); err != nil || m.operator {
		return ErrUnavailable
	}
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	defer func() { stop(); cancel() }()
	ctx = work
	j, err := loadSwitchJournal(ctx, m.runtime)
	if err != nil {
		return err
	}
	defer m.adoptSwitchJournal(j)
	id, activated, err := j.inspect(ctx)
	if err != nil {
		return err
	}
	if id == "" {
		return nil
	}
	t, op := j.data.Transition, j.operation()
	if !activated || t.After != m.current || t.Phase != "initializing" && t.Phase != "accepting" && t.Phase != "accepted" {
		return ErrConflict
	}
	if t.Phase == "initializing" {
		t.Phase = "accepting"
		if err := j.save(ctx); err != nil {
			return err
		}
	}
	if err := m.recordSystem(ctx, *op, activity.ActionRestoreApplied, activity.StateCompleted); err != nil {
		return err
	}
	if t.Phase != "accepted" {
		t.Phase, op.ActivationAccepted = "accepted", true
		if err := j.save(ctx); err != nil {
			return err
		}
	}
	pending, err := m.runtime.lifecycle.Pending(ctx)
	if err != nil {
		return err
	}
	if pending != nil {
		if pending.ID != t.PlanID || pending.After != t.After || pending.Status != lifecycle.PlanActivated {
			return ErrConflict
		}
		if err := m.runtime.lifecycle.Finish(ctx, pending.ID); err != nil {
			return err
		}
	}
	active := j.slot(t.After.DatabaseSlot)
	active.State, active.ImageID, active.Retained, active.Operation = "active", t.After.GenerationID, nil, op.ID
	if !t.CanReturn {
		old := j.slot(t.Before.DatabaseSlot)
		old.State, old.Retained, old.ImageID, old.Operation = "unclaimed", nil, "", ""
	}
	op.ActivationAccepted = true
	op.State, op.Phase, op.ErrorCode, op.UpdatedAt = "completed", "finished", "", time.Now().UTC()
	op.Revision++
	j.data.Transition = nil
	return j.save(ctx)
}

func (j *switchJournal) abortBeforePublication(ctx context.Context) error {
	t := j.data.Transition
	if t == nil || j.fault || returningPhase(t.Phase) || t.Phase == "accepting" || t.Phase == "accepted" {
		return ErrConflict
	}
	current, err := j.runtime.lifecycle.Current()
	if err != nil || current != t.Before {
		return ErrConflict
	}
	pending, err := j.runtime.lifecycle.Pending(ctx)
	if err != nil {
		return err
	}
	op := j.operation()
	if op == nil {
		return ErrConflict
	}
	if pending != nil {
		if op == nil || pending.Before != t.Before || pending.Status != lifecycle.PlanPrepared || pending.After.GenerationID != op.GenerationID ||
			pending.After.DatabaseSlot != op.TargetSlot || pending.After.Master != t.TargetMaster || t.PlanID != "" && t.PlanID != pending.ID {
			return ErrConflict
		}
	}
	if t.Phase != "aborting" {
		t.Phase = "aborting"
		if err := j.save(ctx); err != nil {
			return err
		}
	}
	// A SQL commit may have lost its reply. Preserve only an exactly known
	// image, and keep the abort intention if that result cannot be inspected.
	if t.TargetAfter != nil {
		cfg, master, _, err := j.runtime.imageConfig(ctx, op.TargetSlot, op.GenerationID)
		clear(master)
		if err != nil {
			return err
		}
		pool, lease, binding, err := j.runtime.openTransitionSlot(ctx, cfg, op.TargetSlot)
		if err != nil {
			return err
		}
		defer func() { pool.Close(); _ = lease.Close() }()
		actual, err := binding.Capture(ctx)
		if err != nil || !sameRetained(&actual, t.TargetBefore) && !sameRetained(&actual, t.TargetAfter) {
			return ErrConflict
		}
		target := j.slot(op.TargetSlot)
		target.Retained, target.State, target.Operation = &actual, "failed", op.ID
		op.Target = &actual
	}
	if pending != nil {
		if err := j.runtime.lifecycle.Abort(ctx, pending.ID); err != nil {
			return err
		}
	}
	active := j.slot(t.Before.DatabaseSlot)
	active.State, active.Retained, active.ImageID = "active", nil, t.Before.GenerationID
	op.State, op.Phase, op.ErrorCode, op.UpdatedAt = "failed", "finished", "activation_failed", time.Now().UTC()
	op.Revision++
	j.data.Transition = nil
	return j.save(ctx)
}

func (r *Runtime) imageConfig(ctx context.Context, slot lifecycle.DatabaseSlot, image string) (config.Config, []byte, lifecycle.MasterSource, error) {
	cfg := r.deployment
	if slot == lifecycle.DatabaseRecovery {
		cfg.DatabaseURL, cfg.Recovery.DatabaseURL = cfg.Recovery.DatabaseURL, cfg.DatabaseURL
	} else if slot != lifecycle.DatabasePrimary {
		return config.Config{}, nil, "", ErrInvalid
	}
	files, err := r.lifecycle.ReadGeneration(ctx, image)
	if err != nil {
		return config.Config{}, nil, "", err
	}
	keep := false
	defer func() {
		if !keep {
			clear(files.Master)
		}
	}()
	defaults, err := config.DecodeBackupDefaults(files.Config)
	if err != nil {
		return config.Config{}, nil, "", err
	}
	cfg, err = defaults.Apply(cfg)
	if err != nil {
		return config.Config{}, nil, "", err
	}
	master := lifecycle.MasterDefault
	if len(files.Master) != 0 {
		master = lifecycle.MasterGeneration
		cfg.APIKeyMasterKeyFile, err = r.lifecycle.MasterKeyPath(ctx, image)
		if err != nil {
			return config.Config{}, nil, "", err
		}
	}
	keep = true
	return cfg, files.Master, master, nil
}

func (r *Runtime) openTransitionSlot(ctx context.Context, cfg config.Config, slot lifecycle.DatabaseSlot) (*pgxpool.Pool, *database.Lease, *recoverydb.Store, error) {
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, nil, ErrUnavailable
	}
	lease, err := database.AcquireLease(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, nil, nil, err
	}
	state, err := r.lifecycle.Current()
	if err != nil {
		pool.Close()
		_ = lease.Close()
		return nil, nil, nil, err
	}
	options := cfg.Recovery.WithDefaults()
	binding, err := recoverydb.New(pool, lease, recoverydb.Config{DeploymentID: state.DeploymentID, Slot: slot,
		Postgres: backuppg.Options{SourceURL: cfg.DatabaseURL, Schema: "public", PGDump: options.PGDumpPath, PGRestore: options.PGRestorePath,
			Timeout: options.OperationTimeout, MaxDumpBytes: options.Backups.MaxObjectBytes, ProbeVersion: media.CurrentProbeVersion}})
	if err != nil {
		pool.Close()
		_ = lease.Close()
		return nil, nil, nil, err
	}
	return pool, lease, binding, nil
}

// ReturnSwitch is available while the old manager still owns its source
// resources. All target resources must already be closed. The source manager
// becomes stale after return and must also be closed and reconstructed.
func (m *Manager) ReturnSwitch(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.fault || m.operator || !m.lease.Protects(m.pool) {
		return ErrUnavailable
	}
	j, err := loadSwitchJournal(ctx, m.runtime)
	if err != nil {
		return err
	}
	defer m.adoptSwitchJournal(j)
	if j.data.Transition == nil || j.data.Transition.Before != m.current {
		return ErrConflict
	}
	return j.returnUnaccepted(ctx, m.pool, m.lease)
}

// ReturnUnaccepted may be called only after all target and source application
// handles have been joined and closed. It opens and fences the retained source
// itself. accepting or accepted phases are never eligible, even if their audit
// commit cannot currently be inspected. Returning creates a new revision; it
// never rewinds the lifecycle CAS token or overwrites retained source data.
func (r *Runtime) ReturnUnaccepted(ctx context.Context) error {
	j, err := loadSwitchJournal(ctx, r)
	if err != nil {
		return err
	}
	return j.returnUnaccepted(ctx, nil, nil)
}

func (j *switchJournal) returnUnaccepted(ctx context.Context, suppliedPool *pgxpool.Pool, suppliedLease *database.Lease) error {
	t, op := j.data.Transition, j.operation()
	if t == nil || op == nil || !t.CanReturn || t.BeforeRetained == nil || !hexID(t.BeforeImage, 32) ||
		t.Phase != "activated" && t.Phase != "initializing" && !returningPhase(t.Phase) || op.ActivationAccepted {
		return ErrConflict
	}
	current, err := j.runtime.lifecycle.Current()
	if err != nil {
		return err
	}
	if current != t.After && (t.ReturnState.Digest == "" || current != t.ReturnState) {
		// An Activate reply can be lost before its return state is persisted;
		// the exact local return plan is inspected below before any action.
		pending, err := j.runtime.lifecycle.Pending(ctx)
		if err != nil || pending == nil || !j.matchesReturnPlan(pending) || pending.Status != lifecycle.PlanActivated || pending.After != current {
			return ErrConflict
		}
		t.ReturnPlanID, t.ReturnState = pending.ID, pending.After
		if err := j.save(ctx); err != nil {
			return err
		}
	}
	// Acquiring the failed target's fence proves the previous generation has
	// actually released it. Documentation alone is not a sufficient join check.
	targetCfg, targetMaster, _, err := j.runtime.imageConfig(ctx, t.After.DatabaseSlot, t.After.GenerationID)
	clear(targetMaster)
	if err != nil {
		return err
	}
	targetPool, targetLease, targetBinding, err := j.runtime.openTransitionSlot(ctx, targetCfg, t.After.DatabaseSlot)
	if err != nil {
		return err
	}
	defer func() { targetPool.Close(); _ = targetLease.Close() }()
	ctx, finishTarget, err := targetBinding.BoundContext(ctx)
	if err != nil {
		return err
	}
	defer finishTarget()
	observed, err := targetBinding.Read(ctx)
	if err != nil {
		return err
	}
	marker, err := recoverydb.DecodeMarker(observed.Raw.Value)
	if err != nil || marker.DeploymentID != t.After.DeploymentID || marker.Slot != t.After.DatabaseSlot || marker.GenerationID != t.After.GenerationID {
		return ErrConflict
	}
	cfg, master, masterSource, err := j.runtime.imageConfig(ctx, t.Before.DatabaseSlot, t.BeforeImage)
	if err != nil {
		return err
	}
	defer clear(master)
	if masterSource != t.BeforeMaster {
		return ErrConflict
	}
	pool, lease := suppliedPool, suppliedLease
	var binding *recoverydb.Store
	if pool == nil {
		pool, lease, binding, err = j.runtime.openTransitionSlot(ctx, cfg, t.Before.DatabaseSlot)
		if err != nil {
			return err
		}
		defer func() { pool.Close(); _ = lease.Close() }()
	} else {
		options := cfg.Recovery.WithDefaults()
		binding, err = recoverydb.New(pool, lease, recoverydb.Config{DeploymentID: t.Before.DeploymentID, Slot: t.Before.DatabaseSlot,
			Postgres: backuppg.Options{SourceURL: cfg.DatabaseURL, Schema: "public", PGDump: options.PGDumpPath, PGRestore: options.PGRestorePath,
				Timeout: options.OperationTimeout, MaxDumpBytes: options.Backups.MaxObjectBytes, ProbeVersion: media.CurrentProbeVersion}})
		if err != nil {
			return err
		}
	}
	bound, finish, err := binding.BoundContext(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if !returningPhase(t.Phase) {
		actual, err := binding.Capture(bound)
		if err != nil || !sameRetained(&actual, t.BeforeRetained) {
			return ErrConflict
		}
		t.Phase = "returning"
		if err := j.save(bound); err != nil {
			return err
		}
	}
	if t.Phase == "returning" {
		desired := recoverydb.Marker{Version: 1, DeploymentID: t.Before.DeploymentID, Slot: t.Before.DatabaseSlot, GenerationID: t.BeforeImage}
		if err := j.mutateCandidate(bound, pool, lease, binding, t.BeforeRetained, &t.ReturnTarget, desired, master, cfg, false); err != nil {
			return err
		}
		t.Phase = "return_planning"
		if err := j.save(bound); err != nil {
			return err
		}
	}
	actual, err := binding.Capture(bound)
	if err != nil || !sameRetained(&actual, t.ReturnTarget) {
		return ErrConflict
	}
	pending, err := j.runtime.lifecycle.Pending(bound)
	if err != nil {
		return err
	}
	if pending != nil && pending.ID == t.PlanID {
		if pending.Before != t.Before || pending.After != t.After || pending.Status != lifecycle.PlanActivated {
			return ErrConflict
		}
		if err := j.runtime.lifecycle.Finish(bound, pending.ID); err != nil {
			return err
		}
		pending = nil
	}
	if pending == nil && current == t.After {
		if t.ReturnPlanID != "" {
			return ErrConflict
		}
		plan, err := j.runtime.lifecycle.Plan(bound, t.After, lifecycle.Candidate{GenerationID: t.BeforeImage, DatabaseSlot: t.Before.DatabaseSlot, Master: t.BeforeMaster})
		if err != nil {
			return err
		}
		pending = &plan
	}
	if pending != nil {
		if !j.matchesReturnPlan(pending) {
			return ErrConflict
		}
		t.ReturnPlanID, t.ReturnState, t.Phase = pending.ID, pending.After, "return_prepared"
		if err := j.save(bound); err != nil {
			return err
		}
		after, err := j.runtime.lifecycle.Activate(bound, pending.ID)
		if err != nil || after != t.ReturnState {
			return ErrUnavailable
		}
		t.Phase = "returned"
		if err := j.save(bound); err != nil {
			return err
		}
		if err := j.runtime.lifecycle.Finish(bound, pending.ID); err != nil {
			return err
		}
	} else if current != t.ReturnState || t.Phase != "returned" {
		return ErrConflict
	}
	active := j.slot(t.Before.DatabaseSlot)
	active.State, active.ImageID, active.Retained, active.Operation = "active", t.BeforeImage, nil, op.ID
	failed := j.slot(t.After.DatabaseSlot)
	// Startup may have changed the failed target. Its ownership remains local,
	// but an obsolete proof must not advertise it as an available rollback.
	failed.State, failed.Retained, failed.Operation = "failed", nil, op.ID
	op.State, op.Phase, op.ErrorCode, op.UpdatedAt = "failed", "finished", "activation_failed", time.Now().UTC()
	op.FailureGeneration = t.ReturnState.Digest
	op.Revision++
	j.data.Transition = nil
	return j.save(bound)
}

func (j *switchJournal) matchesReturnPlan(plan *lifecycle.Plan) bool {
	t := j.data.Transition
	return t != nil && plan != nil && t.ReturnTarget != nil && plan.Before == t.After &&
		plan.After.GenerationID == t.BeforeImage && plan.After.DatabaseSlot == t.Before.DatabaseSlot && plan.After.Master == t.BeforeMaster &&
		(t.ReturnPlanID == "" || plan.ID == t.ReturnPlanID) && (t.ReturnState.Digest == "" || plan.After == t.ReturnState)
}

package recovery

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/recoverycontrol"
	"github.com/moooyo/goby/internal/recoverydb"
)

var (
	ErrNotFound = errors.New("recovery resource not found")
	ErrCapacity = errors.New("recovery capacity exceeded")
)

type activeJob struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Manager owns one application generation's workers. Runtime, the source
// lease, and the source pool outlive it. Admission is committed in PostgreSQL
// before a worker may publish, delete, or replace data. The private journal
// records both sides of that boundary so restart can inspect an uncertain
// result without inferring authorization from a filesystem file alone.
type Manager struct {
	mu       sync.Mutex
	runtime  *Runtime
	cfg      config.Config
	pool     *pgxpool.Pool
	lease    *database.Lease
	vault    *identity.ApplicationKeyVault
	engine   *Engine
	current  lifecycle.State
	data     controlData
	control  recoverycontrol.Snapshot
	ctx      context.Context
	cancel   context.CancelFunc
	jobs     map[string]*activeJob
	closed   bool
	fault    bool
	switches chan string
	operator bool
}

func NewManager(ctx context.Context, runtime *Runtime, cfg config.Config, pool *pgxpool.Pool, lease *database.Lease, vault *identity.ApplicationKeyVault, version string) (*Manager, error) {
	if runtime == nil || pool == nil || vault == nil || !lease.Protects(pool) {
		return nil, ErrInvalid
	}
	current, err := runtime.lifecycle.Current()
	if err != nil {
		return nil, err
	}
	data, control, err := readControl(ctx, runtime)
	if err != nil {
		return nil, err
	}
	engine, engineErr := NewEngine(cfg, pool, vault, runtime.backups, version)
	if engineErr != nil && !errors.Is(engineErr, ErrUnavailable) {
		return nil, engineErr
	}
	work, cancel := context.WithCancel(ctx)
	manager := &Manager{runtime: runtime, cfg: cfg, pool: pool, lease: lease, vault: vault,
		engine: engine, current: current, data: data, control: control,
		ctx: work, cancel: cancel, jobs: make(map[string]*activeJob), switches: make(chan string, 1)}
	if len(control.Payload) == 0 {
		if current.Revision != 0 || current.GenerationID != "" {
			cancel()
			return nil, ErrUnavailable
		}
		manager.control, err = writeControl(work, runtime, control, data)
		if err != nil {
			cancel()
			return nil, err
		}
	}
	go func() {
		select {
		case <-lease.Done():
			cancel()
		case <-work.Done():
		}
	}()
	return manager, nil
}

func (m *Manager) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.closed = true
	m.cancel()
	jobs := make([]<-chan struct{}, 0, len(m.jobs))
	for _, job := range m.jobs {
		job.cancel()
		jobs = append(jobs, job.done)
	}
	m.mu.Unlock()
	for _, done := range jobs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
	}
	return nil
}

func (m *Manager) SwitchRequests() <-chan string { return m.switches }

func (m *Manager) healthyLocked() error {
	if m.closed || m.fault || m.ctx.Err() != nil || !m.operator && !m.lease.Protects(m.pool) {
		return ErrUnavailable
	}
	state, err := m.runtime.lifecycle.Current()
	if err != nil || state != m.current {
		return ErrConflict
	}
	return nil
}

func (m *Manager) persistLocked(ctx context.Context) error {
	updated, err := writeControl(ctx, m.runtime, m.control, m.data)
	if err != nil {
		m.fault = true
		return ErrUnavailable
	}
	m.control = updated
	return nil
}

func (m *Manager) operationLocked(id string) *operationRecord {
	for index := range m.data.Operations {
		if m.data.Operations[index].ID == id {
			return &m.data.Operations[index]
		}
	}
	return nil
}

func (m *Manager) busyLocked(except string) bool {
	for _, op := range m.data.Operations {
		if op.ID != except && !terminalOperation(op.State) {
			return true
		}
	}
	return m.data.Transition != nil && m.data.Transition.OperationID != except
}

func (m *Manager) findRequestLocked(id, kind, fingerprint, actorID string) (*operationRecord, error) {
	for index := range m.data.Operations {
		op := &m.data.Operations[index]
		if op.RequestID == id {
			if op.Kind != kind || op.Fingerprint != fingerprint || op.ActorID != actorID || op.Operator != m.operator {
				return nil, ErrConflict
			}
			return op, nil
		}
	}
	return nil, nil
}

func (m *Manager) newOperationLocked(actor identity.Principal, requestID, kind, fingerprint string) (*operationRecord, error) {
	if !hexID(requestID, 32) || !validOperationKind(kind) || !hexID(fingerprint, 64) {
		return nil, ErrInvalid
	}
	if m.busyLocked("") {
		return nil, ErrBusy
	}
	// Idempotency history is retained for at least seven days. A bounded
	// journal rejects admission rather than silently evicting recent requests.
	if len(m.data.Operations) >= maxOperations {
		cutoff := time.Now().Add(-7 * 24 * time.Hour)
		m.data.Operations = slices.DeleteFunc(m.data.Operations, func(op operationRecord) bool {
			for _, slot := range m.data.Slots {
				if slot.Operation == op.ID {
					return false
				}
			}
			return terminalOperation(op.State) && op.UpdatedAt.Before(cutoff) &&
				(m.data.Transition == nil || m.data.Transition.OperationID != op.ID)
		})
	}
	if len(m.data.Operations) >= maxOperations {
		return nil, ErrCapacity
	}
	id, err := randomOperationID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	m.data.Operations = append(m.data.Operations, operationRecord{
		ID: id, RequestID: requestID, Fingerprint: fingerprint, Revision: 1,
		Kind: kind, State: "pending", Phase: "admission", SourceState: m.current,
		ActorID: actor.User.ID, CredentialID: actor.SessionID, Operator: m.operator, CreatedAt: now, UpdatedAt: now,
	})
	return &m.data.Operations[len(m.data.Operations)-1], nil
}

func (m *Manager) changeLocked(op *operationRecord, state, phase, code string) {
	op.State, op.Phase, op.ErrorCode = state, phase, code
	op.Revision++
	op.UpdatedAt = time.Now().UTC()
}

func (m *Manager) updateOperation(id, state, phase, code string, change func(*operationRecord)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	op := m.operationLocked(id)
	if op == nil || m.fault {
		return ErrUnavailable
	}
	if change != nil {
		change(op)
	}
	m.changeLocked(op, state, phase, code)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.persistLocked(ctx)
}

func (m *Manager) startLocked(id string, work func(context.Context)) {
	ctx, cancel := context.WithTimeout(m.ctx, m.cfg.Recovery.WithDefaults().OperationTimeout)
	job := &activeJob{cancel: cancel, done: make(chan struct{})}
	m.jobs[id] = job
	go func() {
		defer func() {
			cancel()
			m.mu.Lock()
			delete(m.jobs, id)
			close(job.done)
			m.mu.Unlock()
		}()
		work(ctx)
	}()
}

func (m *Manager) authorize(ctx context.Context, actor identity.Principal) error {
	if m != nil && m.operator {
		if ctx.Value(operatorAuthority{}) != m {
			return identity.ErrUnauthorized
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.healthyLocked()
	}
	if m == nil || m.pool == nil || !m.lease.Protects(m.pool) {
		return ErrUnavailable
	}
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return ErrUnavailable
	}
	defer rollbackRestore(tx)
	return identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false)
}

// grantLocked is called while the manager serializes admission. The local
// operation already exists but remains unauthorized until this transaction
// commits. A failed subsequent CAS is an inspectable, uncertain outcome; the
// caller must not start its worker by guessing that the CAS succeeded.
func (m *Manager) grantLocked(ctx context.Context, actor identity.Principal, op *operationRecord, action activity.Action) error {
	if m.operator {
		if ctx.Value(operatorAuthority{}) != m {
			return identity.ErrUnauthorized
		}
		// The OS operator owns the exclusive private deployment lock. Its
		// authorization and result are committed in the private journal; the
		// original database is deliberately not a recovery prerequisite.
		return nil
	}
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ErrUnavailable
	}
	defer rollbackRestore(tx)
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, true); err != nil {
		return err
	}
	event := activity.Event{Action: action, Source: activity.SourceNative,
		Actor:    activity.Actor{Kind: activity.ActorUser, ID: actor.User.ID, CredentialID: actor.SessionID},
		Resource: activity.Resource{Kind: operationResource(op.Kind), ID: op.ID}}
	if err := activity.Record(ctx, tx, event); err != nil {
		return ErrUnavailable
	}
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		// The durable journal retains the unauthorised phase. Restart checks
		// the exact receipt rather than treating a lost COMMIT reply as denial.
		return ErrUnavailable
	}
	return nil
}

func operationResource(kind string) activity.ResourceKind {
	if kind == "restore" || kind == "rollback" {
		return activity.ResourceRestore
	}
	return activity.ResourceBackup
}

func (m *Manager) recordSystem(ctx context.Context, op operationRecord, action activity.Action, state activity.State) error {
	if m.operator {
		return ctx.Err()
	}
	if !m.lease.Protects(m.pool) {
		return ErrUnavailable
	}
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ErrUnavailable
	}
	defer rollbackRestore(tx)
	var exists, same bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM activity_entries WHERE action=$1 AND resource_id=$2),
		EXISTS(SELECT 1 FROM activity_entries WHERE action=$1 AND resource_id=$2 AND resource_kind=$3
		AND source='system' AND actor_kind='system' AND actor_id='' AND actor_credential_id='' AND state=$4
		AND revision=0 AND affected_count=0 AND cardinality(changed_fields)=0)`,
		string(action), op.ID, string(operationResource(op.Kind)), string(state)).Scan(&exists, &same); err != nil {
		return ErrUnavailable
	}
	if exists && !same {
		return ErrConflict
	}
	if !exists {
		event := activity.Event{Action: action, Source: activity.SourceSystem, Actor: activity.Actor{Kind: activity.ActorSystem},
			Resource: activity.Resource{Kind: operationResource(op.Kind), ID: op.ID}, State: state}
		if err := activity.Record(ctx, tx, event); err != nil {
			return ErrUnavailable
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrUnavailable
	}
	return nil
}

func errorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "operation_cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "operation_interrupted"
	case errors.Is(err, ErrCapacity), errors.Is(err, backupstore.ErrQuota), errors.Is(err, backupformat.ErrLimit), errors.Is(err, backuppg.ErrLimit):
		return "capacity_exceeded"
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials):
		return "authority_changed"
	case errors.Is(err, ErrArchive), errors.Is(err, backupformat.ErrInvalidArchive), errors.Is(err, backuppg.ErrArchive),
		errors.Is(err, identity.ErrApplicationKeyBackupInvalid), errors.Is(err, identity.ErrApplicationKeyVaultMissing),
		errors.Is(err, identity.ErrApplicationKeyVaultCiphertext):
		return "invalid_archive"
	case errors.Is(err, backuppg.ErrTarget):
		return "target_not_ready"
	case errors.Is(err, ErrConflict), errors.Is(err, backupstore.ErrConflict), errors.Is(err, recoverydb.ErrConflict), errors.Is(err, backuppg.ErrSchema):
		return "source_changed"
	case errors.Is(err, backupstore.ErrUnavailable), errors.Is(err, backupstore.ErrIntegrity):
		return "storage_unavailable"
	default:
		return "database_unavailable"
	}
}

func (m *Manager) failJob(id string, cause error) {
	m.mu.Lock()
	op := m.operationLocked(id)
	if op == nil {
		m.mu.Unlock()
		return
	}
	copy := *op
	m.mu.Unlock()
	// Some subprocess/catalog layers deliberately classify their own errors.
	// A committed cancellation still controls the terminal state even when a
	// cancelled query arrives wrapped as an inspection failure.
	if copy.CancelAuthorized {
		cause = context.Canceled
	}
	state, code := "failed", errorCode(cause)
	terminal := activity.StateFailed
	if errors.Is(cause, context.Canceled) {
		if copy.CancelAuthorized {
			state, code, terminal = "cancelled", "operation_cancelled", activity.StateCancelled
		} else {
			state, code, terminal = "interrupted", "operation_interrupted", activity.StateInterrupted
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if copy.Authorized {
		var action activity.Action
		if copy.Kind == "create" || copy.Kind == "import" {
			action = activity.ActionBackupFinished
		} else if (copy.Kind == "restore" || copy.Kind == "rollback") && state == "failed" {
			action, terminal = activity.ActionRestoreFailed, activity.StateFailed
		}
		if action != "" && m.recordSystem(ctx, copy, action, terminal) != nil {
			code = "audit_unavailable"
		}
	}
	_ = m.updateOperation(id, state, "finished", code, nil)
}

func decimal(value uint64) string { return strconv.FormatUint(value, 10) }

func parseRevision(value string) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || strconv.FormatUint(parsed, 10) != value {
		return 0, ErrInvalid
	}
	return parsed, nil
}

func (m *Manager) operationCopy(id string) (operationRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if op := m.operationLocked(id); op != nil {
		return *op, nil
	}
	return operationRecord{}, ErrNotFound
}

func (m *Manager) String() string {
	return fmt.Sprintf("recovery manager for generation %d", m.current.Revision)
}

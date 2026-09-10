package recovery

import (
	"context"
	"errors"
	"io"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
)

// operatorAuthority is an in-process capability, never populated from request
// headers, tokens, IDs, or JSON. Only the explicit offline command methods
// create it, and an online Manager rejects those methods.
type operatorAuthority struct{}

// NewOfflineManager requires the exclusive local Runtime lock. It deliberately
// neither connects to the original database nor opens its application-key
// vault. The caller must run as the deployment owner while its service is
// stopped. Authorization and operation outcomes live in the private journal;
// normal database-backed audit resumes when the target generation is accepted.
func NewOfflineManager(ctx context.Context, runtime *Runtime, cfg config.Config, version string) (*Manager, error) {
	if runtime == nil {
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
	engine, err := NewOfflineEngine(cfg, runtime.backups, version)
	if err != nil && !errors.Is(err, ErrUnavailable) {
		return nil, err
	}
	work, cancel := context.WithCancel(ctx)
	m := &Manager{runtime: runtime, cfg: cfg, engine: engine, current: current, data: data, control: control,
		ctx: work, cancel: cancel, jobs: make(map[string]*activeJob), switches: make(chan string, 1), operator: true}
	if len(control.Payload) == 0 {
		if current.Revision != 0 || current.GenerationID != "" {
			cancel()
			return nil, ErrUnavailable
		}
		if err := m.persistLocked(work); err != nil {
			cancel()
			return nil, err
		}
	}
	return m, nil
}

func (m *Manager) operatorContext(ctx context.Context) (context.Context, error) {
	if m == nil || !m.operator {
		return nil, identity.ErrUnauthorized
	}
	return context.WithValue(ctx, operatorAuthority{}, m), nil
}

func (m *Manager) OperatorStatus(ctx context.Context) (StatusView, error) {
	ctx, err := m.operatorContext(ctx)
	if err != nil {
		return StatusView{}, err
	}
	return m.Status(ctx, identity.Principal{})
}

func (m *Manager) OperatorBackups(ctx context.Context, start, limit int) (BackupPage, error) {
	ctx, err := m.operatorContext(ctx)
	if err != nil {
		return BackupPage{}, err
	}
	return m.ListBackups(ctx, identity.Principal{}, start, limit)
}

func (m *Manager) OperatorOperations(ctx context.Context, start, limit int) (OperationPage, error) {
	ctx, err := m.operatorContext(ctx)
	if err != nil {
		return OperationPage{}, err
	}
	return m.ListOperations(ctx, identity.Principal{}, start, limit)
}

func (m *Manager) OperatorOperation(ctx context.Context, id string) (OperationView, error) {
	ctx, err := m.operatorContext(ctx)
	if err != nil {
		return OperationView{}, err
	}
	return m.Operation(ctx, identity.Principal{}, id)
}

func (m *Manager) OperatorImport(ctx context.Context, requestID string, input io.ReadCloser) (OperationView, error) {
	ctx, err := m.operatorContext(ctx)
	if err != nil {
		return OperationView{}, err
	}
	return m.Import(ctx, identity.Principal{}, requestID, input)
}

func (m *Manager) OperatorPlan(ctx context.Context, request PlanRequest) (OperationView, error) {
	ctx, err := m.operatorContext(ctx)
	if err != nil {
		return OperationView{}, err
	}
	return m.Plan(ctx, identity.Principal{}, request)
}

func (m *Manager) OperatorApply(ctx context.Context, id string, request ApplyRequest) (OperationView, error) {
	ctx, err := m.operatorContext(ctx)
	if err != nil {
		return OperationView{}, err
	}
	return m.Apply(ctx, identity.Principal{}, id, request)
}

func (m *Manager) OperatorRollback(ctx context.Context, request RollbackRequest) (OperationView, error) {
	ctx, err := m.operatorContext(ctx)
	if err != nil {
		return OperationView{}, err
	}
	return m.Rollback(ctx, identity.Principal{}, request)
}

func (m *Manager) OperatorCancel(ctx context.Context, id, revision string) (OperationView, error) {
	ctx, err := m.operatorContext(ctx)
	if err != nil {
		return OperationView{}, err
	}
	return m.Cancel(ctx, identity.Principal{}, id, revision)
}

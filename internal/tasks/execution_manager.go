package tasks

import (
	"context"
	"errors"
	"fmt"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

type workerExecution struct {
	runID  string
	token  string
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	group  string
}

// Fencing forbids further database writes, but does not transfer ownership of
// an executing provider's resources. Keep Close pending until every worker has
// acknowledged cancellation before the caller can close its dependencies.
func (m *Manager) drainExecutions() {
	for _, execution := range m.executions {
		execution.cancel()
	}
	for childID, execution := range m.executions {
		<-execution.done
		delete(m.executions, childID)
	}
}

// Reap completed work independently of library pagination. A large waiting
// tail must not keep completed workers occupying every concurrency slot.
func (m *Manager) reapExecutions(ctx context.Context, shutdown bool) error {
	for childID, execution := range m.executions {
		select {
		case <-execution.done:
			var run Run
			var err error
			if shutdown {
				run, err = m.store.SystemStopRun(ctx, execution.runID, "shutdown")
			} else {
				run, err = m.store.GetRun(ctx, execution.runID)
				if err == nil {
					_, err = m.enforceRuntime(ctx, run)
				}
			}
			if err != nil {
				return err
			}
			if err := m.store.finishExecution(ctx, execution.runID, childID, execution.token, execution.err, shutdown); err != nil {
				return err
			}
			execution.cancel()
			delete(m.executions, childID)
		default:
		}
	}
	return nil
}

// The coordinator alone owns the execution map. The closed done channel
// publishes the result. Failed finalization keeps that result for retry and
// never executes an external side effect twice within the same server life.
func (m *Manager) reconcileExecution(ctx context.Context, run Run, child Child, shutdown bool) (bool, error) {
	if execution, exists := m.executions[child.ID]; exists {
		if shutdown || run.State == RunStopping {
			execution.cancel()
		}
		select {
		case <-execution.done:
			if err := m.store.finishExecution(ctx, run.ID, child.ID, execution.token, execution.err, shutdown); err != nil {
				return false, err
			}
			execution.cancel()
			delete(m.executions, child.ID)
		default:
		}
		return false, nil
	}
	if child.State != ChildWaiting {
		return false, fmt.Errorf("%w: a running generic child has no owned worker", ErrInconsistent)
	}
	if shutdown || run.State == RunStopping {
		return false, nil
	}
	group := ""
	if isAnalysisTask(run.TaskKey) {
		group = analysisConcurrencyGroup
		for _, execution := range m.executions {
			if execution.group == group {
				return false, nil
			}
		}
		next, err := m.store.nextAnalysisRun(ctx, m.lastAnalysisRunID)
		if err != nil {
			return false, err
		}
		if next != run.ID {
			return false, nil
		}
	}
	limit := 2
	if m.options.MaxConcurrent != nil {
		limit = m.options.MaxConcurrent()
	}
	if limit < 1 || limit > 16 {
		return false, fmt.Errorf("%w: executor concurrency is outside 1..16", ErrInvalidInput)
	}
	if len(m.executions) >= limit {
		return true, nil
	}
	if !m.enter() {
		return false, context.Canceled
	}
	defer m.operations.Done()
	token, err := randomID()
	if err != nil {
		return false, err
	}
	if _, err := m.store.claimExecution(ctx, run.ID, child.ID, token); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, errAnalysisGroupBusy) {
			return false, nil
		}
		return false, err
	}
	workCtx, cancel := context.WithCancel(systemevents.WithDerived(m.ctx))
	execution := &workerExecution{runID: run.ID, token: token, cancel: cancel, done: make(chan struct{}), group: group}
	m.executions[child.ID] = execution
	if group != "" {
		m.lastAnalysisRunID = run.ID
	}
	entry, registered := m.store.executors.lookup(run.TaskKey)
	work := executionWork(workCtx, run, child, token)
	go func() {
		defer func() {
			if recover() != nil {
				execution.err = fmt.Errorf("executor panicked")
			}
			close(execution.done)
			m.Wake()
		}()
		if !registered || !entry.Executor.Available() {
			execution.err = ErrUnavailable
			return
		}
		if isAnalysisTask(run.TaskKey) {
			if err := m.store.owner.WithOwnedTx(workCtx, func(tx library.OwnedTx) error { return work.Fence(tx) }); err != nil {
				execution.err = err
				return
			}
		}
		execution.err = entry.Executor.Execute(workCtx, work, func(progress Progress) error {
			if err := workCtx.Err(); err != nil {
				return err
			}
			return m.store.updateExecutionProgress(workCtx, run.ID, child.ID, token, progress)
		})
	}()
	return false, nil
}

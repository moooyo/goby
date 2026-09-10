package tasks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/library"
)

const (
	defaultReconcileInterval = 500 * time.Millisecond
	managerCycleTimeout      = 5 * time.Second
	managerScheduleBatch     = 32
	minimumScheduleRetry     = 100 * time.Millisecond
)

// ScanExecutor exposes only task-owned scan operations. The manager never
// cancels a job by library ID or adopts an independent scan returned as busy.
type ScanExecutor interface {
	AdmitTaskScan(context.Context, string) (library.ScanAdmission, error)
	CancelTaskScan(context.Context, string) error
	ScanUpdates() <-chan struct{}
	Available() bool
	CheckOwnership(context.Context) error
}

type ManagerOptions struct {
	ReconcileInterval time.Duration
	BatchSize         int
	Logger            *slog.Logger
}

// Manager initializes persisted schedules, admits due occurrences, and
// coordinates their runs. Construct it only after registry reconciliation,
// scanner recovery, and Store.RecoverRuns have completed. Previous executions
// remain interrupted; a startup event has a new, fixed lifecycle identity.
type Manager struct {
	store    *Store
	scans    ScanExecutor
	options  ManagerOptions
	ctx      context.Context
	cancel   context.CancelFunc
	wake     chan struct{}
	loopDone chan struct{}
	done     chan struct{}

	mu            sync.Mutex
	closing       bool
	scheduleReady bool
	closeErr      error
	closeOnce     sync.Once
	operations    sync.WaitGroup

	// Only the coordinator loop, then its shutdown successor, uses cursors.
	runOffset            int
	childOffsets         map[string]int
	lastErrorLog         time.Time
	startupAt            time.Time
	schedulesInitialized bool
	nextScheduleAttempt  time.Time
	nextDue              *time.Time
	lastScheduleErrorLog time.Time
	runtimeDeadlines     map[string]time.Time
}

func NewManager(store *Store, scans ScanExecutor, options ManagerOptions) (*Manager, error) {
	if store == nil || scans == nil {
		return nil, fmt.Errorf("%w: task repository and scan executor are required", ErrInvalidInput)
	}
	if options.ReconcileInterval == 0 {
		options.ReconcileInterval = defaultReconcileInterval
	}
	if options.ReconcileInterval < 250*time.Millisecond || options.ReconcileInterval > time.Second {
		return nil, fmt.Errorf("%w: reconciliation interval must be between 250 milliseconds and one second", ErrInvalidInput)
	}
	if options.BatchSize == 0 {
		options.BatchSize = MaxPageLimit
	}
	if options.BatchSize < 1 || options.BatchSize > MaxPageLimit {
		return nil, fmt.Errorf("%w: reconciliation batch must contain between 1 and 200 entries", ErrInvalidInput)
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if !scans.Available() {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{store: store, scans: scans, options: options, ctx: ctx, cancel: cancel,
		wake: make(chan struct{}, 1), loopDone: make(chan struct{}), done: make(chan struct{}), childOffsets: make(map[string]int), runtimeDeadlines: make(map[string]time.Time)}
	go m.loop()
	return m, nil
}

// Available also remains false until schedule initialization and dispatch have
// succeeded. A scheduler failure does not prevent reconciliation of admitted
// manual work, but it must not make server readiness claim a healthy scheduler.
func (m *Manager) Available() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	ready := !m.closing && m.scheduleReady
	m.mu.Unlock()
	return ready && m.scans.Available()
}

func (m *Manager) enter() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return false
	}
	m.operations.Add(1)
	return true
}

func (m *Manager) Start(ctx context.Context, actor Actor, request StartRequest) (Admission, error) {
	if !m.enter() {
		return Admission{}, ErrUnavailable
	}
	defer m.operations.Done()
	defer m.Wake()
	if err := m.checkOwnership(ctx); err != nil {
		m.observeFailure(err)
		return Admission{}, err
	}
	result, err := m.store.Start(ctx, actor, request)
	m.observeFailure(err)
	return result, err
}

func (m *Manager) Stop(ctx context.Context, actor Actor, runID string) (Run, error) {
	if !m.enter() {
		return Run{}, ErrUnavailable
	}
	defer m.operations.Done()
	defer m.Wake()
	if err := m.checkOwnership(ctx); err != nil {
		m.observeFailure(err)
		return Run{}, err
	}
	result, err := m.store.Stop(ctx, actor, runID)
	m.observeFailure(err)
	return result, err
}

func (m *Manager) StopByDefinition(ctx context.Context, actor Actor, taskID string) (Run, error) {
	if !m.enter() {
		return Run{}, ErrUnavailable
	}
	defer m.operations.Done()
	defer m.Wake()
	if err := m.checkOwnership(ctx); err != nil {
		m.observeFailure(err)
		return Run{}, err
	}
	result, err := m.store.StopByDefinition(ctx, actor, taskID)
	m.observeFailure(err)
	return result, err
}

// Wake coalesces notifications. Durable state and periodic reconciliation cover
// a full notification buffer, dropped scanner notifications, and uncertain HTTP
// results after an admission transaction has committed.
func (m *Manager) Wake() {
	if m == nil {
		return
	}
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// BeginClose synchronously fences new admissions and starts cleanup once. It
// deliberately does not wait, so other server transports can stop concurrently.
func (m *Manager) BeginClose() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closing = true
	m.mu.Unlock()
	m.closeOnce.Do(func() {
		m.cancel()
		go m.shutdown()
	})
}

// Close never releases catalog ownership or closes the scanner. The caller must
// close the library only after this cleanup finishes. A caller deadline stops
// waiting, while background cleanup continues with its own bounded operations.
func (m *Manager) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.BeginClose()
	select {
	case <-m.done:
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) checkOwnership(ctx context.Context) error {
	if !m.scans.Available() {
		return library.ErrUnavailable
	}
	return m.scans.CheckOwnership(ctx)
}

func (m *Manager) observeFailure(err error) {
	if err == nil || !errors.Is(err, library.ErrUnavailable) {
		return
	}
	m.mu.Lock()
	if m.closeErr == nil {
		m.closeErr = fmt.Errorf("%w: task execution owner became unavailable: %w", ErrUnavailable, err)
	}
	m.mu.Unlock()
	m.BeginClose()
}

func (m *Manager) loop() {
	defer close(m.loopDone)
	timer := time.NewTimer(m.options.ReconcileInterval)
	defer timer.Stop()
	updates := m.scans.ScanUpdates()
	for {
		scheduleCtx, stopSchedule := context.WithTimeout(m.ctx, managerCycleTimeout)
		scheduleErr := m.schedule(scheduleCtx)
		stopSchedule()
		m.observeFailure(scheduleErr)
		// Initialization can fail or consume its own database budget. Admitted
		// manual runs still receive a separate reconciliation opportunity.
		ctx, cancel := context.WithTimeout(m.ctx, managerCycleTimeout)
		_, err := m.reconcile(ctx, false)
		cancel()
		m.observeFailure(err)
		if err != nil && m.ctx.Err() == nil {
			m.logRetry(err)
		}
		updates = consumeScanHint(updates)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(m.nextDelay())
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		case _, open := <-updates:
			if !open {
				updates = nil
			}
		case <-timer.C:
		}
	}
}

func (m *Manager) shutdown() {
	defer close(m.done)
	<-m.loopDone
	// Start or Stop admitted before the fence can finish its protected owned
	// transaction. Include every committed result in the shutdown sweep.
	m.operations.Wait()
	m.runOffset = 0
	m.childOffsets = make(map[string]int)
	clear(m.runtimeDeadlines)
	tick := time.NewTicker(m.options.ReconcileInterval)
	defer tick.Stop()
	updates := m.scans.ScanUpdates()
	for {
		m.mu.Lock()
		fenced := m.closeErr != nil
		m.mu.Unlock()
		if fenced {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), managerCycleTimeout)
		idle, err := m.reconcile(ctx, true)
		cancel()
		m.observeFailure(err)
		if err == nil && idle {
			return
		}
		if err != nil {
			m.logRetry(err)
		}
		updates = consumeScanHint(updates)
		select {
		case _, open := <-updates:
			if !open {
				updates = nil
			}
		case <-tick.C:
		}
	}
}

func (m *Manager) schedule(ctx context.Context) (err error) {
	if time.Now().Before(m.nextScheduleAttempt) {
		return nil
	}
	defer func() {
		m.nextScheduleAttempt = time.Now().Add(minimumScheduleRetry)
		if err != nil {
			m.nextScheduleAttempt = time.Now().Add(m.options.ReconcileInterval)
		}
		m.mu.Lock()
		m.scheduleReady = err == nil && m.schedulesInitialized
		m.mu.Unlock()
		if err != nil && m.ctx.Err() == nil && time.Since(m.lastScheduleErrorLog) >= 5*time.Second {
			m.lastScheduleErrorLog = time.Now()
			m.options.Logger.Warn("Task scheduling will retry; scheduler readiness is unavailable", "error", err)
		}
	}()
	if err = m.checkOwnership(ctx); err != nil {
		return err
	}
	if m.startupAt.IsZero() {
		m.startupAt, err = m.store.ScheduleClock(ctx)
		if err != nil {
			return err
		}
	}
	if !m.schedulesInitialized {
		if !m.enter() {
			return context.Canceled
		}
		err = m.store.InitializeSchedules(ctx, m.startupAt)
		m.operations.Done()
		if err != nil {
			return err
		}
		m.schedulesInitialized = true
	}
	// Initialization itself can admit startup runs. A second admission fence
	// prevents a concurrent BeginClose from allowing subsequent due dispatch.
	if !m.enter() {
		return context.Canceled
	}
	_, err = m.store.DispatchDue(ctx, managerScheduleBatch)
	m.operations.Done()
	if err != nil {
		return err
	}
	m.nextDue, err = m.store.NextDue(ctx)
	return err
}

// Absolute due timestamps are wake-up hints only: DispatchDue compares the
// authoritative database clock again. An overdue batch cannot create a zero
// delay loop, while the normal bounded poll still covers cancellation and lost
// scanner notifications, regardless of wall-clock offset or a stale hint.
func (m *Manager) nextDelay() time.Duration {
	delay := m.options.ReconcileInterval
	if m.nextDue != nil && m.schedulesInitialized {
		dueIn := time.Until(*m.nextDue)
		if dueIn < minimumScheduleRetry {
			dueIn = minimumScheduleRetry
		}
		if cooldown := time.Until(m.nextScheduleAttempt); dueIn < cooldown {
			dueIn = cooldown
		}
		if dueIn < delay {
			delay = dueIn
		}
	}
	return delay
}

func (m *Manager) rememberRuntime(run Run) error {
	if run.State != RunRunning {
		return nil
	}
	if _, exists := m.runtimeDeadlines[run.ID]; exists {
		return nil
	}
	limit, err := ScheduleRuntimeLimit(ScheduleRule{MaxRuntimeTicks: run.MaxRuntimeTicks})
	if err != nil {
		return fmt.Errorf("%w: invalid admitted runtime snapshot: %w", ErrInconsistent, err)
	}
	if limit > 0 {
		// time.Now retains its monotonic reading; UTC/database timestamps do
		// not. This origin is established once, before waiting for scan slots.
		m.runtimeDeadlines[run.ID] = time.Now().Add(limit)
	}
	return nil
}

func (m *Manager) enforceRuntime(ctx context.Context, run Run) (Run, error) {
	if !run.State.Active() || run.State == RunStopping {
		delete(m.runtimeDeadlines, run.ID)
		return run, nil
	}
	deadline, limited := m.runtimeDeadlines[run.ID]
	if !limited {
		if run.MaxRuntimeTicks != nil && *run.MaxRuntimeTicks != 0 {
			return Run{}, fmt.Errorf("%w: active limited run has no process runtime origin", ErrInconsistent)
		}
		return run, nil
	}
	if time.Now().Before(deadline) {
		return run, nil
	}
	stopped, err := m.store.SystemStopRun(ctx, run.ID, "max_runtime")
	if err == nil {
		delete(m.runtimeDeadlines, run.ID)
	}
	return stopped, err
}

// Admission and cancellation publish on the same coalesced channel as worker
// completion. Consume one hint after a pass so repeated cancellation cannot
// wake its own loop indefinitely. The bounded timer covers a concurrent worker
// notification coalesced with that hint, including a terminal-state race.
func consumeScanHint(updates <-chan struct{}) <-chan struct{} {
	select {
	case _, open := <-updates:
		if !open {
			return nil
		}
	default:
	}
	return updates
}

func (m *Manager) logRetry(err error) {
	if time.Since(m.lastErrorLog) < 5*time.Second {
		return
	}
	m.lastErrorLog = time.Now()
	m.options.Logger.Warn("Task coordination will retry", "error", err)
}

// Each pass examines at most BatchSize children or childless runs. Advancing
// through active children, including queued/running entries, prevents a large
// terminal prefix or a full first page of running scans from hiding later work.
// Active sets can shrink between pages; wrapping provides eventual coverage.
func (m *Manager) reconcile(ctx context.Context, shutdown bool) (bool, error) {
	if err := m.checkOwnership(ctx); err != nil {
		return false, err
	}
	page, err := m.store.ListActiveRuns(ctx, Page{StartIndex: m.runOffset, Limit: m.options.BatchSize})
	if err != nil {
		return false, err
	}
	if page.TotalRecordCount == 0 {
		m.runOffset = 0
		clear(m.childOffsets)
		clear(m.runtimeDeadlines)
		return true, nil
	}
	if len(page.Items) == 0 {
		m.runOffset = 0
		return false, nil
	}
	budget := m.options.BatchSize
	for index, listed := range page.Items {
		if budget == 0 {
			break
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		m.runOffset = page.StartIndex + index + 1
		var run Run
		if shutdown {
			run, err = m.store.SystemStopRun(ctx, listed.ID, "shutdown")
		} else if listed.State == RunPending {
			run, err = m.store.BeginRun(ctx, listed.ID)
			if err == nil {
				err = m.rememberRuntime(run)
			}
		} else {
			run = listed
		}
		if err != nil {
			return false, err
		}
		run, err = m.store.RefreshRun(ctx, run.ID)
		if err != nil {
			return false, err
		}
		if !shutdown {
			run, err = m.enforceRuntime(ctx, run)
			if err != nil {
				return false, err
			}
		}
		if !run.State.Active() {
			delete(m.childOffsets, run.ID)
			delete(m.runtimeDeadlines, run.ID)
			budget--
			continue
		}
		children, err := m.store.ListActiveChildren(ctx, run.ID, Page{StartIndex: m.childOffsets[run.ID], Limit: budget})
		if err != nil {
			return false, err
		}
		if len(children.Items) == 0 {
			m.childOffsets[run.ID] = 0
			budget--
		}
		queueFull := false
		for childIndex, child := range children.Items {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			budget--
			m.childOffsets[run.ID] = children.StartIndex + childIndex + 1
			err = nil
			if !shutdown {
				run, err = m.enforceRuntime(ctx, run)
				if err != nil {
					return false, err
				}
				if !run.State.Active() {
					break
				}
			}
			if shutdown || run.State == RunStopping {
				err = m.scans.CancelTaskScan(ctx, child.ID)
			} else if child.State == ChildWaiting {
				if !m.enter() {
					return false, context.Canceled
				}
				var admission library.ScanAdmission
				admission, err = m.scans.AdmitTaskScan(ctx, child.ID)
				m.operations.Done()
				if err == nil {
					switch admission.Kind {
					case library.ScanAdmitted, library.ScanAlreadyOwned, library.ScanDuplicate:
					case library.ScanQueueFull:
						queueFull = true
					default:
						return false, fmt.Errorf("%w: scanner returned an unknown task admission outcome", ErrInconsistent)
					}
				}
			}
			if err != nil && !errors.Is(err, library.ErrTaskScanInactive) && !errors.Is(err, library.ErrNotFound) {
				return false, err
			}
			if queueFull {
				break
			}
		}
		if int64(m.childOffsets[run.ID]) >= children.TotalRecordCount {
			m.childOffsets[run.ID] = 0
		}
		refreshed, err := m.store.RefreshRun(ctx, run.ID)
		if err != nil {
			return false, err
		}
		if !refreshed.State.Active() {
			delete(m.childOffsets, run.ID)
			delete(m.runtimeDeadlines, run.ID)
		}
		if queueFull {
			break
		}
	}
	if int64(m.runOffset) >= page.TotalRecordCount {
		m.runOffset = 0
	}
	return false, nil
}

package transcode

import (
	"context"
	"errors"
	"time"
)

var errManagerFinalization = errors.New("transcode finalization ownership is invalid")

var errJobResourceOwnership = errors.New("transcode resource ownership transition is invalid")

// enqueueFinalization transfers an already accepted job to the fixed executor.
// The runner calls it only after Run and its descriptor/observer defers return.
// This legacy path does not attest a native command domain or all-writer policy;
// execution capacity remains charged until complete cache inspection returns.
func (m *Manager) enqueueFinalization(j *managedJob, runErr error, progressFailure *ProgressFailure) (err error) {
	if m == nil || j == nil {
		return errManagerFinalization
	}
	returned := false
	defer func() {
		if !returned {
			m.quarantineJobFinalization(j, errManagerFinalization)
		}
	}()
	claimed := false
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if j.finalizationQueued || j.finished {
			return
		}
		if j.completion == nil || m.completions == nil || m.finalizers == nil || j.completion.pool != m.completions {
			err = errManagerFinalization
			return
		}
		j.finalizationQueued = true
		claimed = true
	}()
	if err != nil {
		m.quarantineJobFinalization(j, err)
		returned = true
		return err
	}
	if !claimed {
		returned = true
		return nil
	}
	if err = closeFinalizationInputs(j); err != nil {
		m.quarantineJobFinalization(j, err)
		returned = true
		return err
	}
	// The callback owns its diagnostic projection after the runner handoff.
	// No runner goroutine remains waiting for a logger or terminal persistence.
	failure := copyFinalizationProgressFailure(progressFailure)
	err = m.finalizers.submit(j.completion, finalizationTask{
		FinalizeContext: context.Background(), TerminalContext: context.Background(),
		Finalize: func(context.Context) error { return m.finalizeJobInspection(j, runErr) },
		Terminal: func(_ context.Context, inspectionErr error) error {
			return m.finalizeJobTerminal(j, runErr, inspectionErr, failure)
		},
		OnQuarantined: func(cause error) { m.quarantineJobFinalization(j, cause) },
	})
	if err != nil {
		// Failed handoff keeps the original launch reservation and job reachable.
		// Shutdown must stop the executor only after all runners have handed off.
		m.quarantineJobFinalization(j, err)
	}
	returned = true
	return err
}

func closeFinalizationInputs(j *managedJob) error {
	return closeSourceReadInputs(StreamInputs{Media: j.input, Bitmap: j.bitmap}, j.sourceRead)
}

func copyFinalizationProgressFailure(failure *ProgressFailure) *ProgressFailure {
	if failure == nil {
		return nil
	}
	copy := *failure
	if failure.Previous != nil {
		previous := *failure.Previous
		copy.Previous = &previous
	}
	return &copy
}

// finalizeJobInspection preserves the complete legacy final scan and outcome
// decisions. Filesystem/accounting locks are released before global, user and
// auth execution slots are returned together. The unfinished record and original
// completion reservation remain through terminal persistence and diagnostics.
func (m *Manager) finalizeJobInspection(j *managedJob, runErr error) error {
	var directory bool
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		directory = j.directory
	}()
	var size int64
	var ready bool
	var scanErr error
	func() {
		if directory {
			m.filesMu.RLock()
			defer m.filesMu.RUnlock()
			j.filesMu.Lock()
			defer j.filesMu.Unlock()
			size, ready, scanErr = m.cache.FinalizePlanJob(j.record.ID, j.record.Spec.Plan)
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.setAccountingPendingLocked(j, false)
		if scanErr != nil {
			m.markAccountingUnknownLocked(j)
			// Failed inspection cannot reduce the last known accounting floor.
			size = j.record.OutputBytes
		}
		if j.accountingUnknown {
			size = max(size, j.record.OutputBytes)
		}
		if j.record.Spec.Plan.SourceMode == "stream" {
			ready = j.mediaReady
		}
		if j.record.Spec.Plan.OutputMode == "progressive" {
			ready = ready && j.mediaReady
			if size < j.record.OutputBytes && j.stopCode == "" {
				j.stopCode = "invalid_output"
			}
		}
		m.bytes += size - j.record.OutputBytes
		j.record.OutputBytes = size
		if j.stopCode == "" {
			switch {
			case size > m.options.MaxJobBytes:
				j.stopCode = "job_quota"
			case m.bytes > m.options.MaxBytes:
				j.stopCode = "cache_quota"
			case runErr != nil && !(j.productionSealed && j.productionSealSafe && ready && scanErr == nil && !j.accountingUnknown && errors.Is(runErr, context.Canceled)):
				j.stopCode = runnerErrorCode(runErr)
			case j.productionSealed && !j.productionSealSafe:
				j.stopCode = "invalid_output"
			case j.record.Spec.Plan.HLS.Window.RequireInputEvidence && !j.windowInputKnown:
				j.stopCode = "invalid_output"
			case scanErr != nil || !ready:
				j.stopCode = "invalid_output"
			}
		}
		if j.stopCode == "" {
			if j.productionSealed {
				j.record.State, j.record.ErrorCode, j.ready = "cancelled", "production_sealed", ready
			} else {
				j.record.State, j.ready = "completed", true
			}
		} else {
			j.record.State = "failed"
			if j.stopCode == "cancelled" || j.stopCode == "session_cancelled" || j.stopCode == "source_replaced" || j.stopCode == "idle_timeout" || j.stopCode == "manager_closed" {
				j.record.State = "cancelled"
			}
			j.record.ErrorCode = j.stopCode
			if m.bySpec[j.record.Spec] == j {
				delete(m.bySpec, j.record.Spec)
			}
		}
		j.record.UpdatedAt = time.Now().UTC()
	}()
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if j.running {
			j.running = false
			m.running--
		}
		if j.subjectOwnershipHeld {
			if !j.record.Spec.Scope.ApplicationKey {
				m.runningUsers[j.record.Spec.Scope.UserID]--
				if m.runningUsers[j.record.Spec.Scope.UserID] == 0 {
					delete(m.runningUsers, j.record.Spec.Scope.UserID)
				}
			}
			m.runningAuth[j.record.Spec.Scope.AuthSessionID]--
			if m.runningAuth[j.record.Spec.Scope.AuthSessionID] == 0 {
				delete(m.runningAuth, j.record.Spec.Scope.AuthSessionID)
			}
			j.subjectOwnershipHeld = false
		}
	}()
	m.signal()
	return scanErr
}

// finalizeJobTerminal owns the bounded persistence attempt and the diagnostic
// sink. A blocked or abnormal sink keeps this fixed worker and original ticket.
// Cancellation is completed before terminal ownership is committed; no caller
// callback runs while the manager or filesystem locks are held.
func (m *Manager) finalizeJobTerminal(j *managedJob, runErr, inspectionErr error, progressFailure *ProgressFailure) error {
	if errors.Is(inspectionErr, errFinalizationPanic) || errors.Is(inspectionErr, errFinalizationExit) || errors.Is(inspectionErr, errManagerFinalization) {
		m.quarantineJobFinalization(j, inspectionErr)
		// A normal terminal return would release an inspection that never
		// returned. Let the executor retain the exact working submission.
		panic(errManagerFinalization)
	}
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if j.running || j.subjectOwnershipHeld {
			// Terminal handling cannot substitute for the inspection boundary
			// or return execution counters for a second time.
			panic(errManagerFinalization)
		}
	}()
	persistErr := m.persist(j)
	if persistErr != nil {
		func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			j.stopCode, j.record.ErrorCode, j.record.State = "persistence", "persistence", "failed"
			if m.bySpec[j.record.Spec] == j {
				delete(m.bySpec, j.record.Spec)
			}
		}()
	}
	if runErr != nil && progressFailure != nil {
		logManagerProgressFailure(j.record.ID, runnerErrorCode(runErr), progressFailure)
	}
	if j.cancel != nil {
		j.cancel()
	}
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if j.finished {
			panic(errManagerFinalization)
		}
		j.finished = true
		close(j.done)
		m.notifyLocked(j)
	}()
	m.signal()
	// Persistence failure is an explicitly handled failed job outcome, matching
	// the existing Close contract. It is not durable terminal success. Only an
	// unhandled ownership interruption fences the executor's terminal lifetime.
	return nil
}

// quarantineJobFinalization is an I/O-free sticky admission fence. It retains
// execution ownership not already returned, the unfinished ticket and existing
// accounting floor. It does not certify process retirement, writer drain or
// safe deletion.
func (m *Manager) quarantineJobFinalization(j *managedJob, _ error) {
	if m == nil {
		return
	}
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.cacheFailed = true
		if !errors.Is(m.closeErr, ErrCacheUnsafe) {
			m.closeErr = errors.Join(m.closeErr, ErrCacheUnsafe)
		}
		if j != nil {
			m.markAccountingUnknownLocked(j)
			if !j.finished {
				j.ready = false
				if j.stopCode == "" {
					j.stopCode = "finalization_failed"
				}
				j.record.State, j.record.ErrorCode = "failed", j.stopCode
				j.record.UpdatedAt = time.Now().UTC()
				if m.bySpec[j.record.Spec] == j {
					delete(m.bySpec, j.record.Spec)
				}
			}
			if j.changed != nil {
				m.notifyLocked(j)
			}
		}
	}()
	m.signal()
}

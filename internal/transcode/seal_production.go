package transcode

// SealProduction stops a finite legacy VOD window without invalidating its
// previously published artifacts. The private VOD publisher requires a closed
// successor before publishing a segment and gives no tail credit on cancellation.
// Other output contracts need their own window, map and timing rules first.
// The method fences deduplication immediately; process retirement and complete
// final accounting retain their existing asynchronous ownership and budget.
func (m *Manager) SealProduction(id string, scope Scope) error {
	if !validScope(scope) {
		return ErrInvalidScope
	}
	if m == nil {
		return ErrManagerClosed
	}
	m.mu.Lock()
	j := m.jobs[id]
	if j == nil || j.reclaiming || j.record.Spec.Scope != scope {
		m.mu.Unlock()
		return ErrJobNotFound
	}
	if m.closing {
		m.mu.Unlock()
		return ErrManagerClosed
	}
	plan := j.record.Spec.Plan
	if plan.SegmentMode != "vod" || plan.OutputMode != "" || plan.SourceMode != "" || GeneratedHLS(plan) {
		m.mu.Unlock()
		return ErrUnsupported
	}
	if m.cacheFailed || j.accountingUnknown {
		// An unsafe observation cannot grant retained-output semantics, but a
		// cache lookup fence must not leave the last owner's encoder running.
		if j.finished {
			m.invalidateFinishedLocked(j, "cache_unavailable")
		} else {
			m.stopLocked(j, "cache_unavailable")
		}
		m.mu.Unlock()
		m.signal()
		return ErrOutputUnavailable
	}
	if err := jobError(j); err != nil {
		m.mu.Unlock()
		return err
	}
	if j.finished || j.record.State == "completed" || j.productionSealed {
		m.mu.Unlock()
		return nil
	}
	j.productionSealed = true
	if m.bySpec[j.record.Spec] == j {
		delete(m.bySpec, j.record.Spec)
	}
	j.cancel()
	m.notifyLocked(j)
	m.mu.Unlock()
	m.signal()
	return nil
}

func readableSealedProduction(j *managedJob) bool {
	return j.productionSealed && j.stopCode == "" && !j.accountingUnknown &&
		j.record.State == "cancelled" && j.record.ErrorCode == "production_sealed"
}

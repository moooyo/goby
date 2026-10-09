package transcode

// finish is the synchronous compatibility boundary for explicit internal test
// helpers. Production runners always transfer through enqueueFinalization.
func (m *Manager) finish(j *managedJob, runErr error) {
	m.finishLegacySynchronously(j, runErr)
}

// finishLegacySynchronously exists only for explicit package test fixtures that
// manually construct an unticketed job. Every production runner hands off to
// enqueueFinalization. A ticketed job cannot bypass its fixed executor lifetime.
func (m *Manager) finishLegacySynchronously(j *managedJob, runErr error) {
	if m == nil || j == nil {
		return
	}
	claimed := false
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if j.completion != nil || j.finalizationQueued || j.finished {
			return
		}
		j.finalizationQueued = true
		if j.running {
			j.subjectOwnershipHeld = true
		}
		claimed = true
	}()
	if !claimed {
		return
	}
	returned := false
	defer func() {
		if !returned {
			m.quarantineJobFinalization(j, errManagerFinalization)
		}
	}()
	if err := closeFinalizationInputs(j); err != nil {
		m.quarantineJobFinalization(j, err)
		returned = true
		return
	}
	inspectionErr := m.finalizeJobInspection(j, runErr)
	_ = m.finalizeJobTerminal(j, runErr, inspectionErr, nil)
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		// Unticketed test fixtures have no executor return phase. Clear their
		// test-only claim only after both synchronous callbacks returned.
		j.finalizationQueued = false
	}()
	returned = true
}

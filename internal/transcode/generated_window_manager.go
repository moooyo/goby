package transcode

import "context"

// GeneratedWindowInputEvidence returns only a normally completed job's observed
// input association after writer drain, complete cache accounting and terminal
// persistence. It never treats first readiness or a nominal duration as closure.
func (m *Manager) GeneratedWindowInputEvidence(ctx context.Context, scope Scope, id string) ([MaxHLSRenditions]GeneratedInputEvidence, error) {
	for {
		if err := ctx.Err(); err != nil {
			return [MaxHLSRenditions]GeneratedInputEvidence{}, err
		}
		m.mu.Lock()
		job, err := m.lookupLocked(scope, id)
		if err == nil {
			err = jobError(job)
		}
		if err != nil {
			m.mu.Unlock()
			return [MaxHLSRenditions]GeneratedInputEvidence{}, err
		}
		if !job.record.Spec.Plan.HLS.Window.RequireInputEvidence {
			m.mu.Unlock()
			return [MaxHLSRenditions]GeneratedInputEvidence{}, ErrInvalidPlan
		}
		if job.finished {
			if job.record.State != "completed" || !job.windowInputKnown || job.accountingUnknown || job.accountingPending {
				m.mu.Unlock()
				return [MaxHLSRenditions]GeneratedInputEvidence{}, ErrOutputUnavailable
			}
			evidence := job.windowInput
			m.touchLocked(job)
			m.mu.Unlock()
			return evidence, nil
		}
		changed := job.changed
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return [MaxHLSRenditions]GeneratedInputEvidence{}, ctx.Err()
		case <-changed:
		}
	}
}

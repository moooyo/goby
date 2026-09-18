package transcode

import "context"

// HLSClock returns only evidence recorded by this live producer. Scope and
// readiness match the artifact API; a cached guessed offset is never accepted.
func (m *Manager) HLSClock(ctx context.Context, scope Scope, id string, rendition int) (HLSMuxClock, error) {
	if _, err := m.WaitReady(ctx, scope, id); err != nil {
		return HLSMuxClock{}, err
	}
	for {
		m.mu.Lock()
		job, err := m.lookupLocked(scope, id)
		if err == nil {
			err = jobError(job)
		}
		if err != nil {
			m.mu.Unlock()
			return HLSMuxClock{}, err
		}
		if !needsHLSClock(job.record.Spec.Plan) || rendition < 0 || rendition >= max(1, job.record.Spec.Plan.HLS.RenditionCount) {
			m.mu.Unlock()
			return HLSMuxClock{}, ErrInvalidTimeline
		}
		if job.hlsClockKnown[rendition] {
			value := job.hlsClocks[rendition]
			m.touchLocked(job)
			m.mu.Unlock()
			return value, nil
		}
		if job.finished {
			m.mu.Unlock()
			return HLSMuxClock{}, ErrOutputUnavailable
		}
		changed := job.changed
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return HLSMuxClock{}, ctx.Err()
		case <-changed:
		}
	}
}

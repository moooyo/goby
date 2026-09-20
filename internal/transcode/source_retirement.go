package transcode

import "context"

// CancelSource fences every output revision for one indexed source and joins
// its processes, including producers whose HTTP registration already retired.
// Callers hold the catalog publication barrier against new source admission.
func (m *Manager) CancelSource(ctx context.Context, itemID, sourceID string) error {
	if itemID == "" || sourceID == "" || len(itemID) > 256 || len(sourceID) > 256 {
		return ErrInvalidScope
	}
	m.mu.Lock()
	var pending []<-chan struct{}
	for _, job := range m.jobs {
		if job.record.Spec.Scope.ItemID != itemID || job.record.Spec.Scope.SourceID != sourceID {
			continue
		}
		if job.finished {
			m.invalidateFinishedLocked(job, "source_replaced")
		} else {
			m.stopLocked(job, "source_replaced")
		}
		pending = append(pending, job.done)
	}
	m.mu.Unlock()
	m.signal()
	for _, done := range pending {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ctx.Err()
}

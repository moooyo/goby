package library

import "time"

const (
	scanProgressCheckpointItems    = 64
	scanProgressCheckpointInterval = 500 * time.Millisecond
)

// Only the serial scan worker accesses its progress checkpoint. Probe workers
// never update counters or advance the projection confirmed by a commit.
type scanProgressCheckpoint struct {
	scanned, added, updated int
	savedAt                 time.Time
}

func (checkpoint scanProgressCheckpoint) due(job Job, now time.Time) bool {
	return checkpoint.savedAt.IsZero() || job.Scanned-checkpoint.scanned >= scanProgressCheckpointItems ||
		now.Sub(checkpoint.savedAt) >= scanProgressCheckpointInterval
}

func (task *scanTask) recordSavedProgress(job Job, now time.Time) {
	task.progress = scanProgressCheckpoint{scanned: job.Scanned, added: job.Added, updated: job.Updated, savedAt: now}
}

// A deleted job can only be matched against its retained durable child, not
// against later buffered visits. Keep the exact job identity and validation.
func (task *scanTask) retainedProgressSnapshot() Job {
	job := task.job
	if !task.progress.savedAt.IsZero() {
		job.Scanned, job.Added, job.Updated = task.progress.scanned, task.progress.added, task.progress.updated
	}
	return job
}

// Check at existing visit boundaries, including when no counters changed.
// Database-only cancellation is observed at the next due boundary; a blocked
// filesystem call or probe is not polled by a separate background worker.
func (s *Store) maybePersistProgress(task *scanTask) error {
	if task == nil || task.ctx == nil {
		return ErrInvalidInput
	}
	if err := task.ctx.Err(); err != nil {
		return err
	}
	if !s.Available() {
		return ErrUnavailable
	}
	if !task.progress.due(task.job, time.Now()) {
		return nil
	}
	return s.persistProgress(task)
}

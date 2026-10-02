package transcode

import (
	"context"
	"math"
)

// ResourceUsage observes retained manager ownership for one exact authenticated
// scope. Counts may overlap: an accepted creation is also queued, and a terminal
// record may still hold a completion reservation or reader pins.
type ResourceUsage struct {
	RetainedJobs                 int
	UnfinishedJobs               int
	CreatingJobs                 int
	QueuedJobs                   int
	ExecutionReservations        int
	ReaderPins                   int
	ReclaimingJobs               int
	KnownChargedOutputFloorBytes int64
	AccountingPendingJobs        int
	AccountingUnknownJobs        int
	CompletionReservedJobs       int
	CompletionQueuedJobs         int
	CompletionWorkingJobs        int
	CompletionReleasedJobs       int
	CompletionUnknownJobs        int
	UnticketedJobs               int
}

// ResourceUsage reads existing ownership and accounting fields without touching
// files, idle leases, reader pins, admission or release state. A valid scope with
// no exact match returns zero counts and exposes no other scope's resources.
//
// ExecutionReservations includes the complete legacy final inspection charged
// to j.running; it is not a live process count or native retirement evidence.
// KnownChargedOutputFloorBytes sums remembered Record.OutputBytes. Pending or
// unknown accounting can retain an incomplete floor: this is not a measurement
// or hard bound of whole workspaces, scratch files or physical allocation.
// Completion phases and ReclaimingJobs describe in-memory ownership, not writer
// drain, actual filesystem deletion or storage-backend readiness.
//
// The scan is bounded by MaxRetainedJobs and holds Manager.mu before the ticket
// pool lock, matching existing ownership operations. Context checks reject an
// expired observation before and after locking and before return; cancellation
// does not interrupt a mutex wait or establish a bounded syscall guarantee.
func (m *Manager) ResourceUsage(ctx context.Context, scope Scope) (ResourceUsage, error) {
	if !validScope(scope) {
		return ResourceUsage{}, ErrInvalidScope
	}
	if m == nil {
		return ResourceUsage{}, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return ResourceUsage{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ResourceUsage{}, err
	}
	if m.options.MaxRetainedJobs < 1 || m.options.MaxRetainedJobs > maxCompletionTickets ||
		len(m.jobs) > m.options.MaxRetainedJobs {
		return ResourceUsage{}, ErrOutputUnavailable
	}
	if m.completions != nil {
		m.completions.mu.Lock()
		defer m.completions.mu.Unlock()
	}
	if err := ctx.Err(); err != nil {
		return ResourceUsage{}, err
	}
	var usage ResourceUsage
	for _, j := range m.jobs {
		if j == nil || j.record.Spec.Scope != scope {
			continue
		}
		usage.RetainedJobs++
		if !j.finished {
			usage.UnfinishedJobs++
		}
		if !j.durable && !j.finished && !j.finalizationQueued {
			usage.CreatingJobs++
		}
		if !j.running && !j.finished && !j.finalizationQueued && j.record.State == "queued" {
			usage.QueuedJobs++
		}
		if j.running {
			usage.ExecutionReservations++
		}
		usage.ReaderPins += j.readers
		if j.reclaiming {
			usage.ReclaimingJobs++
		}
		if j.record.OutputBytes < 0 || j.record.OutputBytes > math.MaxInt64-usage.KnownChargedOutputFloorBytes {
			return ResourceUsage{}, ErrOutputUnavailable
		}
		usage.KnownChargedOutputFloorBytes += j.record.OutputBytes
		if j.accountingPending {
			usage.AccountingPendingJobs++
		}
		if j.accountingUnknown {
			usage.AccountingUnknownJobs++
		}
		if j.completion == nil {
			usage.UnticketedJobs++
			continue
		}
		if m.completions == nil || j.completion.pool != m.completions {
			// A foreign pool is not held and its phase must not be read.
			usage.CompletionUnknownJobs++
			continue
		}
		switch j.completion.phase {
		case completionReserved:
			usage.CompletionReservedJobs++
		case completionQueued:
			usage.CompletionQueuedJobs++
		case completionWorking:
			usage.CompletionWorkingJobs++
		case completionReleased:
			usage.CompletionReleasedJobs++
		default:
			usage.CompletionUnknownJobs++
		}
	}
	if err := ctx.Err(); err != nil {
		return ResourceUsage{}, err
	}
	return usage, nil
}

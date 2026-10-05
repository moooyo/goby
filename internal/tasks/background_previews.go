package tasks

import (
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

// Cancellation is an explicit stop of work already requested before that stop.
// A later request remains pending, while interruption preserves all unfinished
// work for a fresh fenced run after restart. Files are never touched here.
func settleBackgroundPreviewQueue(tx library.OwnedTx, run Run) error {
	if run.TaskKey != library.TaskBackgroundPreviewGenerationKey || run.State.Active() {
		return nil
	}
	if run.State == RunCancelled || run.StopRequestedAt != nil && run.StopReason != "" && run.StopReason != "shutdown" {
		_, err := tx.Exec(`UPDATE background_preview_queue q SET
   completed_revision=CASE WHEN q.requested_at<=COALESCE(r.stop_requested_at,r.finished_at) THEN q.requested_revision
    ELSE GREATEST(q.completed_revision,q.claimed_revision) END,
   state=CASE WHEN q.requested_at<=COALESCE(r.stop_requested_at,r.finished_at) THEN 'cancelled' ELSE 'pending' END,
   error_code=CASE WHEN q.requested_at<=COALESCE(r.stop_requested_at,r.finished_at) THEN 'cancelled' ELSE '' END,
   finished_at=clock_timestamp()
   FROM task_runs r WHERE r.id=$1 AND q.requested_revision>q.completed_revision`, run.ID)
		return err
	}
	if _, err := tx.Exec(`UPDATE background_preview_queue SET state='pending',error_code='server_interrupted'
  WHERE run_id=$1 AND state='running' AND requested_revision>completed_revision`, run.ID); err != nil {
		return err
	}
	if run.State == RunInterrupted {
		return systemevents.Record(tx.Exec, systemevents.BackgroundPreviewGenerationRequested)
	}
	return nil
}

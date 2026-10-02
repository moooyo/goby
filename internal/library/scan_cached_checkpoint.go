package library

import (
	"context"
	"time"
)

// checkCachedTaskScanProgress replaces the already required image-absence
// ownership query at the end of one unchanged media visit. It is not a reusable
// authorization result. Dependent materialized CTEs lock run, child, then job in
// the established order and observe post-wait state in this one statement.
//
// This statement still has one PostgreSQL autocommit transaction. Combining it
// with an existing query removes the separate completion checkpoint transaction;
// replacing that checkpoint alone would only remove explicit BEGIN/COMMIT.
func (s *Store) checkCachedTaskScanProgress(task *scanTask) error {
	if task == nil || task.job.TaskChildID == "" {
		return ErrInvalidInput
	}
	if !s.Available() {
		return ErrUnavailable
	}
	var unchanged bool
	err := func() error {
		if err := s.lockOwnedSession(task.ctx); err != nil {
			return err
		}
		defer s.ownership.mu.Unlock()
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(task.ctx), 20*time.Second)
		defer cancel()
		err := s.ownership.conn.QueryRow(readCtx, `WITH progress_run AS MATERIALIZED (
		SELECT r.id,r.state,r.task_key FROM task_runs r
		WHERE r.id=(SELECT run_id FROM task_run_children WHERE id=$2)
		FOR SHARE OF r
	), progress_child AS MATERIALIZED (
		SELECT c.* FROM task_run_children c JOIN progress_run r ON r.id=c.run_id
		WHERE c.id=$2 FOR SHARE OF c
	), progress_job AS MATERIALIZED (
		SELECT j.* FROM scan_jobs j JOIN progress_child c ON c.scan_job_id=j.id
		WHERE j.id=$1 FOR SHARE OF j
	)
	SELECT EXISTS(SELECT 1 FROM progress_run r JOIN progress_child c ON c.run_id=r.id
		JOIN progress_job j ON j.id=c.scan_job_id
		WHERE j.id=$1 AND j.task_child_id=$2 AND c.id=$2
		AND j.library_id=$3 AND c.library_id=$3
		AND j.status='Running' AND c.state='running' AND NOT j.cancel_requested
		AND r.state IN ('pending','running') AND j.force_probe=$7
		AND ((r.task_key=$8 AND NOT j.force_probe) OR (r.task_key=$9 AND j.force_probe))
		AND j.scanned=$4 AND j.added=$5 AND j.updated=$6
		AND c.scanned=j.scanned AND c.added=j.added AND c.updated=j.updated
		AND c.error_code='' AND c.error_message=j.error
		AND c.started_at IS NOT DISTINCT FROM j.started_at
		AND c.finished_at IS NOT DISTINCT FROM j.finished_at)`, task.job.ID, task.job.TaskChildID,
			task.job.LibraryID, task.job.Scanned, task.job.Added, task.job.Updated, task.job.ForceProbe,
			TaskLibraryScanKey, TaskLibraryRefreshMediaKey).Scan(&unchanged)
		return s.ownershipErrorLocked(err)
	}()
	if err != nil {
		return err
	}
	if err := task.ctx.Err(); err != nil {
		return err
	}
	if unchanged {
		return nil
	}
	// A changed or missing relation is never accepted from the read path. The
	// ordinary checkpoint re-locks and rechecks current authority, persists any
	// cancellation/repair, and validates retained terminal or deleted snapshots.
	return s.persistProgress(task)
}

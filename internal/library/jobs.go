package library

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const jobColumns = `id, library_id, status, error, scanned, added, updated, force_probe, created_at, started_at, finished_at,
	COALESCE(task_child_id, '') AS task_child_id, cancel_requested`

// StartScan durably queues work independently from the requesting HTTP context.
func (s *Store) StartScan(ctx context.Context, libraryID string) (Job, error) {
	return s.StartScanWithOptions(ctx, libraryID, ScanOptions{})
}

// StartScanWithOptions persists the requested probe policy with the queued job.
func (s *Store) StartScanWithOptions(ctx context.Context, libraryID string, options ScanOptions) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Job{}, ErrUnavailable
	}
	var job Job
	err := s.taskScanTransaction(ctx, func(tx OwnedTx) error {
		var existing string
		if err := tx.QueryRow("SELECT id FROM libraries WHERE id = $1 FOR KEY SHARE", libraryID).Scan(&existing); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		err := tx.QueryRow("SELECT id FROM scan_jobs WHERE library_id = $1 AND status IN ('Queued', 'Running')", libraryID).Scan(&existing)
		if err == nil {
			return ErrScanAlreadyActive
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// Determine duplicates before capacity so a full queue cannot hide an
		// already active scan behind the generic legacy ErrBusy category.
		if len(s.queue) == cap(s.queue) {
			return ErrScanQueueFull
		}
		id, err := randomID()
		if err != nil {
			return err
		}
		job, err = scanJob(tx.QueryRow(`INSERT INTO scan_jobs (id, library_id, status, force_probe)
			VALUES ($1, $2, 'Queued', $3) RETURNING `+jobColumns, id, libraryID, options.ForceProbe))
		return err
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "scan_jobs_one_active_library_idx" {
		return Job{}, ErrScanAlreadyActive
	}
	if err != nil {
		return Job{}, fmt.Errorf("queue library scan: %w", err)
	}
	s.enqueueScan(job)
	return job, nil
}

func (s *Store) ListJobs(ctx context.Context) ([]Job, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+jobColumns+" FROM scan_jobs ORDER BY created_at DESC, id LIMIT 1000")
	if err != nil {
		return nil, fmt.Errorf("list scan jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("read scan job: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) GetJob(ctx context.Context, id string) (Job, error) {
	job, err := scanJob(s.pool.QueryRow(ctx, "SELECT "+jobColumns+" FROM scan_jobs WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get scan job: %w", err)
	}
	return job, nil
}

// CancelJob is idempotent for terminal jobs. Running jobs publish their final
// Cancelled state after their worker stops; queued jobs can finish immediately.
func (s *Store) CancelJob(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.taskScanTransaction(ctx, func(tx OwnedTx) error {
		relation, err := lockTaskScanRelation(tx, id, "")
		if err != nil {
			return err
		}
		return s.cancelLockedScan(tx, relation.job, relation.child)
	})
	if err != nil {
		return fmt.Errorf("request scan cancellation: %w", err)
	}
	if task := s.active[id]; task != nil {
		task.cancel()
	}
	s.notifyScanUpdate()
	return nil
}

func scanJob(row rowScanner) (Job, error) {
	var job Job
	err := row.Scan(&job.ID, &job.LibraryID, &job.Status, &job.Error, &job.Scanned, &job.Added, &job.Updated,
		&job.ForceProbe, &job.CreatedAt, &job.StartedAt, &job.FinishedAt, &job.TaskChildID, &job.CancelRequested)
	return job, err
}

func (s *Store) worker() {
	defer s.workers.Done()
	for task := range s.queue {
		s.notifyScanUpdate()
		s.runTask(task)
		task.cancel()
		s.mu.Lock()
		delete(s.active, task.job.ID)
		s.mu.Unlock()
		s.notifyScanUpdate()
	}
}

func (s *Store) runTask(task *scanTask) {
	status, message := "Completed", ""
	if task.ctx.Err() == nil {
		started, err := s.startTaskScan(task)
		if err == nil && started {
			message, err = s.scanLibrary(task)
		} else if err == nil {
			status, message = task.job.Status, task.job.Error
		}
		if err != nil {
			status = "Failed"
			if message == "" {
				message = "The scan could not finish; existing catalog records were retained"
			}
		}
	}
	if task.ctx.Err() != nil {
		status, message = "Cancelled", "Scan cancelled"
	}
	// A transient database error must not leave an ownerless active job holding
	// the unique library scan slot. Keep its worker until finalization succeeds.
	for attempt := 0; ; attempt++ {
		if task.ctx.Err() != nil {
			status, message = "Cancelled", "Scan cancelled"
		}
		err := s.finishTask(task, status, message)
		if err == nil {
			return
		}
		if attempt == 0 {
			slog.Warn("Retrying scan job finalization", "job_id", task.job.ID, "error", err)
		}
		if s.ctx.Err() != nil {
			s.mu.Lock()
			s.shutdownErr = fmt.Errorf("persist final scan status: %w", err)
			s.mu.Unlock()
			return
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-timer.C:
		case <-s.ctx.Done():
			timer.Stop()
		}
	}
}

func (s *Store) finishTask(task *scanTask, status, message string) error {
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var finished Job
	err := s.taskScanTransaction(finishCtx, func(tx OwnedTx) error {
		relation, err := lockTaskScanRelation(tx, task.job.ID, task.job.TaskChildID)
		if errors.Is(err, ErrNotFound) && task.job.TaskChildID == "" {
			return nil
		}
		if err != nil {
			return err
		}
		if relation.missing {
			return validateRetainedTaskSnapshot(relation.child, task.job)
		}
		finished = relation.job
		if finished.Status != "Queued" && finished.Status != "Running" {
			return copyTaskScanSnapshot(tx, relation.child, finished)
		}
		// The parent and child are fixed before the scan. Cancellation wins
		// until the terminal scan and child snapshot commit together.
		if finished.CancelRequested || task.ctx.Err() != nil || relation.child != nil && !activeTaskRun(relation.child.runState) {
			status, message = s.taskCancellationStatus(relation.child)
		}
		finished, err = scanJob(tx.QueryRow(`UPDATE scan_jobs SET status = $2, error = $3,
			scanned = $4, added = $5, updated = $6, finished_at = clock_timestamp()
			WHERE id = $1 RETURNING `+jobColumns, task.job.ID, status, message, task.job.Scanned, task.job.Added, task.job.Updated))
		if err != nil {
			return err
		}
		if err := copyTaskScanSnapshot(tx, relation.child, finished); err != nil {
			return err
		}
		if finished.Status == "Completed" {
			if _, err := tx.Exec("UPDATE libraries SET last_scan_at = clock_timestamp() WHERE id = $1", finished.LibraryID); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		if finished.ID != "" {
			task.job = finished
		}
		s.notifyScanUpdate()
	}
	return err
}

func (s *Store) persistProgress(task *scanTask) error {
	var cancelled bool
	err := s.taskScanTransaction(task.ctx, func(tx OwnedTx) error {
		relation, err := lockTaskScanRelation(tx, task.job.ID, task.job.TaskChildID)
		if err != nil {
			return err
		}
		if relation.missing {
			cancelled = true
			return validateRetainedTaskSnapshot(relation.child, task.job)
		}
		if relation.job.Status != "Queued" && relation.job.Status != "Running" {
			cancelled = true
			return copyTaskScanSnapshot(tx, relation.child, relation.job)
		}
		cancelled = relation.job.CancelRequested || relation.child != nil && !activeTaskRun(relation.child.runState)
		job, err := scanJob(tx.QueryRow(`UPDATE scan_jobs SET scanned = $2, added = $3, updated = $4,
			cancel_requested = cancel_requested OR $5 WHERE id = $1 RETURNING `+jobColumns,
			task.job.ID, task.job.Scanned, task.job.Added, task.job.Updated, cancelled))
		if err != nil {
			return err
		}
		return copyTaskScanSnapshot(tx, relation.child, job)
	})
	if err == nil {
		s.notifyScanUpdate()
	}
	if err == nil && cancelled {
		task.cancel()
		return context.Canceled
	}
	return err
}

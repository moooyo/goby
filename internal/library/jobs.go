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

const jobColumns = `id, library_id, status, error, scanned, added, updated, created_at, started_at, finished_at`

// StartScan durably queues work independently from the requesting HTTP context.
func (s *Store) StartScan(ctx context.Context, libraryID string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Job{}, ErrUnavailable
	}
	if len(s.queue) == cap(s.queue) {
		return Job{}, ErrBusy
	}
	id, err := randomID()
	if err != nil {
		return Job{}, err
	}
	job, err := scanJob(s.queryOwnedRow(ctx, `INSERT INTO scan_jobs (id, library_id, status)
		SELECT $1, id, 'Queued' FROM libraries WHERE id = $2 RETURNING `+jobColumns, id, libraryID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "scan_jobs_one_active_library_idx" {
		return Job{}, ErrBusy
	}
	if err != nil {
		return Job{}, fmt.Errorf("queue library scan: %w", err)
	}
	taskCtx, cancel := context.WithCancel(s.ctx)
	task := &scanTask{job: job, ctx: taskCtx, cancel: cancel}
	s.active[id] = task
	s.queue <- task
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
	job, err := s.GetJob(ctx, id)
	if err != nil {
		return err
	}
	if job.Status != "Queued" && job.Status != "Running" {
		return nil
	}
	if _, err := s.execOwned(ctx, `UPDATE scan_jobs SET cancel_requested = true,
		status = CASE WHEN status = 'Queued' THEN 'Cancelled' ELSE status END,
		error = CASE WHEN status = 'Queued' THEN 'Scan cancelled' ELSE error END,
		finished_at = CASE WHEN status = 'Queued' THEN now() ELSE finished_at END
		WHERE id = $1 AND status IN ('Queued', 'Running')`, id); err != nil {
		return fmt.Errorf("request scan cancellation: %w", err)
	}
	if task := s.active[id]; task != nil {
		task.cancel()
	}
	return nil
}

func scanJob(row rowScanner) (Job, error) {
	var job Job
	err := row.Scan(&job.ID, &job.LibraryID, &job.Status, &job.Error, &job.Scanned, &job.Added, &job.Updated, &job.CreatedAt, &job.StartedAt, &job.FinishedAt)
	return job, err
}

func (s *Store) worker() {
	defer s.workers.Done()
	for task := range s.queue {
		s.runTask(task)
		task.cancel()
		s.mu.Lock()
		delete(s.active, task.job.ID)
		s.mu.Unlock()
	}
}

func (s *Store) runTask(task *scanTask) {
	status, message := "Completed", ""
	if task.ctx.Err() == nil {
		var started time.Time
		err := s.queryOwnedRow(task.ctx, `UPDATE scan_jobs SET status = 'Running', started_at = now()
			WHERE id = $1 AND status = 'Queued' AND NOT cancel_requested RETURNING started_at`, task.job.ID).Scan(&started)
		if err == nil {
			task.job.StartedAt = &started
			message, err = s.scanLibrary(task)
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
	tx, err := s.beginOwnedTx(finishCtx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	// Cancellation wins a race with completion until the status becomes final.
	err = tx.QueryRow(finishCtx, `UPDATE scan_jobs SET
		status = CASE WHEN cancel_requested THEN 'Cancelled' ELSE $2 END,
		error = CASE WHEN cancel_requested THEN 'Scan cancelled' ELSE $3 END,
		scanned = $4, added = $5, updated = $6, finished_at = now()
		WHERE id = $1 AND status IN ('Queued', 'Running') RETURNING status`,
		task.job.ID, status, message, task.job.Scanned, task.job.Added, task.job.Updated).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status == "Completed" {
		if _, err := tx.Exec(finishCtx, "UPDATE libraries SET last_scan_at = now() WHERE id = $1", task.job.LibraryID); err != nil {
			return err
		}
	}
	return tx.Commit(finishCtx)
}

func (s *Store) persistProgress(task *scanTask) error {
	var cancelled bool
	err := s.queryOwnedRow(task.ctx, `UPDATE scan_jobs SET scanned = $2, added = $3, updated = $4
		WHERE id = $1 RETURNING cancel_requested`, task.job.ID, task.job.Scanned, task.job.Added, task.job.Updated).Scan(&cancelled)
	if cancelled {
		task.cancel()
		return context.Canceled
	}
	return err
}

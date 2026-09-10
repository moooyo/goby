package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
)

var (
	ErrScanAlreadyActive = fmt.Errorf("%w: a scan is already active for this library", ErrBusy)
	ErrScanQueueFull     = fmt.Errorf("%w: the scan queue has no available capacity", ErrBusy)
	ErrTaskScanInactive  = errors.New("task scan child is no longer admissible")
)

type ScanAdmissionKind string

const (
	ScanAdmitted     ScanAdmissionKind = "admitted"
	ScanAlreadyOwned ScanAdmissionKind = "already_owned"
	ScanDuplicate    ScanAdmissionKind = "duplicate_active"
	ScanQueueFull    ScanAdmissionKind = "queue_full"
)

type ScanAdmission struct {
	Kind ScanAdmissionKind
	Job  Job
}

// ScanUpdates is a coalesced hint that progress or admission capacity changed.
// Consumers must also poll authoritative state, and stop through their own
// context. This channel is never closed while a worker could publish a hint.
func (s *Store) ScanUpdates() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.scanUpdates
}

func (s *Store) notifyScanUpdate() {
	select {
	case s.scanUpdates <- struct{}{}:
	default:
	}
}

func (s *Store) taskScanTransaction(ctx context.Context, callback func(OwnedTx) error) error {
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	return s.withOwnedTxCallback(tx, callback)
}

func validTaskChildID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, character := range id {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

type taskScanChild struct {
	id, runID, runState, stopReason string
	libraryID, state, scanID        string
	scanned, added, updated         int64
	errorCode, errorMessage         string
	startedAt, finishedAt           *time.Time
}

func taskChildTerminal(state string) bool {
	switch state {
	case "completed", "failed", "cancelled", "unavailable", "interrupted":
		return true
	}
	return false
}

func activeTaskRun(state string) bool { return state == "pending" || state == "running" }

func taskScanAssociationError() error {
	return fmt.Errorf("%w: task child and scan ownership or terminal snapshot disagree", ErrUnavailable)
}

// Lookups do not retain child or scan locks before the immutable parent ID is
// known. All state changes then acquire run, child, and scan rows in that order.
func lockTaskScanChild(tx OwnedTx, childID string) (*taskScanChild, error) {
	child := &taskScanChild{id: childID}
	err := tx.QueryRow("SELECT run_id FROM task_run_children WHERE id = $1", childID).Scan(&child.runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	err = tx.QueryRow("SELECT state, stop_reason FROM task_runs WHERE id = $1 FOR UPDATE", child.runID).
		Scan(&child.runState, &child.stopReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	err = tx.QueryRow(`SELECT library_id, state, COALESCE(scan_job_id, ''), scanned, added, updated,
		error_code, error_message, started_at, finished_at FROM task_run_children
		WHERE id = $1 AND run_id = $2 FOR UPDATE`, child.id, child.runID).
		Scan(&child.libraryID, &child.state, &child.scanID, &child.scanned, &child.added, &child.updated,
			&child.errorCode, &child.errorMessage, &child.startedAt, &child.finishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return child, nil
}

func validateTaskScanLink(child *taskScanChild, job Job) error {
	if child == nil || child.scanID != job.ID || child.id != job.TaskChildID || child.libraryID != job.LibraryID {
		return taskScanAssociationError()
	}
	return nil
}

func ensureTaskChildUnlinked(tx OwnedTx, child *taskScanChild) error {
	var scanID string
	err := tx.QueryRow("SELECT id FROM scan_jobs WHERE task_child_id = $1", child.id).Scan(&scanID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return taskScanAssociationError()
}

func sameScanTime(first, second *time.Time) bool {
	return first == nil && second == nil || first != nil && second != nil && first.Equal(*second)
}

func taskScanErrorCode(status string) string {
	switch status {
	case "Failed":
		return "scan_failed"
	case "Cancelled":
		return "scan_cancelled"
	case "Interrupted":
		return "scan_interrupted"
	default:
		return ""
	}
}

func taskSnapshotMatches(child *taskScanChild, job Job) bool {
	return child.state == strings.ToLower(job.Status) && child.scanned == int64(job.Scanned) &&
		child.added == int64(job.Added) && child.updated == int64(job.Updated) &&
		child.errorCode == taskScanErrorCode(job.Status) && child.errorMessage == job.Error &&
		sameScanTime(child.startedAt, job.StartedAt) && sameScanTime(child.finishedAt, job.FinishedAt)
}

func copyTaskScanSnapshot(tx OwnedTx, child *taskScanChild, job Job) error {
	if child == nil {
		return nil
	}
	if err := validateTaskScanLink(child, job); err != nil {
		return err
	}
	if taskChildTerminal(child.state) {
		if !taskSnapshotMatches(child, job) {
			return taskScanAssociationError()
		}
		return nil
	}
	result, err := tx.Exec(`UPDATE task_run_children SET state = $3, scanned = $4, added = $5, updated = $6,
		error_code = $7, error_message = $8, started_at = $9, finished_at = $10
		WHERE id = $1 AND scan_job_id = $2 AND state IN ('queued', 'running')`, child.id, job.ID,
		strings.ToLower(job.Status), job.Scanned, job.Added, job.Updated, taskScanErrorCode(job.Status), job.Error,
		job.StartedAt, job.FinishedAt)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return taskScanAssociationError()
	}
	return nil
}

type taskScanRelation struct {
	job     Job
	child   *taskScanChild
	missing bool
}

func lockTaskScanRelation(tx OwnedTx, jobID, expectedChildID string) (taskScanRelation, error) {
	var childID string
	err := tx.QueryRow("SELECT COALESCE(task_child_id, '') FROM scan_jobs WHERE id = $1", jobID).Scan(&childID)
	if errors.Is(err, pgx.ErrNoRows) {
		if expectedChildID == "" {
			return taskScanRelation{}, ErrNotFound
		}
		child, childErr := lockTaskScanChild(tx, expectedChildID)
		if childErr != nil {
			return taskScanRelation{}, childErr
		}
		if child.scanID != jobID || !taskChildTerminal(child.state) || child.finishedAt == nil {
			return taskScanRelation{}, taskScanAssociationError()
		}
		return taskScanRelation{child: child, missing: true}, nil
	}
	if err != nil {
		return taskScanRelation{}, err
	}
	if expectedChildID != "" && childID != expectedChildID {
		return taskScanRelation{}, taskScanAssociationError()
	}
	var child *taskScanChild
	if childID != "" {
		child, err = lockTaskScanChild(tx, childID)
		if err != nil {
			return taskScanRelation{}, err
		}
	}
	job, err := scanJob(tx.QueryRow("SELECT "+jobColumns+" FROM scan_jobs WHERE id = $1 FOR UPDATE", jobID))
	if err != nil {
		return taskScanRelation{}, err
	}
	if child != nil {
		if err := validateTaskScanLink(child, job); err != nil {
			return taskScanRelation{}, err
		}
	} else if job.TaskChildID != "" {
		return taskScanRelation{}, taskScanAssociationError()
	}
	return taskScanRelation{job: job, child: child}, nil
}

func validateRetainedTaskSnapshot(child *taskScanChild, cached Job) error {
	if child == nil || child.scanID != cached.ID || child.id != cached.TaskChildID || child.libraryID != cached.LibraryID ||
		!taskChildTerminal(child.state) || child.finishedAt == nil || child.scanned != int64(cached.Scanned) ||
		child.added != int64(cached.Added) || child.updated != int64(cached.Updated) || !sameScanTime(child.startedAt, cached.StartedAt) {
		return taskScanAssociationError()
	}
	if cached.Status != "Queued" && cached.Status != "Running" && !taskSnapshotMatches(child, cached) {
		return taskScanAssociationError()
	}
	return nil
}

func (s *Store) taskCancellationStatus(child *taskScanChild) (string, string) {
	if child != nil && (child.stopReason == "shutdown" || child.runState == "interrupted" || s.ctx.Err() != nil) {
		return "Interrupted", "Server stopped before the scan finished"
	}
	return "Cancelled", "Scan cancelled"
}

func (s *Store) enqueueScan(job Job) {
	taskCtx, cancel := context.WithCancel(s.ctx)
	task := &scanTask{job: job, ctx: taskCtx, cancel: cancel}
	s.active[job.ID] = task
	s.queue <- task
	s.notifyScanUpdate()
}

// AdmitTaskScan links a new scan and its child atomically, then publishes only
// that owned job after commit. An unrelated scan never becomes this child's job.
func (s *Store) AdmitTaskScan(ctx context.Context, childID string) (ScanAdmission, error) {
	if !validTaskChildID(childID) {
		return ScanAdmission{}, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ScanAdmission{}, ErrUnavailable
	}
	var result ScanAdmission
	var afterCommit error
	err := s.taskScanTransaction(ctx, func(tx OwnedTx) error {
		child, err := lockTaskScanChild(tx, childID)
		if err != nil {
			return err
		}
		if !activeTaskRun(child.runState) || taskChildTerminal(child.state) {
			return ErrTaskScanInactive
		}
		if child.scanID != "" {
			job, err := scanJob(tx.QueryRow("SELECT "+jobColumns+" FROM scan_jobs WHERE id = $1 FOR UPDATE", child.scanID))
			if err != nil {
				return errors.Join(taskScanAssociationError(), err)
			}
			if err := validateTaskScanLink(child, job); err != nil {
				return err
			}
			if !taskSnapshotMatches(child, job) {
				return taskScanAssociationError()
			}
			result = ScanAdmission{Kind: ScanAlreadyOwned, Job: job}
			return nil
		}
		if child.state != "waiting" {
			return taskScanAssociationError()
		}
		if err := ensureTaskChildUnlinked(tx, child); err != nil {
			return err
		}
		var libraryID string
		err = tx.QueryRow("SELECT id FROM libraries WHERE id = $1 FOR KEY SHARE", child.libraryID).Scan(&libraryID)
		if errors.Is(err, pgx.ErrNoRows) {
			_, err = tx.Exec(`UPDATE task_run_children SET state = 'unavailable', error_code = 'library_unavailable',
				error_message = 'Library is unavailable', finished_at = clock_timestamp()
				WHERE id = $1 AND state = 'waiting' AND scan_job_id IS NULL`, child.id)
			afterCommit = ErrNotFound
			return err
		}
		if err != nil {
			return err
		}
		active, err := scanJob(tx.QueryRow("SELECT "+jobColumns+" FROM scan_jobs WHERE library_id = $1 AND status IN ('Queued', 'Running')", libraryID))
		if err == nil {
			result = ScanAdmission{Kind: ScanDuplicate, Job: active}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if len(s.queue) == cap(s.queue) {
			result.Kind = ScanQueueFull
			return nil
		}
		id, err := randomID()
		if err != nil {
			return err
		}
		job, err := scanJob(tx.QueryRow(`INSERT INTO scan_jobs (id, library_id, status, task_child_id)
			VALUES ($1, $2, 'Queued', $3) RETURNING `+jobColumns, id, libraryID, child.id))
		if err != nil {
			return err
		}
		linked, err := tx.Exec(`UPDATE task_run_children SET state = 'queued', scan_job_id = $2
			WHERE id = $1 AND state = 'waiting' AND scan_job_id IS NULL`, child.id, job.ID)
		if err != nil {
			return err
		}
		if linked.RowsAffected() != 1 {
			return taskScanAssociationError()
		}
		if err := activity.RecordOwned(tx, catalogSystemEvent(activity.ActionScanRequested,
			activity.Resource{Kind: activity.ResourceScan, ID: job.ID})); err != nil {
			return err
		}
		result = ScanAdmission{Kind: ScanAdmitted, Job: job}
		return nil
	})
	if err != nil {
		return ScanAdmission{}, err
	}
	if afterCommit != nil {
		s.notifyScanUpdate()
		return ScanAdmission{}, afterCommit
	}
	if result.Kind == ScanAdmitted {
		s.enqueueScan(result.Job)
	}
	return result, nil
}

// CancelTaskScan revalidates the child-to-scan association before signalling
// local work. Unlinked children are the task repository's responsibility.
func (s *Store) CancelTaskScan(ctx context.Context, childID string) error {
	if !validTaskChildID(childID) {
		return ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var cancelID string
	err := s.taskScanTransaction(ctx, func(tx OwnedTx) error {
		child, err := lockTaskScanChild(tx, childID)
		if err != nil {
			return err
		}
		if child.scanID == "" {
			return ensureTaskChildUnlinked(tx, child)
		}
		job, err := scanJob(tx.QueryRow("SELECT "+jobColumns+" FROM scan_jobs WHERE id = $1 FOR UPDATE", child.scanID))
		if errors.Is(err, pgx.ErrNoRows) && taskChildTerminal(child.state) {
			return nil
		}
		if err != nil {
			return errors.Join(taskScanAssociationError(), err)
		}
		if err := validateTaskScanLink(child, job); err != nil {
			return err
		}
		if err := s.cancelLockedScan(tx, job, child); err != nil {
			return err
		}
		cancelID = job.ID
		return nil
	})
	if err != nil {
		return err
	}
	if task := s.active[cancelID]; task != nil {
		task.cancel()
	}
	s.notifyScanUpdate()
	return nil
}

func (s *Store) cancelLockedScan(tx OwnedTx, job Job, child *taskScanChild) error {
	return s.cancelLockedScanAs(tx, job, child, nil)
}

func (s *Store) cancelLockedScanAs(tx OwnedTx, job Job, child *taskScanChild, administrator *catalogAdministrator) error {
	if job.Status != "Queued" && job.Status != "Running" {
		return copyTaskScanSnapshot(tx, child, job)
	}
	if child != nil && taskChildTerminal(child.state) {
		return taskScanAssociationError()
	}
	status, message := s.taskCancellationStatus(child)
	changed, err := scanJob(tx.QueryRow(`UPDATE scan_jobs SET cancel_requested = true,
		status = CASE WHEN status = 'Queued' THEN $2 ELSE status END,
		error = CASE WHEN status = 'Queued' THEN $3 ELSE error END,
		finished_at = CASE WHEN status = 'Queued' THEN clock_timestamp() ELSE finished_at END
		WHERE id = $1 RETURNING `+jobColumns, job.ID, status, message))
	if err != nil {
		return err
	}
	if err := copyTaskScanSnapshot(tx, child, changed); err != nil {
		return err
	}
	if !job.CancelRequested {
		if err := activity.RecordOwned(tx, administrator.event(activity.ActionScanCancelRequested,
			activity.Resource{Kind: activity.ResourceScan, ID: job.ID})); err != nil {
			return err
		}
	}
	if changed.Status != "Queued" && changed.Status != "Running" {
		return recordScanFinished(tx, changed)
	}
	return nil
}

func (s *Store) startTaskScan(task *scanTask) (bool, error) {
	started := false
	var current Job
	err := s.taskScanTransaction(task.ctx, func(tx OwnedTx) error {
		relation, err := lockTaskScanRelation(tx, task.job.ID, task.job.TaskChildID)
		if err != nil {
			return err
		}
		if relation.missing {
			return validateRetainedTaskSnapshot(relation.child, task.job)
		}
		current = relation.job
		if current.Status != "Queued" {
			return copyTaskScanSnapshot(tx, relation.child, current)
		}
		if current.CancelRequested || relation.child != nil && !activeTaskRun(relation.child.runState) {
			if err := s.cancelLockedScan(tx, current, relation.child); err != nil {
				return err
			}
			current, err = scanJob(tx.QueryRow("SELECT "+jobColumns+" FROM scan_jobs WHERE id = $1", current.ID))
			return err
		}
		if relation.child != nil && relation.child.state != "queued" {
			return taskScanAssociationError()
		}
		current, err = scanJob(tx.QueryRow(`UPDATE scan_jobs SET status = 'Running', started_at = clock_timestamp()
			WHERE id = $1 AND status = 'Queued' AND NOT cancel_requested RETURNING `+jobColumns, current.ID))
		if err != nil {
			return err
		}
		if err := copyTaskScanSnapshot(tx, relation.child, current); err != nil {
			return err
		}
		started = true
		return nil
	})
	if err == nil && current.ID != "" {
		task.job = current
	}
	if err == nil {
		s.notifyScanUpdate()
	}
	return started, err
}

func (s *Store) recoverTaskScans(ctx context.Context) error {
	return s.taskScanTransaction(ctx, func(tx OwnedTx) error {
		rows, err := tx.Query(`SELECT scan.id, COALESCE(scan.task_child_id, ''), COALESCE(child.run_id, '')
			FROM scan_jobs scan LEFT JOIN task_run_children child ON child.id = scan.task_child_id
			WHERE scan.status IN ('Queued', 'Running') OR child.state IN ('queued', 'running')
			ORDER BY child.run_id NULLS FIRST, child.id NULLS FIRST, scan.id`)
		if err != nil {
			return err
		}
		type recovery struct{ job, child, run string }
		var pending []recovery
		var runIDs, childIDs, jobIDs []string
		for rows.Next() {
			var item recovery
			if err := rows.Scan(&item.job, &item.child, &item.run); err != nil {
				rows.Close()
				return err
			}
			pending = append(pending, item)
			jobIDs = append(jobIDs, item.job)
			if item.child != "" {
				childIDs = append(childIDs, item.child)
				runIDs = append(runIDs, item.run)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// Recovery observes every affected parent before locking any child or
		// scan, and completes before the task repository recovers parent runs.
		for _, group := range []struct {
			table string
			ids   []string
		}{{"task_runs", runIDs}, {"task_run_children", childIDs}, {"scan_jobs", jobIDs}} {
			if len(group.ids) == 0 {
				continue
			}
			locked, err := tx.Query("SELECT id FROM "+group.table+" WHERE id = ANY($1::text[]) ORDER BY id FOR UPDATE", group.ids)
			if err != nil {
				return err
			}
			for locked.Next() {
				var ignored string
				if err := locked.Scan(&ignored); err != nil {
					locked.Close()
					return err
				}
			}
			locked.Close()
			if err := locked.Err(); err != nil {
				return err
			}
		}
		for _, item := range pending {
			relation, err := lockTaskScanRelation(tx, item.job, item.child)
			if err != nil {
				return err
			}
			job := relation.job
			if job.Status == "Queued" || job.Status == "Running" {
				job, err = scanJob(tx.QueryRow(`UPDATE scan_jobs SET status = 'Interrupted',
					error = 'Server stopped before the scan finished', finished_at = clock_timestamp()
					WHERE id = $1 RETURNING `+jobColumns, job.ID))
				if err != nil {
					return err
				}
				if err := recordScanFinished(tx, job); err != nil {
					return err
				}
			}
			if err := copyTaskScanSnapshot(tx, relation.child, job); err != nil {
				return err
			}
		}
		return nil
	})
}

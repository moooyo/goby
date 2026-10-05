package library

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/systemevents"
)

const jobColumns = `id, library_id, status, error, scanned, added, updated, force_probe, created_at, started_at, finished_at,
	COALESCE(task_child_id, '') AS task_child_id, cancel_requested`

// StartScan durably queues work independently from the requesting HTTP context.
func (s *Store) StartScan(ctx context.Context, libraryID string) (Job, error) {
	return s.StartScanWithOptions(ctx, libraryID, ScanOptions{})
}

// StartScanWithOptions persists the requested probe policy with the queued job.
func (s *Store) StartScanWithOptions(ctx context.Context, libraryID string, options ScanOptions) (Job, error) {
	return s.startScan(ctx, nil, libraryID, options)
}

// StartScanAsAdministrator admits a manual scan and its activity in the owned
// transaction while retaining administrator authority until commit.
func (s *Store) StartScanAsAdministrator(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, libraryID string, options ScanOptions) (Job, error) {
	return s.startScan(ctx, &catalogAdministrator{actor: actor, audience: audience}, libraryID, options)
}

func (s *Store) startScan(ctx context.Context, administrator *catalogAdministrator, libraryID string, options ScanOptions) (Job, error) {
	if libraryID == collectionLibraryID {
		return Job{}, ErrNotFound
	}
	raw, err := s.beginOwnedAdmission(ctx, false)
	if err != nil {
		return Job{}, fmt.Errorf("queue library scan: %w", err)
	}
	defer s.mu.Unlock()
	defer rollback(raw)
	var job Job
	err = s.withOwnedTxCallback(raw, func(tx OwnedTx) error {
		authorization := catalogAuthorizationTx{tx: tx}
		if err := administrator.check(ctx, authorization, true); err != nil {
			return err
		}
		var existing string
		if err := tx.QueryRow("SELECT id FROM libraries WHERE id = $1 FOR UPDATE", libraryID).Scan(&existing); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if err := administrator.check(ctx, authorization, false); err != nil {
			return err
		}
		err := tx.QueryRow("SELECT id FROM scan_jobs WHERE library_id = $1 AND status IN ('Queued', 'Running')", libraryID).Scan(&existing)
		if err == nil {
			return ErrScanAlreadyActive
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var publicationActive bool
		if err := tx.QueryRow(mediaPublicationLibraryActiveSQL, libraryID).Scan(&publicationActive); err != nil {
			return err
		}
		if publicationActive {
			return ErrBusy
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
		if err != nil {
			return err
		}
		if err := activity.RecordOwned(tx, administrator.event(activity.ActionScanRequested,
			activity.Resource{Kind: activity.ResourceScan, ID: job.ID})); err != nil {
			return err
		}
		return administrator.check(ctx, authorization, false)
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
	return s.cancelJob(ctx, nil, id)
}

// CancelJobAsAdministrator revalidates the manual cancellation actor before
// mutating the owned scan and again after its activity has been recorded.
func (s *Store) CancelJobAsAdministrator(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, id string) error {
	return s.cancelJob(ctx, &catalogAdministrator{actor: actor, audience: audience}, id)
}

func (s *Store) cancelJob(ctx context.Context, administrator *catalogAdministrator, id string) error {
	raw, err := s.beginOwnedAdmission(ctx, true)
	if err != nil {
		return fmt.Errorf("request scan cancellation: %w", err)
	}
	defer s.mu.Unlock()
	defer rollback(raw)
	err = s.withOwnedTxCallback(raw, func(tx OwnedTx) error {
		authorization := catalogAuthorizationTx{tx: tx}
		if err := administrator.check(ctx, authorization, true); err != nil {
			return err
		}
		relation, err := lockTaskScanRelation(tx, id, "")
		if err != nil {
			return err
		}
		if err := administrator.check(ctx, authorization, false); err != nil {
			return err
		}
		if err := s.cancelLockedScanAs(tx, relation.job, relation.child, administrator); err != nil {
			return err
		}
		return administrator.check(ctx, authorization, false)
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
			task.recordSavedProgress(task.job, time.Now())
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
			slog.Warn("Retrying scan job finalization", "job_id", task.job.ID, "error_code", "scan_finalize_failed")
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
			return validateRetainedTaskSnapshot(relation.child, task.retainedProgressSnapshot())
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
		accepted := retainAcceptedScanProgress(task.job, finished)
		finished, err = scanJob(tx.QueryRow(`UPDATE scan_jobs SET status = $2, error = $3,
			scanned = $4, added = $5, updated = $6, finished_at = clock_timestamp()
			WHERE id = $1 RETURNING `+jobColumns, task.job.ID, status, message, accepted.Scanned, accepted.Added, accepted.Updated))
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
			var detectIntros, generatePreviews, generateBackgrounds, generateWaveforms, detectCredits, generateSubtitleTimelines bool
			if err := tx.QueryRow(`SELECT collection_type='tvshows'
				AND COALESCE((options->>'EnableIntroDetection')::boolean,false),
				collection_type IN ('movies','tvshows','mixed')
				AND COALESCE((options->>'EnablePreviewGeneration')::boolean,false),
				collection_type IN ('movies','tvshows','mixed')
				AND COALESCE((options->>'EnableBackgroundPreviewGeneration')::boolean,false),
				collection_type IN ('movies','tvshows','mixed')
				AND COALESCE((options->>'EnableAudioWaveformGeneration')::boolean,false),
				collection_type IN ('movies','tvshows','mixed')
				AND COALESCE((options->>'EnableCreditsDetection')::boolean,false),
				collection_type IN ('movies','tvshows','mixed')
				AND COALESCE((options->>'EnableSubtitleTimelineGeneration')::boolean,false)
				FROM libraries WHERE id=$1`, finished.LibraryID).Scan(&detectIntros, &generatePreviews, &generateBackgrounds, &generateWaveforms, &detectCredits, &generateSubtitleTimelines); err != nil {
				return err
			}
			if detectIntros {
				// A dedicated signal follows both independent and task-owned scans.
				// It must not re-emit LibraryChanged and recursively schedule scans.
				if err := systemevents.Record(tx.Exec, systemevents.IntroAnalysisRequested); err != nil {
					return err
				}
			}
			if generatePreviews {
				if err := systemevents.Record(tx.Exec, systemevents.PreviewGenerationRequested); err != nil {
					return err
				}
			}
			if generateBackgrounds {
				if err := systemevents.Record(tx.Exec, systemevents.BackgroundPreviewGenerationRequested); err != nil {
					return err
				}
			}
			if generateWaveforms {
				if err := systemevents.Record(tx.Exec, systemevents.AudioWaveformGenerationRequested); err != nil {
					return err
				}
			}
			if detectCredits {
				if err := systemevents.Record(tx.Exec, systemevents.CreditsAnalysisRequested); err != nil {
					return err
				}
			}
			if generateSubtitleTimelines {
				if err := systemevents.Record(tx.Exec, systemevents.SubtitleTimelineGenerationRequested); err != nil {
					return err
				}
			}
		}
		return recordScanFinished(tx, finished)
	})
	if err == nil {
		if finished.ID != "" {
			task.job = finished
			task.recordSavedProgress(finished, time.Now())
		}
		s.notifyScanUpdate()
	}
	return err
}

// A rejected publication can leave the worker behind a durable checkpoint.
// Repair and finalization keep that accepted prefix rather than overwriting it
// with the stale worker projection. Identity and state still come from the lock.
func retainAcceptedScanProgress(candidate, committed Job) Job {
	committed.Scanned = max(candidate.Scanned, committed.Scanned)
	committed.Added = max(candidate.Added, committed.Added)
	committed.Updated = max(candidate.Updated, committed.Updated)
	return committed
}

// lockScanPublicationProgress fixes scan authority before any publication root
// or item locks. The caller keeps this relation locked until its publication
// commits; the returned snapshot is not reusable outside that transaction.
func lockScanPublicationProgress(tx OwnedTx, task *scanTask) (taskScanRelation, error) {
	relation, err := lockTaskScanRelation(tx, task.job.ID, task.job.TaskChildID)
	if err != nil {
		return taskScanRelation{}, err
	}
	if relation.missing {
		if err := validateRetainedTaskSnapshot(relation.child, task.retainedProgressSnapshot()); err != nil {
			return taskScanRelation{}, err
		}
		return taskScanRelation{}, context.Canceled
	}
	if relation.job.ID != task.job.ID || relation.job.LibraryID != task.job.LibraryID ||
		relation.job.TaskChildID != task.job.TaskChildID || relation.job.ForceProbe != task.job.ForceProbe {
		return taskScanRelation{}, taskScanAssociationError()
	}
	if relation.job.Status != "Running" {
		return taskScanRelation{}, context.Canceled
	}
	if relation.job.CancelRequested || relation.child != nil &&
		(!activeTaskRun(relation.child.runState) || relation.child.state != "running") {
		return taskScanRelation{}, context.Canceled
	}
	if err := task.ctx.Err(); err != nil {
		return taskScanRelation{}, err
	}
	return relation, nil
}

// writeScanPublicationProgress keeps accepted counters and the child snapshot
// atomic with the existing publication. It never changes the worker's in-memory
// job: the caller may publish the returned projection only after Commit succeeds.
func writeScanPublicationProgress(tx OwnedTx, relation taskScanRelation, candidate Job) (Job, error) {
	if relation.missing || relation.job.Status != "Running" || relation.job.CancelRequested {
		return Job{}, context.Canceled
	}
	if relation.job.ID != candidate.ID || relation.job.LibraryID != candidate.LibraryID ||
		relation.job.TaskChildID != candidate.TaskChildID || relation.job.ForceProbe != candidate.ForceProbe {
		return Job{}, taskScanAssociationError()
	}
	// A stale checkpoint must not reduce an already committed prefix, even
	// when the current publication would otherwise succeed.
	if candidate.Scanned < relation.job.Scanned || candidate.Added < relation.job.Added || candidate.Updated < relation.job.Updated {
		return Job{}, fmt.Errorf("%w: scan publication counters would regress accepted progress", ErrUnavailable)
	}
	job := relation.job
	if job.Scanned != candidate.Scanned || job.Added != candidate.Added || job.Updated != candidate.Updated {
		var err error
		job, err = scanJob(tx.QueryRow(`UPDATE scan_jobs SET scanned = $2, added = $3, updated = $4
			WHERE id = $1 AND status = 'Running' AND NOT cancel_requested RETURNING `+jobColumns,
			job.ID, candidate.Scanned, candidate.Added, candidate.Updated))
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, context.Canceled
		}
		if err != nil {
			return Job{}, err
		}
	}
	if err := copyTaskScanSnapshot(tx, relation.child, job); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *Store) persistProgress(task *scanTask) error {
	var cancelled bool
	var accepted Job
	err := s.taskScanTransaction(task.ctx, func(tx OwnedTx) error {
		relation, err := lockTaskScanRelation(tx, task.job.ID, task.job.TaskChildID)
		if err != nil {
			return err
		}
		if relation.missing {
			cancelled = true
			return validateRetainedTaskSnapshot(relation.child, task.retainedProgressSnapshot())
		}
		if relation.job.Status != "Queued" && relation.job.Status != "Running" {
			cancelled = true
			return copyTaskScanSnapshot(tx, relation.child, relation.job)
		}
		cancelled = relation.job.CancelRequested || relation.child != nil && !activeTaskRun(relation.child.runState)
		accepted = retainAcceptedScanProgress(task.job, relation.job)
		// A second checkpoint for a cached media visit still fences ownership,
		// cancellation and the current parent/child association. Persist counters
		// only when that fresh locked snapshot differs from the accepted values.
		if relation.job.Scanned == accepted.Scanned && relation.job.Added == accepted.Added &&
			relation.job.Updated == accepted.Updated && relation.job.CancelRequested == cancelled {
			return copyTaskScanSnapshot(tx, relation.child, relation.job)
		}
		job, err := scanJob(tx.QueryRow(`UPDATE scan_jobs SET scanned = $2, added = $3, updated = $4,
			cancel_requested = cancel_requested OR $5 WHERE id = $1 RETURNING `+jobColumns,
			task.job.ID, accepted.Scanned, accepted.Added, accepted.Updated, cancelled))
		if err != nil {
			return err
		}
		if err := copyTaskScanSnapshot(tx, relation.child, job); err != nil {
			return err
		}
		if cancelled && !relation.job.CancelRequested {
			return activity.RecordOwned(tx, catalogSystemEvent(activity.ActionScanCancelRequested,
				activity.Resource{Kind: activity.ResourceScan, ID: job.ID}))
		}
		return nil
	})
	if err == nil {
		if accepted.ID != "" {
			task.job.Scanned, task.job.Added, task.job.Updated = accepted.Scanned, accepted.Added, accepted.Updated
			task.recordSavedProgress(accepted, time.Now())
		}
		s.notifyScanUpdate()
	}
	if err == nil && cancelled {
		task.cancel()
		return context.Canceled
	}
	return err
}

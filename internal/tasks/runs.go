package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

// Start admits a durable built-in library task or returns the run already
// associated with this request. A coalesced request receives its own receipt.
func (s *Store) Start(ctx context.Context, actor Actor, request StartRequest) (Admission, error) {
	request.AnalysisInput = cloneAnalysisSelection(request.AnalysisInput)
	if err := validateRequestID(request.RequestID); err != nil {
		return Admission{}, err
	}
	id, err := randomID()
	if err != nil {
		return Admission{}, err
	}
	source := "manual"
	if actor.Audience == identity.AdministratorEmby {
		source = "compatibility"
	}
	var result Admission
	err = s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if err := checkActor(tx, actor, true); err != nil {
			return err
		}
		var definition Definition
		if err := decodeRow(tx.QueryRow(`SELECT to_jsonb(d) FROM task_definitions d
            WHERE id = $1 FOR UPDATE`, request.TaskID), &definition); err != nil {
			return err
		}
		if err := checkActor(tx, actor, false); err != nil {
			return err
		}
		if err := s.checkTaskExecutor(definition.Key, actor); err != nil {
			return err
		}
		input, err := normalizedTaskAnalysis(definition.Key, request.AnalysisInput)
		if err != nil {
			return err
		}
		fingerprint := taskRequestFingerprint(request, definition.Key, source, input)
		if request.RequestID != "" {
			var priorID string
			var priorFingerprint []byte
			err := tx.QueryRow(`SELECT run_id, fingerprint FROM task_run_requests
                WHERE task_id = $1 AND request_id = $2`, definition.ID, request.RequestID).
				Scan(&priorID, &priorFingerprint)
			if err == nil {
				if !bytes.Equal(priorFingerprint, fingerprint[:]) {
					return ErrRequestConflict
				}
				run, err := readRun(tx, priorID, false)
				if err != nil {
					return err
				}
				result.Run = run
				return checkActor(tx, actor, false)
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("read task request receipt: %w", err)
			}
		}
		if !definition.Enabled {
			return ErrDisabled
		}
		binding, selectedLibraries, err := s.prepareAnalysisAdmission(tx, definition.Key, source, input, &actor)
		if err != nil {
			return err
		}
		var activeID string
		var activeRaw []byte
		err = tx.QueryRow(`SELECT id, to_jsonb(r) - 'request_fingerprint' FROM task_runs r WHERE task_id = $1
            AND state IN ('pending','running','stopping') FOR UPDATE`, definition.ID).Scan(&activeID, &activeRaw)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read active task run: %w", err)
		}
		if err := checkActor(tx, actor, false); err != nil {
			return err
		}
		if activeID != "" {
			run, err := decodeRunData(activeRaw)
			if err != nil {
				return err
			}
			if isAnalysisTask(definition.Key) && (!sameAnalysisInput(run.AnalysisInput, input) || run.AnalysisConfigFingerprint != binding.ConfigurationFingerprint) {
				return ErrActiveRunConflict
			}
			result.Run = run
		} else {
			// Receipt replay and active-run coalescing remain available after a
			// provider is disabled. A new manual run requires a usable executor.
			if entry, generic := s.executors.lookup(definition.Key); generic && !entry.Executor.Available() {
				return ErrUnavailable
			}
			var requestID any
			var initialFingerprint any
			if request.RequestID != "" {
				requestID, initialFingerprint = request.RequestID, fingerprint[:]
			}
			_, err := tx.Exec(`INSERT INTO task_runs (id,task_id,state,source,request_id,request_fingerprint,
                actor_user_id,actor_session_id,actor_kind,task_key,task_emby_key,task_name,
				analysis_input,analysis_config_fingerprint,actor_application_key_id,actor_client_session_id,actor_peer_ip)
                VALUES ($1,$2,'pending',$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, id, definition.ID, source,
				requestID, initialFingerprint, actor.Principal.User.ID, actor.Principal.SessionID,
				actor.Principal.Kind, definition.Key, definition.EmbyKey, definition.Name,
				analysisDatabaseInput(input), binding.ConfigurationFingerprint, actor.Principal.ApplicationKeyID, actor.Principal.ClientSessionID, actor.Principal.PeerIP)
			if err != nil {
				return fmt.Errorf("admit task run: %w", err)
			}
			// A per-run random namespace and stable library IDs give every
			// snapshot child an independent deterministic 32-hex identity.
			childCount, err := s.bindAnalysisChildren(tx, id, definition.Key, input, binding, selectedLibraries)
			if err != nil {
				return fmt.Errorf("snapshot task libraries: %w", err)
			}
			var run Run
			if err := decodeRow(tx.QueryRow(`UPDATE task_runs SET total_children = $2 WHERE id = $1
				RETURNING to_jsonb(task_runs) - 'request_fingerprint'`, id, childCount), &run); err != nil {
				return fmt.Errorf("record task child count: %w", err)
			}
			if err := recordTaskActivity(tx, &actor, activity.ActionTaskAdmitted, id,
				definition.Revision, childCount, ""); err != nil {
				return err
			}
			run, err = refreshLockedRun(tx, run)
			if err != nil {
				return err
			}
			result.Run, result.Admitted = run, true
		}
		if request.RequestID != "" {
			if _, err := tx.Exec(`INSERT INTO task_run_requests (task_id,request_id,run_id,fingerprint)
                VALUES ($1,$2,$3,$4)`, definition.ID, request.RequestID, result.Run.ID, fingerprint[:]); err != nil {
				return fmt.Errorf("persist task request receipt: %w", err)
			}
		}
		return checkActor(tx, actor, false)
	})
	if err != nil {
		return Admission{}, err
	}
	return result, nil
}

// Stop persists the run and owned-scan cancellation flags before returning.
// An application-owned coordinator must repeatedly signal stopping children.
func (s *Store) Stop(ctx context.Context, actor Actor, runID string) (Run, error) {
	return s.stopRun(ctx, &actor, runID, "administrator")
}

// StopByDefinition implements the compatibility boundary atomically. Pending
// and running runs project as Running; stopping or absent runs cannot be
// cancelled again through this boundary. Native Stop remains idempotent.
func (s *Store) StopByDefinition(ctx context.Context, actor Actor, taskID string) (Run, error) {
	var result Run
	err := s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if err := checkActor(tx, actor, true); err != nil {
			return err
		}
		var definitionID string
		if err := tx.QueryRow(`SELECT id FROM task_definitions WHERE id = $1 FOR UPDATE`, taskID).Scan(&definitionID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("lock task definition for stop: %w", err)
		}
		if err := checkActor(tx, actor, false); err != nil {
			return err
		}
		var run Run
		err := decodeRow(tx.QueryRow(`SELECT to_jsonb(r) - 'request_fingerprint' FROM task_runs r
			WHERE task_id = $1 AND state IN ('pending','running','stopping') FOR UPDATE`, definitionID), &run)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := checkActor(tx, actor, false); err != nil {
			return err
		}
		if errors.Is(err, ErrNotFound) || (run.State != RunPending && run.State != RunRunning) {
			return ErrNotRunning
		}
		result, err = stopLocked(tx, &actor, run, "administrator")
		if err != nil {
			return err
		}
		return checkActor(tx, actor, false)
	})
	if err != nil {
		return Run{}, err
	}
	return result, nil
}

// SystemStopRun is for the application's scheduler and shutdown lifecycle.
// It is separate from administrator authorization and accepts no HTTP actor.
func (s *Store) SystemStopRun(ctx context.Context, runID, reason string) (Run, error) {
	if reason != "shutdown" && reason != "max_runtime" {
		return Run{}, &ValidationError{Fields: map[string]string{"Reason": "system stop reason must be shutdown or max_runtime"}}
	}
	return s.stopRun(ctx, nil, runID, reason)
}

func (s *Store) stopRun(ctx context.Context, actor *Actor, runID, reason string) (Run, error) {
	var result Run
	err := s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if actor != nil {
			if err := checkActor(tx, *actor, true); err != nil {
				return err
			}
		}
		run, err := readRun(tx, runID, true)
		if err != nil {
			return err
		}
		if actor != nil {
			if err := checkActor(tx, *actor, false); err != nil {
				return err
			}
		}
		result, err = stopLocked(tx, actor, run, reason)
		if err != nil {
			return err
		}
		if actor != nil {
			return checkActor(tx, *actor, false)
		}
		return nil
	})
	if err != nil {
		return Run{}, err
	}
	return result, nil
}

// The caller already owns the parent run lock and performs any final actor
// check after this helper returns. No public store call or second transaction
// is allowed inside this shared native/compatibility/system stop path.
func stopLocked(tx library.OwnedTx, actor *Actor, run Run, reason string) (Run, error) {
	if !run.State.Active() {
		return run, nil
	}
	if err := lockRunChildrenAndScans(tx, run.ID, run.TaskKey); err != nil {
		return Run{}, err
	}
	if actor != nil {
		if err := checkActor(tx, *actor, false); err != nil {
			return Run{}, err
		}
	}
	var cancellationCount int64
	stoppingRun := run
	if run.State != RunStopping {
		counts, err := totals(tx, run.ID)
		if err != nil {
			return Run{}, err
		}
		cancellationCount = counts.total - counts.terminal
		if err := decodeRow(tx.QueryRow(`UPDATE task_runs SET state = 'stopping',
			stop_requested_at = COALESCE(stop_requested_at, clock_timestamp()),
			stop_reason = CASE WHEN stop_requested_at IS NULL THEN $2 ELSE stop_reason END
			WHERE id = $1 RETURNING to_jsonb(task_runs) - 'request_fingerprint'`, run.ID, reason), &stoppingRun); err != nil {
			return Run{}, fmt.Errorf("request task stop: %w", err)
		}
	}
	effectiveReason := run.StopReason
	if effectiveReason == "" {
		effectiveReason = reason
	}
	childState, childCode, childMessage := ChildCancelled, "cancelled", "Task stopped before this library scan was admitted."
	if effectiveReason == "shutdown" {
		childState, childCode, childMessage = ChildInterrupted, "server_interrupted", "The server stopped before this library scan was admitted."
	} else if effectiveReason == "max_runtime" {
		childCode, childMessage = "max_runtime", "The task reached its maximum runtime before this library scan was admitted."
	}
	if _, err := tx.Exec(`UPDATE task_run_children SET state = $2,
		finished_at = clock_timestamp(), error_code = $3, error_message = $4
		WHERE run_id = $1 AND state = 'waiting'`, run.ID, childState, childCode, childMessage); err != nil {
		return Run{}, fmt.Errorf("cancel unadmitted task children: %w", err)
	}
	scanIDs, err := cancelTaskScans(tx, run.ID)
	if err != nil {
		return Run{}, fmt.Errorf("persist owned scan cancellation: %w", err)
	}
	if run.State != RunStopping {
		if err := recordTaskActivity(tx, actor, activity.ActionTaskCancelRequested, run.ID,
			0, cancellationCount, ""); err != nil {
			return Run{}, err
		}
	}
	if err := recordTaskScanCancellations(tx, scanIDs); err != nil {
		return Run{}, err
	}
	if run.StopRequestedAt == nil && effectiveReason == "shutdown" && automaticAnalysisRun(run) {
		// The manager stops scheduling before its shutdown sweep. Persist the
		// next admission now, because graceful cleanup can finish the old run
		// before RecoverRuns sees it. Repeated stops retain the first request.
		if err := systemevents.Record(tx.Exec, analysisAutomation(run.TaskKey).event); err != nil {
			return Run{}, err
		}
	}
	return refreshLockedRun(tx, stoppingRun)
}

func automaticAnalysisRun(run Run) bool {
	return isAnalysisTask(run.TaskKey) && run.ActorKind == "system" &&
		(run.Source == "schedule" || run.Source == "startup" || run.Source == "system_event")
}

// BeginRun records runtime origin before the coordinator waits for scan slots.
func (s *Store) BeginRun(ctx context.Context, runID string) (Run, error) {
	var result Run
	err := s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		run, err := readRun(tx, runID, true)
		if err != nil {
			return err
		}
		if run.State != RunPending {
			result = run
			return nil
		}
		var started time.Time
		if err := tx.QueryRow(`SELECT clock_timestamp()`).Scan(&started); err != nil {
			return fmt.Errorf("read task start clock: %w", err)
		}
		var deadline *time.Time
		if run.MaxRuntimeTicks != nil {
			if *run.MaxRuntimeTicks <= 0 || *run.MaxRuntimeTicks > 92233720368547758 {
				return ErrInconsistent
			}
			value := started.Add(time.Duration(*run.MaxRuntimeTicks) * 100)
			deadline = &value
		}
		if _, err := tx.Exec(`UPDATE task_runs SET state = 'running', started_at = $2,
            deadline_at = $3 WHERE id = $1`, runID, started, deadline); err != nil {
			return fmt.Errorf("start task coordinator: %w", err)
		}
		result, err = readRun(tx, runID, false)
		return err
	})
	if err != nil {
		return Run{}, err
	}
	return result, nil
}

// RefreshRun recomputes aggregates from durable children instead of applying
// increments that could be counted twice after a retry or missed wake-up.
func (s *Store) RefreshRun(ctx context.Context, runID string) (Run, error) {
	var result Run
	err := s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		var err error
		result, err = refreshRun(tx, runID)
		return err
	})
	if err != nil {
		return Run{}, err
	}
	return result, nil
}

type childTotals struct {
	total, terminal, completed, failed, cancelled, interrupted, unavailable int64
	scanned, added, updated, warnings                                       int64
}

func totals(tx library.OwnedTx, runID string) (childTotals, error) {
	var value childTotals
	err := tx.QueryRow(`SELECT count(*),
        count(*) FILTER (WHERE state NOT IN ('waiting','queued','running')),
        count(*) FILTER (WHERE state = 'completed'), count(*) FILTER (WHERE state = 'failed'),
        count(*) FILTER (WHERE state = 'cancelled'), count(*) FILTER (WHERE state = 'interrupted'),
        count(*) FILTER (WHERE state = 'unavailable'),
        COALESCE(sum(scanned),0)::bigint, COALESCE(sum(added),0)::bigint, COALESCE(sum(updated),0)::bigint,
        count(*) FILTER (WHERE state = 'completed' AND error_message <> '')
        FROM task_run_children WHERE run_id = $1`, runID).
		Scan(&value.total, &value.terminal, &value.completed, &value.failed, &value.cancelled,
			&value.interrupted, &value.unavailable, &value.scanned, &value.added, &value.updated, &value.warnings)
	if err != nil {
		return childTotals{}, fmt.Errorf("aggregate task children: %w", err)
	}
	return value, nil
}

func refreshRun(tx library.OwnedTx, runID string) (Run, error) {
	run, err := readRun(tx, runID, true)
	if err != nil {
		return run, err
	}
	return refreshLockedRun(tx, run)
}

// The caller holds this Run's lock in the same transaction and has not changed
// the parent since reading it. Child mutations still require fresh totals.
func refreshLockedRun(tx library.OwnedTx, run Run) (Run, error) {
	if !run.State.Active() {
		return run, nil
	}
	counts, err := totals(tx, run.ID)
	if err != nil {
		return Run{}, err
	}
	if counts.total != run.TotalChildren {
		return Run{}, ErrInconsistent
	}
	state, code, message := run.State, run.ErrorCode, run.ErrorMessage
	if counts.terminal == counts.total {
		switch {
		case run.StopReason == "shutdown":
			state, code, message = RunInterrupted, "server_interrupted", "The server stopped before this task finished."
		case run.State == RunStopping:
			state, code, message = RunCancelled, "cancelled", "The task was cancelled."
			if run.StopReason == "max_runtime" {
				code, message = "max_runtime", "The task reached its maximum runtime."
			}
		case counts.failed > 0 || counts.unavailable > 0:
			state, code, message = RunFailed, "child_failed", "One or more task work items failed."
		case counts.interrupted > 0:
			state, code, message = RunInterrupted, "child_interrupted", "One or more task work items were interrupted."
		case counts.cancelled > 0:
			state, code, message = RunCancelled, "child_cancelled", "One or more task work items were cancelled."
		default:
			state, code, message = RunCompleted, "", ""
			if counts.warnings > 0 {
				code, message = "scan_warnings", "Some library scans completed with warnings."
			}
		}
	}
	// The run is locked and the totals come from durable children in this
	// transaction. An unchanged poll must not create a new parent row version.
	// Terminal transitions still pass through persistTotals and its side effects.
	if state == run.State && code == run.ErrorCode && message == run.ErrorMessage &&
		counts.total == run.TotalChildren && counts.terminal == run.TerminalChildren &&
		counts.completed == run.CompletedChildren && counts.failed == run.FailedChildren &&
		counts.cancelled == run.CancelledChildren && counts.interrupted == run.InterruptedChildren &&
		counts.unavailable == run.UnavailableChildren && counts.scanned == run.Scanned &&
		counts.added == run.Added && counts.updated == run.Updated {
		return run, nil
	}
	return persistTotals(tx, run.ID, state, code, message, counts)
}

func persistTotals(tx library.OwnedTx, runID string, state RunState, code, message string, counts childTotals) (Run, error) {
	statement := `UPDATE task_runs SET state = $2, error_code = $3, error_message = $4,
        total_children = $5, terminal_children = $6, completed_children = $7,
        failed_children = $8, cancelled_children = $9, interrupted_children = $10,
        unavailable_children = $11, scanned = $12, added = $13, updated = $14,
        started_at = CASE WHEN $2 = 'completed' AND started_at IS NULL THEN clock_timestamp() ELSE started_at END,
        finished_at = CASE WHEN $2 IN ('pending','running','stopping') THEN NULL
            ELSE COALESCE(finished_at,clock_timestamp()) END
        WHERE id = $1 AND state IN ('pending','running','stopping')`
	args := []any{runID, state, code, message,
		counts.total, counts.terminal, counts.completed, counts.failed, counts.cancelled,
		counts.interrupted, counts.unavailable, counts.scanned, counts.added, counts.updated}
	if state.Active() {
		if _, err := tx.Exec(statement, args...); err != nil {
			return Run{}, fmt.Errorf("persist task aggregate: %w", err)
		}
		// This may be the transaction's final SQL observation. Retain its
		// protected-context failure boundary before owned transaction commit.
		return readRun(tx, runID, false)
	}
	var run Run
	err := decodeRow(tx.QueryRow(statement+` RETURNING to_jsonb(task_runs) - 'request_fingerprint'`, args...), &run)
	if errors.Is(err, ErrNotFound) {
		// No transition occurred. Preserve the existing read result without
		// settling queues or recording a second completion event.
		return readRun(tx, runID, false)
	}
	if err != nil {
		return Run{}, fmt.Errorf("persist task aggregate: %w", err)
	}
	if err := settleBackgroundPreviewQueue(tx, run); err != nil {
		return Run{}, err
	}
	if err := settleAudioWaveformQueue(tx, run); err != nil {
		return Run{}, err
	}
	if err := settleSubtitleTimelineQueue(tx, run); err != nil {
		return Run{}, err
	}
	var revision int64
	if run.TriggerRevision != nil {
		revision = *run.TriggerRevision
	}
	if err := recordTaskActivity(tx, nil, activity.ActionTaskFinished, runID,
		revision, run.TerminalChildren, activity.State(run.State)); err != nil {
		return Run{}, err
	}
	return run, nil
}

// RecoverRuns runs only after scanner startup recovery and before scheduling.
// It never resumes an old execution or invents missing scan outcomes. Abandoned
// automatic analysis work requests a new admission with current library policy.
func (s *Store) RecoverRuns(ctx context.Context) error {
	return s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		rows, err := tx.Query(`SELECT id, task_key,
			source IN ('schedule','startup','system_event') AND actor_kind='system'
				AND stop_reason IN ('','shutdown') AS restart_automatic_analysis
			FROM task_runs WHERE state IN ('pending','running','stopping')
            ORDER BY id FOR UPDATE`)
		if err != nil {
			return fmt.Errorf("lock abandoned task runs: %w", err)
		}
		runs, err := collectAbandonedRuns(rows)
		if err != nil {
			return err
		}
		var requestIntro, requestPreview, requestCredits bool
		for _, run := range runs {
			if !run.restartAutomaticAnalysis {
				continue
			}
			switch run.taskKey {
			case library.TaskIntroAnalysisKey:
				requestIntro = true
			case library.TaskPreviewGenerationKey:
				requestPreview = true
			case library.TaskCreditsAnalysisKey:
				requestCredits = true
			}
		}
		if len(runs) == 0 {
			// An empty recovery has no later SQL. Retain the final observation
			// that can fail before an expired owned transaction reaches Commit.
			if err := tx.QueryRow(`SELECT COALESCE(bool_or(task_key=$2),false),COALESCE(bool_or(task_key=$3),false),COALESCE(bool_or(task_key=$4),false)
				FROM task_runs WHERE id=ANY($1::text[])
				AND source IN ('schedule','startup','system_event') AND actor_kind='system'
				AND stop_reason IN ('','shutdown')`, []string{}, library.TaskIntroAnalysisKey, library.TaskPreviewGenerationKey, library.TaskCreditsAnalysisKey).Scan(&requestIntro, &requestPreview, &requestCredits); err != nil {
				return fmt.Errorf("inspect abandoned automatic analysis work: %w", err)
			}
		}
		for _, run := range runs {
			id := run.id
			if err := lockRunChildrenAndScans(tx, id, run.taskKey); err != nil {
				return err
			}
			var activeLinked bool
			if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_run_children
                WHERE run_id = $1 AND scan_job_id IS NOT NULL AND state IN ('queued','running'))`, id).Scan(&activeLinked); err != nil {
				return fmt.Errorf("check recovered scan children: %w", err)
			}
			if activeLinked {
				return fmt.Errorf("%w: scanner recovery must finish linked children first", ErrInconsistent)
			}
			if _, err := tx.Exec(`UPDATE task_run_children SET state = 'interrupted',
                finished_at = clock_timestamp(), error_code = 'server_interrupted',
                error_message = 'The server stopped before this library scan was admitted.'
                WHERE run_id = $1 AND (state = 'waiting' OR (executor_token IS NOT NULL AND state IN ('queued','running')))`, id); err != nil {
				return fmt.Errorf("recover unadmitted task children: %w", err)
			}
			counts, err := totals(tx, id)
			if err != nil {
				return err
			}
			if _, err := persistTotals(tx, id, RunInterrupted, "server_interrupted",
				"The server stopped before this task finished.", counts); err != nil {
				return err
			}
		}
		if requestIntro {
			// Recovery and the replacement request commit together. A retry sees
			// terminal old runs and cannot issue this signal a second time.
			if err := systemevents.Record(tx.Exec, systemevents.IntroAnalysisRequested); err != nil {
				return err
			}
		}
		if requestPreview {
			if err := systemevents.Record(tx.Exec, systemevents.PreviewGenerationRequested); err != nil {
				return err
			}
		}
		if requestCredits {
			if err := systemevents.Record(tx.Exec, systemevents.CreditsAnalysisRequested); err != nil {
				return err
			}
		}
		return nil
	})
}

type abandonedRun struct {
	id, taskKey              string
	restartAutomaticAnalysis bool
}

func collectAbandonedRuns(rows library.OwnedRows) ([]abandonedRun, error) {
	defer rows.Close()
	runs := make([]abandonedRun, 0)
	for rows.Next() {
		var run abandonedRun
		if err := rows.Scan(&run.id, &run.taskKey, &run.restartAutomaticAnalysis); err != nil {
			return nil, fmt.Errorf("read locked task identity: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read task identities: %w", err)
	}
	return runs, nil
}

func decodeRunData(raw []byte) (Run, error) {
	var run Run
	if err := json.Unmarshal(raw, &run); err != nil {
		return Run{}, fmt.Errorf("decode task resource: %w", err)
	}
	normalizeRun(&run)
	return run, nil
}

func readRun(tx library.OwnedTx, id string, lock bool) (Run, error) {
	statement := `SELECT to_jsonb(r) - 'request_fingerprint' FROM task_runs r WHERE id = $1`
	if lock {
		statement += " FOR UPDATE"
	}
	var run Run
	err := decodeRow(tx.QueryRow(statement, id), &run)
	return run, err
}

// The parent run is already locked. Child and scan locks follow in a stable
// order; cancelled scans from another run or a standalone request are excluded.
func lockRunChildrenAndScans(tx library.OwnedTx, runID, taskKey string) error {
	// The caller holds the parent lock. Cancellation and startup recovery must
	// not relabel a normal scan as a forced refresh, or the reverse.
	options, supported := library.TaskScanOptions(taskKey)
	rows, err := tx.Query(`SELECT id FROM task_run_children WHERE run_id = $1 ORDER BY id FOR UPDATE`, runID)
	if err != nil {
		return fmt.Errorf("lock task children: %w", err)
	}
	if _, err := collectIDs(rows); err != nil {
		return err
	}
	if !supported {
		var invalid bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_run_children WHERE run_id=$1
			AND (scan_job_id IS NOT NULL OR (state IN ('queued','running') AND executor_token IS NULL)))`, runID).Scan(&invalid); err != nil {
			return err
		}
		if invalid {
			return ErrInconsistent
		}
		return nil
	}
	rows, err = tx.Query(`SELECT j.id FROM scan_jobs j JOIN task_run_children c
        ON c.id = j.task_child_id AND c.scan_job_id = j.id
        WHERE c.run_id = $1 ORDER BY j.id FOR UPDATE OF j`, runID)
	if err != nil {
		return fmt.Errorf("lock owned task scans: %w", err)
	}
	if _, err := collectIDs(rows); err != nil {
		return err
	}
	var inconsistent bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_run_children c LEFT JOIN scan_jobs j
        ON j.id = c.scan_job_id AND j.task_child_id = c.id WHERE c.run_id = $1
        AND ((c.state IN ('queued','running') AND j.id IS NULL)
            OR (j.id IS NOT NULL AND (j.library_id <> c.library_id OR j.force_probe <> $2))))`, runID, options.ForceProbe).Scan(&inconsistent); err != nil {
		return fmt.Errorf("check task scan ownership: %w", err)
	}
	if inconsistent {
		return ErrInconsistent
	}
	return nil
}

func collectIDs(rows library.OwnedRows) ([]string, error) {
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read locked task identity: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read task identities: %w", err)
	}
	return ids, nil
}

func validateRequestID(id string) error {
	if len(id) > MaxRequestIDBytes {
		return &ValidationError{Fields: map[string]string{"RequestId": "request ID must contain at most 128 visible ASCII bytes"}}
	}
	for _, value := range []byte(id) {
		if value < 33 || value > 126 {
			return &ValidationError{Fields: map[string]string{"RequestId": "request ID must contain only visible ASCII bytes"}}
		}
	}
	return nil
}

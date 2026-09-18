package tasks

import (
	"context"
	"errors"
	"fmt"

	"github.com/moooyo/goby/internal/library"
)

func (s *Store) snapshotChildren(tx library.OwnedTx, runID, key string) (int64, error) {
	entry, generic := s.executors.lookup(key)
	if generic && entry.Global {
		_, err := tx.Exec(`INSERT INTO task_run_children (id,run_id,library_id,library_name,ordinal)
            VALUES (md5($1 || ':global'),$1,'','Server',0)`, runID)
		return 1, err
	}
	children, err := tx.Exec(`INSERT INTO task_run_children (id,run_id,library_id,library_name,ordinal)
        SELECT md5($1 || ':' || id), $1, id, name,
            (row_number() OVER (ORDER BY id) - 1)::integer FROM libraries
        WHERE id <> $2`, runID, library.CollectionsLibraryID)
	if err != nil {
		return 0, fmt.Errorf("snapshot task libraries: %w", err)
	}
	// A due occurrence with no libraries must still record provider failure
	// when that executor is unavailable. The explicit unavailable child is
	// terminal already; it cannot later invoke a library executor with no ID.
	if children.RowsAffected() == 0 && generic && !entry.Executor.Available() {
		_, err := tx.Exec(`INSERT INTO task_run_children
            (id,run_id,library_id,library_name,ordinal,state,error_code,error_message,finished_at)
            VALUES (md5($1 || ':unavailable'),$1,'','Executor availability',0,'unavailable',
                'executor_unavailable','The configured executor is currently unavailable.',clock_timestamp())`, runID)
		return 1, err
	}
	return children.RowsAffected(), nil
}

func (s *Store) claimExecution(ctx context.Context, runID, childID, token string) (Child, error) {
	var child Child
	err := s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		run, err := readRun(tx, runID, true)
		if err != nil {
			return err
		}
		if run.State != RunRunning {
			return context.Canceled
		}
		if _, supported := library.TaskScanOptions(run.TaskKey); supported {
			return ErrInconsistent
		}
		if _, exists := s.executors.lookup(run.TaskKey); !exists {
			return ErrUnavailable
		}
		if err := decodeRow(tx.QueryRow(`SELECT to_jsonb(c) FROM task_run_children c
            WHERE id=$1 AND run_id=$2 FOR UPDATE`, childID, runID), &child); err != nil {
			return err
		}
		if child.State != ChildWaiting || child.ScanJobID != nil {
			return ErrInconsistent
		}
		_, err = tx.Exec(`UPDATE task_run_children SET state='running', executor_token=$2,
            started_at=clock_timestamp() WHERE id=$1 AND state='waiting'`, childID, token)
		return err
	})
	return child, err
}

func (s *Store) updateExecutionProgress(ctx context.Context, runID, childID, token string, value Progress) error {
	if !validProgress(value) {
		return ErrInvalidInput
	}
	return s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		run, err := readRun(tx, runID, true)
		if err != nil {
			return err
		}
		if run.State != RunRunning {
			return context.Canceled
		}
		changed, err := tx.Exec(`UPDATE task_run_children SET scanned=$4,added=$5,updated=$6
            WHERE id=$1 AND run_id=$2 AND executor_token=$3 AND scan_job_id IS NULL
              AND state='running' AND scanned <= $4 AND added <= $5 AND updated <= $6`,
			childID, runID, token, value.Processed, value.Added, value.Updated)
		if err != nil {
			return err
		}
		if changed.RowsAffected() != 1 {
			return ErrInconsistent
		}
		_, err = refreshRun(tx, runID)
		return err
	})
}

func (s *Store) finishExecution(ctx context.Context, runID, childID, token string, cause error, shutdown bool) error {
	return s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		run, err := readRun(tx, runID, true)
		if err != nil {
			return err
		}
		var stateBefore ChildState
		var claimedToken, scanJobID *string
		if err := tx.QueryRow(`SELECT state,executor_token,scan_job_id FROM task_run_children
            WHERE id=$1 AND run_id=$2 FOR UPDATE`, childID, runID).Scan(&stateBefore, &claimedToken, &scanJobID); err != nil {
			return err
		}
		if claimedToken == nil || *claimedToken != token || scanJobID != nil {
			return ErrInconsistent
		}
		if !stateBefore.Active() {
			return nil
		}
		if stateBefore != ChildRunning || !run.State.Active() {
			return ErrInconsistent
		}
		state, code, message := ChildCompleted, "", ""
		switch {
		case shutdown || run.StopReason == "shutdown":
			state, code, message = ChildInterrupted, "server_interrupted", "The server stopped before this work finished."
		case run.State == RunStopping || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded):
			state, code, message = ChildCancelled, "cancelled", "The work was cancelled."
			if run.StopReason == "max_runtime" {
				code, message = "max_runtime", "The work reached its maximum runtime."
			}
		case errors.Is(cause, ErrUnavailable):
			state, code, message = ChildUnavailable, "executor_unavailable", "The configured executor is currently unavailable."
		case cause != nil:
			state, code, message = ChildFailed, "executor_failed", "The executor could not finish this work."
		}
		changed, err := tx.Exec(`UPDATE task_run_children SET state=$4,error_code=$5,error_message=$6,
            finished_at=clock_timestamp() WHERE id=$1 AND run_id=$2 AND executor_token=$3
            AND scan_job_id IS NULL AND state='running'`, childID, runID, token, state, code, message)
		if err != nil {
			return err
		}
		if changed.RowsAffected() != 1 {
			return ErrInconsistent
		}
		_, err = refreshRun(tx, runID)
		return err
	})
}

package library

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// MediaOperationStatus contains coordinator polling facts. It cannot replace
// a complete work record when compensation needs the current private journal.
type MediaOperationStatus struct {
	ID                string
	State             string
	WorkerToken       string
	CancelRequestedAt *time.Time
	PublicationPhase  string
}

// Worker presence affects cleanup capabilities even though the token itself
// never appears in the HTTP projection.
const mediaOperationSummaryColumns = mediaOperationPublicColumns + `,o.worker_token`

const mediaOperationStatusColumns = `o.id,o.state,o.parameters,o.worker_token,o.cancel_requested_at,o.publication_phase,
	o.request_actor_id,o.request_credential_id,o.apply_actor_id,o.apply_credential_id`

const mediaOperationPendingPredicate = `worker_token='' AND (state IN ('queued','applying') OR (state IN ('ready','interrupted') AND cancel_requested_at IS NOT NULL))
	AND (NOT $2::boolean OR (cancel_requested_at IS NOT NULL AND publication_phase='none'))`

func scanMediaOperationSummary(row rowScanner) (MediaOperation, error) {
	var op MediaOperation
	var parameters []byte
	err := row.Scan(&op.ID, &op.Kind, &op.State, &op.Revision, &op.ItemID, &op.LibraryID, &op.RootID,
		&op.MediaSourceID, &op.SourceRevision, &op.StreamIndex, &parameters, &op.Progress.Stage, &op.Progress.Processed, &op.Progress.Total,
		&op.ResultSummary, &op.ResultHash, &op.PublicationPhase, &op.CancelRequestedAt, &op.CreatedAt, &op.UpdatedAt, &op.StartedAt, &op.FinishedAt,
		&op.ErrorCode, &op.ErrorMessage, &op.TargetPresent, &op.WorkerToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return op, ErrNotFound
	}
	if err != nil {
		return op, err
	}
	if json.Unmarshal(parameters, &op.Parameters) != nil || !mediaOperationStateValid(op.State) {
		return op, ErrUnavailable
	}
	projectMediaOperation(&op)
	return op, nil
}

func mediaOperationReadProjection(summary bool) (string, func(rowScanner) (MediaOperation, error)) {
	if summary {
		return mediaOperationSummaryColumns, scanMediaOperationSummary
	}
	return mediaOperationColumns, scanMediaOperation
}

func readMediaOperationProjection(ctx context.Context, tx mediaOperationQuerier, id string, summary bool) (MediaOperation, error) {
	columns, scan := mediaOperationReadProjection(summary)
	return scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM media_operations o WHERE o.id=$1", id))
}

func scanMediaOperationStatus(row rowScanner) (MediaOperation, error) {
	var op MediaOperation
	var parameters []byte
	err := row.Scan(&op.ID, &op.State, &parameters, &op.WorkerToken, &op.CancelRequestedAt, &op.PublicationPhase,
		&op.RequestActor.User.ID, &op.RequestActor.SessionID, &op.ApplyActor.User.ID, &op.ApplyActor.SessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return op, ErrNotFound
	}
	if err != nil {
		return op, err
	}
	if json.Unmarshal(parameters, &op.Parameters) != nil || !mediaOperationStateValid(op.State) {
		return op, ErrUnavailable
	}
	op.RequestActor.Kind, op.ApplyActor.Kind = "admin", "admin"
	return op, nil
}

// PendingMediaOperationIDs validates every selected candidate before returning
// IDs. ClaimMediaOperation later reads and locks the complete current record.
func (s *Store) PendingMediaOperationIDs(ctx context.Context, limit int) ([]string, error) {
	return s.pendingMediaOperationIDs(ctx, limit, false)
}

// PendingMediaOperationCancellationIDs gives explicit cleanup the same priority
// and dormant-inventory isolation as the full-record cancellation page.
func (s *Store) PendingMediaOperationCancellationIDs(ctx context.Context, limit int) ([]string, error) {
	return s.pendingMediaOperationIDs(ctx, limit, true)
}

func (s *Store) pendingMediaOperationIDs(ctx context.Context, limit int, cancellationOnly bool) ([]string, error) {
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	if limit < 1 || limit > 128 {
		return nil, ErrInvalidInput
	}
	rows, err := s.pool.Query(ctx, `SELECT o.id,o.state,o.parameters FROM media_operations o WHERE `+mediaOperationPendingPredicate+`
		ORDER BY (cancel_requested_at IS NOT NULL) DESC,created_at,id LIMIT $1`, limit, cancellationOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var id, state string
		var raw []byte
		var parameters MediaOperationParameters
		if err = rows.Scan(&id, &state, &raw); err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &parameters) != nil || !mediaOperationStateValid(state) {
			return nil, ErrUnavailable
		}
		result = append(result, id)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

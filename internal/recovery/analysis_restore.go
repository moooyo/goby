package recovery

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
)

type analysisRestoreResult struct {
	PublicationEpoch   int64
	DisabledDetections int64
	RemovedPreviews    int64
	RemovedFeatures    int64
}

// normalizeRestoredAnalysis runs after the original archive's complete raw
// witness has passed, in the same finalizer transaction as credential recovery.
// Historical admission/evidence and explicit decisions survive. No old proof
// can publish, and no incoming row can name an active filesystem derivative.
// Epoch exhaustion rejects the transaction rather than wrapping or adopting an
// old authority. Task interruption remains owned by normalizeRestoredWork.
func normalizeRestoredAnalysis(ctx context.Context, tx pgx.Tx) (analysisRestoreResult, error) {
	var result analysisRestoreResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if tx == nil {
		return result, ErrInvalid
	}
	var previous int64
	if err := tx.QueryRow(ctx, `SELECT publication_epoch FROM analysis_settings WHERE id=1 FOR UPDATE`).Scan(&previous); err != nil {
		return result, analysisRestoreError(ctx, err)
	}
	if previous < 1 || previous == math.MaxInt64 {
		return result, fmt.Errorf("%w: media analysis publication epoch cannot advance", ErrConflict)
	}
	if tag, err := tx.Exec(ctx, `UPDATE analysis_settings SET publication_epoch=publication_epoch+1 WHERE id=1 AND publication_epoch=$1`, previous); err != nil || tag.RowsAffected() != 1 {
		return result, analysisRestoreError(ctx, err)
	}
	result.PublicationEpoch = previous + 1
	tag, err := tx.Exec(ctx, `UPDATE analysis_detections SET auto_published=false WHERE auto_published`)
	if err != nil {
		return analysisRestoreResult{}, analysisRestoreError(ctx, err)
	}
	result.DisabledDetections = tag.RowsAffected()
	tag, err = tx.Exec(ctx, `DELETE FROM analysis_previews`)
	if err != nil {
		return analysisRestoreResult{}, analysisRestoreError(ctx, err)
	}
	result.RemovedPreviews = tag.RowsAffected()
	tag, err = tx.Exec(ctx, `DELETE FROM analysis_feature_cache`)
	if err != nil {
		return analysisRestoreResult{}, analysisRestoreError(ctx, err)
	}
	result.RemovedFeatures = tag.RowsAffected()
	if err := ctx.Err(); err != nil {
		return analysisRestoreResult{}, err
	}
	return result, nil
}

func analysisRestoreError(ctx context.Context, err error) error {
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return ErrUnavailable
}

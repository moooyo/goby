package recovery

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type analysisRestoreTestTx struct {
	pgx.Tx
	epoch    int64
	queryErr error
	execErr  error
	writes   int
	counts   []string
}
type analysisRestoreTestRow struct {
	epoch int64
	err   error
}

func (row analysisRestoreTestRow) Scan(target ...any) error {
	if row.err != nil {
		return row.err
	}
	*target[0].(*int64) = row.epoch
	return nil
}
func (tx *analysisRestoreTestTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return analysisRestoreTestRow{tx.epoch, tx.queryErr}
}
func (tx *analysisRestoreTestTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tx.writes++
	if tx.execErr != nil {
		return pgconn.CommandTag{}, tx.execErr
	}
	count := "UPDATE 1"
	if len(tx.counts) >= tx.writes {
		count = tx.counts[tx.writes-1]
	}
	return pgconn.NewCommandTag(count), nil
}

func TestAnalysisRestoreAdvancesEpochAndReportsOnlyActualInvalidation(t *testing.T) {
	tx := &analysisRestoreTestTx{epoch: 9007199254740993, counts: []string{"UPDATE 1", "UPDATE 3", "DELETE 9", "DELETE 7"}}
	result, err := normalizeRestoredAnalysis(context.Background(), tx)
	if err != nil || result.PublicationEpoch != 9007199254740994 || result.DisabledDetections != 3 || result.RemovedPreviews != 9 || result.RemovedFeatures != 7 || tx.writes != 4 {
		t.Fatalf("analysis normalization=%+v error=%v writes=%d", result, err, tx.writes)
	}
}

func TestAnalysisRestoreRejectsEpochExhaustionBeforeInvalidationWrites(t *testing.T) {
	for _, epoch := range []int64{0, -1, math.MaxInt64} {
		tx := &analysisRestoreTestTx{epoch: epoch}
		if _, err := normalizeRestoredAnalysis(context.Background(), tx); !errors.Is(err, ErrConflict) || tx.writes != 0 {
			t.Fatal("unadvanceable analysis epoch reached mutation")
		}
	}
}

func TestAnalysisRestorePreservesCancellationAndNeverReturnsPartialSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &analysisRestoreTestTx{epoch: 1}
	if _, err := normalizeRestoredAnalysis(ctx, tx); !errors.Is(err, context.Canceled) || tx.writes != 0 {
		t.Fatal("canceled restore performed analysis writes")
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errors.New("private database detail")} {
		tx := &analysisRestoreTestTx{epoch: 1, execErr: cause}
		result, err := normalizeRestoredAnalysis(context.Background(), tx)
		want := cause
		if !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) {
			want = ErrUnavailable
		}
		if !errors.Is(err, want) || result != (analysisRestoreResult{}) {
			t.Fatal("failed analysis normalization returned partial success or private driver detail")
		}
	}
}

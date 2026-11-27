package library

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAnalysisAdmissionReserveExhaustionIsNotOwnershipLoss(t *testing.T) {
	tx, driver, events := callbackFixture(t)
	driver.query = func(_ context.Context, statement string, _ ...any) (pgx.Rows, error) {
		if statement != `SELECT current_setting('statement_timeout')` {
			t.Fatalf("unexpected query: %s", statement)
		}
		return &ownedCallbackRowsFixture{next: func() bool { return true }, scan: func(dest ...any) error {
			*dest[0].(*string) = "0"
			return nil
		}}, nil
	}
	driver.exec = func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		t.Fatal("admission issued SQL after reaching the rollback reserve")
		return pgconn.CommandTag{}, nil
	}
	refresh, _, err := analysisAdmissionSQLBudget(tx)
	if err != nil {
		t.Fatal(err)
	}
	// Inside the two-second rollback reserve, without waiting for a timer.
	bounded, cancel := context.WithTimeout(tx.ctx, time.Second)
	defer cancel()
	tx.ctx = bounded
	err = tx.finish(refresh())
	if !errors.Is(err, ErrAnalysisAdmissionBudget) || errors.Is(err, ErrUnavailable) || !slices.Equal(*events, []string{"rollback"}) {
		t.Fatalf("reserve exhaustion did not roll back as a scoped admission failure: events=%v error=%v", *events, err)
	}
}

func TestAnalysisSnapshotCancellationKeepsCauseAndChecksOwnership(t *testing.T) {
	for _, lost := range []bool{false, true} {
		tx, driver, events := callbackFixture(t)
		cancelled := &pgconn.PgError{Code: "57014"}
		driver.query = func(_ context.Context, statement string, _ ...any) (pgx.Rows, error) {
			if statement == `SELECT current_setting('statement_timeout')` {
				return &ownedCallbackRowsFixture{next: func() bool { return true }, scan: func(dest ...any) error {
					*dest[0].(*string) = "0"
					return nil
				}}, nil
			}
			if lost {
				return nil, errors.Join(cancelled, ErrUnavailable)
			}
			return nil, cancelled
		}
		_, err := snapshotAnalysisChildren(tx, "unused-run", TaskPreviewGenerationKey, AnalysisSelection{}, DefaultAnalysisProfile())
		err = tx.finish(err)
		if !errors.Is(err, cancelled) || errors.Is(err, ErrUnavailable) != lost || errors.Is(err, ErrAnalysisAdmissionBudget) == lost || !slices.Equal(*events, []string{"rollback"}) {
			t.Fatalf("unexpected classification after cancellation (owner lost=%t): events=%v error=%v", lost, *events, err)
		}
	}
}

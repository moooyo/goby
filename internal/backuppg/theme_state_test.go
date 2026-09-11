package backuppg

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type themeStateTestTx struct {
	pgx.Tx
	row     themeStateTestRow
	queries int
}

func (tx *themeStateTestTx) QueryRow(context.Context, string, ...any) pgx.Row {
	tx.queries++
	return tx.row
}

type themeStateTestRow struct {
	valid  bool
	err    error
	onScan func()
}

func (row themeStateTestRow) Scan(dest ...any) error {
	if row.onScan != nil {
		row.onScan()
	}
	if row.err != nil {
		return row.err
	}
	*dest[0].(*bool) = row.valid
	return nil
}

func TestValidateThemeStatePreservesCallerContextBeforeValidation(t *testing.T) {
	for _, version := range []int64{23, 24, 25, 26} {
		for _, expired := range []bool{false, true} {
			t.Run(fmt.Sprintf("schema_%d/deadline_%t", version, expired), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				want := context.Canceled
				if expired {
					ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
					defer cancel()
					want = context.DeadlineExceeded
				}
				tx := &themeStateTestTx{row: themeStateTestRow{valid: true}}
				if err := validateThemeState(ctx, tx, version); !errors.Is(err, want) {
					t.Fatalf("caller context was replaced: got %v, want %v", err, want)
				}
				if tx.queries != 0 {
					t.Fatal("an already cancelled caller reached the semantic query")
				}
			})
		}
	}
	for _, version := range []int64{23, 24, 25} {
		if err := validateThemeState(context.Background(), nil, version); err != nil {
			t.Fatalf("schema %d queried Theme tables before their migration: %v", version, err)
		}
	}
}

func TestValidateThemeStatePreservesCancellationDuringValidation(t *testing.T) {
	for _, fixture := range []struct {
		name  string
		valid bool
		err   error
	}{
		{name: "valid_result", valid: true},
		{name: "invalid_result"},
		{name: "database_error", err: errors.New("connection interrupted")},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tx := &themeStateTestTx{row: themeStateTestRow{valid: fixture.valid, err: fixture.err, onScan: cancel}}
			if err := validateThemeState(ctx, tx, 26); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation during the semantic read was replaced: %v", err)
			}
			if tx.queries != 1 {
				t.Fatalf("semantic query count = %d, want 1", tx.queries)
			}
		})
	}
}

func TestValidateThemeStateClassifiesSemanticAndDatabaseErrors(t *testing.T) {
	for _, fixture := range []struct {
		name  string
		valid bool
		err   error
		want  error
	}{
		{name: "valid", valid: true},
		{name: "invalid_theme_state", want: ErrSchema},
		{name: "database_error", err: errors.New("database unavailable"), want: ErrDatabase},
		{name: "wrapped_cancellation", err: fmt.Errorf("driver: %w", context.Canceled), want: context.Canceled},
		{name: "wrapped_deadline", err: fmt.Errorf("driver: %w", context.DeadlineExceeded), want: context.DeadlineExceeded},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			tx := &themeStateTestTx{row: themeStateTestRow{valid: fixture.valid, err: fixture.err}}
			if err := validateThemeState(context.Background(), tx, 26); !errors.Is(err, fixture.want) {
				t.Fatalf("semantic error classification = %v, want %v", err, fixture.want)
			}
			if tx.queries != 1 {
				t.Fatalf("semantic query count = %d, want 1", tx.queries)
			}
		})
	}
}

package backuppg

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type resourceStateTestTx struct {
	pgx.Tx
	rows    []themeStateTestRow
	queries int
}

func (tx *resourceStateTestTx) QueryRow(context.Context, string, ...any) pgx.Row {
	row := tx.rows[tx.queries]
	tx.queries++
	return row
}

func TestValidateResourceStatePreservesHistoricalSchemaBoundaries(t *testing.T) {
	for _, version := range []int64{23, 24, 25, 26, 27} {
		t.Run(fmt.Sprintf("schema_%d", version), func(t *testing.T) {
			tx := &resourceStateTestTx{rows: []themeStateTestRow{{valid: true}, {valid: true}}}
			if err := validateResourceState(context.Background(), tx, version); err != nil {
				t.Fatalf("validate historical semantic boundary: %v", err)
			}
			want := 0
			if version >= 26 {
				want++
			}
			if version >= 27 {
				want++
			}
			if tx.queries != want {
				t.Fatalf("schema %d queried tables outside its migration prefix: got %d queries, want %d", version, tx.queries, want)
			}
			ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
			defer cancel()
			if err := validateResourceState(ctx, nil, version); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("historical validation discarded an expired caller context: %v", err)
			}
		})
	}
}

func TestValidateExtraStatePreservesContextAndErrorClassification(t *testing.T) {
	for _, test := range []struct {
		name   string
		valid  bool
		err    error
		cancel bool
		want   error
	}{
		{name: "valid", valid: true},
		{name: "invalid_extra_state", want: ErrSchema},
		{name: "database_failure", err: errors.New("connection unavailable"), want: ErrDatabase},
		{name: "wrapped_cancel", err: fmt.Errorf("driver: %w", context.Canceled), want: context.Canceled},
		{name: "wrapped_deadline", err: fmt.Errorf("driver: %w", context.DeadlineExceeded), want: context.DeadlineExceeded},
		{name: "cancel_valid_read", valid: true, cancel: true, want: context.Canceled},
		{name: "cancel_invalid_read", cancel: true, want: context.Canceled},
		{name: "cancel_database_failure", err: errors.New("connection interrupted"), cancel: true, want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			row := themeStateTestRow{valid: test.valid, err: test.err}
			if test.cancel {
				row.onScan = cancel
			}
			tx := &themeStateTestTx{row: row}
			if err := validateExtraState(ctx, tx, 27); !errors.Is(err, test.want) {
				t.Fatalf("extra-state error classification = %v, want %v", err, test.want)
			}
			if tx.queries != 1 {
				t.Fatal("extra validation did not execute exactly one semantic read")
			}
		})
	}
}

func TestValidateResourceStateRejectsEitherInvalidClassification(t *testing.T) {
	for _, test := range []struct {
		name  string
		rows  []themeStateTestRow
		count int
	}{
		{name: "theme", rows: []themeStateTestRow{{valid: false}}, count: 1},
		{name: "extra", rows: []themeStateTestRow{{valid: true}, {valid: false}}, count: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &resourceStateTestTx{rows: test.rows}
			if err := validateResourceState(context.Background(), tx, 27); !errors.Is(err, ErrSchema) || tx.queries != test.count {
				t.Fatalf("combined validation accepted a corrupt %s classification or queried past failure: %v", test.name, err)
			}
		})
	}
}

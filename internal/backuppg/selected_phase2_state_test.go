package backuppg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type selectedPhase2StateTestTx struct {
	themeStateTestTx
	statement string
}

func (tx *selectedPhase2StateTestTx) QueryRow(ctx context.Context, statement string, args ...any) pgx.Row {
	tx.statement = statement
	return tx.themeStateTestTx.QueryRow(ctx, statement, args...)
}

func TestValidateSelectedPhase2StatePreservesMigrationBoundaries(t *testing.T) {
	for _, version := range []int64{25, 41, 43, 44, 45} {
		t.Run(fmt.Sprintf("schema_%d", version), func(t *testing.T) {
			tx := &selectedPhase2StateTestTx{themeStateTestTx: themeStateTestTx{row: themeStateTestRow{valid: true}}}
			if err := validateSelectedPhase2State(context.Background(), tx, version); err != nil {
				t.Fatalf("validate media processing schema boundary: %v", err)
			}
			wantQueries := 0
			if version >= 44 {
				wantQueries = 1
			}
			if tx.queries != wantQueries {
				t.Fatalf("schema %d semantic reads = %d, want %d", version, tx.queries, wantQueries)
			}
			for _, table := range []string{"media_operations", "media_operation_cues", "item_owned_subtitles"} {
				if strings.Contains(tx.statement, table) != (version >= 44) {
					t.Fatalf("schema %d crossed the %s migration boundary", version, table)
				}
			}
			if strings.Contains(tx.statement, "item_embedded_artwork") != (version >= 45) {
				t.Fatalf("schema %d crossed the embedded artwork migration boundary", version)
			}
			if version < 44 {
				if err := validateSelectedPhase2State(context.Background(), nil, version); err != nil {
					t.Fatalf("historical schema %d required media processing tables: %v", version, err)
				}
			} else if err := validateSelectedPhase2State(context.Background(), nil, version); !errors.Is(err, ErrDatabase) {
				t.Fatalf("missing current transaction returned %v, want database failure", err)
			}
		})
	}
}

func TestValidateSelectedPhase2StatePreservesContextBeforeValidation(t *testing.T) {
	for _, version := range []int64{43, 44, 45} {
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
				if err := validateSelectedPhase2State(ctx, tx, version); !errors.Is(err, want) {
					t.Fatalf("caller context was replaced: got %v, want %v", err, want)
				}
				if tx.queries != 0 {
					t.Fatal("an expired caller reached the media processing semantic read")
				}
			})
		}
	}
}

func TestValidateSelectedPhase2StateClassifiesReadResults(t *testing.T) {
	for _, version := range []int64{44, 45} {
		for _, fixture := range []struct {
			name   string
			valid  bool
			err    error
			cancel bool
			want   error
		}{
			{name: "valid", valid: true},
			{name: "invalid_state", want: ErrSchema},
			{name: "database_failure", err: errors.New("connection unavailable"), want: ErrDatabase},
			{name: "wrapped_cancellation", err: fmt.Errorf("driver: %w", context.Canceled), want: context.Canceled},
			{name: "wrapped_deadline", err: fmt.Errorf("driver: %w", context.DeadlineExceeded), want: context.DeadlineExceeded},
			{name: "cancel_valid_read", valid: true, cancel: true, want: context.Canceled},
			{name: "cancel_invalid_read", cancel: true, want: context.Canceled},
			{name: "cancel_failed_read", err: errors.New("query interrupted"), cancel: true, want: context.Canceled},
		} {
			t.Run(fmt.Sprintf("schema_%d/%s", version, fixture.name), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				row := themeStateTestRow{valid: fixture.valid, err: fixture.err}
				if fixture.cancel {
					row.onScan = cancel
				}
				tx := &themeStateTestTx{row: row}
				if err := validateSelectedPhase2State(ctx, tx, version); !errors.Is(err, fixture.want) {
					t.Fatalf("media processing error classification = %v, want %v", err, fixture.want)
				}
				if tx.queries != 1 {
					t.Fatalf("media processing semantic reads = %d, want 1", tx.queries)
				}
			})
		}
	}
}

func TestValidateResourceStateIncludesSelectedPhase2AfterRootBindings(t *testing.T) {
	for _, version := range []int64{43, 44, 45} {
		for _, valid := range []bool{true, false} {
			t.Run(fmt.Sprintf("schema_%d/valid_%t", version, valid), func(t *testing.T) {
				tx := &rootBindingResourceTestTx{
					rows:       &rootBindingResourceTestRows{},
					rowResults: []themeStateTestRow{{valid: true}, {valid: true}, {valid: valid}},
				}
				var want error
				wantRows := 2
				if version >= 44 {
					wantRows++
					if !valid {
						want = ErrSchema
					}
				}
				if err := validateResourceState(context.Background(), tx, version); !errors.Is(err, want) {
					t.Fatalf("combined media processing gate returned %v, want %v", err, want)
				}
				if tx.rowQueries != wantRows || tx.queries != 1 {
					t.Fatalf("combined semantic reads = %d rows, %d bindings", tx.rowQueries, tx.queries)
				}
			})
		}
	}
}

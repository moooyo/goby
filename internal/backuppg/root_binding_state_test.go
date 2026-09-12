package backuppg

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

type rootBindingResourceTestTx struct {
	pgx.Tx
	rows       *rootBindingResourceTestRows
	queryErr   error
	onQuery    func()
	rowResults []themeStateTestRow
	queries    int
	rowQueries int
}

func (tx *rootBindingResourceTestTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	tx.queries++
	if tx.onQuery != nil {
		tx.onQuery()
	}
	return tx.rows, tx.queryErr
}

func (tx *rootBindingResourceTestTx) QueryRow(context.Context, string, ...any) pgx.Row {
	row := tx.rowResults[tx.rowQueries]
	tx.rowQueries++
	return row
}

type rootBindingResourceTestRows struct {
	pgx.Rows
	hasRow      bool
	emitted     bool
	coherent    bool
	document    *string
	scanErr     error
	terminalErr error
	closed      bool
}

func (rows *rootBindingResourceTestRows) Next() bool {
	if !rows.hasRow || rows.emitted {
		return false
	}
	rows.emitted = true
	return true
}

func (rows *rootBindingResourceTestRows) Scan(destinations ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	if len(destinations) != 5 {
		return errors.New("unexpected binding state fixture columns")
	}
	*destinations[0].(*bool) = rows.coherent
	*destinations[1].(**string) = rows.document
	if rows.document != nil {
		approved, registered, relative := "/approved", "/approved/library", "library"
		*destinations[2].(**string) = &approved
		*destinations[3].(**string) = &registered
		*destinations[4].(**string) = &relative
	}
	return nil
}

func (rows *rootBindingResourceTestRows) Err() error { return rows.terminalErr }
func (rows *rootBindingResourceTestRows) Close()     { rows.closed = true }

func TestValidateRootBindingStatePreservesContextAndErrorClassification(t *testing.T) {
	invalidDocument := "{}"
	for _, test := range []struct {
		name     string
		rows     rootBindingResourceTestRows
		queryErr error
		cancel   bool
		want     error
	}{
		{name: "empty roots"},
		{name: "legacy unbound", rows: rootBindingResourceTestRows{hasRow: true, coherent: true}},
		{name: "incoherent columns", rows: rootBindingResourceTestRows{hasRow: true}, want: ErrSchema},
		{name: "invalid model", rows: rootBindingResourceTestRows{hasRow: true, coherent: true, document: &invalidDocument}, want: ErrSchema},
		{name: "query failure", queryErr: errors.New("query failed"), want: ErrDatabase},
		{name: "row failure", rows: rootBindingResourceTestRows{hasRow: true, scanErr: errors.New("scan failed")}, want: ErrDatabase},
		{name: "iteration failure", rows: rootBindingResourceTestRows{terminalErr: errors.New("iteration failed")}, want: ErrDatabase},
		{name: "wrapped cancellation", queryErr: fmt.Errorf("driver: %w", context.Canceled), want: context.Canceled},
		{name: "wrapped deadline", rows: rootBindingResourceTestRows{terminalErr: fmt.Errorf("driver: %w", context.DeadlineExceeded)}, want: context.DeadlineExceeded},
		{name: "cancel during valid read", cancel: true, rows: rootBindingResourceTestRows{hasRow: true, coherent: true}, want: context.Canceled},
		{name: "cancel during invalid read", cancel: true, rows: rootBindingResourceTestRows{hasRow: true}, want: context.Canceled},
		{name: "cancel during failed query", cancel: true, queryErr: errors.New("query interrupted"), want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rows := test.rows
			tx := &rootBindingResourceTestTx{rows: &rows, queryErr: test.queryErr}
			if test.cancel {
				tx.onQuery = cancel
			}
			if err := validateRootBindingState(ctx, tx, 28); !errors.Is(err, test.want) {
				t.Fatalf("root binding error classification = %v, want %v", err, test.want)
			}
			if tx.queries != 1 || tx.rowQueries != 0 {
				t.Fatal("root binding validation used an unexpected query path")
			}
			if test.queryErr == nil && !rows.closed {
				t.Fatal("root binding validation retained its result rows")
			}
		})
	}
}

func TestValidateResourceStateAddsRootBindingsOnlyAtSchema28(t *testing.T) {
	for _, version := range []int64{23, 24, 25, 26, 27, 28} {
		t.Run(fmt.Sprintf("schema_%d", version), func(t *testing.T) {
			rows := &rootBindingResourceTestRows{}
			tx := &rootBindingResourceTestTx{rows: rows, rowResults: []themeStateTestRow{{valid: true}, {valid: true}}}
			if err := validateResourceState(context.Background(), tx, version); err != nil {
				t.Fatal(err)
			}
			wantRows, wantBindings := 0, 0
			if version >= 26 {
				wantRows++
			}
			if version >= 27 {
				wantRows++
			}
			if version >= 28 {
				wantBindings = 1
			}
			if tx.rowQueries != wantRows || tx.queries != wantBindings {
				t.Fatalf("schema %d crossed a semantic migration boundary: rows=%d binding=%d", version, tx.rowQueries, tx.queries)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := validateResourceState(ctx, nil, version); !errors.Is(err, context.Canceled) {
				t.Fatalf("the schema boundary discarded caller cancellation: %v", err)
			}
		})
	}
}

func TestValidateResourceStateStopsAtTheFirstInvalidSchema28Population(t *testing.T) {
	for _, test := range []struct {
		name       string
		previous   []themeStateTestRow
		rowQueries int
		queries    int
	}{
		{"theme", []themeStateTestRow{{valid: false}}, 1, 0},
		{"extra", []themeStateTestRow{{valid: true}, {valid: false}}, 2, 0},
		{"root binding", []themeStateTestRow{{valid: true}, {valid: true}}, 2, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &rootBindingResourceTestTx{rowResults: test.previous, rows: &rootBindingResourceTestRows{hasRow: true}}
			if err := validateResourceState(context.Background(), tx, 28); !errors.Is(err, ErrSchema) {
				t.Fatalf("the combined gate accepted invalid %s state: %v", test.name, err)
			}
			if tx.rowQueries != test.rowQueries || tx.queries != test.queries {
				t.Fatal("the combined semantic gate queried beyond a failed population")
			}
		})
	}
}

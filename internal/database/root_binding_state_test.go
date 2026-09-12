package database_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/storagebinding"
)

func TestValidateRootBindingStateHistoricalSchemas(t *testing.T) {
	for _, version := range []int64{0, 26, 27} {
		tx := &rootBindingStateTestTx{}
		if err := database.ValidateRootBindingState(context.Background(), tx, version); err != nil || tx.queries != 0 {
			t.Fatalf("historical schema %d queried binding columns: queries=%d error=%v", version, tx.queries, err)
		}
		if err := database.ValidateRootBindingState(nil, nil, version); err != nil {
			t.Fatalf("historical schema %d required a binding transaction: %v", version, err)
		}
	}
	if err := database.ValidateRootBindingState(context.Background(), nil, 28); err == nil || errors.Is(err, database.ErrRootBindingState) {
		t.Fatalf("missing current transaction was classified as persisted corruption: %v", err)
	}
	tx := &rootBindingStateTestTx{}
	if err := database.ValidateRootBindingState(nil, tx, 28); err == nil || errors.Is(err, database.ErrRootBindingState) || tx.queries != 0 {
		t.Fatalf("missing current context queried binding state: queries=%d error=%v", tx.queries, err)
	}
}

func TestValidateRootBindingStateChecksCompleteMapping(t *testing.T) {
	snapshot := rootBindingStateTestSnapshot()
	bound := rootBindingStateTestBoundRow(t, snapshot)
	equalMapping := snapshot.Clone()
	equalMapping.Mapping.RegisteredPath = equalMapping.Mapping.ApprovedPath
	equalMapping.RegisteredRoot = equalMapping.Anchor
	for _, test := range []struct {
		name    string
		values  []rootBindingStateTestRow
		invalid bool
	}{
		{name: "empty database"},
		{name: "legacy unbound", values: []rootBindingStateTestRow{{coherent: true}}},
		{name: "complete bound", values: []rootBindingStateTestRow{{coherent: true}, bound}},
		{name: "root equals anchor", values: []rootBindingStateTestRow{rootBindingStateTestBoundRow(t, equalMapping)}},
		{name: "legacy empty anchor-relative path", values: []rootBindingStateTestRow{rootBindingStateTestBoundRow(t, equalMapping).withPath(2, rootBindingStateTestString(""))}},
		{name: "incoherent metadata", values: []rootBindingStateTestRow{{}}, invalid: true},
		{name: "malformed document", values: []rootBindingStateTestRow{bound.withDocument("{")}, invalid: true},
		{name: "incomplete document", values: []rootBindingStateTestRow{bound.withDocument("{}")}, invalid: true},
		{name: "unknown field", values: []rootBindingStateTestRow{bound.withDocument(strings.TrimSuffix(*bound.document, "}") + `,"unexpected":true}`)}, invalid: true},
		{name: "unknown profile", values: []rootBindingStateTestRow{bound.withDocument(strings.Replace(*bound.document, storagebinding.IdentityProfile, "future-profile", 1))}, invalid: true},
		{name: "approved path mismatch", values: []rootBindingStateTestRow{bound.withPath(0, rootBindingStateTestString(snapshot.Mapping.ApprovedPath+"-other"))}, invalid: true},
		{name: "registered path mismatch", values: []rootBindingStateTestRow{bound.withPath(1, rootBindingStateTestString(snapshot.Mapping.RegisteredPath+"-other"))}, invalid: true},
		{name: "relative path mismatch", values: []rootBindingStateTestRow{bound.withPath(2, rootBindingStateTestString("elsewhere"))}, invalid: true},
		{name: "empty nested relative path", values: []rootBindingStateTestRow{bound.withPath(2, rootBindingStateTestString(""))}, invalid: true},
		{name: "noncanonical anchor-relative path", values: []rootBindingStateTestRow{rootBindingStateTestBoundRow(t, equalMapping).withPath(2, rootBindingStateTestString("./"))}, invalid: true},
		{name: "unbounded approved path", values: []rootBindingStateTestRow{bound.withPath(0, nil)}, invalid: true},
		{name: "unbounded registered path", values: []rootBindingStateTestRow{bound.withPath(1, nil)}, invalid: true},
		{name: "unbounded relative path", values: []rootBindingStateTestRow{bound.withPath(2, nil)}, invalid: true},
		{name: "corruption after valid row", values: []rootBindingStateTestRow{bound, {}}, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := &rootBindingStateTestRows{values: test.values}
			tx := &rootBindingStateTestTx{rows: rows}
			err := database.ValidateRootBindingState(context.Background(), tx, 28)
			if test.invalid {
				if !errors.Is(err, database.ErrRootBindingState) {
					t.Fatalf("invalid persisted binding was accepted: %v", err)
				}
				if strings.Contains(err.Error(), snapshot.Mapping.ApprovedPath) || strings.Contains(err.Error(), "future-profile") {
					t.Fatal("semantic validation exposed persisted document content")
				}
			} else if err != nil {
				t.Fatalf("valid persisted binding was rejected: %v", err)
			}
			if !rows.closed {
				t.Fatal("binding validation left query rows open")
			}
			if tx.queries != 1 || len(tx.arguments) != 2 || tx.arguments[0] != storagebinding.MaxDocumentBytes || tx.arguments[1] != storagebinding.MaxPathBytes {
				t.Fatalf("binding query did not supply document and path byte budgets: queries=%d arguments=%v", tx.queries, tx.arguments)
			}
		})
	}
}

func TestValidateRootBindingStatePreservesReadErrorsAndCancellation(t *testing.T) {
	readFailure := &pgconn.PgError{Code: "08006", Message: "binding read failed"}
	for _, test := range []struct {
		name     string
		phase    string
		cause    error
		cancelAt string
	}{
		{name: "query failure", phase: "query", cause: readFailure},
		{name: "scan failure", phase: "scan", cause: readFailure},
		{name: "iteration failure", phase: "rows", cause: readFailure},
		{name: "query cancellation", phase: "query", cause: context.Canceled},
		{name: "scan deadline", phase: "scan", cause: context.DeadlineExceeded},
		{name: "iteration cancellation", phase: "rows", cause: context.Canceled},
		{name: "canceled before query", cause: context.Canceled, cancelAt: "before"},
		{name: "canceled during iteration", cause: context.Canceled, cancelAt: "row"},
		{name: "canceled after last row", cause: context.Canceled, cancelAt: "end"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rows := &rootBindingStateTestRows{values: []rootBindingStateTestRow{{coherent: true}}}
			tx := &rootBindingStateTestTx{rows: rows}
			switch test.phase {
			case "query":
				tx.queryErr = test.cause
			case "scan":
				rows.scanErr = test.cause
			case "rows":
				rows.rowsErr = test.cause
			}
			switch test.cancelAt {
			case "before":
				cancel()
			case "row":
				rows.onNext = func(int) { cancel() }
			case "end":
				rows.onNext = func(index int) {
					if index == len(rows.values) {
						cancel()
					}
				}
			}
			err := database.ValidateRootBindingState(ctx, tx, 28)
			if !errors.Is(err, test.cause) || errors.Is(err, database.ErrRootBindingState) {
				t.Fatalf("operational error lost its identity or became persisted corruption: %v", err)
			}
			if test.phase != "" && err == test.cause {
				t.Fatal("database read error lost its operation context")
			}
			wantQueries := 1
			if test.cancelAt == "before" {
				wantQueries = 0
			}
			if tx.queries != wantQueries {
				t.Fatalf("binding query count=%d, want %d", tx.queries, wantQueries)
			}
			if wantClosed := wantQueries != 0 && test.phase != "query"; rows.closed != wantClosed {
				t.Fatalf("binding rows closed=%v, want %v", rows.closed, wantClosed)
			}
		})
	}
}

func rootBindingStateTestSnapshot() storagebinding.Snapshot {
	anchor := storagebinding.Identity{
		Version:        storagebinding.IdentityVersion,
		Profile:        storagebinding.IdentityProfile,
		FilesystemUUID: "0123456789abcdef0123456789abcdef",
		HandleType:     1,
		Handle:         []byte{0, 1, 255, 0},
	}
	registered := anchor
	registered.Handle = []byte{0, 2, 128, 0}
	return storagebinding.Snapshot{
		Version: storagebinding.TopologyVersion,
		Mapping: storagebinding.Mapping{
			ApprovedPath:   "/goby-root-binding-fixture-never-created",
			RegisteredPath: "/goby-root-binding-fixture-never-created/library",
		},
		Anchor:         anchor,
		RegisteredRoot: registered,
		Boundaries: []storagebinding.Boundary{
			{RelativePath: "nested/archive", Identity: registered},
		},
	}
}

func rootBindingStateTestBoundRow(t *testing.T, snapshot storagebinding.Snapshot) rootBindingStateTestRow {
	t.Helper()
	raw, err := storagebinding.EncodeSnapshot(snapshot)
	if err != nil {
		t.Fatalf("encode complete binding fixture: %v", err)
	}
	relative, err := snapshot.Mapping.Relative()
	if err != nil {
		t.Fatalf("derive binding fixture traversal path: %v", err)
	}
	return rootBindingStateTestRow{
		coherent:   true,
		document:   rootBindingStateTestString(string(raw)),
		approved:   rootBindingStateTestString(snapshot.Mapping.ApprovedPath),
		registered: rootBindingStateTestString(snapshot.Mapping.RegisteredPath),
		relative:   rootBindingStateTestString(relative),
	}
}

func rootBindingStateTestString(value string) *string {
	return &value
}

type rootBindingStateTestRow struct {
	coherent   bool
	document   *string
	approved   *string
	registered *string
	relative   *string
}

func (row rootBindingStateTestRow) withDocument(document string) rootBindingStateTestRow {
	row.document = &document
	return row
}

func (row rootBindingStateTestRow) withPath(index int, value *string) rootBindingStateTestRow {
	paths := []**string{&row.approved, &row.registered, &row.relative}
	*paths[index] = value
	return row
}

type rootBindingStateTestTx struct {
	pgx.Tx
	rows      *rootBindingStateTestRows
	queryErr  error
	queries   int
	arguments []any
}

func (tx *rootBindingStateTestTx) Query(_ context.Context, _ string, arguments ...any) (pgx.Rows, error) {
	tx.queries++
	tx.arguments = append([]any(nil), arguments...)
	if tx.queryErr != nil {
		return nil, tx.queryErr
	}
	return tx.rows, nil
}

type rootBindingStateTestRows struct {
	pgx.Rows
	values  []rootBindingStateTestRow
	index   int
	scanErr error
	rowsErr error
	onNext  func(int)
	closed  bool
}

func (rows *rootBindingStateTestRows) Next() bool {
	if rows.onNext != nil {
		rows.onNext(rows.index)
	}
	if rows.index >= len(rows.values) {
		return false
	}
	rows.index++
	return true
}

func (rows *rootBindingStateTestRows) Scan(destinations ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	row := rows.values[rows.index-1]
	*destinations[0].(*bool) = row.coherent
	*destinations[1].(**string) = row.document
	*destinations[2].(**string) = row.approved
	*destinations[3].(**string) = row.registered
	*destinations[4].(**string) = row.relative
	return nil
}

func (rows *rootBindingStateTestRows) Err() error {
	return rows.rowsErr
}

func (rows *rootBindingStateTestRows) Close() {
	rows.closed = true
}

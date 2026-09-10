package library

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type ownedCallbackDriverFixture struct {
	pgx.Tx
	exec  func(context.Context, string, ...any) (pgconn.CommandTag, error)
	query func(context.Context, string, ...any) (pgx.Rows, error)
}

func (driver *ownedCallbackDriverFixture) Exec(ctx context.Context, statement string, args ...any) (pgconn.CommandTag, error) {
	return driver.exec(ctx, statement, args...)
}

func (driver *ownedCallbackDriverFixture) Query(ctx context.Context, statement string, args ...any) (pgx.Rows, error) {
	return driver.query(ctx, statement, args...)
}

type ownedCallbackRowsFixture struct {
	pgx.Rows
	next       func() bool
	scan       func(...any) error
	onClose    func()
	terminal   error
	closeCalls int
}

func (rows *ownedCallbackRowsFixture) Next() bool { return rows.next() }
func (rows *ownedCallbackRowsFixture) Scan(destinations ...any) error {
	return rows.scan(destinations...)
}
func (rows *ownedCallbackRowsFixture) Err() error { return rows.terminal }
func (rows *ownedCallbackRowsFixture) Close() {
	rows.closeCalls++
	if rows.onClose != nil {
		rows.onClose()
	}
}

func callbackFixture(t *testing.T) (*ownedCallbackTx, *ownedCallbackDriverFixture, *[]string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	events := make([]string, 0)
	driver := &ownedCallbackDriverFixture{}
	driver.exec = func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	tx := &ownedCallbackTx{driver: driver, ctx: ctx, rows: make(map[*ownedCallbackRows]struct{}),
		classify: func(err error) error { return err },
		commit:   func() error { events = append(events, "commit"); return nil },
		rollback: func() error { events = append(events, "rollback"); return nil }}
	return tx, driver, &events
}

func TestOwnedTransactionViewsExposeOnlyTheRestrictedMethods(t *testing.T) {
	cases := []struct {
		value any
		want  []string
	}{
		{(*ownedCallbackTx)(nil), []string{"Exec", "Query", "QueryRow"}},
		{(*ownedCallbackRow)(nil), []string{"Scan"}},
		{(*ownedCallbackRows)(nil), []string{"Close", "Err", "Next", "Scan"}},
	}
	for _, test := range cases {
		typ := reflect.TypeOf(test.value)
		var names []string
		for index := range typ.NumMethod() {
			names = append(names, typ.Method(index).Name)
		}
		if !slices.Equal(names, test.want) {
			t.Errorf("%s exposes methods %v, want only %v", typ, names, test.want)
		}
		for index := range typ.Elem().NumField() {
			if typ.Elem().Field(index).PkgPath == "" {
				t.Errorf("%s exposes a raw field %s", typ, typ.Elem().Field(index).Name)
			}
		}
	}
}

func TestOwnedTransactionUsesOneProtectedContextAndClosesEscapedHandles(t *testing.T) {
	tx, driver, events := callbackFixture(t)
	seenContexts := make([]context.Context, 0)
	driver.exec = func(ctx context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
		seenContexts = append(seenContexts, ctx)
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	cursors := make([]*ownedCallbackRowsFixture, 0)
	driver.query = func(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
		seenContexts = append(seenContexts, ctx)
		rows := &ownedCallbackRowsFixture{next: func() bool { return true }, scan: func(...any) error { return nil },
			onClose: func() { *events = append(*events, "close") }}
		cursors = append(cursors, rows)
		return rows, nil
	}
	if _, err := tx.Exec("UPDATE fixture SET value = $1", 1); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query("SELECT fixture")
	if err != nil {
		t.Fatal(err)
	}
	row := tx.QueryRow("SELECT fixture")
	if err := tx.finish(nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*events, []string{"close", "close", "commit"}) {
		t.Fatalf("transaction completion preceded cursor cleanup: %v", *events)
	}
	for _, ctx := range seenContexts {
		if ctx != tx.ctx {
			t.Error("a database operation escaped the transaction's protected context")
		}
		if _, bounded := ctx.Deadline(); !bounded {
			t.Error("a database operation used an unbounded context")
		}
	}
	if rows.Next() || !errors.Is(rows.Scan(new(int)), pgx.ErrTxClosed) || !errors.Is(rows.Err(), pgx.ErrTxClosed) ||
		!errors.Is(row.Scan(new(int)), pgx.ErrTxClosed) {
		t.Error("a row handle remained usable after callback completion")
	}
	rows.Close()
	if _, err := tx.Exec("UPDATE fixture"); !errors.Is(err, pgx.ErrTxClosed) {
		t.Error("an escaped transaction retained write access")
	}
	if _, err := tx.Query("SELECT fixture"); !errors.Is(err, pgx.ErrTxClosed) {
		t.Error("an escaped transaction retained cursor access")
	}
	if err := tx.QueryRow("SELECT fixture").Scan(new(int)); !errors.Is(err, pgx.ErrTxClosed) {
		t.Error("an escaped transaction retained row access")
	}
	if len(seenContexts) != 3 || len(tx.rows) != 0 {
		t.Error("expired handles performed database operations or retained outstanding cursors")
	}
	for _, cursor := range cursors {
		if cursor.closeCalls != 1 {
			t.Error("cursor cleanup was omitted or repeated after its lifetime ended")
		}
	}
}

func TestOwnedTransactionObservesEveryCursorConnectionFailureEvenWhenIgnored(t *testing.T) {
	for _, stage := range []string{"query", "next", "scan", "err", "close", "automatic-close", "query-row-scan"} {
		t.Run(stage, func(t *testing.T) {
			tx, driver, events := callbackFixture(t)
			connectionError := errors.New("synthetic owner connection ended")
			fenced := false
			tx.classify = func(err error) error {
				if errors.Is(err, connectionError) {
					fenced = true
					return errors.Join(ErrUnavailable, err)
				}
				return err
			}
			cursor := &ownedCallbackRowsFixture{scan: func(...any) error { return nil }}
			cursor.next = func() bool {
				if stage == "next" {
					cursor.terminal = connectionError
					return false
				}
				return true
			}
			cursor.onClose = func() {
				*events = append(*events, "close")
				if stage == "close" || stage == "automatic-close" {
					cursor.terminal = connectionError
				}
			}
			if stage == "scan" || stage == "query-row-scan" {
				cursor.scan = func(...any) error { return connectionError }
			}
			driver.query = func(context.Context, string, ...any) (pgx.Rows, error) {
				if stage == "query" {
					return cursor, connectionError
				}
				return cursor, nil
			}
			if stage == "query-row-scan" {
				_ = tx.QueryRow("SELECT fixture").Scan(new(int))
			} else {
				rows, _ := tx.Query("SELECT fixture")
				switch stage {
				case "next":
					_ = rows.Next()
				case "scan":
					_ = rows.Next()
					_ = rows.Scan(new(int))
				case "err":
					cursor.terminal = connectionError
					_ = rows.Err()
				case "close":
					rows.Close()
				}
			}
			if err := tx.finish(nil); !errors.Is(err, ErrUnavailable) || !errors.Is(err, connectionError) {
				t.Errorf("ignored cursor failure allowed successful completion: %v", err)
			}
			if !fenced || !slices.Equal(*events, []string{"close", "rollback"}) || len(tx.rows) != 0 {
				t.Fatalf("cursor error did not fence and roll back after cleanup: fenced=%t, events=%v", fenced, *events)
			}
		})
	}
}

func TestOwnedTransactionHandlesNoRowsWithoutFencingAndKeepsNormalErrors(t *testing.T) {
	t.Run("no-rows", func(t *testing.T) {
		tx, driver, events := callbackFixture(t)
		classified := 0
		tx.classify = func(err error) error { classified++; return err }
		driver.query = func(context.Context, string, ...any) (pgx.Rows, error) {
			return &ownedCallbackRowsFixture{next: func() bool { return false }, scan: func(...any) error { return nil },
				onClose: func() { *events = append(*events, "close") }}, nil
		}
		if err := tx.QueryRow("SELECT fixture WHERE false").Scan(new(int)); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("empty query row did not retain pgx.ErrNoRows: %v", err)
		}
		if err := tx.finish(nil); err != nil || classified != 0 || !slices.Equal(*events, []string{"close", "commit"}) {
			t.Fatalf("handled no-rows result affected ownership or commit: error=%v, classified=%d, events=%v", err, classified, *events)
		}
	})
	t.Run("normal-terminal-error", func(t *testing.T) {
		tx, driver, events := callbackFixture(t)
		queryError := errors.New("synthetic decoding failure")
		driver.query = func(context.Context, string, ...any) (pgx.Rows, error) {
			return &ownedCallbackRowsFixture{next: func() bool { return false }, terminal: queryError,
				onClose: func() { *events = append(*events, "close") }}, nil
		}
		rows, _ := tx.Query("SELECT fixture")
		_ = rows.Next()
		if err := tx.finish(nil); !errors.Is(err, queryError) || errors.Is(err, ErrUnavailable) {
			t.Fatalf("ordinary terminal error was hidden or mislabeled as connection loss: %v", err)
		}
		if !slices.Equal(*events, []string{"close", "rollback"}) {
			t.Fatal("ordinary cursor error did not roll back after cleanup")
		}
	})
}

func TestOwnedTransactionCompletionPreservesCallbackAndCleanupFailures(t *testing.T) {
	tx, driver, events := callbackFixture(t)
	callbackError := errors.New("synthetic repository refusal")
	rollbackError := errors.New("synthetic protected rollback failure")
	driver.query = func(context.Context, string, ...any) (pgx.Rows, error) {
		return &ownedCallbackRowsFixture{next: func() bool { return true },
			onClose: func() { *events = append(*events, "close") }}, nil
	}
	tx.rollback = func() error { *events = append(*events, "rollback"); return rollbackError }
	_ = tx.QueryRow("SELECT fixture")
	err := tx.finish(callbackError)
	if !errors.Is(err, callbackError) || !errors.Is(err, rollbackError) || !slices.Equal(*events, []string{"close", "rollback"}) {
		t.Fatalf("callback or cleanup failure was lost: error=%v, events=%v", err, *events)
	}
	if err := tx.finish(nil); !errors.Is(err, pgx.ErrTxClosed) || len(*events) != 2 {
		t.Fatal("completion attempted to commit or release an already finished transaction")
	}
}

func TestWithOwnedTxRejectsMissingCallbackAndUnavailableStore(t *testing.T) {
	ctx := context.Background()
	if err := (&Store{}).WithOwnedTx(ctx, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil callback was not rejected without acquiring a transaction: %v", err)
	}
	for _, store := range []*Store{nil, {}, {closed: true}} {
		called := false
		err := store.WithOwnedTx(ctx, func(OwnedTx) error { called = true; return nil })
		if !errors.Is(err, ErrUnavailable) || called {
			t.Fatalf("unavailable store admitted a callback: error=%v, called=%t", err, called)
		}
	}
}

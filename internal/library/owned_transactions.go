package library

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// OwnedTransactions supplies bounded transactions on the session that owns the
// catalog. It is intended for trusted internal repositories, not arbitrary SQL.
type OwnedTransactions interface {
	WithOwnedTx(context.Context, func(OwnedTx) error) error
}

// OwnedTx deliberately exposes no connection or transaction-control methods.
// Its statements use the protected transaction context, not a request context.
type OwnedTx interface {
	Exec(string, ...any) (pgconn.CommandTag, error)
	QueryRow(string, ...any) OwnedRow
	Query(string, ...any) (OwnedRows, error)
}

type OwnedRow interface {
	Scan(...any) error
}

// OwnedRows never exposes raw pgx rows, values, connections, or protocol state.
// Unclosed rows are closed before the callback's transaction is completed.
type OwnedRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

var errOwnedCallbackInterrupted = errors.New("owned transaction callback did not complete")

// WithOwnedTx admits a callback before shutdown and executes it on the reserved
// owner session. Once the transaction begins, caller cancellation cannot close
// that session: database operations retain beginOwnedTx's independent 20-second
// context. Every iterator is closed before commit or protected rollback.
//
// Callbacks must perform only bounded repository operations. They must not call
// Store public methods, retain handles, wait for workers, start processes, or
// issue SQL that controls transactions, connections, or the ownership lock.
// This method is a narrow internal capability, not a SQL sandbox.
func (s *Store) WithOwnedTx(ctx context.Context, callback func(OwnedTx) error) error {
	if s == nil {
		return ErrUnavailable
	}
	if callback == nil {
		return fmt.Errorf("%w: owned transaction callback is required", ErrInvalidInput)
	}
	// Admission and shutdown use Store.mu before the owner mutex. Release the
	// admission mutex before invoking repository code; never take it from there.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrUnavailable
	}
	raw, err := s.beginOwnedTx(ctx)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.withOwnedTxCallback(raw, callback)
}

// withOwnedTxCallback completes a transaction already admitted by beginOwnedTx.
// Scanner admission may hold Store.mu; this helper never acquires that mutex.
func (s *Store) withOwnedTxCallback(raw pgx.Tx, callback func(OwnedTx) error) error {
	transaction := raw.(*ownedTx)
	view := &ownedCallbackTx{driver: transaction.Tx, ctx: transaction.ctx, classify: s.ownershipErrorLocked,
		commit:   func() error { return transaction.Commit(context.Background()) },
		rollback: func() error { return transaction.Rollback(context.Background()) },
		rows:     make(map[*ownedCallbackRows]struct{})}
	completed := false
	defer func() {
		if !completed {
			// Also runs during panic or Goexit; the original control flow is
			// preserved after all cursors and the owner mutex are released.
			_ = view.finish(errOwnedCallbackInterrupted)
		}
	}()
	err := view.finish(callback(view))
	completed = true
	return err
}

// This wrapper intentionally does not embed pgx.Tx. Its only exported methods
// are the OwnedTx operations, including when inspected through a type assertion.
type ownedCallbackTx struct {
	mu       sync.Mutex
	driver   pgx.Tx
	ctx      context.Context
	classify func(error) error
	commit   func() error
	rollback func() error
	rows     map[*ownedCallbackRows]struct{}
	finished bool
	terminal error
}

func (tx *ownedCallbackTx) observeLocked(err error) error {
	if err == nil {
		return nil
	}
	err = tx.classify(err)
	if !errors.Is(err, pgx.ErrNoRows) {
		if tx.terminal == nil {
			tx.terminal = err
		} else if errors.Is(err, ErrUnavailable) && !errors.Is(tx.terminal, ErrUnavailable) {
			// Preserve a later fencing observation even after a normal query
			// error. Repeated Err calls do not grow the accumulated error.
			tx.terminal = errors.Join(tx.terminal, err)
		}
	}
	return err
}

func (tx *ownedCallbackTx) Exec(statement string, args ...any) (pgconn.CommandTag, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.finished {
		return pgconn.CommandTag{}, pgx.ErrTxClosed
	}
	tag, err := tx.driver.Exec(tx.ctx, statement, args...)
	return tag, tx.observeLocked(err)
}

func (tx *ownedCallbackTx) Query(statement string, args ...any) (OwnedRows, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.finished {
		return nil, pgx.ErrTxClosed
	}
	rows, err := tx.driver.Query(tx.ctx, statement, args...)
	var view *ownedCallbackRows
	if rows != nil {
		view = &ownedCallbackRows{tx: tx, rows: rows}
		tx.rows[view] = struct{}{}
	}
	if err != nil {
		err = tx.observeLocked(err)
		if view != nil {
			view.closeLocked()
		}
		return nil, errors.Join(err, tx.terminal)
	}
	return view, nil
}

func (tx *ownedCallbackTx) QueryRow(statement string, args ...any) OwnedRow {
	rows, err := tx.Query(statement, args...)
	if err != nil {
		return &ownedCallbackRow{tx: tx, err: err}
	}
	// QueryRow is built on the same tracked cursor so omitting Scan cannot
	// leave an unread result holding the owner session busy at callback exit.
	return &ownedCallbackRow{tx: tx, rows: rows.(*ownedCallbackRows)}
}

func (tx *ownedCallbackTx) finish(callbackErr error) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.finished {
		return pgx.ErrTxClosed
	}
	tx.finished = true
	for rows := range tx.rows {
		rows.closeLocked()
	}
	if err := errors.Join(callbackErr, tx.terminal); err != nil {
		return errors.Join(err, tx.rollback())
	}
	return tx.commit()
}

type ownedCallbackRows struct {
	tx     *ownedCallbackTx
	rows   pgx.Rows
	closed bool
	err    error
}

func (rows *ownedCallbackRows) observeLocked(err error) error {
	if err != nil {
		classified := rows.tx.observeLocked(err)
		if rows.err == nil || errors.Is(classified, ErrUnavailable) {
			rows.err = classified
		}
	}
	return rows.err
}

func (rows *ownedCallbackRows) closeLocked() {
	if rows.closed {
		return
	}
	rows.rows.Close()
	rows.observeLocked(rows.rows.Err())
	rows.closed = true
	delete(rows.tx.rows, rows)
}

func (rows *ownedCallbackRows) nextLocked() bool {
	if rows.closed {
		return false
	}
	if !rows.rows.Next() {
		rows.closeLocked()
		return false
	}
	return true
}

func (rows *ownedCallbackRows) Next() bool {
	rows.tx.mu.Lock()
	defer rows.tx.mu.Unlock()
	return !rows.tx.finished && rows.nextLocked()
}

func (rows *ownedCallbackRows) Scan(destinations ...any) error {
	rows.tx.mu.Lock()
	defer rows.tx.mu.Unlock()
	if rows.tx.finished || rows.closed {
		return pgx.ErrTxClosed
	}
	return rows.observeLocked(rows.rows.Scan(destinations...))
}

func (rows *ownedCallbackRows) Err() error {
	rows.tx.mu.Lock()
	defer rows.tx.mu.Unlock()
	if rows.tx.finished {
		return pgx.ErrTxClosed
	}
	if !rows.closed {
		rows.observeLocked(rows.rows.Err())
	}
	return rows.err
}

func (rows *ownedCallbackRows) Close() {
	rows.tx.mu.Lock()
	defer rows.tx.mu.Unlock()
	if !rows.tx.finished {
		rows.closeLocked()
	}
}

type ownedCallbackRow struct {
	tx      *ownedCallbackTx
	rows    *ownedCallbackRows
	err     error
	scanned bool
}

func (row *ownedCallbackRow) Scan(destinations ...any) error {
	row.tx.mu.Lock()
	defer row.tx.mu.Unlock()
	if row.tx.finished {
		return pgx.ErrTxClosed
	}
	if row.err != nil {
		return row.err
	}
	if row.scanned {
		return pgx.ErrNoRows
	}
	row.scanned = true
	if !row.rows.nextLocked() {
		if row.rows.err != nil {
			return row.rows.err
		}
		return pgx.ErrNoRows
	}
	err := row.rows.observeLocked(row.rows.rows.Scan(destinations...))
	row.rows.closeLocked()
	return errors.Join(err, row.rows.err)
}

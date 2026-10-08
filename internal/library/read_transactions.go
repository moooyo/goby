package library

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReadTx exposes only row reads from a bounded, request-cancellable pool
// transaction. It permits shared authorization locks without occupying the
// catalog's reserved writer session.
type ReadTx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type poolReadTx struct {
	tx  pgx.Tx
	ctx context.Context
}

func (tx poolReadTx) QueryRow(_ context.Context, statement string, args ...any) pgx.Row {
	return tx.tx.QueryRow(tx.ctx, statement, args...)
}

// WithReadTx admits trusted read-only repository work for this Store generation.
// PostgreSQL READ ONLY is intentionally not used: authorization may lock actors
// FOR SHARE. The callback must not mutate data or retain the transaction view.
func (s *Store) WithReadTx(ctx context.Context, callback func(ReadTx) error) (resultErr error) {
	if callback == nil || ctx == nil {
		return fmt.Errorf("%w: read transaction context and callback are required", ErrInvalidInput)
	}
	if !s.Available() || s.pool == nil || s.ctx == nil {
		return ErrUnavailable
	}
	// A Store never replaces its ownership session. Capturing it binds both
	// admission and completion to the same generation, including during close.
	ownership := s.ownership
	work, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	if s.ctx.Err() != nil {
		return ErrUnavailable
	}
	connection, err := s.pool.Acquire(work)
	if err != nil {
		return err
	}
	var tx pgx.Tx
	defer func() {
		defer connection.Release()
		cleanup, done := context.WithTimeout(context.WithoutCancel(work), 2*time.Second)
		defer done()
		if tx != nil {
			_ = tx.Rollback(cleanup)
		}
		// pgx returns from cancelled queries before its asynchronous connection
		// cleanup sends CancelRequest/Terminate and drains the server response.
		// Join that cleanup before reporting completion while actor locks remain
		// on the departing backend. Keep the connection borrowed until this join.
		pgconn := connection.Conn().PgConn()
		if pgconn.IsClosed() {
			select {
			case <-pgconn.CleanupDone():
			case <-cleanup.Done():
				resultErr = errors.Join(resultErr, fmt.Errorf("%w: retire cancelled read transaction: %w", ErrUnavailable, cleanup.Err()))
			}
		}
	}()
	tx, err = connection.BeginTx(work, pgx.TxOptions{})
	if err != nil {
		return err
	}
	if !s.Available() || s.ownership != ownership {
		return ErrUnavailable
	}
	if err := callback(poolReadTx{tx: tx, ctx: work}); err != nil {
		return err
	}
	if !s.Available() || s.ownership != ownership {
		return ErrUnavailable
	}
	if err := tx.Commit(work); err != nil {
		return err
	}
	if !s.Available() || s.ownership != ownership {
		return ErrUnavailable
	}
	return nil
}

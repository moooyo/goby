package library

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// scanOwnership reserves a PostgreSQL session for the lifetime of the store.
// Returning a session that still holds an advisory lock to the pool is unsafe.
type scanOwnership struct {
	mu   sync.Mutex
	lost atomic.Bool
	conn *pgxpool.Conn
	key  int64
}

func acquireScanOwnership(ctx context.Context, pool *pgxpool.Pool) (*scanOwnership, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire library ownership session: %w", err)
	}
	var databaseName, schemaName, catalogSchema string
	if err := conn.QueryRow(ctx, `SELECT current_database(), current_schema(),
		COALESCE((SELECT namespace.nspname FROM pg_class relation
			JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
			WHERE relation.oid = to_regclass('scan_jobs')), '')`).Scan(&databaseName, &schemaName, &catalogSchema); err != nil {
		conn.Release()
		return nil, fmt.Errorf("read library ownership scope: %w", err)
	}
	if catalogSchema != "" && catalogSchema != schemaName {
		conn.Release()
		return nil, fmt.Errorf("%w: scan catalog must belong to the current schema", ErrInvalidInput)
	}
	// Include a namespace and unambiguous separators. Different test schemas
	// and databases must not block one another's independent catalogs.
	digest := sha256.Sum256([]byte("goby.library.owner\x00" + databaseName + "\x00" + schemaName))
	ownership := &scanOwnership{conn: conn, key: int64(binary.BigEndian.Uint64(digest[:8]))}
	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1::bigint)", ownership.key).Scan(&acquired); err != nil {
		// A cancelled request can have an uncertain server-side result, so this
		// connection must be discarded even if the lock response was not read.
		ownership.discardLocked()
		return nil, fmt.Errorf("acquire library ownership lock: %w", err)
	}
	if !acquired {
		conn.Release()
		return nil, fmt.Errorf("%w: another library service owns this database schema", ErrBusy)
	}
	return ownership, nil
}

func (ownership *scanOwnership) release() error {
	if ownership == nil {
		return nil
	}
	ownership.mu.Lock()
	defer ownership.mu.Unlock()
	if ownership.conn == nil {
		return nil
	}
	if ownership.lost.Load() {
		ownership.discardLocked()
		return fmt.Errorf("%w: library ownership session was lost", ErrUnavailable)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var unlocked bool
	err := ownership.conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1::bigint)", ownership.key).Scan(&unlocked)
	if err != nil || !unlocked {
		ownership.lost.Store(true)
		ownership.discardLocked()
		if err != nil {
			return fmt.Errorf("%w: release library ownership lock: %w", ErrUnavailable, err)
		}
		return fmt.Errorf("%w: library ownership lock was lost", ErrUnavailable)
	}
	ownership.conn.Release()
	ownership.conn = nil
	return nil
}

func (ownership *scanOwnership) discardLocked() {
	if ownership.conn == nil {
		return
	}
	conn := ownership.conn.Hijack()
	ownership.conn = nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = conn.Close(ctx)
}

// Available reports the last known write availability. CheckOwnership verifies
// the live session when a readiness check needs to detect an idle backend loss.
func (s *Store) Available() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.ownership != nil && !s.ownership.lost.Load()
}

// CheckOwnership verifies the existing lock session without reconnecting or
// reacquiring a lock that may already belong to a successor server.
func (s *Store) CheckOwnership(ctx context.Context) error {
	if !s.Available() {
		return ErrUnavailable
	}
	ownership := s.ownership
	ownership.mu.Lock()
	defer ownership.mu.Unlock()
	if ownership.conn == nil || ownership.lost.Load() {
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	pingCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.ownershipErrorLocked(ownership.conn.Ping(pingCtx))
}

// Catalog writes use the exact session that holds the advisory lock. An aborted
// PostgreSQL backend therefore fences every old-owner write, including jobs
// finishing after a successor has recovered them. Media probes still run in
// parallel; only short database transactions share this mutex.
func (s *Store) beginOwnedTx(ctx context.Context) (pgx.Tx, error) {
	ownership := s.ownership
	if ownership == nil || ownership.lost.Load() {
		return nil, ErrUnavailable
	}
	ownership.mu.Lock()
	if ownership.conn == nil || ownership.lost.Load() {
		ownership.mu.Unlock()
		return nil, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		ownership.mu.Unlock()
		return nil, err
	}
	// pgx's default cancellation handler can close a connection. Once a short
	// write transaction starts, complete it independently from an HTTP disconnect
	// or scan cancellation so normal cancellation cannot destroy the lock session.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	tx, err := ownership.conn.Begin(writeCtx)
	if err != nil {
		cancel()
		err = s.ownershipErrorLocked(err)
		ownership.mu.Unlock()
		return nil, err
	}
	return &ownedTx{Tx: tx, store: s, ctx: writeCtx, cancel: cancel}, nil
}

type ownedTx struct {
	pgx.Tx
	store          *Store
	ctx            context.Context
	cancel         context.CancelFunc
	finished       bool
	catalogChanges catalogChangeBatch
}

func (tx *ownedTx) Exec(_ context.Context, statement string, args ...any) (pgconn.CommandTag, error) {
	if tx.finished {
		return pgconn.CommandTag{}, pgx.ErrTxClosed
	}
	tag, err := tx.Tx.Exec(tx.ctx, statement, args...)
	return tag, tx.store.ownershipErrorLocked(err)
}

type ownedTxRow struct {
	tx  *ownedTx
	row pgx.Row
}

func (tx *ownedTx) QueryRow(_ context.Context, statement string, args ...any) pgx.Row {
	if tx.finished {
		return ownedTxRow{tx: tx}
	}
	return ownedTxRow{tx: tx, row: tx.Tx.QueryRow(tx.ctx, statement, args...)}
}

func (row ownedTxRow) Scan(destinations ...any) error {
	if row.tx.finished {
		return pgx.ErrTxClosed
	}
	return row.tx.store.ownershipErrorLocked(row.row.Scan(destinations...))
}

func (tx *ownedTx) Commit(_ context.Context) error {
	if tx.finished {
		return pgx.ErrTxClosed
	}
	err := tx.Tx.Commit(tx.ctx)
	tx.cancel()
	err = tx.store.ownershipErrorLocked(err)
	tx.finished = true
	defer tx.store.ownership.mu.Unlock()
	notification := tx.catalogChanges.take()
	if err == nil {
		tx.store.notifyCatalogChanges(notification)
	}
	return err
}

func (tx *ownedTx) Rollback(_ context.Context) error {
	if tx.finished {
		return pgx.ErrTxClosed
	}
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := tx.Tx.Rollback(rollbackCtx)
	tx.cancel()
	err = tx.store.ownershipErrorLocked(err)
	tx.finished = true
	tx.catalogChanges = catalogChangeBatch{}
	tx.store.ownership.mu.Unlock()
	return err
}

// ownershipErrorLocked never takes Store.mu: management calls may already hold
// it before acquiring the write mutex. Cancellation and the lost flag are safe
// without reversing that lock order.
func (s *Store) ownershipErrorLocked(err error) error {
	if err == nil {
		return nil
	}
	var databaseError *pgconn.PgError
	connectionFailure := errors.As(err, &databaseError) &&
		(strings.HasPrefix(databaseError.Code, "08") || databaseError.Code == "57P01" || databaseError.Code == "57P02" || databaseError.Code == "57P03")
	if s.ownership.conn == nil || s.ownership.conn.Conn().IsClosed() || connectionFailure {
		s.ownership.lost.Store(true)
		s.cancel()
		return fmt.Errorf("%w: library ownership session ended: %w", ErrUnavailable, err)
	}
	return err
}

func (s *Store) execOwned(ctx context.Context, statement string, args ...any) (pgconn.CommandTag, error) {
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	defer rollback(tx)
	tag, err := tx.Exec(ctx, statement, args...)
	if err != nil {
		return tag, err
	}
	if err := tx.Commit(ctx); err != nil {
		return tag, err
	}
	return tag, nil
}

type ownedRow struct {
	store     *Store
	ctx       context.Context
	statement string
	args      []any
}

func (s *Store) queryOwnedRow(ctx context.Context, statement string, args ...any) rowScanner {
	return ownedRow{store: s, ctx: ctx, statement: statement, args: args}
}

func (row ownedRow) Scan(destinations ...any) error {
	tx, err := row.store.beginOwnedTx(row.ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err := tx.QueryRow(row.ctx, row.statement, row.args...).Scan(destinations...); err != nil {
		return err
	}
	return tx.Commit(row.ctx)
}

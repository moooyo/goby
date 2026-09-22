package library

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/moooyo/goby/internal/notificationjournal"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/systemevents"
)

// scanOwnership reserves a PostgreSQL session for the lifetime of the store.
// Returning a session that still holds an advisory lock to the pool is unsafe.
type scanOwnership struct {
	mu   sync.Mutex
	lost atomic.Bool
	conn *pgxpool.Conn
	key  int64

	// The dirty flag is set before the first private DDL is sent and never
	// cleared. Even a failed or rolled-back initialization cannot return this
	// physical session to an unrelated pool borrower.
	scanStagingDirty      bool
	scanStagingReady      bool
	scanStagingGeneration string
	scanStagingPasses     map[string]*scanReconciliationStaging
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
		return errors.Join(fmt.Errorf("%w: library ownership session was lost", ErrUnavailable), ownership.discardLocked())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var unlocked bool
	err := ownership.conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1::bigint)", ownership.key).Scan(&unlocked)
	if err != nil || !unlocked {
		ownership.lost.Store(true)
		closeErr := ownership.discardLocked()
		if err != nil {
			return errors.Join(fmt.Errorf("%w: release library ownership lock: %w", ErrUnavailable, err), closeErr)
		}
		return errors.Join(fmt.Errorf("%w: library ownership lock was lost", ErrUnavailable), closeErr)
	}
	if ownership.scanStagingDirty {
		// ON COMMIT PRESERVE ROWS is session state. Explicitly unlocking above
		// preserves the existing handoff ordering, but never makes a dirty
		// physical session safe for normal pool reuse.
		if err := ownership.discardLocked(); err != nil {
			return fmt.Errorf("%w: close private scan staging session: %w", ErrUnavailable, err)
		}
		return nil
	}
	ownership.conn.Release()
	ownership.conn = nil
	return nil
}

func (ownership *scanOwnership) discardLocked() error {
	if ownership.conn == nil {
		return nil
	}
	conn := ownership.conn.Hijack()
	ownership.conn = nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.Close(ctx)
}

// Available reports the last known write availability. CheckOwnership verifies
// the live session when a readiness check needs to detect an idle backend loss.
// It does not acquire the admission mutex: executor availability can be checked
// inside an owned transaction while another admission is waiting for that owner.
func (s *Store) Available() bool {
	if s == nil {
		return false
	}
	// New publishes ownership once. Close sets closing before it can set closed,
	// and ownership loss is atomic, so no mutable admission state is read here.
	return !s.closing.Load() && s.ownership != nil && !s.ownership.lost.Load()
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
	if err := s.lockOwnedSession(ctx); err != nil {
		return nil, err
	}
	return s.beginOwnedTxLocked(ctx)
}

// lockOwnedSession must be called without Store.mu. A queued writer must not
// prevent unrelated root admission or independent pool transactions.
func (s *Store) lockOwnedSession(ctx context.Context) error {
	if s == nil {
		return ErrUnavailable
	}
	ownership := s.ownership
	if ownership == nil || ownership.lost.Load() {
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ownership.mu.Lock()
	if ownership.conn == nil || ownership.lost.Load() {
		ownership.mu.Unlock()
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		ownership.mu.Unlock()
		return err
	}
	return nil
}

// lockOwnedAdmission returns with both ownership.mu and Store.mu held. The
// owner is acquired first, and shutdown is checked again under admission.
// Existing scan cancellation may finish while Close joins its workers.
func (s *Store) lockOwnedAdmission(ctx context.Context, allowClosing bool) error {
	if s == nil || !allowClosing && s.closing.Load() {
		return ErrUnavailable
	}
	if err := s.lockOwnedSession(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	if !allowClosing && (s.closed || s.closing.Load()) {
		s.mu.Unlock()
		s.ownership.mu.Unlock()
		return ErrUnavailable
	}
	return nil
}

// beginOwnedAdmission leaves Store.mu held only on success. The caller retains
// it through any queue or root-anchor publication, or releases it before a
// repository callback. The returned transaction owns ownership.mu as usual.
func (s *Store) beginOwnedAdmission(ctx context.Context, allowClosing bool) (pgx.Tx, error) {
	if err := s.lockOwnedAdmission(ctx, allowClosing); err != nil {
		return nil, err
	}
	tx, err := s.beginOwnedTxLocked(ctx)
	if err != nil {
		s.mu.Unlock()
	}
	return tx, err
}

// beginOwnedTxLocked transfers the held owner mutex to the transaction, or
// releases it on failure. Check cancellation after any admission mutex wait.
func (s *Store) beginOwnedTxLocked(ctx context.Context) (pgx.Tx, error) {
	ownership := s.ownership
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
	store                       *Store
	ctx                         context.Context
	cancel                      context.CancelFunc
	finished                    bool
	catalogChanges              catalogChangeBatch
	systemEventRecorded         bool
	notificationMutationID      string
	notificationJournalStamp    [32]byte
	notificationJournalRecorded bool
	notificationReferences      []notificationjournal.Reference
}

func (tx *ownedTx) Exec(_ context.Context, statement string, args ...any) (pgconn.CommandTag, error) {
	if tx.finished {
		return pgconn.CommandTag{}, pgx.ErrTxClosed
	}
	if err := tx.flushSystemEvent(); err != nil {
		return pgconn.CommandTag{}, err
	}
	tag, err := tx.Tx.Exec(tx.ctx, statement, args...)
	return tag, tx.store.ownershipErrorLocked(err)
}

type ownedTxRow struct {
	tx  *ownedTx
	row pgx.Row
	err error
}

func (tx *ownedTx) QueryRow(_ context.Context, statement string, args ...any) pgx.Row {
	if tx.finished {
		return ownedTxRow{tx: tx}
	}
	if err := tx.flushSystemEvent(); err != nil {
		return ownedTxRow{tx: tx, err: err}
	}
	return ownedTxRow{tx: tx, row: tx.Tx.QueryRow(tx.ctx, statement, args...)}
}

func (row ownedTxRow) Scan(destinations ...any) error {
	if row.tx.finished {
		return pgx.ErrTxClosed
	}
	if row.err != nil {
		return row.err
	}
	return row.tx.store.ownershipErrorLocked(row.row.Scan(destinations...))
}

func (tx *ownedTx) Commit(_ context.Context) error {
	if tx.finished {
		return pgx.ErrTxClosed
	}
	if err := tx.flushSystemEvent(); err != nil {
		_ = tx.Rollback(context.Background())
		return errors.Join(pgx.ErrTxCommitRollback, err)
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

// Flush before subsequent statements so a final authorization query remains
// after the event write. Unauthenticated/internal transactions may instead
// flush at commit. One signal covers all catalog facts in this transaction.
func (tx *ownedTx) flushSystemEvent() error {
	if err := tx.recordNotificationJournal(); err != nil {
		return err
	}
	if tx.systemEventRecorded || (!tx.catalogChanges.resync && len(tx.catalogChanges.changes) == 0) || systemevents.IsDerived(tx.ctx) {
		return nil
	}
	err := systemevents.Record(func(sql string, args ...any) (pgconn.CommandTag, error) { return tx.Tx.Exec(tx.ctx, sql, args...) }, systemevents.LibraryChanged)
	if err == nil {
		tx.systemEventRecorded = true
	}
	return tx.store.ownershipErrorLocked(err)
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
// admission while repository callbacks deliberately run without it. Cancellation
// and the lost flag do not require another admission lock.
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

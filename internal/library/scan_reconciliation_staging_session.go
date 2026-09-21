package library

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type scanStagingTx struct {
	tx           pgx.Tx
	ctx          context.Context
	owner        *scanOwnership
	ddl, discard bool
}

// Only private staging statements use this internal capability. It follows the
// existing Store-before-owner admission order and protected SQL/rollback
// lifetimes, without reopening public writes while Store.Close joins workers.
// The commit callback runs under the owner mutex after confirmed commit.
func (s *Store) withScanStagingTx(ctx context.Context, cleanup bool, operation func(*scanStagingTx) (func(), error)) (resultErr error) {
	if s == nil || s.ownership == nil {
		return ErrUnavailable
	}
	if ctx == nil || operation == nil {
		return ErrInvalidInput
	}
	owner := s.ownership
	if cleanup {
		owner.mu.Lock()
	} else {
		s.mu.Lock()
		if s.closed || s.closing.Load() {
			s.mu.Unlock()
			return ErrUnavailable
		}
		owner.mu.Lock()
		s.mu.Unlock()
	}
	defer func() {
		if cleanup && resultErr != nil && owner.conn != nil {
			// Cleanup also owns failures before Begin or before the first DDL.
			// A closed handle cannot leave a live, reusable orphaned pass behind.
			owner.lost.Store(true)
			s.cancel()
			resultErr = errors.Join(resultErr, fmt.Errorf("%w: failed scan staging cleanup discarded its session", ErrUnavailable), owner.discardLocked())
		}
		owner.mu.Unlock()
	}()
	if owner.conn == nil || owner.lost.Load() {
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	raw, err := owner.conn.Begin(writeCtx)
	if err != nil {
		return s.ownershipErrorLocked(err)
	}
	transaction := &scanStagingTx{tx: raw, ctx: writeCtx, owner: owner}
	finished, committed, rolledBack := false, false, false
	defer func() {
		if !finished {
			rollbackCtx, cancelRollback := context.WithTimeout(context.Background(), 5*time.Second)
			rollbackErr := raw.Rollback(rollbackCtx)
			cancelRollback()
			rolledBack = rollbackErr == nil
			resultErr = errors.Join(resultErr, s.ownershipErrorLocked(rollbackErr))
			if rollbackErr != nil {
				transaction.discard = true
			}
		}
		if !committed && !transaction.ddl && !transaction.discard && owner.scanStagingReady && !owner.lost.Load() {
			// An aborted INSERT can retain heap/index pages. Check the physical
			// high-water mark after rollback, even for an ordinary SQL or logical
			// quota failure, instead of treating rolled-back bytes as reclaimed.
			auditCtx, cancelAudit := context.WithTimeout(context.Background(), 5*time.Second)
			bytes, auditErr := scanStagingPhysicalBytes(auditCtx, owner.conn)
			cancelAudit()
			if auditErr != nil {
				resultErr = errors.Join(resultErr, s.ownershipErrorLocked(auditErr))
				transaction.discard = true
			} else if bytes < 0 || bytes > owner.scanStagingPhysicalLimit() {
				resultErr = errors.Join(resultErr, errScanReconciliationStagingBudget)
				transaction.discard = true
			}
		}
		if transaction.discard || transaction.ddl && !committed && !rolledBack {
			// Rollback does not guarantee that relation high-water allocation was
			// released. Unknown DDL/cleanup outcomes cannot escape through a pool
			// release or leave another pass using a poisoned staging backend.
			owner.lost.Store(true)
			s.cancel()
			resultErr = errors.Join(resultErr, fmt.Errorf("%w: scan staging session was discarded", ErrUnavailable), owner.discardLocked())
		}
	}()
	// Keep SQL and lock failures inside the protected owner lifetime, leaving
	// explicit time for rollback instead of canceling the reserved connection.
	if _, err := raw.Exec(writeCtx, `SELECT set_config('statement_timeout','5000',true), set_config('lock_timeout','1000',true)`); err != nil {
		return s.ownershipErrorLocked(err)
	}
	afterCommit, err := operation(transaction)
	if err != nil {
		return s.ownershipErrorLocked(err)
	}
	if !cleanup {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	err = raw.Commit(writeCtx)
	finished = true
	if err != nil {
		// A failed commit response cannot establish whether this private batch
		// became durable. Fence the session rather than retrying on ambiguity.
		transaction.discard = true
		return s.ownershipErrorLocked(err)
	}
	committed = true
	if afterCommit != nil {
		afterCommit()
	}
	return nil
}

func (transaction *scanStagingTx) create() error {
	transaction.owner.scanStagingDirty = true
	transaction.ddl = true
	// Do not adopt a pre-existing relation with IF NOT EXISTS. A collision is
	// a failed initialization and the entire physical session is discarded.
	if _, err := transaction.tx.Exec(transaction.ctx, `CREATE TEMP TABLE `+scanReconciliationPassTable+` (
		generation text COLLATE "C" NOT NULL CHECK (octet_length(generation)=64),
		scan_id text COLLATE "C" NOT NULL UNIQUE CHECK (octet_length(scan_id) BETWEEN 1 AND 256),
		library_id text COLLATE "C" NOT NULL CHECK (octet_length(library_id) BETWEEN 1 AND 256),
		sealed boolean NOT NULL DEFAULT false,
		seen_rows bigint NOT NULL DEFAULT 0 CHECK (seen_rows BETWEEN 0 AND 262144),
		serialized_bytes bigint NOT NULL DEFAULT 0 CHECK (serialized_bytes BETWEEN 0 AND 67108864),
		PRIMARY KEY (generation,scan_id,library_id)
	) ON COMMIT PRESERVE ROWS`); err != nil {
		transaction.discard = true
		return err
	}
	_, err := transaction.tx.Exec(transaction.ctx, `CREATE TEMP TABLE `+scanReconciliationSeenTable+` (
		generation text COLLATE "C" NOT NULL,
		scan_id text COLLATE "C" NOT NULL,
		library_id text COLLATE "C" NOT NULL,
		item_id text COLLATE "C" NOT NULL CHECK (octet_length(item_id) BETWEEN 1 AND 256),
		PRIMARY KEY (generation,scan_id,library_id,item_id),
		FOREIGN KEY (generation,scan_id,library_id) REFERENCES `+scanReconciliationPassTable+`
			(generation,scan_id,library_id) ON DELETE CASCADE
	) ON COMMIT PRESERVE ROWS`)
	if err != nil {
		transaction.discard = true
	}
	return err
}

func (transaction *scanStagingTx) drop() error {
	transaction.owner.scanStagingDirty = true
	transaction.ddl = true
	if _, err := transaction.tx.Exec(transaction.ctx, `DROP TABLE `+scanReconciliationSeenTable); err != nil {
		transaction.discard = true
		return err
	}
	_, err := transaction.tx.Exec(transaction.ctx, `DROP TABLE `+scanReconciliationPassTable)
	if err != nil {
		transaction.discard = true
	}
	return err
}

func (transaction *scanStagingTx) physicalBytes(limit int64) (int64, error) {
	limit = min(limit, transaction.owner.scanStagingPhysicalLimit())
	bytes, err := scanStagingPhysicalBytes(transaction.ctx, transaction.tx)
	if err != nil {
		return 0, err
	}
	if bytes < 0 || bytes > limit {
		transaction.discard = true
		return 0, fmt.Errorf("%w: PostgreSQL temporary relations and indexes: observed %d bytes, limit %d", errScanReconciliationStagingBudget, bytes, limit)
	}
	return bytes, nil
}

func (owner *scanOwnership) scanStagingPhysicalLimit() int64 {
	limit := int64(scanReconciliationStagingMaxPhysicalBytes)
	for _, stage := range owner.scanStagingPasses {
		limit = min(limit, stage.limits.physicalBytes)
	}
	return limit
}

func scanStagingPhysicalBytes(ctx context.Context, query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (int64, error) {
	var bytes int64
	if err := query.QueryRow(ctx, `SELECT
		pg_total_relation_size('pg_temp.goby_scan_reconciliation_pass'::regclass) +
		pg_total_relation_size('pg_temp.goby_scan_reconciliation_seen'::regclass)`).Scan(&bytes); err != nil {
		return 0, err
	}
	return bytes, nil
}

package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	scanReconciliationPassTable = "pg_temp.goby_scan_reconciliation_pass"
	scanReconciliationSeenTable = "pg_temp.goby_scan_reconciliation_seen"

	scanReconciliationStagingBatchIDs         = 512
	scanReconciliationStagingBatchBytes       = 128 << 10
	scanReconciliationStagingArrayBytes       = 20
	scanReconciliationStagingMaxRows          = 262144
	scanReconciliationStagingMaxBytes         = 64 << 20
	scanReconciliationStagingMaxPhysicalBytes = 128 << 20
	scanReconciliationStagingMaxTempBuffers   = 16 << 20
	scanReconciliationStagingMaxPasses        = 2
)

var (
	// These errors are deliberately not observation-only errors. A failed SQL
	// staging batch must never be mistaken for an empty successful Seen set.
	errScanReconciliationStagingBudget = errors.New("scan reconciliation Seen staging exceeds its budget")
	errScanReconciliationStagingState  = fmt.Errorf("%w: scan reconciliation Seen staging state is unavailable", ErrUnavailable)
)

type scanReconciliationStagingLimits struct {
	rows, serializedBytes, physicalBytes int64
}

func defaultScanReconciliationStagingLimits() scanReconciliationStagingLimits {
	return scanReconciliationStagingLimits{scanReconciliationStagingMaxRows, scanReconciliationStagingMaxBytes, scanReconciliationStagingMaxPhysicalBytes}
}

func (limits scanReconciliationStagingLimits) valid() bool {
	return limits.rows > 0 && limits.rows <= scanReconciliationStagingMaxRows &&
		limits.serializedBytes > 0 && limits.serializedBytes <= scanReconciliationStagingMaxBytes &&
		limits.physicalBytes > 0 && limits.physicalBytes <= scanReconciliationStagingMaxPhysicalBytes
}

// Rows and SerializedBytes count unique committed identities in this pass.
// SerializedBytes includes a four-byte length for each complete UTF-8 ID.
// SessionPhysicalBytes is this handle's last observation of both shared tables,
// their indexes and TOAST, and can change when its sibling writes. The peak is
// likewise observed by this handle, not a sum or a fresh session measurement.
// Neither value includes PostgreSQL temp_buffers memory usage.
type scanReconciliationStagingStats struct {
	Rows, SerializedBytes                          int64
	BufferedIDs, BufferedBytes                     int
	Flushes, PeakBufferedIDs, PeakBufferedBytes    int
	SessionPhysicalBytes, PeakSessionPhysicalBytes int64
}

// A staging handle is valid only for its original reserved owner session. The
// local buffer is bounded; exact cross-batch membership lives in PostgreSQL.
// Record, Seal, Close and Stats are called outside OwnedTx callbacks. Only
// RequireSealed and Contains accept the already-running final transaction.
type scanReconciliationStaging struct {
	mu                            sync.Mutex
	store                         *Store
	owner                         *scanOwnership
	generation, scanID, libraryID string
	limits                        scanReconciliationStagingLimits
	pending                       []string
	pendingSet                    map[string]struct{}
	pendingBytes                  int
	err, closeErr                 error
	sealed, failed, closed        atomic.Bool
	sealedRows, sealedBytes       atomic.Int64
	stats                         scanReconciliationStagingStats
}

func (s *Store) beginScanReconciliationStaging(ctx context.Context, scanID, libraryID string) (*scanReconciliationStaging, error) {
	return s.beginScanReconciliationStagingWithLimits(ctx, scanID, libraryID, defaultScanReconciliationStagingLimits())
}

// Lower limits are used by focused resource-boundary tests. Production uses
// the fixed defaults; no caller can increase the reviewed ceilings here.
func (s *Store) beginScanReconciliationStagingWithLimits(ctx context.Context, scanID, libraryID string, limits scanReconciliationStagingLimits) (*scanReconciliationStaging, error) {
	if s == nil || s.ownership == nil {
		return nil, ErrUnavailable
	}
	if ctx == nil || !validCatalogLibraryIdentifier(scanID) || !validCatalogLibraryIdentifier(libraryID) || !limits.valid() {
		return nil, ErrInvalidInput
	}
	nonce, err := randomID()
	if err != nil {
		return nil, err
	}
	stage := &scanReconciliationStaging{store: s, owner: s.ownership, scanID: strings.Clone(scanID), libraryID: strings.Clone(libraryID), limits: limits}
	err = s.withScanStagingTx(ctx, false, func(transaction *scanStagingTx) (func(), error) {
		owner := transaction.owner
		if owner.scanStagingPasses[scanID] != nil {
			return nil, fmt.Errorf("%w: duplicate active scan staging identity", ErrInvalidInput)
		}
		if len(owner.scanStagingPasses) >= scanReconciliationStagingMaxPasses {
			return nil, fmt.Errorf("%w: scan staging pass limit", ErrBusy)
		}
		if owner.scanStagingGeneration == "" {
			var err error
			owner.scanStagingGeneration, err = randomID()
			if err != nil {
				return nil, err
			}
		}
		stage.generation = owner.scanStagingGeneration + nonce
		var tempBuffers int64
		if err := transaction.tx.QueryRow(transaction.ctx, `SELECT pg_size_bytes(current_setting('temp_buffers'))`).Scan(&tempBuffers); err != nil {
			return nil, err
		}
		if tempBuffers < 0 || tempBuffers > scanReconciliationStagingMaxTempBuffers {
			return nil, fmt.Errorf("%w: PostgreSQL temp_buffers", errScanReconciliationStagingBudget)
		}
		if !owner.scanStagingReady {
			if len(owner.scanStagingPasses) != 0 {
				return nil, errScanReconciliationStagingState
			}
			if err := transaction.create(); err != nil {
				return nil, err
			}
		}
		if _, err := transaction.tx.Exec(transaction.ctx, `INSERT INTO `+scanReconciliationPassTable+`
			(generation, scan_id, library_id) VALUES ($1,$2,$3)`, stage.generation, stage.scanID, stage.libraryID); err != nil {
			return nil, err
		}
		physical, err := transaction.physicalBytes(limits.physicalBytes)
		if err != nil {
			return nil, err
		}
		return func() {
			// Initialized is distinct from dirty and is published only after a
			// confirmed commit, while the owner mutex is still held.
			owner.scanStagingReady = true
			if owner.scanStagingPasses == nil {
				owner.scanStagingPasses = make(map[string]*scanReconciliationStaging, scanReconciliationStagingMaxPasses)
			}
			owner.scanStagingPasses[stage.scanID] = stage
			stage.stats.SessionPhysicalBytes, stage.stats.PeakSessionPhysicalBytes = physical, physical
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return stage, nil
}

func (stage *scanReconciliationStaging) fail(err error) error {
	if stage.err == nil {
		stage.err = err
	}
	stage.failed.Store(true)
	stage.sealed.Store(false)
	return stage.err
}

func (stage *scanReconciliationStaging) writable(ctx context.Context) error {
	if stage.err != nil {
		return stage.err
	}
	if ctx == nil {
		return stage.fail(ErrInvalidInput)
	}
	if stage.closed.Load() || stage.store == nil || stage.store.closing.Load() || stage.owner == nil || stage.owner.lost.Load() {
		return stage.fail(errScanReconciliationStagingState)
	}
	if err := ctx.Err(); err != nil {
		return stage.fail(err)
	}
	return nil
}

func (stage *scanReconciliationStaging) Record(ctx context.Context, id string) error {
	if stage == nil {
		return ErrUnavailable
	}
	stage.mu.Lock()
	defer stage.mu.Unlock()
	if err := stage.writable(ctx); err != nil {
		return err
	}
	if stage.sealed.Load() {
		return stage.fail(errScanReconciliationStagingState)
	}
	if !validCatalogLibraryIdentifier(id) {
		return stage.fail(fmt.Errorf("%w: invalid Seen identity", ErrInvalidInput))
	}
	if _, exists := stage.pendingSet[id]; exists {
		return nil
	}
	charge := len(id) + 4
	if len(stage.pending) >= scanReconciliationStagingBatchIDs ||
		charge > scanReconciliationStagingBatchBytes-scanReconciliationStagingArrayBytes-stage.pendingBytes {
		if err := stage.flush(ctx, false); err != nil {
			return stage.fail(err)
		}
	}
	if stage.pendingSet == nil {
		stage.pendingSet = make(map[string]struct{}, scanReconciliationStagingBatchIDs)
	}
	id = strings.Clone(id)
	stage.pendingSet[id] = struct{}{}
	stage.pending = append(stage.pending, id)
	stage.pendingBytes += charge
	stage.stats.PeakBufferedIDs = max(stage.stats.PeakBufferedIDs, len(stage.pending))
	stage.stats.PeakBufferedBytes = max(stage.stats.PeakBufferedBytes, stage.pendingBytes+scanReconciliationStagingArrayBytes)
	if len(stage.pending) == scanReconciliationStagingBatchIDs {
		if err := stage.flush(ctx, false); err != nil {
			return stage.fail(err)
		}
	}
	return nil
}

func (stage *scanReconciliationStaging) Seal(ctx context.Context) error {
	if stage == nil {
		return ErrUnavailable
	}
	stage.mu.Lock()
	defer stage.mu.Unlock()
	if err := stage.writable(ctx); err != nil {
		return err
	}
	if stage.sealed.Load() {
		return nil
	}
	if err := stage.flush(ctx, true); err != nil {
		return stage.fail(err)
	}
	// A cancellation racing the successful commit still revokes this handle's
	// authority. The caller also retains its original task cancellation fence.
	if err := ctx.Err(); err != nil {
		return stage.fail(err)
	}
	return nil
}

func (stage *scanReconciliationStaging) flush(ctx context.Context, seal bool) error {
	if !seal && len(stage.pending) == 0 {
		return nil
	}
	return stage.store.withScanStagingTx(ctx, false, func(transaction *scanStagingTx) (func(), error) {
		if transaction.owner != stage.owner || !transaction.owner.scanStagingReady || transaction.owner.scanStagingPasses[stage.scanID] != stage {
			return nil, errScanReconciliationStagingState
		}
		var rows, bytes int64
		var sealed bool
		if err := transaction.tx.QueryRow(transaction.ctx, `SELECT seen_rows,serialized_bytes,sealed FROM `+scanReconciliationPassTable+`
			WHERE generation=$1 AND scan_id=$2 AND library_id=$3 FOR UPDATE`, stage.generation, stage.scanID, stage.libraryID).Scan(&rows, &bytes, &sealed); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, errScanReconciliationStagingState
			}
			return nil, err
		}
		if sealed || rows != stage.stats.Rows || bytes != stage.stats.SerializedBytes {
			return nil, errScanReconciliationStagingState
		}
		if len(stage.pending) != 0 {
			var addedRows, addedBytes int64
			if err := transaction.tx.QueryRow(transaction.ctx, `WITH inserted AS (
				INSERT INTO `+scanReconciliationSeenTable+` (generation,scan_id,library_id,item_id)
				SELECT $1,$2,$3,item_id FROM unnest($4::text[]) AS input(item_id)
				ON CONFLICT DO NOTHING RETURNING octet_length(convert_to(item_id,'UTF8'))+4 AS serialized_bytes)
				SELECT count(*)::bigint,COALESCE(sum(serialized_bytes),0)::bigint FROM inserted`,
				stage.generation, stage.scanID, stage.libraryID, stage.pending).Scan(&addedRows, &addedBytes); err != nil {
				return nil, err
			}
			if addedRows < 0 || addedBytes < 0 || addedRows > stage.limits.rows-rows || addedBytes > stage.limits.serializedBytes-bytes {
				return nil, errScanReconciliationStagingBudget
			}
			rows, bytes = rows+addedRows, bytes+addedBytes
		}
		tag, err := transaction.tx.Exec(transaction.ctx, `UPDATE `+scanReconciliationPassTable+`
			SET seen_rows=$4,serialized_bytes=$5,sealed=$6 WHERE generation=$1 AND scan_id=$2 AND library_id=$3 AND NOT sealed`,
			stage.generation, stage.scanID, stage.libraryID, rows, bytes, seal)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() != 1 {
			return nil, errScanReconciliationStagingState
		}
		physical, err := transaction.physicalBytes(stage.limits.physicalBytes)
		if err != nil {
			return nil, err
		}
		return func() {
			stage.stats.Rows, stage.stats.SerializedBytes = rows, bytes
			stage.stats.SessionPhysicalBytes = physical
			stage.stats.PeakSessionPhysicalBytes = max(stage.stats.PeakSessionPhysicalBytes, physical)
			if len(stage.pending) != 0 {
				stage.stats.Flushes++
			}
			stage.pending, stage.pendingSet, stage.pendingBytes = nil, nil, 0
			if seal {
				stage.sealedRows.Store(rows)
				stage.sealedBytes.Store(bytes)
				stage.sealed.Store(true)
			}
		}, nil
	})
}

// Scope supplies the three exact anti-join parameters in generation/scan/library
// order. RequireSealed must succeed in the same final OwnedTx before using them.
func (stage *scanReconciliationStaging) Scope() (generation, scanID, libraryID string) {
	if stage == nil {
		return "", "", ""
	}
	return stage.generation, stage.scanID, stage.libraryID
}

func (stage *scanReconciliationStaging) RequireSealed(tx OwnedTx) error {
	if stage == nil || stage.closed.Load() || stage.failed.Load() || !stage.sealed.Load() || stage.owner == nil || stage.owner.lost.Load() {
		return errScanReconciliationStagingState
	}
	view, ok := tx.(*ownedCallbackTx)
	if !ok || view.catalog == nil || view.catalog.store != stage.store || stage.store.ownership != stage.owner {
		return errScanReconciliationStagingState
	}
	var sealed bool
	var rows, bytes, physical int64
	err := tx.QueryRow(`SELECT sealed,seen_rows,serialized_bytes,
		pg_total_relation_size('pg_temp.goby_scan_reconciliation_pass'::regclass) +
		pg_total_relation_size('pg_temp.goby_scan_reconciliation_seen'::regclass)
		FROM `+scanReconciliationPassTable+`
		WHERE generation=$1 AND scan_id=$2 AND library_id=$3`, stage.generation, stage.scanID, stage.libraryID).Scan(&sealed, &rows, &bytes, &physical)
	if errors.Is(err, pgx.ErrNoRows) {
		return errScanReconciliationStagingState
	}
	if err != nil {
		return err
	}
	if !sealed || rows < 0 || rows > stage.limits.rows || bytes < 0 || bytes > stage.limits.serializedBytes ||
		rows != stage.sealedRows.Load() || bytes != stage.sealedBytes.Load() {
		return errScanReconciliationStagingState
	}
	if physical < 0 || physical > stage.limits.physicalBytes {
		return errScanReconciliationStagingBudget
	}
	return nil
}

// Contains returns only a bounded, independent membership map. Filesystem
// observers may retain that map, but never this handle or a transaction.
func (stage *scanReconciliationStaging) Contains(tx OwnedTx, ids []string) (map[string]bool, error) {
	if len(ids) > scanReconciliationStagingBatchIDs {
		return nil, errScanReconciliationStagingBudget
	}
	bytes := scanReconciliationStagingArrayBytes
	for _, id := range ids {
		if !validCatalogLibraryIdentifier(id) {
			return nil, ErrInvalidInput
		}
		charge := len(id) + 4
		if charge > scanReconciliationStagingBatchBytes-bytes {
			return nil, errScanReconciliationStagingBudget
		}
		bytes += charge
	}
	if err := stage.RequireSealed(tx); err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := tx.Query(`SELECT item_id FROM `+scanReconciliationSeenTable+`
		WHERE generation=$1 AND scan_id=$2 AND library_id=$3 AND item_id=ANY($4::text[]) ORDER BY item_id`,
		stage.generation, stage.scanID, stage.libraryID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if len(result) >= len(ids) || !validCatalogLibraryIdentifier(id) {
			return nil, errScanReconciliationStagingState
		}
		result[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (stage *scanReconciliationStaging) Stats() scanReconciliationStagingStats {
	if stage == nil {
		return scanReconciliationStagingStats{}
	}
	stage.mu.Lock()
	defer stage.mu.Unlock()
	result := stage.stats
	result.BufferedIDs = len(stage.pending)
	if len(stage.pending) != 0 {
		result.BufferedBytes = stage.pendingBytes + scanReconciliationStagingArrayBytes
	}
	return result
}

func (stage *scanReconciliationStaging) Close() error {
	if stage == nil {
		return nil
	}
	stage.mu.Lock()
	defer stage.mu.Unlock()
	if stage.closed.Swap(true) {
		return stage.closeErr
	}
	stage.sealed.Store(false)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := stage.store.withScanStagingTx(ctx, true, func(transaction *scanStagingTx) (func(), error) {
		if transaction.owner != stage.owner || !transaction.owner.scanStagingReady || transaction.owner.scanStagingPasses[stage.scanID] != stage {
			transaction.discard = true
			return nil, errScanReconciliationStagingState
		}
		physical := int64(0)
		last := len(transaction.owner.scanStagingPasses) == 1
		if last {
			if err := transaction.drop(); err != nil {
				return nil, err
			}
		} else {
			tag, err := transaction.tx.Exec(transaction.ctx, `DELETE FROM `+scanReconciliationPassTable+`
				WHERE generation=$1 AND scan_id=$2 AND library_id=$3`, stage.generation, stage.scanID, stage.libraryID)
			if err != nil || tag.RowsAffected() != 1 {
				transaction.discard = true
				if err != nil {
					return nil, err
				}
				return nil, errScanReconciliationStagingState
			}
			physical, err = transaction.physicalBytes(stage.limits.physicalBytes)
			if err != nil {
				transaction.discard = true
				return nil, err
			}
		}
		return func() {
			delete(transaction.owner.scanStagingPasses, stage.scanID)
			if last {
				transaction.owner.scanStagingReady = false
			}
			stage.pending, stage.pendingSet, stage.pendingBytes = nil, nil, 0
			stage.stats.SessionPhysicalBytes = physical
		}, nil
	})
	stage.closeErr = err
	return err
}

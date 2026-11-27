package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/identity"
)

// AcquireLocalBackdropLock issues a bounded window for one pending physical
// source. It does not rename files, change scheduler settings or advance work.
func (s *Store) AcquireLocalBackdropLock(ctx context.Context, actor identity.Principal, itemID string) (_ LocalBackdropLockInfo, resultErr error) {
	if ctx == nil || !validCatalogLibraryIdentifier(itemID) {
		return LocalBackdropLockInfo{}, ErrInvalidInput
	}
	if !s.Available() {
		return LocalBackdropLockInfo{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, localBackdropRereadTimeout)
	defer cancel()
	defer func() { resultErr = localBackdropWriterError(resultErr) }()
	target, err := s.prepareLocalBackdropReread(ctx, actor, itemID, true)
	if err != nil {
		return LocalBackdropLockInfo{}, err
	}
	if s.backdropExecution.current() != nil {
		return LocalBackdropLockInfo{}, ErrLocalBackdropLocked
	}
	plan, op, err := s.openLocalBackdropSource(ctx, target)
	if err != nil {
		return LocalBackdropLockInfo{}, err
	}
	defer func() {
		if err := op.Close(); err != nil {
			resultErr = errors.Join(resultErr, ErrUnavailable, err)
		}
	}()
	lockID, err := randomID()
	if err != nil {
		return LocalBackdropLockInfo{}, err
	}
	admin := &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
	tx, err := s.beginLocalBackdropWriter(ctx)
	if err != nil {
		return LocalBackdropLockInfo{}, err
	}
	committed, attempted, reserved := false, false, false
	defer func() {
		knownRollback := !attempted || errors.Is(resultErr, pgx.ErrTxCommitRollback)
		if !committed {
			resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr))
		}
		if reserved && (committed || knownRollback) {
			s.backdropExecution.finishChange(committed)
			if knownRollback {
				s.notifyScanUpdate()
			}
		}
		// An ambiguous commit retains the in-memory hold, but does not permit
		// reread or release. A new catalog owner loads its durable disposition.
	}()
	if err := admin.check(ctx, tx, true); err != nil {
		return LocalBackdropLockInfo{}, err
	}
	if _, err := readLocalBackdropTarget(ctx, tx, itemID, &target, true); err != nil {
		return LocalBackdropLockInfo{}, err
	}
	if err := checkLocalBackdropUnshadowed(ctx, tx, itemID); err != nil {
		return LocalBackdropLockInfo{}, err
	}
	lock := newLocalBackdropLock(target, actor.SessionID, lockID, time.Now().Add(localBackdropLockWindow))
	lock.Aliases, err = proveLocalBackdropLockDirectory(ctx, op, plan, target)
	if err != nil {
		return LocalBackdropLockInfo{}, err
	}
	if !lock.valid() {
		return LocalBackdropLockInfo{}, ErrSourceChanged
	}
	var activeScan bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scan_jobs j JOIN library_roots r ON r.library_id=j.library_id
		WHERE j.library_id<>$1 AND j.status IN ('Queued','Running') AND `+localBackdropScanOverlapsDirectorySQL+`)`, lock.LibraryID, lock.directories()).
		Scan(&activeScan); err != nil {
		return LocalBackdropLockInfo{}, err
	}
	if activeScan {
		return LocalBackdropLockInfo{}, ErrBusy
	}
	if err := s.backdropExecution.reserve(lock); err != nil {
		return LocalBackdropLockInfo{}, err
	}
	reserved = true
	encoded, err := json.Marshal(lock)
	if err != nil || len(encoded) > localBackdropLockMaxBytes {
		return LocalBackdropLockInfo{}, ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `INSERT INTO server_settings(key,value) VALUES($1,$2)`, localBackdropLockKey, string(encoded)); err != nil {
		return LocalBackdropLockInfo{}, err
	}
	aliases, err := proveLocalBackdropLockDirectory(ctx, op, plan, target)
	if err != nil {
		return LocalBackdropLockInfo{}, err
	}
	if !lock.sameAliases(aliases) {
		return LocalBackdropLockInfo{}, ErrSourceChanged
	}
	if err := admin.check(ctx, tx, false); err != nil {
		return LocalBackdropLockInfo{}, err
	}
	if err := ctx.Err(); err != nil {
		return LocalBackdropLockInfo{}, err
	}
	if !s.Available() {
		return LocalBackdropLockInfo{}, ErrUnavailable
	}
	attempted = true
	if err := tx.Commit(ctx); err != nil {
		return LocalBackdropLockInfo{}, errors.Join(ErrUnavailable, err)
	}
	committed = true
	return lock.Info, nil
}

func proveLocalBackdropLockDirectory(ctx context.Context, op *PrimaryRootIO, plan seriesNFOAuditRootPlan, target localBackdropTarget) ([]string, error) {
	proof, finishProof := context.WithTimeout(ctx, 2*time.Second)
	defer finishProof()
	path := target.item.directory(target.root.root)
	directory := target.workScope().directory
	aliases, err := runSeriesNFOAuditRoot(proof, op, plan, func(work context.Context, root *os.Root, _ seriesNFOAuditRootWitness) (_ []string, resultErr error) {
		dir, err := openRootTopologyRelative(work, root, path)
		if err != nil {
			return nil, errors.Join(ErrSourceChanged, work.Err())
		}
		defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(work, dir)) }()
		info, err := dir.Stat(".")
		if err != nil || !info.IsDir() || fileIdentity(info) != target.queue.directoryIdentity {
			return nil, errors.Join(ErrSourceChanged, work.Err())
		}
		aliases, err := observeBackdropLockAliases(work, dir, directory)
		if err != nil {
			return nil, errors.Join(ErrSourceChanged, err)
		}
		current, err := openRootTopologyRelative(work, root, path)
		if err != nil {
			return nil, errors.Join(ErrSourceChanged, work.Err())
		}
		defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(work, current)) }()
		after, err := current.Stat(".")
		if err != nil || !sameSubtitleDirectoryInfo(info, after) {
			return nil, errors.Join(ErrSourceChanged, work.Err())
		}
		return aliases, work.Err()
	})
	if errors.Is(err, errSeriesNFOAuditRoot) {
		err = errors.Join(ErrSourceChanged, err)
	}
	return aliases, err
}

// ReleaseLocalBackdropLock is a compare-and-delete of the acknowledged hold.
// An active native administrator can recover an expired or abandoned window;
// only the original session can publish under it. A pinned reread always wins.
func (s *Store) ReleaseLocalBackdropLock(ctx context.Context, actor identity.Principal, itemID, lockID string) (resultErr error) {
	if ctx == nil || !validCatalogLibraryIdentifier(itemID) || !validLocalBackdropLockID(lockID) {
		return ErrInvalidInput
	}
	if !s.Available() {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, localBackdropRereadTimeout)
	defer cancel()
	defer func() { resultErr = localBackdropWriterError(resultErr) }()
	tx, err := s.beginLocalBackdropWriter(ctx)
	if err != nil {
		return err
	}
	committed, attempted, releasing := false, false, false
	defer func() {
		knownRollback := !attempted || errors.Is(resultErr, pgx.ErrTxCommitRollback)
		if !committed {
			resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr))
		}
		if releasing && (committed || knownRollback) {
			s.backdropExecution.finishChange(!committed)
			if committed {
				s.notifyScanUpdate()
			}
		}
	}()
	admin := &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
	if err := admin.check(ctx, tx, true); err != nil {
		return err
	}
	if err := s.backdropExecution.startRelease(itemID, lockID); err != nil {
		return err
	}
	releasing = true
	deleted, err := tx.Exec(ctx, `DELETE FROM server_settings WHERE key=$1
		AND value::jsonb #>> '{info,ItemId}'=$2 AND value::jsonb #>> '{info,LockId}'=$3`, localBackdropLockKey, itemID, lockID)
	if err != nil {
		return err
	}
	if deleted.RowsAffected() != 1 {
		return ErrLocalBackdropLockChanged
	}
	if err := admin.check(ctx, tx, false); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.Available() {
		return ErrUnavailable
	}
	attempted = true
	if err := tx.Commit(ctx); err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	committed = true
	return nil
}

func (s *Store) beginLocalBackdropWriter(ctx context.Context) (*ownedTx, error) {
	if !s.Available() {
		return nil, ErrUnavailable
	}
	if !s.ownership.mu.TryLock() {
		return nil, ErrBusy
	}
	if !s.Available() || s.ownership.conn == nil {
		s.ownership.mu.Unlock()
		return nil, ErrUnavailable
	}
	raw, err := s.beginOwnedTxLocked(ctx)
	if err != nil {
		return nil, err
	}
	tx := raw.(*ownedTx)
	for _, statement := range []string{
		`SET TRANSACTION ISOLATION LEVEL READ COMMITTED`,
		`SELECT set_config('statement_timeout','3s',true), set_config('lock_timeout','750ms',true)`,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return nil, errors.Join(err, rollbackSidecarTransaction(tx, err))
		}
	}
	return tx, nil
}

func localBackdropWriterError(err error) error {
	var sqlError *pgconn.PgError
	if errors.As(err, &sqlError) && (sqlError.Code == "57014" || sqlError.Code == "55P03") {
		return errors.Join(ErrUnavailable, err)
	}
	return err
}

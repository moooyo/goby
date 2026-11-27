package library

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/systemevents"
)

const localNFORereadTimeout = 12 * time.Second

var (
	ErrLocalNFODisabled      = errors.New("local NFO reading is disabled for this library")
	ErrLocalNFOMissing       = errors.New("no local NFO candidate exists for this item")
	ErrLocalNFORejected      = errors.New("the preferred local NFO could not be accepted")
	ErrLocalNFORecoveryOwned = errors.New("the registered root is managed by automatic NFO recovery")
)

type LocalNFORereadResult struct {
	ItemID  string `json:"ItemId"`
	Changed bool   `json:"Changed"`
}

type localNFORereadObserver func(context.Context, *PrimaryRootIO, seriesNFOAuditRootPlan, localNFORereadTarget) (localNFORereadObservation, error)

// RereadLocalNFO imports the current local sidecar of one indexed item. It does
// not create scan authority, finish pending work, or write anything to storage.
func (s *Store) RereadLocalNFO(ctx context.Context, actor identity.Principal, itemID string) (LocalNFORereadResult, error) {
	return s.rereadLocalNFO(ctx, actor, itemID, observeLocalNFOReread)
}

func (s *Store) rereadLocalNFO(ctx context.Context, actor identity.Principal, itemID string, observe localNFORereadObserver) (_ LocalNFORereadResult, resultErr error) {
	if ctx == nil || !validCatalogLibraryIdentifier(itemID) {
		return LocalNFORereadResult{}, ErrInvalidInput
	}
	if s == nil || s.pool == nil || !s.Available() {
		return LocalNFORereadResult{}, ErrUnavailable
	}
	if !s.localNFORereadRunning.CompareAndSwap(false, true) {
		return LocalNFORereadResult{}, ErrBusy
	}
	var retired <-chan struct{}
	defer func() {
		releaseAfterSourceOperationRetires(retired, func() { s.localNFORereadRunning.Store(false) })
		var sqlError *pgconn.PgError
		if errors.As(resultErr, &sqlError) && (sqlError.Code == "57014" || sqlError.Code == "55P03") {
			resultErr = errors.Join(ErrUnavailable, resultErr)
		}
	}()
	ctx, cancel := context.WithTimeout(systemevents.WithDerived(ctx), localNFORereadTimeout)
	defer cancel()

	target, fence, row, err := s.prepareLocalNFOReread(ctx, actor, itemID)
	if err != nil {
		return LocalNFORereadResult{}, err
	}
	approved, err := row.validate()
	if err != nil || row.boundAt != nil && row.boundAt.After(fence.finishedAt) {
		return LocalNFORereadResult{}, ErrSourceChanged
	}
	// These roots declare a single automatic writer. Leave their accepted
	// original-byte provenance and publication to the scan/recovery workflow.
	if s.nfoRecoveryWritable(row.root) {
		return LocalNFORereadResult{}, ErrLocalNFORecoveryOwned
	}
	if _, err := s.configuredRootBindingPath(row.root); err != nil {
		return LocalNFORereadResult{}, err
	}
	plan := seriesNFOAuditRootPlan{root: row.root, revision: row.revision}
	if approved != nil {
		plan.fingerprint, err = approved.Fingerprint()
		if err != nil {
			return LocalNFORereadResult{}, ErrSourceChanged
		}
	}
	operation, err := s.preparePrimaryRootIO(ctx, []mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
	if err != nil {
		return LocalNFORereadResult{}, err
	}
	retired = operation.handle.state.done
	defer func() {
		if err := operation.Close(); err != nil {
			resultErr = errors.Join(resultErr, ErrUnavailable, err)
		}
	}()
	observed, err := observe(ctx, operation, plan, target)
	if err != nil {
		return LocalNFORereadResult{}, err
	}
	if observed.nfo.invalid || observed.nfo.path == "" {
		if err := s.finishAnalysisAdminRead(ctx, actor); err != nil {
			return LocalNFORereadResult{}, err
		}
		if observed.nfo.invalid {
			return LocalNFORereadResult{}, ErrLocalNFORejected
		}
		return LocalNFORereadResult{}, ErrLocalNFOMissing
	}
	pairSource := target.pairSource(row, observed)
	binding, err := pairSource.readBinding(ctx, s.pool, false)
	if err != nil {
		return LocalNFORereadResult{}, err
	}
	local, err := target.parse(observed.nfo, binding)
	if err != nil {
		if authErr := s.finishAnalysisAdminRead(ctx, actor); authErr != nil {
			return LocalNFORereadResult{}, authErr
		}
		return LocalNFORereadResult{}, err
	}
	administrator := &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
	changed, err := s.publishLocalNFOReread(ctx, administrator, target, fence, row, pairSource, binding, local, func(work context.Context) error {
		verified, err := observeLocalNFOReread(work, operation, plan, target)
		if err != nil {
			return err
		}
		if !observed.same(verified) {
			return ErrSourceChanged
		}
		return nil
	})
	if err != nil {
		return LocalNFORereadResult{}, err
	}
	return LocalNFORereadResult{ItemID: itemID, Changed: changed}, nil
}

func (s *Store) prepareLocalNFOReread(ctx context.Context, actor identity.Principal, itemID string) (localNFORereadTarget, localNFORereadFence, rootBindingRow, error) {
	tx, err := s.beginAnalysisAdminRead(ctx, actor)
	if err != nil {
		return localNFORereadTarget{}, localNFORereadFence{}, rootBindingRow{}, err
	}
	defer rollback(tx)
	target, fence, row, err := readLocalNFOReread(ctx, tx, itemID, nil)
	if err != nil {
		return target, fence, row, err
	}
	return target, fence, row, tx.Commit(ctx)
}

func (s *Store) publishLocalNFOReread(ctx context.Context, admin *catalogAdministrator, target localNFORereadTarget,
	fence localNFORereadFence, row rootBindingRow, pairSource episodePairSource, binding *episodePairBinding, local localMetadata,
	prove func(context.Context) error) (_ bool, resultErr error) {
	// Do not queue behind a long catalog writer. This operation is optional and
	// can be retried with a fresh observation after the writer has retired.
	if !s.ownership.mu.TryLock() {
		return false, ErrBusy
	}
	if !s.Available() || s.ownership.conn == nil {
		s.ownership.mu.Unlock()
		return false, ErrUnavailable
	}
	tx, err := s.beginOwnedTxLocked(ctx)
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr))
		}
	}()
	for _, statement := range []string{
		`SET TRANSACTION ISOLATION LEVEL READ COMMITTED`,
		`SELECT set_config('statement_timeout','3s',true), set_config('lock_timeout','750ms',true)`,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return false, err
		}
	}
	if err := admin.check(ctx, tx, true); err != nil {
		return false, err
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM libraries WHERE id=$1 FOR UPDATE`, target.libraryID).Scan(new(string)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrSourceChanged
		}
		return false, err
	}
	current, currentFence, currentRoot, err := readLocalNFOReread(ctx, tx, target.id, &target)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrLocalNFODisabled) {
		return false, ErrSourceChanged
	}
	if err != nil {
		return false, err
	}
	if !target.same(current) || !fence.same(currentFence) || !row.same(currentRoot) {
		return false, ErrSourceChanged
	}
	currentBinding, err := pairSource.readBinding(ctx, tx, true)
	if err != nil {
		return false, err
	}
	if !binding.same(currentBinding) {
		return false, ErrSourceChanged
	}
	var previous localMetadata
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT local_metadata,local_metadata_hash,local_metadata_path FROM items WHERE id=$1`, target.id).
		Scan(&raw, &previous.hash, &previous.path); err != nil {
		return false, err
	}
	if err := decodeLocalMetadata(raw, &previous); err != nil {
		return false, err
	}
	if previous.pair != nil && !previous.pair.same(binding) && local.pair == nil {
		// Only a newly bound pair may replace stale pair evidence. A single
		// root cannot remove the policy gate after the indexed source moved.
		return false, ErrSourceChanged
	}
	local.raw = previous.raw
	changed := local.hash != previous.hash || local.path != previous.path || !local.pair.same(previous.pair) || !reflect.DeepEqual(local.value, previous.value)
	if changed {
		name, sortName, overview := describeFromLocal(target.fallbackName(row.root), local)
		base := &scannedMetadataBase{Name: name, SortName: sortName, Overview: overview}
		if target.kind == "Episode" {
			base.IndexNumber = &target.index
		}
		if err := publishLocalNFOProjection(ctx, tx, target.libraryID, target.id, local, base, nil); err != nil {
			return false, err
		}
	}
	// Flush the catalog notifications and recheck authorization after waiting
	// for rows. Leave time for rollback even if the physical check times out.
	if err := admin.check(ctx, tx, false); err != nil {
		return false, err
	}
	deadline, _ := tx.(*ownedTx).ctx.Deadline()
	bounded, cancel := context.WithDeadline(ctx, deadline.Add(-scanReconciliationRollbackSpace))
	defer cancel()
	if err := prove(bounded); err != nil {
		return false, err
	}
	if err := admin.check(ctx, tx, false); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !s.Available() {
		return false, ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	committed = true
	return changed, nil
}

func (target localNFORereadTarget) fallbackName(root libraryRoot) string {
	base := filepath.Base(target.sourcePath(root))
	if target.relative == tvRootSeriesPath {
		base = filepath.Base(root.path)
	} else if !target.folder {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return cleanName(base)
}

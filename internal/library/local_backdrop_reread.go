package library

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/systemevents"
)

const (
	localBackdropRereadTimeout = 12 * time.Second
	localBackdropImageType     = "Backdrop"
)

var (
	ErrLocalBackdropDisabled    = errors.New("local artwork reading is disabled for this library")
	ErrLocalBackdropNotPending  = errors.New("the series has no eligible image-only pending import")
	ErrLocalBackdropRejected    = errors.New("a nonempty, fully valid local backdrop set could not be verified")
	ErrLocalBackdropOtherImages = errors.New("other local artwork does not match the accepted image rows")
	ErrLocalBackdropShadowed    = errors.New("the series has managed or provider backdrop artwork")
	ErrLocalBackdropOnlineGap   = errors.New("completing the local image phase would expose an online image gap")
)

type LocalBackdropRereadResult struct {
	ItemID  string `json:"ItemId"`
	Changed bool   `json:"Changed"`
}

// RereadLocalBackdrop accepts one physical series' pending backdrop without
// changing files, admitting a scan or signalling any downstream task.
func (s *Store) RereadLocalBackdrop(ctx context.Context, actor identity.Principal, itemID string) (LocalBackdropRereadResult, error) {
	return s.rereadLocalBackdrop(ctx, actor, itemID, "", s.observeLocalBackdrop)
}

// RereadLockedLocalBackdrop publishes under an acknowledged maintenance hold.
// A different session cannot borrow it, and success does not release it.
func (s *Store) RereadLockedLocalBackdrop(ctx context.Context, actor identity.Principal, itemID, lockID string) (LocalBackdropRereadResult, error) {
	if !validLocalBackdropLockID(lockID) {
		return LocalBackdropRereadResult{}, ErrInvalidInput
	}
	return s.rereadLocalBackdrop(ctx, actor, itemID, lockID, s.observeLocalBackdrop)
}

func (s *Store) rereadLocalBackdrop(ctx context.Context, actor identity.Principal, itemID, lockID string, observe localBackdropObserver) (_ LocalBackdropRereadResult, resultErr error) {
	if ctx == nil || !validCatalogLibraryIdentifier(itemID) {
		return LocalBackdropRereadResult{}, ErrInvalidInput
	}
	if s == nil || s.pool == nil || !s.Available() {
		return LocalBackdropRereadResult{}, ErrUnavailable
	}
	if !s.localBackdropRereadRunning.CompareAndSwap(false, true) {
		return LocalBackdropRereadResult{}, ErrBusy
	}
	var retired <-chan struct{}
	var releaseWork func()
	defer func() {
		releaseAfterSourceOperationRetires(retired, func() {
			if releaseWork != nil {
				releaseWork()
			}
			s.localBackdropRereadRunning.Store(false)
		})
		resultErr = localBackdropWriterError(resultErr)
	}()
	ctx, cancel := context.WithTimeout(systemevents.WithDerived(ctx), localBackdropRereadTimeout)
	defer cancel()

	var lock *localBackdropLock
	if lockID != "" {
		if err := s.finishAnalysisAdminRead(ctx, actor); err != nil {
			return LocalBackdropRereadResult{}, err
		}
		var err error
		lock, releaseWork, err = s.backdropExecution.pin(itemID, lockID, actor.SessionID)
		if err != nil {
			return LocalBackdropRereadResult{}, err
		}
		bounded, stop := context.WithTimeout(ctx, time.Until(lock.Info.ExpiresAt))
		defer stop()
		ctx = bounded
	} else if held := s.backdropExecution.current(); held != nil && held.Info.ItemID == itemID {
		if err := s.finishAnalysisAdminRead(ctx, actor); err != nil {
			return LocalBackdropRereadResult{}, err
		}
		return LocalBackdropRereadResult{}, ErrLocalBackdropLocked
	}
	target, err := s.prepareLocalBackdropReread(ctx, actor, itemID, lock != nil)
	if err != nil {
		return LocalBackdropRereadResult{}, err
	}
	if lock != nil {
		if !lock.matches(target) {
			return LocalBackdropRereadResult{}, ErrSourceChanged
		}
	} else {
		var admitted bool
		releaseWork, admitted = s.backdropExecution.beginWork(target.workScope())
		if !admitted {
			return LocalBackdropRereadResult{}, ErrLocalBackdropLocked
		}
	}
	plan, op, err := s.openLocalBackdropSource(ctx, target)
	if err != nil {
		return LocalBackdropRereadResult{}, err
	}
	retired = op.handle.state.done
	defer func() {
		if err := op.Close(); err != nil {
			resultErr = errors.Join(resultErr, ErrUnavailable, err)
		}
	}()
	observed, err := observe(ctx, op, plan, target)
	if err != nil {
		if errors.Is(err, ErrLocalBackdropRejected) || errors.Is(err, ErrLocalBackdropOtherImages) || errors.Is(err, ErrSourceChanged) {
			if authErr := s.finishAnalysisAdminRead(ctx, actor); authErr != nil {
				return LocalBackdropRereadResult{}, errors.Join(authErr, err)
			}
		}
		return LocalBackdropRereadResult{}, err
	}
	admin := &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
	changed, err := s.publishLocalBackdropReread(ctx, admin, target, lock, observed, func(work context.Context) error {
		if lock != nil {
			aliases, err := proveLocalBackdropLockDirectory(work, op, plan, target)
			if err != nil {
				return err
			}
			if !lock.sameAliases(aliases) {
				return ErrSourceChanged
			}
		}
		_, err := s.readLocalBackdropSource(work, op, plan, target, &observed)
		return err
	})
	if err != nil {
		return LocalBackdropRereadResult{}, err
	}
	return LocalBackdropRereadResult{ItemID: itemID, Changed: changed}, nil
}

func (s *Store) openLocalBackdropSource(ctx context.Context, target localBackdropTarget) (seriesNFOAuditRootPlan, *PrimaryRootIO, error) {
	row := target.root
	approved, err := row.validate()
	if err != nil || row.boundAt != nil && row.boundAt.After(target.generation.finishedAt) {
		return seriesNFOAuditRootPlan{}, nil, ErrSourceChanged
	}
	if _, err := s.configuredRootBindingPath(row.root); err != nil {
		return seriesNFOAuditRootPlan{}, nil, err
	}
	plan := seriesNFOAuditRootPlan{root: row.root, revision: row.revision}
	if approved != nil {
		plan.fingerprint, err = approved.Fingerprint()
		if err != nil {
			return seriesNFOAuditRootPlan{}, nil, ErrSourceChanged
		}
	}
	op, err := s.preparePrimaryRootIO(ctx, []mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
	return plan, op, err
}

func (s *Store) prepareLocalBackdropReread(ctx context.Context, actor identity.Principal, id string, scopedImporter bool) (localBackdropTarget, error) {
	tx, err := s.beginAnalysisAdminRead(ctx, actor)
	if err != nil {
		return localBackdropTarget{}, err
	}
	defer rollback(tx)
	target, err := readLocalBackdropTarget(ctx, tx, id, nil, scopedImporter)
	if err != nil {
		return localBackdropTarget{}, err
	}
	return target, tx.Commit(ctx)
}

func (s *Store) publishLocalBackdropReread(ctx context.Context, admin *catalogAdministrator, target localBackdropTarget,
	lock *localBackdropLock, observed localBackdropObservation, prove func(context.Context) error) (_ bool, resultErr error) {
	tx, err := s.beginLocalBackdropWriter(ctx)
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr))
		}
	}()
	if err := admin.check(ctx, tx, true); err != nil {
		return false, err
	}
	if lock != nil {
		current, err := readDurableLocalBackdropLock(ctx, tx)
		if err != nil {
			return false, err
		}
		if !lock.same(current) {
			return false, ErrLocalBackdropLockChanged
		}
	}
	item := target.item
	if _, err := readLocalBackdropTarget(ctx, tx, item.id, &target, lock != nil); err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrLocalBackdropDisabled) {
			err = ErrSourceChanged
		}
		return false, err
	}
	if err := checkLocalBackdropUnshadowed(ctx, tx, item.id); err != nil {
		return false, err
	}
	_, otherChanged, err := readImageCatalogReplacement(ctx, tx, item.id, item.libraryID, item.rootID, localBackdropOtherTypes, observed.other)
	if err != nil {
		return false, err
	}
	if otherChanged {
		return false, ErrLocalBackdropOtherImages
	}
	changed, err := publishImageCatalogReplacement(ctx, tx, item.id, item.libraryID, item.rootID,
		[]string{localBackdropImageType}, []int{len(observed.backdrops.types)}, observed.backdrops)
	if err != nil {
		return false, err
	}
	if target.generation.onlineImages {
		if err := checkLocalBackdropOnlineCoverage(ctx, tx, item); err != nil {
			return false, err
		}
	}
	// Do not use the ordinary completion helper: it emits downstream signals.
	q := target.queue
	deleted, err := tx.Exec(ctx, `DELETE FROM local_metadata_ingest WHERE item_id=$1 AND scan_job_id=$2 AND source_key=$3
		AND directory_identity=$4 AND index_number=$5 AND parent_index_number=$6 AND parent_number_defined=$7
		AND import_images AND NOT import_nfo AND NOT enrich_online`, item.id, q.jobID, q.sourceKey, q.directoryIdentity,
		q.index, q.parentIndex, q.parentDefined)
	if err != nil {
		return false, err
	}
	if deleted.RowsAffected() != 1 {
		return false, ErrSourceChanged
	}
	// Flush client notifications before proving the filesystem. The derived
	// context still suppresses LibraryChanged, including on an effective change.
	if err := admin.check(ctx, tx, false); err != nil {
		return false, err
	}
	deadline, _ := tx.ctx.Deadline()
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
	if lock != nil && !time.Now().Before(lock.Info.ExpiresAt) {
		return false, ErrLocalBackdropLockExpired
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	committed = true
	return changed, nil
}

func checkLocalBackdropUnshadowed(ctx context.Context, tx pgx.Tx, itemID string) error {
	var shadowed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM artwork_state WHERE item_id=$1 AND $2=ANY(managed_types))
		OR EXISTS(SELECT 1 FROM item_provider_images WHERE item_id=$1 AND image_type=$2)`, itemID, localBackdropImageType).Scan(&shadowed); err != nil {
		return err
	}
	if shadowed {
		return ErrLocalBackdropShadowed
	}
	return nil
}

// Use the worker's Series types, and conservatively require coverage without
// relying on a provider ID or generated fallback. Managed absences count as
// deliberate; a stale local row on a different root cannot cover a gap.
func checkLocalBackdropOnlineCoverage(ctx context.Context, tx pgx.Tx, item localBackdropItem) error {
	var missing bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM unnest($3::text[]) wanted(image_type)
		WHERE NOT EXISTS(SELECT 1 FROM artwork_state a WHERE a.item_id=$1 AND wanted.image_type=ANY(a.managed_types))
		AND NOT EXISTS(SELECT 1 FROM item_images im WHERE im.item_id=$1 AND im.root_id=$2 AND im.image_type=wanted.image_type)
		AND NOT EXISTS(SELECT 1 FROM item_provider_images im WHERE im.item_id=$1 AND im.image_type=wanted.image_type))`,
		item.id, item.rootID, onlineImageTypes("Series")).Scan(&missing); err != nil {
		return err
	}
	if missing {
		return ErrLocalBackdropOnlineGap
	}
	return nil
}

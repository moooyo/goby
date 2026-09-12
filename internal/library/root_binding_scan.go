package library

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/storagebinding"
)

// rootBindingScanCapture retains the exact approval read before walking and
// independent named-storage witnesses. Verified is only an observation state;
// it does not establish complete walking or authorize missing-item deletion.
// The caller must Close every result, including non-verified observations.
type rootBindingScanCapture struct {
	row     rootBindingRow
	status  RootBindingStatus
	opened  *os.Root
	capture rootBindingWriteCapture
	closed  bool
}

// Revalidate never accesses Store.mu and can run inside an owned transaction.
// The original scan context must be supplied even when SQL uses a protected
// transaction context. Approval rows still require an independent final read.
func (capture *rootBindingScanCapture) Revalidate(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if capture == nil || capture.closed || capture.status != RootBindingVerified || capture.capture == nil || capture.opened == nil {
		return ErrRootTopologyUnavailable
	}
	if err := capture.capture.Revalidate(ctx); err != nil {
		return err
	}
	return ctx.Err()
}

func (capture *rootBindingScanCapture) Close() error {
	if capture == nil || capture.closed {
		return nil
	}
	capture.closed = true
	var err error
	if capture.opened != nil {
		err = capture.opened.Close()
		capture.opened = nil
	}
	if capture.capture != nil {
		err = errors.Join(err, capture.capture.Close())
		capture.capture = nil
	}
	return err
}

// CloneRegisteredRoot keeps the exact directory observed by the named capture.
// Reopening the relative name could pair a transient replacement scan root
// with the original capture after the pathname is restored again.
func (capture *rootBindingNamedCapture) CloneRegisteredRoot() (*os.Root, error) {
	return capture.registered.OpenRoot(".")
}

type rootBindingScanRootCloner interface {
	CloneRegisteredRoot() (*os.Root, error)
}

// prepareRootBindingScan observes the current named storage independently from
// any cached anchor. Only the complete persisted approval can recover an anchor;
// the scanner never learns replacement identities or writes binding/audit rows.
// A non-verified result with no error permits the caller's existing additions
// and updates to continue through its ordinary root-opening path. Database,
// mapping, ownership and task failures remain errors and must not be downgraded.
func (s *Store) prepareRootBindingScan(task *scanTask, root libraryRoot) (*rootBindingScanCapture, error) {
	return s.prepareRootBindingScanWithCapture(task, root, s.captureRootBindingWrite)
}

func (s *Store) prepareRootBindingScanWithCapture(task *scanTask, root libraryRoot, captureRoot rootBindingCaptureFactory) (*rootBindingScanCapture, error) {
	if task == nil || task.ctx == nil || captureRoot == nil || !validCatalogLibraryIdentifier(task.job.ID) ||
		!validCatalogLibraryIdentifier(task.job.LibraryID) || !validCatalogLibraryIdentifier(root.id) || root.libraryID != task.job.LibraryID {
		return nil, ErrInvalidInput
	}
	if s == nil {
		return nil, ErrUnavailable
	}
	previous, err := s.admitRootBindingScan(task, root, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	approved, err := previous.validate()
	if err != nil {
		return nil, err
	}
	result := &rootBindingScanCapture{row: previous, status: RootBindingUnbound}
	if approved == nil {
		return result, nil
	}
	approvedFingerprint, err := approved.Fingerprint()
	if err != nil {
		return nil, fmt.Errorf("%w: invalid scan root approval", ErrUnavailable)
	}

	// Every descriptor is acquired before the second owned transaction. A
	// caller-owned scan root and the Store candidate are separate from the
	// capture's original lease, registered root and topology descriptors.
	var anchor *os.Root
	retained := false
	defer func() {
		if anchor != nil {
			_ = anchor.Close()
		}
		if !retained {
			_ = result.Close()
		}
	}()
	result.status = RootBindingUnavailable
	result.capture, err = captureRoot(task.ctx, previous.root)
	if err == nil && result.capture != nil {
		var observed RootTopologySnapshot
		observed, err = result.capture.Snapshot()
		if err == nil {
			if observed.Mapping.ApprovedPath != previous.root.allowedPath || observed.Mapping.RegisteredPath != previous.root.path {
				return nil, ErrRootBindingConflict
			}
			var fingerprint string
			fingerprint, err = observed.Fingerprint()
			if err == nil {
				result.status = RootBindingMismatch
				if fingerprint == approvedFingerprint {
					result.status = RootBindingUnavailable
					anchor, err = result.capture.CloneApprovedAnchor()
					if err == nil && anchor != nil {
						if cloner, ok := result.capture.(rootBindingScanRootCloner); ok {
							result.opened, err = cloner.CloneRegisteredRoot()
							if err == nil && result.opened != nil {
								err = result.capture.Revalidate(task.ctx)
								if err == nil {
									result.status = RootBindingVerified
								}
							}
						}
					}
				}
			}
		}
	}
	if contextErr := task.ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	var matched rootBindingWriteCapture
	var candidate **os.Root
	if result.status == RootBindingVerified {
		matched, candidate = result.capture, &anchor
	}
	// Recheck the job and exact row even after an unavailable observation. Only
	// filesystem observation errors are recoverable; a lost owner or changed
	// mapping must not accidentally authorize ordinary scan work with stale data.
	_, err = s.admitRootBindingScan(task, root, &previous, matched, candidate)
	if err != nil {
		observationErr, observationOnly := rootBindingScanObservationOnly(err)
		if !observationOnly {
			return nil, err
		}
		if contextErr := task.ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		if errors.Is(observationErr, context.Canceled) || errors.Is(observationErr, context.DeadlineExceeded) {
			return nil, observationErr
		}
		result.status = RootBindingUnavailable
	}
	if result.status != RootBindingVerified {
		_ = result.Close()
		result.closed = false
	}
	retained = true
	return result, nil
}

// This wrapper marks only a failed filesystem revalidation. Owned transaction
// errors, including a rollback failure, are never converted into observations.
type rootBindingScanObservationFailure struct{ err error }

func (failure rootBindingScanObservationFailure) Error() string { return failure.err.Error() }

func (failure rootBindingScanObservationFailure) Unwrap() error { return failure.err }

// An owned callback joins rollback errors with its callback error. Only an
// error tree consisting entirely of marked observations may become a soft
// storage status; even an unrelated rollback error must remain fatal.
func rootBindingScanObservationOnly(err error) (error, bool) {
	if failure, ok := err.(rootBindingScanObservationFailure); ok {
		return failure.err, true
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return nil, false
	}
	var observation error
	for _, child := range joined.Unwrap() {
		if child == nil {
			continue
		}
		current, ok := rootBindingScanObservationOnly(child)
		if !ok {
			return nil, false
		}
		observation = errors.Join(observation, current)
	}
	return observation, observation != nil
}

// admitRootBindingScan retains admission through commit and anchor publication.
// All filesystem acquisition precedes this call, and revalidation never takes
// Store.mu. The lock order is Store.mu followed by ownership.mu throughout.
func (s *Store) admitRootBindingScan(task *scanTask, root libraryRoot, previous *rootBindingRow, capture rootBindingWriteCapture, anchor **os.Root) (rootBindingRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := task.ctx.Err(); err != nil {
		return rootBindingRow{}, err
	}
	if s.closed || !s.rootBindingPathConfiguredLocked(root.allowedPath) {
		return rootBindingRow{}, ErrUnavailable
	}
	if s.active[task.job.ID] != task || task.job.LibraryID != root.libraryID || task.job.Status != "Running" {
		return rootBindingRow{}, ErrTaskScanInactive
	}
	if task.job.CancelRequested {
		return rootBindingRow{}, context.Canceled
	}
	var current rootBindingRow
	err := s.taskScanTransaction(task.ctx, func(tx OwnedTx) error {
		if err := task.ctx.Err(); err != nil {
			return err
		}
		relation, err := lockTaskScanRelation(tx, task.job.ID, task.job.TaskChildID)
		if err != nil {
			return err
		}
		if relation.missing || relation.job.ID != task.job.ID || relation.job.LibraryID != root.libraryID ||
			relation.job.TaskChildID != task.job.TaskChildID || relation.job.Status != "Running" {
			return ErrTaskScanInactive
		}
		if relation.job.CancelRequested || relation.child != nil && (!activeTaskRun(relation.child.runState) || relation.child.state != "running") {
			return context.Canceled
		}
		current, err = readRootBindingScanRow(tx, root.libraryID, root.id)
		if err != nil {
			return err
		}
		if _, err := current.validate(); err != nil {
			return err
		}
		if current.root != root || previous != nil && !previous.same(current) {
			return ErrRootBindingConflict
		}
		if capture != nil {
			if err := capture.Revalidate(task.ctx); err != nil {
				return rootBindingScanObservationFailure{err: err}
			}
		}
		return task.ctx.Err()
	})
	if err != nil {
		return rootBindingRow{}, err
	}
	if err := task.ctx.Err(); err != nil {
		return rootBindingRow{}, err
	}
	if anchor != nil && *anchor != nil {
		s.installRootBindingAnchorLocked(current.root, *anchor)
		*anchor = nil
	}
	return current, nil
}

func readRootBindingScanRow(tx OwnedTx, libraryID, rootID string) (rootBindingRow, error) {
	var row rootBindingRow
	err := tx.QueryRow(`SELECT `+rootBindingMetadataColumns+`, r.storage_binding IS NOT NULL,
		CASE WHEN octet_length(r.storage_binding::text) <= $3 THEN r.storage_binding::text END,
		r.bound_at, CASE WHEN r.bound_by IS NULL THEN NULL WHEN octet_length(r.bound_by) <= 256 THEN r.bound_by ELSE '' END
		FROM library_roots r WHERE r.library_id = $1 AND r.id = $2 FOR UPDATE OF r`, libraryID, rootID, storagebinding.MaxDocumentBytes).
		Scan(&row.root.id, &row.root.libraryID, &row.root.path, &row.root.allowedPath, &row.root.relativePath,
			&row.revision, &row.stored, &row.document, &row.boundAt, &row.boundBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return rootBindingRow{}, ErrNotFound
	}
	if err != nil {
		return rootBindingRow{}, fmt.Errorf("read scan root binding: %w", err)
	}
	return row, nil
}

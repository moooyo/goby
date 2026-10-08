package library

import (
	"context"
	"errors"
	"os"
	"sync"
)

// This witness retains the original physical source root, not a later sample
// of its pathname. It supplies no permission or I/O admission of its own.
// Callers keep their operation owner and use its actual phases for Check/Open.
type scanSourceRootWitness struct {
	observation  storageObservationLifetime
	store        *Store
	row          rootBindingRow
	borrowed     *os.Root
	anchor       *os.Root
	root         *os.Root
	capture      *rootBindingScanCapture
	reference    *rootAnchorReference
	cleanup      context.Context
	ownedAnchor  bool
	ownedRoot    bool
	expectedRoot os.FileInfo
	origin       *storageObservationLifetime
}

// Preparation pins already retained descriptors using memory only. In the
// ordinary verified path both anchor and registered root already belong to the
// capture, so a potential later write does not add filesystem preparation.
func (state *scanState) prepareScanSourceRoot(ctx context.Context, row rootBindingRow) (_ *scanSourceRootWitness, resultErr error) {
	granted, err := state.readPrimaryScanAuthority(ctx)
	if err != nil {
		return nil, err
	}
	if !granted.same(row) {
		return nil, ErrRootBindingConflict
	}
	if state.sourceRoot != nil {
		if !state.sourceRoot.row.same(row) {
			return nil, ErrRootBindingConflict
		}
		return state.sourceRoot.borrow()
	}
	witness := &scanSourceRootWitness{store: state.store, row: row, borrowed: state.opened, cleanup: ctx}
	accepted := false
	defer func() {
		if !accepted {
			resultErr = errors.Join(resultErr, witness.Close())
		}
	}()
	if pass := state.reconciliationPass; pass != nil {
		if capture := pass.byRoot[row.root.id]; capture != nil && capture.status == RootBindingVerified && capture.capture != nil {
			if !capture.row.same(row) || capture.opened == nil || !capture.observation.retain() {
				return nil, ErrRootBindingConflict
			}
			witness.capture, witness.root = capture, capture.opened
			if named, ok := capture.capture.(*rootBindingNamedCapture); ok && named.lease != nil {
				witness.anchor = named.lease.approved
			}
		}
	}
	if witness.capture == nil {
		if witness.borrowed == nil {
			// Completed auxiliary roots have already retired the walk handle.
			// Keep the first directory facts instead of accepting a later root
			// opened from its pathname as a new baseline.
			if state.themes != nil {
				witness.expectedRoot = state.themes.directories["."]
			}
			if witness.expectedRoot == nil || !witness.expectedRoot.IsDir() {
				return nil, ErrRootBindingConflict
			}
		}
		// Selecting an already held descriptor is not current authorization.
		// The startup grant authorizes this root even if configuration later
		// retires the Store's reference; our pin keeps the original FD alive.
		state.store.mu.Lock()
		if bound, exists := state.store.rootBindingAnchors[row.root.id]; exists && bound.root == row.root {
			witness.anchor = bound.approved
		}
		if witness.anchor == nil {
			for _, configured := range state.store.roots {
				if configured.path == row.root.allowedPath {
					witness.anchor = configured.root
					break
				}
			}
		}
		if witness.anchor != nil {
			witness.reference = state.store.borrowRootAnchorLocked(witness.anchor)
		}
		state.store.mu.Unlock()
		if witness.anchor == nil {
			// An unchanged registered child cannot recover its original allowed
			// anchor. Production walks hand off that witness at their first open;
			// standalone callers must already retain a Store anchor or capture.
			return nil, ErrRootBindingConflict
		}
	}
	accepted = true
	return witness, nil
}

// Initial capture runs in an actual phase. Verified captures need no new FDs;
// legacy or standalone callers clone only the missing independent witnesses.
func (state *scanState) captureScanSourceRoot(ctx context.Context, row rootBindingRow) (_ *scanSourceRootWitness, resultErr error) {
	witness, err := state.prepareScanSourceRoot(ctx, row)
	if err != nil {
		return nil, err
	}
	if err := witness.initialize(ctx); err != nil {
		return nil, errors.Join(err, witness.Close())
	}
	return witness, nil
}

func (witness *scanSourceRootWitness) initialize(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	witness.cleanup = context.WithoutCancel(ctx)
	if witness.anchor == nil {
		if witness.capture == nil {
			return ErrRootBindingConflict
		}
		var err error
		witness.anchor, err = witness.capture.capture.CloneApprovedAnchor()
		if err != nil {
			return err
		}
		witness.ownedAnchor = true
	}
	if witness.root == nil {
		var err error
		if witness.borrowed != nil {
			witness.root, err = witness.borrowed.OpenRoot(".")
		} else {
			if err := checkTaskSourceAnchorName(witness.row.root.allowedPath, witness.anchor); err != nil {
				return errors.Join(ErrRootBindingConflict, err)
			}
			witness.root, err = openRegisteredRoot(witness.anchor, witness.row.root.relativePath)
		}
		if err != nil {
			return err
		}
		witness.ownedRoot = true
		if witness.expectedRoot != nil {
			current, err := witness.root.Stat(".")
			if err != nil || !sameMediaSourceFile(witness.expectedRoot, current) {
				return errors.Join(ErrRootBindingConflict, err)
			}
			if err := checkTaskSourceAnchorName(witness.row.root.allowedPath, witness.anchor); err != nil {
				return errors.Join(ErrRootBindingConflict, err)
			}
		}
	}
	return nil
}

// Auxiliary groups capture from the first already-proved input before retiring
// it. Borrowing or cloning its original descriptors never resamples a replaced
// pathname, and the group's existing operation owns the bounded root witnesses.
func (input *primaryScanRead) captureSourceRoot(ctx context.Context) (_ *scanSourceRootWitness, resultErr error) {
	if input == nil || input.namedAnchor == nil || input.namedRoot == nil {
		return nil, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	witness := &scanSourceRootWitness{row: input.row, cleanup: context.WithoutCancel(ctx)}
	if input.anchorSource != nil {
		var err error
		witness, err = input.anchorSource.borrow()
		if err != nil {
			return nil, err
		}
	}
	accepted := false
	defer func() {
		if !accepted {
			resultErr = errors.Join(resultErr, witness.Close())
		}
	}()
	if input.anchorSource == nil {
		var err error
		witness.anchor, err = input.namedAnchor.OpenRoot(".")
		if err != nil {
			return nil, err
		}
		witness.ownedAnchor = true
		witness.root, err = input.namedRoot.OpenRoot(".")
		if err != nil {
			return nil, err
		}
		witness.ownedRoot = true
	}
	if err := witness.Check(ctx); err != nil {
		return nil, err
	}
	accepted = true
	return witness, nil
}

// Retain supports an independent publication/worker lifetime without another
// filesystem open or governor owner. The original Close may precede release.
func (witness *scanSourceRootWitness) Retain() (func() error, error) {
	if witness == nil || !witness.observation.retain() {
		return nil, ErrUnavailable
	}
	var once sync.Once
	var releaseErr error
	return func() error {
		once.Do(func() { releaseErr = witness.observation.release() })
		return releaseErr
	}, nil
}

// Each caller retires its own handle. The first root-open witness remains
// reachable until the walk, auxiliary groups and any late workers all release
// their borrowed handles; borrowing opens no descriptor and creates no owner.
func (witness *scanSourceRootWitness) borrow() (*scanSourceRootWitness, error) {
	if witness == nil || !witness.observation.retain() {
		return nil, ErrUnavailable
	}
	return &scanSourceRootWitness{row: witness.row, anchor: witness.anchor, root: witness.root,
		cleanup: witness.cleanup, origin: &witness.observation}, nil
}

func (witness *scanSourceRootWitness) Open(ctx context.Context) (_ *os.Root, resultErr error) {
	if witness == nil || witness.anchor == nil || witness.root == nil {
		return nil, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checkTaskSourceAnchorName(witness.row.root.allowedPath, witness.anchor); err != nil {
		return nil, errors.Join(ErrRootBindingConflict, err)
	}
	root, err := openRegisteredRoot(witness.anchor, witness.row.root.relativePath)
	if err != nil {
		return nil, errors.Join(ErrRootBindingConflict, err)
	}
	accepted := false
	defer func() {
		if !accepted {
			resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, root))
		}
	}()
	if !sameMediaSourceDirectory(witness.root, root) {
		return nil, ErrRootBindingConflict
	}
	if err := checkTaskSourceAnchorName(witness.row.root.allowedPath, witness.anchor); err != nil {
		return nil, errors.Join(ErrRootBindingConflict, err)
	}
	accepted = true
	return root, nil
}

func (witness *scanSourceRootWitness) Check(ctx context.Context) error {
	root, err := witness.Open(ctx)
	if err != nil {
		return err
	}
	return closeDirectoryPrimaryRoot(ctx, root)
}

func (witness *scanSourceRootWitness) Close() error {
	if witness == nil {
		return nil
	}
	var err error
	if witness.origin != nil {
		err = witness.observation.retireReference(witness.origin)
	} else {
		err = witness.observation.retire(witness.closeResources)
	}
	if err != nil {
		return errors.Join(errSidecarRetirementUnknown, err)
	}
	return nil
}

func (witness *scanSourceRootWitness) closeResources() (resultErr error) {
	if witness.ownedRoot {
		resultErr = closeAuxiliaryRoot(witness.root)
	}
	if witness.ownedAnchor {
		resultErr = errors.Join(resultErr, closeAuxiliaryRoot(witness.anchor))
	}
	if witness.capture != nil {
		resultErr = errors.Join(resultErr, witness.capture.observation.release())
	}
	if witness.reference != nil {
		resultErr = errors.Join(resultErr, witness.store.releasePrimaryRootAnchor(witness.cleanup, witness.reference))
	}
	return resultErr
}

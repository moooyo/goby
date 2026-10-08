package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/primaryio"
)

// A source-backed folder retains only its named anchor, registered directory,
// and metadata-directory identity. It does not carry media probe authority or
// require complete topology or a second NFO content read at publication.
type scanFolderSource struct {
	observation storageObservationLifetime
	operation   *PrimaryRootIO
	sourceRoot  *scanSourceRootWitness
	row         rootBindingRow
	relative    string
	expected    os.FileInfo
	directory   *os.Root
}

// Permission comes only from the complete startup grant. Existing physical
// anchors are pinned before waiting, so replacing their names during admission
// cannot silently establish a new source for this directory observation.
func (state *scanState) prepareFolderSource(relative string) (_ *scanFolderSource, resultErr error) {
	row, err := state.readPrimaryScanAuthority(state.task.ctx)
	if err != nil {
		return nil, err
	}
	expected := state.directoryIdentities[filepath.Clean(relative)]
	if state.opened == nil || expected == nil || !expected.IsDir() || filepath.IsAbs(relative) || hasTraversal(relative) {
		return nil, ErrRootBindingConflict
	}
	source := &scanFolderSource{row: row, relative: relative, expected: expected}
	accepted := false
	defer func() {
		if !accepted {
			resultErr = errors.Join(resultErr, source.Close())
		}
	}()
	if state.walkIO != nil {
		if !state.walkRow.same(row) {
			return nil, ErrRootBindingConflict
		}
		source.operation, err = state.walkIO.Fork(state.task.ctx)
	} else {
		source.operation, err = state.store.prepareScanOperationRootIO(state.task.ctx, state.task,
			[]mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
	}
	if err != nil {
		return nil, err
	}
	source.sourceRoot, err = state.prepareScanSourceRoot(state.task.ctx, row)
	if err != nil {
		return nil, err
	}
	accepted = true
	return source, nil
}

func (source *scanFolderSource) initialize(ctx context.Context) error {
	if err := source.sourceRoot.initialize(ctx); err != nil {
		return err
	}
	if source.directory == nil {
		var err error
		source.directory, err = openRegisteredRoot(source.sourceRoot.root, source.relative)
		if err != nil {
			return err
		}
	}
	return source.validate(ctx)
}

func (source *scanFolderSource) readMetadata(ctx context.Context, state *scanState, work func(context.Context) error) error {
	return source.operation.Run(ctx, source.row.root.id, primaryio.Background, func(admitted context.Context) error {
		granted, err := state.readPrimaryScanAuthority(admitted)
		if err != nil {
			return err
		}
		if !source.row.same(granted) {
			return ErrRootBindingConflict
		}
		if err := source.initialize(admitted); err != nil {
			return err
		}
		if err := work(admitted); err != nil {
			return err
		}
		return source.validate(admitted)
	})
}

// The final worker uses only independently retained descriptors and immutable
// identity facts. No Store lock, SQL query or permission refresh occurs here.
func (source *scanFolderSource) validate(ctx context.Context) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := source.sourceRoot.Open(ctx)
	if err != nil {
		return errors.Join(ErrRootBindingConflict, err)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, root)) }()
	directory, err := openRegisteredRoot(root, source.relative)
	if err != nil {
		return errors.Join(ErrRootBindingConflict, err)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, directory)) }()
	current, err := directory.Stat(".")
	held, heldErr := source.directory.Stat(".")
	if err != nil || heldErr != nil || !current.IsDir() || !os.SameFile(source.expected, current) || !os.SameFile(source.expected, held) {
		return errors.Join(ErrRootBindingConflict, err, heldErr)
	}
	if err := checkTaskSourceAnchorName(source.row.root.allowedPath, source.sourceRoot.anchor); err != nil {
		return errors.Join(ErrRootBindingConflict, err)
	}
	return ctx.Err()
}

func (source *scanFolderSource) final(ctx context.Context, tx pgx.Tx) error {
	owned, ok := tx.(*ownedTx)
	if !ok || owned.ctx == nil {
		return ErrUnavailable
	}
	deadline, ok := owned.ctx.Deadline()
	if !ok {
		return ErrUnavailable
	}
	proof, cancel := context.WithDeadline(ctx, deadline.Add(-scanReconciliationRollbackSpace))
	defer cancel()
	return source.operation.RunImmediate(proof, source.row.root.id, primaryio.Background, func(work context.Context) error {
		return runStorageObservation(work, []*storageObservationLifetime{&source.observation}, source.validate)
	})
}

func (source *scanFolderSource) Close() error {
	if source == nil {
		return nil
	}
	if err := source.observation.retire(source.closeResources); err != nil {
		return errors.Join(errSidecarRetirementUnknown, err)
	}
	return nil
}

func (source *scanFolderSource) closeResources() (resultErr error) {
	resultErr = errors.Join(closeAuxiliaryRoot(source.directory), source.sourceRoot.Close())
	if source.operation != nil {
		resultErr = errors.Join(resultErr, source.operation.Close())
	}
	return resultErr
}

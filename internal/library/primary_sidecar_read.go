package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

var (
	errSidecarRetirementUnknown = errors.New("sidecar descriptor retirement is unknown")
	errSidecarRollbackUnknown   = errors.New("sidecar transaction rollback is unknown")
)

// Failed native retirement keeps the exact resource strongly reachable. The
// existing retained-owner and per-source descriptor bounds still apply.
var retainedSidecarResources = struct {
	sync.Mutex
	resources map[*PrimaryRootIO][]io.Closer
}{resources: make(map[*PrimaryRootIO][]io.Closer)}

func closePrimarySidecarResource(ctx context.Context, resource io.Closer) error {
	if resource == nil {
		return nil
	}
	err := resource.Close()
	if err == nil || errors.Is(err, os.ErrClosed) {
		return nil
	}
	operation := PrimaryRootIOFromContext(ctx)
	if operation != nil {
		retainedSidecarResources.Lock()
		retainedSidecarResources.resources[operation] = append(retainedSidecarResources.resources[operation], resource)
		retainedSidecarResources.Unlock()
		return operation.MarkUnknown(errors.Join(errSidecarRetirementUnknown, err))
	}
	return fmt.Errorf("%w: native sidecar retirement cannot be established: %w", ErrUnavailable,
		errors.Join(errSidecarRetirementUnknown, err))
}

// Wrapping capacity errors in the existing unavailable API category does not
// make actual admission fatal. Retirement uncertainty and source changes must
// never be hidden by a joined Busy error or converted into a retry.
func sidecarAdmissionRetryable(err error) bool {
	return errors.Is(err, ErrBusy) && !errors.Is(err, errSidecarRetirementUnknown) &&
		!errors.Is(err, errSidecarRollbackUnknown) && !errors.Is(err, media.ErrProcessRetirementUnknown) && !errors.Is(err, ErrSourceChanged) &&
		!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) &&
		onlySidecarCapacityErrors(err)
}

func onlySidecarCapacityErrors(err error) bool {
	if err == nil {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if !onlySidecarCapacityErrors(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlySidecarCapacityErrors(wrapped.Unwrap())
	}
	// These are admission sentinels and the existing API classification label.
	// A joined filesystem/observation/SQL failure is never merely backpressure.
	return err == ErrBusy || err == ErrUnavailable || err == primaryio.ErrBusy || err == primaryio.ErrOwnerBusy
}

func waitSidecarRetryBackoff(ctx context.Context) error {
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// This wait owns no transaction, payload or descriptor. Each grant is followed
// by fresh authority, then fully retired before the caller restarts its entire
// source observation. A grant never becomes a reusable publication proof.
func (s *Store) waitSidecarAdmission(ctx context.Context, hint mediaSourceRootHint, class primaryio.Class, fresh func(context.Context) error) error {
	if ctx == nil || fresh == nil {
		return ErrInvalidInput
	}
	for {
		operation, err := s.preparePrimaryRootIO(ctx, []mediaSourceRootHint{hint})
		if err == nil {
			err = operation.Run(ctx, hint.root.id, class, fresh)
			err = errors.Join(err, operation.Close())
		}
		if !sidecarAdmissionRetryable(err) {
			return err
		}
		// Register/queue bounds can reject before a waiter exists. Keep the
		// established caps and retry only this descriptor-free preparation.
		if err := waitSidecarRetryBackoff(ctx); err != nil {
			return err
		}
	}
}

func (state *scanState) waitSidecarScanAdmission() error {
	row, err := state.readPrimaryScanAuthority(state.task.ctx)
	if err != nil {
		return err
	}
	return state.store.waitSidecarAdmission(state.task.ctx,
		mediaSourceRootHint{root: row.root, bindingRevision: row.revision}, primaryio.Background,
		func(work context.Context) error { return state.checkSidecarScanAuthority(work, row) })
}

func ordinarySidecarSourceChange(err error) bool {
	return errors.Is(err, ErrSourceChanged) && !errors.Is(err, errSidecarRetirementUnknown) &&
		!errors.Is(err, errSidecarRollbackUnknown) && !errors.Is(err, media.ErrProcessRetirementUnknown) && !errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded)
}

func rollbackSidecarTransaction(tx pgx.Tx, previous error) error {
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := tx.Rollback(cleanup)
	if err == nil || errors.Is(err, pgx.ErrTxClosed) && (previous == nil || errors.Is(previous, pgx.ErrTxCommitRollback)) {
		return nil
	}
	return errors.Join(ErrUnavailable, errSidecarRollbackUnknown, err)
}

// Each attempt returns only after transaction rollback and exact descriptor
// cleanup. Busy restarts every authority/payload/source proof outside SQL;
// ordinary instability retains the previous sidecar catalog with a warning.
func (state *scanState) retrySidecarScan(attempt func() error) error {
	for {
		warnings := state.warnings
		err := attempt()
		if !sidecarAdmissionRetryable(err) {
			if ordinarySidecarSourceChange(err) {
				state.warnings++
				return nil
			}
			return err
		}
		state.warnings = warnings
		if err := state.waitSidecarScanAdmission(); err != nil {
			return scanReadFailure(err)
		}
	}
}

// readSidecarRootHint classifies only the root selected by a committed catalog
// authorization snapshot. It neither probes storage nor grants item authority.
func (s *Store) readSidecarRootHint(ctx context.Context, expected libraryRoot) (mediaSourceRootHint, error) {
	if s == nil || s.pool == nil {
		return mediaSourceRootHint{}, ErrUnavailable
	}
	var hint mediaSourceRootHint
	err := s.pool.QueryRow(ctx, `SELECT `+rootBindingMetadataColumns+`
		FROM library_roots r WHERE r.id=$1 AND r.library_id=$2`, expected.id, expected.libraryID).
		Scan(&hint.root.id, &hint.root.libraryID, &hint.root.path, &hint.root.allowedPath,
			&hint.root.relativePath, &hint.bindingRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return mediaSourceRootHint{}, ErrNotFound
	}
	if err != nil {
		return mediaSourceRootHint{}, fmt.Errorf("%w: read sidecar root routing: %w", ErrUnavailable, err)
	}
	if hint.root != expected || (rootBindingRow{root: hint.root, revision: hint.bindingRevision}).validateMapping() != nil {
		return mediaSourceRootHint{}, fmt.Errorf("%w: %w: sidecar root mapping changed", ErrUnavailable, ErrSourceChanged)
	}
	return hint, nil
}

func (s *Store) prepareSidecarRootIO(ctx context.Context, root libraryRoot) (*PrimaryRootIO, mediaSourceRootHint, error) {
	hint, err := s.readSidecarRootHint(ctx, root)
	if err != nil {
		return nil, mediaSourceRootHint{}, err
	}
	operation, err := s.preparePrimaryRootIO(ctx, []mediaSourceRootHint{hint})
	return operation, hint, err
}

func (s *Store) checkSidecarRootHint(ctx context.Context, expected mediaSourceRootHint) error {
	current, err := s.readSidecarRootHint(ctx, expected.root)
	if err != nil {
		return err
	}
	if current != expected {
		return fmt.Errorf("%w: %w: sidecar root binding changed while queued", ErrUnavailable, ErrSourceChanged)
	}
	return ctx.Err()
}

// Scan preparation preserves the active task relation and exact committed root
// revision. Storage admission never retains the authority transaction.
func (state *scanState) prepareSidecarScanIO() (*PrimaryRootIO, rootBindingRow, error) {
	row, err := state.readPrimaryScanAuthority(state.task.ctx)
	if err != nil {
		return nil, rootBindingRow{}, err
	}
	operation, err := state.store.preparePrimaryRootIO(state.task.ctx,
		[]mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
	return operation, row, err
}

func (state *scanState) checkSidecarScanAuthority(ctx context.Context, expected rootBindingRow) error {
	current, err := state.readPrimaryScanAuthority(ctx)
	if err != nil {
		return err
	}
	if !current.same(expected) {
		return ErrRootBindingConflict
	}
	return ctx.Err()
}

func (state *scanState) checkSidecarScanRootTx(tx pgx.Tx, expected rootBindingRow) error {
	current, err := readRootBindingForUpdate(state.task.ctx, tx, expected.root.libraryID, expected.root.id)
	if err != nil {
		return err
	}
	if !current.same(expected) {
		return ErrRootBindingConflict
	}
	return nil
}

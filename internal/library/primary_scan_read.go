package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

// JoinedProbeCallbacks is an explicit synchronous input borrowing contract.
// Implementations join every direct read and use the actual media gateway for
// all children; they cannot leave an external reader alive after ProbeFile.
// This declaration is not an operating-system or native-domain certificate.
type JoinedProbeCallbacks interface{ ProbeFileJoinedContract() bool }

type primaryScanRead struct {
	state              *scanState
	work               context.Context
	finish             func()
	owner              *primaryio.Owner
	route              primaryio.Route
	claim              *originalMediaReadDomainClaim
	row                rootBindingRow
	file               *os.File
	stamp              os.FileInfo
	path               string
	namedRoot          *os.Root
	namedAnchor        *os.Root
	anchorCapture      *rootBindingScanCapture
	anchorSource       *scanSourceRootWitness
	ownedAnchor        bool
	ownedRoot          bool
	phase              *primaryio.PrimaryReadLease
	receipt            *media.ProbeRetirementReceipt
	publicationIO      *PrimaryRootIO
	publicationWorkers sync.WaitGroup
	missingReceipt     bool
	mu                 sync.Mutex
	closeOnce          sync.Once
	closeErr           error
	closed             chan struct{}
}

type primaryScanReadFailure struct{ err error }

var errScanPublicationRetirementUnknown = errors.New("scan publication operation retirement is unknown")

func (failure *primaryScanReadFailure) Error() string { return failure.err.Error() }
func (failure *primaryScanReadFailure) Unwrap() error { return failure.err }

func scanReadFailure(err error) error {
	if err == nil {
		return nil
	}
	var failure *primaryScanReadFailure
	if errors.As(err, &failure) {
		return err
	}
	return &primaryScanReadFailure{err: err}
}

func (state *scanState) recordPrimaryScanReadFailure(err error) {
	if err == nil {
		return
	}
	if state.primaryReadParent != nil {
		state.primaryReadParent.recordPrimaryScanReadFailure(err)
		return
	}
	state.primaryReadMu.Lock()
	state.primaryReadErr = errors.Join(state.primaryReadErr, scanReadFailure(err))
	state.primaryReadMu.Unlock()
}

func (state *scanState) primaryScanReadError() error {
	if state.primaryReadParent != nil {
		return state.primaryReadParent.primaryScanReadError()
	}
	state.primaryReadMu.Lock()
	defer state.primaryReadMu.Unlock()
	return state.primaryReadErr
}

// The first scan entry commits the operation's complete authorization before
// source delivery. Later phases reuse it with live cancellation/owner checks.
func (state *scanState) readPrimaryScanAuthority(ctx context.Context) (rootBindingRow, error) {
	if ctx == nil || state == nil || state.store == nil || state.task == nil {
		return rootBindingRow{}, ErrUnavailable
	}
	return state.store.readScanOperationAuthority(ctx, state.task, state.root)
}

// Routing and reads use the same immutable operation grant. A retained walk
// mapping must agree with it and cannot authorize another root or task.
func (state *scanState) primaryScanRoutingRow(ctx context.Context) (rootBindingRow, error) {
	if ctx == nil || state == nil || state.store == nil || state.task == nil {
		return rootBindingRow{}, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return rootBindingRow{}, err
	}
	row, err := state.readPrimaryScanAuthority(ctx)
	if err != nil {
		return rootBindingRow{}, err
	}
	if state.walkIO != nil && !state.walkRow.same(row) {
		return rootBindingRow{}, ErrRootBindingConflict
	}
	return row, nil
}

func (state *scanState) runPrimaryScanMetadata(ctx context.Context, work func(context.Context) error) (resultErr error) {
	row, err := state.primaryScanRoutingRow(ctx)
	if err != nil {
		return err
	}
	return state.runPrimaryScanMetadataWithRouting(ctx, row, work)
}

// The caller supplies this operation's committed mapping. Admission still
// checks live cancellation and ownership before any metadata read starts.
func (state *scanState) runPrimaryScanMetadataWithRouting(ctx context.Context, row rootBindingRow, work func(context.Context) error) (resultErr error) {
	if ctx == nil || state == nil || state.store == nil || state.task == nil || work == nil {
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if row.root != state.root || row.validateMapping() != nil {
		return ErrRootBindingConflict
	}
	if state.walkRow.root.id != "" && !state.walkRow.same(row) {
		return ErrRootBindingConflict
	}
	operation := state.walkIO
	if operation == nil {
		var err error
		operation, err = state.store.prepareScanOperationRootIO(ctx, state.task,
			[]mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, operation.Close()) }()
	}
	return operation.Run(ctx, row.root.id, primaryio.Background, func(workContext context.Context) error {
		fresh, err := state.readPrimaryScanAuthority(workContext)
		if err != nil || !row.same(fresh) {
			return errors.Join(err, ErrRootBindingConflict)
		}
		return work(workContext)
	})
}

func (input *primaryScanRead) preparePublicationIO() (*PrimaryRootIO, error) {
	if input == nil || input.state == nil || input.row.root != input.state.root || input.row.validateMapping() != nil {
		return nil, ErrRootBindingConflict
	}
	state := input.state
	if state.walkIO != nil {
		if !state.walkRow.same(input.row) {
			return nil, ErrRootBindingConflict
		}
		return state.walkIO.Fork(input.work)
	}
	if pass := state.reconciliationPass; pass != nil {
		if capture := pass.byRoot[input.row.root.id]; capture != nil && capture.primaryIO != nil {
			if !capture.row.same(input.row) {
				return nil, ErrRootBindingConflict
			}
			return capture.primaryIO.Fork(input.work)
		}
	}
	return state.store.prepareScanOperationRootIO(input.work, state.task,
		[]mediaSourceRootHint{{root: input.row.root, bindingRevision: input.row.revision}})
}

func (input *primaryScanRead) runPublicationMetadata(operation *PrimaryRootIO, work func(context.Context) error) error {
	if operation == nil || work == nil {
		return scanReadFailure(ErrUnavailable)
	}
	return operation.Run(input.work, input.row.root.id, primaryio.Background, func(ctx context.Context) error {
		row, err := input.state.readPrimaryScanAuthority(ctx)
		if err != nil {
			return scanReadFailure(err)
		}
		if !input.row.same(row) {
			return scanReadFailure(ErrRootBindingConflict)
		}
		if err := input.checkPhysicalSource(ctx); err != nil {
			return err
		}
		if err := work(ctx); err != nil {
			return err
		}
		return input.checkPhysicalSource(ctx)
	})
}

func (input *primaryScanRead) revalidatePublicationSource(tx pgx.Tx) error {
	owned, ok := tx.(*ownedTx)
	if !ok || owned.ctx == nil || input.publicationIO == nil {
		return scanReadFailure(ErrUnavailable)
	}
	deadline, ok := owned.ctx.Deadline()
	if !ok {
		return scanReadFailure(ErrUnavailable)
	}
	ctx, cancel := context.WithDeadline(input.work, deadline.Add(-scanReconciliationRollbackSpace))
	defer cancel()
	return input.publicationIO.RunImmediate(ctx, input.row.root.id, primaryio.Background, func(work context.Context) error {
		return input.observePublicationSource(work, input.checkPhysicalSource)
	})
}

func (input *primaryScanRead) observePublicationSource(ctx context.Context, observe func(context.Context) error) error {
	// Retain before the observation worker starts, not from inside its callback.
	// Close can therefore join the exact worker even if its caller times out
	// before the first source syscall. The phase keeps its existing reference.
	retained := withStorageObservationPhaseRetention(ctx, func() (func() error, error) {
		release, err := retainStorageObservationPhase(ctx)
		if err != nil {
			return nil, err
		}
		input.publicationWorkers.Add(1)
		return func() error {
			defer input.publicationWorkers.Done()
			return release()
		}, nil
	})
	return runStorageObservation(retained, nil, observe)
}

func closeScanPublicationIO(operation *PrimaryRootIO) error {
	if operation == nil {
		return nil
	}
	if err := operation.Close(); err != nil {
		return scanReadFailure(errors.Join(errScanPublicationRetirementUnknown, err))
	}
	return nil
}

func (state *scanState) preparePrimaryScanRead() (_ *primaryScanRead, resultErr error) {
	if state == nil || state.store == nil || state.task == nil {
		return nil, ErrUnavailable
	}
	work, finish, err := state.store.beginMediaSourceLifetime(state.task.ctx)
	if err != nil {
		return nil, err
	}
	owner, err := originalMediaReadOwners.Register(work)
	if err != nil {
		finish()
		if errors.Is(err, primaryio.ErrBusy) {
			return nil, scanReadFailure(fmt.Errorf("%w: retain scan input: %w", ErrBusy, err))
		}
		return nil, fmt.Errorf("%w: retain scan input: %w", ErrUnavailable, err)
	}
	input := &primaryScanRead{state: state, work: work, finish: finish, owner: owner, namedRoot: state.opened, closed: make(chan struct{})}
	accepted := false
	defer func() {
		if !accepted {
			resultErr = errors.Join(resultErr, input.retireRegistration())
			resultErr = scanReadFailure(resultErr)
		}
	}()
	row, err := state.primaryScanRoutingRow(work)
	if err != nil {
		return nil, err
	}
	input.row = row
	prepared, err := state.task.authority.Load().copyRoute(row.root.id)
	if err != nil {
		return nil, err
	}
	claim, err := originalMediaReadDomains.acquire(prepared.domain)
	if err != nil {
		return nil, err
	}
	input.route, input.claim = prepared.route, claim
	accepted = true
	return input, nil
}

func (input *primaryScanRead) readAuthority() (rootBindingRow, error) {
	if input == nil || input.state == nil {
		return rootBindingRow{}, ErrUnavailable
	}
	state := input.state
	return state.store.readScanOperationAuthority(input.work, state.task, state.root)
}

func (input *primaryScanRead) attach(file *os.File, path string, stamp os.FileInfo) error {
	input.file, input.path = file, path
	if file == nil || input.namedRoot == nil {
		return scanReadFailure(ErrUnavailable)
	}
	if stamp == nil || !stamp.Mode().IsRegular() {
		return scanReadFailure(errScanProbeSourceChanged)
	}
	// Preserve the first observation used by the walker and cache decision.
	// checkSource still observes the live descriptor and named path.
	input.stamp = stamp
	if input.namedAnchor == nil {
		if source := input.state.sourceRoot; source != nil {
			if !source.row.same(input.row) {
				return scanReadFailure(ErrRootBindingConflict)
			}
			var err error
			input.anchorSource, err = source.borrow()
			if err != nil {
				return scanReadFailure(err)
			}
			input.namedAnchor = input.anchorSource.anchor
		} else if pass := input.state.reconciliationPass; pass != nil {
			if capture := pass.byRoot[input.row.root.id]; capture != nil && capture.status == RootBindingVerified && capture.capture != nil {
				// The normal verified path already owns its approved anchor.
				// Pin that capture until this input and any late proof have joined,
				// instead of opening another anchor descriptor for every file.
				if !capture.observation.retain() {
					return scanReadFailure(ErrUnavailable)
				}
				input.anchorCapture = capture
				if named, ok := capture.capture.(*rootBindingNamedCapture); ok && named.lease != nil {
					input.namedAnchor = named.lease.approved
				} else {
					var err error
					input.namedAnchor, err = capture.capture.CloneApprovedAnchor()
					if err != nil {
						return scanReadFailure(err)
					}
					input.ownedAnchor = true
				}
				if input.namedAnchor == nil {
					return scanReadFailure(ErrUnavailable)
				}
			}
		}
		if input.namedAnchor == nil {
			lease, err := input.state.store.leaseScanOperationRoot(input.work, input.state.task, input.row.root)
			if err != nil {
				return scanReadFailure(err)
			}
			input.namedAnchor = lease.approved
			input.ownedAnchor = true
		}
	}
	return input.checkSource()
}

func (input *primaryScanRead) checkPhysicalSource(ctx context.Context) (resultErr error) {
	if input.namedAnchor == nil || input.namedRoot == nil || input.file == nil || input.stamp == nil {
		return scanReadFailure(ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkTaskSourceAnchorName(input.row.root.allowedPath, input.namedAnchor); err != nil {
		return errors.Join(ErrRootBindingConflict, err)
	}
	current, err := openRegisteredRoot(input.namedAnchor, input.row.root.relativePath)
	if err != nil {
		return errors.Join(ErrRootBindingConflict, err)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, current)) }()
	if !sameMediaSourceDirectory(input.namedRoot, current) {
		return ErrRootBindingConflict
	}
	if err := checkScanProbeFileAt(ctx, current, input.file, input.stamp, input.path); err != nil {
		return err
	}
	if err := checkTaskSourceAnchorName(input.row.root.allowedPath, input.namedAnchor); err != nil {
		return errors.Join(ErrRootBindingConflict, err)
	}
	return ctx.Err()
}

func (input *primaryScanRead) checkSource() (resultErr error) {
	if input.file == nil || input.stamp == nil || input.namedRoot == nil {
		return scanReadFailure(ErrUnavailable)
	}
	work := input.owner.Context()
	if err := work.Err(); err != nil {
		return scanReadFailure(err)
	}
	if capture := input.state.reconciliationPass; capture != nil {
		if retained := capture.byRoot[input.row.root.id]; retained != nil && retained.status == RootBindingVerified {
			if !retained.row.same(input.row) {
				return scanReadFailure(ErrRootBindingConflict)
			}
		}
	}
	err := input.checkPhysicalSource(work)
	if scanProbeSourceChangedOnly(err) {
		input.mu.Lock()
		receipt, phase, missing := input.receipt, input.phase, input.missingReceipt
		input.mu.Unlock()
		// Stale facts are recoverable only after a known-retired probe or before
		// invoking a backend whose acquired phase the caller still must retire.
		// Root, cancellation and cleanup failures remain ownership failures.
		if receipt != nil && receipt.RetirementComplete() && !receipt.UnknownObserved() ||
			receipt == nil && phase != nil && !missing {
			return errScanProbeSourceChanged
		}
	}
	return scanReadFailure(err)
}

func (input *primaryScanRead) probe(prober Prober, file *os.File) (media.Info, error) {
	return input.probeContext(input.work, prober, file)
}

func (input *primaryScanRead) probeContext(ctx context.Context, prober Prober, file *os.File) (_ media.Info, resultErr error) {
	if ctx == nil || file == nil || file != input.file {
		return media.Info{}, scanReadFailure(ErrUnavailable)
	}
	stop := context.AfterFunc(ctx, func() { _ = input.owner.Cancel() })
	defer stop()
	phase, err := input.owner.Acquire(input.route, primaryio.Background)
	if err != nil {
		if errors.Is(err, primaryio.ErrBusy) {
			return media.Info{}, scanReadFailure(fmt.Errorf("%w: admit scan primary read: %w", ErrBusy, err))
		}
		return media.Info{}, scanReadFailure(fmt.Errorf("%w: admit scan primary read: %w", ErrUnavailable, err))
	}
	input.mu.Lock()
	input.phase = phase
	input.mu.Unlock()
	called := false
	defer func() {
		if !called {
			releaseErr := phase.Release()
			input.mu.Lock()
			input.phase = nil
			input.mu.Unlock()
			if releaseErr != nil || resultErr != errScanProbeSourceChanged {
				resultErr = scanReadFailure(errors.Join(resultErr, releaseErr))
			}
		}
	}()
	if err := ctx.Err(); err != nil {
		return media.Info{}, err
	}
	if err := input.owner.Context().Err(); err != nil {
		return media.Info{}, err
	}
	row, err := input.readAuthority()
	if err != nil || !row.same(input.row) {
		if err == nil {
			err = ErrRootBindingConflict
		}
		return media.Info{}, err
	}
	if err := ctx.Err(); err != nil {
		return media.Info{}, err
	}
	if err := input.owner.Context().Err(); err != nil {
		return media.Info{}, err
	}
	if err := input.checkSource(); err != nil {
		return media.Info{}, err
	}
	if err := ctx.Err(); err != nil {
		return media.Info{}, err
	}
	if err := input.owner.Context().Err(); err != nil {
		return media.Info{}, err
	}
	var info media.Info
	var receipt *media.ProbeRetirementReceipt
	work, cancelProbe := context.WithCancel(media.WithBackgroundProcess(ctx))
	stopOwner := context.AfterFunc(input.owner.Context(), cancelProbe)
	defer func() { stopOwner(); cancelProbe() }()
	if err := input.owner.Context().Err(); err != nil {
		return media.Info{}, err
	}
	if !media.ProbeFileOwnershipAvailable(work) {
		return media.Info{}, ErrUnavailable
	}
	// An explicit callback declaration wins over an embedded Owned method, so
	// a custom ProbeFile barrier/override is never bypassed by method promotion.
	if joined, ok := prober.(JoinedProbeCallbacks); ok {
		if !joined.ProbeFileJoinedContract() {
			return media.Info{}, ErrUnavailable
		}
		called = true
		input.mu.Lock()
		input.missingReceipt = true
		input.mu.Unlock()
		info, receipt, err = media.ProbeFileJoinedCallbacks(work, file, prober.ProbeFile)
	} else if owned, ok := prober.(interface {
		ProbeFileOwned(context.Context, *os.File) (media.Info, *media.ProbeRetirementReceipt, error)
	}); ok {
		called = true
		input.mu.Lock()
		input.missingReceipt = true
		input.mu.Unlock()
		info, receipt, err = owned.ProbeFileOwned(work, file)
	} else {
		err = fmt.Errorf("%w: probe requires explicit joined input ownership", ErrUnavailable)
	}
	input.mu.Lock()
	input.receipt = receipt
	input.missingReceipt = called && receipt == nil
	input.mu.Unlock()
	if receipt == nil {
		// Only a branch that never invoked a backend can retire without a
		// receipt. A backend violating its opaque contract stays quarantined.
		if called {
			input.mu.Lock()
			input.missingReceipt = true
			input.mu.Unlock()
			err = errors.Join(err, media.ErrProcessRetirementUnknown)
		}
		if called {
			return media.Info{}, scanReadFailure(err)
		}
		return media.Info{}, err
	}
	complete := receipt.RetirementComplete()
	var sourceErr error
	if complete && !receipt.UnknownObserved() {
		sourceErr = input.checkSource()
	}
	if complete {
		if releaseErr := phase.Release(); releaseErr != nil {
			return media.Info{}, scanReadFailure(errors.Join(err, sourceErr, releaseErr))
		}
		input.mu.Lock()
		input.phase = nil
		input.mu.Unlock()
	}
	if receipt.UnknownObserved() || !complete {
		return media.Info{}, scanReadFailure(errors.Join(err, media.ErrProcessRetirementUnknown))
	}
	if sourceErr != nil {
		return media.Info{}, errors.Join(err, sourceErr)
	}
	return info, err
}

func (input *primaryScanRead) retireRegistration() error {
	if err := input.owner.Complete(); err != nil {
		return fmt.Errorf("%w: retire scan registration: %w", ErrUnavailable, err)
	}
	if input.claim != nil {
		input.claim.release()
	}
	input.finish()
	return nil
}

func (input *primaryScanRead) close(closeFile func() error) error {
	input.closeOnce.Do(func() {
		input.mu.Lock()
		receipt, phase, missing := input.receipt, input.phase, input.missingReceipt
		input.mu.Unlock()
		for missing {
			time.Sleep(time.Second)
		}
		if receipt != nil {
			// The exact child cleanup owner remains alive across caller deadlines.
			// Do not return to the walker while it still owns borrowed root data.
			for !receipt.RetirementComplete() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				_ = receipt.Close(ctx)
				cancel()
				if !receipt.RetirementComplete() {
					time.Sleep(time.Millisecond)
				}
			}
			if receipt.UnknownObserved() {
				input.closeErr = errors.Join(input.closeErr, media.ErrProcessRetirementUnknown)
			}
		}
		if phase != nil {
			if err := phase.Release(); err != nil {
				input.closeErr = errors.Join(input.closeErr, err)
				return
			}
			input.mu.Lock()
			input.phase = nil
			input.mu.Unlock()
		}
		input.publicationWorkers.Wait()
		if closeFile != nil {
			if err := closeFile(); err != nil && !errors.Is(err, os.ErrClosed) {
				input.closeErr = errors.Join(input.closeErr, err)
				// Keep the actual named root alive until the exact descriptor is
				// demonstrably closed, even when a cleanup callback reports a fault.
				for input.file != nil {
					if _, statErr := input.file.Stat(); errors.Is(statErr, os.ErrClosed) {
						break
					}
					_ = input.file.Close()
					time.Sleep(time.Millisecond)
				}
			}
		}
		if input.ownedRoot && input.namedRoot != nil {
			if err := input.namedRoot.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				input.closeErr = errors.Join(input.closeErr, err)
			}
			for {
				if _, err := input.namedRoot.Stat("."); errors.Is(err, os.ErrClosed) {
					break
				}
				_ = input.namedRoot.Close()
				time.Sleep(time.Millisecond)
			}
		}
		if input.ownedAnchor && input.namedAnchor != nil {
			input.closeErr = errors.Join(input.closeErr, closeAuxiliaryRoot(input.namedAnchor))
		}
		if input.anchorCapture != nil {
			input.closeErr = errors.Join(input.closeErr, input.anchorCapture.observation.release())
		}
		input.closeErr = errors.Join(input.closeErr, input.anchorSource.Close())
		input.closeErr = errors.Join(input.closeErr, input.retireRegistration())
		close(input.closed)
	})
	return scanReadFailure(input.closeErr)
}

package library

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

// Source-open workers have their own finite descriptor and Store lifetimes.
// Their FS observations share the actual-I/O governor without consuming one
// of the 64 retained body-reader registrations.
var sourceMetadataReadOwners = func() *primaryio.OwnerRuntime {
	owners, err := primaryio.NewOwnerRuntime(originalMediaReadGovernor, mediaSourceOwnerLimit)
	if err != nil {
		panic("invalid source metadata owner defaults")
	}
	return owners
}()

type sourceMetadataReservationKey struct{}

type sourceMetadataReservation struct {
	mu     sync.Mutex
	work   context.Context
	finish func()
	routes map[string]primaryRootIORoute
	refs   int
	once   sync.Once
	worker sync.Once
}

type retainedSourceMetadataWarmCleanup struct {
	warm        *warmMediaSourceRoot
	reservation *sourceMetadataReservation
	operation   *PrimaryRootIO
}

// Only a failed admitted cleanup can enter this registry. Its four metadata
// owners bound the registry and keep exact resources and reservation pins live.
var sourceMetadataWarmUnknown = struct {
	sync.Mutex
	owners map[*PrimaryRootIO]retainedSourceMetadataWarmCleanup
}{owners: make(map[*PrimaryRootIO]retainedSourceMetadataWarmCleanup)}

// This restrictive finisher may only close its already-held warm anchor. It
// cannot reopen a pathname or classify after Store shutdown. Normal metadata
// owners remain Store-cancelled; mandatory cleanup retains a separate context.
func closeSourceMetadataWarm(ctx context.Context, reservation *sourceMetadataReservation, warm *warmMediaSourceRoot) error {
	if warm == nil || warm.store == nil {
		return nil
	}
	warm.store.mu.Lock()
	released := warm.released
	warm.store.mu.Unlock()
	if released {
		return nil
	}
	if reservation == nil || reservation.preparedRoutes() == nil {
		return ErrUnavailable
	}
	for {
		if !reservation.retain() {
			return ErrUnavailable
		}
		operation, err := newPrimaryRootIOWithClaimAcquire(context.WithoutCancel(reservation.work), sourceMetadataReadOwners,
			&originalMediaReadDomains, reservation.preparedRoutes(), reservation.release, originalMediaReadDomains.acquireShared)
		if err != nil {
			// The existing actual source worker still owns this pin and its
			// finite handoff/cleanup slot. A capacity or canonical-domain
			// rejection cannot prove that its held resource was closed. Keep
			// the exact pin and immutable route while retrying only memory
			// admission; caller cancellation cannot drop this cleanup owner.
			time.Sleep(10 * time.Millisecond)
			continue
		}
		err = operation.Run(context.WithoutCancel(ctx), warm.root.id, primaryio.Foreground, func(work context.Context) error {
			return closeDirectoryPrimaryResource(work, warm.releaseChecked)
		})
		if err != nil {
			_ = operation.MarkUnknown(err)
			if reservation.retain() {
				sourceMetadataWarmUnknown.Lock()
				sourceMetadataWarmUnknown.owners[operation] = retainedSourceMetadataWarmCleanup{warm: warm, reservation: reservation, operation: operation}
				sourceMetadataWarmUnknown.Unlock()
			}
		}
		return errors.Join(err, operation.Close())
	}
}

func (s *Store) reserveSourceMetadata(ctx context.Context) (context.Context, *sourceMetadataReservation, error) {
	work, finish, err := s.beginMediaSourceLifetime(context.WithoutCancel(ctx))
	if err != nil {
		return nil, nil, err
	}
	reservation := &sourceMetadataReservation{work: work, finish: finish, refs: 1}
	return context.WithValue(ctx, sourceMetadataReservationKey{}, reservation), reservation, nil
}

func (reservation *sourceMetadataReservation) finishUnused() {
	if reservation != nil {
		reservation.worker.Do(reservation.release)
	}
}

func (reservation *sourceMetadataReservation) retain() bool {
	reservation.mu.Lock()
	defer reservation.mu.Unlock()
	if reservation.refs == 0 {
		return false
	}
	reservation.refs++
	return true
}

func (reservation *sourceMetadataReservation) release() {
	reservation.mu.Lock()
	reservation.refs--
	done := reservation.refs == 0
	reservation.mu.Unlock()
	if done {
		reservation.once.Do(reservation.finish)
	}
}

func (reservation *sourceMetadataReservation) setRoutes(routes map[string]primaryRootIORoute) {
	reservation.mu.Lock()
	reservation.routes = routes
	reservation.mu.Unlock()
}

func (reservation *sourceMetadataReservation) preparedRoutes() map[string]primaryRootIORoute {
	reservation.mu.Lock()
	defer reservation.mu.Unlock()
	return reservation.routes
}

func (s *Store) sourceMetadataRoutes(hint mediaSourceRootHint) (map[string]primaryRootIORoute, error) {
	return s.sourceMetadataRoutesContext(context.Background(), hint)
}

func (s *Store) sourceMetadataRoutesContext(ctx context.Context, hint mediaSourceRootHint) (map[string]primaryRootIORoute, error) {
	route, err := s.primaryReadRouteContext(ctx, hint)
	if err != nil {
		return nil, err
	}
	_, domain, err := s.mediaSourceRootLaneContext(ctx, hint)
	if err != nil {
		return nil, err
	}
	return map[string]primaryRootIORoute{hint.root.id: {route: route, domain: domain}}, nil
}

func sourceMetadataClass(ctx context.Context, snapshot indexedMediaSource) primaryio.Class {
	if phase, _ := ctx.Value(primaryRootIOPhaseKey{}).(*primaryRootIOPhase); phase != nil {
		return phase.class
	}
	if snapshot.mediaFile.primaryReadProof != nil {
		return snapshot.mediaFile.primaryReadProof.class
	}
	if route, _ := ctx.Value(mediaSourceAuthorizationRouteKey{}).(mediaSourceAuthorizationRoute); route.background {
		return primaryio.Background
	}
	return primaryio.Foreground
}

func (s *Store) runSourceMetadata(ctx context.Context, snapshot indexedMediaSource, work func(context.Context) error,
	cleanup ...func(context.Context) error) (resultErr error) {
	failed := func(work context.Context, err error) error {
		if err != nil && len(cleanup) == 1 {
			err = errors.Join(err, cleanup[0](work))
		}
		return err
	}
	if operation := PrimaryRootIOFromContext(ctx); operation != nil {
		err := operation.Run(ctx, snapshot.root.id, sourceMetadataClass(ctx, snapshot), work)
		return failed(operation.Context(context.WithoutCancel(ctx)), err)
	}
	// Private fixtures without an owned catalog keep their original adapter.
	// Every production Store has both the catalog pool and ownership fence.
	if s == nil || s.pool == nil || s.ownership == nil {
		return failed(ctx, work(ctx))
	}
	reservation, _ := ctx.Value(sourceMetadataReservationKey{}).(*sourceMetadataReservation)
	local := reservation == nil
	if local {
		var err error
		ctx, reservation, err = s.reserveSourceMetadata(ctx)
		if err != nil {
			return err
		}
		defer reservation.finishUnused()
	}
	hint := mediaSourceRootHint{root: snapshot.root, bindingRevision: snapshot.rootBindingRevision}
	if prepared, ok := ctx.Value(mediaSourceRootContextKey{}).(mediaSourceRootHint); ok {
		if prepared.root != snapshot.root {
			return ErrSourceChanged
		}
		hint = prepared
	}
	if hint.bindingRevision <= 0 {
		current, err := s.readMediaSourceRootHint(ctx, snapshot.mediaFile.Item.ID)
		if err != nil || current.root != snapshot.root {
			return errors.Join(ErrSourceChanged, err)
		}
		hint = current
	}
	routes := reservation.preparedRoutes()
	if routes == nil {
		var err error
		routes, err = s.sourceMetadataRoutesContext(ctx, hint)
		if err != nil {
			return err
		}
	}
	if !reservation.retain() {
		return ErrUnavailable
	}
	finish := reservation.release
	operation, err := newPrimaryRootIOWithClaimAcquire(reservation.work, sourceMetadataReadOwners,
		&originalMediaReadDomains, routes, finish, originalMediaReadDomains.acquireShared)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, operation.Close()) }()
	err = operation.Run(ctx, snapshot.root.id, sourceMetadataClass(ctx, snapshot), work)
	return failed(operation.Context(context.WithoutCancel(ctx)), err)
}

func (s *Store) runSourceMetadataOpen(ctx context.Context, snapshot indexedMediaSource, open func(context.Context) (*os.File, error)) (file *os.File, resultErr error) {
	cleanup := func(work context.Context) error {
		if file == nil {
			return nil
		}
		closeErr := file.Close()
		if closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			closeErr = media.SourceReadRetirementError(closeErr, file)
			if operation := PrimaryRootIOFromContext(work); operation != nil {
				closeErr = errors.Join(closeErr, operation.MarkUnknown(closeErr))
			}
		} else {
			closeErr = nil
		}
		file = nil
		return closeErr
	}
	resultErr = s.runSourceMetadata(ctx, snapshot, func(work context.Context) error {
		var err error
		file, err = open(work)
		if err == nil {
			err = work.Err()
		}
		if err != nil && file != nil {
			err = errors.Join(err, cleanup(work))
		}
		return err
	}, cleanup)
	return file, resultErr
}

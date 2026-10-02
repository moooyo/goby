package library

import (
	"context"
	"errors"
	"sync"
	"time"
)

const storageObservationTimeout = 5 * time.Second

var (
	storageObservationSlots          = make(chan struct{}, 2)
	errStorageObservationUnavailable = errors.New("bounded storage observation is unavailable")
)

type storageObservationPhaseRetentionKey struct{}

// withStorageObservationPhaseRetention carries only a memory-only retention
// capability. An observation transfers its phase ownership to the actual
// worker before launch, so a caller deadline cannot retire a still-used phase.
func withStorageObservationPhaseRetention(ctx context.Context, retain func() (func() error, error)) context.Context {
	return context.WithValue(ctx, storageObservationPhaseRetentionKey{}, retain)
}

func retainStorageObservationPhase(ctx context.Context) (func() error, error) {
	retain, _ := ctx.Value(storageObservationPhaseRetentionKey{}).(func() (func() error, error))
	if retain == nil {
		return func() error { return nil }, nil
	}
	return retain()
}

// storageObservationLifetime keeps filesystem-only work alive after its caller
// stops waiting. Retirement never closes a descriptor that an admitted worker
// still uses. The final worker releases retired resources without application or
// database locks; a blocked close continues to occupy its observation slot.
type storageObservationLifetime struct {
	mu       sync.Mutex
	active   int
	retired  bool
	close    func() error
	closeErr error
}

type storageObservationRetirementFailure struct {
	closer func() error
	err    error
}

func (failure *storageObservationRetirementFailure) Error() string { return failure.err.Error() }
func (failure *storageObservationRetirementFailure) Unwrap() error { return failure.err }

// Join a close callback independently. Goexit and a panic in an outer defer
// otherwise skip that worker's remaining retirement code. A failed callback
// stays reachable through the error retained by its opaque operation owner.
func closeStorageObservationResources(closer func() error) error {
	finished := make(chan error, 1)
	go func() {
		completed := false
		var result error
		defer func() {
			if recover() != nil || !completed {
				result = ErrUnavailable
			}
			if result != nil {
				result = &storageObservationRetirementFailure{closer: closer, err: result}
			}
			finished <- result
		}()
		result = closer()
		completed = true
	}()
	return <-finished
}

func (lifetime *storageObservationLifetime) closeResources(closer func() error) error {
	err := closeStorageObservationResources(closer)
	if err != nil {
		lifetime.mu.Lock()
		lifetime.close = closer
		lifetime.closeErr = err
		lifetime.mu.Unlock()
	}
	return err
}

func (lifetime *storageObservationLifetime) retain() bool {
	lifetime.mu.Lock()
	defer lifetime.mu.Unlock()
	if lifetime.retired {
		return false
	}
	lifetime.active++
	return true
}

func (lifetime *storageObservationLifetime) release() error {
	lifetime.mu.Lock()
	lifetime.active--
	var closeResources func() error
	if lifetime.active == 0 && lifetime.retired {
		closeResources, lifetime.close = lifetime.close, nil
	}
	lifetime.mu.Unlock()
	if closeResources != nil {
		return lifetime.closeResources(closeResources)
	}
	return nil
}

func (lifetime *storageObservationLifetime) retire(closeResources func() error) error {
	lifetime.mu.Lock()
	if lifetime.retired {
		err := lifetime.closeErr
		lifetime.mu.Unlock()
		return err
	}
	lifetime.retired = true
	if lifetime.active != 0 {
		lifetime.close = closeResources
		lifetime.mu.Unlock()
		return nil
	}
	lifetime.mu.Unlock()
	return lifetime.closeResources(closeResources)
}

// runStorageObservation admits only a bounded number of filesystem workers.
// work must never retain a transaction, execute SQL, or access Store.mu. Its
// resource lifetimes are retained before launch and released by that worker,
// including when the caller times out and rolls back its database transaction.
func runStorageObservation(ctx context.Context, lifetimes []*storageObservationLifetime, work func(context.Context) error) error {
	if PrimaryRootIOFromContext(ctx) != nil {
		// Trusted operation and phase references already supply the process,
		// root, domain and retained-worker bounds. A separate global storage
		// gate would let two stalled domains block an unrelated admitted root.
		return runStorageObservationWithLimit(ctx, make(chan struct{}, 1), storageObservationTimeout, lifetimes, work)
	}
	return runStorageObservationWithLimit(ctx, storageObservationSlots, storageObservationTimeout, lifetimes, work)
}

func runStorageObservationWithLimit(ctx context.Context, slots chan struct{}, timeout time.Duration, lifetimes []*storageObservationLifetime, work func(context.Context) error) error {
	if ctx == nil || work == nil || slots == nil || timeout <= 0 {
		return ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case slots <- struct{}{}:
	default:
		return errStorageObservationUnavailable
	}
	retained := make([]*storageObservationLifetime, 0, len(lifetimes))
	for _, lifetime := range lifetimes {
		if lifetime == nil || !lifetime.retain() {
			closeErr := releaseStorageObservationLifetimes(ctx, retained)
			<-slots
			return errors.Join(errStorageObservationUnavailable, closeErr)
		}
		retained = append(retained, lifetime)
	}
	releasePhase, err := retainStorageObservationPhase(ctx)
	if err != nil {
		closeErr := releaseStorageObservationLifetimes(ctx, retained)
		<-slots
		return errors.Join(err, closeErr)
	}
	observation, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		err := errStorageObservationUnavailable
		completed := false
		defer func() {
			if recover() != nil || !completed {
				err = errStorageObservationUnavailable
				if operation := PrimaryRootIOFromContext(observation); operation != nil {
					_ = operation.MarkUnknown(err)
				}
			}
			retirementErr := releaseStorageObservationLifetimes(observation, retained)
			err = errors.Join(err, retirementErr)
			err = errors.Join(err, releasePhase())
			<-slots
			finished <- err
		}()
		if observation.Err() != nil {
			err = observation.Err()
			completed = true
			return
		}
		err = work(observation)
		completed = true
	}()
	select {
	case err := <-finished:
		if callerErr := ctx.Err(); callerErr != nil {
			return callerErr
		}
		if observation.Err() != nil {
			return errStorageObservationUnavailable
		}
		return err
	case <-observation.Done():
		if err := ctx.Err(); err != nil {
			return err
		}
		return errStorageObservationUnavailable
	}
}

func releaseStorageObservationLifetimes(ctx context.Context, lifetimes []*storageObservationLifetime) error {
	var closeErr error
	for _, lifetime := range lifetimes {
		closeErr = errors.Join(closeErr, lifetime.release())
	}
	if closeErr != nil {
		if operation := PrimaryRootIOFromContext(ctx); operation != nil {
			_ = operation.MarkUnknown(closeErr)
		}
	}
	return closeErr
}

func scanObservationLifetimes(captures []*rootBindingScanCapture, evidence *scanReconciliationEvidence) []*storageObservationLifetime {
	lifetimes := make([]*storageObservationLifetime, 0, len(captures)+1)
	if evidence != nil {
		lifetimes = append(lifetimes, &evidence.observation)
	}
	for _, capture := range captures {
		if capture == nil {
			lifetimes = append(lifetimes, nil)
		} else {
			lifetimes = append(lifetimes, &capture.observation)
		}
	}
	return lifetimes
}

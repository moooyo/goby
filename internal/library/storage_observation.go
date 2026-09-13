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

// storageObservationLifetime keeps filesystem-only work alive after its caller
// stops waiting. Retirement never closes a descriptor that an admitted worker
// still uses. The final worker releases retired resources without application or
// database locks; a blocked close continues to occupy its observation slot.
type storageObservationLifetime struct {
	mu      sync.Mutex
	active  int
	retired bool
	close   func() error
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

func (lifetime *storageObservationLifetime) release() {
	lifetime.mu.Lock()
	lifetime.active--
	var closeResources func() error
	if lifetime.active == 0 && lifetime.retired {
		closeResources, lifetime.close = lifetime.close, nil
	}
	lifetime.mu.Unlock()
	if closeResources != nil {
		_ = closeResources()
	}
}

func (lifetime *storageObservationLifetime) retire(closeResources func() error) error {
	lifetime.mu.Lock()
	if lifetime.retired {
		lifetime.mu.Unlock()
		return nil
	}
	lifetime.retired = true
	if lifetime.active != 0 {
		lifetime.close = closeResources
		lifetime.mu.Unlock()
		return nil
	}
	lifetime.mu.Unlock()
	return closeResources()
}

// runStorageObservation admits only a bounded number of filesystem workers.
// work must never retain a transaction, execute SQL, or access Store.mu. Its
// resource lifetimes are retained before launch and released by that worker,
// including when the caller times out and rolls back its database transaction.
func runStorageObservation(ctx context.Context, lifetimes []*storageObservationLifetime, work func(context.Context) error) error {
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
			for _, held := range retained {
				held.release()
			}
			<-slots
			return errStorageObservationUnavailable
		}
		retained = append(retained, lifetime)
	}
	observation, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		err := errStorageObservationUnavailable
		defer func() {
			if recover() != nil {
				err = errStorageObservationUnavailable
			}
			for _, lifetime := range retained {
				lifetime.release()
			}
			<-slots
			finished <- err
		}()
		if observation.Err() != nil {
			err = observation.Err()
			return
		}
		err = work(observation)
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

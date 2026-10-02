package transcode

import (
	"errors"
	"sync"

	"github.com/moooyo/goby/internal/commanddomain"
)

type fixedPoolNativeExecutable struct {
	state *fixedPoolNativeExecutableState
}

type fixedPoolNativeExecutableState struct {
	mu     sync.Mutex
	use    *fixedPoolExecutableUse
	native *commanddomain.ExecutableCapability
}

// The storage issuer's descriptor use and the native issuer's held duplicate
// remain separate owners of the same inode. A Domain registers its own borrow
// with native; no pathname reopen or wire approval is used for execution.
// Callers retain this bridge through actual child/copier/owned-consumer joins
// and native domain retirement. Close does not release a storage reservation.
func (capability *fixedPoolExecutableCapability) nativeExecutable() (*fixedPoolNativeExecutable, error) {
	use, err := capability.duplicate()
	if use == nil {
		return nil, err
	}
	bridge := &fixedPoolNativeExecutable{state: &fixedPoolNativeExecutableState{use: use}}
	if err != nil {
		return bridge, err
	}
	native, nativeErr := commanddomain.NewExecutableCapability(use.file, capability.path, capability.digest)
	bridge.state.native = native
	if nativeErr != nil {
		if native != nil {
			return bridge, nativeErr
		}
		return bridge, errors.Join(nativeErr, use.close())
	}
	return bridge, nil
}

func (bridge *fixedPoolNativeExecutable) close() error {
	if bridge == nil || bridge.state == nil {
		return errFixedExecutableRetained
	}
	state := bridge.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.native != nil {
		// This rejects every active or unknown concrete native Domain borrow.
		if err := state.native.Close(); err != nil {
			return errors.Join(err, errFixedExecutableRetained)
		}
	}
	return state.use.close()
}

func (fixedPoolNativeExecutable) MarshalJSON() ([]byte, error) { return nil, errFixedBackingPoolUnsafe }
func (*fixedPoolNativeExecutable) UnmarshalJSON([]byte) error  { return errFixedBackingPoolUnsafe }

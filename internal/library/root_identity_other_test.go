//go:build !linux

package library

import (
	"errors"
	"testing"
)

func TestRootStorageIdentityOtherPlatformsAreUnsupported(t *testing.T) {
	observation, err := ObserveRootStorageIdentity(nil)
	if !errors.Is(err, ErrRootStorageIdentityUnsupported) || observation.Identity.Version != 0 || observation.Identity.Handle != nil ||
		observation.Live != (RootStorageWitness{}) {
		t.Fatal("an unsupported platform returned a substitute root identity")
	}
}

//go:build !linux

package library

import "os"

// ObserveRootStorageIdentity has no substitute identity on other platforms.
func ObserveRootStorageIdentity(*os.File) (RootStorageObservation, error) {
	return RootStorageObservation{}, ErrRootStorageIdentityUnsupported
}

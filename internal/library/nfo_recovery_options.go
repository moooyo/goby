package library

import (
	"fmt"
	"runtime"
	"slices"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/storagebinding"
)

// WithNFORecoveryRoots explicitly declares Goby the exclusive sidecar writer
// for the listed registered roots. It grants no other write permission.
func WithNFORecoveryRoots(paths []string) Option {
	paths = append([]string{}, paths...)
	return func(settings *storeOptions) error {
		if settings.nfoRecoveryPaths != nil {
			return fmt.Errorf("%w: repeated NFO recovery roots option", ErrInvalidInput)
		}
		settings.nfoRecoveryPaths = paths
		return nil
	}
}

func assignNFORecoveryRoots(paths []string, approved []approvedRoot) (map[string]struct{}, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	if err := config.ValidateNFORecoveryRootPaths(paths); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("%w: GOBY_NFO_RECOVERY_ROOTS requires Linux", ErrInvalidInput)
	}
	roots := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if !slices.ContainsFunc(approved, func(root approvedRoot) bool {
			return storagebinding.ValidAbsolutePath(root.path) && storagebinding.PathWithin(root.path, path)
		}) {
			return nil, fmt.Errorf("%w: GOBY_NFO_RECOVERY_ROOTS must be under the configured media allowlist", ErrInvalidInput)
		}
		roots[path] = struct{}{}
	}
	return roots, nil
}

// nfoRecoveryWritable is a deployment permission check, not an observation of
// access bits, a filesystem witness, or a substitute for task write authority.
func (s *Store) nfoRecoveryWritable(root libraryRoot) bool {
	if s == nil || runtime.GOOS != "linux" {
		return false
	}
	if _, allowed := s.nfoRecoveryRoots[root.path]; !allowed {
		return false
	}
	if !validCatalogLibraryIdentifier(root.id) || !validCatalogLibraryIdentifier(root.libraryID) {
		return false
	}
	relative, err := (RootTopologyMapping{ApprovedPath: root.allowedPath, RegisteredPath: root.path}).Relative()
	if err != nil || root.relativePath != relative && !(relative == "." && root.relativePath == "") {
		return false
	}
	for i := range s.roots {
		// The startup path is immutable; the live anchor descriptor is not.
		if s.roots[i].path == root.allowedPath {
			return true
		}
	}
	return false
}

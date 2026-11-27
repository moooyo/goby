package config

import (
	"errors"

	"github.com/moooyo/goby/internal/storagebinding"
)

// ValidateNFORecoveryRootPaths checks the registered path syntax without
// consulting storage. The catalog checks containment against the same
// canonical allowlist it uses for ordinary media authorization.
func ValidateNFORecoveryRootPaths(paths []string) error {
	for _, path := range paths {
		if !storagebinding.ValidAbsolutePath(path) {
			return errors.New("GOBY_NFO_RECOVERY_ROOTS must list exact canonical absolute library paths")
		}
	}
	return nil
}

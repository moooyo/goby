//go:build !linux

package library

import (
	"context"
	"os"
)

func captureRootTopologyPlatform(context.Context, *libraryRootLease, RootTopologyMapping, *os.Root) (*RootTopologyCapture, error) {
	return nil, ErrRootStorageIdentityUnsupported
}

func revalidateRootTopologyPlatform(context.Context, *RootTopologyCapture) error {
	return ErrRootStorageIdentityUnsupported
}

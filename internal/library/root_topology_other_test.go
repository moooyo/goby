//go:build !linux

package library

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestRootTopologyOtherPlatformsHaveNoSubstituteCapture(t *testing.T) {
	// The platform stub must not dereference these otherwise valid API inputs
	// or fabricate a device/inode substitute for a Linux storage topology.
	lease := &libraryRootLease{approved: new(os.Root), relativePath: "registered"}
	value, err := lease.CaptureTopology(context.Background(), RootTopologyMapping{
		ApprovedPath: "/approved", RegisteredPath: "/approved/registered",
	}, new(os.Root))
	if !errors.Is(err, ErrRootStorageIdentityUnsupported) || value != nil {
		t.Fatal("an unsupported platform returned a replacement topology identity")
	}
}

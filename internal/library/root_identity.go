package library

import "github.com/moooyo/goby/internal/storagebinding"

const (
	RootStorageIdentityVersion = storagebinding.IdentityVersion
	RootStorageIdentityProfile = storagebinding.IdentityProfile
	MaxRootStorageHandleBytes  = storagebinding.MaxHandleBytes
)

var (
	ErrRootStorageIdentityUnsupported = storagebinding.ErrIdentityUnsupported
	ErrRootStorageIdentityUnavailable = storagebinding.ErrIdentityUnavailable
	ErrInvalidRootStorageIdentity     = storagebinding.ErrInvalidIdentity
)

// RootStorageIdentity retains the complete filesystem UUID and opaque directory
// file handle. Equality describes these observations only: it does not approve
// a library root, establish mount topology, or grant missing-file deletion.
// Reboot persistence and filesystem-specific behavior require separate evidence.
type RootStorageIdentity = storagebinding.Identity

// RootStorageWitness describes this particular open directory. Device numbers,
// inode numbers and the legacy mount ID are not part of the stable profile and
// must not be substituted for it across mounts, namespaces or system boots.
type RootStorageWitness struct {
	Device  uint64 `json:"device"`
	Inode   uint64 `json:"inode"`
	MountID int    `json:"mount_id"`
}

type RootStorageObservation struct {
	Identity RootStorageIdentity `json:"identity"`
	Live     RootStorageWitness  `json:"live"`
}

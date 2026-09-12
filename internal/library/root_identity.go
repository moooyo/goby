package library

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	RootStorageIdentityVersion = 1
	RootStorageIdentityProfile = "linux-fsuuid-filehandle-v1"
	MaxRootStorageHandleBytes  = 128
)

var (
	ErrRootStorageIdentityUnsupported = errors.New("root storage identity is unsupported")
	ErrRootStorageIdentityUnavailable = errors.New("root storage identity is unavailable")
	ErrInvalidRootStorageIdentity     = errors.New("root storage identity is invalid")
)

// RootStorageIdentity retains the complete filesystem UUID and opaque directory
// file handle. Equality describes these observations only: it does not approve
// a library root, establish mount topology, or grant missing-file deletion.
// Reboot persistence and filesystem-specific behavior require separate evidence.
type RootStorageIdentity struct {
	Version        int    `json:"version"`
	Profile        string `json:"profile"`
	FilesystemUUID string `json:"filesystem_uuid"`
	HandleType     int32  `json:"handle_type"`
	Handle         []byte `json:"handle"`
}

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

// Validate rejects unknown profiles and incomplete persisted identities. UUIDs
// are the exact 16 bytes encoded as canonical lowercase hex, without assuming
// an RFC UUID version or interpreting the filesystem's opaque handle type.
func (identity RootStorageIdentity) Validate() error {
	if identity.Version != RootStorageIdentityVersion || identity.Profile != RootStorageIdentityProfile ||
		len(identity.FilesystemUUID) != 32 || strings.ToLower(identity.FilesystemUUID) != identity.FilesystemUUID ||
		identity.FilesystemUUID == "00000000000000000000000000000000" ||
		len(identity.Handle) == 0 || len(identity.Handle) > MaxRootStorageHandleBytes {
		return ErrInvalidRootStorageIdentity
	}
	if _, err := hex.DecodeString(identity.FilesystemUUID); err != nil {
		return ErrInvalidRootStorageIdentity
	}
	return nil
}

// Equal never treats two absent or malformed identities as a verified match.
func (identity RootStorageIdentity) Equal(other RootStorageIdentity) bool {
	return identity.Validate() == nil && other.Validate() == nil && identity.Version == other.Version &&
		identity.Profile == other.Profile && identity.FilesystemUUID == other.FilesystemUUID &&
		identity.HandleType == other.HandleType && bytes.Equal(identity.Handle, other.Handle)
}

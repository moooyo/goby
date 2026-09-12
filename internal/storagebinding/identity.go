// Package storagebinding defines pure storage identity and topology observations.
// Its models do not open media paths, persist approval, or authorize deletion.
package storagebinding

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	IdentityVersion = 1
	IdentityProfile = "linux-fsuuid-filehandle-v1"
	MaxHandleBytes  = 128
)

var (
	ErrIdentityUnsupported = errors.New("root storage identity is unsupported")
	ErrIdentityUnavailable = errors.New("root storage identity is unavailable")
	ErrInvalidIdentity     = errors.New("root storage identity is invalid")
)

// Identity retains the complete filesystem UUID and opaque directory file
// handle. Equality describes these observations only: it does not approve a
// library root, establish mount topology, or grant missing-file deletion.
// Reboot persistence and filesystem-specific behavior require separate evidence.
type Identity struct {
	Version        int    `json:"version"`
	Profile        string `json:"profile"`
	FilesystemUUID string `json:"filesystem_uuid"`
	HandleType     int32  `json:"handle_type"`
	Handle         []byte `json:"handle"`
}

// Validate rejects unknown profiles and incomplete persisted identities. UUIDs
// are the exact 16 bytes encoded as canonical lowercase hex, without assuming
// an RFC UUID version or interpreting the filesystem's opaque handle type.
func (identity Identity) Validate() error {
	if identity.Version != IdentityVersion || identity.Profile != IdentityProfile ||
		len(identity.FilesystemUUID) != 32 || strings.ToLower(identity.FilesystemUUID) != identity.FilesystemUUID ||
		identity.FilesystemUUID == "00000000000000000000000000000000" ||
		len(identity.Handle) == 0 || len(identity.Handle) > MaxHandleBytes {
		return ErrInvalidIdentity
	}
	if _, err := hex.DecodeString(identity.FilesystemUUID); err != nil {
		return ErrInvalidIdentity
	}
	return nil
}

// Equal never treats two absent or malformed identities as a verified match.
func (identity Identity) Equal(other Identity) bool {
	return identity.Validate() == nil && other.Validate() == nil && identity.Version == other.Version &&
		identity.Profile == other.Profile && identity.FilesystemUUID == other.FilesystemUUID &&
		identity.HandleType == other.HandleType && bytes.Equal(identity.Handle, other.Handle)
}

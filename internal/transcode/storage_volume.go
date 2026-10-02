package transcode

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

var (
	errFixedVolumeUnavailable = errors.New("fixed volume provider is unavailable")
	errFixedVolumeUnsafe      = errors.New("fixed volume ownership is unknown or unsafe")
	errFixedVolumeRetained    = errors.New("fixed volume ownership must be retained")
)

// The primitive owns allocation and deletion, but does not launch writers. A
// broker that confines every output, scratch file, subtitle, font, cache and
// temporary publication file to this one filesystem is still required. Neither
// chmod nor a read-only remount proves that preexisting writable FDs are gone.
const storageBackendReady = false

const (
	fixedVolumeMarkerName            = ".goby-fixed-volume-root"
	fixedVolumeLockName              = ".goby-fixed-volume-lock"
	fixedVolumeMarkerVersion         = "goby-fixed-volume-root-v1\n"
	maxFixedVolumeReceiptBytes       = 16 * 1024
	fixedVolumeOwnerBytes            = 2*maxFixedVolumeReceiptBytes + 8*4096
	fixedVolumeOwnerInodes           = 8
	fixedVolumeBlockSize       int64 = 4096
)

// fixedVolumeConfig is trusted broker configuration, never a service RPC. The
// dedicated root and exact marker must already have been created by its owner.
// Mke2fsPath must name an immutable root-owned approved binary with this digest.
type fixedVolumeConfig struct {
	Enabled           bool
	ProvisionRoot     string
	ExpectedRootToken string
	Mke2fsPath        string
	Mke2fsSHA256      string
	Ledger            *storageReservationLedger
	poolPreparation   *fixedPoolPreparation
}

// fixedVolumeIdentity is the durable kernel identity of a whole fixed domain.
// FilesystemBytes is statfs total allocatable space, not current free space.
// BackingBytes is fixed logical/device length, including inner ext4 metadata.
// BackingAllocatedBytes additionally counts the host inode's extent metadata;
// BackingAllowanceBytes is its whole durable physical reservation. The separate
// fixedVolumeOwnerBytes control allowance is not spent on those same blocks.
// Reservations never shrink to observed usage while any ownership is retained.
// These physical limits do not bound sparse logical output length; existing
// logical-output validation remains necessary. Namespace identifies the private
// control directory; MountNamespace identifies the kernel mount namespace.
type fixedVolumeIdentity struct {
	ID                    string `json:"id"`
	ProviderToken         string `json:"provider_token"`
	ProvisionRootDevice   uint64 `json:"provision_root_device"`
	ProvisionRootInode    uint64 `json:"provision_root_inode"`
	LedgerRootDevice      uint64 `json:"ledger_root_device"`
	LedgerRootInode       uint64 `json:"ledger_root_inode"`
	BootID                string `json:"boot_id"`
	MountNamespaceDevice  uint64 `json:"mount_namespace_device"`
	MountNamespaceInode   uint64 `json:"mount_namespace_inode"`
	NamespaceDevice       uint64 `json:"namespace_device"`
	NamespaceInode        uint64 `json:"namespace_inode"`
	MountpointDevice      uint64 `json:"mountpoint_device"`
	MountpointInode       uint64 `json:"mountpoint_inode"`
	BackingDevice         uint64 `json:"backing_device"`
	BackingInode          uint64 `json:"backing_inode"`
	BackingBytes          int64  `json:"backing_bytes"`
	BackingObservedBytes  int64  `json:"backing_observed_bytes"`
	BackingAllocatedBytes int64  `json:"backing_allocated_bytes"`
	BackingAllowanceBytes int64  `json:"backing_allowance_bytes"`
	BackingMapSHA256      string `json:"backing_map_sha256"`
	LoopDevice            uint64 `json:"loop_device"`
	LoopNumber            uint32 `json:"loop_number"`
	MountID               uint64 `json:"mount_id"`
	RootDevice            uint64 `json:"root_device"`
	RootInode             uint64 `json:"root_inode"`
	FilesystemUUID        string `json:"filesystem_uuid"`
	FilesystemBytes       int64  `json:"filesystem_bytes"`
	FilesystemInodes      int64  `json:"filesystem_inodes"`
	BlockSize             int64  `json:"block_size"`
}

func (i fixedVolumeIdentity) validate(bytes, inodes int64) error {
	if !fixedVolumeValidID(i.ID) || !fixedVolumeHex(i.ProviderToken, 16) ||
		i.ProvisionRootDevice == 0 || i.ProvisionRootInode == 0 ||
		i.LedgerRootDevice == 0 || i.LedgerRootInode == 0 ||
		!fixedVolumeValidUUID(i.BootID) || i.MountNamespaceDevice == 0 || i.MountNamespaceInode == 0 ||
		i.NamespaceDevice == 0 || i.NamespaceInode == 0 ||
		i.MountpointDevice == 0 || i.MountpointInode == 0 ||
		i.BackingDevice == 0 || i.BackingInode == 0 || i.LoopDevice == 0 ||
		i.MountID == 0 || i.RootDevice != i.LoopDevice || i.RootInode == 0 ||
		i.BackingBytes != bytes || i.BackingObservedBytes != bytes || i.BackingAllocatedBytes < bytes ||
		i.BackingAllocatedBytes%512 != 0 || i.BackingAllocatedBytes > i.BackingAllowanceBytes || !fixedVolumeHex(i.BackingMapSHA256, 32) ||
		bytes < 16*1024*1024 || bytes%fixedVolumeBlockSize != 0 ||
		i.BlockSize != fixedVolumeBlockSize || i.FilesystemBytes <= 0 ||
		i.FilesystemBytes > bytes || i.FilesystemInodes <= 0 ||
		i.FilesystemInodes > inodes || !fixedVolumeValidUUID(i.FilesystemUUID) {
		return fmt.Errorf("%w: incomplete fixed filesystem identity", errFixedVolumeUnsafe)
	}
	return nil
}

func (i fixedVolumeIdentity) validateReservation(bytes, inodes, ownerBytes int64) error {
	if err := i.validate(bytes, inodes); err != nil {
		return err
	}
	if ownerBytes < fixedVolumeOwnerBytes {
		return errFixedVolumeUnsafe
	}
	allowance, ok := addStorageCapacity(bytes, ownerBytes-fixedVolumeOwnerBytes)
	if !ok || i.BackingAllowanceBytes != allowance {
		return errFixedVolumeUnsafe
	}
	return nil
}

func fixedVolumeValidID(id string) bool {
	if len(id) == 0 || len(id) > 192 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func fixedVolumeHex(value string, bytes int) bool {
	if len(value) != bytes*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == bytes
}

func fixedVolumeValidUUID(value string) bool {
	return len(value) == 36 && value[8] == '-' && value[13] == '-' &&
		value[18] == '-' && value[23] == '-' &&
		fixedVolumeHex(strings.ReplaceAll(value, "-", ""), 16) &&
		value != "00000000-0000-0000-0000-000000000000"
}

// A receipt is only minted after exact nonlazy unmount, exact loop detachment,
// backing unlink and namespace removal have all completed and been synced. The
// ledger must also match this exact permit pointer, its revision and identity.
type storageRetirementReceipt struct {
	permit          *storageRetirePermit
	identity        fixedVolumeIdentity
	cleanupIdentity fixedVolumeIdentity
	providerToken   string
	proof           [32]byte
	acknowledged    atomic.Bool
}

func fixedVolumeLeaseID(id string, serial uint64) string {
	return "volume-" + id + "-" + strconv.FormatUint(serial, 10)
}

type fixedVolumeProvisionFailure struct {
	Stage    string
	Identity fixedVolumeIdentity
	Cause    error
}

func (e *fixedVolumeProvisionFailure) Error() string {
	return fmt.Sprintf("%v: provisioning stopped at %s: %v", errFixedVolumeRetained, e.Stage, e.Cause)
}

func (e *fixedVolumeProvisionFailure) Unwrap() error { return e.Cause }

func (e *fixedVolumeProvisionFailure) Is(target error) bool { return target == errFixedVolumeRetained }

// fixedVolumeProvider is an optional root broker primitive. No client receives
// the root, backing, loop-control, loop or mount authority descriptors.
type fixedVolumeProvider interface {
	Provision(context.Context, *storageProvisionPermit) (fixedVolumeIdentity, error)
	Retire(context.Context, *storageRetirePermit) (*storageRetirementReceipt, error)
	AcknowledgeRetirement(context.Context, *storageRetirementReceipt) error
	Close() error
}

package transcode

import (
	"errors"
	"fmt"
	"math"
	"os"
)

var errFixedBackingPoolUnsafe = errors.New("fixed backing pool identity or capacity is unknown")

const (
	fixedBackingPoolMarkerName       = ".goby-fixed-backing-pool"
	fixedBackingPoolLockName         = ".goby-fixed-backing-pool-lock"
	fixedBackingPoolVersion          = "goby-fixed-backing-pool-v1\n"
	maxFixedBackingPoolStaticObjects = 16
)

type fixedBackingPoolSourceKind uint8

// Only the pooled factory mints this capability. Its Linux validation requires
// the concrete native issuer and rechecks its held device/mount/static objects;
// an in-memory issuer or client boolean cannot attest a backing domain.
type fixedPoolPreparation struct{ issuer any }

const (
	fixedBackingPoolPartition fixedBackingPoolSourceKind = iota + 1
	fixedBackingPoolInitializedLoop
)

// This identity describes a guest-visible kernel allocation domain. It does
// not attest hypervisor, controller, snapshot or lower-layer physical usage.
// The trusted root owner retains block, resize, mount and namespace authority;
// no service or workspace writer receives those descriptors or controls.
// V3 reserves per-image extent metadata separately from fixed logical device
// capacity, initializes its complete map and retains the whole allowance. The
// archived v1 exact-length accounting is not evidence for this revision.
type fixedBackingPoolIdentity struct {
	RootDevice                      uint64
	RootInode                       uint64
	MountID                         uint64
	BootID                          string
	MountNamespaceDevice            uint64
	MountNamespaceInode             uint64
	DeviceNodeDevice                uint64
	DeviceNodeInode                 uint64
	DeviceNumber                    uint64
	DeviceBytes                     int64
	FilesystemUUID                  string
	FilesystemBytes                 int64
	FilesystemInodes                int64
	JournalInode                    uint64
	VolumesInode                    uint64
	PartitionNumber                 uint64
	PartitionStartSector            uint64
	PartitionSectors                uint64
	CoveredMountpointDevice         uint64
	CoveredMountpointInode          uint64
	CoveredMountpointAllocatedBytes int64
	LoopNumber                      uint32
	BackingDevice                   uint64
	BackingInode                    uint64
	BackingBytes                    int64
	BackingAllocatedBytes           int64
	BackingMapSHA256                string
}

// Additional outside objects must be immutable, already-installed regular
// files or directories. The formatter, device node and borrowed covered
// mountpoint are always counted automatically. Duplicate inodes count once.
// SHA256 is optional for data files; the provider captures and rechecks their
// immutable content digest. Executable approval still requires the configured
// formatter digest. All reviewed outside setup/control objects must be listed.
type fixedBackingPoolStaticObject struct {
	Path           string
	Device         uint64
	Inode          uint64
	AllocatedBytes int64
	SHA256         string
}

// Configuration is root-owned setup evidence, never a client-provided flag or
// path. PoolRoot is an existing dedicated ext4 mount on a direct partition or
// a controlled, fully initialized loop image. Stacked mutable mappings are not
// supported. Virtio partitions are supported in the declared guest-kernel scope.
// Preinstalled OS/kernel/tool dependencies are trusted deployment inputs, not
// task-added allocations. The private approved immutable formatter is charged;
// its environment and arguments accept no client config or preload settings.
// CoveredMountpoint is borrowed from before mounting; ownership is not moved
// and the factory never closes the caller's descriptor. The owner keeps it open
// and close-on-exec until provider closure, then closes it explicitly. Flags
// alone do not prove a future launcher's descriptor confinement. Every dynamic broker
// file, receipt, journal copy and backing image must remain inside this pool.
type fixedBackingPoolConfig struct {
	PoolRoot            string
	DevicePath          string
	SourceKind          fixedBackingPoolSourceKind
	BackingPath         string
	Expected            fixedBackingPoolIdentity
	CoveredMountpoint   *os.File
	GlobalPhysicalBytes int64
	// GlobalOuterInodes bounds the pool's inode table plus outside static inodes.
	GlobalOuterInodes int64
	// GlobalKernelInodes additionally includes every admitted nested inode table.
	GlobalKernelInodes  int64
	OutsideStaticBytes  int64
	OutsideStaticInodes int64
	OutsideStatic       []fixedBackingPoolStaticObject
}

type fixedBackingPoolBudget struct {
	AdmittedLeases    int64
	ReservationBytes  int64
	OuterInodes       int64
	HardPhysicalBytes int64
	HardOuterInodes   int64
	HardKernelInodes  int64
}

func checkedFixedPoolAdd(a, b int64) (int64, bool) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, false
	}
	return a + b, true
}

func checkedFixedPoolMultiply(a, b int64) (int64, bool) {
	if a < 0 || b < 0 || a != 0 && b > math.MaxInt64/a {
		return 0, false
	}
	return a * b, true
}

func fixedBackingPoolCapacity(config fixedBackingPoolConfig, limits storageReservationLimits, outsideBytes, outsideInodes int64) (fixedBackingPoolBudget, error) {
	var budget fixedBackingPoolBudget
	identity := config.Expected
	poolBytes := identity.DeviceBytes
	if config.SourceKind == fixedBackingPoolInitializedLoop {
		if identity.BackingBytes != identity.DeviceBytes || identity.BackingAllocatedBytes < identity.BackingBytes {
			return budget, errFixedBackingPoolUnsafe
		}
		poolBytes = identity.BackingAllocatedBytes
	} else if config.SourceKind != fixedBackingPoolPartition {
		return budget, errFixedBackingPoolUnsafe
	}
	if !validStorageReservationLimits(limits) || identity.DeviceBytes <= 0 ||
		identity.FilesystemBytes <= 0 || identity.FilesystemBytes > identity.DeviceBytes || identity.FilesystemInodes <= 0 ||
		config.OutsideStaticBytes < outsideBytes || config.OutsideStaticInodes < outsideInodes ||
		outsideBytes < 0 || outsideInodes < 0 || config.GlobalPhysicalBytes <= 0 || config.GlobalOuterInodes <= 0 || config.GlobalKernelInodes <= 0 {
		return budget, errFixedBackingPoolUnsafe
	}
	jobBytes, bytesOK := checkedFixedPoolAdd(limits.VolumeBytes, limits.OwnerBytes)
	jobInodes, inodesOK := checkedFixedPoolAdd(limits.VolumeInodes, limits.OwnerInodes)
	if !bytesOK || !inodesOK || jobBytes == 0 || jobInodes == 0 {
		return budget, errFixedBackingPoolUnsafe
	}
	admitted := int64(limits.MaxLeases)
	if byBytes := (limits.Bytes - limits.BaseBytes) / jobBytes; byBytes < admitted {
		admitted = byBytes
	}
	if byInodes := (limits.Inodes - limits.BaseInodes) / jobInodes; byInodes < admitted {
		admitted = byInodes
	}
	imageBytes, bytesOK := checkedFixedPoolMultiply(admitted, jobBytes)
	outerInodes, inodesOK := checkedFixedPoolMultiply(admitted, limits.OwnerInodes)
	if admitted <= 0 || !bytesOK || !inodesOK {
		return budget, errFixedBackingPoolUnsafe
	}
	reservation, bytesOK := checkedFixedPoolAdd(limits.BaseBytes, imageBytes)
	outerInodes, inodesOK = checkedFixedPoolAdd(limits.BaseInodes, outerInodes)
	hardBytes, hardBytesOK := checkedFixedPoolAdd(poolBytes, outsideBytes)
	hardInodes, hardInodesOK := checkedFixedPoolAdd(identity.FilesystemInodes, outsideInodes)
	innerInodes, innerInodesOK := checkedFixedPoolMultiply(admitted, limits.VolumeInodes)
	hardKernelInodes, kernelInodesOK := checkedFixedPoolAdd(hardInodes, innerInodes)
	if !bytesOK || !inodesOK || !hardBytesOK || !hardInodesOK || !innerInodesOK || !kernelInodesOK ||
		reservation > identity.FilesystemBytes || outerInodes > identity.FilesystemInodes ||
		hardBytes > config.GlobalPhysicalBytes || hardInodes > config.GlobalOuterInodes || hardKernelInodes > config.GlobalKernelInodes {
		return budget, fmt.Errorf("%w: full reservations do not fit the fixed byte and outer inode domains", errFixedBackingPoolUnsafe)
	}
	// The whole device, or its fully allocated loop source, is counted once,
	// including ext4 metadata. Images and
	// nested inode tables are already inside it and are not added a second time.
	// Fragmentation or metadata work may still produce ENOSPC before these
	// accounting ceilings. No free-space or successful-admission promise follows.
	return fixedBackingPoolBudget{AdmittedLeases: admitted, ReservationBytes: reservation,
		OuterInodes: outerInodes, HardPhysicalBytes: hardBytes, HardOuterInodes: hardInodes, HardKernelInodes: hardKernelInodes}, nil
}

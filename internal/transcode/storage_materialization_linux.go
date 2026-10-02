//go:build linux

package transcode

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	fixedVolumeUnwrittenFlag          = uint32(0x800)
	fixedVolumeMaterializationTimeout = 5 * time.Minute
)

// This audit is not activation authority. Only a durably recorded final strict
// map in the ordinary identity can authorize the subsequent loop/mount stages.
type fixedVolumeMaterializationAudit struct {
	InitialMapSHA256       string `json:"initial_map_sha256"`
	PostFormatterMapSHA256 string `json:"post_formatter_map_sha256"`
	FinalMapSHA256         string `json:"final_map_sha256"`
	UnwrittenBytes         int64  `json:"unwritten_bytes"`
	MaterializedBytes      int64  `json:"materialized_bytes"`
}

func fixedVolumeUnactivated(identity fixedVolumeIdentity) bool {
	return identity.LoopDevice == 0 && identity.LoopNumber == 0 && identity.MountID == 0 &&
		identity.RootDevice == 0 && identity.RootInode == 0 && identity.MountpointDevice == 0 && identity.MountpointInode == 0
}

func fixedVolumePreparationReceiptValid(receipt fixedVolumeDiskReceipt) bool {
	audit := receipt.Materialization
	for _, digest := range []string{receipt.InitialBackingMapSHA256, audit.InitialMapSHA256, audit.PostFormatterMapSHA256, audit.FinalMapSHA256} {
		if digest != "" && !fixedVolumeHex(digest, sha256.Size) {
			return false
		}
	}
	if audit.UnwrittenBytes < 0 || audit.MaterializedBytes < 0 || audit.MaterializedBytes > audit.UnwrittenBytes ||
		audit.UnwrittenBytes > receipt.Bytes || audit.InitialMapSHA256 != "" && audit.InitialMapSHA256 != receipt.InitialBackingMapSHA256 {
		return false
	}
	complete := fixedVolumeHex(receipt.InitialBackingMapSHA256, sha256.Size) && audit.InitialMapSHA256 == receipt.InitialBackingMapSHA256 &&
		fixedVolumeHex(audit.PostFormatterMapSHA256, sha256.Size) && fixedVolumeHex(audit.FinalMapSHA256, sha256.Size) &&
		audit.FinalMapSHA256 == receipt.Identity.BackingMapSHA256 && audit.MaterializedBytes == audit.UnwrittenBytes
	switch receipt.Stage {
	case "initialized":
		return fixedVolumeUnactivated(receipt.Identity) && receipt.InitialBackingMapSHA256 == receipt.Identity.BackingMapSHA256 &&
			fixedVolumeHex(receipt.InitialBackingMapSHA256, sha256.Size)
	case "format-intent", "materialize-intent":
		return fixedVolumeUnactivated(receipt.Identity) && receipt.Identity.BackingMapSHA256 == "" &&
			fixedVolumeHex(receipt.InitialBackingMapSHA256, sha256.Size) && fixedVolumeValidUUID(receipt.Identity.FilesystemUUID)
	case "materialized", "formatted":
		return fixedVolumeUnactivated(receipt.Identity) && complete && fixedVolumeValidUUID(receipt.Identity.FilesystemUUID)
	}
	// An activated resource always carries the final audit/pin. Earlier canceled
	// preparation may retire by exact partial identity, never a guessed path.
	if receipt.Identity.LoopDevice != 0 || receipt.Identity.MountpointInode != 0 || receipt.Identity.MountID != 0 {
		return complete
	}
	return true
}

// The callback supplies the broker's actual durable receipt store. It does not
// supply allocation proof or backend activation; native file guards below are
// mandatory even when a test injects a receipt persistence failure.
func fixedVolumeCommitPreparation(ctx context.Context, backing *os.File, receipt *fixedVolumeDiskReceipt, persist func(fixedVolumeDiskReceipt) error) error {
	ctx, cancel := context.WithTimeout(ctx, fixedVolumeMaterializationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if receipt == nil || persist == nil || receipt.Version != fixedVolumeReceiptVersion || receipt.Stage != "format-intent" ||
		!fixedVolumePreparationReceiptValid(*receipt) {
		return errFixedVolumeUnsafe
	}
	receipt.Stage = "materialize-intent"
	receipt.Materialization.InitialMapSHA256 = receipt.InitialBackingMapSHA256
	if err := persist(*receipt); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	finalIdentity, audit, materializeErr := fixedVolumeMaterializeFormattedBacking(ctx, backing, receipt.Identity)
	audit.InitialMapSHA256 = receipt.InitialBackingMapSHA256
	receipt.Identity, receipt.Materialization = finalIdentity, audit
	observeErr := fixedVolumeObserveBacking(backing, &receipt.Identity)
	if materializeErr == nil && observeErr == nil {
		receipt.Stage = "materialized"
	} else {
		// Failed preparation remains partial; an audit never becomes an active
		// initialized identity or releases any part of the whole reservation.
		receipt.Identity.BackingMapSHA256 = ""
	}
	persistErr := persist(*receipt)
	return errors.Join(materializeErr, observeErr, persistErr, ctx.Err())
}

func fixedVolumePreparationIdentity(backing *os.File, identity fixedVolumeIdentity) (fixedVolumeIdentity, error) {
	if backing == nil || identity.BackingDevice == 0 || identity.BackingInode == 0 || identity.BackingBytes <= 0 ||
		identity.BackingBytes%fixedVolumeBlockSize != 0 || identity.BackingAllowanceBytes < identity.BackingBytes ||
		!fixedVolumeUnactivated(identity) {
		return identity, fmt.Errorf("%w: materialization is restricted to unactivated preparation", errFixedVolumeUnsafe)
	}
	var stat unix.Stat_t
	if err := fixedVolumeOwnedRegular(backing, &stat); err != nil {
		return identity, err
	}
	flags, err := unix.FcntlInt(backing.Fd(), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 || stat.Gid != 0 || stat.Mode&0o777 != 0o600 ||
		uint64(stat.Dev) != identity.BackingDevice || stat.Ino != identity.BackingInode || stat.Size != identity.BackingBytes ||
		stat.Blocks < 0 || stat.Blocks > math.MaxInt64/512 || stat.Blocks*512 < identity.BackingBytes ||
		stat.Blocks*512 > identity.BackingAllowanceBytes {
		return identity, fmt.Errorf("%w: exact backing ownership or whole allocation allowance changed", errFixedVolumeUnsafe)
	}
	identity.BackingObservedBytes, identity.BackingAllocatedBytes = stat.Size, stat.Blocks*512
	return identity, nil
}

func fixedVolumeValidatePreparationMap(extents []fixedPoolFiemapExtent, bytes uint64) error {
	if len(extents) == 0 || len(extents) > maxFixedPoolExtents {
		return errFixedVolumeUnsafe
	}
	initialized := make([]fixedPoolFiemapExtent, len(extents))
	for index, extent := range extents {
		if extent.Flags & ^(uint32(fixedPoolFiemapLast)|fixedVolumeUnwrittenFlag) != 0 {
			return fmt.Errorf("%w: forbidden preparation extent flags 0x%x", errFixedVolumeUnsafe, extent.Flags)
		}
		initialized[index] = extent
		initialized[index].Flags &= uint32(fixedPoolFiemapLast)
	}
	// The frozen strict parser independently checks complete coverage, LAST,
	// alignment, physical overlap, reserved fields and integer bounds.
	if _, err := fixedPoolMapDigest(initialized, bytes); err != nil {
		return errors.Join(errFixedVolumeUnsafe, err)
	}
	return nil
}

func fixedVolumePreparationMap(ctx context.Context, backing *os.File, bytes int64) ([]fixedPoolFiemapExtent, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if bytes <= 0 || bytes%fixedVolumeBlockSize != 0 || unsafe.Offsetof(fixedPoolFiemapRequest{}.Extents) != 32 ||
		unsafe.Sizeof(fixedPoolFiemapExtent{}) != 56 {
		return nil, "", errFixedVolumeUnsafe
	}
	request := &fixedPoolFiemapRequest{Length: uint64(bytes), Flags: fixedPoolFiemapSync, Count: maxFixedPoolExtents}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, backing.Fd(), uintptr(fixedPoolFiemapIOCTL), uintptr(unsafe.Pointer(request)))
	if errno != 0 {
		return nil, "", errno
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if request.Mapped == 0 || request.Mapped > maxFixedPoolExtents || request.Start != 0 || request.Length != uint64(bytes) ||
		request.Flags != fixedPoolFiemapSync || request.Count != maxFixedPoolExtents || request.Reserved != 0 {
		return nil, "", errFixedVolumeUnsafe
	}
	extents := request.Extents[:request.Mapped]
	if err := fixedVolumeValidatePreparationMap(extents, uint64(bytes)); err != nil {
		return nil, "", err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("goby-fixed-backing-fiemap-v1\n"))
	var header [12]byte
	binary.LittleEndian.PutUint64(header[:8], uint64(bytes))
	binary.LittleEndian.PutUint32(header[8:], uint32(len(extents)))
	_, _ = hash.Write(header[:])
	for _, extent := range extents {
		var encoded [28]byte
		binary.LittleEndian.PutUint64(encoded[:8], extent.Logical)
		binary.LittleEndian.PutUint64(encoded[8:16], extent.Physical)
		binary.LittleEndian.PutUint64(encoded[16:24], extent.Length)
		binary.LittleEndian.PutUint32(encoded[24:], extent.Flags)
		_, _ = hash.Write(encoded[:])
	}
	return extents, hex.EncodeToString(hash.Sum(nil)), nil
}

// Caller authority is the root broker's successful formatter join followed by
// its durable materialize-intent receipt. This helper never creates a loop,
// mounts a filesystem, activates a lease, or repairs an active workspace.
// The finite context is checked around bounded I/O; uninterruptible kernel I/O
// is not claimed to obey a hard wall-clock deadline.
func fixedVolumeMaterializeFormattedBacking(ctx context.Context, backing *os.File, identity fixedVolumeIdentity) (fixedVolumeIdentity, fixedVolumeMaterializationAudit, error) {
	ctx, cancel := context.WithTimeout(ctx, fixedVolumeMaterializationTimeout)
	defer cancel()
	audit := fixedVolumeMaterializationAudit{InitialMapSHA256: identity.BackingMapSHA256}
	if err := ctx.Err(); err != nil {
		return identity, audit, err
	}
	observed, err := fixedVolumePreparationIdentity(backing, identity)
	if err != nil {
		return identity, audit, err
	}
	extents, digest, err := fixedVolumePreparationMap(ctx, backing, identity.BackingBytes)
	if err != nil {
		return observed, audit, err
	}
	audit.PostFormatterMapSHA256 = digest
	zeros := make([]byte, 1<<20)
	readback := make([]byte, len(zeros))
	for _, extent := range extents {
		if extent.Flags&fixedVolumeUnwrittenFlag == 0 {
			continue
		}
		audit.UnwrittenBytes += int64(extent.Length)
		for offset, end := int64(extent.Logical), int64(extent.Logical+extent.Length); offset < end; {
			if err = ctx.Err(); err != nil {
				return observed, audit, err
			}
			if observed, err = fixedVolumePreparationIdentity(backing, identity); err != nil {
				return observed, audit, err
			}
			length := int64(len(zeros))
			if end-offset < length {
				length = end - offset
			}
			buffer := readback[:int(length)]
			if count, readErr := backing.ReadAt(buffer, offset); readErr != nil || count != len(buffer) {
				return observed, audit, errors.Join(readErr, io.ErrUnexpectedEOF)
			}
			for _, value := range buffer {
				if value != 0 {
					return observed, audit, fmt.Errorf("%w: unwritten preparation region did not read as zero", errFixedVolumeUnsafe)
				}
			}
			if err = ctx.Err(); err != nil {
				return observed, audit, err
			}
			if count, writeErr := backing.WriteAt(zeros[:int(length)], offset); writeErr != nil || count != int(length) {
				return observed, audit, errors.Join(writeErr, io.ErrShortWrite)
			}
			if count, readErr := backing.ReadAt(buffer, offset); readErr != nil || count != len(buffer) {
				return observed, audit, errors.Join(readErr, io.ErrUnexpectedEOF)
			}
			for _, value := range buffer {
				if value != 0 {
					return observed, audit, fmt.Errorf("%w: materialized zero readback changed", errFixedVolumeUnsafe)
				}
			}
			offset += length
			audit.MaterializedBytes += length
		}
	}
	if err = ctx.Err(); err != nil {
		return observed, audit, err
	}
	if err = backing.Sync(); err != nil {
		return observed, audit, err
	}
	if err = ctx.Err(); err != nil {
		return observed, audit, err
	}
	first, err := fixedPoolInitializedMap(backing, identity.BackingBytes)
	if err != nil {
		return observed, audit, err
	}
	firstIdentity, err := fixedVolumePreparationIdentity(backing, identity)
	if err != nil {
		return observed, audit, err
	}
	if err = ctx.Err(); err != nil {
		return firstIdentity, audit, err
	}
	if err = backing.Sync(); err != nil {
		return firstIdentity, audit, err
	}
	second, err := fixedPoolInitializedMap(backing, identity.BackingBytes)
	if err != nil {
		return firstIdentity, audit, err
	}
	finalIdentity, err := fixedVolumePreparationIdentity(backing, identity)
	if err != nil {
		return firstIdentity, audit, err
	}
	if err = ctx.Err(); err != nil {
		return finalIdentity, audit, err
	}
	if first != second || firstIdentity != finalIdentity || audit.UnwrittenBytes != audit.MaterializedBytes {
		return finalIdentity, audit, fmt.Errorf("%w: initialized preparation mapping is not stable", errFixedVolumeUnsafe)
	}
	finalIdentity.BackingMapSHA256 = second
	audit.FinalMapSHA256 = second
	return finalIdentity, audit, nil
}

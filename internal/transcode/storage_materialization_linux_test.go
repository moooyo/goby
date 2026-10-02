//go:build linux

package transcode

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// These fixtures exercise actual kernel file mappings without creating a
// provider, loop, mount or pretend hard-domain activation capability.
func materializationFile(t *testing.T, size int64) (*os.File, fixedVolumeIdentity) {
	t.Helper()
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		t.Skip("root-owned preparation file fixture requires UID/GID 0")
	}
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "prepared.ext4"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if err = unix.Fallocate(int(file.Fd()), 0, 0, size); err != nil {
		t.Skipf("kernel preallocation prerequisite unavailable: %v", err)
	}
	if err = fixedVolumeInitializeBacking(context.Background(), file, size); err != nil {
		t.Fatal(err)
	}
	if err = file.Sync(); err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err = unix.Fstat(int(file.Fd()), &stat); err != nil {
		t.Fatal(err)
	}
	digest, err := fixedPoolInitializedMap(file, size)
	if err != nil {
		t.Skipf("strict initialized kernel FIEMAP prerequisite unavailable: %v", err)
	}
	return file, fixedVolumeIdentity{BackingDevice: uint64(stat.Dev), BackingInode: stat.Ino,
		BackingBytes: size, BackingObservedBytes: size, BackingAllocatedBytes: stat.Blocks * 512,
		BackingAllowanceBytes: size + 1<<20, BackingMapSHA256: digest, BlockSize: fixedVolumeBlockSize}
}

func materializationZeroRange(t *testing.T, file *os.File, offset, length int64) {
	t.Helper()
	if err := unix.Fallocate(int(file.Fd()), unix.FALLOC_FL_ZERO_RANGE|unix.FALLOC_FL_KEEP_SIZE, offset, length); err != nil {
		t.Skipf("kernel ZERO_RANGE prerequisite unavailable: %v", err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
}

func TestFixedVolumeMaterializationUsesActualUnwrittenZeroRanges(t *testing.T) {
	const size = int64(8 << 20)
	file, identity := materializationFile(t, size)
	metadata := bytes.Repeat([]byte{0x5a}, int(fixedVolumeBlockSize))
	if _, err := file.WriteAt(metadata, 0); err != nil {
		t.Fatal(err)
	}
	materializationZeroRange(t, file, fixedVolumeBlockSize, size-fixedVolumeBlockSize)
	extents, _, err := fixedVolumePreparationMap(context.Background(), file, size)
	if err != nil {
		t.Fatal(err)
	}
	var unwritten int64
	for _, extent := range extents {
		if extent.Flags&fixedVolumeUnwrittenFlag != 0 {
			unwritten += int64(extent.Length)
		}
	}
	if unwritten == 0 {
		t.Skip("this filesystem did not produce genuine UNWRITTEN mappings for ZERO_RANGE")
	}
	if _, err = fixedPoolInitializedMap(file, size); err == nil {
		t.Fatal("strict active map accepted the actual unwritten preparation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	finalIdentity, audit, err := fixedVolumeMaterializeFormattedBacking(ctx, file, identity)
	if err != nil {
		t.Fatal(err)
	}
	if audit.UnwrittenBytes != unwritten || audit.MaterializedBytes != unwritten || audit.InitialMapSHA256 != identity.BackingMapSHA256 ||
		!fixedVolumeHex(audit.PostFormatterMapSHA256, 32) || audit.FinalMapSHA256 != finalIdentity.BackingMapSHA256 {
		t.Fatal("actual zero conversion or final strict map was not fully observed")
	}
	if finalIdentity.BackingAllowanceBytes != identity.BackingAllowanceBytes || finalIdentity.BackingAllocatedBytes > identity.BackingAllowanceBytes {
		t.Fatal("whole durable allowance was shrunk or exceeded")
	}
	actual := make([]byte, len(metadata))
	if _, err = file.ReadAt(actual, 0); err != nil || !bytes.Equal(actual, metadata) {
		t.Fatal("written formatter metadata was modified")
	}
	zeros := make([]byte, 1<<20)
	for offset := fixedVolumeBlockSize; offset < size; {
		length := int64(len(zeros))
		if size-offset < length {
			length = size - offset
		}
		if _, err = file.ReadAt(zeros[:int(length)], offset); err != nil {
			t.Fatal(err)
		}
		for _, value := range zeros[:int(length)] {
			if value != 0 {
				t.Fatal("materialized range did not remain zero")
			}
		}
		offset += length
	}
	if err = fixedVolumeCheckBacking(file, finalIdentity, true); err != nil {
		t.Fatal(err)
	}
}

func TestFixedVolumeMaterializationRejectsCancellationAndActiveIdentity(t *testing.T) {
	file, identity := materializationFile(t, 64<<10)
	materializationZeroRange(t, file, 0, identity.BackingBytes)
	before, beforeDigest, err := fixedVolumePreparationMap(context.Background(), file, identity.BackingBytes)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err = fixedVolumeMaterializeFormattedBacking(ctx, file, identity); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled preparation was not rejected before data writes")
	}
	_, afterDigest, err := fixedVolumePreparationMap(context.Background(), file, identity.BackingBytes)
	if err != nil || beforeDigest != afterDigest || len(before) == 0 {
		t.Fatal("canceled preparation changed its actual mapping")
	}
	active := identity
	active.LoopDevice, active.MountID, active.RootDevice, active.RootInode = unix.Mkdev(7, 1), 1, unix.Mkdev(7, 1), 2
	if _, _, err = fixedVolumeMaterializeFormattedBacking(context.Background(), file, active); !errors.Is(err, errFixedVolumeUnsafe) {
		t.Fatal("active resource entered preparation materialization")
	}
}

func TestFixedVolumeMaterializationRejectsActualHoleAndInsufficientAllowance(t *testing.T) {
	file, identity := materializationFile(t, 64<<10)
	insufficient := identity
	insufficient.BackingAllowanceBytes = identity.BackingAllocatedBytes - 512
	if _, _, err := fixedVolumeMaterializeFormattedBacking(context.Background(), file, insufficient); !errors.Is(err, errFixedVolumeUnsafe) {
		t.Fatal("observed physical allocation escaped the durable whole allowance")
	}
	if err := unix.Fallocate(int(file.Fd()), unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, fixedVolumeBlockSize, fixedVolumeBlockSize); err != nil {
		t.Skipf("kernel hole fixture prerequisite unavailable: %v", err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixedVolumeMaterializeFormattedBacking(context.Background(), file, identity); err == nil {
		t.Fatal("actual sparse preparation was accepted")
	}
}

func TestFixedVolumeMaterializationChargesActualExtentMetadataGrowth(t *testing.T) {
	file, identity := materializationFile(t, 8<<20)
	initialAllocation := identity.BackingAllocatedBytes
	// Alternating initialized/unwritten pages require a real extent tree with
	// more than the inode's inline extent slots on supporting ext4 kernels.
	for index := int64(1); index < 64; index += 2 {
		materializationZeroRange(t, file, index*fixedVolumeBlockSize, fixedVolumeBlockSize)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Blocks*512 <= initialAllocation {
		t.Skip("this filesystem did not expose additional extent-tree allocation")
	}
	if stat.Blocks*512 > identity.BackingAllowanceBytes {
		t.Fatal("small real metadata fixture unexpectedly exceeded its full reservation")
	}
	final, audit, err := fixedVolumeMaterializeFormattedBacking(context.Background(), file, identity)
	if err != nil {
		t.Fatal(err)
	}
	if final.BackingAllowanceBytes != identity.BackingAllowanceBytes || final.BackingAllocatedBytes < final.BackingBytes ||
		final.BackingAllocatedBytes > final.BackingAllowanceBytes || audit.MaterializedBytes == 0 {
		t.Fatal("known real metadata growth was not retained inside the whole allowance")
	}
	if err = unix.Fstat(int(file.Fd()), &stat); err != nil || final.BackingAllocatedBytes != stat.Blocks*512 {
		t.Fatal("final physical allocation did not match the actual inode")
	}
}

func TestFixedVolumePreparationRejectsUnknownExtentStates(t *testing.T) {
	base := fixedPoolFiemapExtent{Physical: 4096, Length: 4096, Flags: fixedPoolFiemapLast | fixedVolumeUnwrittenFlag}
	if err := fixedVolumeValidatePreparationMap([]fixedPoolFiemapExtent{base}, 4096); err != nil {
		t.Fatal(err)
	}
	for _, flags := range []uint32{2, 4, 8, 0x80, 0x100, 0x200, 0x400, 0x1000, 0x2000, 0x80000000} {
		changed := base
		changed.Flags |= flags
		if err := fixedVolumeValidatePreparationMap([]fixedPoolFiemapExtent{changed}, 4096); err == nil {
			t.Fatalf("unknown/nonexclusive extent state 0x%x was accepted", flags)
		}
	}
	changed := base
	changed.Reserved64[0] = 1
	if err := fixedVolumeValidatePreparationMap([]fixedPoolFiemapExtent{changed}, 4096); err == nil {
		t.Fatal("reserved extent data was accepted")
	}
}

func TestFixedVolumePreparationReceiptRequiresFinalProofBeforeActivation(t *testing.T) {
	initial, final := strings.Repeat("a", 64), strings.Repeat("b", 64)
	receipt := fixedVolumeDiskReceipt{Version: 4, Stage: "materialize-intent", Bytes: 64 << 20,
		InitialBackingMapSHA256: initial, Identity: fixedVolumeIdentity{FilesystemUUID: "01234567-89ab-4def-8123-456789abcdef"},
		Materialization: fixedVolumeMaterializationAudit{InitialMapSHA256: initial}}
	if !fixedVolumePreparationReceiptValid(receipt) {
		t.Fatal("durable unactivated preparation intent was rejected")
	}
	receipt.Identity.LoopDevice = unix.Mkdev(7, 1)
	if fixedVolumePreparationReceiptValid(receipt) {
		t.Fatal("a partial intent acquired active authority")
	}
	receipt.Identity.LoopDevice = 0
	receipt.Stage, receipt.Identity.BackingMapSHA256 = "materialized", final
	receipt.Materialization.PostFormatterMapSHA256, receipt.Materialization.FinalMapSHA256 = initial, final
	if !fixedVolumePreparationReceiptValid(receipt) {
		t.Fatal("complete strict preparation receipt was rejected")
	}
	receipt.Materialization.MaterializedBytes = 4096
	if fixedVolumePreparationReceiptValid(receipt) {
		t.Fatal("unobserved zero conversion was accepted")
	}
}

func TestFixedVolumePreparationPersistenceFailureNeverAuthorizesActivation(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			file, identity := materializationFile(t, 64<<10)
			initial := identity.BackingMapSHA256
			materializationZeroRange(t, file, 0, identity.BackingBytes)
			_, beforeMap, err := fixedVolumePreparationMap(context.Background(), file, identity.BackingBytes)
			if err != nil {
				t.Fatal(err)
			}
			identity.BackingMapSHA256 = ""
			identity.FilesystemUUID = "01234567-89ab-4def-8123-456789abcdef"
			receipt := fixedVolumeDiskReceipt{Version: fixedVolumeReceiptVersion, Stage: "format-intent",
				Bytes: identity.BackingBytes, Identity: identity, InitialBackingMapSHA256: initial}
			fault := errors.New("durable receipt store failed")
			calls := 0
			var durable fixedVolumeDiskReceipt
			err = fixedVolumeCommitPreparation(context.Background(), file, &receipt, func(next fixedVolumeDiskReceipt) error {
				calls++
				if calls == failAt {
					return fault
				}
				durable = next
				return nil
			})
			if !errors.Is(err, fault) || calls != failAt || !fixedVolumeUnactivated(receipt.Identity) ||
				receipt.Identity.BackingAllowanceBytes != identity.BackingAllowanceBytes {
				t.Fatal("failed durability boundary acquired active authority or shrank the reservation")
			}
			if failAt == 1 {
				_, afterMap, mapErr := fixedVolumePreparationMap(context.Background(), file, identity.BackingBytes)
				if mapErr != nil || afterMap != beforeMap {
					t.Fatal("zero conversion occurred before the durable materialization intent")
				}
			} else {
				if durable.Stage != "materialize-intent" || durable.Identity.BackingMapSHA256 != "" ||
					receipt.Stage != "materialized" || !fixedVolumeHex(receipt.Identity.BackingMapSHA256, 32) {
					t.Fatal("final persistence fault silently promoted the durable partial receipt")
				}
				if _, mapErr := fixedPoolInitializedMap(file, identity.BackingBytes); mapErr != nil {
					t.Fatal("fixture did not reach the actual post-conversion persistence boundary")
				}
			}
		})
	}
}

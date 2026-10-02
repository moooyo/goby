//go:build linux

package transcode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func fixedPoolTestLimits() storageReservationLimits {
	return storageReservationLimits{Bytes: minStorageJournalBaseBytes + 2*(64<<20+fixedVolumeOwnerBytes+(1<<20)),
		Inodes: 64 + 2*(4096+fixedVolumeOwnerInodes), VolumeBytes: 64 << 20, VolumeInodes: 4096,
		OwnerBytes: fixedVolumeOwnerBytes + (1 << 20), OwnerInodes: fixedVolumeOwnerInodes,
		BaseBytes: minStorageJournalBaseBytes, BaseInodes: 64, MaxLeases: 2, MaxPins: 4}
}

func fixedPoolTestConfig() fixedBackingPoolConfig {
	return fixedBackingPoolConfig{SourceKind: fixedBackingPoolPartition, Expected: fixedBackingPoolIdentity{DeviceBytes: 256 << 20, FilesystemBytes: 240 << 20, FilesystemInodes: 8192},
		GlobalPhysicalBytes: (256 << 20) + 4096, GlobalOuterInodes: 8195, GlobalKernelInodes: 8195 + 2*4096,
		OutsideStaticBytes: 4096, OutsideStaticInodes: 3}
}

func TestFixedBackingPoolCountsDeviceOnceAndSeparatesInodes(t *testing.T) {
	limits, config := fixedPoolTestLimits(), fixedPoolTestConfig()
	budget, err := fixedBackingPoolCapacity(config, limits, 4096, 3)
	if err != nil {
		t.Fatal(err)
	}
	if budget.AdmittedLeases != 2 || budget.ReservationBytes != limits.BaseBytes+2*(limits.VolumeBytes+limits.OwnerBytes) ||
		budget.HardPhysicalBytes != config.Expected.DeviceBytes+4096 || budget.OuterInodes != limits.BaseInodes+2*limits.OwnerInodes ||
		budget.HardOuterInodes != 8195 || budget.HardKernelInodes != 8195+2*4096 {
		t.Fatalf("incorrect fixed-domain accounting: %+v", budget)
	}
	if budget.HardPhysicalBytes == config.Expected.DeviceBytes+budget.ReservationBytes+4096 {
		t.Fatal("images inside the fixed device were counted twice")
	}
	if budget.OuterInodes == limits.Inodes {
		t.Fatal("nested inode tables were confused with the outer table")
	}
}

func TestFixedBackingPoolAdmissionMaximumUsesAllBookLimits(t *testing.T) {
	limits, config := fixedPoolTestLimits(), fixedPoolTestConfig()
	limits.MaxLeases = 4
	limits.Bytes = limits.BaseBytes + 3*(limits.VolumeBytes+limits.OwnerBytes)
	limits.Inodes = limits.BaseInodes + limits.VolumeInodes + limits.OwnerInodes
	budget, err := fixedBackingPoolCapacity(config, limits, 4096, 3)
	if err != nil || budget.AdmittedLeases != 1 {
		t.Fatalf("book inode bound was ignored: %+v, %v", budget, err)
	}
}

func TestFixedBackingPoolInitializedLoopDoesNotDoubleChargeImage(t *testing.T) {
	config := fixedPoolTestConfig()
	config.SourceKind = fixedBackingPoolInitializedLoop
	config.Expected.BackingBytes = config.Expected.DeviceBytes
	config.Expected.BackingAllocatedBytes = config.Expected.DeviceBytes + 4096
	config.GlobalPhysicalBytes += 4096
	config.OutsideStaticInodes = 4
	config.GlobalOuterInodes++
	config.GlobalKernelInodes++
	budget, err := fixedBackingPoolCapacity(config, fixedPoolTestLimits(), 4096, 4)
	if err != nil || budget.HardPhysicalBytes != config.Expected.BackingAllocatedBytes+4096 {
		t.Fatalf("loop source counted incorrectly: %+v, %v", budget, err)
	}
	config.Expected.BackingAllocatedBytes--
	config.Expected.BackingAllocatedBytes -= 4096
	if _, err = fixedBackingPoolCapacity(config, fixedPoolTestLimits(), 4096, 4); !errors.Is(err, errFixedBackingPoolUnsafe) {
		t.Fatal("sparse or uncharged loop source accepted")
	}
}

func TestFixedBackingPoolFiemapRequiresFullyInitializedUniqueMap(t *testing.T) {
	valid := []fixedPoolFiemapExtent{{Logical: 0, Physical: 4096, Length: 4096}, {Logical: 4096, Physical: 16384, Length: 4096, Flags: fixedPoolFiemapLast}}
	digest, err := fixedPoolMapDigest(valid, 8192)
	if err != nil || !fixedVolumeHex(digest, sha256.Size) {
		t.Fatal("fully initialized map rejected")
	}
	for _, flag := range []uint32{2, 4, 8, 0x80, 0x100, 0x200, 0x400, 0x800, 0x1000, 0x2000, 0x80000000} {
		changed := append([]fixedPoolFiemapExtent(nil), valid...)
		changed[0].Flags = flag
		if _, err = fixedPoolMapDigest(changed, 8192); !errors.Is(err, errFixedBackingPoolUnsafe) {
			t.Fatalf("uncertain extent flags %#x accepted", flag)
		}
	}
	for _, changed := range [][]fixedPoolFiemapExtent{
		{{Logical: 4096, Physical: 4096, Length: 4096, Flags: fixedPoolFiemapLast}},
		{{Logical: 0, Physical: 0, Length: 8192, Flags: fixedPoolFiemapLast}},
		{{Logical: 0, Physical: 4096, Length: 4096, Flags: fixedPoolFiemapLast}},
		{{Logical: 0, Physical: 4096, Length: 4096}, {Logical: 4096, Physical: 4096, Length: 4096, Flags: fixedPoolFiemapLast}},
		{{Logical: 0, Physical: 4096, Length: 8192}},
	} {
		if _, err = fixedPoolMapDigest(changed, 8192); !errors.Is(err, errFixedBackingPoolUnsafe) {
			t.Fatal("hole, overlap or incomplete map accepted")
		}
	}
	changed := append([]fixedPoolFiemapExtent(nil), valid...)
	changed[1].Physical += 4096
	if shifted, err := fixedPoolMapDigest(changed, 8192); err != nil || shifted == digest {
		t.Fatal("physical map identity was omitted from digest")
	}
}

func TestFixedBackingPoolRejectsUnknownAndOverflowedCaps(t *testing.T) {
	cases := []struct {
		name   string
		change func(*fixedBackingPoolConfig)
	}{
		{"unknown byte cap", func(c *fixedBackingPoolConfig) { c.Expected.DeviceBytes = 0 }},
		{"unknown outer inode cap", func(c *fixedBackingPoolConfig) { c.GlobalOuterInodes = 0 }},
		{"unknown nested inode cap", func(c *fixedBackingPoolConfig) { c.GlobalKernelInodes = 0 }},
		{"book does not fit", func(c *fixedBackingPoolConfig) { c.Expected.FilesystemBytes = 64 << 20 }},
		{"outside blocks omitted", func(c *fixedBackingPoolConfig) { c.OutsideStaticBytes = 0 }},
		{"outside inode omitted", func(c *fixedBackingPoolConfig) { c.OutsideStaticInodes = 2 }},
		{"global byte cap exceeded", func(c *fixedBackingPoolConfig) { c.GlobalPhysicalBytes = 256 << 20 }},
		{"outer inode cap exceeded", func(c *fixedBackingPoolConfig) { c.GlobalOuterInodes = 8194 }},
		{"nested inode cap exceeded", func(c *fixedBackingPoolConfig) { c.GlobalKernelInodes-- }},
		{"overflow", func(c *fixedBackingPoolConfig) {
			c.Expected.DeviceBytes = math.MaxInt64
			c.GlobalPhysicalBytes = math.MaxInt64
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config := fixedPoolTestConfig()
			test.change(&config)
			if _, err := fixedBackingPoolCapacity(config, fixedPoolTestLimits(), 4096, 3); !errors.Is(err, errFixedBackingPoolUnsafe) {
				t.Fatal("unproved capacity was accepted")
			}
		})
	}
	if _, ok := checkedFixedPoolMultiply(math.MaxInt64, 2); ok {
		t.Fatal("multiplication overflow accepted")
	}
	if _, ok := checkedFixedPoolSectorEnd(math.MaxUint64, 1); ok {
		t.Fatal("partition range overflow accepted")
	}
}

func TestFixedBackingPoolMountInfoRequiresExactMountedRoot(t *testing.T) {
	root := "/var/lib/goby-fixed-pools/example"
	identity := fixedBackingPoolIdentity{MountID: 42, DeviceNumber: unix.Mkdev(252, 1)}
	line := "42 30 252:1 / " + root + " rw,nosuid,nodev,noexec - ext4 /dev/vdb1 rw,errors=remount-ro\n"
	if err := fixedPoolParseMountInfo([]byte(line), root, identity); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []string{
		strings.Replace(line, "252:1", "252:2", 1), strings.Replace(line, " / ", " /subtree ", 1),
		strings.Replace(line, ",noexec", "", 1), strings.Replace(line, "ext4", "xfs", 1),
		strings.Replace(line, "rw,errors", "rw,discard,errors", 1), line + line, "",
	} {
		if !errors.Is(fixedPoolParseMountInfo([]byte(changed), root, identity), errFixedBackingPoolUnsafe) {
			t.Fatal("ambiguous or changed mount domain accepted")
		}
	}
}

func TestFixedBackingPoolSysfsNumbersFailClosed(t *testing.T) {
	if value, err := fixedPoolParseSysfsNumber([]byte("131072\n")); err != nil || value != 131072 {
		t.Fatal("valid kernel numeric metadata rejected")
	}
	for _, value := range []string{"", "1", "-1\n", "+1\n", "1 2\n", "18446744073709551616\n", "1\n2\n"} {
		if _, err := fixedPoolParseSysfsNumber([]byte(value)); !errors.Is(err, errFixedBackingPoolUnsafe) {
			t.Fatalf("untrusted numeric metadata accepted: %q", value)
		}
	}
}

func TestFixedBackingPoolMissingPrerequisitesCannotOpen(t *testing.T) {
	provider, err := openPooledFixedVolumeProvider(fixedVolumeConfig{Enabled: true}, fixedBackingPoolConfig{})
	if provider != nil || !errors.Is(err, errFixedVolumeUnavailable) {
		t.Fatal("missing actual pool authority unexpectedly opened")
	}
	if storageBackendReady {
		t.Fatal("pool allocation alone authorized production admission")
	}
}

func TestFixedVolumeV3MemoryPreparationCannotAttestPool(t *testing.T) {
	for _, issuer := range []any{nil, true, struct{}{}, &pooledFixedVolumeProvider{}} {
		preparation := &fixedPoolPreparation{issuer: issuer}
		if !errors.Is(preparation.validate(fixedVolumeConfig{}), errFixedVolumeUnavailable) {
			t.Fatal("a non-native preparation attested a pool")
		}
	}
	config := fixedVolumeConfig{Enabled: true, Ledger: &storageReservationLedger{},
		ExpectedRootToken: strings.Repeat("a", 32), Mke2fsSHA256: strings.Repeat("b", 64)}
	if provider, err := openFixedVolumeProvider(config); provider != nil || !errors.Is(err, errFixedVolumeUnavailable) {
		t.Fatal("direct primitive bypassed its native pool prerequisite")
	}
}

// The root owner supplies a prepared fixed-pool fixture and an immutable
// JSON config file. The pre-mount directory FD must be inherited deliberately
// into the already-built test binary. No test prepares or retires the pool.
// Verification is opt-in and must be scheduled separately from quiet workloads.
func TestFixedBackingPoolPrivilegedPreparedFixture(t *testing.T) {
	if os.Getenv("GOBY_FIXED_POOL_KERNEL_TEST") != "1" {
		t.Skip("explicit root-authorized prepared pool fixture is not enabled")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the explicitly enabled fixture requires root")
	}
	path := os.Getenv("GOBY_FIXED_POOL_CONFIG")
	file, err := fixedVolumeOpenAbsolute(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var stat unix.Stat_t
	if err = fixedVolumeOwnedRegular(file, &stat); err != nil || stat.Size <= 0 || stat.Size > 16<<10 {
		t.Fatal("fixture config must be an exact bounded root-owned file")
	}
	flags, err := unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil || flags&fixedVolumeImmutableFlag == 0 {
		t.Fatal("fixture config must be immutable")
	}
	var fixture struct {
		Volume fixedVolumeConfig
		Pool   fixedBackingPoolConfig
		Limits storageReservationLimits
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatal("fixture config contains trailing data")
	}
	fd, err := strconv.ParseUint(os.Getenv("GOBY_FIXED_POOL_COVERED_FD"), 10, 32)
	if err != nil || fd < 3 {
		t.Fatal("explicitly inherited pre-mount directory FD is required")
	}
	fixture.Pool.CoveredMountpoint = os.NewFile(uintptr(fd), "borrowed-pre-mount-pool-directory")
	if fixture.Pool.CoveredMountpoint == nil {
		t.Fatal("invalid borrowed pool descriptor")
	}
	defer fixture.Pool.CoveredMountpoint.Close()
	// This is an explicit setup-owner action in the receiving process. The
	// factory checks borrowed flags and never silently changes its caller's FD.
	borrowedFlags, err := unix.FcntlInt(fixture.Pool.CoveredMountpoint.Fd(), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = unix.FcntlInt(fixture.Pool.CoveredMountpoint.Fd(), unix.F_SETFD, borrowedFlags|unix.FD_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	allocated, err := fixedPoolAllocatedBytes(stat)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, io.NewSectionReader(file, 0, stat.Size)); err != nil {
		t.Fatal(err)
	}
	fixture.Pool.OutsideStatic = append(fixture.Pool.OutsideStatic, fixedBackingPoolStaticObject{Path: path,
		Device: uint64(stat.Dev), Inode: stat.Ino, AllocatedBytes: allocated, SHA256: hex.EncodeToString(hash.Sum(nil))})
	journal, err := openStorageReservationJournal(fixture.Pool.PoolRoot+"/journal", fixture.Volume.ExpectedRootToken)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := newStorageReservationLedger(fixture.Limits, journal)
	if err != nil {
		_ = journal.close()
		t.Fatal(err)
	}
	defer ledger.close()
	fixture.Volume.Ledger = ledger
	binary, binaryPath, binarySHA := fixedPoolPreparedFixtureExecutable(t, fixture.Pool, fixture.Volume.ExpectedRootToken, hex.EncodeToString(hash.Sum(nil)))
	defer binary.Close()
	var executable *fixedPoolExecutableCapability
	provider, err := openPooledFixedVolumeProviderPrepared(fixture.Volume, fixture.Pool, func(preparation *fixedPoolExecutablePreparation) error {
		var approveErr error
		executable, approveErr = preparation.approve(binary, binaryPath, binarySHA)
		return approveErr
	})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixedVolumeExercisePreparedKernelProvider(t, ctx, provider, ledger, fixture.Volume.ProvisionRoot, fixture.Volume.ExpectedRootToken, executable)
	if err = provider.Close(); err != nil {
		t.Fatal(err)
	}
	// The factory duplicates the borrowed descriptor; it cannot consume the
	// setup owner's authority or silently transfer its teardown obligation.
	if _, err = fixture.Pool.CoveredMountpoint.Stat(); err != nil {
		t.Fatal("factory closed the setup owner's borrowed descriptor")
	}
}

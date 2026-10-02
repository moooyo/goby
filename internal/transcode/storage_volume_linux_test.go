//go:build linux

package transcode

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func fixedVolumeTestIdentity() fixedVolumeIdentity {
	return fixedVolumeIdentity{ID: fixedVolumeLeaseID("0123456789abcdef0123456789abcdef", 1),
		ProviderToken: "abcdef0123456789abcdef0123456789", ProvisionRootDevice: 1, ProvisionRootInode: 2, LedgerRootDevice: 1, LedgerRootInode: 3,
		BootID: "01234567-89ab-4def-8123-456789abcdef", MountNamespaceDevice: 3, MountNamespaceInode: 4,
		NamespaceDevice: 1, NamespaceInode: 5, MountpointDevice: 1, MountpointInode: 6,
		BackingDevice: 1, BackingInode: 7, BackingBytes: 64 << 20, BackingObservedBytes: 64 << 20,
		BackingAllocatedBytes: (64 << 20) + 4096, BackingAllowanceBytes: (64 << 20) + (1 << 20), BackingMapSHA256: strings.Repeat("a", 64),
		LoopDevice: unix.Mkdev(7, 8), LoopNumber: 8, MountID: 9, RootDevice: unix.Mkdev(7, 8), RootInode: 2,
		FilesystemUUID: "fedcba98-7654-4321-8123-456789abcdef", FilesystemBytes: 60 << 20,
		FilesystemInodes: 4096, BlockSize: 4096}
}

func TestFixedVolumeIdentityRequiresWholePhysicalDomain(t *testing.T) {
	identity := fixedVolumeTestIdentity()
	if err := identity.validateReservation(64<<20, 4096, fixedVolumeOwnerBytes+(1<<20)); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*fixedVolumeIdentity)
	}{
		{"sparse backing", func(i *fixedVolumeIdentity) { i.BackingAllocatedBytes = (64 << 20) - 4096 }},
		{"metadata exceeds allowance", func(i *fixedVolumeIdentity) { i.BackingAllocatedBytes = i.BackingAllowanceBytes + 4096 }},
		{"missing initialized map", func(i *fixedVolumeIdentity) { i.BackingMapSHA256 = "" }},
		{"unknown file length", func(i *fixedVolumeIdentity) { i.BackingObservedBytes-- }},
		{"expanded backing", func(i *fixedVolumeIdentity) { i.BackingBytes += 4096 }},
		{"expanded filesystem", func(i *fixedVolumeIdentity) { i.FilesystemBytes = 65 << 20 }},
		{"expanded inode domain", func(i *fixedVolumeIdentity) { i.FilesystemInodes++ }},
		{"missing mount", func(i *fixedVolumeIdentity) { i.MountID = 0 }},
		{"wrong loop root", func(i *fixedVolumeIdentity) { i.RootDevice++ }},
		{"missing namespace", func(i *fixedVolumeIdentity) { i.MountNamespaceInode = 0 }},
		{"unknown boot", func(i *fixedVolumeIdentity) { i.BootID = "" }},
		{"zero UUID", func(i *fixedVolumeIdentity) { i.FilesystemUUID = "00000000-0000-0000-0000-000000000000" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed := identity
			test.change(&changed)
			if !errors.Is(changed.validate(64<<20, 4096), errFixedVolumeUnsafe) {
				t.Fatal("invalid physical domain was accepted")
			}
		})
	}
	if storageBackendReady {
		t.Fatal("allocation primitives do not prove writer or command confinement")
	}
}

func TestFixedVolumeV3ReservesExtentMetadataSeparately(t *testing.T) {
	identity := fixedVolumeTestIdentity()
	if identity.BackingAllocatedBytes <= identity.BackingBytes {
		t.Fatal("fixture did not include extent metadata")
	}
	if err := identity.validateReservation(64<<20, 4096, fixedVolumeOwnerBytes+(1<<20)); err != nil {
		t.Fatal(err)
	}
	if err := identity.validateReservation(64<<20, 4096, fixedVolumeOwnerBytes); !errors.Is(err, errFixedVolumeUnsafe) {
		t.Fatal("control allowance was consumed twice for backing metadata")
	}
	identity.BackingAllowanceBytes++
	if err := identity.validateReservation(64<<20, 4096, fixedVolumeOwnerBytes+(1<<20)); !errors.Is(err, errFixedVolumeUnsafe) {
		t.Fatal("undurable metadata allowance was accepted")
	}
}

func fixedVolumeTestSuperblock() []byte {
	data := make([]byte, 1024)
	binary.LittleEndian.PutUint32(data[:4], 4096)
	binary.LittleEndian.PutUint32(data[4:8], (64<<20)/4096)
	binary.LittleEndian.PutUint32(data[24:28], 2)
	binary.LittleEndian.PutUint16(data[56:58], 0xef53)
	binary.LittleEndian.PutUint16(data[88:90], 256)
	binary.LittleEndian.PutUint32(data[92:96], 0x4)
	binary.LittleEndian.PutUint32(data[96:100], 0x40|0x80)
	for index := 104; index < 120; index++ {
		data[index] = byte(index)
	}
	return data
}

func TestFixedVolumeSuperblockRequiresExplicitExt4Totals(t *testing.T) {
	data := fixedVolumeTestSuperblock()
	superblock, err := fixedVolumeParseSuperblock(data)
	if err != nil || superblock.Bytes != 64<<20 || superblock.Inodes != 4096 || superblock.BlockSize != 4096 {
		t.Fatalf("unexpected filesystem totals: %+v, %v", superblock, err)
	}
	for _, offset := range []int{24, 56, 89, 92, 96} {
		corrupt := append([]byte(nil), data...)
		corrupt[offset] = 0
		if _, err := fixedVolumeParseSuperblock(corrupt); !errors.Is(err, errFixedVolumeUnsafe) {
			t.Fatalf("corrupt required metadata at %d was accepted", offset)
		}
	}
	binary.LittleEndian.PutUint32(data[336:340], 0xffffffff)
	if _, err := fixedVolumeParseSuperblock(data); !errors.Is(err, errFixedVolumeUnsafe) {
		t.Fatal("overflowed filesystem byte domain was accepted")
	}
}

func TestFixedVolumeSuperblockReadUsesOrdinaryFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "superblock-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = file.WriteAt(fixedVolumeTestSuperblock(), 1024); err != nil {
		t.Fatal(err)
	}
	superblock, err := fixedVolumeReadSuperblock(file)
	if err != nil || superblock.Inodes != 4096 {
		t.Fatalf("ordinary file metadata parsing failed: %+v, %v", superblock, err)
	}
}

func TestFixedVolumeDisabledAndMissingAuthorityFailClosed(t *testing.T) {
	if provider, err := openFixedVolumeProvider(fixedVolumeConfig{}); provider != nil || !errors.Is(err, errFixedVolumeUnavailable) {
		t.Fatal("disabled provider unexpectedly opened")
	}
	if provider, err := openFixedVolumeProvider(fixedVolumeConfig{Enabled: true}); provider != nil || !errors.Is(err, errFixedVolumeUnavailable) {
		t.Fatal("missing root marker and formatter approval unexpectedly opened")
	}
	for _, id := range []string{"", "..", "one/two", "one\x00two", strings.Repeat("a", 129)} {
		if _, err := fixedVolumeNamespaceName(id, 1); err == nil {
			t.Fatalf("unsafe lease component %q was accepted", id)
		}
	}
	if _, err := fixedVolumeNamespaceName("safe", 0); err == nil {
		t.Fatal("zero lease serial was accepted")
	}
	cause := context.Canceled
	failure := &fixedVolumeProvisionFailure{Stage: "mount-intent", Cause: cause}
	if !errors.Is(failure, cause) || !errors.Is(failure, errFixedVolumeRetained) {
		t.Fatal("partial allocation cancellation hid retained ownership")
	}
}

func TestFixedVolumeCleanupProofBindsExactIdentityAndRevision(t *testing.T) {
	disk := fixedVolumeDiskReceipt{Version: fixedVolumeReceiptVersion, ID: "0123456789abcdef0123456789abcdef",
		Serial: 1, Stage: "retired", Bytes: 64 << 20, Inodes: 4096, Identity: fixedVolumeTestIdentity(),
		BackingRemoved: true, MountpointRemoved: true, DeleteRevision: 3, RetirementIdentity: fixedVolumeTestIdentity()}
	proof := fixedVolumeCleanupProof(disk)
	if proof == ([32]byte{}) {
		t.Fatal("empty proof")
	}
	changed := disk
	changed.DeleteRevision++
	if fixedVolumeCleanupProof(changed) == proof {
		t.Fatal("delete revision was omitted from proof")
	}
	changed = disk
	changed.Identity.BackingInode++
	if fixedVolumeCleanupProof(changed) == proof {
		t.Fatal("backing identity was omitted from proof")
	}
	if fixedVolumeReceiptStage("unknown") {
		t.Fatal("unknown ownership stage was accepted")
	}
}

// The old standalone fixture cannot bypass the v3 backing-pool prerequisite.
func TestFixedVolumePrivilegedKernelFixture(t *testing.T) {
	if os.Getenv("GOBY_FIXED_VOLUME_KERNEL_TEST") != "1" {
		t.Skip("explicit root-authorized fixed-volume fixture is not enabled")
	}
	t.Skip("v3 requires TestFixedBackingPoolPrivilegedPreparedFixture and an actually verified owned pool")
}

// Only the explicitly enabled prepared-pool fixture invokes this kernel work.
// Failure retains the durable ledger, exact receipts and whole reservation.
func fixedVolumeExercisePreparedKernelProvider(t *testing.T, ctx context.Context, provider fixedVolumeProvider, ledger *storageReservationLedger, root, generation string, executable *fixedPoolExecutableCapability) {
	t.Helper()
	ledger.mu.Lock()
	limits := ledger.state.Limits
	ledger.mu.Unlock()
	if ledger.snapshot().RecoveryRequired {
		t.Fatal("existing fixture ownership requires root recovery before a new run")
	}
	lease, err := ledger.reserve("0123456789abcdef0123456789abcdef", generation)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := lease.authorizeProvision()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := provider.Provision(ctx, permit)
	if err != nil {
		t.Fatalf("owned provisioning failed and must retain its receipt: %v", err)
	}
	if err = lease.activate(identity); err != nil {
		t.Fatal(err)
	}
	workspacePath := filepath.Join(root, identity.ID, fixedVolumeWorkspaceName)
	workspace, err := os.Open(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	if err = unix.Fchown(int(workspace.Fd()), 65534, 65534); err != nil {
		_ = workspace.Close()
		t.Fatal(err)
	}
	if err = unix.Fchmod(int(workspace.Fd()), 0o700); err != nil {
		_ = workspace.Close()
		t.Fatal(err)
	}
	fixedVolumeRunKernelWriter(t, ctx, workspace, identity, "bytes", executable)
	fixedVolumeRunKernelWriter(t, ctx, workspace, identity, "inodes", executable)
	if err = workspace.Close(); err != nil {
		t.Fatal(err)
	}
	if err = lease.writersDrained(); err != nil {
		t.Fatal(err)
	}
	if err = lease.terminalHandled(true); err != nil {
		t.Fatal(err)
	}
	retire, err := lease.beginRetirement()
	if err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	charge := ledger.snapshot()
	if receipt, err := provider.Retire(ctx, retire); receipt != nil || !errors.Is(err, errFixedVolumeRetained) {
		_ = held.Close()
		t.Fatalf("busy volume retirement did not retain ownership: %v", err)
	}
	if ledger.snapshot().ReservedBytes != charge.ReservedBytes {
		_ = held.Close()
		t.Fatal("busy retirement released storage capacity")
	}
	if err = held.Close(); err != nil {
		t.Fatal(err)
	}
	receipt, err := provider.Retire(ctx, retire)
	if err != nil {
		t.Fatal(err)
	}
	if err = lease.confirmRetirement(receipt); err != nil {
		t.Fatal(err)
	}
	if err = lease.completeRetirement(receipt); !errors.Is(err, ErrStorageUnsafe) {
		t.Fatal("unacknowledged final metadata released its reservation")
	}
	if err = provider.AcknowledgeRetirement(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	if err = lease.completeRetirement(receipt); err != nil {
		t.Fatal(err)
	}
	if got := ledger.snapshot(); got.Leases != 0 || got.ReservedBytes != limits.BaseBytes || got.ReservedInodes != limits.BaseInodes {
		t.Fatalf("successful exact cleanup retained unexpected charge: %+v", got)
	}
}

func fixedVolumeRunKernelWriter(t *testing.T, ctx context.Context, workspace *os.File, identity fixedVolumeIdentity, mode string, executable *fixedPoolExecutableCapability) {
	t.Helper()
	use, err := executable.duplicate()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := use.close(); err != nil {
			t.Errorf("retain unresolved approved writer executable: %v", err)
		}
	}()
	command := exec.CommandContext(ctx, "/proc/self/fd/4", "-test.run=^TestFixedVolumeKernelWriterHelper$")
	command.ExtraFiles = []*os.File{workspace, use.file}
	// Go changes cwd before remapping ExtraFiles. This inherited parent FD is
	// still live at chdir; child FD 3 is valid only after the subsequent shuffle.
	command.Dir = fixedVolumeFDPath(workspace)
	command.Env = []string{"PATH=/nonexistent", "HOME=/nonexistent", "TMPDIR=/proc/self/fd/3", "TMP=/proc/self/fd/3",
		"TEMP=/proc/self/fd/3", "XDG_CACHE_HOME=/proc/self/fd/3", "GOBY_FIXED_VOLUME_WRITER=" + mode,
		"GOBY_FIXED_VOLUME_DEVICE=" + strconv.FormatUint(identity.RootDevice, 10),
		"GOBY_FIXED_VOLUME_BYTES=" + strconv.FormatInt(identity.BackingBytes, 10),
		"GOBY_FIXED_VOLUME_INODES=" + strconv.FormatInt(identity.FilesystemInodes, 10)}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534, Groups: []uint32{}}, Pdeathsig: syscall.SIGKILL}
	output := &fixedVolumeBoundedOutput{}
	command.Stdout, command.Stderr = output, output
	if err = use.start(command); err != nil {
		// A returned Process remains an actual Wait obligation even when Start
		// reported an error. Unknown ownership still retains the descriptor use.
		if command.Process != nil {
			err = errors.Join(err, use.wait())
		}
		t.Fatalf("approved fixed-volume %s writer start failed: %v", mode, err)
	}
	if err = use.wait(); err != nil {
		t.Fatalf("capability-free fixed-volume %s fixture failed: %v\n%s", mode, err, output.data)
	}
}

func TestFixedVolumeKernelWriterHelper(t *testing.T) {
	mode := os.Getenv("GOBY_FIXED_VOLUME_WRITER")
	if mode == "" {
		t.Skip("private root-authorized writer helper")
	}
	if os.Geteuid() != 65534 || os.Getegid() != 65534 {
		t.Fatal("writer credentials were not dropped")
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"CapInh", "CapPrm", "CapEff", "CapAmb"} {
		if !strings.Contains(string(status), field+":\t0000000000000000\n") {
			t.Fatalf("writer retained %s authority", field)
		}
	}
	device, err := strconv.ParseUint(os.Getenv("GOBY_FIXED_VOLUME_DEVICE"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err = unix.Stat(".", &stat); err != nil || uint64(stat.Dev) != device || stat.Ino != 2 ||
		stat.Uid != 65534 || stat.Gid != 65534 || stat.Mode&0o7777 != 0o700 {
		t.Fatal("writer cwd is not its borrowed volume root")
	}
	switch mode {
	case "bytes":
		limit, err := strconv.ParseInt(os.Getenv("GOBY_FIXED_VOLUME_BYTES"), 10, 64)
		if err != nil || limit <= 0 || limit > 64<<20 {
			t.Fatal("invalid fixture byte bound")
		}
		file, err := os.OpenFile("byte-exhaustion", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		chunk := make([]byte, 1<<20)
		var written int64
		for written <= limit {
			n, writeErr := file.Write(chunk)
			written += int64(n)
			if errors.Is(writeErr, syscall.ENOSPC) {
				if written == 0 || written > limit {
					t.Fatal("invalid dense byte exhaustion outcome")
				}
				if err = file.Close(); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove("byte-exhaustion"); err != nil {
					t.Fatal(err)
				}
				if err = unix.Syncfs(3); err != nil {
					t.Fatal(err)
				}
				return
			}
			if writeErr != nil {
				_ = file.Close()
				t.Fatal(writeErr)
			}
		}
		_ = file.Close()
		t.Fatal("writer exceeded the fixed filesystem byte domain")
	case "inodes":
		limit, err := strconv.ParseInt(os.Getenv("GOBY_FIXED_VOLUME_INODES"), 10, 64)
		if err != nil || limit <= 0 || limit > 4096 {
			t.Fatal("invalid fixture inode bound")
		}
		for index := int64(0); index <= limit; index++ {
			file, createErr := os.OpenFile(fmt.Sprintf("inode-%d", index), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if errors.Is(createErr, syscall.ENOSPC) {
				if index == 0 {
					t.Fatal("inode fixture did not create any file")
				}
				var fs unix.Statfs_t
				if err = unix.Fstatfs(3, &fs); err != nil || fs.Files != uint64(limit) || fs.Ffree != 0 {
					t.Fatal("ENOSPC did not establish the fixed inode ceiling")
				}
				return
			}
			if createErr != nil {
				t.Fatal(createErr)
			}
			if err = file.Close(); err != nil {
				t.Fatal(err)
			}
		}
		t.Fatal("writer exceeded the fixed filesystem inode domain")
	default:
		t.Fatal("unknown private writer fixture mode")
	}
}

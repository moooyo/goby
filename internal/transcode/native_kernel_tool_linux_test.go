//go:build linux

package transcode

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	nativeKernelToolSelector       = "goby-native-kernel-tool-v1"
	nativeKernelToolSentinel       = "GOBY_NATIVE_SENTINEL_V1\nGOBY_NATIVE_TAIL_V1\n"
	nativeKernelToolPolicyOK       = "GOBY_NATIVE_POLICY_OK_V1\n"
	nativeKernelToolCancelReady    = "GOBY_NATIVE_CANCEL_READY_V1\n"
	nativeKernelToolOutsideInitial = "GOBY_NATIVE_OUTSIDE_SENTINEL_V1\n"
	nativeKernelToolOutsideBad     = "GOBY_NATIVE_OUTSIDE_WRITE_UNEXPECTED_V1\n"
	nativeKernelToolWorkspaceLeaf  = "native-kernel-tool-policy.bin"
)

// TestNativeKernelToolHelper is an actual executable entry, selected only by:
// -test.run=^TestNativeKernelToolHelper$ -- goby-native-kernel-tool-v1 MODE ARGS
// The ordinary test suite skips it. A fixture must route the same immutable test
// binary through its real executable capability and native launcher; this entry
// does not create a Domain, reopen os.Executable, inject environment or substitute
// a mock process. It exits directly to preserve exact bounded output without the
// Go test harness's PASS line. It never claims whole-backend readiness.
func TestNativeKernelToolHelper(t *testing.T) {
	var arguments []string
	for index, argument := range os.Args {
		if argument == "--" && index+1 < len(os.Args) && os.Args[index+1] == nativeKernelToolSelector {
			arguments = os.Args[index+2:]
			break
		}
	}
	if arguments == nil {
		t.Skip("this executable entry requires the actual native fixture selector")
	}
	if err := runNativeKernelTool(arguments); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "native kernel tool helper failed:", err)
		os.Exit(93)
	}
	os.Exit(0)
}

func runNativeKernelTool(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("missing helper mode")
	}
	if err := nativeKernelToolCheckIdentity(); err != nil {
		return err
	}
	if err := nativeKernelToolCheckStdout(); err != nil {
		return err
	}
	switch arguments[0] {
	case "sentinel":
		if len(arguments) != 1 {
			return errors.New("unexpected sentinel arguments")
		}
		// This entire payload is less than the minimum pipe capacity. The owned
		// child can exit before an actual TemplatePipes consumer reads its tail.
		return nativeKernelToolWrite(nativeKernelToolSentinel)
	case "policy":
		return nativeKernelToolCheckPolicy(arguments[1:])
	case "cancel":
		if len(arguments) != 1 {
			return errors.New("unexpected cancellation arguments")
		}
		if err := nativeKernelToolWrite(nativeKernelToolCancelReady); err != nil {
			return err
		}
		// Real cancellation must terminate this owned process. No environment
		// switch, synthetic context result or test-only cancellation bypass exists.
		// Sleep keeps the runtime alive without creating another file or consumer.
		for {
			time.Sleep(time.Second)
		}
	default:
		return errors.New("unknown helper mode")
	}
}

func nativeKernelToolCheckIdentity() error {
	ruid, euid, suid := unix.Getresuid()
	rgid, egid, sgid := unix.Getresgid()
	groups, err := unix.Getgroups()
	if err != nil || ruid != 65534 || euid != ruid || suid != ruid || rgid != 65534 || egid != rgid || sgid != rgid || len(groups) != 0 {
		return errors.New("actual tool credentials differ from the configured unprivileged identity")
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	sets := [2]unix.CapUserData{}
	if unix.Capget(&header, &sets[0]) != nil || sets != [2]unix.CapUserData{} {
		return errors.New("actual tool capability sets are not empty")
	}
	capabilityEnd := false
	for capability := 0; capability < 64; capability++ {
		bounding, err := unix.PrctlRetInt(unix.PR_CAPBSET_READ, uintptr(capability), 0, 0, 0)
		if errors.Is(err, unix.EINVAL) {
			if capability <= unix.CAP_LAST_CAP {
				return errors.New("kernel capability boundary is incomplete")
			}
			capabilityEnd = true
			break
		}
		ambient, ambientErr := unix.PrctlRetInt(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_IS_SET, uintptr(capability), 0, 0)
		if err != nil || ambientErr != nil || bounding != 0 || ambient != 0 {
			return errors.New("actual tool bounding or ambient capabilities are not empty")
		}
	}
	if !capabilityEnd {
		return errors.New("kernel capability space exceeds the checked bound")
	}
	nnp, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	seccomp, seccompErr := unix.PrctlRetInt(unix.PR_GET_SECCOMP, 0, 0, 0, 0)
	if err != nil || seccompErr != nil || nnp != 1 || seccomp != 2 {
		return errors.New("actual tool privilege and syscall restrictions are absent")
	}
	return nil
}

func nativeKernelToolCheckStdout() error {
	var stat unix.Stat_t
	access, err := unix.FcntlInt(1, unix.F_GETFL, 0)
	if err != nil || unix.Fstat(1, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFIFO || access&unix.O_ACCMODE != unix.O_WRONLY {
		return errors.New("helper stdout is not the actual owned write pipe")
	}
	return nil
}

// Policy args are data-dev, data-inode, data-mount-id, source-dev, source-inode,
// source-mount-id, each in canonical positive decimal. The only payload is FD3:
// a dedicated, non-immutable UID/GID 65534 mode0600 regular sentinel in the
// already whole-charged control filesystem. It is fixture data, never a broker,
// cgroup, setup receipt or other authority/control descriptor. The root fixture
// must verify its non-immutable state and unchanged bytes after the actual join.
func nativeKernelToolCheckPolicy(arguments []string) error {
	if len(arguments) != 6 {
		return errors.New("policy requires six actual filesystem identity fields")
	}
	var identities [6]uint64
	for index, argument := range arguments {
		value, err := strconv.ParseUint(argument, 10, 64)
		if err != nil || value == 0 || strconv.FormatUint(value, 10) != argument {
			return errors.New("policy identity field is not canonical positive decimal")
		}
		identities[index] = value
	}
	if identities[1] != 2 || identities[0] == identities[3] {
		return errors.New("policy data root or separate control filesystem identity is invalid")
	}
	data, err := unix.Open(".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return errors.New("actual data working directory is inaccessible")
	}
	var dataStat unix.Stat_t
	var dataFS unix.Statfs_t
	dataErr := nativeKernelToolCheckObject(data, identities[0], identities[1], identities[2])
	statErr, fsErr := unix.Fstat(data, &dataStat), unix.Fstatfs(data, &dataFS)
	closeErr := unix.Close(data)
	if dataErr != nil || statErr != nil || fsErr != nil || closeErr != nil || dataStat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		dataStat.Uid != 65534 || dataStat.Gid != 65534 || dataStat.Mode&07777 != 0700 || dataFS.Type != unix.EXT4_SUPER_MAGIC {
		return errors.New("actual working directory is not the assigned whole ext4 root")
	}
	var sourceStat unix.Stat_t
	access, err := unix.FcntlInt(3, unix.F_GETFL, 0)
	if err != nil || access&unix.O_ACCMODE != unix.O_RDONLY || access&unix.O_PATH != 0 ||
		nativeKernelToolCheckObject(3, identities[3], identities[4], identities[5]) != nil || unix.Fstat(3, &sourceStat) != nil ||
		sourceStat.Mode&unix.S_IFMT != unix.S_IFREG || sourceStat.Uid != 65534 || sourceStat.Gid != 65534 ||
		sourceStat.Mode&07777 != 0600 || sourceStat.Nlink != 1 || sourceStat.Size != int64(len(nativeKernelToolOutsideInitial)) {
		return errors.New("actual source payload is not the dedicated writable-by-owner sentinel")
	}
	content := make([]byte, len(nativeKernelToolOutsideInitial))
	if n, err := unix.Pread(3, content, 0); err != nil || n != len(content) || !bytes.Equal(content, []byte(nativeKernelToolOutsideInitial)) {
		return errors.New("actual source sentinel bytes differ from the fixture contract")
	}
	workspace, err := unix.Open(nativeKernelToolWorkspaceLeaf, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC, 0600)
	if err != nil {
		return errors.New("assigned data filesystem rejected a normal output write")
	}
	output := []byte("GOBY_NATIVE_WORKSPACE_WRITE_V1\n")
	n, writeErr := unix.Write(workspace, output)
	syncErr, outputCloseErr := unix.Fsync(workspace), unix.Close(workspace)
	if writeErr != nil || n != len(output) || syncErr != nil || outputCloseErr != nil {
		return errors.New("assigned data output did not complete its actual write and close")
	}
	outside, err := unix.Open("/proc/self/fd/3", unix.O_WRONLY|unix.O_CLOEXEC, 0)
	if err == nil {
		// An unexpected success writes only to the already charged control
		// sentinel. It cannot create an ordinary /tmp or outside-cache path.
		_, _ = unix.Write(outside, []byte(nativeKernelToolOutsideBad))
		_ = unix.Close(outside)
		return errors.New("the native policy unexpectedly reopened its readonly source for writing")
	}
	if !errors.Is(err, unix.EACCES) && !errors.Is(err, unix.EPERM) {
		return errors.New("source writable reopen failed for an unrelated reason")
	}
	return nativeKernelToolWrite(nativeKernelToolPolicyOK)
}

func nativeKernelToolCheckObject(fd int, device, inode, mountID uint64) error {
	var stat unix.Stat_t
	var statx unix.Statx_t
	if unix.Fstat(fd, &stat) != nil || stat.Dev != device || stat.Ino != inode ||
		unix.Statx(fd, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_INO|unix.STATX_MNT_ID, &statx) != nil ||
		statx.Mask&(unix.STATX_INO|unix.STATX_MNT_ID) != unix.STATX_INO|unix.STATX_MNT_ID ||
		statx.Ino != inode || statx.Mnt_id != mountID || unix.Mkdev(statx.Dev_major, statx.Dev_minor) != device {
		return errors.New("actual descriptor identity differs from the expected kernel object")
	}
	return nil
}

func nativeKernelToolWrite(value string) error {
	if len(value) > 512 {
		return errors.New("helper output exceeds its bounded pipe contract")
	}
	n, err := os.Stdout.Write([]byte(value))
	if err != nil || n != len(value) {
		return errors.New("helper could not write its complete bounded output")
	}
	return nil
}

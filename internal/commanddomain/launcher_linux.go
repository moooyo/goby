//go:build linux && amd64

package commanddomain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

const launcherFailureExit = 126

// RunLauncher must be called only by the dedicated helper executable. The
// broker starts that helper atomically in its owned cgroup, with sealed role
// descriptors and a clean helper environment. No untrusted tool code executes
// until this locked thread has dropped privileges and installed both policies.
func RunLauncher() int {
	runtime.LockOSThread()
	if err := runLauncher(); err != nil {
		// Do not print paths, arguments, descriptor contents or inherited values.
		_, _ = fmt.Fprintln(os.Stderr, "command launcher refused the command")
	}
	return launcherFailureExit
}

func runLauncher() error {
	if unix.Geteuid() != 0 || unix.Getuid() != 0 {
		return ErrLauncherConfig
	}
	config, err := readLauncherConfig()
	if err != nil {
		return err
	}
	if err = checkLauncherDomain(config); err != nil {
		return err
	}
	if err = checkLauncherWorkspace(config.Workspace); err != nil {
		return err
	}
	if err = checkLauncherTool(config.ToolSHA256); err != nil {
		return err
	}
	if err = checkLauncherDescriptors(config); err != nil {
		return err
	}
	// Mark every descriptor, including unknown runtime or inherited controls,
	// close-on-exec. Only explicitly remapped payloads become inheritable.
	if err = unix.CloseRange(3, ^uint(0), 4 /* CLOSE_RANGE_CLOEXEC */); err != nil {
		return err
	}
	toolFD, err := unix.FcntlInt(LauncherToolFD, unix.F_DUPFD_CLOEXEC, 1024)
	if err != nil {
		return err
	}
	defer unix.Close(toolFD)
	if err = unix.Fchdir(LauncherWorkspaceFD); err != nil {
		return err
	}
	if err = dropLauncherCredentials(config); err != nil {
		return err
	}
	if err = armLauncherParentDeath(config.BrokerPID); err != nil {
		return err
	}
	if err = installLauncherLandlock(config); err != nil {
		return err
	}
	if err = remapLauncherPayloads(config); err != nil {
		return err
	}
	if err = checkLauncherHardwareSources(config, true); err != nil {
		return err
	}
	// Descriptor setup, including the original high executable FD, is complete
	// before lowering NOFILE. Existing descriptors remain owned through exec.
	if err = applyLauncherLimits(config); err != nil {
		return err
	}
	if err = installLauncherSeccomp(config.Hardware); err != nil {
		return err
	}
	args := make([]string, 1, len(config.Args)+1)
	args[0] = "goby-approved-tool"
	args = append(args, config.Args...)
	// ELF execution can close its executable descriptor during exec. Scripts
	// cannot safely use this arrangement and were rejected by tool validation.
	return unix.Exec("/proc/self/fd/"+strconv.Itoa(toolFD), args, launcherEnvironment(config.Hardware))
}

func readLauncherConfig() (LauncherConfig, error) {
	var stat unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(LauncherConfigFD, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 0 ||
		stat.Uid != 0 || stat.Size < 1 || stat.Size > MaxLauncherConfigBytes ||
		unix.Fstatfs(LauncherConfigFD, &fs) != nil || fs.Type != unix.TMPFS_MAGIC {
		return LauncherConfig{}, ErrLauncherConfig
	}
	seals, err := unix.FcntlInt(LauncherConfigFD, unix.F_GET_SEALS, 0)
	const required = unix.F_SEAL_SEAL | unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE
	if err != nil || seals&required != required {
		return LauncherConfig{}, ErrLauncherConfig
	}
	data := make([]byte, int(stat.Size))
	read := 0
	for read < len(data) {
		n, err := unix.Pread(LauncherConfigFD, data[read:], int64(read))
		if err != nil || n == 0 {
			return LauncherConfig{}, ErrLauncherConfig
		}
		read += n
	}
	return DecodeLauncherConfig(data)
}

func launcherReadBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, ErrLauncherConfig
	}
	return data, nil
}

func checkLauncherDomain(config LauncherConfig) error {
	boot, err := launcherReadBounded("/proc/sys/kernel/random/boot_id", 37)
	if err != nil || string(boot) != config.BrokerBootID+"\n" {
		return ErrLauncherConfig
	}
	fd, err := unix.Open("/proc/thread-self/ns/mnt", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	var stat unix.Stat_t
	var fs unix.Statfs_t
	checkErr := unix.Fstat(fd, &stat)
	fsErr := unix.Fstatfs(fd, &fs)
	_ = unix.Close(fd)
	if checkErr != nil || fsErr != nil || fs.Type != unix.NSFS_MAGIC ||
		uint64(stat.Dev) != config.MountNamespace.Device || stat.Ino != config.MountNamespace.Inode {
		return ErrLauncherConfig
	}
	cgroup, err := launcherReadBounded("/proc/self/cgroup", 4096)
	if err != nil || string(cgroup) != "0::"+config.CgroupPath+"\n" {
		return ErrLauncherConfig
	}
	return nil
}

func launcherStatIdentity(fd int, device, inode, mountID uint64) (unix.Stat_t, error) {
	var stat unix.Stat_t
	var statx unix.Statx_t
	if unix.Fstat(fd, &stat) != nil || uint64(stat.Dev) != device || stat.Ino != inode ||
		unix.Statx(fd, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_INO|unix.STATX_MNT_ID, &statx) != nil ||
		statx.Mask&(unix.STATX_INO|unix.STATX_MNT_ID) != unix.STATX_INO|unix.STATX_MNT_ID ||
		statx.Mnt_id != mountID || statx.Ino != inode || unix.Mkdev(statx.Dev_major, statx.Dev_minor) != device {
		return stat, ErrLauncherConfig
	}
	return stat, nil
}

func checkLauncherWorkspace(identity LauncherWorkspaceIdentity) error {
	stat, err := launcherStatIdentity(LauncherWorkspaceFD, identity.Device, identity.Inode, identity.MountID)
	var fs unix.Statfs_t
	if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Ino != 2 ||
		unix.Fstatfs(LauncherWorkspaceFD, &fs) != nil || fs.Type != unix.EXT4_SUPER_MAGIC || fs.Flags&unix.ST_RDONLY != 0 {
		return ErrLauncherConfig
	}
	// An ext4 inode number alone is not enough: the exact mount must expose
	// the filesystem root rather than a bind-mounted subdirectory.
	info, err := launcherReadBounded("/proc/self/mountinfo", 1<<20)
	if err != nil {
		return err
	}
	matched := 0
	for _, line := range strings.Split(string(info), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[0] != strconv.FormatUint(identity.MountID, 10) {
			continue
		}
		if fields[3] != "/" || fields[2] != fmt.Sprintf("%d:%d", unix.Major(identity.Device), unix.Minor(identity.Device)) {
			return ErrLauncherConfig
		}
		separator := -1
		for index, field := range fields {
			if field == "-" {
				separator = index
				break
			}
		}
		if separator < 6 || separator+3 >= len(fields) || fields[separator+1] != "ext4" {
			return ErrLauncherConfig
		}
		matched++
	}
	if matched != 1 {
		return ErrLauncherConfig
	}
	return nil
}

func checkLauncherTool(digest string) error {
	var stat unix.Stat_t
	flags, err := unix.FcntlInt(LauncherToolFD, unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY || unix.Fstat(LauncherToolFD, &stat) != nil ||
		stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Mode&(0022|unix.S_ISUID|unix.S_ISGID) != 0 || stat.Mode&0111 == 0 ||
		stat.Size < 64 || stat.Size > 512<<20 {
		return ErrLauncherConfig
	}
	if _, capabilityErr := unix.Fgetxattr(LauncherToolFD, "security.capability", nil); capabilityErr == nil ||
		(!errors.Is(capabilityErr, unix.ENODATA) && !errors.Is(capabilityErr, unix.EOPNOTSUPP)) {
		// A privilege-associated executable can clear the rearmed parent-death
		// setting during secureexec. It is outside the approved tool contract.
		return ErrLauncherConfig
	}
	header := make([]byte, 64)
	if n, err := unix.Pread(LauncherToolFD, header, 0); err != nil || n != len(header) ||
		string(header[:4]) != "\x7fELF" || header[4] != 2 || header[5] != 1 || header[6] != 1 ||
		(header[16] != 2 && header[16] != 3) || header[17] != 0 || header[18] != 62 || header[19] != 0 {
		return ErrLauncherConfig
	}
	hash := sha256.New()
	buffer := make([]byte, 32<<10)
	for offset := int64(0); offset < stat.Size; {
		chunk := min(int64(len(buffer)), stat.Size-offset)
		n, err := unix.Pread(LauncherToolFD, buffer[:int(chunk)], offset)
		if err != nil || n == 0 {
			return ErrLauncherConfig
		}
		_, _ = hash.Write(buffer[:n])
		offset += int64(n)
	}
	var after unix.Stat_t
	if unix.Fstat(LauncherToolFD, &after) != nil || after.Dev != stat.Dev || after.Ino != stat.Ino ||
		after.Size != stat.Size || after.Mode != stat.Mode || after.Uid != stat.Uid || after.Gid != stat.Gid ||
		after.Nlink != stat.Nlink || after.Mtim != stat.Mtim || after.Ctim != stat.Ctim ||
		hex.EncodeToString(hash.Sum(nil)) != digest {
		return ErrLauncherConfig
	}
	// Root-installed tools and their loader/libraries are trusted provisioning
	// inputs. The broker must pin their approved immutable installation; this
	// digest check alone cannot authorize concurrent root-side replacement.
	return nil
}

func checkLauncherDescriptors(config LauncherConfig) error {
	for fd := 0; fd < 3; fd++ {
		if err := checkLauncherStream(fd, fd != 0, config.Workspace); err != nil {
			return err
		}
	}
	for index, payload := range config.Payload {
		fd := LauncherPayloadFDBase + index
		var stat unix.Stat_t
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err != nil || flags&unix.O_PATH != 0 || unix.Fstat(fd, &stat) != nil {
			return ErrLauncherConfig
		}
		typeBits := stat.Mode & unix.S_IFMT
		if !payload.Writable {
			if flags&unix.O_ACCMODE != unix.O_RDONLY || (typeBits != unix.S_IFREG && typeBits != unix.S_IFIFO) {
				return ErrLauncherConfig
			}
		} else if payload.Role == "progress-output" {
			if flags&unix.O_ACCMODE != unix.O_WRONLY || typeBits != unix.S_IFIFO {
				return ErrLauncherConfig
			}
		} else if payload.Role == "workspace-output" {
			var statx unix.Statx_t
			if flags&unix.O_ACCMODE == unix.O_RDONLY || typeBits != unix.S_IFREG || uint64(stat.Dev) != config.Workspace.Device ||
				unix.Statx(fd, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &statx) != nil ||
				statx.Mask&unix.STATX_MNT_ID == 0 || statx.Mnt_id != config.Workspace.MountID {
				return ErrLauncherConfig
			}
		} else {
			return ErrLauncherConfig
		}
	}
	for index, hardware := range config.HardwareDevices {
		fd := LauncherPayloadFDBase + len(config.Payload) + index
		stat, err := launcherStatIdentity(fd, hardware.Device, hardware.Inode, hardware.MountID)
		if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFCHR || stat.Uid != 0 || uint64(stat.Rdev) != hardware.Rdev {
			return ErrLauncherConfig
		}
		opened, err := unix.Open(hardware.Path, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		pathStat, pathErr := launcherStatIdentity(opened, hardware.Device, hardware.Inode, hardware.MountID)
		_ = unix.Close(opened)
		if pathErr != nil || pathStat.Mode&unix.S_IFMT != unix.S_IFCHR || pathStat.Rdev != stat.Rdev {
			return ErrLauncherConfig
		}
	}
	return checkLauncherHardwareSources(config, false)
}

func checkLauncherHardwareSources(config LauncherConfig, remapped bool) error {
	if config.Hardware == "none" {
		return nil
	}
	check := func(fd int) error {
		var stat unix.Stat_t
		if unix.Fstat(fd, &stat) != nil {
			return ErrLauncherConfig
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFREG {
			var fs unix.Statfs_t
			// Seccomp cannot resolve an FD's filesystem for a driver-family
			// IOCTL. Only generic VFS and ext4 dispatch have been audited against
			// this allowlist. Other filesystems need an audited source bridge.
			if unix.Fstatfs(fd, &fs) != nil || fs.Type != unix.EXT4_SUPER_MAGIC {
				return ErrLauncherConfig
			}
		}
		return nil
	}
	if err := check(0); err != nil {
		return err
	}
	for index, payload := range config.Payload {
		if payload.Writable {
			continue
		}
		fd := LauncherPayloadFDBase + index
		if remapped {
			fd = 3 + index
		}
		if err := check(fd); err != nil {
			return err
		}
	}
	return nil
}

func checkLauncherStream(fd int, writable bool, workspace LauncherWorkspaceIdentity) error {
	var stat unix.Stat_t
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_PATH != 0 || unix.Fstat(fd, &stat) != nil ||
		(!writable && flags&unix.O_ACCMODE != unix.O_RDONLY) || (writable && flags&unix.O_ACCMODE == unix.O_RDONLY) {
		return ErrLauncherConfig
	}
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFIFO:
		if writable && flags&unix.O_ACCMODE != unix.O_WRONLY {
			return ErrLauncherConfig
		}
	case unix.S_IFREG:
		if writable {
			var statx unix.Statx_t
			if uint64(stat.Dev) != workspace.Device ||
				unix.Statx(fd, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &statx) != nil ||
				statx.Mask&unix.STATX_MNT_ID == 0 || statx.Mnt_id != workspace.MountID {
				return ErrLauncherConfig
			}
		}
	case unix.S_IFCHR:
		// os/exec legitimately supplies /dev/null for an unset standard stream.
		// Match the actual root-owned node, rather than accepting arbitrary
		// preopened character devices that would bypass Landlock IOCTL checks.
		var null unix.Stat_t
		if unix.Stat("/dev/null", &null) != nil || null.Mode&unix.S_IFMT != unix.S_IFCHR ||
			null.Uid != 0 || stat.Uid != 0 || uint64(stat.Rdev) != unix.Mkdev(1, 3) ||
			stat.Dev != null.Dev || stat.Ino != null.Ino || stat.Rdev != null.Rdev {
			return ErrLauncherConfig
		}
	default:
		return ErrLauncherConfig
	}
	return nil
}

func dropLauncherCredentials(config LauncherConfig) error {
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		return err
	}
	lastData, err := launcherReadBounded("/proc/sys/kernel/cap_last_cap", 4)
	last, parseErr := strconv.Atoi(strings.TrimSpace(string(lastData)))
	if err != nil || parseErr != nil || last < unix.CAP_LAST_CAP || last > 63 {
		return ErrLauncherConfig
	}
	for capability := 0; capability <= last; capability++ {
		if err = unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(capability), 0, 0, 0); err != nil {
			return err
		}
	}
	// Lock NOROOT and NO_CAP_AMBIENT_RAISE without KEEP_CAPS or preserving
	// capabilities across the explicit real/effective/saved UID transition.
	if err = unix.Prctl(unix.PR_SET_SECUREBITS, 1|2|64|128, 0, 0, 0); err != nil {
		return err
	}
	groups := make([]int, len(config.Groups))
	for index, group := range config.Groups {
		groups[index] = int(group)
	}
	if err = unix.Setgroups(groups); err != nil {
		return err
	}
	if err = unix.Setresgid(int(config.GID), int(config.GID), int(config.GID)); err != nil {
		return err
	}
	if err = unix.Setresuid(int(config.UID), int(config.UID), int(config.UID)); err != nil {
		return err
	}
	capHeader := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	capData := [2]unix.CapUserData{}
	if err = unix.Capset(&capHeader, &capData[0]); err != nil {
		return err
	}
	if err = unix.Capget(&capHeader, &capData[0]); err != nil || capData != [2]unix.CapUserData{} ||
		unix.Getuid() != int(config.UID) || unix.Geteuid() != int(config.UID) ||
		unix.Getgid() != int(config.GID) || unix.Getegid() != int(config.GID) {
		return ErrLauncherConfig
	}
	ruid, euid, suid := unix.Getresuid()
	rgid, egid, sgid := unix.Getresgid()
	actualGroups, groupsErr := unix.Getgroups()
	if groupsErr != nil || ruid != int(config.UID) || euid != ruid || suid != ruid ||
		rgid != int(config.GID) || egid != rgid || sgid != rgid || len(actualGroups) != len(groups) {
		return ErrLauncherConfig
	}
	for _, actual := range actualGroups {
		found := false
		for _, expected := range groups {
			found = found || actual == expected
		}
		if !found {
			return ErrLauncherConfig
		}
	}
	if err = unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return err
	}
	return unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)
}

func armLauncherParentDeath(brokerPID uint32) error {
	// Linux v6.12 commit_creds clears pdeath_signal when effective or filesystem
	// IDs change. Rearm after every credential operation and close the parent
	// exit/reparent race using the identity supplied by the root broker.
	if uint32(unix.Getppid()) != brokerPID || unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0) != nil {
		return ErrLauncherConfig
	}
	signal := new(int32)
	var signalPin runtime.Pinner
	signalPin.Pin(signal)
	defer signalPin.Unpin()
	if unix.Prctl(unix.PR_GET_PDEATHSIG, uintptr(unsafe.Pointer(signal)), 0, 0, 0) != nil ||
		*signal != int32(unix.SIGKILL) || uint32(unix.Getppid()) != brokerPID {
		return ErrLauncherConfig
	}
	// This is a leader lifetime guard. It does not prove the complete cgroup
	// is empty or authorize retirement, storage release or broker join.
	return nil
}

func launcherLandlockCall(number uintptr, a, b, c uintptr) (uintptr, error) {
	result, _, errno := unix.Syscall6(number, a, b, c, 0, 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return result, nil
}

func installLauncherLandlock(config LauncherConfig) error {
	abi, err := launcherLandlockCall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if err != nil || abi < RequiredLauncherLandlockABI {
		return ErrLauncherConfig
	}
	// ABI 6 retains all filesystem rights through device IOCTL and adds the
	// actual SIGNAL domain scope. Recycled numeric PIDs cannot authorize a
	// descendant to signal another job outside this inherited Landlock scope.
	handled := uint64((1 << 16) - 1)
	attribute := &unix.LandlockRulesetAttr{Access_fs: handled, Scoped: unix.LANDLOCK_SCOPE_SIGNAL}
	// The syscall wrapper takes uintptr arguments. Pin objects explicitly so
	// an intervening Go stack growth cannot invalidate their native addresses.
	var attributePin runtime.Pinner
	attributePin.Pin(attribute)
	defer attributePin.Unpin()
	ruleset, err := launcherLandlockCall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(attribute)), unsafe.Sizeof(*attribute), 0)
	if err != nil {
		return err
	}
	defer unix.Close(int(ruleset))
	add := func(fd int, rights uint64) error {
		// The kernel path-beneath structure is packed and exactly 12 bytes.
		path := &unix.LandlockPathBeneathAttr{Allowed_access: rights, Parent_fd: int32(fd)}
		var pathPin runtime.Pinner
		pathPin.Pin(path)
		defer pathPin.Unpin()
		_, err := launcherLandlockCall(unix.SYS_LANDLOCK_ADD_RULE, ruleset, unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(path)))
		return err
	}
	workspaceRights := handled &^ uint64(unix.LANDLOCK_ACCESS_FS_IOCTL_DEV|unix.LANDLOCK_ACCESS_FS_EXECUTE|
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR|unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK|unix.LANDLOCK_ACCESS_FS_MAKE_SOCK)
	if err = add(LauncherWorkspaceFD, workspaceRights); err != nil {
		return err
	}
	if err = add(LauncherToolFD, unix.LANDLOCK_ACCESS_FS_EXECUTE|unix.LANDLOCK_ACCESS_FS_READ_FILE); err != nil {
		return err
	}
	// These root-installed read-only inputs support dynamic ELF loaders,
	// codecs, hardware libraries and font configuration. There is no root or
	// home-directory rule, and no inherited environment selects another path.
	readPaths := []string{"/lib", "/lib64", "/usr/lib", "/usr/lib64", "/usr/share", "/etc/ld.so.cache", "/etc/fonts"}
	if config.Hardware != "none" {
		// Driver discovery reads kernel-owned topology. These rules grant no
		// writes or IOCTLs; device access still requires the exact leaf grant.
		readPaths = append(readPaths, "/sys", "/proc/driver/nvidia", "/proc/devices")
	}
	for _, path := range readPaths {
		fd, openErr := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) {
			continue
		}
		if openErr != nil {
			return openErr
		}
		var stat unix.Stat_t
		err = unix.Fstat(fd, &stat)
		rights := uint64(unix.LANDLOCK_ACCESS_FS_READ_FILE)
		if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
			rights |= unix.LANDLOCK_ACCESS_FS_READ_DIR
			if path == "/lib" || path == "/lib64" || path == "/usr/lib" || path == "/usr/lib64" {
				rights |= unix.LANDLOCK_ACCESS_FS_EXECUTE
			}
		}
		if err == nil && stat.Uid == 0 && stat.Mode&0022 == 0 {
			err = add(fd, rights)
		} else {
			err = ErrLauncherConfig
		}
		_ = unix.Close(fd)
		if err != nil {
			return err
		}
	}
	for index, payload := range config.Payload {
		if payload.Writable {
			continue
		}
		fd := LauncherPayloadFDBase + index
		var stat unix.Stat_t
		if err = unix.Fstat(fd, &stat); err != nil {
			return err
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFREG {
			if err = add(fd, unix.LANDLOCK_ACCESS_FS_READ_FILE); err != nil {
				return err
			}
		}
	}
	var stdin unix.Stat_t
	if err = unix.Fstat(0, &stdin); err != nil {
		return err
	}
	if stdin.Mode&unix.S_IFMT == unix.S_IFREG {
		if err = add(0, unix.LANDLOCK_ACCESS_FS_READ_FILE); err != nil {
			return err
		}
	}
	for index := range config.HardwareDevices {
		fd := LauncherPayloadFDBase + len(config.Payload) + index
		if err = add(fd, unix.LANDLOCK_ACCESS_FS_READ_FILE|unix.LANDLOCK_ACCESS_FS_WRITE_FILE|unix.LANDLOCK_ACCESS_FS_IOCTL_DEV); err != nil {
			return err
		}
	}
	_, err = launcherLandlockCall(unix.SYS_LANDLOCK_RESTRICT_SELF, ruleset, 0, 0)
	return err
}

func remapLauncherPayloads(config LauncherConfig) error {
	for _, fd := range []int{LauncherConfigFD, LauncherWorkspaceFD, LauncherToolFD} {
		if err := unix.Close(fd); err != nil {
			return err
		}
	}
	for index := range config.Payload {
		source, target := LauncherPayloadFDBase+index, 3+index
		if err := unix.Dup3(source, target, 0); err != nil {
			return err
		}
		if err := unix.Close(source); err != nil {
			return err
		}
	}
	for index := range config.HardwareDevices {
		if err := unix.Close(LauncherPayloadFDBase + len(config.Payload) + index); err != nil {
			return err
		}
	}
	return nil
}

func launcherEnvironment(hardware string) []string {
	env := []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "AV_LOG_FORCE_NOCOLOR=1",
		"TMPDIR=/proc/self/cwd", "TMP=/proc/self/cwd", "TEMP=/proc/self/cwd", "HOME=/proc/self/cwd",
		"XDG_CACHE_HOME=/proc/self/cwd", "XDG_CONFIG_HOME=/proc/self/cwd", "XDG_DATA_HOME=/proc/self/cwd"}
	if hardware == "nvidia" {
		env = append(env, "CUDA_CACHE_DISABLE=1", "CUDA_CACHE_PATH=/proc/self/cwd", "CUDA_DEVICE_ORDER=PCI_BUS_ID")
	}
	return env
}

func installLauncherSeccomp(hardware string) error {
	load := func(offset uint32) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: offset}
	}
	ret := func(value uint32) unix.SockFilter { return unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: value} }
	jeq := func(value uint32, yes, no uint8) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jt: yes, Jf: no, K: value}
	}
	deny := uint32(unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM))
	filter := []unix.SockFilter{load(4), jeq(unix.AUDIT_ARCH_X86_64, 1, 0), ret(unix.SECCOMP_RET_KILL_PROCESS), load(0),
		{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, Jt: 0, Jf: 1, K: 0x40000000}, ret(unix.SECCOMP_RET_KILL_PROCESS)}
	// Linux v6.12 ends this native syscall table at mseal (462). New syscall
	// additions require source review rather than acquiring ambient authority
	// automatically, including newer metadata setters that bypass Landlock.
	filter = append(filter, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K, Jt: 0, Jf: 1, K: 463}, ret(deny))
	// clone3 cannot be inspected through its userspace pointer in classic BPF.
	// The broker's atomic clone already happened; ENOSYS preserves libc's
	// ordinary clone fallback for tool threads without permitting new namespaces.
	filter = append(filter, jeq(unix.SYS_CLONE3, 0, 1), ret(unix.SECCOMP_RET_ERRNO|uint32(unix.ENOSYS)))
	for _, number := range []uint32{
		unix.SYS_UNSHARE, unix.SYS_SETNS, unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT, unix.SYS_CHROOT,
		unix.SYS_OPEN_BY_HANDLE_AT, unix.SYS_NAME_TO_HANDLE_AT, unix.SYS_MOVE_MOUNT, unix.SYS_OPEN_TREE,
		unix.SYS_FSOPEN, unix.SYS_FSCONFIG, unix.SYS_FSMOUNT, unix.SYS_FSPICK, unix.SYS_MOUNT_SETATTR,
		unix.SYS_PTRACE, unix.SYS_PIDFD_GETFD, unix.SYS_PIDFD_OPEN, unix.SYS_PIDFD_SEND_SIGNAL,
		unix.SYS_PROCESS_VM_WRITEV, unix.SYS_PROCESS_VM_READV, unix.SYS_KCMP,
		unix.SYS_BPF, unix.SYS_PERF_EVENT_OPEN, unix.SYS_USERFAULTFD, unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_REGISTER, unix.SYS_IO_URING_ENTER,
		// Anonymous shmem/secret regular files cannot escape assigned ext4
		// accounting. The root-created sealed protocol memfd predates this
		// filter and has its independent 16 KiB protocol bound. Pipes/eventfd
		// are transient I/O, not a claim of an overall memory allocation bound.
		unix.SYS_MEMFD_CREATE, unix.SYS_MEMFD_SECRET,
		unix.SYS_SOCKET, unix.SYS_SOCKETPAIR, unix.SYS_CONNECT, unix.SYS_BIND, unix.SYS_LISTEN, unix.SYS_ACCEPT, unix.SYS_ACCEPT4,
		unix.SYS_ADD_KEY, unix.SYS_REQUEST_KEY, unix.SYS_KEYCTL, unix.SYS_INIT_MODULE, unix.SYS_FINIT_MODULE, unix.SYS_DELETE_MODULE,
		unix.SYS_KEXEC_LOAD, unix.SYS_KEXEC_FILE_LOAD, unix.SYS_REBOOT, unix.SYS_SWAPON, unix.SYS_SWAPOFF,
		unix.SYS_QUOTACTL, unix.SYS_QUOTACTL_FD, unix.SYS_LSM_SET_SELF_ATTR,
		unix.SYS_FANOTIFY_INIT, unix.SYS_FANOTIFY_MARK, unix.SYS_MKNOD, unix.SYS_MKNODAT,
		unix.SYS_CHMOD, unix.SYS_FCHMOD, unix.SYS_FCHMODAT, unix.SYS_FCHMODAT2,
		unix.SYS_CHOWN, unix.SYS_FCHOWN, unix.SYS_LCHOWN, unix.SYS_FCHOWNAT,
		// Tool-side credential changes can clear the rearmed parent-death
		// signal. Configured identity changes happened before this filter.
		unix.SYS_SETUID, unix.SYS_SETGID, unix.SYS_SETREUID, unix.SYS_SETREGID,
		unix.SYS_SETRESUID, unix.SYS_SETRESGID, unix.SYS_SETFSUID, unix.SYS_SETFSGID, unix.SYS_SETGROUPS,
		unix.SYS_UTIME, unix.SYS_UTIMES, unix.SYS_FUTIMESAT, unix.SYS_UTIMENSAT,
		unix.SYS_SETXATTR, unix.SYS_LSETXATTR, unix.SYS_FSETXATTR, unix.SYS_REMOVEXATTR, unix.SYS_LREMOVEXATTR, unix.SYS_FREMOVEXATTR,
		unix.SYS_SETXATTRAT, unix.SYS_REMOVEXATTRAT, unix.SYS_OPEN_TREE_ATTR,
		469, // file_setattr: reject the newer metadata setter explicitly.
		unix.SYS_SHMGET, unix.SYS_SHMAT, unix.SYS_SHMDT, unix.SYS_SHMCTL, unix.SYS_MSGGET, unix.SYS_MSGSND, unix.SYS_MSGRCV, unix.SYS_MSGCTL,
		unix.SYS_SEMGET, unix.SYS_SEMOP, unix.SYS_SEMCTL, unix.SYS_SEMTIMEDOP, unix.SYS_TKILL,
		unix.SYS_RT_SIGQUEUEINFO, unix.SYS_RT_TGSIGQUEUEINFO,
	} {
		filter = append(filter, jeq(number, 0, 1), ret(deny))
	}
	// clone flags are argument 0. Reject high-word flags and all namespace or
	// alternate-parent/tracer requests while retaining ordinary pthread clones.
	mask := uint32(unix.CLONE_NEWNS | unix.CLONE_NEWCGROUP | unix.CLONE_NEWUTS | unix.CLONE_NEWIPC |
		unix.CLONE_NEWUSER | unix.CLONE_NEWPID | unix.CLONE_NEWNET | unix.CLONE_PTRACE | unix.CLONE_PARENT)
	filter = append(filter, jeq(unix.SYS_CLONE, 0, 7), load(20), jeq(0, 1, 0), ret(deny), load(16),
		unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, Jt: 0, Jf: 1, K: mask}, ret(deny), ret(unix.SECCOMP_RET_ALLOW))
	// The tool cannot explicitly clear its rearmed leader lifetime guard.
	filter = append(filter, jeq(unix.SYS_PRCTL, 0, 4), load(16), jeq(unix.PR_SET_PDEATHSIG, 0, 1),
		ret(deny), ret(unix.SECCOMP_RET_ALLOW))
	// kill/tgkill are governed by the actual inherited Landlock SIGNAL scope,
	// rather than a numeric helper PID that can be recycled after leader reap.
	// Queued signals and tkill remain denied above as additional restrictions.
	// prlimit64 accepts only pid 0, the kernel's current-process selector. This
	// remains correct in descendants and cannot address a recycled leader PID.
	// Overall resource bounds still require broker-installed limits.
	filter = append(filter, jeq(unix.SYS_PRLIMIT64, 0, 4), load(16), jeq(0, 0, 1),
		ret(unix.SECCOMP_RET_ALLOW), ret(deny))
	// IOCTL is deny-by-default. Read-only inherited sources do not establish
	// metadata immutability: Linux v6.12 fs/ioctl.c and fs/ext4/ioctl.c permit
	// setters such as FS_IOC_SETVERSION without an O_WRONLY descriptor. A
	// finite filesystem-family denylist therefore cannot establish the scope.
	ioctlBranch := len(filter)
	filter = append(filter, jeq(unix.SYS_IOCTL, 0, 0), load(24))
	for _, command := range []uint32{0x5401 /* TCGETS */, 0x5413 /* TIOCGWINSZ */, 0x541b /* FIONREAD */} {
		filter = append(filter, jeq(command, 0, 1), ret(unix.SECCOMP_RET_ALLOW))
	}
	if hardware == "nvidia" {
		// NVIDIA 580.126.09 uvm_linux_ioctl.h and uvm_ioctl.h define these
		// process-local runtime operations exactly. Legacy raw commands 1/2,
		// testing, MPS sessions and TOOLS process-memory operations are omitted.
		// No listed command collides with generic or ext4 filesystem setters in
		// Linux v6.12. Unknown UVM extensions fail closed pending source review.
		for _, command := range []uint32{0x30000001, 0x30000002,
			23, 24, 25, 26, 27, 28, 29, 30, 31, 33, 34, 35, 37, 38, 39, 40, 41,
			42, 43, 44, 45, 46, 47, 51, 53, 54, 55, 65, 66, 68, 69, 70, 71, 72, 73, 74, 75, 78, 80,
		} {
			filter = append(filter, jeq(command, 0, 1), ret(unix.SECCOMP_RET_ALLOW))
		}
	}
	if hardware == "vaapi" || hardware == "nvidia" {
		filter = append(filter,
			unix.SockFilter{Code: unix.BPF_ALU | unix.BPF_RSH | unix.BPF_K, K: 8},
			unix.SockFilter{Code: unix.BPF_ALU | unix.BPF_AND | unix.BPF_K, K: 0xff})
		// DRM's official ioctl family is 'd'; QSV derives from the same DRM
		// render-node grant as VAAPI. NVIDIA's official NV_IOCTL_MAGIC is 'F'.
		// Neither family is a generic/ext4 filesystem setter in Linux v6.12.
		// Landlock IOCTL_DEV restricts newly opened character devices to the
		// actual root-approved leaf nodes; all preopened hardware FDs were closed.
		family := uint32(0x64)
		if hardware == "nvidia" {
			family = 0x46
		}
		filter = append(filter, jeq(family, 0, 1), ret(unix.SECCOMP_RET_ALLOW))
	}
	filter = append(filter, ret(deny))
	// The IOCTL branch is bounded by the fixed allowlist, so a classic BPF
	// uint8 jump reaches exactly the final non-IOCTL allow instruction.
	skip := len(filter) - ioctlBranch - 1
	if skip > 255 {
		return ErrLauncherConfig
	}
	filter[ioctlBranch].Jf = uint8(skip)
	filter = append(filter, ret(unix.SECCOMP_RET_ALLOW))
	program := &unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	var programPin runtime.Pinner
	programPin.Pin(program)
	programPin.Pin(&filter[0])
	defer programPin.Unpin()
	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, 0, uintptr(unsafe.Pointer(program)))
	if errno != 0 {
		return errno
	}
	return nil
}

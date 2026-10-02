//go:build linux

package commanddomain

import (
	"os"

	"golang.org/x/sys/unix"
)

// Scope construction checks borrowed inputs without creating a leaf, modifying
// a parent controller or reopening an executable pathname. Real leaf creation
// repeats native validation and registers actual executable-capability borrows.
func initializeCommandScope(s *commandScopeState, config Config) error {
	issuer, err := executableIssuerNow()
	if err != nil {
		return err
	}
	abi, _, policyErr := unix.Syscall6(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION, 0, 0, 0)
	if policyErr != 0 || abi < RequiredLauncherLandlockABI {
		return ErrUnavailable
	}
	s.issuer = issuer
	if s.config.Hardware == "" {
		s.config.Hardware = "none"
	}
	if err := checkCommandScopePolicy(s); err != nil {
		return err
	}
	if err := checkCommandScopeDirectory(config.Parent, config.ParentIdentity, config.UID, config.GID, true); err != nil {
		return err
	}
	if err := checkCommandScopeDirectory(config.Workspace, config.WorkspaceIdentity, config.UID, config.GID, false); err != nil {
		return err
	}
	s.config.Parent, err = duplicate(config.Parent)
	if err != nil {
		return err
	}
	s.config.Workspace, err = duplicate(config.Workspace)
	if err != nil {
		return err
	}
	return checkCommandScopeLive(s)
}

func checkCommandScopePolicy(s *commandScopeState) error {
	config := s.config
	probe := LauncherConfig{Version: LauncherConfigVersion, ToolSHA256: config.Launcher.SHA256,
		Workspace: LauncherWorkspaceIdentity(config.WorkspaceIdentity), UID: config.UID, GID: config.GID,
		Groups: config.Groups, Hardware: config.Hardware, HardwareDevices: config.HardwareDevices,
		CgroupPath: "/probe", BrokerPID: uint32(s.issuer.pid), BrokerBootID: s.issuer.boot,
		MountNamespace: s.issuer.namespace, Args: []string{"--help"}}
	if ValidateLauncherConfig(probe) != nil {
		return ErrUnsafe
	}
	if err := checkCommandScopeCapability(s.launcher, config.Launcher, s.issuer); err != nil {
		return err
	}
	for index, approval := range config.Tools {
		for previous := 0; previous < index; previous++ {
			if config.Tools[previous].Path == approval.Path {
				return ErrUnsafe
			}
		}
		if err := checkCommandScopeCapability(s.tools[index], approval, s.issuer); err != nil {
			return err
		}
	}
	return nil
}

func checkCommandScopeCapability(capability *ExecutableCapability, approval ApprovedExecutable, issuer executableCapabilityIssuer) error {
	if capability == nil || capability.executableCapabilityState == nil {
		return ErrUnsafe
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	if capability.file == nil {
		return ErrClosed
	}
	if capability.approval != approval || capability.issuer != issuer {
		return ErrUnsafe
	}
	return checkExecutableCapability(capability)
}

func checkCommandScopeLive(s *commandScopeState) error {
	issuer, err := executableIssuerNow()
	if err != nil || issuer != s.issuer {
		return ErrUnsafe
	}
	if err := checkCommandScopeDirectory(s.config.Parent, s.config.ParentIdentity, s.config.UID, s.config.GID, true); err != nil {
		return err
	}
	if err := checkCommandScopeDirectory(s.config.Workspace, s.config.WorkspaceIdentity, s.config.UID, s.config.GID, false); err != nil {
		return err
	}
	if _, err := parentCgroupPath(s.config.Parent, s.config.ParentIdentity.MountID); err != nil {
		return err
	}
	return checkCommandScopePolicy(s)
}

func checkCommandScopeDirectory(file *os.File, expected Identity, uid, gid uint32, parent bool) error {
	if file == nil || expected.Device == 0 || expected.Inode == 0 || expected.MountID == 0 {
		return ErrUnsafe
	}
	flags, flagErr := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
	access, accessErr := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	if flagErr != nil || accessErr != nil || flags&unix.FD_CLOEXEC == 0 || access&unix.O_ACCMODE != unix.O_RDONLY || access&unix.O_PATH != 0 {
		return ErrUnsafe
	}
	actual, err := directoryIdentity(file)
	var stat unix.Stat_t
	var fs unix.Statfs_t
	if err != nil || actual != expected || unix.Fstat(int(file.Fd()), &stat) != nil || unix.Fstatfs(int(file.Fd()), &fs) != nil {
		return ErrUnsafe
	}
	if parent {
		if stat.Uid != 0 || stat.Mode&0077 != 0 || fs.Type != unix.CGROUP2_SUPER_MAGIC {
			return ErrUnsafe
		}
	} else if stat.Ino != 2 || stat.Uid != uid || stat.Gid != gid || stat.Mode&07777 != 0700 || fs.Type != unix.EXT4_SUPER_MAGIC || fs.Flags&unix.ST_RDONLY != 0 {
		return ErrUnsafe
	}
	return nil
}

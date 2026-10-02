//go:build linux

package commanddomain

import (
	"os"
	"os/exec"
	"strconv"

	"golang.org/x/sys/unix"
)

func duplicateScopeMetadata(s *commandScopeState, capability *ExecutableCapability, approval ApprovedExecutable, owner *scopeMetadataDuplicate) (*os.File, error) {
	if capability == nil || capability.executableCapabilityState == nil {
		return nil, ErrUnsafe
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	if capability.file == nil {
		return nil, ErrClosed
	}
	if capability.approval != approval || capability.issuer != s.issuer || checkExecutableCapability(capability) != nil {
		return nil, ErrUnsafe
	}
	file, err := duplicate(capability.file)
	if err != nil {
		return nil, err
	}
	// Publish the exact FD before validation or a possible failed Close. This
	// owner has no execute authority and survives an abnormal duplicate exit.
	owner.mu.Lock()
	owner.file = file
	owner.mu.Unlock()
	if err = checkExecutableCapabilityFile(file, approval, capability.identity); err != nil {
		if file.Close() != nil {
			return nil, ErrRetained
		}
		owner.mu.Lock()
		owner.file = nil
		owner.mu.Unlock()
		return nil, err
	}
	return file, nil
}

func selectScopeExecutableFromFD(s *commandScopeState, file *os.File) (*ExecutableCapability, ApprovedExecutable, error) {
	if file == nil {
		return nil, ApprovedExecutable{}, ErrUnsafe
	}
	var stat unix.Stat_t
	flags, flagErr := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
	access, accessErr := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	if flagErr != nil || accessErr != nil || flags&unix.FD_CLOEXEC == 0 || access&unix.O_ACCMODE != unix.O_RDONLY || access&unix.O_PATH != 0 ||
		unix.Fstat(int(file.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, ApprovedExecutable{}, ErrUnsafe
	}
	var selected *ExecutableCapability
	var approval ApprovedExecutable
	for index, capability := range s.tools {
		if capability == nil || capability.executableCapabilityState == nil || index >= len(s.config.Tools) {
			continue
		}
		capability.mu.Lock()
		matches := capability.identity.Device == stat.Dev && capability.identity.Inode == stat.Ino && capability.approval == s.config.Tools[index] && capability.issuer == s.issuer
		closed := capability.file == nil
		capability.mu.Unlock()
		if !matches {
			continue
		}
		if closed {
			return nil, ApprovedExecutable{}, ErrClosed
		}
		if selected != nil {
			return nil, ApprovedExecutable{}, ErrUnsafe
		}
		selected, approval = capability, s.config.Tools[index]
	}
	if selected == nil {
		return nil, ApprovedExecutable{}, ErrUnsafe
	}
	return selected, approval, nil
}

func initializeExecutableUse(use *ExecutableUse, source *os.File) error {
	capability := use.capability
	if capability == nil || capability.executableCapabilityState == nil {
		return ErrUnsafe
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	if capability.file == nil {
		return ErrClosed
	}
	if capability.approval != use.approval || capability.issuer != use.scope.issuer || checkExecutableCapability(capability) != nil {
		return ErrUnsafe
	}
	if source != nil && checkExecutableCapabilityFile(source, capability.approval, capability.identity) != nil {
		return ErrUnsafe
	}
	slot := -1
	for index, current := range capability.uses {
		if current == nil {
			slot = index
			break
		}
	}
	if slot < 0 {
		return ErrCapacity
	}
	// Register before duplication; a failed duplicate Close keeps this exact use
	// in both bounded owners, including across failed constructor cleanup.
	capability.uses[slot] = use
	use.mu.Lock()
	use.capSlot, use.registered = slot, true
	use.mu.Unlock()
	file, err := duplicateExecutableCapability(capability)
	use.mu.Lock()
	use.file = file
	use.mu.Unlock()
	return err
}

// The caller holds use.mu. Native capability ownership is checked separately
// under its own lock; no pathname is resolved by this validation.
func checkExecutableUse(use *ExecutableUse) error {
	if use == nil || use.executableUseState == nil || use.file == nil || !use.registered || use.scope == nil {
		return ErrUnsafe
	}
	capability := use.capability
	if capability == nil || capability.executableCapabilityState == nil {
		return ErrUnsafe
	}
	capability.mu.Lock()
	defer capability.mu.Unlock()
	if capability.file == nil {
		return ErrClosed
	}
	if use.capSlot < 0 || use.capSlot >= len(capability.uses) || capability.uses[use.capSlot] != use ||
		capability.approval != use.approval || capability.issuer != use.scope.issuer || checkExecutableCapability(capability) != nil {
		return ErrUnsafe
	}
	return checkExecutableCapabilityFile(use.file, use.approval, capability.identity)
}

func checkExecutableUseTemplate(s *commandScopeState, use *ExecutableUse, template *exec.Cmd) error {
	if use == nil || use.executableUseState == nil || template == nil {
		return ErrUnsafe
	}
	use.mu.Lock()
	defer use.mu.Unlock()
	if use.scope != s || use.closed || use.unknown || use.initializing {
		return ErrUnsafe
	}
	if err := checkExecutableUse(use); err != nil {
		return err
	}
	if template.Path == use.approval.Path {
		return nil
	}
	for index, borrowed := range template.ExtraFiles {
		if template.Path != "/proc/self/fd/"+strconv.Itoa(3+index) {
			continue
		}
		capability := use.capability
		capability.mu.Lock()
		err := checkExecutableCapabilityFile(borrowed, use.approval, capability.identity)
		capability.mu.Unlock()
		return err
	}
	return ErrUnsafe
}

func bindDomainExecutableUse(domain *Domain, use *ExecutableUse, class LimitsClass) error {
	if domain == nil || use == nil || use.executableUseState == nil {
		return ErrUnsafe
	}
	if err := checkLimitsPlatform(class); err != nil {
		return err
	}
	domain.mu.Lock()
	defer domain.mu.Unlock()
	use.mu.Lock()
	defer use.mu.Unlock()
	if domain.closed || domain.removed || domain.quarantined || domain.active != 0 || domain.executableUse != nil || use.closed || use.unknown || use.initializing {
		return ErrUnsafe
	}
	if err := checkExecutableUse(use); err != nil {
		return err
	}
	matched := false
	for _, tool := range domain.tools {
		matched = matched || tool.capability == use.capability && tool.approval == use.approval
	}
	if !matched {
		return ErrUnsafe
	}
	domain.executableUse, domain.limitsClass = use, class
	return nil
}

func selectDomainExecutableUse(domain *Domain, template *exec.Cmd) (*ownedExecutable, error) {
	use := domain.executableUse
	if err := checkExecutableUseTemplate(use.scope, use, template); err != nil {
		return nil, err
	}
	for index := range domain.tools {
		tool := &domain.tools[index]
		if tool.capability != use.capability || tool.approval != use.approval {
			continue
		}
		use.mu.Lock()
		capability := use.capability
		capability.mu.Lock()
		err := checkExecutableCapabilityFile(tool.file, use.approval, capability.identity)
		capability.mu.Unlock()
		use.mu.Unlock()
		return tool, err
	}
	return nil, ErrUnsafe
}

//go:build !linux

package commanddomain

import (
	"os"
	"os/exec"
)

func duplicateScopeMetadata(*commandScopeState, *ExecutableCapability, ApprovedExecutable, *scopeMetadataDuplicate) (*os.File, error) {
	return nil, ErrUnavailable
}
func selectScopeExecutableFromFD(*commandScopeState, *os.File) (*ExecutableCapability, ApprovedExecutable, error) {
	return nil, ApprovedExecutable{}, ErrUnavailable
}
func initializeExecutableUse(*ExecutableUse, *os.File) error { return ErrUnavailable }
func checkExecutableUse(*ExecutableUse) error                { return ErrUnavailable }
func checkExecutableUseTemplate(*commandScopeState, *ExecutableUse, *exec.Cmd) error {
	return ErrUnavailable
}
func bindDomainExecutableUse(*Domain, *ExecutableUse, LimitsClass) error { return ErrUnavailable }

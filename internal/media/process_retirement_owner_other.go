//go:build !linux

package media

import "os/exec"

type conventionalProcessIdentity struct{}

func conventionalRetirementSupported() bool { return false }

func captureConventionalProcess(*exec.Cmd) (conventionalProcessIdentity, int, error) {
	return conventionalProcessIdentity{}, -1, ErrProcessRetirementUnknown
}
func closeConventionalProcessPin(int) {}
func cancelConventionalProcess(conventionalProcessIdentity, int) error {
	return ErrProcessRetirementUnknown
}
func fenceConventionalProcess(conventionalProcessIdentity, int) error {
	return ErrProcessRetirementUnknown
}

func retryConventionalProcessCapture(*conventionalMediaProcessOwner) error {
	return ErrProcessRetirementUnknown
}

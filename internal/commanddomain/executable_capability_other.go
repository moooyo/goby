//go:build !linux

package commanddomain

import "os"

func newExecutableCapability(*os.File, ApprovedExecutable) (*ExecutableCapability, error) {
	return nil, ErrUnavailable
}
func checkExecutableCapability(*ExecutableCapability) error { return ErrUnavailable }
func duplicateExecutableCapability(*ExecutableCapability) (*os.File, error) {
	return nil, ErrUnavailable
}

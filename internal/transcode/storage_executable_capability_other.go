//go:build !linux

package transcode

import (
	"os"
	"os/exec"
)

func (*fixedPoolExecutablePreparation) approve(*os.File, string, string) (*fixedPoolExecutableCapability, error) {
	return nil, errFixedVolumeUnavailable
}
func (*fixedPoolExecutableCapability) duplicate() (*fixedPoolExecutableUse, error) {
	return nil, errFixedVolumeUnavailable
}
func (*fixedPoolExecutableUse) start(*exec.Cmd) error { return errFixedVolumeUnavailable }
func (*fixedPoolExecutableUse) wait() error           { return errFixedVolumeUnavailable }
func (*fixedPoolExecutableUse) close() error          { return errFixedVolumeUnavailable }

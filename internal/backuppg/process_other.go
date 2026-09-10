//go:build !linux

package backuppg

import "os/exec"

func configureBackupProcess(_ *exec.Cmd) (func() error, error) {
	return nil, ErrUnsupported
}

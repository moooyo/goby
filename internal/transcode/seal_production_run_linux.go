//go:build linux

package transcode

import (
	"errors"
	"os/exec"
	"syscall"
)

func productionSealProcessExitSafe(waitError error, cancelSignalled bool) bool {
	if waitError == nil {
		return true
	}
	if errors.Is(waitError, exec.ErrWaitDelay) {
		return false
	}
	if !cancelSignalled {
		return false
	}
	var exit *exec.ExitError
	if !errors.As(waitError, &exit) {
		return false
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok {
		return false
	}
	if status.Signaled() {
		return status.Signal() == syscall.SIGTERM || status.Signal() == syscall.SIGKILL
	}
	// FFmpeg's own SIGTERM handler returns 255 after closing its outputs. Its
	// error-level stderr and all observer evidence are checked independently.
	return status.Exited() && status.ExitStatus() == 255
}

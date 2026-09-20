package media

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func mediaEditProcessHitFileLimit(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == syscall.SIGXFSZ
}

func mediaEditResourceLimiter() (string, error) {
	const path = "/usr/bin/prlimit"
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%w: root-owned util-linux /usr/bin/prlimit is required", ErrSubtitleRemovalUnsupported)
	}
	for current := resolved; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("%w: resource limiter identity is unavailable", ErrSubtitleRemovalUnsupported)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: resource limiter is not administratively protected", ErrSubtitleRemovalUnsupported)
		}
		if current == resolved {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() <= 0 || info.Size() > 64<<20 {
				return "", fmt.Errorf("%w: resource limiter is not a regular executable", ErrSubtitleRemovalUnsupported)
			}
		} else if !info.IsDir() {
			return "", fmt.Errorf("%w: resource limiter parent is not a directory", ErrSubtitleRemovalUnsupported)
		}
		if current == string(filepath.Separator) {
			break
		}
	}
	return resolved, nil
}

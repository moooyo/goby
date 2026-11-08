//go:build linux

package library

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// Faccessat2 uses effective credentials and the held directory, including ACLs.
// A read-only filesystem is checked separately because permission bits alone
// cannot describe a container's read-only bind mount. Unsupported kernels or
// uncertain errors remain unknown rather than being presented as read-only.
func observeDirectoryWriteAccess(ctx context.Context, directory *os.File) (*bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if directory == nil {
		return nil, nil
	}
	var filesystem unix.Statfs_t
	if unix.Fstatfs(int(directory.Fd()), &filesystem) != nil {
		return nil, ctx.Err()
	}
	writable := false
	if filesystem.Flags&unix.ST_RDONLY != 0 {
		return &writable, ctx.Err()
	}
	err := unix.Faccessat2(int(directory.Fd()), ".", unix.W_OK|unix.X_OK, unix.AT_EACCESS)
	if err == nil {
		writable = true
		return &writable, ctx.Err()
	}
	if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.EROFS) {
		return &writable, ctx.Err()
	}
	return nil, ctx.Err()
}

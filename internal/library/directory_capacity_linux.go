//go:build linux

package library

import (
	"context"
	"os"

	"golang.org/x/sys/unix"
)

// The caller owns the admitted descriptor for this entire observation. Control
// pins it across the kernel calls, including a concurrent caller cancellation.
func observeDirectoryCapacity(ctx context.Context, directory *os.File) (*directoryCapacity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if directory == nil {
		return nil, nil
	}
	connection, err := directory.SyscallConn()
	if err != nil {
		return nil, ctx.Err()
	}
	var capacity *directoryCapacity
	err = connection.Control(func(descriptor uintptr) {
		var info unix.Stat_t
		if unix.Fstat(int(descriptor), &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFDIR {
			return
		}
		var status unix.Statfs_t
		if unix.Fstatfs(int(descriptor), &status) != nil || status.Bsize <= 0 {
			return
		}
		capacity, _ = directoryCapacityBytes(status.Blocks, status.Bfree, status.Bavail, uint64(status.Bsize))
	})
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if err != nil {
		return nil, nil
	}
	return capacity, nil
}

func observeDirectoryReadAccess(ctx context.Context, directory *os.File) (*bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if directory == nil {
		return nil, nil
	}
	connection, err := directory.SyscallConn()
	if err != nil {
		return nil, ctx.Err()
	}
	var readable *bool
	err = connection.Control(func(descriptor uintptr) {
		accessErr := unix.Faccessat2(int(descriptor), ".", unix.R_OK|unix.X_OK, unix.AT_EACCESS)
		if accessErr == nil {
			value := true
			readable = &value
		} else if accessErr == unix.EACCES || accessErr == unix.EPERM {
			value := false
			readable = &value
		}
	})
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if err != nil {
		return nil, nil
	}
	return readable, nil
}

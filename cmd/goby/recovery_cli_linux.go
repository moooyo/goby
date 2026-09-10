//go:build linux

package main

import (
	"context"
	"io"
	"os"

	"github.com/moooyo/goby/internal/recovery"
	"golang.org/x/sys/unix"
)

func readCLIPassphrase(ctx context.Context, path string, stdin bool) ([]byte, error) {
	if (path == "") == !stdin {
		return nil, errCLIInput
	}
	file := os.Stdin
	if !stdin {
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return nil, errCLIInput
		}
		file = os.NewFile(uintptr(fd), "passphrase")
		defer file.Close()
		var info unix.Stat_t
		if unix.Fstat(fd, &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFREG ||
			info.Mode&07777 != 0600 || info.Uid != uint32(os.Geteuid()) || info.Nlink != 1 || info.Size > 1024 {
			return nil, errCLIInput
		}
	}
	stop := context.AfterFunc(ctx, func() { _ = file.Close() })
	defer stop()
	value, err := io.ReadAll(io.LimitReader(file, 1025))
	if err != nil || ctx.Err() != nil || recovery.ValidatePassphrase(value) != nil {
		clear(value)
		return nil, errCLIInput
	}
	return value, nil
}

func openCLIArchive(path string) (*os.File, error) {
	if path == "" {
		return nil, errCLIInput
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errCLIInput
	}
	file := os.NewFile(uintptr(fd), "archive")
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Size < 1 {
		file.Close()
		return nil, errCLIInput
	}
	return file, nil
}

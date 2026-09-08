//go:build linux

package library

import (
	"fmt"
	"os"
	"syscall"
)

// A renamed directory entry might become a FIFO between ReadDir and Open.
// O_NONBLOCK prevents that entry from indefinitely occupying a scan worker.
func openScanFile(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}

func fileIdentity(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d", uint64(stat.Dev), uint64(stat.Ino))
}

//go:build linux

package diagnostics

import (
	"syscall"
	"unsafe"
)

// linkAt is used only to publish a new registry with no replacement semantics.
// The two names are generated basenames under the same pinned directory.
func linkAt(directoryFD int, source, destination string) error {
	from, err := syscall.BytePtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.BytePtrFromString(destination)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_LINKAT, uintptr(directoryFD), uintptr(unsafe.Pointer(from)), uintptr(directoryFD), uintptr(unsafe.Pointer(to)), 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

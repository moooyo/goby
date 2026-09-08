package media

import (
	"os"
	"syscall"
)

// O_NONBLOCK prevents an unexpected FIFO from blocking before the file-type
// check. It has no effect on reads from regular files.
func openLocalMedia(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

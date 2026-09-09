//go:build !linux

package media

import "os"

// FileChangeTime returns zero because inode change times are unavailable.
func FileChangeTime(os.FileInfo) int64 {
	return 0
}

//go:build !linux

package media

import "os"

func subtitleOCRDirectoryOwned(os.FileInfo) bool { return false }

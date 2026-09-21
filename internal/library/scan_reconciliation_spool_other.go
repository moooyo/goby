//go:build !linux

package library

import "os"

func openScanSpoolFile(root *os.Root, name string, flags int) (*os.File, error) {
	return root.OpenFile(name, flags, 0o600)
}

func scanSpoolDirectoryIdentity(*os.File) (scanSpoolIdentity, bool, error) {
	return scanSpoolIdentity{}, false, nil
}

func removeScanSpoolLeaf(root *os.Root, name string) error      { return root.Remove(name) }
func removeScanSpoolDirectory(root *os.Root, name string) error { return root.Remove(name) }

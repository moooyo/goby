//go:build !linux

package library

import "os"

func openScanFile(root *os.Root, path string) (*os.File, error) {
	return root.Open(path)
}

// Other platforms retain stable IDs through the catalog path key.
func fileIdentity(os.FileInfo) string { return "" }

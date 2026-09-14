//go:build !goby_embed_admin

package main

import (
	"io/fs"
	"os"
)

// Ordinary source builds do not require generated frontend files.
func defaultDashboardAssets(directory string) (fs.FS, error) {
	return os.DirFS(directory), nil
}

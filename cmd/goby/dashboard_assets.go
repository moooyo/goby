package main

import (
	"io/fs"
	"os"
)

// Keep Config.Load's existing path/default semantics. A nonempty explicit
// GOBY_WEB_DIR always selects that resolved path, even in an embedded build.
func dashboardAssets(directory string) (fs.FS, error) {
	if os.Getenv("GOBY_WEB_DIR") != "" {
		return os.DirFS(directory), nil
	}
	return defaultDashboardAssets(directory)
}

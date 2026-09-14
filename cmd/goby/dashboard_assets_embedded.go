//go:build goby_embed_admin

package main

import (
	"io/fs"

	adminassets "github.com/moooyo/goby/web/admin"
)

func defaultDashboardAssets(_ string) (fs.FS, error) {
	return adminassets.Files()
}

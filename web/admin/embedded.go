//go:build goby_embed_admin

// Package adminassets exposes the production administrator bundle to release
// builds. The build tag keeps normal source builds independent of generated
// Vite output. Missing dist/index.html is a compile-time packaging error.
package adminassets

import (
	"embed"
	"io/fs"
)

// Include the complete bundle, including underscore-prefixed chunks. The
// explicit index pattern prevents an otherwise nonempty partial dist from
// satisfying the release build when its HTML entry is missing.
//
//go:embed all:dist dist/index.html
var files embed.FS

// Files returns a filesystem rooted at the generated production bundle.
func Files() (fs.FS, error) {
	return fs.Sub(files, "dist")
}

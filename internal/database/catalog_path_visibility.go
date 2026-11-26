package database

import (
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Keep this pattern valid in both Go and PostgreSQL. Requiring a trailing
// slash matches only directory components, not prefixes or file names.
const systemDirectoryPattern = `(^|/)(#recycle|@eaDir|[$]RECYCLE[.]BIN)/`

var systemDirectory = regexp.MustCompile(systemDirectoryPattern)

// CatalogPathExcluded reports whether a physical, slash-separated catalog path
// belongs to a skipped system directory. Virtual keys are not filesystem paths.
func CatalogPathExcluded(relative string, directory bool) bool {
	if strings.HasPrefix(relative, "//") {
		return false
	}
	if directory {
		relative += "/"
	}
	return systemDirectory.MatchString(relative)
}

// CatalogPathVisibleSQL is the catalog-side counterpart of CatalogPathExcluded.
// This is a read/scan policy, not a stored-role validation or absence proof.
func CatalogPathVisibleSQL(alias string) string {
	item := pgx.Identifier{alias}.Sanitize()
	return `(left(` + item + `.relative_path,2)='//' OR (` + item + `.relative_path || CASE WHEN ` + item +
		`.is_folder THEN '/' ELSE '' END) COLLATE "C" !~ '` + systemDirectoryPattern + `')`
}

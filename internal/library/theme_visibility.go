package library

import "github.com/moooyo/goby/internal/database"

// Ordinary enumeration excludes all permanent attachment roles and reserved
// paths, including inactive history. Keep this shared with current-schema checks.
func ordinaryItemSQL(alias string) string {
	return database.CatalogOrdinaryItemSQL(alias)
}

// Direct reads retain active, correctly owned auxiliary media without making it an
// ordinary catalog item. Callers still apply their existing subject policy.
func directItemSQL(alias string) string {
	return database.CatalogDirectItemSQL(alias)
}

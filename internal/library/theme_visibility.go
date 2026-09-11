package library

import "github.com/moooyo/goby/internal/database"

// Ordinary enumeration excludes permanent theme roles and reserved paths even
// after an attachment is inactive. Keep this policy shared with schema checks.
func ordinaryItemSQL(alias string) string {
	return database.ThemeOrdinaryItemSQL(alias)
}

// Direct reads retain active, correctly owned theme media without making it an
// ordinary catalog item. Callers still apply their existing subject policy.
func directItemSQL(alias string) string {
	return database.ThemeDirectItemSQL(alias)
}

package library

import "strings"

var movieQueryItemColumns = strings.NewReplacer(
	itemAlbumChildCountColumn, "NULL::bigint",
	itemAlbumColumn, "NULL::jsonb",
	itemTVParentsColumn, "NULL::jsonb",
).Replace(itemColumns)

// itemQueryColumns specializes only a normalized, exclusively Movie result
// population. Its type filter proves these three CASE results are SQL NULL;
// retaining their types and positions preserves scanItem and every other field.
// Apply scopeSQL afterwards so all remaining nested authorization is unchanged.
func itemQueryColumns(query Query) string {
	if query.expectedEpisodePopulation || len(query.IncludeItemTypes) != 1 || query.IncludeItemTypes[0] != "Movie" {
		return itemColumns
	}
	return movieQueryItemColumns
}

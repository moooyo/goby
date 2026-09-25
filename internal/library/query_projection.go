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

// itemPageQuerySQL keeps result-only subqueries behind the pagination boundary.
// The selector retains every existing visibility/filter clause and the exact
// ordering. A primary-key join in the same statement snapshot preserves each
// selected row while avoiding wide projections for rows discarded by OFFSET.
func itemPageQuerySQL(query Query, prefix, population, columns, filter, order, pagination string) string {
	const movieOrder = "lower(i.sort_name) ASC NULLS LAST, i.id ASC"
	if population != "items" || query.expectedEpisodePopulation || query.Resumable || len(query.Ids) != 0 ||
		len(query.IncludeItemTypes) != 1 || query.IncludeItemTypes[0] != "Movie" ||
		query.SortBy != "SortName" || query.SortOrder != "ASC" || order != movieOrder {
		return prefix + "SELECT " + columns + " FROM " + population + " i WHERE " + filter +
			" ORDER BY " + order + pagination
	}
	selected := `selected_page AS MATERIALIZED (
		SELECT i.id, lower(i.sort_name) AS page_sort FROM items i WHERE ` + filter +
		" ORDER BY " + order + pagination + ")"
	return appendItemQueryCTE(prefix, selected) + "SELECT " + columns +
		" FROM selected_page page JOIN items i ON i.id = page.id" +
		" ORDER BY page.page_sort ASC NULLS LAST, page.id ASC"
}

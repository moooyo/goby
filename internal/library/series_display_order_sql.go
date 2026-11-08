package library

import "strings"

// An unmapped episode in an explicitly selected order is unknown, rather than
// an aired-order fallback. The sentinel sorts after all admitted provider ranks
// and is removed from the direction-sensitive key so it stays last for DESC.
const seriesDisplayOrderUnmappedRank = "2147483647"

func seriesDisplayOrderRankSQL(order, ordinal string) string {
	return "(CASE WHEN " + order + " IN ('Dvd','Absolute') THEN COALESCE(" + ordinal + "," + seriesDisplayOrderUnmappedRank + ") END)"
}

// seriesDisplayOrderSequenceSQL is shared by series traversal and item browse.
// Canonical episode numbers remain authoritative identity and matching facts;
// they are used for sorting only when no alternate order is selected.
func seriesDisplayOrderSequenceSQL(rank, season, episode, name, id, direction string) string {
	clauses := []string{
		"NULLIF(" + rank + "," + seriesDisplayOrderUnmappedRank + ") " + direction + " NULLS LAST",
		seriesDisplayOrderFallbackSQL(rank, season) + " " + direction + " NULLS LAST",
		seriesDisplayOrderFallbackSQL(rank, episode) + " " + direction + " NULLS LAST",
		seriesDisplayOrderFallbackSQL(rank, name) + " " + direction + " NULLS LAST",
		id + " " + direction,
	}
	return strings.Join(clauses, ", ")
}

func seriesDisplayOrderFallbackSQL(rank, value string) string {
	return "(CASE WHEN " + rank + " IS NULL THEN " + value + " END)"
}

func seriesDisplayOrderProviderSQL(alias string) string {
	metadata := strings.ReplaceAll(itemMetadataColumn, "i.", alias+".")
	return "(" + metadata + "->'ProviderIDs'->>'Tmdb')"
}

// seriesDisplayOrderBrowseRankSQL follows the same two physical parent edges
// as the public television parent projection. Every edge stays in the item's
// library and ordinary-item scope; indexes never infer a missing Series edge.
// scopeSQL applies current user policy to the standard tv_parent/tv_series
// aliases before this expression reaches the database.
func seriesDisplayOrderBrowseRankSQL() string {
	return `(SELECT ` + seriesDisplayOrderRankSQL("display_order.display_order", "display_entry.ordinal") + `
		FROM items tv_parent
		LEFT JOIN items tv_series ON tv_parent.type='Season'
			AND tv_series.id=tv_parent.parent_id AND tv_series.library_id=i.library_id
			AND tv_series.id<>i.id AND tv_series.id<>tv_parent.id
			AND tv_series.type='Series' AND tv_series.is_folder AND ` + ordinaryItemSQL("tv_series") + `
		JOIN series_display_orders display_order ON display_order.series_id=
			CASE WHEN tv_parent.type='Series' THEN tv_parent.id ELSE tv_series.id END
		LEFT JOIN series_display_order_entries display_entry ON display_entry.series_id=display_order.series_id
			AND display_order.provider_series_id=CASE WHEN tv_parent.type='Series'
				THEN ` + seriesDisplayOrderProviderSQL("tv_parent") + ` ELSE ` + seriesDisplayOrderProviderSQL("tv_series") + ` END
			AND display_entry.season_number=COALESCE(i.parent_index_number,
				CASE WHEN tv_parent.type='Season' THEN tv_parent.index_number END)
			AND display_entry.episode_number=i.index_number
		WHERE i.type='Episode' AND NOT i.is_folder
			AND tv_parent.id=i.parent_id AND tv_parent.library_id=i.library_id
			AND tv_parent.id<>i.id AND tv_parent.is_folder
			AND tv_parent.type IN ('Series','Season') AND ` + ordinaryItemSQL("tv_parent") + `)`
}

// Alternate provider order is a series-browse policy, not a replacement for an
// explicit personal, chronological, or rating sort on a mixed catalog page.
func seriesDisplayOrderBrowseQuery(query Query) bool {
	if query.ParentID == "" || len(query.IncludeItemTypes) != 1 || query.IncludeItemTypes[0] != "Episode" {
		return false
	}
	field, _, _ := strings.Cut(query.SortBy, ",")
	return field == "SortName" || field == "IndexNumber" || field == "ParentIndexNumber"
}

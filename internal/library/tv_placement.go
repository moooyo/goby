package library

// A standalone special has season-zero episode identity but no placement in a
// positive regular season. An episode number alone is not a season placement.
// This is independent of physical ancestry, which remains scanner-owned.
func (item Item) IsStandaloneSpecial() bool {
	if item.Type != "Episode" || item.ParentIndexNumber != 0 {
		return false
	}
	if item.Metadata == nil {
		return true
	}
	before, after := item.Metadata.AirsBeforeSeasonNumber, item.Metadata.AirsAfterSeasonNumber
	return (before == nil || *before <= 0) && (after == nil || *after <= 0)
}

func standaloneSpecialSQL() string {
	before := navigationNumberSQL(itemMetadataColumn, "AirsBeforeSeasonNumber")
	after := navigationNumberSQL(itemMetadataColumn, "AirsAfterSeasonNumber")
	return "(i.type = 'Episode' AND i.parent_index_number = 0 AND COALESCE(" + before + ",0) <= 0 AND COALESCE(" + after + ",0) <= 0)"
}

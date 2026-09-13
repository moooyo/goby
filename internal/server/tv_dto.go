package server

import "github.com/moooyo/goby/internal/library"

// TV relationships are present in the observed default lists as well as details.
// Only the library's authorized parent projection supplies their IDs and names.
func addTVParentFields(dto map[string]any, item library.Item) {
	episode := item.Type == "Episode" && !item.IsFolder
	if !episode && !(item.Type == "Season" && item.IsFolder) {
		return
	}
	if item.Series != nil && item.Series.ID != "" {
		dto["SeriesId"] = item.Series.ID
		dto["SeriesName"] = item.Series.Name
	}
	if episode && item.Season != nil && item.Season.ID != "" {
		dto["SeasonId"] = item.Season.ID
		dto["SeasonName"] = item.Season.Name
	}
}

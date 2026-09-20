package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/moooyo/goby/internal/library"
)

// Emby Web 4.9.5.0's single-episode playback expands a series with both flags
// false, without Limit or StartIndex. Its playback manager locates the selected
// Id in this complete result and consumes EnableNextEpisodeAutoPlay itself.
// Ordinary episode browsing and explicitly paginated requests keep their own
// query contract; disabling autoplay must not prevent manual next/replay.
func (s *Server) tryEpisodePlaybackQueue(w http.ResponseWriter, r *http.Request, userID, seriesID string) bool {
	values, err := embyBusinessQuery(r)
	if err != nil {
		s.libraryError(w, r, library.ErrInvalidInput)
		return true
	}
	for _, name := range []string{"IsMissing", "IsVirtualUnaired"} {
		entries, present := values[name]
		if !present {
			return false
		}
		if len(entries) != 1 {
			s.libraryError(w, r, library.ErrInvalidInput)
			return true
		}
		flag, err := strconv.ParseBool(entries[0])
		if err != nil {
			s.libraryError(w, r, library.ErrInvalidInput)
			return true
		}
		if flag {
			return false
		}
	}
	for name := range values {
		switch name {
		case "IsMissing", "IsVirtualUnaired", "UserId", "Fields", "ExcludeFields", "EnableImages", "EnableUserData", "ImageTypeLimit", "EnableImageTypes":
		default:
			return false
		}
	}
	result, err := s.library.EpisodePlaybackQueue(r.Context(), requestLibrarySubject(r, userID), seriesID)
	if errors.Is(err, library.ErrEpisodePlaybackQueueLimit) {
		apiError(w, r, http.StatusUnprocessableEntity, "episode_queue_limit", "This series exceeds the complete playback queue limit; request an explicitly paginated episode list.")
		return true
	}
	if err != nil {
		s.libraryError(w, r, err)
		return true
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, entry := range result.Items {
		item := s.itemDTOForRequest(r, entry, queryValues(values["Fields"]), false)
		applyItemSwitches(item, r)
		items = append(items, item)
	}
	if !s.applyIndexedImages(w, r, userID, items, false) {
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount})
	return true
}

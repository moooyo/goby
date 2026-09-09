package server

import (
	"net/http"

	"github.com/moooyo/goby/internal/library"
)

func (s *Server) nextUpItems(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	query, ok := readItemQuery(w, r, userID)
	if !ok {
		return
	}
	zeroLimit := query.Limit == 0
	if zeroLimit {
		query.Limit = 1
	}
	result, err := s.library.NextUp(r.Context(), library.NextUpQuery{
		UserID: userID, SeriesID: r.URL.Query().Get("SeriesId"), ParentID: query.ParentID,
		StartIndex: query.StartIndex, Limit: query.Limit,
	})
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	if !zeroLimit {
		for _, item := range result.Items {
			dto := s.itemDTO(item, queryValues(r.URL.Query()["Fields"]), false)
			applyItemSwitches(dto, r)
			items = append(items, dto)
		}
	}
	if !s.applyIndexedImages(w, r, userID, items, false) {
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount})
}

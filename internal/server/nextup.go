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
	if hasNavigationFilters(query) || query.IsFavoriteOrLikes != nil || query.SortBy != "" || query.SortOrder != "" {
		s.libraryError(w, r, library.ErrInvalidInput)
		return
	}
	if len(query.ListItemIds) != 0 {
		s.libraryError(w, r, library.ErrUnsupportedFilter)
		return
	}
	attachApplicationCredentialID(r, &query)
	result, err := s.library.NextUp(r.Context(), library.NextUpQuery{
		UserID: userID, SeriesID: r.URL.Query().Get("SeriesId"), ParentID: query.ParentID,
		ApplicationCredentialID: query.ApplicationCredentialID,
		StartIndex:              query.StartIndex, Limit: query.Limit,
		CountOnly:  query.Limit == 0,
		Projection: query.Projection,
	})
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	p := readItemPresentation(r)
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		dto := s.itemDTOWithToken(item, p.fields, false, p.deliveryToken)
		p.applySwitches(dto)
		items = append(items, dto)
	}
	if !s.applyIndexedImages(w, r, userID, items, false, p) {
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount})
}

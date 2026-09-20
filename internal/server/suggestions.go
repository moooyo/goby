package server

import (
	"net/http"

	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerSuggestionsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Users/{UserId}/Suggestions", s.requireEmby(s.embySuggestions))
}

func (s *Server) embySuggestions(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	query, ok := readItemQuery(w, r, userID)
	if !ok {
		return
	}
	if _, supplied := r.URL.Query()["Recursive"]; !supplied {
		query.Recursive = true
	}
	if _, supplied := r.URL.Query()["Limit"]; !supplied {
		query.Limit = 20
	}
	preferences, err := s.requestUserConfiguration(r.Context(), r, userID)
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	applySuggestionPlayedDefault(&query, preferences.HidePlayedInSuggestions)
	attachApplicationCredentialID(r, &query)
	zeroLimit := query.Limit == 0
	if zeroLimit {
		query.Limit = 1
	}
	result, err := s.library.QuerySuggestions(r.Context(), query)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	if !zeroLimit {
		fields := queryValues(r.URL.Query()["Fields"])
		for _, entry := range result.Items {
			item := s.itemDTOForRequest(r, entry, fields, false)
			applyItemSwitches(item, r)
			items = append(items, item)
		}
	}
	if !s.applyIndexedImages(w, r, userID, items, false) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount})
}

func applySuggestionPlayedDefault(query *library.Query, hidePlayed bool) {
	// readItemQuery has already intersected IsPlayed with played-state Filters
	// and rejected conflicting explicit values. Neither can be overwritten.
	if query.IsPlayed == nil && hidePlayed {
		value := false
		query.IsPlayed = &value
	}
}

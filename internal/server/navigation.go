package server

import (
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) registerNavigationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Items/{Id}/Ancestors", s.requireEmby(s.itemAncestors))
	mux.HandleFunc("GET /emby/Items/Counts", s.requireEmby(s.itemCounts))
	mux.HandleFunc("GET /emby/Videos/{Id}/AdditionalParts", s.requireEmby(s.additionalParts))
}

func navigationQuery(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	if !normalizeItemProjectionQuery(w, r) {
		return false
	}
	values, err := embyBusinessQuery(r)
	if err != nil {
		apiError(w, r, 400, "invalid_navigation_query", "The navigation query is invalid.")
		return false
	}
	known := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		known[name] = true
	}
	for name, entries := range values {
		if !known[name] || len(entries) != 1 || strings.ContainsRune(entries[0], 0) {
			apiError(w, r, 400, "invalid_navigation_query", "The navigation query contains an unsupported or repeated field.")
			return false
		}
	}
	return true
}

func (s *Server) itemAncestors(w http.ResponseWriter, r *http.Request) {
	if !navigationQuery(w, r, "UserId") {
		return
	}
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	items, err := s.library.Ancestors(r.Context(), requestLibrarySubject(r, userID), r.PathValue("Id"))
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		dto := s.itemDTOForRequest(r, item, nil, false)
		if item.Type == "CollectionFolder" {
			root, err := s.library.GetLibrary(r.Context(), item.LibraryID)
			if err != nil {
				s.libraryError(w, r, err)
				return
			}
			dto["CollectionType"] = root.CollectionType
		}
		result = append(result, dto)
	}
	if !s.applyIndexedImages(w, r, userID, result, false) {
		return
	}
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) itemCounts(w http.ResponseWriter, r *http.Request) {
	if !navigationQuery(w, r, "UserId", "IsFavorite") {
		return
	}
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	var favorite *bool
	if raw, exists := r.URL.Query()["IsFavorite"]; exists {
		value, err := strconv.ParseBool(raw[0])
		if err != nil {
			apiError(w, r, 400, "invalid_navigation_query", "IsFavorite must be a boolean.")
			return
		}
		favorite = &value
	}
	counts, err := s.library.CountItems(r.Context(), requestLibrarySubject(r, userID), favorite)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, counts)
}

func (s *Server) additionalParts(w http.ResponseWriter, r *http.Request) {
	if !navigationQuery(w, r, "UserId", "Fields", "ExcludeFields", "EnableImages", "ImageTypeLimit", "EnableImageTypes", "EnableUserData") {
		return
	}
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	result, err := s.library.AdditionalParts(r.Context(), requestLibrarySubject(r, userID), r.PathValue("Id"))
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		dto := s.itemDTOForRequest(r, item, queryValues(r.URL.Query()["Fields"]), false)
		applyItemSwitches(dto, r)
		items = append(items, dto)
	}
	if !s.applyIndexedImages(w, r, userID, items, false) {
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount})
}

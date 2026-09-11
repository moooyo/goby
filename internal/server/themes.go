package server

import (
	"net/http"
	"strconv"

	"github.com/moooyo/goby/internal/library"
)

func readThemeQuery(w http.ResponseWriter, r *http.Request) (library.ThemeQuery, bool) {
	query := library.ThemeQuery{EnableThemeSongs: true, EnableThemeVideos: true}
	values := r.URL.Query()
	for _, control := range []struct {
		name  string
		value *bool
	}{
		{"InheritFromParent", &query.InheritFromParent},
		{"EnableThemeSongs", &query.EnableThemeSongs},
		{"EnableThemeVideos", &query.EnableThemeVideos},
	} {
		raw, supplied := values[control.name]
		if !supplied {
			continue
		}
		if len(raw) != 1 {
			apiError(w, r, http.StatusBadRequest, "invalid_input", control.name+" must be one boolean.")
			return library.ThemeQuery{}, false
		}
		value, err := strconv.ParseBool(raw[0])
		if err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_input", control.name+" must be one boolean.")
			return library.ThemeQuery{}, false
		}
		*control.value = value
	}
	return query, true
}

func (s *Server) embyThemeMedia(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	query, ok := readThemeQuery(w, r)
	if !ok {
		return
	}
	query.Subject = requestLibrarySubject(r, userID)
	result, err := s.library.QueryThemeMedia(r.Context(), r.PathValue("Id"), query)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	fields := queryValues(r.URL.Query()["Fields"])
	response := make(map[string]any, 3)
	for _, group := range []struct {
		name   string
		result library.ThemeResult
	}{
		{"ThemeSongsResult", result.ThemeSongsResult},
		{"ThemeVideosResult", result.ThemeVideosResult},
		{"SoundtrackSongsResult", result.SoundtrackSongsResult},
	} {
		items := make([]map[string]any, 0, len(group.result.Items))
		for _, entry := range group.result.Items {
			// Positive reference captures explicitly selected their detailed
			// fields. A Theme request alone does not imply detail projection.
			item := s.itemDTOForRequest(r, entry, fields, false)
			applyItemSwitches(item, r)
			items = append(items, item)
		}
		if !s.applyIndexedImages(w, r, userID, items, false) {
			return
		}
		response[group.name] = map[string]any{
			"OwnerId": group.result.OwnerID, "Items": items, "TotalRecordCount": group.result.TotalRecordCount,
		}
	}
	jsonResponse(w, http.StatusOK, response)
}

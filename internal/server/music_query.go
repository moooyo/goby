package server

import (
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/library"
)

func readMusicFilters(w http.ResponseWriter, r *http.Request, query *library.Query) bool {
	values := r.URL.Query()
	for _, field := range []struct {
		name   string
		target *[]int64
	}{
		{"ArtistIds", &query.ArtistIds}, {"AlbumArtistIds", &query.AlbumArtistIds},
	} {
		ids, valid := entityIDValues(values[field.name])
		if !valid || len(ids) > 1024 {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "Music artist filters must contain positive decimal identifiers.")
			return false
		}
		*field.target = ids
	}
	for _, field := range []struct {
		name   string
		target *[]string
	}{
		{"AlbumIds", &query.AlbumIds}, {"ExcludeItemIds", &query.ExcludeItemIds}, {"ListItemIds", &query.ListItemIds},
	} {
		ids := make([]string, 0)
		for _, value := range values[field.name] {
			if value == "" {
				if field.name == "ListItemIds" {
					apiError(w, r, http.StatusBadRequest, "invalid_input", "ListItemIds must contain bounded nonempty identifiers.")
					return false
				}
				continue
			}
			for _, part := range strings.Split(value, ",") {
				id := strings.TrimSpace(part)
				if id == "" || len(id) > 256 || !utf8.ValidString(id) || strings.IndexFunc(id, unicode.IsControl) >= 0 || len(ids) >= 1024 {
					apiError(w, r, http.StatusBadRequest, "invalid_input", "Music item filters must contain bounded nonempty identifiers.")
					return false
				}
				ids = append(ids, id)
			}
		}
		*field.target = ids
	}
	return true
}

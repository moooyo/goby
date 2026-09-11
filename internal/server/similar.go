package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/library"
)

func readSimilarQuery(w http.ResponseWriter, r *http.Request, userID string) (library.SimilarQuery, bool) {
	query, ok := readItemQuery(w, r, userID)
	if !ok {
		return library.SimilarQuery{}, false
	}
	values := r.URL.Query()
	if _, supplied := values["Recursive"]; !supplied {
		query.Recursive = true
	}
	excluded, valid := entityIDValues(values["ExcludeArtistIds"])
	if !valid || len(excluded) > 1024 {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Excluded artists must use bounded positive decimal identifiers.")
		return library.SimilarQuery{}, false
	}
	if raw, supplied := values["EnableTotalRecordCount"]; supplied {
		if len(raw) != 1 {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "EnableTotalRecordCount must be one boolean.")
			return library.SimilarQuery{}, false
		}
		if _, err := strconv.ParseBool(raw[0]); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "EnableTotalRecordCount must be one boolean.")
			return library.SimilarQuery{}, false
		}
	}
	if raw, supplied := values["ArtistType"]; supplied {
		if len(raw) != 1 || (!strings.EqualFold(raw[0], "Artist") && !strings.EqualFold(raw[0], "AlbumArtist")) {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "ArtistType must identify Artist or AlbumArtist.")
			return library.SimilarQuery{}, false
		}
	}
	// These observed ArtistType variants do not select the seed's authority or
	// replace its accepted artist relationships. Similar counts describe the
	// returned page even when EnableTotalRecordCount is false.
	return library.SimilarQuery{Query: query, ExcludeArtistIds: excluded,
		ExplicitSort: strings.TrimSpace(query.SortBy) != ""}, true
}

func (s *Server) embySimilar(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	query, ok := readSimilarQuery(w, r, userID)
	if !ok {
		return
	}
	attachApplicationCredentialID(r, &query.Query)
	result, err := s.library.QuerySimilar(r.Context(), r.PathValue("Id"), query)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	fields := queryValues(r.URL.Query()["Fields"])
	items := make([]map[string]any, 0, len(result.Items))
	for _, entry := range result.Items {
		item := s.itemDTOForRequest(r, entry, fields, false)
		applyItemSwitches(item, r)
		items = append(items, item)
	}
	if !s.applyIndexedImages(w, r, userID, items, false) {
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount})
}

package server

import (
	"net/http"
	"strconv"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerMusicEntityRoutes(mux *http.ServeMux) {
	for _, route := range []struct{ path, family string }{
		{"Artists", "artists"}, {"Artists/AlbumArtists", "albumartists"},
		{"AlbumArtists", "albumartists"}, {"MusicGenres", "genres"},
	} {
		mux.HandleFunc("GET /emby/"+route.path, s.requireEmby(s.musicEntityList(route.family)))
		if route.path != "Artists/AlbumArtists" {
			mux.HandleFunc("GET /emby/"+route.path+"/{Name}", s.requireEmby(s.musicEntityByName(route.family)))
		}
	}
	mux.HandleFunc("GET /admin/v1/music/artists", s.requireAdmin(s.adminMusicEntities("artists")))
	mux.HandleFunc("GET /admin/v1/music/genres", s.requireAdmin(s.adminMusicEntities("genres")))
}

func musicEntityFamily(r *http.Request, family string) (string, bool) {
	values, supplied := r.URL.Query()["ArtistType"]
	if !supplied {
		return family, true
	}
	if len(values) != 1 {
		return "", false
	}
	artist, albumArtist, valid := musicArtistRoles(values[0])
	if !valid {
		return "", false
	}
	if family == "artists" && albumArtist {
		if artist {
			return "allartists", true
		}
		return "albumartists", true
	}
	return family, true
}

func (s *Server) musicEntityDTO(entity library.Entity, family string, fields []string, detail bool) map[string]any {
	dto := s.entityDTO(entity, fields, detail)
	if family == "genres" {
		dto["Type"] = "MusicGenre"
	}
	return dto
}

func (s *Server) musicEntityList(family string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.itemUser(w, r)
		if !ok {
			return
		}
		selected, valid := musicEntityFamily(r, family)
		if !valid {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "ArtistType must identify Artist, AlbumArtist, or both.")
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
		attachApplicationCredentialID(r, &query)
		result, err := s.library.ListMusicEntities(r.Context(), selected, query)
		if err != nil {
			s.libraryError(w, r, err)
			return
		}
		items := make([]map[string]any, 0, len(result.Items))
		if !zeroLimit {
			for _, entity := range result.Items {
				dto := s.musicEntityDTO(entity, selected, queryValues(r.URL.Query()["Fields"]), false)
				applyItemSwitches(dto, r)
				items = append(items, dto)
			}
		}
		jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount})
	}
}

func (s *Server) musicEntityByName(family string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.itemUser(w, r)
		if !ok {
			return
		}
		entity, err := s.library.GetMusicEntityFor(r.Context(), requestLibrarySubject(r, userID), family, r.PathValue("Name"))
		if err != nil {
			s.libraryError(w, r, err)
			return
		}
		dto := s.musicEntityDTO(entity, family, queryValues(r.URL.Query()["Fields"]), true)
		applyItemSwitches(dto, r)
		jsonResponse(w, http.StatusOK, dto)
	}
}

func parseAdminMusicEntityQuery(r *http.Request, family string) (library.Query, string, error) {
	query := library.Query{Limit: 25, Recursive: true, SortBy: "SortName", SortOrder: "Ascending"}
	for key, values := range r.URL.Query() {
		if len(values) != 1 {
			return library.Query{}, "", library.ErrInvalidInput
		}
		value := values[0]
		switch key {
		case "Role":
			if family != "artists" {
				return library.Query{}, "", library.ErrInvalidInput
			}
			switch value {
			case "Artist":
			case "AlbumArtist":
				family = "albumartists"
			default:
				return library.Query{}, "", library.ErrInvalidInput
			}
		case "SearchTerm":
			query.SearchTerm = value
		case "ParentId":
			query.ParentID = value
		case "StartIndex", "Limit":
			number, err := strconv.ParseInt(value, 10, 32)
			if err != nil || number < 0 || key == "Limit" && (number < 1 || number > 200) {
				return library.Query{}, "", library.ErrInvalidInput
			}
			if key == "StartIndex" {
				query.StartIndex = int(number)
			} else {
				query.Limit = int(number)
			}
		case "IsFavorite":
			favorite, err := strconv.ParseBool(value)
			if err != nil {
				return library.Query{}, "", library.ErrInvalidInput
			}
			query.IsFavorite = &favorite
		default:
			return library.Query{}, "", library.ErrInvalidInput
		}
	}
	return query, family, nil
}

func (s *Server) adminMusicEntities(family string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query, selected, err := parseAdminMusicEntityQuery(r, family)
		if err != nil {
			s.adminMetadataError(w, r, err)
			return
		}
		actor := r.Context().Value(principalKey).(identity.Principal)
		result, err := s.library.QueryMusicMetadataEntities(r.Context(), actor, selected, query)
		if err != nil {
			s.adminMetadataError(w, r, err)
			return
		}
		items := make([]map[string]any, 0, len(result.Items))
		for _, entity := range result.Items {
			dto := s.musicEntityDTO(entity, selected, nil, false)
			dto["ItemCount"] = entity.Count
			items = append(items, dto)
		}
		jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount,
			"StartIndex": query.StartIndex, "Limit": query.Limit})
	}
}

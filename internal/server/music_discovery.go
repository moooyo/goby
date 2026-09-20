package server

import (
	"net/http"
	"strings"

	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerMusicDiscoveryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Items/Prefixes", s.requireEmby(s.embyMusicPrefixes("")))
	mux.HandleFunc("GET /emby/Artists/Prefixes", s.requireEmby(s.embyMusicPrefixes("artists")))
	mux.HandleFunc("GET /emby/Albums/{Id}/Similar", s.requireEmby(s.embyMusicSimilar(false)))
	// A single dispatcher avoids overlapping ServeMux patterns for the legacy
	// Artists/AlbumArtists/{Name} alias and Artists/{Id}/Similar.
	mux.HandleFunc("GET /emby/Artists/{Id}/{Action}", s.requireEmby(s.embyArtistDiscoveryAction))
	for _, route := range []struct{ path, kind string }{
		{"Items/{Id}/InstantMix", "Item"}, {"Songs/{Id}/InstantMix", "Song"},
		{"Albums/{Id}/InstantMix", "Album"}, {"Playlists/{Id}/InstantMix", "Playlist"},
		{"Artists/InstantMix", "Artist"}, {"MusicGenres/InstantMix", "Genre"},
		{"MusicGenres/{Name}/InstantMix", "GenreName"},
	} {
		mux.HandleFunc("GET /emby/"+route.path, s.requireEmby(s.embyInstantMix(route.kind)))
	}
}

func (s *Server) embyArtistDiscoveryAction(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("Id") == "AlbumArtists" {
		r.SetPathValue("Name", r.PathValue("Action"))
		s.musicEntityByName("albumartists")(w, r)
		return
	}
	if r.PathValue("Action") == "Similar" {
		s.embyMusicSimilar(true)(w, r)
		return
	}
	apiError(w, r, http.StatusNotFound, "not_found", "The music resource was not found.")
}

func (s *Server) embyMusicPrefixes(family string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.itemUser(w, r)
		if !ok {
			return
		}
		selected := family
		if family != "" {
			var valid bool
			selected, valid = musicEntityFamily(r, family)
			if !valid {
				apiError(w, r, http.StatusBadRequest, "invalid_input", "ArtistType must identify Artist, AlbumArtist, or both.")
				return
			}
		}
		query, ok := readItemQuery(w, r, userID)
		if !ok {
			return
		}
		attachApplicationCredentialID(r, &query)
		result, err := s.library.QueryNamePrefixes(r.Context(), selected, query)
		if err != nil {
			s.libraryError(w, r, err)
			return
		}
		jsonResponse(w, http.StatusOK, result)
	}
}

func readInstantMixQuery(w http.ResponseWriter, r *http.Request, userID, kind string) (library.MusicMixSeed, library.InstantMixQuery, bool) {
	seed := library.MusicMixSeed{Kind: kind}
	switch kind {
	case "Artist", "Genre":
		// The older exported contract and existing clients use query Id here.
		// The pinned newer SDK omits that seed field; retaining it is deliberate.
		ids, supplied := r.URL.Query()["Id"]
		if !supplied || len(ids) != 1 {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "An instant mix requires exactly one seed Id.")
			return seed, library.InstantMixQuery{}, false
		}
		if _, valid := positiveEntityID(ids[0]); !valid {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "Music entity seeds require a positive decimal Id.")
			return seed, library.InstantMixQuery{}, false
		}
		seed.ID = ids[0]
	case "GenreName":
		seed.Name = r.PathValue("Name")
	default:
		seed.ID = r.PathValue("Id")
	}
	query, ok := readSimilarQuery(w, r, userID)
	if !ok {
		return seed, library.InstantMixQuery{}, false
	}
	return seed, library.InstantMixQuery{Query: query.Query, ExcludeArtistIds: query.ExcludeArtistIds, ExplicitSort: query.ExplicitSort}, true
}

func (s *Server) embyInstantMix(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.itemUser(w, r)
		if !ok {
			return
		}
		seed, query, ok := readInstantMixQuery(w, r, userID, kind)
		if !ok {
			return
		}
		attachApplicationCredentialID(r, &query.Query)
		// InstantMix is an explicit music queue. Suggestions and Similar played
		// defaults do not silently change this queue's requested candidate set.
		result, err := s.library.QueryInstantMix(r.Context(), seed, query)
		if err != nil {
			s.libraryError(w, r, err)
			return
		}
		s.sendMusicDiscoveryItems(w, r, userID, result)
	}
}

func (s *Server) embyMusicSimilar(artist bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.itemUser(w, r)
		if !ok {
			return
		}
		query, ok := readSimilarQuery(w, r, userID)
		if !ok {
			return
		}
		attachApplicationCredentialID(r, &query.Query)
		preferences, err := s.requestUserConfiguration(r.Context(), r, userID)
		if err != nil {
			s.identityError(w, r, err)
			return
		}
		if query.IsPlayed == nil && preferences.HidePlayedInMoreLikeThis {
			value := false
			query.IsPlayed = &value
		}
		if artist {
			result, err := s.library.QuerySimilarArtists(r.Context(), r.PathValue("Id"), query)
			if err != nil {
				s.libraryError(w, r, err)
				return
			}
			items := make([]map[string]any, 0, len(result.Items))
			for _, entity := range result.Items {
				item := s.musicEntityDTO(entity, "artists", queryValues(r.URL.Query()["Fields"]), false)
				applyItemSwitches(item, r)
				items = append(items, item)
			}
			jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount})
			return
		}
		result, err := s.library.QuerySimilarAlbums(r.Context(), r.PathValue("Id"), query)
		if err != nil {
			s.libraryError(w, r, err)
			return
		}
		s.sendMusicDiscoveryItems(w, r, userID, result)
	}
}

func (s *Server) sendMusicDiscoveryItems(w http.ResponseWriter, r *http.Request, userID string, result library.ItemResult) {
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

func musicArtistRoles(value string) (artist, albumArtist, valid bool) {
	parts := strings.Split(value, ",")
	if len(parts) == 0 || len(parts) > 2 {
		return false, false, false
	}
	for _, part := range parts {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "artist":
			if artist {
				return false, false, false
			}
			artist = true
		case "albumartist":
			if albumArtist {
				return false, false, false
			}
			albumArtist = true
		default:
			return false, false, false
		}
	}
	return artist, albumArtist, true
}

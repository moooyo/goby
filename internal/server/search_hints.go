package server

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerSearchHintRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Search/Hints", s.requireEmby(s.embySearchHints))
	// This typed extension cannot collide with physical IDs or the reserved
	// artist-name routes such as AlbumArtists, Prefixes and InstantMix.
	mux.HandleFunc("GET /emby/Search/Entities/{Id}", s.requireEmby(s.embySearchEntity))
}

func parseSearchHintsQuery(values url.Values) (library.SearchHintsQuery, error) {
	query := library.SearchHintsQuery{Limit: 20}
	term, supplied := values["SearchTerm"]
	if !supplied || len(term) != 1 {
		return query, library.ErrInvalidInput
	}
	query.SearchTerm = term[0]
	for _, field := range []struct {
		name   string
		target **bool
	}{
		{"IncludeMedia", &query.IncludeMedia}, {"IncludePeople", &query.IncludePeople},
		{"IncludeGenres", &query.IncludeGenres}, {"IncludeStudios", &query.IncludeStudios},
		{"IncludeArtists", &query.IncludeArtists}, {"IsMovie", &query.IsMovie}, {"IsSeries", &query.IsSeries},
	} {
		if entries, supplied := values[field.name]; supplied {
			if len(entries) != 1 {
				return query, library.ErrInvalidInput
			}
			value, err := strconv.ParseBool(entries[0])
			if err != nil {
				return query, library.ErrInvalidInput
			}
			*field.target = &value
		}
	}
	for _, field := range []struct {
		name    string
		target  *int
		maximum int64
	}{{"StartIndex", &query.StartIndex, 1<<31 - 1}, {"Limit", &query.Limit, 1000}} {
		if entries, supplied := values[field.name]; supplied {
			if len(entries) != 1 {
				return query, library.ErrInvalidInput
			}
			value, err := strconv.ParseInt(entries[0], 10, 32)
			if err != nil || value < 0 || value > field.maximum {
				return query, library.ErrInvalidInput
			}
			*field.target = int(value)
		}
	}
	for _, field := range []struct {
		name   string
		target *[]string
	}{
		{"IncludeItemTypes", &query.IncludeItemTypes}, {"ExcludeItemTypes", &query.ExcludeItemTypes},
		{"MediaTypes", &query.MediaTypes},
	} {
		entries, supplied := values[field.name]
		if supplied {
			if len(entries) > 32 {
				return query, library.ErrInvalidInput
			}
			*field.target = queryValues(entries)
			if len(*field.target) == 0 || len(*field.target) > 32 {
				return query, library.ErrInvalidInput
			}
		}
	}
	// These classifications have no admitted source facts. Even explicit false
	// must not silently claim that unknown items were classified successfully.
	for _, name := range []string{"IsNews", "IsKids", "IsSports"} {
		if _, supplied := values[name]; supplied {
			return query, library.ErrInvalidInput
		}
	}
	for _, name := range []string{"UserId", "EnableImages"} {
		if entries, supplied := values[name]; supplied {
			if len(entries) != 1 || entries[0] == "" {
				return query, library.ErrInvalidInput
			}
			if name == "EnableImages" {
				if _, err := strconv.ParseBool(entries[0]); err != nil {
					return query, library.ErrInvalidInput
				}
			}
		}
	}
	return query, nil
}

func (s *Server) embySearchHints(w http.ResponseWriter, r *http.Request) {
	query, err := parseSearchHintsQuery(r.URL.Query())
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Search hints require one SearchTerm and valid supported search controls.")
		return
	}
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	result, err := s.library.SearchHints(r.Context(), requestLibrarySubject(r, userID), query)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	images := true
	if raw := r.URL.Query().Get("EnableImages"); raw != "" {
		images, _ = strconv.ParseBool(raw)
	}
	hints := make([]map[string]any, 0, len(result.SearchHints))
	for _, hint := range result.SearchHints {
		hints = append(hints, s.searchHintDTO(hint, userID, images))
	}
	jsonResponse(w, http.StatusOK, map[string]any{"SearchHints": hints, "TotalRecordCount": result.TotalRecordCount})
}

func (s *Server) embySearchEntity(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	id, valid := positiveEntityID(r.PathValue("Id"))
	if !valid {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "An entity reference requires a canonical positive decimal ID.")
		return
	}
	entity, err := s.library.GetEntityByIDFor(r.Context(), requestLibrarySubject(r, userID), id)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	dto := s.entityDTO(entity, queryValues(r.URL.Query()["Fields"]), true)
	dto["GobyReference"] = map[string]string{"Kind": "Entity", "Id": strconv.FormatInt(entity.ID, 10)}
	dto["GobyNavigationUrl"] = searchHintURL("/emby/Search/Entities/"+strconv.FormatInt(entity.ID, 10), userID, "")
	applyItemSwitches(dto, r)
	jsonResponse(w, http.StatusOK, dto)
}

func (s *Server) searchHintDTO(hint library.SearchHint, userID string, images bool) map[string]any {
	dto := map[string]any{
		"Id": hint.Reference.ID, "ItemId": hint.Reference.ID,
		"GobyReference": map[string]string{"Kind": hint.Reference.Kind, "Id": hint.Reference.ID},
	}
	imageBase := ""
	if item := hint.Item; item != nil {
		dto["Name"], dto["MatchedTerm"], dto["Type"], dto["IsFolder"] = item.Name, item.Name, item.Type, item.IsFolder
		dto["GobyNavigationUrl"] = searchHintURL("/emby/Items/"+url.PathEscape(item.ID), userID, "")
		imageBase = "/emby/Items/" + url.PathEscape(item.ID) + "/Images/"
		if item.Metadata != nil && item.Metadata.ProductionYear != nil {
			dto["ProductionYear"] = *item.Metadata.ProductionYear
		}
		if item.Type == "Season" || item.Type == "Episode" || item.Type == "Audio" {
			dto["IndexNumber"] = item.IndexNumber
		}
		if item.Type == "Episode" || item.Type == "Audio" {
			dto["ParentIndexNumber"] = item.ParentIndexNumber
		}
		if item.Series != nil {
			dto["Series"], dto["SeriesId"] = item.Series.Name, item.Series.ID
		}
		if item.Media != nil {
			if item.Type == "Audio" {
				dto["MediaType"] = "Audio"
			} else {
				dto["MediaType"] = "Video"
			}
			dto["RunTimeTicks"] = item.Media.DurationTicks
		}
		if item.Type == "Audio" || item.Type == "MusicAlbum" {
			names, _ := musicEntityDTOs(item.Entities.Artists)
			dto["Artists"] = names
			albumArtists := item.Entities.AlbumArtists
			if item.Type == "Audio" && item.Album != nil {
				dto["Album"], dto["AlbumId"] = item.Album.Name, item.Album.ID
				if len(albumArtists) == 0 {
					albumArtists = item.Album.AlbumArtists
				}
			}
			if len(albumArtists) == 1 {
				dto["AlbumArtist"] = albumArtists[0].Name
			}
		}
	} else if entity := hint.Entity; entity != nil {
		dto["Name"], dto["MatchedTerm"], dto["Type"], dto["IsFolder"] = entity.Name, entity.Name, entity.Type, true
		dto["GobyNavigationUrl"] = searchHintURL("/emby/Search/Entities/"+url.PathEscape(hint.Reference.ID), userID, "")
		family := map[string]string{"Person": "Persons", "Genre": "Genres", "Studio": "Studios", "MusicArtist": "Artists"}[entity.Type]
		if family != "" {
			imageBase = "/emby/" + family + "/" + url.PathEscape(entity.Name) + "/Images/"
		}
	}
	if images && imageBase != "" {
		addSearchHintImages(dto, hint, imageBase, userID)
	}
	return dto
}

func addSearchHintImages(dto map[string]any, hint library.SearchHint, imageBase, userID string) {
	seen := map[string]bool{}
	for _, image := range hint.Images {
		if image.Tag == "" || image.ImageIndex != 0 || seen[image.ImageType] {
			continue
		}
		switch image.ImageType {
		case "Primary", "Thumb", "Backdrop":
		default:
			continue
		}
		seen[image.ImageType] = true
		dto[image.ImageType+"ImageUrl"] = searchHintURL(imageBase+image.ImageType+"/0", userID, image.Tag)
		if hint.LegacyImageReference {
			dto[image.ImageType+"ImageTag"] = image.Tag
			if image.ImageType != "Primary" {
				dto[image.ImageType+"ImageItemId"] = hint.Reference.ID
			}
		}
		if image.ImageType == "Primary" && image.Width > 0 && image.Height > 0 {
			dto["PrimaryImageAspectRatio"] = float64(image.Width) / float64(image.Height)
		}
	}
}

// URLs are relative to this server and preserve escaped path segments. UserId
// scopes the next authorized read; authentication tokens never enter a DTO URL.
func searchHintURL(path, userID, tag string) string {
	values := url.Values{}
	if userID != "" {
		values.Set("UserId", userID)
	}
	if tag != "" {
		values.Set("tag", tag)
	}
	if encoded := values.Encode(); encoded != "" {
		return path + "?" + encoded
	}
	return path
}

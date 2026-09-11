package server

import (
	"net/http"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func (s *Server) embySpecialFeatures(w http.ResponseWriter, r *http.Request) {
	s.embyExtraItems(w, r, false)
}

func (s *Server) embyLocalTrailers(w http.ResponseWriter, r *http.Request) {
	s.embyExtraItems(w, r, true)
}

func (s *Server) embyExtraItems(w http.ResponseWriter, r *http.Request, trailers bool) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	query := s.library.QuerySpecialFeatures
	if trailers {
		query = s.library.QueryLocalTrailers
	}
	result, err := query(r.Context(), r.PathValue("Id"), requestLibrarySubject(r, userID))
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	fields := queryValues(r.URL.Query()["Fields"])
	items := make([]map[string]any, 0, len(result))
	for _, entry := range result {
		item := s.itemDTOForRequest(r, entry, fields, false)
		applyItemSwitches(item, r)
		items = append(items, item)
	}
	if !s.applyIndexedImages(w, r, userID, items, false) {
		return
	}
	jsonResponse(w, http.StatusOK, items)
}

// Extra list defaults and the observed detailed Fields projection differ from
// ordinary browse defaults. Keep both list and direct identity mapping here,
// while the media source continues to use its own filename and stable item ID.
func applyExtraItemDTO(dto map[string]any, item library.Item, fields []string, detail bool) {
	if item.Type != "Video" || item.ThemeKind != "" {
		return
	}
	switch item.ExtraKind {
	case library.ExtraKindClip:
		dto["ExtraType"] = "Clip"
	case library.ExtraKindDeletedScene:
		dto["ExtraType"] = "DeletedScene"
	case library.ExtraKindTrailer:
		dto["ExtraType"], dto["Type"] = "Trailer", "Trailer"
		// Native controls are independent per field. Their explicit presence,
		// including a saved lock or a value equal to an automatic name, keeps
		// the effective resource value supplied by the authorized item query.
		if !item.ExtraNameControlled {
			dto["Name"] = item.ExtraOwnerName + " - Trailer"
		}
		if !item.ExtraSortNameControlled {
			dto["SortName"] = item.ExtraOwnerName + " - Trailer"
		}
		if len(item.Entities.People) == 0 && (item.Metadata == nil || len(item.Metadata.People) == 0) {
			delete(dto, "People")
		}
	default:
		return
	}
	for _, field := range []string{"ParentId", "SortName", "DateCreated"} {
		if !detail && !hasField(fields, field) {
			delete(dto, field)
		}
	}
	if item.Overview == "" {
		delete(dto, "Overview")
	}
	// Neither recorded list nor direct-item projection has VideoType. The
	// direct non-trailer details report one physical part for each resource.
	delete(dto, "VideoType")
	if detail && item.ExtraKind != library.ExtraKindTrailer {
		dto["PartCount"] = 1
	}
	if item.Media != nil && (detail || hasField(fields, "MediaSources") || hasField(fields, "MediaStreams")) {
		dto["Container"] = media.CanonicalContainer(*item.Media, item.Path)
		dto["Bitrate"], dto["Size"] = item.Media.Bitrate, item.Media.Size
	}
}

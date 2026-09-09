package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerEntityRoutes(mux *http.ServeMux) {
	for _, route := range []struct{ path, kind string }{
		{path: "Genres", kind: "genre"},
		{path: "Tags", kind: "tag"},
		{path: "Studios", kind: "studio"},
		{path: "Persons", kind: "person"},
	} {
		mux.HandleFunc("GET /emby/"+route.path, s.requireEmby(s.entityList(route.kind)))
		if route.kind != "tag" {
			mux.HandleFunc("GET /emby/"+route.path+"/{Name}", s.requireEmby(s.entityByName(route.kind)))
		}
	}
}

func (s *Server) entityList(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.itemUser(w, r)
		if !ok {
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
		result, err := s.library.ListEntities(r.Context(), kind, query)
		if err != nil {
			s.libraryError(w, r, err)
			return
		}
		items := make([]map[string]any, 0, len(result.Items))
		if !zeroLimit {
			for _, entity := range result.Items {
				// Tag lists use UserLibrary.TagItem rather than BaseItemDto.
				if kind == "tag" {
					items = append(items, map[string]any{"Name": entity.Name, "Id": strconv.FormatInt(entity.ID, 10)})
					continue
				}
				dto := s.entityDTO(entity, queryValues(r.URL.Query()["Fields"]), false)
				applyItemSwitches(dto, r)
				items = append(items, dto)
			}
		}
		jsonResponse(w, 200, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount})
	}
}

func (s *Server) entityByName(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.itemUser(w, r)
		if !ok {
			return
		}
		entity, err := s.library.GetEntityFor(r.Context(), requestLibrarySubject(r, userID), kind, r.PathValue("Name"))
		if err != nil {
			s.libraryError(w, r, err)
			return
		}
		dto := s.entityDTO(entity, queryValues(r.URL.Query()["Fields"]), true)
		applyItemSwitches(dto, r)
		jsonResponse(w, 200, dto)
	}
}

func (s *Server) entityDTO(entity library.Entity, fields []string, detail bool) map[string]any {
	dto := map[string]any{
		"Name": entity.Name, "Id": strconv.FormatInt(entity.ID, 10), "Type": entity.Type, "ServerId": s.serverID,
		"ImageTags": map[string]string{}, "BackdropImageTags": []string{},
	}
	if detail || hasField(fields, "SortName") {
		dto["SortName"] = entity.Name
	}
	if detail || hasField(fields, "ProviderIds") {
		dto["ProviderIds"] = map[string]string{}
	}
	if detail {
		dto["CanDelete"] = false
		dto["CanDownload"] = false
		dto["ExternalUrls"] = []map[string]string{}
		dto["Taglines"] = []string{}
		dto["RemoteTrailers"] = []map[string]string{}
		dto["LockedFields"] = []string{}
		dto["LockData"] = false
	}
	// Visible source counts are available in the domain, but no observed
	// reference contract exposes them by default on these entity responses.
	return dto
}

func readEntityFilters(w http.ResponseWriter, r *http.Request, query *library.Query) bool {
	values := r.URL.Query()
	query.Genres = entityNameValues(values["Genres"])
	query.Tags = entityNameValues(values["Tags"])
	query.Studios = entityNameValues(values["Studios"])
	query.Person = values.Get("Person")
	query.PersonTypes = queryValues(values["PersonTypes"])
	for _, field := range []struct {
		name   string
		target *[]int64
	}{
		{name: "GenreIds", target: &query.GenreIds},
		{name: "TagIds", target: &query.TagIds},
		{name: "StudioIds", target: &query.StudioIds},
	} {
		ids, valid := entityIDValues(values[field.name])
		if !valid {
			apiError(w, r, 400, "invalid_input", field.name+" must contain positive decimal 64-bit identifiers.")
			return false
		}
		*field.target = ids
	}
	people, valid := entityIDValues(values["PersonIds"])
	if !valid {
		apiError(w, r, 400, "invalid_input", "PersonIds must contain positive decimal 64-bit identifiers.")
		return false
	}
	query.PersonIds = make([]string, 0, len(people))
	for _, id := range people {
		query.PersonIds = append(query.PersonIds, strconv.FormatInt(id, 10))
	}
	return true
}

func entityNameValues(values []string) []string {
	var names []string
	for _, value := range values {
		for _, name := range strings.Split(value, "|") {
			if name = strings.TrimSpace(name); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func entityIDValues(values []string) ([]int64, bool) {
	var ids []int64
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		// StudioIds is documented as pipe-separated. Other clients also use
		// comma-separated entity IDs, so both delimiters share strict parsing.
		for _, raw := range strings.Split(strings.ReplaceAll(value, "|", ","), ",") {
			id, valid := positiveEntityID(strings.TrimSpace(raw))
			if !valid {
				return nil, false
			}
			ids = append(ids, id)
		}
	}
	return ids, true
}

func positiveEntityID(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0
}

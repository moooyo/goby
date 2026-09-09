package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerAdminMetadataRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/libraries/{id}/items", s.requireAdmin(s.adminMetadataItems))
	mux.HandleFunc("GET /admin/v1/items/{id}/metadata", s.requireAdmin(s.adminItemMetadata))
	mux.HandleFunc("PUT /admin/v1/items/{id}/metadata", s.requireAdmin(s.updateAdminItemMetadata))
}

func (s *Server) adminMetadataItems(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok {
		return
	}
	query, err := parseAdminMetadataQuery(r)
	if err != nil {
		s.adminMetadataError(w, r, err)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.library.QueryMetadataItems(r.Context(), actor, id, query)
	if err != nil {
		s.adminMetadataError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, nativeMetadataItems(result, query))
}

func nativeMetadataItems(result library.MetadataItemResult, query library.MetadataItemQuery) map[string]any {
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, map[string]any{
			"Id": item.ItemID, "LibraryId": item.LibraryID, "ParentId": item.ParentID,
			"ParentName": item.ParentName, "Name": item.Name, "Type": item.Type, "Path": item.Path,
			"IsFolder": item.IsFolder, "IndexNumber": item.IndexNumber, "ParentIndexNumber": item.ParentIndexNumber,
			"ProductionYear": item.ProductionYear, "HasOverrides": item.HasOverrides, "LockedFieldCount": item.LockedFieldCount,
		})
	}
	return map[string]any{
		"Library": map[string]any{"Id": result.Library.ID, "Name": result.Library.Name, "CollectionType": result.Library.CollectionType},
		"Items":   items, "TotalRecordCount": result.TotalRecordCount, "StartIndex": query.StartIndex, "Limit": query.Limit,
	}
}

func (s *Server) adminItemMetadata(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok || !adminMetadataNoQuery(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	detail, err := s.library.GetItemMetadata(r.Context(), actor, id)
	if err != nil {
		s.adminMetadataError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, nativeItemMetadata(detail))
}

func (s *Server) updateAdminItemMetadata(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok || !adminMetadataNoQuery(w, r) {
		return
	}
	edit, ok := decodeAdminMetadataEdit(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	detail, err := s.library.UpdateItemMetadata(r.Context(), actor, id, edit)
	if err != nil {
		s.adminMetadataError(w, r, err)
		return
	}
	// Values and local paths stay out of the mutation log.
	s.log.Info("administrator metadata mutation", "actor_id", actor.User.ID, "item_id", id, "revision", detail.Revision)
	jsonResponse(w, http.StatusOK, nativeItemMetadata(detail))
}

func nativeItemMetadata(detail library.ItemMetadataDetail) map[string]any {
	lastEditedAt := detail.LastEditedAt
	if lastEditedAt != nil {
		utc := lastEditedAt.UTC()
		lastEditedAt = &utc
	}
	return map[string]any{
		"Item": map[string]any{
			"Id": detail.ItemID, "LibraryId": detail.LibraryID, "ParentId": detail.ParentID,
			"ParentName": detail.ParentName, "Name": detail.Name, "Type": detail.Type, "Path": detail.Path, "IsFolder": detail.IsFolder,
		},
		"Revision": detail.Revision, "Automatic": detail.Automatic, "Effective": detail.Effective,
		"Overrides": detail.Overrides, "LockedValues": detail.LockedValues,
		"LockedFields": append([]string{}, detail.LockedFields...), "EditableFields": append([]string{}, detail.EditableFields...),
		"InactiveFields": append([]string{}, detail.InactiveFields...),
		"LastEditedBy":   detail.LastEditedBy, "LastEditedAt": lastEditedAt,
	}
}

func adminMetadataID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if id == "" || len(id) > 256 || !utf8.ValidString(id) || strings.TrimSpace(id) != id || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		adminMetadataInputError(w, r, map[string]string{"Id": "Supply an identifier of 1 to 256 UTF-8 bytes without surrounding whitespace or control characters."})
		return "", false
	}
	return id, true
}

func adminMetadataNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" {
		adminMetadataInputError(w, r, map[string]string{"Query": "This operation does not accept query parameters."})
		return false
	}
	return true
}

func parseAdminMetadataQuery(r *http.Request) (library.MetadataItemQuery, error) {
	result := library.MetadataItemQuery{Limit: 50}
	invalid := func(field, message string) (library.MetadataItemQuery, error) {
		return library.MetadataItemQuery{}, &library.MetadataValidationError{Fields: map[string]string{field: message}}
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return invalid("Query", "Supply a valid URL query.")
	}
	for name, entries := range values {
		if name != "SearchTerm" && name != "Types" && name != "StartIndex" && name != "Limit" {
			return invalid("Query", "The query contains an unsupported parameter.")
		}
		if len(entries) != 1 || !utf8.ValidString(entries[0]) {
			return invalid(name, "Supply this UTF-8 parameter exactly once.")
		}
	}
	rawSearch := values.Get("SearchTerm")
	if len(rawSearch) > 1024 || strings.IndexFunc(rawSearch, unicode.IsControl) >= 0 {
		return invalid("SearchTerm", "Use at most 1024 UTF-8 bytes without control characters.")
	}
	result.SearchTerm = strings.TrimSpace(rawSearch)
	for _, field := range []struct {
		name     string
		min, max int64
		target   *int
	}{{"StartIndex", 0, math.MaxInt32, &result.StartIndex}, {"Limit", 1, 200, &result.Limit}} {
		if entries, exists := values[field.name]; exists {
			value, err := strconv.ParseInt(entries[0], 10, 32)
			if err != nil || value < field.min || value > field.max || strconv.FormatInt(value, 10) != entries[0] {
				return invalid(field.name, "Supply a canonical integer within the supported pagination range.")
			}
			*field.target = int(value)
		}
	}
	if raw := values.Get("Types"); raw != "" {
		allowed := map[string]bool{"Movie": true, "Video": true, "Series": true, "Season": true, "Episode": true,
			"Folder": true, "MusicArtist": true, "MusicAlbum": true, "Audio": true}
		seen := map[string]bool{}
		for _, value := range strings.Split(raw, ",") {
			if !allowed[value] || seen[value] {
				return invalid("Types", "Supply distinct supported item types separated by commas.")
			}
			seen[value] = true
			result.Types = append(result.Types, value)
		}
	}
	return result, nil
}

func adminMetadataInputError(w http.ResponseWriter, r *http.Request, fields map[string]string) {
	requestID, _ := r.Context().Value(requestIDKey).(string)
	jsonResponse(w, http.StatusBadRequest, map[string]any{
		"Error": map[string]any{"Code": "invalid_input", "Message": "Check the highlighted metadata fields.", "Fields": fields}, "RequestId": requestID,
	})
}

func (s *Server) adminMetadataError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *library.MetadataValidationError
	switch {
	case errors.Is(err, library.ErrRevisionConflict):
		apiError(w, r, http.StatusConflict, "revision_conflict", "This item's metadata or automatic source changed. Reload it before saving again.")
	case errors.As(err, &validation):
		adminMetadataInputError(w, r, validation.Fields)
	case errors.Is(err, identity.ErrUnauthorized):
		s.identityError(w, r, err)
	default:
		s.libraryError(w, r, err)
	}
}

func decodeAdminMetadataEdit(w http.ResponseWriter, r *http.Request) (library.MetadataEdit, bool) {
	var edit library.MetadataEdit
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this request.")
		return edit, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(data) || !metadataUniqueJSON(data) {
		adminMetadataInputError(w, r, map[string]string{"Body": "Supply one UTF-8 JSON object within 1 MiB, without duplicate fields or excessive nesting."})
		return edit, false
	}
	values, invalid := managedUserObject(data, []string{"Revision", "Overrides", "LockedFields"}, "")
	if invalid != nil {
		adminMetadataInputError(w, r, invalid)
		return edit, false
	}
	invalid = map[string]string{}
	revision := managedUserRevision(values["Revision"], invalid)
	edit.Revision = strconv.FormatInt(revision, 10)
	managedUserValue(values["Overrides"], "Overrides", &edit.Overrides, invalid)
	var locks []json.RawMessage
	managedUserValue(values["LockedFields"], "LockedFields", &locks, invalid)
	edit.LockedFields = make([]string, len(locks))
	for index, lock := range locks {
		managedUserValue(lock, "LockedFields", &edit.LockedFields[index], invalid)
	}
	if len(invalid) != 0 {
		adminMetadataInputError(w, r, invalid)
		return library.MetadataEdit{}, false
	}
	return edit, true
}

// Preserve sparse override values while rejecting duplicate keys at every level.
// Decoding directly into a map would silently accept last-key-wins mutations.
func metadataUniqueJSON(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	nodes := 0
	var walk func(int) bool
	walk = func(depth int) bool {
		nodes++
		if depth > 16 || nodes > 65536 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !walk(depth + 1) {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim('}')
		case json.Delim('['):
			for decoder.More() {
				if !walk(depth + 1) {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim(']')
		default:
			_, delimiter := token.(json.Delim)
			return !delimiter
		}
	}
	if !walk(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

const (
	collectionInputBytes = 256 * 1024
	collectionInputCount = 1000
	collectionIDBytes    = 128
)

func (s *Server) registerCollectionRoutes(mux *http.ServeMux) {
	for _, route := range []struct {
		path, kind string
		admin      bool
	}{
		{"/emby/Playlists", library.PlaylistKind, false},
		{"/emby/Collections", library.BoxSetKind, false},
		{"/admin/v1/playlists", library.PlaylistKind, true},
		{"/admin/v1/collections", library.BoxSetKind, true},
	} {
		kind := route.kind
		authenticate := s.requireCollectionEmby
		itemsPath, deletePath := "Items", "Delete"
		if route.admin {
			authenticate = s.requireCollectionAdmin
			itemsPath, deletePath = "items", "delete"
			mux.HandleFunc("GET "+route.path, authenticate(func(w http.ResponseWriter, r *http.Request) {
				s.listCollections(w, r, kind)
			}))
		}
		mux.HandleFunc("POST "+route.path, authenticate(func(w http.ResponseWriter, r *http.Request) {
			s.createCollection(w, r, kind)
		}))
		mux.HandleFunc("GET "+route.path+"/{Id}", authenticate(func(w http.ResponseWriter, r *http.Request) {
			s.getCollection(w, r, kind)
		}))
		for _, method := range []string{http.MethodPost, http.MethodPatch} {
			mux.HandleFunc(method+" "+route.path+"/{Id}", authenticate(func(w http.ResponseWriter, r *http.Request) {
				s.updateCollection(w, r, kind)
			}))
		}
		mux.HandleFunc("DELETE "+route.path+"/{Id}", authenticate(func(w http.ResponseWriter, r *http.Request) {
			s.deleteCollection(w, r, kind)
		}))
		mux.HandleFunc("GET "+route.path+"/{Id}/"+itemsPath, authenticate(func(w http.ResponseWriter, r *http.Request) {
			s.collectionItems(w, r, kind)
		}))
		mux.HandleFunc("POST "+route.path+"/{Id}/"+itemsPath, authenticate(func(w http.ResponseWriter, r *http.Request) {
			s.addCollectionItems(w, r, kind)
		}))
		for _, methodPath := range []string{"DELETE " + route.path + "/{Id}/" + itemsPath, "POST " + route.path + "/{Id}/" + itemsPath + "/" + deletePath} {
			mux.HandleFunc(methodPath, authenticate(func(w http.ResponseWriter, r *http.Request) {
				s.removeCollectionItems(w, r, kind)
			}))
		}
	}
	mux.HandleFunc("GET /emby/Playlists/{Id}/AddToPlaylistInfo", s.requireCollectionEmby(s.previewPlaylistItems))
	mux.HandleFunc("POST /emby/Playlists/{Id}/Items/{ItemId}/Move/{NewIndex}", s.requireCollectionEmby(s.movePlaylistEntry))
	mux.HandleFunc("GET /admin/v1/playlists/{Id}/items/preview", s.requireCollectionAdmin(s.previewPlaylistItems))
	mux.HandleFunc("POST /admin/v1/playlists/{Id}/items/{ItemId}/move/{NewIndex}", s.requireCollectionAdmin(s.movePlaylistEntry))
}

func (s *Server) requireCollectionEmby(next http.HandlerFunc) http.HandlerFunc {
	return s.requireEmby(func(w http.ResponseWriter, r *http.Request) {
		principal := r.Context().Value(principalKey).(identity.Principal)
		next(w, r.WithContext(library.WithCollectionActor(r.Context(), principal)))
	})
}

func (s *Server) requireCollectionAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		principal := r.Context().Value(principalKey).(identity.Principal)
		ctx := library.WithCollectionAdministrator(r.Context(), principal)
		next(w, r.WithContext(ctx))
	})
}

func collectionInputError(w http.ResponseWriter, r *http.Request) {
	apiError(w, r, http.StatusBadRequest, "invalid_input", "Check the collection fields, identifiers, and query parameters.")
}

func collectionMutationResponse(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/admin/v1/") {
		jsonResponse(w, http.StatusOK, map[string]any{})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func collectionQuery(w http.ResponseWriter, r *http.Request, allowed ...string) (url.Values, bool) {
	if len(r.URL.RawQuery) > collectionInputBytes {
		collectionInputError(w, r)
		return nil, false
	}
	values, err := embyBusinessQuery(r)
	if err != nil {
		collectionInputError(w, r)
		return nil, false
	}
	seen := make(map[string]bool, len(values))
	for name, entries := range values {
		lower := strings.ToLower(name)
		if len(entries) != 1 || seen[lower] {
			collectionInputError(w, r)
			return nil, false
		}
		seen[lower] = true
		if lower == "reqformat" && entries[0] == "json" {
			delete(values, name)
			continue
		}
		if !slices.Contains(allowed, name) {
			collectionInputError(w, r)
			return nil, false
		}
	}
	return values, true
}

func collectionText(value string, maxBytes int, allowEmpty bool) bool {
	return len(value) <= maxBytes && utf8.ValidString(value) && strings.TrimSpace(value) == value &&
		strings.IndexFunc(value, unicode.IsControl) < 0 && (allowEmpty || value != "")
}

func collectionName(value string) bool {
	return collectionText(value, 1024, false) && utf8.RuneCountInString(value) <= 256
}

// A projected UserId must never become an ordinary session's owner authority.
// Application credentials retain their own authority and select an explicit
// owner when creating a user-owned collection.
func (s *Server) collectionSubject(w http.ResponseWriter, r *http.Request, userID string, create bool) (library.Subject, bool) {
	principal, ok := r.Context().Value(principalKey).(identity.Principal)
	if !ok {
		embyTextError(w, r, http.StatusUnauthorized, embyInvalidTokenMessage)
		return library.Subject{}, false
	}
	if !collectionText(userID, collectionIDBytes, true) {
		collectionInputError(w, r)
		return library.Subject{}, false
	}
	if principal.IsApplicationKey() {
		if create && userID == "" {
			collectionInputError(w, r)
			return library.Subject{}, false
		}
		return librarySubject(principal, userID), true
	}
	if userID != "" && userID != principal.User.ID {
		apiError(w, r, http.StatusForbidden, "access_denied", "A collection request cannot impersonate another user.")
		return library.Subject{}, false
	}
	return librarySubject(principal, principal.User.ID), true
}

func collectionObject(data []byte, allowed ...string) (map[string]json.RawMessage, error) {
	if !utf8.Valid(data) || !adminSettingsUnicode(data) {
		return nil, library.ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, library.ErrInvalidInput
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || !slices.Contains(allowed, name) {
			return nil, library.ErrInvalidInput
		}
		if _, exists := fields[name]; exists {
			return nil, library.ErrInvalidInput
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, library.ErrInvalidInput
		}
		fields[name] = value
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') {
		return nil, library.ErrInvalidInput
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, library.ErrInvalidInput
	}
	return fields, nil
}

func readCollectionObject(w http.ResponseWriter, r *http.Request, allowed ...string) (map[string]json.RawMessage, bool) {
	if r.Body == nil || r.Body == http.NoBody {
		return map[string]json.RawMessage{}, true
	}
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || len(r.Header.Values("Content-Type")) != 1 ||
		(mediaType != "application/json" && mediaType != "text/plain") ||
		len(parameters) > 1 || len(parameters) == 1 && !strings.EqualFold(parameters["charset"], "utf-8") ||
		r.Header.Get("Content-Encoding") != "" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use an uncompressed UTF-8 JSON object for this request.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, collectionInputBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			apiError(w, r, http.StatusRequestEntityTooLarge, "invalid_input", "The collection request exceeds the supported size.")
		} else if errors.Is(err, context.DeadlineExceeded) {
			apiError(w, r, http.StatusRequestTimeout, "request_timeout", "The request body was not received within its time limit.")
		} else {
			collectionInputError(w, r)
		}
		return nil, false
	}
	fields, err := collectionObject(data, allowed...)
	if err != nil {
		collectionInputError(w, r)
		return nil, false
	}
	return fields, true
}

func collectionString(values url.Values, body map[string]json.RawMessage, name string) (string, bool, bool) {
	value, present := values[name]
	var result string
	if present {
		result = value[0]
	}
	if raw, exists := body[name]; exists {
		var decoded string
		if json.Unmarshal(raw, &decoded) != nil || present && result != decoded {
			return "", false, false
		}
		result, present = decoded, true
	}
	return result, present, true
}

func collectionBoolean(values url.Values, body map[string]json.RawMessage, name string) (*bool, bool) {
	var result *bool
	if entries, present := values[name]; present {
		if entries[0] != "true" && entries[0] != "false" {
			return nil, false
		}
		parsed := entries[0] == "true"
		result = &parsed
	}
	if raw, present := body[name]; present {
		var parsed bool
		if json.Unmarshal(raw, &parsed) != nil || result != nil && *result != parsed {
			return nil, false
		}
		result = &parsed
	}
	return result, true
}

func collectionIDs(values url.Values, body map[string]json.RawMessage, name string, required bool) ([]string, bool) {
	var ids []string
	entries, present := values[name]
	if present && entries[0] != "" {
		ids = strings.Split(entries[0], ",")
		for i := range ids {
			if strings.IndexFunc(ids[i], unicode.IsControl) >= 0 {
				return nil, false
			}
			ids[i] = strings.TrimSpace(ids[i])
		}
	}
	if raw, exists := body[name]; exists {
		var decoded []string
		if json.Unmarshal(raw, &decoded) != nil || present && !slices.Equal(ids, decoded) {
			return nil, false
		}
		ids = decoded
	}
	if len(ids) > collectionInputCount || required && len(ids) == 0 {
		return nil, false
	}
	for _, id := range ids {
		if !collectionText(id, collectionIDBytes, false) {
			return nil, false
		}
	}
	return ids, true
}

func readCollectionCreate(w http.ResponseWriter, r *http.Request) (library.CollectionInput, string, bool) {
	var input library.CollectionInput
	allowed := []string{"Name", "ParentId", "MediaType", "IsPublic", "IsLocked", "Ids", "UserId"}
	values, ok := collectionQuery(w, r, allowed...)
	if !ok {
		return input, "", false
	}
	body, ok := readCollectionObject(w, r, allowed...)
	if !ok {
		return input, "", false
	}
	var userID string
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"Name", &input.Name}, {"ParentId", &input.ParentID}, {"MediaType", &input.MediaType}, {"UserId", &userID},
	} {
		value, _, valid := collectionString(values, body, field.name)
		if !valid {
			collectionInputError(w, r)
			return input, "", false
		}
		*field.target = value
	}
	for _, field := range []struct {
		name   string
		target *bool
	}{
		{"IsPublic", &input.IsPublic}, {"IsLocked", &input.IsLocked},
	} {
		value, valid := collectionBoolean(values, body, field.name)
		if !valid {
			collectionInputError(w, r)
			return input, "", false
		}
		if value != nil {
			*field.target = *value
		}
	}
	input.ItemIDs, ok = collectionIDs(values, body, "Ids", false)
	if !ok || !collectionName(input.Name) || !collectionText(input.ParentID, collectionIDBytes, true) ||
		!collectionText(userID, collectionIDBytes, true) || !slices.Contains([]string{"", "Audio", "Video"}, input.MediaType) {
		collectionInputError(w, r)
		return input, "", false
	}
	return input, userID, true
}

func readCollectionPatch(w http.ResponseWriter, r *http.Request) (library.CollectionPatch, string, bool) {
	var patch library.CollectionPatch
	values, ok := collectionQuery(w, r, "UserId", "Name", "IsPublic", "IsLocked")
	if !ok {
		return patch, "", false
	}
	body, ok := readCollectionObject(w, r, "Name", "IsPublic", "IsLocked", "Shares")
	if !ok {
		return patch, "", false
	}
	name, present, valid := collectionString(values, body, "Name")
	if !valid || present && !collectionName(name) {
		collectionInputError(w, r)
		return patch, "", false
	}
	if present {
		patch.Name = &name
	}
	patch.IsPublic, valid = collectionBoolean(values, body, "IsPublic")
	if !valid {
		collectionInputError(w, r)
		return patch, "", false
	}
	patch.IsLocked, valid = collectionBoolean(values, body, "IsLocked")
	if !valid {
		collectionInputError(w, r)
		return patch, "", false
	}
	if raw, present := body["Shares"]; present {
		var entries []json.RawMessage
		if json.Unmarshal(raw, &entries) != nil || len(entries) > collectionInputCount {
			collectionInputError(w, r)
			return patch, "", false
		}
		shares := make([]library.CollectionShare, 0, len(entries))
		seen := make(map[string]bool, len(entries))
		for _, rawEntry := range entries {
			entry, err := collectionObject(rawEntry, "UserId", "CanEdit")
			userID, _, valid := collectionString(nil, entry, "UserId")
			canEdit, validEdit := collectionBoolean(nil, entry, "CanEdit")
			if err != nil || !valid || !validEdit || !collectionText(userID, collectionIDBytes, false) || seen[userID] {
				collectionInputError(w, r)
				return patch, "", false
			}
			seen[userID] = true
			share := library.CollectionShare{UserID: userID}
			if canEdit != nil {
				share.CanEdit = *canEdit
			}
			shares = append(shares, share)
		}
		patch.Shares = &shares
	}
	if patch.Name == nil && patch.IsPublic == nil && patch.IsLocked == nil && patch.Shares == nil {
		collectionInputError(w, r)
		return patch, "", false
	}
	return patch, values.Get("UserId"), true
}

func collectionWithoutBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body != nil && r.Body != http.NoBody || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		collectionInputError(w, r)
		return false
	}
	return true
}

func readCollectionNumber(value string) (int, bool) {
	parsed, err := strconv.ParseInt(value, 10, 32)
	return int(parsed), err == nil && parsed >= 0 && strconv.FormatInt(parsed, 10) == value
}

func readCollectionPagination(w http.ResponseWriter, r *http.Request, values url.Values) (int, int, bool) {
	start, limit := 0, 100
	for _, field := range []struct {
		name   string
		target *int
	}{{"StartIndex", &start}, {"Limit", &limit}} {
		if entries, exists := values[field.name]; exists {
			value, valid := readCollectionNumber(entries[0])
			if !valid {
				collectionInputError(w, r)
				return 0, 0, false
			}
			*field.target = value
		}
	}
	return start, min(limit, 1000), true
}

func collectionDTO(info library.CollectionInfo) map[string]any {
	shares := make([]map[string]any, 0, len(info.Shares))
	for _, share := range info.Shares {
		shares = append(shares, map[string]any{"UserId": share.UserID, "CanEdit": share.CanEdit})
	}
	result := map[string]any{
		"Id": info.ID, "Name": info.Name, "Type": info.Kind, "IsFolder": true,
		"ParentId": info.ParentID, "OwnerId": info.OwnerID, "MediaType": info.MediaType,
		"IsPublic": info.IsPublic, "IsLocked": info.IsLocked, "ChildCount": info.ItemCount, "Shares": shares,
	}
	if info.UserData != nil {
		result["UserData"] = userDataDTO(*info.UserData, false)
	}
	return result
}

func (s *Server) listCollections(w http.ResponseWriter, r *http.Request, kind string) {
	values, ok := collectionQuery(w, r, "StartIndex", "Limit", "SearchTerm")
	if !ok || !collectionWithoutBody(w, r) {
		return
	}
	if !collectionText(values.Get("SearchTerm"), 1024, true) {
		collectionInputError(w, r)
		return
	}
	start, limit, ok := readCollectionPagination(w, r, values)
	if !ok {
		return
	}
	subject, ok := s.collectionSubject(w, r, "", false)
	if !ok {
		return
	}
	result, err := s.library.QueryItems(r.Context(), library.Query{
		UserID: subject.UserID, ApplicationCredentialID: subject.ApplicationCredentialID,
		Recursive: true, IncludeItemTypes: []string{kind}, SearchTerm: values.Get("SearchTerm"),
		StartIndex: start, Limit: max(limit, 1),
	})
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	if limit != 0 {
		for _, entry := range result.Items {
			if entry.Collection == nil {
				apiError(w, r, http.StatusInternalServerError, "internal_error", "The collection metadata could not be loaded.")
				return
			}
			items = append(items, collectionDTO(*entry.Collection))
		}
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount})
}

func (s *Server) createCollection(w http.ResponseWriter, r *http.Request, kind string) {
	input, userID, ok := readCollectionCreate(w, r)
	if !ok {
		return
	}
	subject, ok := s.collectionSubject(w, r, userID, true)
	if !ok {
		return
	}
	info, err := s.library.CreateCollection(r.Context(), subject, kind, input)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	result := map[string]any{"Id": info.ID, "Name": info.Name}
	if kind == library.PlaylistKind {
		result["ItemAddedCount"] = info.ItemCount
	}
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) collectionRequestSubject(w http.ResponseWriter, r *http.Request, allowed ...string) (library.Subject, url.Values, bool) {
	values, ok := collectionQuery(w, r, append(allowed, "UserId")...)
	if !ok || !collectionWithoutBody(w, r) {
		return library.Subject{}, nil, false
	}
	if !collectionText(r.PathValue("Id"), collectionIDBytes, false) {
		collectionInputError(w, r)
		return library.Subject{}, nil, false
	}
	subject, ok := s.collectionSubject(w, r, values.Get("UserId"), false)
	return subject, values, ok
}

func (s *Server) getCollection(w http.ResponseWriter, r *http.Request, kind string) {
	subject, _, ok := s.collectionRequestSubject(w, r)
	if !ok {
		return
	}
	info, err := s.library.GetCollection(r.Context(), subject, r.PathValue("Id"), kind)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, collectionDTO(info))
}

func (s *Server) updateCollection(w http.ResponseWriter, r *http.Request, kind string) {
	patch, userID, ok := readCollectionPatch(w, r)
	if !ok {
		return
	}
	if !collectionText(r.PathValue("Id"), collectionIDBytes, false) {
		collectionInputError(w, r)
		return
	}
	subject, ok := s.collectionSubject(w, r, userID, false)
	if !ok {
		return
	}
	info, err := s.library.UpdateCollection(r.Context(), subject, r.PathValue("Id"), kind, patch)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, collectionDTO(info))
}

func (s *Server) deleteCollection(w http.ResponseWriter, r *http.Request, kind string) {
	subject, _, ok := s.collectionRequestSubject(w, r)
	if !ok {
		return
	}
	if err := s.library.DeleteCollection(r.Context(), subject, r.PathValue("Id"), kind); err != nil {
		s.libraryError(w, r, err)
		return
	}
	collectionMutationResponse(w, r)
}

func (s *Server) collectionItems(w http.ResponseWriter, r *http.Request, kind string) {
	if !strings.HasPrefix(r.URL.Path, "/admin/v1/") && !normalizeItemProjectionQuery(w, r) {
		return
	}
	subject, values, ok := s.collectionRequestSubject(w, r, "StartIndex", "Limit", "Fields", "ExcludeFields", "EnableImages", "EnableUserData", "ImageTypeLimit", "EnableImageTypes")
	if !ok {
		return
	}
	start, limit, ok := readCollectionPagination(w, r, values)
	if !ok {
		return
	}
	for _, name := range []string{"EnableImages", "EnableUserData"} {
		if _, ok := collectionBoolean(values, nil, name); !ok {
			collectionInputError(w, r)
			return
		}
	}
	if entries, exists := values["ImageTypeLimit"]; exists {
		if _, ok := readCollectionNumber(entries[0]); !ok {
			collectionInputError(w, r)
			return
		}
	}
	for _, name := range []string{"Fields", "ExcludeFields", "EnableImageTypes"} {
		if !collectionText(values.Get(name), 4096, true) {
			collectionInputError(w, r)
			return
		}
	}
	result, err := s.library.CollectionItems(r.Context(), subject, r.PathValue("Id"), kind, start, max(limit, 1))
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, entry := range result.Items {
		if limit == 0 {
			break
		}
		dto := s.itemDTOForRequest(r, entry, queryValues(values["Fields"]), false)
		if kind == library.PlaylistKind {
			dto["PlaylistItemId"] = entry.PlaylistItemID
		}
		applyItemSwitches(dto, r)
		items = append(items, dto)
	}
	if !s.applyIndexedImages(w, r, subject.UserID, items, false) {
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount})
}

func (s *Server) addCollectionItems(w http.ResponseWriter, r *http.Request, kind string) {
	subject, values, ok := s.collectionRequestSubject(w, r, "Ids")
	if !ok {
		return
	}
	ids, ok := collectionIDs(values, nil, "Ids", true)
	if !ok {
		collectionInputError(w, r)
		return
	}
	count, err := s.library.AddCollectionItems(r.Context(), subject, r.PathValue("Id"), kind, ids)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	if kind == library.PlaylistKind {
		jsonResponse(w, http.StatusOK, map[string]any{"Id": r.PathValue("Id"), "ItemAddedCount": count})
		return
	}
	collectionMutationResponse(w, r)
}

func (s *Server) previewPlaylistItems(w http.ResponseWriter, r *http.Request) {
	subject, values, ok := s.collectionRequestSubject(w, r, "Ids")
	if !ok {
		return
	}
	ids, ok := collectionIDs(values, nil, "Ids", true)
	if !ok {
		collectionInputError(w, r)
		return
	}
	preview, err := s.library.PreviewCollectionItems(r.Context(), subject, r.PathValue("Id"), library.PlaylistKind, ids)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"ItemCount": preview.ItemCount, "ContainsDuplicates": preview.ContainsDuplicates})
}

func (s *Server) removeCollectionItems(w http.ResponseWriter, r *http.Request, kind string) {
	name := "Ids"
	if kind == library.PlaylistKind {
		name = "EntryIds"
	}
	subject, values, ok := s.collectionRequestSubject(w, r, name)
	if !ok {
		return
	}
	ids, ok := collectionIDs(values, nil, name, true)
	if !ok {
		collectionInputError(w, r)
		return
	}
	if err := s.library.RemoveCollectionItems(r.Context(), subject, r.PathValue("Id"), kind, ids); err != nil {
		s.libraryError(w, r, err)
		return
	}
	collectionMutationResponse(w, r)
}

func (s *Server) movePlaylistEntry(w http.ResponseWriter, r *http.Request) {
	subject, _, ok := s.collectionRequestSubject(w, r)
	if !ok {
		return
	}
	index, ok := readCollectionNumber(r.PathValue("NewIndex"))
	if !ok || !collectionText(r.PathValue("ItemId"), collectionIDBytes, false) {
		collectionInputError(w, r)
		return
	}
	if err := s.library.MoveCollectionEntry(r.Context(), subject, r.PathValue("Id"), r.PathValue("ItemId"), index); err != nil {
		s.libraryError(w, r, err)
		return
	}
	collectionMutationResponse(w, r)
}

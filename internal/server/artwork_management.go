package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

// Admission covers the upload body as well as decoding and transactional work.
// Waiting for the store's decoder budget must not retain unlimited raw bodies.
var artworkRequestSlots = make(chan struct{}, 2)

func admitArtworkMutation() (func(), bool) {
	select {
	case artworkRequestSlots <- struct{}{}:
		return func() { <-artworkRequestSlots }, true
	default:
		return nil, false
	}
}

func (s *Server) registerArtworkManagementRoutes(mux *http.ServeMux) {
	for _, kind := range []string{"items", "entities"} {
		base := "/admin/v1/" + kind + "/{id}/images"
		mux.HandleFunc("GET "+base, s.requireAdmin(s.adminArtwork(kind, "list")))
		mux.HandleFunc("GET "+base+"/{type}/{index}", s.requireAdmin(s.adminArtworkContent(kind)))
		mux.HandleFunc("PUT "+base+"/{type}/{index}", s.requireAdmin(s.adminArtwork(kind, "upload")))
		mux.HandleFunc("DELETE "+base+"/{type}/{index}", s.requireAdmin(s.adminArtwork(kind, "delete")))
		mux.HandleFunc("POST "+base+"/{type}/reorder", s.requireAdmin(s.adminArtwork(kind, "reorder")))
		mux.HandleFunc("POST "+base+"/{type}/reset", s.requireAdmin(s.adminArtwork(kind, "reset")))
	}
	mux.HandleFunc("GET /admin/v1/entities", s.requireAdmin(s.adminArtworkEntities))
	for _, suffix := range []string{"/{Type}", "/{Type}/{Index}"} {
		base := "/emby/Items/{Id}/Images" + suffix
		mux.HandleFunc("POST "+base, s.requireEmby(s.embyArtworkMutation("upload")))
		mux.HandleFunc("DELETE "+base, s.requireEmby(s.embyArtworkMutation("delete")))
		mux.HandleFunc("POST "+base+"/Delete", s.requireEmby(s.embyArtworkMutation("delete")))
	}
	mux.HandleFunc("POST /emby/Items/{Id}/Images/{Type}/{Index}/Index", s.requireEmby(s.embyArtworkMutation("move")))
	for _, route := range []struct{ path, kind, family string }{
		{"Genres", "Genre", ""}, {"Studios", "Studio", ""}, {"Persons", "Person", ""}, {"Tags", "Tag", ""},
		{"Artists", "", "artists"}, {"MusicGenres", "", "genres"},
	} {
		for _, suffix := range []string{"/{Type}", "/{Type}/{Index}"} {
			mux.HandleFunc("GET /emby/"+route.path+"/{Name}/Images"+suffix, s.requireEmby(s.entityArtworkByName(route.kind, route.family)))
		}
	}
}

func artworkNativeTarget(r *http.Request, kind string) (library.ArtworkTarget, error) {
	if kind == "items" {
		return library.ArtworkTarget{ItemID: r.PathValue("id")}, nil
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != r.PathValue("id") {
		return library.ArtworkTarget{}, library.ErrInvalidInput
	}
	return library.ArtworkTarget{EntityID: id}, nil
}

func artworkRevisionHeader(r *http.Request, required bool) (string, error) {
	values := r.Header.Values("If-Match")
	if len(values) == 0 && !required {
		return "", nil
	}
	if len(values) != 1 {
		return "", library.ErrInvalidInput
	}
	quoted := values[0]
	if len(quoted) < 3 || len(quoted) > 80 || quoted[0] != '"' || quoted[len(quoted)-1] != '"' {
		return "", library.ErrInvalidInput
	}
	value := quoted[1 : len(quoted)-1]
	if len(value) > 1 && value[0] == '0' {
		return "", library.ErrInvalidInput
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return "", library.ErrInvalidInput
		}
	}
	return value, nil
}

func nativeArtworkCollection(kind, id string, collection library.ArtworkCollection) map[string]any {
	items := make([]map[string]any, 0, len(collection.Items))
	for _, image := range collection.Items {
		source := image.Source
		if source == "" {
			source = "provider"
			if image.Path != "" {
				source = "local"
			}
		}
		preview := "/admin/v1/" + kind + "/" + url.PathEscape(id) + "/images/" + image.ImageType + "/" + strconv.Itoa(image.ImageIndex) + "?Tag=" + url.QueryEscape(image.Tag)
		items = append(items, map[string]any{"ImageType": image.ImageType, "ImageIndex": image.ImageIndex, "Tag": image.Tag,
			"Width": image.Width, "Height": image.Height, "Size": strconv.FormatInt(image.Size, 10), "PreviewUrl": preview, "Source": source})
	}
	return map[string]any{"Revision": collection.Revision, "Items": items}
}

func readArtworkBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if len(r.Header.Values("Content-Type")) != 1 {
		return nil, artwork.ErrUnsupportedFormat
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, artwork.ErrUnsupportedFormat
	}
	switch contentType {
	case "application/octet-stream", "image/jpeg", "image/png", "image/gif":
	default:
		return nil, artwork.ErrUnsupportedFormat
	}
	if r.ContentLength > artwork.ManagedUploadBytes {
		return nil, artwork.ErrLimitExceeded
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, artwork.ManagedUploadBytes))
	if err != nil || len(body) == 0 {
		return nil, artwork.ErrInvalidImage
	}
	return body, nil
}

func decodeArtworkMutation(w http.ResponseWriter, r *http.Request, reorder bool) (string, []int, error) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return "", nil, library.ErrInvalidInput
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil || !metadataUniqueJSON(data) {
		return "", nil, library.ErrInvalidInput
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return "", nil, library.ErrInvalidInput
	}
	var revision string
	if json.Unmarshal(fields["Revision"], &revision) != nil || revision == "" || len(revision) > 78 || len(revision) > 1 && revision[0] == '0' {
		return "", nil, library.ErrInvalidInput
	}
	expected := 1
	var indexes []int
	if reorder {
		expected = 2
		if json.Unmarshal(fields["Indexes"], &indexes) != nil || indexes == nil || len(indexes) > 32 {
			return "", nil, library.ErrInvalidInput
		}
	}
	if len(fields) != expected {
		return "", nil, library.ErrInvalidInput
	}
	for name := range fields {
		if name != "Revision" && !(reorder && name == "Indexes") {
			return "", nil, library.ErrInvalidInput
		}
	}
	for _, digit := range revision {
		if digit < '0' || digit > '9' {
			return "", nil, library.ErrInvalidInput
		}
	}
	return revision, indexes, nil
}

func (s *Server) adminArtwork(kind, operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			s.artworkManagementError(w, r, library.ErrInvalidInput)
			return
		}
		target, err := artworkNativeTarget(r, kind)
		if err != nil {
			s.artworkManagementError(w, r, err)
			return
		}
		actor := r.Context().Value(principalKey).(identity.Principal)
		collection, err := s.library.GetArtwork(r.Context(), actor, identity.AdministratorNative, target)
		if err != nil {
			s.artworkManagementError(w, r, err)
			return
		}
		if operation != "list" {
			release, admitted := admitArtworkMutation()
			if !admitted {
				s.artworkManagementError(w, r, artwork.ErrManagedLimit)
				return
			}
			defer release()
			imageType, err := artwork.NormalizeManagedType(r.PathValue("type"))
			if err != nil {
				s.artworkManagementError(w, r, err)
				return
			}
			var revision string
			var indexes []int
			index := 0
			if operation == "reorder" || operation == "reset" {
				revision, indexes, err = decodeArtworkMutation(w, r, operation == "reorder")
			} else {
				revision, err = artworkRevisionHeader(r, true)
				if r.Header.Get("If-Match") == "" {
					apiError(w, r, http.StatusPreconditionRequired, "artwork_revision_required", "Load the current image revision before editing.")
					return
				}
				if err == nil {
					index, err = strconv.Atoi(r.PathValue("index"))
				}
			}
			if err != nil {
				s.artworkManagementError(w, r, library.ErrInvalidInput)
				return
			}
			if revision != collection.Revision {
				s.artworkManagementError(w, r, library.ErrRevisionConflict)
				return
			}
			switch operation {
			case "upload":
				data, readErr := readArtworkBody(w, r)
				if readErr != nil {
					s.artworkManagementError(w, r, readErr)
					return
				}
				collection, err = s.library.UploadArtwork(r.Context(), actor, identity.AdministratorNative, target, revision, imageType, index, data)
			case "delete":
				if !emptyArtworkBody(r) {
					s.artworkManagementError(w, r, library.ErrInvalidInput)
					return
				}
				collection, err = s.library.DeleteArtwork(r.Context(), actor, identity.AdministratorNative, target, revision, imageType, index)
			case "reorder":
				collection, err = s.library.ReorderArtwork(r.Context(), actor, identity.AdministratorNative, target, revision, imageType, indexes)
			case "reset":
				collection, err = s.library.ResetArtwork(r.Context(), actor, identity.AdministratorNative, target, revision, imageType)
			}
			if err != nil {
				s.artworkManagementError(w, r, err)
				return
			}
		}
		w.Header().Set("ETag", strconv.Quote(collection.Revision))
		w.Header().Set("Cache-Control", "no-store")
		jsonResponse(w, http.StatusOK, nativeArtworkCollection(kind, r.PathValue("id"), collection))
	}
}

func (s *Server) adminArtworkContent(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target, err := artworkNativeTarget(r, kind)
		if err != nil {
			s.artworkManagementError(w, r, err)
			return
		}
		copy := r.Clone(r.Context())
		copy.SetPathValue("Type", r.PathValue("type"))
		copy.SetPathValue("Index", r.PathValue("index"))
		request, err := parseImageRequest(copy)
		if err != nil {
			s.artworkManagementError(w, r, artwork.ErrInvalidOptions)
			return
		}
		actor := r.Context().Value(principalKey).(identity.Principal)
		s.serveArtworkImage(w, copy, func(ctx context.Context) (io.ReadCloser, library.Image, error) {
			return s.library.OpenArtwork(ctx, actor, identity.AdministratorNative, target, request.typeName, request.index)
		})
	}
}

func (s *Server) embyArtworkTarget(ctx context.Context, actor identity.Principal, id string) (library.ArtworkTarget, error) {
	target := library.ArtworkTarget{ItemID: id}
	if _, err := s.library.GetArtwork(ctx, actor, identity.AdministratorEmby, target); err != nil {
		if !errors.Is(err, library.ErrNotFound) {
			return target, err
		}
		entity, parseErr := strconv.ParseInt(id, 10, 64)
		if parseErr != nil || entity <= 0 {
			return target, err
		}
		target = library.ArtworkTarget{EntityID: entity}
		if _, err := s.library.GetArtwork(ctx, actor, identity.AdministratorEmby, target); err != nil {
			return target, err
		}
	}
	return target, nil
}

func (s *Server) embyArtworkMutation(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := r.Context().Value(principalKey).(identity.Principal)
		target, err := s.embyArtworkTarget(r.Context(), actor, r.PathValue("Id"))
		if err != nil {
			s.artworkManagementError(w, r, err)
			return
		}
		release, admitted := admitArtworkMutation()
		if !admitted {
			s.artworkManagementError(w, r, artwork.ErrManagedLimit)
			return
		}
		defer release()
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			s.artworkManagementError(w, r, library.ErrInvalidInput)
			return
		}
		values, err := imageQuery(query)
		if err != nil {
			s.artworkManagementError(w, r, library.ErrInvalidInput)
			return
		}
		for name := range values {
			switch name {
			case "index", "newindex", "api_key", "x-emby-token", "x-emby-client", "x-emby-client-version", "x-emby-device-id", "x-emby-device-name":
			default:
				s.artworkManagementError(w, r, library.ErrInvalidInput)
				return
			}
			if name == "newindex" && operation != "move" {
				s.artworkManagementError(w, r, library.ErrInvalidInput)
				return
			}
		}
		request, err := parseImageRequest(r)
		if err != nil {
			s.artworkManagementError(w, r, artwork.ErrInvalidOptions)
			return
		}
		revision, err := artworkRevisionHeader(r, false)
		if err != nil {
			s.artworkManagementError(w, r, err)
			return
		}
		switch operation {
		case "upload":
			data, readErr := readArtworkBody(w, r)
			if readErr != nil {
				s.artworkManagementError(w, r, readErr)
				return
			}
			_, err = s.library.UploadArtwork(r.Context(), actor, identity.AdministratorEmby, target, revision, request.typeName, request.index, data)
		case "delete":
			if !emptyArtworkBody(r) {
				s.artworkManagementError(w, r, library.ErrInvalidInput)
				return
			}
			_, err = s.library.DeleteArtwork(r.Context(), actor, identity.AdministratorEmby, target, revision, request.typeName, request.index)
		case "move":
			if !emptyArtworkBody(r) {
				s.artworkManagementError(w, r, library.ErrInvalidInput)
				return
			}
			var collection library.ArtworkCollection
			collection, err = s.library.GetArtwork(r.Context(), actor, identity.AdministratorEmby, target)
			if err != nil {
				break
			}
			if revision != "" && collection.Revision != revision {
				err = library.ErrRevisionConflict
				break
			}
			newIndex, parseErr := strconv.Atoi(values["newindex"])
			var indexes []int
			selected := -1
			for _, image := range collection.Items {
				if image.ImageType != request.typeName {
					continue
				}
				if image.ImageIndex == request.index {
					selected = len(indexes)
				}
				indexes = append(indexes, image.ImageIndex)
			}
			if parseErr != nil || selected < 0 || newIndex < 0 || newIndex >= len(indexes) {
				err = library.ErrInvalidInput
				break
			}
			moved := indexes[selected]
			indexes = append(indexes[:selected], indexes[selected+1:]...)
			indexes = append(indexes, 0)
			copy(indexes[newIndex+1:], indexes[newIndex:])
			indexes[newIndex] = moved
			_, err = s.library.ReorderArtwork(r.Context(), actor, identity.AdministratorEmby, target, collection.Revision, request.typeName, indexes)
		}
		if err != nil {
			s.artworkManagementError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func emptyArtworkBody(r *http.Request) bool {
	if r.Body == nil || r.Body == http.NoBody {
		return true
	}
	if r.ContentLength > 0 {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 1))
	return err == nil && len(data) == 0
}

func (s *Server) entityArtworkByName(kind, family string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.itemUser(w, r)
		if !ok {
			return
		}
		request, err := parseImageRequest(r)
		if err != nil {
			s.artworkManagementError(w, r, artwork.ErrInvalidOptions)
			return
		}
		s.serveArtworkImage(w, r, func(ctx context.Context) (io.ReadCloser, library.Image, error) {
			subject := requestLibrarySubject(r, userID)
			var entity library.Entity
			var err error
			if family != "" {
				entity, err = s.library.GetMusicEntityFor(ctx, subject, family, r.PathValue("Name"))
			} else {
				entity, err = s.library.GetEntityFor(ctx, subject, kind, r.PathValue("Name"))
			}
			if err != nil {
				return nil, library.Image{}, err
			}
			return s.library.OpenEntityImageFor(ctx, subject, entity.ID, request.typeName, request.index)
		})
	}
}

func (s *Server) adminArtworkEntities(w http.ResponseWriter, r *http.Request) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		s.artworkManagementError(w, r, library.ErrInvalidInput)
		return
	}
	for name, entries := range values {
		if len(entries) != 1 || name != "Kind" && name != "SearchTerm" && name != "StartIndex" && name != "Limit" {
			s.artworkManagementError(w, r, library.ErrInvalidInput)
			return
		}
	}
	query := library.Query{UserID: r.Context().Value(principalKey).(identity.Principal).User.ID, SearchTerm: values.Get("SearchTerm"), SortBy: "SortName", Limit: 25}
	for name, target := range map[string]*int{"StartIndex": &query.StartIndex, "Limit": &query.Limit} {
		if raw := values.Get(name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 0 || name == "Limit" && (value == 0 || value > 100) {
				s.artworkManagementError(w, r, library.ErrInvalidInput)
				return
			}
			*target = value
		}
	}
	kind := values.Get("Kind")
	if kind == "" {
		kind = "Person"
	}
	var result library.EntityResult
	actor := r.Context().Value(principalKey).(identity.Principal)
	if strings.EqualFold(kind, "MusicArtist") {
		result, err = s.library.QueryMusicMetadataEntities(r.Context(), actor, "allartists", query)
	} else {
		result, err = s.library.QueryArtworkEntities(r.Context(), actor, kind, query)
	}
	if err != nil {
		s.artworkManagementError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, entity := range result.Items {
		items = append(items, s.entityDTO(entity, nil, false))
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount, "StartIndex": query.StartIndex, "Limit": query.Limit})
}

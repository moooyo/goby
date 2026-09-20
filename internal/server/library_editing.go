package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerLibraryEditingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/libraries/{id}", s.requireAdmin(s.getLibraryEditing))
	mux.HandleFunc("PATCH /admin/v1/libraries/{id}", s.requireAdmin(s.updateLibraryEditing))
	mux.HandleFunc("GET /admin/v1/storage/directories", s.requireAdmin(s.browseServerDirectories))
	mux.HandleFunc("POST /admin/v1/storage/directories/validate", s.requireAdmin(s.validateServerDirectory))
	s.registerEmbyLibraryEditingRoutes(mux)
}

func libraryEditingDTO(value library.LibraryEditing) map[string]any {
	result := libraryDTO(value.Library)
	result["RegisteredPaths"] = value.RegisteredPaths
	return result
}

func nativeLibraryCreationOptions(update *library.LibraryOptionsUpdate) library.LibraryOptions {
	options := library.DefaultLibraryOptions()
	if update != nil {
		if update.EnableLocalMetadata != nil {
			options.EnableLocalMetadata = *update.EnableLocalMetadata
		}
		if update.EnableLocalImages != nil {
			options.EnableLocalImages = *update.EnableLocalImages
		}
		if update.EnableEmbeddedArtwork != nil {
			options.EnableEmbeddedArtwork = *update.EnableEmbeddedArtwork
		}
	}
	return options
}

// The editing contract is closed. Unsupported options must not be acknowledged
// as saved settings without an actual consumer.
func decodeLibraryEditingBody(w http.ResponseWriter, r *http.Request, target any) bool {
	var raw json.RawMessage
	if !decodeBody(w, r, &raw) {
		return false
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "A library editing object is required.")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "The request contains an unsupported library field or invalid value.")
		return false
	}
	return true
}

func (s *Server) getLibraryEditing(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value(principalKey).(identity.Principal)
	value, err := s.library.GetLibraryEditingAsAdministrator(r.Context(), actor, identity.AdministratorNative, r.PathValue("id"))
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Library": libraryEditingDTO(value)})
}

func (s *Server) updateLibraryEditing(w http.ResponseWriter, r *http.Request) {
	var body struct {
		library.LibraryUpdate
		Scan bool
	}
	if !decodeLibraryEditingBody(w, r, &body) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	value, err := s.library.UpdateLibraryAsAdministrator(r.Context(), actor, identity.AdministratorNative, r.PathValue("id"), body.LibraryUpdate)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	response := map[string]any{"Library": libraryEditingDTO(value)}
	if body.Scan {
		job, err := s.library.StartScanAsAdministrator(r.Context(), actor, identity.AdministratorNative, value.Library.ID, library.ScanOptions{})
		if err != nil {
			_, code, message := libraryErrorInfo(err)
			response["ScanError"] = map[string]string{"Code": code, "Message": message}
		} else {
			response["Job"] = jobDTO(job)
		}
	}
	jsonResponse(w, http.StatusOK, response)
}

func serverDirectoryPageQuery(r *http.Request) (string, int, int, error) {
	query := r.URL.Query()
	for key, values := range query {
		if (key != "Path" && key != "StartIndex" && key != "Limit") || len(values) != 1 {
			return "", 0, 0, library.ErrInvalidInput
		}
	}
	start, limit := 0, 100
	var err error
	if value, exists := query["StartIndex"]; exists {
		start, err = strconv.Atoi(value[0])
		if err != nil {
			return "", 0, 0, library.ErrInvalidInput
		}
	}
	if value, exists := query["Limit"]; exists {
		limit, err = strconv.Atoi(value[0])
		if err != nil || limit < 1 {
			return "", 0, 0, library.ErrInvalidInput
		}
	}
	return query.Get("Path"), start, limit, nil
}

func (s *Server) serverDirectoryError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		apiError(w, r, http.StatusServiceUnavailable, "directory_unavailable", "The directory operation did not complete within its deadline.")
		return
	}
	s.libraryError(w, r, err)
}

func (s *Server) browseServerDirectories(w http.ResponseWriter, r *http.Request) {
	path, start, limit, err := serverDirectoryPageQuery(r)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	page, err := s.library.BrowseServerDirectories(r.Context(), actor, identity.AdministratorNative, path, start, limit)
	if err != nil {
		s.serverDirectoryError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, page)
}

func (s *Server) validateServerDirectory(w http.ResponseWriter, r *http.Request) {
	var body struct{ Path string }
	if !decodeLibraryEditingBody(w, r, &body) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	path, err := s.library.ValidateServerDirectory(r.Context(), actor, identity.AdministratorNative, body.Path)
	if err != nil {
		s.serverDirectoryError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Path": path, "Available": true})
}

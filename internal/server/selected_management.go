package server

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerSelectedManagementRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/policy/deletion-folders", s.requireAdmin(s.adminDeletionFolders))
	mux.HandleFunc("GET /emby/Libraries/AvailableOptions", s.requireEmby(s.availableLibraryOptions))
}

func selectedManagementEmptyBody(w http.ResponseWriter, r *http.Request) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
	if err != nil || len(raw) != 0 {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "This read operation does not accept a request body.")
		return false
	}
	return true
}

func parseDeletionFolderQuery(r *http.Request) (identity.DeletionFolderQuery, error) {
	query := identity.DeletionFolderQuery{Limit: 100}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return query, identity.ErrInvalidInput
	}
	for name, entries := range values {
		if len(entries) != 1 || !utf8.ValidString(entries[0]) {
			return query, identity.ErrInvalidInput
		}
		switch name {
		case "SearchTerm":
			query.SearchTerm = entries[0]
		case "LibraryId":
			query.LibraryID = entries[0]
		case "StartIndex", "Limit":
			value, err := strconv.ParseInt(entries[0], 10, 32)
			if err != nil || value < 0 || strconv.FormatInt(value, 10) != entries[0] || name == "Limit" && (value < 1 || value > 200) {
				return query, identity.ErrInvalidInput
			}
			if name == "StartIndex" {
				query.StartIndex = int(value)
			} else {
				query.Limit = int(value)
			}
		default:
			return query, identity.ErrInvalidInput
		}
	}
	return query, nil
}

func (s *Server) adminDeletionFolders(w http.ResponseWriter, r *http.Request) {
	if !selectedManagementEmptyBody(w, r) {
		return
	}
	query, err := parseDeletionFolderQuery(r)
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	page, err := s.identity.ListDeletionFolders(r.Context(), r.Context().Value(principalKey).(identity.Principal), query)
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, page)
}

func supportedLibraryOptionsDTO() map[string]any {
	defaults := library.DefaultLibraryOptions()
	return map[string]any{
		"MetadataSavers":   []any{},
		"MetadataReaders":  []map[string]any{{"Name": "Nfo", "DefaultEnabled": defaults.EnableLocalMetadata, "Features": []string{}}},
		"SubtitleFetchers": []any{}, "LyricsFetchers": []any{},
		"TypeOptions":           []map[string]any{{"Type": "Audio", "MetadataFetchers": []any{}, "ImageFetchers": []map[string]any{{"Name": embeddedArtworkFetcherName, "DefaultEnabled": true, "Features": []string{}}}, "SupportedImageTypes": []string{"Primary"}, "DefaultImageOptions": []any{}}},
		"DefaultLibraryOptions": embyEditableLibraryOptions(library.Library{Options: &defaults}),
	}
}

// Capability discovery describes only the implemented local reader and its
// actual defaults. It is safe for an authenticated user and carries no paths,
// provider credentials, plugin URLs, or unsupported subsystem options.
func (s *Server) availableLibraryOptions(w http.ResponseWriter, r *http.Request) {
	values, err := embyBusinessQuery(r)
	if err != nil || len(values) != 0 {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "This operation accepts only authentication parameters.")
		return
	}
	if !selectedManagementEmptyBody(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, supportedLibraryOptionsDTO())
}

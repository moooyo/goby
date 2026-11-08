package server

import (
	"net/http"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

type libraryStorageDirectoriesDTO struct {
	Items            []library.LibraryStorageDirectory
	TotalRecordCount int
}

func (s *Server) libraryStorageDirectories(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		apiError(w, r, http.StatusBadRequest, "invalid_query", "Library directory observations do not accept query parameters.")
		return
	}
	if s.library == nil {
		apiError(w, r, http.StatusServiceUnavailable, "library_unavailable", "Library directory observations are unavailable.")
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	items, err := s.library.LibraryStorageDirectories(r.Context(), actor, identity.AdministratorNative)
	if err != nil {
		s.serverDirectoryError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, libraryStorageDirectoriesDTO{Items: items, TotalRecordCount: len(items)})
}

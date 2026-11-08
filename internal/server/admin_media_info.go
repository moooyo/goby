package server

import (
	"net/http"

	"github.com/moooyo/goby/internal/identity"
)

func (s *Server) adminItemMediaInfo(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok || !adminMetadataNoQuery(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	info, err := s.library.GetAdminMediaInfo(r.Context(), actor, id)
	if err != nil {
		s.adminMetadataError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, info)
}

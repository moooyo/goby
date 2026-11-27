package server

import (
	"net/http"

	"github.com/moooyo/goby/internal/identity"
)

const localBackdropLockHeader = "X-Goby-Backdrop-Lock"

func localBackdropLockHeaderValue(w http.ResponseWriter, r *http.Request, required bool) (string, bool) {
	values := r.Header.Values(localBackdropLockHeader)
	if len(values) == 0 && !required {
		return "", true
	}
	if len(values) != 1 || values[0] == "" {
		adminMetadataInputError(w, r, map[string]string{localBackdropLockHeader: "Supply exactly one backdrop lock ID."})
		return "", false
	}
	return values[0], true
}

func (s *Server) adminLocalBackdropLock(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, ok := adminMetadataID(w, r)
	if !ok || !adminEmptyMutation(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	switch r.Method {
	case http.MethodPost:
		result, err := s.library.AcquireLocalBackdropLock(r.Context(), actor, id)
		if err != nil {
			s.localBackdropError(w, r, err)
			return
		}
		jsonResponse(w, http.StatusCreated, result)
	case http.MethodDelete:
		lockID, ok := localBackdropLockHeaderValue(w, r, true)
		if !ok {
			return
		}
		if err := s.library.ReleaseLocalBackdropLock(r.Context(), actor, id, lockID); err != nil {
			s.localBackdropError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default: // GET and its implicit HEAD
		result, err := s.library.GetLocalBackdropLock(r.Context(), actor, id)
		if err != nil {
			s.localBackdropError(w, r, err)
			return
		}
		jsonResponse(w, http.StatusOK, result)
	}
}

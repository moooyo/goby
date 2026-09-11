package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/moooyo/goby/internal/identity"
)

func (s *Server) registerUserSettingsRoutes(mux *http.ServeMux) {
	wrap := func(handler http.HandlerFunc) http.HandlerFunc {
		authenticated := s.requireEmby(handler)
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			authenticated(w, r)
		}
	}
	mux.HandleFunc("GET /emby/UserSettings/{Id}", wrap(s.embyUserSettings))
	mux.HandleFunc("POST /emby/UserSettings/{Id}", wrap(s.fullEmbyUserSettings))
	mux.HandleFunc("POST /emby/UserSettings/{Id}/Partial", wrap(s.patchEmbyUserSettings))
}

func (s *Server) userSettingsAccess(w http.ResponseWriter, r *http.Request) (map[string]string, bool) {
	return s.userSettingsAccessFor(w, r, r.PathValue("Id"))
}

func (s *Server) userSettingsAccessFor(w http.ResponseWriter, r *http.Request, userID string) (map[string]string, bool) {
	actor := r.Context().Value(principalKey).(identity.Principal)
	values, err := s.identity.GetUserSettings(r.Context(), actor, userID)
	if err != nil {
		s.userSettingsError(w, r, err)
		return nil, false
	}
	return values, true
}

func (s *Server) embyUserSettings(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value(principalKey).(identity.Principal)
	userID := r.PathValue("Id")
	if !actor.CanManageServer() {
		// Ordinary-user reference requests return the authenticated user's
		// settings even when the URL contains another existing user's ID.
		userID = actor.User.ID
	}
	values, ok := s.userSettingsAccessFor(w, r, userID)
	if !ok || !userSettingsQuery(w, r) {
		return
	}
	data, err := json.Marshal(values)
	if err != nil {
		s.userSettingsError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (s *Server) fullEmbyUserSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.userSettingsAccess(w, r); !ok || !userSettingsQuery(w, r) {
		return
	}
	// The pinned reference rejects full-object writes with this exact response.
	// Partial is the observed persistence contract; no replacement is invented.
	embyTextError(w, r, http.StatusBadRequest, "Expected configuration type is UserSettings")
}

func (s *Server) patchEmbyUserSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.userSettingsAccess(w, r); !ok || !userSettingsQuery(w, r) {
		return
	}
	patch, ok := userSettingsBody(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	if err := s.identity.PatchUserSettings(r.Context(), actor, r.PathValue("Id"), patch); err != nil {
		s.userSettingsError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) userSettingsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials), errors.Is(err, identity.ErrNotFound):
		s.identityError(w, r, err)
	case errors.Is(err, identity.ErrClientSessionForbidden):
		embyTextError(w, r, http.StatusForbidden, "The requested user settings are not accessible.")
	case errors.Is(err, identity.ErrUserSettingsLimit):
		embyTextError(w, r, http.StatusRequestEntityTooLarge, "User settings exceed the supported limits.")
	case errors.Is(err, identity.ErrInvalidInput):
		userSettingsInputError(w, r)
	case errors.Is(err, identity.ErrStoredUserSettings), errors.Is(err, context.DeadlineExceeded):
		embyTextError(w, r, http.StatusServiceUnavailable, "User settings are currently unavailable.")
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		return
	default:
		s.log.Error("user settings operation failed", "request_id", r.Context().Value(requestIDKey))
		embyTextError(w, r, http.StatusInternalServerError, "The user settings operation could not be completed.")
	}
}

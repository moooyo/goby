package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
)

func (s *Server) registerLocalCredentialRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/users/{id}/local-credentials", s.requireAdmin(s.getLocalCredentials))
	mux.HandleFunc("PUT /admin/v1/users/{id}/local-credentials", s.requireAdmin(s.updateLocalCredentials))
}

func (s *Server) localCredentialError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrApplicationKeyVaultUnavailable), errors.Is(err, identity.ErrApplicationKeyVaultMissing),
		errors.Is(err, identity.ErrApplicationKeyVaultUnsafe), errors.Is(err, identity.ErrApplicationKeyVaultCiphertext),
		errors.Is(err, identity.ErrApplicationKeyVaultUnsupported):
		apiError(w, r, http.StatusServiceUnavailable, "credential_storage_unavailable", "Protected credential storage is unavailable.")
	default:
		s.preferenceError(w, r, err)
	}
}

func (s *Server) getLocalCredentials(w http.ResponseWriter, r *http.Request) {
	if !preferenceNoQuery(w, r) {
		return
	}
	id, ok := managedUserID(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.identity.GetLocalCredentials(r.Context(), actor, id)
	if err != nil {
		s.localCredentialError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if !s.retireNotificationSessionsForRequest(w, r, result.RevokedSessionIDs...) {
		return
	}
	jsonResponse(w, http.StatusOK, result)
}

func decodeLocalCredentials(w http.ResponseWriter, r *http.Request) (identity.LocalCredentialsUpdate, bool) {
	if !preferenceNoQuery(w, r) {
		return identity.LocalCredentialsUpdate{}, false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || len(r.Header.Values("Content-Type")) != 1 || mediaType != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this request.")
		return identity.LocalCredentialsUpdate{}, false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil || !utf8.Valid(data) || !adminSettingsUnicode(data) {
		managedUserInputError(w, r, map[string]string{"Body": "Supply a lossless UTF-8 JSON object no larger than 4 KiB."})
		return identity.LocalCredentialsUpdate{}, false
	}
	values, fields := userManagementObject(data, []string{"Revision", "EnableLocalPassword", "LocalPassword", "ProfilePin"}, false, "")
	if len(fields) != 0 {
		managedUserInputError(w, r, fields)
		return identity.LocalCredentialsUpdate{}, false
	}
	invalid := make(map[string]string)
	input := identity.LocalCredentialsUpdate{Revision: managedUserRevision(values["Revision"], invalid)}
	managedUserValue(values["EnableLocalPassword"], "EnableLocalPassword", &input.EnableLocalPassword, invalid)
	for name, target := range map[string]**string{"LocalPassword": &input.LocalPassword, "ProfilePin": &input.ProfilePin} {
		if raw, found := values[name]; found {
			var value string
			managedUserValue(raw, name, &value, invalid)
			*target = &value
		}
	}
	if len(invalid) != 0 {
		managedUserInputError(w, r, invalid)
		return identity.LocalCredentialsUpdate{}, false
	}
	return input, true
}

func (s *Server) updateLocalCredentials(w http.ResponseWriter, r *http.Request) {
	id, ok := managedUserID(w, r)
	if !ok {
		return
	}
	input, ok := decodeLocalCredentials(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.identity.UpdateLocalCredentials(r.Context(), actor, id, input)
	if err != nil {
		s.localCredentialError(w, r, err)
		return
	}
	s.retireManagedUserSessions(result.RevokedSessionIDs)
	if len(result.RevokedSessionIDs) != 0 {
		s.mediaDiagnostics.cancelActor(id, "")
	}
	if result.CurrentSessionRevoked {
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/admin", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, result)
}

// attachOwnProfilePin is deliberately separate from userDTO. Public and
// administrator list/detail projections cannot accidentally serialize a PIN.
func (s *Server) attachOwnProfilePin(r *http.Request, actor identity.Principal, userID string, configuration any) (map[string]any, error) {
	pin := ""
	if actor.Kind == "emby" && !actor.IsApplicationKey() && actor.User.ID == userID {
		raw, value, err := s.identity.GetOwnProfileConfiguration(r.Context(), actor, userID)
		if err != nil {
			return nil, err
		}
		configuration = projectUserConfiguration(raw)
		pin = value
	}
	encoded, err := json.Marshal(configuration)
	if err != nil {
		return nil, err
	}
	var dto map[string]any
	if err := json.Unmarshal(encoded, &dto); err != nil {
		return nil, err
	}
	if pin != "" {
		dto["ProfilePin"] = pin
	}
	return dto, nil
}

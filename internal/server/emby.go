package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/moooyo/goby/internal/identity"
)

func (s *Server) userDTO(user identity.User) map[string]any {
	return map[string]any{
		"Id": user.ID, "Name": user.Name, "ServerId": s.serverID,
		"HasPassword": user.HasPassword, "HasConfiguredPassword": user.HasPassword,
		"Policy":        map[string]any{"IsAdministrator": user.IsAdministrator, "IsDisabled": user.IsDisabled, "EnableMediaPlayback": false, "EnableAudioPlaybackTranscoding": false, "EnableVideoPlaybackTranscoding": false, "EnableContentDeletion": false},
		"Configuration": map[string]any{},
	}
}

func (s *Server) publicSystemInfo(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.identity.Initialized(r.Context())
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"Id": s.serverID, "ServerName": s.cfg.ServerName, "Version": s.version, "ProductName": "Goby", "LocalAddress": s.cfg.PublicURL, "StartupWizardCompleted": initialized})
}

func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) { s.publicSystemInfo(w, r) }

func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	// This wire identifier is confirmed by the Emby 4.9.5.0 reference captures.
	const body = "Emby Server"
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(body))
	}
}

func (s *Server) publicUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.identity.ListUsers(r.Context())
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(users))
	for _, user := range users {
		if !user.IsDisabled && !user.IsAdministrator {
			items = append(items, map[string]any{"Id": user.ID, "Name": user.Name, "ServerId": s.serverID, "HasPassword": user.HasPassword, "HasConfiguredPassword": user.HasPassword})
		}
	}
	jsonResponse(w, 200, items)
}

func (s *Server) embyLogin(w http.ResponseWriter, r *http.Request) {
	if !s.allowLogin(w, r) {
		return
	}
	var body struct{ Username, Pw string }
	if !decodeBody(w, r, &body) {
		return
	}
	s.authenticateEmby(w, r, body.Username, body.Pw)
}

func (s *Server) embyLoginByID(w http.ResponseWriter, r *http.Request) {
	if !s.allowLogin(w, r) {
		return
	}
	var body struct{ Pw string }
	if !decodeBody(w, r, &body) {
		return
	}
	user, err := s.identity.GetUser(r.Context(), r.PathValue("Id"))
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			embyTextError(w, r, http.StatusUnauthorized, embyInvalidLoginMessage)
		} else {
			s.identityError(w, r, err)
		}
		return
	}
	s.authenticateEmby(w, r, user.Name, body.Pw)
}

func (s *Server) authenticateEmby(w http.ResponseWriter, r *http.Request, name, password string) {
	_, client, err := parseEmbyCredentials(r)
	if err != nil {
		apiError(w, r, 400, "invalid_client", "Supply Emby Client and DeviceId authorization metadata.")
		return
	}
	if client.Name == "" {
		embyTextError(w, r, http.StatusBadRequest, embyMissingClientMessage)
		return
	}
	if client.DeviceID == "" {
		embyTextError(w, r, http.StatusBadRequest, embyMissingDeviceMessage)
		return
	}
	credentials, err := s.identity.Authenticate(r.Context(), name, password, client, "emby")
	if err != nil {
		if errors.Is(err, identity.ErrInvalidCredentials) || errors.Is(err, identity.ErrUnauthorized) {
			embyTextError(w, r, http.StatusUnauthorized, embyInvalidLoginMessage)
		} else {
			s.identityError(w, r, err)
		}
		return
	}
	jsonResponse(w, 200, map[string]any{
		"User": s.userDTO(credentials.User), "AccessToken": credentials.Token, "ServerId": s.serverID,
		"SessionInfo": map[string]any{"Id": credentials.SessionID, "UserId": credentials.User.ID, "UserName": credentials.User.Name, "DeviceId": client.DeviceID, "DeviceName": client.Device, "Client": client.Name, "ApplicationVersion": client.Version, "ServerId": s.serverID, "SupportsRemoteControl": false},
	})
}

func (s *Server) embyUser(w http.ResponseWriter, r *http.Request) {
	principal := r.Context().Value(principalKey).(identity.Principal)
	id := r.PathValue("Id")
	if id != principal.User.ID && !principal.User.IsAdministrator {
		apiError(w, r, 403, "access_denied", "The requested user is not accessible.")
		return
	}
	user, err := s.identity.GetUser(r.Context(), id)
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	jsonResponse(w, 200, s.userDTO(user))
}

func (s *Server) embyUsers(w http.ResponseWriter, r *http.Request) {
	principal := r.Context().Value(principalKey).(identity.Principal)
	if !principal.User.IsAdministrator {
		apiError(w, r, 403, "administrator_required", "Administrator access is required.")
		return
	}
	start, limit := 0, 100
	for name, target := range map[string]*int{"StartIndex": &start, "Limit": &limit} {
		if value := r.URL.Query().Get(name); value != "" {
			number, err := strconv.Atoi(value)
			if err != nil || number < 0 {
				apiError(w, r, 400, "invalid_input", "Pagination must use non-negative integers.")
				return
			}
			*target = number
		}
	}
	if limit > 1000 {
		limit = 1000
	}
	users, err := s.identity.ListUsers(r.Context())
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	total := len(users)
	if start > total {
		start = total
	}
	end := start + min(limit, total-start)
	items := make([]map[string]any, 0, end-start)
	for _, user := range users[start:end] {
		items = append(items, s.userDTO(user))
	}
	jsonResponse(w, 200, map[string]any{"Items": items, "TotalRecordCount": total})
}

func (s *Server) embyLogout(w http.ResponseWriter, r *http.Request) {
	token, _, _ := parseEmbyCredentials(r)
	if err := s.identity.Revoke(r.Context(), token); err != nil {
		s.identityError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

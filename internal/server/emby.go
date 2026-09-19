package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
)

// embyAPIVersion identifies the pinned wire-contract baseline. Consumer clients
// use SystemInfo.Version for protocol feature gates, independently of Goby's
// product release. It does not assert that every operation has been implemented.
const embyAPIVersion = "4.9.5.0"

func (s *Server) userDTO(user identity.User) map[string]any {
	policy := embyUserPolicy(user)
	if s.hls != nil && policy["EnableMediaPlayback"] == true {
		// User DTOs project permissions, independently of per-plan output limits.
		limits := hlsUserLimits(config.TranscodingConfig{Enabled: s.cfg.Transcoding.Enabled}, user)
		policy["EnablePlaybackRemuxing"] = limits.AllowRemux
		policy["EnableAudioPlaybackTranscoding"] = limits.AllowAudioTranscode
		policy["EnableVideoPlaybackTranscoding"] = limits.AllowVideoTranscode
	}
	return map[string]any{
		"Id": user.ID, "Name": user.Name, "ServerId": s.serverID,
		"HasPassword": user.HasPassword, "HasConfiguredPassword": user.HasPassword,
		"Policy":        policy,
		"Configuration": projectUserConfiguration(user.Configuration),
	}
}

// This is a supported-policy projection, never an authorization source. Media
// opening and playback events independently recheck current database policy.
func embyUserPolicy(user identity.User) map[string]any {
	parsed, err := identity.ParseRuntimePolicy(user.Policy)
	valid := err == nil && !user.IsDisabled
	if err != nil {
		parsed = identity.ProjectManagedPolicy(user.Policy)
	}
	policy := nativeManagedPolicy(parsed)
	policy["IsAdministrator"] = user.IsAdministrator
	policy["IsDisabled"] = user.IsDisabled
	policy["EnableMediaPlayback"] = valid && parsed.EnableMediaPlayback
	policy["EnableAllFolders"] = valid && (user.IsAdministrator || parsed.EnableAllFolders)
	policy["EnabledFolders"] = []string{}
	if valid && !user.IsAdministrator && !parsed.EnableAllFolders {
		policy["EnabledFolders"] = append([]string{}, parsed.EnabledFolders...)
	}
	policy["EnableAudioPlaybackTranscoding"] = false
	policy["EnableVideoPlaybackTranscoding"] = false
	policy["EnablePlaybackRemuxing"] = false
	for name, value := range deferredEmbyPolicy {
		policy[name] = value
	}
	for name, value := range readOnlyEmbyPolicy(user) {
		policy[name] = value
	}
	return policy
}

func (s *Server) publicSystemInfo(w http.ResponseWriter, r *http.Request) {
	snapshot := s.requestSettings(r)
	initialized, err := s.identity.Initialized(r.Context())
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	jsonResponse(w, 200, map[string]any{
		"Id": s.serverID, "ServerName": snapshot.Effective.ServerName,
		"Version": embyAPIVersion, "ProductName": "Goby", "GobyVersion": s.version,
		"LocalAddress": s.cfg.PublicURL, "StartupWizardCompleted": initialized,
	})
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
	w.Header().Set("Cache-Control", "no-store")
	_, client, err := parseEmbyCredentials(r)
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_client", "Supply unambiguous client metadata.")
		return
	}
	users, err := s.identity.ListUsers(r.Context())
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(users))
	remote := !s.endpointInfo(r).IsInNetwork
	for _, user := range users {
		visible, err := s.publicUserVisible(r, user, remote, client.DeviceID)
		if err != nil {
			s.identityError(w, r, err)
			return
		}
		if visible {
			items = append(items, map[string]any{"Id": user.ID, "Name": user.Name, "ServerId": s.serverID, "HasPassword": user.HasPassword, "HasConfiguredPassword": user.HasPassword})
		}
	}
	s.attachAvatarDTOs(r.Context(), items)
	jsonResponse(w, 200, items)
}

func (s *Server) embyLogin(w http.ResponseWriter, r *http.Request) {
	if !s.allowLogin(w, r) {
		return
	}
	body, ok := decodeEmbyLogin(w, r, true)
	if !ok {
		return
	}
	s.authenticateEmby(w, r, body.Username, body.Pw)
}

func (s *Server) embyLoginByID(w http.ResponseWriter, r *http.Request) {
	if !s.allowLogin(w, r) {
		return
	}
	body, ok := decodeEmbyLogin(w, r, false)
	if !ok {
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
	credentials, err := s.identity.AuthenticateWithPeer(r.Context(), name, password, client, "emby", s.policyClientAddress(r))
	if err != nil {
		if errors.Is(err, identity.ErrInvalidCredentials) || errors.Is(err, identity.ErrUnauthorized) {
			embyTextError(w, r, http.StatusUnauthorized, embyInvalidLoginMessage)
		} else {
			s.identityError(w, r, err)
		}
		return
	}
	jsonResponse(w, 200, map[string]any{
		"User": s.avatarUserDTO(r.Context(), s.userDTO(credentials.User)), "AccessToken": credentials.Token, "ServerId": s.serverID,
		"SessionInfo": s.clientSessionDTO(identity.ClientSession{SessionID: credentials.SessionID,
			UserID: credentials.User.ID, UserName: credentials.User.Name, Client: credentials.Client,
			CreatedAt: credentials.CreatedAt, LastSeenAt: credentials.CreatedAt, ExpiresAt: credentials.ExpiresAt}),
	})
}

func (s *Server) embyUser(w http.ResponseWriter, r *http.Request) {
	principal := r.Context().Value(principalKey).(identity.Principal)
	id := r.PathValue("Id")
	if id != principal.User.ID && !principal.CanManageServer() {
		apiError(w, r, 403, "access_denied", "The requested user is not accessible.")
		return
	}
	user, err := s.identity.GetUser(r.Context(), id)
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	jsonResponse(w, 200, s.avatarUserDTO(r.Context(), s.userDTO(user)))
}

func (s *Server) embyUsers(w http.ResponseWriter, r *http.Request) {
	principal := r.Context().Value(principalKey).(identity.Principal)
	if !principal.CanManageServer() {
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
	s.attachAvatarDTOs(r.Context(), items)
	jsonResponse(w, 200, map[string]any{"Items": items, "TotalRecordCount": total})
}

func (s *Server) embyLogout(w http.ResponseWriter, r *http.Request) {
	token, _, _ := parseEmbyCredentials(r)
	principal := r.Context().Value(principalKey).(identity.Principal)
	if principal.IsApplicationKey() {
		result, err := s.identity.RevokeApplicationKeyToken(r.Context(), principal, token)
		if err != nil {
			s.applicationKeyError(w, r, err)
			return
		}
		s.retireApplicationKey(result)
		noKeyCache(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.identity.Revoke(r.Context(), token); err != nil {
		s.identityError(w, r, err)
		return
	}
	if s.eventHub != nil {
		principal := r.Context().Value(principalKey).(identity.Principal)
		s.eventHub.DisconnectSession(principal.SessionID)
	}
	s.cancelPlaybackResources(principal.SessionID, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) embyUsersBare(w http.ResponseWriter, r *http.Request) {
	principal := r.Context().Value(principalKey).(identity.Principal)
	if !principal.CanManageServer() {
		apiError(w, r, http.StatusForbidden, "administrator_required", "Administrator access is required.")
		return
	}
	users, err := s.identity.ListUsers(r.Context())
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(users))
	for _, user := range users {
		items = append(items, s.userDTO(user))
	}
	s.attachAvatarDTOs(r.Context(), items)
	jsonResponse(w, http.StatusOK, items)
}

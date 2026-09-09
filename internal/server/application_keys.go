package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
)

func (s *Server) registerApplicationKeyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/api-keys", s.requireAdmin(s.adminApplicationKeys))
	mux.HandleFunc("POST /admin/v1/api-keys", s.requireAdmin(s.createAdminApplicationKey))
	mux.HandleFunc("POST /admin/v1/api-keys/{id}/reveal", s.requireAdmin(s.revealAdminApplicationKey))
	mux.HandleFunc("POST /admin/v1/api-keys/{id}/revoke", s.requireAdmin(s.revokeAdminApplicationKey))
	mux.HandleFunc("GET /emby/Auth/Keys", s.requireEmby(s.embyApplicationKeys))
	mux.HandleFunc("POST /emby/Auth/Keys", s.requireEmby(s.createEmbyApplicationKey))
	mux.HandleFunc("DELETE /emby/Auth/Keys/{Key}", s.requireEmby(s.revokeEmbyApplicationKey))
	mux.HandleFunc("POST /emby/Auth/Keys/{Key}/Delete", s.requireEmby(s.revokeEmbyApplicationKey))
}

func nativeApplicationKey(key identity.ApplicationKey) map[string]any {
	var lastUsed, revoked any
	var creator any
	status := "active"
	if key.LastUsedAt != nil {
		lastUsed = key.LastUsedAt.UTC()
	}
	if key.RevokedAt != nil {
		revoked, status = key.RevokedAt.UTC(), "revoked"
	}
	if key.CreatedBy != "" {
		creator = key.CreatedBy
	}
	return map[string]any{"Id": strconv.FormatInt(key.ID, 10), "AppName": key.AppName,
		"CreatedAt": key.CreatedAt.UTC(), "LastUsedAt": lastUsed, "RevokedAt": revoked,
		"CreatedBy": creator, "IPAddress": key.IPAddress, "Status": status}
}

func noKeyCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func applicationKeyBody(w http.ResponseWriter, r *http.Request, fields []string) (map[string]json.RawMessage, bool) {
	if r.URL.RawQuery != "" {
		apiError(w, r, 400, "invalid_input", "This operation does not accept query parameters.")
		return nil, false
	}
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		apiError(w, r, 415, "unsupported_media_type", "Use application/json for this request.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	data, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(data) {
		apiError(w, r, 400, "invalid_input", "Supply a UTF-8 JSON object no larger than 4 KiB.")
		return nil, false
	}
	values, invalid := managedUserObject(data, fields, "")
	if invalid != nil {
		apiError(w, r, 400, "invalid_input", "Supply the required application key fields exactly once.")
		return nil, false
	}
	return values, true
}

func applicationKeyID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value := r.PathValue("id")
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		apiError(w, r, 400, "invalid_input", "Supply a positive decimal application key identifier.")
		return 0, false
	}
	return id, true
}

func parseApplicationKeyQuery(r *http.Request, native bool) (identity.ApplicationKeyFilter, error) {
	filter := identity.ApplicationKeyFilter{Limit: 50}
	if !native {
		filter.Limit, filter.RevealTokens = 200, true
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return filter, identity.ErrInvalidInput
	}
	seen := map[string]bool{}
	for name, entries := range values {
		field := name
		if !native {
			field = strings.ToLower(name)
		}
		if field == "api_key" && !native {
			continue
		}
		if len(entries) != 1 || seen[field] || !utf8.ValidString(entries[0]) {
			return filter, identity.ErrInvalidInput
		}
		seen[field] = true
		value := entries[0]
		switch {
		case field == "StartIndex" || (!native && field == "startindex"):
			n, err := strconv.ParseInt(value, 10, 32)
			if err != nil || n < 0 || (native && strconv.FormatInt(n, 10) != value) {
				return filter, identity.ErrInvalidInput
			}
			filter.StartIndex = int(n)
		case field == "Limit" || (!native && field == "limit"):
			n, err := strconv.ParseInt(value, 10, 32)
			if err != nil || n < 1 || n > 200 || (native && strconv.FormatInt(n, 10) != value) {
				return filter, identity.ErrInvalidInput
			}
			filter.Limit = int(n)
		case native && field == "SearchTerm":
			filter.SearchTerm = value
		case native && field == "IncludeRevoked":
			if value != "true" && value != "false" {
				return filter, identity.ErrInvalidInput
			}
			filter.IncludeRevoked = value == "true"
		default:
			return filter, identity.ErrInvalidInput
		}
	}
	return filter, nil
}

func (s *Server) adminApplicationKeys(w http.ResponseWriter, r *http.Request) {
	noKeyCache(w)
	filter, err := parseApplicationKeyQuery(r, true)
	if err != nil {
		s.applicationKeyError(w, r, err)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	page, err := s.identity.ListApplicationKeys(r.Context(), actor, filter)
	if err != nil {
		s.applicationKeyError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, key := range page.Items {
		items = append(items, nativeApplicationKey(key))
	}
	jsonResponse(w, 200, map[string]any{"Items": items, "TotalRecordCount": page.TotalRecordCount, "StartIndex": page.StartIndex, "Limit": page.Limit})
}

func (s *Server) createKey(r *http.Request, appName string) (identity.ApplicationKey, error) {
	actor := r.Context().Value(principalKey).(identity.Principal)
	key, err := s.identity.CreateApplicationKey(r.Context(), actor, appName, s.clientAddress(r),
		identity.Client{DeviceID: s.serverID, Device: s.cfg.ServerName, Version: s.version})
	if err == nil {
		s.log.Info("application key created", "actor_credential_id", actor.SessionID, "application_key_id", key.ID)
	}
	return key, err
}

func (s *Server) createAdminApplicationKey(w http.ResponseWriter, r *http.Request) {
	noKeyCache(w)
	values, ok := applicationKeyBody(w, r, []string{"AppName"})
	if !ok {
		return
	}
	var name string
	if string(values["AppName"]) == "null" || json.Unmarshal(values["AppName"], &name) != nil {
		s.applicationKeyError(w, r, identity.ErrInvalidInput)
		return
	}
	key, err := s.createKey(r, name)
	if err != nil {
		s.applicationKeyError(w, r, err)
		return
	}
	jsonResponse(w, 201, map[string]any{"Key": nativeApplicationKey(key), "AccessToken": key.Token})
}

func (s *Server) revealAdminApplicationKey(w http.ResponseWriter, r *http.Request) {
	noKeyCache(w)
	id, ok := applicationKeyID(w, r)
	if !ok {
		return
	}
	if _, ok := applicationKeyBody(w, r, nil); !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	key, err := s.identity.GetApplicationKey(r.Context(), actor, id, true)
	if err != nil {
		s.applicationKeyError(w, r, err)
		return
	}
	if key.RevokedAt != nil {
		apiError(w, r, 409, "key_revoked", "This application key has already been revoked.")
		return
	}
	s.log.Info("application key revealed", "actor_credential_id", actor.SessionID, "application_key_id", key.ID)
	jsonResponse(w, 200, map[string]any{"Id": strconv.FormatInt(key.ID, 10), "AccessToken": key.Token})
}

func (s *Server) retireApplicationKey(result identity.ApplicationKeyRevocation) {
	if result.CredentialID == "" {
		return
	}
	if s.eventHub != nil {
		s.eventHub.DisconnectCredential(result.CredentialID)
	}
	s.hls.cancelMatching(result.CredentialID, "")
	s.log.Info("application key revoked", "application_key_id", result.ID)
}

func (s *Server) revokeAdminApplicationKey(w http.ResponseWriter, r *http.Request) {
	noKeyCache(w)
	id, ok := applicationKeyID(w, r)
	if !ok {
		return
	}
	if _, ok := applicationKeyBody(w, r, nil); !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.identity.RevokeApplicationKey(r.Context(), actor, id)
	if err != nil {
		s.applicationKeyError(w, r, err)
		return
	}
	s.retireApplicationKey(result)
	jsonResponse(w, 200, map[string]any{"Id": strconv.FormatInt(result.ID, 10), "RevokedAt": result.RevokedAt.UTC()})
}

func (s *Server) keyManager(w http.ResponseWriter, r *http.Request) bool {
	actor := r.Context().Value(principalKey).(identity.Principal)
	if !actor.CanManageServer() {
		embyTextError(w, r, http.StatusForbidden, fmt.Sprintf("User %s does not have access to ManageServer feature.", actor.User.Name))
		return false
	}
	return true
}

func (s *Server) embyApplicationKeys(w http.ResponseWriter, r *http.Request) {
	noKeyCache(w)
	if !s.keyManager(w, r) {
		return
	}
	filter, err := parseApplicationKeyQuery(r, false)
	if err != nil {
		s.applicationKeyError(w, r, err)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	page, err := s.identity.ListApplicationKeys(r.Context(), actor, filter)
	if err != nil {
		s.applicationKeyError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, key := range page.Items {
		item := map[string]any{"Id": key.ID, "AccessToken": key.Token, "ReportedDeviceId": key.Client.DeviceID,
			"DeviceId": key.ReportedDeviceNumericID, "AppName": key.AppName, "AppVersion": key.Client.Version,
			"IpAddress": key.IPAddress, "DeviceName": key.Client.Device, "UserId": 0,
			"DateCreated": key.CreatedAt.UTC(), "IsActive": true}
		if key.LastUsedAt != nil {
			item["DateLastActivity"] = key.LastUsedAt.UTC()
		}
		items = append(items, item)
	}
	jsonResponse(w, 200, map[string]any{"Items": items, "TotalRecordCount": page.TotalRecordCount})
}

func (s *Server) createEmbyApplicationKey(w http.ResponseWriter, r *http.Request) {
	noKeyCache(w)
	if !s.keyManager(w, r) {
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	var name string
	found := false
	if err == nil {
		for field, entries := range values {
			if strings.EqualFold(field, "App") {
				if found || len(entries) != 1 {
					err = identity.ErrInvalidInput
					break
				}
				name, found = entries[0], true
			} else if field != "api_key" {
				err = identity.ErrInvalidInput
				break
			}
		}
	}
	if err != nil || !found {
		s.applicationKeyError(w, r, identity.ErrInvalidInput)
		return
	}
	if _, err := s.createKey(r, name); err != nil {
		s.applicationKeyError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) revokeEmbyApplicationKey(w http.ResponseWriter, r *http.Request) {
	noKeyCache(w)
	if !s.keyManager(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.identity.RevokeApplicationKeyToken(r.Context(), actor, r.PathValue("Key"))
	if err != nil {
		s.applicationKeyError(w, r, err)
		return
	}
	s.retireApplicationKey(result)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) applicationKeyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		apiError(w, r, 400, "invalid_input", "Check the application name, key identifier and pagination fields.")
	case errors.Is(err, identity.ErrNotFound):
		apiError(w, r, 404, "not_found", "The application key was not found.")
	case errors.Is(err, identity.ErrApplicationKeyVaultUnavailable), errors.Is(err, identity.ErrApplicationKeyVaultMissing),
		errors.Is(err, identity.ErrApplicationKeyVaultUnsafe), errors.Is(err, identity.ErrApplicationKeyVaultCiphertext),
		errors.Is(err, identity.ErrApplicationKeyVaultUnsupported):
		apiError(w, r, 503, "key_vault_unavailable", "The application key secret store is unavailable. Check its persistent key file and permissions.")
	default:
		s.identityError(w, r, err)
	}
}

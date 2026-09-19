package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerUserPreferenceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/users/{id}/preferences", s.requireAdmin(s.getNativeUserPreferences))
	mux.HandleFunc("PUT /admin/v1/users/{id}/preferences", s.requireAdmin(s.updateNativeUserPreferences))
	mux.HandleFunc("GET /emby/Users/{Id}/Configuration", s.requireEmby(s.getEmbyUserConfiguration))
	mux.HandleFunc("POST /emby/Users/{Id}/Configuration", s.requireEmby(s.updateEmbyUserConfiguration))
	mux.HandleFunc("POST /emby/Users/{Id}/Configuration/Partial", s.requireEmby(s.updateEmbyUserConfiguration))
	mux.HandleFunc("GET /emby/DisplayPreferences/{Id}", s.requireEmby(s.getEmbyDisplayPreferences))
	mux.HandleFunc("POST /emby/DisplayPreferences/{Id}", s.requireEmby(s.updateEmbyDisplayPreferences))
}

func preferenceBody(w http.ResponseWriter, r *http.Request, fields []string) (map[string]json.RawMessage, bool) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || len(r.Header.Values("Content-Type")) != 1 || contentType != "application/json" && contentType != "text/plain" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Supply a JSON preference object.")
		return nil, false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, identity.MaxUserConfigurationBytes))
	if err != nil || !utf8.Valid(data) || !adminSettingsUnicode(data) {
		managedUserInputError(w, r, map[string]string{"Body": "Supply a bounded, lossless UTF-8 preference object."})
		return nil, false
	}
	values, invalid := userManagementObject(data, fields, true, "")
	if len(invalid) != 0 {
		managedUserInputError(w, r, invalid)
		return nil, false
	}
	return values, true
}

func preferenceRevision(raw json.RawMessage, allowZero bool) (int64, bool) {
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value == "" {
		return 0, false
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	return revision, err == nil && strconv.FormatInt(revision, 10) == value && (revision > 0 || allowZero && revision == 0)
}

func preferenceNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/admin/") && (r.URL.RawQuery != "" || r.URL.ForceQuery) {
		managedUserInputError(w, r, map[string]string{"Query": "This preference operation does not accept query parameters."})
		return false
	}
	values, err := embyBusinessQuery(r)
	if err != nil || len(values) != 0 {
		managedUserInputError(w, r, map[string]string{"Query": "This preference operation does not accept query parameters."})
		return false
	}
	return true
}

func (s *Server) preferenceError(w http.ResponseWriter, r *http.Request, err error) {
	var fields *identity.ManagedUserValidationError
	switch {
	case errors.Is(err, identity.ErrClientSessionForbidden):
		apiError(w, r, http.StatusForbidden, "access_denied", "The requested user preferences are not accessible.")
	case errors.Is(err, identity.ErrUserSettingsLimit):
		apiError(w, r, http.StatusRequestEntityTooLarge, "preferences_limit", "User preferences exceed the supported limits.")
	case errors.Is(err, identity.ErrStoredUserSettings):
		apiError(w, r, http.StatusServiceUnavailable, "preferences_unavailable", "Stored preferences are currently unavailable.")
	case errors.As(err, &fields):
		managedUserInputError(w, r, fields.Fields)
	default:
		s.managedUserError(w, r, err)
	}
}

func (s *Server) getNativeUserPreferences(w http.ResponseWriter, r *http.Request) {
	if !preferenceNoQuery(w, r) {
		return
	}
	id, ok := managedUserID(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.identity.GetUserPreferences(r.Context(), actor, id)
	if err != nil {
		s.preferenceError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) updateNativeUserPreferences(w http.ResponseWriter, r *http.Request) {
	if !preferenceNoQuery(w, r) {
		return
	}
	id, ok := managedUserID(w, r)
	if !ok {
		return
	}
	values, ok := preferenceBody(w, r, []string{"Revision", "Configuration"})
	if !ok {
		return
	}
	revision, valid := preferenceRevision(values["Revision"], false)
	if !valid {
		managedUserInputError(w, r, map[string]string{"Revision": "Supply the current revision as a canonical positive decimal string."})
		return
	}
	patch, invalid := userManagementObject(values["Configuration"], identity.UserConfigurationFields, true, "Configuration")
	if len(invalid) != 0 {
		managedUserInputError(w, r, invalid)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.identity.UpdateUserPreferences(r.Context(), actor, id, &revision, identity.UserConfigurationPatch(patch))
	if err != nil {
		s.preferenceError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) getEmbyUserConfiguration(w http.ResponseWriter, r *http.Request) {
	if !preferenceNoQuery(w, r) {
		return
	}
	id, ok := embyManagedUserID(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.identity.GetUserPreferences(r.Context(), actor, id)
	if err != nil {
		s.preferenceError(w, r, err)
		return
	}
	configuration, err := s.attachOwnProfilePin(r, actor, id, result.Configuration)
	if err != nil {
		s.localCredentialError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, configuration)
}

func (s *Server) updateEmbyUserConfiguration(w http.ResponseWriter, r *http.Request) {
	if !preferenceNoQuery(w, r) {
		return
	}
	id, ok := embyManagedUserID(w, r)
	if !ok {
		return
	}
	values, ok := preferenceBody(w, r, identity.UserConfigurationFields)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	if _, err := s.identity.UpdateUserPreferences(r.Context(), actor, id, nil, identity.UserConfigurationPatch(values)); err != nil {
		s.localCredentialError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}

func displayPreferenceScope(w http.ResponseWriter, r *http.Request, body map[string]json.RawMessage) (string, string, bool) {
	rawQuery, err := embyBusinessQuery(r)
	if err != nil {
		managedUserInputError(w, r, map[string]string{"Query": "Supply unambiguous preference scope parameters."})
		return "", "", false
	}
	values := make(map[string]string)
	for name, entries := range rawQuery {
		key := strings.ToLower(name)
		if key != "userid" && key != "client" {
			managedUserInputError(w, r, map[string]string{"Query": "Only UserId and Client are supported."})
			return "", "", false
		}
		if _, duplicate := values[key]; duplicate || len(entries) != 1 {
			managedUserInputError(w, r, map[string]string{"Query": "Supply each preference scope parameter exactly once."})
			return "", "", false
		}
		values[key] = entries[0]
	}
	userID, client := values["userid"], values["client"]
	if raw, present := body["Client"]; present {
		var named string
		if json.Unmarshal(raw, &named) != nil || client != "" && named != client {
			managedUserInputError(w, r, map[string]string{"Client": "Client scope must agree in the query and body."})
			return "", "", false
		}
		client = named
	}
	if userID == "" || client == "" {
		managedUserInputError(w, r, map[string]string{"Query": "UserId and Client are required."})
		return "", "", false
	}
	return userID, client, true
}

func (s *Server) getEmbyDisplayPreferences(w http.ResponseWriter, r *http.Request) {
	userID, client, ok := displayPreferenceScope(w, r, nil)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.identity.GetDisplayPreferences(r.Context(), actor, userID, r.PathValue("Id"), client)
	if err != nil {
		s.preferenceError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) updateEmbyDisplayPreferences(w http.ResponseWriter, r *http.Request) {
	values, ok := preferenceBody(w, r, []string{"Id", "Client", "SortBy", "SortOrder", "CustomPrefs", "Revision"})
	if !ok {
		return
	}
	userID, client, ok := displayPreferenceScope(w, r, values)
	if !ok {
		return
	}
	var revision *int64
	if raw, present := values["Revision"]; present {
		value, valid := preferenceRevision(raw, true)
		if !valid {
			managedUserInputError(w, r, map[string]string{"Revision": "Supply a canonical decimal revision string; zero identifies an unsaved scope."})
			return
		}
		revision = &value
		delete(values, "Revision")
	}
	if raw, exists := values["SortBy"]; exists {
		var order string
		if json.Unmarshal(raw, &order) != nil || library.ValidatePreferenceSort(order, "Ascending") != nil {
			managedUserInputError(w, r, map[string]string{"SortBy": "Supply a supported item sort field or field list."})
			return
		}
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	if _, err := s.identity.UpdateDisplayPreferences(r.Context(), actor, userID, r.PathValue("Id"), client, revision, identity.DisplayPreferencesPatch(values)); err != nil {
		s.preferenceError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}

// applyDisplayPreferenceDefaults consumes only the standard sort fields. Other
// CustomPrefs remain bounded opaque client layout values, not server behavior.
// Explicit request fields always win; a missing stored scope changes no default.
func (s *Server) applyDisplayPreferenceDefaults(w http.ResponseWriter, r *http.Request, query *library.Query) bool {
	raw, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "The item preference query is ambiguous.")
		return false
	}
	values := make(map[string]string)
	for name, entries := range raw {
		key := strings.ToLower(name)
		if key != "displaypreferencesid" && key != "client" && key != "sortby" && key != "sortorder" {
			continue
		}
		if _, seen := values[key]; seen || len(entries) != 1 {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply each preference and sort parameter once.")
			return false
		}
		values[key] = entries[0]
	}
	if value, explicit := values["sortby"]; explicit {
		query.SortBy = value
	}
	if value, explicit := values["sortorder"]; explicit {
		query.SortOrder = value
	}
	id, client := values["displaypreferencesid"], values["client"]
	if id == "" {
		id = query.ParentID
	}
	if id == "" {
		id = r.URL.Query().Get("ParentId")
	}
	if client == "" || id == "" {
		return true
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	if actor.IsApplicationKey() || query.UserID == "" {
		return true
	}
	prefs, err := s.identity.GetDisplayPreferences(r.Context(), actor, query.UserID, id, client)
	if errors.Is(err, identity.ErrClientSessionForbidden) {
		// Preference access is independent from media browsing. An implicit
		// defaults lookup must not turn an otherwise authorized list into a
		// preference-disclosure request or deny ordinary catalog access.
		return true
	}
	if err != nil {
		s.preferenceError(w, r, err)
		return false
	}
	if prefs.Revision == 0 {
		return true
	}
	if _, explicit := values["sortby"]; !explicit {
		query.SortBy = prefs.SortBy
	}
	if _, explicit := values["sortorder"]; !explicit {
		query.SortOrder = prefs.SortOrder
	}
	if library.ValidatePreferenceSort(query.SortBy, query.SortOrder) != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "The saved and explicit sort fields are not supported.")
		return false
	}
	return true
}

func (s *Server) requestUserConfiguration(ctx context.Context, r *http.Request, userID string) (identity.UserConfiguration, error) {
	actor := r.Context().Value(principalKey).(identity.Principal)
	if actor.IsApplicationKey() {
		// This helper serves catalog consumers only. An application's explicit
		// target selects ACLs and user-data projection, not a personal preference
		// owner. A neutral projection also avoids injecting fresh-user defaults
		// such as HidePlayedInLatest when no preference document was stored.
		return identity.UserConfiguration{}, nil
	}
	if userID == "" {
		return identity.DefaultUserConfiguration(), nil
	}
	if actor.User.ID == userID {
		return identity.ProjectUserConfiguration(actor.User.Configuration), nil
	}
	user, err := s.identity.GetUser(ctx, userID)
	if err != nil {
		return identity.UserConfiguration{}, err
	}
	return identity.ProjectUserConfiguration(user.Configuration), nil
}

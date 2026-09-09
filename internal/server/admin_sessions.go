package server

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
)

func (s *Server) registerAdminSessionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/sessions", s.requireAdmin(s.adminSessions))
	mux.HandleFunc("POST /admin/v1/sessions/{id}/revoke", s.requireAdmin(s.revokeAdminSession))
}

func (s *Server) adminSessions(w http.ResponseWriter, r *http.Request) {
	filter, err := parseAdminSessionQuery(r)
	if err != nil {
		s.adminSessionError(w, r, err)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	page, err := s.identity.ListManagedSessions(r.Context(), actor, filter)
	if err != nil {
		s.adminSessionError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, session := range page.Items {
		items = append(items, nativeManagedSession(session))
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"Items": items, "TotalRecordCount": page.TotalRecordCount, "StartIndex": page.StartIndex, "Limit": page.Limit,
	})
}

func nativeManagedSession(session identity.ManagedSession) map[string]any {
	revokedAt := session.RevokedAt
	if revokedAt != nil {
		utc := revokedAt.UTC()
		revokedAt = &utc
	}
	return map[string]any{
		"Id": session.SessionID, "UserId": session.UserID, "UserName": session.UserName,
		"UserIsAdministrator": session.UserIsAdministrator, "UserIsDisabled": session.UserIsDisabled,
		"Kind": session.Kind, "Client": session.Client.Name, "DeviceId": session.Client.DeviceID,
		"DeviceName": session.Client.Device, "ApplicationVersion": session.Client.Version,
		"CreatedAt": session.CreatedAt.UTC(), "LastSeenAt": session.LastSeenAt.UTC(),
		"ExpiresAt": session.ExpiresAt.UTC(), "RevokedAt": revokedAt,
		"Status": session.Status, "IsCurrent": session.IsCurrent,
	}
}

func parseAdminSessionQuery(r *http.Request) (identity.ManagedSessionFilter, error) {
	filter := identity.ManagedSessionFilter{Limit: identity.ManagedSessionDefaultLimit}
	invalid := func(field, message string) (identity.ManagedSessionFilter, error) {
		return identity.ManagedSessionFilter{}, &identity.ManagedSessionValidationError{Fields: map[string]string{field: message}}
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return invalid("Query", "Supply a valid URL query.")
	}
	for name, entries := range values {
		if len(entries) != 1 || !utf8.ValidString(entries[0]) {
			return invalid(name, "Supply each parameter once as valid UTF-8.")
		}
		value := entries[0]
		switch name {
		case "UserId":
			filter.UserID = value
		case "Kind":
			filter.Kind = value
		case "Status":
			filter.Status = value
		case "DeviceId":
			filter.DeviceID = value
		case "SearchTerm":
			filter.SearchTerm = value
		case "StartIndex", "Limit":
			parsed, err := strconv.ParseInt(value, 10, 32)
			if err != nil || parsed < 0 || strconv.FormatInt(parsed, 10) != value {
				return invalid(name, "Supply a canonical nonnegative decimal integer.")
			}
			if name == "StartIndex" {
				filter.StartIndex = int(parsed)
			} else {
				if parsed < 1 || parsed > identity.MaxManagedSessions {
					return invalid(name, "Supply a limit between 1 and 200.")
				}
				filter.Limit = int(parsed)
			}
		default:
			return invalid("Query", "The query contains an unsupported parameter.")
		}
	}
	return filter, nil
}

func (s *Server) revokeAdminSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || len(id) > 256 || !utf8.ValidString(id) || strings.TrimSpace(id) != id || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		adminSessionInputError(w, r, map[string]string{"Id": "Supply a session identifier of 1 to 256 UTF-8 bytes without surrounding whitespace or controls."})
		return
	}
	if r.URL.RawQuery != "" {
		adminSessionInputError(w, r, map[string]string{"Query": "This operation does not accept query parameters."})
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this request.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	data, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(data) {
		adminSessionInputError(w, r, map[string]string{"Body": "Supply an empty UTF-8 JSON object no larger than 4 KiB."})
		return
	}
	if _, fields := managedUserObject(data, nil, ""); fields != nil {
		adminSessionInputError(w, r, fields)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.identity.RevokeManagedSession(r.Context(), actor, id)
	if err != nil {
		s.adminSessionError(w, r, err)
		return
	}
	// Durable authentication revocation precedes process-local retirement.
	// Original file responses also retain their existing authorization watcher.
	if s.eventHub != nil {
		s.eventHub.DisconnectSession(result.SessionID)
	}
	s.hls.cancelMatching(result.SessionID, "")
	if result.CurrentSessionRevoked {
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/admin", HttpOnly: true,
			Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
	s.log.Info("administrator session revocation", "actor_id", actor.User.ID,
		"user_id", result.UserID, "session_id", result.SessionID, "kind", result.Kind)
	jsonResponse(w, http.StatusOK, map[string]any{
		"SessionId": result.SessionID, "UserId": result.UserID, "Kind": result.Kind,
		"RevokedAt": result.RevokedAt.UTC(), "CurrentSessionRevoked": result.CurrentSessionRevoked,
	})
}

func adminSessionInputError(w http.ResponseWriter, r *http.Request, fields map[string]string) {
	requestID, _ := r.Context().Value(requestIDKey).(string)
	jsonResponse(w, http.StatusBadRequest, map[string]any{
		"Error":     map[string]any{"Code": "invalid_input", "Message": "Check the session request fields.", "Fields": fields},
		"RequestId": requestID,
	})
}

func (s *Server) adminSessionError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *identity.ManagedSessionValidationError
	switch {
	case errors.Is(err, identity.ErrManagedSessionNotFound):
		apiError(w, r, http.StatusNotFound, "not_found", "The requested login session was not found.")
	case errors.As(err, &validation):
		adminSessionInputError(w, r, validation.Fields)
	case errors.Is(err, identity.ErrInvalidInput):
		adminSessionInputError(w, r, map[string]string{"Session": "The session request is invalid."})
	default:
		s.identityError(w, r, err)
	}
}

package server

import (
	"encoding/json"
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

func (s *Server) registerAdminDeviceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/devices", s.requireAdmin(s.adminDevices))
	mux.HandleFunc("POST /admin/v1/devices/{id}/options", s.requireAdmin(s.updateAdminDeviceOptions))
	mux.HandleFunc("POST /admin/v1/devices/{id}/delete", s.requireAdmin(s.deleteAdminDevice))
}

func noDeviceCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func nativeManagedDevice(device identity.ManagedDevice) map[string]any {
	return map[string]any{
		"Id": strconv.FormatInt(device.ID, 10), "Revision": strconv.FormatInt(device.Revision, 10),
		"ReportedDeviceId": device.ReportedDeviceID, "Name": device.Name,
		"ReportedName": device.ReportedName, "CustomName": device.CustomName,
		"AppName": device.AppName, "AppVersion": device.AppVersion,
		"LastUserId": device.LastUserID, "LastUserName": device.LastUserName,
		"CreatedAt": device.CreatedAt.UTC(), "LastSeenAt": device.LastSeenAt.UTC(),
		"IpAddress": device.IPAddress, "ActiveLoginCount": device.ActiveLoginCount,
	}
}

func parseAdminDeviceQuery(r *http.Request) (identity.ManagedDeviceFilter, error) {
	filter := identity.ManagedDeviceFilter{Limit: 50}
	invalid := func(field, message string) (identity.ManagedDeviceFilter, error) {
		return identity.ManagedDeviceFilter{}, &identity.DeviceValidationError{Fields: map[string]string{field: message}}
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
		case "SearchTerm":
			if len(value) > 256 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
				return invalid(name, "Supply at most 256 UTF-8 bytes without controls.")
			}
			filter.SearchTerm = value
		case "StartIndex", "Limit":
			parsed, err := strconv.ParseInt(value, 10, 32)
			if err != nil || parsed < 0 || strconv.FormatInt(parsed, 10) != value {
				return invalid(name, "Supply a canonical nonnegative decimal integer.")
			}
			if name == "StartIndex" {
				filter.StartIndex = int(parsed)
			} else {
				if parsed < 1 || parsed > 200 {
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

func (s *Server) adminDevices(w http.ResponseWriter, r *http.Request) {
	noDeviceCache(w)
	filter, err := parseAdminDeviceQuery(r)
	if err != nil {
		s.deviceError(w, r, err)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	page, err := s.identity.ListManagedDevices(r.Context(), actor, filter)
	if err != nil {
		s.deviceError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, device := range page.Items {
		items = append(items, nativeManagedDevice(device))
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"Items": items, "TotalRecordCount": page.TotalRecordCount,
		"StartIndex": page.StartIndex, "Limit": page.Limit,
	})
}

func adminDeviceID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value := r.PathValue("id")
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != value {
		adminDeviceInputError(w, r, map[string]string{"Id": "Supply a canonical positive decimal device identifier."})
		return 0, false
	}
	return id, true
}

func adminDeviceBody(w http.ResponseWriter, r *http.Request, fields []string) (map[string]json.RawMessage, bool) {
	if r.URL.RawQuery != "" {
		adminDeviceInputError(w, r, map[string]string{"Query": "This operation does not accept query parameters."})
		return nil, false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this request.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	data, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(data) {
		adminDeviceInputError(w, r, map[string]string{"Body": "Supply a UTF-8 JSON object no larger than 4 KiB."})
		return nil, false
	}
	values, fieldsInvalid := managedUserObject(data, fields, "")
	if fieldsInvalid != nil {
		adminDeviceInputError(w, r, fieldsInvalid)
		return nil, false
	}
	return values, true
}

func decodeAdminDeviceOptions(w http.ResponseWriter, r *http.Request) (int64, string, bool) {
	values, ok := adminDeviceBody(w, r, []string{"Revision", "CustomName"})
	if !ok {
		return 0, "", false
	}
	invalid := make(map[string]string)
	revision := managedUserRevision(values["Revision"], invalid)
	var customName string
	managedUserValue(values["CustomName"], "CustomName", &customName, invalid)
	if !utf8.ValidString(customName) || strings.IndexFunc(customName, unicode.IsControl) >= 0 {
		invalid["CustomName"] = "Supply a UTF-8 name without controls."
	}
	customName = strings.TrimSpace(customName)
	if len(customName) > 256 {
		invalid["CustomName"] = "Supply a name of at most 256 UTF-8 bytes."
	}
	if len(invalid) != 0 {
		adminDeviceInputError(w, r, invalid)
		return 0, "", false
	}
	return revision, customName, true
}

func decodeAdminDeviceDelete(w http.ResponseWriter, r *http.Request) (int64, bool) {
	values, ok := adminDeviceBody(w, r, []string{"Revision"})
	if !ok {
		return 0, false
	}
	invalid := make(map[string]string)
	revision := managedUserRevision(values["Revision"], invalid)
	if len(invalid) != 0 {
		adminDeviceInputError(w, r, invalid)
		return 0, false
	}
	return revision, true
}

func (s *Server) updateAdminDeviceOptions(w http.ResponseWriter, r *http.Request) {
	noDeviceCache(w)
	id, ok := adminDeviceID(w, r)
	if !ok {
		return
	}
	revision, customName, ok := decodeAdminDeviceOptions(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	device, err := s.identity.UpdateManagedDeviceOptions(r.Context(), actor, id, revision, customName)
	if err != nil {
		s.deviceError(w, r, err)
		return
	}
	s.log.Info("administrator device options updated", "actor_id", actor.User.ID, "device_id", device.ID)
	jsonResponse(w, http.StatusOK, nativeManagedDevice(device))
}

// Retire process-local consumers only after the store commits credential
// revocation. IDs identify ordinary logins or parent application credentials,
// whose client contexts must all close. Original responses retain their bounded
// authorization watchers.
func (s *Server) retireDeviceLogins(result identity.DeviceDeletion) {
	for _, sessionID := range result.RevokedSessionIDs {
		if s.eventHub != nil {
			s.eventHub.DisconnectCredential(sessionID)
		}
		s.hls.cancelMatching(sessionID, "")
	}
}

func (s *Server) deleteAdminDevice(w http.ResponseWriter, r *http.Request) {
	noDeviceCache(w)
	id, ok := adminDeviceID(w, r)
	if !ok {
		return
	}
	revision, ok := decodeAdminDeviceDelete(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.identity.DeleteManagedDevice(r.Context(), actor, id, revision)
	if err != nil {
		s.deviceError(w, r, err)
		return
	}
	s.retireDeviceLogins(result)
	s.log.Info("administrator device removed", "actor_id", actor.User.ID, "device_id", result.ID,
		"revoked_login_count", result.RevokedLoginCount)
	jsonResponse(w, http.StatusOK, map[string]any{
		"Id": strconv.FormatInt(result.ID, 10), "DeletedAt": result.DeletedAt.UTC(),
		"RevokedLoginCount": result.RevokedLoginCount,
	})
}

func adminDeviceInputError(w http.ResponseWriter, r *http.Request, fields map[string]string) {
	requestID, _ := r.Context().Value(requestIDKey).(string)
	jsonResponse(w, http.StatusBadRequest, map[string]any{
		"Error":     map[string]any{"Code": "invalid_input", "Message": "Check the device request fields.", "Fields": fields},
		"RequestId": requestID,
	})
}

func (s *Server) deviceError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *identity.DeviceValidationError
	compatibility := strings.HasPrefix(r.URL.Path, "/emby/")
	switch {
	case errors.Is(err, identity.ErrClientSessionForbidden):
		if compatibility {
			actor, _ := r.Context().Value(principalKey).(identity.Principal)
			embyTextError(w, r, http.StatusForbidden, "User "+actor.User.Name+" does not have access to ManageServer feature.")
			return
		}
		apiError(w, r, http.StatusForbidden, "administrator_required", "Administrator access is required.")
	case errors.Is(err, identity.ErrDeviceNotFound):
		if compatibility {
			embyTextError(w, r, http.StatusNotFound, "Exception of type 'MediaBrowser.Common.Extensions.ResourceNotFoundException' was thrown.")
			return
		}
		apiError(w, r, http.StatusNotFound, "not_found", "The requested device was not found.")
	case errors.Is(err, identity.ErrDeviceRevisionConflict):
		apiError(w, r, http.StatusConflict, "revision_conflict", "The device changed. Refresh it before trying again.")
	case errors.As(err, &validation) && !compatibility:
		adminDeviceInputError(w, r, validation.Fields)
	case errors.Is(err, identity.ErrInvalidInput):
		if compatibility {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "Check the device identifier and option values.")
			return
		}
		adminDeviceInputError(w, r, map[string]string{"Device": "The device request is invalid."})
	default:
		s.identityError(w, r, err)
	}
}

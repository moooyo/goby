package server

import (
	"bytes"
	"context"
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

func (s *Server) registerDeviceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Devices", s.requireEmby(s.embyDevices))
	mux.HandleFunc("GET /emby/Devices/Info", s.requireEmby(s.embyDeviceInfo))
	mux.HandleFunc("GET /emby/Devices/Options", s.requireEmby(s.embyDeviceOptions))
	mux.HandleFunc("POST /emby/Devices/Options", s.requireEmby(s.updateEmbyDeviceOptions))
	mux.HandleFunc("DELETE /emby/Devices", s.requireEmby(s.deleteEmbyDevice))
	mux.HandleFunc("POST /emby/Devices/Delete", s.requireEmby(s.deleteEmbyDevice))
}

func embyDeviceDTO(device identity.ManagedDevice) map[string]any {
	result := map[string]any{
		"Id": strconv.FormatInt(device.ID, 10), "ReportedDeviceId": device.ReportedDeviceID,
		"Name": device.Name, "AppName": device.AppName, "AppVersion": device.AppVersion,
		"DateLastActivity": device.LastSeenAt.UTC(), "IpAddress": device.IPAddress,
	}
	if device.LastUserID != nil {
		result["LastUserId"] = *device.LastUserID
	}
	if device.LastUserName != nil {
		result["LastUserName"] = *device.LastUserName
	}
	return result
}

func embyDeviceOptionsDTO(device identity.ManagedDevice) map[string]any {
	result := map[string]any{}
	if device.CustomName != nil && *device.CustomName != "" {
		result["CustomName"] = *device.CustomName
	}
	return result
}

// Compatibility query names are case-insensitive; lookup values remain opaque.
// Reject duplicate aliases before selecting a value. SortOrder is accepted but
// does not change the registry's deterministic activity order, as observed.
func parseEmbyDeviceQuery(r *http.Request, list bool) (string, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return "", identity.ErrInvalidInput
	}
	seen := make(map[string]bool)
	lookup := ""
	for name, entries := range values {
		field := strings.ToLower(name)
		if len(entries) != 1 || seen[field] || !utf8.ValidString(entries[0]) {
			return "", identity.ErrInvalidInput
		}
		seen[field] = true
		switch {
		case field == "api_key":
			// Credential parsing already validated the transport before dispatch.
		case list && field == "sortorder":
			if len(entries[0]) > 256 || strings.IndexFunc(entries[0], unicode.IsControl) >= 0 {
				return "", identity.ErrInvalidInput
			}
		case !list && field == "id":
			lookup = entries[0]
			if len(lookup) > 256 || strings.IndexFunc(lookup, unicode.IsControl) >= 0 {
				return "", identity.ErrInvalidInput
			}
		default:
			return "", identity.ErrInvalidInput
		}
	}
	if !list && lookup == "" {
		return "", identity.ErrDeviceNotFound
	}
	return lookup, nil
}

func numericDeviceLookup(value string) bool {
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == value
}

// Shared server-device aliases take precedence in the compatibility namespace.
// Only a definitive missing alias permits the ordinary-device fallback; stale
// authority and storage failures must never select a different mutation target.
func (s *Server) lookupCompatibilityDevice(ctx context.Context, actor identity.Principal, lookup string) (identity.ManagedDevice, error) {
	device, err := s.identity.LookupApplicationKeyDevice(ctx, actor, lookup)
	if !errors.Is(err, identity.ErrDeviceNotFound) || errors.Is(err, identity.ErrApplicationKeyDeviceRemoved) {
		return device, err
	}
	return s.identity.LookupEmbyDevice(ctx, actor, lookup)
}

func (s *Server) updateCompatibilityDeviceOptions(ctx context.Context, actor identity.Principal, lookup, customName string) (identity.ManagedDevice, error) {
	device, err := s.identity.UpdateApplicationKeyDeviceOptions(ctx, actor, lookup, customName)
	if !errors.Is(err, identity.ErrDeviceNotFound) || errors.Is(err, identity.ErrApplicationKeyDeviceRemoved) {
		return device, err
	}
	return s.identity.UpdateEmbyDeviceOptions(ctx, actor, lookup, customName)
}

func (s *Server) deleteCompatibilityDevice(ctx context.Context, actor identity.Principal, lookup string) (identity.DeviceDeletion, error) {
	result, err := s.identity.DeleteApplicationKeyDevice(ctx, actor, lookup)
	if !errors.Is(err, identity.ErrDeviceNotFound) || errors.Is(err, identity.ErrApplicationKeyDeviceRemoved) {
		// A known removed shared generation succeeds idempotently here. It must
		// not fall through and remove a newer ordinary device with another ID.
		return result, err
	}
	return s.identity.DeleteEmbyDevice(ctx, actor, lookup)
}

func (s *Server) embyDevices(w http.ResponseWriter, r *http.Request) {
	noDeviceCache(w)
	if !s.keyManager(w, r) {
		return
	}
	if _, err := parseEmbyDeviceQuery(r, true); err != nil {
		s.deviceError(w, r, err)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	devices, err := s.identity.ListEmbyDevices(r.Context(), actor)
	if err != nil {
		s.deviceError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(devices))
	for _, device := range devices {
		items = append(items, embyDeviceDTO(device))
	}
	// The reference's nonempty list can report zero; Goby supplies a coherent
	// count rather than using that defective count as an absence signal.
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": len(items)})
}

func (s *Server) embyDeviceInfo(w http.ResponseWriter, r *http.Request) {
	noDeviceCache(w)
	if !s.keyManager(w, r) {
		return
	}
	lookup, err := parseEmbyDeviceQuery(r, false)
	if err != nil {
		s.deviceError(w, r, err)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	device, err := s.lookupCompatibilityDevice(r.Context(), actor, lookup)
	if errors.Is(err, identity.ErrDeviceNotFound) && numericDeviceLookup(lookup) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		s.deviceError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, embyDeviceDTO(device))
}

func (s *Server) embyDeviceOptions(w http.ResponseWriter, r *http.Request) {
	noDeviceCache(w)
	if !s.keyManager(w, r) {
		return
	}
	lookup, err := parseEmbyDeviceQuery(r, false)
	if err != nil {
		s.deviceError(w, r, err)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	device, err := s.lookupCompatibilityDevice(r.Context(), actor, lookup)
	if errors.Is(err, identity.ErrDeviceNotFound) && numericDeviceLookup(lookup) {
		jsonResponse(w, http.StatusOK, map[string]any{})
		return
	}
	if err != nil {
		s.deviceError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, embyDeviceOptionsDTO(device))
}

func decodeEmbyDeviceOptions(w http.ResponseWriter, r *http.Request) (string, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this request.")
		return "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	data, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(data) {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply a UTF-8 JSON object no larger than 4 KiB.")
		return "", false
	}
	fields, err := remoteCommandObject(data, true)
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply one JSON object without duplicate fields.")
		return "", false
	}
	var customName string
	// Missing, null and empty CustomName all clear an existing override. Unknown
	// bounded fields are discarded rather than becoming registry properties.
	if raw, present := fields["customname"]; present && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if json.Unmarshal(raw, &customName) != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "CustomName must be a string or null.")
			return "", false
		}
	}
	if !utf8.ValidString(customName) || strings.IndexFunc(customName, unicode.IsControl) >= 0 {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "CustomName must be valid UTF-8 without controls.")
		return "", false
	}
	customName = strings.TrimSpace(customName)
	if len(customName) > 256 {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "CustomName must contain at most 256 UTF-8 bytes.")
		return "", false
	}
	return customName, true
}

func (s *Server) updateEmbyDeviceOptions(w http.ResponseWriter, r *http.Request) {
	noDeviceCache(w)
	if !s.keyManager(w, r) {
		return
	}
	lookup, err := parseEmbyDeviceQuery(r, false)
	if err != nil {
		s.deviceError(w, r, err)
		return
	}
	customName, ok := decodeEmbyDeviceOptions(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	device, err := s.updateCompatibilityDeviceOptions(r.Context(), actor, lookup, customName)
	if err != nil {
		s.deviceError(w, r, err)
		return
	}
	s.log.Info("compatibility device options updated", "actor_credential_id", actor.SessionID, "device_id", device.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteEmbyDevice(w http.ResponseWriter, r *http.Request) {
	noDeviceCache(w)
	if !s.keyManager(w, r) {
		return
	}
	lookup, err := parseEmbyDeviceQuery(r, false)
	if err != nil {
		s.deviceError(w, r, err)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.deleteCompatibilityDevice(r.Context(), actor, lookup)
	if errors.Is(err, identity.ErrDeviceNotFound) && numericDeviceLookup(lookup) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		s.deviceError(w, r, err)
		return
	}
	s.retireDeviceLogins(result)
	s.log.Info("compatibility device removed", "actor_credential_id", actor.SessionID, "device_id", result.ID,
		"revoked_login_count", result.RevokedLoginCount)
	w.WriteHeader(http.StatusNoContent)
}

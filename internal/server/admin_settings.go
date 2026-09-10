package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/settings"
)

const maxAdminSettingsBodyBytes = 16 * 1024

var adminSettingsValueFields = []string{"ServerName", "MaxBitrate", "MaxWidth", "MaxHeight", "MaxAudioChannels"}

func (s *Server) registerAdminSettingsRoutes(mux *http.ServeMux) {
	wrap := func(handler http.HandlerFunc) http.HandlerFunc {
		authorized := s.requireAdmin(handler)
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			authorized(w, r)
		}
	}
	mux.HandleFunc("GET /admin/v1/settings", wrap(s.adminSettings))
	mux.HandleFunc("PUT /admin/v1/settings", wrap(s.updateAdminSettings))
	mux.HandleFunc("POST /admin/v1/settings/reset", wrap(s.resetAdminSettings))
}

func adminSettingsInputError(w http.ResponseWriter, r *http.Request, fields map[string]string) {
	requestID, _ := r.Context().Value(requestIDKey).(string)
	jsonResponse(w, http.StatusBadRequest, map[string]any{
		"Error":     map[string]any{"Code": "invalid_input", "Message": "Check the settings request fields.", "Fields": fields},
		"RequestId": requestID,
	})
}

func adminSettingsNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		adminSettingsInputError(w, r, map[string]string{"Query": "This operation does not accept query parameters."})
		return false
	}
	return true
}

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. Settings must
// reject that lossy input while retaining valid pairs and actual U+FFFD text.
func adminSettingsUnicode(data []byte) bool {
	hexUnit := func(value []byte) (uint16, bool) {
		if len(value) != 4 {
			return 0, false
		}
		var result uint16
		for _, digit := range value {
			result <<= 4
			switch {
			case digit >= '0' && digit <= '9':
				result += uint16(digit - '0')
			case digit >= 'a' && digit <= 'f':
				result += uint16(digit-'a') + 10
			case digit >= 'A' && digit <= 'F':
				result += uint16(digit-'A') + 10
			default:
				return 0, false
			}
		}
		return result, true
	}
	for index := 0; index < len(data); index++ {
		if data[index] != '"' {
			continue
		}
		for index++; index < len(data) && data[index] != '"'; index++ {
			if data[index] != '\\' {
				continue
			}
			index++
			if index >= len(data) {
				return false
			}
			if data[index] != 'u' {
				continue
			}
			if index+4 >= len(data) {
				return false
			}
			unit, ok := hexUnit(data[index+1 : index+5])
			if !ok {
				return false
			}
			index += 4
			if unit >= 0xdc00 && unit <= 0xdfff {
				return false
			}
			if unit < 0xd800 || unit > 0xdbff {
				continue
			}
			if index+6 >= len(data) || data[index+1] != '\\' || data[index+2] != 'u' {
				return false
			}
			low, ok := hexUnit(data[index+3 : index+7])
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			index += 6
		}
	}
	return true
}

func adminSettingsBody(w http.ResponseWriter, r *http.Request, fields []string) (map[string]json.RawMessage, bool) {
	if !adminSettingsNoQuery(w, r) {
		return nil, false
	}
	contentTypes := r.Header.Values("Content-Type")
	rawType := r.Header.Get("Content-Type")
	_, parameter, hasParameter := strings.Cut(rawType, ";")
	parameterName, _, hasParameterValue := strings.Cut(parameter, "=")
	// ParseMediaType may normalize or discard RFC 2231 extensions. Accept
	// only the literal optional charset parameter before that normalization.
	validParameter := !hasParameter || hasParameterValue && strings.EqualFold(strings.TrimSpace(parameterName), "charset")
	mediaType, parameters, err := mime.ParseMediaType(rawType)
	if len(contentTypes) != 1 || err != nil || mediaType != "application/json" || len(parameters) > 1 ||
		!validParameter || len(parameters) == 1 && !strings.EqualFold(parameters["charset"], "utf-8") || strings.Count(rawType, ";") > 1 {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json with UTF-8 for this request.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminSettingsBodyBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(data) || !adminSettingsUnicode(data) {
		adminSettingsInputError(w, r, map[string]string{"Body": "Supply a lossless UTF-8 JSON object no larger than 16 KiB."})
		return nil, false
	}
	values, invalid := adminTaskObject(data, fields, fields, "")
	if invalid != nil {
		adminSettingsInputError(w, r, invalid)
		return nil, false
	}
	return values, true
}

func adminSettingsRevision(raw json.RawMessage, invalid map[string]string) int64 {
	revision := managedUserRevision(raw, invalid)
	if revision == math.MaxInt64 {
		invalid["Revision"] = "Supply the current positive decimal revision string with room for its successor."
	}
	return revision
}

func adminSettingsNumber(raw json.RawMessage, field string, invalid map[string]string) *int64 {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value int64
	if json.Unmarshal(raw, &value) != nil || value < 1 || value > 1_000_000_000 {
		invalid[field] = "Supply an integer JSON number between 1 and 1000000000, or null."
		return nil
	}
	return &value
}

func decodeAdminSettingsUpdate(w http.ResponseWriter, r *http.Request) (settings.UpdateRequest, bool) {
	values, ok := adminSettingsBody(w, r, []string{"Revision", "Overrides"})
	if !ok {
		return settings.UpdateRequest{}, false
	}
	invalid := make(map[string]string)
	request := settings.UpdateRequest{Revision: adminSettingsRevision(values["Revision"], invalid)}
	overrides, fieldsInvalid := adminTaskObject(values["Overrides"], adminSettingsValueFields, adminSettingsValueFields, "Overrides")
	for field, message := range fieldsInvalid {
		invalid[field] = message
	}
	if fieldsInvalid == nil {
		if !bytes.Equal(bytes.TrimSpace(overrides["ServerName"]), []byte("null")) {
			var value string
			if json.Unmarshal(overrides["ServerName"], &value) != nil {
				invalid["Overrides.ServerName"] = "Supply a server name string or null."
			} else {
				// Preserve exact valid text. The shared domain/config validator
				// owns name policy, including whitespace and byte-length rules.
				request.Overrides.ServerName = &value
			}
		}
		request.Overrides.MaxBitrate = adminSettingsNumber(overrides["MaxBitrate"], "Overrides.MaxBitrate", invalid)
		for _, field := range []struct {
			name   string
			target **int
		}{
			{"MaxWidth", &request.Overrides.MaxWidth}, {"MaxHeight", &request.Overrides.MaxHeight},
			{"MaxAudioChannels", &request.Overrides.MaxAudioChannels},
		} {
			if value := adminSettingsNumber(overrides[field.name], "Overrides."+field.name, invalid); value != nil {
				converted := int(*value) // The wire bound fits even a 32-bit int.
				*field.target = &converted
			}
		}
	}
	if len(invalid) != 0 {
		adminSettingsInputError(w, r, invalid)
		return settings.UpdateRequest{}, false
	}
	return request, true
}

func decodeAdminSettingsReset(w http.ResponseWriter, r *http.Request) (settings.ResetRequest, bool) {
	values, ok := adminSettingsBody(w, r, []string{"Revision", "Fields"})
	if !ok {
		return settings.ResetRequest{}, false
	}
	invalid := make(map[string]string)
	request := settings.ResetRequest{Revision: adminSettingsRevision(values["Revision"], invalid)}
	var fields []settings.Field
	if json.Unmarshal(values["Fields"], &fields) != nil || len(fields) < 1 || len(fields) > len(adminSettingsValueFields) {
		invalid["Fields"] = "Select between one and five supported settings to reset."
	} else {
		seen := make(map[settings.Field]bool, len(fields))
		for _, field := range fields {
			switch field {
			case settings.FieldServerName, settings.FieldMaxBitrate, settings.FieldMaxWidth, settings.FieldMaxHeight, settings.FieldMaxAudioChannels:
				if seen[field] {
					invalid["Fields"] = "Select each supported setting at most once."
				}
				seen[field] = true
			default:
				invalid["Fields"] = "Select only supported settings to reset."
			}
		}
	}
	if len(invalid) != 0 {
		adminSettingsInputError(w, r, invalid)
		return settings.ResetRequest{}, false
	}
	request.Fields = fields
	return request, true
}

func adminSettingsActor(r *http.Request) settings.Actor {
	principal, _ := r.Context().Value(principalKey).(identity.Principal)
	return settings.Actor{Principal: principal, Audience: identity.AdministratorNative}
}

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	if !adminSettingsNoQuery(w, r) {
		return
	}
	if s.settings == nil {
		s.settingsError(w, r, settings.ErrUnavailable)
		return
	}
	snapshot, err := s.settings.Get(r.Context(), adminSettingsActor(r))
	if err != nil {
		s.settingsError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, settingsDTO(snapshot, s.cfg.Transcoding))
}

func (s *Server) updateAdminSettings(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeAdminSettingsUpdate(w, r)
	if !ok {
		return
	}
	if s.settings == nil {
		s.settingsError(w, r, settings.ErrUnavailable)
		return
	}
	committed, err := s.settings.Update(r.Context(), adminSettingsActor(r), request)
	if err != nil {
		s.settingsError(w, r, err)
		return
	}
	// A request-entry snapshot predates this commit and is never a response.
	jsonResponse(w, http.StatusOK, settingsDTO(committed, s.cfg.Transcoding))
}

func (s *Server) resetAdminSettings(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeAdminSettingsReset(w, r)
	if !ok {
		return
	}
	if s.settings == nil {
		s.settingsError(w, r, settings.ErrUnavailable)
		return
	}
	committed, err := s.settings.Reset(r.Context(), adminSettingsActor(r), request)
	if err != nil {
		s.settingsError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, settingsDTO(committed, s.cfg.Transcoding))
}

func (s *Server) settingsError(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *settings.ValidationError
	switch {
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials):
		s.identityError(w, r, err)
	case errors.Is(err, identity.ErrClientSessionForbidden):
		apiError(w, r, http.StatusForbidden, "administrator_required", "Administrator access is required.")
	case errors.Is(err, settings.ErrUnavailable), errors.Is(err, settings.ErrStoredSettings), errors.Is(err, library.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		apiError(w, r, http.StatusServiceUnavailable, "settings_unavailable", "Server settings are currently unavailable.")
	case errors.Is(err, settings.ErrRevisionConflict):
		apiError(w, r, http.StatusConflict, "revision_conflict", "Settings changed. Refresh them before trying again.")
	case errors.As(err, &invalid):
		fields := make(map[string]string)
		for field := range invalid.Fields {
			switch field {
			case "ServerName":
				fields["Overrides.ServerName"] = "Use a nonblank UTF-8 server name of at most 128 bytes without NUL characters."
			case "MaxBitrate":
				fields["Overrides.MaxBitrate"] = "Supply an integer between 1 and 1000000000."
			case "MaxWidth", "MaxHeight":
				fields["Overrides."+field] = "Supply an integer between 1 and 8192."
			case "MaxAudioChannels":
				fields["Overrides.MaxAudioChannels"] = "Supply an integer between 1 and 8."
			case "Revision":
				fields[field] = "Supply the current positive decimal revision string with room for its successor."
			case "Fields":
				fields[field] = "Select between one and five unique supported settings."
			default:
				fields["Body"] = "Supply supported settings values."
			}
		}
		if len(fields) == 0 {
			fields["Body"] = "Supply supported settings values."
		}
		adminSettingsInputError(w, r, fields)
	case errors.Is(err, settings.ErrInvalidInput):
		adminSettingsInputError(w, r, map[string]string{"Body": "Supply supported settings values."})
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		return
	default:
		if s.log != nil {
			s.log.Error("settings operation failed", "request_id", r.Context().Value(requestIDKey))
		}
		apiError(w, r, http.StatusInternalServerError, "internal_error", "The settings operation could not be completed.")
	}
}

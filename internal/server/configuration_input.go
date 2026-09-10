package server

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/settings"
)

const maxConfigurationBodyBytes = 32 * 1024

func configurationInputError(w http.ResponseWriter, r *http.Request) {
	// Submitted values and unknown field names never enter diagnostics.
	embyTextError(w, r, http.StatusBadRequest, "Supply valid supported configuration fields.")
}

func configurationQuery(w http.ResponseWriter, r *http.Request) bool {
	if len(r.URL.RawQuery) > 4096 {
		configurationInputError(w, r)
		return false
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		configurationInputError(w, r)
		return false
	}
	for field, values := range query {
		if field != "api_key" || len(values) != 1 {
			configurationInputError(w, r)
			return false
		}
	}
	return true
}

func configurationBody(w http.ResponseWriter, r *http.Request, section settings.ConfigurationSection) (map[string]json.RawMessage, bool) {
	rawType := r.Header.Get("Content-Type")
	_, parameter, hasParameter := strings.Cut(rawType, ";")
	parameterName, _, hasParameterValue := strings.Cut(parameter, "=")
	validParameter := !hasParameter || hasParameterValue && strings.EqualFold(strings.TrimSpace(parameterName), "charset")
	mediaType, parameters, err := mime.ParseMediaType(rawType)
	validType := mediaType == "application/json" || section == settings.ConfigurationEncoding && mediaType == "application/octet-stream"
	if len(r.Header.Values("Content-Type")) != 1 || err != nil || !validType || !validParameter || len(parameters) > 1 ||
		len(parameters) == 1 && !strings.EqualFold(parameters["charset"], "utf-8") || strings.Count(rawType, ";") > 1 {
		embyTextError(w, r, http.StatusUnsupportedMediaType, "Use UTF-8 JSON for this configuration request.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxConfigurationBodyBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(data) || !adminSettingsUnicode(data) {
		configurationInputError(w, r)
		return nil, false
	}
	// The shared object reader rejects duplicate decoded names, including
	// aliases differing only in case. Values are decoded only after allowlisting.
	values, err := remoteCommandObject(data, true)
	if err != nil {
		configurationInputError(w, r)
		return nil, false
	}
	return values, true
}

func decodeConfiguration(w http.ResponseWriter, r *http.Request, section settings.ConfigurationSection) (settings.ConfigurationMutation, bool) {
	values, ok := configurationBody(w, r, section)
	if !ok {
		return settings.ConfigurationMutation{}, false
	}
	mutation := settings.ConfigurationMutation{Section: section}
	for field, raw := range values {
		switch {
		case section != settings.ConfigurationEncoding && field == "servername":
			mutation.ServerNamePresent = true
			if json.Unmarshal(raw, &mutation.ServerName) != nil {
				configurationInputError(w, r)
				return settings.ConfigurationMutation{}, false
			}
		case section != settings.ConfigurationEncoding && field == "isstartupwizardcompleted":
			if json.Unmarshal(raw, &mutation.StartupWizardCompleted) != nil || mutation.StartupWizardCompleted == nil {
				configurationInputError(w, r)
				return settings.ConfigurationMutation{}, false
			}
		case section == settings.ConfigurationEncoding && field == "transcodingmaxwidth":
			var width *int
			if json.Unmarshal(raw, &width) != nil || width == nil || *width < 0 || *width > 8192 {
				configurationInputError(w, r)
				return settings.ConfigurationMutation{}, false
			}
			mutation.TranscodingMaxWidthPresent = true
			mutation.TranscodingMaxWidth = *width
		default:
			configurationInputError(w, r)
			return settings.ConfigurationMutation{}, false
		}
	}
	return mutation, true
}

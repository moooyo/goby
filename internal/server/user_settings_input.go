package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
)

const (
	maxUserSettingsBodyBytes = 256 * 1024
	maxUserSettingsDepth     = 16
	maxUserSettingsNodes     = 4096
)

func userSettingsInputError(w http.ResponseWriter, r *http.Request) {
	embyTextError(w, r, http.StatusBadRequest, "Supply a valid user settings object.")
}

func userSettingsQuery(w http.ResponseWriter, r *http.Request) bool {
	if len(r.URL.RawQuery) > 4096 {
		userSettingsInputError(w, r)
		return false
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		userSettingsInputError(w, r)
		return false
	}
	seen := make(map[string]bool, len(query))
	for name, values := range query {
		name = strings.ToLower(name)
		if seen[name] || len(values) != 1 {
			userSettingsInputError(w, r)
			return false
		}
		seen[name] = true
		switch name {
		case "api_key", "x-emby-token", "x-emby-client", "x-emby-client-version", "x-emby-device-id", "x-emby-device-name":
			// Authentication already resolved these carriers. They never select
			// a target user or an additional preference namespace.
		case "reqformat":
			if !strings.EqualFold(values[0], "json") {
				userSettingsInputError(w, r)
				return false
			}
		case "x-emby-language":
			// The original Web Client sends its UI language on every request.
			// It does not select another user or a preference partition.
			if len(values[0]) > 256 || !utf8.ValidString(values[0]) || strings.ContainsRune(values[0], '\x00') {
				userSettingsInputError(w, r)
				return false
			}
		case "client":
			if r.Method != http.MethodGet && r.Method != http.MethodHead || len(values[0]) > identity.MaxUserSettingsKeyBytes ||
				!utf8.ValidString(values[0]) || strings.ContainsRune(values[0], '\x00') {
				userSettingsInputError(w, r)
				return false
			}
		default:
			userSettingsInputError(w, r)
			return false
		}
	}
	return true
}

func userSettingsBody(w http.ResponseWriter, r *http.Request) (identity.UserSettingsPatch, bool) {
	rawType := r.Header.Get("Content-Type")
	_, parameter, hasParameter := strings.Cut(rawType, ";")
	parameterName, _, hasParameterValue := strings.Cut(parameter, "=")
	validParameter := !hasParameter || hasParameterValue && strings.EqualFold(strings.TrimSpace(parameterName), "charset")
	mediaType, parameters, err := mime.ParseMediaType(rawType)
	validType := mediaType == "text/plain" || mediaType == "application/json" || mediaType == "application/octet-stream"
	if len(r.Header.Values("Content-Type")) != 1 || err != nil || !validType || !validParameter || len(parameters) > 1 ||
		len(parameters) == 1 && !strings.EqualFold(parameters["charset"], "utf-8") || strings.Count(rawType, ";") > 1 ||
		r.Header.Get("Content-Encoding") != "" {
		embyTextError(w, r, http.StatusUnsupportedMediaType, "Use an uncompressed UTF-8 JSON object for this request.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUserSettingsBodyBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			embyTextError(w, r, http.StatusRequestEntityTooLarge, "User settings exceed the supported limits.")
		} else {
			userSettingsInputError(w, r)
		}
		return nil, false
	}
	patch, err := decodeUserSettingsPatch(data)
	if err != nil {
		if errors.Is(err, identity.ErrUserSettingsLimit) {
			embyTextError(w, r, http.StatusRequestEntityTooLarge, "User settings exceed the supported limits.")
		} else {
			userSettingsInputError(w, r)
		}
		return nil, false
	}
	return patch, true
}

func decodeUserSettingsPatch(data []byte) (identity.UserSettingsPatch, error) {
	if len(data) > maxUserSettingsBodyBytes {
		return nil, identity.ErrUserSettingsLimit
	}
	if !utf8.Valid(data) || !adminSettingsUnicode(data) {
		return nil, identity.ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, identity.ErrInvalidInput
	}
	patch := identity.UserSettingsPatch{}
	nodes := 0
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, identity.ErrInvalidInput
		}
		key, ok := token.(string)
		if !ok {
			return nil, identity.ErrInvalidInput
		}
		if len(key) > identity.MaxUserSettingsKeyBytes {
			return nil, identity.ErrUserSettingsLimit
		}
		for existing := range patch {
			if strings.EqualFold(existing, key) {
				key = existing
				break
			}
		}
		_, exists := patch[key]
		if !exists && len(patch) >= identity.MaxUserSettingsEntries {
			return nil, identity.ErrUserSettingsLimit
		}
		value, err := readUserSettingsValue(decoder, 0, &nodes)
		if err != nil {
			return nil, err
		}
		patch[key] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, identity.ErrInvalidInput
	}
	// One extra token is enough to reject another value without parsing an
	// unbounded generic tree outside the value reader's depth and node limits.
	if _, err := decoder.Token(); err != io.EOF {
		return nil, identity.ErrInvalidInput
	}
	return patch, nil
}

// The pinned reference converts structured values to ordered text, retaining
// delimiters but not adding quotes or escapes around nested string values.
// Parsing and output both remain bounded independently of the request size.
func readUserSettingsValue(decoder *json.Decoder, depth int, nodes *int) (*string, error) {
	*nodes = *nodes + 1
	if depth > maxUserSettingsDepth || *nodes > maxUserSettingsNodes {
		return nil, identity.ErrUserSettingsLimit
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, identity.ErrInvalidInput
	}
	var text string
	switch value := token.(type) {
	case nil:
		if depth == 0 {
			return nil, nil
		}
		text = "null"
	case string:
		text = value
	case json.Number:
		text = value.String()
	case bool:
		text = "false"
		if value {
			text = "true"
		}
	case json.Delim:
		if value != '{' && value != '[' {
			return nil, identity.ErrInvalidInput
		}
		var output strings.Builder
		output.WriteRune(rune(value))
		appendText := func(value string) error {
			if output.Len()+len(value) > identity.MaxUserSettingsValueBytes {
				return identity.ErrUserSettingsLimit
			}
			output.WriteString(value)
			return nil
		}
		first := true
		for decoder.More() {
			if !first {
				if err := appendText(","); err != nil {
					return nil, err
				}
			}
			first = false
			if value == '{' {
				keyToken, err := decoder.Token()
				key, ok := keyToken.(string)
				if err != nil || !ok {
					return nil, identity.ErrInvalidInput
				}
				if err := appendText(key + ":"); err != nil {
					return nil, err
				}
			}
			nested, err := readUserSettingsValue(decoder, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			if err := appendText(*nested); err != nil {
				return nil, err
			}
		}
		end, err := decoder.Token()
		closing := json.Delim(']')
		if value == '{' {
			closing = '}'
		}
		if err != nil || end != closing {
			return nil, identity.ErrInvalidInput
		}
		if err := appendText(string(closing)); err != nil {
			return nil, err
		}
		text = output.String()
	default:
		return nil, identity.ErrInvalidInput
	}
	if len(text) > identity.MaxUserSettingsValueBytes {
		return nil, identity.ErrUserSettingsLimit
	}
	return &text, nil
}

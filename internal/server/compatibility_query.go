package server

import (
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
)

// embyBusinessQuery separates explicitly supported transport metadata from
// endpoint parameters. Authentication remains the caller's responsibility;
// this helper never resolves a token or authorizes a public or privileged API.
// Reusing the credential parser preserves its header/query conflict rules even
// when a caller exercises the query boundary independently of auth middleware.
func embyBusinessQuery(r *http.Request) (url.Values, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, identity.ErrInvalidInput
	}
	if !validEmbyLanguageQuery(values) {
		return nil, identity.ErrInvalidInput
	}
	_, client, err := parseEmbyCredentials(r)
	if err != nil {
		return nil, identity.ErrInvalidInput
	}
	for _, value := range []string{client.Name, client.Version, client.DeviceID, client.Device} {
		if len(value) > identity.MaxDeviceFieldBytes || !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
			return nil, identity.ErrInvalidInput
		}
	}
	for name := range values {
		field := strings.ToLower(name)
		switch field {
		case "api_key", "x-emby-token", "x-emby-client", "x-emby-client-version", "x-emby-device-id", "x-emby-device-name":
			// Matching repetitions retain the authentication parser's policy.
			// Unknown X-Emby names remain business input for the caller to reject.
			delete(values, name)
		case "x-emby-language":
			// The original client sends its UI language on catalog and management
			// requests. This bounded hint does not change identity or metadata.
			delete(values, name)
		}
	}
	return values, nil
}

func validEmbyLanguageQuery(values url.Values) bool {
	seen := false
	for name, entries := range values {
		if !strings.EqualFold(name, "X-Emby-Language") {
			continue
		}
		if seen || len(entries) != 1 || !validEmbyLanguageHint(entries[0]) {
			return false
		}
		seen = true
	}
	return true
}

func validEmbyLanguageHint(value string) bool {
	return len(value) <= 256 && utf8.ValidString(value) && strings.TrimSpace(value) == value &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

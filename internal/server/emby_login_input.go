package server

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const maxEmbyLoginFormBytes = 1 << 20

type embyLoginInput struct {
	Username string
	Pw       string
}

// The original browser client submits Username/Pw as a UTF-8 URLencoded body.
// This adapter is limited to the two Emby login routes. Native administrator
// authentication and the existing JSON login decoder keep their own contracts.
func decodeEmbyLogin(w http.ResponseWriter, r *http.Request, byName bool) (embyLoginInput, bool) {
	var input embyLoginInput
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err == nil && mediaType == "application/json" {
		if byName {
			ok := decodeBody(w, r, &input)
			return input, ok
		}
		var body struct{ Pw string }
		ok := decodeBody(w, r, &body)
		input.Pw = body.Pw
		return input, ok
	}
	if err != nil || mediaType != "application/x-www-form-urlencoded" || len(r.Header.Values("Content-Type")) != 1 ||
		len(r.Header.Values("Content-Encoding")) != 0 || len(parameters) > 1 ||
		strings.Count(r.Header.Get("Content-Type"), ";") > 1 ||
		(len(parameters) == 1 && !strings.EqualFold(parameters["charset"], "utf-8")) {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json or a UTF-8 application/x-www-form-urlencoded body for this request.")
		return input, false
	}
	invalid := func() (embyLoginInput, bool) {
		apiError(w, r, http.StatusBadRequest, "invalid_form", "Supply one unambiguous UTF-8 login form body.")
		return embyLoginInput{}, false
	}
	if r.Body == nil {
		return invalid()
	}
	// Read the actual stream, including unknown/chunked lengths, through the
	// same one-MiB ceiling as JSON. Successful ReadAll consumes EOF. Do not
	// install or expire a connection deadline during normal body cleanup.
	r.Body = http.MaxBytesReader(w, r.Body, maxEmbyLoginFormBytes)
	body, err := io.ReadAll(r.Body)
	defer clear(body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apiError(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "The login form exceeds its request body limit.")
			return input, false
		}
		return invalid()
	}
	if !utf8.Valid(body) || len(body) == 0 || bytes.Count(body, []byte("&")) >= 16 {
		return invalid()
	}
	// Parse only body bytes. ParseForm would merge URL credentials and expose
	// query precedence as an alternative identity source for authentication.
	values, err := url.ParseQuery(string(body))
	if err != nil || len(values) == 0 || len(values) > 2 {
		return invalid()
	}
	seen := make(map[string]bool, 2)
	for name, entries := range values {
		name = strings.ToLower(name)
		if seen[name] || len(entries) != 1 || !utf8.ValidString(entries[0]) || strings.ContainsRune(entries[0], '\x00') {
			return invalid()
		}
		seen[name] = true
		switch name {
		case "username":
			if !byName {
				return invalid()
			}
			input.Username = entries[0]
		case "pw":
			input.Pw = entries[0]
		default:
			return invalid()
		}
	}
	return input, true
}

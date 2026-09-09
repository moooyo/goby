package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/library"
)

// decodeScanOptions preserves legacy empty POSTs while making the costly
// cache-bypass mode an explicit, bounded administrator request.
func decodeScanOptions(w http.ResponseWriter, r *http.Request) (library.ScanOptions, bool) {
	invalid := func(message string) (library.ScanOptions, bool) {
		apiError(w, r, http.StatusBadRequest, "invalid_input", message)
		return library.ScanOptions{}, false
	}
	if r.URL.RawQuery != "" {
		return invalid("This operation does not accept query parameters.")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	data, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(data) {
		return invalid("Supply a UTF-8 JSON object no larger than 4 KiB.")
	}
	if len(data) == 0 {
		return library.ScanOptions{}, true
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this request.")
		return library.ScanOptions{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return invalid("Supply a JSON object with only the optional ForceProbe boolean.")
	}
	var options library.ScanOptions
	if decoder.More() {
		name, err := decoder.Token()
		if err != nil || name != "ForceProbe" {
			return invalid("The object contains an unsupported field.")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return invalid("ForceProbe must be a JSON boolean.")
		}
		switch string(bytes.TrimSpace(value)) {
		case "true":
			options.ForceProbe = true
		case "false":
		default:
			return invalid("ForceProbe must be a JSON boolean.")
		}
		if decoder.More() {
			return invalid("Supply ForceProbe at most once, with no additional fields.")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return invalid("Supply a valid JSON object.")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return invalid("Supply exactly one JSON object.")
	}
	return options, true
}

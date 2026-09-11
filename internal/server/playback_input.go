package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/playback"
)

const (
	maxPlaybackInfoBodyBytes = 1 << 20
	maxPlaybackInfoJSONDepth = 16
	maxPlaybackInfoJSONNodes = 16384
	maxPlaybackInfoJSONText  = 4096
)

// PlaybackInfo has a dedicated wire adapter. The reference Web Client encodes
// two profile scalars as strings; native JSON and other APIs retain their types.
func decodePlaybackInfoBody(w http.ResponseWriter, r *http.Request, request *playback.Request) bool {
	rawType := r.Header.Get("Content-Type")
	_, parameter, hasParameter := strings.Cut(rawType, ";")
	parameterName, _, hasParameterValue := strings.Cut(parameter, "=")
	validParameter := !hasParameter || hasParameterValue && strings.EqualFold(strings.TrimSpace(parameterName), "charset")
	mediaType, parameters, err := mime.ParseMediaType(rawType)
	if err != nil || len(r.Header.Values("Content-Type")) != 1 ||
		mediaType != "application/json" && mediaType != "text/plain" || !validParameter || strings.Count(rawType, ";") > 1 || len(parameters) > 1 ||
		len(parameters) == 1 && !strings.EqualFold(parameters["charset"], "utf-8") {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json or text/plain with UTF-8 JSON for this request.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPlaybackInfoBodyBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_json", "The playback request exceeds its JSON limits or is incomplete.")
		return false
	}
	parsed, err := parsePlaybackInfoBody(data)
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_json", "Supply a valid playback request with supported profile fields.")
		return false
	}
	*request = parsed
	return true
}

func parsePlaybackInfoBody(data []byte) (playback.Request, error) {
	invalid := playback.ErrInvalidRequest
	if len(data) == 0 || len(data) > maxPlaybackInfoBodyBytes || !utf8.Valid(data) || !adminSettingsUnicode(data) {
		return playback.Request{}, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	nodes := 0
	value, err := playbackInfoJSONValue(decoder, nil, 0, &nodes)
	if err != nil {
		return playback.Request{}, err
	}
	if _, ok := value.(map[string]any); !ok {
		return playback.Request{}, invalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return playback.Request{}, invalid
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > maxPlaybackInfoBodyBytes {
		return playback.Request{}, invalid
	}
	// Preserve the previous PlaybackInfo extension contract: unmodeled fields
	// are ignored only after the entire JSON tree passes the bounds above.
	// They cannot select streams or provide authorization to the typed request.
	typed := json.NewDecoder(bytes.NewReader(encoded))
	var request playback.Request
	if typed.Decode(&request) != nil {
		return playback.Request{}, invalid
	}
	return request, nil
}

func playbackInfoJSONValue(decoder *json.Decoder, path []string, depth int, nodes *int) (any, error) {
	*nodes = *nodes + 1
	if depth > maxPlaybackInfoJSONDepth || *nodes > maxPlaybackInfoJSONNodes {
		return nil, playback.ErrInvalidRequest
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, playback.ErrInvalidRequest
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			result := make(map[string]any)
			for decoder.More() {
				keyToken, err := decoder.Token()
				key, ok := keyToken.(string)
				if err != nil || !ok || len(key) == 0 || len(key) > 128 || len(result) >= 64 {
					return nil, playback.ErrInvalidRequest
				}
				// Protocol member names are ASCII. Go's typed decoder folds
				// Unicode aliases such as long s, which lowercase alone misses.
				for _, character := range key {
					if character > 127 {
						return nil, playback.ErrInvalidRequest
					}
				}
				key = strings.ToLower(key)
				if _, exists := result[key]; exists {
					return nil, playback.ErrInvalidRequest
				}
				item, err := playbackInfoJSONValue(decoder, appendPlaybackJSONPath(path, key), depth+1, nodes)
				if err != nil {
					return nil, err
				}
				result[key] = item
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return nil, playback.ErrInvalidRequest
			}
			return result, nil
		case '[':
			result := make([]any, 0)
			for decoder.More() {
				if len(result) >= 1024 {
					return nil, playback.ErrInvalidRequest
				}
				item, err := playbackInfoJSONValue(decoder, appendPlaybackJSONPath(path, "[]"), depth+1, nodes)
				if err != nil {
					return nil, err
				}
				result = append(result, item)
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return nil, playback.ErrInvalidRequest
			}
			return result, nil
		default:
			return nil, playback.ErrInvalidRequest
		}
	case string:
		if len(value) > maxPlaybackInfoJSONText || strings.ContainsRune(value, '\x00') {
			return nil, playback.ErrInvalidRequest
		}
		switch strings.Join(path, "/") {
		case "deviceprofile/transcodingprofiles/[]/minsegments":
			// Only a canonical nonnegative int32 spelling is accepted. Other
			// integer fields do not acquire string coercion from this exception.
			number, err := strconv.ParseInt(value, 10, 32)
			if err != nil || number < 0 || strconv.FormatInt(number, 10) != value {
				return nil, playback.ErrInvalidRequest
			}
			return number, nil
		case "deviceprofile/containerprofiles/[]/conditions/[]/isrequired",
			"deviceprofile/codecprofiles/[]/conditions/[]/isrequired",
			"deviceprofile/codecprofiles/[]/applyconditions/[]/isrequired",
			"deviceprofile/responseprofiles/[]/conditions/[]/isrequired":
			switch value {
			case "true":
				return true, nil
			case "false":
				return false, nil
			default:
				return nil, playback.ErrInvalidRequest
			}
		}
		return value, nil
	case json.Number:
		if len(value) > 32 {
			return nil, playback.ErrInvalidRequest
		}
		return value, nil
	case bool, nil:
		return value, nil
	default:
		return nil, playback.ErrInvalidRequest
	}
}

func appendPlaybackJSONPath(path []string, key string) []string {
	result := make([]string, len(path)+1)
	copy(result, path)
	result[len(path)] = key
	return result
}

package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/identity"
)

const (
	embyInvalidTokenMessage  = "Access token is invalid or expired."
	embyInvalidLoginMessage  = "Invalid username or password. Please try again."
	embyMissingClientMessage = "Value cannot be null. (Parameter 'appName')"
	embyMissingDeviceMessage = "Value cannot be null. (Parameter 'reportedDeviceId')"
)

// embyTextError preserves the authentication wire contract captured from Emby.
// http.Error cannot be used because it adds a newline and a charset parameter.
func embyTextError(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Length", strconv.Itoa(len(message)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(message))
	}
}

func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func apiError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	if len(r.URL.Path) >= 6 && r.URL.Path[:6] == "/emby/" {
		jsonResponse(w, status, map[string]any{"ResponseStatus": map[string]string{"ErrorCode": code, "Message": message}})
		return
	}
	requestID, _ := r.Context().Value(requestIDKey).(string)
	jsonResponse(w, status, map[string]any{"Error": map[string]string{"Code": code, "Message": message}, "RequestId": requestID})
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONBodyTypes(w, r, dst, false)
}

// decodeEmbyJSONBody accepts the JSON payload media types sent by the reference
// Web Client. Native administrator callers continue to use decodeBody.
func decodeEmbyJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONBodyTypes(w, r, dst, true)
}

func decodeJSONBodyTypes(w http.ResponseWriter, r *http.Request, dst any, allowPlainText bool) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (allowPlainText && len(r.Header.Values("Content-Type")) != 1) ||
		(mediaType != "application/json" && (!allowPlainText || mediaType != "text/plain")) {
		message := "Use application/json for this request."
		if allowPlainText {
			message = "Use application/json or text/plain with a JSON body for this request."
		}
		apiError(w, r, 415, "unsupported_media_type", message)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		apiError(w, r, 400, "invalid_json", "The request must contain a valid JSON object.")
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		apiError(w, r, 400, "invalid_json", "The request must contain exactly one JSON object.")
		return false
	}
	return true
}

func (s *Server) identityError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrInvalidCredentials), errors.Is(err, identity.ErrUnauthorized):
		if strings.HasPrefix(r.URL.Path, "/emby/") {
			embyTextError(w, r, http.StatusUnauthorized, embyInvalidTokenMessage)
			return
		}
		apiError(w, r, 401, "invalid_credentials", "The credentials are invalid or no longer active.")
	case errors.Is(err, identity.ErrAlreadyInitialized):
		apiError(w, r, 409, "already_initialized", "The server has already been initialized.")
	case errors.Is(err, identity.ErrInvalidInput):
		apiError(w, r, 400, "invalid_input", "Check the name and password. The name must be unique and the password must meet the account requirements.")
	case errors.Is(err, identity.ErrNotFound):
		apiError(w, r, 404, "not_found", "The requested user was not found.")
	default:
		s.log.Error("identity operation failed", "request_id", r.Context().Value(requestIDKey))
		apiError(w, r, 500, "internal_error", "The request could not be completed.")
	}
}

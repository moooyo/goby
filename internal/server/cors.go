package server

import (
	"net/http"
	"net/url"
	"strings"
)

const embyAllowHeaders = "Accept, Accept-Language, Authorization, Cache-Control, Content-Disposition, Content-Encoding, Content-Language, Content-Length, Content-MD5, Content-Range, Content-Type, Date, Host, If-Match, If-Modified-Since, If-None-Match, If-Range, If-Unmodified-Since, Origin, OriginToken, Pragma, Range, Slug, Transfer-Encoding, Want-Digest, X-MediaBrowser-Token, X-Emby-Token, X-Emby-Client, X-Emby-Client-Version, X-Emby-Device-Id, X-Emby-Device-Name, X-Emby-Authorization"

// Third-party browser clients use the token-authenticated compatibility API.
// Administrator cookies and CSRF routes remain on the separate same-origin API.
func (s *Server) embyCORS(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/emby" && !strings.HasPrefix(r.URL.Path, "/emby/") {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	} else {
		w.Header().Add("Vary", "Origin")
		if !validOrigin(origin) {
			if r.Method == http.MethodOptions {
				apiError(w, r, 400, "invalid_origin", "The request origin is invalid.")
				return true
			}
			return false
		}
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	if origin != "*" {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	w.Header().Set("Access-Control-Allow-Headers", embyAllowHeaders)
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, DELETE, PATCH, OPTIONS")
	w.Header().Set("Access-Control-Allow-Private-Network", "true")
	if r.Method == http.MethodOptions {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
		return true
	}
	return false
}

func validOrigin(origin string) bool {
	if origin == "null" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

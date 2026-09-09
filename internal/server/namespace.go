package server

import (
	"net/http"
	"net/url"
	"strings"
)

// compatibilityNamespace accepts the API with or without its /emby prefix.
// Only route literals are canonicalized; identifiers and escaped entity names
// retain their spelling and segment boundaries. Administrator paths are never
// translated into the token-authenticated namespace.
func compatibilityNamespace(r *http.Request) *http.Request {
	parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
	if len(parts) == 0 {
		return r
	}
	first, err := url.PathUnescape(parts[0])
	if err != nil {
		return r
	}
	prefixed := strings.EqualFold(first, "emby")
	if prefixed {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return r
	}
	resources := []string{"System", "Users", "Items", "Videos", "Audio", "Sessions", "Library", "Shows", "Genres", "Tags", "Studios", "Persons"}
	resource := ""
	decoded, err := url.PathUnescape(parts[0])
	if err != nil {
		return r
	}
	for _, candidate := range resources {
		if strings.EqualFold(decoded, candidate) {
			resource = candidate
			break
		}
	}
	if resource == "" {
		return r
	}
	parts[0] = resource
	literal := func(index int, choices ...string) {
		if index >= len(parts) {
			return
		}
		value, err := url.PathUnescape(parts[index])
		if err != nil {
			return
		}
		for _, choice := range choices {
			if strings.EqualFold(value, choice) {
				parts[index] = choice
				return
			}
		}
	}
	switch resource {
	case "System":
		literal(1, "Info", "Ping", "Configuration")
		literal(2, "Public")
	case "Users":
		literal(1, "Public", "Query", "New", "AuthenticateByName")
		literal(2, "Items", "Views", "Authenticate", "PlayedItems", "FavoriteItems", "Password", "Policy", "Configuration")
		if len(parts) > 2 && parts[2] == "Items" {
			literal(3, "Root", "Latest", "Resume")
			literal(4, "UserData", "HideFromResume")
		}
		literal(4, "Delete")
	case "Items":
		literal(2, "PlaybackInfo", "Images", "Refresh", "File")
		literal(3, "Subtitles")
	case "Videos", "Audio":
		literal(1, "ActiveEncodings")
		literal(2, "stream")
		literal(3, "Subtitles")
	case "Sessions":
		literal(1, "Playing", "Logout", "Capabilities")
		literal(2, "Playing", "Progress", "Ping", "Stopped", "Full", "Command")
	case "Library":
		literal(1, "VirtualFolders", "Refresh")
		literal(2, "Query", "Delete", "LibraryOptions")
	case "Shows":
		literal(1, "NextUp")
		literal(2, "Seasons", "Episodes")
	}
	escaped := "/emby/" + strings.Join(parts, "/")
	if escaped == r.URL.EscapedPath() {
		return r
	}
	path, err := url.PathUnescape(escaped)
	if err != nil {
		return r
	}
	copy := r.Clone(r.Context())
	copy.URL.Path, copy.URL.RawPath = path, escaped
	return copy
}

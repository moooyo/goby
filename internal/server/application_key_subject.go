package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

// librarySubject keeps the authenticated credential separate from the optional
// user whose catalog state is being projected or explicitly edited.
func librarySubject(principal identity.Principal, userID string) library.Subject {
	subject := library.Subject{UserID: userID}
	if principal.IsApplicationKey() {
		subject.ApplicationCredentialID = principal.SessionID
	}
	return subject
}

// Media URLs already carry PlaySessionId. Without explicit client metadata,
// that correlation can recover a key's client context only after the parent
// credential has authenticated. It never grants access to another key's play.
func (s *Server) bindKeyPlaybackContext(r *http.Request, principal identity.Principal, playID string) (identity.Principal, error) {
	if !principal.IsApplicationKey() || playID == "" {
		return principal, nil
	}
	_, client, err := parseEmbyCredentials(r)
	if err != nil {
		return identity.Principal{}, identity.ErrUnauthorized
	}
	if client != (identity.Client{}) {
		return principal, nil
	}
	bound, err := s.identity.ResolveApplicationKeyPlaybackContext(r.Context(), principal, playID)
	if errors.Is(err, identity.ErrNotFound) {
		// Keep missing/foreign/expired resource behavior at the media handler.
		return principal, nil
	}
	return bound, err
}

func playbackContextHint(r *http.Request) string {
	path := strings.ToLower(r.URL.Path)
	if !strings.HasPrefix(path, "/emby/videos/") && !strings.HasPrefix(path, "/emby/audio/") &&
		!(strings.HasPrefix(path, "/emby/items/") && strings.HasSuffix(path, "/playbackinfo")) {
		return ""
	}
	var hint string
	for name, values := range r.URL.Query() {
		if !strings.EqualFold(name, "PlaySessionId") && !strings.EqualFold(name, "CurrentPlaySessionId") {
			continue
		}
		for _, value := range values {
			if value == "" || (hint != "" && hint != value) {
				return ""
			}
			hint = value
		}
	}
	return hint
}

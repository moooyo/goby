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
	} else {
		subject.Actor = &principal
	}
	return subject
}

type keyPlaybackBindingContextKey struct{}

// keyPlaybackBinding records only successful request-local routing. Reusing it
// still refreshes credential authority after body reads and activity writes
// can wait. Media reads and playback mutations also retain their own checks.
type keyPlaybackBinding struct {
	credentialID string
	keyID        int64
	playID       string
	clientID     string
	client       identity.Client
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
	users, err := s.identity.ForPlaybackControl(r.Context())
	if err != nil {
		return identity.Principal{}, err
	}
	binding, _ := r.Context().Value(keyPlaybackBindingContextKey{}).(*keyPlaybackBinding)
	if binding != nil && binding.credentialID == principal.SessionID && binding.keyID == principal.ApplicationKeyID &&
		binding.playID == playID && binding.clientID == principal.ClientSessionID && binding.client == principal.Client {
		// Refresh authority without repeating the routing transaction. This
		// preserves authentication failures after waits and the trusted peer.
		return users.RevalidateSessionAuthority(r.Context(), principal)
	}
	bound, err := users.ResolveApplicationKeyPlaybackContext(r.Context(), principal, playID)
	if errors.Is(err, identity.ErrNotFound) {
		// Keep missing/foreign/expired resource behavior at the media handler.
		return principal, nil
	}
	if err == nil && binding != nil {
		*binding = keyPlaybackBinding{credentialID: bound.SessionID, keyID: bound.ApplicationKeyID,
			playID: playID, clientID: bound.ClientSessionID, client: bound.Client}
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

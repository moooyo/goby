package server

import "net/http"

// Authentication revocation is already committed before this function runs.
// A successful response also confirms that matching notification attempts have
// stopped. Even when the request is cancelled, fence every revoked credential.
func (s *Server) retireNotificationSessionsForRequest(w http.ResponseWriter, r *http.Request, ids ...string) bool {
	return s.retireNotificationAuthorityForRequest(w, r, "", ids...)
}

// Policy edits may narrow source visibility without revoking login sessions.
// Fence that user's attempts as well as credentials explicitly retired by the
// transaction before acknowledging the new authority to the caller.
func (s *Server) retireNotificationAuthorityForRequest(w http.ResponseWriter, r *http.Request, userID string, ids ...string) bool {
	seen := make(map[string]struct{}, len(ids))
	var firstErr error
	if userID != "" && s.notificationRuntime != nil {
		firstErr = s.notificationRuntime.FenceUser(r.Context(), userID)
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, found := seen[id]; found {
			continue
		}
		seen[id] = struct{}{}
		if err := s.retireNotificationSession(r.Context(), id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		s.notificationError(w, r, firstErr)
		return false
	}
	return true
}

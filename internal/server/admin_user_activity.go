package server

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/moooyo/goby/internal/identity"
)

func parseAdminUserActivityQuery(r *http.Request) ([]string, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(values) != 1 || len(values["UserIds"]) != 1 {
		return nil, identity.ErrInvalidInput
	}
	ids := strings.Split(values.Get("UserIds"), ",")
	if err := identity.ValidateUserActivityIDs(ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *Server) adminUserActivity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	ids, err := parseAdminUserActivityQuery(r)
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply between 1 and 200 distinct user IDs in UserIds.")
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	page, err := s.identity.ListUserActivity(r.Context(), actor, ids)
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, page)
}

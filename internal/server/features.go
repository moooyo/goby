package server

import (
	"net/http"
	"strings"

	"github.com/moooyo/goby/internal/identity"
)

func (s *Server) registerFeatureRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Features", s.requireEmby(s.embyFeatures))
	mux.HandleFunc("GET /admin/v1/features", s.requireAdmin(s.adminFeatures))
}

func (s *Server) embyFeatures(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value(principalKey).(identity.Principal)
	if !actor.CanManageServer() {
		apiError(w, r, http.StatusForbidden, "administrator_required", "Administrator access is required.")
		return
	}
	values, err := streamValues(r)
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Check the feature query.")
		return
	}
	features := identity.UserFeatures()
	switch strings.ToLower(values["featuretype"]) {
	case "", "user":
	case "system":
		features = []identity.FeatureInfo{}
	default:
		apiError(w, r, http.StatusBadRequest, "invalid_input", "FeatureType must be User or System.")
		return
	}
	jsonResponse(w, http.StatusOK, features)
}

func (s *Server) adminFeatures(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "This operation does not accept query parameters.")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": identity.UserFeatures()})
}

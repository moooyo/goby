package server

import "net/http"

// featureRegistrationInfo is the server-local RegistrationInfo contract. It is
// a license declaration, not a capability list or a user authorization result.
type featureRegistrationInfo struct {
	Name           string
	ExpirationDate string
	IsTrial        bool
	IsRegistered   bool
}

func (s *Server) registerFeatureLicensingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Registrations/{Feature}", s.requireEmby(s.embyFeatureRegistration))
}

func (s *Server) embyFeatureRegistration(w http.ResponseWriter, r *http.Request) {
	// Goby does not sell feature licenses. Every feature name has no commercial
	// license restriction; unsupported operations and all access controls remain
	// governed by their own routes and policies. Goby uses the maximum supported
	// calendar year as a non-expiring sentinel, never as a trial deadline.
	// This response does not assert an entitlement on any third-party service.
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, featureRegistrationInfo{
		Name: r.PathValue("Feature"), ExpirationDate: "9999-12-31T23:59:59Z",
		IsTrial: false, IsRegistered: true,
	})
}

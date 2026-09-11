package server

import "net/http"

// The consumer client reads branding before authentication. Goby currently has
// no configured login disclaimer or custom stylesheet, so these projections
// describe the actual empty configuration. They do not enable branding writes.
func (s *Server) registerBrandingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Branding/Configuration", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, struct{}{})
	})
	css := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	}
	mux.HandleFunc("GET /emby/Branding/Css", css)
	mux.HandleFunc("GET /emby/Branding/Css.css", css)
}

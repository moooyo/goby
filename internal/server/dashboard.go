package server

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		apiError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET or HEAD to request the dashboard.")
		return
	}
	assets := os.DirFS(s.cfg.WebDirectory)
	name := strings.TrimPrefix(r.URL.Path, "/admin/")
	if name == "" {
		name = "index.html"
	}
	if !fs.ValidPath(name) || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}
	info, err := fs.Stat(assets, name)
	if err != nil || info.IsDir() {
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
	}
	if _, err := fs.Stat(assets, name); err != nil {
		apiError(w, r, 503, "dashboard_unavailable", "The administrator dashboard is unavailable.")
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFileFS(w, r, assets, name)
}

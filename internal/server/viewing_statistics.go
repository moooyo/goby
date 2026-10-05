package server

import "net/http"

func (s *Server) registerViewingStatisticsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Users/{UserId}/ViewingStatistics", s.requireEmby(s.embyViewingStatistics))
}

func (s *Server) embyViewingStatistics(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	statistics, err := s.library.ViewingStatisticsFor(r.Context(), requestLibrarySubject(r, userID))
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	jsonResponse(w, http.StatusOK, statistics)
}

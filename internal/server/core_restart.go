package server

import (
	"net/http"
	"time"
)

func (s *Server) registerCoreRestartRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /emby/System/Restart", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		s.requireEmby(s.embyRestart)(w, r)
	})
}

func (s *Server) embyRestart(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.configurationAccess(w, r, true, "RestartServer"); !ok || !configurationQuery(w, r) {
		return
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "This operation does not accept a request body.")
		return
	}
	controller := s.restartController
	if controller == nil || controller.request == nil {
		apiError(w, r, http.StatusConflict, "restart_unavailable", "This deployment has not enabled a managed restart.")
		return
	}
	if !controller.pending.CompareAndSwap(false, true) {
		apiError(w, r, http.StatusConflict, "restart_pending", "A restart has already been requested. Reconnect after it completes.")
		return
	}
	// The core contract acknowledges acceptance without inventing a response DTO.
	// Both API audiences share one request latch and the same generation owner.
	w.WriteHeader(http.StatusOK)
	_ = http.NewResponseController(w).Flush()
	time.AfterFunc(250*time.Millisecond, controller.request)
}

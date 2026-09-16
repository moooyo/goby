package server

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
)

func (s *Server) registerAdminMediaDiagnosticRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/media-diagnostics", s.requireAdmin(s.adminMediaDiagnostics))
	mux.HandleFunc("POST /admin/v1/media-diagnostics/runs", s.requireAdmin(s.startAdminMediaDiagnostic))
	mux.HandleFunc("GET /admin/v1/media-diagnostics/runs/{instance}/{id}", s.requireAdmin(s.adminMediaDiagnosticRun))
	mux.HandleFunc("POST /admin/v1/media-diagnostics/runs/{instance}/{id}/cancel", s.requireAdmin(s.cancelAdminMediaDiagnostic))
}

func diagnosticHexID(value string) bool {
	if len(value) != 32 {
		return false
	}
	data, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(data) == value
}

func (s *Server) mediaDiagnosticError(w http.ResponseWriter, r *http.Request, err error) {
	var problem *mediaDiagnosticAPIError
	if errors.As(err, &problem) {
		apiError(w, r, problem.status, problem.code, problem.message)
		return
	}
	s.identityError(w, r, err)
}

func mediaDiagnosticRequest(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		apiError(w, r, 400, "invalid_input", "This operation does not accept query parameters.")
		return false
	}
	return true
}

func mediaDiagnosticBody(w http.ResponseWriter, r *http.Request, fields []string) (map[string]json.RawMessage, bool) {
	if !mediaDiagnosticRequest(w, r) {
		return nil, false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if len(r.Header.Values("Content-Type")) != 1 || err != nil || mediaType != "application/json" {
		apiError(w, r, 415, "unsupported_media_type", "Use application/json for this request.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	data, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(data) {
		apiError(w, r, 400, "invalid_input", "Supply a UTF-8 JSON object no larger than 4 KiB.")
		return nil, false
	}
	values, invalid := adminTaskObject(data, fields, fields, "")
	if invalid != nil {
		apiError(w, r, 400, "invalid_input", "Supply exactly the supported diagnostic request fields, without duplicates.")
		return nil, false
	}
	return values, true
}

func (s *Server) adminMediaDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !mediaDiagnosticRequest(w, r) {
		return
	}
	if s.mediaDiagnostics == nil {
		s.mediaDiagnosticError(w, r, errMediaDiagnosticUnavailable)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	jsonResponse(w, 200, s.mediaDiagnostics.status(actor))
}

func (s *Server) startAdminMediaDiagnostic(w http.ResponseWriter, r *http.Request) {
	values, ok := mediaDiagnosticBody(w, r, []string{"InstanceId", "RequestId", "StartToken", "Mode"})
	if !ok {
		return
	}
	var request mediaDiagnosticStart
	for field, target := range map[string]*string{"InstanceId": &request.InstanceId, "RequestId": &request.RequestId, "StartToken": &request.StartToken, "Mode": &request.Mode} {
		if string(values[field]) == "null" || json.Unmarshal(values[field], target) != nil || len(*target) > 128 {
			apiError(w, r, 400, "invalid_input", "Supply bounded string diagnostic request fields.")
			return
		}
	}
	if !diagnosticHexID(request.InstanceId) || !diagnosticHexID(request.RequestId) || request.StartToken == "" || request.Mode != "software" && request.Mode != "configured" {
		apiError(w, r, 400, "invalid_input", "Supply valid instance and request identifiers and a software or configured mode.")
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	run, admitted, err := s.mediaDiagnostics.start(r.Context(), actor, request)
	if err != nil {
		s.mediaDiagnosticError(w, r, err)
		return
	}
	status := http.StatusOK
	if admitted {
		status = http.StatusAccepted
	}
	jsonResponse(w, status, map[string]any{"Run": run})
}

func (s *Server) adminMediaDiagnosticRun(w http.ResponseWriter, r *http.Request) {
	if !mediaDiagnosticRequest(w, r) {
		return
	}
	s.respondMediaDiagnosticRun(w, r, false)
}

func (s *Server) cancelAdminMediaDiagnostic(w http.ResponseWriter, r *http.Request) {
	if _, ok := mediaDiagnosticBody(w, r, []string{}); !ok {
		return
	}
	// Recheck native authority immediately before this asynchronous mutation.
	if err := s.checkMediaDiagnosticActor(r.Context(), r.Context().Value(principalKey).(identity.Principal)); err != nil {
		s.mediaDiagnosticError(w, r, err)
		return
	}
	s.respondMediaDiagnosticRun(w, r, true)
}

func (s *Server) respondMediaDiagnosticRun(w http.ResponseWriter, r *http.Request, cancel bool) {
	instance, id := r.PathValue("instance"), r.PathValue("id")
	if !diagnosticHexID(instance) || !diagnosticHexID(id) {
		apiError(w, r, 400, "invalid_input", "Supply valid diagnostic instance and request identifiers.")
		return
	}
	run, err := s.mediaDiagnostics.get(instance, id, cancel)
	if err != nil {
		s.mediaDiagnosticError(w, r, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"Run": run})
}

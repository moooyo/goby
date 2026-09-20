package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func (s *Server) registerAdminMediaOperationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/media-operations/capabilities", s.requireAdmin(s.adminMediaOperationCapabilities))
	mux.HandleFunc("GET /admin/v1/items/{id}/media-processing", s.requireAdmin(s.adminMediaProcessingTarget))
	mux.HandleFunc("POST /admin/v1/items/{id}/media-operations", s.requireAdmin(s.startAdminMediaOperation))
	mux.HandleFunc("GET /admin/v1/media-operations", s.requireAdmin(s.adminMediaOperations))
	mux.HandleFunc("GET /admin/v1/media-operations/{id}", s.requireAdmin(s.adminMediaOperation))
	mux.HandleFunc("GET /admin/v1/media-operations/{id}/review", s.requireAdmin(s.adminMediaOperationReview))
	mux.HandleFunc("PUT /admin/v1/media-operations/{id}/review", s.requireAdmin(s.updateAdminMediaOperationReview))
	mux.HandleFunc("GET /admin/v1/media-operations/{id}/cues/{ordinal}/image", s.requireAdmin(s.adminMediaOperationCueImage))
	mux.HandleFunc("POST /admin/v1/media-operations/{id}/apply", s.requireAdmin(s.applyAdminMediaOperation))
	mux.HandleFunc("POST /admin/v1/media-operations/{id}/cancel", s.requireAdmin(s.cancelAdminMediaOperation))
	mux.HandleFunc("POST /admin/v1/media-operations/{id}/recover", s.requireAdmin(s.recoverAdminMediaOperation))
}

func adminMediaOperationActor(r *http.Request) identity.Principal {
	return r.Context().Value(principalKey).(identity.Principal)
}

func adminMediaOperationResponse(w http.ResponseWriter, r *http.Request, status int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		apiError(w, r, http.StatusInternalServerError, "internal_error", "The media operation response could not be encoded.")
		return
	}
	if len(encoded) > maxAdminMediaOperationResponseBytes {
		apiError(w, r, http.StatusUnprocessableEntity, "response_limit", "The result exceeds the response budget. Request a smaller page.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}

func (s *Server) adminMediaOperationCapabilities(w http.ResponseWriter, r *http.Request) {
	if !adminMediaOperationNoQuery(w, r) {
		return
	}
	if s.mediaOperations == nil {
		s.mediaOperationError(w, r, library.ErrUnavailable)
		return
	}
	capabilities, err := s.mediaOperations.Capabilities(r.Context(), adminMediaOperationActor(r))
	if err != nil {
		s.mediaOperationError(w, r, err)
		return
	}
	adminMediaOperationResponse(w, r, http.StatusOK, capabilities)
}

func (s *Server) adminMediaProcessingTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMediaOperationID(w, r)
	if !ok || !adminMediaOperationNoQuery(w, r) {
		return
	}
	if s.mediaOperations == nil {
		s.mediaOperationError(w, r, library.ErrUnavailable)
		return
	}
	actor := adminMediaOperationActor(r)
	target, err := s.library.GetMediaOperationTarget(r.Context(), actor, id)
	if err != nil {
		s.mediaOperationError(w, r, err)
		return
	}
	capabilities, err := s.mediaOperations.Capabilities(r.Context(), actor)
	if err != nil {
		s.mediaOperationError(w, r, err)
		return
	}
	adminMediaOperationResponse(w, r, http.StatusOK, mediaProcessingTargetDTO(target, capabilities))
}

func (s *Server) startAdminMediaOperation(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMediaOperationID(w, r)
	if !ok {
		return
	}
	input, ok := decodeAdminMediaOperationStart(w, r, id)
	if !ok {
		return
	}
	if s.mediaOperations == nil {
		s.mediaOperationError(w, r, library.ErrUnavailable)
		return
	}
	result, err := s.mediaOperations.Start(r.Context(), adminMediaOperationActor(r), input)
	if err != nil {
		s.mediaOperationError(w, r, err)
		return
	}
	adminMediaOperationResponse(w, r, http.StatusAccepted, map[string]any{"Operation": mediaOperationDTO(result.Operation), "Admitted": result.Admitted})
}

func (s *Server) adminMediaOperations(w http.ResponseWriter, r *http.Request) {
	page, ok := adminMediaOperationPage(w, r, true)
	if !ok {
		return
	}
	result, err := s.library.ListMediaOperations(r.Context(), adminMediaOperationActor(r), page)
	if err != nil {
		s.mediaOperationError(w, r, err)
		return
	}
	items := make([]adminMediaOperationDTO, 0, len(result.Items))
	for _, operation := range result.Items {
		items = append(items, mediaOperationDTO(operation))
	}
	adminMediaOperationResponse(w, r, http.StatusOK, map[string]any{
		"Items": items, "TotalRecordCount": result.TotalRecordCount, "StartIndex": result.StartIndex, "Limit": result.Limit,
	})
}

func (s *Server) adminMediaOperation(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMediaOperationID(w, r)
	if !ok || !adminMediaOperationNoQuery(w, r) {
		return
	}
	operation, err := s.library.GetMediaOperation(r.Context(), adminMediaOperationActor(r), id)
	if err != nil {
		s.mediaOperationError(w, r, err)
		return
	}
	adminMediaOperationResponse(w, r, http.StatusOK, map[string]any{"Operation": mediaOperationDTO(operation)})
}

func (s *Server) adminMediaOperationReview(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMediaOperationID(w, r)
	if !ok {
		return
	}
	page, ok := adminMediaOperationPage(w, r, false)
	if !ok {
		return
	}
	result, err := s.library.GetMediaOperationReview(r.Context(), adminMediaOperationActor(r), id, page)
	if err != nil {
		s.mediaOperationError(w, r, err)
		return
	}
	if result.Items == nil {
		result.Items = []library.MediaOperationCue{}
	}
	adminMediaOperationResponse(w, r, http.StatusOK, map[string]any{
		"Operation": mediaOperationDTO(result.Operation), "Items": result.Items,
		"TotalRecordCount": result.TotalRecordCount, "StartIndex": result.StartIndex, "Limit": result.Limit,
	})
}

func (s *Server) updateAdminMediaOperationReview(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMediaOperationID(w, r)
	if !ok {
		return
	}
	revision, edits, ok := decodeAdminMediaOperationReview(w, r)
	if !ok {
		return
	}
	operation, err := s.library.UpdateMediaOperationReview(r.Context(), adminMediaOperationActor(r), id, revision, edits)
	if err != nil {
		s.mediaOperationError(w, r, err)
		return
	}
	adminMediaOperationResponse(w, r, http.StatusOK, map[string]any{"Operation": mediaOperationDTO(operation)})
}

func (s *Server) adminMediaOperationCueImage(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMediaOperationID(w, r)
	if !ok || !adminMediaOperationNoQuery(w, r) {
		return
	}
	ordinalText := r.PathValue("ordinal")
	ordinal, err := strconv.ParseInt(ordinalText, 10, 32)
	if err != nil || ordinal < 0 || ordinal >= library.MaxMediaOperationCues || strconv.FormatInt(ordinal, 10) != ordinalText {
		adminMediaOperationInputError(w, r, map[string]string{"Ordinal": "Supply a canonical nonnegative cue ordinal."})
		return
	}
	data, err := s.library.GetMediaOperationCueImage(r.Context(), adminMediaOperationActor(r), id, int(ordinal))
	if err != nil {
		s.mediaOperationError(w, r, err)
		return
	}
	if len(data) == 0 || len(data) > media.MaxSubtitleOCRCueImageBytes || http.DetectContentType(data) != "image/png" {
		s.mediaOperationError(w, r, library.ErrUnavailable)
		return
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", `"`+digest+`"`)
	// Re-read and authorize the cue before considering a conditional response.
	if r.Header.Get("If-None-Match") == w.Header().Get("ETag") {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) applyAdminMediaOperation(w http.ResponseWriter, r *http.Request) {
	s.admitAdminMediaOperationApply(w, r, false)
}

func (s *Server) recoverAdminMediaOperation(w http.ResponseWriter, r *http.Request) {
	s.admitAdminMediaOperationApply(w, r, true)
}

func (s *Server) admitAdminMediaOperationApply(w http.ResponseWriter, r *http.Request, recover bool) {
	id, ok := adminMediaOperationID(w, r)
	if !ok {
		return
	}
	input, ok := decodeAdminMediaOperationApply(w, r)
	if !ok {
		return
	}
	if s.mediaOperations == nil {
		s.mediaOperationError(w, r, library.ErrUnavailable)
		return
	}
	var result library.MediaOperationAdmission
	var err error
	if recover {
		result, err = s.mediaOperations.Recover(r.Context(), adminMediaOperationActor(r), id, input)
	} else {
		result, err = s.mediaOperations.Apply(r.Context(), adminMediaOperationActor(r), id, input)
	}
	if err != nil {
		s.mediaOperationError(w, r, err)
		return
	}
	adminMediaOperationResponse(w, r, http.StatusAccepted, map[string]any{"Operation": mediaOperationDTO(result.Operation), "Admitted": result.Admitted})
}

func (s *Server) cancelAdminMediaOperation(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMediaOperationID(w, r)
	if !ok {
		return
	}
	revision, ok := decodeAdminMediaOperationCancel(w, r)
	if !ok {
		return
	}
	if s.mediaOperations == nil {
		s.mediaOperationError(w, r, library.ErrUnavailable)
		return
	}
	operation, err := s.mediaOperations.Cancel(r.Context(), adminMediaOperationActor(r), id, revision)
	if err != nil {
		s.mediaOperationError(w, r, err)
		return
	}
	adminMediaOperationResponse(w, r, http.StatusAccepted, map[string]any{"Operation": mediaOperationDTO(operation)})
}

func (s *Server) mediaOperationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials):
		s.identityError(w, r, err)
	case errors.Is(err, identity.ErrClientSessionForbidden), errors.Is(err, library.ErrForbidden):
		apiError(w, r, http.StatusForbidden, "administrator_required", "A current native administrator session is required.")
	case errors.Is(err, library.ErrMediaOperationConflict):
		apiError(w, r, http.StatusConflict, "media_operation_conflict", "The operation or request changed. Reload its current revision before retrying.")
	case errors.Is(err, library.ErrSourceChanged):
		apiError(w, r, http.StatusConflict, "source_changed", "The indexed media source changed. Reload its current source revision.")
	case errors.Is(err, library.ErrMediaOperationRecovery):
		apiError(w, r, http.StatusConflict, "recovery_required", "This operation requires an explicit recovery request.")
	case errors.Is(err, library.ErrMediaOperationState):
		apiError(w, r, http.StatusConflict, "media_operation_state", "The operation state does not allow this action.")
	case errors.Is(err, library.ErrBusy):
		apiError(w, r, http.StatusConflict, "media_operation_busy", "The item is busy or the media operation queue is full.")
	case errors.Is(err, library.ErrInvalidInput):
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Check the media operation identifiers, stream and parameters.")
	case errors.Is(err, library.ErrNotFound):
		apiError(w, r, http.StatusNotFound, "not_found", "The media operation resource was not found.")
	case errors.Is(err, library.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		apiError(w, r, http.StatusServiceUnavailable, "media_operation_unavailable", "Media processing is currently unavailable.")
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		return
	default:
		if s.log != nil {
			s.log.Error("media operation request failed", "request_id", r.Context().Value(requestIDKey))
		}
		apiError(w, r, http.StatusInternalServerError, "internal_error", "The media operation could not be completed.")
	}
}

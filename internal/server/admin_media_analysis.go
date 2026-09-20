package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/notificationjournal"
	"github.com/moooyo/goby/internal/tasks"
)

func (s *Server) registerAdminMediaAnalysisRoutes(mux *http.ServeMux) {
	wrap := func(next http.HandlerFunc) http.HandlerFunc {
		authorized := s.requireAdmin(next)
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			authorized(w, r)
		}
	}
	mux.HandleFunc("GET /admin/v1/media-analysis", wrap(s.adminMediaAnalysis))
	mux.HandleFunc("PUT /admin/v1/media-analysis/configuration", wrap(s.updateAdminMediaAnalysisConfiguration))
	mux.HandleFunc("GET /admin/v1/media-analysis/items", wrap(s.adminMediaAnalysisItems))
	mux.HandleFunc("GET /admin/v1/media-analysis/items/{id}", wrap(s.adminMediaAnalysisItem))
	mux.HandleFunc("POST /admin/v1/media-analysis/runs", wrap(s.startAdminMediaAnalysisRun))
	mux.HandleFunc("POST /admin/v1/media-analysis/items/{id}/decision", wrap(s.decideAdminMediaAnalysisIntro))
	mux.HandleFunc("POST /admin/v1/media-analysis/cache/prune", wrap(s.pruneAdminMediaAnalysisCache))
}

func adminMediaAnalysisActor(r *http.Request) identity.Principal {
	actor, _ := r.Context().Value(principalKey).(identity.Principal)
	return actor
}

// A runtime status snapshot has no SQL transaction of its own. Revalidate its
// reader after composing the configuration and runtime response. Writes retain
// their domain repository's own transactional authorization and final fence.
func (s *Server) checkAdminMediaAnalysisActor(ctx context.Context, actor identity.Principal) error {
	if s.db == nil {
		return library.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return library.ErrUnavailable
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer done()
		_ = tx.Rollback(cleanup)
	}()
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return library.ErrUnavailable
	}
	return nil
}

func (s *Server) adminMediaAnalysis(w http.ResponseWriter, r *http.Request) {
	if !adminMediaAnalysisNoQuery(w, r) {
		return
	}
	if s.library == nil {
		s.mediaAnalysisError(w, r, library.ErrUnavailable)
		return
	}
	actor := adminMediaAnalysisActor(r)
	configuration, err := s.library.GetAnalysisConfiguration(r.Context(), actor)
	if err != nil {
		s.mediaAnalysisError(w, r, err)
		return
	}
	runtime := adminMediaAnalysisRuntimeStatus{Reasons: []string{"not_configured"}}
	if s.mediaAnalysis != nil {
		runtime = s.mediaAnalysis.Status()
	}
	if err := s.checkAdminMediaAnalysisActor(r.Context(), actor); err != nil {
		s.mediaAnalysisError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Configuration": adminMediaAnalysisConfigurationDTO(configuration), "Runtime": adminMediaAnalysisRuntimeDTO(runtime)})
}

func (s *Server) updateAdminMediaAnalysisConfiguration(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeAdminMediaAnalysisConfiguration(w, r)
	if !ok {
		return
	}
	if s.library == nil {
		s.mediaAnalysisError(w, r, library.ErrUnavailable)
		return
	}
	configuration, err := s.library.UpdateAnalysisConfiguration(r.Context(), adminMediaAnalysisActor(r), input)
	if err != nil {
		s.mediaAnalysisError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, adminMediaAnalysisConfigurationDTO(configuration))
}

func (s *Server) adminMediaAnalysisItems(w http.ResponseWriter, r *http.Request) {
	input, ok := adminMediaAnalysisQuery(w, r)
	if !ok {
		return
	}
	if s.library == nil {
		s.mediaAnalysisError(w, r, library.ErrUnavailable)
		return
	}
	page, err := s.library.ListAnalysisItems(r.Context(), adminMediaAnalysisActor(r), input)
	if err != nil {
		s.mediaAnalysisError(w, r, err)
		return
	}
	items := make([]adminMediaAnalysisItem, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, s.residentMediaAnalysisItemDTO(item))
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": page.TotalRecordCount, "StartIndex": input.StartIndex, "Limit": input.Limit})
}

func (s *Server) adminMediaAnalysisItem(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMediaAnalysisID(w, r)
	if !ok || !adminMediaAnalysisNoQuery(w, r) {
		return
	}
	if s.library == nil {
		s.mediaAnalysisError(w, r, library.ErrUnavailable)
		return
	}
	item, err := s.library.GetAnalysisItem(r.Context(), adminMediaAnalysisActor(r), id)
	if err != nil {
		s.mediaAnalysisError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, s.residentMediaAnalysisItemDTO(item))
}

func (s *Server) startAdminMediaAnalysisRun(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeAdminMediaAnalysisRun(w, r)
	if !ok {
		return
	}
	if s.taskStore == nil || s.taskManager == nil {
		s.taskError(w, r, tasks.ErrUnavailable)
		return
	}
	definition, err := s.taskStore.GetByKey(r.Context(), input.TaskKey)
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	result, err := s.taskManager.Start(r.Context(), adminTaskActor(r), tasks.StartRequest{
		TaskID: definition.ID, RequestID: input.RequestID, AnalysisInput: &input.Selection,
	})
	if err != nil {
		if errors.Is(err, library.ErrInvalidInput) || errors.Is(err, library.ErrNotFound) || errors.Is(err, library.ErrForbidden) ||
			errors.Is(err, library.ErrAnalysisConflict) || errors.Is(err, library.ErrAnalysisSourceChanged) || errors.Is(err, library.ErrAnalysisSuppressed) {
			s.mediaAnalysisError(w, r, err)
		} else {
			s.taskError(w, r, err)
		}
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]any{"RunId": result.Run.ID, "TaskId": result.Run.TaskID, "Admitted": result.Admitted})
}

func (s *Server) decideAdminMediaAnalysisIntro(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMediaAnalysisID(w, r)
	if !ok {
		return
	}
	input, ok := decodeAdminMediaAnalysisDecision(w, r)
	if !ok {
		return
	}
	if s.library == nil {
		s.mediaAnalysisError(w, r, library.ErrUnavailable)
		return
	}
	result, err := s.library.DecideAnalysisIntro(r.Context(), adminMediaAnalysisActor(r), id, input)
	if err != nil {
		s.mediaAnalysisError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, adminMediaAnalysisDetectionDTO(result))
}

func (s *Server) pruneAdminMediaAnalysisCache(w http.ResponseWriter, r *http.Request) {
	revision, ok := decodeAdminMediaAnalysisPrune(w, r)
	if !ok {
		return
	}
	if s.mediaAnalysis == nil {
		s.mediaAnalysisError(w, r, library.ErrUnavailable)
		return
	}
	result, err := s.mediaAnalysis.Prune(r.Context(), adminMediaAnalysisActor(r), revision)
	if err != nil {
		s.mediaAnalysisError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) mediaAnalysisError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials):
		s.identityError(w, r, err)
	case errors.Is(err, identity.ErrClientSessionForbidden), errors.Is(err, library.ErrForbidden):
		apiError(w, r, http.StatusForbidden, "administrator_required", "A current native administrator session is required.")
	case errors.Is(err, library.ErrAnalysisConflict):
		apiError(w, r, http.StatusConflict, "analysis_conflict", "The configuration or decision changed. Reload its current revision before retrying.")
	case errors.Is(err, library.ErrAnalysisSourceChanged), errors.Is(err, library.ErrSourceChanged):
		apiError(w, r, http.StatusConflict, "source_changed", "The indexed media source or supporting cohort changed. Reload the item before retrying.")
	case errors.Is(err, library.ErrAnalysisSuppressed):
		apiError(w, r, http.StatusConflict, "analysis_suppressed", "The current decision prevents this analysis result from being published.")
	case errors.Is(err, library.ErrInvalidInput), errors.Is(err, identity.ErrInvalidInput):
		adminMediaAnalysisInputError(w, r, map[string]string{"Body": "Supply supported media analysis identifiers, revisions and values."})
	case errors.Is(err, library.ErrNotFound):
		apiError(w, r, http.StatusNotFound, "not_found", "The media analysis resource was not found.")
	case errors.Is(err, notificationjournal.ErrCapacity), errors.Is(err, notificationjournal.ErrJournal), errors.Is(err, library.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		apiError(w, r, http.StatusServiceUnavailable, "analysis_unavailable", "Media analysis is currently unavailable.")
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		return
	default:
		if s.log != nil {
			s.log.Error("media analysis request failed", "request_id", r.Context().Value(requestIDKey))
		}
		apiError(w, r, http.StatusInternalServerError, "internal_error", "The media analysis operation could not be completed.")
	}
}

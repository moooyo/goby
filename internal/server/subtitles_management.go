package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) deleteSubtitle(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.ParseInt(r.PathValue("Index"), 10, 32)
	if err != nil || index < 0 {
		apiError(w, r, http.StatusBadRequest, "invalid_subtitle_request", "Supply a nonnegative subtitle stream index in the request path.")
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.library.DeleteSubtitleAsUser(ctx, actor, r.PathValue("Id"), int(index)); err != nil {
		switch {
		case errors.Is(err, library.ErrEmbeddedSubtitle):
			apiError(w, r, http.StatusBadRequest, "embedded_subtitle_read_only", "An embedded subtitle cannot be deleted from its media container.")
		case errors.Is(err, library.ErrMediaDeletionRecovery):
			apiError(w, r, http.StatusServiceUnavailable, "subtitle_deletion_pending", "The subtitle deletion requires a recovery retry after its storage is available.")
		case errors.Is(err, library.ErrSourceChanged):
			apiError(w, r, http.StatusConflict, "subtitle_source_changed", "The subtitle source changed. Refresh the item before deleting its subtitle.")
		case errors.Is(err, library.ErrBusy):
			w.Header().Set("Retry-After", "2")
			apiError(w, r, http.StatusConflict, "subtitle_library_busy", "The library is busy. Retry subtitle deletion after its current operation finishes.")
		default:
			s.playbackError(w, r, err)
		}
		return
	}
	// Retire every affected immutable conversion immediately. The catalog's
	// committed change notification separately refreshes authorized item DTOs.
	s.retireDeletedSubtitle(r.PathValue("Id"), int(index))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) retireDeletedSubtitle(itemID string, index int) {
	if s.hls == nil {
		return
	}
	s.hls.mu.Lock()
	var affected []*hlsSession
	for _, session := range s.hls.sessions {
		if session.key.scope.ItemID == itemID && session.key.plan.Subtitle.Mode != "" && session.key.plan.Subtitle.StreamIndex == index {
			affected = append(affected, session)
		}
	}
	s.hls.mu.Unlock()
	for _, session := range affected {
		s.hls.retire(session)
	}
}

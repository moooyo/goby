package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerMediaManagementRoutes(mux *http.ServeMux) {
	mux.HandleFunc("DELETE /emby/Items/{Id}", s.requireEmby(s.deleteMediaItem))
	mux.HandleFunc("POST /emby/Items/{Id}/Delete", s.requireEmby(s.deleteMediaItem))
	mux.HandleFunc("GET /emby/Items/{Id}/DeleteInfo", s.requireEmby(s.mediaDeleteInfo))
}

func mediaManagementRequest(w http.ResponseWriter, r *http.Request) bool {
	query, err := embyBusinessQuery(r)
	if err != nil || len(query) != 0 {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "This operation accepts only the item identifier and authentication parameters.")
		return false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil || len(strings.TrimSpace(string(data))) != 0 {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "This operation does not accept a request body.")
		return false
	}
	return true
}

// One dispatcher owns the shared Item deletion routes. Virtual collections
// remove their own records; physical media uses the recoverable file workflow.
func (s *Server) deleteMediaItem(w http.ResponseWriter, r *http.Request) {
	if !mediaManagementRequest(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	id := r.PathValue("Id")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	item, err := s.library.GetItemFor(ctx, librarySubject(actor, actor.User.ID), id)
	if err == nil && (item.Type == library.PlaylistKind || item.Type == library.BoxSetKind) {
		err = s.library.DeleteCollection(library.WithCollectionActor(ctx, actor), librarySubject(actor, actor.User.ID), id, item.Type)
	} else if err == nil || errors.Is(err, library.ErrNotFound) {
		// A missing current item may have a committed journal that still owns a
		// staged file. The store authorizes recovery independently and never
		// interprets an arbitrary missing identifier as deletion success.
		err = s.library.DeleteMediaFor(ctx, actor, id)
	}
	if err != nil {
		if errors.Is(err, library.ErrMediaDeletionRecovery) {
			s.retireDeletedMedia(id)
		}
		s.mediaManagementError(w, r, err)
		return
	}
	s.retireDeletedMedia(id)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) mediaDeleteInfo(w http.ResponseWriter, r *http.Request) {
	if !mediaManagementRequest(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	id := r.PathValue("Id")
	item, err := s.library.GetItemFor(r.Context(), librarySubject(actor, actor.User.ID), id)
	if err != nil {
		s.mediaManagementError(w, r, err)
		return
	}
	if item.Type == library.PlaylistKind || item.Type == library.BoxSetKind {
		jsonResponse(w, http.StatusOK, map[string]any{"Paths": []string{}})
		return
	}
	paths, err := s.library.MediaDeletionInfo(r.Context(), actor, id)
	if err != nil {
		s.mediaManagementError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, map[string]any{"Paths": paths})
}

func (s *Server) mediaManagementError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, library.ErrMediaDeletionRecovery):
		apiError(w, r, http.StatusServiceUnavailable, "media_deletion_pending", "Deletion is pending recovery. Retry with an authorized session on the original storage host.")
	case errors.Is(err, library.ErrSourceChanged):
		apiError(w, r, http.StatusConflict, "media_source_changed", "The source changed or an interrupted deletion was restored. Rescan the library before retrying.")
	case errors.Is(err, library.ErrBusy):
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusConflict, "library_busy", "A library operation is already in progress. Retry after it finishes.")
	default:
		s.libraryError(w, r, err)
	}
}

func (s *Server) retireDeletedMedia(itemID string) {
	if s.hls == nil {
		return
	}
	s.hls.mu.Lock()
	var affected []*hlsSession
	for _, session := range s.hls.sessions {
		if session.key.scope.ItemID == itemID {
			affected = append(affected, session)
		}
	}
	s.hls.mu.Unlock()
	for _, session := range affected {
		s.hls.retire(session)
	}
}

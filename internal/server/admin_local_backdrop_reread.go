package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) adminRereadLocalBackdrop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, ok := adminMetadataID(w, r)
	if !ok || !adminEmptyMutation(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.library.RereadLocalBackdrop(r.Context(), actor, id)
	switch {
	case err == nil:
		jsonResponse(w, http.StatusOK, result)
	case errors.Is(err, library.ErrForbidden):
		apiError(w, r, http.StatusForbidden, "access_denied", "The backdrop reread is not permitted for this session.")
	case errors.Is(err, library.ErrUnavailable), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		apiError(w, r, http.StatusServiceUnavailable, "local_backdrop_unavailable", "Local backdrop reread is currently unavailable. Reload the artwork and check pending work before retrying.")
	case errors.Is(err, library.ErrBusy):
		apiError(w, r, http.StatusConflict, "local_backdrop_busy", "The library or catalog writer is not ready for a local backdrop reread.")
	case errors.Is(err, library.ErrSourceChanged):
		apiError(w, r, http.StatusConflict, "local_backdrop_source_changed", "The current source could not be verified against the indexed series and scan.")
	case errors.Is(err, library.ErrLocalBackdropDisabled):
		apiError(w, r, http.StatusConflict, "local_backdrop_disabled", "Local artwork reading is disabled for this library.")
	case errors.Is(err, library.ErrLocalBackdropNotPending):
		apiError(w, r, http.StatusConflict, "local_backdrop_not_pending", "This series has no eligible image-only pending import.")
	case errors.Is(err, library.ErrLocalBackdropOtherImages):
		apiError(w, r, http.StatusConflict, "local_backdrop_other_images", "Other selected local artwork does not match the accepted artwork.")
	case errors.Is(err, library.ErrLocalBackdropShadowed):
		apiError(w, r, http.StatusConflict, "local_backdrop_shadowed", "Managed or provider backdrop artwork is already present.")
	case errors.Is(err, library.ErrLocalBackdropOnlineGap):
		apiError(w, r, http.StatusConflict, "local_backdrop_online_gap", "Finishing this import would expose missing artwork to online image processing.")
	case errors.Is(err, library.ErrLocalBackdropRejected):
		apiError(w, r, http.StatusUnprocessableEntity, "local_backdrop_rejected", "A nonempty, fully valid local backdrop set could not be verified.")
	default:
		s.libraryError(w, r, err)
	}
}

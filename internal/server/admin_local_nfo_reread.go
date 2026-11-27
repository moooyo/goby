package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) adminRereadLocalNFO(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, ok := adminMetadataID(w, r)
	if !ok {
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		adminMetadataInputError(w, r, map[string]string{"Query": "This operation does not accept query parameters."})
		return
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		adminMetadataInputError(w, r, map[string]string{"Body": "This operation requires an empty request body."})
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.library.RereadLocalNFO(r.Context(), actor, id)
	switch {
	case err == nil:
		jsonResponse(w, http.StatusOK, result)
	case errors.Is(err, library.ErrForbidden):
		apiError(w, r, http.StatusForbidden, "access_denied", "The NFO reread is not permitted for this session.")
	case errors.Is(err, library.ErrUnavailable), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		apiError(w, r, http.StatusServiceUnavailable, "local_nfo_unavailable", "Local NFO reread is currently unavailable. Reload the item before retrying.")
	case errors.Is(err, library.ErrBusy):
		apiError(w, r, http.StatusConflict, "local_nfo_busy", "The item, library, or catalog writer is not ready for a local NFO reread.")
	case errors.Is(err, library.ErrSourceChanged):
		apiError(w, r, http.StatusConflict, "local_nfo_source_changed", "The current source could not be verified against the indexed item.")
	case errors.Is(err, library.ErrLocalNFODisabled):
		apiError(w, r, http.StatusConflict, "local_nfo_disabled", "Local NFO reading is disabled for this library.")
	case errors.Is(err, library.ErrLocalNFORecoveryOwned):
		apiError(w, r, http.StatusConflict, "local_nfo_recovery_managed", "Use the normal import workflow for an automatic NFO recovery root.")
	case errors.Is(err, library.ErrLocalNFOMissing):
		apiError(w, r, http.StatusNotFound, "local_nfo_missing", "No local NFO candidate was found for this item.")
	case errors.Is(err, library.ErrLocalNFORejected):
		apiError(w, r, http.StatusUnprocessableEntity, "local_nfo_rejected", "The preferred local NFO could not be accepted for this item.")
	default:
		s.libraryError(w, r, err)
	}
}

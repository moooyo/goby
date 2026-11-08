package server

import (
	"context"
	"net/http"
	"time"

	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/providers"
)

func (s *Server) adminProviderMissingImage(w http.ResponseWriter, r *http.Request) {
	var input struct {
		LibraryID string `json:"LibraryId"`
		ImageType string `json:"ImageType"`
	}
	if !providerBody(w, r, &input) {
		return
	}
	id, ok := adminMetadataID(w, r)
	if !ok {
		return
	}
	release, admitted := admitArtworkMutation()
	if !admitted {
		s.artworkManagementError(w, r, artwork.ErrManagedLimit)
		return
	}
	defer release()
	actor := r.Context().Value(principalKey).(identity.Principal)
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := s.library.DownloadMissingOnlineImage(ctx, actor, providers.New(s.onlineProviderConfig()), input.LibraryID, id, input.ImageType)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, map[string]any{
		"Status": result.Status, "ItemId": result.ItemID, "LibraryId": result.LibraryID, "ImageType": result.ImageType,
		"Artwork": nativeArtworkCollection("items", result.ItemID, result.Artwork),
	})
}

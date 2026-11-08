package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/providers"
)

func (s *Server) registerSeriesDisplayOrderRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/series/{id}/display-order", s.requireAdmin(s.adminSeriesDisplayOrder))
	mux.HandleFunc("PUT /admin/v1/series/{id}/display-order", s.requireAdmin(s.updateAdminSeriesDisplayOrder))
	mux.HandleFunc("GET /admin/v1/series/{id}/display-order/groups", s.requireAdmin(s.adminSeriesDisplayOrderGroups))
	mux.HandleFunc("GET /admin/v1/series/{id}/display-order/groups/{group}", s.requireAdmin(s.previewAdminSeriesDisplayOrder))
}

func (s *Server) adminSeriesDisplayOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok || !episodeRosterNoQuery(w, r) {
		return
	}
	detail, err := s.library.GetSeriesDisplayOrder(r.Context(), r.Context().Value(principalKey).(identity.Principal), id)
	if err != nil {
		s.adminMetadataError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, detail)
}

func (s *Server) seriesOrderProviderTarget(w http.ResponseWriter, r *http.Request) (identity.Principal, library.SeriesDisplayOrder, bool) {
	id, ok := adminMetadataID(w, r)
	if !ok || !episodeRosterNoQuery(w, r) {
		return identity.Principal{}, library.SeriesDisplayOrder{}, false
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	detail, err := s.library.GetSeriesDisplayOrder(r.Context(), actor, id)
	if err != nil {
		s.adminMetadataError(w, r, err)
		return actor, detail, false
	}
	if detail.ProviderSeriesID == "" {
		apiError(w, r, http.StatusConflict, "series_provider_required", "Set the series TMDB identifier before choosing an alternate episode order.")
		return actor, detail, false
	}
	return actor, detail, true
}

func (s *Server) adminSeriesDisplayOrderGroups(w http.ResponseWriter, r *http.Request) {
	_, detail, ok := s.seriesOrderProviderTarget(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	items, err := providers.New(s.onlineProviderConfig()).TVEpisodeGroups(ctx, detail.ProviderSeriesID)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	// An outbound lookup can outlive the session's authorization.
	if _, err := s.library.GetSeriesDisplayOrder(r.Context(), r.Context().Value(principalKey).(identity.Principal), detail.SeriesID); err != nil {
		s.adminMetadataError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items})
}

func (s *Server) previewAdminSeriesDisplayOrder(w http.ResponseWriter, r *http.Request) {
	actor, detail, ok := s.seriesOrderProviderTarget(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	mapping, err := providers.New(s.onlineProviderConfig()).TVEpisodeGroup(ctx, detail.ProviderSeriesID, r.PathValue("group"))
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	preview, err := s.library.PreviewSeriesDisplayOrder(r.Context(), actor, detail.SeriesID, mapping)
	if err != nil {
		s.adminMetadataError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, preview)
}

func (s *Server) updateAdminSeriesDisplayOrder(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision      string `json:"Revision"`
		DisplayOrder  string `json:"DisplayOrder"`
		GroupID       string `json:"GroupId"`
		MappingSHA256 string `json:"MappingSHA256"`
	}
	if !providerBody(w, r, &input) {
		return
	}
	id, ok := adminMetadataID(w, r)
	if !ok {
		return
	}
	if input.DisplayOrder != "Aired" && input.DisplayOrder != "Dvd" && input.DisplayOrder != "Absolute" ||
		input.DisplayOrder == "Aired" && (input.GroupID != "" || input.MappingSHA256 != "") ||
		input.DisplayOrder != "Aired" && (input.GroupID == "" || len(input.GroupID) > 128 || len(input.MappingSHA256) != 64 || strings.ToLower(input.MappingSHA256) != input.MappingSHA256) {
		adminMetadataInputError(w, r, map[string]string{"DisplayOrder": "Choose Aired, Dvd, or Absolute. Preview an explicit provider group before saving an alternate order."})
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	var mapping *providers.TVEpisodeGroupDetail
	if input.DisplayOrder != "Aired" {
		detail, err := s.library.GetSeriesDisplayOrder(r.Context(), actor, id)
		if err != nil {
			s.adminMetadataError(w, r, err)
			return
		}
		if detail.Revision != input.Revision {
			s.adminMetadataError(w, r, library.ErrRevisionConflict)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		loaded, err := providers.New(s.onlineProviderConfig()).TVEpisodeGroup(ctx, detail.ProviderSeriesID, input.GroupID)
		if err != nil {
			s.providerError(w, r, err)
			return
		}
		digest, err := library.SeriesOrderMappingSHA256(loaded)
		if err != nil || loaded.DisplayOrder != input.DisplayOrder || subtle.ConstantTimeCompare([]byte(digest), []byte(input.MappingSHA256)) != 1 {
			apiError(w, r, http.StatusConflict, "series_mapping_changed", "The provider group changed after the preview. Reload its preview before saving.")
			return
		}
		mapping = &loaded
	}
	detail, err := s.library.SetSeriesDisplayOrder(r.Context(), actor, id, input.Revision, input.DisplayOrder, mapping)
	if err != nil {
		var coverageError *library.SeriesOrderMappingError
		if errors.As(err, &coverageError) {
			adminMetadataInputError(w, r, map[string]string{"GroupId": fmt.Sprintf("The selected group leaves %d catalog or expected episodes unmapped. Choose a complete group or keep aired order.", coverageError.Coverage.UnmappedCount)})
		} else {
			s.adminMetadataError(w, r, err)
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, detail)
}

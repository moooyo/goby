package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/providers"
)

func (s *Server) registerProviderRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Items/{Id}/RemoteSearch/Subtitles/{Language}", s.requireEmby(s.embyProviderSubtitleSearch))
	mux.HandleFunc("POST /emby/Items/{Id}/RemoteSearch/Subtitles/{SubtitleId}", s.requireEmby(s.embyProviderSubtitleDownload))
	mux.HandleFunc("GET /admin/v1/providers", s.requireAdmin(s.adminProviders))
	mux.HandleFunc("GET /admin/v1/items/{id}/providers/provenance", s.requireAdmin(s.adminProviderSources))
	mux.HandleFunc("POST /admin/v1/items/{id}/providers/search", s.requireAdmin(s.adminProviderSearch))
	mux.HandleFunc("POST /admin/v1/items/{id}/providers/apply", s.requireAdmin(s.adminProviderApply))
	mux.HandleFunc("POST /admin/v1/items/{id}/providers/refresh", s.requireAdmin(s.adminProviderRefresh))
	mux.HandleFunc("POST /admin/v1/items/{id}/providers/images", s.requireAdmin(s.adminProviderImages))
	mux.HandleFunc("POST /admin/v1/items/{id}/providers/image", s.requireAdmin(s.adminProviderImage))
	mux.HandleFunc("GET /admin/v1/items/{id}/providers/image-preview", s.requireAdmin(s.adminProviderImagePreview))
	mux.HandleFunc("POST /admin/v1/items/{id}/providers/subtitles/search", s.requireAdmin(s.adminProviderSubtitleSearch))
	mux.HandleFunc("POST /admin/v1/items/{id}/providers/subtitles/download", s.requireAdmin(s.adminProviderSubtitleDownload))
}

func (s *Server) adminProviders(w http.ResponseWriter, r *http.Request) {
	if !adminMetadataNoQuery(w, r) {
		return
	}
	config := s.onlineProviderConfig()
	jsonResponse(w, http.StatusOK, map[string]any{"Enabled": config.Enabled, "Items": config.Status()})
}

func providerBody(w http.ResponseWriter, r *http.Request, target any) bool {
	if !adminMetadataNoQuery(w, r) {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this request.")
		return false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil || !utf8.Valid(data) || !metadataUniqueJSON(data) {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply one bounded UTF-8 JSON object without duplicate fields.")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' || decoder.Decode(target) != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Check the provider request fields.")
		return false
	}
	return true
}

func (s *Server) providerItem(w http.ResponseWriter, r *http.Request) (identity.Principal, library.ItemMetadataDetail, bool) {
	id, ok := adminMetadataID(w, r)
	if !ok {
		return identity.Principal{}, library.ItemMetadataDetail{}, false
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	detail, err := s.library.GetItemMetadata(r.Context(), actor, id)
	if err != nil {
		s.adminMetadataError(w, r, err)
		return actor, detail, false
	}
	return actor, detail, true
}

func (s *Server) providerError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, providers.ErrDisabled):
		apiError(w, r, http.StatusConflict, "providers_disabled", "Enable online providers in metadata settings.")
	case errors.Is(err, providers.ErrNotConfigured):
		apiError(w, r, http.StatusServiceUnavailable, "provider_not_configured", "The selected provider requires valid server-side credentials or application identification.")
	case errors.Is(err, providers.ErrQuota):
		apiError(w, r, http.StatusTooManyRequests, "provider_quota", "The provider rate or download quota was reached. Retry later.")
	case errors.Is(err, providers.ErrNotFound):
		apiError(w, r, http.StatusNotFound, "provider_not_found", "The provider no longer has this result.")
	case errors.Is(err, providers.ErrInvalidInput):
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Check the provider, item type, identifiers, and language.")
	case errors.Is(err, providers.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		apiError(w, r, http.StatusBadGateway, "provider_unavailable", "The provider could not complete this request.")
	case errors.Is(err, library.ErrSourceChanged):
		apiError(w, r, http.StatusConflict, "source_changed", "The media source changed. Search for subtitles again.")
	default:
		s.adminMetadataError(w, r, err)
	}
}

func (s *Server) adminProviderSources(w http.ResponseWriter, r *http.Request) {
	actor, detail, ok := s.providerItem(w, r)
	if !ok || !adminMetadataNoQuery(w, r) {
		return
	}
	result, err := s.library.ProviderSources(r.Context(), actor, detail.ItemID)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": result})
}

type providerSelectionRequest struct {
	Provider string `json:"Provider"`
	ID       string `json:"Id"`
	Language string `json:"Language"`
	Revision string `json:"Revision"`
}

func (request providerSelectionRequest) selection(detail library.ItemMetadataDetail) providers.Selection {
	return providers.Selection{Provider: request.Provider, ID: request.ID, Type: detail.Type, Language: request.Language}
}

func (s *Server) adminProviderSearch(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Provider string `json:"Provider"`
		Name     string `json:"Name"`
		Year     int    `json:"Year"`
		Language string `json:"Language"`
	}
	if !providerBody(w, r, &input) {
		return
	}
	actor, detail, ok := s.providerItem(w, r)
	if !ok {
		return
	}
	query, err := s.library.OnlineMetadataQuery(r.Context(), actor, detail.ItemID)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	if input.Name != "" {
		query.Name = input.Name
	}
	if input.Year != 0 {
		query.Year = input.Year
	}
	query.Language = input.Language
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	items, err := providers.New(s.onlineProviderConfig()).Search(ctx, input.Provider, query)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items})
}

func (s *Server) adminProviderApply(w http.ResponseWriter, r *http.Request) {
	var input providerSelectionRequest
	if !providerBody(w, r, &input) {
		return
	}
	actor, detail, ok := s.providerItem(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := providers.New(s.onlineProviderConfig()).Lookup(ctx, input.selection(detail))
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	updated, err := s.library.ApplyOnlineMetadata(ctx, actor, detail.ItemID, input.Revision, result)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, nativeItemMetadata(updated))
}

func (s *Server) adminProviderRefresh(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Provider string `json:"Provider"`
		Language string `json:"Language"`
		Revision string `json:"Revision"`
	}
	if !providerBody(w, r, &input) {
		return
	}
	actor, detail, ok := s.providerItem(w, r)
	if !ok {
		return
	}
	sources, err := s.library.ProviderSources(r.Context(), actor, detail.ItemID)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	selection := providers.Selection{Provider: input.Provider, Language: input.Language, Type: detail.Type}
	for _, source := range sources {
		if input.Provider == "" || input.Provider == source.Provider {
			selection.Provider, selection.ID = source.Provider, source.ProviderID
			if selection.Language == "" {
				selection.Language = source.Language
			}
			break
		}
	}
	if selection.ID == "" {
		for name, id := range detail.Effective.ProviderIDs {
			if strings.EqualFold(name, selection.Provider) {
				selection.ID = id
				break
			}
		}
	}
	if selection.ID == "" {
		apiError(w, r, http.StatusConflict, "provider_match_required", "Select a provider match before refreshing this item.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := providers.New(s.onlineProviderConfig()).Lookup(ctx, selection)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	updated, err := s.library.ApplyOnlineMetadata(ctx, actor, detail.ItemID, input.Revision, result)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, nativeItemMetadata(updated))
}

func (s *Server) adminProviderImages(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Provider string `json:"Provider"`
		ID       string `json:"Id"`
		Language string `json:"Language"`
	}
	if !providerBody(w, r, &input) {
		return
	}
	_, detail, ok := s.providerItem(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	items, err := providers.New(s.onlineProviderConfig()).Images(ctx, providers.Selection{Provider: input.Provider, ID: input.ID, Type: detail.Type, Language: input.Language})
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	for index := range items {
		item := &items[index]
		values := url.Values{"Provider": {item.Provider}, "Id": {item.ID}, "ImageId": {item.ImageID}, "ImageType": {item.ImageType}, "Language": {item.Language}}
		item.PreviewURL = "/admin/v1/items/" + url.PathEscape(detail.ItemID) + "/providers/image-preview?" + values.Encode()
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items})
}

func (s *Server) adminProviderImage(w http.ResponseWriter, r *http.Request) {
	var input struct {
		providerSelectionRequest
		ImageID    string `json:"ImageId"`
		ImageType  string `json:"ImageType"`
		ImageIndex int    `json:"ImageIndex"`
	}
	if !providerBody(w, r, &input) {
		return
	}
	actor, detail, ok := s.providerItem(w, r)
	if !ok {
		return
	}
	selected := providers.RemoteImage{Selection: input.selection(detail), ImageID: input.ImageID, ImageType: input.ImageType}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	data, err := providers.New(s.onlineProviderConfig()).DownloadImage(ctx, selected)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	result, err := s.library.SelectProviderImage(ctx, actor, detail.ItemID, input.Revision, selected, input.ImageIndex, data)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) adminProviderSubtitleSearch(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Languages       []string `json:"Languages"`
		HearingImpaired bool     `json:"HearingImpaired"`
	}
	if !providerBody(w, r, &input) {
		return
	}
	actor, detail, ok := s.providerItem(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	query, err := s.library.OnlineMetadataQuery(ctx, actor, detail.ItemID)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	items, err := providers.New(s.onlineProviderConfig()).SearchSubtitles(ctx, providers.SubtitleQuery{Query: query, Languages: input.Languages, HearingImpaired: input.HearingImpaired})
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items})
}

func (s *Server) adminProviderSubtitleDownload(w http.ResponseWriter, r *http.Request) {
	var input providers.RemoteSubtitle
	if !providerBody(w, r, &input) {
		return
	}
	actor, detail, ok := s.providerItem(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	client := providers.New(s.onlineProviderConfig())
	// Revalidate the selected file against a fresh search for this catalog item.
	query, err := s.library.OnlineMetadataQuery(ctx, actor, detail.ItemID)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	items, err := client.SearchSubtitles(ctx, providers.SubtitleQuery{Query: query, Languages: []string{input.Language}})
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	var selected *providers.RemoteSubtitle
	for _, candidate := range items {
		if candidate.Provider == input.Provider && candidate.ID == input.ID && candidate.FileID == input.FileID && candidate.Language == input.Language {
			copy := candidate
			selected = &copy
			break
		}
	}
	if selected == nil {
		s.providerError(w, r, providers.ErrNotFound)
		return
	}
	download, err := client.DownloadSubtitle(ctx, *selected)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	if err := s.library.RegisterDownloadedSubtitle(ctx, actor, detail.ItemID, download); err != nil {
		s.providerError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Downloaded": true, "Provider": download.Provider, "RemoteId": download.RemoteID, "Language": download.Language, "Format": download.Format})
}

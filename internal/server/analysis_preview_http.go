package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/bif"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

const analysisPreviewMaxBIFBytes = 128 << 20

func (s *Server) registerAnalysisPreviewRoutes(mux *http.ServeMux, provider analysisPreviewProvider) {
	for _, route := range []struct{ path, kind string }{
		{"/emby/Items/{Id}/ThumbnailSet", "set"},
		{"/emby/Videos/{Id}/index.bif", "bif"},
		{"/emby/Items/{Id}/Images/Thumbnail", "image"},
	} {
		mux.HandleFunc("GET "+route.path, s.requireEmby(func(w http.ResponseWriter, r *http.Request) {
			s.serveAnalysisPreview(w, r, provider, route.kind)
		}))
	}
}

func analysisPreviewIdentity(metadata analysisPreviewMetadata, kind string, request analysisPreviewRequest) string {
	encoded, _ := json.Marshal(struct {
		Version, Item, Source, Revision, Content, Kind string
		Width, MaxWidth, Quality                       int
		Position                                       int64
	}{"seek-preview-v1", metadata.ItemID, metadata.MediaSourceID, metadata.SourceRevision, metadata.BIFSHA256,
		kind, metadata.Width, request.maxWidth, request.quality, request.positionTicks})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func analysisPreviewImageTag(metadata analysisPreviewMetadata, position int64) string {
	return "goby-preview-" + strconv.Itoa(metadata.Width) + "-" + analysisPreviewIdentity(metadata, "frame", analysisPreviewRequest{positionTicks: position})
}

func validateAnalysisPreviewMetadata(metadata analysisPreviewMetadata, request analysisPreviewRequest, itemID string) bool {
	if metadata.ItemID != itemID || !analysisPreviewOpaque(metadata.ItemID, 256) || !analysisPreviewOpaque(metadata.MediaSourceID, 256) ||
		!analysisPreviewOpaque(metadata.SourceRevision, 512) || request.sourceID != "" && request.sourceID != metadata.MediaSourceID {
		return false
	}
	if !metadata.Ready {
		return true
	}
	return analysisPreviewWidth(metadata.Width) && (request.width == 0 || metadata.Width == request.width) &&
		metadata.Height > 0 && metadata.Height <= 4096 && metadata.Size >= 72 && metadata.Size <= analysisPreviewMaxBIFBytes && analysisPreviewDigest(metadata.BIFSHA256)
}

func (s *Server) serveAnalysisPreview(w http.ResponseWriter, r *http.Request, provider analysisPreviewProvider, kind string) {
	w.Header().Set("Cache-Control", "private, no-store")
	request, err := parseAnalysisPreviewRequest(r, kind == "image")
	if err != nil {
		s.analysisPreviewError(w, r, err)
		return
	}
	principal, authenticated := r.Context().Value(principalKey).(identity.Principal)
	if !authenticated {
		s.analysisPreviewError(w, r, identity.ErrUnauthorized)
		return
	}
	if provider == nil {
		s.analysisPreviewError(w, r, library.ErrUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	lease, err := provider.OpenPreview(ctx, principal, r.PathValue("Id"), request.sourceID, request.width)
	if err != nil {
		if lease != nil {
			_ = lease.Close()
		}
		s.analysisPreviewError(w, r, err)
		return
	}
	if lease == nil || lease.Context() == nil {
		if lease != nil {
			_ = lease.Close()
		}
		s.analysisPreviewError(w, r, library.ErrUnavailable)
		return
	}
	// Close the descriptor on either cancellation and join that callback before
	// returning. The provider keeps its admission occupied until Close finishes.
	var closeOnce sync.Once
	closeLease := func() { closeOnce.Do(func() { _ = lease.Close() }) }
	defer closeLease()
	work, stopWork := context.WithCancel(ctx)
	stopLifetime := context.AfterFunc(lease.Context(), stopWork)
	if lease.Context().Err() != nil {
		stopWork()
	}
	defer func() { stopLifetime(); stopWork() }()
	closed := make(chan struct{})
	stopClose := context.AfterFunc(work, func() { defer close(closed); closeLease() })
	defer func() {
		if !stopClose() {
			<-closed
		}
	}()
	if err := work.Err(); err != nil {
		s.analysisPreviewError(w, r, err)
		return
	}
	metadata := lease.Metadata()
	if !validateAnalysisPreviewMetadata(metadata, request, r.PathValue("Id")) {
		s.analysisPreviewError(w, r, library.ErrUnavailable)
		return
	}
	reader := lease.Reader()
	if !metadata.Ready {
		if err := lease.Revalidate(work); err != nil {
			s.analysisPreviewError(w, r, err)
			return
		}
		if kind != "bif" {
			s.analysisPreviewError(w, r, library.ErrNotFound)
			return
		}
		empty, err := bif.Encode(work, nil, 1000, bif.Limits{})
		if err != nil {
			s.analysisPreviewError(w, r, err)
			return
		}
		metadata.Width = request.width
		s.serveAnalysisPreviewContent(w, r.WithContext(work), bytes.NewReader(empty), int64(len(empty)), "application/octet-stream",
			`"`+analysisPreviewIdentity(metadata, "missing-bif", request)+`"`, true)
		return
	}
	if reader == nil {
		s.analysisPreviewError(w, r, library.ErrUnavailable)
		return
	}
	index, err := bif.Open(reader, metadata.Size, bif.DefaultLimits())
	if err != nil || index.Len() == 0 {
		s.analysisPreviewError(w, r, library.ErrUnavailable)
		return
	}
	var body []byte
	contentType := "application/octet-stream"
	switch kind {
	case "set":
		dto := analysisThumbnailSetDTO{AspectRatio: float64(metadata.Width) / float64(metadata.Height), Thumbnails: make([]analysisThumbnailDTO, index.Len())}
		for number := range dto.Thumbnails {
			entry, err := index.Entry(number)
			if err != nil || entry.TimestampMillis > math.MaxInt64/10000 {
				s.analysisPreviewError(w, r, library.ErrUnavailable)
				return
			}
			position := int64(entry.TimestampMillis) * 10000
			dto.Thumbnails[number] = analysisThumbnailDTO{PositionTicks: position, ImageTag: analysisPreviewImageTag(metadata, position)}
		}
		body, err = json.Marshal(dto)
		contentType = "application/json; charset=utf-8"
	case "image":
		number, found := index.AtMillis(uint64(request.positionTicks / 10000))
		if !found {
			s.analysisPreviewError(w, r, library.ErrNotFound)
			return
		}
		entry, _ := index.Entry(number)
		if entry.TimestampMillis > math.MaxInt64/10000 {
			s.analysisPreviewError(w, r, library.ErrUnavailable)
			return
		}
		position := int64(entry.TimestampMillis) * 10000
		if request.tag != "" && request.tag != analysisPreviewImageTag(metadata, position) {
			s.analysisPreviewError(w, r, library.ErrNotFound)
			return
		}
		body, err = index.JPEG(work, number)
		if err == nil {
			var rendered artwork.Result
			rendered, err = artwork.Render(work, bytes.NewReader(body), artwork.Options{Format: "jpeg", MaxWidth: request.maxWidth, Quality: request.quality})
			body = rendered.Bytes
		}
		request.positionTicks = position
		contentType = "image/jpeg"
	case "bif":
	default:
		err = library.ErrInvalidInput
	}
	if err != nil {
		s.analysisPreviewError(w, r, err)
		return
	}
	if err := lease.Revalidate(work); err != nil {
		s.analysisPreviewError(w, r, err)
		return
	}
	if err := work.Err(); err != nil {
		s.analysisPreviewError(w, r, err)
		return
	}
	size := metadata.Size
	var content io.ReadSeeker = reader
	if kind != "bif" {
		content, size = bytes.NewReader(body), int64(len(body))
	}
	s.serveAnalysisPreviewContent(w, r.WithContext(work), content, size, contentType,
		`"`+analysisPreviewIdentity(metadata, kind, request)+`"`, kind == "bif")
}

func (s *Server) analysisPreviewError(w http.ResponseWriter, r *http.Request, err error) {
	w.Header().Set("Cache-Control", "private, no-store")
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials):
		s.identityError(w, r, err)
	case errors.Is(err, library.ErrForbidden), errors.Is(err, identity.ErrClientSessionForbidden):
		apiError(w, r, http.StatusForbidden, "access_denied", "The preview is not accessible.")
	case errors.Is(err, library.ErrNotFound):
		apiError(w, r, http.StatusNotFound, "not_found", "No preview is available for this media selection.")
	case errors.Is(err, library.ErrInvalidInput):
		apiError(w, r, http.StatusBadRequest, "invalid_preview_request", "Check the preview width, position, source and supported image options.")
	case errors.Is(err, library.ErrBusy), errors.Is(err, artwork.ErrLimitExceeded):
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "preview_limit", "The bounded preview read capacity has been reached.")
	default:
		apiError(w, r, http.StatusServiceUnavailable, "preview_unavailable", "The preview is currently unavailable.")
	}
}

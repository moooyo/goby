package server

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/providers"
)

func (s *Server) adminProviderImagePreview(w http.ResponseWriter, r *http.Request) {
	s.serveProviderImagePreview(w, r, func(ctx context.Context, selected providers.RemoteImage) ([]byte, error) {
		return providers.New(s.onlineProviderConfig()).DownloadImage(ctx, selected)
	})
}

func (s *Server) serveProviderImagePreview(w http.ResponseWriter, r *http.Request, download func(context.Context, providers.RemoteImage) ([]byte, error)) {
	actor, detail, ok := s.providerItem(w, r)
	if !ok {
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		s.providerError(w, r, providers.ErrInvalidInput)
		return
	}
	for name, entries := range values {
		if len(entries) != 1 || (name != "Provider" && name != "Id" && name != "ImageId" && name != "ImageType" && name != "Language") {
			s.providerError(w, r, providers.ErrInvalidInput)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	select {
	case s.images.slots <- struct{}{}:
	case <-ctx.Done():
		s.providerError(w, r, ctx.Err())
		return
	}
	processing := true
	releaseProcessing := func() {
		if processing {
			<-s.images.slots
			processing = false
		}
	}
	defer releaseProcessing()
	selected := providers.RemoteImage{Selection: providers.Selection{Provider: values.Get("Provider"), ID: values.Get("Id"), Type: detail.Type, Language: values.Get("Language")}, ImageID: values.Get("ImageId"), ImageType: values.Get("ImageType")}
	data, err := download(ctx, selected)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	preview, err := artwork.Render(ctx, bytes.NewReader(data), artwork.Options{Format: "jpeg", MaxWidth: 320, MaxHeight: 480})
	// Do not retain the downloaded source during response output.
	data = nil
	if err != nil {
		s.imageError(w, r, err)
		return
	}
	if _, err := s.library.GetItemMetadata(ctx, actor, detail.ItemID); err != nil {
		s.providerError(w, r, err)
		return
	}
	// The response retains preview.Bytes even for HEAD. Reserve its backing
	// capacity before handing processing ownership to the transfer budget.
	release, admitted := s.images.beginTransfer(cap(preview.Bytes))
	if !admitted {
		preview.Bytes = nil
		releaseProcessing()
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "image_transfer_limit", "The active image transfer limit has been reached.")
		return
	}
	defer release()
	releaseProcessing()
	writer, err := newIdleResponseWriter(w, r.Context(), mediaWriteIdle)
	if err != nil {
		return
	}
	defer writer.finish()
	w.Header().Set("Content-Type", preview.MIMEType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(preview.Bytes)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = writer.Write(preview.Bytes)
	}
}

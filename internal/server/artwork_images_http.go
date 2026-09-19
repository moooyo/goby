package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

// Every open is an authorization/source operation, including the second open
// before a cache validator or response. Public avatars supply their own narrow
// public-login predicate; item and entity openers always require current ACLs.
func (s *Server) serveArtworkImage(w http.ResponseWriter, r *http.Request, open func(context.Context) (io.ReadCloser, library.Image, error)) {
	request, err := parseImageRequest(r)
	if err != nil {
		s.artworkManagementError(w, r, artwork.ErrInvalidOptions)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	select {
	case s.images.slots <- struct{}{}:
	case <-ctx.Done():
		s.artworkManagementError(w, r, ctx.Err())
		return
	}
	processing := true
	defer func() {
		if processing {
			<-s.images.slots
		}
	}()
	reader, source, err := open(ctx)
	if err != nil {
		s.artworkManagementError(w, r, err)
		return
	}
	key := imageVariantKey(source.Tag, request.options)
	result, found := s.images.get(key)
	if !found {
		rendered, renderErr := artwork.Render(ctx, reader, request.options)
		_ = reader.Close()
		if renderErr != nil {
			s.artworkManagementError(w, r, renderErr)
			return
		}
		if rendered.Source.Tag != source.Tag {
			s.artworkManagementError(w, r, library.ErrUnavailable)
			return
		}
		result = cachedImage{key: key, contentType: rendered.MIMEType, etag: imageETag(source.Tag, request.options), data: rendered.Bytes}
		s.images.put(result)
	} else {
		_ = reader.Close()
	}
	fresh, current, err := open(ctx)
	if err != nil {
		s.artworkManagementError(w, r, err)
		return
	}
	_ = fresh.Close()
	if source.Tag != current.Tag {
		s.artworkManagementError(w, r, library.ErrRevisionConflict)
		return
	}
	if err := ctx.Err(); err != nil {
		s.artworkManagementError(w, r, err)
		return
	}
	transferSize := cap(result.data)
	unchanged := matchesImageETag(strings.Join(r.Header.Values("If-None-Match"), ","), result.etag)
	if r.Method == http.MethodHead || unchanged {
		transferSize = 0
	}
	release, admitted := s.images.beginTransfer(transferSize)
	if !admitted {
		s.artworkManagementError(w, r, artwork.ErrManagedLimit)
		return
	}
	defer release()
	<-s.images.slots
	processing = false
	writer, err := newIdleResponseWriter(w, r.Context(), mediaWriteIdle)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	defer writer.finish()
	w.Header().Set("Content-Type", result.contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", result.etag)
	w.Header().Set("Cache-Control", "private, no-cache, no-transform")
	if unchanged {
		w.Header().Del("Content-Length")
		writer.WriteHeader(http.StatusNotModified)
		return
	}
	if request.tag == source.Tag && !source.ModifiedAt.IsZero() {
		w.Header().Set("Last-Modified", source.ModifiedAt.UTC().Format(http.TimeFormat))
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(result.data)))
	writer.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = writer.Write(result.data)
	}
}

func (s *Server) artworkManagementError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, library.ErrRevisionConflict), errors.Is(err, artwork.ErrManagedConflict):
		apiError(w, r, http.StatusConflict, "artwork_conflict", "The images changed. Reload them before applying this edit.")
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials):
		s.identityError(w, r, err)
	case errors.Is(err, identity.ErrClientSessionForbidden), errors.Is(err, library.ErrForbidden):
		apiError(w, r, http.StatusForbidden, "access_denied", "The image operation is not permitted.")
	case errors.Is(err, identity.ErrNotFound), errors.Is(err, library.ErrNotFound):
		apiError(w, r, http.StatusNotFound, "not_found", "The image or its owner was not found.")
	case errors.Is(err, artwork.ErrManagedLimit):
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "artwork_limit", "The bounded artwork storage or transfer capacity has been reached.")
	case errors.Is(err, artwork.ErrManagedTarget), errors.Is(err, artwork.ErrInvalidOptions), errors.Is(err, library.ErrInvalidInput), errors.Is(err, identity.ErrInvalidInput):
		apiError(w, r, http.StatusBadRequest, "invalid_artwork_request", "Check the target, type, index, revision and image order.")
	case errors.Is(err, artwork.ErrManagedStorage), errors.Is(err, library.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		apiError(w, r, http.StatusServiceUnavailable, "artwork_unavailable", "The artwork is currently unavailable.")
	default:
		s.imageError(w, r, err)
	}
}

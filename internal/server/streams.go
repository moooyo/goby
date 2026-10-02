package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"

	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/primaryio"
	"github.com/moooyo/goby/internal/transcode"
)

// Shared media aliases dispatch to original or negotiated conversion delivery.
// A filename here is a delivery alias, never a filesystem lookup argument.
func (s *Server) registerStreamRoutes(mux *http.ServeMux) {
	for _, resource := range []string{"Videos", "Audio"} {
		handler := s.videoStream
		if resource == "Audio" {
			handler = s.audioStream
		}
		for _, base := range []string{"/emby/" + resource, "/emby/" + strings.ToLower(resource), "/" + resource, "/" + strings.ToLower(resource)} {
			mux.HandleFunc("GET "+base+"/{Id}/stream", s.requireEmby(handler))
			mux.HandleFunc("GET "+base+"/{Id}/{StreamFileName}", s.requireEmby(handler))
		}
	}
}

func streamValues(r *http.Request) (map[string]string, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	return imageQuery(values)
}

func streamAlias(name string) (container string, original bool, valid bool) {
	if name == "" || strings.EqualFold(name, "stream") {
		return "", false, true
	}
	prefix, extension, found := strings.Cut(strings.ToLower(name), ".")
	if !found || (prefix != "stream" && prefix != "original") || extension == "" || len(extension) > 16 {
		return "", false, false
	}
	for _, character := range extension {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9') {
			return "", false, false
		}
	}
	return extension, prefix == "original", true
}

// serveOriginalMedia consumes an owned source reopened against the exact planning
// snapshot. Universal and legacy responses use the same bounded reader and retain
// Store ownership until actual reads, cancellation cleanup and FD Close finish.
func (s *Server) serveOriginalMedia(w http.ResponseWriter, r *http.Request, file *os.File, source library.MediaFile, content *primaryio.ReadSeeker) {
	defer func() {
		if err := content.Close(); err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	r = r.WithContext(content.Context())
	work, finish, err := s.guardOriginalMedia(w, r, file, source)
	if err != nil {
		if errors.Is(err, identity.ErrUnauthorized) {
			s.identityError(w, r, err)
		} else if errors.Is(err, library.ErrBusy) || errors.Is(err, transcode.ErrBusy) {
			w.Header().Set("Retry-After", "2")
			apiError(w, r, http.StatusTooManyRequests, "stream_limit", "The authenticated owner has reached its active media stream limit.")
		} else if errors.Is(err, context.Canceled) {
			if r.Context().Err() == nil {
				apiError(w, r, http.StatusServiceUnavailable, "media_cancelled", "The original media response was cancelled.")
			}
		} else {
			s.libraryError(w, r, err)
		}
		return
	}
	defer func() {
		// Keep per-owner original admission until actual source methods and
		// descriptor cleanup finish, then join/release authorization watchers.
		defer finish()
		if err := content.Close(); err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	defer bridgeOriginalReadCancellation(work, content)()
	r = r.WithContext(work)
	writer, err := newIdleResponseWriter(w, work, mediaWriteIdle)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	finishWriter := finishOriginalPrimaryWriter(writer, func() { s.failOriginalMediaPolicy(work) })
	defer finishWriter()
	w.Header().Set("Content-Type", source.MIMEType)
	w.Header().Set("ETag", source.ETag)
	w.Header().Set("Cache-Control", "private, no-transform")
	w.Header().Set("Access-Control-Expose-Headers", "Accept-Ranges, Content-Length, Content-Range, ETag, Last-Modified")
	modified := source.ModifiedAt
	if source.Item.Media != nil && source.Item.Media.FileChangeTimeNs > 0 {
		changed := time.Unix(0, source.Item.Media.FileChangeTimeNs).UTC()
		if changed.After(modified) {
			modified = changed
		}
	}
	// net/http provides byte/suffix/multipart ranges, HEAD, If-Range and
	// conditional responses. Ticks never become a guessed source-byte offset.
	// Linux ctime invalidates date validators when a writer restores mtime;
	// ETags remain the precise validator because HTTP dates have second precision.
	counted := &originalPrimaryResponseWriter{writer: writer}
	if err := serveOriginalPrimaryContent(counted, r, "original."+source.Container, modified, source.MIMEType, source.Size, content); err != nil {
		s.failOriginalMediaPolicy(work)
		panic(http.ErrAbortHandler)
	}
	if originalPrimaryBodyError(r, content, writer, counted) != nil {
		s.failOriginalMediaPolicy(work)
		panic(http.ErrAbortHandler)
	}
	if work.Err() != nil {
		s.failOriginalMediaPolicy(work)
		panic(http.ErrAbortHandler)
	}
	// Buffered transport delivery is not complete until the final flush succeeds.
	// This also stops and joins the deadline callback before reporting presence.
	finishWriter()
	if work.Err() != nil {
		s.failOriginalMediaPolicy(work)
		panic(http.ErrAbortHandler)
	}
	if r.Method != http.MethodHead && (writer.status == http.StatusOK || writer.status == http.StatusPartialContent) {
		s.touchOriginalMediaPolicy(work, r.Context().Value(principalKey).(identity.Principal))
	}
	if r.Method != http.MethodHead && writer.err == nil && writer.status == http.StatusOK && r.Header.Get("Range") == "" {
		s.completeOriginalMediaPolicy(work)
	} else if writer.err != nil || writer.status != http.StatusOK && writer.status != http.StatusPartialContent {
		s.failOriginalMediaPolicy(work)
	}
}

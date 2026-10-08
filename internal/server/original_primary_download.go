package server

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/primaryio"
)

// serveOriginalDownloadSnapshot preserves ownership for internal callers that
// already hold a planning descriptor. HTTP requests plan without opening a file.
func (s *Server) serveOriginalDownloadSnapshot(w http.ResponseWriter, r *http.Request, planning *os.File, expected library.MediaFile) {
	if planning == nil {
		s.downloadMediaError(w, r, library.ErrUnavailable)
		return
	}
	if err := planning.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		s.downloadMediaError(w, r, library.ErrUnavailable)
		return
	}
	prepare, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	s.servePreparedOriginalDownloadSnapshot(w, r, prepare, expected)
}

// Fresh download authority and the planning ETag are checked after admission.
// Only preparation observes its deadline; delivery retains the request lifetime.
func (s *Server) servePreparedOriginalDownloadSnapshot(w http.ResponseWriter, r *http.Request, prepare context.Context, expected library.MediaFile) {
	principal := r.Context().Value(principalKey).(identity.Principal)
	file, source, content, err := s.library.OpenPreparedOriginalDownloadFor(prepare, r.Context(), librarySubject(principal, principal.User.ID), expected.Item.ID, expected.SourceID, expected.ETag)
	if err != nil {
		s.downloadMediaError(w, r, err)
		return
	}
	if source.Item.ID != expected.Item.ID || source.SourceID != expected.SourceID || source.Item.Type != expected.Item.Type {
		_ = content.Close()
		s.downloadMediaError(w, r, library.ErrSourceChanged)
		return
	}
	s.serveOriginalDownload(w, r, file, source, content)
}

// Downloads preserve independent authorization, disposition and cache behavior.
// The bounded synchronous copier shares actual read capacity with playback but
// never touches its presence, bitrate policy or delivery lease.
func (s *Server) serveOriginalDownload(w http.ResponseWriter, r *http.Request, file *os.File, source library.MediaFile, content *primaryio.ReadSeeker) {
	defer func() {
		if err := content.Close(); err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	r = r.WithContext(content.Context())
	work, finish, err := s.guardDownloadMedia(w, r, file, source)
	if err != nil {
		s.downloadMediaError(w, r, err)
		return
	}
	defer func() {
		// Retain original response admission until source calls, cancellation
		// cleanup and FD Close finish, then join the authorization watcher.
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
	finishWriter := finishOriginalPrimaryWriter(writer, func() {})
	defer finishWriter()
	disposition := "inline"
	if strings.EqualFold(filepath.Base(r.URL.Path), "Download") {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": filepath.Base(source.Item.Path)}))
	w.Header().Set("Content-Type", source.MIMEType)
	w.Header().Set("ETag", source.ETag)
	w.Header().Set("Cache-Control", "private, no-cache, no-transform")
	w.Header().Set("Access-Control-Expose-Headers", "Accept-Ranges, Content-Disposition, Content-Length, Content-Range, ETag, Last-Modified")
	modified := source.ModifiedAt
	if source.Item.Media != nil && source.Item.Media.FileChangeTimeNs > 0 {
		if changed := time.Unix(0, source.Item.Media.FileChangeTimeNs).UTC(); changed.After(modified) {
			modified = changed
		}
	}
	// Authority precedes conditional and range processing, including metadata-
	// only responses. Genuine multipart copies finish synchronously here.
	counted := &originalPrimaryResponseWriter{writer: writer}
	if err := serveOriginalPrimaryContent(counted, r, filepath.Base(source.Item.Path), modified, source.MIMEType, source.Size, content); err != nil {
		panic(http.ErrAbortHandler)
	}
	if originalPrimaryBodyError(r, content, writer, counted) != nil || work.Err() != nil {
		panic(http.ErrAbortHandler)
	}
	finishWriter()
	if work.Err() != nil {
		panic(http.ErrAbortHandler)
	}
}

package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/primaryio"
)

// serveOriginalMediaSnapshot consumes the planning descriptor, then opens the
// actual response descriptor with retained ownership registered before opening.
// Fresh authority, rooted identity/publication checks and the exact planning
// ETag remain mandatory. Conversion inputs retain their existing ownership API.
func (s *Server) serveOriginalMediaSnapshot(w http.ResponseWriter, r *http.Request, planning *os.File, expected library.MediaFile) {
	if planning == nil {
		s.libraryError(w, r, library.ErrUnavailable)
		return
	}
	if err := planning.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		s.libraryError(w, r, library.ErrUnavailable)
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	file, source, content, err := s.library.OpenOriginalMediaFor(r.Context(), librarySubject(principal, principal.User.ID), expected.Item.ID, expected.SourceID, expected.ETag)
	if err != nil {
		if errors.Is(err, library.ErrBusy) {
			w.Header().Set("Retry-After", "2")
			apiError(w, r, http.StatusTooManyRequests, "stream_limit", "The active original media response limit has been reached.")
		} else if r.Context().Err() == nil {
			s.libraryError(w, r, err)
		}
		return
	}
	if source.Item.ID != expected.Item.ID || source.SourceID != expected.SourceID || source.Item.Type != expected.Item.Type {
		_ = content.Close()
		s.libraryError(w, r, library.ErrSourceChanged)
		return
	}
	s.serveOriginalMedia(w, r, file, source, content)
}

// This writer deliberately promotes no ReaderFrom fast path. It records actual
// body bytes so ServeContent cannot hide a short EOF or failed chunk admission.
type originalPrimaryResponseWriter struct {
	writer  http.ResponseWriter
	written int64
}

func (w *originalPrimaryResponseWriter) Header() http.Header    { return w.writer.Header() }
func (w *originalPrimaryResponseWriter) WriteHeader(status int) { w.writer.WriteHeader(status) }
func (w *originalPrimaryResponseWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if n < 0 || n > len(data) {
		return 0, io.ErrShortWrite
	}
	w.written += int64(n)
	return n, err
}

func originalPrimaryBodyError(r *http.Request, content *primaryio.ReadSeeker, writer *idleResponseWriter, counted *originalPrimaryResponseWriter) error {
	if err := content.Err(); err != nil {
		return err
	}
	if writer.err != nil {
		return writer.err
	}
	if r.Method != http.MethodHead && (writer.status == http.StatusOK || writer.status == http.StatusPartialContent) {
		length, err := strconv.ParseInt(counted.Header().Get("Content-Length"), 10, 64)
		if err != nil || length < 0 || counted.written != length {
			return io.ErrUnexpectedEOF
		}
	}
	return nil
}

// Store lifetime cancels content.Context, which is the guard's parent. This
// reverse bridge also cancels queued reads when the guard's child work is
// revoked. Stopping/joining it never completes actual descriptor ownership.
func bridgeOriginalReadCancellation(work context.Context, content *primaryio.ReadSeeker) func() {
	done := make(chan struct{})
	stop := context.AfterFunc(work, func() { defer close(done); _ = content.Cancel() })
	return func() {
		if !stop() {
			<-done
		}
	}
}

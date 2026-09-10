package server

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/identity"
)

const diagnosticDownloadLifetime = 60 * time.Second

type diagnosticDownloadSnapshot interface {
	io.ReadSeekCloser
	Name() string
	Size() int64
	ModTime() time.Time
}

func (s *Server) adminDiagnosticDownload(w http.ResponseWriter, r *http.Request) {
	principal := observabilityPrincipal(r)
	if err := s.checkObservabilityAdministrator(r.Context(), principal, identity.AdministratorNative); err != nil {
		s.observabilityError(w, r, err, true)
		return
	}
	if !s.sameOrigin(w, r) {
		return
	}
	name, err := diagnosticRequestName(r)
	if err != nil {
		s.observabilityError(w, r, err, true)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("Range")) > 1 || len(r.Header.Get("Range")) > 4096 {
		s.observabilityError(w, r, diagnostics.ErrInvalid, true)
		return
	}
	if s.diagnostics == nil {
		s.observabilityError(w, r, diagnostics.ErrUnavailable, true)
		return
	}
	deadline := time.Now().Add(diagnosticDownloadLifetime)
	if !principal.ExpiresAt.IsZero() && principal.ExpiresAt.Before(deadline) {
		deadline = principal.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	snapshot, readErr := s.diagnostics.Snapshot(ctx, name)
	if snapshot != nil {
		defer snapshot.Close()
	}
	if err := s.checkObservabilityAdministrator(ctx, principal, identity.AdministratorNative); err != nil {
		s.observabilityError(w, r, err, true)
		return
	}
	if readErr != nil {
		s.observabilityError(w, r, readErr, true)
		return
	}
	err = serveDiagnosticSnapshot(w, r.WithContext(ctx), snapshot, func(ctx context.Context) error {
		return s.checkObservabilityAdministrator(ctx, principal, identity.AdministratorNative)
	})
	if err != nil {
		s.observabilityError(w, r, err, true)
	}
}

// serveDiagnosticSnapshot preserves ServeContent's HEAD, conditional request,
// and byte-range semantics while its reader remains pinned to one byte length.
// Cancellation closes the descriptor and interrupts a blocked network write.
// A failed transfer must abort the HTTP stream instead of reporting normal EOF.
func serveDiagnosticSnapshot(w http.ResponseWriter, r *http.Request, snapshot diagnosticDownloadSnapshot, recheck func(context.Context) error) (result error) {
	if snapshot == nil {
		return diagnostics.ErrUnavailable
	}
	if snapshot.Size() < 0 || snapshot.Size() > diagnostics.MaxReadBytes {
		_ = snapshot.Close()
		return diagnostics.ErrUnavailable
	}
	ctx, cancel := context.WithCancelCause(r.Context())
	defer cancel(nil)
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(diagnosticDownloadLifetime)
	if requestDeadline, ok := ctx.Deadline(); ok && requestDeadline.Before(deadline) {
		deadline = requestDeadline
	}
	if err := controller.SetWriteDeadline(deadline); err != nil {
		_ = snapshot.Close()
		return diagnostics.ErrUnavailable
	}
	writer := &diagnosticDownloadWriter{ResponseWriter: w, ctx: ctx, maximum: diagnostics.MaxReadBytes + 1<<20}
	reader := &diagnosticDownloadReader{ReadSeeker: snapshot, ctx: ctx}
	callbackDone := make(chan struct{})
	stopCallback := context.AfterFunc(ctx, func() {
		defer close(callbackDone)
		_ = controller.SetWriteDeadline(time.Now())
		_ = snapshot.Close()
	})
	watchStop, watchDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watchDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchStop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := recheck(ctx); err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	defer func() {
		// Join both users of ResponseController before resetting its deadline.
		// Otherwise a late cancellation callback could poison a reused connection.
		close(watchStop)
		<-watchDone
		if !stopCallback() {
			<-callbackDone
		}
		closeErr := snapshot.Close()
		_ = controller.SetWriteDeadline(time.Time{})
		if result == nil {
			result = closeErr
		}
		if cause := context.Cause(ctx); result == nil && cause != nil {
			result = cause
		}
		if result != nil && writer.status != 0 {
			panic(http.ErrAbortHandler)
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/x-ndjson")
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": snapshot.Name()}))
	http.ServeContent(writer, r.WithContext(ctx), snapshot.Name(), snapshot.ModTime(), reader)
	if writer.err != nil {
		return writer.err
	}
	if reader.err != nil {
		return reader.err
	}
	if r.Method != http.MethodHead && (writer.status == http.StatusOK || writer.status == http.StatusPartialContent) {
		length, err := strconv.ParseInt(w.Header().Get("Content-Length"), 10, 64)
		if err != nil || writer.written != length {
			return io.ErrUnexpectedEOF
		}
	}
	// Flush headers and the final buffered bytes while the authorization watcher
	// and write deadline still belong to this handler, not after it returns.
	if err := controller.Flush(); err != nil {
		return err
	}
	return nil
}

// These wrappers intentionally omit ReaderFrom and WriterTo so every transfer
// crosses the cancellation and length checks even when io.Copy optimizes I/O.
type diagnosticDownloadWriter struct {
	http.ResponseWriter
	ctx     context.Context
	maximum int64
	written int64
	status  int
	err     error
}

func (w *diagnosticDownloadWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *diagnosticDownloadWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	// ServeContent removes cache headers on errors such as an invalid range.
	// Diagnostic responses remain private on those paths as well.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *diagnosticDownloadWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		w.err = err
		return 0, err
	}
	if int64(len(data)) > w.maximum-w.written {
		w.err = diagnostics.ErrUnavailable
		return 0, w.err
	}
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.written += int64(n)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
	}
	return n, err
}

type diagnosticDownloadReader struct {
	io.ReadSeeker
	ctx context.Context
	err error
}

func (r *diagnosticDownloadReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		r.err = err
		return 0, err
	}
	n, err := r.ReadSeeker.Read(data)
	if err != nil && !errors.Is(err, io.EOF) {
		r.err = err
	}
	return n, err
}

func (r *diagnosticDownloadReader) Seek(offset int64, whence int) (int64, error) {
	if err := r.ctx.Err(); err != nil {
		r.err = err
		return 0, err
	}
	position, err := r.ReadSeeker.Seek(offset, whence)
	if err != nil {
		r.err = err
	}
	return position, err
}

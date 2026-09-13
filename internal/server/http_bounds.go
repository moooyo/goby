package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	requestBodyLifetime = 15 * time.Second
	mediaWriteIdle      = 30 * time.Second
	mediaWriteChunk     = 32 * 1024
)

// Normal API bodies are small, byte-bounded messages. Start their time budget
// on the first read, after the route's origin, authentication and rate checks.
// Backup routes retain their independent JSON and long-upload body owners.
func withBoundedRequestBodies(next http.Handler) http.Handler {
	return boundedRequestBodies(next, requestBodyLifetime)
}

func boundedRequestBodies(next http.Handler, lifetime time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody || separateBackupBody(r) {
			next.ServeHTTP(w, r)
			return
		}
		body := &boundedRequestBody{body: r.Body, controller: http.NewResponseController(w), parent: r.Context(), lifetime: lifetime}
		copy := r.WithContext(r.Context())
		copy.Body = body
		response := &boundedBodyResponse{ResponseWriter: w, body: body, http1: r.ProtoMajor == 1}
		defer body.finish()
		next.ServeHTTP(response, copy)
	})
}

func separateBackupBody(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/admin/v1/") || strings.Contains(strings.ToLower(r.URL.EscapedPath()), "%2f") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/admin/v1/"), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	// Exempt only actual backup handlers. Unmatched methods, descendant paths,
	// and mux-cleaning redirects still need ordinary unread-body retirement.
	switch parts[0] {
	case "backups":
		if len(parts) == 1 {
			return read || r.Method == http.MethodPost
		}
		if len(parts) == 2 {
			return read || r.Method == http.MethodDelete || r.Method == http.MethodPost && parts[1] == "import"
		}
		return len(parts) == 3 && read && parts[2] == "file"
	case "backup-operations":
		return read && (len(parts) == 1 || len(parts) == 2) ||
			r.Method == http.MethodPost && len(parts) == 3 && parts[2] == "cancel"
	case "restores":
		return r.Method == http.MethodPost && (len(parts) == 2 && (parts[1] == "plans" || parts[1] == "rollback") ||
			len(parts) == 3 && parts[2] == "apply")
	default:
		return false
	}
}

type boundedBodyResponse struct {
	http.ResponseWriter
	body         *boundedRequestBody
	http1        bool
	responseOnce sync.Once
}

func (w *boundedBodyResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *boundedBodyResponse) WriteHeader(status int) {
	if status >= 200 {
		w.prepareResponse()
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *boundedBodyResponse) Write(data []byte) (int, error) {
	w.prepareResponse()
	return w.ResponseWriter.Write(data)
}

func (w *boundedBodyResponse) prepareResponse() {
	w.responseOnce.Do(func() {
		if !w.body.complete() {
			// Do not drain or Close the raw body here: a failed connection read
			// cancels net/http's request context and can abort the authorized
			// response before its first flush. Connection: close suppresses the
			// normal response-time drain; the outer owner closes after return.
			if w.http1 {
				w.Header().Set("Connection", "close")
				// HTTP/1 must not consume the original body behind this wrapper
				// while flushing a response. HTTP/2 already permits duplex I/O;
				// Connection: close there would unnecessarily retire all streams.
				_ = http.NewResponseController(w.ResponseWriter).EnableFullDuplex()
			}
			w.body.stopReading()
		}
	})
}

func (w *boundedBodyResponse) Flush() { _ = w.FlushError() }

func (w *boundedBodyResponse) FlushError() error {
	// ResponseController must not unwrap past unread-body retirement when a
	// handler flushes its headers without calling Write or WriteHeader first.
	w.prepareResponse()
	return http.NewResponseController(w.ResponseWriter).Flush()
}

type boundedRequestBody struct {
	body       io.ReadCloser
	controller *http.ResponseController
	parent     context.Context
	lifetime   time.Duration
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	stop       func() bool
	callback   chan struct{}
	eof        bool
	closed     bool
	watchOnce  sync.Once
	closeOnce  sync.Once
	rawOnce    sync.Once
	closeErr   error
}

func (b *boundedRequestBody) complete() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.eof
}

// EOF retires the read deadline before application/database work continues.
// In particular, a timer must not expire net/http's subsequent keep-alive read.
func (b *boundedRequestBody) Read(data []byte) (int, error) {
	b.mu.Lock()
	if b.eof {
		b.mu.Unlock()
		return 0, io.EOF
	}
	if b.closed {
		var err error = io.ErrClosedPipe
		if b.ctx != nil && b.ctx.Err() != nil {
			err = b.ctx.Err()
		}
		b.mu.Unlock()
		return 0, err
	}
	if b.ctx == nil {
		b.ctx, b.cancel = context.WithTimeout(b.parent, b.lifetime)
		deadline, _ := b.ctx.Deadline()
		if err := supportedDeadline(b.controller.SetReadDeadline(deadline)); err != nil {
			b.cancel()
			b.mu.Unlock()
			return 0, err
		}
		b.callback = make(chan struct{})
		b.stop = context.AfterFunc(b.ctx, func() {
			defer close(b.callback)
			b.interrupt()
		})
	}
	ctx := b.ctx
	b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	n, err := b.body.Read(data)
	if errors.Is(err, io.EOF) {
		b.mu.Lock()
		b.eof = true
		if !b.closed {
			_ = b.controller.SetReadDeadline(time.Time{})
		}
		b.mu.Unlock()
		b.stopWatch()
	} else {
		var timeout interface{ Timeout() bool }
		deadline, _ := ctx.Deadline()
		if errors.As(err, &timeout) && timeout.Timeout() && !time.Now().Before(deadline) {
			// net/http cancels the request context when its read deadline fires.
			// Preserve this owner's elapsed body budget as the more precise cause.
			err = context.DeadlineExceeded
		} else if cause := ctx.Err(); cause != nil {
			err = cause
		}
	}
	return n, err
}

func (b *boundedRequestBody) closeRaw() {
	b.rawOnce.Do(func() { b.closeErr = b.body.Close() })
}

func (b *boundedRequestBody) interrupt() {
	b.mu.Lock()
	if b.eof {
		b.mu.Unlock()
		return
	}
	b.closed = true
	_ = b.controller.SetReadDeadline(time.Now())
	b.mu.Unlock()
	// Expire the connection read before Close, which may wait for its read lock.
	// Close also interrupts well-behaved synthetic bodies without a connection.
	b.closeRaw()
}

func (b *boundedRequestBody) stopWatch() {
	b.watchOnce.Do(func() {
		b.mu.Lock()
		stop, done, cancel := b.stop, b.callback, b.cancel
		b.mu.Unlock()
		if stop != nil && !stop() {
			<-done
		}
		if cancel != nil {
			cancel()
		}
	})
}

func (b *boundedRequestBody) stopReading() {
	// Fence a concurrent first Read before snapshotting the callback. No newly
	// installed timer may survive cleanup and touch a later request.
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.stopWatch()
	b.mu.Lock()
	if !b.eof {
		_ = b.controller.SetReadDeadline(time.Now())
	}
	b.mu.Unlock()
}

func (b *boundedRequestBody) Close() error {
	b.closeOnce.Do(func() {
		b.stopReading()
		b.closeRaw()
		// An incomplete body retains its interruption deadline for the rest of
		// the handler. Only the outer owner clears it after response work ends.
		if b.complete() {
			_ = b.controller.SetReadDeadline(time.Time{})
		}
	})
	return b.closeErr
}

func (b *boundedRequestBody) finish() {
	_ = b.Close()
	// Close has joined the timer and raw body before this final reset. A
	// completed body remains reusable; an unread body's connection is closing.
	_ = b.controller.SetReadDeadline(time.Time{})
}

// Memory-only ResponseRecorders have no connection deadline. Production's
// net/http transport and wrappers exposing Unwrap support these operations.
// Preserve recorder-based contract tests while propagating real transport errors.
func supportedDeadline(err error) error {
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

// idleResponseWriter has no ReaderFrom fast path: every bounded chunk refreshes
// an idle deadline. An active transfer has no fixed movie-length time limit.
type idleResponseWriter struct {
	http.ResponseWriter
	ctx        context.Context
	controller *http.ResponseController
	idle       time.Duration
	mu         sync.Mutex
	stop       func() bool
	callback   chan struct{}
	status     int
	err        error
}

func newIdleResponseWriter(w http.ResponseWriter, ctx context.Context, idle time.Duration) (*idleResponseWriter, error) {
	writer := &idleResponseWriter{ResponseWriter: w, ctx: ctx, controller: http.NewResponseController(w), idle: idle, callback: make(chan struct{})}
	if err := writer.arm(); err != nil {
		return nil, err
	}
	writer.stop = context.AfterFunc(ctx, func() {
		defer close(writer.callback)
		writer.mu.Lock()
		_ = writer.controller.SetWriteDeadline(time.Now())
		writer.mu.Unlock()
	})
	return writer, nil
}

func (w *idleResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *idleResponseWriter) arm() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(w.idle)
	if bound, ok := w.ctx.Deadline(); ok && bound.Before(deadline) {
		deadline = bound
	}
	return supportedDeadline(w.controller.SetWriteDeadline(deadline))
}

func (w *idleResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if err := w.arm(); err != nil {
		w.err = err
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *idleResponseWriter) Write(data []byte) (int, error) {
	total := 0
	for len(data) > 0 {
		if err := w.arm(); err != nil {
			w.err = err
			return total, err
		}
		if w.status == 0 {
			w.WriteHeader(http.StatusOK)
			if w.err != nil {
				return total, w.err
			}
		}
		chunk := data[:min(len(data), mediaWriteChunk)]
		n, err := w.ResponseWriter.Write(chunk)
		total += n
		if err == nil && n != len(chunk) {
			err = io.ErrShortWrite
		}
		if err != nil {
			w.err = err
			return total, err
		}
		data = data[n:]
	}
	return total, nil
}

func (w *idleResponseWriter) FlushError() error {
	if w.err != nil {
		return w.err
	}
	if err := w.arm(); err != nil {
		w.err = err
		return err
	}
	if err := w.controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		w.err = err
		return err
	}
	return nil
}

// Flush the final net/http buffer while this handler still owns the deadline.
// A truncated transfer aborts its HTTP stream instead of reporting normal EOF.
func (w *idleResponseWriter) finish() {
	err := w.FlushError()
	if !w.stop() {
		<-w.callback
	}
	_ = w.controller.SetWriteDeadline(time.Time{})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}

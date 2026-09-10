package server

import (
	"bufio"
	"context"
	"log/slog"
	"math"
	"net"
	"net/http"
	"time"
)

// requestLogWriter counts only bytes accepted by ResponseWriter. Bytes written
// directly after Hijack belong to the upgraded protocol and are not included.
type requestLogWriter struct {
	writer   http.ResponseWriter
	status   int
	bytes    int64
	aborted  bool
	failed   bool
	hijacked bool
}

func (w *requestLogWriter) Header() http.Header { return w.writer.Header() }

// Unwrap preserves ResponseController operations, including write deadlines.
func (w *requestLogWriter) Unwrap() http.ResponseWriter { return w.writer }

func (w *requestLogWriter) WriteHeader(status int) {
	// Let the underlying writer validate codes and preserve its normal handling
	// of repeated headers. Informational responses do not commit a final code.
	w.writer.WriteHeader(status)
	if w.status == 0 && !w.hijacked && (status >= 200 || status == http.StatusSwitchingProtocols) {
		w.status = status
	}
}

func (w *requestLogWriter) Write(data []byte) (int, error) {
	if w.status == 0 && !w.hijacked {
		w.status = http.StatusOK
	}
	n, err := w.writer.Write(data)
	if n > 0 {
		if int64(n) > math.MaxInt64-w.bytes {
			w.bytes = math.MaxInt64
		} else {
			w.bytes += int64(n)
		}
	}
	if err != nil {
		w.failed = true
	}
	return n, err
}

func (w *requestLogWriter) Flush() { _ = w.FlushError() }

// FlushError must be present in addition to Flush. ResponseController prefers
// it and can therefore observe transport and deadline failures without losing
// the error through the legacy http.Flusher interface.
func (w *requestLogWriter) FlushError() error {
	err := http.NewResponseController(w.writer).Flush()
	if err != nil {
		w.failed = true
	} else if w.status == 0 && !w.hijacked {
		w.status = http.StatusOK
	}
	return err
}

func (w *requestLogWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	connection, buffer, err := http.NewResponseController(w.writer).Hijack()
	if err == nil {
		w.hijacked = true
		if w.status == 0 {
			w.status = http.StatusSwitchingProtocols
		}
	}
	return connection, buffer, err
}

func (w *requestLogWriter) markAborted() { w.aborted = true }

// beginRequestLogging is called after middleware creates its request context,
// but before any early response or recovery defer. The returned completion
// function must itself be deferred so escaped panic values are rethrown intact.
func (s *Server) beginRequestLogging(w http.ResponseWriter, r *http.Request) (*requestLogWriter, func()) {
	response := &requestLogWriter{writer: w}
	started := time.Now()
	return response, func() {
		recovered := recover()
		if recovered != nil {
			response.markAborted()
		}
		outcome := "completed"
		switch {
		case response.aborted:
			outcome = "aborted"
		case r.Context().Err() != nil:
			outcome = "cancelled"
		case response.failed:
			outcome = "aborted"
		}
		status := response.status
		if status == 0 && outcome != "aborted" {
			status = http.StatusOK
		}
		route := r.Pattern
		if route == "" {
			route = "unmatched"
			if isSocketRequest(r) {
				route = "websocket"
			} else if r.Method == http.MethodOptions {
				route = "cors"
			}
		}
		attrs := []slog.Attr{
			slog.String("route", route), slog.Int64("duration_ms", time.Since(started).Milliseconds()),
			slog.Int64("bytes", response.bytes), slog.String("outcome", outcome),
		}
		if id, ok := r.Context().Value(requestIDKey).(string); ok {
			attrs = append(attrs, slog.String("request_id", id))
		}
		if status != 0 {
			attrs = append(attrs, slog.Int("status", status))
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
			http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
			attrs = append(attrs, slog.String("method", r.Method))
		}
		if s.log != nil {
			// Request cancellation must not suppress its final diagnostic record.
			// No request context, URL, headers, body, or error text reaches logging.
			s.log.LogAttrs(context.Background(), slog.LevelInfo, "request completed", attrs...)
		}
		if recovered != nil {
			panic(recovered)
		}
	}
}

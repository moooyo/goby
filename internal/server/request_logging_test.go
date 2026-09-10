package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/diagnostics"
)

func requestLoggingServer() (*Server, *bytes.Buffer) {
	output := new(bytes.Buffer)
	return &Server{log: slog.New(diagnostics.NewHandler(nil, slog.NewJSONHandler(output, nil)))}, output
}

func requestLoggingCompletion(t *testing.T, output *bytes.Buffer) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	var completion map[string]any
	for {
		var record map[string]any
		if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if record["event"] == "request.completed" {
			if completion != nil {
				t.Fatal("request completion was logged more than once")
			}
			completion = record
		}
	}
	if completion == nil {
		t.Fatal("request completion was not logged")
	}
	return completion
}

func TestRequestLoggingMiddlewareUsesPatternAndAcceptedBytes(t *testing.T) {
	server, output := requestLoggingServer()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /emby/Items/{Id}/PlaybackInfo", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "response \u4e2d\u6587")
	})
	request := httptest.NewRequest(http.MethodGet, "/emby/Items/path-secret/PlaybackInfo?api_key=query-secret", strings.NewReader("body-secret"))
	request.Header.Set("Authorization", "Bearer header-secret")
	response := httptest.NewRecorder()
	server.middleware(mux).ServeHTTP(response, request)
	completion := requestLoggingCompletion(t, output)
	if completion["route"] != "GET /emby/Items/{Id}/PlaybackInfo" || completion["method"] != http.MethodGet ||
		completion["status"] != float64(http.StatusCreated) || completion["bytes"] != float64(response.Body.Len()) || completion["outcome"] != "completed" {
		t.Fatalf("incorrect request completion: %#v", completion)
	}
	if completion["request_id"] != response.Header().Get("X-Request-Id") || response.Header().Get("X-Request-Id") == "" {
		t.Fatal("request completion did not use the generated request identity")
	}
	if duration, ok := completion["duration_ms"].(float64); !ok || duration < 0 {
		t.Fatal("request duration is missing or negative")
	}
	for _, secret := range []string{"path-secret", "query-secret", "header-secret", "body-secret"} {
		if strings.Contains(output.String(), secret) {
			t.Fatal("request diagnostics exposed request content")
		}
	}
}

func TestRequestLoggingMiddlewareCoversPreflightEarlyReturn(t *testing.T) {
	server, output := requestLoggingServer()
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("preflight reached the router") })
	request := httptest.NewRequest(http.MethodOptions, "/emby/Items?api_key=query-secret", nil)
	request.Header.Set("Origin", "https://client.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	response := httptest.NewRecorder()
	server.middleware(next).ServeHTTP(response, request)
	completion := requestLoggingCompletion(t, output)
	if completion["route"] != "cors" || completion["status"] != float64(http.StatusOK) || completion["bytes"] != float64(0) {
		t.Fatalf("preflight completion: %#v", completion)
	}
}

func TestRequestLoggingMiddlewarePreservesPanicRecoveryAndAbort(t *testing.T) {
	for _, abort := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovered panic", true: "abort handler"}[abort], func(t *testing.T) {
			server, output := requestLoggingServer()
			mux := http.NewServeMux()
			mux.HandleFunc("GET /healthz", func(http.ResponseWriter, *http.Request) {
				if abort {
					panic(http.ErrAbortHandler)
				}
				panic("panic-secret")
			})
			response := httptest.NewRecorder()
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				server.middleware(mux).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			}()
			completion := requestLoggingCompletion(t, output)
			if completion["outcome"] != "aborted" || strings.Contains(output.String(), "panic-secret") {
				t.Fatalf("panic was not safely recorded: %#v", completion)
			}
			if abort {
				if recovered != http.ErrAbortHandler {
					t.Fatal("ErrAbortHandler was not rethrown intact")
				}
				if _, exists := completion["status"]; exists || completion["bytes"] != float64(0) {
					t.Fatal("uncommitted abort invented an HTTP response")
				}
			} else if recovered != nil || response.Code != http.StatusInternalServerError ||
				completion["status"] != float64(http.StatusInternalServerError) || completion["bytes"] != float64(response.Body.Len()) {
				t.Fatalf("normal panic recovery changed: recovered=%v response=%d record=%#v", recovered, response.Code, completion)
			}
		})
	}
}

type requestLoggingWriter struct {
	header        http.Header
	headers       []int
	limit         int
	writeErr      error
	flushErr      error
	flushCalls    int
	writeDeadline time.Time
	hijackConn    net.Conn
	hijackBuffer  *bufio.ReadWriter
	hijackErr     error
}

func (w *requestLoggingWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *requestLoggingWriter) WriteHeader(status int) { w.headers = append(w.headers, status) }
func (w *requestLoggingWriter) Write(data []byte) (int, error) {
	n := len(data)
	if w.limit > 0 && n > w.limit {
		n = w.limit
	}
	return n, w.writeErr
}
func (w *requestLoggingWriter) FlushError() error {
	w.flushCalls++
	return w.flushErr
}
func (w *requestLoggingWriter) SetWriteDeadline(deadline time.Time) error {
	w.writeDeadline = deadline
	return nil
}
func (w *requestLoggingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijackConn, w.hijackBuffer, w.hijackErr
}

type requestLoggingUnwrapper struct{ http.ResponseWriter }

func (w requestLoggingUnwrapper) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func TestRequestLogWriterInformationalHeadersAndPartialWrite(t *testing.T) {
	server, output := requestLoggingServer()
	wantErr := errors.New("transport-secret")
	base := &requestLoggingWriter{limit: 3, writeErr: wantErr}
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Pattern = "GET /healthz"
	response, complete := server.beginRequestLogging(base, request)
	response.WriteHeader(http.StatusEarlyHints)
	response.WriteHeader(http.StatusProcessing)
	if response.status != 0 {
		t.Fatal("informational headers committed a final status")
	}
	response.WriteHeader(http.StatusPartialContent)
	response.WriteHeader(http.StatusInternalServerError)
	n, err := response.Write([]byte("abcdef"))
	if n != 3 || err != wantErr {
		t.Fatal("partial write result was not passed through")
	}
	complete()
	completion := requestLoggingCompletion(t, output)
	if completion["status"] != float64(http.StatusPartialContent) || completion["bytes"] != float64(3) || completion["outcome"] != "aborted" {
		t.Fatalf("partial response completion: %#v", completion)
	}
	if strings.Contains(output.String(), "transport-secret") {
		t.Fatal("write error text entered request diagnostics")
	}
}

func TestRequestLogWriterFlushErrorAndResponseControllerUnwrap(t *testing.T) {
	server, output := requestLoggingServer()
	wantErr := errors.New("flush-secret")
	base := &requestLoggingWriter{flushErr: wantErr}
	inner := requestLoggingUnwrapper{ResponseWriter: base}
	request := httptest.NewRequest(http.MethodGet, "/admin/v1/logs/file-secret/download", nil)
	request.Pattern = "GET /admin/v1/logs/{name}/download"
	response, complete := server.beginRequestLogging(inner, request)
	if response.Unwrap() != inner {
		t.Fatal("ResponseWriter unwrap did not preserve its immediate delegate")
	}
	response.WriteHeader(http.StatusPartialContent)
	controller := http.NewResponseController(response)
	deadline := time.Now().Add(time.Second)
	if err := controller.SetWriteDeadline(deadline); err != nil || !base.writeDeadline.Equal(deadline) {
		t.Fatalf("write deadline did not reach the underlying writer: %v", err)
	}
	if err := controller.Flush(); err != wantErr || base.flushCalls != 1 {
		t.Fatal("ResponseController swallowed the underlying FlushError result")
	}
	response.Flush()
	if base.flushCalls != 2 {
		t.Fatal("legacy Flush did not delegate to FlushError")
	}
	complete()
	completion := requestLoggingCompletion(t, output)
	if completion["outcome"] != "aborted" || completion["status"] != float64(http.StatusPartialContent) {
		t.Fatalf("failed flush completion: %#v", completion)
	}
	if strings.Contains(output.String(), "flush-secret") || strings.Contains(output.String(), "file-secret") {
		t.Fatal("flush diagnostics exposed request or error data")
	}
}

func TestRequestLogWriterFlushCommitsImplicitOK(t *testing.T) {
	server, output := requestLoggingServer()
	base := &requestLoggingWriter{}
	response, complete := server.beginRequestLogging(base, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if err := response.FlushError(); err != nil {
		t.Fatal(err)
	}
	response.WriteHeader(http.StatusInternalServerError)
	complete()
	if completion := requestLoggingCompletion(t, output); completion["status"] != float64(http.StatusOK) || completion["outcome"] != "completed" {
		t.Fatalf("flushed response did not retain its initial OK status: %#v", completion)
	}
}

func TestRequestLogWriterPreservesHijackAndSwitchingProtocols(t *testing.T) {
	server, output := requestLoggingServer()
	connection, peer := net.Pipe()
	defer connection.Close()
	defer peer.Close()
	buffer := bufio.NewReadWriter(bufio.NewReader(connection), bufio.NewWriter(connection))
	base := &requestLoggingWriter{hijackConn: connection, hijackBuffer: buffer}
	request := httptest.NewRequest(http.MethodGet, "/emby/socket?api_key=socket-secret", nil)
	request.Header.Set("Upgrade", "websocket")
	response, complete := server.beginRequestLogging(requestLoggingUnwrapper{ResponseWriter: base}, request)
	gotConnection, gotBuffer, err := http.NewResponseController(response).Hijack()
	if err != nil || gotConnection != connection || gotBuffer != buffer {
		t.Fatal("Hijack did not preserve the underlying connection and buffer")
	}
	complete()
	completion := requestLoggingCompletion(t, output)
	if completion["route"] != "websocket" || completion["status"] != float64(http.StatusSwitchingProtocols) || completion["bytes"] != float64(0) {
		t.Fatalf("upgraded response completion: %#v", completion)
	}
	if strings.Contains(output.String(), "socket-secret") {
		t.Fatal("WebSocket diagnostics exposed the credential")
	}
}

func TestRequestLogWriterPreservesUnsupportedControllerOperations(t *testing.T) {
	base := &requestLoggingBasicWriter{}
	response := &requestLogWriter{writer: base}
	if err := http.NewResponseController(response).Flush(); !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("unsupported Flush changed error semantics: %v", err)
	}
	if _, _, err := http.NewResponseController(response).Hijack(); !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("unsupported Hijack changed error semantics: %v", err)
	}
}

type requestLoggingBasicWriter struct{}

func (*requestLoggingBasicWriter) Header() http.Header            { return make(http.Header) }
func (*requestLoggingBasicWriter) WriteHeader(int)                {}
func (*requestLoggingBasicWriter) Write(data []byte) (int, error) { return len(data), nil }

type requestLoggingContextProbe struct {
	slog.Handler
	contextErr error
}

func (h *requestLoggingContextProbe) Handle(ctx context.Context, record slog.Record) error {
	h.contextErr = ctx.Err()
	return h.Handler.Handle(ctx, record)
}

func TestRequestLoggingCancellationUsesLiveLoggingContext(t *testing.T) {
	output := new(bytes.Buffer)
	probe := &requestLoggingContextProbe{Handler: slog.NewJSONHandler(output, nil)}
	server := &Server{log: slog.New(diagnostics.NewHandler(nil, probe))}
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil).WithContext(ctx)
	request.Pattern = "GET /healthz"
	_, complete := server.beginRequestLogging(httptest.NewRecorder(), request)
	cancel()
	complete()
	completion := requestLoggingCompletion(t, output)
	if completion["outcome"] != "cancelled" || probe.contextErr != nil {
		t.Fatal("request cancellation suppressed or changed its completion record")
	}
}

func TestRequestLoggingOmitsArbitraryMethodAndUnmatchedURL(t *testing.T) {
	server, output := requestLoggingServer()
	request := httptest.NewRequest("METHOD_SECRET", "/path-secret?key=query-secret", nil)
	_, complete := server.beginRequestLogging(httptest.NewRecorder(), request)
	complete()
	completion := requestLoggingCompletion(t, output)
	if completion["route"] != "unmatched" {
		t.Fatalf("unknown request did not use a fixed route classification: %#v", completion)
	}
	if _, exists := completion["method"]; exists || strings.Contains(output.String(), "SECRET") || strings.Contains(output.String(), "secret") {
		t.Fatal("unknown method or URL entered request diagnostics")
	}
}

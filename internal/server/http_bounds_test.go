package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestBoundedRequestBodiesInterruptStalledReadsAndClearDeadlines(t *testing.T) {
	for _, reason := range []string{"request cancellation", "body lifetime"} {
		t.Run(reason, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const lifetime = 5 * time.Second
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				body := newBackupTestLockedBody()
				response := &backupTestBodyDeadline{ResponseRecorder: httptest.NewRecorder(), body: body}
				request := httptest.NewRequest(http.MethodPost, "/emby/Items/owned/PlaybackInfo", nil).WithContext(ctx)
				request.Body, request.ContentLength = body, -1
				finished := make(chan error, 1)
				go func() {
					var readErr error
					handler := boundedRequestBodies(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
						_, readErr = io.ReadAll(r.Body)
					}), lifetime)
					handler.ServeHTTP(response, request)
					finished <- readErr
				}()
				<-body.entered
				if reason == "request cancellation" {
					cancel()
				} else {
					time.Sleep(lifetime)
				}
				err := <-finished
				if reason == "request cancellation" && !errors.Is(err, context.Canceled) {
					t.Fatalf("request cancellation returned %v", err)
				}
				if reason == "body lifetime" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("stalled body did not return a deadline failure: %v", err)
				}
				time.Sleep(2 * lifetime)
				response.mu.Lock()
				reset := response.last.IsZero()
				response.mu.Unlock()
				if !body.closed.Load() || !reset {
					t.Fatal("stalled body retained its read lock, descriptor, or deadline owner")
				}
			})
		})
	}
}

func TestBoundedRequestBodiesStartOnReadAndRetireAtEOF(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const lifetime = time.Second
		response := &backupTestCompletionResponse{ResponseRecorder: httptest.NewRecorder()}
		request := httptest.NewRequest(http.MethodPost, "/admin/v1/session", strings.NewReader(`{"complete":true}`))
		handler := boundedRequestBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * lifetime)
			response.mu.Lock()
			premature := !response.last.IsZero()
			response.mu.Unlock()
			if premature {
				t.Fatal("body lifetime started before the first body read")
			}
			data, err := io.ReadAll(r.Body)
			if err != nil || string(data) != `{"complete":true}` {
				t.Fatalf("complete body failed after delayed admission: %v", err)
			}
			if n, err := r.Body.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
				t.Fatal("a second completed-body read did not retain EOF")
			}
			if err := r.Body.Close(); err != nil {
				t.Fatalf("close completed body: %v", err)
			}
			time.Sleep(2 * lifetime)
			if r.Context().Err() != nil {
				t.Fatal("completed body invalidated subsequent application work")
			}
			w.WriteHeader(http.StatusAccepted)
		}), lifetime)
		handler.ServeHTTP(response, request)
		response.mu.Lock()
		expired, reset := response.expired, response.last.IsZero()
		response.mu.Unlock()
		if response.Code != http.StatusAccepted || response.Header().Get("Connection") == "close" || expired != 0 || !reset {
			t.Fatalf("EOF cleanup changed the response or expired a reusable connection: expired=%d, reset=%t", expired, reset)
		}
	})
}

func TestBoundedRequestBodiesPreserveBackupBodyOwnership(t *testing.T) {
	for _, path := range []string{"/admin/v1/backups", "/admin/v1/backups/import", "/admin/v1/backup-operations/owned/cancel", "/admin/v1/restores/plans"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader("owned backup body"))
			original := request.Body
			handler := boundedRequestBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Body != original {
					t.Fatal("normal API lifetime replaced the independent backup body owner")
				}
				w.WriteHeader(http.StatusAccepted)
			}), time.Nanosecond)
			handler.ServeHTTP(httptest.NewRecorder(), request)
		})
	}
}

func TestBoundedRequestBodiesLoopbackCompletesAndReusesConnection(t *testing.T) {
	for _, framing := range []string{"content length", "chunked"} {
		t.Run(framing, func(t *testing.T) {
			const lifetime = 100 * time.Millisecond
			var accepted atomic.Int32
			completed := make(chan error, 1)
			handler := boundedRequestBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/next" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				data, err := io.ReadAll(r.Body)
				if err == nil && string(data) != `{"complete":true}` {
					err = errors.New("completed request lost its body bytes")
				}
				if err == nil {
					select {
					case <-time.After(2 * lifetime):
					case <-r.Context().Done():
						err = r.Context().Err()
					}
				}
				completed <- err
				w.WriteHeader(http.StatusAccepted)
			}), lifetime)
			server := httptest.NewUnstartedServer(handler)
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					accepted.Add(1)
				}
			}
			server.Start()
			t.Cleanup(server.Close)
			transport := &http.Transport{MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1}
			t.Cleanup(transport.CloseIdleConnections)
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			request, err := http.NewRequest(http.MethodPost, server.URL+"/body", strings.NewReader(`{"complete":true}`))
			if err != nil {
				t.Fatal("construct the bounded loopback body")
			}
			if framing == "chunked" {
				request.ContentLength = -1
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatalf("complete loopback body transport: %v", err)
			}
			_, readErr := io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if response.StatusCode != http.StatusAccepted || response.Close || readErr != nil || closeErr != nil {
				t.Fatal("completed body did not produce a reusable HTTP response")
			}
			if err := <-completed; err != nil {
				t.Fatalf("body EOF did not retire its lifetime before application work: %v", err)
			}
			var reused atomic.Bool
			trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused.Store(info.Reused) }}
			next, err := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), http.MethodGet, server.URL+"/next", nil)
			if err != nil {
				t.Fatal("construct the keep-alive follow-up")
			}
			response, err = client.Do(next)
			if err != nil {
				t.Fatalf("keep-alive follow-up transport: %v", err)
			}
			_, readErr = io.Copy(io.Discard, response.Body)
			closeErr = response.Body.Close()
			if response.StatusCode != http.StatusNoContent || readErr != nil || closeErr != nil || !reused.Load() || accepted.Load() != 1 {
				t.Fatalf("completed body poisoned keep-alive: connections=%d, reused=%t", accepted.Load(), reused.Load())
			}
		})
	}
}

func TestBoundedRequestBodiesLoopbackStalledAndUnreadBodiesFinish(t *testing.T) {
	for _, consume := range []bool{false, true} {
		name := "early rejection"
		if consume {
			name = "stalled body"
		}
		t.Run(name, func(t *testing.T) {
			readResult := make(chan error, 1)
			status := http.StatusUnauthorized
			if consume {
				status = http.StatusRequestTimeout
			}
			server := httptest.NewServer(boundedRequestBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if consume {
					_, err := io.ReadAll(r.Body)
					readResult <- err
				}
				w.WriteHeader(status)
			}), 100*time.Millisecond))
			t.Cleanup(server.Close)
			connection, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal("connect the incomplete loopback body")
			}
			t.Cleanup(func() { _ = connection.Close() })
			if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal("bound the incomplete loopback connection")
			}
			_, err = io.WriteString(connection, "POST /body HTTP/1.1\r\nHost: goby.example.test\r\nContent-Length: 1024\r\nContent-Type: application/json\r\n\r\n{")
			if err != nil {
				t.Fatal("send incomplete loopback body headers")
			}
			response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodPost})
			if err != nil {
				t.Fatalf("incomplete request blocked its response: %v", err)
			}
			_, readErr := io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if response.StatusCode != status || !response.Close || readErr != nil || closeErr != nil {
				t.Fatal("incomplete request did not complete and close its HTTP connection")
			}
			if consume {
				if err := <-readResult; err == nil {
					t.Fatal("incomplete body reached ordinary EOF")
				}
			}
		})
	}
}

func TestBoundedRequestBodiesDoNotExemptUnmatchedBackupRoutes(t *testing.T) {
	for _, target := range []struct{ method, path string }{
		{http.MethodPost, "/admin/v1/backups/a/unknown"},
		{http.MethodPut, "/admin/v1/backups"},
		{http.MethodPost, "/admin/v1/backups//unknown"},
		{http.MethodPost, "/admin/v1/backups/"},
		{http.MethodPost, "/admin%2Fv1/backups/import"},
	} {
		t.Run(target.method+target.path, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("POST /admin/v1/backups/import", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
			})
			mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
			server := httptest.NewServer(boundedRequestBodies(mux, time.Second))
			t.Cleanup(server.Close)
			connection, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal("connect the unmatched backup body")
			}
			t.Cleanup(func() { _ = connection.Close() })
			if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal("bound the unmatched backup connection")
			}
			_, err = io.WriteString(connection, target.method+" "+target.path+" HTTP/1.1\r\nHost: goby.example.test\r\nContent-Length: 1024\r\nContent-Type: application/json\r\n\r\n{")
			if err != nil {
				t.Fatal("send unmatched backup request")
			}
			response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: target.method})
			if err != nil {
				t.Fatalf("unmatched backup request blocked body retirement: %v", err)
			}
			_, readErr := io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if response.StatusCode == http.StatusAccepted || !response.Close || readErr != nil || closeErr != nil {
				t.Fatal("unmatched backup route bypassed ordinary body retirement")
			}
		})
	}
}

func TestBoundedRequestBodiesUnreadGETPreservesResponseContext(t *testing.T) {
	for _, guarded := range []bool{false, true} {
		for _, output := range []string{"write", "flush first", "reader copy"} {
			name := "ordinary/" + output
			if guarded {
				name = "observability/" + output
			}
			t.Run(name, func(t *testing.T) {
				const payload = "authorized response without reading the GET body"
				result := make(chan error, 1)
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Model a response that must abort as soon as its authenticated
					// request loses authorization or its connection context ends.
					writer, err := newIdleResponseWriter(w, r.Context(), time.Second)
					if err != nil {
						result <- err
						return
					}
					defer writer.finish()
					w.Header().Set("Content-Type", "text/plain")
					if output == "flush first" {
						err = http.NewResponseController(w).Flush()
					}
					if err == nil && output == "reader copy" {
						// Omit WriterTo so io.Copy would use ReaderFrom if the
						// response wrapper incorrectly exposed a bypass path.
						_, err = io.Copy(w, struct{ io.Reader }{strings.NewReader(payload)})
					} else if err == nil {
						_, err = writer.Write([]byte(payload))
					}
					if err == nil {
						err = r.Context().Err()
					}
					result <- err
				})
				if guarded {
					handler = observabilityNoCache(handler)
				}
				server := httptest.NewServer(boundedRequestBodies(handler, time.Second))
				t.Cleanup(server.Close)
				connection, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
				if err != nil {
					t.Fatal("connect the unread GET fixture")
				}
				t.Cleanup(func() { _ = connection.Close() })
				if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
					t.Fatal("bound the unread GET fixture")
				}
				_, err = io.WriteString(connection, "GET /response HTTP/1.1\r\nHost: goby.example.test\r\nContent-Length: 1\r\n\r\n")
				if err != nil {
					t.Fatal("send GET headers without the promised body")
				}
				response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodGet})
				if err != nil {
					t.Fatalf("unread GET aborted or stalled before response headers: %v", err)
				}
				body, readErr := io.ReadAll(response.Body)
				closeErr := response.Body.Close()
				if response.StatusCode != http.StatusOK || !response.Close || readErr != nil || closeErr != nil || string(body) != payload {
					t.Fatalf("unread GET lost its complete response: status=%d close=%t read=%v closeError=%v", response.StatusCode, response.Close, readErr, closeErr)
				}
				if err := <-result; err != nil {
					t.Fatalf("response-time body retirement cancelled the active request: %v", err)
				}
			})
		}
	}
}

func TestBoundedRequestBodiesHTTP2UnreadBodyKeepsConnectionReusable(t *testing.T) {
	var accepted atomic.Int32
	results := make(chan error, 2)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			w.WriteHeader(http.StatusHTTPVersionNotSupported)
			return
		}
		if r.URL.Path == "/next" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Deliberately never consume the unexpected GET body. Only this
		// request's incoming direction should end, not the HTTP/2 connection.
		w.Header().Set("Content-Type", "text/plain")
		_, err := io.WriteString(w, "retained HTTP/2 connection")
		if err == nil {
			err = http.NewResponseController(w).Flush()
		}
		if err == nil {
			err = r.Context().Err()
		}
		results <- err
	})
	server := httptest.NewUnstartedServer(boundedRequestBodies(handler, time.Second))
	server.EnableHTTP2 = true
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			accepted.Add(1)
		}
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 3 * time.Second
	t.Cleanup(client.CloseIdleConnections)
	for index, path := range []string{"/unread", "/next", "/unread", "/next"} {
		var content io.Reader
		if path == "/unread" {
			content = strings.NewReader("unexpected request body")
		}
		var reused atomic.Bool
		trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused.Store(info.Reused) }}
		request, err := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), http.MethodGet, server.URL+path, content)
		if err != nil {
			t.Fatal("construct the HTTP/2 body-ownership request")
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("HTTP/2 request failed after unread-body retirement: %v", err)
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if response.ProtoMajor != 2 || response.Close || readErr != nil || closeErr != nil {
			t.Fatal("unread-body cleanup ended the HTTP/2 response or connection")
		}
		if index > 0 && !reused.Load() {
			t.Fatal("unread-body cleanup sent GOAWAY instead of preserving HTTP/2 reuse")
		}
		if path == "/unread" {
			if response.StatusCode != http.StatusOK || string(body) != "retained HTTP/2 connection" {
				t.Fatal("the unread HTTP/2 request lost its complete response")
			}
			if err := <-results; err != nil {
				t.Fatalf("unread-body retirement cancelled the active HTTP/2 response: %v", err)
			}
		} else if response.StatusCode != http.StatusNoContent {
			t.Fatal("the HTTP/2 follow-up request did not complete")
		}
	}
	if accepted.Load() != 1 {
		t.Fatalf("HTTP/2 body retirement required %d connections instead of one", accepted.Load())
	}
}

// Deadline changes wake an in-flight operation; each operation also observes
// natural expiration without an explicit cancellation callback setting Now.
type boundsTimedResponse struct {
	*httptest.ResponseRecorder
	mu                     sync.Mutex
	deadline               time.Time
	deadlines              []time.Time
	changed                chan struct{}
	entered                chan struct{}
	enterOnce              sync.Once
	writeDelay             time.Duration
	blockWrite, blockFlush bool
	writeSizes             []int
}

func newBoundsTimedResponse() *boundsTimedResponse {
	return &boundsTimedResponse{ResponseRecorder: httptest.NewRecorder(), changed: make(chan struct{}), entered: make(chan struct{})}
}

func (w *boundsTimedResponse) SetWriteDeadline(deadline time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deadline = deadline
	w.deadlines = append(w.deadlines, deadline)
	close(w.changed)
	w.changed = make(chan struct{})
	return nil
}

func (w *boundsTimedResponse) awaitProgress(blocked bool, delay time.Duration) error {
	var progress <-chan time.Time
	if !blocked {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		progress = timer.C
	}
	for {
		w.mu.Lock()
		deadline, changed := w.deadline, w.changed
		w.mu.Unlock()
		var timer *time.Timer
		var expired <-chan time.Time
		if !deadline.IsZero() {
			if !deadline.After(time.Now()) {
				return os.ErrDeadlineExceeded
			}
			timer = time.NewTimer(time.Until(deadline))
			expired = timer.C
		}
		select {
		case <-progress:
			if timer != nil {
				timer.Stop()
			}
			return nil
		case <-changed:
			if timer != nil {
				timer.Stop()
			}
		case <-expired:
			return os.ErrDeadlineExceeded
		}
	}
}

func (w *boundsTimedResponse) Write(data []byte) (int, error) {
	w.mu.Lock()
	w.writeSizes = append(w.writeSizes, len(data))
	w.mu.Unlock()
	if w.blockWrite {
		n, _ := w.ResponseRecorder.Write(data[:min(8, len(data))])
		w.enterOnce.Do(func() { close(w.entered) })
		return n, w.awaitProgress(true, 0)
	}
	if err := w.awaitProgress(false, w.writeDelay); err != nil {
		return 0, err
	}
	return w.ResponseRecorder.Write(data)
}

func (w *boundsTimedResponse) FlushError() error {
	if w.blockFlush {
		w.enterOnce.Do(func() { close(w.entered) })
		return w.awaitProgress(true, 0)
	}
	w.ResponseRecorder.Flush()
	return nil
}

func (w *boundsTimedResponse) assertDeadlineCleared(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.deadlines) < 2 || !w.deadline.IsZero() || !w.deadlines[len(w.deadlines)-1].IsZero() {
		t.Fatal("response cleanup retained a connection deadline")
	}
}

func TestIdleResponseWriterNaturalTimeoutAbortsWriteAndFinalFlush(t *testing.T) {
	for _, phase := range []string{"write", "final flush"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const idle = 2 * time.Second
				response := newBoundsTimedResponse()
				response.blockWrite = phase == "write"
				response.blockFlush = phase == "final flush"
				writer, err := newIdleResponseWriter(response, context.Background(), idle)
				if err != nil {
					t.Fatal("initialize the timed response")
				}
				started := time.Now()
				finished := make(chan any, 1)
				go func() {
					defer func() { finished <- recover() }()
					_, _ = writer.Write(bytes.Repeat([]byte("x"), 128))
					writer.finish()
				}()
				<-response.entered
				if recovered := <-finished; recovered != http.ErrAbortHandler {
					t.Fatal("natural idle expiration did not abort the committed response")
				}
				if elapsed := time.Since(started); elapsed < idle || elapsed > idle+time.Millisecond {
					t.Fatalf("natural write idle expired outside its bound: %s", elapsed)
				}
				if !errors.Is(writer.err, os.ErrDeadlineExceeded) {
					t.Fatalf("idle failure lost its transport error: %v", writer.err)
				}
				time.Sleep(2 * idle)
				response.assertDeadlineCleared(t)
			})
		})
	}
}

func TestIdleResponseWriterSlidingProgressOutlivesOneIdleInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const idle = 4 * time.Second
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		response := newBoundsTimedResponse()
		response.writeDelay = 3 * time.Second
		writer, err := newIdleResponseWriter(response, ctx, idle)
		if err != nil {
			t.Fatal("initialize the progressing response")
		}
		payload := bytes.Repeat([]byte("p"), mediaWriteChunk*3+17)
		started := time.Now()
		n, err := writer.Write(payload)
		if err != nil || n != len(payload) || !bytes.Equal(response.Body.Bytes(), payload) {
			t.Fatalf("active transfer failed despite progress within each idle interval: bytes=%d, error=%v", n, err)
		}
		writer.finish()
		if time.Since(started) <= idle {
			t.Fatal("progress fixture did not outlive the original idle deadline")
		}
		response.mu.Lock()
		sizes := append([]int(nil), response.writeSizes...)
		deadlineCount := len(response.deadlines)
		response.mu.Unlock()
		if len(sizes) != 4 {
			t.Fatalf("large response used %d writes instead of bounded chunks", len(sizes))
		}
		for _, size := range sizes {
			if size > mediaWriteChunk {
				t.Fatal("large write bypassed the per-chunk idle refresh")
			}
		}
		cancel()
		synctest.Wait()
		time.Sleep(2 * idle)
		response.assertDeadlineCleared(t)
		response.mu.Lock()
		lateDeadline := len(response.deadlines) != deadlineCount
		response.mu.Unlock()
		if lateDeadline {
			t.Fatal("a completed response retained its cancellation callback")
		}
	})
}

func TestIdleResponseWriterCancellationInterruptsWriteAndFinalFlush(t *testing.T) {
	for _, phase := range []string{"write", "final flush"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const idle = time.Minute
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				response := newBoundsTimedResponse()
				response.blockWrite = phase == "write"
				response.blockFlush = phase == "final flush"
				writer, err := newIdleResponseWriter(response, ctx, idle)
				if err != nil {
					t.Fatal("initialize the cancellable response")
				}
				finished := make(chan any, 1)
				go func() {
					defer func() { finished <- recover() }()
					_, _ = writer.Write(bytes.Repeat([]byte("x"), 128))
					writer.finish()
				}()
				<-response.entered
				cancelled := time.Now()
				cancel()
				if recovered := <-finished; recovered != http.ErrAbortHandler {
					t.Fatal("cancellation did not abort the committed response")
				}
				if time.Since(cancelled) >= idle {
					t.Fatal("cancellation waited for the ordinary write idle deadline")
				}
				select {
				case <-writer.callback:
				default:
					t.Fatal("response cleanup returned before its cancellation callback")
				}
				time.Sleep(2 * idle)
				response.assertDeadlineCleared(t)
			})
		})
	}
}

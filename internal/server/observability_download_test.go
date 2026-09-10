//go:build linux

package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/diagnostics"
)

// A recorder cannot block on network I/O. Tests explicitly provide its deadline
// capability; production writers without that capability must fail closed.
type diagnosticRecorderWithDeadline struct{ *httptest.ResponseRecorder }

func (w diagnosticRecorderWithDeadline) SetWriteDeadline(time.Time) error { return nil }

func (w diagnosticRecorderWithDeadline) SetReadDeadline(time.Time) error { return nil }

func TestNativeActivityProjectionRejectsUnsafeTotalsAndRetainsDecimalFacts(t *testing.T) {
	page := activity.Page{TotalRecordCount: observabilityMaxJSONInteger + 1}
	if _, err := nativeActivityDTO(page, 30); !errors.Is(err, activity.ErrUnavailable) {
		t.Fatal("an unsafe JSON count must not be rounded or truncated")
	}
	page.TotalRecordCount = -1
	if _, err := nativeActivityDTO(page, 30); !errors.Is(err, activity.ErrUnavailable) {
		t.Fatal("a negative stored total must not become a successful response")
	}
	page.TotalRecordCount = 1
	page.Items = []activity.Entry{{ID: 9223372036854775807, Event: activity.Event{
		Count: 9223372036854775807, Revision: 9223372036854775807,
	}}}
	result, err := nativeActivityDTO(page, 30)
	if err != nil || len(result.Items) != 1 || result.Items[0].ID != "9223372036854775807" ||
		result.Items[0].Count != "9223372036854775807" || result.Items[0].Revision == nil || *result.Items[0].Revision != "9223372036854775807" {
		t.Fatal("stored int64 facts lost precision in their decimal-string projection")
	}
}

// This writer models a socket accepting a prefix and then blocking until its
// write deadline is advanced. ResponseController reaches it through Unwrap.
type blockedDiagnosticResponse struct {
	*httptest.ResponseRecorder
	entered      chan struct{}
	unblocked    chan struct{}
	startOnce    sync.Once
	stopOnce     sync.Once
	mu           sync.Mutex
	deadlines    []time.Time
	blockOnFlush bool
}

func newBlockedDiagnosticResponse() *blockedDiagnosticResponse {
	return &blockedDiagnosticResponse{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), unblocked: make(chan struct{})}
}

func (w *blockedDiagnosticResponse) SetWriteDeadline(deadline time.Time) error {
	w.mu.Lock()
	w.deadlines = append(w.deadlines, deadline)
	w.mu.Unlock()
	if !deadline.IsZero() && !deadline.After(time.Now()) {
		w.stopOnce.Do(func() { close(w.unblocked) })
	}
	return nil
}

func (w *blockedDiagnosticResponse) Write(data []byte) (int, error) {
	if w.blockOnFlush {
		return w.ResponseRecorder.Write(data)
	}
	n := min(len(data), 8)
	_, _ = w.ResponseRecorder.Write(data[:n])
	w.startOnce.Do(func() { close(w.entered) })
	<-w.unblocked
	return n, os.ErrDeadlineExceeded
}

func (w *blockedDiagnosticResponse) FlushError() error {
	if w.blockOnFlush {
		w.startOnce.Do(func() { close(w.entered) })
		<-w.unblocked
		return os.ErrDeadlineExceeded
	}
	w.ResponseRecorder.Flush()
	return nil
}

func (w *blockedDiagnosticResponse) release() {
	w.stopOnce.Do(func() { close(w.unblocked) })
}

func (w *blockedDiagnosticResponse) assertDrainedDeadline(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.deadlines) < 3 || !w.deadlines[len(w.deadlines)-1].IsZero() {
		t.Fatal("the download did not interrupt its writer and reset the deadline after draining its watchers")
	}
	if w.deadlines[0].After(time.Now().Add(diagnosticDownloadLifetime)) {
		t.Fatal("the download exceeded its maximum write lifetime")
	}
}

func TestHTTPDiagnosticDownloadRevocationAndCancellationInterruptBlockedWrites(t *testing.T) {
	for _, reason := range []string{"revocation", "revocation during flush", "request cancellation", "credential expiry"} {
		t.Run(reason, func(t *testing.T) {
			f := newServerFixture(t)
			f.bootstrap(t)
			cookie, _ := f.adminLogin(t)
			principal, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
			if err != nil {
				t.Fatal("resolve the owned administrator")
			}
			store, _ := nativeDiagnosticStore(t, f)
			nativeDiagnosticRecords(t, f, store)
			page, err := store.List(f.ctx, diagnostics.ListOptions{Limit: 1})
			if err != nil || len(page.Items) != 1 {
				t.Fatal("read the owned diagnostic file")
			}
			if reason == "credential expiry" {
				if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
					expires_at = clock_timestamp() + interval '2 seconds' WHERE id = $1`, principal.SessionID); err != nil {
					t.Fatal("prepare the owned credential expiry")
				}
			}
			ctx, cancel := context.WithCancel(f.ctx)
			t.Cleanup(cancel)
			request := httptest.NewRequest(http.MethodGet, "/admin/v1/logs/"+page.Items[0].Name+"/download", nil).WithContext(ctx)
			request.AddCookie(cookie)
			response := newBlockedDiagnosticResponse()
			response.blockOnFlush = reason == "revocation during flush"
			t.Cleanup(response.release)
			finished := make(chan any, 1)
			go func() {
				defer func() { finished <- recover() }()
				f.handler.ServeHTTP(response, request)
			}()
			select {
			case <-response.entered:
			case <-finished:
				t.Fatal("the owned download did not enter its blocked response write")
			case <-time.After(5 * time.Second):
				t.Fatal("the owned download did not begin within its admission bound")
			}
			switch reason {
			case "revocation", "revocation during flush":
				mutation, done := context.WithTimeout(f.ctx, 2*time.Second)
				_, err := f.pool.Exec(mutation, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", principal.SessionID)
				done()
				if err != nil {
					t.Fatal("a download retained an actor lock across its blocked file transfer")
				}
			case "request cancellation":
				cancel()
			}
			select {
			case recovered := <-finished:
				if recovered != http.ErrAbortHandler {
					t.Fatal("an interrupted download reported ordinary completion instead of aborting the HTTP stream")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("revocation or cancellation failed to interrupt a blocked download write")
			}
			response.assertDrainedDeadline(t)
			length, err := strconv.ParseInt(response.Header().Get("Content-Length"), 10, 64)
			if err != nil || length != page.Items[0].Size || response.Body.Len() == 0 || !response.blockOnFlush && int64(response.Body.Len()) >= length {
				t.Fatal("the aborted response did not retain its original length and a visibly incomplete body")
			}
			// All eight slots must be immediately reusable after an aborted transfer.
			for range diagnostics.MaxReaders {
				reader, err := store.Snapshot(f.ctx, page.Items[0].Name)
				if err != nil {
					t.Fatal("the interrupted download leaked its pinned reader slot")
				}
				t.Cleanup(func() { _ = reader.Close() })
			}
		})
	}
}

type faultyDiagnosticSnapshot struct {
	*bytes.Reader
	size     int64
	readErr  error
	closeErr error
	closed   atomic.Bool
}

func (s *faultyDiagnosticSnapshot) Name() string       { return "owned.jsonl" }
func (s *faultyDiagnosticSnapshot) Size() int64        { return s.size }
func (s *faultyDiagnosticSnapshot) ModTime() time.Time { return time.Unix(100, 0).UTC() }
func (s *faultyDiagnosticSnapshot) Close() error {
	s.closed.Store(true)
	return s.closeErr
}
func (s *faultyDiagnosticSnapshot) Read(data []byte) (int, error) {
	if s.readErr != nil {
		return 0, s.readErr
	}
	return s.Reader.Read(data)
}

func (s *faultyDiagnosticSnapshot) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekEnd {
		return s.Reader.Seek(s.size+offset, io.SeekStart)
	}
	return s.Reader.Seek(offset, whence)
}

func TestDiagnosticDownloadReaderFailureAndCloseFailureAbortAfterHeaders(t *testing.T) {
	for _, failure := range []string{"read failure", "close failure", "short read"} {
		t.Run(failure, func(t *testing.T) {
			body := []byte("{\"event\":\"service.started\"}\n")
			snapshot := &faultyDiagnosticSnapshot{Reader: bytes.NewReader(body), size: int64(len(body))}
			switch failure {
			case "read failure":
				snapshot.readErr = diagnostics.ErrUnavailable
			case "close failure":
				snapshot.closeErr = diagnostics.ErrUnavailable
			case "short read":
				snapshot.Reader = bytes.NewReader(nil)
			}
			request := httptest.NewRequest(http.MethodGet, "/admin/v1/logs/owned.jsonl/download", nil)
			response := diagnosticRecorderWithDeadline{httptest.NewRecorder()}
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				_ = serveDiagnosticSnapshot(response, request, snapshot, func(context.Context) error { return nil })
			}()
			if recovered != http.ErrAbortHandler || !snapshot.closed.Load() {
				t.Fatal("a failed pinned reader was not closed and surfaced as an aborted HTTP response")
			}
		})
	}
}

func TestDiagnosticDownloadSnapshotDoesNotExpandAfterOpening(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal("restrict the owned diagnostics directory")
	}
	store, err := diagnostics.Open(diagnostics.Config{Directory: directory, MinFreeBytes: 1})
	if err != nil {
		t.Fatal("open the owned diagnostics store")
	}
	t.Cleanup(func() { _ = store.Close() })
	logger := slog.New(diagnostics.NewHandler(store, nil))
	logger.Info("server listening")
	page, err := store.List(context.Background(), diagnostics.ListOptions{Limit: 1})
	if err != nil || len(page.Items) != 1 {
		t.Fatal("read the owned diagnostic registry")
	}
	snapshot, err := store.Snapshot(context.Background(), page.Items[0].Name)
	if err != nil {
		t.Fatal("pin the original diagnostic snapshot")
	}
	t.Cleanup(func() { _ = snapshot.Close() })
	body, err := io.ReadAll(snapshot)
	if err != nil {
		t.Fatal("read the original diagnostic bytes")
	}
	if _, err := snapshot.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the pinned diagnostic snapshot")
	}
	logger.Info("server stopped")
	later, err := store.List(context.Background(), diagnostics.ListOptions{Limit: 1})
	if err != nil || later.Items[0].Size <= int64(len(body)) {
		t.Fatal("the active diagnostic file did not grow after its snapshot was pinned")
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/v1/logs/owned.jsonl/download", nil)
	response := diagnosticRecorderWithDeadline{httptest.NewRecorder()}
	if err := serveDiagnosticSnapshot(response, request, snapshot, func(context.Context) error { return nil }); err != nil {
		t.Fatal("serve the fixed diagnostic snapshot")
	}
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), body) {
		t.Fatal("the completed snapshot changed its bytes or retained its reader")
	}
	if response.Header().Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Fatal("the diagnostic response did not retain its original content length")
	}
	if _, err := snapshot.Read(make([]byte, 1)); !errors.Is(err, diagnostics.ErrUnavailable) {
		t.Fatal("the completed diagnostic download retained its pinned reader")
	}
}

func TestDiagnosticDownloadRequiresAnInterruptibleWriter(t *testing.T) {
	body := []byte("{\"event\":\"service.started\"}\n")
	snapshot := &faultyDiagnosticSnapshot{Reader: bytes.NewReader(body), size: int64(len(body))}
	request := httptest.NewRequest(http.MethodGet, "/admin/v1/logs/owned.jsonl/download", nil)
	response := httptest.NewRecorder()
	err := serveDiagnosticSnapshot(response, request, snapshot, func(context.Context) error { return nil })
	if !errors.Is(err, diagnostics.ErrUnavailable) || !snapshot.closed.Load() || response.Body.Len() != 0 {
		t.Fatal("a writer without cancellation deadlines exposed a diagnostic snapshot or retained its reader")
	}
}

func TestHTTPDiagnosticDownloadDoesNotWaitForAnUnsentRequestBody(t *testing.T) {
	f, cookie, _, _ := adminSettingsHTTPFixture(t)
	store, _ := nativeDiagnosticStore(t, f)
	want := nativeDiagnosticRecords(t, f, store)
	page, err := store.List(f.ctx, diagnostics.ListOptions{Limit: 1})
	if err != nil || len(page.Items) != 1 {
		t.Fatal("resolve the owned diagnostic download")
	}
	server := httptest.NewServer(f.handler)
	t.Cleanup(server.Close)
	for _, authenticated := range []bool{false, true} {
		connection, err := net.DialTimeout("tcp", server.Listener.Addr().String(), 2*time.Second)
		if err != nil {
			t.Fatal("connect to the owned HTTP fixture")
		}
		if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			connection.Close()
			t.Fatal("bound the owned slow-body connection")
		}
		cookieHeader := ""
		if authenticated {
			cookieHeader = "Cookie: " + sessionCookie + "=" + cookie.Value + "\r\n"
		}
		_, err = io.WriteString(connection, "GET /admin/v1/logs/"+page.Items[0].Name+"/download HTTP/1.1\r\nHost: goby.example.test\r\n"+
			cookieHeader+"Content-Length: 1\r\n\r\n")
		if err != nil {
			connection.Close()
			t.Fatal("send owned download headers without the promised request body")
		}
		response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodGet})
		if err != nil {
			connection.Close()
			t.Fatal("the download waited for the unsent request body before returning headers")
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		_ = connection.Close()
		if readErr != nil || !response.Close {
			t.Fatal("the download did not complete and close its unread-body connection")
		}
		if authenticated {
			if response.StatusCode != http.StatusOK || !bytes.Equal(body, want) {
				t.Fatal("the authenticated download did not serve its fixed snapshot without reading the request body")
			}
		} else if response.StatusCode != http.StatusUnauthorized || bytes.Contains(body, want) {
			t.Fatal("the unread-body request bypassed diagnostic authentication")
		}
	}
}

var _ io.ReadSeekCloser = (*faultyDiagnosticSnapshot)(nil)

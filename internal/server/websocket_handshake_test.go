package server

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
)

// blockingHandshakeWriter models an unread 101 response. An expired write
// deadline interrupts the pending write; Hijack can either fail or complete
// after that interruption to exercise both sides of the shutdown race.
type blockingHandshakeWriter struct {
	header http.Header
	mu     sync.Mutex
	until  time.Time
	conn   net.Conn

	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingHandshakeWriter(conn net.Conn) *blockingHandshakeWriter {
	return &blockingHandshakeWriter{header: make(http.Header), conn: conn, entered: make(chan struct{}), release: make(chan struct{})}
}

func (w *blockingHandshakeWriter) Header() http.Header { return w.header }

func (w *blockingHandshakeWriter) WriteHeader(status int) {
	if status == http.StatusSwitchingProtocols {
		close(w.entered)
		<-w.release
	}
}

func (w *blockingHandshakeWriter) Write([]byte) (int, error) {
	return 0, os.ErrDeadlineExceeded
}

func (w *blockingHandshakeWriter) SetWriteDeadline(deadline time.Time) error {
	w.mu.Lock()
	w.until = deadline
	w.mu.Unlock()
	if !deadline.IsZero() && !deadline.After(time.Now()) {
		w.interrupt()
	}
	return nil
}

func (w *blockingHandshakeWriter) deadline() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.until
}

func (w *blockingHandshakeWriter) interrupt() {
	w.once.Do(func() { close(w.release) })
}

func (w *blockingHandshakeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.conn == nil {
		return nil, nil, os.ErrDeadlineExceeded
	}
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

func handshakeFixture(t *testing.T) (*Server, *http.Request, string) {
	t.Helper()
	hub, err := events.New(events.Options{MaxConnections: 1, MaxConnectionsPerUser: 1, MaxConnectionsPerSession: 1})
	if err != nil {
		t.Fatalf("create handshake event hub: %v", err)
	}
	app := &Server{eventHub: hub, sockets: newSocketRuntime()}
	t.Cleanup(func() {
		app.sockets.cancel()
		_ = hub.Close()
	})
	principal := identity.Principal{
		User: identity.User{ID: "handshake-user"}, SessionID: "handshake-session", Kind: "emby",
		Client: identity.Client{DeviceID: "handshake-device"},
	}
	request := httptest.NewRequest(http.MethodGet, "/emby/socket", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", "MTIzNDU2Nzg5MGFiY2RlZg==")
	request = request.WithContext(context.WithValue(request.Context(), principalKey, principal))
	return app, request, principal.SessionID
}

func startBlockedHandshake(t *testing.T, app *Server, writer *blockingHandshakeWriter, request *http.Request) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.clientSocket(writer, request)
	}()
	t.Cleanup(func() {
		app.sockets.cancel()
		writer.interrupt()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("pending handshake did not leave its handler during cleanup")
		}
	})
	select {
	case <-writer.entered:
	case <-done:
		t.Fatal("handshake exited before its controlled 101 write")
	case <-time.After(5 * time.Second):
		t.Fatal("handshake never reached its controlled 101 write")
	}
	return done
}

func closeHandshakeRuntime(app *Server) <-chan struct{} {
	// Use the same admission fence as closeSockets without constructing a
	// database-backed catalog: this test only exercises transport ownership.
	app.sockets.mu.Lock()
	app.sockets.closed = true
	app.sockets.cancel()
	app.sockets.mu.Unlock()
	done := make(chan struct{})
	go func() {
		app.sockets.wg.Wait()
		close(done)
	}()
	return done
}

func waitHandshakeDone(t *testing.T, done <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s remained blocked after transport cancellation", name)
	}
}

func TestClientSocketPendingHandshakeCancellationReleasesQuotaAndWaitGroup(t *testing.T) {
	app, request, sessionID := handshakeFixture(t)
	writer := newBlockingHandshakeWriter(nil)
	handlerDone := startBlockedHandshake(t, app, writer, request)
	if deadline := writer.deadline(); deadline.IsZero() || time.Until(deadline) > 5*time.Second {
		t.Errorf("pending handshake has no bounded write deadline: %v", deadline)
	}
	if count := app.eventHub.CountForSession(sessionID); count != 1 {
		t.Fatalf("pending handshake occupies %d quota slots, want 1", count)
	}
	if app.hasClientControlTransport(sessionID) {
		t.Error("a pending handshake was advertised as a ready control transport")
	}
	if extra, err := app.eventHub.Subscribe(events.Scope{UserID: "other-user", SessionID: "other-session"}); !errors.Is(err, events.ErrConnectionLimit) {
		if extra != nil {
			_ = extra.Close()
		}
		t.Errorf("pending handshake did not reserve the global connection quota: %v", err)
	}
	shutdownDone := closeHandshakeRuntime(app)
	waitHandshakeDone(t, handlerDone, "handshake handler")
	waitHandshakeDone(t, shutdownDone, "transport WaitGroup")
	if count := app.eventHub.CountForSession(sessionID); count != 0 {
		t.Errorf("cancelled handshake retained %d subscription slots", count)
	}
	app.sockets.mu.Lock()
	ready := len(app.sockets.ready)
	app.sockets.mu.Unlock()
	if ready != 0 {
		t.Errorf("cancelled handshake registered %d ready transports", ready)
	}
}

func TestClientSocketUpgradeCompletingDuringShutdownNeverBecomesReady(t *testing.T) {
	app, request, sessionID := handshakeFixture(t)
	serverConn, peerConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = peerConn.Close()
	})
	writer := newBlockingHandshakeWriter(serverConn)
	handlerDone := startBlockedHandshake(t, app, writer, request)
	shutdownDone := closeHandshakeRuntime(app)
	waitHandshakeDone(t, handlerDone, "late upgrade handler")
	waitHandshakeDone(t, shutdownDone, "late upgrade WaitGroup")
	if !writer.deadline().IsZero() {
		t.Error("handshake completion did not synchronize cancellation before clearing its deadline")
	}
	app.sockets.mu.Lock()
	ready := len(app.sockets.ready)
	app.sockets.mu.Unlock()
	if ready != 0 || app.hasClientControlTransport(sessionID) || app.eventHub.CountForSession(sessionID) != 0 {
		t.Error("an upgrade completing after the shutdown fence retained a ready transport or subscription")
	}
}

func TestClientSocketUnsupportedHandshakeDeadlineReleasesQuota(t *testing.T) {
	app, request, sessionID := handshakeFixture(t)
	response := httptest.NewRecorder()
	app.clientSocket(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Errorf("an unbounded handshake writer returned %d, want 503", response.Code)
	}
	if app.eventHub.CountForSession(sessionID) != 0 || app.hasClientControlTransport(sessionID) {
		t.Error("a rejected handshake retained a quota slot or ready transport")
	}
	waitHandshakeDone(t, closeHandshakeRuntime(app), "rejected upgrade WaitGroup")
}

func TestClientSocketShutdownFenceRejectsNewUpgrades(t *testing.T) {
	app, request, sessionID := handshakeFixture(t)
	waitHandshakeDone(t, closeHandshakeRuntime(app), "empty transport WaitGroup")
	response := httptest.NewRecorder()
	app.clientSocket(response, request)
	if response.Code != http.StatusServiceUnavailable || app.eventHub.CountForSession(sessionID) != 0 {
		t.Errorf("upgrade crossed the shutdown admission fence: status = %d, subscriptions = %d", response.Code, app.eventHub.CountForSession(sessionID))
	}
}

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
)

const maxSocketInputBytes = 64 * 1024

// socketRuntime owns hijacked connections independently of the startup and HTTP
// request contexts. net/http.Shutdown does not wait for WebSockets, so Close
// explicitly cancels their work before the catalog and database are released.
type socketRuntime struct {
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	closed      bool
	ready       map[string]int
	wg          sync.WaitGroup
	once        sync.Once
	done        chan struct{}
	shutdownErr error

	revalidateEvery time.Duration
	pingEvery       time.Duration
}

func newSocketRuntime() *socketRuntime {
	ctx, cancel := context.WithCancel(context.Background())
	return &socketRuntime{ctx: ctx, cancel: cancel, ready: map[string]int{}, done: make(chan struct{}),
		revalidateEvery: 5 * time.Second, pingEvery: 30 * time.Second}
}

func (s *Server) closeSockets(ctx context.Context) error {
	if s.sockets == nil {
		return s.library.Close(ctx)
	}
	runtime := s.sockets
	runtime.once.Do(func() {
		runtime.mu.Lock()
		runtime.closed = true
		runtime.cancel()
		runtime.mu.Unlock()
		_ = s.eventHub.Close()
		go func() {
			s.notifier.Close()
			runtime.wg.Wait()
			// Cleanup continues even if an individual Close caller times out.
			runtime.shutdownErr = s.library.Close(context.Background())
			close(runtime.done)
		}()
	})
	select {
	case <-runtime.done:
		return runtime.shutdownErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func isSocketRequest(r *http.Request) bool {
	if r.Method != http.MethodGet || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	switch strings.ToLower(r.URL.EscapedPath()) {
	case "/", "/emby", "/emby/", "/embywebsocket", "/emby/socket":
		return true
	default:
		return false
	}
}

func (s *Server) hasClientControlTransport(sessionID string) bool {
	if s.sockets == nil || s.eventHub == nil {
		return false
	}
	s.sockets.mu.Lock()
	ready := !s.sockets.closed && s.sockets.ready[sessionID] > 0
	s.sockets.mu.Unlock()
	return ready && s.eventHub.CountForSession(sessionID) > 0
}

func (s *Server) clientSocket(w http.ResponseWriter, r *http.Request) {
	if s.sockets == nil || s.eventHub == nil {
		embyTextError(w, r, http.StatusServiceUnavailable, "The notification service is unavailable.")
		return
	}
	// Emby credentials are explicit tokens; administrator cookies are never
	// accepted here. Cross-origin browser clients may connect with their token.
	if origins := r.Header.Values("Origin"); len(origins) > 1 || (len(origins) == 1 && (len(origins[0]) > 2048 || !validOrigin(origins[0]))) {
		embyTextError(w, r, http.StatusBadRequest, "The request origin is invalid.")
		return
	}
	runtime := s.sockets
	runtime.mu.Lock()
	if runtime.closed {
		runtime.mu.Unlock()
		embyTextError(w, r, http.StatusServiceUnavailable, "The notification service is closing.")
		return
	}
	// Add under the same lock that fences shutdown, including pending upgrades.
	runtime.wg.Add(1)
	runtime.mu.Unlock()
	defer runtime.wg.Done()
	principal := r.Context().Value(principalKey).(identity.Principal)
	sub, err := s.eventHub.Subscribe(events.Scope{UserID: principal.User.ID, SessionID: principal.SessionID, DeviceID: principal.Client.DeviceID})
	if err != nil {
		status := http.StatusTooManyRequests
		if errors.Is(err, events.ErrClosed) {
			status = http.StatusServiceUnavailable
		}
		embyTextError(w, r, status, "The notification connection limit has been reached.")
		return
	}
	defer sub.Close()
	// A pending 101 response is still HTTP traffic. Bound its write before
	// hijacking and interrupt it on shutdown; otherwise an unread handshake
	// could retain a quota slot and hold the shutdown WaitGroup indefinitely.
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		embyTextError(w, r, http.StatusServiceUnavailable, "The transport cannot bound an upgrade handshake.")
		return
	}
	cancelled := make(chan struct{})
	stopHandshakeCancel := context.AfterFunc(runtime.ctx, func() {
		_ = controller.SetWriteDeadline(time.Now())
		close(cancelled)
	})
	// InsecureSkipVerify disables the library's same-origin check only. Token
	// authentication and origin syntax validation have already run above; TLS
	// verification is unaffected. Compression stays disabled to bound memory.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if !stopHandshakeCancel() {
		<-cancelled
	}
	if err != nil {
		return
	}
	defer conn.CloseNow()
	// Cancel callback completion is synchronized before clearing its deadline.
	// Post-upgrade reads and writes use the connection's own bounded contexts.
	if err := controller.SetWriteDeadline(time.Time{}); err != nil {
		return
	}
	conn.SetReadLimit(maxSocketInputBytes)
	runtime.mu.Lock()
	if runtime.closed {
		runtime.mu.Unlock()
		return
	}
	runtime.ready[principal.SessionID]++
	runtime.mu.Unlock()
	defer func() {
		runtime.mu.Lock()
		if runtime.ready[principal.SessionID] <= 1 {
			delete(runtime.ready, principal.SessionID)
		} else {
			runtime.ready[principal.SessionID]--
		}
		runtime.mu.Unlock()
	}()
	s.serveClientSocket(conn, sub, principal)
}

func (s *Server) serveClientSocket(conn *websocket.Conn, sub *events.Subscription, principal identity.Principal) {
	ctx, cancel := context.WithCancel(s.sockets.ctx)
	var workers sync.WaitGroup
	workers.Add(3)
	defer func() {
		cancel()
		_ = conn.CloseNow()
		workers.Wait()
	}()
	go func() {
		defer workers.Done()
		defer cancel()
		readSocketMessages(ctx, conn)
	}()
	go func() {
		defer workers.Done()
		defer cancel()
		s.maintainSocket(ctx, conn, principal)
	}()
	go func() {
		defer workers.Done()
		select {
		case <-ctx.Done():
		case <-sub.Done():
		}
		cancel()
		// Interrupt blocked network writes immediately on revocation, overflow,
		// or shutdown; queued data must not drain after authorization is lost.
		_ = conn.CloseNow()
	}()
	for {
		event, err := sub.Next(ctx)
		if err != nil {
			return
		}
		authorizationCtx, stopAuthorization := context.WithTimeout(ctx, 2*time.Second)
		fresh, err := s.identity.RevalidateSession(authorizationCtx, principal)
		var payload []byte
		if err == nil {
			payload, err = s.socketEventPayload(authorizationCtx, fresh, event)
		}
		stopAuthorization()
		if err != nil {
			return
		}
		if len(payload) == 0 {
			continue
		}
		writeCtx, stopWrite := context.WithTimeout(ctx, 5*time.Second)
		err = conn.Write(writeCtx, websocket.MessageText, payload)
		stopWrite()
		if err != nil {
			return
		}
	}
}

func readSocketMessages(ctx context.Context, conn *websocket.Conn) {
	window, messages := time.Now(), 0
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if time.Since(window) >= time.Second {
			window, messages = time.Now(), 0
		}
		messages++
		if messages > 64 {
			_ = conn.Close(websocket.StatusPolicyViolation, "Message rate exceeded.")
			return
		}
		var envelope struct {
			MessageType string
			Data        json.RawMessage
		}
		if !utf8.Valid(data) || json.Unmarshal(data, &envelope) != nil || strings.TrimSpace(envelope.MessageType) == "" ||
			len(envelope.MessageType) > 128 || strings.IndexFunc(envelope.MessageType, unicode.IsControl) >= 0 {
			_ = conn.Close(websocket.StatusInvalidFramePayloadData, "Expected a JSON message envelope.")
			return
		}
		// The verified progress operation is HTTP Sessions/Playing/Progress.
		// Unsupported application messages are inert. Conn.Read handles RFC
		// Ping/Pong and Close independently of these text/binary JSON messages.
	}
}

func (s *Server) maintainSocket(ctx context.Context, conn *websocket.Conn, principal identity.Principal) {
	revalidate := time.NewTicker(s.sockets.revalidateEvery)
	ping := time.NewTicker(s.sockets.pingEvery)
	defer revalidate.Stop()
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-revalidate.C:
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			fresh, err := s.identity.RevalidateSession(checkCtx, principal)
			if err == nil && time.Since(fresh.LastSeenAt) >= identity.ClientSessionTouchInterval {
				err = s.identity.TouchClientSession(checkCtx, fresh)
			}
			cancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// Event content is immutable after publication. Recheck resource visibility at
// delivery time without replacing the snapshot with a per-recipient DB value;
// recipients of the same publication retain the same MessageId and state.
func (s *Server) socketEventPayload(ctx context.Context, principal identity.Principal, event events.Event) ([]byte, error) {
	if event.MessageType() != "UserDataChanged" {
		switch event.MessageType() {
		case "Play", "Playstate", "GeneralCommand":
			allowed, err := s.authorizeRemoteSocketEvent(ctx, principal, event)
			if err != nil || !allowed {
				return nil, err
			}
			return event.Bytes(), nil
		default:
			return nil, nil
		}
	}
	var envelope events.Envelope
	var data struct {
		UserID       string            `json:"UserId"`
		UserDataList []json.RawMessage `json:"UserDataList"`
	}
	if err := json.Unmarshal(event.Bytes(), &envelope); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return nil, err
	}
	if data.UserID != principal.User.ID || len(data.UserDataList) > 256 {
		return nil, errors.New("invalid user notification scope")
	}
	ids := make([]string, 0, len(data.UserDataList))
	for _, entry := range data.UserDataList {
		var item struct {
			ItemID string `json:"ItemId"`
		}
		if err := json.Unmarshal(entry, &item); err != nil || item.ItemID == "" {
			return nil, errors.New("invalid user notification item")
		}
		ids = append(ids, item.ItemID)
	}
	visible, err := s.library.GetUserDataBatch(ctx, principal.User.ID, ids)
	if err != nil {
		return nil, err
	}
	filtered := make([]json.RawMessage, 0, len(ids))
	for i, id := range ids {
		if _, ok := visible[id]; ok {
			filtered = append(filtered, data.UserDataList[i])
		}
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	if len(filtered) == len(ids) {
		return event.Bytes(), nil
	}
	data.UserDataList = filtered
	envelope.Data, err = json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope)
}

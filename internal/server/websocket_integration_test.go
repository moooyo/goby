//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/moooyo/goby/internal/events"
)

// The reader stays active for RFC Ping/Pong processing and gives every test a
// bounded way to observe application messages and remote connection closure.
type websocketHTTPClient struct {
	conn     *websocket.Conn
	messages chan []byte
	done     chan struct{}
	readErr  error
}

func websocketHTTPServer(t *testing.T, fixture *serverFixture, maintenance time.Duration) *httptest.Server {
	t.Helper()
	// These runtime intervals are immutable after the first connection starts.
	fixture.app.sockets.revalidateEvery = maintenance
	fixture.app.sockets.pingEvery = time.Hour
	server := httptest.NewServer(fixture.handler)
	t.Cleanup(server.Close)
	return server
}

func websocketHTTPDial(t *testing.T, server *httptest.Server, path string, headers http.Header, wantStatus int) *websocketHTTPClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, response, err := websocket.Dial(ctx, server.URL+path, &websocket.DialOptions{
		HTTPClient: server.Client(), HTTPHeader: headers,
	})
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	if status != wantStatus || (wantStatus == http.StatusSwitchingProtocols) != (err == nil) {
		if conn != nil {
			_ = conn.CloseNow()
		}
		// Dial errors may contain credential-bearing URLs; never print them.
		t.Fatalf("WebSocket handshake status = %d, want %d; error type = %T", status, wantStatus, err)
	}
	if wantStatus != http.StatusSwitchingProtocols {
		return nil
	}
	client := &websocketHTTPClient{conn: conn, messages: make(chan []byte, 16), done: make(chan struct{})}
	readCtx, stopRead := context.WithCancel(context.Background())
	go func() {
		defer close(client.done)
		for {
			kind, payload, err := conn.Read(readCtx)
			if err != nil {
				client.readErr = err
				return
			}
			if kind != websocket.MessageText {
				client.readErr = io.ErrUnexpectedEOF
				return
			}
			select {
			case client.messages <- payload:
			case <-readCtx.Done():
				client.readErr = readCtx.Err()
				return
			}
		}
	}()
	t.Cleanup(func() {
		stopRead()
		_ = conn.CloseNow()
		select {
		case <-client.done:
		case <-time.After(3 * time.Second):
			t.Error("WebSocket fixture reader did not terminate")
		}
	})
	return client
}

func (client *websocketHTTPClient) ping(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.conn.Ping(ctx); err != nil {
		t.Fatalf("RFC WebSocket Ping failed (%T)", err)
	}
}

func (client *websocketHTTPClient) closed(t *testing.T) {
	t.Helper()
	select {
	case <-client.done:
		if client.readErr == nil {
			t.Error("closed WebSocket reader did not report termination")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("WebSocket remained connected past the closure deadline")
	}
}

func (client *websocketHTTPClient) next(t *testing.T) []byte {
	t.Helper()
	select {
	case payload := <-client.messages:
		return payload
	case <-client.done:
		t.Fatalf("WebSocket closed before its expected event (%T)", client.readErr)
	case <-time.After(3 * time.Second):
		t.Fatal("WebSocket did not receive its expected event")
	}
	return nil
}

func (client *websocketHTTPClient) quiet(t *testing.T) {
	t.Helper()
	select {
	case <-client.messages:
		t.Error("WebSocket received an unauthorized or unsolicited event")
	case <-client.done:
		t.Fatalf("unaffected WebSocket closed unexpectedly (%T)", client.readErr)
	case <-time.After(150 * time.Millisecond):
	}
}

func websocketHTTPWaitCount(t *testing.T, fixture *serverFixture, sessionID string, want int) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if count := fixture.app.eventHub.CountForSession(sessionID); count == want &&
			(want == 0 || fixture.app.hasClientControlTransport(sessionID)) {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("session WebSocket count did not become %d", want)
		}
	}
}

func websocketHTTPPost(t *testing.T, server *httptest.Server, path string, headers http.Header, wantStatus int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+path, nil)
	if err != nil {
		t.Fatalf("create WebSocket companion HTTP request (%T)", err)
	}
	request.Header = headers.Clone()
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("perform WebSocket companion HTTP request (%T)", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != wantStatus {
		t.Fatalf("WebSocket companion HTTP status = %d, want %d", response.StatusCode, wantStatus)
	}
}

func TestHTTPWebSocketAliasesAuthenticationLimitsAndInertMessages(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	server := websocketHTTPServer(t, f, time.Hour)
	for index, path := range []string{"/", "/emby", "/emby/", "/embywebsocket", "/emby/socket"} {
		headers := accounts.viewer.headers.Clone()
		if index%2 == 0 {
			path += "?" + url.Values{"api_key": {headers.Get("X-Emby-Token")}}.Encode()
			headers.Del("X-Emby-Token")
		}
		if index == 1 {
			headers.Set("Cookie", accounts.cookie.String())
			headers.Set("Origin", "https://cross-origin-client.example")
		}
		client := websocketHTTPDial(t, server, path, headers, http.StatusSwitchingProtocols)
		websocketHTTPWaitCount(t, f, accounts.viewer.id, 1)
		if f.app.eventHub.CountForSession(accounts.admin.id) != 0 {
			t.Error("administrator cookie changed the token-authenticated socket owner")
		}
		client.ping(t)
		_ = client.conn.CloseNow()
		client.closed(t)
		websocketHTTPWaitCount(t, f, accounts.viewer.id, 0)
	}
	for _, test := range []struct {
		name    string
		headers http.Header
		status  int
	}{
		{"missing token", nil, http.StatusUnauthorized},
		{"bad token", http.Header{"X-Emby-Token": {"invalid-token"}}, http.StatusUnauthorized},
		{"cookie only", http.Header{"Cookie": {accounts.cookie.String()}}, http.StatusUnauthorized},
		{"cookie and bad token", http.Header{"Cookie": {accounts.cookie.String()}, "X-Emby-Token": {"invalid-token"}}, http.StatusUnauthorized},
		{"cookie token has wrong kind", http.Header{"X-Emby-Token": {accounts.cookie.Value}}, http.StatusUnauthorized},
		{"malformed origin", http.Header{"X-Emby-Token": {accounts.viewer.headers.Get("X-Emby-Token")}, "Origin": {"invalid-origin"}}, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			websocketHTTPDial(t, server, "/emby/socket", test.headers, test.status)
		})
	}
	clients := make([]*websocketHTTPClient, 4)
	for index := range clients {
		clients[index] = websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	}
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 4)
	websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusTooManyRequests)
	_ = clients[0].conn.CloseNow()
	clients[0].closed(t)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 3)
	clients[0] = websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 4)
	type snapshot struct {
		userDataRows, playRows int
		capabilities           string
	}
	readState := func() snapshot {
		t.Helper()
		var state snapshot
		if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM user_item_data),
			(SELECT count(*) FROM play_sessions), client_capabilities::text FROM sessions WHERE id = $1`,
			accounts.viewer.id).Scan(&state.userDataRows, &state.playRows, &state.capabilities); err != nil {
			t.Fatal("read inert socket message state")
		}
		return state
	}
	before := readState()
	for _, kind := range []websocket.MessageType{websocket.MessageText, websocket.MessageBinary} {
		ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
		err := clients[0].conn.Write(ctx, kind, []byte(`{"MessageType":"UnimplementedClientOperation","Data":{"PositionTicks":123,"IsFavorite":true,"SupportsMediaControl":true}}`))
		cancel()
		if err != nil {
			t.Fatalf("send supported text/binary JSON envelope (%T)", err)
		}
	}
	// The returned Pong follows both input messages on the same connection.
	clients[0].ping(t)
	if after := readState(); after != before {
		t.Error("unsupported socket JSON mutated playback, user data, or capabilities")
	}
	clients[0].quiet(t)
}

func TestHTTPWebSocketLogoutDisconnectsOnlyItsSessionAndShutdownIsIdempotent(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	// Logout must interrupt connections independently of periodic maintenance.
	server := websocketHTTPServer(t, f, time.Hour)
	first := websocketHTTPDial(t, server, "/", accounts.viewer.headers, http.StatusSwitchingProtocols)
	second := websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	sibling := websocketHTTPDial(t, server, "/emby/", accounts.second.headers, http.StatusSwitchingProtocols)
	other := websocketHTTPDial(t, server, "/embywebsocket", accounts.other.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 2)
	websocketHTTPPost(t, server, "/emby/Sessions/Logout", accounts.viewer.headers, http.StatusOK)
	first.closed(t)
	second.closed(t)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 0)
	sibling.ping(t)
	other.ping(t)
	websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusUnauthorized)
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := f.app.Close(ctx)
		cancel()
		if err != nil {
			t.Fatalf("server Close attempt %d failed (%T)", attempt+1, err)
		}
	}
	sibling.closed(t)
	other.closed(t)
	websocketHTTPWaitCount(t, f, accounts.second.id, 0)
	websocketHTTPWaitCount(t, f, accounts.other.id, 0)
	select {
	case <-f.app.sockets.done:
	default:
		t.Error("server Close returned before hijacked socket workers stopped")
	}
}

func TestHTTPWebSocketMaintenanceClosesDisabledAndExpiredPrincipals(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	server := websocketHTTPServer(t, f, 50*time.Millisecond)
	first := websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	second := websocketHTTPDial(t, server, "/emby/socket", accounts.second.headers, http.StatusSwitchingProtocols)
	other := websocketHTTPDial(t, server, "/emby/socket", accounts.other.headers, http.StatusSwitchingProtocols)
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = true WHERE id = $1", accounts.viewer.userID); err != nil {
		t.Fatal("disable connected WebSocket account")
	}
	first.closed(t)
	second.closed(t)
	other.ping(t)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 0)
	websocketHTTPWaitCount(t, f, accounts.second.id, 0)
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = false WHERE id = $1", accounts.viewer.userID); err != nil {
		t.Fatal("restore WebSocket account")
	}
	first = websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	second = websocketHTTPDial(t, server, "/emby/socket", accounts.second.headers, http.StatusSwitchingProtocols)
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET created_at = now() - interval '31 days',
		expires_at = now() - interval '1 second' WHERE id = $1`, accounts.viewer.id); err != nil {
		t.Fatal("expire connected WebSocket authentication")
	}
	first.closed(t)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 0)
	second.ping(t)
	other.ping(t)
	websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusUnauthorized)
}

func websocketHTTPPublishedEvent(t *testing.T, envelope events.Envelope, userID string) events.Event {
	t.Helper()
	hub, err := events.New(events.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	subscription, err := hub.Subscribe(events.Scope{UserID: userID, SessionID: "payload-fixture-session"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hub.PublishUser(userID, envelope); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := subscription.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func websocketHTTPUserData(t *testing.T, payload []byte) (events.Envelope, string, []map[string]any) {
	t.Helper()
	var envelope events.Envelope
	var data struct {
		UserID       string           `json:"UserId"`
		UserDataList []map[string]any `json:"UserDataList"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.MessageType != "UserDataChanged" || envelope.MessageID == "" {
		t.Fatal("socket event must be a UserDataChanged envelope with a stable MessageId")
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil || data.UserID == "" || data.UserDataList == nil {
		t.Fatal("socket event has invalid user data scope or list")
	}
	return envelope, data.UserID, data.UserDataList
}

func TestHTTPWebSocketUserDataFiltersCurrentACLAndSharesPublicationIdentity(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	const visibleLibrary, hiddenLibrary = "socket-visible-library", "socket-hidden-library"
	const visibleItem, hiddenItem = "socket-visible-item", "socket-hidden-item"
	for _, entry := range []struct{ libraryID, itemID string }{{visibleLibrary, visibleItem}, {hiddenLibrary, hiddenItem}} {
		if _, err := f.pool.Exec(f.ctx, "INSERT INTO libraries (id, name, collection_type) VALUES ($1, $1, 'movies')", entry.libraryID); err != nil {
			t.Fatal("create socket event catalog library")
		}
		if _, err := f.pool.Exec(f.ctx, "INSERT INTO items (id, library_id, name, sort_name, type) VALUES ($1, $2, $1, $1, 'Movie')", entry.itemID, entry.libraryID); err != nil {
			t.Fatal("create socket event catalog item")
		}
	}
	principal, err := f.users.Resolve(f.ctx, accounts.viewer.headers.Get("X-Emby-Token"), "emby")
	if err != nil {
		t.Fatal("resolve socket payload viewer")
	}
	data, err := json.Marshal(map[string]any{"UserId": accounts.viewer.userID, "UserDataList": []map[string]any{
		{"ItemId": visibleItem, "IsFavorite": true}, {"ItemId": hiddenItem, "IsFavorite": true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	event := websocketHTTPPublishedEvent(t, events.Envelope{MessageType: "UserDataChanged", MessageID: "stable-filtered-publication", Data: data}, accounts.viewer.userID)
	before := event.Bytes()
	setFolders := func(folders []string) {
		t.Helper()
		policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": folders, "EnableMediaPlayback": true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy = $2::jsonb WHERE id = $1", accounts.viewer.userID, policy); err != nil {
			t.Fatal("update socket event resource policy")
		}
	}
	// Publish before revoking access, then deliver with a formerly valid policy.
	setFolders([]string{visibleLibrary})
	payload, err := f.app.socketEventPayload(f.ctx, principal, event)
	if err != nil {
		t.Fatalf("filter current socket event resource access (%T)", err)
	}
	envelope, userID, list := websocketHTTPUserData(t, payload)
	if envelope.MessageID != event.MessageID() || userID != accounts.viewer.userID || len(list) != 1 ||
		list[0]["ItemId"] != visibleItem || list[0]["IsFavorite"] != true {
		t.Error("ACL filtering changed publication identity, immutable data, or visible items")
	}
	if !bytes.Equal(before, event.Bytes()) {
		t.Error("recipient filtering mutated the published event")
	}
	// Database defaults are false. Delivery must retain the published true value.
	current, err := f.app.library.GetUserData(f.ctx, accounts.viewer.userID, visibleItem)
	if err != nil || current.IsFavorite {
		t.Fatal("socket event fixture must have a different current favorite value")
	}
	for _, login := range []clientSessionHTTPLogin{accounts.other, accounts.admin} {
		other, err := f.users.Resolve(f.ctx, login.headers.Get("X-Emby-Token"), "emby")
		if err != nil {
			t.Fatal("resolve unrelated socket payload recipient")
		}
		if payload, err := f.app.socketEventPayload(f.ctx, other, event); err == nil || len(payload) != 0 {
			t.Error("another account received a private user data publication")
		}
	}
	setFolders([]string{})
	if payload, err := f.app.socketEventPayload(f.ctx, principal, event); err != nil || len(payload) != 0 {
		t.Error("fully revoked event should produce no application frame")
	}
	setFolders([]string{visibleLibrary})
	server := websocketHTTPServer(t, f, time.Hour)
	first := websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	second := websocketHTTPDial(t, server, "/emby/socket", accounts.second.headers, http.StatusSwitchingProtocols)
	other := websocketHTTPDial(t, server, "/emby/socket", accounts.other.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 1)
	websocketHTTPWaitCount(t, f, accounts.second.id, 1)
	websocketHTTPPost(t, server, "/emby/Users/"+accounts.viewer.userID+"/FavoriteItems/"+visibleItem, accounts.viewer.headers, http.StatusOK)
	firstPayload, secondPayload := first.next(t), second.next(t)
	firstEnvelope, firstUser, firstList := websocketHTTPUserData(t, firstPayload)
	secondEnvelope, secondUser, secondList := websocketHTTPUserData(t, secondPayload)
	if firstEnvelope.MessageID != secondEnvelope.MessageID || !bytes.Equal(firstPayload, secondPayload) ||
		firstUser != accounts.viewer.userID || secondUser != accounts.viewer.userID ||
		len(firstList) != 1 || len(secondList) != 1 || firstList[0]["ItemId"] != visibleItem || firstList[0]["IsFavorite"] != true {
		t.Error("one committed favorite change did not yield one shared user-scoped publication")
	}
	other.ping(t)
	other.quiet(t)
}

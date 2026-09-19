//go:build linux

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/moooyo/goby/internal/events"
)

func sendSessionSubscription(t *testing.T, client *websocketHTTPClient, kind string, data any) {
	t.Helper()
	message := map[string]any{"MessageType": kind}
	if data != nil {
		message["Data"] = data
	}
	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.conn.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatalf("send session subscription (%T)", err)
	}
}

func sessionSubscriptionSnapshot(t *testing.T, client *websocketHTTPClient) []map[string]any {
	t.Helper()
	var envelope events.Envelope
	if err := json.Unmarshal(client.next(t), &envelope); err != nil || envelope.MessageType != "Sessions" {
		t.Fatalf("missing session snapshot (%T)", err)
	}
	var sessions []map[string]any
	if err := json.Unmarshal(envelope.Data, &sessions); err != nil || sessions == nil {
		t.Fatal("Sessions must contain a JSON array")
	}
	for _, session := range sessions {
		assertClientSessionHTTPPrivate(t, session)
	}
	return sessions
}

func TestHTTPWebSocketSessionsRefreshIsPerConnectionAndRechecksPolicy(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	server := websocketHTTPServer(t, f, time.Hour)
	viewer := websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	idle := websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 2)
	sendSessionSubscription(t, viewer, "SessionsStart", "0,1000")
	assertClientSessionHTTPIDs(t, sessionSubscriptionSnapshot(t, viewer), accounts.viewer.id, accounts.second.id)
	idle.quiet(t)
	third := loginClientSessionHTTP(t, f, "Session Viewer", "session-viewer-password", "session-viewer-three")
	assertClientSessionHTTPIDs(t, sessionSubscriptionSnapshot(t, viewer), accounts.viewer.id, accounts.second.id, third.id)
	// A broadening and narrowing policy change each affects the next snapshot;
	// the original principal must not retain cross-user control authority.
	setHTTPUserPolicy(t, f, accounts.viewer.userID, `{"EnableRemoteControlOfOtherUsers":true}`)
	assertClientSessionHTTPIDs(t, sessionSubscriptionSnapshot(t, viewer), accounts.admin.id, accounts.viewer.id, accounts.second.id, third.id, accounts.other.id)
	setHTTPUserPolicy(t, f, accounts.viewer.userID, `{}`)
	assertClientSessionHTTPIDs(t, sessionSubscriptionSnapshot(t, viewer), accounts.viewer.id, accounts.second.id, third.id)
	websocketHTTPPost(t, server, "/emby/Sessions/Logout", accounts.viewer.headers, http.StatusNoContent)
	viewer.closed(t)
	idle.closed(t)
}

func TestHTTPWebSocketSessionsStopCancelsPendingRefreshAndCanRestart(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	server := websocketHTTPServer(t, f, time.Hour)
	client := websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 1)
	sendSessionSubscription(t, client, "SessionsStart", "1000,1000")
	sendSessionSubscription(t, client, "SessionsStop", nil)
	client.ping(t)
	select {
	case <-client.messages:
		t.Fatal("stopped subscription emitted its delayed snapshot")
	case <-client.done:
		t.Fatal("stopping a subscription closed its transport")
	case <-time.After(1250 * time.Millisecond):
	}
	sendSessionSubscription(t, client, "SessionsStart", "0,60000")
	assertClientSessionHTTPIDs(t, sessionSubscriptionSnapshot(t, client), accounts.viewer.id, accounts.second.id)
	sendSessionSubscription(t, client, "SessionsStart", "0,999")
	client.closed(t)
	if status := websocket.CloseStatus(client.readErr); status != websocket.StatusPolicyViolation {
		t.Fatalf("invalid subscription close code = %d", status)
	}
}

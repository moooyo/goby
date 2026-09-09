//go:build linux

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

type applicationSessionContext struct {
	principal identity.Principal
	headers   http.Header
}

func newApplicationSessionContext(t *testing.T, f *serverFixture, key applicationMediaKey, suffix string) applicationSessionContext {
	t.Helper()
	client := identity.Client{Name: "GobyContext-" + suffix, DeviceID: "context-device-" + suffix,
		Device: "Context Device " + suffix, Version: "9.8.7"}
	headers := key.headers.Clone()
	headers.Set("Authorization", `Emby Client="`+client.Name+`", DeviceId="`+client.DeviceID+`", Device="`+client.Device+`", Version="`+client.Version+`"`)
	sessions := clientSessionHTTPArray(t, f.request(t, http.MethodGet, "/emby/Sessions?DeviceId="+url.QueryEscape(client.DeviceID), nil, headers))
	if len(sessions) != 1 || sessions[0]["Client"] != client.Name || sessions[0]["DeviceName"] != client.Device || sessions[0]["ApplicationVersion"] != client.Version {
		t.Fatal("authorization metadata did not create one matching application client")
	}
	principal, err := f.users.ResolveEmbyForClient(f.ctx, key.key.Token, client)
	if err != nil || principal.User.ID != "" || principal.SessionID != key.key.CredentialID || principal.ClientSessionID != sessions[0]["Id"] {
		t.Fatal("application client did not retain its independent parent and context identifiers")
	}
	return applicationSessionContext{principal: principal, headers: headers}
}

func applicationContextSessionDTO(t *testing.T, f *serverFixture, headers http.Header, id string) map[string]any {
	t.Helper()
	sessions := clientSessionHTTPArray(t, f.request(t, http.MethodGet, "/emby/Sessions?Id="+url.QueryEscape(id), nil, headers))
	assertClientSessionHTTPIDs(t, sessions, id)
	for _, name := range []string{"UserId", "UserName", "ExpiresAt", "CredentialId", "ApplicationKeyId"} {
		if _, exists := sessions[0][name]; exists {
			t.Errorf("userless session exposed %s", name)
		}
	}
	assertClientSessionHTTPPrivate(t, sessions[0])
	return sessions[0]
}

func TestHTTPApplicationClientContextsKeepCapabilitiesAndNowPlayingIndependent(t *testing.T) {
	fixture := newApplicationMediaFixture(t)
	f, key := fixture.f, fixture.keys[0]
	alpha := newApplicationSessionContext(t, f, key, "alpha")
	beta := newApplicationSessionContext(t, f, key, "beta")
	if alpha.principal.ClientSessionID == beta.principal.ClientSessionID || alpha.principal.ClientSessionID == key.principal.ClientSessionID ||
		alpha.principal.SessionID != beta.principal.SessionID {
		t.Fatal("application client contexts collapsed together or issued another credential")
	}
	contexts := []applicationSessionContext{alpha, beta}
	items := []streamHTTPItem{fixture.stream.video, fixture.stream.audio}
	commands := []string{"SetVolume", "DisplayMessage"}
	before := applicationMediaSnapshot(t, f)
	for index, client := range contexts {
		capabilities := map[string]any{"PlayableMediaTypes": []string{[]string{"Video", "Audio"}[index]},
			"SupportedCommands": []string{commands[index]}, "SupportsMediaControl": index == 0}
		// A stale or foreign Id hint must remain inert for the authenticated context.
		target := "/emby/Sessions/Capabilities/Full?Id=" + contexts[1-index].principal.ClientSessionID
		applicationMediaStatus(t, f.request(t, http.MethodPost, target, capabilities, client.headers), http.StatusNoContent)
		response := f.request(t, http.MethodGet, "/emby/Items/"+items[index].id+"/PlaybackInfo?DeviceId=unrelated-query-device", nil, client.headers)
		applicationMediaStatus(t, response, http.StatusOK)
		object, _ := playbackHTTPSource(t, response)
		playID := stringValue(t, object, "PlaySessionId")
		body := map[string]any{"PlaySessionId": playID, "SessionId": client.principal.ClientSessionID,
			"ItemId": items[index].id, "MediaSourceId": media.SourceID(items[index].id),
			"PositionTicks": int64(index+1) * media.TicksPerSecond}
		applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing", body, client.headers), http.StatusNoContent)
	}
	for index, client := range contexts {
		dto := applicationContextSessionDTO(t, f, beta.headers, client.principal.ClientSessionID)
		if !reflect.DeepEqual(dto["SupportedCommands"], []any{commands[index]}) || dto["DeviceId"] != client.principal.Client.DeviceID {
			t.Fatal("application contexts shared capabilities or query DeviceId overwrote authorization metadata")
		}
		if item := objectValue(t, dto, "NowPlayingItem"); item["Id"] != items[index].id {
			t.Fatal("application context displayed a sibling client's now playing item")
		}
		if state := objectValue(t, dto, "PlayState"); state["PositionTicks"] != float64(int64(index+1)*media.TicksPerSecond) {
			t.Fatal("application context displayed a sibling client's playback position")
		}
	}
	baseline := applicationContextSessionDTO(t, f, alpha.headers, key.principal.ClientSessionID)
	assertClientSessionHTTPIdle(t, baseline)
	if after := applicationMediaSnapshot(t, f); after != before {
		t.Fatal("application sessions or playback fabricated personal user state")
	}
}

func TestHTTPApplicationClientWebSocketsRouteCommandsAndRevokeAllParentContexts(t *testing.T) {
	fixture := newApplicationMediaFixture(t)
	f, key := fixture.f, fixture.keys[0]
	alpha := newApplicationSessionContext(t, f, key, "alpha")
	beta := newApplicationSessionContext(t, f, key, "beta")
	if _, err := f.users.CreateUser(f.ctx, "Context Viewer", "context-viewer-password", false); err != nil {
		t.Fatalf("create context viewer (%T)", err)
	}
	viewer := loginClientSessionHTTP(t, f, "Context Viewer", "context-viewer-password", "context-viewer")
	sibling := fixture.keys[1]
	for _, headers := range []http.Header{alpha.headers, beta.headers, sibling.headers, viewer.headers} {
		applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full",
			map[string]any{"SupportsMediaControl": true, "SupportedCommands": []string{"VolumeUp"}}, headers), http.StatusNoContent)
	}
	server := websocketHTTPServer(t, f, time.Hour)
	alphaSocket := websocketHTTPDial(t, server, "/emby/socket", alpha.headers, http.StatusSwitchingProtocols)
	betaSocket := websocketHTTPDial(t, server, "/emby/socket", beta.headers, http.StatusSwitchingProtocols)
	siblingSocket := websocketHTTPDial(t, server, "/emby/socket", sibling.headers, http.StatusSwitchingProtocols)
	viewerSocket := websocketHTTPDial(t, server, "/emby/socket", viewer.headers, http.StatusSwitchingProtocols)
	for _, id := range []string{alpha.principal.ClientSessionID, beta.principal.ClientSessionID, sibling.principal.ClientSessionID, viewer.id} {
		websocketHTTPWaitCount(t, f, id, 1)
	}
	if f.app.eventHub.CountForSession(key.principal.SessionID) != 0 || f.app.hasClientControlTransport(key.principal.SessionID) {
		t.Fatal("application transport presence was recorded against a parent credential")
	}
	for _, client := range []applicationSessionContext{alpha, beta} {
		if dto := applicationContextSessionDTO(t, f, sibling.headers, client.principal.ClientSessionID); dto["SupportsRemoteControl"] != true {
			t.Fatal("connected application client did not advertise its own declared control transport")
		}
	}
	commandPath := "/emby/Sessions/" + beta.principal.ClientSessionID + "/Playing/Pause"
	applicationMediaStatus(t, f.request(t, http.MethodPost, commandPath, nil, viewer.headers), http.StatusNotFound)
	applicationMediaStatus(t, f.request(t, http.MethodPost, commandPath, nil, alpha.headers), http.StatusNoContent)
	var received struct {
		MessageType string
		Data        map[string]any
	}
	if err := json.Unmarshal(betaSocket.next(t), &received); err != nil || received.MessageType != "Playstate" ||
		received.Data["Id"] != beta.principal.ClientSessionID || received.Data["Command"] != "Pause" {
		t.Fatal("application command missed its exact target context")
	}
	if _, exists := received.Data["ControllingUserId"]; exists {
		t.Fatal("application command exposed a synthetic controlling user")
	}
	for _, client := range []*websocketHTTPClient{alphaSocket, siblingSocket, viewerSocket} {
		client.quiet(t)
	}
	data, _ := json.Marshal(map[string]any{"UserId": viewer.userID, "UserDataList": []map[string]any{{"ItemId": fixture.stream.video.id}}})
	if count, err := f.app.eventHub.PublishUser(viewer.userID, events.Envelope{MessageType: "UserDataChanged", Data: data}); err != nil || count != 1 {
		t.Fatal("user event publication entered an application scope")
	}
	if err := json.Unmarshal(viewerSocket.next(t), &received); err != nil || received.MessageType != "UserDataChanged" {
		t.Fatal("ordinary user notification was not delivered")
	}
	for _, client := range []*websocketHTTPClient{alphaSocket, betaSocket, siblingSocket} {
		client.quiet(t)
	}
	// The HTTP revoke hook must close every context immediately, independently
	// of the maintenance interval, while sibling credentials remain connected.
	applicationMediaStatus(t, f.request(t, http.MethodDelete, "/emby/Auth/Keys/"+url.PathEscape(key.key.Token), nil, sibling.headers), http.StatusNoContent)
	alphaSocket.closed(t)
	betaSocket.closed(t)
	websocketHTTPWaitCount(t, f, alpha.principal.ClientSessionID, 0)
	websocketHTTPWaitCount(t, f, beta.principal.ClientSessionID, 0)
	siblingSocket.ping(t)
	viewerSocket.ping(t)
	for _, headers := range []http.Header{alpha.headers, beta.headers} {
		applicationMediaStatus(t, f.request(t, http.MethodGet, "/emby/Sessions", nil, headers), http.StatusUnauthorized)
	}
	siblingSocket.quiet(t)
	viewerSocket.quiet(t)
}

func applicationQueuedCommand(t *testing.T, envelope events.Envelope, receiver identity.Principal) events.Event {
	t.Helper()
	hub, err := events.New(events.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	sub, err := hub.Subscribe(clientEventScope(receiver))
	if err != nil {
		t.Fatal(err)
	}
	if count, err := hub.PublishScope(clientEventScope(receiver), envelope); err != nil || count != 1 {
		t.Fatal("queue application command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestApplicationRemoteCommandsRecheckKeyAuthorityAndTargetPolicy(t *testing.T) {
	fixture := newApplicationMediaFixture(t)
	f, actor, receiver := fixture.f, fixture.keys[0], fixture.keys[1]
	applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full",
		map[string]any{"SupportsMediaControl": true}, receiver.headers), http.StatusNoContent)
	target := identity.ClientSession{Kind: identity.ApplicationKeyKind, ApplicationKeyID: receiver.key.ID,
		CredentialID: receiver.principal.SessionID, SessionID: receiver.principal.ClientSessionID}
	envelope, err := remoteCommandEnvelope("Play", map[string]any{"ItemIds": []string{fixture.stream.video.id}, "PlayCommand": "PlayNow"}, actor.principal, target, true)
	if err != nil {
		t.Fatal(err)
	}
	queued := applicationQueuedCommand(t, envelope, receiver.principal)
	if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver.principal, queued); err != nil || !allowed {
		t.Fatal("active application controller or userless target was rejected")
	}
	ordinary := loginClientSessionHTTP(t, f, "Denied media context", "media-context-password", "restricted-remote-target")
	playBody := map[string]any{"ItemIds": []string{fixture.stream.video.id}, "PlayCommand": "PlayNow"}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy = '{"EnableMediaPlayback":false,"EnableAllFolders":true}'::jsonb WHERE id = $1`, ordinary.userID); err != nil {
		t.Fatalf("restrict remote target playback (%T)", err)
	}
	applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/"+ordinary.id+"/Playing", playBody, actor.headers), http.StatusForbidden)
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy = '{"EnableMediaPlayback":true,"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id = $1`, ordinary.userID); err != nil {
		t.Fatalf("restrict remote target catalog (%T)", err)
	}
	applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/"+ordinary.id+"/Playing", playBody, actor.headers), http.StatusNotFound)
	applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full", map[string]any{}, receiver.headers), http.StatusNoContent)
	if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver.principal, queued); err != nil || allowed {
		t.Fatal("application command survived target capability removal")
	}
	applicationMediaStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full",
		map[string]any{"SupportsMediaControl": true}, receiver.headers), http.StatusNoContent)
	if _, err := f.users.RevokeApplicationKey(f.ctx, receiver.principal, actor.key.ID); err != nil {
		t.Fatalf("revoke queued command authority (%T)", err)
	}
	if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver.principal, queued); err != nil || allowed {
		t.Fatal("queued command survived application controller revocation")
	}
	forged := envelope
	forged.Authority = events.Authority{}
	if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver.principal, applicationQueuedCommand(t, forged, receiver.principal)); err != nil || allowed {
		t.Fatal("a userless wire body established application controller authority")
	}
}

//go:build linux

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func nativePlaybackHTTPItems(t *testing.T, f *serverFixture, cookie *http.Cookie) []map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, "/admin/v1/playback/sessions", nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	page := jsonObject(t, response)
	values, ok := page["Items"].([]any)
	if !ok || page["TotalRecordCount"] != float64(len(values)) || page["Limit"] != float64(identity.MaxClientSessions) {
		t.Fatal("native playback snapshot lost its bounded, non-null envelope")
	}
	items := make([]map[string]any, 0, len(values))
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			t.Fatal("native playback snapshot contained a non-object session")
		}
		for _, forbidden := range []string{"CredentialId", "AccessToken", "Token", "Password", "PushToken"} {
			if _, present := item[forbidden]; present {
				t.Fatal("native playback snapshot leaked a credential field")
			}
		}
		items = append(items, item)
	}
	return items
}

func TestHTTPAdminPlaybackKeepsCookieCSRFAndEmbyBoundaries(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	csrf := adminSessionHTTPCSRF(t, f, accounts.cookie)
	path := "/admin/v1/playback/sessions/" + accounts.viewer.id + "/command"
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/admin/v1/playback/sessions", nil},
		{http.MethodPost, path, map[string]any{"Command": "Stop"}},
		{http.MethodPost, "/admin/v1/playback/message", map[string]any{"Text": "Maintenance"}},
	} {
		expectStatus(t, f.request(t, route.method, route.path, route.body, nil), http.StatusUnauthorized)
		expectStatus(t, f.request(t, route.method, route.path, route.body, accounts.admin.headers), http.StatusUnauthorized)
		expectStatus(t, f.request(t, route.method, route.path, route.body, nil, &http.Cookie{Name: sessionCookie, Value: accounts.admin.headers.Get("X-Emby-Token")}), http.StatusUnauthorized)
	}
	expectAPIError(t, f.request(t, http.MethodPost, path, map[string]any{"Command": "Stop"}, nil, accounts.cookie), http.StatusForbidden, "csrf_invalid", false)
	expectAPIError(t, f.request(t, http.MethodPost, path, map[string]any{"Command": "Stop"}, http.Header{
		"X-CSRF-Token": {csrf}, "Origin": {"https://foreign.example.test"},
	}, accounts.cookie), http.StatusForbidden, "origin_denied", false)
	items := nativePlaybackHTTPItems(t, f, accounts.cookie)
	if len(items) != 4 {
		t.Fatal("native playback list included an administrator-cookie target or lost client presence")
	}
	for _, item := range items {
		if item["TransportConnected"] != false || objectValue(t, item, "Controls")["Stop"] != false {
			t.Fatal("native playback list fabricated transport or playback availability")
		}
	}
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/playback/sessions?api_key=ignored", nil, nil, accounts.cookie), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{"Command": "DisplayMessage", "Text": "Maintenance", "ControllingUserId": accounts.viewer.userID}, http.Header{"X-CSRF-Token": {csrf}}, accounts.cookie), http.StatusBadRequest)
	actor, err := f.users.Resolve(f.ctx, accounts.cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.users.ListClientSessionSummaries(f.ctx, actor, identity.ClientSessionFilter{}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("native playback support broadened the Emby session-list credential audience")
	}
	if _, err := f.users.RevalidateSessionAuthority(f.ctx, actor); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("native playback support turned an administrator cookie into an Emby identity")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator=false WHERE id=$1", actor.User.ID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/playback/sessions", nil, nil, accounts.cookie), http.StatusUnauthorized)
}

func nextNativePlaybackEvent(t *testing.T, client *websocketHTTPClient, kind string) events.Envelope {
	t.Helper()
	for count := 0; count < 8; count++ {
		var envelope events.Envelope
		if json.Unmarshal(client.next(t), &envelope) != nil {
			t.Fatal("native playback control emitted an invalid client event")
		}
		if envelope.MessageType == kind {
			return envelope
		}
		if envelope.MessageType != "UserDataChanged" {
			t.Fatal("native playback control emitted an unrelated event")
		}
	}
	t.Fatal("native playback control did not reach the connected client")
	return events.Envelope{}
}

func TestHTTPAdminPlaybackQueuesRealCommandsWithoutFabricatingReports(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	f := p.s.f
	csrf := adminSessionHTTPCSRF(t, f, p.s.cookie)
	prepared, _ := p.prepare(t, matchingPlaybackHTTPBody())
	playID := stringValue(t, prepared, "PlaySessionId")
	p.report(t, "Started", playID, 0)
	report := map[string]any{"PlaySessionId": playID, "ItemId": p.s.video.id, "PositionTicks": 120 * media.TicksPerSecond, "CanSeek": true, "IsPaused": false}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Progress", report, p.headers), http.StatusNoContent)
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full", map[string]any{
		"SupportsMediaControl": true, "PlayableMediaTypes": []string{"Video"}, "SupportedCommands": []string{"DisplayMessage"},
	}, p.headers), http.StatusNoContent)
	before := readPlaybackHTTPState(t, p, playID)
	path := "/admin/v1/playback/sessions/" + p.authSessionID + "/command"
	headers := http.Header{"X-CSRF-Token": {csrf}}
	expectAPIError(t, f.request(t, http.MethodPost, path, map[string]any{"Command": "PlayNow", "ItemIds": []string{p.s.video.id, p.secondItemID}}, headers, p.s.cookie), http.StatusBadRequest, "single_playback_item_required", false)
	expectAPIError(t, f.request(t, http.MethodPost, path, map[string]any{"Command": "Stop", "PlaySessionId": playID}, headers, p.s.cookie), http.StatusConflict, "playback_client_offline", false)
	client := websocketHTTPDial(t, p.s.server, "/embywebsocket", p.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, p.authSessionID, 1)
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full", map[string]any{
		"SupportsMediaControl": true, "PlayableMediaTypes": []string{"Audio"}, "SupportedCommands": []string{"DisplayMessage"},
	}, p.headers), http.StatusNoContent)
	expectAPIError(t, f.request(t, http.MethodPost, path, map[string]any{"Command": "PlayNow", "ItemIds": []string{p.s.video.id}}, headers, p.s.cookie), http.StatusConflict, "playback_command_unsupported", false)
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full", map[string]any{
		"SupportsMediaControl": true, "PlayableMediaTypes": []string{"Video"}, "SupportedCommands": []string{"DisplayMessage"},
	}, p.headers), http.StatusNoContent)
	items := nativePlaybackHTTPItems(t, f, p.s.cookie)
	var current map[string]any
	for _, item := range items {
		if item["Id"] == p.authSessionID {
			current = item
		}
	}
	if current == nil || current["PlaySessionId"] != playID || objectValue(t, current, "NowPlayingItem")["Id"] != p.s.video.id || objectValue(t, current, "Controls")["Seek"] != true {
		t.Fatal("native playback did not project the real current media and seek capability")
	}
	response := f.request(t, http.MethodPost, path, map[string]any{"Command": "Pause", "PlaySessionId": playID}, headers, p.s.cookie)
	expectStatus(t, response, http.StatusAccepted)
	receipt := jsonObject(t, response)
	if receipt["Status"] != "queued" || receipt["ExecutionConfirmed"] != false || receipt["DeliveryCount"] != float64(1) {
		t.Fatal("native playback receipt confused queue admission with client execution")
	}
	event := nextNativePlaybackEvent(t, client, "Playstate")
	var command map[string]any
	if json.Unmarshal(event.Data, &command) != nil || command["Command"] != "Pause" || command["PlaySessionId"] != playID || command["Id"] != p.authSessionID {
		t.Fatal("native playback did not send the bounded command to the expected client")
	}
	if after := readPlaybackHTTPState(t, p, playID); after != before {
		t.Fatal("native playback queue admission changed authoritative playback or user state")
	}
	expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{"Command": "Seek", "SeekPositionTicks": "300000000", "PlaySessionId": "stale-playback"}, headers, p.s.cookie), http.StatusNotFound)
	response = f.request(t, http.MethodPost, "/admin/v1/playback/message", map[string]any{"Text": "Maintenance in five minutes", "Header": "Server", "TimeoutMs": 8000}, headers, p.s.cookie)
	expectStatus(t, response, http.StatusAccepted)
	results, ok := jsonObject(t, response)["Results"].([]any)
	if !ok || len(results) != 1 || results[0].(map[string]any)["Status"] != "queued" {
		t.Fatal("native broadcast did not report its actual per-playing-client queue result")
	}
	event = nextNativePlaybackEvent(t, client, "GeneralCommand")
	if json.Unmarshal(event.Data, &command) != nil || command["Name"] != "DisplayMessage" || objectValue(t, command, "Arguments")["Text"] != "Maintenance in five minutes" {
		t.Fatal("native message was not delivered through the authenticated control transport")
	}
	if after := readPlaybackHTTPState(t, p, playID); after != before {
		t.Fatal("native message or stale seek changed authoritative playback state")
	}
	// Only an authenticated player report establishes the resulting state.
	report["IsPaused"] = true
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Progress", report, p.headers), http.StatusNoContent)
	for _, item := range nativePlaybackHTTPItems(t, f, p.s.cookie) {
		if item["Id"] == p.authSessionID && objectValue(t, item, "PlayState")["IsPaused"] != true {
			t.Fatal("native playback did not reflect the subsequently authenticated pause report")
		}
	}
}

func TestQueuedNativePlaybackRechecksCookieAndTargetCapabilities(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	controller, err := f.users.Resolve(f.ctx, accounts.cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := f.users.Resolve(f.ctx, accounts.viewer.headers.Get("X-Emby-Token"), "emby")
	if err != nil {
		t.Fatal(err)
	}
	capabilities := map[string]any{"SupportsMediaControl": true, "SupportedCommands": []string{"DisplayMessage"}}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full", capabilities, accounts.viewer.headers), http.StatusNoContent)
	makeEvent := func(actor identity.Principal) events.Event {
		t.Helper()
		data := map[string]any{"Name": "DisplayMessage", "Arguments": map[string]string{"Text": "Maintenance"}}
		return remoteDeliveryEvent(t, actor, receiver, "GeneralCommand", data)
	}
	event := makeEvent(controller)
	if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, event); err != nil || !allowed {
		t.Fatalf("current native cookie could not authorize its queued message: %v", err)
	}
	for _, change := range []struct {
		name, query string
		id          string
	}{
		{"demoted", "UPDATE users SET is_administrator=false WHERE id=$1", controller.User.ID},
		{"disabled", "UPDATE users SET is_disabled=true WHERE id=$1", controller.User.ID},
		{"revoked", "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", controller.SessionID},
		{"expired", "UPDATE sessions SET created_at=clock_timestamp()-interval '2 days', expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", controller.SessionID},
		{"feature-denied", `UPDATE users SET policy='{"RestrictedFeatures":["goby_remote_control"]}'::jsonb WHERE id=$1`, controller.User.ID},
	} {
		t.Run(change.name, func(t *testing.T) {
			if _, err := f.pool.Exec(f.ctx, change.query, change.id); err != nil {
				t.Fatal(err)
			}
			if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, event); err != nil || allowed {
				t.Errorf("queued native command survived a current cookie or account denial: allowed=%t error=%v", allowed, err)
			}
			if _, err := f.pool.Exec(f.ctx, `UPDATE users SET is_administrator=true,is_disabled=false,policy='{}'::jsonb WHERE id=$1`, controller.User.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at=NULL,expires_at=clock_timestamp()+interval '1 day' WHERE id=$1", controller.SessionID); err != nil {
				t.Fatal(err)
			}
		})
	}
	capabilities["SupportedCommands"] = []string{}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full", capabilities, accounts.viewer.headers), http.StatusNoContent)
	if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, event); err != nil || allowed {
		t.Fatal("queued native message survived removal of the receiver's DisplayMessage capability")
	}
	// Changing only a trusted-metadata kind cannot promote an Emby credential.
	forged, err := f.users.Resolve(f.ctx, accounts.admin.headers.Get("X-Emby-Token"), "emby")
	if err != nil {
		t.Fatal(err)
	}
	forged.Kind = "admin"
	if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, makeEvent(forged)); err != nil || allowed {
		t.Fatal("an Emby credential was accepted as an administrator-cookie controller")
	}
}

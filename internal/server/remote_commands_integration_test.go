//go:build linux

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func expectRemoteCommandAccepted(t *testing.T, f *serverFixture, path string, body any, headers http.Header) {
	t.Helper()
	response := f.request(t, http.MethodPost, path, body, headers)
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("remote command status = %d, want empty 204", response.Code)
	}
}

func TestHTTPRemoteCommandsUseCurrentOwnersAndAdministratorsWithoutTransport(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	for _, test := range []struct {
		actor  clientSessionHTTPLogin
		target clientSessionHTTPLogin
	}{
		{accounts.viewer, accounts.viewer},
		{accounts.viewer, accounts.second},
		{accounts.admin, accounts.other},
	} {
		if f.app.hasClientControlTransport(test.target.id) {
			t.Fatal("HTTP fixture unexpectedly has a remote-control transport")
		}
		base := "/emby/Sessions/" + test.target.id
		expectRemoteCommandAccepted(t, f, base+"/Playing/Pause", map[string]any{"Command": "Pause"}, test.actor.headers)
		expectRemoteCommandAccepted(t, f, base+"/Command/VolumeUp", map[string]any{"Name": "Ignored", "Arguments": map[string]any{"Volume": 37}}, test.actor.headers)
		expectRemoteCommandAccepted(t, f, base+"/Command", map[string]any{"Name": "SetVolume", "Arguments": map[string]string{"Volume": "37"}}, test.actor.headers)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET last_seen_at = now() - interval '1 hour' WHERE id = $1", accounts.second.id); err != nil {
		t.Fatal(err)
	}
	expectRemoteCommandAccepted(t, f, "/emby/Sessions/"+accounts.second.id+"/Playing/Stop", nil, accounts.viewer.headers)
	for _, target := range []string{accounts.other.id, "unknown-target"} {
		response := f.request(t, http.MethodPost, "/emby/Sessions/"+target+"/Playing/Pause", nil, accounts.viewer.headers)
		expectStatus(t, response, http.StatusNotFound)
	}
	for _, headers := range []http.Header{nil, {"X-Emby-Token": {accounts.cookie.Value}}} {
		response := f.request(t, http.MethodPost, "/emby/Sessions/"+accounts.viewer.id+"/Command/VolumeUp", nil, headers, accounts.cookie)
		expectStatus(t, response, http.StatusUnauthorized)
	}
	for _, test := range []struct{ name, statement, id string }{
		{"disabled-target", "UPDATE users SET is_disabled = true WHERE id = $1", accounts.other.userID},
		{"revoked-target", "UPDATE sessions SET revoked_at = now() WHERE id = $1", accounts.other.id},
		{"expired-target", "UPDATE sessions SET created_at = now() - interval '31 days', expires_at = now() - interval '1 second' WHERE id = $1", accounts.other.id},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := f.pool.Exec(f.ctx, test.statement, test.id); err != nil {
				t.Fatal(err)
			}
			response := f.request(t, http.MethodPost, "/emby/Sessions/"+accounts.other.id+"/Command/VolumeUp", nil, accounts.admin.headers)
			expectStatus(t, response, http.StatusNotFound)
			if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = false WHERE id = $1", accounts.other.userID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at = NULL, expires_at = now() + interval '30 days' WHERE id = $1", accounts.other.id); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("administrator-cookie-target", func(t *testing.T) {
		// Use the real native login, whose credential has no ordinary-device
		// registry binding. Changing an Emby row's kind fabricates an invalid
		// identity under the device registry's credential-scope constraint.
		native, err := f.users.Resolve(f.ctx, accounts.cookie.Value, "admin")
		if err != nil {
			t.Fatal(err)
		}
		response := f.request(t, http.MethodPost, "/emby/Sessions/"+native.SessionID+"/Command/VolumeUp", nil, accounts.admin.headers)
		expectStatus(t, response, http.StatusNotFound)
		expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/session", nil, nil, accounts.cookie), http.StatusOK)
	})
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator = false WHERE id = $1", accounts.admin.userID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/"+accounts.other.id+"/Command/VolumeUp", nil, accounts.admin.headers), http.StatusNotFound)
	expectRemoteCommandAccepted(t, f, "/emby/Sessions/"+accounts.admin.id+"/Command/VolumeUp", nil, accounts.admin.headers)
	var plays, dataRows int
	if err := f.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM play_sessions), (SELECT count(*) FROM user_item_data)").Scan(&plays, &dataRows); err != nil {
		t.Fatal(err)
	}
	if plays != 0 || dataRows != 0 {
		t.Error("remote command acceptance fabricated playback sessions or user state")
	}
}

func TestHTTPRemotePlayChecksTargetMediaAccessAndNeverReportsPlayback(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	f := p.s.f
	admin := loginClientSessionHTTP(t, f, "Administrator", "administrator-password", "remote-controller")
	prepared, _ := p.prepare(t, matchingPlaybackHTTPBody())
	playID := stringValue(t, prepared, "PlaySessionId")
	p.report(t, "Started", playID, 0)
	p.report(t, "Progress", playID, 120*media.TicksPerSecond)
	before := readPlaybackHTTPState(t, p, playID)
	base := "/emby/Sessions/" + p.authSessionID
	body := map[string]any{
		"ItemIds": []string{p.s.video.id, p.secondItemID}, "PlayCommand": "PlayNow",
		"StartPositionTicks": 0, "AudioStreamIndex": 5, "SubtitleStreamIndex": -1,
		"ControllingUserId": "untrusted-controller",
	}
	expectRemoteCommandAccepted(t, f, base+"/Playing", body, admin.headers)
	expectRemoteCommandAccepted(t, f, base+"/Playing?ItemIds="+p.s.video.id+"&PlayCommand=PlayNext",
		map[string]any{"MediaSourceId": media.SourceID(p.s.video.id)}, p.headers)
	expectRemoteCommandAccepted(t, f, base+"/Playing/Seek?SeekPositionTicks=3000000000", nil, admin.headers)
	expectRemoteCommandAccepted(t, f, base+"/Playing/Stop", map[string]any{"Command": "Stop"}, p.headers)
	for _, test := range []struct {
		path string
		body any
	}{
		{base + "/Playing/Pause?Command=Stop", nil},
		{base + "/Playing/Seek?SeekPositionTicks=1&seekpositionticks=2", nil},
		{base + "/Playing/Seek?SeekPositionTicks=9223372036854775808", nil},
		{base + "/Playing/Pause", map[string]any{"Command": "Unpause"}},
		{base + "/Command", map[string]any{"Name": "SetVolume", "Arguments": map[string]any{"Volume": 37}}},
	} {
		expectStatus(t, f.request(t, http.MethodPost, test.path, test.body, p.headers), http.StatusBadRequest)
	}
	p.s.setPolicy(t, p.s.viewerID, false, []string{p.s.video.libraryID})
	expectStatus(t, f.request(t, http.MethodPost, base+"/Playing", body, admin.headers), http.StatusForbidden)
	p.s.setPolicy(t, p.s.viewerID, true, []string{})
	expectStatus(t, f.request(t, http.MethodPost, base+"/Playing", body, admin.headers), http.StatusNotFound)
	p.s.setPolicy(t, p.s.viewerID, true, []string{p.s.video.libraryID})
	missing := map[string]any{"ItemIds": []string{p.s.video.id, "missing-second-item"}, "PlayCommand": "PlayNow"}
	expectStatus(t, f.request(t, http.MethodPost, base+"/Playing", missing, admin.headers), http.StatusNotFound)
	if after := readPlaybackHTTPState(t, p, playID); after != before {
		t.Error("accepted or rejected remote commands modified authoritative playback or user progress")
	}
	session := clientSessionHTTPOne(t, f, clientSessionHTTPLogin{id: p.authSessionID, headers: p.headers}, p.authSessionID)
	state := objectValue(t, session, "PlayState")
	if state["PositionTicks"] != float64(120*media.TicksPerSecond) || state["IsPaused"] != false {
		t.Error("remote control acceptance was presented as a client playback acknowledgement")
	}
}

func remoteCommandQueuedEvent(t *testing.T, envelope events.Envelope, receiver identity.Principal) events.Event {
	t.Helper()
	hub, err := events.New(events.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	subscription, err := hub.Subscribe(events.Scope{UserID: receiver.User.ID, SessionID: receiver.SessionID, DeviceID: receiver.Client.DeviceID})
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	if delivered, err := hub.PublishSession(receiver.User.ID, receiver.SessionID, envelope); err != nil || delivered != 1 {
		t.Fatal("queue remote command test event")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := subscription.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestRemoteSocketCommandsRecheckCapabilitiesControllerAndItemPolicies(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	f := p.s.f
	admin := loginClientSessionHTTP(t, f, "Administrator", "administrator-password", "queued-controller")
	actor, err := f.users.Resolve(f.ctx, admin.headers.Get("X-Emby-Token"), "emby")
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := f.users.Resolve(f.ctx, p.s.token, "emby")
	if err != nil {
		t.Fatal(err)
	}
	target := identity.ClientSession{SessionID: receiver.SessionID, UserID: receiver.User.ID}
	setControl := func(enabled bool) {
		t.Helper()
		response := f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full", map[string]any{"SupportsMediaControl": enabled}, p.headers)
		expectStatus(t, response, http.StatusNoContent)
	}
	setControl(true)
	data := map[string]any{"ItemIds": []string{p.s.video.id, p.secondItemID}, "PlayCommand": "PlayNow"}
	envelope, err := remoteCommandEnvelope("Play", data, actor, target, false)
	if err != nil {
		t.Fatal(err)
	}
	queued := remoteCommandQueuedEvent(t, envelope, receiver)
	if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, queued); err != nil || !allowed {
		t.Fatalf("authorized queued Play was rejected: %v", err)
	}
	for _, test := range []struct {
		name   string
		change func()
		reset  func()
	}{
		{"target-control-disabled", func() { setControl(false) }, func() { setControl(true) }},
		{"target-playback-disabled", func() { p.s.setPolicy(t, p.s.viewerID, false, []string{p.s.video.libraryID}) }, func() { p.s.setPolicy(t, p.s.viewerID, true, []string{p.s.video.libraryID}) }},
		{"target-library-revoked", func() { p.s.setPolicy(t, p.s.viewerID, true, []string{}) }, func() { p.s.setPolicy(t, p.s.viewerID, true, []string{p.s.video.libraryID}) }},
		{"controller-demoted", func() {
			if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator = false WHERE id = $1", admin.userID); err != nil {
				t.Fatal(err)
			}
		}, func() {
			if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator = true WHERE id = $1", admin.userID); err != nil {
				t.Fatal(err)
			}
		}},
		{"controller-disabled", func() {
			if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = true WHERE id = $1", admin.userID); err != nil {
				t.Fatal(err)
			}
		}, func() {
			if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = false WHERE id = $1", admin.userID); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.change()
			t.Cleanup(test.reset)
			if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, queued); err != nil || allowed {
				t.Errorf("queued command survived current authorization revocation: allowed=%t error=%v", allowed, err)
			}
		})
	}
	for _, messageType := range []string{"Playstate", "GeneralCommand"} {
		data := map[string]any{"Command": "Pause", "Name": "SetVolume", "Arguments": map[string]string{}}
		envelope, err := remoteCommandEnvelope(messageType, data, receiver, target, true)
		if err != nil {
			t.Fatal(err)
		}
		event := remoteCommandQueuedEvent(t, envelope, receiver)
		if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, event); err != nil || !allowed {
			t.Error("same-user queued command was not authorized")
		}
		data["Id"] = "foreign-session"
		envelope.Data, _ = json.Marshal(data)
		event = remoteCommandQueuedEvent(t, envelope, receiver)
		if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, event); err != nil || allowed {
			t.Error("queued command with a foreign Data.Id was authorized")
		}
	}
	unrelated := remoteCommandQueuedEvent(t, events.Envelope{MessageType: "UserDataChanged", Data: json.RawMessage("{}")}, receiver)
	if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, unrelated); err != nil || !allowed {
		t.Error("remote-command authorization intercepted an unrelated user event")
	}
	before := clientSessionHTTPStoredCapabilities(t, f, p.authSessionID)
	if !reflect.DeepEqual(before, map[string]any{"SupportsMediaControl": true}) {
		t.Error("command output checks changed the receiver's declared capabilities")
	}
}

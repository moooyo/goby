//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
)

func TestQueuedRemoteCommandsRevalidateOrdinarySenderSessionAndPolicy(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	receiver, err := f.users.ResolveWithPeer(f.ctx, accounts.viewer.headers.Get("X-Emby-Token"), "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full",
		map[string]any{"SupportsMediaControl": true}, accounts.viewer.headers), http.StatusNoContent)
	target := identity.ClientSession{Kind: receiver.Kind, SessionID: receiver.SessionID, UserID: receiver.User.ID}
	// Two days ahead excludes both today's day and a midnight boundary crossed
	// during this test while retaining a valid, nonempty access schedule.
	schedule, err := json.Marshal(map[string]any{"AccessSchedules": []identity.AccessSchedule{
		{DayOfWeek: time.Now().Add(48 * time.Hour).Weekday().String(), StartHour: 0, EndHour: 24},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		revoke     bool
		sessionSQL string
		policy     string
	}{
		{name: "revoked", revoke: true},
		{name: "expired", sessionSQL: `UPDATE sessions SET created_at=clock_timestamp()-interval '31 days',
			expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`},
		{name: "device-denied", policy: `{"EnableAllDevices":false,"EnabledDevices":[]}`},
		{name: "schedule-denied", policy: string(schedule)},
		{name: "remote-access-denied", policy: `{"EnableRemoteAccess":false}`},
		{name: "remote-feature-denied", policy: `{"RestrictedFeatures":["goby_remote_control"]}`},
		{name: "cross-user-control-denied", policy: `{"EnableRemoteControlOfOtherUsers":false}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			user, err := f.users.CreateUser(f.ctx, "Queued Controller "+test.name, "controller-password", false)
			if err != nil {
				t.Fatal(err)
			}
			setHTTPUserPolicy(t, f, user.ID, `{"EnableRemoteControlOfOtherUsers":true}`)
			// These cases exercise queued-command authorization, not HTTP login
			// throttling. Issue real credentials through the identity store so
			// the shared fixture never spends the production login rate budget.
			credentials, err := f.users.AuthenticateWithPeer(f.ctx, user.Name, "controller-password", identity.Client{
				Name: "Queued Command Integration", DeviceID: "queued-" + test.name,
				Device: "Queued Command Fixture", Version: "1.0",
			}, "emby", "203.0.113.17")
			if err != nil {
				t.Fatal(err)
			}
			token := credentials.Token
			actor, err := f.users.ResolveWithPeer(f.ctx, token, "emby", "203.0.113.17")
			if err != nil {
				t.Fatal(err)
			}
			var queued []events.Event
			for _, command := range []struct {
				messageType string
				data        map[string]any
			}{
				{"Playstate", map[string]any{"Command": "Pause"}},
				{"GeneralCommand", map[string]any{"Name": "SetVolume", "Arguments": map[string]string{"Volume": "37"}}},
			} {
				envelope, err := remoteCommandEnvelope(command.messageType, command.data, actor, target, true)
				if err != nil {
					t.Fatal(err)
				}
				want := events.Authority{Kind: actor.Kind, UserID: actor.User.ID, SessionID: actor.SessionID, PeerIP: actor.PeerIP}
				if envelope.Authority != want {
					t.Fatal("ordinary command lost its original authenticated sender context")
				}
				event := remoteCommandQueuedEvent(t, envelope, receiver)
				if payload, err := f.app.socketEventPayload(f.ctx, receiver, event); err != nil || len(payload) == 0 {
					t.Fatalf("authorized ordinary command was rejected: %v", err)
				}
				queued = append(queued, event)
			}
			switch {
			case test.revoke:
				err = f.users.Revoke(f.ctx, token)
			case test.sessionSQL != "":
				_, err = f.pool.Exec(f.ctx, test.sessionSQL, actor.SessionID)
			default:
				_, err = f.pool.Exec(f.ctx, "UPDATE users SET policy=policy || $2::jsonb WHERE id=$1", actor.User.ID, test.policy)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.users.RevalidateSession(f.ctx, receiver); err != nil {
				t.Fatalf("sender revocation changed the independent receiver: %v", err)
			}
			for _, event := range queued {
				if payload, err := f.app.socketEventPayload(f.ctx, receiver, event); err != nil || len(payload) != 0 {
					t.Errorf("queued %s survived sender authorization loss: bytes=%d error=%v", event.MessageType(), len(payload), err)
				}
			}
		})
	}
}

func TestQueuedRemoteCommandsRequireOriginalOrdinaryAuthority(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	actor, err := f.users.ResolveWithPeer(f.ctx, accounts.admin.headers.Get("X-Emby-Token"), "emby", "203.0.113.17")
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := f.users.ResolveWithPeer(f.ctx, accounts.viewer.headers.Get("X-Emby-Token"), "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full",
		map[string]any{"SupportsMediaControl": true}, accounts.viewer.headers), http.StatusNoContent)
	target := identity.ClientSession{Kind: receiver.Kind, SessionID: receiver.SessionID, UserID: receiver.User.ID}
	for _, test := range []struct {
		name   string
		change func(*events.Envelope, map[string]any)
	}{
		{"missing-authority", func(envelope *events.Envelope, _ map[string]any) { envelope.Authority = events.Authority{} }},
		{"missing-wire-controller", func(_ *events.Envelope, data map[string]any) { delete(data, "ControllingUserId") }},
		{"conflicting-wire-controller", func(_ *events.Envelope, data map[string]any) { data["ControllingUserId"] = receiver.User.ID }},
		{"mismatched-session-owner", func(envelope *events.Envelope, data map[string]any) {
			envelope.Authority.UserID = receiver.User.ID
			data["ControllingUserId"] = receiver.User.ID
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := map[string]any{"Command": "Pause"}
			envelope, err := remoteCommandEnvelope("Playstate", data, actor, target, true)
			if err != nil {
				t.Fatal(err)
			}
			test.change(&envelope, data)
			envelope.Data, err = json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			event := remoteCommandQueuedEvent(t, envelope, receiver)
			if payload, err := f.app.socketEventPayload(f.ctx, receiver, event); err != nil || len(payload) != 0 {
				t.Fatalf("untrusted ordinary command authority reached the receiver: bytes=%d error=%v", len(payload), err)
			}
		})
	}
}

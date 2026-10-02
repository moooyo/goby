//go:build linux

package server

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func dynamicHeartbeatHTTPFixture(t *testing.T) (*playbackHTTPFixture, identity.Principal, library.PlaySession, *mediaPolicyLease, *atomic.Int64) {
	return playbackHeartbeatHTTPFixture(t, true)
}

func playbackHeartbeatHTTPFixture(t *testing.T, dynamic bool) (*playbackHTTPFixture, identity.Principal, library.PlaySession, *mediaPolicyLease, *atomic.Int64) {
	t.Helper()
	fixture := newPlaybackHTTPFixture(t)
	f := fixture.s.f
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy=policy || '{"SimultaneousStreamLimit":1}'::jsonb WHERE id=$1`, fixture.s.viewerID); err != nil {
		t.Fatal(err)
	}
	principal, err := f.users.ResolveWithPeer(f.ctx, fixture.s.token, "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	var play library.PlaySession
	if dynamic {
		play, err = f.app.library.PrepareDynamicPlayback(f.ctx, playbackOwner(principal), fixture.s.video.id, media.SourceID(fixture.s.video.id), "")
	} else {
		play, err = f.app.library.PreparePlayback(f.ctx, playbackOwner(principal), fixture.s.video.id, media.SourceID(fixture.s.video.id), "")
	}
	if err != nil {
		t.Fatal(err)
	}
	gate := f.app.playbackPolicyGate()
	clock := mediaPolicyHeartbeatClock(gate)
	scope := mediaPolicyTestScope(principal, play.ID)
	scope.ItemID, scope.SourceID = play.ItemID, play.MediaSourceID
	work, release, err := f.app.acquireMediaPolicy(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	lease := mediaPolicyContextLease(work)
	f.app.touchMediaPolicy(work, principal, scope)
	release()
	return fixture, principal, play, lease, clock
}

func TestHTTPLocalPlaybackPauseHeartbeatsRetainDeliveryAndExitRetiresResources(t *testing.T) {
	for _, termination := range []string{"idle", "stopped", "logout"} {
		t.Run(termination, func(t *testing.T) {
			fixture, _, play, lease, clock := playbackHeartbeatHTTPFixture(t, false)
			f := fixture.s.f
			gate := f.app.playbackPolicyGate()
			h, jobs := hlsRuntimeTestFixture(t)
			f.app.hls = h
			session := hlsRuntimeTestSession(t, h, "local-heartbeat", false)
			delete(h.byKey, session.key)
			session.key.scope = lease.scope
			h.byKey[session.key] = session
			hlsRuntimeTestOwnProducer(t, h, session)
			body := map[string]any{"PlaySessionId": play.ID, "ItemId": play.ItemID, "MediaSourceId": play.MediaSourceID,
				"IsPaused": true, "PositionTicks": 0}
			for _, path := range []string{"/emby/Sessions/Playing", "/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Ping?PlaySessionId=" + play.ID} {
				clock.Add(int64(80 * time.Second))
				var report any = body
				if path == "/emby/Sessions/Playing/Ping?PlaySessionId="+play.ID {
					report = nil
				}
				expectStatus(t, f.request(t, http.MethodPost, path, report, fixture.headers), http.StatusNoContent)
				gate.mu.Lock()
				retained := gate.leases[lease.key] == lease && lease.delivered && lease.refs == 0
				version := lease.version
				gate.mu.Unlock()
				gate.expire(lease, version-1)
				if !retained || lease.ctx.Err() != nil || session.ctx.Err() != nil {
					t.Fatal("authenticated local pause heartbeats lost delivery quota or its HLS registration")
				}
			}
			switch termination {
			case "idle":
				gate.mu.Lock()
				version := lease.version
				gate.mu.Unlock()
				clock.Add(int64(mediaPolicyIdleTTL - time.Second))
				gate.expire(lease, version)
				if lease.ctx.Err() != nil || session.ctx.Err() != nil {
					t.Fatal("a segmented playback gap was retired before its idle deadline")
				}
				clock.Add(int64(2 * time.Second))
				gate.expire(lease, version)
			case "stopped":
				expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Stopped", body, fixture.headers), http.StatusNoContent)
			case "logout":
				expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Logout", nil, fixture.headers), http.StatusNoContent)
			}
			select {
			case <-session.ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("playback exit did not cancel its producer within the cleanup deadline")
			}
			if lease.ctx.Err() == nil || h.sessions[session.id] != nil {
				t.Fatal("playback exit retained its delivery quota or output registration")
			}
			jobs.mu.Lock()
			cancelled := len(jobs.cancels) > 0 && jobs.cancels[0].scope == lease.scope && len(jobs.playbackCancels) > 0
			jobs.mu.Unlock()
			if !cancelled {
				t.Fatal("playback exit failed to cancel both registered and detached producer scopes")
			}
			if termination != "logout" {
				expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Ping?PlaySessionId="+play.ID, nil, fixture.headers), http.StatusNoContent)
				gate.mu.Lock()
				remaining := len(gate.leases)
				gate.mu.Unlock()
				if remaining != 0 {
					t.Fatal("a late playback heartbeat revived retired media delivery")
				}
			}
		})
	}
}

func TestHTTPDynamicPlaybackReportsKeepPausedQuotaAndStoppedReportsRetireIt(t *testing.T) {
	fixture, principal, play, lease, clock := dynamicHeartbeatHTTPFixture(t)
	f := fixture.s.f
	gate := f.app.playbackPolicyGate()
	for _, step := range []struct {
		path, state string
		paused      bool
	}{
		{"/emby/Sessions/Playing", "Playing", false},
		{"/emby/Sessions/Playing/Progress", "Paused", true},
		{"/emby/Sessions/Playing/Ping?PlaySessionId=" + play.ID, "Paused", true},
		{"/emby/Sessions/Playing/Progress", "Playing", false},
	} {
		gate.mu.Lock()
		previous := lease.last
		gate.mu.Unlock()
		clock.Add(int64(80 * time.Second))
		var body any
		if step.path != "/emby/Sessions/Playing/Ping?PlaySessionId="+play.ID {
			body = map[string]any{"PlaySessionId": play.ID, "ItemId": play.ItemID, "MediaSourceId": play.MediaSourceID,
				"IsPaused": step.paused, "PositionTicks": 0}
		}
		expectStatus(t, f.request(t, http.MethodPost, step.path, body, fixture.headers), http.StatusNoContent)
		gate.mu.Lock()
		refreshed := gate.leases[lease.key] == lease && lease.last.After(previous) && lease.refs == 0 && lease.delivered
		gate.mu.Unlock()
		if !refreshed || lease.ctx.Err() != nil {
			t.Fatal("an authorized dynamic playback report failed to refresh its delivered lease")
		}
		stored, err := f.app.library.GetPlaybackSession(f.ctx, playbackOwner(principal), play.ID)
		if err != nil || stored.State != step.state {
			t.Fatalf("client report did not retain the expected dynamic playback state: %v", err)
		}
		if _, _, err := gate.acquire(context.Background(), principal, mediaPolicyTestScope(principal, "unrelated-play")); !errors.Is(err, transcode.ErrBusy) {
			t.Fatal("a paused or pinging client stopped occupying simultaneous-stream quota")
		}
	}
	stopped := map[string]any{"PlaySessionId": play.ID, "ItemId": play.ItemID, "MediaSourceId": play.MediaSourceID}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Stopped", stopped, fixture.headers), http.StatusNoContent)
	if lease.ctx.Err() == nil {
		t.Fatal("an explicit stop did not cancel the dynamic media lease")
	}
	for _, path := range []string{"/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Ping?PlaySessionId=" + play.ID} {
		var body any
		if path == "/emby/Sessions/Playing/Progress" {
			body = stopped
		}
		expectStatus(t, f.request(t, http.MethodPost, path, body, fixture.headers), http.StatusNoContent)
		gate.mu.Lock()
		remaining := len(gate.leases)
		gate.mu.Unlock()
		if remaining != 0 {
			t.Fatal("a late client report recreated a stopped quota lease")
		}
	}
}

func TestHTTPDynamicPlaybackHeartbeatRejectsForeignSourceAndRevokedPermissions(t *testing.T) {
	fixture, _, play, lease, clock := dynamicHeartbeatHTTPFixture(t)
	f := fixture.s.f
	gate := f.app.playbackPolicyGate()
	gate.mu.Lock()
	before, version := lease.last, lease.version
	gate.mu.Unlock()
	clock.Add(int64(30 * time.Second))
	admin := f.embyLogin(t, "Administrator", "administrator-password")
	foreignHeaders := http.Header{"X-Emby-Token": {stringValue(t, admin, "AccessToken")}}
	body := map[string]any{"PlaySessionId": play.ID, "ItemId": play.ItemID, "MediaSourceId": play.MediaSourceID, "IsPaused": true}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Progress", body, foreignHeaders), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Ping?PlaySessionId="+play.ID, nil, foreignHeaders), http.StatusNoContent)
	wrongSource := map[string]any{"PlaySessionId": play.ID, "ItemId": play.ItemID, "MediaSourceId": "foreign-source", "IsPaused": true}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Progress", wrongSource, fixture.headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Ping?PlaySessionId=unknown-play", nil, fixture.headers), http.StatusNoContent)
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy=policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id=$1`, fixture.s.viewerID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Ping?PlaySessionId=" + play.ID} {
		var report any
		if path == "/emby/Sessions/Playing/Progress" {
			report = body
		}
		response := f.request(t, http.MethodPost, path, report, fixture.headers)
		if response.Code != http.StatusForbidden && response.Code != http.StatusNotFound {
			t.Fatalf("revoked media permission accepted a playback heartbeat: status=%d", response.Code)
		}
	}
	gate.mu.Lock()
	unchanged := lease.last.Equal(before) && lease.version == version
	gate.mu.Unlock()
	if !unchanged {
		t.Fatal("an unauthorized or mismatched report extended the existing media lease")
	}
	clock.Add(int64(mediaPolicyIdleTTL))
	gate.expire(lease, version)
	if lease.ctx.Err() == nil {
		t.Fatal("unauthorized client traffic prevented idle quota retirement")
	}
}

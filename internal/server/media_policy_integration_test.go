//go:build linux

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestHTTPMediaPolicyCountsActualOriginalRangesAcrossMediaKinds(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	f := fixture.f
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy = jsonb_set(policy, '{SimultaneousStreamLimit}', '1'::jsonb) WHERE id = $1`, fixture.viewerID); err != nil {
		t.Fatal(err)
	}
	principal, err := f.users.ResolveWithPeer(f.ctx, fixture.token, "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := f.app.library.PreparePlayback(f.ctx, playbackOwner(principal), fixture.video.id, media.SourceID(fixture.video.id), ""); err != nil {
			t.Fatal(err)
		}
	}
	videoURL := "/emby/Videos/" + fixture.video.id + "/stream?Static=true"
	audioURL := "/emby/Audio/" + fixture.audio.id + "/stream?Static=true"
	firstRange := fixture.request(t, http.MethodGet, videoURL, fixture.token, http.Header{"Range": {"bytes=0-15"}}, nil)
	expectStreamStatus(t, firstRange, http.StatusPartialContent)
	expectStreamStatus(t, fixture.request(t, http.MethodGet, videoURL, fixture.token, http.Header{"Range": {"bytes=16-31"}}, nil), http.StatusPartialContent)
	expectStreamStatus(t, fixture.request(t, http.MethodGet, videoURL, fixture.token, http.Header{"If-None-Match": {firstRange.header.Get("ETag")}}, nil), http.StatusNotModified)
	expectStreamStatus(t, fixture.request(t, http.MethodHead, audioURL, fixture.token, nil, nil), http.StatusOK)
	expectStreamStatus(t, fixture.request(t, http.MethodGet, audioURL, fixture.token, http.Header{"Range": {"bytes=0-15"}}, nil), http.StatusTooManyRequests)
	f.app.cancelMediaPolicySource(principal, fixture.video.id, media.SourceID(fixture.video.id))
	expectStreamStatus(t, fixture.request(t, http.MethodGet, audioURL, fixture.token, http.Header{"Range": {"bytes=0-15"}}, nil), http.StatusPartialContent)
	f.app.cancelMediaPolicy(principal.SessionID, "")
	// The same gate used by HLS/progressive admission must also block original
	// delivery, even though it does not consume an original-runtime HTTP slot.
	scope := mediaPolicyTestScope(principal, "conversion-play")
	scope.ItemID, scope.SourceID = fixture.video.id, media.SourceID(fixture.video.id)
	work, release, err := f.app.acquireMediaPolicy(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	expectStreamStatus(t, fixture.request(t, http.MethodGet, audioURL, fixture.token, http.Header{"Range": {"bytes=0-15"}}, nil), http.StatusTooManyRequests)
	f.app.completeMediaPolicy(work, scope)
	release()
	expectStreamStatus(t, fixture.request(t, http.MethodGet, audioURL, fixture.token, nil, nil), http.StatusOK)
}

func TestHTTPRemoteBitratePolicyDeniesOriginalBytesWithoutTrustingForwardedClaims(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	f := fixture.f
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy = jsonb_set(policy, '{RemoteClientBitrateLimit}', '64000'::jsonb) WHERE id = $1`, fixture.viewerID); err != nil {
		t.Fatal(err)
	}
	path := "/emby/Videos/" + fixture.video.id + "/stream?Static=true"
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.RemoteAddr = "203.0.113.20:40123"
	request.Header.Set("X-Emby-Token", fixture.token)
	request.Header.Set("Range", "bytes=0-15")
	request.Header.Set("X-Forwarded-For", "127.0.0.1")
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("remote over-budget original was delivered: status=%d", response.Code)
	}
	if response.Header().Get("Content-Range") != "" {
		t.Fatal("rejected remote original exposed a successful media range")
	}
	// The trusted socket peer of this fixture is loopback. A user remote cap
	// does not silently become a local-network restriction.
	expectStreamStatus(t, fixture.request(t, http.MethodGet, path, fixture.token, nil, nil), http.StatusOK)
}

func TestHTTPOriginalTerminalCorrelationPreservesStateAndDeliveryQuota(t *testing.T) {
	fixture := newPlaybackHTTPFixture(t)
	f := fixture.s.f
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy = jsonb_set(policy, '{SimultaneousStreamLimit}', '1'::jsonb) WHERE id = $1`, fixture.s.viewerID); err != nil {
		t.Fatal(err)
	}
	object, source := fixture.prepare(t, matchingPlaybackHTTPBody())
	playID := stringValue(t, object, "PlaySessionId")
	target := stringValue(t, source, "DirectStreamUrl")
	prepared := readPlaybackHTTPState(t, fixture, playID)
	ranges := http.Header{"Range": {"bytes=0-15"}}
	expectStreamStatus(t, fixture.s.request(t, http.MethodGet, target, "", ranges, nil), http.StatusPartialContent)
	if after := readPlaybackHTTPState(t, fixture, playID); !reflect.DeepEqual(prepared, after) {
		t.Fatal("an original-file range refreshed its optional playback correlation")
	}
	gate := f.app.playbackPolicyGate()
	gate.mu.Lock()
	matchedLive := len(gate.leases) == 1
	for _, lease := range gate.leases {
		matchedLive = matchedLive && lease.scope.PlaySessionID == playID && lease.scope.ItemID == fixture.s.video.id
	}
	gate.mu.Unlock()
	if !matchedLive {
		t.Fatal("an owned live source correlation did not share its canonical delivery identity")
	}
	other := "/emby/Videos/" + fixture.secondItemID + "/stream?Static=true&PlaySessionId=" + url.QueryEscape(playID)
	expectStreamStatus(t, fixture.s.request(t, http.MethodGet, other, fixture.s.token, ranges, nil), http.StatusTooManyRequests)
	fixture.report(t, "Started", playID, 0)
	fixture.report(t, "Stopped", playID, 0)
	terminal := readPlaybackHTTPState(t, fixture, playID)
	if terminal.State != "Stopped" {
		t.Fatal("terminal correlation fixture did not stop")
	}
	var beforeCount int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM play_sessions WHERE user_id=$1", fixture.s.viewerID).Scan(&beforeCount); err != nil {
		t.Fatal(err)
	}
	expectStreamStatus(t, fixture.s.request(t, http.MethodGet, target, "", ranges, nil), http.StatusPartialContent)
	if after := readPlaybackHTTPState(t, fixture, playID); !reflect.DeepEqual(terminal, after) {
		t.Fatal("reading an original source revived or changed its terminal playback state")
	}
	gate.mu.Lock()
	implicit := len(gate.leases) == 1
	for _, lease := range gate.leases {
		implicit = implicit && lease.scope.PlaySessionID == "" && lease.scope.UserID == fixture.s.viewerID && lease.scope.ItemID == fixture.s.video.id
	}
	gate.mu.Unlock()
	if !implicit {
		t.Fatal("a terminal correlation bypassed actual original-delivery quota accounting")
	}
	expectStreamStatus(t, fixture.s.request(t, http.MethodGet, other, fixture.s.token, ranges, nil), http.StatusTooManyRequests)
	adminLogin := f.embyLogin(t, "Administrator", "administrator-password")
	admin, err := f.users.ResolveWithPeer(f.ctx, stringValue(t, adminLogin, "AccessToken"), "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := f.app.library.PreparePlayback(f.ctx, playbackOwner(admin), fixture.s.video.id, media.SourceID(fixture.s.video.id), "")
	if err != nil {
		t.Fatal(err)
	}
	var foreignBefore string
	if err := f.pool.QueryRow(f.ctx, "SELECT to_jsonb(play)::text FROM play_sessions play WHERE id=$1", foreign.ID).Scan(&foreignBefore); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal("parse original correlation fixture URL")
	}
	for _, reference := range []string{foreign.ID, "unbound-original-reference"} {
		query := parsed.Query()
		query.Set("PlaySessionId", reference)
		parsed.RawQuery = query.Encode()
		expectStreamStatus(t, fixture.s.request(t, http.MethodGet, parsed.String(), "", ranges, nil), http.StatusPartialContent)
	}
	var foreignAfter string
	var afterCount int
	if err := f.pool.QueryRow(f.ctx, "SELECT to_jsonb(play)::text FROM play_sessions play WHERE id=$1", foreign.ID).Scan(&foreignAfter); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM play_sessions WHERE user_id=$1", fixture.s.viewerID).Scan(&afterCount); err != nil {
		t.Fatal(err)
	}
	if foreignBefore != foreignAfter || beforeCount != afterCount || !reflect.DeepEqual(terminal, readPlaybackHTTPState(t, fixture, playID)) {
		t.Fatal("an optional original correlation mutated, rebound, or created playback state")
	}
	gate.mu.Lock()
	for _, lease := range gate.leases {
		if lease.scope.PlaySessionID != "" || lease.scope.UserID != fixture.s.viewerID {
			gate.mu.Unlock()
			t.Fatal("an original read associated with another owner's play")
		}
	}
	gate.mu.Unlock()
}

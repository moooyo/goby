//go:build linux

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func playbackTextJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal("encode text/plain playback fixture JSON")
	}
	return string(encoded)
}

func playbackTextRequest(t *testing.T, p *playbackHTTPFixture, method, path, body string, headers http.Header, reused *atomic.Bool) *httptest.ResponseRecorder {
	t.Helper()
	ctx := p.s.f.ctx
	if reused != nil {
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused.Store(info.Reused) }})
	}
	request, err := http.NewRequestWithContext(ctx, method, p.s.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal("construct real text/plain playback request")
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	if method == http.MethodPost && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "text/plain; charset=UTF-8")
		request.ContentLength = -1
	}
	response, err := p.s.client.Do(request)
	if err != nil {
		// Playback URLs and headers carry real fixture credentials. Do not print
		// the URL-bearing transport error or response body on a failed request.
		t.Fatalf("text/plain playback request failed: %T", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		t.Fatalf("read text/plain playback response: %T", err)
	}
	result := httptest.NewRecorder()
	for key, values := range response.Header {
		result.Header()[key] = append([]string(nil), values...)
	}
	result.WriteHeader(response.StatusCode)
	_, _ = result.Write(data)
	return result
}

func playbackTextReport(t *testing.T, p *playbackHTTPFixture, route, playID string, position int64, paused bool) *httptest.ResponseRecorder {
	t.Helper()
	return playbackTextRequest(t, p, http.MethodPost, route, playbackTextJSON(t, map[string]any{
		"PlaySessionId": playID, "ItemId": p.s.video.id, "MediaSourceId": media.SourceID(p.s.video.id),
		"SessionId": p.authSessionID, "PositionTicks": position, "IsPaused": paused, "CanSeek": true,
		"PlayMethod": "DirectStream", "EventName": "TimeUpdate",
	}), p.headers, nil)
}

func TestHTTPPlaybackTextJSONNegotiationAndDurableLifecycle(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	transport := &http.Transport{MaxConnsPerHost: 1}
	p.s.client.Transport = transport
	t.Cleanup(transport.CloseIdleConnections)
	path := "/Items/" + p.s.video.id + "/playbackinfo"
	response := playbackTextRequest(t, p, http.MethodPost, path, playbackTextJSON(t, matchingPlaybackHTTPBody()), p.headers, nil)
	object, source := playbackHTTPSource(t, response)
	playID := stringValue(t, object, "PlaySessionId")
	playbackHTTPFlags(t, source, true, true)
	if source["Id"] != media.SourceID(p.s.video.id) || source["RunTimeTicks"] != float64(600*media.TicksPerSecond) || playID == p.authSessionID {
		t.Fatal("text/plain negotiation changed indexed media or playback ownership")
	}
	var reused atomic.Bool
	response = playbackTextRequest(t, p, http.MethodGet, "/emby/Users/"+p.s.viewerID+"/Items/"+p.s.video.id, "", p.headers, &reused)
	expectStatus(t, response, http.StatusOK)
	if !reused.Load() {
		t.Fatal("completed text/plain JSON decoding prevented connection reuse")
	}
	assertPlaybackHTTPData(t, objectValue(t, jsonObject(t, response), "UserData"), 0, 0, false, false)
	expectStatus(t, playbackTextReport(t, p, "/Sessions/Playing", playID, 0, false), http.StatusNoContent)
	expectStatus(t, playbackTextReport(t, p, "/emby/sessions/playing/progress", playID, 120*media.TicksPerSecond, true), http.StatusNoContent)
	paused := readPlaybackHTTPState(t, p, playID)
	if paused.State != "Paused" || paused.Position != 120*media.TicksPerSecond || paused.Count != 1 {
		t.Fatal("text/plain progress did not persist the owned paused position and single play count")
	}
	position := int64(90 * media.TicksPerSecond)
	expectStatus(t, playbackTextReport(t, p, "/emby/Sessions/Playing/Progress", playID, position, false), http.StatusNoContent)
	expectStatus(t, playbackTextReport(t, p, "/emby/Sessions/Playing/Stopped", playID, position, false), http.StatusNoContent)
	stopped := readPlaybackHTTPState(t, p, playID)
	if stopped.State != "Stopped" || stopped.Position != position || stopped.SessionPosition != position || stopped.Count != 1 || stopped.Played {
		t.Fatal("text/plain seek and stop did not persist a resumable terminal state")
	}
	response = playbackTextRequest(t, p, http.MethodGet, "/emby/Users/"+p.s.viewerID+"/Items/Resume", "", p.headers, nil)
	items, total := responseItems(t, response)
	if total != 1 || len(items) != 1 || items[0]["Id"] != p.s.video.id {
		t.Fatal("text/plain playback did not appear in the authenticated user's resume query")
	}
	assertPlaybackHTTPData(t, objectValue(t, items[0], "UserData"), position, 1, false, false)
	for _, route := range []string{"/emby/Sessions/Playing/Stopped", "/emby/Sessions/Playing/Progress"} {
		expectStatus(t, playbackTextReport(t, p, route, playID, 590*media.TicksPerSecond, false), http.StatusNoContent)
	}
	if after := readPlaybackHTTPState(t, p, playID); !reflect.DeepEqual(after, stopped) {
		t.Fatal("late text/plain reports changed terminal playback state")
	}
	response = playbackTextRequest(t, p, http.MethodPost, path, playbackTextJSON(t, matchingPlaybackHTTPBody()), p.headers, nil)
	object, _ = playbackHTTPSource(t, response)
	nextID := stringValue(t, object, "PlaySessionId")
	prepared := readPlaybackHTTPState(t, p, nextID)
	if nextID == playID || prepared.State != "Prepared" || prepared.SessionPosition != position || prepared.Duration != 600*media.TicksPerSecond {
		t.Fatal("text/plain renegotiation revived an old session or discarded its resume position")
	}
	expectStatus(t, playbackTextReport(t, p, "/emby/Sessions/Playing", nextID, position, false), http.StatusNoContent)
	expectStatus(t, playbackTextReport(t, p, "/emby/Sessions/Playing/Stopped", nextID, 590*media.TicksPerSecond, false), http.StatusNoContent)
	completed := readPlaybackHTTPState(t, p, nextID)
	if completed.State != "Stopped" || completed.Position != 0 || completed.Count != 2 || !completed.Played {
		t.Fatal("text/plain resumed completion lost the existing watched-state semantics")
	}
}

func TestHTTPPlaybackTextJSONPreservesUserAndTokenIsolation(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	f := p.s.f
	response := playbackTextRequest(t, p, http.MethodPost, "/emby/Items/"+p.s.video.id+"/PlaybackInfo", playbackTextJSON(t, matchingPlaybackHTTPBody()), p.headers, nil)
	object, _ := playbackHTTPSource(t, response)
	playID := stringValue(t, object, "PlaySessionId")
	expectStatus(t, playbackTextReport(t, p, "/emby/Sessions/Playing", playID, 30*media.TicksPerSecond, false), http.StatusNoContent)
	before := readPlaybackHTTPState(t, p, playID)
	other, err := f.users.CreateUser(f.ctx, "Text Playback Other", "text-playback-other-password", false)
	if err != nil {
		t.Fatal("create isolated text/plain playback user")
	}
	p.s.setPolicy(t, other.ID, true, []string{p.s.video.libraryID})
	login := f.embyLogin(t, other.Name, "text-playback-other-password")
	otherHeaders := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	for _, route := range []string{"/emby/Sessions/Playing", "/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Stopped"} {
		body := playbackTextJSON(t, map[string]any{"PlaySessionId": playID, "ItemId": p.s.video.id, "PositionTicks": 590 * media.TicksPerSecond})
		response := playbackTextRequest(t, p, http.MethodPost, route, body, otherHeaders, nil)
		expectAPIError(t, response, http.StatusNotFound, "not_found", true)
		response = playbackTextRequest(t, p, http.MethodPost, route, body, http.Header{"Cookie": {p.s.cookie.String()}}, nil)
		expectEmbyTextError(t, response, http.StatusUnauthorized, embyInvalidTokenMessage)
	}
	response = playbackTextRequest(t, p, http.MethodPost, "/emby/Items/"+p.s.video.id+"/PlaybackInfo",
		playbackTextJSON(t, map[string]any{"UserId": p.s.viewerID}), otherHeaders, nil)
	expectAPIError(t, response, http.StatusForbidden, "access_denied", true)
	response = playbackTextRequest(t, p, http.MethodPost, "/emby/Items/"+p.s.video.id+"/PlaybackInfo",
		playbackTextJSON(t, map[string]any{"CurrentPlaySessionId": playID}), otherHeaders, nil)
	expectAPIError(t, response, http.StatusNotFound, "not_found", true)
	if after := readPlaybackHTTPState(t, p, playID); !reflect.DeepEqual(after, before) {
		t.Fatal("foreign or cookie-only text/plain requests changed another user's playback")
	}
	var foreignState int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM user_item_data WHERE user_id=$1 AND (play_count>0 OR playback_position_ticks>0 OR played)", other.ID).Scan(&foreignState); err != nil || foreignState != 0 {
		t.Fatal("foreign playback reports wrote the other user's personal state")
	}
}

func TestHTTPPlaybackTextJSONRejectsMalformedAndOversizedBodiesWithoutStateChanges(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	prepared, _ := p.prepare(t, matchingPlaybackHTTPBody())
	playID := stringValue(t, prepared, "PlaySessionId")
	expectStatus(t, playbackTextReport(t, p, "/emby/Sessions/Playing", playID, 30*media.TicksPerSecond, false), http.StatusNoContent)
	before := readPlaybackHTTPState(t, p, playID)
	var beforeSessions int
	if err := p.s.f.pool.QueryRow(p.s.f.ctx, "SELECT count(*) FROM play_sessions").Scan(&beforeSessions); err != nil {
		t.Fatal("read baseline playback session count")
	}
	for _, route := range []string{"/emby/Items/" + p.s.video.id + "/PlaybackInfo", "/emby/Sessions/Playing", "/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Stopped"} {
		// Each typed route recognizes at least one of these invalid integer
		// fields; the other remains an ordinary forward-compatible extra field.
		for _, body := range []string{"not-json", `{"PositionTicks":"not-an-integer","StartTimeTicks":"not-an-integer"}`,
			`{"unterminated":`, `{}` + strings.Repeat(" ", 1<<20), `{} {}`} {
			response := playbackTextRequest(t, p, http.MethodPost, route, body, p.headers, nil)
			expectAPIError(t, response, http.StatusBadRequest, "invalid_json", true)
		}
		unsupported := p.headers.Clone()
		unsupported.Set("Content-Type", "application/x-www-form-urlencoded")
		response := playbackTextRequest(t, p, http.MethodPost, route, "PlaySessionId="+playID, unsupported, nil)
		expectAPIError(t, response, http.StatusUnsupportedMediaType, "unsupported_media_type", true)
	}
	if after := readPlaybackHTTPState(t, p, playID); !reflect.DeepEqual(after, before) {
		t.Fatal("malformed text/plain requests changed persisted playback state")
	}
	var afterSessions int
	if err := p.s.f.pool.QueryRow(p.s.f.ctx, "SELECT count(*) FROM play_sessions").Scan(&afterSessions); err != nil || afterSessions != beforeSessions {
		t.Fatal("malformed text/plain requests created playback sessions")
	}
}

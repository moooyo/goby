//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

type clientSessionHTTPLogin struct {
	id, userID, deviceID string
	headers              http.Header
}

type clientSessionHTTPAccounts struct {
	admin, viewer, second, other clientSessionHTTPLogin
	cookie                       *http.Cookie
}

func loginClientSessionHTTP(t *testing.T, f *serverFixture, name, password, deviceID string) clientSessionHTTPLogin {
	t.Helper()
	response := f.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", map[string]any{
		"Username": name, "Pw": password,
	}, http.Header{
		"X-Emby-Client": {"Session Integration"}, "X-Emby-Device-Id": {deviceID},
		"X-Emby-Device-Name": {"Linux Session Fixture"}, "X-Emby-Client-Version": {"1.2.3"},
	})
	expectStatus(t, response, http.StatusOK)
	object := jsonObject(t, response)
	session := objectValue(t, object, "SessionInfo")
	return clientSessionHTTPLogin{
		id: stringValue(t, session, "Id"), userID: stringValue(t, session, "UserId"), deviceID: deviceID,
		headers: http.Header{"X-Emby-Token": {stringValue(t, object, "AccessToken")}},
	}
}

func newClientSessionHTTPAccounts(t *testing.T) (*serverFixture, clientSessionHTTPAccounts) {
	t.Helper()
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, _ := f.adminLogin(t)
	for _, name := range []string{"Session Viewer", "Session Other"} {
		if _, err := f.users.CreateUser(f.ctx, name, "session-viewer-password", false); err != nil {
			t.Fatalf("create session account: %v", err)
		}
	}
	return f, clientSessionHTTPAccounts{
		admin:  loginClientSessionHTTP(t, f, "Administrator", "administrator-password", "session-admin"),
		viewer: loginClientSessionHTTP(t, f, "Session Viewer", "session-viewer-password", "session-viewer-one"),
		second: loginClientSessionHTTP(t, f, "Session Viewer", "session-viewer-password", "session-viewer-two"),
		other:  loginClientSessionHTTP(t, f, "Session Other", "session-viewer-password", "session-other"),
		cookie: cookie,
	}
}

func clientSessionHTTPArray(t *testing.T, response *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatal("session response must use a JSON content type")
	}
	var sessions []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &sessions); err != nil || sessions == nil {
		t.Fatal("session response must be a top-level JSON array, including empty results")
	}
	return sessions
}

func assertClientSessionHTTPIDs(t *testing.T, sessions []map[string]any, want ...string) {
	t.Helper()
	ids := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		id := stringValue(t, session, "Id")
		if ids[id] {
			t.Error("session list contains a duplicate authentication session")
		}
		ids[id] = true
	}
	if len(sessions) != len(want) {
		t.Fatalf("session count = %d, want %d", len(sessions), len(want))
	}
	for _, id := range want {
		if !ids[id] {
			t.Error("session list omitted an expected authorized session")
		}
	}
}

func clientSessionHTTPGet(t *testing.T, f *serverFixture, login clientSessionHTTPLogin, query string) []map[string]any {
	t.Helper()
	return clientSessionHTTPArray(t, f.request(t, http.MethodGet, "/emby/Sessions"+query, nil, login.headers))
}

func clientSessionHTTPOne(t *testing.T, f *serverFixture, login clientSessionHTTPLogin, id string) map[string]any {
	t.Helper()
	sessions := clientSessionHTTPGet(t, f, login, "?Id="+url.QueryEscape(id))
	assertClientSessionHTTPIDs(t, sessions, id)
	return sessions[0]
}

func assertClientSessionHTTPIdle(t *testing.T, session map[string]any) {
	t.Helper()
	if _, exists := session["NowPlayingItem"]; exists {
		t.Error("idle session advertised a NowPlayingItem")
	}
	state := objectValue(t, session, "PlayState")
	for key, want := range map[string]any{"CanSeek": false, "IsPaused": false, "IsMuted": false,
		"RepeatMode": "RepeatNone", "SleepTimerMode": "None", "Shuffle": false, "PlaybackRate": float64(1)} {
		if state[key] != want {
			t.Errorf("idle player field %s = %v, want %v", key, state[key], want)
		}
	}
	for _, key := range []string{"PositionTicks", "MediaSourceId", "VolumeLevel", "AudioStreamIndex", "SubtitleStreamIndex"} {
		if _, exists := state[key]; exists {
			t.Errorf("idle player retained %s from an inactive playback", key)
		}
	}
}

func assertClientSessionHTTPPrivate(t *testing.T, value any) {
	t.Helper()
	switch object := value.(type) {
	case map[string]any:
		for key, child := range object {
			switch strings.ToLower(key) {
			case "accesstoken", "token", "tokenhash", "token_hash", "pushtoken", "pushtokentype", "iconurl", "path", "userdata", "password", "password_hash":
				t.Errorf("session projection exposed private field %s", key)
			}
			assertClientSessionHTTPPrivate(t, child)
		}
	case []any:
		for _, child := range object {
			assertClientSessionHTTPPrivate(t, child)
		}
	}
}

func clientSessionHTTPStoredCapabilities(t *testing.T, f *serverFixture, id string) map[string]any {
	t.Helper()
	var encoded []byte
	if err := f.pool.QueryRow(f.ctx, "SELECT client_capabilities FROM sessions WHERE id = $1", id).Scan(&encoded); err != nil {
		t.Fatalf("read persisted client capabilities: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil || result == nil {
		t.Fatal("stored capabilities must be a JSON object")
	}
	return result
}

func expectClientCapabilityHTTPSuccess(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	expectStatus(t, response, http.StatusNoContent)
	if response.Body.Len() != 0 {
		t.Error("successful capability update must have an empty response")
	}
}

func TestHTTPClientSessionsVisibilityFiltersAndCookieIsolation(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	assertClientSessionHTTPIDs(t, clientSessionHTTPGet(t, f, accounts.viewer, ""), accounts.viewer.id, accounts.second.id)
	assertClientSessionHTTPIDs(t, clientSessionHTTPGet(t, f, accounts.other, ""), accounts.other.id)
	all := clientSessionHTTPGet(t, f, accounts.admin, "")
	assertClientSessionHTTPIDs(t, all, accounts.admin.id, accounts.viewer.id, accounts.second.id, accounts.other.id)
	for _, session := range all {
		assertClientSessionHTTPIdle(t, session)
		assertClientSessionHTTPPrivate(t, session)
		if session["SupportsRemoteControl"] != false || session["ServerId"] != f.app.serverID {
			t.Error("session projection advertised unsupported control or a foreign server")
		}
		for _, key := range []string{"PlayableMediaTypes", "SupportedCommands", "AdditionalUsers"} {
			if _, ok := session[key].([]any); !ok {
				t.Errorf("session field %s must be an array", key)
			}
		}
		if _, err := time.Parse(time.RFC3339Nano, stringValue(t, session, "LastActivityDate")); err != nil {
			t.Error("session activity date must be an RFC3339 timestamp")
		}
	}
	for _, test := range []struct {
		query string
		want  []string
	}{
		{"?Id=" + accounts.second.id, []string{accounts.second.id}},
		{"?SessionId=" + accounts.viewer.id, []string{accounts.viewer.id}},
		{"?DeviceId=" + accounts.second.deviceID, []string{accounts.second.id}},
		{"?Id=" + accounts.second.id + "&DeviceId=" + accounts.viewer.deviceID, nil},
		{"?Id=" + accounts.other.id, nil},
		{"?Id=unknown-session", nil},
		{"?DeviceId=unknown-device", nil},
		{"?ControllableByUserId=" + accounts.viewer.userID, nil},
	} {
		t.Run(test.query, func(t *testing.T) {
			assertClientSessionHTTPIDs(t, clientSessionHTTPGet(t, f, accounts.viewer, test.query), test.want...)
		})
	}
	alias := f.request(t, http.MethodGet, "/sessions?Id="+accounts.viewer.id, nil, accounts.viewer.headers)
	assertClientSessionHTTPIDs(t, clientSessionHTTPArray(t, alias), accounts.viewer.id)
	withCookie := f.request(t, http.MethodGet, "/emby/Sessions?UserId="+accounts.admin.userID, nil, accounts.viewer.headers, accounts.cookie)
	assertClientSessionHTTPIDs(t, clientSessionHTTPArray(t, withCookie), accounts.viewer.id, accounts.second.id)
	for _, headers := range []http.Header{nil, {"X-Emby-Token": {accounts.cookie.Value}}} {
		expectStatus(t, f.request(t, http.MethodGet, "/emby/Sessions", nil, headers, accounts.cookie), http.StatusUnauthorized)
	}
	for _, query := range []string{"?Id=a&SessionId=b", "?Id=a&id=b", "?ActiveWithinSeconds=-1", "?ActiveWithinSeconds=invalid", "?ActiveWithinSeconds=2147483648"} {
		expectStatus(t, f.request(t, http.MethodGet, "/emby/Sessions"+query, nil, accounts.viewer.headers), http.StatusBadRequest)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Sessions?ControllableByUserId="+accounts.other.userID, nil, accounts.viewer.headers), http.StatusForbidden)
}

func TestHTTPClientCapabilitiesPersistOnlySupportedOwnDeclarations(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	simple := "/emby/Sessions/Capabilities?Id=" + accounts.viewer.id + "&PlayableMediaTypes=Video,Audio&SupportedCommands=SetVolume,ToggleMute&SupportsMediaControl=true&SupportsSync=false"
	expectClientCapabilityHTTPSuccess(t, f.request(t, http.MethodPost, simple, nil, accounts.viewer.headers))
	stored := clientSessionHTTPStoredCapabilities(t, f, accounts.viewer.id)
	if !reflect.DeepEqual(stored["PlayableMediaTypes"], []any{"Video", "Audio"}) || stored["SupportsMediaControl"] != true {
		t.Error("simple capability query was not persisted as a typed snapshot")
	}
	const pushToken = "private-push-token-must-not-be-retained"
	const iconURL = "https://untrusted.example/session-icon.png"
	full := map[string]any{
		"PlayableMediaTypes": []string{"Audio"}, "SupportedCommands": []string{"SetVolume"},
		"SupportsMediaControl": true, "SupportsSync": true, "PushToken": pushToken,
		"PushTokenType": "Firebase", "IconUrl": iconURL, "AppId": "session-fixture",
		"UserId": accounts.other.userID, "FutureCapability": map[string]any{"Enabled": true},
		"DeviceProfile": map[string]any{
			"Name": "Persisted HTTP Profile", "MaxStreamingBitrate": 20_000_000,
			"FutureProfileField": "discard",
			"DirectPlayProfiles": []map[string]any{{"Type": "Video", "Container": "mp4", "VideoCodec": "h264", "AudioCodec": "aac", "FutureCodecField": true}},
		},
	}
	expectClientCapabilityHTTPSuccess(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full?SessionId="+accounts.viewer.id, full, accounts.viewer.headers))
	stored = clientSessionHTTPStoredCapabilities(t, f, accounts.viewer.id)
	for _, key := range []string{"PushToken", "PushTokenType", "UserId", "FutureCapability"} {
		if _, exists := stored[key]; exists {
			t.Errorf("capability storage retained unsupported or private field %s", key)
		}
	}
	profile := objectValue(t, stored, "DeviceProfile")
	if profile["Name"] != "Persisted HTTP Profile" || profile["MaxStreamingBitrate"] != float64(20_000_000) {
		t.Error("supported device profile facts did not survive JSON persistence")
	}
	if _, exists := profile["FutureProfileField"]; exists {
		t.Error("unknown nested device profile field was retained")
	}
	direct, ok := profile["DirectPlayProfiles"].([]any)
	if !ok || len(direct) != 1 {
		t.Fatal("persisted direct-play profiles lost their array structure")
	}
	if !reflect.DeepEqual(direct[0], map[string]any{"Type": "Video", "Container": "mp4", "VideoCodec": "h264", "AudioCodec": "aac"}) {
		t.Error("nested profile declarations were not sanitized correctly")
	}
	for _, login := range []clientSessionHTTPLogin{accounts.viewer, accounts.admin} {
		session := clientSessionHTTPOne(t, f, login, accounts.viewer.id)
		assertClientSessionHTTPPrivate(t, session)
		if !reflect.DeepEqual(session["PlayableMediaTypes"], []any{"Audio"}) || !reflect.DeepEqual(session["SupportedCommands"], []any{"SetVolume"}) || session["SupportsRemoteControl"] != false {
			t.Error("session projection did not reflect declared capabilities without enabling remote control")
		}
		encoded, err := json.Marshal(session)
		if err != nil || strings.Contains(string(encoded), pushToken) || strings.Contains(string(encoded), iconURL) {
			t.Error("session projection exposed a push token or client-provided icon URL")
		}
	}
	if other := clientSessionHTTPStoredCapabilities(t, f, accounts.other.id); len(other) != 0 {
		t.Error("capability body identity changed another user's stored declarations")
	}
	expectClientCapabilityHTTPSuccess(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full", map[string]any{}, accounts.viewer.headers))
	if len(clientSessionHTTPStoredCapabilities(t, f, accounts.viewer.id)) != 0 {
		t.Error("empty capability replacement retained prior declarations")
	}
}

func TestHTTPClientCapabilitiesIgnoreTargetHintsAndRejectMalformedDeclarations(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	seed := map[string]any{"PlayableMediaTypes": []string{"Video"}, "SupportedCommands": []string{"SetVolume"}}
	expectClientCapabilityHTTPSuccess(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full", seed, accounts.viewer.headers))
	for _, test := range []struct {
		name, path string
		login      clientSessionHTTPLogin
		hintedID   string
	}{
		{"same-user-other-device", "/emby/Sessions/Capabilities?Id=" + accounts.second.id, accounts.viewer, accounts.second.id},
		{"different-user", "/emby/Sessions/Capabilities/Full?Id=" + accounts.other.id, accounts.viewer, accounts.other.id},
		{"admin-other-session", "/emby/Sessions/Capabilities/Full?Id=" + accounts.viewer.id, accounts.admin, accounts.viewer.id},
		{"unknown-target", "/emby/Sessions/Capabilities/Full?Id=unknown", accounts.viewer, ""},
		{"different-hints", "/emby/Sessions/Capabilities?Id=" + accounts.viewer.id + "&SessionId=" + accounts.second.id, accounts.viewer, accounts.second.id},
	} {
		t.Run(test.name, func(t *testing.T) {
			var hintedBefore map[string]any
			if test.hintedID != "" {
				hintedBefore = clientSessionHTTPStoredCapabilities(t, f, test.hintedID)
			}
			body := map[string]any{"PlayableMediaTypes": []string{"Audio"}, "SupportedCommands": []string{"ToggleMute"}}
			path := test.path + "&PlayableMediaTypes=Audio&SupportedCommands=ToggleMute"
			expectClientCapabilityHTTPSuccess(t, f.request(t, http.MethodPost, path, body, test.login.headers))
			own := clientSessionHTTPStoredCapabilities(t, f, test.login.id)
			if !reflect.DeepEqual(own["PlayableMediaTypes"], []any{"Audio"}) || !reflect.DeepEqual(own["SupportedCommands"], []any{"ToggleMute"}) {
				t.Error("capability hint prevented the current session from receiving its declaration")
			}
			if test.hintedID != "" && !reflect.DeepEqual(hintedBefore, clientSessionHTTPStoredCapabilities(t, f, test.hintedID)) {
				t.Error("capability hint overwrote a different authentication session")
			}
		})
	}
	before := clientSessionHTTPStoredCapabilities(t, f, accounts.viewer.id)
	for _, path := range []string{
		"/emby/Sessions/Capabilities?Id=a&id=b",
		"/emby/Sessions/Capabilities?SupportsMediaControl=perhaps",
	} {
		expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{}, accounts.viewer.headers), http.StatusBadRequest)
	}
	for _, test := range []struct{ name, body string }{
		{"wrong-array-type", "{\"PlayableMediaTypes\":\"Video\"}"},
		{"wrong-entry-type", "{\"SupportedCommands\":[42]}"},
		{"wrong-boolean-type", "{\"SupportsMediaControl\":\"true\"}"},
		{"wrong-profile-type", "{\"DeviceProfile\":[]}"},
		{"fractional-profile-bitrate", "{\"DeviceProfile\":{\"MaxStreamingBitrate\":1.5}}"},
		{"wrong-profile-enum", "{\"DeviceProfile\":{\"DirectPlayProfiles\":[{\"Type\":\"Unknown\"}]}}"},
		{"duplicate-key", "{\"SupportsSync\":true,\"SupportsSync\":false}"},
		{"nested-duplicate-key", "{\"DeviceProfile\":{\"Name\":\"one\",\"Name\":\"two\"}}"},
		{"non-object", "[]"},
		{"null-object", "null"},
		{"oversized-body", "{\"FutureField\":\"" + strings.Repeat("x", 64*1024) + "\"}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full", json.RawMessage(test.body), accounts.viewer.headers)
			expectStatus(t, response, http.StatusBadRequest)
		})
	}
	for _, path := range []string{"/emby/Sessions/Capabilities", "/emby/Sessions/Capabilities/Full"} {
		expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{}, nil, accounts.cookie), http.StatusUnauthorized)
		expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{}, http.Header{"X-Emby-Token": {accounts.cookie.Value}}, accounts.cookie), http.StatusUnauthorized)
	}
	for _, contentType := range []string{"", "text/plain"} {
		request := httptest.NewRequest(http.MethodPost, "/emby/Sessions/Capabilities/Full", strings.NewReader("{}")).WithContext(f.ctx)
		request.Header.Set("X-Emby-Token", accounts.viewer.headers.Get("X-Emby-Token"))
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, request)
		expectStatus(t, response, http.StatusUnsupportedMediaType)
	}
	if !reflect.DeepEqual(before, clientSessionHTTPStoredCapabilities(t, f, accounts.viewer.id)) {
		t.Error("rejected capability request mutated the previously accepted snapshot")
	}
	for _, id := range []string{accounts.second.id, accounts.other.id} {
		if len(clientSessionHTTPStoredCapabilities(t, f, id)) != 0 {
			t.Error("rejected capability request mutated a different session")
		}
	}
}

func clientSessionHTTPTimes(t *testing.T, f *serverFixture, id string) (time.Time, time.Time) {
	t.Helper()
	var seen, expires time.Time
	if err := f.pool.QueryRow(f.ctx, "SELECT last_seen_at, expires_at FROM sessions WHERE id = $1", id).Scan(&seen, &expires); err != nil {
		t.Fatalf("read client session activity times: %v", err)
	}
	return seen, expires
}

func TestHTTPClientSessionPresenceTouchAndLoginLifetimeAreIndependent(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET last_seen_at = now() - interval '6 minutes' WHERE id = $1", accounts.second.id); err != nil {
		t.Fatal(err)
	}
	oldSeen, expires := clientSessionHTTPTimes(t, f, accounts.second.id)
	assertClientSessionHTTPIDs(t, clientSessionHTTPGet(t, f, accounts.viewer, ""), accounts.viewer.id)
	for _, seconds := range []string{"0", "600"} {
		assertClientSessionHTTPIDs(t, clientSessionHTTPGet(t, f, accounts.viewer, "?ActiveWithinSeconds="+seconds), accounts.viewer.id, accounts.second.id)
	}
	// An ordinary authenticated endpoint reactivates presence without relogin.
	expectStatus(t, f.request(t, http.MethodGet, "/emby/System/Info", nil, accounts.second.headers), http.StatusOK)
	touched, afterExpiry := clientSessionHTTPTimes(t, f, accounts.second.id)
	if !touched.After(oldSeen) || !afterExpiry.Equal(expires) {
		t.Error("authenticated activity failed to refresh presence without extending login")
	}
	assertClientSessionHTTPIDs(t, clientSessionHTTPGet(t, f, accounts.viewer, ""), accounts.viewer.id, accounts.second.id)
	for _, test := range []struct {
		seconds int64
		changed bool
	}{{10, false}, {16, true}} {
		if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET last_seen_at = now() - ($2::bigint * interval '1 second') WHERE id = $1", accounts.second.id, test.seconds); err != nil {
			t.Fatal(err)
		}
		before, _ := clientSessionHTTPTimes(t, f, accounts.second.id)
		expectStatus(t, f.request(t, http.MethodGet, "/emby/System/Info", nil, accounts.second.headers), http.StatusOK)
		after, currentExpiry := clientSessionHTTPTimes(t, f, accounts.second.id)
		if after.After(before) != test.changed || !currentExpiry.Equal(expires) {
			t.Errorf("activity age %d seconds did not honor the 15-second write interval and fixed login lifetime", test.seconds)
		}
	}
}

func TestHTTPClientSessionsRecheckRevocationDisableExpiryAndAdministratorRole(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	for _, test := range []struct{ name, statement, id string }{
		{"disabled", "UPDATE users SET is_disabled = true WHERE id = $1", accounts.viewer.userID},
		{"revoked", "UPDATE sessions SET revoked_at = now() WHERE id = $1", accounts.viewer.id},
		{"expired", "UPDATE sessions SET created_at = now() - interval '31 days', expires_at = now() - interval '1 second' WHERE id = $1", accounts.viewer.id},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := f.pool.Exec(f.ctx, test.statement, test.id); err != nil {
				t.Fatal(err)
			}
			assertClientSessionHTTPIDs(t, clientSessionHTTPGet(t, f, accounts.admin, "?Id="+accounts.viewer.id+"&ActiveWithinSeconds=0"))
			expectStatus(t, f.request(t, http.MethodGet, "/emby/Sessions", nil, accounts.viewer.headers), http.StatusUnauthorized)
			expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full", map[string]any{}, accounts.viewer.headers), http.StatusUnauthorized)
			if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = false WHERE id = $1", accounts.viewer.userID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at = NULL, expires_at = now() + interval '30 days' WHERE id = $1", accounts.viewer.id); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator = false WHERE id = $1", accounts.admin.userID); err != nil {
		t.Fatal(err)
	}
	assertClientSessionHTTPIDs(t, clientSessionHTTPGet(t, f, accounts.admin, ""), accounts.admin.id)
	assertClientSessionHTTPIDs(t, clientSessionHTTPGet(t, f, accounts.admin, "?Id="+accounts.viewer.id))
	before := clientSessionHTTPStoredCapabilities(t, f, accounts.viewer.id)
	expectClientCapabilityHTTPSuccess(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full?Id="+accounts.viewer.id,
		map[string]any{"PlayableMediaTypes": []string{"Audio"}}, accounts.admin.headers))
	if !reflect.DeepEqual(before, clientSessionHTTPStoredCapabilities(t, f, accounts.viewer.id)) {
		t.Error("demoted administrator's capability hint changed another user's session")
	}
}

func TestHTTPClientSessionsProjectPlaybackHintsAndReturnToIdle(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	f := p.s.f
	viewer := clientSessionHTTPLogin{id: p.authSessionID, userID: p.s.viewerID, headers: p.headers}
	admin := loginClientSessionHTTP(t, f, "Administrator", "administrator-password", "playback-session-observer")
	// Stored capabilities describe presence; negotiation consumes its own request profile.
	storedProfile := map[string]any{"DeviceProfile": map[string]any{
		"MaxStreamingBitrate": 1,
		"DirectPlayProfiles":  []map[string]any{{"Type": "Video", "Container": "avi", "VideoCodec": "mpeg4", "AudioCodec": "mp3"}},
	}}
	expectClientCapabilityHTTPSuccess(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full", storedProfile, p.headers))
	_, genericSource := p.prepare(t, map[string]any{})
	playbackHTTPFlags(t, genericSource, true, true)
	if _, exists := genericSource["DirectStreamUrl"]; exists {
		t.Error("minimal PlaybackInfo unexpectedly applied a stored device profile")
	}
	prepared, _ := p.prepare(t, matchingPlaybackHTTPBody())
	playID := stringValue(t, prepared, "PlaySessionId")
	assertClientSessionHTTPIdle(t, clientSessionHTTPOne(t, f, viewer, viewer.id))
	started := map[string]any{
		"PlaySessionId": playID, "ItemId": p.s.video.id, "MediaSourceId": media.SourceID(p.s.video.id),
		"PositionTicks": 0, "IsPaused": false, "CanSeek": true, "IsMuted": true,
		"VolumeLevel": 0, "AudioStreamIndex": 5, "SubtitleStreamIndex": -1,
		"PlayMethod": "DirectStream", "PlaybackRate": 1.5, "RunTimeTicks": 1, "Shuffle": false,
	}
	expectClientCapabilityHTTPSuccess(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing", started, p.headers))
	for _, login := range []clientSessionHTTPLogin{viewer, admin} {
		session := clientSessionHTTPOne(t, f, login, viewer.id)
		assertClientSessionHTTPPrivate(t, session)
		item := objectValue(t, session, "NowPlayingItem")
		if item["Id"] != p.s.video.id || item["RunTimeTicks"] != float64(600*media.TicksPerSecond) {
			t.Error("NowPlayingItem must use the authorized indexed item and duration")
		}
		state := objectValue(t, session, "PlayState")
		for key, want := range map[string]any{"PositionTicks": float64(0), "IsPaused": false, "CanSeek": true,
			"IsMuted": true, "VolumeLevel": float64(0), "AudioStreamIndex": float64(5),
			"SubtitleStreamIndex": float64(-1), "PlayMethod": "DirectStream", "PlaybackRate": 1.5,
			"MediaSourceId": media.SourceID(p.s.video.id), "Shuffle": false} {
			if state[key] != want {
				t.Errorf("started player field %s = %v, want %v", key, state[key], want)
			}
		}
	}
	for _, test := range []struct {
		seconds int64
		paused  bool
	}{{120, true}, {600, false}} {
		// Later reports omit display hints, retaining explicit zero and false values.
		progress := map[string]any{"PlaySessionId": playID, "PositionTicks": test.seconds * media.TicksPerSecond, "IsPaused": test.paused}
		expectClientCapabilityHTTPSuccess(t, f.request(t, http.MethodPost, "/emby/Sessions/Playing/Progress", progress, p.headers))
		session := clientSessionHTTPOne(t, f, viewer, viewer.id)
		state := objectValue(t, session, "PlayState")
		if state["PositionTicks"] != float64(test.seconds*media.TicksPerSecond) || state["IsPaused"] != test.paused {
			t.Error("session player state did not follow the authoritative progress report")
		}
		for key, want := range map[string]any{"CanSeek": true, "IsMuted": true, "VolumeLevel": float64(0),
			"AudioStreamIndex": float64(5), "SubtitleStreamIndex": float64(-1), "PlaybackRate": 1.5, "Shuffle": false} {
			if state[key] != want {
				t.Errorf("omitted progress hint %s failed to retain its accepted value", key)
			}
		}
		assertClientSessionHTTPPrivate(t, session)
	}
	p.report(t, "Stopped", playID, 600*media.TicksPerSecond)
	for _, login := range []clientSessionHTTPLogin{viewer, admin} {
		assertClientSessionHTTPIdle(t, clientSessionHTTPOne(t, f, login, viewer.id))
	}
}

func TestHTTPClientSessionsHidePlaybackWhenCurrentPolicyRevokesIt(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	f := p.s.f
	viewer := clientSessionHTTPLogin{id: p.authSessionID, userID: p.s.viewerID, headers: p.headers}
	admin := loginClientSessionHTTP(t, f, "Administrator", "administrator-password", "policy-session-observer")
	prepared, _ := p.prepare(t, matchingPlaybackHTTPBody())
	p.report(t, "Started", stringValue(t, prepared, "PlaySessionId"), 0)
	if _, exists := clientSessionHTTPOne(t, f, admin, viewer.id)["NowPlayingItem"]; !exists {
		t.Fatal("authorized active playback was not projected before policy change")
	}
	for _, test := range []struct {
		name     string
		playback bool
		folders  []string
	}{{"playback-disabled", false, []string{p.s.video.libraryID}}, {"library-revoked", true, []string{}}} {
		t.Run(test.name, func(t *testing.T) {
			p.s.setPolicy(t, viewer.userID, test.playback, test.folders)
			for _, login := range []clientSessionHTTPLogin{viewer, admin} {
				session := clientSessionHTTPOne(t, f, login, viewer.id)
				assertClientSessionHTTPIdle(t, session)
				assertClientSessionHTTPPrivate(t, session)
			}
			p.s.setPolicy(t, viewer.userID, true, []string{p.s.video.libraryID})
		})
	}
}

func TestHTTPPlaybackPingUsesReferenceResponsesWithoutCrossSessionWrites(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	f := p.s.f
	admin := loginClientSessionHTTP(t, f, "Administrator", "administrator-password", "ping-session-observer")
	prepared, _ := p.prepare(t, matchingPlaybackHTTPBody())
	playID := stringValue(t, prepared, "PlaySessionId")
	p.report(t, "Started", playID, 0)
	p.report(t, "Progress", playID, 120*media.TicksPerSecond)
	if _, err := f.pool.Exec(f.ctx, "UPDATE play_sessions SET expires_at = now() + interval '2 minutes' WHERE id = $1", playID); err != nil {
		t.Fatal(err)
	}
	type pingSnapshot struct {
		state                   playbackHTTPPersistedState
		playerState             string
		expires                 time.Time
		playCount, dataRowCount int
	}
	snapshot := func() pingSnapshot {
		t.Helper()
		value := pingSnapshot{state: readPlaybackHTTPState(t, p, playID)}
		if err := f.pool.QueryRow(f.ctx, "SELECT expires_at, player_state::text FROM play_sessions WHERE id = $1", playID).
			Scan(&value.expires, &value.playerState); err != nil {
			t.Fatal(err)
		}
		if err := f.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM play_sessions), (SELECT count(*) FROM user_item_data)").
			Scan(&value.playCount, &value.dataRowCount); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	for _, query := range []string{"", "?PlaySessionId="} {
		response := f.request(t, http.MethodPost, "/emby/Sessions/Playing/Ping"+query, nil, p.headers)
		expectEmbyTextError(t, response, http.StatusBadRequest, "Value cannot be null. (Parameter 'key')")
	}
	for _, test := range []struct {
		name, id string
		headers  http.Header
	}{
		{"unknown", "unknown-play-session", p.headers},
		{"foreign", playID, admin.headers},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := f.request(t, http.MethodPost, "/emby/Sessions/Playing/Ping?PlaySessionId="+url.QueryEscape(test.id), nil, test.headers)
			expectClientCapabilityHTTPSuccess(t, response)
			if after := snapshot(); after != before {
				t.Error("unknown or foreign ping changed playback state, data, or row counts")
			}
		})
	}
	response := f.request(t, http.MethodPost, "/emby/Sessions/Playing/Ping?PlaySessionId="+url.QueryEscape(playID), nil, p.headers)
	expectClientCapabilityHTTPSuccess(t, response)
	after := snapshot()
	if !after.expires.After(before.expires) || after.state.State != "Playing" {
		t.Error("owned ping failed to extend the live playback lease")
	}
	// A liveness update may change playback timestamps, but never user progress.
	before.state.Updated = after.state.Updated
	before.expires = after.expires
	if after != before {
		t.Error("owned ping changed playback position, hints, user data, or row counts")
	}
}

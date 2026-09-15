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
)

const liveTVProgramsPath = "/emby/LiveTv/Programs"

func assertEmptyLiveTVPrograms(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatal("Programs did not use the observed JSON content type")
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil || len(value) != 2 ||
		string(value["Items"]) != "[]" || string(value["TotalRecordCount"]) != "0" {
		t.Fatal("Programs must expose only the observed empty Items array and zero TotalRecordCount")
	}
}

func liveTVProgramBusinessState(t *testing.T, f *serverFixture) map[string]string {
	t.Helper()
	state := make(map[string]string)
	for _, table := range []string{"items", "catalog_entities", "libraries", "library_roots", "user_item_data", "play_sessions", "client_playback_references", "encoding_jobs"} {
		// The relation list is fixed. Include xmin to detect same-value rewrites.
		statement := "SELECT COALESCE(jsonb_agg(value ORDER BY value::text), '[]'::jsonb)::text FROM " +
			"(SELECT to_jsonb(t) || jsonb_build_object('_xmin', t.xmin::text) AS value FROM " + table + " t) rows"
		var value string
		if err := f.pool.QueryRow(f.ctx, statement).Scan(&value); err != nil {
			t.Fatalf("snapshot Programs business relation %s: %v", table, err)
		}
		state[table] = value
	}
	return state
}

func seedLiveTVProgramHistory(t *testing.T, a *applicationKeyCatalogFixture) {
	t.Helper()
	f := a.f
	var sessionID, deviceID string
	if err := f.pool.QueryRow(f.ctx, "SELECT id, device_id FROM sessions WHERE user_id = $1 AND kind = 'emby' AND revoked_at IS NULL ORDER BY id LIMIT 1", a.viewerID).
		Scan(&sessionID, &deviceID); err != nil {
		t.Fatalf("read retained Programs fixture authentication: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data
		(user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_at)
		VALUES ($1, $2, 420000000, 2, true, false, clock_timestamp())`, a.viewerID, a.firstEpisodeID); err != nil {
		t.Fatalf("seed nonzero Programs user state: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `WITH instant AS (SELECT clock_timestamp() AS at)
		INSERT INTO play_sessions (id, user_id, auth_session_id, device_id, item_id, media_source_id,
		state, position_ticks, duration_ticks, counted, created_at, updated_at, started_at, stopped_at, expires_at, client_correlated)
		SELECT 'play_programs_retained', $1, $2, $3, $4, $5, 'Stopped', 420000000, 1200000000, true,
		at, at, at, at, at + interval '1 hour', true FROM instant`, a.viewerID, sessionID, deviceID, a.firstEpisodeID, "mediasource_"+a.firstEpisodeID); err != nil {
		t.Fatalf("seed retained Programs playback: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO client_playback_references
		(user_id, auth_session_id, device_id, client_nonce, play_session_id)
		VALUES ($1, $2, $3, 'programs-history', 'play_programs_retained')`, a.viewerID, sessionID, deviceID); err != nil {
		t.Fatalf("seed retained Programs reference: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `WITH instant AS (SELECT clock_timestamp() AS at)
		INSERT INTO encoding_jobs (id, user_id, auth_session_id, device_id, play_session_id, item_id,
		media_source_id, source_stamp, plan, state, output_bytes, created_at, updated_at, last_access_at)
		SELECT 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', $1, $2, $3, 'play_programs_retained', $4, $5,
		'programs-retained-source', '{"Fixture":"retained"}'::jsonb, 'completed', 128, at, at, at FROM instant`,
		a.viewerID, sessionID, deviceID, a.firstEpisodeID, "mediasource_"+a.firstEpisodeID); err != nil {
		t.Fatalf("seed retained Programs encoding history: %v", err)
	}
}

func TestHTTPLiveTVProgramsRecordedQueryAndBusinessState(t *testing.T) {
	a := newApplicationKeyCatalogFixture(t)
	f := a.f
	seedLiveTVProgramHistory(t, a)
	setHTTPUserPolicy(t, f, a.viewerID, `{"EnableAllFolders":true}`)
	before := liveTVProgramBusinessState(t, f)
	query := url.Values{
		"UserId": {a.viewerID}, "HasAired": {"false"}, "SortBy": {"StartDate"}, "ImageTypeLimit": {"1"},
		"EnableImageTypes": {"Primary,Thumb,Backdrop"}, "EnableUserData": {"false"},
		"Fields": {"PrimaryImageAspectRatio,ChannelInfo"}, "Limit": {"12"}, "LibrarySeriesId": {a.seriesID},
		"X-Emby-Client": {"Emby Web"}, "X-Emby-Client-Version": {"4.9.5.0"},
		"X-Emby-Token": {a.viewerHeaders.Get("X-Emby-Token")}, "X-Emby-Language": {"en-us"},
	}
	assertEmptyLiveTVPrograms(t, f.request(t, http.MethodGet, liveTVProgramsPath+"?"+query.Encode(), nil, nil))
	for _, suffix := range []string{"", "?UserId=", "?Limit=0&EnableUserData=false", "?HasAired=true&Limit=2147483647&ImageTypeLimit=0", "?LibrarySeriesId=" + a.seriesID} {
		assertEmptyLiveTVPrograms(t, f.request(t, http.MethodGet, liveTVProgramsPath+suffix, nil, a.viewerHeaders))
	}
	if after := liveTVProgramBusinessState(t, f); !reflect.DeepEqual(before, after) {
		t.Fatal("read-only Programs queries changed catalog, user data, playback, references, or encoding history")
	}
}

func TestHTTPLiveTVProgramsSubjectsAndLibraryVisibility(t *testing.T) {
	a := newApplicationKeyCatalogFixture(t)
	f := a.f
	login := f.embyLogin(t, "Administrator", "administrator-password")
	adminHeaders := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	for _, test := range []struct {
		name, query string
		headers     http.Header
		status      int
	}{
		{"viewer_self", "", a.viewerHeaders, 200},
		{"viewer_cross_user", "?UserId=" + a.adminID + "&Limit=0", a.viewerHeaders, 403},
		{"viewer_hidden_series", "?LibrarySeriesId=" + a.seriesID, a.viewerHeaders, 404},
		{"viewer_hidden_zero_limit", "?LibrarySeriesId=" + a.seriesID + "&Limit=0", a.viewerHeaders, 404},
		{"admin_target_hidden_series", "?UserId=" + a.viewerID + "&LibrarySeriesId=" + a.seriesID, adminHeaders, 404},
		{"admin_disabled_target", "?UserId=" + a.targetID, adminHeaders, 403},
		{"admin_unknown_target", "?UserId=missing-programs-user", adminHeaders, 403},
		{"key_userless", "", a.headers, 200},
		{"key_unknown_target", "?UserId=missing-programs-user", a.headers, 404},
		{"key_disabled_target", "?UserId=" + a.targetID + "&LibrarySeriesId=" + a.seriesID, a.headers, 200},
		{"key_target_hidden_series", "?UserId=" + a.viewerID + "&LibrarySeriesId=" + a.seriesID, a.headers, 404},
		{"key_visible_series", "?LibrarySeriesId=" + a.seriesID, a.headers, 200},
		{"key_missing_series", "?LibrarySeriesId=missing-programs-series", a.headers, 404},
		{"key_non_series", "?LibrarySeriesId=" + a.firstEpisodeID, a.headers, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := f.request(t, http.MethodGet, liveTVProgramsPath+test.query, nil, test.headers, a.adminCookie)
			if test.status == http.StatusOK {
				assertEmptyLiveTVPrograms(t, response)
			} else {
				expectStatus(t, response, test.status)
			}
		})
	}
}

func TestHTTPLiveTVProgramsCurrentCredentialsAndPolicies(t *testing.T) {
	a := newApplicationKeyCatalogFixture(t)
	f := a.f
	for _, headers := range []http.Header{nil, {"X-Emby-Token": {"invalid-token"}}, {"X-Emby-Token": {a.adminCookie.Value}}} {
		expectStatus(t, f.request(t, http.MethodGet, liveTVProgramsPath, nil, headers, a.adminCookie), http.StatusUnauthorized)
	}
	setHTTPUserPolicy(t, f, a.viewerID, `{"EnableAllFolders":true}`)
	assertEmptyLiveTVPrograms(t, f.request(t, http.MethodGet, liveTVProgramsPath+"?LibrarySeriesId="+a.seriesID, nil, a.viewerHeaders))
	setHTTPUserPolicy(t, f, a.viewerID, `{"EnableAllFolders":false,"EnabledFolders":[]}`)
	expectStatus(t, f.request(t, http.MethodGet, liveTVProgramsPath+"?LibrarySeriesId="+a.seriesID+"&Limit=0", nil, a.viewerHeaders), http.StatusNotFound)
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = true WHERE id = $1", a.viewerID); err != nil {
		t.Fatalf("disable Programs viewer: %v", err)
	}
	expectStatus(t, f.request(t, http.MethodGet, liveTVProgramsPath, nil, a.viewerHeaders), http.StatusUnauthorized)
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = true WHERE id = $1", a.adminID); err != nil {
		t.Fatalf("disable Programs key creator: %v", err)
	}
	assertEmptyLiveTVPrograms(t, f.request(t, http.MethodGet, liveTVProgramsPath, nil, a.headers))
	if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", a.key.CredentialID); err != nil {
		t.Fatalf("revoke Programs application credential: %v", err)
	}
	expectStatus(t, f.request(t, http.MethodGet, liveTVProgramsPath, nil, a.headers), http.StatusUnauthorized)
}

func TestHTTPLiveTVProgramsInputAndStorageFailuresRemainErrors(t *testing.T) {
	a := newApplicationKeyCatalogFixture(t)
	f := a.f
	for _, query := range []string{"Limit=-1", "Limit=2147483648", "Limit=1&Limit=1", "Fields=ChannelInfo,Unknown", "Limit=0&UserId=x&UserId=x", "X-Emby-Language=%00", "X-Emby-Unknown=x"} {
		expectStatus(t, f.request(t, http.MethodGet, liveTVProgramsPath+"?"+query, nil, a.headers), http.StatusBadRequest)
	}
	expectStatus(t, f.request(t, http.MethodGet, liveTVProgramsPath+"?broken=%gg", nil, a.headers), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodGet, liveTVProgramsPath+"?api_key=conflicting-token", nil, a.headers), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodPost, liveTVProgramsPath, nil, a.headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/LiveTv/Programs", nil, a.headers), http.StatusNotFound)
	request := httptest.NewRequest(http.MethodGet, liveTVProgramsPath, strings.NewReader("unexpected"))
	request.Header = a.headers.Clone()
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	expectStatus(t, response, http.StatusBadRequest)
	// Keep the running fixture unchanged while authenticating through its real store.
	unavailable := &Server{log: f.log}
	handler := f.app.requireEmby(unavailable.embyLiveTVPrograms)
	for _, query := range []string{"", "?Limit=0", "?LibrarySeriesId=" + a.seriesID} {
		request := httptest.NewRequest(http.MethodGet, liveTVProgramsPath+query, nil)
		request.Header = a.headers.Clone()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		expectStatus(t, response, http.StatusServiceUnavailable)
	}
}

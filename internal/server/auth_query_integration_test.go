//go:build linux

package server

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestHTTPEmbyQueryCarriersFormLoginMediaAndLogout(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	query := embyQueryClientValues()
	response := playbackTextRequest(t, p, http.MethodPost, "/emby/Users/authenticatebyname?"+query.Encode(),
		"Username=Playback+Viewer&Pw=playback-viewer-password", http.Header{"Content-Type": {"application/x-www-form-urlencoded; charset=UTF-8"}}, nil)
	expectStatus(t, response, http.StatusOK)
	login := jsonObject(t, response)
	token := stringValue(t, login, "AccessToken")
	session := objectValue(t, login, "SessionInfo")
	if objectValue(t, login, "User")["Id"] != p.s.viewerID || session["UserId"] != p.s.viewerID ||
		session["Client"] != "Emby Web" || session["DeviceId"] != "query-browser-device" ||
		session["DeviceName"] != "Chrome Linux" || session["ApplicationVersion"] != "4.9.5.0" {
		t.Fatal("query-metadata form authentication lost the actual user or client identity")
	}
	principal, err := p.s.f.users.Resolve(p.s.f.ctx, token, "emby")
	if err != nil || principal.User.ID != p.s.viewerID || principal.SessionID != session["Id"] || principal.Client.DeviceID != "query-browser-device" {
		t.Fatal("query-metadata login did not persist its returned authentication session")
	}
	query.Set("X-Emby-Token", token)
	query.Set("UserId", p.s.adminID)
	response = playbackTextRequest(t, p, http.MethodGet, "/emby/Users/"+p.s.viewerID+"?"+query.Encode(), "", nil, nil)
	expectStatus(t, response, http.StatusOK)
	if jsonObject(t, response)["Id"] != p.s.viewerID {
		t.Fatal("a query UserId claim changed the token-authenticated user")
	}
	response = playbackTextRequest(t, p, http.MethodGet, "/emby/Users/"+p.s.adminID+"?"+query.Encode(), "", nil, nil)
	expectAPIError(t, response, http.StatusForbidden, "access_denied", true)
	response = playbackTextRequest(t, p, http.MethodGet, "/admin/v1/session?"+query.Encode(), "", nil, nil)
	expectAPIError(t, response, http.StatusUnauthorized, "authentication_required", false)
	mediaQuery := url.Values{"Static": {"true"}, "X-Emby-Token": {token}}
	mediaPath := "/emby/Videos/" + p.s.video.id + "/stream?" + mediaQuery.Encode()
	response = playbackTextRequest(t, p, http.MethodGet, mediaPath, "", http.Header{"Range": {"bytes=0-15"}}, nil)
	expectStatus(t, response, http.StatusPartialContent)
	if !bytes.Equal(response.Body.Bytes(), p.s.video.data[:16]) ||
		response.Header().Get("Content-Range") != "bytes 0-15/"+strconv.Itoa(len(p.s.video.data)) {
		t.Fatal("query-token media authorization did not preserve the original byte-range contract")
	}
	response = playbackTextRequest(t, p, http.MethodPost, "/Sessions/Logout?x-emby-token="+url.QueryEscape(token), "", nil, nil)
	expectStatus(t, response, http.StatusNoContent)
	if response.Body.Len() != 0 {
		t.Fatal("successful query-token logout must have an empty response")
	}
	if _, err := p.s.f.users.Resolve(p.s.f.ctx, token, "emby"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("query-token logout did not revoke its exact authentication session")
	}
	for _, path := range []string{"/emby/Users/" + p.s.viewerID + "?" + query.Encode(), mediaPath} {
		response = playbackTextRequest(t, p, http.MethodGet, path, "", nil, nil)
		expectEmbyTextError(t, response, http.StatusUnauthorized, embyInvalidTokenMessage)
	}
	response = playbackTextRequest(t, p, http.MethodGet, "/emby/Users/"+p.s.viewerID, "", p.headers, nil)
	expectStatus(t, response, http.StatusOK)
}

func TestHTTPEmbyQueryCarriersRejectConflictsWithoutRevokingOtherSessions(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	f := p.s.f
	other, err := f.users.CreateUser(f.ctx, "Query Token Other", "query-token-other-password", false)
	if err != nil {
		t.Fatal("create query-token isolation user")
	}
	otherLogin := f.embyLogin(t, other.Name, "query-token-other-password")
	otherToken := stringValue(t, otherLogin, "AccessToken")
	first, second := url.QueryEscape(p.s.token), url.QueryEscape(otherToken)
	for _, test := range []struct {
		query   string
		headers http.Header
	}{
		{"X-Emby-Token=" + first + "&X-Emby-Token=" + second, nil},
		{"X-Emby-Token=" + first + "&x-emby-token=" + second, nil},
		{"X-Emby-Token=" + first + "&%58-Emby-Token=" + second, nil},
		{"X-Emby-Token=" + first + "&api_key=" + second, nil},
		{"X-Emby-Token=" + first, http.Header{"X-Emby-Token": {otherToken}}},
		{"X-Emby-Token=" + first, http.Header{"Authorization": {`MediaBrowser Token="` + otherToken + `"`}}},
		{"X-Emby-Token=" + first + "&X-Emby-Token=%GG", nil},
	} {
		response := playbackTextRequest(t, p, http.MethodGet, "/emby/Users/"+p.s.viewerID+"?"+test.query, "", test.headers, nil)
		expectEmbyTextError(t, response, http.StatusUnauthorized, embyInvalidTokenMessage)
	}
	response := playbackTextRequest(t, p, http.MethodGet, "/emby/Users/"+p.s.viewerID+"?X-Emby-Token="+first+"&x-emby-token="+first, "", nil, nil)
	expectStatus(t, response, http.StatusOK)
	response = playbackTextRequest(t, p, http.MethodGet, "/emby/Users/"+other.ID+"?X-Emby-Token="+first, "", nil, nil)
	expectAPIError(t, response, http.StatusForbidden, "access_denied", true)
	for _, token := range []string{p.s.token, otherToken} {
		if _, err := f.users.Resolve(f.ctx, token, "emby"); err != nil {
			t.Fatal("rejected query conflicts changed an existing authentication session")
		}
	}
}

func TestHTTPEmbyQueryMetadataCannotReplaceBodyCredentialsOrOverrideHeaders(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	query := embyQueryClientValues()
	query.Set("Username", "Playback Viewer")
	query.Set("Pw", "playback-viewer-password")
	var before int
	if err := p.s.f.pool.QueryRow(p.s.f.ctx, "SELECT count(*) FROM sessions").Scan(&before); err != nil {
		t.Fatal("count preexisting authentication sessions")
	}
	response := playbackTextRequest(t, p, http.MethodPost, "/emby/Users/AuthenticateByName?"+query.Encode(),
		"Pw=playback-viewer-password", http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, nil)
	expectEmbyTextError(t, response, http.StatusUnauthorized, embyInvalidLoginMessage)
	response = playbackTextRequest(t, p, http.MethodPost, "/emby/Users/AuthenticateByName?"+query.Encode(),
		"Username=Playback+Viewer&Pw=playback-viewer-password", http.Header{
			"Content-Type": {"application/x-www-form-urlencoded"}, "X-Emby-Device-Id": {"conflicting-header-device"},
		}, nil)
	expectAPIError(t, response, http.StatusBadRequest, "invalid_client", true)
	var after int
	if err := p.s.f.pool.QueryRow(p.s.f.ctx, "SELECT count(*) FROM sessions").Scan(&after); err != nil || after != before {
		t.Fatal("query credential substitution or metadata conflicts created a session")
	}
}

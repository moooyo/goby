//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func serverPolicyHTTPRequest(t *testing.T, f *serverFixture, peer, method, path string, body any, cookie *http.Cookie, csrf string, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(data)).WithContext(f.ctx)
	request.RemoteAddr = peer
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
		request.Header.Set("Origin", f.cfg.PublicURL)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	return response
}

func TestHTTPServerPolicyGlobalBoundaryAndTrustedProxy(t *testing.T) {
	f, cookie, csrf, _ := adminSettingsHTTPFixture(t)
	const remote = "203.0.113.17:12345"
	path := "/admin/v1/server-policy"
	expectStatus(t, serverPolicyHTTPRequest(t, f, remote, http.MethodGet, path, nil, nil, "", nil), http.StatusUnauthorized)
	initial := adminSettingsHTTPObject(t, serverPolicyHTTPRequest(t, f, remote, http.MethodGet, path, nil, cookie, "", nil), http.StatusOK)
	if initial["Revision"] != "1" || initial["EnableRemoteAccess"] != true || initial["UICulture"] != "zh-CN" {
		t.Fatal("global policy did not preserve its explicit migration defaults")
	}
	body := map[string]any{"Revision": "1", "EnableRemoteAccess": false, "LocalNetworkSubnets": []string{}, "UICulture": "en-US"}
	preview := adminSettingsHTTPObject(t, serverPolicyHTTPRequest(t, f, remote, http.MethodPost, path+"/preview", body, cookie, csrf, nil), http.StatusOK)
	if preview["WouldDisconnect"] != true || f.app.serverPolicy.Snapshot().Revision != 1 {
		t.Fatal("self-lockout preview wrote state or failed to identify the current client")
	}
	body["ConfirmDisconnect"] = false
	expectStatus(t, serverPolicyHTTPRequest(t, f, remote, http.MethodPut, path, body, cookie, csrf, nil), http.StatusBadRequest)
	body["ConfirmDisconnect"] = true
	expectStatus(t, serverPolicyHTTPRequest(t, f, remote, http.MethodPut, path, body, cookie, "", nil), http.StatusForbidden)
	changed := adminSettingsHTTPObject(t, serverPolicyHTTPRequest(t, f, remote, http.MethodPut, path, body, cookie, csrf, nil), http.StatusOK)
	if changed["Revision"] != "2" || changed["DisconnectExpected"] != true || f.app.serverUICulture() != "en-US" {
		t.Fatal("confirmed policy did not publish its real values")
	}
	for _, target := range []string{path, "/admin/", "/emby/System/Ping", "/emby/System/Info/Public"} {
		expectStatus(t, serverPolicyHTTPRequest(t, f, remote, http.MethodGet, target, nil, cookie, "", nil), http.StatusForbidden)
	}
	expectStatus(t, serverPolicyHTTPRequest(t, f, remote, http.MethodOptions, "/emby/System/Info", nil, nil, "", http.Header{"Origin": {"https://client.example.test"}, "Access-Control-Request-Method": {"GET"}}), http.StatusForbidden)
	expectStatus(t, serverPolicyHTTPRequest(t, f, remote, http.MethodGet, "/embywebsocket", nil, nil, "", http.Header{"Upgrade": {"websocket"}, "Connection": {"Upgrade"}}), http.StatusForbidden)
	expectStatus(t, serverPolicyHTTPRequest(t, f, remote, http.MethodGet, path, nil, cookie, "", http.Header{"X-Forwarded-For": {"127.0.0.1"}}), http.StatusForbidden)
	expectStatus(t, serverPolicyHTTPRequest(t, f, remote, http.MethodGet, "/healthz", nil, nil, "", nil), http.StatusOK)
	expectStatus(t, serverPolicyHTTPRequest(t, f, "127.0.0.1:12345", http.MethodGet, path, nil, cookie, "", nil), http.StatusOK)
	f.app.cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	for _, forwarded := range []string{"203.0.113.17", "invalid"} {
		expectStatus(t, serverPolicyHTTPRequest(t, f, "10.0.0.1:12345", http.MethodGet, path, nil, cookie, "", http.Header{"X-Forwarded-For": {forwarded}}), http.StatusForbidden)
	}
	expectStatus(t, serverPolicyHTTPRequest(t, f, "10.0.0.1:12345", http.MethodGet, path, nil, cookie, "", http.Header{"X-Forwarded-For": {"127.0.0.1"}}), http.StatusOK)
}

func TestHTTPServerRestartRequiresCapabilityConfirmationAndCSRF(t *testing.T) {
	f, cookie, csrf, _ := adminSettingsHTTPFixture(t)
	path := "/admin/v1/server/restart"
	body := map[string]any{"Revision": "1", "Confirm": true}
	request := func(csrf string) *httptest.ResponseRecorder {
		return serverPolicyHTTPRequest(t, f, "127.0.0.1:12345", http.MethodPost, path, body, cookie, csrf, nil)
	}
	expectStatus(t, request(csrf), http.StatusConflict)
	requested := make(chan struct{}, 1)
	f.app.restartController = &adminRestartController{request: func() { requested <- struct{}{} }}
	expectStatus(t, request(""), http.StatusForbidden)
	body["Confirm"] = false
	expectStatus(t, request(csrf), http.StatusBadRequest)
	body["Confirm"] = true
	expectStatus(t, request(csrf), http.StatusAccepted)
	expectStatus(t, request(csrf), http.StatusConflict)
	select {
	case <-requested:
	case <-time.After(2 * time.Second):
		t.Fatal("accepted restart did not reach its injected process owner")
	}
	select {
	case <-requested:
		t.Fatal("a duplicate request restarted twice")
	default:
	}
}

//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestHTTPProfilePinUsesOwnerOnlyCompatibleConfigurationAndNativeFlags(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	f.users = identity.NewWithApplicationKeyVault(f.pool, identity.NewApplicationKeyVault(filepath.Join(t.TempDir(), "profile-master.key")))
	f.app.identity = f.users
	path := "/emby/Users/" + accounts.viewer.userID + "/Configuration/Partial"
	response := f.request(t, http.MethodPost, path, map[string]any{"ProfilePin": "4826", "SubtitleMode": "Always"}, accounts.viewer.headers)
	expectStatus(t, response, http.StatusOK)
	if response.Body.Len() != 0 {
		t.Fatal("compatible profile update did not return an empty body")
	}
	for _, path := range []string{"/emby/Users/" + accounts.viewer.userID, "/emby/Users/Me"} {
		response := f.request(t, http.MethodGet, path, nil, accounts.viewer.headers)
		expectStatus(t, response, http.StatusOK)
		configuration := objectValue(t, jsonObject(t, response), "Configuration")
		if configuration["ProfilePin"] != "4826" || configuration["SubtitleMode"] != "Always" {
			t.Fatal("owner DTO did not project the atomic compatible profile configuration")
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("profile PIN response is cacheable")
		}
	}
	configuration := f.request(t, http.MethodGet, "/emby/Users/"+accounts.viewer.userID+"/Configuration", nil, accounts.second.headers)
	expectStatus(t, configuration, http.StatusOK)
	if jsonObject(t, configuration)["ProfilePin"] != "4826" {
		t.Fatal("another authenticated owner device could not consume the profile gate")
	}
	for _, request := range []struct {
		path    string
		headers http.Header
		native  bool
	}{
		{path: "/emby/Users/Public"},
		{path: "/emby/Users", headers: accounts.admin.headers},
		{path: "/emby/Users/Query", headers: accounts.admin.headers},
		{path: "/emby/Users/" + accounts.viewer.userID, headers: accounts.admin.headers},
		{path: "/emby/Users/" + accounts.viewer.userID + "/Configuration", headers: accounts.admin.headers},
		{path: "/admin/v1/users/" + accounts.viewer.userID + "/preferences", native: true},
		{path: "/admin/v1/users/" + accounts.viewer.userID + "/local-credentials", native: true},
	} {
		var response *httptest.ResponseRecorder
		if request.native {
			response = f.request(t, http.MethodGet, request.path, nil, nil, accounts.cookie)
		} else {
			response = f.request(t, http.MethodGet, request.path, nil, request.headers)
		}
		expectStatus(t, response, http.StatusOK)
		if bytes.Contains(response.Body.Bytes(), []byte(`"ProfilePin"`)) || bytes.Contains(response.Body.Bytes(), []byte("profile_pin_ciphertext")) {
			t.Fatal("a public or administrator DTO exposed another user's profile PIN")
		}
	}
	nativePath := "/admin/v1/users/" + accounts.viewer.userID + "/local-credentials"
	status := jsonObject(t, f.request(t, http.MethodGet, nativePath, nil, nil, accounts.cookie))
	if status["HasProfilePin"] != true || status["HasLocalPassword"] != false {
		t.Fatal("native flags disagree with the encrypted profile state")
	}
	patch := map[string]any{"Revision": status["Revision"], "EnableLocalPassword": false, "ProfilePin": "2468"}
	expectStatus(t, f.request(t, http.MethodPut, nativePath, patch, nil, accounts.cookie), http.StatusForbidden)
	response = f.request(t, http.MethodPut, nativePath, patch, http.Header{"X-CSRF-Token": {csrfToken(accounts.cookie.Value)}}, accounts.cookie)
	expectStatus(t, response, http.StatusOK)
	if jsonObject(t, response)["CurrentSessionRevoked"] != false {
		t.Fatal("profile-only update revoked a session")
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/Me", nil, accounts.viewer.headers), http.StatusOK)
	login := f.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", map[string]any{"Username": "Session Viewer", "Pw": "session-viewer-password"}, http.Header{"X-Emby-Client": {"Profile Login"}, "X-Emby-Device-Id": {"profile-login"}})
	expectStatus(t, login, http.StatusOK)
	if objectValue(t, objectValue(t, jsonObject(t, login), "User"), "Configuration")["ProfilePin"] != "2468" {
		t.Fatal("normal-password login did not return the owner's compatible profile state")
	}
	expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{"ProfilePin": "1357"}, accounts.other.headers), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{"ProfilePin": "1357", "SubtitleMode": "Invalid"}, accounts.viewer.headers), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{"ProfilePin": nil}, accounts.viewer.headers), http.StatusOK)
	cleared := f.request(t, http.MethodGet, "/emby/Users/Me", nil, accounts.viewer.headers)
	expectStatus(t, cleared, http.StatusOK)
	if _, exists := objectValue(t, jsonObject(t, cleared), "Configuration")["ProfilePin"]; exists {
		t.Fatal("null did not clear the client profile gate")
	}
}

func TestHTTPLocalPasswordRejectsSpoofedPeersAndRevokesTokensOnClear(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	path := "/admin/v1/users/" + accounts.viewer.userID + "/local-credentials"
	headers := http.Header{"X-CSRF-Token": {csrfToken(accounts.cookie.Value)}}
	configured := f.request(t, http.MethodPut, path, map[string]any{"Revision": "1", "EnableLocalPassword": true, "LocalPassword": "local-http-password"}, headers, accounts.cookie)
	expectStatus(t, configured, http.StatusOK)
	requestLogin := func(peer, forwarded string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"Username": "Session Viewer", "Pw": "local-http-password"})
		request := httptest.NewRequest(http.MethodPost, "/emby/Users/AuthenticateByName", bytes.NewReader(body)).WithContext(f.ctx)
		request.RemoteAddr = peer
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Emby-Client", "Local Peer Test")
		request.Header.Set("X-Emby-Device-Id", "local-peer-device")
		if forwarded != "" {
			request.Header.Set("X-Forwarded-For", forwarded)
		}
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, request)
		return response
	}
	expectStatus(t, requestLogin("192.0.2.77:1234", "192.168.1.5"), http.StatusUnauthorized)
	f.app.cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.90.0.0/24")}
	expectStatus(t, requestLogin("10.90.0.2:1234", "8.8.8.8"), http.StatusUnauthorized)
	expectStatus(t, requestLogin("10.90.0.2:1234", "invalid"), http.StatusUnauthorized)
	expectStatus(t, requestLogin("10.90.0.2:1234", ""), http.StatusUnauthorized)
	login := requestLogin("10.90.0.2:1234", "192.168.1.5")
	expectStatus(t, login, http.StatusOK)
	token := stringValue(t, jsonObject(t, login), "AccessToken")
	if _, err := f.users.ResolveWithPeer(f.ctx, token, "emby", "192.0.2.77"); err == nil {
		t.Fatal("locally issued token crossed the trusted peer boundary")
	}
	status := objectValue(t, jsonObject(t, configured), "Credentials")
	cleared := f.request(t, http.MethodPut, path, map[string]any{"Revision": status["Revision"], "EnableLocalPassword": false, "LocalPassword": ""}, headers, accounts.cookie)
	expectStatus(t, cleared, http.StatusOK)
	if _, err := f.users.ResolveWithPeer(f.ctx, token, "emby", "192.168.1.5"); err == nil {
		t.Fatal("cleared local credential retained a usable token")
	}
	expectStatus(t, requestLogin("192.168.1.5:1234", ""), http.StatusUnauthorized)
}

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type embyFormHTTPFixture struct {
	f      *serverFixture
	server *httptest.Server
	client *http.Client
}

func newEmbyFormHTTPFixture(t *testing.T, f *serverFixture) embyFormHTTPFixture {
	t.Helper()
	server := httptest.NewServer(f.handler)
	t.Cleanup(server.Close)
	transport := &http.Transport{MaxConnsPerHost: 1}
	t.Cleanup(transport.CloseIdleConnections)
	return embyFormHTTPFixture{f: f, server: server, client: &http.Client{Transport: transport, Timeout: 10 * time.Second}}
}

func (f embyFormHTTPFixture) request(t *testing.T, method, target, mediaType, body string, headers http.Header, reused *atomic.Bool, chunked ...bool) *httptest.ResponseRecorder {
	t.Helper()
	ctx := f.f.ctx
	if reused != nil {
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused.Store(info.Reused) }})
	}
	request, err := http.NewRequestWithContext(ctx, method, f.server.URL+target, strings.NewReader(body))
	if err != nil {
		t.Fatal("create real HTTP login fixture request")
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	if mediaType != "" {
		request.Header.Set("Content-Type", mediaType)
	}
	if len(chunked) > 0 && chunked[0] {
		request.ContentLength = -1
	}
	response, err := f.client.Do(request)
	if err != nil {
		t.Fatalf("perform real HTTP login fixture request: %v", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		t.Fatalf("read real HTTP login fixture response: %v", err)
	}
	result := httptest.NewRecorder()
	for name, values := range response.Header {
		result.Header()[name] = append([]string(nil), values...)
	}
	result.WriteHeader(response.StatusCode)
	_, _ = result.Write(data)
	return result
}

func TestHTTPEmbyFormLoginCreatesScopedSessionAndKeepsConnectionAlive(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	name, password := "Form + Viewer", "form password +&=% \u5bc6\u7801"
	viewer, err := f.users.CreateUser(f.ctx, name, password, false)
	if err != nil {
		t.Fatalf("create form-login viewer: %v", err)
	}
	client := newEmbyFormHTTPFixture(t, f)
	headers := referenceEmbySeparateHeaders()
	headers.Set("Origin", "https://client.example.test")
	response := client.request(t, http.MethodPost, "/emby/users/authenticatebyname", "application/x-www-form-urlencoded; charset=UTF-8",
		url.Values{"Username": {name}, "Pw": {password}}.Encode(), headers, nil)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("Access-Control-Allow-Origin") != "https://client.example.test" {
		t.Fatal("form authentication lost compatibility CORS")
	}
	login := jsonObject(t, response)
	token := stringValue(t, login, "AccessToken")
	user, session := objectValue(t, login, "User"), objectValue(t, login, "SessionInfo")
	if user["Id"] != viewer.ID || objectValue(t, user, "Policy")["IsAdministrator"] != false || session["UserId"] != viewer.ID ||
		session["Client"] != "Goby Reference Recorder" || session["DeviceId"] != "goby-reference-recorder" {
		t.Fatal("form authentication changed the user, role, or client identity")
	}
	principal, err := f.users.Resolve(f.ctx, token, "emby")
	if err != nil || principal.User.ID != viewer.ID || principal.SessionID != session["Id"] {
		t.Fatal("form authentication did not persist the returned owned token session")
	}
	var reused atomic.Bool
	protected := client.request(t, http.MethodGet, "/emby/Users/"+viewer.ID, "", "", http.Header{"X-Emby-Token": {token}}, &reused)
	expectStatus(t, protected, http.StatusOK)
	if !reused.Load() || jsonObject(t, protected)["Id"] != viewer.ID {
		t.Fatal("completed form-body cleanup prevented authenticated connection reuse")
	}
	denied := client.request(t, http.MethodGet, "/emby/Users/Query", "", "", http.Header{"X-Emby-Token": {token}}, nil)
	expectAPIError(t, denied, http.StatusForbidden, "administrator_required", true)
}

func TestHTTPEmbyFormLoginByIDAndBodyOnlyCredentials(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Selected Form Viewer", "selected-form-password", false)
	if err != nil {
		t.Fatal("create selected form viewer")
	}
	client := newEmbyFormHTTPFixture(t, f)
	headers := referenceEmbySeparateHeaders()
	response := client.request(t, http.MethodPost, "/Users/"+viewer.ID+"/authenticate?UserId="+adminID+"&Pw=wrong-query-password",
		"application/x-www-form-urlencoded", "pW=selected-form-password", headers, nil, true)
	expectStatus(t, response, http.StatusOK)
	if objectValue(t, jsonObject(t, response), "User")["Id"] != viewer.ID {
		t.Fatal("selected-user form authentication used an untrusted query principal")
	}
	response = client.request(t, http.MethodPost, "/Users/AuthenticateByName?Username=Administrator&Pw=administrator-password",
		"application/x-www-form-urlencoded", "Username=Selected+Form+Viewer&Pw=selected-form-password", headers, nil)
	expectStatus(t, response, http.StatusOK)
	if objectValue(t, jsonObject(t, response), "User")["Id"] != viewer.ID {
		t.Fatal("login query credentials overrode the complete form body")
	}
	encoded, err := json.Marshal(map[string]string{"Username": viewer.Name, "Pw": "selected-form-password"})
	if err != nil {
		t.Fatal("encode JSON login regression")
	}
	response = client.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", "application/json", string(encoded), headers, nil)
	expectStatus(t, response, http.StatusOK)
	if objectValue(t, jsonObject(t, response), "User")["Id"] != viewer.ID {
		t.Fatal("the form adapter changed ordinary JSON login identity")
	}
}

func TestHTTPEmbyFormLoginFailuresDoNotCreateSessions(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Disabled Form Viewer", "disabled-form-password", false)
	if err != nil {
		t.Fatal("create disabled form viewer")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled=true WHERE id=$1", viewer.ID); err != nil {
		t.Fatal("disable form-login fixture user")
	}
	client := newEmbyFormHTTPFixture(t, f)
	conflicting := referenceEmbySeparateHeaders()
	conflicting.Set("Authorization", `Emby Client="Different Client", DeviceId="goby-reference-recorder"`)
	for _, test := range []struct {
		name, target, body, code, text string
		headers                        http.Header
		status                         int
	}{
		{name: "wrong_password", target: "/emby/Users/AuthenticateByName", body: "Username=Administrator&Pw=wrong-password", status: 401, text: embyInvalidLoginMessage},
		{name: "unknown_user", target: "/emby/Users/AuthenticateByName", body: "Username=Unknown+Form+Viewer&Pw=administrator-password", status: 401, text: embyInvalidLoginMessage},
		{name: "unknown_selected_user", target: "/emby/Users/missing-form-user/Authenticate", body: "Pw=administrator-password", status: 401, text: embyInvalidLoginMessage},
		{name: "disabled_user", target: "/emby/Users/" + viewer.ID + "/Authenticate", body: "Pw=disabled-form-password", status: 401, text: embyInvalidLoginMessage},
		{name: "duplicate_principal", target: "/emby/Users/AuthenticateByName", body: "Username=Administrator&username=Other&Pw=administrator-password", status: 400, code: "invalid_form"},
		{name: "bad_encoding", target: "/emby/Users/AuthenticateByName", body: "Username=Administrator&Pw=%GG", status: 400, code: "invalid_form"},
		{name: "query_does_not_supply_name", target: "/emby/Users/AuthenticateByName?Username=Administrator&Pw=administrator-password", body: "Pw=administrator-password", status: 401, text: embyInvalidLoginMessage},
		{name: "conflicting_client", target: "/emby/Users/AuthenticateByName", body: "Username=Administrator&Pw=administrator-password", headers: conflicting, status: 400, code: "invalid_client"},
		{name: "form_cannot_select_other_path_user", target: "/emby/Users/" + adminID + "/Authenticate", body: "Username=Other&Pw=administrator-password", status: 400, code: "invalid_form"},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := test.headers
			if headers == nil {
				headers = referenceEmbySeparateHeaders()
			}
			response := client.request(t, http.MethodPost, test.target, "application/x-www-form-urlencoded; charset=UTF-8", test.body, headers, nil)
			if test.text != "" {
				expectEmbyTextError(t, response, test.status, test.text)
			} else {
				expectAPIError(t, response, test.status, test.code, true)
			}
			var sessions int
			if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM sessions").Scan(&sessions); err != nil || sessions != 0 {
				t.Fatal("rejected form authentication persisted a session")
			}
		})
	}
}

func TestHTTPEmbyFormLoginKeepsRateLimitAndNativeJSONBoundary(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	client := newEmbyFormHTTPFixture(t, f)
	headers := referenceEmbySeparateHeaders()
	for index := 0; index < 10; index++ {
		response := client.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", "application/x-www-form-urlencoded",
			"Username=Administrator&Pw=wrong-password", headers, nil)
		expectEmbyTextError(t, response, http.StatusUnauthorized, embyInvalidLoginMessage)
	}
	response := client.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", "application/x-www-form-urlencoded",
		"Username=Administrator&Pw=administrator-password", headers, nil)
	expectAPIError(t, response, http.StatusTooManyRequests, "rate_limited", true)
	if response.Header().Get("Retry-After") != "60" {
		t.Fatal("form authentication lost its existing login rate-limit response")
	}
	// A different fresh fixture keeps the native media-type assertion separate
	// from the intentionally exhausted compatibility login quota above.
	native := newServerFixture(t)
	native.bootstrap(t)
	nativeClient := newEmbyFormHTTPFixture(t, native)
	response = nativeClient.request(t, http.MethodPost, "/admin/v1/session", "application/x-www-form-urlencoded",
		"Name=Administrator&Password=administrator-password", http.Header{"Origin": {native.cfg.PublicURL}}, nil)
	expectAPIError(t, response, http.StatusUnsupportedMediaType, "unsupported_media_type", false)
}

func TestHTTPEmbyFormLoginRejectsOversizedChunkedBody(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	client := newEmbyFormHTTPFixture(t, f)
	response := client.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", "application/x-www-form-urlencoded; charset=UTF-8",
		"Username=Administrator&Pw="+strings.Repeat("x", maxEmbyLoginFormBytes), referenceEmbySeparateHeaders(), nil, true)
	expectAPIError(t, response, http.StatusRequestEntityTooLarge, "payload_too_large", true)
	var sessions int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM sessions").Scan(&sessions); err != nil || sessions != 0 {
		t.Fatal("an oversized chunked login form created a session")
	}
}

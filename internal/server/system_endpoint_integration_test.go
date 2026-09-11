package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestHTTPSystemEndpointUsesCurrentTokenAndObservedLoopbackShape(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Endpoint Viewer", "endpoint-viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	login := f.embyLogin(t, viewer.Name, "endpoint-viewer-password")
	token := stringValue(t, login, "AccessToken")
	host := httptest.NewServer(f.handler)
	defer host.Close()
	query := url.Values{
		"X-Emby-Token": {token}, "X-Emby-Client": {"Emby Web"},
		"X-Emby-Device-Name": {"Chromium"}, "X-Emby-Device-Id": {"endpoint-client"},
		"X-Emby-Client-Version": {"4.9.5.0"}, "X-Emby-Language": {"en-us"},
	}
	for _, route := range []string{"/emby/System/Endpoint", "/System/Endpoint", "/EMBY/system/endpoint"} {
		response, err := host.Client().Get(host.URL + route + "?" + query.Encode())
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]bool
		err = json.NewDecoder(response.Body).Decode(&got)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || err != nil ||
			!reflect.DeepEqual(got, map[string]bool{"IsLocal": true, "IsInNetwork": true}) {
			t.Fatalf("loopback endpoint response: status=%d shape=%v decode=%v", response.StatusCode, got, err)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("a connection-specific result was cacheable")
		}
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/System/Endpoint", nil, nil), http.StatusUnauthorized)
	cookie, _ := f.adminLogin(t)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/System/Endpoint", nil, nil, cookie), http.StatusUnauthorized)
	if err := f.users.Revoke(f.ctx, token); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/System/Endpoint?"+query.Encode(), nil, nil), http.StatusUnauthorized)
}

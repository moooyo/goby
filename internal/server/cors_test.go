package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbyPreflightMatchesReferenceAndDoesNotExposeAdmin(t *testing.T) {
	handler := (&Server{}).Handler()
	request := httptest.NewRequest(http.MethodOptions, "/emby/Items", nil)
	request.Header.Set("Origin", "https://example.invalid")
	request.Header.Set("Access-Control-Request-Method", "GET")
	request.Header.Set("Access-Control-Request-Headers", "X-Emby-Token,Content-Type")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || response.Body.Len() != 0 {
		t.Fatalf("preflight = %d %q", response.Code, response.Body.String())
	}
	for name, want := range map[string]string{"Access-Control-Allow-Origin": "https://example.invalid", "Access-Control-Allow-Credentials": "true", "Content-Type": "text/plain", "Content-Length": "0"} {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	for _, header := range []string{"X-Emby-Token", "Content-Type", "X-MediaBrowser-Token", "X-Emby-Device-Id"} {
		if !strings.Contains(response.Header().Get("Access-Control-Allow-Headers"), header) {
			t.Errorf("missing allowed header %s", header)
		}
	}
	admin := httptest.NewRequest(http.MethodOptions, "/admin/v1/users", nil)
	admin.Header = request.Header.Clone()
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, admin)
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("administrator API was exposed through compatibility CORS")
	}
}

func TestCompatibilityCORSAllowsAuthenticatedClientOriginsOnlyAsCORSData(t *testing.T) {
	server := &Server{}
	for _, origin := range []string{"https://client.example", "http://127.0.0.1:1234", "null"} {
		request := httptest.NewRequest(http.MethodGet, "/emby/System/Ping", nil)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Header().Get("Access-Control-Allow-Origin") != origin || response.Body.String() != "Emby Server" {
			t.Fatalf("client origin was not preserved: %q", origin)
		}
	}
	for _, origin := range []string{"https://client.example/path", "https://user:password@example.com", "https://client.example?query=x"} {
		request := httptest.NewRequest(http.MethodOptions, "/emby/Items", nil)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Header().Get("Access-Control-Allow-Origin") != "" || response.Code != 400 {
			t.Fatalf("invalid origin accepted: %q", origin)
		}
	}
}

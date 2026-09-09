package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompatibilityNamespacePreservesIdentifiersAndEncodedNames(t *testing.T) {
	for _, test := range []struct{ path, want string }{
		{"/videos/AbC123/original.mp4?api_key=opaque", "/emby/Videos/AbC123/original.mp4"},
		{"/EMBY/users/AbC/items/ReS123", "/emby/Users/AbC/Items/ReS123"},
		{"/users/authenticatebyname", "/emby/Users/AuthenticateByName"},
		{"/genres/AC%2FDC", "/emby/Genres/AC%2FDC"},
		{"/persons/Playing", "/emby/Persons/Playing"},
		{"/admin/v1/users", "/admin/v1/users"},
		{"/admin/", "/admin/"},
		{"/healthz", "/healthz"},
		{"/unknown/path", "/unknown/path"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		before := request.URL.String()
		result := compatibilityNamespace(request)
		if result.URL.EscapedPath() != test.want || result.URL.RawQuery != request.URL.RawQuery {
			t.Errorf("namespace %q = %q, want %q", test.path, result.URL.String(), test.want)
		}
		if request.URL.String() != before {
			t.Error("namespace translation mutated the original request")
		}
	}
}

func TestRootCompatibilityAliasesSharePingAndMediaCORS(t *testing.T) {
	handler := (&Server{}).Handler()
	for _, path := range []string{"/System/Ping", "/system/ping", "/Emby/system/ping"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Body.String() != "Emby Server" {
			t.Errorf("root ping alias failed: %s, %d", path, response.Code)
		}
	}
	for _, path := range []string{"/Videos/id/original.mp4", "/audio/id/stream"} {
		request := httptest.NewRequest(http.MethodOptions, path, nil)
		request.Header.Set("Origin", "https://client.example")
		request.Header.Set("Access-Control-Request-Method", "HEAD")
		request.Header.Set("Access-Control-Request-Headers", "X-Emby-Token,If-Range")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Access-Control-Allow-Origin") != "https://client.example" ||
			!strings.Contains(response.Header().Get("Access-Control-Allow-Headers"), "If-Range") {
			t.Errorf("media alias did not share compatibility CORS: %s", path)
		}
	}
}

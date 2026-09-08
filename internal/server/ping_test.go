package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbyPingMatchesReferenceMethods(t *testing.T) {
	handler := (&Server{}).Handler()
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(method, "/emby/System/Ping", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d", response.Code)
			}
			if got := response.Header().Get("Content-Type"); got != "text/plain" {
				t.Errorf("Content-Type = %q", got)
			}
			if got := response.Header().Get("Content-Length"); got != "11" {
				t.Errorf("Content-Length = %q", got)
			}
			want := "Emby Server"
			if method == http.MethodHead {
				want = ""
			}
			if got := response.Body.String(); got != want {
				t.Errorf("body = %q, want %q", got, want)
			}
		})
	}
}

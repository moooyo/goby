package server

import (
	"net/http"
	"testing"
)

func TestHTTPAnonymousBrandingDefaultsAndNamespaceIsolation(t *testing.T) {
	f := newServerFixture(t)
	origin := "https://client.example.test"
	headers := http.Header{"Origin": {origin}}
	for _, prefix := range []string{"/emby/Branding", "/Branding", "/emby/branding", "/branding"} {
		response := f.request(t, http.MethodGet, prefix+"/configuration", nil, headers)
		expectStatus(t, response, http.StatusOK)
		if len(jsonObject(t, response)) != 0 || response.Header().Get("Access-Control-Allow-Origin") != origin {
			t.Fatal("anonymous branding must project the actual empty configuration with compatibility CORS")
		}
		for _, suffix := range []string{"/css", "/css.CSS"} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				response = f.request(t, method, prefix+suffix, nil, headers)
				expectStatus(t, response, http.StatusOK)
				if response.Header().Get("Content-Type") != "text/css" || response.Header().Get("Content-Length") != "0" || response.Body.Len() != 0 {
					t.Fatal("the default stylesheet must be an empty CSS response for GET and HEAD")
				}
			}
		}
		response = f.request(t, http.MethodPost, prefix+"/Configuration", map[string]string{"LoginDisclaimer": "unsupported write"}, headers)
		if response.Code < http.StatusBadRequest {
			t.Fatal("read-only branding must not acknowledge unsupported configuration writes")
		}
	}
	response := f.request(t, http.MethodGet, "/admin/v1/Branding/Configuration", nil, headers)
	expectStatus(t, response, http.StatusNotFound)
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("compatibility branding must not expand administrator CORS or routes")
	}
}

package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/moooyo/goby/internal/config"
)

const dashboardTestCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"

func TestDashboardInjectedAssetsThroughHandler(t *testing.T) {
	index := "<!doctype html><html><body>Bundled dashboard</body></html>\n"
	script := "export const dashboard = 'bundled';\n"
	stylesheet := "body { color: #123456; }\n"
	assets := fstest.MapFS{
		"index.html":              &fstest.MapFile{Data: []byte(index)},
		"assets/app-0a1b2c3d.js":  &fstest.MapFile{Data: []byte(script)},
		"assets/app-4e5f6a7b.css": &fstest.MapFile{Data: []byte(stylesheet)},
	}
	handler := dashboardTestHandler(filepath.Join(t.TempDir(), "missing-external-assets"), WithDashboardAssets(assets))
	cases := []struct {
		name        string
		path        string
		body        string
		contentType string
		cache       string
	}{
		{"index", "/admin/", index, "text/html", "no-store"},
		{"spa", "/admin/libraries", index, "text/html", "no-store"},
		{"authentication-shell", "/admin/login", index, "text/html", "no-store"},
		{"nested-spa", "/admin/backups/operations/example", index, "text/html", "no-store"},
		{"script", "/admin/assets/app-0a1b2c3d.js", script, "javascript", "public, max-age=31536000, immutable"},
		{"stylesheet", "/admin/assets/app-4e5f6a7b.css", stylesheet, "text/css", "public, max-age=31536000, immutable"},
	}
	for _, test := range cases {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(test.name+"/"+method, func(t *testing.T) {
				response := dashboardTestRequest(handler, method, test.path)
				if response.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body = %q", response.Code, response.Body.String())
				}
				if got := response.Header().Get("Content-Type"); !strings.Contains(got, test.contentType) {
					t.Errorf("Content-Type = %q, want %q", got, test.contentType)
				}
				if got := response.Header().Get("Content-Length"); got != strconv.Itoa(len(test.body)) {
					t.Errorf("Content-Length = %q, want %d", got, len(test.body))
				}
				if got := response.Header().Get("Cache-Control"); got != test.cache {
					t.Errorf("Cache-Control = %q, want %q", got, test.cache)
				}
				if got := response.Header().Get("Content-Security-Policy"); got != dashboardTestCSP {
					t.Errorf("Content-Security-Policy = %q, want %q", got, dashboardTestCSP)
				}
				wantBody := test.body
				if method == http.MethodHead {
					wantBody = ""
				}
				if got := response.Body.String(); got != wantBody {
					t.Errorf("body = %q, want %q", got, wantBody)
				}
			})
		}
	}
}

func TestDashboardDoesNotReplaceMissingFilesOrAdministratorAPIs(t *testing.T) {
	index := "<!doctype html><html><body>Dashboard fallback marker</body></html>"
	handler := dashboardTestHandler("", WithDashboardAssets(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(index)},
	}))
	cases := []struct {
		name      string
		path      string
		code      int
		errorCode string
	}{
		{"missing-script", "/admin/assets/missing-deadbeef.js", http.StatusNotFound, ""},
		{"missing-html", "/admin/missing.html", http.StatusNotFound, ""},
		{"unknown-api", "/admin/v1/unknown-dashboard-route", http.StatusNotFound, "not_found"},
		{"session-without-cookie", "/admin/v1/session", http.StatusUnauthorized, "authentication_required"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			response := dashboardTestRequest(handler, http.MethodGet, test.path)
			if response.Code != test.code {
				t.Fatalf("status = %d, want %d; body = %q", response.Code, test.code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "Dashboard fallback marker") {
				t.Fatal("a missing file or API request received the dashboard document")
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
			if test.errorCode == "" {
				return
			}
			if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			var body struct {
				Error struct {
					Code string
				}
				RequestId string
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode API response: %v", err)
			}
			if body.Error.Code != test.errorCode || body.RequestId == "" {
				t.Errorf("API error = %#v, want code %q and a request ID", body, test.errorCode)
			}
		})
	}
}

func TestDashboardExternalDirectoryFallbackAndInjectedPriority(t *testing.T) {
	index := "<!doctype html><html><body>External dashboard</body></html>\n"
	script := "export const dashboard = 'external';\n"
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"index.html": index, "assets/external-deadbeef.js": script} {
		if err := os.WriteFile(filepath.Join(directory, filepath.FromSlash(name)), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name    string
		options []Option
	}{
		{"default-fallback", nil},
		{"nil-assets-fallback", []Option{WithDashboardAssets(nil)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := dashboardTestHandler(directory, test.options...)
			for path, want := range map[string]string{"/admin/": index, "/admin/assets/external-deadbeef.js": script} {
				response := dashboardTestRequest(handler, http.MethodGet, path)
				if response.Code != http.StatusOK || response.Body.String() != want {
					t.Errorf("GET %s = (%d, %q), want (200, %q)", path, response.Code, response.Body.String(), want)
				}
			}
		})
	}
	t.Run("injected-files-replace-external-directory", func(t *testing.T) {
		injected := "<!doctype html><html><body>Injected dashboard</body></html>\n"
		handler := dashboardTestHandler(directory, WithDashboardAssets(fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte(injected)},
		}))
		response := dashboardTestRequest(handler, http.MethodGet, "/admin/")
		if response.Code != http.StatusOK || response.Body.String() != injected {
			t.Fatalf("injected index = (%d, %q), want (200, %q)", response.Code, response.Body.String(), injected)
		}
		response = dashboardTestRequest(handler, http.MethodGet, "/admin/assets/external-deadbeef.js")
		if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), script) {
			t.Errorf("injected FS fell back to external-only asset: status = %d, body = %q", response.Code, response.Body.String())
		}
	})
}

func dashboardTestHandler(directory string, options ...Option) http.Handler {
	server := &Server{
		cfg: config.Config{WebDirectory: directory},
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, option := range options {
		option(server)
	}
	return server.Handler()
}

func dashboardTestRequest(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	return response
}

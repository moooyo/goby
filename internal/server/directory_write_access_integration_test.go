//go:build linux

package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestHTTPDirectoryWriteValidationUsesAllowedRootsWithoutProbeFiles(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	f.bootstrap(t)
	path := filepath.Join(root, "empty-write-check")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	login := f.embyLogin(t, "Administrator", "administrator-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	response := f.request(t, http.MethodPost, "/emby/Environment/ValidatePath?Path="+url.QueryEscape(path), map[string]any{"ValidateWriteable": true}, headers)
	expectStatus(t, response, http.StatusNoContent)
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("write access validation changed a media directory: entries=%v err=%v", entries, err)
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Environment/ValidatePath?Path="+url.QueryEscape(t.TempDir()), map[string]any{"ValidateWriteable": true}, headers), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Environment/ValidatePath?Path="+url.QueryEscape(path), map[string]any{"ValidateWriteable": true}, nil), http.StatusUnauthorized)
}

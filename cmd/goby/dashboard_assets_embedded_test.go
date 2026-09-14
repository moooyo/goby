//go:build goby_embed_admin

package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardAssetsEmbeddedWithoutExternalDirectory(t *testing.T) {
	t.Setenv("GOBY_WEB_DIR", "")
	assets, err := dashboardAssets(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatal(err)
	}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil || len(index) == 0 {
		t.Fatalf("embedded index is unavailable: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	response := httptest.NewRecorder()
	http.ServeFileFS(response, request, assets, "index.html")
	if response.Code != http.StatusOK || response.Body.String() != string(index) {
		t.Fatalf("embedded index response = %d, bytes = %d", response.Code, response.Body.Len())
	}
	chunks := 0
	err = fs.WalkDir(assets, "assets", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".css") {
			chunks++
			request := httptest.NewRequest(http.MethodHead, "/admin/"+name, nil)
			response := httptest.NewRecorder()
			http.ServeFileFS(response, request, assets, name)
			if response.Code != http.StatusOK || response.Body.Len() != 0 {
				t.Errorf("embedded chunk %q response = %d, body bytes = %d", name, response.Code, response.Body.Len())
			}
		}
		return nil
	})
	if err != nil || chunks == 0 {
		t.Fatalf("production chunks are unavailable: chunks = %d, error = %v", chunks, err)
	}
}

package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDashboardAssetsExplicitDirectory(t *testing.T) {
	directory := t.TempDir()
	want := []byte("explicit administrator assets")
	if err := os.WriteFile(filepath.Join(directory, "index.html"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOBY_WEB_DIR", directory)
	assets, err := dashboardAssets(directory)
	if err != nil {
		t.Fatal(err)
	}
	response := dashboardAssetResponse(assets)
	if response.Code != http.StatusOK || response.Body.String() != string(want) {
		t.Fatalf("explicit directory response = %d, body = %q", response.Code, response.Body.String())
	}
	if err := os.Remove(filepath.Join(directory, "index.html")); err != nil {
		t.Fatal(err)
	}
	response = dashboardAssetResponse(assets)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing override file must not fall back to embedded assets: status = %d", response.Code)
	}
}

func dashboardAssetResponse(assets fs.FS) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	http.ServeFileFS(response, httptest.NewRequest(http.MethodGet, "/admin/", nil), assets, "index.html")
	return response
}

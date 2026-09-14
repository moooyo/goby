//go:build !goby_embed_admin

package main

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestDashboardAssetsDevelopmentDirectory(t *testing.T) {
	t.Setenv("GOBY_WEB_DIR", "")
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("development bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	assets, err := dashboardAssets(directory)
	if err != nil {
		t.Fatal(err)
	}
	response := dashboardAssetResponse(assets)
	if response.Code != http.StatusOK || response.Body.String() != "development bundle" {
		t.Fatalf("development directory response = %d, body = %q", response.Code, response.Body.String())
	}
}

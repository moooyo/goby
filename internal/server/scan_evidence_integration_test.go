//go:build linux

package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/config"
)

func TestServerScanEvidenceUsesIndependentDeploymentRoot(t *testing.T) {
	f := newServerFixture(t)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := f.cfg
	cfg.ScanEvidence = config.ScanEvidenceConfig{Enabled: true, Directory: directory, MaxBytes: 1 << 30,
		MaxDirectories: 131072, MaxEntries: 1048576, MaxFallbackHandles: 4096}
	app, err := New(f.ctx, cfg, f.pool, f.users, f.log, "scan-evidence-test")
	if err != nil {
		t.Fatal(err)
	}
	if !app.library.ScanEvidenceStatus().Enabled {
		t.Fatal("server omitted the configured scan evidence inventory")
	}
	if err := app.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, ".goby-scan-evidence.json")); err != nil {
		t.Fatal("owned scan evidence manifest was not created")
	}
	cfg.MediaRoots = []string{directory}
	if app, err := New(f.ctx, cfg, f.pool, f.users, f.log, "scan-evidence-test"); err == nil {
		_ = app.Close(f.ctx)
		t.Fatal("server admitted a scan evidence parent inside media")
	}
}

//go:build linux

package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanEvidenceStoreCatalogScopeAndCallerCompatibility(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, original, mediaRoot, _ := libraryIntegrationStore(t, prober)
	if original.ScanEvidenceStatus().Enabled {
		t.Fatal("legacy caller enabled a spool")
	}
	if err := original.Close(ctx); err != nil {
		t.Fatal(err)
	}
	options := scanEvidenceTestOptions(t)
	for attempt := 0; attempt < 2; attempt++ {
		store, err := New(pool, prober, []string{mediaRoot}, WithScanEvidence(options))
		if err != nil {
			t.Fatal(err)
		}
		pass, err := store.newScanReconciliationEvidence(ctx)
		if err != nil {
			_ = store.Close(context.Background())
			t.Fatal(err)
		}
		if !store.ScanEvidenceStatus().Enabled || pass.spool == nil {
			t.Fatal("configured store did not use its owned evidence parent")
		}
		if err := pass.Close(); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(options.Directory, scanEvidenceManifest))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), options.ServerID) || strings.Contains(string(raw), "postgresql:") {
		t.Fatal("owner manifest exposed an unnormalized identity or connection secret")
	}
	options.ServerID = "another-persistent-server"
	if store, err := New(pool, prober, []string{mediaRoot}, WithScanEvidence(options)); err == nil {
		_ = store.Close(ctx)
		t.Fatal("changed server identity adopted old spool ownership")
	}
}

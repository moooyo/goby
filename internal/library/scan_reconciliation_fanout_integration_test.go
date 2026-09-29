//go:build linux

package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Run explicitly with GOBY_SCAN_RECONCILIATION_FANOUT=1. The fixture exercises
// the final deletion proof across thousands of directory observations without
// repeating a full real-media scan.
func TestScanReconciliationStagedHighDirectoryFanoutDeletionProof(t *testing.T) {
	if os.Getenv("GOBY_SCAN_RECONCILIATION_FANOUT") != "1" {
		t.Skip("set GOBY_SCAN_RECONCILIATION_FANOUT=1 for the high-fanout proof")
	}
	fixture := newRootBindingScanFixture(t)
	const directories = 4200
	for index := 0; index < directories; index++ {
		path := filepath.Join(fixture.scanRoot.path, "fanout", fmt.Sprintf("%04d", index))
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("create fanout directory %d: %v", index, err)
		}
	}
	const acceptedID = "high-fanout-accepted"
	const missingID = "high-fanout-missing"
	const acceptedRelative = "fanout/0001/Accepted.mkv"
	const missingRelative = "fanout/0002/Missing.mkv"
	libraryIntegrationFile(t, fixture.scanRoot.path, acceptedRelative, "video:high-fanout-accepted")
	scanReconciliationCommitInsertItem(t, fixture, acceptedID, acceptedRelative, "Movie", fixture.library.ID, false)
	scanReconciliationCommitInsertItem(t, fixture, missingID, missingRelative, "Movie", fixture.library.ID, false)
	stage := scanReconciliationStageFixture(t, fixture, []string{acceptedID})
	capture := scanReconciliationCommitCapture(t, fixture, nil)
	evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{})
	scanReconciliationCommitObserveRoot(t, evidence, capture)
	if err := evidence.requireComplete(fixture.ctx); err != nil {
		t.Fatalf("high-fanout directory evidence is incomplete: %v", err)
	}

	// A wrong stored path must retain both catalog rows and all notifications.
	wrongPath := filepath.Join(fixture.scanRoot.path, "different", "Missing.mkv")
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET path=$2 WHERE id=$1`, missingID, wrongPath); err != nil {
		t.Fatal(err)
	}
	before := scanReconciliationCommitSnapshot(t, fixture)
	notifications := catalogChangesTestListener(t, fixture.store)
	if _, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library,
		[]*rootBindingScanCapture{capture}, evidence, nil, stage); !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
		t.Fatalf("unproven physical path was accepted: %v", err)
	}
	if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
		t.Fatal("rejected high-fanout deletion changed catalog state")
	}
	scanReconciliationCommitAssertItem(t, fixture, missingID, true)
	scanReconciliationCommitAssertItem(t, fixture, acceptedID, true)
	assertNoCatalogTestNotification(t, notifications)

	correctPath := filepath.Join(fixture.scanRoot.path, filepath.FromSlash(missingRelative))
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET path=$2 WHERE id=$1`, missingID, correctPath); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library,
		[]*rootBindingScanCapture{capture}, evidence, nil, stage); err != nil {
		t.Fatalf("proven high-fanout deletion failed: %v", err)
	}
	t.Logf("high-fanout deletion proof directories=%d elapsed=%s", directories, time.Since(started))
	scanReconciliationCommitAssertItem(t, fixture, missingID, false)
	scanReconciliationCommitAssertItem(t, fixture, acceptedID, true)
	scanReconciliationCommitAssertItem(t, fixture, fixture.library.ID, true)
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{
		Kind: CatalogRemoved, ItemID: missingID, LibraryID: fixture.library.ID, ParentID: fixture.library.ID,
	}})
	assertNoCatalogTestNotification(t, notifications)
}

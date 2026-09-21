//go:build linux

package library

import (
	"errors"
	"fmt"
	"testing"
)

func scanReconciliationStageFixture(t *testing.T, fixture rootBindingScanFixture, ids []string) *scanReconciliationStaging {
	t.Helper()
	stage, err := fixture.store.beginScanReconciliationStaging(fixture.ctx, fixture.task.job.ID, fixture.library.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := stage.Close(); err != nil && !fixture.store.ownership.lost.Load() {
			t.Error(err)
		}
	})
	for _, id := range ids {
		if err := stage.Record(fixture.ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := stage.Seal(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	return stage
}

// This is a database/locking regression, not real-media capacity acceptance.
// Its accepted rows deliberately exceed the old all-library retention budget.
func TestScanReconciliationStagedPagingExcludesLargeAcceptedPopulationBeforeLocks(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	const count = 40000
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
		SELECT 'seen-'||lpad(n::text,6,'0'),$1,$2,$1,'Accepted','accepted','Movie',false,
		$3||'/Accepted-'||n::text||'.mkv','Accepted-'||n::text||'.mkv'
		FROM generate_series(1,$4::integer) n`, fixture.library.ID, fixture.scanRoot.id, fixture.scanRoot.path, count); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, count)
	for index := range ids {
		ids[index] = fmt.Sprintf("seen-%06d", index+1)
	}
	stage := scanReconciliationStageFixture(t, fixture, ids)
	scanReconciliationCommitInsertItem(t, fixture, "missing-only", "Missing.mkv", "Movie", fixture.library.ID, false)
	locked, err := fixture.pool.Begin(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(locked)
	if _, err := locked.Exec(fixture.ctx, `SELECT id FROM items WHERE id='seen-000001' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	err = fixture.store.WithOwnedTx(fixture.ctx, func(tx OwnedTx) error {
		if err := stage.RequireSealed(tx); err != nil {
			return err
		}
		page, err := readScanReconciliationPage(tx, fixture.library.ID, "", true, stage)
		if err != nil {
			return err
		}
		if len(page) != 1 || page[0].id != "missing-only" {
			return fmt.Errorf("unseen page retained accepted rows: %d rows", len(page))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("accepted row lock blocked the unseen page: %v", err)
	}
	if err := locked.Rollback(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	capture := scanReconciliationCommitCapture(t, fixture, nil)
	evidence := scanReconciliationCommitEvidence(t, capture)
	if _, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library,
		[]*rootBindingScanCapture{capture}, evidence, nil, stage); err != nil {
		t.Fatal(err)
	}
	scanReconciliationCommitAssertItem(t, fixture, "missing-only", false)
	var survivors int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM items WHERE library_id=$1 AND id LIKE 'seen-%'`, fixture.library.ID).Scan(&survivors); err != nil || survivors != count {
		t.Fatalf("accepted population changed: count=%d error=%v", survivors, err)
	}
	stats := stage.Stats()
	if stats.Rows != count || stats.BufferedIDs != 0 || stats.PeakBufferedIDs > 512 || stats.PeakBufferedBytes > 128<<10 {
		t.Fatalf("accepted-identity buffer was not bounded and sealed: %+v", stats)
	}
}

func TestScanReconciliationStagedSecondPageFailureKeepsFirstPageAtomic(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	const count = scanReconciliationPageItems + 1
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
		SELECT 'missing-'||lpad(n::text,6,'0'),$1,$2,$1,'Missing','missing','Movie',false,
		$3||'/Missing-'||n::text||'.mkv','Missing-'||n::text||'.mkv'
		FROM generate_series(1,$4::integer) n`, fixture.library.ID, fixture.scanRoot.id, fixture.scanRoot.path, count); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET path='/different-root/Missing.mkv' WHERE id=$1`, fmt.Sprintf("missing-%06d", count)); err != nil {
		t.Fatal(err)
	}
	stage := scanReconciliationStageFixture(t, fixture, nil)
	capture := scanReconciliationCommitCapture(t, fixture, nil)
	evidence := scanReconciliationCommitEvidence(t, capture)
	before := scanReconciliationCommitSnapshot(t, fixture)
	notifications := catalogChangesTestListener(t, fixture.store)
	_, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library,
		[]*rootBindingScanCapture{capture}, evidence, nil, stage)
	if !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
		t.Fatalf("invalid second page was accepted: %v", err)
	}
	if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
		t.Fatal("second-page failure committed a partial deletion")
	}
	assertNoCatalogTestNotification(t, notifications)
	if err := fixture.store.CheckOwnership(fixture.ctx); err != nil {
		t.Fatalf("proof rejection lost the healthy owner: %v", err)
	}
}

func TestScanReconciliationStagedEmptyCandidateSetAvoidsFilesystemProof(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	scanReconciliationCommitInsertItem(t, fixture, "accepted-only", "Accepted.mkv", "Movie", fixture.library.ID, false)
	adapter := &rootBindingScanTestCapture{snapshot: fixture.snapshot}
	capture := scanReconciliationCommitCapture(t, fixture, adapter)
	evidence := scanReconciliationCommitEvidence(t, capture)
	stage := scanReconciliationStageFixture(t, fixture, []string{"accepted-only"})
	checks := adapter.checks
	before := scanReconciliationCommitSnapshot(t, fixture)
	if _, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library,
		[]*rootBindingScanCapture{capture}, evidence, nil, stage); err != nil {
		t.Fatal(err)
	}
	if adapter.checks != checks {
		t.Fatal("a zero-deletion pass unnecessarily re-read storage")
	}
	if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
		t.Fatal("zero-deletion pass changed catalog state")
	}
}

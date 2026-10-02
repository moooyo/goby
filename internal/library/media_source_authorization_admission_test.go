package library

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newSourceAuthorizationTestAdmission() *mediaSourceAdmissionQueue {
	return newMediaSourceAdmissionWithLimits(mediaSourceAdmissionLimits{8, 3, 7, 2, 1})
}

func TestSourceAuthorizationUnknownDebtCannotConsumeHealthyRootReserve(t *testing.T) {
	admission := newSourceAuthorizationTestAdmission()
	domainA, domainB := t.TempDir(), t.TempDir()
	for range 7 {
		mediaSourceRootTestAcquire(t, admission, false, "catalog", "root-a", domainA)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	unknown, err := admission.requestRoot(ctx, false, true, mediaSourceRootKey{}, "", mediaSourceScopedQueueLimit)
	if err != nil || unknown.granted {
		t.Fatalf("unknown authority consumed a saturated root's reserved slot: %v", err)
	}
	mediaSourceRootTestAcquire(t, admission, false, "catalog", "root-b", domainB)
	mediaSourceAdmissionTestCounts(t, admission, 8, 0, 1)
	cancel()
	if release, err := admission.wait(unknown); release != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("unknown authority queue did not cancel: %v", err)
	}
}

func TestSourceAuthorizationUnknownDebtCountsAgainstLaterRootOwners(t *testing.T) {
	admission := newSourceAuthorizationTestAdmission()
	unknown, err := admission.requestRoot(context.Background(), false, true, mediaSourceRootKey{}, "", mediaSourceScopedQueueLimit)
	if err != nil || !unknown.granted {
		t.Fatalf("first unknown authority did not enter: %v", err)
	}
	t.Cleanup(func() { admission.release(unknown) })
	domainA, domainB := t.TempDir(), t.TempDir()
	for range 6 {
		mediaSourceRootTestAcquire(t, admission, false, "catalog", "root-a", domainA)
	}
	blocked := mediaSourceRootTestQueue(t, admission, context.Background(), false, "catalog", "root-a", domainA)
	if blocked.granted {
		t.Fatal("a later root ignored its unclassified debt")
	}
	mediaSourceRootTestAcquire(t, admission, false, "catalog", "root-b", domainB)
	mediaSourceAdmissionTestCounts(t, admission, 8, 0, 1)
	admission.release(unknown)
	mediaSourceAdmissionTestWait(t, blocked.ready, "known authority after unknown debt release")
	mediaSourceAdmissionTestCounts(t, admission, 8, 0, 0)
}

func TestSourceAuthorizationUnknownDebtPreservesBackgroundRootReserve(t *testing.T) {
	admission := newSourceAuthorizationTestAdmission()
	unknown, err := admission.requestRoot(context.Background(), true, true, mediaSourceRootKey{}, "", mediaSourceScopedQueueLimit)
	if err != nil || !unknown.granted {
		t.Fatalf("background unknown authority did not enter: %v", err)
	}
	t.Cleanup(func() { admission.release(unknown) })
	domainA, domainB := t.TempDir(), t.TempDir()
	mediaSourceRootTestAcquire(t, admission, true, "catalog", "root-a", domainA)
	blocked := mediaSourceRootTestQueue(t, admission, context.Background(), true, "catalog", "root-a", domainA)
	if blocked.granted {
		t.Fatal("unknown background debt allowed another analysis owner on the same root")
	}
	mediaSourceRootTestAcquire(t, admission, true, "catalog", "root-b", domainB)
	mediaSourceRootTestAcquire(t, admission, false, "catalog", "root-a", domainA)
	mediaSourceAdmissionTestCounts(t, admission, 4, 3, 1)
}

func TestSourceAuthorizationUnknownDebtCoversNestedDomainSiblings(t *testing.T) {
	admission := newSourceAuthorizationTestAdmission()
	unknown, err := admission.requestRoot(context.Background(), false, true, mediaSourceRootKey{}, "", mediaSourceScopedQueueLimit)
	if err != nil || !unknown.granted {
		t.Fatalf("unknown authority did not enter: %v", err)
	}
	t.Cleanup(func() { admission.release(unknown) })
	parent := t.TempDir()
	mediaSourceRootTestAcquire(t, admission, false, "catalog-a", "parent", parent)
	for range 5 {
		mediaSourceRootTestAcquire(t, admission, false, "catalog-b", "child-a", filepath.Join(parent, "a"))
	}
	blocked := mediaSourceRootTestQueue(t, admission, context.Background(), false, "catalog-c", "child-b", filepath.Join(parent, "b"))
	if blocked.granted {
		t.Fatal("a nested sibling ignored shared ancestor and unknown charges")
	}
	mediaSourceRootTestAcquire(t, admission, false, "catalog-d", "independent", t.TempDir())
	mediaSourceAdmissionTestCounts(t, admission, 8, 0, 1)
}

func TestMediaSourceTryAdmissionNeverQueuesAndKeepsEligibleFIFO(t *testing.T) {
	admission := newMediaSourceAdmission()
	domainA, domainB, domainC := t.TempDir(), t.TempDir(), t.TempDir()
	for range mediaSourceRootOwnerLimit {
		mediaSourceRootTestAcquire(t, admission, false, "catalog", "root-a", domainA)
	}
	if release, granted, err := admission.tryAcquireRoot(context.Background(), false, mediaSourceRootKey{catalog: "catalog", id: "root-a"}, domainA); release != nil || granted || err != nil {
		t.Fatalf("a failed transaction try retained ownership: %v", err)
	}
	mediaSourceAdmissionTestCounts(t, admission, 3, 0, 0)
	older := &mediaSourceWaiter{ctx: context.Background(), root: mediaSourceRootKey{catalog: "catalog", id: "root-b"}, domain: domainB, ready: make(chan struct{})}
	admission.mu.Lock()
	admission.waiters = append(admission.waiters, older)
	admission.mu.Unlock()
	t.Cleanup(func() { admission.release(older) })
	if release, granted, err := admission.tryAcquireRoot(context.Background(), false, mediaSourceRootKey{catalog: "catalog", id: "root-c"}, domainC); release != nil || granted || err != nil {
		t.Fatalf("transaction try jumped ahead of an older eligible owner: %v", err)
	}
	mediaSourceAdmissionTestWait(t, older.ready, "oldest eligible owner before transaction try")
	mediaSourceAdmissionTestCounts(t, admission, 4, 0, 0)
}

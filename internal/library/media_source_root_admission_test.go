package library

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func mediaSourceRootTestAcquire(t *testing.T, admission *mediaSourceAdmissionQueue, background bool, catalog, root, domain string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := admission.acquireRoot(ctx, background, mediaSourceRootKey{catalog: catalog, id: root}, domain)
	if err != nil {
		t.Fatalf("root source admission failed: %v", err)
	}
	t.Cleanup(release)
	return release
}

func mediaSourceRootTestQueue(t *testing.T, admission *mediaSourceAdmissionQueue, ctx context.Context, background bool, catalog, root, domain string) *mediaSourceWaiter {
	t.Helper()
	waiter, err := admission.requestRoot(ctx, background, false, mediaSourceRootKey{catalog: catalog, id: root}, domain, mediaSourceScopedQueueLimit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admission.release(waiter) })
	return waiter
}

func TestMediaSourceRootSaturationLeavesAnotherDomainCapacityAndFIFO(t *testing.T) {
	admission := newMediaSourceAdmission()
	domainA, domainB := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	var held []func()
	for range mediaSourceRootOwnerLimit {
		held = append(held, mediaSourceRootTestAcquire(t, admission, false, "catalog", "root-a", domainA))
	}
	old := mediaSourceRootTestQueue(t, admission, context.Background(), false, "catalog", "root-a", domainA)
	newer := mediaSourceRootTestQueue(t, admission, context.Background(), false, "catalog", "root-a", domainA)
	healthy := mediaSourceRootTestAcquire(t, admission, false, "catalog", "root-b", domainB)
	mediaSourceAdmissionTestCounts(t, admission, 4, 0, 2)
	healthy()
	select {
	case <-old.ready:
		t.Fatal("a saturated root received the spare global slot")
	default:
	}
	held[0]()
	mediaSourceAdmissionTestWait(t, old.ready, "oldest eligible root owner")
	select {
	case <-newer.ready:
		t.Fatal("a newer root waiter jumped ahead of its eligible predecessor")
	default:
	}
	admission.release(old)
	mediaSourceAdmissionTestWait(t, newer.ready, "next eligible root owner")
}

func TestMediaSourceRootFullFaultQueueDoesNotRejectHealthyDomain(t *testing.T) {
	admission := newMediaSourceAdmission()
	domainA, domainB := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	for range mediaSourceRootOwnerLimit {
		mediaSourceRootTestAcquire(t, admission, false, "catalog", "root-a", domainA)
	}
	for range mediaSourceScopedQueueLimit {
		mediaSourceRootTestQueue(t, admission, context.Background(), false, "catalog", "root-a", domainA)
	}
	if waiter, err := admission.requestRoot(context.Background(), false, true, mediaSourceRootKey{catalog: "catalog", id: "root-a"}, domainA, mediaSourceScopedQueueLimit); waiter != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("a full root queue admitted another blocked waiter: %v", err)
	}
	mediaSourceRootTestAcquire(t, admission, false, "catalog", "root-b", domainB)
	mediaSourceAdmissionTestCounts(t, admission, 4, 0, mediaSourceScopedQueueLimit)
}

func TestMediaSourceRootAnalysisReserveSurvivesFullBackgroundQueue(t *testing.T) {
	admission := newMediaSourceAdmission()
	domain := t.TempDir()
	for range analysisSourceRootOwnerLimit {
		mediaSourceRootTestAcquire(t, admission, true, "catalog", "root", domain)
	}
	for range mediaSourceScopedQueueLimit {
		mediaSourceRootTestQueue(t, admission, context.Background(), true, "catalog", "root", domain)
	}
	mediaSourceRootTestAcquire(t, admission, false, "catalog", "root", domain)
	mediaSourceAdmissionTestCounts(t, admission, 3, 2, mediaSourceScopedQueueLimit)
}

func TestMediaSourceRootDifferentCatalogsShareNestedConfiguredDomains(t *testing.T) {
	admission := newMediaSourceAdmission()
	parent := t.TempDir()
	child := filepath.Join(parent, "nested")
	other := t.TempDir()
	mediaSourceRootTestAcquire(t, admission, false, "catalog-a", "same-id", parent)
	mediaSourceRootTestAcquire(t, admission, false, "catalog-b", "same-id", child)
	mediaSourceRootTestAcquire(t, admission, false, "catalog-b", "other-id", child)
	blocked := mediaSourceRootTestQueue(t, admission, context.Background(), false, "catalog-c", "third-id", parent)
	select {
	case <-blocked.ready:
		t.Fatal("nested anchors in another catalog created a new domain budget")
	default:
	}
	mediaSourceRootTestAcquire(t, admission, false, "catalog-d", "same-id", other)
	mediaSourceAdmissionTestCounts(t, admission, 4, 0, 1)
}

func TestMediaSourceRootDomainComparisonUsesPathComponents(t *testing.T) {
	admission := newMediaSourceAdmission()
	parent := t.TempDir()
	domain := filepath.Join(parent, "media")
	for range mediaSourceRootOwnerLimit {
		mediaSourceRootTestAcquire(t, admission, false, "catalog-a", "root-a", domain)
	}
	mediaSourceRootTestAcquire(t, admission, false, "catalog-b", "root-b", filepath.Join(parent, "media-other"))
	mediaSourceAdmissionTestCounts(t, admission, 4, 0, 0)
}

func TestMediaSourceRootNestedChildrenRetainTheirSharedAncestorBudget(t *testing.T) {
	for _, background := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreground", true: "background"}[background], func(t *testing.T) {
			admission := newMediaSourceAdmission()
			parent := t.TempDir()
			childA, childB := filepath.Join(parent, "a"), filepath.Join(parent, "b")
			mediaSourceRootTestAcquire(t, admission, background, "catalog-a", "parent", parent)
			mediaSourceRootTestAcquire(t, admission, background, "catalog-b", "child-a", childA)
			if !background {
				mediaSourceRootTestAcquire(t, admission, false, "catalog-c", "child-b", childB)
			}
			blocked := mediaSourceRootTestQueue(t, admission, context.Background(), background, "catalog-d", "incoming", childB)
			select {
			case <-blocked.ready:
				t.Fatal("a nested child ignored its sibling's charge to a live shared ancestor")
			default:
			}
			if background {
				// The parent background reserve rejects another child's analysis
				// even while total capacity remains for foreground work there.
				mediaSourceRootTestAcquire(t, admission, false, "catalog-c", "child-b", childB)
			}
			mediaSourceRootTestAcquire(t, admission, false, "catalog-e", "independent", t.TempDir())
			mediaSourceAdmissionTestCounts(t, admission, 4, map[bool]int{false: 0, true: 2}[background], 1)
		})
	}
}

func TestMediaSourceRootCancellationAndReleaseClearAllLaneCharges(t *testing.T) {
	admission := newMediaSourceAdmission()
	domain := t.TempDir()
	var held []func()
	for range mediaSourceRootOwnerLimit {
		held = append(held, mediaSourceRootTestAcquire(t, admission, false, "catalog", "root", domain))
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiter := mediaSourceRootTestQueue(t, admission, ctx, true, "catalog", "root", domain)
	held[0]()
	mediaSourceAdmissionTestWait(t, waiter.ready, "background root grant")
	cancel()
	if release, err := admission.wait(waiter); release != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled unclaimed root grant survived: %v", err)
	}
	for _, release := range held {
		release()
		release()
	}
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if admission.active != 0 || admission.background != 0 || len(admission.waiters) != 0 || len(admission.roots) != 0 || len(admission.domains) != 0 {
		t.Fatalf("root cancellation retained accounting: active=%d background=%d queued=%d roots=%d domains=%d", admission.active, admission.background, len(admission.waiters), len(admission.roots), len(admission.domains))
	}
}

func TestMediaSourceRootRejectsUnclassifiedProductionAdmission(t *testing.T) {
	admission := newMediaSourceAdmission()
	for _, value := range []struct {
		root   mediaSourceRootKey
		domain string
	}{
		{mediaSourceRootKey{}, t.TempDir()},
		{mediaSourceRootKey{catalog: "catalog"}, t.TempDir()},
		{mediaSourceRootKey{id: "root"}, t.TempDir()},
		{mediaSourceRootKey{catalog: "catalog", id: "root"}, ""},
	} {
		if release, err := admission.acquireRoot(context.Background(), false, value.root, value.domain); release != nil || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("unclassified root received capacity: %v", err)
		}
	}
	mediaSourceAdmissionTestCounts(t, admission, 0, 0, 0)
}

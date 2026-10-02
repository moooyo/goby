package library

import (
	"context"
	"testing"
)

func TestMediaSourceCleanupAdmissionUsesIndependentQueueAndEligiblePriority(t *testing.T) {
	admission := newMediaSourceAdmission()
	domainA, domainB := t.TempDir(), t.TempDir()
	rootA := mediaSourceRootKey{catalog: "catalog", id: "root-a"}
	var held []func()
	for range mediaSourceRootOwnerLimit {
		held = append(held, mediaSourceRootTestAcquire(t, admission, false, rootA.catalog, rootA.id, domainA))
	}
	for range mediaSourceScopedQueueLimit {
		mediaSourceRootTestQueue(t, admission, context.Background(), false, rootA.catalog, rootA.id, domainA)
	}
	cleanup, err := admission.requestRootPolicy(context.Background(), false, true, rootA, domainA, 8, true)
	if err != nil || cleanup.granted {
		t.Fatalf("a full normal queue rejected cleanup or exceeded the root budget: %v", err)
	}
	t.Cleanup(func() { admission.release(cleanup) })
	healthy := mediaSourceRootTestAcquire(t, admission, false, "catalog", "root-b", domainB)
	healthy()
	if cleanup.granted {
		t.Fatal("ineligible cleanup consumed another domain's global slot")
	}
	held[0]()
	mediaSourceAdmissionTestWait(t, cleanup.ready, "cleanup before older ordinary opens")
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if admission.active != 3 || len(admission.waiters) != mediaSourceScopedQueueLimit {
		t.Fatalf("cleanup priority broke actual owner or waiting bounds: owners=%d waiters=%d", admission.active, len(admission.waiters))
	}
}

func TestMediaSourceCleanupAdmissionRejectsNinthRegisteredCleanup(t *testing.T) {
	admission := newMediaSourceAdmission()
	domain := t.TempDir()
	root := mediaSourceRootKey{catalog: "catalog", id: "root"}
	for range mediaSourceRootOwnerLimit {
		mediaSourceRootTestAcquire(t, admission, false, root.catalog, root.id, domain)
	}
	for range 8 {
		waiter, err := admission.requestRootPolicy(context.Background(), false, true, root, domain, 8, true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admission.release(waiter) })
	}
	if waiter, err := admission.requestRootPolicy(context.Background(), false, true, root, domain, 8, true); waiter != nil || err != ErrBusy {
		t.Fatalf("cleanup queue exceeded its independent eight-owner bound: %v", err)
	}
}

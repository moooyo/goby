package library

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type mediaSourceQueueFactTestResult struct {
	release func()
	queued  bool
	err     error
}

func mediaSourceQueueFactTestHold(t *testing.T, admission *mediaSourceAdmissionQueue, root mediaSourceRootKey, domain string) []func() {
	t.Helper()
	var releases []func()
	for range mediaSourceRootOwnerLimit {
		release, err := admission.acquireRoot(context.Background(), false, root, domain)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
		t.Cleanup(release)
	}
	return releases
}

func mediaSourceQueueFactTestWaiter(t *testing.T, admission *mediaSourceAdmissionQueue, root mediaSourceRootKey) *mediaSourceWaiter {
	t.Helper()
	admission.mu.Lock()
	defer admission.mu.Unlock()
	for _, waiter := range admission.waiters {
		if waiter.root == root && waiter.queued {
			return waiter
		}
	}
	t.Fatal("queued callback did not run after registering its own waiter")
	return nil
}

func TestMediaSourceQueueFactMissThenImmediatePreservesFacts(t *testing.T) {
	admission := newMediaSourceAdmission()
	root := mediaSourceRootKey{catalog: "queue-fact", id: "root"}
	domain := t.TempDir()
	held := mediaSourceQueueFactTestHold(t, admission, root, domain)
	if release, granted, err := admission.tryAcquireRoot(context.Background(), false, root, domain); release != nil || granted || err != nil {
		t.Fatalf("saturated pure try did not miss: granted=%t error=%v", granted, err)
	}
	// A real previous owner retires between the try and the fallback request.
	held[0]()
	factsPresent, cleared := true, 0
	release, queued, err := admission.acquireRootWithQueueFact(context.Background(), false, root, domain, func() {
		factsPresent = false
		cleared++
	})
	if err != nil || release == nil || queued || !factsPresent || cleared != 0 {
		t.Fatalf("immediate fallback invalidated fresh facts: queued=%t cleared=%d error=%v", queued, cleared, err)
	}
	mediaSourceAdmissionTestCounts(t, admission, 3, 0, 0)
	release()
	release()
	mediaSourceAdmissionTestCounts(t, admission, 2, 0, 0)
	for _, finish := range held {
		finish()
	}
	mediaSourceAdmissionTestCounts(t, admission, 0, 0, 0)
}

func TestMediaSourceQueueFactReadyBeforeWaitStillInvalidatesFacts(t *testing.T) {
	admission := newMediaSourceAdmission()
	root := mediaSourceRootKey{catalog: "queue-fact", id: "root"}
	domain := t.TempDir()
	held := mediaSourceQueueFactTestHold(t, admission, root, domain)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	factsPresent, cleared := true, 0
	var captured *mediaSourceWaiter
	release, queued, err := admission.acquireRootWithQueueFact(ctx, false, root, domain, func() {
		// Locking and releasing a real owner also prove the callback is outside
		// the governor mutex. It must clear facts before wait sees readiness.
		captured = mediaSourceQueueFactTestWaiter(t, admission, root)
		factsPresent = false
		cleared++
		held[0]()
		select {
		case <-captured.ready:
		default:
			t.Error("the real retired owner did not close ready before wait")
		}
	})
	if err != nil || release == nil || !queued || factsPresent || cleared != 1 || captured == nil || !captured.queued {
		t.Fatalf("ready channel state replaced the immutable queue fact: queued=%t cleared=%d error=%v", queued, cleared, err)
	}
	release()
	for _, finish := range held {
		finish()
	}
	mediaSourceAdmissionTestCounts(t, admission, 0, 0, 0)
}

func TestMediaSourceQueueFactCancellationAfterReadyReclaimsGrant(t *testing.T) {
	admission := newMediaSourceAdmission()
	root := mediaSourceRootKey{catalog: "queue-fact", id: "root"}
	domain := t.TempDir()
	held := mediaSourceQueueFactTestHold(t, admission, root, domain)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleared := 0
	release, queued, err := admission.acquireRootWithQueueFact(ctx, false, root, domain, func() {
		cleared++
		held[0]()
		cancel()
	})
	if release != nil || !queued || !errors.Is(err, context.Canceled) || cleared != 1 {
		t.Fatalf("canceled unclaimed grant hid its queue fact or capacity: queued=%t cleared=%d error=%v", queued, cleared, err)
	}
	mediaSourceAdmissionTestCounts(t, admission, 2, 0, 0)
	for _, finish := range held {
		finish()
	}
	mediaSourceAdmissionTestCounts(t, admission, 0, 0, 0)
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if len(admission.roots) != 0 || len(admission.domains) != 0 {
		t.Fatal("canceled queue-fact acquisition retained root or domain charges")
	}
}

func TestMediaSourceQueueFactRejectedRequestDoesNotInvokeCallback(t *testing.T) {
	admission := newMediaSourceAdmission()
	root := mediaSourceRootKey{catalog: "queue-fact", id: "root"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cleared := 0
	callback := func() { cleared++ }
	if release, queued, err := admission.acquireRootWithQueueFact(ctx, false, root, t.TempDir(), callback); release != nil || queued || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation before enqueue changed the queue fact: queued=%t error=%v", queued, err)
	}
	if release, queued, err := admission.acquireRootWithQueueFact(context.Background(), false, mediaSourceRootKey{}, "", callback); release != nil || queued || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unclassified acquisition bypassed its route guard: queued=%t error=%v", queued, err)
	}
	if cleared != 0 {
		t.Fatal("a rejected request invoked the queued-wait callback")
	}
	mediaSourceAdmissionTestCounts(t, admission, 0, 0, 0)
}

func TestMediaSourceQueueFactCallbackPanicReleasesQueuedOrReadyOwner(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(map[bool]string{false: "queued", true: "ready"}[ready], func(t *testing.T) {
			admission := newMediaSourceAdmission()
			root := mediaSourceRootKey{catalog: "queue-fact", id: "root"}
			domain := t.TempDir()
			held := mediaSourceQueueFactTestHold(t, admission, root, domain)
			marker := errors.New("injected queued fact callback panic")
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				_, _, _ = admission.acquireRootWithQueueFact(context.Background(), false, root, domain, func() {
					if ready {
						held[0]()
					}
					panic(marker)
				})
			}()
			if recovered != marker {
				t.Fatalf("callback panic did not propagate after reclaiming its owner: %v", recovered)
			}
			want := 3
			if ready {
				want = 2
			}
			mediaSourceAdmissionTestCounts(t, admission, want, 0, 0)
			for _, finish := range held {
				finish()
			}
			mediaSourceAdmissionTestCounts(t, admission, 0, 0, 0)
		})
	}
}

func TestMediaSourceQueueFactPreservesCleanupPriorityFIFOAndOtherRoot(t *testing.T) {
	admission := newMediaSourceAdmission()
	root := mediaSourceRootKey{catalog: "queue-fact", id: "root"}
	domain := filepath.Join(t.TempDir(), "blocked")
	held := mediaSourceQueueFactTestHold(t, admission, root, domain)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	older, err := admission.requestRoot(ctx, false, false, root, domain, mediaSourceScopedQueueLimit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admission.release(older) })
	cleanup, err := admission.requestRootPolicy(ctx, false, true, root, domain, 8, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admission.release(cleanup) })
	other := mediaSourceRootKey{catalog: "queue-fact", id: "healthy"}
	var healthyCallback atomic.Int32
	healthy, queued, err := admission.acquireRootWithQueueFact(ctx, false, other, filepath.Join(filepath.Dir(domain), "healthy"), func() { healthyCallback.Add(1) })
	if err != nil || healthy == nil || queued || healthyCallback.Load() != 0 {
		t.Fatalf("an ineligible root queue prevented unrelated capacity: queued=%t error=%v", queued, err)
	}
	healthy()
	entered := make(chan struct{})
	results := make(chan mediaSourceQueueFactTestResult, 1)
	var callbackCount atomic.Int32
	go func() {
		release, wasQueued, acquireErr := admission.acquireRootWithQueueFact(ctx, false, root, domain, func() {
			callbackCount.Add(1)
			held[0]()
			close(entered)
		})
		results <- mediaSourceQueueFactTestResult{release: release, queued: wasQueued, err: acquireErr}
	}()
	mediaSourceAdmissionTestWait(t, entered, "registered fallback behind cleanup and older work")
	select {
	case <-cleanup.ready:
	default:
		t.Fatal("new fallback skipped the eligible cleanup class")
	}
	select {
	case <-older.ready:
		t.Fatal("ordinary work bypassed the cleanup owner")
	default:
	}
	cleanupRelease, err := admission.wait(cleanup)
	if err != nil {
		t.Fatal(err)
	}
	cleanupRelease()
	mediaSourceAdmissionTestWait(t, older.ready, "older eligible ordinary root owner")
	select {
	case <-results:
		t.Fatal("new fallback skipped its older eligible predecessor")
	default:
	}
	olderRelease, err := admission.wait(older)
	if err != nil {
		t.Fatal(err)
	}
	olderRelease()
	var result mediaSourceQueueFactTestResult
	select {
	case result = <-results:
	case <-ctx.Done():
		t.Fatal("fallback did not follow cleanup and its FIFO predecessor")
	}
	if result.err != nil || result.release == nil || !result.queued || callbackCount.Load() != 1 {
		t.Fatalf("fair queue fact changed before delivery: queued=%t error=%v", result.queued, result.err)
	}
	result.release()
	for _, finish := range held {
		finish()
	}
	mediaSourceAdmissionTestCounts(t, admission, 0, 0, 0)
}

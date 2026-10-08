package library

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mediaSourceAdmissionTestWait(t *testing.T, done <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func mediaSourceAdmissionTestAcquire(t *testing.T, admission *mediaSourceAdmissionQueue, background bool) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := admission.acquire(ctx, background)
	if err != nil {
		t.Fatalf("source admission failed: %v", err)
	}
	t.Cleanup(release)
	return release
}

func mediaSourceAdmissionTestCounts(t *testing.T, admission *mediaSourceAdmissionQueue, active, background, queued int) {
	t.Helper()
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if admission.active != active || admission.background != background || len(admission.waiters) != queued {
		t.Fatalf("source owners: active=%d background=%d queued=%d; want %d/%d/%d",
			admission.active, admission.background, len(admission.waiters), active, background, queued)
	}
}

func TestMediaSourceAdmissionPreservesFourForegroundOwners(t *testing.T) {
	admission := newMediaSourceAdmission()
	for range mediaSourceOwnerLimit {
		mediaSourceAdmissionTestAcquire(t, admission, false)
	}
	mediaSourceAdmissionTestCounts(t, admission, 4, 0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiter, err := admission.enqueue(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-waiter.ready:
		t.Fatal("the fifth foreground owner exceeded the process budget")
	default:
	}
	cancel()
	if release, err := admission.wait(waiter); release != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("queued cancellation acquired an owner: %v", err)
	}
	mediaSourceAdmissionTestCounts(t, admission, 4, 0, 0)
}

func TestMediaSourceAdmissionLeavesForegroundCapacityUnderSlowAnalysis(t *testing.T) {
	admission := newMediaSourceAdmission()
	analysisReleases := make([]func(), 0, analysisSourceOwnerLimit)
	for range analysisSourceOwnerLimit {
		analysisReleases = append(analysisReleases, mediaSourceAdmissionTestAcquire(t, admission, true))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	background, err := admission.enqueue(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	foreground := mediaSourceAdmissionTestAcquire(t, admission, false)
	mediaSourceAdmissionTestCounts(t, admission, 4, 3, 1)
	select {
	case <-background.ready:
		t.Fatal("analysis consumed the foreground reserve")
	default:
	}
	foreground()
	mediaSourceAdmissionTestCounts(t, admission, 3, 3, 1)
	analysisReleases[0]()
	mediaSourceAdmissionTestWait(t, background.ready, "eligible analysis admission")
	backgroundRelease, err := admission.wait(background)
	if err != nil {
		t.Fatal(err)
	}
	defer backgroundRelease()
	mediaSourceAdmissionTestCounts(t, admission, 3, 3, 0)
}

func TestMediaSourceAdmissionGivesQueuedAnalysisItsFIFOOpportunity(t *testing.T) {
	admission := newMediaSourceAdmission()
	foregroundReleases := make([]func(), 0, mediaSourceOwnerLimit)
	for range mediaSourceOwnerLimit {
		foregroundReleases = append(foregroundReleases, mediaSourceAdmissionTestAcquire(t, admission, false))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	background, err := admission.enqueue(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	foreground, err := admission.enqueue(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	foregroundReleases[0]()
	mediaSourceAdmissionTestWait(t, background.ready, "oldest eligible analysis admission")
	backgroundRelease, err := admission.wait(background)
	if err != nil {
		t.Fatal(err)
	}
	defer backgroundRelease()
	select {
	case <-foreground.ready:
		t.Fatal("newer foreground work jumped ahead of eligible analysis")
	default:
	}
	mediaSourceAdmissionTestCounts(t, admission, 4, 1, 1)
	cancel()
	if release, err := admission.wait(foreground); release != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("queued foreground cancellation failed: %v", err)
	}
}

func TestMediaSourceAdmissionCancellationReclaimsAnUnclaimedGrant(t *testing.T) {
	admission := newMediaSourceAdmission()
	foregroundReleases := make([]func(), 0, mediaSourceOwnerLimit)
	for range mediaSourceOwnerLimit {
		foregroundReleases = append(foregroundReleases, mediaSourceAdmissionTestAcquire(t, admission, false))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiter, err := admission.enqueue(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	foregroundReleases[0]()
	mediaSourceAdmissionTestWait(t, waiter.ready, "queued analysis grant")
	cancel()
	if release, err := admission.wait(waiter); release != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation retained an unclaimed grant: %v", err)
	}
	admission.release(waiter)
	mediaSourceAdmissionTestCounts(t, admission, 3, 0, 0)
}

func TestMediaSourceAdmissionImmediateGrantUsesNoQueueStorage(t *testing.T) {
	admission := newMediaSourceAdmission()
	var granted []*mediaSourceWaiter
	for index := range mediaSourceOwnerLimit {
		waiter, err := admission.request(context.Background(), index < analysisSourceOwnerLimit, true)
		if err != nil {
			t.Fatal(err)
		}
		granted = append(granted, waiter)
		t.Cleanup(func() { admission.release(waiter) })
		if waiter.ready != nil || !waiter.granted || admission.waiters != nil {
			t.Fatal("uncontended admission allocated a wakeup channel or queue backing")
		}
	}
	mediaSourceAdmissionTestCounts(t, admission, 4, 3, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queued, err := admission.request(ctx, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if queued.ready == nil || queued.granted {
		t.Fatal("immediate admission exceeded the process owner limit")
	}
	cancel()
	if release, err := admission.wait(queued); release != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("queued cancellation retained source capacity: %v", err)
	}
	for _, waiter := range granted {
		admission.release(waiter)
		admission.release(waiter)
	}
	mediaSourceAdmissionTestCounts(t, admission, 0, 0, 0)
}

func TestMediaSourceAdmissionImmediateGrantCancellationReclaimsCapacity(t *testing.T) {
	admission := newMediaSourceAdmission()
	ctx, cancel := context.WithCancel(context.Background())
	waiter, err := admission.request(ctx, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if waiter.ready != nil || !waiter.granted {
		t.Fatal("uncontended analysis did not receive an immediate owner")
	}
	cancel()
	if release, err := admission.wait(waiter); release != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled immediate owner was handed to its caller: %v", err)
	}
	admission.release(waiter)
	mediaSourceAdmissionTestCounts(t, admission, 0, 0, 0)
	if _, err := admission.request(ctx, false, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled admission created a source request: %v", err)
	}
	mediaSourceAdmissionTestCounts(t, admission, 0, 0, 0)
}

func TestMediaSourceAdmissionImmediateGrantCannotSkipQueuedEligibleWork(t *testing.T) {
	admission := newMediaSourceAdmission()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	older := &mediaSourceWaiter{ctx: ctx, background: true, ready: make(chan struct{})}
	// Prepare the scheduler state immediately before its next dispatch. A new
	// arrival must select the queued owner before using its immediate path.
	admission.mu.Lock()
	admission.active = mediaSourceOwnerLimit - 1
	admission.waiters = append(admission.waiters, older)
	admission.mu.Unlock()
	newer, err := admission.request(ctx, false, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admission.release(older); admission.release(newer) })
	mediaSourceAdmissionTestWait(t, older.ready, "older eligible source owner")
	if newer.ready == nil || newer.granted {
		t.Fatal("immediate admission bypassed an older eligible owner")
	}
	mediaSourceAdmissionTestCounts(t, admission, 4, 1, 1)
	cancel()
	if release, err := admission.wait(newer); release != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("newer queued source request did not cancel: %v", err)
	}
	admission.release(older)
	mediaSourceAdmissionTestCounts(t, admission, 3, 0, 0)
}

func TestMediaSourceAdmissionRetainsOnlyClearedBoundedQueueStorage(t *testing.T) {
	admission := newMediaSourceAdmission()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	admission.mu.Lock()
	admission.active = mediaSourceOwnerLimit
	admission.waiters = make([]*mediaSourceWaiter, 0, 2)
	admission.mu.Unlock()
	first, err := admission.enqueue(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := admission.enqueue(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	admission.release(first)
	admission.release(second)
	admission.mu.Lock()
	backing := admission.waiters[:cap(admission.waiters)]
	if len(backing) != 2 || len(admission.waiters) != 0 {
		admission.mu.Unlock()
		t.Fatal("small empty queue storage was not retained")
	}
	for _, waiter := range backing {
		if waiter != nil {
			admission.mu.Unlock()
			t.Fatal("retained empty queue kept a caller context alive")
		}
	}
	admission.mu.Unlock()
	third, err := admission.enqueue(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	admission.mu.Lock()
	if &admission.waiters[:cap(admission.waiters)][0] != &backing[0] {
		admission.mu.Unlock()
		t.Fatal("the next queued request did not reuse bounded storage")
	}
	admission.mu.Unlock()
	admission.release(third)
	admission.mu.Lock()
	admission.waiters = make([]*mediaSourceWaiter, 0, mediaSourceQueueRetainedCapacity+1)
	largeBacking := admission.waiters[:cap(admission.waiters)]
	admission.mu.Unlock()
	fourth, err := admission.enqueue(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	admission.release(fourth)
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if admission.waiters != nil {
		t.Fatal("an empty burst queue retained storage above its bound")
	}
	for _, waiter := range largeBacking {
		if waiter != nil {
			t.Fatal("a retired burst queue retained a caller reference")
		}
	}
}

type mediaSourceAdmissionTestErrObserver struct {
	context.Context
	errors atomic.Int32
}

func (ctx *mediaSourceAdmissionTestErrObserver) Err() error {
	ctx.errors.Add(1)
	return ctx.Context.Err()
}

func TestMediaSourceAdmissionFullBudgetDoesNotRescanWaitingContexts(t *testing.T) {
	admission := newMediaSourceAdmission()
	for range mediaSourceOwnerLimit {
		mediaSourceAdmissionTestAcquire(t, admission, false)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observer := &mediaSourceAdmissionTestErrObserver{Context: ctx}
	first, err := admission.enqueue(observer, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admission.release(first) })
	before := observer.errors.Load()
	second, err := admission.enqueue(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admission.release(second) })
	if observer.errors.Load() != before {
		t.Fatal("a full source budget rescanned a waiting caller context")
	}
	mediaSourceAdmissionTestCounts(t, admission, 4, 0, 2)
}

func mediaSourceAdmissionTestStore() *Store {
	ctx, cancel := context.WithCancel(context.Background())
	return &Store{ctx: ctx, cancel: cancel, queue: make(chan *scanTask), done: make(chan struct{})}
}

type mediaSourceAdmissionTestQueuedContext struct {
	context.Context
	doneCalls     atomic.Int32
	queueDoneCall int32
	queued        chan struct{}
}

func (ctx *mediaSourceAdmissionTestQueuedContext) Done() <-chan struct{} {
	// Observe the selected admission wait without delaying its cancellation.
	if ctx.doneCalls.Add(1) == ctx.queueDoneCall {
		close(ctx.queued)
	}
	return ctx.Context.Done()
}

func TestMediaSourceOwnersAreSharedAcrossStoresAndLegacyWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, releaseWork := make(chan struct{}, 8), make(chan struct{})
	results := make(chan error, 8)
	stores := make([]*Store, 8)
	var legacyDone chan struct{}
	var localSlots chan struct{}
	var releaseOnce sync.Once
	var startedCount atomic.Int32
	t.Cleanup(func() {
		cancel()
		for _, store := range stores {
			if store != nil {
				store.beginCloseMediaSources()
			}
		}
		releaseOnce.Do(func() { close(releaseWork) })
		if legacyDone != nil {
			mediaSourceAdmissionTestWait(t, legacyDone, "legacy source request cleanup")
			select {
			case localSlots <- struct{}{}:
				<-localSlots
			case <-time.After(5 * time.Second):
				t.Error("legacy source cleanup retained its local slot")
			}
		}
		for _, store := range stores {
			if store == nil {
				continue
			}
			done := make(chan struct{})
			go func() { store.mediaSourceOwners.owners.Wait(); close(done) }()
			mediaSourceAdmissionTestWait(t, done, "actual source owner cleanup")
			store.cancel()
		}
	})
	for index := range stores {
		store := mediaSourceAdmissionTestStore()
		stores[index] = store
		go func() {
			_, _, err := store.runMediaSourceWorker(ctx, func(context.Context) (*os.File, MediaFile, error) {
				startedCount.Add(1)
				started <- struct{}{}
				<-releaseWork
				return nil, MediaFile{}, nil
			})
			results <- err
		}()
	}
	for range mediaSourceOwnerLimit {
		mediaSourceAdmissionTestWait(t, started, "four process-wide foreground owners")
	}
	mediaSourceAdmission.mu.Lock()
	active := mediaSourceAdmission.active
	mediaSourceAdmission.mu.Unlock()
	if active != 4 {
		t.Fatalf("separate Stores did not share the process owner budget: %d", active)
	}
	legacyCtx, cancelLegacy := context.WithCancel(context.Background())
	defer cancelLegacy()
	queuedLegacy := &mediaSourceAdmissionTestQueuedContext{Context: legacyCtx, queueDoneCall: 2, queued: make(chan struct{})}
	localSlots = make(chan struct{}, 1)
	legacyDone = make(chan struct{})
	legacyStarted := make(chan struct{}, 1)
	legacyResult := make(chan error, 1)
	go func() {
		defer close(legacyDone)
		_, _, err := runMediaSourceWorker(queuedLegacy, localSlots, func() (*os.File, MediaFile, error) {
			legacyStarted <- struct{}{}
			return nil, MediaFile{}, nil
		})
		legacyResult <- err
	}()
	select {
	case <-queuedLegacy.queued:
	case <-legacyStarted:
		t.Fatal("legacy worker bypassed the process owner budget")
	case <-time.After(5 * time.Second):
		t.Fatal("legacy worker did not enter the shared source queue")
	}
	mediaSourceAdmission.mu.Lock()
	legacyActive := mediaSourceAdmission.active
	mediaSourceAdmission.mu.Unlock()
	if legacyActive != mediaSourceOwnerLimit {
		t.Fatalf("legacy admission exceeded the shared owner budget: %d", legacyActive)
	}
	cancelLegacy()
	select {
	case err := <-legacyResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("legacy worker did not honor cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("legacy source worker remained queued after cancellation")
	}
	select {
	case <-legacyStarted:
		t.Fatal("legacy worker bypassed the process owner budget")
	default:
	}
	if len(localSlots) != 0 {
		t.Fatal("canceled legacy admission retained its local slot")
	}
	cancel()
	for range stores {
		select {
		case err := <-results:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled source request returned %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("source request did not return while its actual owner remained blocked")
		}
	}
	if startedCount.Load() != 4 {
		t.Fatalf("more than four foreground workers started across Stores: %d", startedCount.Load())
	}
	mediaSourceAdmissionTestCounts(t, mediaSourceAdmission, 4, 0, 0)
	releaseOnce.Do(func() { close(releaseWork) })
}

func TestMediaSourceStoreForegroundEntersWhileThreeAnalysisOwnersAreBlocked(t *testing.T) {
	backgroundStore := mediaSourceAdmissionTestStore()
	foregroundStore := mediaSourceAdmissionTestStore()
	startedBackground := make(chan struct{}, analysisSourceOwnerLimit)
	startedForeground := make(chan struct{})
	releaseWork := make(chan struct{})
	results := make(chan error, mediaSourceOwnerLimit)
	var releaseOnce sync.Once
	t.Cleanup(func() {
		backgroundStore.beginCloseMediaSources()
		foregroundStore.beginCloseMediaSources()
		releaseOnce.Do(func() { close(releaseWork) })
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := backgroundStore.Close(ctx); err != nil {
			t.Errorf("background source cleanup failed: %v", err)
		}
		if err := foregroundStore.Close(ctx); err != nil {
			t.Errorf("foreground source cleanup failed: %v", err)
		}
	})
	for range analysisSourceOwnerLimit {
		go func() {
			_, _, err := backgroundStore.runAnalysisSourceWorker(context.Background(), func(context.Context) (*os.File, MediaFile, error) {
				startedBackground <- struct{}{}
				<-releaseWork
				return nil, MediaFile{}, nil
			})
			results <- err
		}()
	}
	for range analysisSourceOwnerLimit {
		mediaSourceAdmissionTestWait(t, startedBackground, "three blocked Store analysis owners")
	}
	go func() {
		_, _, err := foregroundStore.runMediaSourceWorker(context.Background(), func(context.Context) (*os.File, MediaFile, error) {
			close(startedForeground)
			<-releaseWork
			return nil, MediaFile{}, nil
		})
		results <- err
	}()
	mediaSourceAdmissionTestWait(t, startedForeground, "foreground work beside blocked analysis")
	mediaSourceAdmissionTestCounts(t, mediaSourceAdmission, 4, 3, 0)
	releaseOnce.Do(func() { close(releaseWork) })
	for range mediaSourceOwnerLimit {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("admitted source work failed: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("admitted source work did not complete")
		}
	}
}

func TestMediaSourceStoreCloseDeadlineKeepsActualCleanupOwned(t *testing.T) {
	store := mediaSourceAdmissionTestStore()
	file, err := os.CreateTemp(t.TempDir(), "source-close-*.media")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	started, canceled, releaseWork := make(chan struct{}), make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseWork) })
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Errorf("source cleanup did not finish: %v", err)
		}
	})
	go func() {
		_, _, err := store.runMediaSourceWorker(context.Background(), func(ctx context.Context) (*os.File, MediaFile, error) {
			close(started)
			<-ctx.Done()
			close(canceled)
			<-releaseWork
			return file, MediaFile{}, nil
		})
		result <- err
	}()
	mediaSourceAdmissionTestWait(t, started, "Store source operation")
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	if err := store.Close(expired); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Store Close ignored its deadline: %v", err)
	}
	mediaSourceAdmissionTestWait(t, canceled, "source work to observe Store shutdown")
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("source request did not observe shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Store shutdown did not cancel its HTTP source request")
	}
	select {
	case <-store.done:
		t.Fatal("Store released resources before the actual source operation finished")
	default:
	}
	if store.catalogChangesClosed.Load() {
		t.Fatal("Store released catalog resources while source I/O was still blocked")
	}
	if _, err := file.Stat(); err != nil {
		t.Fatalf("the actual worker lost its descriptor before cleanup: %v", err)
	}
	var lateWork atomic.Bool
	if _, _, err := store.runMediaSourceWorker(context.Background(), func(context.Context) (*os.File, MediaFile, error) {
		lateWork.Store(true)
		return nil, MediaFile{}, nil
	}); !errors.Is(err, ErrUnavailable) || lateWork.Load() {
		t.Fatalf("closed Store admitted another source operation: %v", err)
	}
	releaseOnce.Do(func() { close(releaseWork) })
	mediaSourceAdmissionTestWait(t, store.done, "Store cleanup after its caller deadline")
	if err := file.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("late source descriptor outlived Store cleanup: %v", err)
	}
}

func TestMediaSourceStoreDrainDoesNotOwnDeliveredFileLifetime(t *testing.T) {
	store := mediaSourceAdmissionTestStore()
	file, err := os.CreateTemp(t.TempDir(), "delivered-source-*.media")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	opened, _, err := store.runMediaSourceWorker(context.Background(), func(context.Context) (*os.File, MediaFile, error) {
		return file, MediaFile{}, nil
	})
	if err != nil || opened != file {
		t.Fatalf("source descriptor was not delivered: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.Close(ctx); err != nil {
		t.Fatalf("delivered descriptor prevented Store drain: %v", err)
	}
	if _, err := opened.Stat(); err != nil {
		t.Fatalf("Store cleanup closed a caller-owned descriptor: %v", err)
	}
	if _, _, err := store.runMediaSourceWorker(context.Background(), func(context.Context) (*os.File, MediaFile, error) {
		t.Fatal("closed Store initialized another source lifetime")
		return nil, MediaFile{}, nil
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed Store accepted source work: %v", err)
	}
}

func TestMediaSourceStoreCloseBeforeLazyInitializationRejectsWork(t *testing.T) {
	store := mediaSourceAdmissionTestStore()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.runMediaSourceWorker(context.Background(), func(context.Context) (*os.File, MediaFile, error) {
		t.Fatal("closed Store started its first source operation")
		return nil, MediaFile{}, nil
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed Store initialized a source runtime: %v", err)
	}
	store.mediaSourceOwners.mu.Lock()
	defer store.mediaSourceOwners.mu.Unlock()
	if store.mediaSourceOwners.ctx != nil {
		t.Fatal("closed Store allocated a new source lifetime")
	}
}

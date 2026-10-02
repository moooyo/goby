//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mediaSourceRootAdmissionTestLane(t *testing.T, fixture mediaSourceFixture, itemID string) (mediaSourceRootHint, mediaSourceRootKey, string) {
	t.Helper()
	_, finish, err := fixture.store.beginMediaSourceLifetime(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	finish()
	hint, err := fixture.store.readMediaSourceRootHint(fixture.ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	root, domain, err := fixture.store.mediaSourceRootLane(hint)
	if err != nil {
		t.Fatal(err)
	}
	return hint, root, domain
}

func mediaSourceRootAdmissionTestHold(t *testing.T, ctx context.Context, root mediaSourceRootKey, domain string, count int) []func() {
	t.Helper()
	releases := make([]func(), 0, count)
	for range count {
		release, err := mediaSourceAdmission.acquireRoot(ctx, false, root, domain)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
		t.Cleanup(release)
	}
	return releases
}

func mediaSourceRootAdmissionTestQueue(t *testing.T, ctx context.Context, admission *mediaSourceAdmissionQueue, root mediaSourceRootKey, count int) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		admission.mu.Lock()
		queued := 0
		for _, waiter := range admission.waiters {
			if waiter.root == root {
				queued++
			}
		}
		admission.mu.Unlock()
		if queued >= count {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("source request did not queue: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func mediaSourceRootAdmissionTestReceive(t *testing.T, ctx context.Context, results <-chan mediaSourceWorkerTestResult) mediaSourceWorkerTestResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-ctx.Done():
		t.Fatalf("routed source caller did not finish: %v", ctx.Err())
		return mediaSourceWorkerTestResult{}
	}
}

func TestMediaSourceRootAdmissionRejectsQueuedMappingChangesBeforeIO(t *testing.T) {
	for _, change := range []string{"revision", "mapping"} {
		t.Run(change, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			_, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			releases := mediaSourceRootAdmissionTestHold(t, fixture.ctx, root, domain, mediaSourceRootOwnerLimit)
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			results := make(chan mediaSourceWorkerTestResult, 1)
			go func() {
				file, source, err := fixture.store.OpenMedia(ctx, fixture.userID, fixture.item.ID, "")
				results <- mediaSourceWorkerTestResult{file: file, source: source, err: err}
			}()
			mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, root, 1)
			if change == "revision" {
				if _, err := fixture.pool.Exec(ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", root.id); err != nil {
					t.Fatal(err)
				}
			} else {
				tx, err := fixture.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(context.Background())
				if _, err := tx.Exec(ctx, `UPDATE library_roots SET path=path || '/Replacement',
					relative_path=relative_path || '/Replacement', binding_revision=binding_revision+1 WHERE id=$1`, root.id); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, "UPDATE items SET path=$2 WHERE id=$1", fixture.item.ID,
					filepath.Join(filepath.Dir(filepath.Dir(fixture.path)), "Replacement", "Nested", filepath.Base(fixture.path))); err != nil {
					t.Fatal(err)
				}
				if err := tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}
			// A missing file would produce only a filesystem availability error.
			// The queued-source error proves the routing check happened first.
			if err := os.Remove(fixture.path); err != nil {
				t.Fatal(err)
			}
			for _, release := range releases {
				release()
			}
			result := mediaSourceRootAdmissionTestReceive(t, ctx, results)
			if result.file != nil {
				_ = result.file.Close()
			}
			if result.file != nil || !errors.Is(result.err, ErrUnavailable) || !errors.Is(result.err, ErrSourceChanged) ||
				!strings.Contains(result.err.Error(), "while queued") {
				t.Fatalf("queued root change reached storage or reused its hint: file=%v error=%v", result.file, result.err)
			}
		})
	}
}

func TestMediaSourceRootAdmissionRechecksPolicyAfterQueue(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	_, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	releases := mediaSourceRootAdmissionTestHold(t, fixture.ctx, root, domain, mediaSourceRootOwnerLimit)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	results := make(chan mediaSourceWorkerTestResult, 1)
	go func() {
		file, source, err := fixture.store.OpenMedia(ctx, fixture.userID, fixture.item.ID, "")
		results <- mediaSourceWorkerTestResult{file: file, source: source, err: err}
	}()
	mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, root, 1)
	if _, err := fixture.pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":true,"EnableMediaPlayback":false}' WHERE id=$1`, fixture.userID); err != nil {
		t.Fatal("the root queue retained authorization locks or policy revocation failed")
	}
	if err := os.Remove(fixture.path); err != nil {
		t.Fatal(err)
	}
	for _, release := range releases {
		release()
	}
	result := mediaSourceRootAdmissionTestReceive(t, ctx, results)
	if result.file != nil {
		_ = result.file.Close()
	}
	if result.file != nil || !errors.Is(result.err, ErrForbidden) {
		t.Fatalf("queued source reused authority or opened unavailable storage: %v", result.err)
	}
	file, _, err := fixture.store.OpenMedia(ctx, fixture.userID, "missing-routed-item", "")
	if file != nil {
		_ = file.Close()
	}
	if file != nil || !errors.Is(err, ErrForbidden) {
		t.Fatalf("routing failure exposed item existence before current policy: %v", err)
	}
}

func TestMediaSourceRootAdmissionHealthyDomainProgressesBesideBlockedRoot(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	_, blockedRoot, blockedDomain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	healthyAllowed := t.TempDir()
	fixture.store.mu.Lock()
	fixture.store.roots = append(fixture.store.roots, approvedRoot{path: healthyAllowed})
	fixture.store.mu.Unlock()
	healthyPath := libraryIntegrationFile(t, healthyAllowed, "healthy/Feature.mkv", fixture.contents)
	healthyLibrary := libraryIntegrationCreate(t, fixture.ctx, fixture.store, "Healthy", "movies", filepath.Dir(healthyPath))
	libraryIntegrationScan(t, fixture.ctx, fixture.store, healthyLibrary.ID, "Completed")
	items := libraryIntegrationQuery(t, fixture.ctx, fixture.store, Query{UserID: fixture.userID, ParentID: healthyLibrary.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
	healthyItem := libraryIntegrationItemByPath(t, items.Items, healthyPath)
	_, healthyRoot, healthyDomain := mediaSourceRootAdmissionTestLane(t, fixture, healthyItem.ID)
	if blockedDomain == healthyDomain || blockedRoot == healthyRoot {
		t.Fatal("independent configured anchors were classified into one test lane")
	}
	releases := mediaSourceRootAdmissionTestHold(t, fixture.ctx, blockedRoot, blockedDomain, mediaSourceRootOwnerLimit)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	results := make(chan mediaSourceWorkerTestResult, 1)
	go func() {
		file, source, err := fixture.store.OpenMedia(ctx, fixture.userID, fixture.item.ID, "")
		results <- mediaSourceWorkerTestResult{file: file, source: source, err: err}
	}()
	mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, blockedRoot, 1)
	file, source, err := fixture.store.OpenMedia(ctx, fixture.userID, healthyItem.ID, "")
	if err != nil || file == nil || source.Item.ID != healthyItem.ID {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("a blocked root prevented preparation or opening on another configured domain: %v", err)
	}
	_ = file.Close()
	for _, release := range releases {
		release()
	}
	result := mediaSourceRootAdmissionTestReceive(t, ctx, results)
	if result.file != nil {
		_ = result.file.Close()
	}
	if result.err != nil || result.file == nil {
		t.Fatalf("blocked root did not resume after its owners released: %v", result.err)
	}
}

func TestMediaSourceRootAdmissionCloseTracksBlockedPreparationAndQueuedCancellation(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	hint, _, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	started, canceled := make(chan struct{}, mediaSourceOwnerLimit), make(chan struct{}, mediaSourceOwnerLimit)
	releasePreparation := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releasePreparation) }) })
	results := make(chan mediaSourceWorkerTestResult, mediaSourceOwnerLimit+1)
	var workStarted atomic.Bool
	prepare := func(ctx context.Context) (mediaSourceRootHint, error) {
		started <- struct{}{}
		<-ctx.Done()
		canceled <- struct{}{}
		<-releasePreparation
		return hint, ctx.Err()
	}
	work := func(context.Context) (*os.File, MediaFile, error) {
		workStarted.Store(true)
		return nil, MediaFile{}, nil
	}
	for range mediaSourceOwnerLimit {
		go func() {
			file, source, err := fixture.store.runPreparedMediaSourceWorker(ctx, false, prepare, work)
			results <- mediaSourceWorkerTestResult{file: file, source: source, err: err}
		}()
	}
	for range mediaSourceOwnerLimit {
		mediaSourceAdmissionTestWait(t, started, "blocked database preparation")
	}
	go func() {
		file, source, err := fixture.store.runPreparedMediaSourceWorker(ctx, false, prepare, work)
		results <- mediaSourceWorkerTestResult{file: file, source: source, err: err}
	}()
	mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourcePreparationAdmission, mediaSourceRootKey{}, 1)
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	if err := fixture.store.Close(expired); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close did not observe its deadline during preparation: %v", err)
	}
	for range mediaSourceOwnerLimit {
		mediaSourceAdmissionTestWait(t, canceled, "preparation shutdown cancellation")
	}
	queued := mediaSourceRootAdmissionTestReceive(t, ctx, results)
	if queued.file != nil || !errors.Is(queued.err, context.Canceled) {
		t.Fatalf("queued preparation did not relinquish its registered lifetime: %v", queued.err)
	}
	select {
	case <-fixture.store.done:
		t.Fatal("Close finished while actual preparation was still blocked")
	default:
	}
	if fixture.store.catalogChangesClosed.Load() || workStarted.Load() {
		t.Fatal("blocked preparation released catalog resources or entered filesystem work")
	}
	releaseOnce.Do(func() { close(releasePreparation) })
	for range mediaSourceOwnerLimit {
		result := mediaSourceRootAdmissionTestReceive(t, ctx, results)
		if result.file != nil || !errors.Is(result.err, context.Canceled) {
			t.Fatalf("blocked preparation ignored shutdown: %v", result.err)
		}
	}
	mediaSourceAdmissionTestWait(t, fixture.store.done, "preparation owner drain")
}

func TestMediaSourceRootAdmissionCancellationRetainsActualOwnerAndCleansFileBeforeDrain(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	hint, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	mediaSourceRootAdmissionTestHold(t, fixture.ctx, root, domain, mediaSourceRootOwnerLimit-1)
	file, err := os.Open(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	caller, cancelCaller := context.WithCancel(ctx)
	defer cancelCaller()
	started, canceled, releaseWork := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseWork) }) })
	results := make(chan mediaSourceWorkerTestResult, 1)
	go func() {
		opened, source, err := fixture.store.runPreparedMediaSourceWorker(caller, false,
			func(context.Context) (mediaSourceRootHint, error) { return hint, nil },
			func(ctx context.Context) (*os.File, MediaFile, error) {
				close(started)
				<-ctx.Done()
				close(canceled)
				<-releaseWork
				return file, MediaFile{}, nil
			})
		results <- mediaSourceWorkerTestResult{file: opened, source: source, err: err}
	}()
	mediaSourceAdmissionTestWait(t, started, "actual routed filesystem owner")
	cancelCaller()
	mediaSourceAdmissionTestWait(t, canceled, "actual worker cancellation")
	result := mediaSourceRootAdmissionTestReceive(t, ctx, results)
	if result.file != nil || !errors.Is(result.err, context.Canceled) {
		t.Fatalf("canceled source caller retained the worker's descriptor: %v", result.err)
	}
	queuedResults := make(chan mediaSourceWorkerTestResult, 1)
	var queuedWork atomic.Bool
	go func() {
		opened, source, err := fixture.store.runPreparedMediaSourceWorker(ctx, false,
			func(context.Context) (mediaSourceRootHint, error) { return hint, nil },
			func(context.Context) (*os.File, MediaFile, error) {
				queuedWork.Store(true)
				return nil, MediaFile{}, nil
			})
		queuedResults <- mediaSourceWorkerTestResult{file: opened, source: source, err: err}
	}()
	mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, root, 1)
	mediaSourceAdmission.mu.Lock()
	active := mediaSourceAdmission.roots[root].active
	mediaSourceAdmission.mu.Unlock()
	if active != mediaSourceRootOwnerLimit || queuedWork.Load() {
		t.Fatalf("HTTP cancellation prematurely released a blocked root owner: active=%d", active)
	}
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	if err := fixture.store.Close(expired); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close did not preserve its caller deadline: %v", err)
	}
	queued := mediaSourceRootAdmissionTestReceive(t, ctx, queuedResults)
	if queued.file != nil || !errors.Is(queued.err, context.Canceled) || queuedWork.Load() {
		t.Fatalf("shutdown queue cancellation entered filesystem work: %v", queued.err)
	}
	select {
	case <-fixture.store.done:
		t.Fatal("Close completed before the actual filesystem owner returned")
	default:
	}
	if _, err := file.Stat(); err != nil {
		t.Fatalf("blocked worker lost its descriptor before its actual return: %v", err)
	}
	releaseOnce.Do(func() { close(releaseWork) })
	mediaSourceAdmissionTestWait(t, fixture.store.done, "actual source owner and descriptor cleanup")
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("undelivered source descriptor outlived Store drain: %v", err)
	}
}

func TestMediaSourceRootAdmissionDrainPreservesDeliveredFile(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	file, source, err := fixture.store.OpenMedia(fixture.ctx, fixture.userID, fixture.item.ID, "")
	if err != nil || file == nil || source.Item.ID != fixture.item.ID {
		t.Fatalf("routed source descriptor was not delivered: %v", err)
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fixture.store.Close(ctx); err != nil {
		t.Fatalf("caller-owned descriptor prevented Store drain: %v", err)
	}
	contents, err := io.ReadAll(file)
	if err != nil || string(contents) != fixture.contents {
		t.Fatalf("Store drain closed or changed a delivered source descriptor: %v", err)
	}
}

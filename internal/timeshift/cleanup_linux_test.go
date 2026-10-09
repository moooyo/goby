package timeshift

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

func blockCleanup(t *testing.T, store *Store, artifactID string) (<-chan struct{}, func()) {
	t.Helper()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finish)
	remove := store.removeArtifact
	store.removeArtifact = func(window *storageWindow, id string) error {
		if id == artifactID {
			close(started)
			<-release
		}
		return remove(window, id)
	}
	return started, finish
}

func awaitCleanupSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("controlled cleanup did not make progress")
	}
}

func awaitCleanupResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not complete after cleanup")
		return nil
	}
}

func TestSlowExpirationAllowsOtherWindowSnapshotAndOpen(t *testing.T) {
	store, advance := testStore(t, nil)
	scope := testScope()
	firstID := testWindow(t, store, scope, time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
	secondID := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
	first := testPublish(t, store, scope, firstID, 1, 1, nil, testInput(t, "video", "expired"))
	second := testPublish(t, store, scope, secondID, 1, 1, nil, testInput(t, "video", "available"))
	started, release := blockCleanup(t, store, first.Segments[0].Artifacts[0].ID)
	advance(2 * time.Second)
	expired := make(chan error, 1)
	go func() { _, err := store.Snapshot(context.Background(), scope, firstID); expired <- err }()
	awaitCleanupSignal(t, started)
	progress := make(chan error, 1)
	go func() {
		if _, err := store.Snapshot(context.Background(), scope, secondID); err != nil {
			progress <- err
			return
		}
		handle, err := store.OpenArtifact(context.Background(), scope, secondID, second.Segments[0].Artifacts[0].ID)
		if err == nil {
			data, readErr := io.ReadAll(handle)
			err = errors.Join(readErr, handle.Close())
			if string(data) != "available" {
				err = errors.Join(err, errors.New("unrelated artifact changed"))
			}
		}
		progress <- err
	}()
	if err := awaitCleanupResult(t, progress); err != nil {
		t.Fatal(err)
	}
	if usage := store.Usage(); usage.Bytes != 2*store.storage.unit || usage.Artifacts != 2 {
		t.Fatalf("blocked unlink released its allocation: %+v", usage)
	}
	select {
	case err := <-expired:
		t.Fatalf("expiration returned before actual cleanup: %v", err)
	default:
	}
	release()
	if err := awaitCleanupResult(t, expired); err != nil {
		t.Fatal(err)
	}
	if usage := store.Usage(); usage.Bytes != store.storage.unit || usage.Artifacts != 1 {
		t.Fatalf("completed expiration retained its charge: %+v", usage)
	}
}

func TestLastExpiredReaderCloseAndDoneWaitForRetirement(t *testing.T) {
	store, advance := testStore(t, nil)
	scope := testScope()
	id := testWindow(t, store, scope, time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
	first := testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "pinned"))
	artifactID := first.Segments[0].Artifacts[0].ID
	handle, err := store.OpenArtifact(context.Background(), scope, id, artifactID)
	if err != nil {
		t.Fatal(err)
	}
	advance(2 * time.Second)
	if _, err := store.Snapshot(context.Background(), scope, id); err != nil {
		t.Fatal(err)
	}
	started, release := blockCleanup(t, store, artifactID)
	closed, duplicate := make(chan error, 1), make(chan error, 1)
	go func() { closed <- handle.Close() }()
	awaitCleanupSignal(t, started)
	go func() { duplicate <- handle.Close() }()
	select {
	case <-handle.Done():
		t.Fatal("reader Done preceded physical retirement")
	default:
	}
	if usage := store.Usage(); usage.Readers != 0 || usage.Bytes != store.storage.unit || usage.Artifacts != 1 {
		t.Fatalf("closed descriptor lost the pending deletion charge: %+v", usage)
	}
	release()
	for _, result := range []<-chan error{closed, duplicate} {
		if err := awaitCleanupResult(t, result); err != nil {
			t.Fatal(err)
		}
	}
	awaitCleanupSignal(t, handle.Done())
	if usage := store.Usage(); usage.Bytes != 0 || usage.Artifacts != 0 {
		t.Fatalf("reader completion did not observe retired storage: %+v", usage)
	}
}

func TestExpiredReaderCompletionDoesNotJoinLaterArtifactCleanup(t *testing.T) {
	store, advance := testStore(t, nil)
	scope := testScope()
	id := testWindow(t, store, scope, 4*time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
	first := testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "first pinned artifact"))
	second := testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "second pinned artifact"))
	firstID, secondID := first.Segments[0].Artifacts[0].ID, second.Segments[1].Artifacts[0].ID
	firstHandle, err := store.OpenArtifact(context.Background(), scope, id, firstID)
	if err != nil {
		t.Fatal(err)
	}
	secondHandle, err := store.OpenArtifact(context.Background(), scope, id, secondID)
	if err != nil {
		t.Fatal(err)
	}
	advance(5 * time.Second)
	if _, err := store.Snapshot(context.Background(), scope, id); err != nil {
		t.Fatal(err)
	}
	firstStarted, secondStarted := make(chan struct{}), make(chan struct{})
	firstRelease, secondRelease := make(chan struct{}), make(chan struct{})
	var firstOnce, secondOnce sync.Once
	releaseFirst := func() { firstOnce.Do(func() { close(firstRelease) }) }
	releaseSecond := func() { secondOnce.Do(func() { close(secondRelease) }) }
	t.Cleanup(releaseFirst)
	t.Cleanup(releaseSecond)
	remove := store.removeArtifact
	store.removeArtifact = func(window *storageWindow, artifactID string) error {
		switch artifactID {
		case firstID:
			close(firstStarted)
			<-firstRelease
		case secondID:
			close(secondStarted)
			<-secondRelease
		}
		return remove(window, artifactID)
	}
	firstClosed, secondClosed := make(chan error, 1), make(chan error, 1)
	go func() { firstClosed <- firstHandle.Close() }()
	awaitCleanupSignal(t, firstStarted)
	go func() { secondClosed <- secondHandle.Close() }()
	deadline := time.After(3 * time.Second)
	for store.Usage().Readers != 0 {
		select {
		case <-deadline:
			t.Fatal("second reader did not enqueue cleanup while the first unlink was blocked")
		case <-time.After(time.Millisecond):
		}
	}
	releaseFirst()
	awaitCleanupSignal(t, secondStarted)
	if err := awaitCleanupResult(t, firstClosed); err != nil {
		t.Fatal(err)
	}
	awaitCleanupSignal(t, firstHandle.Done())
	if _, err := os.Stat(filepath.Join(store.options.Root, id, firstID+".media")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first reader completed without its artifact being removed: %v", err)
	}
	select {
	case <-secondHandle.Done():
		t.Fatal("later reader completed before its blocked unlink")
	default:
	}
	if usage := store.Usage(); usage.Bytes != store.storage.unit || usage.Artifacts != 1 || usage.Readers != 0 {
		t.Fatalf("independent reader completion changed pending resource accounting: %+v", usage)
	}
	releaseSecond()
	if err := awaitCleanupResult(t, secondClosed); err != nil {
		t.Fatal(err)
	}
	if usage := store.Usage(); usage.Bytes != 0 || usage.Artifacts != 0 {
		t.Fatalf("later reader did not retire its own artifact: %+v", usage)
	}
}

func TestPublicationWaitsForQuotaRetirementWithoutOverEviction(t *testing.T) {
	for _, quota := range []string{"bytes", "artifacts"} {
		t.Run(quota, func(t *testing.T) {
			store, _ := testStore(t, nil)
			scope := testScope()
			id := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
			first := testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "first"))
			testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "second"))
			store.mu.Lock()
			if quota == "bytes" {
				store.options.MaxBytes = 2 * store.storage.unit
				store.windows[id].options.MaxBytes = 2 * store.storage.unit
			} else {
				store.options.MaxArtifacts = 2
			}
			store.mu.Unlock()
			started, release := blockCleanup(t, store, first.Segments[0].Artifacts[0].ID)
			input := testInput(t, "video", "third")
			done := make(chan error, 1)
			go func() {
				_, err := store.Publish(context.Background(), scope, id, Publication{Generation: 1, DurationTicks: TicksPerSecond, Segments: []ArtifactInput{input}})
				done <- err
			}()
			awaitCleanupSignal(t, started)
			if usage := store.Usage(); usage.Bytes != 2*store.storage.unit || usage.Artifacts != 2 || usage.PendingBytes != 0 || usage.Publishing != 1 {
				t.Fatalf("admission spent capacity before unlink completed: %+v", usage)
			}
			release()
			if err := awaitCleanupResult(t, done); err != nil {
				t.Fatal(err)
			}
			snapshot, err := store.Snapshot(context.Background(), scope, id)
			if err != nil || len(snapshot.Segments) != 2 || snapshot.Segments[0].Sequence != 1 || snapshot.NextSequence != 3 || snapshot.Bytes != 2*store.storage.unit {
				t.Fatalf("waiting admission changed successful retention: %+v, %v", snapshot, err)
			}
		})
	}
}

func TestPublicationCanUseAnotherWindowsPendingRetirement(t *testing.T) {
	store, advance := testStore(t, nil)
	scope := testScope()
	firstID := testWindow(t, store, scope, time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
	secondID := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
	first := testPublish(t, store, scope, firstID, 1, 1, nil, testInput(t, "video", "old allocation"))
	store.mu.Lock()
	store.options.MaxBytes = store.storage.unit
	store.mu.Unlock()
	started, release := blockCleanup(t, store, first.Segments[0].Artifacts[0].ID)
	advance(2 * time.Second)
	expired := make(chan error, 1)
	go func() { _, err := store.Snapshot(context.Background(), scope, firstID); expired <- err }()
	awaitCleanupSignal(t, started)
	input := testInput(t, "video", "replacement allocation")
	published := make(chan error, 1)
	go func() {
		_, err := store.Publish(context.Background(), scope, secondID, Publication{Generation: 1, DurationTicks: TicksPerSecond, Segments: []ArtifactInput{input}})
		published <- err
	}()
	// Observe admission under the mutex rather than relying on a scheduling
	// delay: a publisher owns its slot while waiting for global retirement.
	deadline := time.After(3 * time.Second)
	for store.Usage().Publishing != 1 {
		select {
		case err := <-published:
			t.Fatalf("pending global retirement became an immediate quota failure: %v", err)
		case <-deadline:
			t.Fatal("publication did not wait for pending global retirement")
		case <-time.After(time.Millisecond):
		}
	}
	release()
	if err := awaitCleanupResult(t, expired); err != nil {
		t.Fatal(err)
	}
	if err := awaitCleanupResult(t, published); err != nil {
		t.Fatal(err)
	}
	if usage := store.Usage(); usage.Bytes != store.storage.unit || usage.Artifacts != 1 || usage.PendingBytes != 0 {
		t.Fatalf("global capacity reuse changed physical accounting: %+v", usage)
	}
}

func TestUnrelatedCleanupDoesNotDelayImpossiblePublicationQuota(t *testing.T) {
	for _, obstruction := range []string{"pinned", "grace", "insufficient_global"} {
		t.Run(obstruction, func(t *testing.T) {
			store, advance := testStore(t, nil)
			scope := testScope()
			firstID := testWindow(t, store, scope, time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
			first := testPublish(t, store, scope, firstID, 1, 1, nil, testInput(t, "video", "unrelated cleanup"))
			options := WindowOptions{Window: 30 * time.Second, Variants: []Variant{{ID: "video", Kind: "video", Format: "ts"}}}
			if obstruction == "grace" {
				options.TargetDurationTicks = TicksPerSecond
			}
			window, err := store.Create(context.Background(), scope, options)
			if err != nil {
				t.Fatal(err)
			}
			id := window.PresentationID
			input := testInput(t, "video", "rejected publication")
			if obstruction == "insufficient_global" {
				thirdID := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
				testPublish(t, store, scope, thirdID, 1, 1, nil, testInput(t, "video", "still allocated"))
				input = testInput(t, "video", strings.Repeat("x", int(store.storage.unit)+1))
				store.mu.Lock()
				store.options.MaxBytes = 2 * store.storage.unit
				store.mu.Unlock()
			} else {
				retained := testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "protected"))
				if obstruction == "grace" {
					for index := 0; index < 2; index++ {
						retained = testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "advertised"))
					}
					if err := store.Advertise(context.Background(), scope, id, retained.Revision); err != nil {
						t.Fatal(err)
					}
				} else {
					handle, err := store.OpenArtifact(context.Background(), scope, id, retained.Segments[0].Artifacts[0].ID)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = handle.Close() })
				}
				store.mu.Lock()
				store.windows[id].options.MaxBytes = retained.Bytes
				store.mu.Unlock()
			}
			started, release := blockCleanup(t, store, first.Segments[0].Artifacts[0].ID)
			advance(2 * time.Second)
			expired := make(chan error, 1)
			go func() { _, err := store.Snapshot(context.Background(), scope, firstID); expired <- err }()
			awaitCleanupSignal(t, started)
			before := store.Usage()
			beforeWindow, err := store.Snapshot(context.Background(), scope, id)
			if err != nil {
				t.Fatal(err)
			}
			published := make(chan error, 1)
			go func() {
				_, err := store.Publish(context.Background(), scope, id, Publication{Generation: 1, DurationTicks: TicksPerSecond, Segments: []ArtifactInput{input}})
				published <- err
			}()
			if err := awaitCleanupResult(t, published); !errors.Is(err, ErrQuota) {
				t.Fatalf("impossible reservation did not reject while unrelated unlink remained blocked: %v", err)
			}
			if usage := store.Usage(); usage != before {
				t.Fatalf("rejected reservation changed resource ownership: before=%+v after=%+v", before, usage)
			}
			if after, err := store.Snapshot(context.Background(), scope, id); err != nil || after.Revision != beforeWindow.Revision || len(after.Segments) != len(beforeWindow.Segments) {
				t.Fatalf("impossible reservation hid protected media: before=%+v after=%+v err=%v", beforeWindow, after, err)
			}
			release()
			if err := awaitCleanupResult(t, expired); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGlobalCapacityWaitHonorsRequestAndWindowCancellation(t *testing.T) {
	for _, cancellation := range []string{"request", "window"} {
		t.Run(cancellation, func(t *testing.T) {
			store, advance := testStore(t, nil)
			scope := testScope()
			firstID := testWindow(t, store, scope, time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
			secondID := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
			first := testPublish(t, store, scope, firstID, 1, 1, nil, testInput(t, "video", "pending allocation"))
			store.mu.Lock()
			store.options.MaxBytes = store.storage.unit
			store.mu.Unlock()
			started, release := blockCleanup(t, store, first.Segments[0].Artifacts[0].ID)
			advance(2 * time.Second)
			expired := make(chan error, 1)
			go func() { _, err := store.Snapshot(context.Background(), scope, firstID); expired <- err }()
			awaitCleanupSignal(t, started)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			input := testInput(t, "video", "never copied")
			published := make(chan error, 1)
			go func() {
				_, err := store.Publish(ctx, scope, secondID, Publication{Generation: 1, DurationTicks: TicksPerSecond, Segments: []ArtifactInput{input}})
				published <- err
			}()
			deadline := time.After(3 * time.Second)
			for store.Usage().Publishing != 1 {
				select {
				case err := <-published:
					t.Fatalf("publication did not wait for sufficient pending global capacity: %v", err)
				case <-deadline:
					t.Fatal("publication did not own its waiting slot")
				case <-time.After(time.Millisecond):
				}
			}
			want := context.Canceled
			if cancellation == "request" {
				cancel()
			} else {
				want = ErrClosed
				closed := make(chan error, 1)
				go func() { closed <- store.ClosePresentation(context.Background(), scope, secondID) }()
				if err := awaitCleanupResult(t, closed); err != nil {
					t.Fatal(err)
				}
			}
			if err := awaitCleanupResult(t, published); !errors.Is(err, want) {
				t.Fatalf("capacity wait did not observe cancellation while unrelated unlink remained blocked: %v", err)
			}
			if usage := store.Usage(); usage.Publishing != 0 || usage.PendingBytes != 0 || usage.Bytes != store.storage.unit || usage.Artifacts != 1 {
				t.Fatalf("cancelled capacity wait retained a publisher or spent unretired capacity: %+v", usage)
			}
			release()
			if err := awaitCleanupResult(t, expired); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCancellationDuringCommittedExpirationKeepsPublicationResult(t *testing.T) {
	store, _ := testStore(t, nil)
	scope := testScope()
	id := testWindow(t, store, scope, time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
	first := testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "old interval"))
	started, release := blockCleanup(t, store, first.Segments[0].Artifacts[0].ID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := testInput(t, "video", "committed interval")
	published := make(chan error, 1)
	go func() {
		result, err := store.PublishLatest(ctx, scope, id, Publication{Generation: 1, DurationTicks: TicksPerSecond, Segments: []ArtifactInput{input}})
		if err == nil && (!result.HasLatest || result.Latest.Sequence != 1) {
			err = errors.New("committed publication did not return its interval")
		}
		published <- err
	}()
	awaitCleanupSignal(t, started)
	cancel()
	if usage := store.Usage(); usage.Bytes != 2*store.storage.unit || usage.Publishing != 1 {
		t.Fatalf("commit or expiration escaped accounting: %+v", usage)
	}
	release()
	if err := awaitCleanupResult(t, published); err != nil {
		t.Fatalf("post-commit cancellation hid a committed interval from its caller: %v", err)
	}
	if snapshot, err := store.Snapshot(context.Background(), scope, id); err != nil || snapshot.NextSequence != 2 || len(snapshot.Segments) != 1 || snapshot.Bytes != store.storage.unit {
		t.Fatalf("committed cancellation changed the retained timeline: %+v, %v", snapshot, err)
	}
}

func TestCancelledPublicationRetirementKeepsRootUntilCloseDrains(t *testing.T) {
	store, _ := testStore(t, nil)
	scope := testScope()
	id := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finish)
	remove := store.removeArtifact
	store.removeArtifact = func(window *storageWindow, artifactID string) error {
		close(started)
		<-release
		return remove(window, artifactID)
	}
	copyArtifact := store.copyArtifact
	store.copyArtifact = func(ctx context.Context, window *storageWindow, artifactID string, input *os.File, before os.FileInfo) (int64, error) {
		charge, err := copyArtifact(ctx, window, artifactID, input, before)
		return charge, errors.Join(err, context.Canceled)
	}
	input := testInput(t, "video", "private rollback")
	published := make(chan error, 1)
	go func() {
		_, err := store.Publish(context.Background(), scope, id, Publication{Generation: 1, DurationTicks: TicksPerSecond, Segments: []ArtifactInput{input}})
		published <- err
	}()
	awaitCleanupSignal(t, started)
	if usage := store.Usage(); usage.Bytes != store.storage.unit || usage.Artifacts != 1 || usage.PendingBytes != 0 || usage.Publishing != 1 {
		t.Fatalf("rollback released reserved storage before actual removal: %+v", usage)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := store.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close did not wait for admitted cleanup: %v", err)
	}
	if other, err := New(Options{Root: store.options.Root}); !errors.Is(err, ErrLocked) {
		if other != nil {
			_ = other.Close(context.Background())
		}
		t.Fatalf("close timeout released the live root: %v", err)
	}
	if usage := store.Usage(); usage.Bytes != store.storage.unit || usage.Windows != 1 {
		t.Fatalf("close timeout pretended resources had retired: %+v", usage)
	}
	finish()
	if err := awaitCleanupResult(t, published); !errors.Is(err, context.Canceled) {
		t.Fatalf("publication lost its cancellation: %v", err)
	}
	if err := store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if usage := store.Usage(); usage.Bytes != 0 || usage.Windows != 0 || usage.Publishing != 0 {
		t.Fatalf("close completed before publication and directory retirement: %+v", usage)
	}
}

func TestFailedRetirementStaysChargedAndDoesNotRetryOrResurrect(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	var clock atomic.Int64
	clock.Store(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC).UnixNano())
	store, err := New(Options{Root: root, SweepInterval: time.Minute, Now: func() time.Time { return time.Unix(0, clock.Load()) }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(context.Background())
	scope := testScope()
	id := testWindow(t, store, scope, time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
	first := testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "retained failure"))
	artifactID := first.Segments[0].Artifacts[0].ID
	failure := errors.New("controlled unlink failure")
	var calls atomic.Int32
	store.removeArtifact = func(*storageWindow, string) error { calls.Add(1); return failure }
	clock.Add(int64(2 * time.Second))
	if snapshot, err := store.Snapshot(context.Background(), scope, id); err != nil || len(snapshot.Segments) != 0 || snapshot.Bytes != store.storage.unit {
		t.Fatalf("failed unlink escaped physical accounting: %+v, %v", snapshot, err)
	}
	clock.Add(-int64(2 * time.Second))
	if _, _, err := store.ResolveArtifact(context.Background(), scope, id, artifactID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed tombstone became available again: %v", err)
	}
	if _, err := store.Create(context.Background(), scope, WindowOptions{Variants: []Variant{{ID: "video", Kind: "video", Format: "ts"}}}); !errors.Is(err, ErrStorage) {
		t.Fatalf("storage failure did not remain sticky: %v", err)
	}
	if err := store.ClosePresentation(context.Background(), scope, id); !errors.Is(err, ErrStorage) {
		t.Fatalf("presentation close hid its failed cleanup: %v", err)
	}
	if err := store.Close(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("store close lost the cleanup failure: %v", err)
	}
	if usage := store.Usage(); usage.Bytes != store.storage.unit || usage.Artifacts != 1 || usage.Windows != 1 || calls.Load() != 1 {
		t.Fatalf("failed retirement was retried or released: %+v, calls=%d", usage, calls.Load())
	}
	if _, err := os.Stat(filepath.Join(root, id, artifactID+".media")); err != nil {
		t.Fatal("failed cleanup no longer has its charged file", err)
	}
	recovered, err := New(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupConcurrencyAndQueueAreBoundedByOwnedArtifacts(t *testing.T) {
	store, advance := testStore(t, nil)
	scope := testScope()
	var ids []string
	for index := 0; index < 3; index++ {
		id := testWindow(t, store, scope, time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
		testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "bounded"))
		ids = append(ids, id)
	}
	started, release := make(chan struct{}, 3), make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finish)
	var active, peak, calls atomic.Int32
	remove := store.removeArtifact
	store.removeArtifact = func(window *storageWindow, id string) error {
		current := active.Add(1)
		for previous := peak.Load(); current > previous && !peak.CompareAndSwap(previous, current); previous = peak.Load() {
		}
		calls.Add(1)
		started <- struct{}{}
		<-release
		err := remove(window, id)
		active.Add(-1)
		return err
	}
	advance(2 * time.Second)
	store.mu.Lock()
	for _, id := range ids {
		store.expireLocked(store.windows[id], store.options.Now())
		store.expireLocked(store.windows[id], store.options.Now())
	}
	queued := store.cleanupCount
	store.mu.Unlock()
	awaitCleanupSignal(t, started)
	if queued != 3 || calls.Load() != 1 || peak.Load() != 1 {
		t.Fatalf("cleanup duplicated tasks or exceeded its worker bound: queued=%d calls=%d peak=%d", queued, calls.Load(), peak.Load())
	}
	finish()
	for _, id := range ids {
		if _, err := store.Snapshot(context.Background(), scope, id); err != nil {
			t.Fatal(err)
		}
	}
	if usage := store.Usage(); calls.Load() != 3 || peak.Load() != 1 || usage.Bytes != 0 || usage.Artifacts != 0 {
		t.Fatalf("bounded cleanup did not drain exactly once: %+v calls=%d peak=%d", usage, calls.Load(), peak.Load())
	}
}

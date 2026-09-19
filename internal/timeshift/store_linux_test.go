package timeshift

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testScope() Scope {
	return Scope{UserID: "user", AuthSessionID: "auth", DeviceID: "device", PlaySessionID: "play", ItemID: "item", SourceID: "source"}
}

func testStore(t *testing.T, modify func(*Options)) (*Store, func(time.Duration)) {
	t.Helper()
	var clock atomic.Int64
	clock.Store(time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC).UnixNano())
	options := Options{Root: filepath.Join(t.TempDir(), "window-cache"), SweepInterval: time.Minute, Now: func() time.Time { return time.Unix(0, clock.Load()) }}
	if modify != nil {
		modify(&options)
	}
	store, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return store, func(duration time.Duration) { clock.Add(int64(duration)) }
}

func testInput(t *testing.T, variant, text string) ArtifactInput {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "complete-media-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, text); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return ArtifactInput{VariantID: variant, File: file}
}

func testWindow(t *testing.T, store *Store, scope Scope, duration time.Duration, variants ...Variant) string {
	t.Helper()
	window, err := store.Create(context.Background(), scope, WindowOptions{Window: duration, Variants: variants})
	if err != nil {
		t.Fatal(err)
	}
	return window.PresentationID
}

func testPublish(t *testing.T, store *Store, scope Scope, id string, generation uint64, seconds int64, initializations []ArtifactInput, segments ...ArtifactInput) WindowSnapshot {
	t.Helper()
	result, err := store.Publish(context.Background(), scope, id, Publication{Generation: generation, DurationTicks: seconds * TicksPerSecond, Initializations: initializations, Segments: segments})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestWindowUsesObservedDurationsAndIndependentEpochInitialization(t *testing.T) {
	store, _ := testStore(t, nil)
	scope := testScope()
	id := testWindow(t, store, scope, 30*time.Second, Variant{ID: "video", Kind: "video", Format: "fmp4"}, Variant{ID: "captions", Kind: "subtitle", Format: "vtt"})
	firstMedia := testInput(t, "video", "first media")
	if _, err := firstMedia.File.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	first := testPublish(t, store, scope, id, 1, 2, []ArtifactInput{testInput(t, "video", "first initialization")}, firstMedia, testInput(t, "captions", "WEBVTT\n\n"))
	if offset, err := firstMedia.File.Seek(0, io.SeekCurrent); err != nil || offset != 3 {
		t.Fatal("publishing changed the borrowed input offset")
	}
	second := testPublish(t, store, scope, id, 1, 3, nil, testInput(t, "video", "second media"), testInput(t, "captions", "WEBVTT\n\nsecond"))
	third := testPublish(t, store, scope, id, 8, 4, []ArtifactInput{testInput(t, "video", "new initialization")}, testInput(t, "video", "third media"), testInput(t, "captions", "WEBVTT\n\nthird"))
	if first.Segments[0].Sequence != 0 || first.Segments[0].Discontinuity || second.Segments[1].Sequence != 1 || third.Segments[2].Sequence != 2 ||
		third.Segments[2].StartTicks != 5*TicksPerSecond || third.LiveEdgeTicks != 9*TicksPerSecond || !third.Segments[2].Discontinuity ||
		third.Segments[2].DiscontinuitySequence != 1 || len(third.Epochs) != 2 || third.Epochs[1].Generation != 8 {
		t.Fatalf("publication fabricated a clock or reused an epoch sequence: %+v", third)
	}
	firstInit, thirdInit := first.Segments[0].Artifacts[0].InitID, third.Segments[2].Artifacts[0].InitID
	if firstInit == "" || firstInit == thirdInit || second.Segments[1].Artifacts[0].InitID != firstInit {
		t.Fatal("epochs did not retain independent initialization resources")
	}
	for _, artifact := range []string{firstInit, thirdInit, first.Segments[0].Artifacts[0].ID} {
		handle, err := store.OpenArtifact(context.Background(), scope, id, artifact)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(handle)
		if err != nil || len(data) == 0 {
			t.Fatal("published artifact was not readable")
		}
		_ = handle.Close()
	}
	position, err := store.Seek(context.Background(), scope, id, 7*TicksPerSecond)
	if err != nil || position.Sequence != 2 || position.Generation != 8 || position.StartTicks != 5*TicksPerSecond {
		t.Fatal("seek did not use the actually retained interval")
	}
	if _, err := store.Seek(context.Background(), scope, id, third.LiveEdgeTicks); !errors.Is(err, ErrNotBuffered) {
		t.Fatal("the unpublished live edge was seekable")
	}
	live, err := store.Live(context.Background(), scope, id)
	if err != nil || live.StartTicks != third.LiveStartTicks {
		t.Fatal("live selection did not use the safe published start")
	}
	third.Segments[0].Artifacts[0].ID = "mutated"
	third.Epochs[0].Initializations[0].ID = "mutated"
	third.Variants[0].ID = "mutated"
	fresh, err := store.Snapshot(context.Background(), scope, id)
	if err != nil || fresh.Segments[0].Artifacts[0].ID == "mutated" || fresh.Epochs[0].Initializations[0].ID == "mutated" || fresh.Variants[0].ID == "mutated" {
		t.Fatal("a snapshot mutated private window state")
	}
}

func TestExpiredReadersRemainChargedUntilTheirHandlesClose(t *testing.T) {
	store, _ := testStore(t, nil)
	scope := testScope()
	id := testWindow(t, store, scope, 4*time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
	first := testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "first"))
	artifact := first.Segments[0].Artifacts[0].ID
	handle, err := store.OpenArtifact(context.Background(), scope, id, artifact)
	if err != nil {
		t.Fatal(err)
	}
	testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "second"))
	third := testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "third"))
	if third.EarliestTicks != 2*TicksPerSecond || len(third.Segments) != 2 || store.Usage().Artifacts != 3 || third.Bytes != 3*store.storage.unit {
		t.Fatal("expired open media escaped physical accounting")
	}
	if _, err := store.OpenArtifact(context.Background(), scope, id, artifact); !errors.Is(err, ErrNotFound) {
		t.Fatal("an expired artifact admitted a new reader")
	}
	if _, err := store.Seek(context.Background(), scope, id, 1); !errors.Is(err, ErrWindowExpired) {
		t.Fatal("expired seek silently moved to the live window")
	}
	if data, err := io.ReadAll(handle); err != nil || string(data) != "first" {
		t.Fatal("eviction invalidated an already bounded reader")
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if store.Usage().Artifacts != 2 || store.Usage().Bytes != 2*store.storage.unit {
		t.Fatal("closing the last expired reader did not release its storage charge")
	}
}

func TestReaderDeadlineAndCancellationReleaseExpiredMedia(t *testing.T) {
	store, advance := testStore(t, func(options *Options) { options.ReadTimeout = 500 * time.Millisecond })
	scope := testScope()
	id := testWindow(t, store, scope, time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
	first := testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "bounded"))
	handle, err := store.OpenArtifact(context.Background(), scope, id, first.Segments[0].Artifacts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	advance(2 * time.Second)
	if snapshot, err := store.Snapshot(context.Background(), scope, id); err != nil || len(snapshot.Segments) != 0 || snapshot.Bytes == 0 {
		t.Fatal("wall-age expiry lost the pinned artifact charge")
	}
	select {
	case <-handle.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the absolute reader deadline was not enforced")
	}
	if _, err := handle.Read(make([]byte, 1)); !errors.Is(err, ErrClosed) || store.Usage().Bytes != 0 {
		t.Fatal("expired reader could retain or read media indefinitely")
	}
	second := testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "next"))
	ctx, cancel := context.WithCancel(context.Background())
	handle, err = store.OpenArtifact(ctx, scope, id, second.Segments[0].Artifacts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-handle.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("request cancellation did not close its reader")
	}
}

func TestEveryOperationRequiresExactScope(t *testing.T) {
	store, _ := testStore(t, nil)
	scope := testScope()
	id := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
	published := testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "private"))
	for _, mutate := range []func(*Scope){
		func(s *Scope) { s.UserID = "other" }, func(s *Scope) { s.AuthSessionID = "other" }, func(s *Scope) { s.DeviceID = "other" },
		func(s *Scope) { s.PlaySessionID = "other" }, func(s *Scope) { s.ItemID = "other" }, func(s *Scope) { s.SourceID = "other" },
		func(s *Scope) { s.ApplicationKey, s.UserID, s.ApplicationClientID = true, "", "other" },
	} {
		foreign := scope
		mutate(&foreign)
		if _, err := store.Snapshot(context.Background(), foreign, id); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign scope inspected a window")
		}
		if _, err := store.Seek(context.Background(), foreign, id, 0); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign scope sought media")
		}
		if _, err := store.Live(context.Background(), foreign, id); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign scope selected live media")
		}
		if _, err := store.OpenArtifact(context.Background(), foreign, id, published.Segments[0].Artifacts[0].ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign scope opened an artifact")
		}
		if _, _, err := store.ResolveArtifact(context.Background(), foreign, id, published.Segments[0].Artifacts[0].ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign scope resolved an artifact")
		}
		if retained, err := store.RetainsArtifact(context.Background(), foreign, id, published.Segments[0].Artifacts[0].ID); retained || !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign scope inspected artifact retention")
		}
		if err := store.Advertise(context.Background(), foreign, id, published.Revision); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign scope advertised a window")
		}
		if _, err := store.Publish(context.Background(), foreign, id, Publication{}); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign scope reached publication input validation")
		}
		if err := store.SetState(context.Background(), foreign, id, State{Ended: true}); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign scope changed source state")
		}
		if err := store.Touch(context.Background(), foreign, id); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign scope renewed a consumer lease")
		}
		if err := store.ClosePresentation(context.Background(), foreign, id); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign scope revoked a window")
		}
	}
	if store.Usage().Readers != 0 || store.Usage().Artifacts != 1 {
		t.Fatal("foreign operations changed resource ownership")
	}
}

func TestPublicationIsAtomicAndConcurrentWritersAreBounded(t *testing.T) {
	store, _ := testStore(t, func(options *Options) { options.MaxPublishing = 1 })
	scope := testScope()
	id := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "fmp4"})
	started, release := make(chan string, 1), make(chan struct{})
	copy := store.copyArtifact
	var called atomic.Bool
	store.copyArtifact = func(ctx context.Context, window *storageWindow, id string, file *os.File, info os.FileInfo) (int64, error) {
		if called.CompareAndSwap(false, true) {
			started <- id
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-release:
			}
		}
		return copy(ctx, window, id, file, info)
	}
	publication := Publication{Generation: 1, DurationTicks: TicksPerSecond, Initializations: []ArtifactInput{testInput(t, "video", "init")}, Segments: []ArtifactInput{testInput(t, "video", "segment")}}
	done := make(chan error, 1)
	go func() { _, err := store.Publish(context.Background(), scope, id, publication); done <- err }()
	var artifact string
	select {
	case artifact = <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("publication did not reach its controlled copy")
	}
	pending, err := store.Snapshot(context.Background(), scope, id)
	if err != nil || len(pending.Segments) != 0 || len(pending.Epochs) != 0 || pending.PendingBytes != 2*store.storage.unit || store.Usage().Publishing != 1 {
		t.Fatal("an incomplete bundle became visible or escaped reservations")
	}
	if _, err := store.OpenArtifact(context.Background(), scope, id, artifact); !errors.Is(err, ErrNotFound) {
		t.Fatal("a private staged artifact was readable")
	}
	if _, err := store.Publish(context.Background(), scope, id, publication); !errors.Is(err, ErrBusy) {
		t.Fatal("a second publisher bypassed serialization")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	committed, err := store.Snapshot(context.Background(), scope, id)
	if err != nil || len(committed.Segments) != 1 || len(committed.Epochs) != 1 || committed.PendingBytes != 0 || store.Usage().Publishing != 0 {
		t.Fatal("complete publication did not commit exactly once")
	}
}

func TestPublicationCancellationAndInputMutationLeaveNoPartialResources(t *testing.T) {
	for _, kind := range []string{"cancel", "mutate"} {
		t.Run(kind, func(t *testing.T) {
			store, _ := testStore(t, nil)
			scope := testScope()
			id := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
			input := testInput(t, "video", "complete")
			copy := store.copyArtifact
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store.copyArtifact = func(work context.Context, window *storageWindow, id string, file *os.File, info os.FileInfo) (int64, error) {
				if kind == "cancel" {
					cancel()
				} else {
					if err := file.Truncate(info.Size() + 1); err != nil {
						return 0, err
					}
				}
				return copy(work, window, id, file, info)
			}
			if _, err := store.Publish(ctx, scope, id, Publication{Generation: 1, DurationTicks: TicksPerSecond, Segments: []ArtifactInput{input}}); err == nil {
				t.Fatal("cancelled or mutated input was published")
			}
			usage := store.Usage()
			if usage.Bytes != 0 || usage.PendingBytes != 0 || usage.Artifacts != 0 || usage.Publishing != 0 {
				t.Fatal("failed publication leaked an artifact charge or reservation")
			}
			entries, err := os.ReadDir(filepath.Join(store.options.Root, id))
			if err != nil || len(entries) != 1 || entries[0].Name() != windowMarker {
				t.Fatal("failed publication left physical unaccounted files")
			}
		})
	}
}

func TestStateEpochAndNumericBounds(t *testing.T) {
	store, _ := testStore(t, func(options *Options) { options.MaxEpochs = 2; options.MaxSegments = 2 })
	scope := testScope()
	id := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "fmp4"})
	for generation := uint64(1); generation <= 3; generation++ {
		result := testPublish(t, store, scope, id, generation, 1, []ArtifactInput{testInput(t, "video", "initialization")}, testInput(t, "video", "media"))
		if len(result.Epochs) > 2 || len(result.Segments) > 2 {
			t.Fatal("segment or epoch retention exceeded its bound")
		}
	}
	if err := store.SetState(context.Background(), scope, id, State{Stalled: true}); err != nil {
		t.Fatal(err)
	}
	stalled, _ := store.Snapshot(context.Background(), scope, id)
	if !stalled.Stalled || stalled.Ended || stalled.LiveEdgeTicks != 3*TicksPerSecond {
		t.Fatal("stall invented media duration or completion")
	}
	if err := store.SetState(context.Background(), scope, id, State{Ended: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetState(context.Background(), scope, id, State{}); !errors.Is(err, ErrEnded) {
		t.Fatal("a terminal presentation restarted in place")
	}
	if _, err := store.Publish(context.Background(), scope, id, Publication{}); !errors.Is(err, ErrEnded) {
		t.Fatal("an ended presentation accepted more media")
	}
	for _, mutation := range []func(*windowState){
		func(w *windowState) { w.nextSequence = math.MaxUint64 }, func(w *windowState) { w.liveEdge = math.MaxInt64 - 1 },
		func(w *windowState) { w.generation = 1; w.discontinuity = math.MaxUint64 },
	} {
		other := scope
		other.PlaySessionID += "numeric"
		windowID := testWindow(t, store, other, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
		store.mu.Lock()
		mutation(store.windows[windowID])
		store.mu.Unlock()
		if _, err := store.Publish(context.Background(), other, windowID, Publication{Generation: 2, DurationTicks: TicksPerSecond, Segments: []ArtifactInput{testInput(t, "video", "bounded")}}); !errors.Is(err, ErrInvalid) {
			t.Fatal("a timeline counter could overflow")
		}
		if err := store.ClosePresentation(context.Background(), other, windowID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPartialBundleFailureRollsBackAllCopiedArtifacts(t *testing.T) {
	store, _ := testStore(t, nil)
	scope := testScope()
	id := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "fmp4"}, Variant{ID: "subtitle", Kind: "subtitle", Format: "vtt"})
	copy := store.copyArtifact
	calls := 0
	store.copyArtifact = func(ctx context.Context, window *storageWindow, id string, file *os.File, info os.FileInfo) (int64, error) {
		calls++
		if calls == 3 {
			return 0, io.ErrUnexpectedEOF
		}
		return copy(ctx, window, id, file, info)
	}
	_, err := store.Publish(context.Background(), scope, id, Publication{Generation: 1, DurationTicks: TicksPerSecond,
		Initializations: []ArtifactInput{testInput(t, "video", "init")}, Segments: []ArtifactInput{testInput(t, "video", "video"), testInput(t, "subtitle", "WEBVTT\n\n")}})
	if !errors.Is(err, io.ErrUnexpectedEOF) || calls != 3 {
		t.Fatal("the controlled late publication failure was not reached")
	}
	snapshot, err := store.Snapshot(context.Background(), scope, id)
	if err != nil || len(snapshot.Segments) != 0 || len(snapshot.Epochs) != 0 || snapshot.LiveEdgeTicks != 0 || snapshot.Bytes != 0 || snapshot.PendingBytes != 0 || store.Usage().Artifacts != 0 {
		t.Fatal("a late bundle failure exposed media or retained an uncharged partial publication")
	}
	entries, err := os.ReadDir(filepath.Join(store.options.Root, id))
	if err != nil || len(entries) != 1 || entries[0].Name() != windowMarker {
		t.Fatal("late failure left copied physical artifacts")
	}
}

func TestFixedVariantsAndEpochInputsRejectIncompleteBundles(t *testing.T) {
	store, _ := testStore(t, nil)
	scope := testScope()
	id := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "fmp4"}, Variant{ID: "subtitle", Kind: "subtitle", Format: "vtt"})
	video, subtitle, initialization := testInput(t, "video", "video"), testInput(t, "subtitle", "WEBVTT\n\n"), testInput(t, "video", "init")
	for _, publication := range []Publication{
		{Generation: 1, DurationTicks: TicksPerSecond, Segments: []ArtifactInput{video, subtitle}},
		{Generation: 1, DurationTicks: TicksPerSecond, Initializations: []ArtifactInput{initialization}, Segments: []ArtifactInput{video}},
		{Generation: 1, DurationTicks: TicksPerSecond, Initializations: []ArtifactInput{initialization}, Segments: []ArtifactInput{video, video}},
		{Generation: 1, DurationTicks: TicksPerSecond, Initializations: []ArtifactInput{initialization, subtitle}, Segments: []ArtifactInput{video, subtitle}},
		{Generation: 1, DurationTicks: 0, Initializations: []ArtifactInput{initialization}, Segments: []ArtifactInput{video, subtitle}},
	} {
		if _, err := store.Publish(context.Background(), scope, id, publication); !errors.Is(err, ErrInvalid) {
			t.Fatal("an incomplete or contradictory bundle was accepted")
		}
		if store.Usage().Bytes != 0 || store.Usage().PendingBytes != 0 {
			t.Fatal("invalid publication reserved or wrote media")
		}
	}
	testPublish(t, store, scope, id, 1, 1, []ArtifactInput{initialization}, video, subtitle)
	if _, err := store.Publish(context.Background(), scope, id, Publication{Generation: 1, DurationTicks: TicksPerSecond, Initializations: []ArtifactInput{initialization}, Segments: []ArtifactInput{video, subtitle}}); !errors.Is(err, ErrInvalid) {
		t.Fatal("same-epoch publication replaced its initialization")
	}
}

func TestApplicationClientIdentityAndCloseDuringPublication(t *testing.T) {
	store, _ := testStore(t, nil)
	scope := testScope()
	scope.UserID = ""
	scope.ApplicationKey = true
	scope.ApplicationClientID = "application-client"
	id := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
	foreign := scope
	foreign.ApplicationClientID = "another-client"
	if _, err := store.Snapshot(context.Background(), foreign, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("an application credential borrowed another client window")
	}
	started := make(chan struct{})
	store.copyArtifact = func(ctx context.Context, _ *storageWindow, _ string, _ *os.File, _ os.FileInfo) (int64, error) {
		close(started)
		<-ctx.Done()
		return 0, ctx.Err()
	}
	done := make(chan error, 1)
	input := testInput(t, "video", "media")
	go func() {
		_, err := store.Publish(context.Background(), scope, id, Publication{Generation: 1, DurationTicks: TicksPerSecond, Segments: []ArtifactInput{input}})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("controlled publisher did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) && !errors.Is(err, ErrClosed) {
		t.Fatal("close did not cancel in-flight publication")
	}
	if usage := store.Usage(); usage.Bytes != 0 || usage.PendingBytes != 0 || usage.Windows != 0 || usage.Publishing != 0 {
		t.Fatal("close released its lock before publication resources drained")
	}
}

func TestWindowReaderAndStorageQuotas(t *testing.T) {
	store, _ := testStore(t, func(options *Options) {
		options.MaxWindows = 2
		options.MaxOwnerWindows = 1
		options.MaxReaders = 1
		options.MaxWindowReaders = 1
	})
	scope := testScope()
	id := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
	duplicate := scope
	duplicate.AuthSessionID = "another-auth"
	if _, err := store.Create(context.Background(), duplicate, WindowOptions{Variants: []Variant{{ID: "video", Kind: "video", Format: "ts"}}}); !errors.Is(err, ErrBusy) {
		t.Fatal("a new credential bypassed the per-user window quota")
	}
	first := testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "first"))
	handle, err := store.OpenArtifact(context.Background(), scope, id, first.Segments[0].Artifacts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenArtifact(context.Background(), scope, id, first.Segments[0].Artifacts[0].ID); !errors.Is(err, ErrBusy) {
		t.Fatal("reader admission exceeded its fixed limit")
	}
	_ = handle.Close()
	other := scope
	other.UserID = "another-user"
	secondID := testWindow(t, store, other, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
	store.mu.Lock()
	store.options.MaxBytes = store.storage.unit
	store.mu.Unlock()
	if _, err := store.Publish(context.Background(), other, secondID, Publication{Generation: 1, DurationTicks: TicksPerSecond, Segments: []ArtifactInput{testInput(t, "video", "second")}}); !errors.Is(err, ErrQuota) {
		t.Fatal("global storage quota was bypassed")
	}
	if first, _ := store.Snapshot(context.Background(), scope, id); len(first.Segments) != 1 {
		t.Fatal("one user evicted another user's window to obtain quota")
	}
}

func TestProductionDoesNotRenewAnIdleConsumerLease(t *testing.T) {
	store, advance := testStore(t, func(options *Options) { options.IdleTimeout = 5 * time.Second })
	scope := testScope()
	id := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
	advance(3 * time.Second)
	testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "producer-only"))
	advance(3 * time.Second)
	if _, err := store.Snapshot(context.Background(), scope, id); !errors.Is(err, ErrClosed) {
		t.Fatal("media production renewed a consumer that stopped sending requests")
	}
	if store.Usage().Windows != 0 || store.Usage().Bytes != 0 {
		t.Fatal("consumer expiration did not revoke its stored media")
	}
	next := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
	advance(3 * time.Second)
	if err := store.Touch(context.Background(), scope, next); err != nil {
		t.Fatal(err)
	}
	advance(3 * time.Second)
	if _, err := store.Snapshot(context.Background(), scope, next); err != nil {
		t.Fatal("an explicit authorized keepalive failed to renew the consumer lease")
	}
}

func TestCloseRevokesReadersAndReleasesExclusiveRoot(t *testing.T) {
	store, _ := testStore(t, nil)
	if second, err := New(Options{Root: store.options.Root}); !errors.Is(err, ErrLocked) {
		if second != nil {
			_ = second.Close(context.Background())
		}
		t.Fatal("a second store obtained the live root")
	}
	scope := testScope()
	id := testWindow(t, store, scope, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
	first := testPublish(t, store, scope, id, 1, 1, nil, testInput(t, "video", "private"))
	handle, err := store.OpenArtifact(context.Background(), scope, id, first.Segments[0].Artifacts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ClosePresentation(context.Background(), scope, id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handle.Done():
	default:
		t.Fatal("presentation revocation retained a reader")
	}
	if store.Usage().Bytes != 0 || store.Usage().Windows != 0 || store.Usage().Readers != 0 {
		t.Fatal("revocation retained media or resource capacity")
	}
	if err := store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(Options{Root: store.options.Root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Snapshot(context.Background(), scope, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("restart reconstructed an unsupported historical window")
	}
	if err := reopened.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryRemovesOnlyVerifiedOwnedRemainders(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "owned")
	root, err := openStorage(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	id := "w_" + strings.Repeat("a", 32)
	window, err := root.create(id)
	if err != nil {
		t.Fatal(err)
	}
	input := testInput(t, "video", "retained bytes")
	info, _ := input.File.Stat()
	artifactID := "a_" + strings.Repeat("b", 32)
	if _, err := window.copy(context.Background(), artifactID, input.File, info); err != nil {
		t.Fatal(err)
	}
	_ = window.close()
	_ = root.close()
	foreign := filepath.Join(rootPath, id, "unrelated-document.txt")
	if err := os.WriteFile(foreign, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if store, err := New(Options{Root: rootPath}); !errors.Is(err, ErrUnsafe) {
		if store != nil {
			_ = store.Close(context.Background())
		}
		t.Fatal("unknown content was claimed by startup recovery")
	}
	if _, err := os.Stat(filepath.Join(rootPath, id, artifactID+".media")); err != nil {
		t.Fatal("recovery deleted recognized media before completing its ownership audit")
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "preserve" {
		t.Fatal("recovery modified unfamiliar content")
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	store, err := New(Options{Root: rootPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(rootPath, id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("verified stale presentation was not recovered")
	}
	if err := store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStorageRejectsUnmarkedNonemptyRootsAndSymlinks(t *testing.T) {
	parent := t.TempDir()
	unmarked := filepath.Join(parent, "unmarked")
	if err := os.Mkdir(unmarked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unmarked, "important"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if store, err := New(Options{Root: unmarked}); err == nil {
		_ = store.Close(context.Background())
		t.Fatal("an unmarked nonempty directory was claimed")
	}
	linked := filepath.Join(parent, "link")
	if err := os.Symlink(unmarked, linked); err != nil {
		t.Fatal(err)
	}
	if store, err := New(Options{Root: linked}); err == nil {
		_ = store.Close(context.Background())
		t.Fatal("a symbolic link root was followed")
	}
	if store, err := New(Options{Root: filepath.Join(linked, "nested")}); err == nil {
		_ = store.Close(context.Background())
		t.Fatal("a symbolic link ancestor was followed")
	}
	if _, err := os.Stat(filepath.Join(unmarked, "nested")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a rejected linked ancestor created outside storage")
	}
}

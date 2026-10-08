//go:build linux

package timeshift

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetainsArtifactsSharesOneWindowObservation(t *testing.T) {
	var observations atomic.Int64
	store, _ := testStore(t, func(options *Options) {
		now := options.Now
		options.Now = func() time.Time {
			observations.Add(1)
			return now()
		}
	})
	scope := testScope()
	id := testWindow(t, store, scope, 30*time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
	first := testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "first"))
	second := testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "second"))
	firstID, secondID := first.Segments[0].Artifacts[0].ID, second.Segments[1].Artifacts[0].ID
	observations.Store(0)
	retained, err := store.RetainsArtifacts(context.Background(), scope, id, []string{firstID, secondID, firstID, "missing"})
	if err != nil || len(retained) != 3 || !retained[firstID] || !retained[secondID] || retained["missing"] {
		t.Fatalf("batch did not preserve current, duplicate and missing artifact results: %v, %v", retained, err)
	}
	// Visible artifacts need no per-artifact grace clock. The whole request
	// observes admission/expiry once, followed by one fresh grace timestamp.
	if got := observations.Load(); got != 2 {
		t.Fatalf("batch repeated its presentation observation: got %d clock reads, want 2", got)
	}
}

func TestRetainsArtifactsFreshGraceDoesNotReleaseAnOpenReader(t *testing.T) {
	var crossing atomic.Bool
	var observations atomic.Int64
	store, advance := testStore(t, func(options *Options) {
		options.MaxSegments = 3
		now := options.Now
		options.Now = func() time.Time {
			value := now()
			if crossing.Load() && observations.Add(1) > 1 {
				value = value.Add(time.Nanosecond)
			}
			return value
		}
	})
	scope := testScope()
	id := testAdvertisedWindow(t, store, scope)
	first := testPublish(t, store, scope, id, 1, 2, []ArtifactInput{testInput(t, "video", "init")}, testInput(t, "video", "first"))
	testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "second"))
	third := testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "third"))
	if err := store.Advertise(context.Background(), scope, id, third.Revision); err != nil {
		t.Fatal(err)
	}
	oldID := first.Segments[0].Artifacts[0].ID
	handle, err := store.OpenArtifact(context.Background(), scope, id, oldID)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	current := testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "fourth"))
	currentID := current.Segments[len(current.Segments)-1].Artifacts[0].ID
	store.mu.Lock()
	graceUntil := store.windows[id].artifacts[oldID].graceUntil
	now := store.options.Now()
	store.mu.Unlock()
	advance(graceUntil.Sub(now) - time.Nanosecond)
	before := store.Usage()
	crossing.Store(true)
	retained, err := store.RetainsArtifacts(context.Background(), scope, id, []string{oldID, currentID, "missing"})
	crossing.Store(false)
	advance(time.Nanosecond)
	if err != nil || retained[oldID] || !retained[currentID] || retained["missing"] {
		t.Fatalf("batch reused a stale grace observation: %v, %v", retained, err)
	}
	if after := store.Usage(); after.Bytes != before.Bytes || after.Readers != 1 {
		t.Fatalf("expired visibility released an owned reader or its charge: before=%+v after=%+v", before, after)
	}
	data, err := io.ReadAll(handle)
	if err != nil || string(data) != "first" {
		t.Fatalf("retention lookup revoked an already open reader: %q, %v", data, err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if after := store.Usage(); after.Readers != 0 || after.Bytes >= before.Bytes {
		t.Fatalf("closing the expired reader did not release its charge: before=%+v after=%+v", before, after)
	}
}

func TestRetainsArtifactsPreservesScopeCancellationAndIdleLease(t *testing.T) {
	store, advance := testStore(t, func(options *Options) { options.IdleTimeout = 5 * time.Second })
	scope := testScope()
	id := testWindow(t, store, scope, 30*time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
	published := testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "media"))
	artifactID := published.Segments[0].Artifacts[0].ID
	ids := []string{artifactID}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if retained, err := store.RetainsArtifacts(ctx, scope, id, ids); retained != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled batch returned an ownership observation: %v, %v", retained, err)
	}
	foreign := scope
	foreign.AuthSessionID = "foreign"
	if retained, err := store.RetainsArtifacts(context.Background(), foreign, id, ids); retained != nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign scope inspected retention: %v, %v", retained, err)
	}
	if retained, err := store.RetainsArtifacts(context.Background(), Scope{}, id, ids); retained != nil || !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid scope inspected retention: %v, %v", retained, err)
	}
	advance(3 * time.Second)
	if retained, err := store.RetainsArtifacts(context.Background(), scope, id, ids); err != nil || !retained[artifactID] {
		t.Fatalf("live presentation lost its current artifact: %v, %v", retained, err)
	}
	advance(2 * time.Second)
	if retained, err := store.RetainsArtifacts(context.Background(), scope, id, ids); retained != nil || !errors.Is(err, ErrClosed) {
		t.Fatalf("background batch renewed an idle consumer: %v, %v", retained, err)
	}
	if usage := store.Usage(); usage.Windows != 0 || usage.Bytes != 0 {
		t.Fatalf("idle presentation retained its storage: %+v", usage)
	}
	if retained, err := store.RetainsArtifacts(context.Background(), scope, "missing", ids); retained != nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing presentation returned artifact facts: %v, %v", retained, err)
	}
	if err := store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if retained, err := store.RetainsArtifacts(context.Background(), scope, id, ids); retained != nil || !errors.Is(err, ErrClosed) {
		t.Fatalf("closed store returned artifact facts: %v, %v", retained, err)
	}
}

func TestRetainsArtifactsEmptyBatchDoesNotExpireOrRenew(t *testing.T) {
	var observations atomic.Int64
	store, advance := testStore(t, func(options *Options) {
		now := options.Now
		options.Now = func() time.Time {
			observations.Add(1)
			return now()
		}
	})
	scope := testScope()
	id := testWindow(t, store, scope, 30*time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
	published := testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "media"))
	advance(31 * time.Second)
	before := store.Usage()
	store.mu.Lock()
	accessed := store.windows[id].accessed
	store.mu.Unlock()
	observations.Store(0)
	if retained, err := store.RetainsArtifacts(context.Background(), scope, id, nil); retained != nil || err != nil {
		t.Fatalf("empty batch did not remain a no-op: %v, %v", retained, err)
	}
	if observations.Load() != 0 || store.Usage() != before {
		t.Fatal("empty batch observed or expired the presentation")
	}
	store.mu.Lock()
	afterAccess := store.windows[id].accessed
	store.mu.Unlock()
	if !afterAccess.Equal(accessed) {
		t.Fatal("empty batch renewed the consumer lease")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if retained, err := store.RetainsArtifacts(ctx, scope, id, nil); retained != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("empty batch ignored cancellation: %v, %v", retained, err)
	}
	artifactID := published.Segments[0].Artifacts[0].ID
	if retained, err := store.RetainsArtifacts(context.Background(), scope, id, []string{artifactID}); err != nil || retained[artifactID] {
		t.Fatalf("nonempty batch omitted the actual expiry pass: %v, %v", retained, err)
	}
}

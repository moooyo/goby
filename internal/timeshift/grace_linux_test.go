package timeshift

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func testAdvertisedWindow(t *testing.T, store *Store, scope Scope) string {
	t.Helper()
	window, err := store.Create(context.Background(), scope, WindowOptions{Window: 30 * time.Second, TargetDurationTicks: 2 * TicksPerSecond,
		Variants: []Variant{{ID: "video", Kind: "video", Format: "fmp4"}}})
	if err != nil {
		t.Fatal(err)
	}
	return window.PresentationID
}

func TestAdvertisedRemovedMediaAndInitializationRemainReadableThroughGrace(t *testing.T) {
	store, advance := testStore(t, func(options *Options) { options.MaxSegments = 3 })
	scope := testScope()
	id := testAdvertisedWindow(t, store, scope)
	first := testPublish(t, store, scope, id, 1, 2, []ArtifactInput{testInput(t, "video", "old-init")}, testInput(t, "video", "old-media"))
	oldMedia, oldInit := first.Segments[0].Artifacts[0].ID, first.Segments[0].Artifacts[0].InitID
	testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "old-second"))
	third := testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "old-third"))
	if err := store.Advertise(context.Background(), scope, id, third.Revision); err != nil {
		t.Fatal(err)
	}
	testPublish(t, store, scope, id, 2, 2, []ArtifactInput{testInput(t, "video", "new-init")}, testInput(t, "video", "new-first"))
	testPublish(t, store, scope, id, 2, 2, nil, testInput(t, "video", "new-second"))
	latest := testPublish(t, store, scope, id, 2, 2, nil, testInput(t, "video", "new-third"))
	if len(latest.Epochs) != 1 || latest.Epochs[0].Generation != 2 || latest.EarliestTicks != 6*TicksPerSecond || store.Usage().Artifacts != 8 {
		t.Fatal("unlisted grace artifacts were discarded or remained in the seekable snapshot")
	}
	if _, err := store.Seek(context.Background(), scope, id, 0); !errors.Is(err, ErrWindowExpired) {
		t.Fatal("grace availability restored an expired seek position")
	}
	var handles []*ReadHandle
	for _, artifactID := range []string{oldMedia, oldInit} {
		artifact, variant, err := store.ResolveArtifact(context.Background(), scope, id, artifactID)
		if err != nil || variant.Format != "fmp4" || artifact.Initialization != (artifactID == oldInit) {
			t.Fatal("grace lookup lost the canonical artifact kind")
		}
		handle, err := store.OpenArtifact(context.Background(), scope, id, artifactID)
		if err != nil {
			t.Fatal(err)
		}
		handles = append(handles, handle)
	}
	advance(7 * time.Second)
	if _, _, err := store.ResolveArtifact(context.Background(), scope, id, oldMedia); err != nil {
		t.Fatal("media disappeared before playlist duration plus segment duration elapsed")
	}
	advance(time.Second)
	if _, _, err := store.ResolveArtifact(context.Background(), scope, id, oldMedia); !errors.Is(err, ErrNotFound) {
		t.Fatal("a grace-expired artifact admitted a new lookup")
	}
	if _, err := store.OpenArtifact(context.Background(), scope, id, oldInit); !errors.Is(err, ErrNotFound) {
		t.Fatal("a grace-expired initialization admitted a new reader")
	}
	if store.Usage().Artifacts != 6 || store.Usage().Bytes != 6*store.storage.unit {
		t.Fatal("expired-but-open grace files escaped physical accounting")
	}
	for _, handle := range handles {
		if data, err := io.ReadAll(handle); err != nil || len(data) == 0 {
			t.Fatal("a bounded existing grace reader was invalidated early")
		}
		_ = handle.Close()
	}
	if store.Usage().Artifacts != 4 || store.Usage().Bytes != 4*store.storage.unit {
		t.Fatal("closing expired grace readers did not reclaim their charged files")
	}
}

func TestAdvertisementRechecksRevisionAndUsesLongestPublishedPlaylist(t *testing.T) {
	store, advance := testStore(t, func(options *Options) { options.MaxSegments = 5 })
	scope := testScope()
	id := testAdvertisedWindow(t, store, scope)
	first := testPublish(t, store, scope, id, 1, 2, []ArtifactInput{testInput(t, "video", "init")}, testInput(t, "video", "first"))
	if err := store.Advertise(context.Background(), scope, id, first.Revision); !errors.Is(err, ErrNotBuffered) {
		t.Fatal("an unready live window was advertised inside its target-duration margin")
	}
	testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "second"))
	third := testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "third"))
	if err := store.Advertise(context.Background(), scope, id, first.Revision); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatal("an obsolete rendered snapshot could be advertised")
	}
	if err := store.Advertise(context.Background(), scope, id, third.Revision); err != nil {
		t.Fatal(err)
	}
	fourth := testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "fourth"))
	if err := store.Advertise(context.Background(), scope, id, fourth.Revision); err != nil {
		t.Fatal(err)
	}
	testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "fifth"))
	advance(time.Second)
	testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "sixth"))
	oldMedia := first.Segments[0].Artifacts[0].ID
	advance(9 * time.Second)
	if _, _, err := store.ResolveArtifact(context.Background(), scope, id, oldMedia); err != nil {
		t.Fatal("grace was based on a shorter playlist or started at advertisement instead of URI removal")
	}
	advance(time.Second)
	if _, _, err := store.ResolveArtifact(context.Background(), scope, id, oldMedia); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired longest-playlist grace was retained indefinitely")
	}
}

func TestGraceQuotaCannotBeReclaimedByHidingAdvertisedMedia(t *testing.T) {
	store, _ := testStore(t, nil)
	scope := testScope()
	id := testAdvertisedWindow(t, store, scope)
	testPublish(t, store, scope, id, 1, 2, []ArtifactInput{testInput(t, "video", "init")}, testInput(t, "video", "first"))
	testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "second"))
	third := testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "third"))
	if err := store.Advertise(context.Background(), scope, id, third.Revision); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.windows[id].options.MaxBytes = 4 * store.storage.unit
	store.mu.Unlock()
	if _, err := store.Publish(context.Background(), scope, id, Publication{Generation: 1, DurationTicks: 2 * TicksPerSecond, Segments: []ArtifactInput{testInput(t, "video", "over-budget")}}); !errors.Is(err, ErrQuota) {
		t.Fatal("advertised grace was deleted to make room for a new interval")
	}
	retained, err := store.Snapshot(context.Background(), scope, id)
	if err != nil || len(retained.Segments) != 3 || retained.EarliestTicks != third.EarliestTicks || store.Usage().Bytes != 4*store.storage.unit || store.Usage().PendingBytes != 0 {
		t.Fatal("an impossible reservation unnecessarily hid or discarded the advertised window")
	}
	if err := store.ClosePresentation(context.Background(), scope, id); err != nil {
		t.Fatal(err)
	}
	if store.Usage().Bytes != 0 || store.Usage().Artifacts != 0 {
		t.Fatal("revocation allowed previously advertised media to outlive its policy")
	}
}

func TestFixedTargetAndVariableDurationSafeLiveStart(t *testing.T) {
	store, _ := testStore(t, nil)
	scope := testScope()
	window, err := store.Create(context.Background(), scope, WindowOptions{Window: 30 * time.Second, TargetDurationTicks: 4 * TicksPerSecond, Variants: []Variant{{ID: "video", Kind: "video", Format: "ts"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := window.PresentationID
	durations := []int64{4 * TicksPerSecond, 4 * TicksPerSecond, 2 * TicksPerSecond, 2 * TicksPerSecond, 2 * TicksPerSecond}
	var snapshot WindowSnapshot
	for _, duration := range durations {
		snapshot, err = store.Publish(context.Background(), scope, id, Publication{Generation: 1, DurationTicks: duration, Segments: []ArtifactInput{testInput(t, "video", "observed")}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if snapshot.TargetDurationTicks != 4*TicksPerSecond || snapshot.LiveEdgeTicks != 14*TicksPerSecond || snapshot.LiveStartTicks != 0 || snapshot.LiveEdgeTicks-snapshot.LiveStartTicks < 3*snapshot.TargetDurationTicks {
		t.Fatal("safe live start counted segments instead of accumulated durations")
	}
	if _, err := store.Publish(context.Background(), scope, id, Publication{Generation: 1, DurationTicks: 45 * TicksPerSecond / 10, Segments: []ArtifactInput{testInput(t, "video", "too-long")}}); !errors.Is(err, ErrInvalid) {
		t.Fatal("a duration rounding beyond the fixed target was accepted")
	}
	if _, err := store.Publish(context.Background(), scope, id, Publication{Generation: 1, DurationTicks: 45*TicksPerSecond/10 - 1, Segments: []ArtifactInput{testInput(t, "video", "legal-rounding")}}); err != nil {
		t.Fatal("a duration within the RFC nearest-integer target rule was rejected")
	}
	plain := testWindow(t, store, Scope{UserID: "other", AuthSessionID: "auth", PlaySessionID: "play", ItemID: "item", SourceID: "source"}, time.Minute, Variant{ID: "video", Kind: "video", Format: "ts"})
	plainScope := Scope{UserID: "other", AuthSessionID: "auth", PlaySessionID: "play", ItemID: "item", SourceID: "source"}
	generic := testPublish(t, store, plainScope, plain, 1, 1, nil, testInput(t, "video", "generic"))
	if err := store.Advertise(context.Background(), plainScope, plain, generic.Revision); !errors.Is(err, ErrInvalid) {
		t.Fatal("a generic window without a fixed target advertised an HLS contract")
	}
}

func TestAdvertisedWindowSlidesBeyondFiveRetentionPeriodsWithoutQuotaGrowth(t *testing.T) {
	store, advance := testStore(t, nil)
	scope := testScope()
	const windowDuration = 30 * time.Second
	limit := 24 * store.storage.unit
	window, err := store.Create(context.Background(), scope, WindowOptions{Window: windowDuration, MaxBytes: limit, TargetDurationTicks: 2 * TicksPerSecond,
		Variants: []Variant{{ID: "video", Kind: "video", Format: "fmp4"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := window.PresentationID
	mediaInput, initInput := testInput(t, "video", "steady media"), testInput(t, "video", "steady initialization")
	var last WindowSnapshot
	var previousEarliest int64
	var sawGrace bool
	for index := 0; index < 100; index++ {
		advance(2 * time.Second)
		publication := Publication{Generation: 1 + uint64(index/20), DurationTicks: 2 * TicksPerSecond, Segments: []ArtifactInput{mediaInput}}
		if index%20 == 0 {
			publication.Initializations = []ArtifactInput{initInput}
		}
		last, err = store.Publish(context.Background(), scope, id, publication)
		if err != nil {
			t.Fatalf("steady publication %d stopped inside its configured budget: %v", index, err)
		}
		if index >= 2 {
			if err := store.Advertise(context.Background(), scope, id, last.Revision); err != nil {
				t.Fatalf("steady playlist %d could not advertise: %v", index, err)
			}
			store.mu.Lock()
			state := store.windows[id]
			active, physical := state.activeBytes, state.bytes
			for _, artifact := range state.artifacts {
				if !artifact.visible && store.options.Now().Before(artifact.graceUntil) {
					sawGrace = true
				}
			}
			store.mu.Unlock()
			if active > limit/3 || physical > limit || last.Bytes > limit {
				t.Fatalf("steady window escaped active or retained storage bounds: active=%d physical=%d limit=%d", active, physical, limit)
			}
			if last.LiveEdgeTicks-last.EarliestTicks < 3*last.TargetDurationTicks {
				t.Fatal("headroom was obtained by advertising an unsafe short live playlist")
			}
		}
		usage := store.Usage()
		if usage.Bytes+usage.PendingBytes > store.options.MaxBytes || usage.PendingBytes != 0 || usage.Publishing != 0 {
			t.Fatal("steady publication leaked a reservation or exceeded the global hard quota")
		}
		if last.EarliestTicks < previousEarliest {
			t.Fatal("a sliding window moved its earliest retained time backwards")
		}
		previousEarliest = last.EarliestTicks
	}
	if last.LiveEdgeTicks < 5*int64(windowDuration/(100*time.Nanosecond)) || last.NextSequence != 100 || last.EarliestTicks == 0 || !sawGrace {
		t.Fatal("the scenario did not exercise sustained sliding, reconnects and advertised grace")
	}
}

func TestFirstAdvertisementReservesHeadroomBeforeCreatingItsPromise(t *testing.T) {
	store, _ := testStore(t, nil)
	scope := testScope()
	limit := 24 * store.storage.unit
	window, err := store.Create(context.Background(), scope, WindowOptions{Window: 30 * time.Second, MaxBytes: limit, TargetDurationTicks: 2 * TicksPerSecond,
		Variants: []Variant{{ID: "video", Kind: "video", Format: "fmp4"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := window.PresentationID
	mediaInput, initInput := testInput(t, "video", "media"), testInput(t, "video", "initialization")
	var snapshot WindowSnapshot
	for index := 0; index < 12; index++ {
		initializations := []ArtifactInput(nil)
		if index == 0 {
			initializations = []ArtifactInput{initInput}
		}
		snapshot = testPublish(t, store, scope, id, 1, 2, initializations, mediaInput)
	}
	if len(snapshot.Segments) != 12 {
		t.Fatal("private unadvertised storage was prematurely reduced to the HLS active budget")
	}
	if err := store.Advertise(context.Background(), scope, id, snapshot.Revision); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatal("a too-large private snapshot created a public retention promise before reserving headroom")
	}
	current, err := store.Snapshot(context.Background(), scope, id)
	if err != nil || len(current.Segments) != 7 || current.Bytes != 8*store.storage.unit {
		t.Fatal("first advertisement did not trim only the unadvertised prefix to its active budget")
	}
	if err := store.Advertise(context.Background(), scope, id, current.Revision); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, segment := range store.windows[id].segments {
		if segment.advertisedDuration != 14*TicksPerSecond {
			t.Fatal("the never-sent private playlist duration contaminated the published promise")
		}
	}
}

func TestEmptyEndedAdvertisementKeepsItsMonotonicNextSequence(t *testing.T) {
	store, advance := testStore(t, nil)
	scope := testScope()
	id := testAdvertisedWindow(t, store, scope)
	first := testPublish(t, store, scope, id, 1, 2, []ArtifactInput{testInput(t, "video", "init")}, testInput(t, "video", "short finite media"))
	if err := store.SetState(context.Background(), scope, id, State{Ended: true}); err != nil {
		t.Fatal(err)
	}
	ended, err := store.Snapshot(context.Background(), scope, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Advertise(context.Background(), scope, id, ended.Revision); err != nil {
		t.Fatal("a short completed presentation could not advertise ENDLIST")
	}
	advance(31 * time.Second)
	empty, err := store.Snapshot(context.Background(), scope, id)
	if err != nil || !empty.Ended || len(empty.Segments) != 0 || empty.NextSequence != first.NextSequence || empty.NextSequence != 1 {
		t.Fatal("expired terminal media lost the next global sequence")
	}
	if err := store.Advertise(context.Background(), scope, id, empty.Revision); err != nil {
		t.Fatal("an empty terminal snapshot could not advertise its final ENDLIST")
	}
}

func TestBackgroundRetentionChecksDoNotRenewTheConsumerLease(t *testing.T) {
	store, advance := testStore(t, func(options *Options) { options.MaxSegments = 3; options.IdleTimeout = 5 * time.Second })
	scope := testScope()
	id := testAdvertisedWindow(t, store, scope)
	first := testPublish(t, store, scope, id, 1, 2, []ArtifactInput{testInput(t, "video", "init")}, testInput(t, "video", "first"))
	testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "second"))
	third := testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "third"))
	if err := store.Advertise(context.Background(), scope, id, third.Revision); err != nil {
		t.Fatal(err)
	}
	testPublish(t, store, scope, id, 1, 2, nil, testInput(t, "video", "fourth"))
	oldID := first.Segments[0].Artifacts[0].ID
	advance(3 * time.Second)
	if retained, err := store.RetainsArtifact(context.Background(), scope, id, oldID); err != nil || !retained {
		t.Fatal("background pruning did not preserve advertised grace")
	}
	if retained, err := store.RetainsArtifact(context.Background(), scope, id, "unknown"); err != nil || retained {
		t.Fatal("missing background retention did not return a clean negative result")
	}
	advance(3 * time.Second)
	if retained, err := store.RetainsArtifact(context.Background(), scope, id, oldID); retained || !errors.Is(err, ErrClosed) {
		t.Fatal("background retention checks kept an abandoned consumer alive")
	}
	if store.Usage().Windows != 0 || store.Usage().Bytes != 0 {
		t.Fatal("idle expiration retained previously advertised media")
	}
}

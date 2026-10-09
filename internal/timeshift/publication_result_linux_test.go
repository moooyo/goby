package timeshift

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublishLatestOwnsArtifactsAcrossGenerationsAndRetention(t *testing.T) {
	store, _ := testStore(t, nil)
	scope := testScope()
	id := testWindow(t, store, scope, 4*time.Second, Variant{ID: "video", Kind: "video", Format: "fmp4"}, Variant{ID: "captions", Kind: "subtitle", Format: "vtt"})
	var firstInitialization string
	var previous PublicationResult
	for index, generation := range []uint64{1, 1, 4} {
		publication := Publication{Generation: generation, DurationTicks: 2 * TicksPerSecond,
			Segments: []ArtifactInput{testInput(t, "video", "media"), testInput(t, "captions", "WEBVTT\n\n")}}
		if index != 1 {
			publication.Initializations = []ArtifactInput{testInput(t, "video", "initialization")}
		}
		result, err := store.PublishLatest(context.Background(), scope, id, publication)
		if err != nil || !result.HasLatest {
			t.Fatalf("publication %d did not return retained media: %+v, %v", index, result, err)
		}
		latest := result.Latest
		if latest.Sequence != uint64(index) || latest.Generation != generation || latest.StartTicks != int64(index)*2*TicksPerSecond ||
			latest.DurationTicks != 2*TicksPerSecond || latest.Discontinuity != (index == 2) || latest.DiscontinuitySequence != uint64(index/2) || len(latest.Artifacts) != 2 {
			t.Fatalf("publication lost observed timing, variants or epoch metadata: %+v", latest)
		}
		if index == 0 {
			firstInitialization = latest.Artifacts[0].InitID
		}
		if firstInitialization == "" || index == 1 && latest.Artifacts[0].InitID != firstInitialization || index == 2 && latest.Artifacts[0].InitID == firstInitialization {
			t.Fatal("compact publication lost epoch-specific initialization")
		}
		snapshot, err := store.Snapshot(context.Background(), scope, id)
		if err != nil || len(snapshot.Segments) == 0 || !reflect.DeepEqual(latest, snapshot.Segments[len(snapshot.Segments)-1]) {
			t.Fatalf("compact result disagrees with committed media: %+v, %v", snapshot, err)
		}
		if index == 2 && (len(snapshot.Segments) != 2 || snapshot.Segments[0].Sequence != 1 || len(snapshot.Epochs) != 2 || snapshot.Revision != 5) {
			t.Fatalf("compact publication changed retention or revision accounting: %+v", snapshot)
		}
		if index > 0 && previous.Latest.Artifacts[0].InitID != firstInitialization {
			t.Fatal("a successor publication changed a previously returned artifact")
		}
		previous = result
	}

	original := previous.Latest.Artifacts[0]
	previous.Latest.Artifacts[0].ID = "caller-owned"
	previous.Latest.Artifacts[0].InitID = "caller-owned-init"
	snapshot, err := store.Snapshot(context.Background(), scope, id)
	if err != nil || len(snapshot.Segments) == 0 || snapshot.Segments[len(snapshot.Segments)-1].Artifacts[0] != original {
		t.Fatalf("changing the compact result mutated store artifacts: %+v, %v", snapshot, err)
	}
	full := testPublish(t, store, scope, id, 4, 2, nil, testInput(t, "video", "next"), testInput(t, "captions", "WEBVTT\n\nnext"))
	if len(full.Segments) != 2 || full.Segments[0].Sequence != 2 || full.Segments[1].Sequence != 3 || len(full.Epochs) != 1 || full.Epochs[0].Generation != 4 {
		t.Fatalf("full publication no longer returns the complete retained history: %+v", full)
	}
}

func TestPublicationResultsSelectAfterExpiration(t *testing.T) {
	for _, full := range []bool{false, true} {
		name := "latest"
		if full {
			name = "snapshot"
		}
		t.Run(name, func(t *testing.T) {
			var copied atomic.Bool
			var observations atomic.Int32
			store, _ := testStore(t, func(options *Options) {
				now := options.Now
				options.Now = func() time.Time {
					current := now()
					// The first post-copy read timestamps the new segment. The
					// next read expires it before the publication result is made.
					if copied.Load() && observations.Add(1) >= 2 {
						return current.Add(31 * time.Second)
					}
					return current
				}
			})
			scope := testScope()
			id := testWindow(t, store, scope, 30*time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
			copyArtifact := store.copyArtifact
			store.copyArtifact = func(ctx context.Context, window *storageWindow, id string, input *os.File, before os.FileInfo) (int64, error) {
				charge, err := copyArtifact(ctx, window, id, input, before)
				copied.Store(err == nil)
				return charge, err
			}
			publication := Publication{Generation: 1, DurationTicks: 2 * TicksPerSecond, Segments: []ArtifactInput{testInput(t, "video", "media")}}
			if full {
				snapshot, err := store.Publish(context.Background(), scope, id, publication)
				if err != nil || len(snapshot.Segments) != 0 || snapshot.NextSequence != 1 || snapshot.Revision != 3 {
					t.Fatalf("full result preceded final expiration: %+v, %v", snapshot, err)
				}
			} else {
				result, err := store.PublishLatest(context.Background(), scope, id, publication)
				if err != nil || result.HasLatest || !reflect.DeepEqual(result.Latest, Segment{}) {
					t.Fatalf("compact result exposed media removed by final expiration: %+v, %v", result, err)
				}
			}
			snapshot, err := store.Snapshot(context.Background(), scope, id)
			if err != nil || len(snapshot.Segments) != 0 || snapshot.NextSequence != 1 || snapshot.LiveEdgeTicks != 2*TicksPerSecond || snapshot.Bytes != 0 {
				t.Fatalf("empty publication lost its committed timeline or retained expired bytes: %+v, %v", snapshot, err)
			}
		})
	}
}

func TestPublishLatestPreservesScopeGenerationAndFailureChecks(t *testing.T) {
	store, _ := testStore(t, nil)
	scope := testScope()
	id := testWindow(t, store, scope, 30*time.Second, Variant{ID: "video", Kind: "video", Format: "ts"})
	publication := Publication{Generation: 5, DurationTicks: 2 * TicksPerSecond, Segments: []ArtifactInput{testInput(t, "video", "media")}}
	foreign := scope
	foreign.UserID = "other-user"
	result, err := store.PublishLatest(context.Background(), foreign, id, publication)
	if !errors.Is(err, ErrNotFound) || result.HasLatest {
		t.Fatalf("compact publication accepted a foreign scope: %+v, %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := store.PublishLatest(ctx, scope, id, publication); !errors.Is(err, context.Canceled) || result.HasLatest {
		t.Fatalf("compact publication ignored cancellation: %+v, %v", result, err)
	}
	if result, err := store.PublishLatest(context.Background(), scope, id, publication); err != nil || !result.HasLatest {
		t.Fatalf("initial compact publication failed: %+v, %v", result, err)
	}
	publication.Generation = 4
	if result, err := store.PublishLatest(context.Background(), scope, id, publication); !errors.Is(err, ErrInvalid) || result.HasLatest {
		t.Fatalf("compact publication accepted an older generation: %+v, %v", result, err)
	}
	publication.Generation = 5
	store.copyArtifact = func(context.Context, *storageWindow, string, *os.File, os.FileInfo) (int64, error) {
		return 0, context.Canceled
	}
	if result, err := store.PublishLatest(context.Background(), scope, id, publication); !errors.Is(err, context.Canceled) || result.HasLatest {
		t.Fatalf("failed compact publication returned an existing segment: %+v, %v", result, err)
	}
	snapshot, err := store.Snapshot(context.Background(), scope, id)
	if err != nil || len(snapshot.Segments) != 1 || snapshot.NextSequence != 1 || snapshot.PendingBytes != 0 || store.Usage().Publishing != 0 {
		t.Fatalf("failed compact publication changed committed state or leaked its reservation: %+v, %v", snapshot, err)
	}
	if err := store.SetState(context.Background(), scope, id, State{Ended: true}); err != nil {
		t.Fatal(err)
	}
	if result, err := store.PublishLatest(context.Background(), scope, id, publication); !errors.Is(err, ErrEnded) || result.HasLatest {
		t.Fatalf("compact publication resumed an ended presentation: %+v, %v", result, err)
	}
}

var publicationBenchmarkSnapshot WindowSnapshot
var publicationBenchmarkLatest PublicationResult

// This isolates result construction while holding the store mutex. It excludes
// file copies and expiration, which retain their existing publication costs.
func BenchmarkPublicationResultCopy(b *testing.B) {
	for _, size := range []int{0, 1, 64, 512, 2048} {
		for _, epochCount := range []int{1, 8} {
			if epochCount > max(1, size) {
				continue
			}
			window := &windowState{id: "window", revision: 1, nextSequence: uint64(size), liveEdge: int64(size) * 2 * TicksPerSecond,
				options: WindowOptions{TargetDurationTicks: 2 * TicksPerSecond, Variants: []Variant{{ID: "video", Kind: "video", Format: "fmp4"}}}}
			if size == 0 {
				// Expiration keeps the current epoch's initialization for the
				// next publication, even after its final segment is removed.
				window.generation = 1
				window.epochs = []*epochState{{epoch: Epoch{Generation: 1, Initializations: []Artifact{{ID: "init-1", VariantID: "video", Initialization: true}}}}}
			}
			for index := 0; index < size; index++ {
				generation := uint64(index*epochCount/size + 1)
				initialization := Artifact{ID: fmt.Sprintf("init-%d", generation), VariantID: "video", Initialization: true}
				if index == 0 || generation != window.segments[index-1].segment.Generation {
					window.epochs = append(window.epochs, &epochState{epoch: Epoch{Generation: generation, FirstSequence: uint64(index), Initializations: []Artifact{initialization}}})
				}
				window.segments = append(window.segments, &segmentState{segment: Segment{Sequence: uint64(index), Generation: generation,
					StartTicks: int64(index) * 2 * TicksPerSecond, DurationTicks: 2 * TicksPerSecond,
					Artifacts: []Artifact{{ID: fmt.Sprintf("media-%d", index), InitID: initialization.ID, VariantID: "video", Size: 4096}}}})
			}
			name := fmt.Sprintf("segments_%d/epochs_%d", size, epochCount)
			b.Run(name+"/latest", func(b *testing.B) {
				var store Store
				b.ReportAllocs()
				for b.Loop() {
					store.mu.Lock()
					publicationBenchmarkLatest = latestPublicationLocked(window)
					store.mu.Unlock()
				}
			})
			b.Run(name+"/subtitle_snapshot", func(b *testing.B) {
				var store Store
				b.ReportAllocs()
				for b.Loop() {
					store.mu.Lock()
					publicationBenchmarkSnapshot = snapshotLocked(window)
					store.mu.Unlock()
				}
			})
		}
	}
}

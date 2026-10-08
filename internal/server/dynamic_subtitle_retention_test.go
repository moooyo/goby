package server

import (
	"context"
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/timeshift"
)

func TestDynamicSubtitleRetentionBatchesOnlyOldBoundCoordinates(t *testing.T) {
	for _, empty := range []bool{false, true} {
		name := "current_snapshot"
		if empty {
			name = "empty_snapshot"
		}
		t.Run(name, func(t *testing.T) {
			second := media.TicksPerSecond
			runtime := &dynamicSubtitleRuntime{current: 1, media: map[uint64]dynamicSegmentClock{
				0: {Generation: 1, StartTicks: 0, EndTicks: 2 * second, artifactID: "retained"},
				1: {Generation: 1, StartTicks: 2 * second, EndTicks: 4 * second, artifactID: "expired"},
				2: {Generation: 1, StartTicks: 4 * second, EndTicks: 6 * second},
				3: {Generation: 1, StartTicks: 6 * second, EndTicks: 8 * second, artifactID: "current"},
				4: {Generation: 1, StartTicks: 8 * second, EndTicks: 10 * second, artifactID: "successor"},
			}}
			snapshot := dynamicSubtitleTestSnapshot(3, 1, 6*second, 2*second, "current")
			if empty {
				snapshot.Segments = nil
				snapshot.NextSequence = 3
			}
			calls := 0
			err := runtime.retain(snapshot, func(ids []string) (map[string]bool, error) {
				calls++
				seen := make(map[string]int)
				for _, id := range ids {
					seen[id]++
				}
				if len(ids) != 2 || seen["retained"] != 1 || seen["expired"] != 1 {
					t.Fatalf("batch included a current, successor or unbound coordinate: %v", ids)
				}
				return map[string]bool{"retained": true}, nil
			})
			if err != nil || calls != 1 {
				t.Fatalf("old coordinates did not share one lookup: calls=%d error=%v", calls, err)
			}
			for sequence, want := range map[uint64]bool{0: true, 1: false, 2: false, 3: true, 4: true} {
				if _, exists := runtime.media[sequence]; exists != want {
					t.Fatalf("coordinate %d retention=%v, want %v", sequence, exists, want)
				}
			}
		})
	}
}

func TestDynamicSubtitleRetentionSkipsEmptyArtifactBatch(t *testing.T) {
	second := media.TicksPerSecond
	runtime := &dynamicSubtitleRuntime{current: 1, media: map[uint64]dynamicSegmentClock{
		0: {Generation: 1, StartTicks: 0, EndTicks: 2 * second},
		1: {Generation: 1, StartTicks: 2 * second, EndTicks: 4 * second, artifactID: "current"},
		2: {Generation: 1, StartTicks: 4 * second, EndTicks: 6 * second, artifactID: "successor"},
	}}
	snapshot := dynamicSubtitleTestSnapshot(1, 1, 2*second, 2*second, "current")
	if err := runtime.retain(snapshot, func([]string) (map[string]bool, error) {
		t.Fatal("a snapshot without old bound artifacts performed a storage observation")
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, exists := runtime.media[0]; exists || len(runtime.media) != 2 {
		t.Fatal("empty batch changed unbound cleanup or successor retention")
	}
}

func TestDynamicSubtitleRetentionKeepsInconclusiveArtifacts(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		keep bool
	}{
		{"success", nil, false},
		{"cancelled", context.Canceled, true},
		{"deadline", context.DeadlineExceeded, true},
		{"storage", timeshift.ErrStorage, true},
		{"invalid", timeshift.ErrInvalid, true},
		{"missing", timeshift.ErrNotFound, false},
		{"closed", timeshift.ErrClosed, false},
		{"wrapped_closed", errors.Join(timeshift.ErrClosed), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			second := media.TicksPerSecond
			runtime := &dynamicSubtitleRuntime{current: 1, media: map[uint64]dynamicSegmentClock{
				0: {Generation: 1, StartTicks: 0, EndTicks: 2 * second, artifactID: "retained"},
				1: {Generation: 1, StartTicks: 2 * second, EndTicks: 4 * second, artifactID: "unknown"},
				2: {Generation: 1, StartTicks: 4 * second, EndTicks: 6 * second, artifactID: "current"},
			}}
			snapshot := dynamicSubtitleTestSnapshot(2, 1, 4*second, 2*second, "current")
			if err := runtime.retain(snapshot, func([]string) (map[string]bool, error) {
				return map[string]bool{"retained": true}, test.err
			}); err != nil {
				t.Fatal(err)
			}
			if _, exists := runtime.media[0]; !exists {
				t.Fatal("a confirmed retained artifact was discarded")
			}
			if _, exists := runtime.media[1]; exists != test.keep {
				t.Fatalf("inconclusive artifact retention=%v, want %v", exists, test.keep)
			}
			if _, exists := runtime.media[2]; !exists {
				t.Fatal("a batch failure discarded the current snapshot")
			}
		})
	}
}

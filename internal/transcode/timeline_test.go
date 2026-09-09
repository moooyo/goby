package transcode

import (
	"errors"
	"reflect"
	"testing"
)

func TestBuildTimelineIrregularKeyframesAndAudioLeadIn(t *testing.T) {
	keys := []int64{213_330, 10_000_000, 25_000_000, 32_500_000, 80_000_000, 82_500_000, 110_000_000}
	timeline, err := BuildTimeline(122_500_000, 3, keys, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []TimelineSegment{
		{Number: 0, StartTicks: 0, DurationTicks: 32_500_000},
		{Number: 1, StartTicks: 32_500_000, DurationTicks: 47_500_000},
		{Number: 2, StartTicks: 80_000_000, DurationTicks: 30_000_000},
		{Number: 3, StartTicks: 110_000_000, DurationTicks: 12_500_000},
	}
	if !reflect.DeepEqual(timeline.Segments, want) || timeline.TargetDuration != 5 || keys[0] != 213_330 {
		t.Fatalf("unexpected timeline or changed input: %+v, keys=%v", timeline, keys)
	}
	for _, check := range []struct {
		position int64
		number   int
		found    bool
	}{
		{-1, 0, false}, {0, 0, true}, {32_499_999, 0, true}, {32_500_000, 1, true},
		{122_499_999, 3, true}, {122_500_000, 0, false}, {999_999_999, 0, false},
	} {
		segment, found := timeline.SegmentAt(check.position)
		if found != check.found || (found && segment.Number != check.number) {
			t.Errorf("position %d: got %+v, %t", check.position, segment, found)
		}
	}
	cuts, err := timeline.BoundaryTicks(1, 3)
	if err != nil || !reflect.DeepEqual(cuts, []int64{80_000_000, 110_000_000}) {
		t.Fatalf("window cuts: %v, %v", cuts, err)
	}
	if cuts, err := timeline.BoundaryTicks(3, 3); err != nil || len(cuts) != 0 {
		t.Fatalf("single segment has no internal cuts: %v, %v", cuts, err)
	}
	for _, bounds := range [][2]int{{-1, 1}, {2, 1}, {0, 4}} {
		if _, err := timeline.BoundaryTicks(bounds[0], bounds[1]); !errors.Is(err, ErrInvalidTimeline) {
			t.Errorf("invalid range %v: %v", bounds, err)
		}
	}
}

func TestBuildTimelineNominalCoverageAndLimits(t *testing.T) {
	for _, duration := range []int64{1, 30_000_000, 30_000_001, 49_151 * ticksPerSecond} {
		timeline, err := BuildTimeline(duration, 3, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		var total int64
		for number, segment := range timeline.Segments {
			if segment.Number != number || segment.StartTicks != total || segment.DurationTicks <= 0 || segment.DurationTicks > 3*ticksPerSecond {
				t.Fatalf("invalid segment: %+v", segment)
			}
			total += segment.DurationTicks
		}
		if total != duration || len(timeline.Segments) > MaxTimelineSegments {
			t.Fatalf("coverage: %d != %d", total, duration)
		}
	}
	if _, err := BuildTimeline(49_153*ticksPerSecond, 3, nil, false); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("nominal segment limit: %v", err)
	}
	keys := make([]int64, MaxTimelineSegments+1)
	for index := range keys {
		keys[index] = int64(index) * ticksPerSecond
	}
	if _, err := BuildTimeline(int64(len(keys))*ticksPerSecond, 1, keys, true); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("copied segment limit: %v", err)
	}
	if _, err := BuildTimeline(10*ticksPerSecond, 3, []int64{0}, true); err != nil {
		t.Fatalf("one long GOP should remain one segment: %v", err)
	}
	for _, keys := range [][]int64{nil, {-1}, {0, 0}, {1, 0}, {0, 100_000_000}} {
		if _, err := BuildTimeline(100_000_000, 3, keys, true); !errors.Is(err, ErrUnsupportedTimeline) {
			t.Errorf("invalid keys %v: %v", keys, err)
		}
	}
	for _, duration := range []int64{-1, 0, maxDurationTicks + 1} {
		if _, err := BuildTimeline(duration, 3, nil, false); !errors.Is(err, ErrInvalidTimeline) {
			t.Errorf("invalid duration %d: %v", duration, err)
		}
	}
	for _, seconds := range []int{0, 11} {
		if _, err := BuildTimeline(10, seconds, nil, false); !errors.Is(err, ErrInvalidTimeline) {
			t.Errorf("invalid segment duration %d: %v", seconds, err)
		}
	}
	if _, found := (Timeline{}).SegmentAt(0); found {
		t.Fatal("empty timeline has a segment")
	}
}

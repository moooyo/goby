package transcode

import (
	"errors"
	"fmt"
	"sort"
)

var (
	ErrInvalidTimeline     = errors.New("invalid media timeline")
	ErrUnsupportedTimeline = errors.New("media timestamps do not support a stable VOD timeline")
	ErrTimelineLimit       = errors.New("media timeline exceeds its resource limit")
	ErrTimelineProbe       = errors.New("media timeline probe failed")
)

const (
	MaxTimelineSegments = 16_384
	maxTimelineKeys     = 262_144
)

// Timeline uses the source format's presentation origin, not a transport's
// nonzero PTS origin. Every segment covers [StartTicks, StartTicks+DurationTicks).
// Segment numbers remain stable when a producer begins at a later segment.
type Timeline struct {
	Segments       []TimelineSegment
	TargetDuration int
}

type TimelineSegment struct {
	Number        int
	StartTicks    int64
	DurationTicks int64
}

// BuildTimeline covers the full source duration. Encoded video uses nominal
// cuts; audio-only producers must use BuildAudioTimeline for packet-aware tails.
// Copied video uses the first keyframe at or after each minimum
// segment duration; a long or irregular GOP therefore produces longer segments.
// The caller must supply validated, ordered keyframes from Keyframes. A first
// video keyframe after zero is allowed: segment zero also covers the container's
// initial audio lead-in. All subsequent boundaries are actual video keyframes.
func BuildTimeline(durationTicks int64, segmentSeconds int, keyframes []int64, copiedVideo bool) (Timeline, error) {
	if durationTicks <= 0 || durationTicks > maxDurationTicks || segmentSeconds < 1 || segmentSeconds > 10 {
		return Timeline{}, ErrInvalidTimeline
	}
	if len(keyframes) > maxTimelineKeys {
		return Timeline{}, ErrTimelineLimit
	}
	nominal := int64(segmentSeconds) * ticksPerSecond
	var cuts []int64
	if copiedVideo {
		if len(keyframes) == 0 {
			return Timeline{}, ErrUnsupportedTimeline
		}
		last := int64(-1)
		for _, key := range keyframes {
			if key < 0 || key >= durationTicks || key <= last {
				return Timeline{}, ErrUnsupportedTimeline
			}
			last = key
		}
		previous := int64(0)
		for _, key := range keyframes[1:] {
			if key-previous >= nominal {
				cuts = append(cuts, key)
				previous = key
				if len(cuts) >= MaxTimelineSegments {
					return Timeline{}, ErrTimelineLimit
				}
			}
		}
	} else {
		if (durationTicks+nominal-1)/nominal > MaxTimelineSegments {
			return Timeline{}, ErrTimelineLimit
		}
		for cut := nominal; cut < durationTicks; cut += nominal {
			cuts = append(cuts, cut)
		}
	}
	return timelineFromCuts(durationTicks, cuts), nil
}

func timelineFromCuts(durationTicks int64, cuts []int64) Timeline {
	result := Timeline{Segments: make([]TimelineSegment, 0, len(cuts)+1)}
	start := int64(0)
	for _, end := range append(cuts, durationTicks) {
		duration := end - start
		result.Segments = append(result.Segments, TimelineSegment{Number: len(result.Segments), StartTicks: start, DurationTicks: duration})
		if seconds := int((duration + ticksPerSecond - 1) / ticksPerSecond); seconds > result.TargetDuration {
			result.TargetDuration = seconds
		}
		start = end
	}
	return result
}

// SegmentAt returns the segment containing a source position. The exclusive
// end of the source, a negative position, and an empty timeline have no segment.
func (t Timeline) SegmentAt(ticks int64) (TimelineSegment, bool) {
	if ticks < 0 || len(t.Segments) == 0 {
		return TimelineSegment{}, false
	}
	index := sort.Search(len(t.Segments), func(i int) bool { return t.Segments[i].StartTicks > ticks }) - 1
	if index < 0 {
		return TimelineSegment{}, false
	}
	segment := t.Segments[index]
	if ticks-segment.StartTicks >= segment.DurationTicks {
		return TimelineSegment{}, false
	}
	return segment, true
}

// BoundaryTicks returns the internal source-time cuts for the inclusive segment
// range [first, last]. A producer starts at Segments[first].StartTicks and ends
// at Segments[last].StartTicks+DurationTicks; neither endpoint is a split point.
func (t Timeline) BoundaryTicks(first, last int) ([]int64, error) {
	if first < 0 || last < first || last >= len(t.Segments) {
		return nil, fmt.Errorf("%w: segment range", ErrInvalidTimeline)
	}
	cuts := make([]int64, 0, last-first)
	for index := first + 1; index <= last; index++ {
		cuts = append(cuts, t.Segments[index].StartTicks)
	}
	return cuts, nil
}

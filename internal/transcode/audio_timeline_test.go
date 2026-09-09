package transcode

import (
	"errors"
	"reflect"
	"testing"
)

func TestBuildAudioTimelineMergesEncodedRemainderWithoutClipping(t *testing.T) {
	for _, codec := range []string{"aac", "mp3"} {
		for _, duration := range []int64{60_010_000, 60_050_000, 60_200_000} {
			timeline, err := BuildAudioTimeline(duration, 3, AudioTimelineOptions{Codec: codec, SampleRate: 48000})
			if err != nil {
				t.Fatal(err)
			}
			want := []TimelineSegment{{Number: 0, StartTicks: 0, DurationTicks: 30_000_000}, {Number: 1, StartTicks: 30_000_000, DurationTicks: duration - 30_000_000}}
			if !reflect.DeepEqual(timeline.Segments, want) || timeline.TargetDuration != 4 {
				t.Fatalf("%s/%d: %+v", codec, duration, timeline)
			}
			// A final seek resolves within the existing final segment. There is
			// no separately declared tail URL that can later be remapped.
			segment, found := timeline.SegmentAt(60_000_000)
			if !found || segment.Number != 1 || segment.StartTicks != 30_000_000 {
				t.Fatalf("final seek: %+v, %t", segment, found)
			}
			cuts, err := timeline.BoundaryTicks(0, 1)
			if err != nil || !reflect.DeepEqual(cuts, []int64{30_000_000}) {
				t.Fatalf("producer cuts: %v, %v", cuts, err)
			}
			if _, found := timeline.SegmentAt(duration); found {
				t.Fatal("source end must remain exclusive")
			}
		}
	}
}

func TestBuildAudioTimelineUsesEncoderFrameSizeAndSampleRate(t *testing.T) {
	for _, check := range []struct {
		codec string
		rate  int
		frame int64
	}{
		{"aac", 0, 213_334}, {"aac", 8000, 1_280_000}, {"aac", 44100, 232_200}, {"aac", 96000, 106_667},
		{"mp3", 8000, 720_000}, {"mp3", 16000, 360_000}, {"mp3", 24000, 240_000},
		{"mp3", 32000, 360_000}, {"mp3", 44100, 261_225}, {"mp3", 48000, 240_000},
	} {
		options := AudioTimelineOptions{Codec: check.codec, SampleRate: check.rate}
		at, err := BuildAudioTimeline(60_000_000+check.frame, 3, options)
		if err != nil || len(at.Segments) != 2 {
			t.Fatalf("%s/%d exact frame: %+v, %v", check.codec, check.rate, at, err)
		}
		after, err := BuildAudioTimeline(60_000_001+check.frame, 3, options)
		if err != nil || len(after.Segments) != 3 || after.Segments[2].DurationTicks != check.frame+1 {
			t.Fatalf("%s/%d beyond frame: %+v, %v", check.codec, check.rate, after, err)
		}
	}
}

func TestBuildAudioTimelineCopyRequiresUsablePacketCoverage(t *testing.T) {
	// The original MP3 at 44.1 kHz ends at 6.000998 s after trimming. Its last
	// retained packet starts at 5.983106 s and spans the nominal six-second cut.
	last := int64(59_831_060)
	timeline, err := BuildAudioTimeline(60_009_980, 3, AudioTimelineOptions{Codec: "copy", LastPacketStartTicks: &last, MaxPacketDurationTicks: 261_225})
	if err != nil || len(timeline.Segments) != 2 || timeline.Segments[1].DurationTicks != 30_009_980 || timeline.TargetDuration != 4 {
		t.Fatalf("MP3 packet spans the final cut: %+v, %v", timeline, err)
	}
	// The exact phase allowance threshold is inclusive. A full packet of
	// space before the last reference packet proves the final cut is usable.
	last = 60_261_225
	at, err := BuildAudioTimeline(60_400_000, 3, AudioTimelineOptions{Codec: "copy", LastPacketStartTicks: &last, MaxPacketDurationTicks: 261_225})
	if err != nil || len(at.Segments) != 3 {
		t.Fatalf("known packet beyond allowance: %+v, %v", at, err)
	}
	last--
	before, err := BuildAudioTimeline(60_400_000, 3, AudioTimelineOptions{Codec: "copy", LastPacketStartTicks: &last, MaxPacketDurationTicks: 261_225})
	if err != nil || len(before.Segments) != 2 || before.Segments[1].DurationTicks != 30_400_000 {
		t.Fatalf("one tick below allowance: %+v, %v", before, err)
	}
	if last != 60_261_224 {
		t.Fatal("the caller's packet fact was changed")
	}
	zero := int64(0)
	one, err := BuildAudioTimeline(100_000, 3, AudioTimelineOptions{Codec: "copy", LastPacketStartTicks: &zero, MaxPacketDurationTicks: 240_000})
	if err != nil || len(one.Segments) != 1 || one.Segments[0].DurationTicks != 100_000 {
		t.Fatalf("known zero-origin single packet: %+v, %v", one, err)
	}
}

func TestBuildAudioTimelineCopyAllowsOnlyOneTickOfOutwardRounding(t *testing.T) {
	last := int64(60_000_000)
	options := AudioTimelineOptions{Codec: "copy", LastPacketStartTicks: &last, MaxPacketDurationTicks: 240_000}
	for _, extra := range []int64{0, 1, 2} {
		duration := last + options.MaxPacketDurationTicks + extra
		timeline, err := BuildAudioTimeline(duration, 3, options)
		if extra == 2 {
			if !errors.Is(err, ErrUnsupportedTimeline) {
				t.Fatalf("two excess ticks must remain unsupported: %v", err)
			}
			continue
		}
		if err != nil || len(timeline.Segments) != 2 || timeline.Segments[1].DurationTicks != duration-30_000_000 {
			t.Fatalf("outward rounding by %d ticks changed coverage: %+v, %v", extra, timeline, err)
		}
	}
	// Endpoint validation must precede subtraction even at the largest
	// permitted source duration; the allowance must not overflow a sum.
	last = 0
	options.MaxPacketDurationTicks = maxDurationTicks - 1
	timeline, err := BuildAudioTimeline(maxDurationTicks, 3, options)
	if err != nil || len(timeline.Segments) != 1 || timeline.Segments[0].DurationTicks != maxDurationTicks {
		t.Fatalf("maximum bounded duration with one rounding tick: %+v, %v", timeline, err)
	}
}

func TestBuildAudioTimelineRejectsUnknownAndInconsistentFacts(t *testing.T) {
	for _, options := range []AudioTimelineOptions{
		{Codec: ""}, {Codec: "opus", SampleRate: 48000}, {Codec: "AAC", SampleRate: 48000},
		{Codec: "aac", SampleRate: -1}, {Codec: "aac", SampleRate: 12345}, {Codec: "mp3", SampleRate: 96000},
		{Codec: "copy"}, {Codec: "copy", LastPacketStartTicks: audioTimelineTickPointer(0)},
		{Codec: "copy", LastPacketStartTicks: audioTimelineTickPointer(-1), MaxPacketDurationTicks: 20_000_000},
		{Codec: "copy", LastPacketStartTicks: audioTimelineTickPointer(10_000_000), MaxPacketDurationTicks: 240_000},
		{Codec: "copy", LastPacketStartTicks: audioTimelineTickPointer(100_000), MaxPacketDurationTicks: 100_000},
		{Codec: "copy", LastPacketStartTicks: audioTimelineTickPointer(0), MaxPacketDurationTicks: maxDurationTicks + 1},
	} {
		if _, err := BuildAudioTimeline(10_000_000, 3, options); !errors.Is(err, ErrUnsupportedTimeline) {
			t.Errorf("unsupported facts %+v: %v", options, err)
		}
	}
	for _, duration := range []int64{0, -1, maxDurationTicks + 1} {
		if _, err := BuildAudioTimeline(duration, 3, AudioTimelineOptions{Codec: "aac"}); !errors.Is(err, ErrInvalidTimeline) {
			t.Errorf("invalid duration %d: %v", duration, err)
		}
	}
	for _, seconds := range []int{0, 11} {
		if _, err := BuildAudioTimeline(10_000_000, seconds, AudioTimelineOptions{Codec: "aac"}); !errors.Is(err, ErrInvalidTimeline) {
			t.Errorf("invalid segment duration %d: %v", seconds, err)
		}
	}
}

func TestBuildAudioTimelineLimitsApplyAfterTailMerge(t *testing.T) {
	options := AudioTimelineOptions{Codec: "aac", SampleRate: 48000}
	duration := int64(MaxTimelineSegments)*30_000_000 + 10_000
	timeline, err := BuildAudioTimeline(duration, 3, options)
	if err != nil || len(timeline.Segments) != MaxTimelineSegments {
		t.Fatalf("last tail should fit after merging: count=%d, %v", len(timeline.Segments), err)
	}
	var covered int64
	for number, segment := range timeline.Segments {
		if segment.Number != number || segment.StartTicks != covered || segment.DurationTicks <= 0 {
			t.Fatalf("gap, overlap, or changed number at %+v", segment)
		}
		covered += segment.DurationTicks
	}
	if covered != duration {
		t.Fatalf("coverage %d differs from source %d", covered, duration)
	}
	if _, err := BuildAudioTimeline(duration+ticksPerSecond, 3, options); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("actual excessive segment count: %v", err)
	}
	for _, duration := range []int64{1, 30_000_000, 60_000_000} {
		timeline, err := BuildAudioTimeline(duration, 3, options)
		if err != nil {
			t.Fatal(err)
		}
		var total int64
		for _, segment := range timeline.Segments {
			total += segment.DurationTicks
		}
		if total != duration {
			t.Fatalf("short or aligned source coverage: %d != %d", total, duration)
		}
	}
}

func audioTimelineTickPointer(value int64) *int64 { return &value }

package transcode

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func liveCaptionTestPlan() Plan {
	p := commandPlan()
	p.SourceMode, p.DurationTicks, p.SegmentSeconds = "stream", 0, 2
	p.HLS.Subtitles.Count = 2
	p.HLS.Subtitles.Tracks[0] = HLSSubtitleTrack{StreamIndex: 2, Codec: "subrip"}
	p.HLS.Subtitles.Tracks[1] = HLSSubtitleTrack{StreamIndex: 3, Codec: "mov_text"}
	return p
}

func TestLiveCaptionCompanionUsesOneDemuxAndAnOrderedCopiedReference(t *testing.T) {
	p := liveCaptionTestPlan()
	for slot, container := range []string{"matroska", "mp4"} {
		args, err := BuildLiveCaptionCompanionArgs(p, slot, 6+slot)
		if err != nil {
			t.Fatal(err)
		}
		for _, pair := range [][2]string{{"-c", "copy"}, {"-reference_stream", "a:0"}, {"-segment_format", container}, {"-reset_timestamps", "0"}, {"-avoid_negative_ts", "disabled"}, {"-segment_time_delta", "0.000001"}} {
			if !hasArgumentPair(args, pair[0], pair[1]) {
				t.Fatalf("caption output lost its source-copy boundary: %v", args)
			}
		}
		if args[0] != "-map" || args[1] != "0:1" || args[2] != "-map" || args[3] != []string{"0:2", "0:3"}[slot] {
			t.Fatal("companion did not map reference first and the authorized source subtitle second")
		}
		for _, argument := range args {
			if argument == "-i" || argument == "-itsoffset" || argument == "-copyts" || strings.Contains(argument, "fix_sub_duration") || argument == "webvtt" {
				t.Fatal("caption companion added an input, repeated the clock offset or introduced asynchronous subtitle conversion")
			}
		}
		if container == "mp4" && !hasArgumentPair(args, "-segment_format_options", "use_editlist=1") {
			t.Fatal("timed text lost the per-track edit-list clock")
		}
	}
	p.AudioStreamIndex, p.AudioCodec, p.AudioBitrate, p.AudioChannels, p.AudioSampleRate = -1, "", 0, 0, 0
	args, err := BuildLiveCaptionCompanionArgs(p, 0, 6)
	if err != nil || !hasArgumentPair(args, "-reference_stream", "v:0") || !hasArgumentPair(args, "-bsf:v:0", "setts=pts=DTS") || !hasArgumentPair(args, "-segment_time_delta", "0.000001") {
		t.Fatalf("video-only reference exposed reordered presentation time as a completeness watermark: %v, %v", args, err)
	}
}

func TestLiveCaptionNamesAndAdmissionRemainPrivateAndBounded(t *testing.T) {
	p := liveCaptionTestPlan()
	for _, test := range []struct {
		slot     int
		sequence int64
		want     string
	}{{0, 0, "caption-s0-000000.mkv.tmp"}, {1, 1234567, "caption-s1-1234567.mp4.tmp"}} {
		name, err := LiveCaptionName(p, test.slot, test.sequence)
		if err != nil || name != test.want {
			t.Fatalf("private caption name = %q, %v", name, err)
		}
	}
	for _, sequence := range []int64{-1, maxLiveCaptionSequence + 1} {
		if _, err := LiveCaptionName(p, 0, sequence); !errors.Is(err, ErrInvalidPlan) {
			t.Fatal("caption filename accepted an invalid sequence")
		}
	}
	for _, fd := range []int{0, 1, 2, 3, 1024} {
		if _, err := BuildLiveCaptionCompanionArgs(p, 0, fd); !errors.Is(err, ErrInvalidPlan) {
			t.Fatal("caption output accepted a reserved or unbounded descriptor")
		}
	}
	p.HLS.Subtitles.Tracks[0].ExternalTag = strings.Repeat("a", 64)
	if _, err := BuildLiveCaptionCompanionArgs(p, 0, 6); !errors.Is(err, ErrInvalidPlan) {
		t.Fatal("an external provider track was treated as an input-demux stream")
	}
}

func TestLiveCaptionGlobalClockDoesNotApplyFiniteMediaDurationLimit(t *testing.T) {
	start := int64(45*24*60*60) * ticksPerSecond
	segment := LiveCaptionSegment{Slot: 0, Sequence: 1, StartTicks: start, EndTicks: start + 2*ticksPerSecond,
		File: &os.File{}, SubtitleStreamIndex: 1, Codec: "subrip", Container: "matroska"}
	if !validLiveCaptionSegment(segment) {
		t.Fatal("a short completed live segment was rejected because of its accumulated source clock")
	}
	segment.EndTicks = -1
	if validLiveCaptionSegment(segment) {
		t.Fatal("an overflowing or backward live source clock was accepted")
	}
}

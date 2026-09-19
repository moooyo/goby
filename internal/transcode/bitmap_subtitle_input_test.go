package transcode

import (
	"slices"
	"strings"
	"testing"
)

func TestBitmapSubtitleInputKeepsAuthorizedSourceAndClock(t *testing.T) {
	for _, test := range []struct {
		mode   string
		origin int64
		offset string
	}{
		{"hls", 0, "-2.0000000"},
		{"progressive", 5 * ticksPerSecond, "-5.0000000"},
		{"progressive", -ticksPerSecond, "1.0000000"},
	} {
		p := commandPlan()
		p.StartTicks = 2 * ticksPerSecond
		p.Subtitle = SubtitlePlan{Mode: "burn", Codec: "hdmv_pgs_subtitle", StreamIndex: 2, OffsetTicks: -ticksPerSecond / 2}
		if test.mode == "progressive" {
			p.OutputMode, p.Container, p.SegmentSeconds = "progressive", "mp4", 0
			p.SourceFormatStartKnown, p.SourceFormatStartTicks = true, test.origin
		}
		args, err := BuildArgs(p, 1)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(args, " ")
		if strings.Count(joined, "-i /proc/self/fd/3") != 2 ||
			!strings.Contains(joined, "-discard:v all -discard:a all -itsoffset "+test.offset+" -i /proc/self/fd/3") ||
			!strings.Contains(joined, "[1:2]settb=AVTB,setpts=PTS+(-0.5000000)/TB") ||
			!hasArgumentPair(args, "-map", "0:1") {
			t.Fatalf("bitmap input changed source authority, audio selection, or source clock: %v", args)
		}
		for index, argument := range args {
			if argument == "-i" && (index+1 >= len(args) || args[index+1] != "/proc/self/fd/3") {
				t.Fatalf("bitmap input introduced a different source: %v", args)
			}
		}
	}
}

func TestBitmapSubtitleInputRetainsVerifiedSeekAudioMapping(t *testing.T) {
	p := commandPlan()
	p.OutputMode, p.Container, p.SegmentSeconds, p.SourceFormatStartKnown = "progressive", "mp4", 0, true
	p.StartTicks, p.SourceFormatStartTicks = 2*ticksPerSecond, 5*ticksPerSecond
	p.Subtitle = SubtitlePlan{Mode: "burn", Codec: "hdmv_pgs_subtitle", StreamIndex: 2}
	args := buildProgressiveVideoArgsWithSeek(p, 1, ticksPerSecond)
	if strings.Count(strings.Join(args, " "), "-i /proc/self/fd/3") != 3 || !hasArgumentPair(args, "-map", "2:1") ||
		!strings.Contains(strings.Join(args, " "), "[1:2]settb=AVTB") || !hasArgumentPair(args, "-ss", "6.0000000") {
		t.Fatalf("adding bitmap decoding changed verified video seek or linear audio input: %v", args)
	}
	p.Subtitle = SubtitlePlan{}
	args = buildProgressiveVideoArgsWithSeek(p, 1, ticksPerSecond)
	if strings.Count(strings.Join(args, " "), "-i /proc/self/fd/3") != 2 || !hasArgumentPair(args, "-map", "1:1") {
		t.Fatalf("ordinary seek/audio mapping changed without a bitmap selection: %v", args)
	}
}

func TestBitmapSubtitleGPUPlaneUsesVideoCadenceAndMicrosecondEvents(t *testing.T) {
	p := commandPlan()
	p.Subtitle = SubtitlePlan{Mode: "burn", Codec: "hdmv_pgs_subtitle", StreamIndex: 2}
	p.VideoFilters.Backend = "vulkan"
	p.Hardware = Hardware{Decode: "vaapi", Encode: "vaapi"}
	args, err := BuildArgs(p, 1)
	if err != nil {
		t.Fatal(err)
	}
	graph := args[slices.Index(args, "-filter_complex")+1]
	if !strings.Contains(graph, "[goby_blank]settb=AVTB,") || !strings.Contains(graph, "[1:2]settb=AVTB,") ||
		!strings.Contains(graph, "[goby_subtitle_clock][goby_bitmap]overlay=eof_action=pass:shortest=0:repeatlast=0") ||
		!strings.Contains(graph, "[goby_canvas][goby_subtitle]libplacebo=inputs=2:") ||
		strings.Contains(graph, "[goby_canvas][goby_bitmap]overlay") {
		t.Fatalf("bitmap events bypassed the primary clock or CPU blended the main video: %s", graph)
	}
}
